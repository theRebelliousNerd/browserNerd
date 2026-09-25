package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"browsernerd-mcp-server/internal/docker"
)

// backendLogSource yields backend log lines between two instants.
// *docker.Client is the production source; tests inject a fake.
type backendLogSource interface {
	LogsBetween(ctx context.Context, since, until time.Time) ([]docker.LogEntry, error)
	LogWindow() time.Duration
}

const (
	// A dead daemon fails its CLI call in about 0.35 s here; the cap keeps a
	// wedged one from costing more than a second.
	backendQueryTimeout = 900 * time.Millisecond
	// After a failed query, Docker is reported unavailable without asking
	// again for this long, so repeated diagnoses cost nothing.
	backendDownCooldown   = 60 * time.Second
	backendMaxLines       = 5
	backendErrorWindow    = 2 * time.Second
	defaultBackendWindow  = 30 * time.Second
	backendLineMaxBytes   = 220
	backendUnavailableTag = "unavailable: "
)

// backendCorrelator attaches the backend log lines that belong to a failed
// request, and remembers an unreachable Docker.
type backendCorrelator struct {
	mu        sync.Mutex
	downUntil time.Time
	downNote  string
}

// linesFor returns the matching lines as short strings, or one short note:
// "unavailable: <why>" when Docker cannot answer, or "none mention <path>".
func (b *backendCorrelator) linesFor(ctx context.Context, src backendLogSource, c *netCall) interface{} {
	b.mu.Lock()
	if time.Now().Before(b.downUntil) {
		note := b.downNote
		b.mu.Unlock()
		return note
	}
	b.mu.Unlock()

	at := time.UnixMilli(c.doneMs())
	if c.doneMs() <= 0 {
		at = time.Now()
	}
	window := src.LogWindow()
	if window <= 0 {
		window = defaultBackendWindow
	}
	qctx, cancel := context.WithTimeout(ctx, backendQueryTimeout)
	defer cancel()
	logs, err := src.LogsBetween(qctx, at.Add(-window), at.Add(window))
	if err != nil {
		note := backendUnavailableTag + docker.ShortError(err)
		b.mu.Lock()
		b.downUntil = time.Now().Add(backendDownCooldown)
		b.downNote = note
		b.mu.Unlock()
		return note
	}
	path := requestPath(c.url)
	matches := docker.MatchRequestLines(logs, path, at, window, backendErrorWindow, backendMaxLines)
	if len(matches) == 0 {
		return fmt.Sprintf("none mention %s within %s", path, window)
	}
	lines := make([]string, 0, len(matches))
	for _, m := range matches {
		lines = append(lines, formatBackendLine(m))
	}
	return lines
}

// formatBackendLine is "container hh:mm:ss.mmm text": a traceback keeps its
// last line (the exception), everything is redacted and clipped.
func formatBackendLine(m docker.LogMatch) string {
	text := m.Entry.Message
	if text == "" {
		text = m.Entry.Raw
	}
	if m.Entry.Tag == "TRACEBACK" || strings.Contains(text, "\n") {
		parts := strings.Split(strings.TrimSpace(text), "\n")
		text = strings.TrimSpace(parts[len(parts)-1])
	}
	stamp := ""
	if !m.Entry.Timestamp.IsZero() {
		stamp = m.Entry.Timestamp.Local().Format("15:04:05.000") + " "
	}
	return m.Entry.Container + " " + stamp + redactSnippet(text, backendLineMaxBytes)
}

// backendSource is the tool's log source: an injected one, else the
// configured Docker client, else none.
func (t *BrowserReasonTool) backendSource() backendLogSource {
	if t.backendLogs != nil {
		return t.backendLogs
	}
	if t.dockerClient != nil {
		return t.dockerClient
	}
	return nil
}
