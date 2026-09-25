package mcp

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

// liveDataPage opens html in headless Chrome with a real engine behind it.
func liveDataPage(t *testing.T, ctx context.Context, html string) (*BrowserActTool, string) {
	t.Helper()
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping live browser tests (SKIP_LIVE_TESTS set)")
	}
	engine, err := mangle.NewEngine(config.MangleConfig{Enable: true, FactBufferLimit: 4096})
	if err != nil {
		t.Fatal(err)
	}
	headless := true
	manager := browser.NewSessionManager(config.BrowserConfig{Headless: &headless}, engine)
	if err := manager.Start(ctx); err != nil {
		t.Skipf("Failed to start browser (Chrome not available or not configured): %v", err)
	}
	t.Cleanup(func() { _ = manager.Shutdown(context.Background()) })
	session, err := manager.CreateSession(ctx, "data:text/html,"+url.PathEscape(html))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	return &BrowserActTool{sessions: manager, engine: engine}, session.ID
}

// TestLiveTextLocatorLooksPastAHiddenExactMatch: react.dev renders a hidden
// mobile "Search" button and a visible "Search Ctrl K" one. A person sees and
// clicks the visible one; so must text:"Search".
func TestLiveTextLocatorLooksPastAHiddenExactMatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	act, sid := liveDataPage(t, ctx, `<html><body>
<button style="display:none" onclick="document.getElementById('s').textContent='mobile'">Search</button>
<button onclick="document.getElementById('s').textContent='desktop'">Search <kbd>Ctrl K</kbd></button>
<p id="s">none</p></body></html>`)
	res, err := act.Execute(ctx, map[string]interface{}{
		"session_id": sid,
		"operations": []interface{}{map[string]interface{}{"type": "click", "text": "Search"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.(map[string]interface{})
	if out["success"] != true {
		t.Fatalf("the visible Search button must be found: %v", out)
	}
	page, _ := act.sessions.Page(sid)
	if text := page.MustElement("#s").MustText(); text != "desktop" {
		t.Fatalf("clicked the wrong button: status %q", text)
	}
}

// TestLiveRouteAnnouncementIsNotAToast: Next.js reads each client-side
// navigation aloud in an assertive live region, and that text is the page
// title. Measured on react.dev as an "error" toast; a real toast beside it
// must still be reported.
func TestLiveRouteAnnouncementIsNotAToast(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	act, sid := liveDataPage(t, ctx, `<html><head><title>Old page</title></head><body>
<p id="__next-route-announcer__" aria-live="assertive" role="alert" style="position:absolute;width:1px;height:1px;overflow:hidden"></p>
<div id="toast" role="alert"></div>
<button onclick="
  document.title='Thinking in React - React';
  document.getElementById('__next-route-announcer__').textContent=document.title;
  document.getElementById('toast').textContent='Could not load profile';
">Go</button></body></html>`)
	res, err := act.Execute(ctx, map[string]interface{}{
		"session_id": sid,
		"operations": []interface{}{map[string]interface{}{"type": "click", "text": "Go"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Read effects from the payload as an agent receives it.
	full, _ := json.Marshal(res)
	var payload map[string]interface{}
	_ = json.Unmarshal(full, &payload)
	parsed, _ := payload["effects"].(map[string]interface{})
	raw, _ := json.Marshal(parsed)
	effects := string(raw)
	for _, toast := range toStringSlice(parsed["toasts"]) {
		if strings.Contains(toast, "Thinking in React") {
			t.Fatalf("the route announcement surfaced as a toast: %s", effects)
		}
	}
	if !strings.Contains(effects, "Could not load profile") {
		t.Fatalf("the real toast beside it must still be reported: %s", effects)
	}
}
