package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

func TestProgressiveObserveDeepGateSchema(t *testing.T) {
	properties := (&BrowserObserveTool{}).InputSchema()["properties"].(map[string]interface{})
	for _, key := range []string{"gate_reason", "approved_by_handle"} {
		field, ok := properties[key].(map[string]interface{})
		if !ok || field["type"] != "string" {
			t.Fatalf("gate field %q is not advertised as a string: %#v", key, properties[key])
		}
	}
	got := properties["gate_reason"].(map[string]interface{})["enum"]
	want := (&ReifyReactTool{}).InputSchema()["properties"].(map[string]interface{})["gate_reason"].(map[string]interface{})["enum"]
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("progressive/focused gate reasons differ: %v != %v", got, want)
	}
}

func TestProgressiveObserveDeepGateRefusals(t *testing.T) {
	for _, mode := range []string{"react", "dom_snapshot"} {
		for _, view := range []string{"summary", "compact", "full"} {
			for _, scenario := range []string{"missing", "explicit_without_handle", "unauthorized_reason", "invalid_reason", "raw_foreign_handle", "raw_foreign_reason", "raw_expired_handle", "raw_expired_reason"} {
				t.Run(mode+"/"+view+"/"+scenario, func(t *testing.T) {
					engine := testMangleEngineForProgressive(t)
					args := map[string]interface{}{"session_id": "inspection", "mode": mode, "view": view}
					now := time.Now()
					fact := mangle.Fact{Predicate: "disclosure_handle", Args: []interface{}{"inspection", "reason:inspection:health", "reason", now.UnixMilli()}, Timestamp: now}
					switch scenario {
					case "explicit_without_handle":
						args["gate_reason"] = "explicit_user_intent"
					case "unauthorized_reason":
						args["gate_reason"] = "no_matching_tool"
					case "invalid_reason":
						args["gate_reason"] = "skip_gate"
					case "raw_foreign_handle", "raw_expired_handle":
						args["approved_by_handle"] = "reason:inspection:health"
					case "raw_foreign_reason", "raw_expired_reason":
						args["gate_reason"] = "no_matching_tool"
						fact.Predicate = "js_gate_open"
						fact.Args = []interface{}{"inspection", "no_matching_tool", now.UnixMilli()}
					}
					if strings.HasPrefix(scenario, "raw_foreign_") {
						fact.Args[0] = "another-session"
					}
					if strings.HasPrefix(scenario, "raw_expired_") {
						fact.Timestamp = now.Add(-jsGateTTL - time.Minute)
					}
					if strings.HasPrefix(scenario, "raw_foreign_") || strings.HasPrefix(scenario, "raw_expired_") {
						if err := engine.AddFacts(context.Background(), []mangle.Fact{fact}); err != nil {
							t.Fatal(err)
						}
					}
					handleCount := len(engine.FactsByPredicate("disclosure_handle"))
					result, err := (&BrowserObserveTool{engine: engine}).Execute(context.Background(), args)
					if err != nil {
						t.Fatal(err)
					}
					out := asMap(result)
					if out["success"] != false || out["status"] != "gated" || out["gated"] != true || out["recommended_tool"] != "browser-reason" || out["error"] == "" {
						t.Fatalf("gate evidence lost: %#v", out)
					}
					if out["evidence_handles"] != nil || out["node_count"] != nil || out["component_count"] != nil || strings.Contains(fmt.Sprint(out["summary"]), "0 ") {
						t.Fatalf("refusal claims successful inspection evidence: %#v", out)
					}
					if len(engine.FactsByPredicate("disclosure_handle")) != handleCount {
						t.Fatal("denied observation emitted a disclosure handle")
					}
				})
			}
		}
	}
}

func TestProgressiveObserveDeepGateForwardsAuthorization(t *testing.T) {
	for _, mode := range []string{"react", "dom_snapshot"} {
		for _, authorization := range []string{"handle", "reason", "explicit"} {
			t.Run(mode+"/"+authorization, func(t *testing.T) {
				engine := testMangleEngineForProgressive(t)
				manager := browser.NewSessionManager(config.BrowserConfig{}, engine)

				args := map[string]interface{}{"session_id": "unknown-inspection", "mode": mode}
				if authorization == "reason" {
					authorizeNoMatchingReason(t, engine, "unknown-inspection")
					args["gate_reason"] = "no_matching_tool"
				} else {
					args["approved_by_handle"] = authorizeReason(t, engine, "unknown-inspection")
					if authorization == "explicit" {
						args["gate_reason"] = "explicit_user_intent"
					}
				}

				result, err := (&BrowserObserveTool{sessions: manager, engine: engine}).Execute(context.Background(), args)
				if err != nil {
					t.Fatal(err)
				}
				out := asMap(result)
				if out["success"] != false || out["status"] != "error" || !strings.Contains(fmt.Sprint(out["error"]), "unknown session") || out["gated"] == true {
					t.Fatalf("authorization did not reach focused inspection: %#v", out)
				}
			})
		}
	}
}

func TestDeepObservationResponsePreservesOutcomes(t *testing.T) {
	for _, mode := range []string{"react", "dom_snapshot"} {
		for _, view := range []string{"summary", "compact", "full"} {
			for _, scenario := range []struct {
				name   string
				data   map[string]interface{}
				err    error
				status string
			}{
				{"error", nil, fmt.Errorf("capture unavailable"), "error"},
				{"timeout", nil, fmt.Errorf("capture: %w", context.DeadlineExceeded), "timeout"},
				{"inner_timeout", map[string]interface{}{"status": "timeout", "error": "capture budget expired"}, nil, "timeout"},
				{"inner_error", map[string]interface{}{"success": false, "error": "capture refused"}, nil, "error"},
				{"false_ok", map[string]interface{}{"success": false, "status": "ok", "error": "capture refused"}, nil, "error"},
				{"missing_receipt", nil, nil, "error"},
			} {
				t.Run(mode+"/"+view+"/"+scenario.name, func(t *testing.T) {
					out := deepObservationResponse(mode, view, scenario.data, scenario.err)
					if out["success"] != false || out["status"] != scenario.status || out["error"] == "" || out["summary"] == "" || out["evidence_handles"] != nil {
						t.Fatalf("failure lost: %#v", out)
					}
				})
			}
			out := deepObservationResponse(mode, view, map[string]interface{}{"success": true, "facts": 7, "status": "captured"}, nil)
			if out["success"] != true || out["status"] != "ok" || out["summary"] == "" || strings.Contains(fmt.Sprint(out["summary"]), "0 ") {
				t.Fatalf("success invented counts: %#v", out)
			}
		}
	}
}
