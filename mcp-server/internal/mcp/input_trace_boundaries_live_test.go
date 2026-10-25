package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"github.com/go-rod/rod"
)

func inputTraceReactFixture(t *testing.T, ctx context.Context) *httptest.Server {
	t.Helper()
	modules := os.Getenv("BROWSERNERD_REACT_NODE_MODULES")
	if modules == "" {
		t.Fatal("BROWSERNERD_REACT_NODE_MODULES is required")
	}
	entry, err := filepath.Abs("../../testdata/probe/input-trace-boundaries.jsx")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	build := exec.CommandContext(ctx, filepath.Join(modules, "esbuild", "bin", "esbuild"), entry, "--bundle", "--format=iife", "--define:process.env.NODE_ENV=\"production\"", "--outfile="+filepath.Join(dir, "fixture.js"))
	build.Env = append(os.Environ(), "NODE_PATH="+modules)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("fixture build: %v %s", err, out)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/unavailable" {
			http.Error(w, "generic fixture unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path == "/fixture.js" {
			http.ServeFile(w, r, filepath.Join(dir, "fixture.js"))
			return
		}
		if r.URL.Path == "/next" {
			fmt.Fprint(w, `<!doctype html><title>Next document</title><h1>Next document</h1>`)
			return
		}
		fmt.Fprint(w, `<!doctype html><title>Input boundaries</title><style>button,input{padding:12px;margin:8px}output{display:block}</style><div id="root"></div><script src="/fixture.js"></script>`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLiveInputTraceCancellationPrivacyAndCleanup(t *testing.T) {
	if os.Getenv("BROWSERNERD_BACKGROUND_HEADED") != "1" {
		t.Skip("opt-in isolated headed trace proof")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	fixture := inputTraceReactFixture(t, ctx)
	startOwnedBackgroundDisplay(t, ctx)
	engine, err := mangle.NewEngine(config.MangleConfig{Enable: true, SchemaPath: "../../schemas/browser.mg", FactBufferLimit: 8192})
	if err != nil {
		t.Fatal(err)
	}
	headless := false
	manager := browser.NewSessionManager(config.BrowserConfig{Headless: &headless, IsolatedDisplay: true, SessionStore: filepath.Join(t.TempDir(), "sessions.json")}, engine)
	if err := manager.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer manager.Shutdown(context.Background())
	observe := &BrowserObserveTool{sessions: manager, engine: engine}
	act := &BrowserActTool{sessions: manager, engine: engine}
	type tab struct {
		id, handle string
		page       *rod.Page
		baseline   int
	}
	newTab := func() *tab {
		t.Helper()
		s, err := manager.CreateSession(ctx, fixture.URL)
		if err != nil {
			t.Fatal(err)
		}
		page, _ := manager.Page(s.ID)
		if _, err := page.Timeout(10 * time.Second).Element("#delivered"); err != nil {
			t.Fatal(err)
		}
		count, err := page.Eval(`()=>window.inputListenerCount()`)
		if err != nil {
			t.Fatal(err)
		}
		return &tab{s.ID, authorizeInputTrace(t, engine, s.ID), page, count.Value.Int()}
	}
	call := func(tab *tab, action string, extras map[string]interface{}, want bool) map[string]interface{} {
		t.Helper()
		args := map[string]interface{}{"mode": "input_trace", "session_id": tab.id, "approved_by_handle": tab.handle, "trace_action": action, "view": "full"}
		for k, v := range extras {
			args[k] = v
		}
		out, err := observe.Execute(ctx, args)
		if err != nil || asMap(out)["success"] != want {
			t.Fatalf("%s: %v %+v", action, err, out)
		}
		return asMap(out)
	}
	arm := func(tab *tab, target string, budgets map[string]interface{}) string {
		t.Helper()
		args := map[string]interface{}{"root_selector": target, "max_trace_bytes": 32768}
		for k, v := range budgets {
			args[k] = v
		}
		return getStringFromMap(asMap(call(tab, "arm", args, true)["data"]), "trace_id")
	}
	native := func(tab *tab, operations ...map[string]interface{}) {
		t.Helper()
		items := make([]interface{}, len(operations))
		for i, op := range operations {
			items[i] = op
		}
		out, err := act.Execute(ctx, map[string]interface{}{"session_id": tab.id, "operations": items, "settle_ms": 0})
		if err != nil || asMap(out)["success"] != true {
			t.Fatalf("native: %v %+v", err, out)
		}
	}
	snapshot := func(out map[string]interface{}) browser.InputTraceSnapshot {
		t.Helper()
		var s browser.InputTraceSnapshot
		raw, _ := json.Marshal(out["data"])
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	assertNoListeners := func(tab *tab) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Second); ; {
			count, err := tab.page.Eval(`()=>window.inputListenerCount()`)
			if err == nil && count.Value.Int() == tab.baseline {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("listeners retained: count=%v baseline=%d err=%v", count, tab.baseline, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	first, second := newTab(), newTab()
	defer manager.CloseSession(first.id)
	defer manager.CloseSession(second.id)
	// Two sessions remain armed simultaneously; foreign trace IDs cannot stop
	// or read the other's evidence even with the caller's valid own grant.
	one, two := arm(first, "#prevented", nil), arm(second, "#delivered", nil)
	call(first, "read", map[string]interface{}{"trace_id": two}, false)
	call(first, "stop", map[string]interface{}{"trace_id": two}, false)
	results := make(chan error, 2)
	for _, item := range []*tab{first, second} {
		go func(tab *tab) {
			target := "prevented"
			if tab == second {
				target = "delivered"
			}
			out, err := act.Execute(ctx, map[string]interface{}{"session_id": tab.id, "operations": ops(map[string]interface{}{"type": "click", "ref": target}), "settle_ms": 0})
			if err == nil && asMap(out)["success"] != true {
				err = fmt.Errorf("native rejected: %+v", out)
			}
			results <- err
		}(item)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	a, b := snapshot(call(first, "stop", map[string]interface{}{"trace_id": one}, true)), snapshot(call(second, "stop", map[string]interface{}{"trace_id": two}, true))
	prevented, delivered := false, false
	for _, event := range a.Events {
		if event.Kind == "mousedown" && event.Final.DefaultPrevented != nil && *event.Final.DefaultPrevented {
			prevented = true
		}
	}
	for _, event := range b.Events {
		if event.Kind == "click" && event.Trusted && event.Final.DefaultPrevented != nil && !*event.Final.DefaultPrevented {
			delivered = true
		}
	}
	if !prevented || !delivered {
		t.Fatalf("cancellation/delivery not discriminated: %+v %+v", a, b)
	}
	assertNoListeners(first)
	assertNoListeners(second)
	state, err := second.page.Eval(`()=>window.fixtureReceipt.delivered`)
	if err != nil || state.Value.Int() != 1 {
		t.Fatalf("fixture callback proof: %v %+v", err, state)
	}
	t.Logf("two concurrent sessions: default-prevented mousedown=%t; trusted delivered click=%t; callback fixture count=%d", prevented, delivered, state.Value.Int())
	// React's delegated capture handler stops propagation before the target.
	id := arm(first, "#blocked", nil)
	native(first, map[string]interface{}{"type": "click", "ref": "blocked"})
	blocked := snapshot(call(first, "stop", map[string]interface{}{"trace_id": id}, true))
	seenStop := false
	targetReached := false
	for _, event := range blocked.Events {
		if event.Kind != "mousedown" {
			continue
		}
		for _, phase := range event.Phases {
			seenStop = seenStop || phase.CancelBubble
			if phase.CurrentTarget == "n0" {
				targetReached = true
			}
		}
	}
	state, err = first.page.Eval(`()=>window.fixtureReceipt.blocked`)
	if err != nil || !seenStop || targetReached || state.Value.Int() != 0 {
		t.Fatalf("propagation boundary incorrect: observed=%t target=%t callback=%v err=%v", seenStop, targetReached, state, err)
	}
	assertNoListeners(first)
	t.Log("capture cancellation observed at ancestor; no target listener sample; fixture callback remained zero")
	id = arm(second, "#immediate", nil)
	native(second, map[string]interface{}{"type": "click", "ref": "immediate"})
	immediate := call(second, "stop", map[string]interface{}{"trace_id": id}, true)
	for _, event := range snapshot(immediate).Events {
		if event.Kind == "click" {
			t.Fatal("earlier stopImmediate unexpectedly reached later window observer")
		}
	}
	if asMap(asMap(immediate["data"])["scope"])["listener_order"] != "earlier_stop_immediate_may_hide_phases" {
		t.Fatal("unobservable listener-order limitation hidden")
	}
	assertNoListeners(second)
	t.Log("earlier window stopImmediate hid click observer; receipt preserved listener-order limitation instead of inferring callback execution")
	// Fixed activation codes are observable; character keys and input values
	// remain excluded even when the target carries private attributes/state.
	id = arm(first, "#key", nil)
	native(first, map[string]interface{}{"type": "click", "ref": "key"}, map[string]interface{}{"type": "key", "key": "Enter"})
	key := snapshot(call(first, "stop", map[string]interface{}{"trace_id": id}, true))
	keySeen := false
	for _, event := range key.Events {
		if event.Kind == "keydown" && event.KeyCode == "Enter" && event.Final.DefaultPrevented != nil && *event.Final.DefaultPrevented {
			keySeen = true
		}
	}
	if !keySeen {
		t.Fatal("cancelled activation key absent")
	}
	assertNoListeners(first)
	id = arm(first, "#private", nil)
	native(first, map[string]interface{}{"type": "fill", "fields": []interface{}{map[string]interface{}{"ref": "testid:InputTracePrivateAttributeCanary_1732", "value": "InputTraceTypedSecretCanary_7153"}}}, map[string]interface{}{"type": "key", "key": "x"}, map[string]interface{}{"type": "key", "key": "Enter"})
	privacy := call(first, "stop", map[string]interface{}{"trace_id": id}, true)
	raw, _ := json.Marshal(privacy)
	for _, canary := range []string{"Canary_", "PRIVATE_STATE", "PrivateFunctionSource", "TypedSecret"} {
		if strings.Contains(string(raw), canary) {
			t.Fatalf("privacy canary escaped: %s", raw)
		}
	}
	for _, event := range snapshot(privacy).Events {
		if strings.Contains(event.Kind, "input") || event.KeyCode == "KeyX" {
			t.Fatal("character/input event captured")
		}
	}
	assertNoListeners(first)
	t.Log("password value, attributes, labels, source, app state and typed character canaries absent from raw trace")
	// Literal-point input can hit a covering sibling. The trace identifies
	// that receiver without pretending it was inside the armed target.
	id = arm(first, "#covered-target", nil)
	point, _ := first.page.Eval(`()=>{const r=document.getElementById('covered-target').getBoundingClientRect();return{x:r.x+r.width/2,y:r.y+r.height/2}}`)
	native(first, map[string]interface{}{"type": "click", "x": point.Value.Get("x").Num(), "y": point.Value.Get("y").Num()})
	covered := snapshot(call(first, "stop", map[string]interface{}{"trace_id": id}, true))
	overlay := false
	for _, event := range covered.Events {
		if event.Kind == "click" && !event.WithinTarget && event.TargetRef != "n0" {
			overlay = true
		}
	}
	if !overlay {
		t.Fatal("covering actual receiver not identified")
	}
	assertNoListeners(first)
	for _, test := range []struct {
		name   string
		budget map[string]interface{}
		want   string
	}{{"timeout", map[string]interface{}{"duration_ms": 100}, "timeout"}, {"events", map[string]interface{}{"max_events": 1}, "event_limit"}, {"bytes", map[string]interface{}{"max_trace_bytes": 2048}, "byte_limit"}} {
		id = arm(first, "#delivered", test.budget)
		if test.name != "timeout" {
			native(first, map[string]interface{}{"type": "click", "ref": "delivered"})
		}
		var out map[string]interface{}
		for deadline := time.Now().Add(3 * time.Second); ; {
			out = call(first, "read", map[string]interface{}{"trace_id": id}, true)
			if asMap(out["data"])["active"] == false {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("bounded trace did not stop")
			}
			time.Sleep(20 * time.Millisecond)
		}
		if asMap(out["data"])["terminal_reason"] != test.want {
			t.Fatalf("wrong limit reason: %+v", out)
		}
		encoded, _ := json.Marshal(out)
		max := 32768
		if test.name == "bytes" {
			max = 2048
		}
		if len(encoded) > max {
			t.Fatalf("response exceeds limit: %d>%d", len(encoded), max)
		}
		assertNoListeners(first)
		t.Logf("%s cleanup: retained events=%v bytes=%d terminal=%s", test.name, asMap(out["data"])["event_count"], len(encoded), test.want)
	}
	// The receipt survives a new document; the exact trace target does not.
	id = arm(first, "#navigate", nil)
	native(first, map[string]interface{}{"type": "click", "ref": "navigate"})
	var navigation map[string]interface{}
	for deadline := time.Now().Add(3 * time.Second); ; {
		navigation = call(first, "read", map[string]interface{}{"trace_id": id}, true)
		if asMap(navigation["data"])["active"] == false {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("navigation left trace active")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if asMap(navigation["data"])["terminal_reason"] != "navigation" || snapshot(navigation).EventCount == 0 {
		t.Fatalf("navigation erased receipt: %+v", navigation)
	}
	t.Logf("navigation retained %d events; final confirmed=%d; no target rebinding", snapshot(navigation).EventCount, snapshot(navigation).FinalConfirmed)
	// Closed session authority disappears even though the trace object can
	// retain its bounded diagnostic copy until its TTL.
	id = arm(second, "#delivered", nil)
	trace, err := manager.InputTrace(second.id, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.CloseSession(second.id); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(3 * time.Second); ; {
		_, closed, _ := trace.Snapshot()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("session close left trace active")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := manager.InputTrace(second.id, id); err == nil {
		t.Fatal("closed session trace still authorized")
	}
}
