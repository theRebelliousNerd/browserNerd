package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"

	"browsernerd-mcp-server/internal/config"
)

func TestCloseSessionIsIdempotentAndCancelsStream(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{}, nil)
	cancelled := false
	manager.sessions["tab-1"] = &sessionRecord{
		meta: Session{ID: "tab-1", BrowserID: "browser-1", LastActive: time.Now()},
		streamCancel: func() {
			cancelled = true
		},
	}

	closed, err := manager.CloseSession("tab-1")
	if err != nil {
		t.Fatal(err)
	}
	if !closed || !cancelled {
		t.Fatalf("expected close and stream cancellation, closed=%v cancelled=%v", closed, cancelled)
	}
	closed, err = manager.CloseSession("tab-1")
	if err != nil || closed {
		t.Fatalf("second close should be an idempotent no-op, closed=%v err=%v", closed, err)
	}
}

func TestReapIdleTabsClosesOnlyExpiredSessions(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{}, nil)
	now := time.Now()
	manager.sessions["old"] = &sessionRecord{meta: Session{ID: "old", LastActive: now.Add(-time.Hour)}}
	manager.sessions["fresh"] = &sessionRecord{meta: Session{ID: "fresh", LastActive: now}}

	manager.reapIdleTabs(now, 30*time.Minute)
	if _, ok := manager.sessions["old"]; ok {
		t.Fatal("expected old tab to be reaped")
	}
	if _, ok := manager.sessions["fresh"]; !ok {
		t.Fatal("fresh tab was reaped")
	}
}

func TestDetachedSessionsDoNotCountTowardTheTabLimit(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{MaxTabs: 2}, nil)
	for _, id := range []string{"stale-1", "stale-2", "stale-3"} {
		// What loadSessions produces after a restart: metadata, no page.
		manager.sessions[id] = &sessionRecord{meta: Session{ID: id, Status: "detached"}}
	}

	_, err := manager.CreateTab(context.Background(), "", "about:blank", false)
	if err != nil && strings.Contains(err.Error(), "tab limit") {
		t.Fatalf("three detached sessions own no tab and must not hit a limit of 2: %v", err)
	}
}

func TestOpenTabsStillHitTheTabLimit(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{MaxTabs: 2}, nil)
	manager.sessions["open-1"] = &sessionRecord{meta: Session{ID: "open-1"}, page: &rod.Page{}}
	manager.sessions["open-2"] = &sessionRecord{meta: Session{ID: "open-2"}, page: &rod.Page{}}

	_, err := manager.CreateTab(context.Background(), "", "about:blank", false)
	if err == nil || !strings.Contains(err.Error(), "tab limit reached (2)") {
		t.Fatalf("two open tabs at a limit of 2 must refuse a third, got %v", err)
	}
}

func TestListBrowsersIncludesTabCounts(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{}, nil)
	manager.browsers["browser-1"] = &browserRecord{meta: BrowserInstance{ID: "browser-1", Status: "connected"}}
	manager.sessions["tab-1"] = &sessionRecord{meta: Session{ID: "tab-1", BrowserID: "browser-1"}}
	manager.sessions["tab-2"] = &sessionRecord{meta: Session{ID: "tab-2", BrowserID: "browser-1"}}

	browsers := manager.ListBrowsers()
	if len(browsers) != 1 || browsers[0].TabCount != 2 {
		t.Fatalf("unexpected browser inventory: %+v", browsers)
	}
}

func TestCreateTabRequiresConnectedBrowser(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{}, nil)
	if _, err := manager.CreateTab(context.Background(), "", "about:blank", false); err == nil {
		t.Fatal("expected disconnected browser error")
	}
}

// Chrome accepts CDP only on /devtools/browser/<id>, so an endpoint-only
// debugger_url is resolved through /json/version and a full socket URL is kept.
func TestStartBrowserResolvesDebuggerEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/abc"}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	m := NewSessionManager(config.BrowserConfig{DebuggerURL: "ws://" + host}, nil)
	got, err := m.startBrowser()
	if err != nil {
		t.Fatalf("startBrowser: %v", err)
	}
	if got.pid != 0 || got.tempDir {
		t.Fatalf("an attached browser was recorded as launched: %+v", got)
	}
	if want := "ws://" + host + "/devtools/browser/abc"; got.controlURL != want {
		t.Fatalf("resolved %q, want %q", got.controlURL, want)
	}

	full := "ws://" + host + "/devtools/browser/kept"
	m = NewSessionManager(config.BrowserConfig{DebuggerURL: full}, nil)
	if got, err := m.startBrowser(); err != nil || got.controlURL != full {
		t.Fatalf("full socket URL changed: %+v, %v", got, err)
	}
}
