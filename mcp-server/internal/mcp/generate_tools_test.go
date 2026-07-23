package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/mangle"
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
	if m["actions_count"] != 3 {
		t.Fatalf("expected 3 actions, got %v", m["actions_count"])
	}

	spec := m["test"].(TestSpec)
	if len(spec.Actions) != 3 {
		t.Fatalf("expected 3 actions in spec, got %d", len(spec.Actions))
	}
	// Order must be navigate -> type -> click by timestamp.
	if spec.Actions[0]["type"] != "navigate" || spec.Actions[1]["type"] != "type" || spec.Actions[2]["type"] != "click" {
		t.Fatalf("actions out of order: %+v", spec.Actions)
	}
	if spec.Actions[1]["value"] != "user@example.com" {
		t.Fatalf("expected typed value captured, got %+v", spec.Actions[1])
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
	if res.(map[string]interface{})["actions_count"] != 0 {
		t.Fatalf("expected 0 actions for filtered session, got %v", res.(map[string]interface{})["actions_count"])
	}
}
