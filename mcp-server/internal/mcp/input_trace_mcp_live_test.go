package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// Explicitly selected candidate only: this starts a fresh stdio server and its
// own headed private browser inside an owned process namespace.
func TestLiveInputTraceCandidateMCP(t *testing.T) {
	binary := os.Getenv("BROWSERNERD_INPUT_TRACE_BINARY")
	sourceChild := os.Getenv("BROWSERNERD_INPUT_TRACE_SOURCE_HEADED") == "1"
	if binary == "" && !sourceChild {
		t.Skip("set BROWSERNERD_INPUT_TRACE_BINARY to a built candidate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	fixture := inputTraceReactFixture(t, ctx)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "trace.yaml")
	configuration := fmt.Sprintf(`server:
  name: bounded-input-trace-proof
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
  fact_buffer_limit: 8192
docker:
  enable: false
specs:
  enabled: false
recorder:
  enabled: true
  trace_dir: %q
security:
  allow_unsafe_javascript: false
  base_dir: %q
  writable_roots: [%q]
`, filepath.Join(dir, "server.log"), filepath.Join(dir, "sessions.json"), filepath.Join(dir, "recorder"), dir, dir)
	if err := os.WriteFile(configPath, []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	command := []string{"--user", "--map-current-user", "--pid", "--fork", "--mount-proc", "--kill-child"}
	env := make([]string, 0, len(os.Environ())+1)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "DISPLAY=") && !strings.HasPrefix(entry, "WAYLAND_DISPLAY=") {
			env = append(env, entry)
		}
	}
	if sourceChild {
		command = append(command, os.Args[0], "-test.run=^TestInputTraceMCPSourceChild$")
		env = append(env, "BROWSERNERD_INPUT_TRACE_CHILD_CONFIG="+configPath)
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
	if _, err := client.Initialize(ctx, sdkmcp.InitializeRequest{Params: sdkmcp.InitializeParams{ProtocolVersion: sdkmcp.LATEST_PROTOCOL_VERSION, ClientInfo: sdkmcp.Implementation{Name: "input-trace-proof", Version: "1"}}}); err != nil {
		t.Fatal(err)
	}
	listed, err := client.ListTools(ctx, sdkmcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var props map[string]interface{}
	for _, tool := range listed.Tools {
		if tool.Name == "browser-observe" {
			props = tool.InputSchema.Properties
		}
	}
	for _, field := range []string{"trace_action", "trace_id", "duration_ms", "max_events", "max_trace_bytes", "root_selector", "root_ref"} {
		if props[field] == nil {
			t.Fatalf("MCP does not advertise %s", field)
		}
	}
	call := func(name string, args map[string]interface{}) map[string]interface{} {
		t.Helper()
		out, err := client.CallTool(ctx, sdkmcp.CallToolRequest{Params: sdkmcp.CallToolParams{Name: name, Arguments: args}})
		if err != nil {
			t.Fatal(err)
		}
		for _, content := range out.Content {
			if block, ok := sdkmcp.AsTextContent(content); ok {
				var result map[string]interface{}
				if err := json.Unmarshal([]byte(block.Text), &result); err != nil {
					t.Fatalf("invalid JSON: %v %s", err, block.Text)
				}
				return result
			}
		}
		t.Fatal("no MCP payload")
		return nil
	}
	if out := call("launch-browser", map[string]interface{}{}); out["success"] == false {
		t.Fatalf("private launch: %+v", out)
	}
	shutdownPending := true
	defer func() {
		if shutdownPending {
			call("shutdown-browser", map[string]interface{}{})
		}
	}()
	created := call("browser-act", map[string]interface{}{"operations": []interface{}{map[string]interface{}{"type": "session_create", "url": fixture.URL}}, "view": "full"})
	session := getStringFromMap(created, "session_id")
	if session == "" || created["success"] != true {
		t.Fatalf("session creation: %+v", created)
	}
	for deadline := time.Now().Add(8 * time.Second); ; {
		state := call("browser-observe", map[string]interface{}{"session_id": session, "mode": "text", "view": "compact"})
		if strings.Contains(fmt.Sprint(state), "Input event boundaries") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("real React fixture not ready: %+v", state)
		}
		time.Sleep(25 * time.Millisecond)
	}
	for _, view := range []string{"summary", "compact", "full"} {
		out := call("browser-observe", map[string]interface{}{"session_id": session, "mode": "input_trace", "trace_action": "arm", "root_selector": "#delivered", "view": view})
		if out["success"] != false || out["gated"] != true {
			t.Fatalf("MCP gate bypass: %+v", out)
		}
	}
	reason := call("browser-reason", map[string]interface{}{"session_id": session, "topic": "health", "view": "compact"})
	handles, _ := reason["evidence_handles"].([]interface{})
	if len(handles) == 0 {
		t.Fatalf("real reasoning yielded no authorization: %+v", reason)
	}
	handle := fmt.Sprint(handles[0])
	if asMap(reason["gate_grants"])["available"] != true {
		t.Fatalf("real reasoning did not issue authority: %+v", reason)
	}
	trace := func(action string, extra map[string]interface{}) map[string]interface{} {
		t.Helper()
		args := map[string]interface{}{"session_id": session, "mode": "input_trace", "trace_action": action, "approved_by_handle": handle, "view": "full"}
		for key, value := range extra {
			args[key] = value
		}
		return call("browser-observe", args)
	}
	for _, selector := range []string{"#delivered", "#prevented", "#blocked", "#key"} {
		for _, actionView := range []string{"summary", "compact", "full"} {
			armed := trace("arm", map[string]interface{}{"root_selector": selector, "max_trace_bytes": 32768, "view": actionView})
			if armed["success"] != true {
				t.Fatalf("MCP arm: %+v", armed)
			}
			id := getStringFromMap(asMap(armed["data"]), "trace_id")
			operations := []interface{}{map[string]interface{}{"type": "click", "ref": strings.TrimPrefix(selector, "#")}}
			if selector == "#key" {
				operations = append(operations, map[string]interface{}{"type": "key", "key": "Enter"})
			}
			if out := call("browser-act", map[string]interface{}{"session_id": session, "operations": operations, "settle_ms": 0}); out["success"] != true {
				t.Fatalf("MCP native action: %+v", out)
			}
			immediate := trace("read", map[string]interface{}{"trace_id": id, "view": actionView})
			if immediate["success"] != true || getStringFromMap(asMap(immediate["data"]), "trace_id") != id {
				t.Fatalf("immediate approved read/%s: %+v", actionView, immediate)
			}
			out := trace("stop", map[string]interface{}{"trace_id": id, "view": actionView})
			data := asMap(out["data"])
			if out["success"] != true || data["active"] != false || data["cleanup_confirmed"] != true || asInt(data["event_count"]) < 1 {
				t.Fatalf("MCP trace failed: %+v", out)
			}
			raw, _ := json.Marshal(out)
			if strings.Contains(string(raw), "Canary_") {
				t.Fatalf("private metadata escaped MCP trace: %s", raw)
			}
			for _, view := range []string{"summary", "compact", "full"} {
				read := call("browser-observe", map[string]interface{}{"session_id": session, "mode": "input_trace", "trace_action": "read", "trace_id": id, "approved_by_handle": handle, "view": view})
				if read["success"] != true || asMap(read["data"])["event_count"] != data["event_count"] {
					t.Fatalf("nondestructive read/all-view counts lost: %+v", read)
				}
			}
			t.Logf("MCP %s/%s arm -> native action -> immediate approved read/stop: events=%v phases=%v final=%v cleanup=%v", selector, actionView, data["event_count"], data["phase_count"], data["final_confirmed"], data["cleanup_confirmed"])
		}
	}
	bad := trace("arm", map[string]interface{}{"root_selector": "PRIVATE_SELECTOR_CANARY_992"})
	raw, _ := json.Marshal(bad)
	if bad["success"] != false || strings.Contains(string(raw), "PRIVATE_SELECTOR") {
		t.Fatalf("selector error echo: %s", raw)
	}
	call("shutdown-browser", map[string]interface{}{})
	shutdownPending = false
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "recorder", "*"))
	if err != nil || len(files) == 0 {
		t.Fatalf("recorder did not run: %v %v", files, err)
	}
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"trace_action", "trace_id", "PRIVATE_SELECTOR_CANARY", "current_target", "not_observable_after_dispatch"} {
			if strings.Contains(string(content), forbidden) {
				t.Fatalf("trace argument/evidence persisted in %s", path)
			}
		}
	}
	t.Log("fresh headed private stdio candidate advertised and gated trace mode; receiver/cancellation evidence worked across all views; enabled recorder excluded trace args/results")
}

// The source child serves only MCP on stdout, even after its parent closes the
// transport. Cleanup completes before exit; the test runner has no trailing
// PASS output to write into the closed protocol pipe.
func TestInputTraceMCPSourceChild(t *testing.T) {
	path := os.Getenv("BROWSERNERD_INPUT_TRACE_CHILD_CONFIG")
	if path == "" {
		t.Skip("source MCP child only")
	}
	// The client closes its stderr pipe before waiting for child cleanup.
	// Keep ordinary lifecycle logs off that transport during graceful EOF.
	log.SetOutput(io.Discard)
	serve := func() error {
		cfg, _, err := config.LoadWithWorkspace(path, config.WorkspaceOptions{Disable: true})
		if err != nil {
			return err
		}
		engine, err := mangle.NewEngine(cfg.Mangle)
		if err != nil {
			return err
		}
		manager := browser.NewSessionManager(cfg.Browser, engine)
		defer manager.Shutdown(context.Background())
		server, err := NewServer(cfg, manager, engine)
		if err != nil {
			return err
		}
		defer server.Close()
		return server.Start(context.Background())
	}
	if err := serve(); err != nil {
		fmt.Fprintln(os.Stderr, "source MCP child:", err)
		os.Exit(1)
	}
	os.Exit(0)
}
