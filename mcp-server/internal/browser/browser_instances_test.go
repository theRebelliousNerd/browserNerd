package browser

import (
	"context"
	"testing"
	"time"

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
