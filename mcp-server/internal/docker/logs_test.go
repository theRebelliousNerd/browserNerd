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
