package browser

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

// TestLiveBrowserSessionManager tests the SessionManager with a real browser.
// These tests require Chrome to be installed and will actually launch browser instances.
func TestLiveBrowserSessionManager(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create a mock engine sink for fact collection
	sink := &mockEngineSink{facts: make([]mangle.Fact, 0)}

	cfg := config.BrowserConfig{
		Headless:              boolPtr(true),
		EnableDOMIngestion:    true,
		EnableHeaderIngestion: true,
		EventThrottleMs:       50,
		EventLoggingLevel:     "verbose",
	}

	manager := NewSessionManager(cfg, sink)

	// Start the browser
	t.Run("Start", func(t *testing.T) {
		err := manager.Start(ctx)
		if err != nil {
			t.Fatalf("Failed to start browser: %v", err)
		}

		if !manager.IsConnected() {
			t.Error("Expected browser to be connected")
		}

		if manager.ControlURL() == "" {
			t.Error("Expected non-empty control URL")
		}
	})

	// Ensure cleanup
	defer func() {
		if err := manager.Shutdown(ctx); err != nil {
			t.Logf("Shutdown warning: %v", err)
		}
	}()

	var sessionID string

	t.Run("CreateSession", func(t *testing.T) {
		session, err := manager.CreateSession(ctx, "about:blank")
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		if session.ID == "" {
			t.Error("Expected session ID to be set")
		}
		if session.Status != "active" {
			t.Errorf("Expected status 'active', got %q", session.Status)
		}

		sessionID = session.ID
	})

	t.Run("List", func(t *testing.T) {
		sessions := manager.List()
		if len(sessions) == 0 {
			t.Error("Expected at least one session")
		}

		found := false
		for _, s := range sessions {
			if s.ID == sessionID {
				found = true
				break
			}
		}
		if !found {
			t.Error("Created session not found in list")
		}
	})

	t.Run("GetSession", func(t *testing.T) {
		session, found := manager.GetSession(sessionID)
		if !found {
			t.Fatal("Session not found")
		}
		if session.ID != sessionID {
			t.Errorf("Expected ID %q, got %q", sessionID, session.ID)
		}
	})

	t.Run("Page", func(t *testing.T) {
		page, found := manager.Page(sessionID)
		if !found {
			t.Fatal("Page not found")
		}
		if page == nil {
			t.Error("Expected non-nil page")
		}
	})

	t.Run("Registry", func(t *testing.T) {
		registry := manager.Registry(sessionID)
		if registry == nil {
			t.Error("Expected non-nil registry")
		}
	})

	t.Run("UpdateMetadata", func(t *testing.T) {
		manager.UpdateMetadata(sessionID, func(s Session) Session {
			s.Title = "Test Title"
			return s
		})

		session, found := manager.GetSession(sessionID)
		if !found {
			t.Fatal("Session not found after update")
		}
		if session.Title != "Test Title" {
			t.Errorf("Expected title 'Test Title', got %q", session.Title)
		}
	})

	t.Run("NavigateAndCaptureEvents", func(t *testing.T) {
		page, _ := manager.Page(sessionID)
		if page == nil {
			t.Skip("No page available")
		}

		// Navigate to a data URL
		dataURL := "data:text/html,<html><head><title>Test</title></head><body><h1>Hello</h1></body></html>"
		err := page.Navigate(dataURL)
		if err != nil {
			t.Fatalf("Navigate failed: %v", err)
		}

		// Wait a bit for events to be captured
		time.Sleep(500 * time.Millisecond)

		// Check if navigation facts were captured
		if len(sink.facts) == 0 {
			t.Log("No facts captured yet (this may be timing-dependent)")
		}
	})

	t.Run("SnapshotDOM", func(t *testing.T) {
		err := manager.SnapshotDOM(ctx, sessionID)
		if err != nil {
			t.Fatalf("SnapshotDOM failed: %v", err)
		}

		// Check for DOM facts
		hasDOMFacts := false
		for _, f := range sink.facts {
			if f.Predicate == "dom_node" || f.Predicate == "dom_text" || f.Predicate == "dom_attr" {
				hasDOMFacts = true
				break
			}
		}
		if !hasDOMFacts {
			t.Log("No DOM facts captured (may be empty page)")
		}
	})

	t.Run("ForkSession", func(t *testing.T) {
		forked, err := manager.ForkSession(ctx, sessionID, "about:blank")
		if err != nil {
			t.Fatalf("ForkSession failed: %v", err)
		}

		if forked.ID == "" {
			t.Error("Expected forked session ID")
		}
		if forked.ID == sessionID {
			t.Error("Forked session should have different ID")
		}
		if forked.Status != "forked" {
			t.Errorf("Expected status 'forked', got %q", forked.Status)
		}
	})

	t.Run("SnapshotDOMNonExistent", func(t *testing.T) {
		err := manager.SnapshotDOM(ctx, "nonexistent-session")
		if err == nil {
			t.Error("Expected error for non-existent session")
		}
	})

	t.Run("Shutdown", func(t *testing.T) {
		err := manager.Shutdown(ctx)
		if err != nil {
			t.Fatalf("Shutdown failed: %v", err)
		}

		if manager.IsConnected() {
			t.Error("Expected browser to be disconnected after shutdown")
		}

		if manager.ControlURL() != "" {
			t.Error("Expected control URL to be empty after shutdown")
		}

		// Sessions should be cleared
		sessions := manager.List()
		if len(sessions) != 0 {
			t.Errorf("Expected no sessions after shutdown, got %d", len(sessions))
		}
	})
}

// TestLiveReifyReact tests React Fiber extraction with a real browser.
func TestLiveReifyReact(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sink := &mockEngineSink{facts: make([]mangle.Fact, 0)}

	cfg := config.BrowserConfig{
		Headless: boolPtr(true),
	}

	manager := NewSessionManager(cfg, sink)
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start browser: %v", err)
	}
	defer manager.Shutdown(ctx)

	// Create a session with a simple React-like page
	session, err := manager.CreateSession(ctx, "about:blank")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	page, _ := manager.Page(session.ID)
	if page == nil {
		t.Fatal("No page available")
	}

	// Navigate to a page that simulates React (without actual React, just to test the function runs)
	_ = page.Navigate("data:text/html,<html><div id='root'><button>Click</button></div></html>")
	time.Sleep(200 * time.Millisecond)

	// ReifyReact should not crash even without React
	facts, err := manager.ReifyReact(ctx, session.ID)
	if err != nil {
		t.Logf("ReifyReact returned error (expected for non-React page): %v", err)
	} else {
		t.Logf("ReifyReact returned %d facts", len(facts))
	}
}

// TestLiveReifyReactNoEngine tests ReifyReact with nil engine.
func TestLiveReifyReactNoEngine(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create manager without engine
	cfg := config.BrowserConfig{
		Headless: boolPtr(true),
	}

	manager := NewSessionManager(cfg, nil)
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start browser: %v", err)
	}
	defer manager.Shutdown(ctx)

	session, err := manager.CreateSession(ctx, "about:blank")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// ReifyReact should fail with no engine
	_, err = manager.ReifyReact(ctx, session.ID)
	if err == nil {
		t.Error("Expected error for nil engine")
	}
	if err.Error() != "mangle engine not configured" {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestLiveAttach tests attaching to an existing target.
func TestLiveAttach(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sink := &mockEngineSink{facts: make([]mangle.Fact, 0)}

	cfg := config.BrowserConfig{
		Headless: boolPtr(true),
	}

	manager := NewSessionManager(cfg, sink)
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start browser: %v", err)
	}
	defer manager.Shutdown(ctx)

	// Create a session first
	session, err := manager.CreateSession(ctx, "about:blank")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Attach to the same target
	if session.TargetID != "" {
		attached, err := manager.Attach(ctx, session.TargetID)
		if err != nil {
			t.Fatalf("Attach failed: %v", err)
		}

		if attached.Status != "attached" {
			t.Errorf("Expected status 'attached', got %q", attached.Status)
		}
	}
}

// TestSessionPersistence tests persisting and loading sessions.
func TestSessionPersistence(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create temp file for session store
	tmpDir := t.TempDir()
	sessionFile := filepath.Join(tmpDir, "sessions.json")

	sink := &mockEngineSink{facts: make([]mangle.Fact, 0)}

	cfg := config.BrowserConfig{
		Headless:     boolPtr(true),
		SessionStore: sessionFile,
	}

	manager := NewSessionManager(cfg, sink)
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start browser: %v", err)
	}

	// Create a session
	session, err := manager.CreateSession(ctx, "about:blank")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Shutdown to persist
	if err := manager.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// Verify session file exists
	if _, err := os.Stat(sessionFile); os.IsNotExist(err) {
		t.Fatal("Session file was not created")
	}

	// Read the file content
	data, err := os.ReadFile(sessionFile)
	if err != nil {
		t.Fatalf("Failed to read session file: %v", err)
	}

	var sessions []Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		t.Fatalf("Failed to parse session file: %v", err)
	}

	// Verify the session was persisted
	found := false
	for _, s := range sessions {
		if s.ID == session.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("Session was not found in persisted file")
	}

	// Create new manager and verify it loads sessions
	manager2 := NewSessionManager(cfg, sink)
	if err := manager2.Start(ctx); err != nil {
		t.Fatalf("Failed to start second browser: %v", err)
	}
	defer manager2.Shutdown(ctx)

	// Check loaded sessions (should be marked as detached)
	loadedSessions := manager2.List()
	foundLoaded := false
	for _, s := range loadedSessions {
		if s.ID == session.ID {
			foundLoaded = true
			if s.Status != "detached" {
				t.Errorf("Expected loaded session status 'detached', got %q", s.Status)
			}
			break
		}
	}
	if !foundLoaded {
		t.Error("Session was not loaded from file")
	}
}

// TestLoadSessionsInvalidJSON tests loadSessions with invalid JSON.
func TestLoadSessionsInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	sessionFile := filepath.Join(tmpDir, "sessions.json")

	// Write invalid JSON
	if err := os.WriteFile(sessionFile, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("Failed to write file: %v", err)
	}

	cfg := config.BrowserConfig{
		SessionStore: sessionFile,
	}

	manager := NewSessionManager(cfg, nil)
	err := manager.loadSessions()
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}

// TestLoadSessionsNoFile tests loadSessions when file doesn't exist.
func TestLoadSessionsNoFile(t *testing.T) {
	cfg := config.BrowserConfig{
		SessionStore: "/nonexistent/path/sessions.json",
	}

	manager := NewSessionManager(cfg, nil)
	err := manager.loadSessions()
	// Should not error for non-existent file
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestLoadSessionsEmptyPath tests loadSessions with empty path.
func TestLoadSessionsEmptyPath(t *testing.T) {
	cfg := config.BrowserConfig{
		SessionStore: "",
	}

	manager := NewSessionManager(cfg, nil)
	err := manager.loadSessions()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestPersistSessionsEmptyPath tests persistSessions with empty path.
func TestPersistSessionsEmptyPath(t *testing.T) {
	cfg := config.BrowserConfig{
		SessionStore: "",
	}

	manager := NewSessionManager(cfg, nil)
	err := manager.persistSessions()
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

// TestStringifyConsoleArgs tests the stringifyConsoleArgs helper function.
func TestStringifyConsoleArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []*proto.RuntimeRemoteObject
		expected string
	}{
		{
			name:     "nil args",
			args:     nil,
			expected: "",
		},
		{
			name:     "empty args",
			args:     []*proto.RuntimeRemoteObject{},
			expected: "",
		},
		{
			name:     "single nil arg",
			args:     []*proto.RuntimeRemoteObject{nil},
			expected: "",
		},
		{
			name: "single string value",
			args: []*proto.RuntimeRemoteObject{
				{Value: gson.New("hello")},
			},
			expected: "hello",
		},
		{
			name: "multiple values",
			args: []*proto.RuntimeRemoteObject{
				{Value: gson.New("hello")},
				{Value: gson.New("world")},
			},
			expected: "hello world",
		},
		{
			name: "with description fallback",
			args: []*proto.RuntimeRemoteObject{
				{Description: "Error: something went wrong"},
			},
			expected: "Error: something went wrong",
		},
		{
			name: "mixed values and descriptions",
			args: []*proto.RuntimeRemoteObject{
				{Value: gson.New("log")},
				{Description: "Object"},
			},
			expected: "log Object",
		},
		{
			name: "number value",
			args: []*proto.RuntimeRemoteObject{
				{Value: gson.New(42)},
			},
			expected: "42",
		},
		{
			name: "boolean value",
			args: []*proto.RuntimeRemoteObject{
				{Value: gson.New(true)},
			},
			expected: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stringifyConsoleArgs(tt.args)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// TestForkSessionNonExistent tests forking a non-existent session.
func TestForkSessionNonExistent(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	sink := &mockEngineSink{facts: make([]mangle.Fact, 0)}

	cfg := config.BrowserConfig{
		Headless: boolPtr(true),
	}

	manager := NewSessionManager(cfg, sink)
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start browser: %v", err)
	}
	defer manager.Shutdown(ctx)

	_, err := manager.ForkSession(ctx, "nonexistent-session-id", "about:blank")
	if err == nil {
		t.Error("Expected error for non-existent session")
	}
}

// TestReifyReactNonExistentSession tests ReifyReact with non-existent session.
func TestReifyReactNonExistentSession(t *testing.T) {
	sink := &mockEngineSink{facts: make([]mangle.Fact, 0)}

	cfg := config.BrowserConfig{}

	manager := NewSessionManager(cfg, sink)

	ctx := context.Background()
	_, err := manager.ReifyReact(ctx, "nonexistent-session")
	if err == nil {
		t.Error("Expected error for non-existent session")
	}
}

// TestBrowserReconnect tests browser reconnection after stale connection.
func TestBrowserReconnect(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sink := &mockEngineSink{facts: make([]mangle.Fact, 0)}

	cfg := config.BrowserConfig{
		Headless: boolPtr(true),
	}

	manager := NewSessionManager(cfg, sink)

	// Start browser
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("First start failed: %v", err)
	}

	// Starting again should reuse existing browser
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Second start failed: %v", err)
	}

	// Should still be connected
	if !manager.IsConnected() {
		t.Error("Expected browser to be connected after restart")
	}

	manager.Shutdown(ctx)
}

// Helper types and functions

type mockEngineSink struct {
	facts []mangle.Fact
}

func (m *mockEngineSink) AddFacts(ctx context.Context, facts []mangle.Fact) error {
	m.facts = append(m.facts, facts...)
	return nil
}

func boolPtr(b bool) *bool {
	return &b
}
