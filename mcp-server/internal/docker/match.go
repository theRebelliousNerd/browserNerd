package docker

import (
	"browsernerd-mcp-server/internal/logs"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
)

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
func (c *Client) LogsBetween(ctx context.Context, since, until time.Time) ([]logs.Entry, error) {
	if c == nil || len(c.containers) == 0 {
		return nil, errors.New("no containers configured")
	}
	type result struct {
		logs []logs.Entry
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

	var all []logs.Entry
	var failures []error
	for _, r := range results {
		all = append(all, r.logs...)
		if r.err != nil {
			failures = append(failures, r.err)
		}
	}
	return all, errors.Join(failures...)
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
