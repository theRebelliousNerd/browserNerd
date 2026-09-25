package docker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// LogMatch is one backend log line picked for a failed browser request, and
// why it was picked: "path" when its text names the request path, "time" when
// it is an error line logged right beside the request.
type LogMatch struct {
	Entry  LogEntry `json:"entry"`
	Reason string   `json:"reason"`
}

// LogWindow is the configured distance, either side of a browser event, that
// backend log lines are searched for it.
func (c *Client) LogWindow() time.Duration {
	if c == nil {
		return 0
	}
	return c.logWindow
}

// LogsBetween reads every configured container's lines in [since, until],
// querying the containers concurrently. Unlike QueryLogs it reports failure:
// when no container answers it returns an error naming why (daemon down,
// container missing, CLI absent), so a caller can say "unavailable" instead
// of presenting silence as "no backend errors".
func (c *Client) LogsBetween(ctx context.Context, since, until time.Time) ([]LogEntry, error) {
	if c == nil || len(c.containers) == 0 {
		return nil, errors.New("no containers configured")
	}
	type result struct {
		logs []LogEntry
		err  error
	}
	results := make([]result, len(c.containers))
	var wg sync.WaitGroup
	for i, container := range c.containers {
		wg.Add(1)
		go func(i int, container string) {
			defer wg.Done()
			logs, err := c.queryContainerBetween(ctx, container, since, until)
			results[i] = result{logs: logs, err: err}
		}(i, container)
	}
	wg.Wait()

	var all []LogEntry
	var firstErr error
	answered := 0
	for _, r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		answered++
		all = append(all, r.logs...)
	}
	if answered == 0 && firstErr != nil {
		return nil, firstErr
	}
	return all, nil
}

func (c *Client) queryContainerBetween(ctx context.Context, container string, since, until time.Time) ([]LogEntry, error) {
	args := []string{"logs", "--timestamps", "--since", since.UTC().Format(time.RFC3339Nano)}
	if !until.IsZero() {
		args = append(args, "--until", until.UTC().Format(time.RFC3339Nano))
	}
	if c.host != "" {
		args = append([]string{"-H", c.host}, args...)
	}
	args = append(args, container)
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("docker logs %s: %w", container, ctxErr)
		}
		return nil, fmt.Errorf("docker logs %s: %w: %s", container, err, strings.TrimSpace(string(output)))
	}
	return c.parseLogs(container, string(output)), nil
}

// ShortError turns a docker CLI failure into a few words an agent can act on.
func ShortError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case errors.Is(err, context.DeadlineExceeded) || strings.Contains(lower, "deadline exceeded"):
		return "docker did not answer in time"
	case errors.Is(err, exec.ErrNotFound) || strings.Contains(lower, "executable file not found"):
		return "docker CLI not installed"
	case strings.Contains(lower, "failed to connect") || strings.Contains(lower, "cannot connect") ||
		strings.Contains(lower, "daemon is running") || strings.Contains(lower, "pipe/docker") ||
		strings.Contains(lower, "connection refused") || strings.Contains(lower, "error during connect") ||
		strings.Contains(lower, "actively refused"):
		return "docker daemon not reachable"
	case strings.Contains(lower, "no such container"):
		return "container not found"
	case strings.Contains(lower, "no containers configured"):
		return "no containers configured"
	}
	if len(msg) > 80 {
		msg = msg[:80]
	}
	return msg
}

// MatchRequestLines picks the backend lines that belong to one failed browser
// request made at `at`: lines within window of it whose text contains path,
// nearest first, then ERROR/CRITICAL lines within errorWindow of it (the
// traceback a failing handler logs beside its access line), up to max lines.
func MatchRequestLines(logs []LogEntry, path string, at time.Time, window, errorWindow time.Duration, max int) []LogMatch {
	if max <= 0 || len(logs) == 0 {
		return nil
	}
	needles := requestPathNeedles(path)
	distance := func(e LogEntry) time.Duration {
		d := e.Timestamp.Sub(at)
		if d < 0 {
			d = -d
		}
		return d
	}

	type candidate struct {
		match LogMatch
		dist  time.Duration
		rank  int
	}
	seen := make(map[string]bool)
	candidates := make([]candidate, 0, 8)
	for _, entry := range logs {
		d := distance(entry)
		text := entry.Message
		if text == "" {
			text = entry.Raw
		}
		key := entry.Container + "|" + entry.Timestamp.String() + "|" + text
		if seen[key] {
			continue
		}
		if d <= window && containsAny(text, entry.Raw, needles) {
			seen[key] = true
			candidates = append(candidates, candidate{match: LogMatch{Entry: entry, Reason: "path"}, dist: d, rank: 0})
			continue
		}
		if errorWindow > 0 && d <= errorWindow && (entry.Level == "ERROR" || entry.Level == "CRITICAL") {
			seen[key] = true
			candidates = append(candidates, candidate{match: LogMatch{Entry: entry, Reason: "time"}, dist: d, rank: 1})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return candidates[i].dist < candidates[j].dist
	})
	if len(candidates) > max {
		candidates = candidates[:max]
	}
	out := make([]LogMatch, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.match)
	}
	return out
}

// requestPathNeedles is the request path, and the same path without its first
// segment when a frontend proxy prefixes what the backend sees (/api/v1/x
// reaching the backend as /v1/x).
func requestPathNeedles(path string) []string {
	path = strings.TrimSpace(path)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if path == "" || path == "/" {
		return nil
	}
	needles := []string{path}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) >= 3 {
		needles = append(needles, "/"+strings.Join(segments[1:], "/"))
	}
	return needles
}

func containsAny(text, raw string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(text, n) || strings.Contains(raw, n) {
			return true
		}
	}
	return false
}
