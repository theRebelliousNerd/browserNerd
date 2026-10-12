package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"

	"gopkg.in/yaml.v3"
)

// TestAssertion is a single check evaluated after a test's actions have run.
// Query is a Mangle query; Expect controls how its result count is scored.
type TestAssertion struct {
	Name   string `yaml:"name" json:"name"`
	Query  string `yaml:"query" json:"query"`
	Expect string `yaml:"expect" json:"expect"`                   // "present" (default) | "absent"
	Scope  string `yaml:"scope,omitempty" json:"scope,omitempty"` // fresh (action tests) | current
}

// TestSpec is a compact, declarative browser test: a sequence of browser-act
// operations to replay followed by Mangle assertions over the resulting fact
// state. It is the token-efficient alternative to an imperative Playwright
// script. The whole test is a handful of operations and conditions, and a
// failure returns a causal chain rather than a log dump.
//
// Steps are written under operations, the key browser-act uses. A fixture
// may spell it actions; parseTestSpec moves those into Operations, so a
// normalized spec and every fixture this server writes carry operations only.
type TestSpec struct {
	Name       string                   `yaml:"name" json:"name"`
	SessionID  string                   `yaml:"session_id,omitempty" json:"session_id,omitempty"`
	Operations []map[string]interface{} `yaml:"operations,omitempty" json:"operations,omitempty"`
	Actions    []map[string]interface{} `yaml:"actions,omitempty" json:"actions,omitempty"`
	Assertions []TestAssertion          `yaml:"assertions" json:"assertions"`
}

// testSpecKeys and testAssertionKeys are the keys a fixture may use; any
// other key is a mistake that would otherwise be dropped without a word.
var (
	testSpecKeys      = []string{"name", "session_id", "operations", "actions", "assertions"}
	testAssertionKeys = []string{"name", "query", "expect", "scope"}
)

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
	sessions                *browser.SessionManager
	engine                  *mangle.Engine
	disableUnsafeJavaScript bool
	redactor                *security.Redactor
	pathPolicy              *security.PathPolicy
}

func (t *RunTestTool) Name() string { return "run-test" }

func (t *RunTestTool) Description() string {
	return `Run a declarative browser test: replay operations, then evaluate Mangle assertions.

TOKEN COST: Low. A test is a list of operations + conditions, not an imperative
script, and a failure returns a causal chain instead of a trace dump.

A TEST HAS:
- operations[]: browser-act operations (actions[] is accepted as the same
  thing). Optional; omit to assert against the current fact state. They are
  validated before anything runs, and a value_env naming an unset or empty
  variable stops the run before the first step.
- assertions[]: {name, query, expect, scope}. query is a Mangle query. expect
  is "present" (default: pass if the query matches at least one row) or
  "absent" (pass if it matches nothing, e.g. "no console errors"). scope
  "fresh" (default when the test has operations) counts what the run caused
  plus state that still holds, such as the tab's current URL; an event from
  before the run never counts. "current" counts every buffered row.
  Omit assertions to replay the operations only: the result says
  replayed, never passed.

EXAMPLE (inline):
run-test(test: {
  name: "login works",
  session_id: "s1",
  operations: [
    {type: "interact", action: "type", label: "Email", value: "user@example.com"},
    {type: "interact", action: "type", label: "Password", value_env: "LOGIN_PASSWORD"},
    {type: "interact", action: "click", text: "Sign in"}
  ],
  assertions: [
    {name: "reached dashboard", query: "login_succeeded(S)", expect: "present"},
    {name: "no visible errors", query: "user_visible_error(S, _, _, _)", expect: "absent"}
  ]
})

You may pass the test as a "test" object or a "test_yaml" string.

Returns: {name, passed, assertions[], replay?, diagnosis?}, or {replay_only,
replayed} for a fixture without assertions. When the run fails, diagnosis
contains derived causal facts (error_chain, failed_request, slow_api, ...).`
}

func (t *RunTestTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"test": map[string]interface{}{
				"type":        "object",
				"description": "Test spec: {name, session_id, operations[], assertions[]}. Alternative to test_yaml.",
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
	operations, issues := validateTestOperations(spec.Operations)
	if len(issues) > 0 {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("fixture operations are invalid; nothing ran. op %d %s: %s", issues[0].Index, issues[0].Type, issues[0].Message),
			"issues":  issues,
		}, nil
	}

	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		sessionID = spec.SessionID
	}
	if len(operations) > 0 && sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required when a test has operations"}, nil
	}

	resolvedOperations, secrets, err := resolveTestOperations(operations)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error() + "; nothing ran"}, nil
	}

	baselines := make([][]mangle.QueryResult, len(spec.Assertions))
	if len(operations) > 0 {
		for idx, assertion := range spec.Assertions {
			baselines[idx], _ = queryAssertionRows(ctx, t.engine, assertion.Query)
		}
	}
	// The run starts once its baselines are read, and acts only after the
	// clock has moved past that instant: a clock that advances in coarse ticks
	// would otherwise stamp the last fact before the run and the run's first
	// fact alike, and a fresh assertion could not tell them apart.
	runStart := time.Now()
	if len(operations) > 0 {
		waitForClockPast(ctx, runStart)
	}

	result := map[string]interface{}{
		"name":          spec.Name,
		"started_at_ms": runStart.UnixMilli(),
	}
	if sessionID != "" {
		result["session_id"] = sessionID
	}

	// Replay through browser-act so fixtures use the same operation vocabulary
	// as interactive MCP automation.
	replayOK := true
	if len(operations) > 0 {
		act := &BrowserActTool{
			sessions:                t.sessions,
			engine:                  t.engine,
			redactor:                t.redactor,
			disableUnsafeJavaScript: t.disableUnsafeJavaScript,
			pathPolicy:              t.pathPolicy,
			secrets:                 secrets,
		}
		actArgs := map[string]interface{}{
			"session_id":    sessionID,
			"operations":    toInterfaceSlice(resolvedOperations),
			"stop_on_error": getBoolArg(args, "stop_on_error", true),
			"view":          "compact",
			"include_specs": false,
		}
		replay, replayErr := act.Execute(ctx, actArgs)
		if replayErr != nil {
			replayOK = false
			result["replay"] = map[string]interface{}{"success": false, "error": replayErr.Error()}
		} else {
			result["replay"] = replay
			if m, ok := replay.(map[string]interface{}); ok {
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
		ar, passed := t.evaluateAssertion(ctx, i, a, baselines[i], len(operations) > 0, runStart)
		if !passed {
			allPassed = false
		}
		assertionResults = append(assertionResults, ar)
	}

	result["assertions"] = assertionResults
	passed := allPassed && replayOK
	if len(spec.Assertions) == 0 {
		// A replay with nothing asserted proves its steps ran - every locator
		// resolved and every typed value read back - and nothing about what
		// they did. It is reported as replayed, never as passed.
		result["replay_only"] = true
		result["replayed"] = replayOK
		result["note"] = "no assertions were checked: this proves the steps ran, not what they caused"
	} else {
		result["passed"] = passed
	}

	// On failure, surface derived causal facts for token-efficient drill-down.
	if !passed && getBoolArg(args, "diagnose_on_failure", true) {
		// A test that acted is diagnosed from what its run caused, not from
		// failures already on the page before it started.
		sinceMs := int64(0)
		if len(operations) > 0 {
			sinceMs = asInt64(result["started_at_ms"])
		}
		if diag := causalDiagnosis(ctx, t.engine, sessionID, sinceMs); len(diag) > 0 {
			result["diagnosis"] = diag
		}
	}

	return result, nil
}

// waitForClockPast returns once time.Now() is after instant, or when ctx ends.
// It waits at most one clock tick.
func waitForClockPast(ctx context.Context, instant time.Time) {
	for !time.Now().After(instant) {
		if sleepWithContext(ctx, time.Millisecond) != nil {
			return
		}
	}
}

// evaluateAssertion runs one assertion's query and scores it against Expect.
// runStart is the instant after the test's baselines were read; the test's
// own facts are stamped after it.
func (t *RunTestTool) evaluateAssertion(
	ctx context.Context,
	index int,
	a TestAssertion,
	baseline []mangle.QueryResult,
	actionTest bool,
	runStart time.Time,
) (map[string]interface{}, bool) {
	name := a.Name
	if name == "" {
		name = fmt.Sprintf("assertion_%d", index)
	}
	scope := strings.ToLower(strings.TrimSpace(a.Scope))
	if scope == "" {
		if actionTest {
			scope = "fresh"
		} else {
			scope = "current"
		}
	}
	if scope == "fresh" {
		return evaluateQueryExpectFresh(ctx, t.engine, name, a.Query, a.Expect, baseline, runStart)
	}
	return evaluateQueryExpect(ctx, t.engine, name, a.Query, a.Expect)
}

// evaluateQueryExpectFresh scores the rows that are true for the run started
// at runStart. A row the baseline did not have is the run's own. A row the
// baseline already had counts only while it still holds without the events
// from before the run - the tab still on /login, not a failed request from an
// earlier page. Subtracting every baseline row instead reads a state that
// never changed as absent, so an absence assertion passed with its fact true.
func evaluateQueryExpectFresh(
	ctx context.Context,
	engine *mangle.Engine,
	name, query, expect string,
	baseline []mangle.QueryResult,
	runStart time.Time,
) (map[string]interface{}, bool) {
	expect = strings.ToLower(strings.TrimSpace(expect))
	if expect == "" {
		expect = "present"
	}
	out := map[string]interface{}{"name": name, "expect": expect, "scope": "fresh"}
	rows, err := queryAssertionRows(ctx, engine, query)
	if err != nil {
		out["passed"] = false
		out["error"] = err.Error()
		return out, false
	}
	fresh := subtractQueryRows(rows, baseline)
	stillTrue := []mangle.QueryResult{}
	if carried := subtractQueryRows(rows, fresh); len(carried) > 0 {
		viewRows, viewErr := queryAssertionRowsSince(ctx, engine, query, runStart)
		if viewErr != nil {
			out["passed"] = false
			out["error"] = fmt.Sprintf("could not tell whether rows from before the run still hold: %v", viewErr)
			return out, false
		}
		stillTrue = intersectQueryRows(carried, subtractQueryRows(viewRows, fresh))
	}
	matched := append(append([]mangle.QueryResult{}, fresh...), stillTrue...)
	out["matched"] = len(matched)
	out["total_matched"] = len(rows)
	if len(stillTrue) > 0 {
		out["still_true"] = len(stillTrue)
	}
	passed := len(matched) > 0
	if expect == "absent" {
		passed = len(matched) == 0
	}
	out["passed"] = passed
	if len(matched) > 0 {
		sample := matched
		if len(sample) > 3 {
			sample = sample[:3]
		}
		out["sample"] = normalizeQueryBindings(sample)
	}
	return out, passed
}

func queryAssertionRowsSince(ctx context.Context, engine *mangle.Engine, query string, since time.Time) ([]mangle.QueryResult, error) {
	statement, err := assertionStatement(query)
	if err != nil {
		return nil, err
	}
	return engine.QuerySince(ctx, statement, since)
}

// intersectQueryRows keeps each row of rows as many times as other has it.
func intersectQueryRows(rows, other []mangle.QueryResult) []mangle.QueryResult {
	counts := make(map[string]int, len(other))
	for _, row := range other {
		counts[queryResultFingerprint(row)]++
	}
	kept := make([]mangle.QueryResult, 0, len(rows))
	for _, row := range rows {
		key := queryResultFingerprint(row)
		if counts[key] == 0 {
			continue
		}
		counts[key]--
		kept = append(kept, row)
	}
	return kept
}

func queryAssertionRows(ctx context.Context, engine *mangle.Engine, query string) ([]mangle.QueryResult, error) {
	statement, err := assertionStatement(query)
	if err != nil {
		return nil, err
	}
	return engine.Query(ctx, statement)
}

// assertionStatement is an assertion's query as a terminated statement.
func assertionStatement(query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", fmt.Errorf("no query")
	}
	if !strings.HasSuffix(query, ".") {
		query += "."
	}
	return query, nil
}

func subtractQueryRows(rows, baseline []mangle.QueryResult) []mangle.QueryResult {
	counts := make(map[string]int, len(baseline))
	for _, row := range baseline {
		counts[queryResultFingerprint(row)]++
	}
	fresh := make([]mangle.QueryResult, 0, len(rows))
	for _, row := range rows {
		key := queryResultFingerprint(row)
		if counts[key] > 0 {
			counts[key]--
			continue
		}
		fresh = append(fresh, row)
	}
	return fresh
}

func queryResultFingerprint(row mangle.QueryResult) string {
	encoded, err := json.Marshal(row)
	if err != nil {
		return fmt.Sprintf("%v", row)
	}
	return string(encoded)
}

// evaluateQueryExpect runs a Mangle query and scores its result cardinality
// against an expectation: "absent" passes on zero matches, "present" (default)
// passes on one or more. It returns a compact result map and the pass flag.
// Shared by run-test assertions and spec-conformance invariants.
func evaluateQueryExpect(ctx context.Context, engine *mangle.Engine, name, query, expect string) (map[string]interface{}, bool) {
	expect = strings.ToLower(strings.TrimSpace(expect))
	if expect == "" {
		expect = "present"
	}

	out := map[string]interface{}{
		"name":   name,
		"expect": expect,
	}

	query = strings.TrimSpace(query)
	if query == "" {
		out["passed"] = false
		out["error"] = "no query"
		return out, false
	}
	if !strings.HasSuffix(query, ".") {
		query += "."
	}

	rows, err := engine.Query(ctx, query)
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

// causalDiagnosis evaluates the derived causal predicates and returns those
// with matching facts, capped for compactness. Shared drill-down for failing
// tests and spec violations.
//
// sessionID (when set) keeps the facts of that session. sinceMs (when set)
// keeps a fact only if it names a request that started at or after sinceMs,
// or carries an epoch-ms timestamp at or after it: derived causal facts carry
// one or the other.
func causalDiagnosis(ctx context.Context, engine *mangle.Engine, sessionID string, sinceMs int64) map[string]interface{} {
	fresh := map[string]bool{}
	if sinceMs > 0 {
		for _, f := range engine.FactsByPredicate("net_request") {
			if len(f.Args) >= 6 && (sessionID == "" || fmt.Sprint(f.Args[0]) == sessionID) && asInt64(f.Args[5]) >= sinceMs {
				fresh[fmt.Sprint(f.Args[1])] = true
			}
		}
	}
	inScope := func(args []interface{}) bool {
		if sessionID != "" && (len(args) == 0 || fmt.Sprint(args[0]) != sessionID) {
			return false
		}
		if sinceMs <= 0 {
			return true
		}
		for _, a := range args[1:] {
			if fresh[fmt.Sprint(a)] {
				return true
			}
			if ts := asInt64(a); ts > 1e12 && ts >= sinceMs {
				return true
			}
		}
		return false
	}
	diag := make(map[string]interface{})
	for _, pred := range diagnosticPredicates {
		facts, err := engine.Evaluate(ctx, pred)
		if err != nil || len(facts) == 0 {
			continue
		}
		count := 0
		samples := make([]interface{}, 0, 3)
		for _, f := range facts {
			if !inScope(f.Args) {
				continue
			}
			count++
			if len(samples) < 3 {
				samples = append(samples, f.Args)
			}
		}
		if count == 0 {
			continue
		}
		diag[pred] = map[string]interface{}{
			"count":   count,
			"samples": samples,
		}
	}
	return diag
}

// parseTestSpec extracts a TestSpec from either a "test" object or a
// "test_yaml" string. The object path round-trips through YAML so nested
// operations and assertions decode uniformly. It refuses a key it does not
// know, a fixture that tests nothing, and steps given under both spellings,
// and returns the steps under Operations.
func parseTestSpec(args map[string]interface{}) (TestSpec, error) {
	var spec TestSpec
	var raw []byte
	if text := getStringArg(args, "test_yaml"); text != "" {
		raw = []byte(text)
	} else if testObj, ok := args["test"]; ok {
		encoded, err := yaml.Marshal(testObj)
		if err != nil {
			return spec, fmt.Errorf("encode test: %w", err)
		}
		raw = encoded
	} else {
		return spec, fmt.Errorf("provide a test object or test_yaml string")
	}

	var shape map[string]interface{}
	if err := yaml.Unmarshal(raw, &shape); err != nil {
		return spec, fmt.Errorf("parse test: %w", err)
	}
	if err := checkFixtureShape(shape); err != nil {
		return spec, err
	}
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		return spec, fmt.Errorf("parse test: %w", err)
	}
	if len(spec.Operations) > 0 && len(spec.Actions) > 0 {
		return spec, fmt.Errorf("the fixture has both operations and actions; put every step under operations")
	}
	if len(spec.Actions) > 0 {
		spec.Operations, spec.Actions = spec.Actions, nil
	}
	if len(spec.Operations) == 0 && len(spec.Assertions) == 0 {
		return spec, fmt.Errorf("the fixture has no operations and no assertions, so it would test nothing")
	}
	return spec, nil
}

// checkFixtureShape refuses keys a fixture does not have, so a misspelled
// key fails instead of being dropped.
func checkFixtureShape(shape map[string]interface{}) error {
	if len(shape) == 0 {
		return fmt.Errorf("the test is empty")
	}
	for key := range shape {
		if inList(testSpecKeys, key) {
			continue
		}
		msg := fmt.Sprintf("unknown fixture key %q", key)
		switch {
		case key == "steps" || key == "ops" || key == "step":
			msg += `; steps go under "operations"`
		case closestName(key, testSpecKeys) != "":
			msg += fmt.Sprintf("; did you mean %q?", closestName(key, testSpecKeys))
		default:
			msg += "; a fixture has name, session_id, operations, assertions"
		}
		return fmt.Errorf("%s", msg)
	}
	list, _ := shape["assertions"].([]interface{})
	for i, rawAssertion := range list {
		assertion, ok := rawAssertion.(map[string]interface{})
		if !ok {
			return fmt.Errorf("assertions[%d] must be {name, query, expect}", i)
		}
		for key := range assertion {
			if !inList(testAssertionKeys, key) {
				msg := fmt.Sprintf("assertions[%d]: unknown key %q", i, key)
				if s := closestName(key, testAssertionKeys); s != "" {
					msg += fmt.Sprintf("; did you mean %q?", s)
				} else {
					msg += "; an assertion has name, query, expect, scope"
				}
				return fmt.Errorf("%s", msg)
			}
		}
		if strings.TrimSpace(fmt.Sprint(assertion["query"])) == "" || assertion["query"] == nil {
			return fmt.Errorf("assertions[%d] needs a query", i)
		}
		if expect, ok := assertion["expect"]; ok && !inList([]string{"present", "absent"}, strings.ToLower(fmt.Sprint(expect))) {
			return fmt.Errorf("assertions[%d]: expect must be present or absent (got %q)", i, fmt.Sprint(expect))
		}
		if scope, ok := assertion["scope"]; ok && !inList([]string{"fresh", "current"}, strings.ToLower(fmt.Sprint(scope))) {
			return fmt.Errorf("assertions[%d]: scope must be fresh or current (got %q)", i, fmt.Sprint(scope))
		}
	}
	return nil
}

func inList(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// validateTestOperations applies browser-act's operation contract to a
// fixture's steps, admitting value_env.
func validateTestOperations(operations []map[string]interface{}) ([]map[string]interface{}, []actOpIssue) {
	if len(operations) == 0 {
		return nil, nil
	}
	normalized, issues, _ := validateActOperations(toInterfaceSlice(operations), true)
	return normalized, issues
}

func toInterfaceSlice(actions []map[string]interface{}) []interface{} {
	out := make([]interface{}, 0, len(actions))
	for _, a := range actions {
		out = append(out, a)
	}
	return out
}

// resolveTestOperations copies a fixture's operations and resolves explicit
// value_env fields only at execution time, returning the resolved values so
// the run can keep them out of its result. Resolved values are never written
// back to the portable fixture.
func resolveTestOperations(operations []map[string]interface{}) ([]map[string]interface{}, map[string]bool, error) {
	resolved := make([]map[string]interface{}, 0, len(operations))
	secrets := map[string]bool{}
	for idx, operation := range operations {
		copyOp := cloneStringMap(operation)
		if err := resolveValueEnv(copyOp, fmt.Sprintf("operations[%d]", idx), secrets); err != nil {
			return nil, nil, err
		}
		if fields, ok := copyOp["fields"].([]interface{}); ok {
			resolvedFields := make([]interface{}, 0, len(fields))
			for fieldIdx, raw := range fields {
				field, ok := raw.(map[string]interface{})
				if !ok {
					resolvedFields = append(resolvedFields, raw)
					continue
				}
				copyField := cloneStringMap(field)
				if err := resolveValueEnv(copyField, fmt.Sprintf("operations[%d].fields[%d]", idx, fieldIdx), secrets); err != nil {
					return nil, nil, err
				}
				resolvedFields = append(resolvedFields, copyField)
			}
			copyOp["fields"] = resolvedFields
		}
		resolved = append(resolved, copyOp)
	}
	return resolved, secrets, nil
}

func cloneStringMap(source map[string]interface{}) map[string]interface{} {
	copyMap := make(map[string]interface{}, len(source))
	for key, value := range source {
		copyMap[key] = value
	}
	return copyMap
}

// resolveValueEnv replaces value_env with the variable's value. An unset or
// empty variable is an error that names the variable and never a value: typed
// into a login form it would submit an empty password, and the failure would
// look like a wrong one.
func resolveValueEnv(value map[string]interface{}, location string, secrets map[string]bool) error {
	envName := strings.TrimSpace(getStringFromMap(value, "value_env"))
	if envName == "" {
		return nil
	}
	if !validEnvironmentName(envName) {
		return fmt.Errorf("%s has invalid value_env name %q", location, envName)
	}
	resolved, ok := os.LookupEnv(envName)
	if !ok {
		return fmt.Errorf("%s requires environment variable %s, which is not set in the BrowserNERD server's environment", location, envName)
	}
	if strings.TrimSpace(resolved) == "" {
		return fmt.Errorf("%s requires environment variable %s, which is empty", location, envName)
	}
	value["value"] = resolved
	delete(value, "value_env")
	secrets[resolved] = true
	return nil
}

func validEnvironmentName(name string) bool {
	for idx, r := range name {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') ||
			r == '_' || (idx > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return name != ""
}
