package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/mangle"
)

func authorizeInputTrace(t *testing.T, engine *mangle.Engine, session string) string {
	t.Helper()
	handle := "reason:" + session + ":trace-proof"
	now := time.Now()
	if err := engine.AddFacts(context.Background(), []mangle.Fact{{Predicate: "disclosure_handle", Args: []interface{}{session, handle, "reason", now.UnixMilli()}, Timestamp: now}}); err != nil {
		t.Fatal(err)
	}
	return handle
}

func TestInputTraceSchemaAndRefusals(t *testing.T) {
	props := (&BrowserObserveTool{}).InputSchema()["properties"].(map[string]interface{})
	for _, name := range []string{"trace_action", "trace_id", "duration_ms", "max_events", "max_trace_bytes", "root_ref", "root_selector"} {
		if props[name] == nil {
			t.Fatalf("missing schema field %s", name)
		}
	}
	for _, view := range []string{"summary", "compact", "full"} {
		for _, action := range []string{"arm", "read", "stop"} {
			engine := testMangleEngineForProgressive(t)
			handle := authorizeInputTrace(t, engine, "different")
			for _, approval := range []string{"", handle} {
				out, err := (&BrowserObserveTool{engine: engine}).Execute(context.Background(), map[string]interface{}{"mode": "input_trace", "session_id": "own", "view": view, "trace_action": action, "approved_by_handle": approval})
				if err != nil || asMap(out)["success"] != false || asMap(out)["status"] != "gated" {
					t.Fatalf("gate lost: %v %+v", err, out)
				}
			}
		}
	}
	for _, bad := range []map[string]interface{}{
		{}, {"trace_action": "bad"}, {"trace_action": "arm"}, {"trace_action": "arm", "root_ref": "x", "root_selector": "y"},
		{"trace_action": "arm", "root_selector": "#ok", "script": "PRIVATE_CANARY"},
		{"trace_action": "arm", "root_selector": "#ok", "duration_ms": 99},
		{"trace_action": "arm", "root_selector": "#ok", "max_events": 257},
		{"trace_action": "arm", "root_selector": "#ok", "max_trace_bytes": 32769},
		{"trace_action": "arm", "root_selector": "#ok", "max_items": 0.5},
		{"trace_action": "read", "trace_id": "x"}, {"trace_action": "stop", "trace_id": "x", "root_ref": "PRIVATE_CANARY"},
	} {
		_, err := parseInputTraceArgs(bad)
		if err == nil || strings.Contains(err.Error(), "PRIVATE_CANARY") {
			t.Fatalf("invalid option admitted or echoed: %v", err)
		}
	}
	for _, action := range []string{"arm", "read", "stop"} {
		if recordInputTraceCall("browser-observe", map[string]interface{}{"mode": "input_trace", "trace_action": action}) {
			t.Fatal("trace payload would be recorded")
		}
	}
	if !recordInputTraceCall("browser-observe", map[string]interface{}{"mode": "interactive"}) {
		t.Fatal("unrelated recording changed")
	}
	raw, _ := json.Marshal(props)
	if !strings.Contains(string(raw), "input_trace") {
		t.Fatal("mode absent")
	}
}
