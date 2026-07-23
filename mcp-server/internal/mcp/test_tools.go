package mcp

import (
	"context"
	"fmt"
	"strings"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"

	"gopkg.in/yaml.v3"
)

// TestAssertion is a single check evaluated after a test's actions have run.
// Query is a Mangle query; Expect controls how its result count is scored.
type TestAssertion struct {
	Name   string `yaml:"name" json:"name"`
	Query  string `yaml:"query" json:"query"`
	Expect string `yaml:"expect" json:"expect"` // "present" (default) | "absent"
}

// TestSpec is a compact, declarative browser test: a sequence of actions to
// replay followed by Mangle assertions over the resulting fact state. It is
// the token-efficient alternative to an imperative Playwright script — the
// whole test is a handful of actions and conditions, and a failure returns a
// causal chain rather than a log dump.
type TestSpec struct {
	Name       string                   `yaml:"name" json:"name"`
	SessionID  string                   `yaml:"session_id" json:"session_id"`
	Actions    []map[string]interface{} `yaml:"actions" json:"actions"`
	Assertions []TestAssertion          `yaml:"assertions" json:"assertions"`
}

// diagnosticPredicates are the derived causal predicates surfaced when a test
// fails, in rough order of usefulness for root-causing a broken frontend.
var diagnosticPredicates = []string{
	"error_chain",
	"user_visible_error",
	"failed_request",
	"slow_api",
	"cascading_failure",
	"caused_by",
}

// RunTestTool replays a TestSpec's actions and evaluates its assertions.
type RunTestTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *RunTestTool) Name() string { return "run-test" }

func (t *RunTestTool) Description() string {
	return `Run a declarative browser test: replay actions, then evaluate Mangle assertions.

TOKEN COST: Low. A test is a list of actions + conditions, not an imperative
script, and a failure returns a causal chain instead of a trace dump.

A TEST HAS:
- actions[]: same shape as execute-plan ({type, ref, value}). Optional — omit to
  assert against the current fact state without acting.
- assertions[]: {name, query, expect}. query is a Mangle query. expect is
  "present" (default: pass if the query matches at least one row) or "absent"
  (pass if it matches nothing — e.g. "no console errors").

EXAMPLE (inline):
run-test(test: {
  name: "login works",
  session_id: "s1",
  actions: [
    {type: "type", ref: "email", value: "user@example.com"},
    {type: "type", ref: "password", value: "secret"},
    {type: "click", ref: "login-btn"}
  ],
  assertions: [
    {name: "reached dashboard", query: "login_succeeded(S)", expect: "present"},
    {name: "no visible errors", query: "user_visible_error(S, _, _, _)", expect: "absent"}
  ]
})

You may pass the test as a "test" object or a "test_yaml" string.

Returns: {name, passed, assertions[], replay?, diagnosis?}. When passed is false,
diagnosis contains derived causal facts (error_chain, failed_request, slow_api, ...).`
}

func (t *RunTestTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"test": map[string]interface{}{
				"type":        "object",
				"description": "Test spec: {name, session_id, actions[], assertions[]}. Alternative to test_yaml.",
			},
			"test_yaml": map[string]interface{}{
				"type":        "string",
				"description": "Test spec as a YAML string. Alternative to the test object.",
			},
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Session to run against. Overrides the spec's session_id when set.",
			},
			"stop_on_error": map[string]interface{}{
				"type":        "boolean",
				"description": "Stop replaying actions on the first error (default: true).",
			},
			"delay_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Delay between actions in milliseconds (default: 100).",
			},
			"diagnose_on_failure": map[string]interface{}{
				"type":        "boolean",
				"description": "Attach derived causal facts when the test fails (default: true).",
			},
		},
	}
}

func (t *RunTestTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	spec, err := parseTestSpec(args)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	if len(spec.Assertions) == 0 {
		return map[string]interface{}{"success": false, "error": "test has no assertions"}, nil
	}

	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		sessionID = spec.SessionID
	}

	result := map[string]interface{}{
		"name": spec.Name,
	}

	// Replay actions by delegating to execute-plan, so the action semantics stay
	// in one place. A failed replay does not short-circuit assertions — they are
	// the source of truth and still run for diagnostic value.
	replayOK := true
	if len(spec.Actions) > 0 {
		if sessionID == "" {
			return map[string]interface{}{"success": false, "error": "session_id is required when a test has actions"}, nil
		}
		plan := &ExecutePlanTool{sessions: t.sessions, engine: t.engine}
		planArgs := map[string]interface{}{
			"session_id":    sessionID,
			"actions":       toInterfaceSlice(spec.Actions),
			"stop_on_error": getBoolArg(args, "stop_on_error", true),
			"delay_ms":      getIntArg(args, "delay_ms", 100),
		}
		replay, replayErr := plan.Execute(ctx, planArgs)
		if replayErr != nil {
			replayOK = false
			result["replay"] = map[string]interface{}{"success": false, "error": replayErr.Error()}
		} else {
			result["replay"] = replay
			if m, ok := replay.(map[string]interface{}); ok {
				if failed, ok := m["failed"].(int); ok && failed > 0 {
					replayOK = false
				}
				if success, ok := m["success"].(bool); ok && !success {
					replayOK = false
				}
			}
		}
	}

	// Evaluate assertions.
	assertionResults := make([]map[string]interface{}, 0, len(spec.Assertions))
	allPassed := true
	for i, a := range spec.Assertions {
		ar, passed := t.evaluateAssertion(ctx, i, a)
		if !passed {
			allPassed = false
		}
		assertionResults = append(assertionResults, ar)
	}

	passed := allPassed && replayOK
	result["assertions"] = assertionResults
	result["passed"] = passed

	// On failure, surface derived causal facts for token-efficient drill-down.
	if !passed && getBoolArg(args, "diagnose_on_failure", true) {
		if diag := t.diagnose(ctx); len(diag) > 0 {
			result["diagnosis"] = diag
		}
	}

	return result, nil
}

// evaluateAssertion runs one assertion's query and scores it against Expect.
func (t *RunTestTool) evaluateAssertion(ctx context.Context, index int, a TestAssertion) (map[string]interface{}, bool) {
	name := a.Name
	if name == "" {
		name = fmt.Sprintf("assertion_%d", index)
	}
	expect := strings.ToLower(strings.TrimSpace(a.Expect))
	if expect == "" {
		expect = "present"
	}

	out := map[string]interface{}{
		"name":   name,
		"expect": expect,
	}

	query := strings.TrimSpace(a.Query)
	if query == "" {
		out["passed"] = false
		out["error"] = "assertion has no query"
		return out, false
	}
	if !strings.HasSuffix(query, ".") {
		query += "."
	}

	rows, err := t.engine.Query(ctx, query)
	if err != nil {
		out["passed"] = false
		out["error"] = err.Error()
		return out, false
	}

	count := len(rows)
	out["matched"] = count

	var passed bool
	switch expect {
	case "absent":
		passed = count == 0
	default: // "present"
		passed = count > 0
	}
	out["passed"] = passed

	// Include a small sample of bindings to explain the result without bloating output.
	if count > 0 {
		sample := rows
		if len(sample) > 3 {
			sample = sample[:3]
		}
		out["sample"] = normalizeQueryBindings(sample)
	}

	return out, passed
}

// diagnose evaluates the derived causal predicates and returns those that have
// matching facts, capped for compactness.
func (t *RunTestTool) diagnose(ctx context.Context) map[string]interface{} {
	diag := make(map[string]interface{})
	for _, pred := range diagnosticPredicates {
		facts, err := t.engine.Evaluate(ctx, pred)
		if err != nil || len(facts) == 0 {
			continue
		}
		samples := make([]interface{}, 0, 3)
		for _, f := range facts {
			if len(samples) >= 3 {
				break
			}
			samples = append(samples, f.Args)
		}
		diag[pred] = map[string]interface{}{
			"count":   len(facts),
			"samples": samples,
		}
	}
	return diag
}

// parseTestSpec extracts a TestSpec from either a "test" object or a
// "test_yaml" string. The object path round-trips through YAML so nested
// actions and assertions decode uniformly.
func parseTestSpec(args map[string]interface{}) (TestSpec, error) {
	var spec TestSpec

	if raw := getStringArg(args, "test_yaml"); raw != "" {
		if err := yaml.Unmarshal([]byte(raw), &spec); err != nil {
			return spec, fmt.Errorf("parse test_yaml: %w", err)
		}
		return spec, nil
	}

	if testObj, ok := args["test"]; ok {
		encoded, err := yaml.Marshal(testObj)
		if err != nil {
			return spec, fmt.Errorf("encode test: %w", err)
		}
		if err := yaml.Unmarshal(encoded, &spec); err != nil {
			return spec, fmt.Errorf("decode test: %w", err)
		}
		return spec, nil
	}

	return spec, fmt.Errorf("provide a test object or test_yaml string")
}

func toInterfaceSlice(actions []map[string]interface{}) []interface{} {
	out := make([]interface{}, 0, len(actions))
	for _, a := range actions {
		out = append(out, a)
	}
	return out
}
