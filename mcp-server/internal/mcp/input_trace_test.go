package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"browsernerd-mcp-server/internal/mangle"
)

func authorizeInputTrace(t *testing.T, engine *mangle.Engine, session string) string {
	t.Helper()
	return authorizeReason(t, engine, session)
}

func TestInputTraceReadStopExplicitContract(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	authorizeNoMatchingReason(t, engine, "own")
	handle := authorizeInputTrace(t, engine, "own")
	tool := &BrowserObserveTool{engine: engine}
	for _, action := range []string{"read", "stop"} {
		for _, view := range []string{"summary", "compact", "full"} {
			args := map[string]interface{}{"session_id": "own", "mode": "input_trace", "trace_action": action, "trace_id": "00000000-0000-0000-0000-000000000000", "view": view, "max_items": 20}
			out, err := tool.Execute(context.Background(), args)
			if err != nil || asMap(out)["gated"] != true {
				t.Fatalf("fresh reason auto-selected a grant: %+v %v", out, err)
			}
			for _, authorization := range []map[string]interface{}{
				{"approved_by_handle": handle}, {"gate_reason": "no_matching_tool"},
				{"gate_reason": "explicit_user_intent", "approved_by_handle": handle},
			} {
				copyArgs := make(map[string]interface{}, len(args)+2)
				for name, value := range args {
					copyArgs[name] = value
				}
				for name, value := range authorization {
					copyArgs[name] = value
				}
				if _, err := parseInputTraceArgs(copyArgs); err != nil {
					t.Fatalf("authorized read/stop shape refused: %v", err)
				}
				out, err := tool.Execute(context.Background(), copyArgs)
				if err != nil || asMap(out)["gated"] == true || asMap(out)["error"] != "input trace requires a live owned session" {
					t.Fatalf("explicit authority lost before ownership check: %+v %v", out, err)
				}
				for _, field := range []string{"root_ref", "root_selector", "duration_ms", "max_events", "max_trace_bytes"} {
					copyArgs[field] = "PRIVATE_CANARY"
					_, err := parseInputTraceArgs(copyArgs)
					if err == nil || !strings.Contains(err.Error(), "approved_by_handle/gate_reason") || !strings.Contains(err.Error(), "arm-only") || strings.Contains(err.Error(), "PRIVATE_CANARY") {
						t.Fatalf("contradictory or private validation error: %v", err)
					}
					delete(copyArgs, field)
				}
			}
		}
	}
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
