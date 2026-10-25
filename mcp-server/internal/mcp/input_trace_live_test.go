package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

func TestLiveInputTraceRealReactBoundaries(t *testing.T) {
	if os.Getenv("BROWSERNERD_BACKGROUND_HEADED") != "1" {
		t.Skip("opt-in owned headed display")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture := realReactInputFixture(t, ctx)
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
	session, err := manager.CreateSession(ctx, fixture.URL)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := manager.Page(session.ID)
	if _, err := page.Timeout(10 * time.Second).Element("#tab-role"); err != nil {
		t.Fatal(err)
	}
	observe := &BrowserObserveTool{sessions: manager, engine: engine}
	act := &BrowserActTool{sessions: manager, engine: engine}
	handle := authorizeInputTrace(t, engine, session.ID)
	traceCall := func(action string, extras map[string]interface{}) map[string]interface{} {
		t.Helper()
		args := map[string]interface{}{"session_id": session.ID, "mode": "input_trace", "trace_action": action, "approved_by_handle": handle, "view": "full"}
		for key, value := range extras {
			args[key] = value
		}
		res, err := observe.Execute(ctx, args)
		if err != nil || asMap(res)["success"] != true {
			t.Fatalf("trace %s: %v %+v", action, err, res)
		}
		return asMap(res)
	}
	observed, err := observe.Execute(ctx, map[string]interface{}{"session_id": session.ID, "mode": "interactive", "view": "full"})
	if err != nil || asMap(observed)["success"] != true || manager.Registry(session.ID).Get("tab-role") == nil {
		t.Fatalf("session ref was not observed: %v %+v", err, observed)
	}
	armed := traceCall("arm", map[string]interface{}{"root_ref": "tab-role", "max_trace_bytes": 32768})
	id := getStringFromMap(asMap(armed["data"]), "trace_id")
	res, err := act.Execute(ctx, map[string]interface{}{"session_id": session.ID, "operations": ops(map[string]interface{}{"type": "click", "role": "tab", "name": "Role section", "exact": true}), "settle_ms": 0})
	if err != nil || asMap(res)["success"] != true {
		t.Fatalf("native action: %v %+v", err, res)
	}
	stopped := traceCall("stop", map[string]interface{}{"trace_id": id})
	raw, _ := json.Marshal(stopped)
	t.Logf("Radix receipt: %s", raw)
	var snapshot browser.InputTraceSnapshot
	dataRaw, _ := json.Marshal(asMap(stopped["data"]))
	if err := json.Unmarshal(dataRaw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.EventCount < 5 || snapshot.FinalConfirmed != snapshot.EventCount {
		t.Fatalf("missing native events/final samples: %s", raw)
	}
	for _, event := range snapshot.Events {
		if !event.Trusted || len(event.Phases) == 0 || !event.Final.Confirmed || event.Final.DefaultPrevented == nil {
			t.Fatalf("incomplete event: %+v", event)
		}
	}
	state, err := page.Eval(`() => ({tab:window.inputProof.tab,rendered:JSON.parse(document.querySelector('output').textContent)})`)
	if err != nil || state.Value.Get("tab").Str() != "role" || state.Value.Get("rendered.tab").Str() != "role" {
		t.Fatalf("actual Radix handler did not run: %v %+v", err, state)
	}
	for _, bad := range []map[string]interface{}{{"root_selector": "["}, {"root_selector": "PRIVATE_SELECTOR_CANARY"}, {"root_selector": "button"}, {"root_ref": "PRIVATE_REF_CANARY"}} {
		args := map[string]interface{}{"session_id": session.ID, "mode": "input_trace", "trace_action": "arm", "approved_by_handle": handle}
		for key, value := range bad {
			args[key] = value
		}
		res, err := observe.Execute(ctx, args)
		encoded, _ := json.Marshal(res)
		if err != nil || asMap(res)["success"] != false || strings.Contains(string(encoded), "PRIVATE_") {
			t.Fatalf("bad scope was admitted or echoed: %v %s", err, encoded)
		}
	}
	t.Log(fmt.Sprintf("actual React/Radix received %d trusted native events across %d listener phases; final samples %d; callback execution assessed only by fixture state", snapshot.EventCount, snapshot.PhaseCount, snapshot.FinalConfirmed))
}
