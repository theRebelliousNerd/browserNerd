package browser

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

func waitOwnedExit(t *testing.T, ctx context.Context, pid int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; {
		exited, err := ownedProcessExited(pid)
		if err == nil && exited {
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			t.Fatalf("own Chrome exit not proved: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLiveConnectionLastContextAndOwnedRecovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	headless := false
	cfg := config.BrowserConfig{Headless: &headless, IsolatedDisplay: true, SessionStore: filepath.Join(t.TempDir(), "sessions.json")}
	if !config.DisplayBackend().Supported {
		if cfg.ValidateDisplayIsolation() == nil {
			t.Fatal("unsupported private display did not refuse")
		}
		return
	}
	if ChromeBinary(cfg) == "" {
		t.Fatal("installed Chrome required; no download")
	}
	m := NewSessionManager(cfg, nil)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer m.Shutdown(context.Background())
	id := m.defaultBrowserID
	for n := 0; n < 3; n++ {
		s, err := m.CreateTab(ctx, id, "about:blank", true)
		if err != nil {
			t.Fatal(err)
		}
		if !s.Isolated || !s.IsolatedDisplay {
			t.Fatalf("not an owned private headed context: %+v", s)
		}
		if closed, err := m.CloseSession(s.ID); !closed || err != nil {
			t.Fatalf("last context close: %v %v", closed, err)
		}
		if h := m.ConnectionHealth(ctx); !h.Connected {
			t.Fatalf("last context closure killed live transport: %+v", h)
		}
	}
	additional, err := m.LaunchAdditional(ctx)
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := m.CreateTab(ctx, additional.ID, "about:blank", true)
	if err != nil {
		t.Fatal(err)
	}
	dead := m.browsers[id]
	pid := dead.pid
	if err := dead.browser.Close(); err != nil {
		t.Fatal(err)
	}
	waitOwnedExit(t, ctx, pid)
	if h := m.ConnectionHealth(ctx); h.Connected || h.State != "unreachable" {
		t.Fatalf("dead CDP reported healthy/ambiguous: %+v", h)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if m.defaultBrowserID != additional.ID || len(m.browsers) != 1 {
		t.Fatalf("healthy sibling removed: %+v", m.ListBrowsers())
	}
	page, ok := m.Page(sibling.ID)
	if !ok {
		t.Fatal("healthy sibling session lost")
	}
	if _, err := page.Info(); err != nil {
		t.Fatalf("healthy sibling transport damaged: %v", err)
	}
	if _, err := m.CloseSession(sibling.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.CloseBrowser(additional.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	prior := m.defaultBrowserID
	record := m.browsers[prior]
	pid = record.pid
	if err := record.browser.Close(); err != nil {
		t.Fatal(err)
	}
	waitOwnedExit(t, ctx, pid)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if m.defaultBrowserID == prior || !m.ConnectionHealth(ctx).Connected {
		t.Fatal("sole exited owned default did not recover")
	}
	final, err := m.CreateTab(ctx, m.defaultBrowserID, "about:blank", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CloseSession(final.ID); err != nil {
		t.Fatal(err)
	}
	t.Log("headed private last-context/create cycle, actual dead-CDP detection, exact owned recovery and healthy sibling/context preservation verified")
}
