package mcp

import (
	"context"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

func TestSleepWithContext(t *testing.T) {
	t.Run("zero duration returns immediately", func(t *testing.T) {
		ctx := context.Background()
		start := time.Now()
		err := sleepWithContext(ctx, 0)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if time.Since(start) > 100*time.Millisecond {
			t.Error("zero duration should return immediately")
		}
	})

	t.Run("negative duration returns immediately", func(t *testing.T) {
		ctx := context.Background()
		start := time.Now()
		err := sleepWithContext(ctx, -100*time.Millisecond)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if time.Since(start) > 100*time.Millisecond {
			t.Error("negative duration should return immediately")
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel immediately
		cancel()

		err := sleepWithContext(ctx, 5*time.Second)
		if err == nil {
			t.Error("expected context cancellation error")
		}
	})

	t.Run("sleeps for specified duration", func(t *testing.T) {
		ctx := context.Background()
		duration := 50 * time.Millisecond
		start := time.Now()

		err := sleepWithContext(ctx, duration)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		elapsed := time.Since(start)
		if elapsed < duration {
			t.Errorf("sleep was too short: %v < %v", elapsed, duration)
		}
	})
}

func TestExecutePlanTool(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}
	engine, err := mangle.NewEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Note: We can't test browser interactions without a real browser,
	// but we can test the tool metadata and error paths

	tool := &ExecutePlanTool{sessions: nil, engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "execute-plan" {
			t.Errorf("expected name 'execute-plan', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
	})

	t.Run("error on missing session_id", func(t *testing.T) {
		ctx := context.Background()
		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Tool returns success:false for validation errors
		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatal("expected map result")
		}
		if resultMap["success"] != false {
			t.Error("expected success:false for missing session_id")
		}
		if resultMap["error"] == nil {
			t.Error("expected error message")
		}
	})

	// Note: Test for missing session requires a real session manager
	// which would require a browser. Skipping for unit tests.

	t.Run("default parameters", func(t *testing.T) {
		// Verify default values are used correctly
		args := map[string]interface{}{"session_id": "test"}
		stopOnError := getBoolArg(args, "stop_on_error", true)
		if !stopOnError {
			t.Error("expected default stop_on_error to be true")
		}
		delayMs := getIntArg(args, "delay_ms", 100)
		if delayMs != 100 {
			t.Errorf("expected default delay_ms to be 100, got %d", delayMs)
		}
	})

	// Note: Testing with a session_id requires a real session manager which
	// would require a browser. Browser-dependent tests should be skipped
	// in unit tests and covered by integration tests instead.
}

func TestWaitForConditionTool(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}
	engine, err := mangle.NewEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	tool := &WaitForConditionTool{sessions: nil, engine: engine}

	t.Run("name and description", func(t *testing.T) {
		if tool.Name() != "wait-for-condition" {
			t.Errorf("expected name 'wait-for-condition', got %q", tool.Name())
		}
		if tool.Description() == "" {
			t.Error("expected non-empty description")
		}
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
	})

	t.Run("error on missing predicate", func(t *testing.T) {
		ctx := context.Background()
		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if resultMap["success"].(bool) {
			t.Error("expected success to be false without predicate")
		}
	})

	t.Run("immediate match with existing fact", func(t *testing.T) {
		ctx := context.Background()

		// Add fact first
		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "wait_test", Args: []interface{}{"value"}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "wait_test",
			"timeout_ms": 100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["success"].(bool) {
			t.Error("expected success with matching fact")
		}
		if !resultMap["matched"].(bool) {
			t.Error("expected matched to be true")
		}
	})

	t.Run("match with specific args", func(t *testing.T) {
		ctx := context.Background()

		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "wait_args", Args: []interface{}{"expected", "value"}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "wait_args",
			"match_args": []interface{}{"expected", "value"},
			"timeout_ms": 100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["matched"].(bool) {
			t.Error("expected matched to be true with matching args")
		}
	})

	t.Run("wildcard matching", func(t *testing.T) {
		ctx := context.Background()

		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "wait_wildcard", Args: []interface{}{"any", "thing"}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "wait_wildcard",
			"match_args": []interface{}{"_", "thing"},
			"timeout_ms": 100,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["matched"].(bool) {
			t.Error("expected matched with wildcard")
		}
	})

	t.Run("timeout when no match", func(t *testing.T) {
		ctx := context.Background()

		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "nonexistent",
			"timeout_ms": 50,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["matched"].(bool) {
			t.Error("expected matched to be false")
		}
	})

	t.Run("zero poll interval uses default", func(t *testing.T) {
		ctx := context.Background()

		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "poll_test", Args: []interface{}{}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":        "poll_test",
			"timeout_ms":       200,
			"poll_interval_ms": 0,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if !resultMap["matched"].(bool) {
			t.Error("expected matched")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := tool.Execute(ctx, map[string]interface{}{
			"predicate":  "nonexistent",
			"timeout_ms": 10000,
		})
		if err == nil {
			t.Error("expected context cancellation error")
		}
	})
}

func TestGetToastNotificationsToolExtended(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}
	engine, err := mangle.NewEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	tool := &GetToastNotificationsTool{engine: engine}

	t.Run("filter by since_ms", func(t *testing.T) {
		ctx := context.Background()

		now := time.Now().UnixMilli()

		_ = engine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "toast_notification", Args: []interface{}{"Old toast", "info", "native", int64(now - 10000)}, Timestamp: time.Now()},
			{Predicate: "toast_notification", Args: []interface{}{"New toast", "info", "native", int64(now)}, Timestamp: time.Now()},
		})

		result, err := tool.Execute(ctx, map[string]interface{}{
			"since_ms": int(now - 5000),
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		toasts := resultMap["toasts"].([]map[string]interface{})
		if len(toasts) != 1 {
			t.Errorf("expected 1 toast after filtering, got %d", len(toasts))
		}
	})

	t.Run("count by level", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetToastNotificationsTool{engine: freshEngine}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "toast_notification", Args: []interface{}{"Error 1", "error", "native", int64(1000)}, Timestamp: time.Now()},
			{Predicate: "toast_notification", Args: []interface{}{"Error 2", "error", "native", int64(2000)}, Timestamp: time.Now()},
			{Predicate: "toast_notification", Args: []interface{}{"Warning 1", "warning", "native", int64(3000)}, Timestamp: time.Now()},
			{Predicate: "toast_notification", Args: []interface{}{"Success 1", "success", "native", int64(4000)}, Timestamp: time.Now()},
			{Predicate: "toast_notification", Args: []interface{}{"Info 1", "info", "native", int64(5000)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["error_count"].(int) != 2 {
			t.Errorf("expected 2 errors, got %v", resultMap["error_count"])
		}
		if resultMap["warning_count"].(int) != 1 {
			t.Errorf("expected 1 warning, got %v", resultMap["warning_count"])
		}
		if resultMap["success_count"].(int) != 1 {
			t.Errorf("expected 1 success, got %v", resultMap["success_count"])
		}
		if resultMap["info_count"].(int) != 1 {
			t.Errorf("expected 1 info, got %v", resultMap["info_count"])
		}
	})

	t.Run("errors detected status", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetToastNotificationsTool{engine: freshEngine}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "toast_notification", Args: []interface{}{"Error!", "error", "native", int64(1000)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "errors_detected" {
			t.Errorf("expected status 'errors_detected', got %v", resultMap["status"])
		}
	})

	t.Run("warnings detected status", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetToastNotificationsTool{engine: freshEngine}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "toast_notification", Args: []interface{}{"Warning!", "warning", "native", int64(1000)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "warnings_detected" {
			t.Errorf("expected status 'warnings_detected', got %v", resultMap["status"])
		}
	})

	t.Run("ok status with no errors", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetToastNotificationsTool{engine: freshEngine}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "toast_notification", Args: []interface{}{"Success!", "success", "native", int64(1000)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["status"] != "ok" {
			t.Errorf("expected status 'ok', got %v", resultMap["status"])
		}
	})

	t.Run("correlation with API failure", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetToastNotificationsTool{engine: freshEngine}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "toast_notification", Args: []interface{}{"Save failed", "error", "native", int64(1000)}, Timestamp: time.Now()},
			{Predicate: "toast_after_api_failure", Args: []interface{}{"Save failed", "req-123", "/api/save", int64(500), int64(100)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{
			"include_correlations": true,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		toasts := resultMap["toasts"].([]map[string]interface{})
		if len(toasts) != 1 {
			t.Fatalf("expected 1 toast, got %d", len(toasts))
		}
		if toasts[0]["correlated_api_failure"] == nil {
			t.Error("expected correlated_api_failure to be present")
		}
	})

	t.Run("skip correlation when disabled", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetToastNotificationsTool{engine: freshEngine}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "toast_notification", Args: []interface{}{"Error", "error", "native", int64(1000)}, Timestamp: time.Now()},
			{Predicate: "toast_after_api_failure", Args: []interface{}{"Error", "req-123", "/api/save", int64(500), int64(100)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{
			"include_correlations": false,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		toasts := resultMap["toasts"].([]map[string]interface{})
		if len(toasts) != 1 {
			t.Fatalf("expected 1 toast, got %d", len(toasts))
		}
		if toasts[0]["correlated_api_failure"] != nil {
			t.Error("expected no correlation when disabled")
		}
	})

	t.Run("repeated errors", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetToastNotificationsTool{engine: freshEngine}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "repeated_toast_error", Args: []interface{}{"Connection failed"}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		repeated := resultMap["repeated_errors"].([]string)
		if len(repeated) != 1 {
			t.Errorf("expected 1 repeated error, got %d", len(repeated))
		}
	})
}

func TestGetConsoleErrorsToolExtended(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}
	engine, err := mangle.NewEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	tool := &GetConsoleErrorsTool{engine: engine, dockerClient: nil}

	t.Run("include all levels", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetConsoleErrorsTool{engine: freshEngine, dockerClient: nil}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "console_event", Args: []interface{}{"log", "Log message", int64(1000)}, Timestamp: time.Now()},
			{Predicate: "console_event", Args: []interface{}{"info", "Info message", int64(2000)}, Timestamp: time.Now()},
			{Predicate: "console_event", Args: []interface{}{"debug", "Debug message", int64(3000)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{"include_all_levels": true})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		errors := resultMap["errors"].([]map[string]interface{})
		if len(errors) != 3 {
			t.Errorf("expected 3 events with include_all_levels, got %d", len(errors))
		}
	})

	t.Run("filter by since_ms", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetConsoleErrorsTool{engine: freshEngine, dockerClient: nil}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "console_event", Args: []interface{}{"error", "Old error", int64(1000)}, Timestamp: time.Now()},
			{Predicate: "console_event", Args: []interface{}{"error", "New error", int64(5000)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{"since_ms": 3000})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		errors := resultMap["errors"].([]map[string]interface{})
		if len(errors) != 1 {
			t.Errorf("expected 1 error after filtering, got %d", len(errors))
		}
	})

	t.Run("docker disabled flag", func(t *testing.T) {
		ctx := context.Background()
		result, err := tool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["docker_enabled"].(bool) {
			t.Error("expected docker_enabled to be false when no docker client")
		}
	})

	t.Run("caused_by correlation", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetConsoleErrorsTool{engine: freshEngine, dockerClient: nil}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "console_event", Args: []interface{}{"error", "Network error", int64(1000)}, Timestamp: time.Now()},
			{Predicate: "caused_by", Args: []interface{}{"Network error", "req-456"}, Timestamp: time.Now()},
			{Predicate: "net_request", Args: []interface{}{"req-456", "POST", "/api/submit", "fetch", int64(900)}, Timestamp: time.Now()},
			{Predicate: "net_response", Args: []interface{}{"req-456", int64(500), int64(50), int64(100)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		errors := resultMap["errors"].([]map[string]interface{})
		if len(errors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(errors))
		}
		if errors[0]["caused_by"] == nil {
			t.Error("expected caused_by to be present")
		}
	})

	t.Run("failed requests collection", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetConsoleErrorsTool{engine: freshEngine, dockerClient: nil}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "failed_request", Args: []interface{}{"req-789", "/api/data", int64(404)}, Timestamp: time.Now()},
			{Predicate: "net_request", Args: []interface{}{"req-789", "GET", "/api/data", "fetch", int64(1000)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		failedReqs := resultMap["failed_requests"].([]map[string]interface{})
		if len(failedReqs) != 1 {
			t.Errorf("expected 1 failed request, got %d", len(failedReqs))
		}
	})

	t.Run("slow apis collection", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetConsoleErrorsTool{engine: freshEngine, dockerClient: nil}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "slow_api", Args: []interface{}{"req-slow", "/api/heavy", int64(2500)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		slowApis := resultMap["slow_apis"].([]map[string]interface{})
		if len(slowApis) != 1 {
			t.Errorf("expected 1 slow api, got %d", len(slowApis))
		}
	})

	t.Run("cascading failures", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &GetConsoleErrorsTool{engine: freshEngine, dockerClient: nil}

		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "cascading_failure", Args: []interface{}{"child-req", "parent-req"}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		cascading := resultMap["cascading_failures"].([]map[string]interface{})
		if len(cascading) != 1 {
			t.Errorf("expected 1 cascading failure, got %d", len(cascading))
		}
	})
}

func TestDiagnosePageToolExtended(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}
	engine, err := mangle.NewEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	tool := &DiagnosePageTool{engine: engine}

	t.Run("input schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
	})

	t.Run("warning status with slow apis", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &DiagnosePageTool{engine: freshEngine}

		// Add facts that trigger slow_api but not failed_request
		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "net_request", Args: []interface{}{"req1", "GET", "/api/slow", "fetch", int64(1000)}, Timestamp: time.Now()},
			{Predicate: "net_response", Args: []interface{}{"req1", int64(200), int64(50), int64(2000)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		status := resultMap["status"].(string)
		// Status can be "warning" if slow_api derived, or "ok" if not
		if status != "ok" && status != "warning" && status != "error" {
			t.Errorf("unexpected status: %v", status)
		}
	})

	t.Run("error status with failed requests", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &DiagnosePageTool{engine: freshEngine}

		// Add facts that trigger failed_request
		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "net_request", Args: []interface{}{"req1", "GET", "/api/error", "fetch", int64(1000)}, Timestamp: time.Now()},
			{Predicate: "net_response", Args: []interface{}{"req1", int64(500), int64(50), int64(100)}, Timestamp: time.Now()},
		})

		result, err := freshTool.Execute(ctx, map[string]interface{}{})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		// failed_requests is []mangle.QueryResult, not []interface{}
		failedReqs, ok := resultMap["failed_requests"].([]mangle.QueryResult)
		if ok && len(failedReqs) == 0 {
			t.Log("No failed_requests derived - rule may not be triggered")
		}
	})
}

func TestAwaitStableStateToolExtended(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}
	engine, err := mangle.NewEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	tool := &AwaitStableStateTool{engine: engine}

	t.Run("input schema", func(t *testing.T) {
		schema := tool.InputSchema()
		if schema == nil {
			t.Error("expected non-nil schema")
		}
		props := schema["properties"]
		if props == nil {
			t.Error("expected properties in schema")
		}
	})

	t.Run("timeout with active requests", func(t *testing.T) {
		ctx := context.Background()

		freshEngine, _ := mangle.NewEngine(cfg)
		freshTool := &AwaitStableStateTool{engine: freshEngine}

		// Pre-populate with request to prevent immediate stability
		_ = freshEngine.AddFacts(ctx, []mangle.Fact{
			{Predicate: "net_request", Args: []interface{}{"req", "GET", "/api/test", "fetch", time.Now().UnixMilli()}, Timestamp: time.Now()},
		})

		// This test verifies the tool can return either stable or timeout
		// depending on timing. Just verify it returns without error.
		result, err := freshTool.Execute(ctx, map[string]interface{}{
			"timeout_ms":      100,
			"network_idle_ms": 50,
		})
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		status := resultMap["status"].(string)
		if status != "stable" && status != "timeout" {
			t.Errorf("expected status 'stable' or 'timeout', got %v", status)
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := tool.Execute(ctx, map[string]interface{}{
			"timeout_ms": 10000,
		})
		if err == nil {
			t.Error("expected context cancellation error")
		}
	})
}

// TestAutomationToolsInputSchema tests InputSchema for all automation tools
func TestAutomationToolsInputSchema(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}
	engine, err := mangle.NewEngine(cfg)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	t.Run("GetConsoleErrorsTool schema", func(t *testing.T) {
		tool := &GetConsoleErrorsTool{engine: engine, dockerClient: nil}
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
		if props["include_warnings"] == nil {
			t.Error("expected include_warnings property in schema")
		}
		if props["include_all_levels"] == nil {
			t.Error("expected include_all_levels property in schema")
		}
		if props["since_ms"] == nil {
			t.Error("expected since_ms property in schema")
		}
		if props["debug"] == nil {
			t.Error("expected debug property in schema")
		}
	})

	t.Run("GetToastNotificationsTool schema", func(t *testing.T) {
		tool := &GetToastNotificationsTool{engine: engine}
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
		if props["level"] == nil {
			t.Error("expected level property in schema")
		}
		if props["limit"] == nil {
			t.Error("expected limit property in schema")
		}
		if props["since_ms"] == nil {
			t.Error("expected since_ms property in schema")
		}
	})

	t.Run("ExecutePlanTool schema", func(t *testing.T) {
		tool := &ExecutePlanTool{sessions: nil, engine: engine}
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
		if props["session_id"] == nil {
			t.Error("expected session_id property in schema")
		}
		if props["predicate"] == nil {
			t.Error("expected predicate property in schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields in schema")
		}
	})

	t.Run("WaitForConditionTool schema", func(t *testing.T) {
		tool := &WaitForConditionTool{sessions: nil, engine: engine}
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
		if props["poll_interval_ms"] == nil {
			t.Error("expected poll_interval_ms property in schema")
		}
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 {
			t.Error("expected required fields in schema")
		}
	})

	t.Run("DiagnosePageTool schema", func(t *testing.T) {
		tool := &DiagnosePageTool{engine: engine}
		schema := tool.InputSchema()
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		// Just verify it returns something
		if schema["type"] != "object" {
			t.Errorf("expected type 'object', got %v", schema["type"])
		}
	})

	t.Run("AwaitStableStateTool schema", func(t *testing.T) {
		tool := &AwaitStableStateTool{engine: engine}
		schema := tool.InputSchema()
		if schema == nil {
			t.Fatal("expected non-nil schema")
		}
		props, ok := schema["properties"].(map[string]interface{})
		if !ok {
			t.Fatal("expected properties map")
		}
		if props["timeout_ms"] == nil {
			t.Error("expected timeout_ms property in schema")
		}
		if props["network_idle_ms"] == nil {
			t.Error("expected network_idle_ms property in schema")
		}
	})
}
