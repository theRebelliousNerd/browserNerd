package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/mangle"
)

func validateOne(t *testing.T, op map[string]interface{}) (map[string]interface{}, []actOpIssue, []string) {
	t.Helper()
	ops, issues, notes := validateActOperations([]interface{}{op}, false)
	return ops[0], issues, notes
}

func TestValidateActOperationsRejectsUnknownFieldsWithTheFix(t *testing.T) {
	cases := []struct {
		op   map[string]interface{}
		want string
	}{
		{map[string]interface{}{"type": "navigate", "urll": "http://x"}, `use "url"`},
		{map[string]interface{}{"type": "interact", "action": "click", "selector": "#save"}, "CSS selectors are not accepted"},
		{map[string]interface{}{"type": "interact", "action": "click", "text": "Save", "url": "/x"}, "url belongs to navigate"},
		{map[string]interface{}{"type": "fill", "fields": []interface{}{map[string]interface{}{"lable": "Qty", "value": "7"}}}, `use "label"`},
		{map[string]interface{}{"type": "navigte", "url": "http://x"}, `did you mean "navigate"`},
	}
	for _, c := range cases {
		_, issues, _ := validateOne(t, c.op)
		if len(issues) == 0 || !strings.Contains(issues[0].Message, c.want) {
			t.Fatalf("%v: expected an issue containing %q, got %+v", c.op, c.want, issues)
		}
	}
}

func TestValidateActOperationsNormalizesSleepAliasesAndCapsSleep(t *testing.T) {
	op, issues, notes := validateOne(t, map[string]interface{}{"type": "sleep", "ms": float64(1500)})
	if len(issues) > 0 || op["duration_ms"] != float64(1500) || op["ms"] != nil {
		t.Fatalf("ms should normalize to duration_ms: %+v %+v", op, issues)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "ms -> duration_ms") {
		t.Fatalf("the normalization should be reported: %+v", notes)
	}
	op, issues, _ = validateOne(t, map[string]interface{}{"type": "sleep", "seconds": float64(2)})
	if len(issues) > 0 || op["duration_ms"] != float64(2000) {
		t.Fatalf("seconds should convert to duration_ms: %+v %+v", op, issues)
	}
	_, issues, _ = validateOne(t, map[string]interface{}{"type": "sleep", "duration_ms": float64(90000)})
	if len(issues) == 0 || !strings.Contains(issues[0].Message, "capped at 60000") {
		t.Fatalf("a sleep over the cap must fail loudly: %+v", issues)
	}
	_, issues, _ = validateOne(t, map[string]interface{}{"type": "sleep"})
	if len(issues) == 0 || !strings.Contains(issues[0].Message, "needs duration_ms") {
		t.Fatalf("a sleep without a duration must fail, not sleep a default: %+v", issues)
	}
}

func TestValidateActOperationsChecksLocatorsAndValues(t *testing.T) {
	bad := []map[string]interface{}{
		{"type": "interact", "action": "click"},
		{"type": "interact", "action": "click", "ref": "a", "text": "b"},
		{"type": "interact", "action": "click", "name": "Save"},
		{"type": "interact", "action": "type", "x": 1.0, "y": 2.0, "value": "v"},
		{"type": "interact", "action": "type", "label": "Name"},
		{"type": "interact", "action": "click", "x": 1.0},
		{"type": "interact", "action": "smash", "text": "Save"},
		{"type": "interact", "action": "type", "label": "Password", "value_env": "SECRET"},
		{"type": "fill", "fields": []interface{}{}},
		{"type": "key", "key": "Ctrl-a"},
		{"type": "navigate", "url": "http://x", "timeout_ms": float64(500000)},
		{"type": "interact", "action": "click", "text": "Delete", "prompt_text": "yes"},
		{"type": "dialog"},
	}
	for _, op := range bad {
		if _, issues, _ := validateOne(t, op); len(issues) == 0 {
			t.Fatalf("expected %v to be rejected", op)
		}
	}
	good := []map[string]interface{}{
		{"type": "interact", "action": "click", "role": "button", "name": "Save"},
		{"type": "interact", "action": "click", "text": "Browse nodes", "index": float64(3)},
		{"type": "interact", "action": "click", "x": 10.0, "y": 20.0},
		{"type": "interact", "action": "click", "text": "Delete", "dialog": "accept"},
		{"type": "fill", "fields": []interface{}{map[string]interface{}{"label": "Quantity", "value": float64(7)}}},
		{"type": "key", "key": "Control+Shift+P"},
		{"type": "dialog", "accept": true, "prompt_text": "yes"},
		{"type": "click", "text": "Save"},
	}
	for _, op := range good {
		if _, issues, _ := validateOne(t, op); len(issues) > 0 {
			t.Fatalf("expected %v to validate, got %+v", op, issues)
		}
	}
	op, _, _ := validateOne(t, map[string]interface{}{"type": "fill", "fields": []interface{}{map[string]interface{}{"label": "Quantity", "value": float64(7)}}})
	field := op["fields"].([]interface{})[0].(map[string]interface{})
	if field["value"] != "7" {
		t.Fatalf("a numeric value should be carried as the exact string, got %#v", field["value"])
	}
	op, _, notes := validateOne(t, map[string]interface{}{"type": "click", "text": "Save"})
	if op["type"] != "interact" || op["action"] != "click" || len(notes) != 1 {
		t.Fatalf("type click should normalize to interact click with a note: %+v %+v", op, notes)
	}
}

func TestParseKeyChord(t *testing.T) {
	cases := map[string]string{
		"Enter":           "Enter",
		"a":               "a",
		"Control+a":       "Control+a",
		"ctrl+A":          "Control+A",
		"Shift+Tab":       "Shift+Tab",
		"Meta+k":          "Meta+k",
		"Control+Shift+P": "Control+Shift+P",
		"+":               "+",
		"Control++":       "Control++",
		"esc":             "Escape",
	}
	for spec, label := range cases {
		chord, err := parseKeyChord(spec, nil)
		if err != nil {
			t.Fatalf("%q: %v", spec, err)
		}
		if chord.label != label {
			t.Fatalf("%q: label %q, want %q", spec, chord.label, label)
		}
	}
	chord, err := parseKeyChord("a", []string{"Ctrl"})
	if err != nil || chord.label != "Control+a" {
		t.Fatalf("separate modifiers should join the chord: %+v %v", chord, err)
	}
	nonASCII := string(rune(233))
	for _, bad := range []string{"Ctrl-a", "Hyper+a", "Enterr", nonASCII} {
		if _, err := parseKeyChord(bad, nil); err == nil {
			t.Fatalf("%q should be refused", bad)
		}
	}
}

func TestActOpOutcomeNeverLosesTheReason(t *testing.T) {
	ok, msg := actOpOutcome(map[string]interface{}{"success": false, "error": "unknown key"}, nil)
	if ok || msg != "unknown key" {
		t.Fatalf("an inner error must be lifted: %v %q", ok, msg)
	}
	ok, msg = actOpOutcome(map[string]interface{}{"success": false}, nil)
	if ok || msg == "" {
		t.Fatalf("a failure with no reason must still say so: %v %q", ok, msg)
	}
	if ok, _ := actOpOutcome(map[string]interface{}{"status": "timeout"}, nil); !ok {
		t.Fatalf("a result without success=false is a success")
	}
}

func TestBrowserActRejectsAnInvalidBatchBeforeRunningAnything(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &BrowserActTool{engine: engine}
	start := time.Now()
	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"operations": []interface{}{
			map[string]interface{}{"type": "sleep", "duration_ms": float64(1500)},
			map[string]interface{}{"type": "sleep", "ms": float64(10), "extra": true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("the first op ran although the batch was invalid")
	}
	m := res.(map[string]interface{})
	if m["success"] != false || m["status"] != "invalid" || m["issues"] == nil {
		t.Fatalf("expected an invalid batch: %+v", m)
	}
	if !strings.Contains(m["summary"].(string), `unknown field "extra"`) {
		t.Fatalf("the summary should name the problem: %v", m["summary"])
	}
}

func TestBrowserActCompactShowsErrorsOnlyForFailures(t *testing.T) {
	engine := setupTestEngine(t)
	sessions := testSessionManagerForProgressive(t, engine)
	tool := &BrowserActTool{sessions: sessions, engine: engine}
	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"session_id": "no-such-session",
		"operations": []interface{}{
			map[string]interface{}{"type": "sleep", "duration_ms": float64(1)},
			map[string]interface{}{"type": "key", "key": "Enter"},
		},
		"view": "compact",
	})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]interface{})
	results := m["results"].([]map[string]interface{})
	if _, has := results[0]["error"]; has {
		t.Fatalf("a successful op should carry no error key: %+v", results[0])
	}
	if msg, _ := results[1]["error"].(string); !strings.Contains(msg, "session not found") {
		t.Fatalf("a failed op must carry its reason in compact view: %+v", results[1])
	}
	if !strings.Contains(m["summary"].(string), "first failure op 1 key") {
		t.Fatalf("the summary should state the first failure: %v", m["summary"])
	}
}

func TestBrowserActIncludeSpecsDefaultsOff(t *testing.T) {
	schema := (&BrowserActTool{}).InputSchema()
	desc := schema["properties"].(map[string]interface{})["include_specs"].(map[string]interface{})["description"].(string)
	if !strings.Contains(desc, "default false") {
		t.Fatalf("include_specs should document a false default: %q", desc)
	}
}

func TestCollectActEffectsDigestsFailuresFromFacts(t *testing.T) {
	engine := setupTestEngine(t)
	since := time.Now()
	later := since.Add(10 * time.Millisecond)
	before := since.Add(-time.Second)
	err := engine.AddFacts(context.Background(), []mangle.Fact{
		{Predicate: "net_request", Args: []interface{}{testSessionID, "old", "GET", "http://app/api/missing", "", before.UnixMilli()}, Timestamp: before},
		{Predicate: "net_response", Args: []interface{}{testSessionID, "old", 404, int64(1), int64(1)}, Timestamp: before},
		{Predicate: "net_request", Args: []interface{}{testSessionID, "r1", "GET", "http://app/api/fail", "", later.UnixMilli()}, Timestamp: later},
		{Predicate: "net_response", Args: []interface{}{testSessionID, "r1", 500, int64(1), int64(1)}, Timestamp: later},
		{Predicate: "net_request", Args: []interface{}{testSessionID, "r2", "GET", "http://app/api/fail", "", later.UnixMilli()}, Timestamp: later},
		{Predicate: "net_response", Args: []interface{}{testSessionID, "r2", 500, int64(1), int64(1)}, Timestamp: later},
		{Predicate: "net_request", Args: []interface{}{"other-session", "r3", "POST", "http://app/api/x", "", later.UnixMilli()}, Timestamp: later},
		{Predicate: "net_response", Args: []interface{}{"other-session", "r3", 503, int64(1), int64(1)}, Timestamp: later},
		{Predicate: "console_event", Args: []interface{}{testSessionID, "error", "Save failed with status 500", later.UnixMilli()}, Timestamp: later},
		{Predicate: "console_event", Args: []interface{}{testSessionID, "warning", "Deprecated prop used", later.UnixMilli()}, Timestamp: later},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := effectsWindow{since: since, sessions: map[string]bool{testSessionID: true}}
	effects := collectActEffects(context.Background(), engine, nil, actBaseline{url: "http://app/index.html"}, testSessionID, w)

	failed, _ := effects["failed_requests"].([]string)
	if len(failed) != 1 || failed[0] != "GET /api/fail 500 (x2)" {
		t.Fatalf("expected one grouped failure on this session only, got %+v", effects)
	}
	errs, _ := effects["errors"].([]string)
	if len(errs) != 1 || errs[0] != "Save failed with status 500" {
		t.Fatalf("expected the console error and not the warning, got %+v", effects)
	}
	raw, _ := json.Marshal(effects)
	if len(raw)/4 > 60 {
		t.Fatalf("the digest should stay small, got ~%d tokens: %s", len(raw)/4, raw)
	}
	line := effectsSummary(effects)
	if !strings.Contains(line, "GET /api/fail 500") || !strings.Contains(line, "Save failed") {
		t.Fatalf("the summary line should carry the digest: %q", line)
	}
}
