package mcp

import (
	"context"
	"os"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

func TestLiveAttachSession(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	progressiveOnly := false
	cfg := config.Config{
		Server: config.ServerConfig{
			Name:    "test-server",
			Version: "1.0.0",
		},
		Browser: config.BrowserConfig{
			Headless: boolPtr(true),
		},
		MCP: config.MCPConfig{
			ProgressiveOnly: &progressiveOnly,
		},
		Mangle: config.MangleConfig{
			Enable:          true,
			SchemaPath:      "../../schemas/browser.mg",
			FactBufferLimit: 1000,
		},
	}

	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)
	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Launch browser. If not configured/available, skip the entire live test.
	if _, err := server.ExecuteTool("launch-browser", nil); err != nil {
		t.Skipf("launch-browser failed (Chrome not available or not configured): %v", err)
	}
	defer server.ExecuteTool("shutdown-browser", nil)

	// Create session
	result, _ := server.ExecuteTool("create-session", map[string]interface{}{
		"url": "about:blank",
	})
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected create-session result map, got %T", result)
	}
	session, ok := resultMap["session"].(*browser.Session)
	if !ok {
		t.Fatalf("expected created session, got %T", resultMap["session"])
	}

	// Attach to the same target
	_ = ctx // Use context
	attachResult, err := server.ExecuteTool("attach-session", map[string]interface{}{
		"target_id": session.TargetID,
	})
	if err != nil {
		t.Fatalf("attach-session failed: %v", err)
	}

	attachMap, ok := attachResult.(map[string]interface{})
	if !ok {
		t.Fatalf("expected attach-session result map, got %T", attachResult)
	}
	attached, ok := attachMap["session"].(*browser.Session)
	if !ok {
		t.Fatalf("expected attached session, got %T", attachMap["session"])
	}
	if attached.Status != "attached" {
		t.Errorf("Expected status 'attached', got %q", attached.Status)
	}
}

// TestWrapToolWithError tests the wrapTool function error path.
func TestWrapToolWithError(t *testing.T) {
	progressiveOnly := false
	cfg := config.Config{
		Server: config.ServerConfig{
			Name:    "test-server",
			Version: "1.0.0",
		},
		MCP: config.MCPConfig{
			ProgressiveOnly: &progressiveOnly,
		},
		Mangle: config.MangleConfig{
			Enable:          true,
			SchemaPath:      "../../schemas/browser.mg",
			FactBufferLimit: 1000,
		},
	}

	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	sessions := browser.NewSessionManager(cfg.Browser, engine)
	server, err := NewServer(cfg, sessions, engine)
	if err != nil {
		t.Fatalf("NewServer failed: %v", err)
	}

	// Try to execute a tool that will fail
	ctx := context.Background()
	tool := server.tools["reify-react"]
	handle := authorizeReason(t, engine, "nonexistent-session")
	_, err = tool.Execute(ctx, map[string]interface{}{
		"session_id":         "nonexistent-session",
		"approved_by_handle": handle,
	})
	if err == nil {
		t.Error("Expected error for nonexistent session")
	}
}

func boolPtr(b bool) *bool {
	return &b
}
