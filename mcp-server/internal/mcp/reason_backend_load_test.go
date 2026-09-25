package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/mangle"
)

// TestReasonAsksTheBackendAboutAPageThatNeverLoaded is the tunnel failure
// from the uplift notes: no request failed in a way the browser could see,
// the tab sits on chrome-error, and the reason exists only in the origin's
// log. why_failed must ask the backend about that URL at that moment.
func TestReasonAsksTheBackendAboutAPageThatNeverLoaded(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	now := time.Now()
	failed := now.Add(-2 * time.Second)
	facts := []mangle.Fact{
		{Predicate: "navigation_event", Args: []interface{}{"s-load", "http://app.local:3001/dashboard", failed.UnixMilli()}, Timestamp: failed},
		{Predicate: "page_load_failed", Args: []interface{}{"s-load", "http://app.local:3001/dashboard", "net::ERR_CONNECTION_REFUSED", failed.UnixMilli()}, Timestamp: failed},
		{Predicate: "current_url", Args: []interface{}{"s-load", "chrome-error://chromewebdata/"}, Timestamp: failed},
	}
	if err := engine.AddFacts(context.Background(), facts); err != nil {
		t.Fatal(err)
	}
	src := &fakeLogs{logs: []docker.LogEntry{
		{Container: "cloudflared", Timestamp: failed.Add(-5 * time.Millisecond), Level: "ERROR", Message: `malformed HTTP response "Unauthorized" originService=http://cross-thread-frontend:9003 path=/dashboard`},
		{Container: "cross-thread-backend", Timestamp: failed.Add(20 * time.Second), Level: "INFO", Message: `"GET /api/poll HTTP/1.1" 200`},
	}}
	tool := &BrowserReasonTool{engine: engine, backendLogs: src}

	res := runReason(t, tool, map[string]interface{}{"session_id": "s-load", "topic": "why_failed"})
	root := asMap(res["root_cause"])
	if root["kind"] != "page_load_failed" {
		t.Fatalf("the page load failure must be the root cause: %v", res)
	}
	lines := toStringSlice(root["backend_logs"])
	if len(lines) != 1 || !strings.Contains(lines[0], `malformed HTTP response "Unauthorized"`) {
		t.Fatalf("the origin's log line about /dashboard must be attached, got %v", root["backend_logs"])
	}
}
