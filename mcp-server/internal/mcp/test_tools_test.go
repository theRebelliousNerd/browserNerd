package mcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/mangle"

	"gopkg.in/yaml.v3"
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

func TestResolveTestOperationsUsesEnvironmentWithoutMutatingFixture(t *testing.T) {
	t.Setenv("BROWSERNERD_TEST_PASSWORD", "runtime-secret")
	operations := []map[string]interface{}{
		{
			"type":      "interact",
			"action":    "type",
			"ref":       "login-password",
			"value_env": "BROWSERNERD_TEST_PASSWORD",
		},
	}
	resolved, secrets, err := resolveTestOperations(operations)
	if err != nil {
		t.Fatalf("resolve failed: %v", err)
	}
	if resolved[0]["value"] != "runtime-secret" {
		t.Fatalf("expected runtime value, got %+v", resolved)
	}
	if !secrets["runtime-secret"] {
		t.Fatalf("resolved value was not recorded as a secret to scrub")
	}
	if resolved[0]["value_env"] != nil {
		t.Fatalf("resolved operation should not retain value_env: %+v", resolved)
	}
	if operations[0]["value_env"] != "BROWSERNERD_TEST_PASSWORD" || operations[0]["value"] != nil {
		t.Fatalf("portable fixture was mutated: %+v", operations)
	}
}

func TestResolveTestOperationsRefusesUnsetAndEmptyEnvironment(t *testing.T) {
	t.Setenv("BROWSERNERD_EMPTY_TEST_SECRET", "   ")
	for _, name := range []string{"BROWSERNERD_MISSING_TEST_SECRET", "BROWSERNERD_EMPTY_TEST_SECRET"} {
		_, _, err := resolveTestOperations([]map[string]interface{}{{
			"type":   "fill",
			"fields": []interface{}{map[string]interface{}{"label": "Password", "value_env": name}},
		}})
		if err == nil || !strings.Contains(err.Error(), "requires environment variable "+name) {
			t.Fatalf("expected an error naming %s, got %v", name, err)
		}
	}
}

// A fixture whose value_env is empty must fail before its first step: typing
// an empty password leaves the page on /login, which reads as a wrong password.
func TestRunTestStopsBeforeAnyStepOnEmptyValueEnv(t *testing.T) {
	t.Setenv("BROWSERNERD_EMPTY_TEST_SECRET", "")
	engine := setupTestEngine(t)
	res, err := (&BrowserTestTool{engine: engine}).Execute(context.Background(), map[string]interface{}{
		"operation": "run",
		"test": map[string]interface{}{
			"name":       "login",
			"session_id": "never-used",
			"operations": []interface{}{
				map[string]interface{}{"type": "fill", "fields": []interface{}{
					map[string]interface{}{"label": "Password", "value_env": "BROWSERNERD_EMPTY_TEST_SECRET"},
				}},
			},
			"assertions": []interface{}{map[string]interface{}{"name": "no errors", "query": "user_visible_error(S, _, _, _)", "expect": "absent"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]interface{})
	if m["success"] != false || m["replay"] != nil {
		t.Fatalf("an empty value_env must fail the run before replay: %+v", m)
	}
	msg := fmt.Sprint(m["error"])
	if !strings.Contains(msg, "BROWSERNERD_EMPTY_TEST_SECRET") || !strings.Contains(msg, "nothing ran") {
		t.Fatalf("error should name the variable and say nothing ran: %q", msg)
	}
}

func TestParseTestSpecReadsOperationsAndActionsAsTheSameSteps(t *testing.T) {
	assertion := []interface{}{map[string]interface{}{"name": "a", "query": "current_url(S, U)"}}
	for _, key := range []string{"operations", "actions"} {
		spec, err := parseTestSpec(map[string]interface{}{"test": map[string]interface{}{
			"name":       "steps",
			key:          []interface{}{map[string]interface{}{"type": "sleep", "duration_ms": 1}},
			"assertions": assertion,
		}})
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if len(spec.Operations) != 1 || len(spec.Actions) != 0 {
			t.Fatalf("%s: steps should land in Operations only: %+v", key, spec)
		}
	}
	encoded, err := yaml.Marshal(TestSpec{Name: "x", Operations: []map[string]interface{}{{"type": "sleep"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "operations:") || strings.Contains(string(encoded), "actions:") {
		t.Fatalf("a written fixture should carry operations only:\n%s", encoded)
	}
}

func TestParseTestSpecRefusesFixturesThatTestNothingOrMisspellKeys(t *testing.T) {
	cases := map[string]map[string]interface{}{
		"no operations and no assertions": {"name": "empty"},
		"both step keys": {
			"operations": []interface{}{map[string]interface{}{"type": "sleep", "duration_ms": 1}},
			"actions":    []interface{}{map[string]interface{}{"type": "sleep", "duration_ms": 1}},
		},
		"steps":                     {"steps": []interface{}{map[string]interface{}{"type": "sleep"}}},
		"misspelled assertion key":  {"assertions": []interface{}{map[string]interface{}{"query": "x(S)", "expected": "absent"}}},
		"assertion without a query": {"assertions": []interface{}{map[string]interface{}{"name": "x"}}},
		"bad expect":                {"assertions": []interface{}{map[string]interface{}{"query": "x(S)", "expect": "none"}}},
	}
	for name, fixture := range cases {
		if _, err := parseTestSpec(map[string]interface{}{"test": fixture}); err == nil {
			t.Fatalf("%s: expected the fixture to be refused", name)
		}
	}
}

func TestRunTestRefusesInvalidOperationsBeforeRunning(t *testing.T) {
	res, err := (&RunTestTool{engine: setupTestEngine(t)}).Execute(context.Background(), map[string]interface{}{
		"test": map[string]interface{}{
			"session_id": "never-used",
			"operations": []interface{}{map[string]interface{}{"type": "interact", "action": "click", "selector": "#save"}},
			"assertions": []interface{}{map[string]interface{}{"query": "current_url(S, U)"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]interface{})
	if m["success"] != false || m["replay"] != nil || !strings.Contains(fmt.Sprint(m["error"]), "selector") {
		t.Fatalf("invalid fixture operations must be refused before replay: %+v", m)
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
	if len(spec.Operations) != 3 || len(spec.Assertions) != 3 {
		t.Fatalf("unexpected fixture shape: %+v", spec)
	}
	if _, issues := validateTestOperations(spec.Operations); len(issues) > 0 {
		t.Fatalf("example fixture operations are invalid: %+v", issues)
	}
	engine := setupTestEngine(t)
	for _, assertion := range spec.Assertions {
		if _, err := queryAssertionRows(context.Background(), engine, assertion.Query); err != nil {
			t.Fatalf("assertion %q does not compile: %v", assertion.Name, err)
		}
	}
}
