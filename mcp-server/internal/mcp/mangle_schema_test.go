package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/mangle"
)

func TestBrowserMangleSchemaOperation(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &BrowserMangleTool{engine: engine}
	ctx := context.Background()

	res, err := tool.Execute(ctx, map[string]interface{}{"operation": "schema"})
	if err != nil {
		t.Fatal(err)
	}
	out := res.(map[string]interface{})
	modules, ok := out["modules"].(map[string][]string)
	if !ok || len(modules["builtin:00_core_events"]) == 0 {
		t.Fatalf("an unfiltered compact schema lists names per module: %v", out)
	}
	if tokens := responseTokens(t, res); tokens > 2500 {
		t.Fatalf("unfiltered compact schema costs ~%d tokens", tokens)
	}
	t.Logf("unfiltered compact schema: ~%d tokens, %v", responseTokens(t, res), out["summary"])

	res, err = tool.Execute(ctx, map[string]interface{}{"operation": "schema", "filter": "net_response"})
	if err != nil {
		t.Fatal(err)
	}
	out = res.(map[string]interface{})
	lines, _ := out["predicates"].([]string)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "net_response(SessionId, Id, Status, Latency, Duration)  base builtin:00_core_events") {
		t.Fatalf("filtered schema must carry the signature and module: %v", out)
	}

	res, err = tool.Execute(ctx, map[string]interface{}{"operation": "schema", "filter": "zzz_nothing"})
	if err != nil {
		t.Fatal(err)
	}
	if out = res.(map[string]interface{}); out["count"] != 0 || out["hint"] == nil {
		t.Fatalf("an empty listing must say why: %v", out)
	}
}

// The agent's measured failure: a join and a wrong-arity query through the
// tool. The join answers; the wrong arity names the signature.
func TestBrowserMangleQueryJoinAndArity(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &BrowserMangleTool{engine: engine}
	ctx := context.Background()
	now := time.Now()
	_ = engine.AddFacts(ctx, []mangle.Fact{
		{Predicate: "net_request", Args: []interface{}{"s1", "r1", "GET", "http://app/api/fail", "", now.UnixMilli()}, Timestamp: now},
		{Predicate: "net_response", Args: []interface{}{"s1", "r1", int64(500), int64(3), int64(4)}, Timestamp: now},
	})

	res, err := tool.Execute(ctx, map[string]interface{}{"operation": "query", "query": "net_request(S, R, M, U, I, T), net_response(S, R, St, L, D)", "view": "full"})
	if err != nil {
		t.Fatal(err)
	}
	out := res.(map[string]interface{})
	if out["success"] != true {
		t.Fatalf("conjunctive query failed: %v", out)
	}
	data := out["data"].(map[string]interface{})
	rows := data["results"].([]map[string]interface{})
	if len(rows) != 1 || rows[0]["U"] != "http://app/api/fail" || fmt.Sprintf("%v", rows[0]["St"]) != "500" {
		t.Fatalf("expected the failed request joined to its URL, got %v", rows)
	}

	res, _ = tool.Execute(ctx, map[string]interface{}{"operation": "query", "query": "net_response(R, 500, U, T)."})
	out = res.(map[string]interface{})
	if out["success"] != false || !strings.Contains(fmt.Sprintf("%v", out["error"]), "net_response(SessionId, Id, Status, Latency, Duration)") {
		t.Fatalf("a wrong-arity query must fail naming the signature, got %v", out)
	}
}

func TestAwaitFactSeesDerivedPredicates(t *testing.T) {
	engine := setupTestEngine(t)
	now := time.Now()
	_ = engine.AddFacts(context.Background(), []mangle.Fact{
		{Predicate: "net_request", Args: []interface{}{"s1", "r1", "GET", "http://app/api/fail", "", now.UnixMilli()}, Timestamp: now},
		{Predicate: "net_response", Args: []interface{}{"s1", "r1", int64(503), int64(3), int64(4)}, Timestamp: now},
	})
	tool := &AwaitFactTool{engine: engine}
	res, err := tool.Execute(context.Background(), map[string]interface{}{"predicate": "failed_request", "timeout_ms": 1000})
	if err != nil {
		t.Fatal(err)
	}
	if status := res.(map[string]interface{})["status"]; status != "passed" {
		t.Fatalf("await_fact on a derived predicate must see its derivation, got %v", status)
	}
}
