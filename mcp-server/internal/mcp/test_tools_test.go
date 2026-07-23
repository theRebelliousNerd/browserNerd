package mcp

import (
	"context"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/mangle"
)

func addNetFacts(t *testing.T, engine *mangle.Engine, reqID, url string, status, duration int64) {
	t.Helper()
	now := time.Now()
	err := engine.AddFacts(context.Background(), []mangle.Fact{
		{Predicate: "net_request", Args: []interface{}{testSessionID, reqID, "GET", url, "fetch", now.UnixMilli()}, Timestamp: now},
		{Predicate: "net_response", Args: []interface{}{testSessionID, reqID, status, int64(50), duration}, Timestamp: now},
	})
	if err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}
}

func TestRunTestTool_Pass(t *testing.T) {
	engine := setupTestEngine(t)
	// A slow but successful request: slow_api derived, no failed_request.
	addNetFacts(t, engine, "r1", "https://api.example.com/slow", 200, 1500)

	tool := &RunTestTool{engine: engine}
	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"test": map[string]interface{}{
			"name": "slow but ok",
			"assertions": []interface{}{
				map[string]interface{}{"name": "slow api present", "query": "slow_api(S, _, _, _)", "expect": "present"},
				map[string]interface{}{"name": "no failed requests", "query": "failed_request(S, _, _, _)", "expect": "absent"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	m := res.(map[string]interface{})
	if passed, _ := m["passed"].(bool); !passed {
		t.Fatalf("expected test to pass, got: %+v", m)
	}
	if _, hasDiag := m["diagnosis"]; hasDiag {
		t.Fatalf("did not expect diagnosis on a passing test: %+v", m["diagnosis"])
	}
}

func TestRunTestTool_FailWithDiagnosis(t *testing.T) {
	engine := setupTestEngine(t)
	// A failed request plus a console error -> failed_request + user_visible_error derived.
	addNetFacts(t, engine, "r2", "https://api.example.com/boom", 500, 100)
	now := time.Now()
	if err := engine.AddFacts(context.Background(), []mangle.Fact{
		{Predicate: "console_event", Args: []interface{}{testSessionID, "error", "request failed", now.UnixMilli()}, Timestamp: now},
	}); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	tool := &RunTestTool{engine: engine}
	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"test": map[string]interface{}{
			"name": "should have no failures",
			"assertions": []interface{}{
				// This assertion is violated: there IS a failed request.
				map[string]interface{}{"name": "no failed requests", "query": "failed_request(S, _, _, _)", "expect": "absent"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	m := res.(map[string]interface{})
	if passed, _ := m["passed"].(bool); passed {
		t.Fatalf("expected test to fail, got: %+v", m)
	}
	diag, ok := m["diagnosis"].(map[string]interface{})
	if !ok || len(diag) == 0 {
		t.Fatalf("expected non-empty diagnosis on failure, got: %+v", m["diagnosis"])
	}
	if _, ok := diag["failed_request"]; !ok {
		t.Fatalf("expected failed_request in diagnosis, got keys: %+v", diag)
	}
}

func TestRunTestTool_ConditionalAssertion(t *testing.T) {
	engine := setupTestEngine(t)
	addNetFacts(t, engine, "r3", "https://api.example.com/err", 503, 100)

	tool := &RunTestTool{engine: engine}
	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"test_yaml": `
name: server errors present
assertions:
  - name: has 5xx failure
    query: "server_err(Id) :- failed_request(_, Id, _, St), St >= 500."
    expect: present
`,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	m := res.(map[string]interface{})
	if passed, _ := m["passed"].(bool); !passed {
		t.Fatalf("expected conditional assertion to pass, got: %+v", m)
	}
	assertions := m["assertions"].([]map[string]interface{})
	if got := assertions[0]["matched"]; got != 1 {
		t.Fatalf("expected 1 matched row, got %v", got)
	}
}
