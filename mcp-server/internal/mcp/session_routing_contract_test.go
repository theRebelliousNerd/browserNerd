package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
	sdkmcp "github.com/mark3labs/mcp-go/mcp"
)

func TestSessionRoutingProviderSchema(t *testing.T) {
	props := actOperationItemSchema()["properties"].(map[string]interface{})
	for field, kind := range map[string]string{
		"browser_id": "string", "isolated": "boolean", "new_instance": "boolean",
		"target_id": "string", "source_session_id": "string",
	} {
		t.Run(field, func(t *testing.T) {
			p, ok := props[field].(map[string]interface{})
			if !ok || p["type"] != kind || p["description"] == "" {
				t.Fatalf("provider cannot express %s (%s): %+v", field, kind, p)
			}
		})
	}
	for _, operation := range []map[string]interface{}{
		{"type": "session_create", "url": "about:blank", "browser_id": "b", "isolated": true},
		{"type": "session_attach", "target_id": "target", "browser_id": "b"},
		{"type": "session_fork", "source_session_id": "source"},
		{"type": "browser_launch", "new_instance": true},
	} {
		if _, issues, _ := validateActOperations(ops(operation), false); len(issues) != 0 {
			t.Fatalf("advertised routing rejected: %+v", issues)
		}
	}
}

func TestSessionRoutingWrongAndUnknownFieldsRefused(t *testing.T) {
	for _, operation := range []map[string]interface{}{
		{"type": "session_create", "browser_idx": "b"},
		{"type": "navigate", "url": "about:blank", "browser_id": "b"},
		{"type": "session_fork", "isolated": false},
	} {
		out, err := (&BrowserActTool{}).Execute(context.Background(), map[string]interface{}{"operations": ops(operation)})
		if err != nil || asMap(out)["status"] != "invalid" {
			t.Fatalf("must reject before dispatch: %+v %v", out, err)
		}
	}
}

func TestSessionRoutingCompactMetadata(t *testing.T) {
	entry := map[string]interface{}{}
	liftActFields(entry, "session_create", map[string]interface{}{"session": &browser.Session{
		ID: "own-tab", BrowserID: "own-browser", Isolated: true, IsolatedDisplay: true,
	}})
	for key, value := range map[string]interface{}{
		"session_id": "own-tab", "browser_id": "own-browser", "isolated": true, "isolated_display": true,
	} {
		if entry[key] != value {
			t.Errorf("session receipt %s: got %v want %v", key, entry[key], value)
		}
	}
	entry = map[string]interface{}{}
	liftActFields(entry, "browser_launch", map[string]interface{}{"browser": &browser.BrowserInstance{
		ID: "own-browser", Managed: true, IsolatedDisplay: true,
	}})
	if entry["browser_id"] != "own-browser" || entry["isolated_display"] != true || entry["managed"] != true || entry["default"] != false {
		t.Fatalf("launch receipt loses owned browser metadata: %+v", entry)
	}
	for _, key := range []string{"browser_id", "isolated", "isolated_display", "managed", "default"} {
		if !strings.Contains(","+strings.Join(actCompactKeys, ",")+",", ","+key+",") {
			t.Errorf("compact receipts omit %s", key)
		}
	}
}

func TestFixtureSessionFlowRefusesBeforeReplay(t *testing.T) {
	for _, operations := range [][]interface{}{
		ops(map[string]interface{}{"type": "sleep", "duration_ms": 1}, map[string]interface{}{"type": "navigate", "url": "about:blank"}, map[string]interface{}{"type": "session_create"}),
		ops(map[string]interface{}{"type": "browser_launch"}, map[string]interface{}{"type": "key", "key": "Enter"}),
		ops(map[string]interface{}{"type": "session_fork"}),
	} {
		out, err := (&RunTestTool{}).Execute(context.Background(), map[string]interface{}{
			"test": map[string]interface{}{"operations": operations}, "stop_on_error": false,
		})
		m := asMap(out)
		if err != nil || m["success"] != false || m["replay"] != nil || !strings.Contains(fmt.Sprint(m["error"]), "op ") {
			t.Fatalf("session-flow rejection must name op before lifecycle/replay: %+v %v", out, err)
		}
	}
}

func TestFixtureSessionFlowCompleteSequence(t *testing.T) {
	for _, tc := range []struct {
		name, initial string
		steps         []interface{}
		valid         bool
	}{
		{"create_first", "", ops(map[string]interface{}{"type": "session_create"}, map[string]interface{}{"type": "navigate", "url": "about:blank"}), true},
		{"attach_first", "", ops(map[string]interface{}{"type": "session_attach", "target_id": "target"}, map[string]interface{}{"type": "key", "key": "Enter"}), true},
		{"explicit_fork", "", ops(map[string]interface{}{"type": "session_fork", "source_session_id": "source"}), true},
		{"explicit_focus", "", ops(map[string]interface{}{"type": "session_focus", "session_id": "target"}, map[string]interface{}{"type": "key", "key": "Enter"}), true},
		{"close_current", "initial", ops(map[string]interface{}{"type": "session_close"}, map[string]interface{}{"type": "key", "key": "Enter"}), false},
		{"close_other", "initial", ops(map[string]interface{}{"type": "session_close", "session_id": "other"}, map[string]interface{}{"type": "key", "key": "Enter"}), true},
		{"browser_close", "initial", ops(map[string]interface{}{"type": "browser_close", "browser_id": "browser"}, map[string]interface{}{"type": "key", "key": "Enter"}), false},
		{"recreate_after_close", "initial", ops(map[string]interface{}{"type": "session_close"}, map[string]interface{}{"type": "session_create"}, map[string]interface{}{"type": "key", "key": "Enter"}), true},
		{"create_with_dialog_needs_prior_session", "", ops(map[string]interface{}{"type": "session_create", "dialog": "accept"}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			steps, issues := validateTestOperations(func() []map[string]interface{} {
				out := make([]map[string]interface{}, len(tc.steps))
				for i, step := range tc.steps {
					out[i] = step.(map[string]interface{})
				}
				return out
			}())
			if len(issues) != 0 {
				t.Fatal(issues)
			}
			if err := validateFixtureSessionFlow(steps, tc.initial); (err == nil) != tc.valid {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
}

func TestFixtureSessionQueryBindingAndGlobalEscape(t *testing.T) {
	engine := setupTestEngine(t)
	ctx := context.Background()
	now := time.Now()
	if err := engine.AddFacts(ctx, []mangle.Fact{
		{Predicate: "current_url", Args: []interface{}{"anchor-A", "https://example.test/anchor-S"}, Timestamp: now},
		{Predicate: "current_url", Args: []interface{}{"private-B", "https://example.test/private"}, Timestamp: now},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, query, session string
		count                int
		bound                bool
	}{
		{"atom", `current_url(S, U)`, "private-B", 1, true},
		{"literal_S_unchanged", `current_url(S, "https://example.test/anchor-S")`, "private-B", 0, true},
		{"conjunction", `current_url(S, U), U != "https://example.test/anchor-S"`, "private-B", 1, true},
		{"rule_S_not_projected", `page(U) :- current_url(S, U)`, "private-B", 1, true},
		{"another_variable_global", `current_url(Session, U)`, "private-B", 2, false},
		{"explicit_literal_other_session", `current_url("anchor-A", U)`, "private-B", 1, false},
		{"no_session_global", `current_url(S, U)`, "", 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, bound, err := fixtureSessionQuery(tc.query, tc.session)
			if err != nil || bound != tc.bound {
				t.Fatalf("binding %q: %v %v", query, bound, err)
			}
			rows, err := queryAssertionRows(ctx, engine, query)
			if err != nil || len(rows) != tc.count {
				t.Fatalf("%q returned %+v, want %d: %v", query, rows, tc.count, err)
			}
		})
	}
}

func TestFixtureSessionFreshAndCurrentScopes(t *testing.T) {
	engine := setupTestEngine(t)
	ctx := context.Background()
	old := time.Now().Add(-time.Minute)
	if err := engine.AddFacts(ctx, []mangle.Fact{
		{Predicate: "net_request", Args: []interface{}{"private-B", "old-B", "GET", "https://example.test/old", "fetch", old.UnixMilli()}, Timestamp: old},
		{Predicate: "net_response", Args: []interface{}{"private-B", "old-B", int64(500), int64(1), int64(1)}, Timestamp: old},
		{Predicate: "current_url", Args: []interface{}{"anchor-A", "https://example.test/anchor"}, Timestamp: old},
		{Predicate: "current_url", Args: []interface{}{"private-B", "https://example.test/private"}, Timestamp: old},
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, query, scope, expect string
		passed                     bool
	}{
		{"old_B_event_not_fresh", "failed_request(S, _, _, _)", "fresh", "absent", true},
		{"old_B_event_is_current", "failed_request(S, _, _, _)", "current", "absent", false},
		{"unchanged_B_state_still_true", `current_url(S, "https://example.test/private")`, "fresh", "present", true},
		{"global_other_variable_preserved", "current_url(Other, U)", "current", "present", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := (&RunTestTool{engine: engine}).Execute(ctx, map[string]interface{}{
				"test":                map[string]interface{}{"session_id": "private-B", "operations": ops(map[string]interface{}{"type": "sleep", "duration_ms": 1}), "assertions": []interface{}{map[string]interface{}{"query": tc.query, "scope": tc.scope, "expect": tc.expect}}},
				"diagnose_on_failure": false,
			})
			if err != nil || asMap(out)["passed"] != tc.passed {
				t.Fatalf("scope changed: %+v %v", out, err)
			}
		})
	}
	// An explicit sessionless assertion fixture remains an engine-wide query.
	out, err := (&RunTestTool{engine: engine}).Execute(ctx, map[string]interface{}{"test": map[string]interface{}{"assertions": []interface{}{map[string]interface{}{"query": "current_url(S, U)"}}}})
	if err != nil || asMap(out)["passed"] != true || asMap(out)["assertions"].([]map[string]interface{})[0]["matched"] != 2 {
		t.Fatalf("sessionless query ceased to be global: %+v %v", out, err)
	}
}

func TestSessionRoutingMangleScopedRead(t *testing.T) {
	engine := setupTestEngine(t)
	ctx := context.Background()
	old := time.Now().Add(-time.Minute)
	seed := []mangle.Fact{
		{Predicate: "net_request", Args: []interface{}{"own-B", "own-1", "GET", "https://example.test/own-1", "fetch", old.UnixMilli()}, Timestamp: old},
		{Predicate: "net_request", Args: []interface{}{"own-B", "own-2", "GET", "https://example.test/own-2", "fetch", old.UnixMilli()}, Timestamp: old},
	}
	for i := 0; i < 5; i++ {
		seed = append(seed, mangle.Fact{Predicate: "net_request", Args: []interface{}{"foreign-A", fmt.Sprint(i), "GET", "https://example.test/foreign", "fetch", old.UnixMilli()}, Timestamp: old})
	}
	if err := engine.AddFacts(ctx, seed); err != nil {
		t.Fatal(err)
	}
	tool := &BrowserMangleTool{engine: engine}
	for _, tc := range []struct {
		name, session string
		limit, count  int
		last          string
	}{
		{"own_filter_before_limit", "own-B", 1, 1, "own-2"},
		{"own_all", "own-B", 100, 2, "own-2"},
		{"unknown_id_no_fallback", "missing-B", 100, 0, ""},
		{"global_explicit", "", 1, 1, "4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]interface{}{"operation": "read", "predicate_filter": "net_request", "limit": tc.limit, "view": "full"}
			if tc.session != "" {
				args["session_id"] = tc.session
			}
			out, err := tool.Execute(ctx, args)
			if err != nil {
				t.Fatal(err)
			}
			data := asMap(asMap(out)["data"])
			facts := data["facts"].([]mangle.Fact)
			if len(facts) != tc.count || data["count"] != tc.count {
				t.Fatalf("wrong scoped count %+v", out)
			}
			if tc.count > 0 && fmt.Sprint(facts[len(facts)-1].Args[1]) != tc.last {
				t.Fatalf("limit was applied before scope: %+v", out)
			}
			if tc.session != "" {
				for _, fact := range facts {
					if len(fact.Args) == 0 || fact.Args[0] != tc.session {
						t.Fatalf("foreign fact returned: %+v", out)
					}
				}
			}
		})
	}
	// A declared zero-argument fact must not be mistaken for session evidence.
	if err := engine.AddFacts(ctx, []mangle.Fact{{Predicate: "page_stable", Args: []interface{}{}, Timestamp: old}}); err != nil {
		t.Fatal(err)
	}
	out, err := tool.Execute(ctx, map[string]interface{}{"operation": "read", "session_id": "own-B", "view": "full", "limit": 100})
	if err != nil {
		t.Fatal(err)
	}
	for _, fact := range asMap(asMap(out)["data"])["facts"].([]mangle.Fact) {
		if len(fact.Args) == 0 || fact.Args[0] != "own-B" {
			t.Fatalf("unfiltered/no-argument fact: %+v", out)
		}
	}
}

func TestSessionRoutingMangleUnsupportedScopeInvalidParams(t *testing.T) {
	// No engine: scope refusal must precede all evaluation, push and waits.
	for _, operation := range []string{"query", "evaluate", "temporal", "await_fact", "await_conditions", "subscribe", "schema", "push", "submit_rule"} {
		t.Run(operation, func(t *testing.T) {
			out, err := (&BrowserMangleTool{}).Execute(context.Background(), map[string]interface{}{"operation": operation, "session_id": "own-B", "query": "current_url(S,U)", "facts": []interface{}{}, "timeout_ms": 1})
			if !errors.Is(err, sdkmcp.ErrInvalidParams) || out != nil {
				t.Fatalf("unsupported scope evaluated or silently ignored: %+v %v", out, err)
			}
		})
	}
	for _, invalid := range []interface{}{42, "", "  ", nil} {
		_, err := (&BrowserMangleTool{}).Execute(context.Background(), map[string]interface{}{"operation": "read", "session_id": invalid})
		if !errors.Is(err, sdkmcp.ErrInvalidParams) {
			t.Fatalf("malformed session became a global read: %v", err)
		}
	}
}
