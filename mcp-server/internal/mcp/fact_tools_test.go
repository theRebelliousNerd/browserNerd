package mcp

import (
	"context"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

const testSessionID = "session-1"

func setupTestEngine(t *testing.T) *mangle.Engine {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}
	engine, err := mangle.NewEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	return engine
}

func TestPushFactsTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &PushFactsTool{engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "push-facts" {
			t.Errorf("expected name 'push-facts', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
	})

	t.Run("push valid facts", func(t *testing.T) {
		ctx := context.Background()
		args := map[string]interface{}{
			"facts": []interface{}{
				map[string]interface{}{
					"predicate": "test_event",
					"args":      []interface{}{"arg1", "arg2"},
				},
				map[string]interface{}{
					"predicate": "another_event",
					"args":      []interface{}{"value"},
				},
			},
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["accepted"].(int) != 2 {
			t.Errorf("expected 2 accepted facts, got %v", resultMap["accepted"])
		}
	})

	t.Run("push facts without args", func(t *testing.T) {
		ctx := context.Background()
		args := map[string]interface{}{
			"facts": []interface{}{
				map[string]interface{}{
					"predicate": "simple_fact",
				},
			},
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["accepted"].(int) != 1 {
			t.Errorf("expected 1 accepted fact, got %v", resultMap["accepted"])
		}
	})

	t.Run("error on invalid facts format", func(t *testing.T) {
		ctx := context.Background()
		args := map[string]interface{}{
			"facts": "not an array",
		}

		_, err := tool.Execute(ctx, args)
		if err == nil {
			t.Error("expected error for invalid facts format")
		}
	})

	t.Run("error on empty facts", func(t *testing.T) {
		ctx := context.Background()
		args := map[string]interface{}{
			"facts": []interface{}{},
		}

		_, err := tool.Execute(ctx, args)
		if err == nil {
			t.Error("expected error for empty facts")
		}
	})

	t.Run("skip facts without predicate", func(t *testing.T) {
		ctx := context.Background()
		args := map[string]interface{}{
			"facts": []interface{}{
				map[string]interface{}{
					"predicate": "valid",
				},
				map[string]interface{}{
					"args": []interface{}{"no predicate"},
				},
			},
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["accepted"].(int) != 1 {
			t.Errorf("expected 1 accepted fact, got %v", resultMap["accepted"])
		}
	})
}

func TestReadFactsTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &ReadFactsTool{engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "read-facts" {
			t.Errorf("expected name 'read-facts', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("read empty facts", func(t *testing.T) {
		ctx := context.Background()
		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["count"].(int) != 0 {
			t.Errorf("expected 0 facts, got %v", resultMap["count"])
		}
	})

	t.Run("read with limit", func(t *testing.T) {
		ctx := context.Background()

		// Add some facts
		for i := 0; i < 50; i++ {
			_ = engine.AddFacts(ctx, []mangle.Fact{
				{Predicate: "test", Args: []interface{}{i}, Timestamp: time.Now()},
			})
		}

		result, err := tool.Execute(ctx, map[string]interface{}{"limit": 10})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["count"].(int) > 10 {
			t.Errorf("expected at most 10 facts, got %v", resultMap["count"])
		}
	})

	t.Run("default limit applied", func(t *testing.T) {
		ctx := context.Background()
		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["count"].(int) > 25 {
			t.Errorf("expected at most 25 facts (default limit), got %v", resultMap["count"])
		}
	})

	t.Run("zero limit uses default", func(t *testing.T) {
		ctx := context.Background()
		result, err := tool.Execute(ctx, map[string]interface{}{"limit": 0})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["count"].(int) > 25 {
			t.Errorf("expected at most 25 facts (default limit), got %v", resultMap["count"])
		}
	})
}

func TestQueryFactsTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &QueryFactsTool{engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "query-facts" {
			t.Errorf("expected name 'query-facts', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("error on empty query", func(t *testing.T) {
		ctx := context.Background()
		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for empty query")
		}
	})

	t.Run("query existing facts", func(t *testing.T) {
		ctx := context.Background()

		// Add a fact
		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "console_event", Args: []interface{}{testSessionID, "error", "test message", int64(1000)}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{"query": "console_event(SessionId, Level, Msg, Ts)."})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["count"].(int) == 0 {
			t.Error("expected at least 1 result")
		}
	})

	t.Run("query tolerates missing trailing period", func(t *testing.T) {
		ctx := context.Background()

		result, err := tool.Execute(ctx, map[string]interface{}{"query": `console_event(SessionId, Level, Msg, Ts)`})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["count"].(int) == 0 {
			t.Error("expected at least 1 result without trailing period")
		}
	})

	t.Run("anonymous wildcard bindings are normalized", func(t *testing.T) {
		ctx := context.Background()
		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "net_request", Args: []interface{}{testSessionID, "req-anon", "GET", "/health", "fetch", int64(1010)}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{"query": `net_request(_, _, _, _, _, _).`})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		rows, ok := resultMap["results"].([]map[string]interface{})
		if !ok {
			t.Fatalf("expected []map[string]interface{} results, got %T", resultMap["results"])
		}
		if len(rows) == 0 {
			t.Fatalf("expected at least one wildcard result")
		}
		if _, exists := rows[0]["_0"]; !exists {
			t.Fatalf("expected normalized anonymous binding key _0 in first row, got keys=%v", rows[0])
		}
	})
}

func TestSubmitRuleTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &SubmitRuleTool{engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "submit-rule" {
			t.Errorf("expected name 'submit-rule', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("error on empty rule", func(t *testing.T) {
		ctx := context.Background()
		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for empty rule")
		}
	})

	t.Run("submit valid rule", func(t *testing.T) {
		ctx := context.Background()
		rule := `
Decl my_test_rule().
my_test_rule() :- console_event(_, "error", _, _).
`
		result, err := tool.Execute(ctx, map[string]interface{}{"rule": rule})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "ok" {
			t.Errorf("expected status 'ok', got %v", resultMap["status"])
		}
	})
}

func TestEvaluateRuleTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &EvaluateRuleTool{engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "evaluate-rule" {
			t.Errorf("expected name 'evaluate-rule', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("error on empty predicate", func(t *testing.T) {
		ctx := context.Background()
		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for empty predicate")
		}
	})

	t.Run("evaluate existing predicate", func(t *testing.T) {
		ctx := context.Background()

		// Add facts that will trigger the failed_request derived predicate
		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "net_request", Args: []interface{}{testSessionID, "req1", "GET", "/api/test", "fetch", int64(1000)}, Timestamp: time.Now()},
			{Predicate: "net_response", Args: []interface{}{testSessionID, "req1", int64(500), int64(50), int64(100)}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{"predicate": "failed_request"})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["predicate"] != "failed_request" {
			t.Errorf("expected predicate 'failed_request', got %v", resultMap["predicate"])
		}
	})
}

func TestQueryTemporalTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &QueryTemporalTool{engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "query-temporal" {
			t.Errorf("expected name 'query-temporal', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("error on empty predicate", func(t *testing.T) {
		ctx := context.Background()
		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for empty predicate")
		}
	})

	t.Run("query with time window", func(t *testing.T) {
		ctx := context.Background()
		now := time.Now()

		// Add facts at different times
		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "temporal_test", Args: []interface{}{"old"}, Timestamp: now.Add(-10 * time.Second)},
			{Predicate: "temporal_test", Args: []interface{}{"new"}, Timestamp: now},
		})

		// Query recent facts
		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate": "temporal_test",
			"after_ms":  int(now.Add(-5 * time.Second).UnixMilli()),
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["predicate"] != "temporal_test" {
			t.Errorf("expected predicate 'temporal_test', got %v", resultMap["predicate"])
		}
	})
}

func TestAwaitFactTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &AwaitFactTool{engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "await-fact" {
			t.Errorf("expected name 'await-fact', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("error on empty predicate", func(t *testing.T) {
		ctx := context.Background()
		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for empty predicate")
		}
	})

	t.Run("immediate match", func(t *testing.T) {
		ctx := context.Background()

		// Add fact first
		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "await_test", Args: []interface{}{"value"}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "await_test",
			"timeout_ms": 100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "passed" {
			t.Errorf("expected status 'passed', got %v", resultMap["status"])
		}
	})

	t.Run("timeout when no match", func(t *testing.T) {
		ctx := context.Background()

		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "nonexistent_predicate",
			"timeout_ms": 100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "timeout" {
			t.Errorf("expected status 'timeout', got %v", resultMap["status"])
		}
	})

	t.Run("match with args", func(t *testing.T) {
		ctx := context.Background()

		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "await_args_test", Args: []interface{}{"expected", "value"}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "await_args_test",
			"args":       []interface{}{"expected", "value"},
			"timeout_ms": 100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "passed" {
			t.Errorf("expected status 'passed', got %v", resultMap["status"])
		}
	})
}

func TestAwaitConditionsTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &AwaitConditionsTool{engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "await-conditions" {
			t.Errorf("expected name 'await-conditions', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("error on missing conditions", func(t *testing.T) {
		ctx := context.Background()
		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for missing conditions")
		}
	})

	t.Run("error on empty conditions", func(t *testing.T) {
		ctx := context.Background()
		_, err := tool.Execute(ctx, map[string]interface{}{
			"conditions": []interface{}{},
		})
		if err == nil {
			t.Error("expected error for empty conditions")
		}
	})

	t.Run("all conditions match", func(t *testing.T) {
		ctx := context.Background()

		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "cond1", Args: []interface{}{"a"}, Timestamp: time.Now()},
			{Predicate: "cond2", Args: []interface{}{"b"}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{"predicate": "cond1"},
				map[string]interface{}{"predicate": "cond2"},
			},
			"timeout_ms": 100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "passed" {
			t.Errorf("expected status 'passed', got %v", resultMap["status"])
		}
	})

	t.Run("timeout when not all match", func(t *testing.T) {
		ctx := context.Background()

		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "partial_cond", Args: []interface{}{"a"}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{"predicate": "partial_cond"},
				map[string]interface{}{"predicate": "missing_cond"},
			},
			"timeout_ms": 100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "timeout" {
			t.Errorf("expected status 'timeout', got %v", resultMap["status"])
		}
	})
}

func TestSubscribeRuleTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &SubscribeRuleTool{engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "subscribe-rule" {
			t.Errorf("expected name 'subscribe-rule', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("error on empty predicate", func(t *testing.T) {
		ctx := context.Background()
		_, err := tool.Execute(ctx, map[string]interface{}{})
		if err == nil {
			t.Error("expected error for empty predicate")
		}
	})

	t.Run("timeout when no events", func(t *testing.T) {
		ctx := context.Background()

		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "no_events_predicate",
			"timeout_ms": 100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "timeout" {
			t.Errorf("expected status 'timeout', got %v", resultMap["status"])
		}
	})
}

func TestGetToastNotificationsTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &GetToastNotificationsTool{engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "get-toast-notifications" {
			t.Errorf("expected name 'get-toast-notifications', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
	})

	t.Run("empty results", func(t *testing.T) {
		ctx := context.Background()
		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "no_toasts" {
			t.Errorf("expected status 'no_toasts', got %v", resultMap["status"])
		}
	})

	t.Run("with toast notifications", func(t *testing.T) {
		ctx := context.Background()

		// Add toast notification facts
		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "toast_notification", Args: []interface{}{testSessionID, "Error message", "error", "shadcn", int64(1000)}, Timestamp: time.Now()},
			{Predicate: "toast_notification", Args: []interface{}{testSessionID, "Success!", "success", "material-ui", int64(2000)}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["toast_count"].(int) != 2 {
			t.Errorf("expected 2 toasts, got %v", resultMap["toast_count"])
		}
	})

	t.Run("filter by level", func(t *testing.T) {
		ctx := context.Background()

		// Use a new engine for this test to avoid pollution
		freshEngine := setupTestEngine(t)
		freshTool := &GetToastNotificationsTool{engine: freshEngine}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "toast_notification", Args: []interface{}{testSessionID, "Error 1", "error", "native", int64(1000)}, Timestamp: time.Now()},
			{Predicate: "toast_notification", Args: []interface{}{testSessionID, "Warning 1", "warning", "native", int64(2000)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{"level": "error"})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["error_count"].(int) != 1 {
			t.Errorf("expected 1 error toast, got %v", resultMap["error_count"])
		}
	})
}

func TestGetConsoleErrorsTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &GetConsoleErrorsTool{engine: engine, dockerClient: nil}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "get-console-errors" {
			t.Errorf("expected name 'get-console-errors', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("empty results", func(t *testing.T) {
		ctx := context.Background()
		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Error("expected success to be true")
		}
	})

	t.Run("with console errors", func(t *testing.T) {
		ctx := context.Background()

		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "console_event", Args: []interface{}{testSessionID, "error", "TypeError: undefined", int64(1000)}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["error_count"].(int) == 0 {
			t.Error("expected at least 1 error")
		}
	})

	t.Run("include warnings", func(t *testing.T) {
		ctx := context.Background()

		freshEngine := setupTestEngine(t)
		freshTool := &GetConsoleErrorsTool{engine: freshEngine, dockerClient: nil}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "console_event", Args: []interface{}{testSessionID, "warning", "Deprecation warning", int64(1000)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{"include_warnings": true})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		errors := resultMap["errors"].([]map[string]interface{})
		if len(errors) == 0 {
			t.Error("expected warning to be included")
		}
	})

	t.Run("debug mode", func(t *testing.T) {
		ctx := context.Background()
		result, err := tool.Execute(ctx, map[string]interface{}{"debug": true})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["debug"] == nil {
			t.Error("expected debug info in result")
		}
	})
}

func TestDiagnosePageTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &DiagnosePageTool{engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "diagnose-page" {
			t.Errorf("expected name 'diagnose-page', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("ok status when no issues", func(t *testing.T) {
		ctx := context.Background()
		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		status := resultMap["status"].(string)
		if status != "ok" && status != "error" && status != "warning" {
			t.Errorf("unexpected status: %v", status)
		}
	})
}

func TestAwaitStableStateTool(t *testing.T) {
	engine := setupTestEngine(t)
	tool := &AwaitStableStateTool{engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "await-stable-state" {
			t.Errorf("expected name 'await-stable-state', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
	})

	t.Run("stable when no activity", func(t *testing.T) {
		ctx := context.Background()
		result, err := tool.Execute(ctx, map[string]interface{}{
			"timeout_ms":      1000,
			"network_idle_ms": 100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "stable" {
			t.Errorf("expected status 'stable', got %v", resultMap["status"])
		}
	})
}

// InputSchema tests - validate schema structure for all fact tools
func TestFactToolsInputSchema(t *testing.T) {
	engine := setupTestEngine(t)

	t.Run("ReadFactsTool schema", func(t *testing.T) {
		tool := &ReadFactsTool{engine: engine}
		schema := tool.InputSchema()
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		if schema["type"] != "object" {
			t.Errorf("expected type 'object', got %v", schema["type"])
		}
		props, ok := schema["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties map")
		}
		if props["limit"] == nil {
			t.Error("expected limit property in schema")
		}
	})

	t.Run("QueryFactsTool schema", func(t *testing.T) {
		tool := &QueryFactsTool{engine: engine}
		schema := tool.InputSchema()
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		props, ok := schema["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties map")
		}
		if props["query"] == nil {
			t.Error("expected query property in schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields")
		}
	})

	t.Run("SubmitRuleTool schema", func(t *testing.T) {
		tool := &SubmitRuleTool{engine: engine}
		schema := tool.InputSchema()
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		props, ok := schema["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties map")
		}
		if props["rule"] == nil {
			t.Error("expected rule property in schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields")
		}
	})

	t.Run("EvaluateRuleTool schema", func(t *testing.T) {
		tool := &EvaluateRuleTool{engine: engine}
		schema := tool.InputSchema()
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		props, ok := schema["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties map")
		}
		if props["predicate"] == nil {
			t.Error("expected predicate property in schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields")
		}
	})

	t.Run("SubscribeRuleTool schema", func(t *testing.T) {
		tool := &SubscribeRuleTool{engine: engine}
		schema := tool.InputSchema()
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		props, ok := schema["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties map")
		}
		if props["predicate"] == nil {
			t.Error("expected predicate property in schema")
		}
		if props["timeout_ms"] == nil {
			t.Error("expected timeout_ms property in schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields")
		}
	})

	t.Run("QueryTemporalTool schema", func(t *testing.T) {
		tool := &QueryTemporalTool{engine: engine}
		schema := tool.InputSchema()
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		props, ok := schema["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties map")
		}
		if props["predicate"] == nil {
			t.Error("expected predicate property in schema")
		}
		if props["after_ms"] == nil {
			t.Error("expected after_ms property in schema")
		}
		if props["before_ms"] == nil {
			t.Error("expected before_ms property in schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields")
		}
	})

	t.Run("AwaitFactTool schema", func(t *testing.T) {
		tool := &AwaitFactTool{engine: engine}
		schema := tool.InputSchema()
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		props, ok := schema["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties map")
		}
		if props["predicate"] == nil {
			t.Error("expected predicate property in schema")
		}
		if props["args"] == nil {
			t.Error("expected args property in schema")
		}
		if props["timeout_ms"] == nil {
			t.Error("expected timeout_ms property in schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields")
		}
	})

	t.Run("AwaitConditionsTool schema", func(t *testing.T) {
		tool := &AwaitConditionsTool{engine: engine}
		schema := tool.InputSchema()
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		props, ok := schema["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties map")
		}
		if props["conditions"] == nil {
			t.Error("expected conditions property in schema")
		}
		if props["timeout_ms"] == nil {
			t.Error("expected timeout_ms property in schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields")
		}
	})
}

// Edge case tests for tool execution
func TestFactToolsEdgeCases(t *testing.T) {
	engine := setupTestEngine(t)

	t.Run("PushFacts with non-map items", func(t *testing.T) {
		tool := &PushFactsTool{engine: engine}
		ctx := context.Background()
		args := map[string]interface{}{
			"facts": []interface{}{
				"not a map",
				123,
				map[string]interface{}{"predicate": "valid_fact"},
			},
		}

		result, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		// Only the valid fact should be accepted
		if resultMap["accepted"].(int) != 1 {
			t.Errorf("expected 1 accepted fact, got %v", resultMap["accepted"])
		}
	})

	t.Run("ReadFacts with negative limit", func(t *testing.T) {
		tool := &ReadFactsTool{engine: engine}
		ctx := context.Background()
		result, err := tool.Execute(ctx, map[string]interface{}{"limit": -5})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		// Should use default limit
		if resultMap["count"].(int) > 25 {
			t.Errorf("expected at most 25 facts (default limit), got %v", resultMap["count"])
		}
	})

	t.Run("AwaitFact with negative timeout", func(t *testing.T) {
		tool := &AwaitFactTool{engine: engine}
		ctx := context.Background()

		// Add fact first so it matches immediately
		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "immediate_fact", Args: []interface{}{}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "immediate_fact",
			"timeout_ms": -100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "passed" {
			t.Errorf("expected status 'passed', got %v", resultMap["status"])
		}
	})

	t.Run("AwaitConditions with invalid condition", func(t *testing.T) {
		tool := &AwaitConditionsTool{engine: engine}
		ctx := context.Background()
		args := map[string]interface{}{
			"conditions": []interface{}{
				"not a map",
				map[string]interface{}{"no_predicate": "value"},
			},
			"timeout_ms": 100,
		}

		_, err := tool.Execute(ctx, args)
		if err == nil {
			t.Error("expected error for all invalid conditions")
		}
	})

	t.Run("SubscribeRule with negative timeout", func(t *testing.T) {
		tool := &SubscribeRuleTool{engine: engine}
		ctx := context.Background()

		// Should use default timeout and eventually timeout
		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "nonexistent_sub",
			"timeout_ms": -100,
		})
		// Since we're using a valid positive timeout due to the fix, it should timeout
		// but not immediately
		if err != nil && err.Error() != "context canceled" {
			t.Fatalf("Execute failed unexpectedly: %v", err)
		}
		if err == nil {
			resultMap := result.(map[string]interface{})
			// Due to default timeout being applied, we expect timeout
			if resultMap["status"] != "timeout" {
				t.Errorf("expected status 'timeout', got %v", resultMap["status"])
			}
		}
	})

	t.Run("QueryTemporal with both time bounds", func(t *testing.T) {
		freshEngine := setupTestEngine(t)
		tool := &QueryTemporalTool{engine: freshEngine}
		ctx := context.Background()
		now := time.Now()

		// Add facts at different times
		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "bounded_test", Args: []interface{}{"in_window"}, Timestamp: now.Add(-2 * time.Second)},
			{Predicate: "bounded_test", Args: []interface{}{"before_window"}, Timestamp: now.Add(-10 * time.Second)},
			{Predicate: "bounded_test", Args: []interface{}{"after_window"}, Timestamp: now.Add(1 * time.Second)},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate": "bounded_test",
			"after_ms":  int(now.Add(-5 * time.Second).UnixMilli()),
			"before_ms": int(now.UnixMilli()),
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["count"].(int) != 1 {
			t.Errorf("expected 1 fact in window, got %v", resultMap["count"])
		}
	})

	t.Run("GetToastNotifications with limit", func(t *testing.T) {
		freshEngine := setupTestEngine(t)
		tool := &GetToastNotificationsTool{engine: freshEngine}
		ctx := context.Background()

		// Add multiple toasts
		for i := 0; i < 10; i++ {
			_ = freshEngine.AddFacts(ctx, []mangle.Fact{
				{Predicate: "toast_notification", Args: []interface{}{testSessionID, "Message " + string(rune('A'+i)), "info", "native", int64(1000 + i)}, Timestamp: time.Now()},
			})
		}

		result, err := tool.Execute(ctx, map[string]interface{}{"limit": 5})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["toast_count"].(int) != 5 {
			t.Errorf("expected 5 toasts (limited), got %v", resultMap["toast_count"])
		}
	})
}

// Context cancellation tests
func TestFactToolsContextCancellation(t *testing.T) {
	engine := setupTestEngine(t)

	t.Run("AwaitFact cancelled", func(t *testing.T) {
		tool := &AwaitFactTool{engine: engine}
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		_, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "never_appears",
			"timeout_ms": 5000,
		})
		if err == nil {
			t.Error("expected context cancellation error")
		}
	})

	t.Run("AwaitConditions cancelled", func(t *testing.T) {
		tool := &AwaitConditionsTool{engine: engine}
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		_, err := tool.Execute(ctx, map[string]interface{}{
			"conditions": []interface{}{
				map[string]interface{}{"predicate": "never_appears"},
			},
			"timeout_ms": 5000,
		})
		if err == nil {
			t.Error("expected context cancellation error")
		}
	})

	t.Run("SubscribeRule cancelled", func(t *testing.T) {
		tool := &SubscribeRuleTool{engine: engine}
		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(50 * time.Millisecond)
			cancel()
		}()

		_, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "never_triggers",
			"timeout_ms": 5000,
		})
		if err == nil {
			t.Error("expected context cancellation error")
		}
	})
}
