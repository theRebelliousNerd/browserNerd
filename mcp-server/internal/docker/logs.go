// Package docker provides Docker log querying and parsing for full-stack error correlation.
package docker

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// LogEntry represents a parsed Docker log line.
type LogEntry struct {
	Container string    `json:"container"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`   // ERROR, WARNING, INFO, DEBUG
	Tag       string    `json:"tag"`     // [STARTUP], [AUDIT], [LIFESPAN], etc.
	Message   string    `json:"message"` // The actual log message
	Raw       string    `json:"raw"`     // Original unparsed line
}

// Client shells out to docker logs for container log retrieval.
type Client struct {
	containers []string
	logWindow  time.Duration
	host       string
}

// NewClient creates a Docker log client.
func NewClient(containers []string, logWindow time.Duration, host string) *Client {
	return &Client{
		containers: containers,
		logWindow:  logWindow,
		host:       host,
	}
}

// QueryLogs fetches logs from all configured containers since the given time.
func (c *Client) QueryLogs(ctx context.Context, since time.Time) ([]LogEntry, error) {
	var allLogs []LogEntry

	for _, container := range c.containers {
		logs, err := c.queryContainer(ctx, container, since)
		if err != nil {
			// Log warning but continue with other containers
			continue
		}
		allLogs = append(allLogs, logs...)
	}

	return allLogs, nil
}

// QueryContainer fetches logs from a single container.
func (c *Client) queryContainer(ctx context.Context, container string, since time.Time) ([]LogEntry, error) {
	args := []string{"logs", "--timestamps"}

	// Add --since flag
	args = append(args, "--since", since.Format(time.RFC3339))

	// Add host if specified
	if c.host != "" {
		args = append([]string{"-H", c.host}, args...)
	}

	args = append(args, container)

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker logs %s: %w (output: %s)", container, err, string(output))
	}

	return c.parseLogs(container, string(output)), nil
}

// parseLogs parses Docker log output into structured entries.
// Handles multiple formats:
// 1. Docker timestamp prefix: "2025-01-25T12:03:45.123456789Z message"
// 2. Bracketed tag format: "[TAG] message"
// 3. Python logging: "LEVEL: message"
// 4. Next.js format: "- event compiled successfully" or "- error ..."
func (c *Client) parseLogs(container string, output string) []LogEntry {
	var entries []LogEntry

	// Docker timestamp pattern: RFC3339Nano at start of line
	dockerTsPattern := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}T[\d:.]+Z?)\s+(.*)$`)

	// Bracketed tag pattern: [TAG] message
	tagPattern := regexp.MustCompile(`^\[([A-Z_]+)\]\s+(.*)$`)

	// Python level pattern: LEVEL: message or level | message
	levelPattern := regexp.MustCompile(`^(ERROR|WARNING|INFO|DEBUG|CRITICAL):\s*(.*)$`)
	logurPattern := regexp.MustCompile(`^.*\|\s*(ERROR|WARNING|INFO|DEBUG)\s*\|\s*(.*)$`)

	// Next.js pattern: - event/error/warn message
	nextjsPattern := regexp.MustCompile(`^-\s+(error|warn|event|wait|ready)\s+(.*)$`)

	// Python traceback detection
	tracebackStart := regexp.MustCompile(`^Traceback \(most recent call last\):`)
	exceptionLine := regexp.MustCompile(`^(\w+Error|\w+Exception):\s*(.*)$`)

	scanner := bufio.NewScanner(strings.NewReader(output))
	var currentTraceback strings.Builder
	inTraceback := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		entry := LogEntry{
			Container: container,
			Timestamp: time.Now(),
			Level:     "INFO",
			Raw:       line,
		}

		remaining := line

		// Extract Docker timestamp if present
		if matches := dockerTsPattern.FindStringSubmatch(line); len(matches) == 3 {
			if ts, err := time.Parse(time.RFC3339Nano, matches[1]); err == nil {
				entry.Timestamp = ts
			} else if ts, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", matches[1]); err == nil {
				entry.Timestamp = ts
			}
			remaining = matches[2]
		}

		// Handle Python tracebacks - collect multi-line
		if tracebackStart.MatchString(remaining) {
			inTraceback = true
			currentTraceback.Reset()
			currentTraceback.WriteString(remaining)
			continue
		}

		if inTraceback {
			if exceptionLine.MatchString(remaining) {
				// End of traceback - emit as single error
				currentTraceback.WriteString("\n")
				currentTraceback.WriteString(remaining)
				entry.Level = "ERROR"
				entry.Tag = "TRACEBACK"
				entry.Message = currentTraceback.String()
				entries = append(entries, entry)
				inTraceback = false
				currentTraceback.Reset()
				continue
			} else if strings.HasPrefix(remaining, " ") || strings.HasPrefix(remaining, "\t") || strings.HasPrefix(remaining, "File ") {
				// Continuation of traceback
				currentTraceback.WriteString("\n")
				currentTraceback.WriteString(remaining)
				continue
			} else {
				// Traceback ended without exception line - emit what we have
				if currentTraceback.Len() > 0 {
					entry.Level = "ERROR"
					entry.Tag = "TRACEBACK"
					entry.Message = currentTraceback.String()
					entries = append(entries, entry)
				}
				inTraceback = false
				currentTraceback.Reset()
				// Fall through to process current line
			}
		}

		// Try bracketed tag format: [TAG] message
		if matches := tagPattern.FindStringSubmatch(remaining); len(matches) == 3 {
			entry.Tag = matches[1]
			entry.Message = matches[2]
			entry.Level = inferLevelFromTag(matches[1], matches[2])
			entries = append(entries, entry)
			continue
		}

		// Try Python level format: ERROR: message
		if matches := levelPattern.FindStringSubmatch(remaining); len(matches) == 3 {
			entry.Level = strings.ToUpper(matches[1])
			entry.Message = matches[2]
			entries = append(entries, entry)
			continue
		}

		// Try Loguru format: timestamp | level | message
		if matches := logurPattern.FindStringSubmatch(remaining); len(matches) == 3 {
			entry.Level = strings.ToUpper(matches[1])
			entry.Message = matches[2]
			entries = append(entries, entry)
			continue
		}

		// Try Next.js format: - event/error/warn message
		if matches := nextjsPattern.FindStringSubmatch(remaining); len(matches) == 3 {
			entry.Tag = "NEXTJS"
			entry.Level = inferLevelFromNextjs(matches[1])
			entry.Message = matches[2]
			entries = append(entries, entry)
			continue
		}

		// Check for common error patterns in message
		entry.Level = inferLevelFromMessage(remaining)
		entry.Message = remaining
		entries = append(entries, entry)
	}

	// Handle any remaining traceback
	if inTraceback && currentTraceback.Len() > 0 {
		entries = append(entries, LogEntry{
			Container: container,
			Timestamp: time.Now(),
			Level:     "ERROR",
			Tag:       "TRACEBACK",
			Message:   currentTraceback.String(),
			Raw:       currentTraceback.String(),
		})
	}

	return entries
}

// inferLevelFromTag determines log level from bracketed log tags.
func inferLevelFromTag(tag, message string) string {
	// Some tags are inherently error-level
	errorTags := map[string]bool{
		"ERROR": true, "CRITICAL": true, "FATAL": true, "EXCEPTION": true,
	}
	warningTags := map[string]bool{
		"WARNING": true, "WARN": true,
	}

	if errorTags[tag] {
		return "ERROR"
	}
	if warningTags[tag] {
		return "WARNING"
	}

	// Also check message content
	return inferLevelFromMessage(message)
}

// inferLevelFromNextjs maps Next.js event types to log levels.
func inferLevelFromNextjs(eventType string) string {
	switch strings.ToLower(eventType) {
	case "error":
		return "ERROR"
	case "warn":
		return "WARNING"
	default:
		return "INFO"
	}
}

// inferLevelFromMessage guesses log level from message content.
func inferLevelFromMessage(message string) string {
	msg := strings.ToLower(message)

	// Strong error indicators
	errorPatterns := []string{
		"error", "exception", "failed", "failure", "traceback",
		"critical", "fatal", "panic", "crash", "segfault",
		"keyerror", "typeerror", "valueerror", "attributeerror",
		"connectionerror", "timeout", "refused", "denied",
	}
	for _, pattern := range errorPatterns {
		if strings.Contains(msg, pattern) {
			return "ERROR"
		}
	}

	// Warning indicators
	warningPatterns := []string{
		"warning", "warn", "deprecated", "slow", "retry",
		"fallback", "degraded", "skipping", "missing",
	}
	for _, pattern := range warningPatterns {
		if strings.Contains(msg, pattern) {
			return "WARNING"
		}
	}

	return "INFO"
}

// FilterErrors returns only ERROR and WARNING level logs.
func (c *Client) FilterErrors(logs []LogEntry) []LogEntry {
	var errors []LogEntry
	for _, log := range logs {
		if log.Level == "ERROR" || log.Level == "WARNING" || log.Level == "CRITICAL" {
			errors = append(errors, log)
		}
	}
	return errors
}

// FilterByLevel returns logs matching the specified level.
func (c *Client) FilterByLevel(logs []LogEntry, level string) []LogEntry {
	var filtered []LogEntry
	for _, log := range logs {
		if log.Level == level {
			filtered = append(filtered, log)
		}
	}
	return filtered
}

// FilterByContainer returns logs from a specific container.
func (c *Client) FilterByContainer(logs []LogEntry, container string) []LogEntry {
	var filtered []LogEntry
	for _, log := range logs {
		if log.Container == container {
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
	Status       string `json:"status"` // healthy, degraded, unhealthy
}

// AnalyzeHealth returns health status for each container.
func (c *Client) AnalyzeHealth(logs []LogEntry) map[string]ContainerHealth {
	health := make(map[string]ContainerHealth)

	// Initialize all configured containers
	for _, container := range c.containers {
		health[container] = ContainerHealth{
			Container: container,
			Status:    "healthy",
		}
	}

	// Count errors and warnings per container
	for _, log := range logs {
		h := health[log.Container]
		h.Container = log.Container
		switch log.Level {
		case "ERROR", "CRITICAL":
			h.ErrorCount++
		case "WARNING":
			h.WarningCount++
		}
		health[log.Container] = h
	}

	// Determine status based on counts
	for container, h := range health {
		if h.ErrorCount > 5 {
			h.Status = "unhealthy"
		} else if h.ErrorCount > 0 || h.WarningCount > 10 {
			h.Status = "degraded"
		}
		health[container] = h
	}

	return health
}
