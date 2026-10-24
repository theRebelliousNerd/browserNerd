// Package docker provides Docker log querying and parsing for full-stack error correlation.
package docker

import (
	"browsernerd-mcp-server/internal/logs"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// Client shells out to docker logs for container log retrieval.
type Client struct {
	containers []string
	logWindow  time.Duration
	host       string
	// query fetches one container's logs; NewClient wires queryContainer.
	query   func(ctx context.Context, container string, since time.Time) ([]logs.Entry, error)
	command func(context.Context, ...string) *exec.Cmd
}

// NewClient creates a Docker log client.
func NewClient(containers []string, logWindow time.Duration, host string) *Client {
	c := &Client{
		containers: containers,
		logWindow:  logWindow,
		host:       host,
	}
	c.query = c.queryContainer
	c.command = func(ctx context.Context, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "docker", args...)
	}
	return c
}

// OwnsSource identifies entries emitted by this configured Docker transport.
func (c *Client) OwnsSource(name string) bool {
	if c == nil {
		return false
	}
	for _, container := range c.containers {
		if container == name {
			return true
		}
	}
	return false
}

// maxContainerErrorBytes bounds each container's share of a QueryLogs error.
const maxContainerErrorBytes = 300

// QueryLogs fetches logs from all configured containers since the given time.
// One container failing still returns the others' logs. Every container
// failing is an error: an empty result with Docker stopped or every name wrong
// would otherwise read as "the backend logged nothing".
func (c *Client) QueryLogs(ctx context.Context, since time.Time) ([]logs.Entry, error) {
	var allLogs []logs.Entry
	var failures []error

	for _, container := range c.containers {
		entries, err := c.query(ctx, container, since)
		allLogs = append(allLogs, entries...)
		if err != nil {
			failures = append(failures, &limitedError{cause: err})
		}
	}

	if len(c.containers) > 0 && len(failures) == len(c.containers) && len(allLogs) == 0 {
		return nil, fmt.Errorf("docker logs failed for every container (%d): %w", len(failures), errors.Join(failures...))
	}
	if len(failures) > 0 {
		return allLogs, fmt.Errorf("docker logs incomplete: %w", errors.Join(failures...))
	}
	return allLogs, nil
}

// boundText trims s to at most limit bytes on a rune boundary.
func boundText(s string, limit int) string {
	s = strings.TrimSpace(s)
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// queryContainer fetches one bounded container log window.
func (c *Client) queryContainer(ctx context.Context, container string, since time.Time) ([]logs.Entry, error) {
	return c.queryContainerBetween(ctx, container, since, time.Time{})
}

// FilterErrors returns only ERROR and WARNING level logs.
func (c *Client) FilterErrors(entries []logs.Entry) []logs.Entry {
	var errors []logs.Entry
	for _, log := range entries {
		if log.Level == "ERROR" || log.Level == "WARNING" || log.Level == "CRITICAL" {
			errors = append(errors, log)
		}
	}
	return errors
}

// FilterByLevel returns logs matching the specified level.
func (c *Client) FilterByLevel(entries []logs.Entry, level string) []logs.Entry {
	var filtered []logs.Entry
	for _, log := range entries {
		if log.Level == level {
			filtered = append(filtered, log)
		}
	}
	return filtered
}

// FilterByContainer returns logs from a specific container.
func (c *Client) FilterByContainer(entries []logs.Entry, container string) []logs.Entry {
	var filtered []logs.Entry
	for _, log := range entries {
		if log.Source == container {
			filtered = append(filtered, log)
		}
	}
	return filtered
}

// ContainerHealth summarizes the health of each container based on log analysis.
type ContainerHealth struct {
	Container    string `json:"container"`
	ErrorCount   int    `json:"error_count"`
	WarningCount int    `json:"warning_count"`
	Status       string `json:"status"` // unassessed, healthy, degraded, unhealthy
}

// AnalyzeHealth returns health status for each container.
func (c *Client) AnalyzeHealth(entries []logs.Entry) map[string]ContainerHealth {
	health := make(map[string]ContainerHealth)

	// Initialize all configured containers
	for _, container := range c.containers {
		health[container] = ContainerHealth{
			Container: container,
			Status:    "unassessed",
		}
	}

	// Count errors and warnings per container
	for _, log := range entries {
		h := health[log.Source]
		h.Container = log.Source
		h.Status = "healthy"
		switch log.Level {
		case "ERROR", "CRITICAL":
			h.ErrorCount++
		case "WARNING":
			h.WarningCount++
		}
		health[log.Source] = h
	}

	// Determine status based on counts
	for container, h := range health {
		if h.Status == "unassessed" {
			continue
		}
		if h.ErrorCount > 5 {
			h.Status = "unhealthy"
		} else if h.ErrorCount > 0 || h.WarningCount > 10 {
			h.Status = "degraded"
		}
		health[container] = h
	}

	return health
}

// limitedError bounds transport diagnostics while retaining typed causes.
type limitedError struct{ cause error }

func (e *limitedError) Error() string { return boundText(e.cause.Error(), maxContainerErrorBytes) }
func (e *limitedError) Unwrap() error { return e.cause }
