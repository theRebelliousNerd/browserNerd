package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

// TestLiveLayoutSmokeAgainstRunningApp audits routes of a running app at the
// phone preset in a browser someone else owns - the shared BrowserNERD Chrome,
// already signed in - opening one tab of its own in the shared context and
// closing only that tab. It runs only when asked:
//
//	BROWSERNERD_LAYOUT_SMOKE_DEBUGGER  the browser's DevTools ws:// URL
//	BROWSERNERD_LAYOUT_SMOKE_BASE      the app's origin, e.g. http://localhost:3001
//	BROWSERNERD_LAYOUT_SMOKE_OUT       a directory for the report and screenshots
//	BROWSERNERD_LAYOUT_SMOKE_ROUTES    comma-separated paths, each optionally path@selector to
//	                                   scroll that element into view first (default /dashboard,/services,/network)
//	BROWSERNERD_LAYOUT_SMOKE_CHECKS    comma-separated finding types (default all)
//	BROWSERNERD_LAYOUT_SMOKE_PRESET    the viewport preset (default phone)
func TestLiveLayoutSmokeAgainstRunningApp(t *testing.T) {
	debugger := os.Getenv("BROWSERNERD_LAYOUT_SMOKE_DEBUGGER")
	base := strings.TrimRight(os.Getenv("BROWSERNERD_LAYOUT_SMOKE_BASE"), "/")
	out := os.Getenv("BROWSERNERD_LAYOUT_SMOKE_OUT")
	if debugger == "" || base == "" || out == "" {
		t.Skip("set BROWSERNERD_LAYOUT_SMOKE_DEBUGGER, _BASE and _OUT to audit a running app")
	}
	routes := []string{"/dashboard", "/services", "/network"}
	if r := os.Getenv("BROWSERNERD_LAYOUT_SMOKE_ROUTES"); r != "" {
		routes = strings.Split(r, ",")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	engine, err := mangle.NewEngine(config.MangleConfig{Enable: true, SchemaPath: "../../schemas/browser.mg", FactBufferLimit: 4096})
	if err != nil {
		t.Fatal(err)
	}
	manager := browser.NewSessionManager(config.BrowserConfig{DebuggerURL: debugger}, engine)
	if err := manager.Start(ctx); err != nil {
		t.Fatalf("connect to %s: %v", debugger, err)
	}
	// Never Shutdown: the browser and its other tabs belong to someone else.
	session, err := manager.CreateSession(ctx, "about:blank")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = manager.CloseSession(session.ID) }()
	policy, err := security.NewPathPolicy(out, []string{out})
	if err != nil {
		t.Fatal(err)
	}
	observe := &BrowserObserveTool{sessions: manager, engine: engine, pathPolicy: policy}
	act := &BrowserActTool{sessions: manager, engine: engine}
	run := func(op map[string]interface{}) {
		res, err := act.Execute(ctx, map[string]interface{}{"session_id": session.ID, "operations": []interface{}{op}})
		if err != nil || asMap(res)["success"] != true {
			t.Fatalf("%v: %v %v", op, err, asMap(res)["summary"])
		}
	}
	var checks []interface{}
	if c := os.Getenv("BROWSERNERD_LAYOUT_SMOKE_CHECKS"); c != "" {
		for _, name := range strings.Split(c, ",") {
			checks = append(checks, strings.TrimSpace(name))
		}
	}
	preset := os.Getenv("BROWSERNERD_LAYOUT_SMOKE_PRESET")
	if preset == "" {
		preset = "phone"
	}
	run(map[string]interface{}{"type": "viewport", "preset": preset})
	report := map[string]interface{}{}
	for _, spec := range routes {
		// "path@selector" scrolls that element to the middle of the screen
		// before auditing, so a defect below the fold is drawn.
		route, focus, _ := strings.Cut(strings.TrimSpace(spec), "@")
		run(map[string]interface{}{"type": "navigate", "url": base + route, "wait_until": "networkidle", "timeout_ms": 20000})
		if focus != "" {
			page, _ := manager.Page(session.ID)
			if _, err := page.Eval(`(sel) => { const el = document.querySelector(sel); if (!el) throw new Error('no ' + sel); el.scrollIntoView({block: 'center'}); }`, focus); err != nil {
				t.Fatalf("%s: focus %s: %v", route, focus, err)
			}
		}
		// What the page itself reports about the device, and a plain screenshot.
		page, _ := manager.Page(session.ID)
		device, err := page.Eval(`() => ({innerWidth, innerHeight, dpr: devicePixelRatio, coarse: matchMedia('(pointer: coarse)').matches, mobileUA: navigator.userAgent.includes('Mobile')})`)
		if err != nil {
			t.Fatalf("%s: device probe: %v", spec, err)
		}
		shot, err := observe.Execute(ctx, map[string]interface{}{"session_id": session.ID, "mode": "screenshot"})
		if err != nil {
			t.Fatalf("%s: screenshot: %v", spec, err)
		}
		t.Logf("%s: device %v, screenshot %v", spec, device.Value, asMap(asMap(shot)["data"]))
		args := map[string]interface{}{"session_id": session.ID, "mode": "layout", "view": "full"}
		if len(checks) > 0 {
			args["checks"] = checks
		}
		res, err := observe.Execute(ctx, args)
		if err != nil {
			t.Fatalf("%s: %v", spec, err)
		}
		m := asMap(res)
		if m["success"] != true {
			t.Fatalf("%s: %v", spec, m["error"])
		}
		t.Logf("%s: %v", spec, m["summary"])
		report[spec] = m
	}
	raw, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "layout_smoke.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
