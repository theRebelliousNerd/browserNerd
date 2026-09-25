package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

func TestGenerateTestFromActions(t *testing.T) {
	engine := setupTestEngine(t)
	base := time.Now()
	// Out-of-order insertion; the tool must sort by timestamp.
	facts := []mangle.Fact{
		{Predicate: "click_event", Args: []interface{}{testSessionID, "login-btn", base.Add(3 * time.Second).UnixMilli()}, Timestamp: base.Add(3 * time.Second)},
		{Predicate: "navigation_event", Args: []interface{}{testSessionID, "https://app/login", base.UnixMilli()}, Timestamp: base},
		{Predicate: "input_event", Args: []interface{}{testSessionID, "email", "user@example.com", base.Add(1 * time.Second).UnixMilli()}, Timestamp: base.Add(1 * time.Second)},
	}
	if err := engine.AddFacts(context.Background(), facts); err != nil {
		t.Fatal(err)
	}

	tool := &GenerateTestTool{engine: engine}
	res, err := tool.Execute(context.Background(), map[string]interface{}{"session_id": testSessionID, "name": "login flow"})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	m := res.(map[string]interface{})
	if m["operation_count"] != 3 {
		t.Fatalf("expected 3 operations, got %v", m["operation_count"])
	}

	spec := m["test"].(TestSpec)
	if len(spec.Operations) != 3 {
		t.Fatalf("expected 3 actions in spec, got %d", len(spec.Operations))
	}
	// Order must be navigate -> interact/type -> interact/click by timestamp.
	if spec.Operations[0]["type"] != "navigate" ||
		spec.Operations[1]["type"] != "interact" || spec.Operations[1]["action"] != "type" ||
		spec.Operations[2]["type"] != "interact" || spec.Operations[2]["action"] != "click" {
		t.Fatalf("actions out of order: %+v", spec.Operations)
	}
	if spec.Operations[1]["value"] != "user@example.com" {
		t.Fatalf("expected typed value captured, got %+v", spec.Operations[1])
	}
	if len(spec.Assertions) != 2 {
		t.Fatalf("expected 2 suggested assertions, got %d", len(spec.Assertions))
	}

	yamlStr := m["test_yaml"].(string)
	if !strings.Contains(yamlStr, "navigate") || !strings.Contains(yamlStr, "user_visible_error") {
		t.Fatalf("test_yaml missing expected content:\n%s", yamlStr)
	}
}

func TestGenerateTestSessionFilter(t *testing.T) {
	engine := setupTestEngine(t)
	now := time.Now()
	if err := engine.AddFacts(context.Background(), []mangle.Fact{
		{Predicate: "click_event", Args: []interface{}{"other-session", "x", now.UnixMilli()}, Timestamp: now},
	}); err != nil {
		t.Fatal(err)
	}
	tool := &GenerateTestTool{engine: engine}
	res, err := tool.Execute(context.Background(), map[string]interface{}{"session_id": testSessionID})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if res.(map[string]interface{})["operation_count"] != 0 {
		t.Fatalf("expected 0 operations for filtered session, got %v", res.(map[string]interface{})["operation_count"])
	}
}

func TestGenerateTestUsesEnvironmentReferenceForRedactedInput(t *testing.T) {
	engine := setupTestEngine(t)
	now := time.Now()
	if err := engine.AddFacts(context.Background(), []mangle.Fact{{
		Predicate: "input_event",
		Args:      []interface{}{testSessionID, "login-password", security.Redacted, now.UnixMilli()},
		Timestamp: now,
	}}); err != nil {
		t.Fatal(err)
	}
	result, err := (&GenerateTestTool{engine: engine}).Execute(context.Background(), map[string]interface{}{
		"session_id": testSessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	spec := result.(map[string]interface{})["test"].(TestSpec)
	if len(spec.Operations) != 1 {
		t.Fatalf("expected one action, got %+v", spec.Operations)
	}
	action := spec.Operations[0]
	if action["value"] != nil {
		t.Fatalf("generated fixture persisted a redacted literal as a value: %+v", action)
	}
	if action["value_env"] != "BROWSERNERD_TEST_LOGIN_PASSWORD" {
		t.Fatalf("expected environment-backed secret, got %+v", action)
	}
}

// Every keystroke records the field's value so far, and a click on an element
// without an id cannot be replayed: the draft keeps one type per field and
// counts the unreplayable click instead of emitting an empty ref.
func TestGenerateTestCollapsesKeystrokesAndSkipsAnonymousClicks(t *testing.T) {
	engine := setupTestEngine(t)
	base := time.Now()
	var facts []mangle.Fact
	for i, typed := range []string{"a", "ab", "abc"} {
		ts := base.Add(time.Duration(i) * time.Millisecond)
		facts = append(facts, mangle.Fact{Predicate: "input_event", Args: []interface{}{testSessionID, "name", typed, ts.UnixMilli()}, Timestamp: ts})
	}
	ts := base.Add(10 * time.Millisecond)
	facts = append(facts, mangle.Fact{Predicate: "click_event", Args: []interface{}{testSessionID, "", ts.UnixMilli()}, Timestamp: ts})
	if err := engine.AddFacts(context.Background(), facts); err != nil {
		t.Fatal(err)
	}
	res, err := (&GenerateTestTool{engine: engine}).Execute(context.Background(), map[string]interface{}{"session_id": testSessionID})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]interface{})
	spec := m["test"].(TestSpec)
	if len(spec.Operations) != 1 || spec.Operations[0]["value"] != "abc" {
		t.Fatalf("expected one type op holding the final value, got %+v", spec.Operations)
	}
	if m["skipped_clicks"] != 1 {
		t.Fatalf("expected the anonymous click to be counted, got %+v", m)
	}
	if _, issues := validateTestOperations(spec.Operations); len(issues) > 0 {
		t.Fatalf("a generated fixture must validate: %+v", issues)
	}
}
