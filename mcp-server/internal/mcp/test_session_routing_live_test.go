package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

// This fixture owns its Chrome and private headed display; it never attaches.
func TestLiveSessionRoutingPrivateFixtures(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	engine, err := mangle.NewEngine(config.MangleConfig{Enable: true, SchemaPath: "../../schemas/browser.mg", FactBufferLimit: 4096})
	if err != nil {
		t.Fatal(err)
	}
	headless := false
	cfg := config.BrowserConfig{Headless: &headless, IsolatedDisplay: true, SessionStore: filepath.Join(t.TempDir(), "sessions.json")}
	if browser.ChromeBinary(cfg) == "" {
		t.Fatal("installed Chrome required; fixture must not download a browser")
	}
	manager := browser.NewSessionManager(cfg, engine)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown(context.Background())
	instances := manager.ListBrowsers()
	if len(instances) != 1 || !instances[0].Managed || !instances[0].IsolatedDisplay || !instances[0].Default {
		t.Fatalf("not an owned private headed browser: %+v", instances)
	}
	browserID := instances[0].ID
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			http.Error(w, "fixture failure", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/anchor" {
			fmt.Fprint(w, `<!doctype html><title>Anchor A</title><h1>Anchor A</h1><script>document.cookie='owner=anchor;path=/';localStorage.setItem('owner','anchor');sessionStorage.setItem('owner','anchor');</script>`)
			return
		}
		fmt.Fprint(w, `<!doctype html><title>Private B</title><h1>Private B</h1><script>window.initialState={cookie:document.cookie,local:localStorage.getItem('owner'),session:sessionStorage.getItem('owner')};</script>`)
		if r.URL.Path == "/private" {
			fmt.Fprint(w, `<script>fetch('/fail');</script>`)
		}
	}))
	defer fixture.Close()
	anchor, err := manager.CreateTab(ctx, browserID, fixture.URL+"/anchor", false)
	if err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, initial, path string, assertions []interface{}) map[string]interface{} {
		t.Helper()
		test := map[string]interface{}{"name": "private B", "operations": ops(
			map[string]interface{}{"type": "session_create", "browser_id": browserID, "isolated": true, "url": fixture.URL + path},
			map[string]interface{}{"type": "await_stable", "timeout_ms": 5000},
		), "assertions": assertions}
		if initial != "" {
			test["session_id"] = initial
		}
		out, err := (&BrowserTestTool{sessions: manager, engine: engine}).Execute(ctx, map[string]interface{}{"operation": "run", "view": "full", "test": test})
		if err != nil {
			t.Fatal(err)
		}
		return asMap(out)
	}
	assertNew := func(t *testing.T, out map[string]interface{}) string {
		t.Helper()
		id, _ := out["session_id"].(string)
		meta, ok := manager.GetSession(id)
		if id == "" || id == anchor.ID || !ok || meta.BrowserID != browserID || !meta.Isolated || !meta.IsolatedDisplay || asMap(out["replay"])["session_id"] != id {
			t.Fatalf("fixture receipt must name actual private B: %+v meta=%+v", out, meta)
		}
		page, _ := manager.Page(id)
		state, err := page.Eval(`() => window.initialState`)
		if err != nil || state.Value.Get("cookie").Str() != "" || !state.Value.Get("local").Nil() || !state.Value.Get("session").Nil() {
			t.Fatalf("new private context inherited anchor auth: %+v %v", state, err)
		}
		if _, err := manager.CloseSession(id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	t.Run("create_first_without_anchor", func(t *testing.T) {
		out := run(t, "", "/private", []interface{}{map[string]interface{}{"query": fmt.Sprintf("current_url(S, %q)", fixture.URL+"/private"), "expect": "present"}})
		if out["passed"] != true {
			t.Fatalf("create-first fixture rejected/failed: %+v", out)
		}
		assertNew(t, out)
	})
	t.Run("assert_B_not_A", func(t *testing.T) {
		out := run(t, anchor.ID, "/private", []interface{}{
			map[string]interface{}{"query": fmt.Sprintf("current_url(S, %q)", fixture.URL+"/private"), "expect": "present", "scope": "current"},
			map[string]interface{}{"query": fmt.Sprintf("current_url(S, %q)", fixture.URL+"/anchor"), "expect": "absent", "scope": "current"},
		})
		if out["passed"] != true {
			t.Fatalf("anchor/global facts contaminated B assertions: %+v", out)
		}
		id := assertNew(t, out)
		for _, row := range out["assertions"].([]map[string]interface{}) {
			if row["session_id"] != id {
				t.Fatalf("assertion omitted actual session: %+v", row)
			}
		}
	})
	t.Run("failure_diagnoses_B", func(t *testing.T) {
		out := run(t, anchor.ID, "/private", []interface{}{map[string]interface{}{"query": "failed_request(S, _, _, _)", "expect": "absent"}})
		if out["passed"] != false || out["diagnosis"] == nil {
			t.Fatalf("expected B's actual failed request and diagnosis: %+v", out)
		}
		id := assertNew(t, out)
		row := asMap(asMap(out["diagnosis"])["failed_request"])
		if row["count"] == nil || !strings.Contains(fmt.Sprint(row["samples"]), id) || strings.Contains(fmt.Sprint(row["samples"]), anchor.ID) {
			t.Fatalf("diagnosis not limited to B: %+v", out)
		}
	})
	t.Run("old_A_errors_cannot_fail_clean_B", func(t *testing.T) {
		page, _ := manager.Page(anchor.ID)
		if _, err := page.Eval(`async () => { await fetch('/fail'); return true }`); err != nil {
			t.Fatal(err)
		}
		query := fmt.Sprintf("failed_request(%q, _, _, _)", anchor.ID)
		for deadline := time.Now().Add(3 * time.Second); ; {
			rows, err := queryAssertionRows(ctx, engine, query)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) > 0 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("anchor's actual error was not captured")
			}
			time.Sleep(10 * time.Millisecond)
		}
		out := run(t, anchor.ID, "/clean", []interface{}{
			map[string]interface{}{"query": "failed_request(S, _, _, _)", "expect": "absent", "scope": "current"},
			map[string]interface{}{"query": "failed_request(S, _, _, _)", "expect": "absent", "scope": "fresh"},
		})
		if out["passed"] != true || out["diagnosis"] != nil {
			t.Fatalf("A error contaminated clean B: %+v", out)
		}
		assertNew(t, out)
	})
	t.Run("failed_switch_never_navigates_A_even_continue", func(t *testing.T) {
		out, err := (&BrowserTestTool{sessions: manager, engine: engine}).Execute(ctx, map[string]interface{}{
			"test": map[string]interface{}{"session_id": anchor.ID, "operations": ops(
				map[string]interface{}{"type": "session_create", "browser_id": "missing-owned-fixture-browser", "isolated": true},
				map[string]interface{}{"type": "navigate", "url": fixture.URL + "/clean"},
			)}, "stop_on_error": false,
		})
		if err != nil || asMap(out)["success"] != false || asMap(asMap(out)["replay"])["counts"].(map[string]interface{})["skipped"] != 1 {
			t.Fatalf("failed create did not stop before old-tab navigation: %+v %v", out, err)
		}
	})
	t.Run("compact_launch_and_selected_private_session", func(t *testing.T) {
		act := &BrowserActTool{sessions: manager, engine: engine}
		launched, err := act.Execute(ctx, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "browser_launch", "new_instance": true}), "view": "compact"})
		if err != nil || asMap(launched)["success"] != true {
			t.Fatalf("own additional launch: %+v %v", launched, err)
		}
		entry := asMap(launched)["results"].([]map[string]interface{})[0]
		additional, _ := entry["browser_id"].(string)
		if additional == "" || additional == browserID || entry["isolated_display"] != true || entry["managed"] != true || entry["default"] != false {
			t.Fatalf("compact launch metadata: %+v", launched)
		}
		defer manager.CloseBrowser(additional)
		created, err := act.Execute(ctx, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "session_create", "browser_id": additional, "isolated": true}), "view": "compact", "settle_ms": 0})
		if err != nil || asMap(created)["success"] != true {
			t.Fatalf("explicit private create: %+v %v", created, err)
		}
		meta := asMap(created)["results"].([]map[string]interface{})[0]
		if meta["browser_id"] != additional || meta["isolated"] != true || meta["isolated_display"] != true {
			t.Fatalf("compact private metadata: %+v", created)
		}
		for _, b := range manager.ListBrowsers() {
			if b.ID == browserID && !b.Default || b.ID == additional && b.Default {
				t.Fatalf("explicit routing flipped default: %+v", manager.ListBrowsers())
			}
		}
	})
	t.Run("invalid_unanchored_page_before_create_is_atomic", func(t *testing.T) {
		before := len(manager.List())
		out, err := (&BrowserTestTool{sessions: manager, engine: engine}).Execute(ctx, map[string]interface{}{"test": map[string]interface{}{"operations": ops(
			map[string]interface{}{"type": "browser_launch", "new_instance": true},
			map[string]interface{}{"type": "navigate", "url": fixture.URL + "/private"},
			map[string]interface{}{"type": "session_create", "isolated": true},
		)}, "stop_on_error": false})
		if err != nil || asMap(out)["success"] != false || asMap(out)["replay"] != nil || len(manager.List()) != before || len(manager.ListBrowsers()) != 1 {
			t.Fatalf("invalid session flow ran lifecycle effects: %+v %v", out, err)
		}
	})
	page, _ := manager.Page(anchor.ID)
	state, err := page.Eval(`() => ({url:location.href,cookie:document.cookie,local:localStorage.getItem('owner'),session:sessionStorage.getItem('owner')})`)
	if err != nil || state.Value.Get("url").Str() != fixture.URL+"/anchor" || !strings.Contains(state.Value.Get("cookie").Str(), "owner=anchor") || state.Value.Get("local").Str() != "anchor" || state.Value.Get("session").Str() != "anchor" {
		t.Fatalf("private fixtures changed retained A state: %+v %v", state, err)
	}
	t.Log("owned headed/private-display fixture: fresh context, actual final session assertions/diagnosis, anchor unchanged")
}
