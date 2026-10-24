package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/logs"
)

const (
	// Bound log evidence collection so a slow source cannot stall diagnosis.
	backendQueryTimeout   = 900 * time.Millisecond
	backendMaxLines       = 5
	backendErrorWindow    = 2 * time.Second
	defaultBackendWindow  = 30 * time.Second
	backendLineMaxBytes   = 220
	backendUnavailableTag = "unavailable: "
)

// backendCorrelator attaches shared source evidence to a failed request.
// Sources are queried afresh so append, rotation and recovery stay observable.
type backendCorrelator struct{}

// linesFor returns matching evidence and explicit collection limitations.
func (b *backendCorrelator) linesFor(ctx context.Context, src logs.Source, c *netCall) interface{} {
	at := time.UnixMilli(c.doneMs())
	if c.doneMs() <= 0 {
		return backendUnavailableTag + "browser request has no recorded timestamp"
	}
	window := src.LogWindow()
	if window <= 0 {
		window = defaultBackendWindow
	}
	qctx, cancel := context.WithTimeout(ctx, backendQueryTimeout)
	defer cancel()
	entries, err := src.LogsBetween(qctx, at.Add(-window), at.Add(window))
	if err != nil && len(entries) == 0 {
		return backendUnavailableTag + backendLogError(err)
	}
	path := requestPath(c.url)
	matches := logs.MatchRequestLines(entries, path, at, window, backendErrorWindow, backendMaxLines)
	if len(matches) == 0 && err == nil {
		return fmt.Sprintf("none mention %s within %s", path, window)
	}
	lines := make([]string, 0, len(matches))
	for _, m := range matches {
		lines = append(lines, formatBackendLine(m))
	}
	if err != nil {
		lines = append(lines, "partial: "+backendLogError(err))
	}
	return lines
}

// formatBackendLine is "source hh:mm:ss.mmm text": a traceback keeps its
// last line (the exception), everything is redacted and clipped.
func formatBackendLine(m logs.Match) string {
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
	return m.Entry.Source + " " + stamp + redactSnippet(text, backendLineMaxBytes)
}

// backendSource is the configured shared source.
func (t *BrowserReasonTool) backendSource() logs.Source { return t.backendLogs }

func backendLogError(err error) string {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		parts := make([]string, 0, len(joined.Unwrap()))
		for _, child := range joined.Unwrap() {
			parts = append(parts, backendLogError(child))
		}
		return redactSnippet(strings.Join(parts, "; "), 600)
	}
	if issue, ok := err.(*logs.Issue); ok {
		text := fmt.Sprintf("log source %s: %s", issue.Source, issue.Kind)
		if issue.Err != nil {
			text += ": " + backendLogError(issue.Err)
		}
		return redactSnippet(text, 600)
	}
	text := err.Error()
	if strings.Contains(strings.ToLower(text), "docker") {
		text = docker.ShortError(err)
	}
	return redactSnippet(text, 600)
}
