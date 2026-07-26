package mcp

import (
	"context"
	"os"
	"strings"
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

func TestEvaluateQueryExpectFreshIgnoresHistoricalRows(t *testing.T) {
	engine := setupTestEngine(t)
	addNetFacts(t, engine, "historical", "https://api.example.com/old", 500, 100)
	baseline, err := queryAssertionRows(context.Background(), engine, "failed_request(S, _, _, _)")
	if err != nil {
		t.Fatalf("baseline query failed: %v", err)
	}

	result, passed := evaluateQueryExpectFresh(
		context.Background(),
		engine,
		"no new failures",
		"failed_request(S, _, _, _)",
		"absent",
		baseline,
	)
	if !passed {
		t.Fatalf("historical rows should not fail a fresh assertion: %+v", result)
	}
	if result["matched"] != 0 {
		t.Fatalf("expected zero fresh rows, got %+v", result)
	}

	addNetFacts(t, engine, "fresh", "https://api.example.com/new", 503, 100)
	result, passed = evaluateQueryExpectFresh(
		context.Background(),
		engine,
		"no new failures",
		"failed_request(S, _, _, _)",
		"absent",
		baseline,
	)
	if passed {
		t.Fatalf("new failure should fail a fresh assertion: %+v", result)
	}
	if result["matched"] != 1 {
		t.Fatalf("expected one fresh row, got %+v", result)
	}
}

func TestBrowserTestToolCreateInspectAndRun(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &BrowserTestTool{engine: engine}
	fixture := map[string]interface{}{
		"name": "health check",
		"assertions": []interface{}{
			map[string]interface{}{
				"name":   "no failed requests",
				"query":  "failed_request(S, _, _, _)",
				"expect": "absent",
			},
		},
	}

	created, err := tool.Execute(context.Background(), map[string]interface{}{
		"operation": "create",
		"test":      fixture,
	})
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	createMap := created.(map[string]interface{})
	if success, _ := createMap["success"].(bool); !success {
		t.Fatalf("expected create success: %+v", createMap)
	}
	testYAML, _ := createMap["test_yaml"].(string)
	if !strings.Contains(testYAML, "failed_request") {
		t.Fatalf("expected portable YAML fixture: %q", testYAML)
	}

	inspected, err := tool.Execute(context.Background(), map[string]interface{}{
		"operation": "inspect",
		"test_yaml": testYAML,
	})
	if err != nil {
		t.Fatalf("inspect failed: %v", err)
	}
	inspectMap := inspected.(map[string]interface{})
	if inspectMap["assertion_count"] != 1 {
		t.Fatalf("expected one assertion: %+v", inspectMap)
	}

	run, err := tool.Execute(context.Background(), map[string]interface{}{
		"operation": "run",
		"test_yaml": testYAML,
		"view":      "summary",
	})
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	runMap := run.(map[string]interface{})
	if success, _ := runMap["success"].(bool); !success {
		t.Fatalf("expected passing declarative test: %+v", runMap)
	}
	if runMap["status"] != "passed" {
		t.Fatalf("expected passed status: %+v", runMap)
	}
	if _, ok := runMap["evidence_handles"].([]string); !ok {
		t.Fatalf("expected progressive evidence handle: %+v", runMap)
	}
}

func TestBrowserTestToolMalformedFixtureReturnsStructuredError(t *testing.T) {
	tool := &BrowserTestTool{engine: setupTestEngine(t)}
	result, err := tool.Execute(context.Background(), map[string]interface{}{
		"operation": "run",
		"test_yaml": "assertions: [",
		"view":      "summary",
	})
	if err != nil {
		t.Fatalf("expected structured error, got: %v", err)
	}
	resultMap := result.(map[string]interface{})
	if success, _ := resultMap["success"].(bool); success {
		t.Fatalf("malformed fixture should fail: %+v", resultMap)
	}
	if resultMap["error"] == nil {
		t.Fatalf("expected parse error: %+v", resultMap)
	}
}

func TestResolveTestActionsUsesEnvironmentWithoutMutatingFixture(t *testing.T) {
	t.Setenv("BROWSERNERD_TEST_PASSWORD", "runtime-secret")
	actions := []map[string]interface{}{
		{
			"type":      "interact",
			"action":    "type",
			"ref":       "login-password",
			"value_env": "BROWSERNERD_TEST_PASSWORD",
		},
	}
	resolved, err := resolveTestActions(actions)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved[0]["value"] != "runtime-secret" {
		t.Fatalf("expected runtime value, got %+v", resolved)
	}
	if resolved[0]["value_env"] != nil {
		t.Fatalf("resolved operation should not retain value_env: %+v", resolved)
	}
	if actions[0]["value_env"] != "BROWSERNERD_TEST_PASSWORD" || actions[0]["value"] != nil {
		t.Fatalf("portable fixture was mutated: %+v", actions)
	}
}

func TestResolveTestActionsRequiresDeclaredEnvironment(t *testing.T) {
	_, err := resolveTestActions([]map[string]interface{}{{
		"type":      "interact",
		"value_env": "BROWSERNERD_MISSING_TEST_SECRET",
	}})
	if err == nil || !strings.Contains(err.Error(), "requires environment variable") {
		t.Fatalf("expected explicit missing environment error, got %v", err)
	}
}

func TestExampleLoginFixtureParsesAndCompilesAssertions(t *testing.T) {
	raw, err := os.ReadFile("../../testdata/fixtures/login.yaml")
	if err != nil {
		t.Fatalf("read example fixture: %v", err)
	}
	spec, err := parseTestSpec(map[string]interface{}{"test_yaml": string(raw)})
	if err != nil {
		t.Fatalf("parse example fixture: %v", err)
	}
	if len(spec.Actions) != 3 || len(spec.Assertions) != 3 {
		t.Fatalf("unexpected fixture shape: %+v", spec)
	}
	engine := setupTestEngine(t)
	for _, assertion := range spec.Assertions {
		if _, err := queryAssertionRows(context.Background(), engine, assertion.Query); err != nil {
			t.Fatalf("assertion %q does not compile: %v", assertion.Name, err)
		}
	}
}
