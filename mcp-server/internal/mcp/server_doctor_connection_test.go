package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"github.com/go-rod/rod"
)

func TestDoctorConnectionUsesLiveCDP(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	cfg := config.DefaultConfig()
	headless := false
	cfg.Browser.Headless = &headless
	cfg.Browser.IsolatedDisplay = true
	cfg.Browser.SessionStore = filepath.Join(t.TempDir(), "sessions.json")
	if !config.DisplayBackend().Supported {
		if cfg.Browser.ValidateDisplayIsolation() == nil {
			t.Fatal("unsupported private display did not refuse")
		}
		return
	}
	if browser.ChromeBinary(cfg.Browser) == "" {
		t.Fatal("installed Chrome required; no download")
	}
	m := browser.NewSessionManager(cfg.Browser, nil)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer m.Shutdown(context.Background())
	before := DoctorReport(ctx, DoctorDeps{Config: cfg, Sessions: m})
	if b := before["browser"].(map[string]interface{}); b["connected"] != true || b["connection_state"] != "reachable" {
		t.Fatalf("live CDP not verified: %+v", b)
	}
	owned := rod.New().ControlURL(m.ControlURL()).Context(context.Background())
	if err := owned.Connect(); err != nil {
		t.Fatal(err)
	}
	if err := owned.Close(); err != nil {
		t.Fatal(err)
	}
	after := DoctorReport(ctx, DoctorDeps{Config: cfg, Sessions: m})
	b := after["browser"].(map[string]interface{})
	if b["connected"] != false || b["connection_state"] != "unreachable" || b["connection_reason"] != "cdp_transport_closed" {
		t.Fatalf("Doctor trusted stale pointer: %+v", b)
	}
	found := false
	for _, p := range after["problems"].([]string) {
		if strings.Contains(p, "browser CDP transport unreachable (cdp_transport_closed)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("dead transport missing named problem: %+v", after)
	}
}
