package docker

import (
	"strings"
	"testing"
	"time"
)

func TestParseLogs(t *testing.T) {
	client := NewClient([]string{"test-container"}, 30*time.Second, "")

	tests := []struct {
		name          string
		input         string
		expectedCount int
		checkFirst    func(LogEntry) bool
	}{
		{
			name: "Docker timestamp with Loguru format",
			input: `2025-11-26T04:15:44.461522993Z 2025-11-26 04:15:44.461 | INFO     | api.main:<module>:586 - [STARTUP] Request signature verification disabled
2025-11-26T04:15:44.592412799Z 2025-11-26 04:15:44.591 | ERROR    | api.main:<module>:132 - Something went wrong`,
			expectedCount: 2,
			checkFirst: func(e LogEntry) bool {
				return e.Level == "INFO" && strings.Contains(e.Message, "STARTUP")
			},
		},
		{
			name:          "Simple tagged format",
			input:         `2025-11-26T04:15:44.461522993Z [STARTUP] GraphRAG router added`,
			expectedCount: 1,
			checkFirst: func(e LogEntry) bool {
				return e.Tag == "STARTUP" && e.Message == "GraphRAG router added"
			},
		},
		{
			name:          "Python level format",
			input:         `2025-11-26T04:15:44.461522993Z ERROR: Database connection failed`,
			expectedCount: 1,
			checkFirst: func(e LogEntry) bool {
				return e.Level == "ERROR" && e.Message == "Database connection failed"
			},
		},
		{
			name: "Python traceback - captures KeyError",
			input: `2025-11-26T04:15:44.461522993Z Traceback (most recent call last):
2025-11-26T04:15:44.461522993Z   File "/app/main.py", line 42, in handler
2025-11-26T04:15:44.461522993Z     result = process(data)
2025-11-26T04:15:44.461522993Z KeyError: 'tenant_id'`,
			expectedCount: 3, // Traceback start, continuation lines, and KeyError
			checkFirst: func(e LogEntry) bool {
				// First entry is the traceback with continuation
				return e.Level == "ERROR" && e.Tag == "TRACEBACK"
			},
		},
		{
			name:          "Next.js error format",
			input:         `2025-11-26T04:15:44.461522993Z - error TypeError: Cannot read property 'map' of undefined`,
			expectedCount: 1,
			checkFirst: func(e LogEntry) bool {
				return e.Level == "ERROR" && e.Tag == "NEXTJS"
			},
		},
		{
			name:          "Next.js warn format",
			input:         `2025-11-26T04:15:44.461522993Z - warn Fast Refresh had to perform a full reload`,
			expectedCount: 1,
			checkFirst: func(e LogEntry) bool {
				return e.Level == "WARNING" && e.Tag == "NEXTJS"
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := client.parseLogs("test-container", tt.input)

			if len(entries) != tt.expectedCount {
				t.Errorf("expected %d entries, got %d", tt.expectedCount, len(entries))
				for i, e := range entries {
					t.Logf("Entry %d: Level=%s, Tag=%s, Message=%s", i, e.Level, e.Tag, e.Message)
				}
				return
			}

			if tt.checkFirst != nil && len(entries) > 0 {
				if !tt.checkFirst(entries[0]) {
					t.Errorf("first entry check failed: Level=%s, Tag=%s, Message=%s",
						entries[0].Level, entries[0].Tag, entries[0].Message)
				}
			}
		})
	}
}

func TestFilterErrors(t *testing.T) {
	client := NewClient([]string{"test"}, 30*time.Second, "")

	logs := []LogEntry{
		{Level: "INFO", Message: "Starting up"},
		{Level: "ERROR", Message: "Something broke"},
		{Level: "WARNING", Message: "Watch out"},
		{Level: "DEBUG", Message: "Debugging"},
		{Level: "CRITICAL", Message: "Fatal error"},
	}

	filtered := client.FilterErrors(logs)

	if len(filtered) != 3 {
		t.Errorf("expected 3 error/warning entries, got %d", len(filtered))
	}

	// Should include ERROR, WARNING, and CRITICAL
	levels := make(map[string]bool)
	for _, l := range filtered {
		levels[l.Level] = true
	}

	if !levels["ERROR"] || !levels["WARNING"] || !levels["CRITICAL"] {
		t.Error("filtered results should include ERROR, WARNING, and CRITICAL")
	}
}

func TestAnalyzeHealth(t *testing.T) {
	client := NewClient([]string{"backend", "frontend"}, 30*time.Second, "")

	logs := []LogEntry{
		{Container: "backend", Level: "ERROR", Message: "Error 1"},
		{Container: "backend", Level: "ERROR", Message: "Error 2"},
		{Container: "backend", Level: "WARNING", Message: "Warning 1"},
		{Container: "frontend", Level: "INFO", Message: "All good"},
	}

	health := client.AnalyzeHealth(logs)

	if health["backend"].Status != "degraded" {
		t.Errorf("backend should be degraded with 2 errors, got %s", health["backend"].Status)
	}

	if health["backend"].ErrorCount != 2 {
		t.Errorf("backend should have 2 errors, got %d", health["backend"].ErrorCount)
	}

	if health["frontend"].Status != "healthy" {
		t.Errorf("frontend should be healthy, got %s", health["frontend"].Status)
	}
}

func TestAnalyzeHealthUnhealthy(t *testing.T) {
	client := NewClient([]string{"backend"}, 30*time.Second, "")

	// Create 6 errors to trigger unhealthy status (threshold is >5)
	logs := []LogEntry{
		{Container: "backend", Level: "ERROR", Message: "Error 1"},
		{Container: "backend", Level: "ERROR", Message: "Error 2"},
		{Container: "backend", Level: "ERROR", Message: "Error 3"},
		{Container: "backend", Level: "ERROR", Message: "Error 4"},
		{Container: "backend", Level: "ERROR", Message: "Error 5"},
		{Container: "backend", Level: "ERROR", Message: "Error 6"},
	}

	health := client.AnalyzeHealth(logs)

	if health["backend"].Status != "unhealthy" {
		t.Errorf("backend should be unhealthy with 6 errors, got %s", health["backend"].Status)
	}

	if health["backend"].ErrorCount != 6 {
		t.Errorf("backend should have 6 errors, got %d", health["backend"].ErrorCount)
	}
}

func TestAnalyzeHealthCritical(t *testing.T) {
	client := NewClient([]string{"backend"}, 30*time.Second, "")

	// CRITICAL level should count as error
	logs := []LogEntry{
		{Container: "backend", Level: "CRITICAL", Message: "Critical error"},
	}

	health := client.AnalyzeHealth(logs)

	if health["backend"].ErrorCount != 1 {
		t.Errorf("CRITICAL should count as error, got count %d", health["backend"].ErrorCount)
	}
}

func TestAnalyzeHealthManyWarnings(t *testing.T) {
	client := NewClient([]string{"backend"}, 30*time.Second, "")

	// 11 warnings should trigger degraded status
	logs := []LogEntry{}
	for i := 0; i < 11; i++ {
		logs = append(logs, LogEntry{Container: "backend", Level: "WARNING", Message: "Warning"})
	}

	health := client.AnalyzeHealth(logs)

	if health["backend"].Status != "degraded" {
		t.Errorf("backend should be degraded with 11 warnings, got %s", health["backend"].Status)
	}
}

func TestFilterByLevel(t *testing.T) {
	client := NewClient([]string{"test"}, 30*time.Second, "")

	logs := []LogEntry{
		{Level: "INFO", Message: "Info message"},
		{Level: "ERROR", Message: "Error message 1"},
		{Level: "ERROR", Message: "Error message 2"},
		{Level: "WARNING", Message: "Warning message"},
		{Level: "DEBUG", Message: "Debug message"},
	}

	// Test filtering by ERROR
	errorLogs := client.FilterByLevel(logs, "ERROR")
	if len(errorLogs) != 2 {
		t.Errorf("expected 2 ERROR entries, got %d", len(errorLogs))
	}

	// Test filtering by INFO
	infoLogs := client.FilterByLevel(logs, "INFO")
	if len(infoLogs) != 1 {
		t.Errorf("expected 1 INFO entry, got %d", len(infoLogs))
	}

	// Test filtering by non-existent level
	criticalLogs := client.FilterByLevel(logs, "CRITICAL")
	if len(criticalLogs) != 0 {
		t.Errorf("expected 0 CRITICAL entries, got %d", len(criticalLogs))
	}
}

func TestFilterByContainer(t *testing.T) {
	client := NewClient([]string{"backend", "frontend"}, 30*time.Second, "")

	logs := []LogEntry{
		{Container: "backend", Level: "INFO", Message: "Backend message 1"},
		{Container: "backend", Level: "ERROR", Message: "Backend error"},
		{Container: "frontend", Level: "INFO", Message: "Frontend message"},
		{Container: "worker", Level: "WARNING", Message: "Worker warning"},
	}

	// Test filtering by backend
	backendLogs := client.FilterByContainer(logs, "backend")
	if len(backendLogs) != 2 {
		t.Errorf("expected 2 backend entries, got %d", len(backendLogs))
	}

	// Test filtering by frontend
	frontendLogs := client.FilterByContainer(logs, "frontend")
	if len(frontendLogs) != 1 {
		t.Errorf("expected 1 frontend entry, got %d", len(frontendLogs))
	}

	// Test filtering by non-existent container
	dbLogs := client.FilterByContainer(logs, "database")
	if len(dbLogs) != 0 {
		t.Errorf("expected 0 database entries, got %d", len(dbLogs))
	}
}

func TestInferLevelFromMessage(t *testing.T) {
	client := NewClient([]string{"test"}, 30*time.Second, "")

	tests := []struct {
		name          string
		input         string
		expectedLevel string
	}{
		// Error patterns
		{"error keyword", "This is an error message", "ERROR"},
		{"exception keyword", "Exception occurred", "ERROR"},
		{"failed keyword", "Connection failed", "ERROR"},
		{"failure keyword", "Database failure", "ERROR"},
		{"traceback keyword", "Traceback in code", "ERROR"},
		{"critical keyword", "Critical system error", "ERROR"},
		{"fatal keyword", "Fatal error occurred", "ERROR"},
		{"panic keyword", "Panic: something went wrong", "ERROR"},
		{"crash keyword", "Application crash detected", "ERROR"},
		{"keyerror", "KeyError: 'missing_key'", "ERROR"},
		{"typeerror", "TypeError: invalid type", "ERROR"},
		{"valueerror", "ValueError: bad value", "ERROR"},
		{"attributeerror", "AttributeError: no attribute", "ERROR"},
		{"connectionerror", "ConnectionError: refused", "ERROR"},
		{"timeout", "Request timeout", "ERROR"},
		{"refused", "Connection refused", "ERROR"},
		{"denied", "Access denied", "ERROR"},

		// Warning patterns
		{"warning keyword", "Warning: deprecated API", "WARNING"},
		{"warn keyword", "Warn: slow query", "WARNING"},
		{"deprecated keyword", "Using deprecated function", "WARNING"},
		{"slow keyword", "Slow query detected", "WARNING"},
		{"retry keyword", "Retry attempt 3", "WARNING"},
		{"fallback keyword", "Using fallback method", "WARNING"},
		{"degraded keyword", "Service degraded", "WARNING"},
		{"skipping keyword", "Skipping invalid record", "WARNING"},
		{"missing keyword", "Missing configuration", "WARNING"},

		// Info patterns (no error or warning keywords)
		{"normal message", "Starting server on port 8000", "INFO"},
		{"status message", "All systems operational", "INFO"},
		{"startup message", "Application initialized", "INFO"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse a log line to trigger inferLevelFromMessage
			entries := client.parseLogs("test", tt.input)
			if len(entries) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(entries))
			}
			if entries[0].Level != tt.expectedLevel {
				t.Errorf("expected level %s for %q, got %s", tt.expectedLevel, tt.input, entries[0].Level)
			}
		})
	}
}

func TestParseLogsTracebackContinuation(t *testing.T) {
	client := NewClient([]string{"test"}, 30*time.Second, "")

	// Test traceback with File continuation - each line needs Docker timestamp
	input := `2025-11-26T04:15:44.461522993Z Traceback (most recent call last):
2025-11-26T04:15:44.461522993Z   File "/app/main.py", line 42, in handler
2025-11-26T04:15:44.461522993Z     result = process(data)
2025-11-26T04:15:44.461522993Z File "/app/utils.py", line 10, in process
2025-11-26T04:15:44.461522993Z     return data["key"]
2025-11-26T04:15:44.461522993Z KeyError: 'key'`

	entries := client.parseLogs("test", input)

	// Should capture a traceback entry
	var foundTraceback bool
	for _, e := range entries {
		if e.Tag == "TRACEBACK" && e.Level == "ERROR" {
			foundTraceback = true
		}
	}
	if !foundTraceback {
		t.Error("expected to find a TRACEBACK entry")
	}
}

func TestParseLogsTracebackNoException(t *testing.T) {
	client := NewClient([]string{"test"}, 30*time.Second, "")

	// Test traceback that ends without a standard exception line
	input := `Traceback (most recent call last):
  File "/app/main.py", line 42, in handler
    result = process(data)
Some other line that doesn't match`

	entries := client.parseLogs("test", input)

	// Should still capture the traceback even without standard exception
	var foundTraceback bool
	for _, e := range entries {
		if e.Tag == "TRACEBACK" {
			foundTraceback = true
		}
	}
	if !foundTraceback {
		t.Error("expected to find a TRACEBACK entry even without exception line")
	}
}

func TestParseLogsTracebackAtEnd(t *testing.T) {
	client := NewClient([]string{"test"}, 30*time.Second, "")

	// Test traceback at end of output (remaining traceback handling)
	input := `Traceback (most recent call last):
  File "/app/main.py", line 42, in handler`

	entries := client.parseLogs("test", input)

	// Should emit the remaining traceback
	if len(entries) == 0 {
		t.Error("expected at least one entry for traceback at end")
	}
}

func TestParseLogsEmptyInput(t *testing.T) {
	client := NewClient([]string{"test"}, 30*time.Second, "")

	entries := client.parseLogs("test", "")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for empty input, got %d", len(entries))
	}
}

func TestParseLogsWhitespaceOnly(t *testing.T) {
	client := NewClient([]string{"test"}, 30*time.Second, "")

	entries := client.parseLogs("test", "   \n\t\n   ")
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for whitespace-only input, got %d", len(entries))
	}
}

func TestParseLogsNextjsEvent(t *testing.T) {
	client := NewClient([]string{"test"}, 30*time.Second, "")

	tests := []struct {
		input         string
		expectedLevel string
	}{
		{"- event compiled successfully", "INFO"},
		{"- wait compiling...", "INFO"},
		{"- ready started server", "INFO"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			entries := client.parseLogs("test", tt.input)
			if len(entries) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(entries))
			}
			if entries[0].Level != tt.expectedLevel {
				t.Errorf("expected level %s, got %s", tt.expectedLevel, entries[0].Level)
			}
			if entries[0].Tag != "NEXTJS" {
				t.Errorf("expected tag NEXTJS, got %s", entries[0].Tag)
			}
		})
	}
}

func TestParseLogsBracketedTags(t *testing.T) {
	client := NewClient([]string{"test"}, 30*time.Second, "")

	tests := []struct {
		input         string
		expectedTag   string
		expectedLevel string
	}{
		{"[ERROR] Something went wrong", "ERROR", "ERROR"},
		{"[CRITICAL] Database down", "CRITICAL", "ERROR"},
		{"[FATAL] System crash", "FATAL", "ERROR"},
		{"[EXCEPTION] Unhandled exception", "EXCEPTION", "ERROR"},
		{"[WARNING] Deprecated API", "WARNING", "WARNING"},
		{"[WARN] Slow query", "WARN", "WARNING"},
		{"[STARTUP] Server started", "STARTUP", "INFO"},
		{"[AUDIT] User login", "AUDIT", "INFO"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			entries := client.parseLogs("test", tt.input)
			if len(entries) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(entries))
			}
			if entries[0].Tag != tt.expectedTag {
				t.Errorf("expected tag %s, got %s", tt.expectedTag, entries[0].Tag)
			}
			if entries[0].Level != tt.expectedLevel {
				t.Errorf("expected level %s, got %s", tt.expectedLevel, entries[0].Level)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	containers := []string{"backend", "frontend", "worker"}
	window := 60 * time.Second
	host := "tcp://localhost:2375"

	client := NewClient(containers, window, host)

	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if len(client.containers) != 3 {
		t.Errorf("expected 3 containers, got %d", len(client.containers))
	}
	if client.logWindow != window {
		t.Errorf("expected logWindow %v, got %v", window, client.logWindow)
	}
	if client.host != host {
		t.Errorf("expected host %s, got %s", host, client.host)
	}
}

func TestParseLogsAlternateTimestampFormat(t *testing.T) {
	client := NewClient([]string{"test"}, 30*time.Second, "")

	// Test alternate timestamp format without nanoseconds
	input := "2025-11-26T04:15:44Z [STARTUP] Server started"

	entries := client.parseLogs("test", input)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Tag != "STARTUP" {
		t.Errorf("expected tag STARTUP, got %s", entries[0].Tag)
	}
}
