package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"

	mcpclient "github.com/mark3labs/mcp-go/client"
	sdkmcp "github.com/mark3labs/mcp-go/mcp"
)

// This proof uses a candidate or source test child through MCP, a real React fixture,
// and headed Chrome on its own managed display. A PID namespace contains every
// candidate child so cleanup cannot target another browser or daemon.
func TestLiveProgressiveObserveDeepGateMCP(t *testing.T) {
	binary := os.Getenv("BROWSERNERD_DEEP_GATE_BINARY")
	sourceChild := os.Getenv("BROWSERNERD_GATE_LEDGER_HEADED") == "1"
	if binary == "" && !sourceChild {
		t.Skip("set BROWSERNERD_DEEP_GATE_BINARY and BROWSERNERD_REACT_FIXTURE_DIR for isolated headed MCP proof")
	}
	assets := os.Getenv("BROWSERNERD_REACT_FIXTURE_DIR")
	t.Setenv("BROWSERNERD_TEST_PASSWORD", "OpaqueFixtureCredential_4919")
	for _, name := range []string{"react.development.js", "react-dom.development.js"} {
		if _, err := os.Stat(filepath.Join(assets, name)); assets == "" || err != nil {
			t.Fatalf("provide local React 18 UMD fixture asset %s: %v", name, err)
		}
	}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/react.development.js", "/react-dom.development.js":
			http.ServeFile(w, r, filepath.Join(assets, filepath.Base(r.URL.Path)))
		case "/api/unavailable":
			http.Error(w, "fixture service unavailable", http.StatusInternalServerError)
		default:
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<!doctype html><title>Generic inspection fixture</title>
<div id="mount"></div><script src="/react.development.js"></script><script src="/react-dom.development.js"></script>
<script>
const e=React.createElement;
function TabControl(props){return e('button',{id:'inspect-button',role:'tab',value:props.value,onClick:props.onValueChange},'Inspection ready');}
function Deep(props){return props.depth?e('div',null,e(Deep,{depth:props.depth-1})):e('span',null,'Deep leaf');}
const fields=Object.fromEntries(Array.from({length:16},(_,i)=>['data-field-'+i,'bounded-'+i]));
function InspectionFixture(){return e('section',{id:'root','data-fixture':'inspection'},
 e('aside',null,Array.from({length:320},(_,i)=>e('span',{key:i},'Shell '+i))),
 e('main',{id:'main'},e(TabControl,{value:'calendar',onValueChange:function mustNeverInvoke(){throw new Error('FUNCTION_SOURCE_MUST_NOT_ESCAPE');}}),
 e('input',{id:'public-value',defaultValue:'calendar'}),
 e('input',{type:'password',value:'FixturePasswordMustBeRedacted!',readOnly:true}),
 e('p',{id:'environment-value'},'OpaqueFixtureCredential_4919'),
 Array.from({length:100},(_,i)=>e('span',{...fields,key:i},'Detail '+i)),e(Deep,{depth:40})),
 e('footer',{id:'outside-main'},'Must stay outside selected subtree'));}
ReactDOM.createRoot(document.getElementById('mount')).render(React.createElement(InspectionFixture));
fetch('/api/unavailable');
</script>`)
		}
	}))
	defer fixture.Close()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "gate.yaml")
	configuration := fmt.Sprintf(`server:
  name: deep-gate-proof
  log_file: %q
browser:
  auto_start: false
  headless: false
  isolated_display: true
  session_store: %q
  enable_dom_ingestion: false
mcp:
  progressive_only: true
mangle:
  enable: true
  fact_buffer_limit: 1024
docker:
  enable: false
specs:
  enabled: false
recorder:
  enabled: false
security:
  allow_unsafe_javascript: false
  base_dir: %q
  writable_roots: [%q]
`, filepath.Join(dir, "server.log"), filepath.Join(dir, "sessions.json"), dir, dir)
	if err := os.WriteFile(configPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	command := []string{"--user", "--map-current-user", "--pid", "--fork", "--mount-proc", "--kill-child"}
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "DISPLAY=") && !strings.HasPrefix(entry, "WAYLAND_DISPLAY=") {
			env = append(env, entry)
		}
	}
	if sourceChild {
		command = append(command, os.Args[0], "-test.run=^TestGateLedgerMCPChild$")
		env = append(env, "BROWSERNERD_GATE_LEDGER_CHILD_CONFIG="+configPath)
	} else {
		command = append(command, binary, "--no-workspace", "--config", configPath)
	}
	client, err := mcpclient.NewStdioMCPClient("unshare", env, command...)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if stderr, ok := mcpclient.GetStderr(client); ok {
		go func() { _, _ = io.Copy(io.Discard, stderr) }()
	}
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Initialize(ctx, sdkmcp.InitializeRequest{Params: sdkmcp.InitializeParams{
		ProtocolVersion: sdkmcp.LATEST_PROTOCOL_VERSION,
		ClientInfo:      sdkmcp.Implementation{Name: "deep-gate-proof", Version: "1"},
	}}); err != nil {
		log, _ := os.ReadFile(filepath.Join(dir, "server.log"))
		t.Fatalf("initialize: %v; %s", err, log)
	}
	listed, err := client.ListTools(ctx, sdkmcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var observeProperties map[string]interface{}
	for _, tool := range listed.Tools {
		if tool.Name == "reify-react" || tool.Name == "snapshot-dom" {
			t.Fatal("focused escape hatch exposed with progressive_only")
		}
		if tool.Name == "browser-observe" {
			observeProperties = tool.InputSchema.Properties
		}
	}
	for _, field := range []string{"gate_reason", "approved_by_handle"} {
		if observeProperties[field] == nil {
			t.Fatalf("MCP tools/list does not advertise %s", field)
		}
	}
	call := func(name string, args map[string]interface{}) map[string]interface{} {
		t.Helper()
		if name == "browser-observe" {
			for field := range args {
				if observeProperties[field] == nil {
					t.Fatalf("proof used unadvertised browser-observe field %s", field)
				}
			}
		}
		result, err := client.CallTool(ctx, sdkmcp.CallToolRequest{Params: sdkmcp.CallToolParams{Name: name, Arguments: args}})
		if err != nil {
			t.Fatal(err)
		}
		for _, content := range result.Content {
			if text, ok := sdkmcp.AsTextContent(content); ok {
				var data map[string]interface{}
				if err := json.Unmarshal([]byte(text.Text), &data); err != nil {
					t.Fatalf("invalid MCP JSON: %v; %s", err, text.Text)
				}
				return data
			}
		}
		t.Fatalf("no MCP payload: %+v", result)
		return nil
	}
	if out := call("launch-browser", map[string]interface{}{}); out["success"] == false || out["control_url"] == nil {
		t.Fatalf("isolated headed launch: %+v", out)
	}
	defer call("shutdown-browser", map[string]interface{}{})
	create := func() string {
		t.Helper()
		out := call("browser-act", map[string]interface{}{"operations": []interface{}{map[string]interface{}{"type": "session_create", "url": fixture.URL}}, "view": "full"})
		if out["success"] != true {
			t.Fatalf("create isolated session: %+v", out)
		}
		id := getStringFromMap(out, "session_id")
		if id == "" {
			t.Fatalf("missing session receipt: %+v", out)
		}
		return id
	}
	session, other := create(), create()
	listedSessions := call("browser-observe", map[string]interface{}{"mode": "sessions", "view": "full"})
	if !strings.Contains(fmt.Sprint(listedSessions), "isolated_display:true") {
		t.Fatalf("candidate did not report a private headed display: %+v", listedSessions)
	}
	for deadline := time.Now().Add(8 * time.Second); ; {
		out := call("browser-observe", map[string]interface{}{"session_id": session, "mode": "interactive", "view": "full"})
		if strings.Contains(fmt.Sprint(out), "Inspection ready") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("React fixture did not render: %+v", out)
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, mode := range []string{"react", "dom_snapshot"} {
		for _, view := range []string{"summary", "compact", "full"} {
			out := call("browser-observe", map[string]interface{}{"session_id": session, "mode": mode, "view": view})
			if out["success"] != false || out["gated"] != true || out["status"] != "gated" || out["error"] == "" {
				t.Fatalf("pre-authorization denial lost: %+v", out)
			}
		}
	}
	authorize := func() string {
		t.Helper()
		reason := call("browser-reason", map[string]interface{}{"session_id": session, "topic": "health", "view": "compact"})
		handles, _ := reason["evidence_handles"].([]interface{})
		if len(handles) == 0 {
			t.Fatalf("real browser-reason emitted no handle for failed fixture request: %+v", reason)
		}
		if asMap(reason["gate_grants"])["available"] != true {
			t.Fatalf("real reason authorization was unavailable: %+v", reason)
		}
		return fmt.Sprint(handles[0])
	}
	handle := authorize()
	for _, mode := range []string{"react", "dom_snapshot"} {
		for _, view := range []string{"summary", "compact", "full"} {
			out := call("browser-observe", map[string]interface{}{"session_id": session, "mode": mode, "view": view, "gate_reason": "explicit_user_intent", "approved_by_handle": handle, "root_selector": "main"})
			if out["success"] != true || (out["status"] != "ok" && out["status"] != "partial") || out["summary"] == "" {
				t.Fatalf("authorized inspection failed: %+v", out)
			}
			if asMap(out["data"])["captured"].(float64) <= 0 {
				t.Fatalf("actual React fixture yielded no reified facts: %+v", out)
			}
			t.Logf("%s/%s approved: %s", mode, view, out["summary"])
			foreign := call("browser-observe", map[string]interface{}{"session_id": other, "mode": mode, "view": view, "approved_by_handle": handle})
			if foreign["success"] != false || foreign["gated"] != true || foreign["error"] == "" {
				t.Fatalf("handle crossed sessions: %+v", foreign)
			}
		}
	}
	inspect := func(mode, view string, extra map[string]interface{}) map[string]interface{} {
		t.Helper()
		args := map[string]interface{}{"session_id": session, "mode": mode, "view": view, "approved_by_handle": authorize()}
		for k, v := range extra {
			args[k] = v
		}
		out := call("browser-observe", args)
		return out
	}
	for _, mode := range []string{"react", "dom_snapshot"} {
		for _, bad := range []map[string]interface{}{{"root_selector": "#missing"}, {"root_selector": "span"}, {"root_selector": "["}, {"root_selector": "main", "root_ref": "inspect-button"}, {"root_ref": "never-observed"}} {
			out := inspect(mode, "full", bad)
			if out["success"] != false || out["error"] == "" {
				t.Fatalf("bad root was not refused: %+v", out)
			}
		}
		prefix := inspect(mode, "full", map[string]interface{}{})
		if prefix["success"] != true || asMap(prefix["data"])["capture_complete"] != false || asMap(asMap(prefix["data"])["scope"])["kind"] != "document_prefix" {
			t.Fatalf("document prefix incompleteness hidden: %+v", prefix)
		}
	}
	for _, mode := range []string{"react", "dom_snapshot"} {
		for _, view := range []string{"summary", "compact", "full"} {
			out := inspect(mode, view, map[string]interface{}{"root_selector": "main", "max_depth": 3, "visible_only": true, "max_snapshot_bytes": 8000})
			data := asMap(out["data"])
			raw, _ := json.Marshal(out)
			if out["success"] != true || data["capture_complete"] != false || data["truncated"] != true || len(raw) > 9000 {
				t.Fatalf("targeted bounded evidence failed: %+v", out)
			}
			if strings.Contains(string(raw), "FixturePasswordMustBeRedacted!") || strings.Contains(string(raw), "OpaqueFixtureCredential_4919") || strings.Contains(string(raw), "FUNCTION_SOURCE_MUST_NOT_ESCAPE") || strings.Contains(string(raw), "outside-main") {
				t.Fatalf("private or out-of-scope content escaped %s/%s", mode, view)
			}
			if view == "summary" {
				if data["returned"] != float64(0) {
					t.Fatal("summary returned rows")
				}
			} else {
				if !strings.Contains(string(raw), `calendar`) {
					t.Fatalf("targeted scalar tab value missing: %+v", out)
				}
				if mode == "react" && !strings.Contains(string(raw), `"type":"function"`) {
					t.Fatalf("function presence type missing: %+v", out)
				}
			}
			t.Logf("targeted %s/%s: captured=%v returned=%v bytes=%d complete=%v", mode, view, data["captured"], data["returned"], len(raw), data["complete"])
		}
	}
	controls := call("browser-observe", map[string]interface{}{"session_id": session, "mode": "interactive", "view": "full"})
	if !strings.Contains(fmt.Sprint(controls), "inspect-button") {
		t.Fatalf("control ref was not observed: %+v", controls)
	}
	for _, mode := range []string{"react", "dom_snapshot"} {
		out := inspect(mode, "full", map[string]interface{}{"root_ref": "inspect-button"})
		if out["success"] != true || asMap(out["data"])["captured"] != float64(1) {
			t.Fatalf("ref capture escaped selected button subtree: %+v", out)
		}
	}
	// Submit many low-priority rows to a deliberately tiny buffer. Direct rows
	// remain authoritative even when optional fact retention discards early fibers.
	retainedHandle := authorize()
	var pressure map[string]interface{}
	for i := 0; i < 3; i++ {
		pressure = call("browser-observe", map[string]interface{}{"session_id": session, "mode": "react", "view": "full", "approved_by_handle": retainedHandle, "root_selector": "main", "max_nodes": 500, "max_depth": 50, "emit_facts": true})
	}
	storage := asMap(asMap(pressure["data"])["fact_storage"])
	if pressure["success"] != true || storage["retention"] != "unknown" || storage["submitted"].(float64) <= 1024 {
		t.Fatalf("fixture did not saturate optional storage honestly: %+v", pressure)
	}
	t.Logf("saturated 1024-fact engine with %v submitted facts; direct capture=%v", storage["submitted"], asMap(pressure["data"])["captured"])
	dataPressure := asMap(pressure["data"])
	rawPressure, _ := json.Marshal(pressure)
	if !strings.Contains(string(rawPressure), "calendar") || !strings.Contains(string(rawPressure), `"type":"function"`) {
		t.Fatalf("direct evidence lost under fact pressure: %+v", pressure)
	}
	if dataPressure["fact_storage"] == nil || dataPressure["scope"] == nil {
		t.Fatal("pressure capture metadata missing")
	}
	diagnostics := call("browser-observe", map[string]interface{}{"mode": "doctor", "view": "full"})
	engine := asMap(asMap(diagnostics["data"])["engine"])
	if engine["limit"] != float64(1024) || engine["evicted"].(float64) <= 0 {
		t.Fatalf("fixture did not actually evict facts: %+v", engine)
	}
	t.Logf("fact retention evicted %v; direct targeted tab value and function marker remain in response", engine["evicted"])
	if sourceChild {
		telemetry := call("browser-mangle", map[string]interface{}{"operation": "read", "session_id": session, "predicate_filter": "disclosure_handle", "limit": 1000, "view": "full"})
		if strings.Contains(fmt.Sprint(telemetry), "reason:") || telemetry["success"] != true {
			t.Fatalf("real producer telemetry did not fully evict: %+v", telemetry)
		}
		for _, view := range []string{"summary", "compact", "full"} {
			for _, mode := range []string{"react", "dom_snapshot"} {
				out := call("browser-observe", map[string]interface{}{"session_id": session, "mode": mode, "view": view, "approved_by_handle": retainedHandle, "root_selector": "main"})
				if out["success"] != true || asMap(out["data"])["captured"].(float64) <= 0 {
					t.Fatalf("eviction revoked %s/%s real authorization: %+v", mode, view, out)
				}
			}
			arm := call("browser-observe", map[string]interface{}{"session_id": session, "mode": "input_trace", "view": view, "approved_by_handle": retainedHandle, "trace_action": "arm", "root_ref": "inspect-button", "duration_ms": 1000, "max_events": 20, "max_trace_bytes": 4000})
			id := getStringFromMap(asMap(arm["data"]), "trace_id")
			if arm["success"] != true || id == "" {
				t.Fatalf("eviction revoked trace arm/%s: %+v", view, arm)
			}
			for _, action := range []string{"read", "stop"} {
				out := call("browser-observe", map[string]interface{}{"session_id": session, "mode": "input_trace", "view": view, "approved_by_handle": retainedHandle, "trace_action": action, "trace_id": id})
				if out["success"] != true || getStringFromMap(asMap(out["data"]), "trace_id") != id {
					t.Fatalf("eviction revoked trace %s/%s: %+v", action, view, out)
				}
			}
		}
		t.Log("same actual reason handle authorized all deep modes/views and trace arm/read/stop after complete disclosure telemetry eviction")
	}

	t.Log("MCP advertised gate fields; both modes/all views denied before browser-reason, inspected with its same-session handle, and denied that handle on another session; headed isolated candidate only")
}

// The parent launches this source-built test process only in its own PID
// namespace. It serves normal MCP tools without installing or switching a daemon.
func TestGateLedgerMCPChild(t *testing.T) {
	path := os.Getenv("BROWSERNERD_GATE_LEDGER_CHILD_CONFIG")
	if path == "" {
		t.Skip("source child only")
	}
	cfg, _, err := config.LoadWithWorkspace(path, config.WorkspaceOptions{Disable: true})
	if err != nil {
		t.Fatal(err)
	}
	engine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		t.Fatal(err)
	}
	manager := browser.NewSessionManager(cfg.Browser, engine)
	defer manager.Shutdown(context.Background())
	server, err := NewServer(cfg, manager, engine)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
}
