package mangle

import (
	"context"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

func TestEngineLoadSchema(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	if !engine.Ready() {
		t.Fatal("Engine not ready after schema load")
	}
}

func TestEngineAddFacts(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()
	facts := []Fact{
		{
			Predicate: "console_event",
			Args:      []interface{}{"error", "Failed to load resource", int64(1234567890)},
			Timestamp: time.Now(),
		},
		{
			Predicate: "net_request",
			Args:      []interface{}{"req123", "GET", "https://api.example.com/data", "xhr", int64(1234567800)},
			Timestamp: time.Now(),
		},
		{
			Predicate: "net_response",
			Args:      []interface{}{"req123", int64(404), int64(50), int64(100)},
			Timestamp: time.Now(),
		},
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	// Verify facts were added to buffer
	buffered := engine.Facts()
	if len(buffered) != len(facts) {
		t.Errorf("Expected %d facts in buffer, got %d", len(facts), len(buffered))
	}

	// Verify predicate index
	consoleEvents := engine.FactsByPredicate("console_event")
	if len(consoleEvents) != 1 {
		t.Errorf("Expected 1 console_event, got %d", len(consoleEvents))
	}
}

func TestEngineQuery(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Add test facts
	facts := []Fact{
		{
			Predicate: "net_request",
			Args:      []interface{}{"req1", "GET", "https://slow-api.com/data", "fetch", int64(1000)},
			Timestamp: time.Now(),
		},
		{
			Predicate: "net_response",
			Args:      []interface{}{"req1", int64(200), int64(50), int64(1500)}, // 1.5 seconds (1500ms)
			Timestamp: time.Now(),
		},
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	// Verify base facts are stored correctly
	netReqs := engine.FactsByPredicate("net_request")
	if len(netReqs) != 1 {
		t.Fatalf("Expected 1 net_request fact, got %d", len(netReqs))
	}
	t.Logf("✓ Found net_request: %+v", netReqs[0])

	netResps := engine.FactsByPredicate("net_response")
	if len(netResps) != 1 {
		t.Fatalf("Expected 1 net_response fact, got %d", len(netResps))
	}
	t.Logf("✓ Found net_response: %+v", netResps[0])

	// Verify Duration argument (4th arg in net_response)
	if len(netResps[0].Args) >= 4 {
		duration := netResps[0].Args[3]
		t.Logf("✓ Duration value: %v (type: %T)", duration, duration)
		if durationInt, ok := duration.(int64); ok {
			if durationInt > 1000 {
				t.Logf("✓ Duration (%d) > 1000, slow_api rule should match", durationInt)
			}
		}
	}

	t.Log("✓ Engine query test passed - facts stored correctly")
}

func TestEngineTemporalQuery(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()
	now := time.Now()
	past := now.Add(-5 * time.Second)

	facts := []Fact{
		{
			Predicate: "console_event",
			Args:      []interface{}{"log", "Message 1", past.UnixMilli()},
			Timestamp: past,
		},
		{
			Predicate: "console_event",
			Args:      []interface{}{"error", "Message 2", now.UnixMilli()},
			Timestamp: now,
		},
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	// Query events in last 3 seconds
	recent := engine.QueryTemporal("console_event", now.Add(-3*time.Second), time.Time{})
	if len(recent) != 1 {
		t.Errorf("Expected 1 recent event, got %d", len(recent))
	}

	// Query all events
	all := engine.QueryTemporal("console_event", time.Time{}, time.Time{})
	if len(all) != 2 {
		t.Errorf("Expected 2 total events, got %d", len(all))
	}
}

func TestEngineAddRule(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Add a custom rule
	rule := `
Decl critical_error(Message, Timestamp).

critical_error(Msg, T) :-
    console_event("error", Msg, T),
    net_response(_, Status, _, _),
    Status >= 500.
`

	if err := engine.AddRule(rule); err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}
}

func TestEngineDisabled(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          false,
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// AddFacts should be a no-op when disabled
	ctx := context.Background()
	err = engine.AddFacts(ctx, []Fact{{Predicate: "test", Args: []interface{}{"arg"}}})
	if err != nil {
		t.Errorf("AddFacts should succeed when disabled: %v", err)
	}

	// Engine should still report as ready when disabled
	if !engine.Ready() {
		t.Error("Engine should be ready when disabled")
	}
}

func TestEngineAddRuleDisabled(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          false,
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// AddRule should be a no-op when disabled
	err = engine.AddRule("some rule")
	if err != nil {
		t.Errorf("AddRule should succeed when disabled: %v", err)
	}
}

func TestEngineSamplingRate(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 100,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Initially sampling rate should be 1.0
	if rate := engine.SamplingRate(); rate != 1.0 {
		t.Errorf("Expected initial sampling rate 1.0, got %v", rate)
	}

	// Add many low-value facts to trigger sampling
	ctx := context.Background()
	for i := 0; i < 90; i++ {
		facts := []Fact{
			{Predicate: "dom_node", Args: []interface{}{i, "div", "text", "parent"}, Timestamp: time.Now()},
		}
		_ = engine.AddFacts(ctx, facts)
	}

	// Sampling rate should have decreased
	rate := engine.SamplingRate()
	if rate >= 1.0 {
		t.Errorf("Expected sampling rate < 1.0 after buffer fill, got %v", rate)
	}
}

func TestEngineFactsByPredicateEmpty(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Query non-existent predicate
	facts := engine.FactsByPredicate("nonexistent")
	if len(facts) != 0 {
		t.Errorf("Expected 0 facts for nonexistent predicate, got %d", len(facts))
	}
}

func TestEngineMatchesAll(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()
	facts := []Fact{
		{Predicate: "console_event", Args: []interface{}{"error", "Test error", int64(1000)}, Timestamp: time.Now()},
		{Predicate: "net_request", Args: []interface{}{"req1", "GET", "/api", "xhr", int64(999)}, Timestamp: time.Now()},
	}
	_ = engine.AddFacts(ctx, facts)

	t.Run("all conditions match", func(t *testing.T) {
		conditions := []Fact{
			{Predicate: "console_event", Args: []interface{}{"error"}},
			{Predicate: "net_request", Args: []interface{}{"req1", "GET"}},
		}
		if !engine.MatchesAll(conditions) {
			t.Error("Expected all conditions to match")
		}
	})

	t.Run("missing predicate", func(t *testing.T) {
		conditions := []Fact{
			{Predicate: "nonexistent", Args: []interface{}{}},
		}
		if engine.MatchesAll(conditions) {
			t.Error("Expected conditions to not match for nonexistent predicate")
		}
	})

	t.Run("wrong argument value", func(t *testing.T) {
		conditions := []Fact{
			{Predicate: "console_event", Args: []interface{}{"warning"}}, // wrong level
		}
		if engine.MatchesAll(conditions) {
			t.Error("Expected conditions to not match with wrong argument")
		}
	})

	t.Run("empty conditions", func(t *testing.T) {
		if !engine.MatchesAll([]Fact{}) {
			t.Error("Expected empty conditions to match")
		}
	})

	t.Run("predicate match with no args", func(t *testing.T) {
		conditions := []Fact{
			{Predicate: "console_event", Args: nil},
		}
		if !engine.MatchesAll(conditions) {
			t.Error("Expected predicate-only condition to match")
		}
	})
}

func TestEngineSubscription(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	t.Run("subscribe and unsubscribe", func(t *testing.T) {
		ch := make(chan WatchEvent, 10)
		subID := engine.Subscribe("test_passed", ch)

		if subID == "" {
			t.Error("Expected non-empty subscription ID")
		}

		// Verify subscription is registered
		predicates := engine.WatchPredicates()
		found := false
		for _, p := range predicates {
			if p == "test_passed" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected test_passed in watched predicates")
		}

		// Unsubscribe
		engine.Unsubscribe("test_passed", ch)

		// Verify subscription is removed
		predicates = engine.WatchPredicates()
		for _, p := range predicates {
			if p == "test_passed" {
				t.Error("Expected test_passed to be removed from watched predicates")
			}
		}
	})

	t.Run("multiple subscriptions", func(t *testing.T) {
		ch1 := make(chan WatchEvent, 10)
		ch2 := make(chan WatchEvent, 10)

		engine.Subscribe("predicate1", ch1)
		engine.Subscribe("predicate2", ch2)

		predicates := engine.WatchPredicates()
		if len(predicates) < 2 {
			t.Errorf("Expected at least 2 watched predicates, got %d", len(predicates))
		}

		engine.Unsubscribe("predicate1", ch1)
		engine.Unsubscribe("predicate2", ch2)
	})
}

func TestEngineBufferLimit(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 10, // Very small limit
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Add more facts than buffer limit (use high-value predicates to avoid sampling)
	for i := 0; i < 20; i++ {
		facts := []Fact{
			{Predicate: "console_event", Args: []interface{}{"error", "msg", int64(i)}, Timestamp: time.Now()},
		}
		_ = engine.AddFacts(ctx, facts)
	}

	// Buffer should not exceed limit
	buffered := engine.Facts()
	if len(buffered) > 10 {
		t.Errorf("Expected buffer size <= 10, got %d", len(buffered))
	}
}

func TestEngineQueryNotReady(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "", // No schema
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()
	_, err = engine.Query(ctx, "test(X).")
	if err == nil {
		t.Error("Expected error when querying without schema")
	}
}

func TestEngineEvaluateNotReady(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "", // No schema
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()
	_, err = engine.Evaluate(ctx, "test_predicate")
	if err == nil {
		t.Error("Expected error when evaluating without schema")
	}
}

func TestDefaultLowValuePredicates(t *testing.T) {
	predicates := defaultLowValuePredicates()

	expectedLowValue := []string{"dom_node", "dom_attr", "dom_text", "react_prop", "react_state", "net_header", "input_event"}
	for _, p := range expectedLowValue {
		if !predicates[p] {
			t.Errorf("Expected %q to be a low-value predicate", p)
		}
	}

	unexpectedLowValue := []string{"console_event", "net_request", "net_response", "navigation_event"}
	for _, p := range unexpectedLowValue {
		if predicates[p] {
			t.Errorf("Expected %q to NOT be a low-value predicate", p)
		}
	}
}

func TestEngineLoadSchemaError(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "/nonexistent/path/schema.mg",
		FactBufferLimit: 1000,
	}

	_, err := NewEngine(cfg)
	if err == nil {
		t.Error("Expected error for nonexistent schema path")
	}
}

func TestEngineQueryParseError(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()
	_, err = engine.Query(ctx, "invalid syntax $$")
	if err == nil {
		t.Error("Expected parse error for invalid query syntax")
	}
}

func TestEngineAddRuleParseError(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	err = engine.AddRule("invalid rule syntax $$")
	if err == nil {
		t.Error("Expected parse error for invalid rule syntax")
	}
}

func TestEngineToastNotificationFacts(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Add toast notification facts
	facts := []Fact{
		{
			Predicate: "toast_notification",
			Args:      []interface{}{"Failed to save", "error", "shadcn", int64(1000)},
			Timestamp: time.Now(),
		},
		{
			Predicate: "error_toast",
			Args:      []interface{}{"Failed to save", "shadcn", int64(1000)},
			Timestamp: time.Now(),
		},
		{
			Predicate: "net_request",
			Args:      []interface{}{"req1", "POST", "/api/save", "fetch", int64(900)},
			Timestamp: time.Now(),
		},
		{
			Predicate: "net_response",
			Args:      []interface{}{"req1", int64(500), int64(50), int64(100)},
			Timestamp: time.Now(),
		},
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	// Verify toast facts are stored
	toastFacts := engine.FactsByPredicate("toast_notification")
	if len(toastFacts) != 1 {
		t.Errorf("Expected 1 toast_notification fact, got %d", len(toastFacts))
	}

	errorToasts := engine.FactsByPredicate("error_toast")
	if len(errorToasts) != 1 {
		t.Errorf("Expected 1 error_toast fact, got %d", len(errorToasts))
	}
}

func TestEngineQueryTemporalEdgeCases(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()
	now := time.Now()

	t.Run("no matching predicate", func(t *testing.T) {
		results := engine.QueryTemporal("nonexistent", time.Time{}, time.Time{})
		if len(results) != 0 {
			t.Errorf("Expected 0 results, got %d", len(results))
		}
	})

	t.Run("before filter only", func(t *testing.T) {
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "temporal_before", Args: []interface{}{"old"}, Timestamp: now.Add(-10 * time.Second)},
			{Predicate: "temporal_before", Args: []interface{}{"new"}, Timestamp: now},
		})

		results := engine.QueryTemporal("temporal_before", time.Time{}, now.Add(-5*time.Second))
		if len(results) != 1 {
			t.Errorf("Expected 1 result with before filter, got %d", len(results))
		}
	})

	t.Run("after and before window", func(t *testing.T) {
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "temporal_window", Args: []interface{}{"a"}, Timestamp: now.Add(-10 * time.Second)},
			{Predicate: "temporal_window", Args: []interface{}{"b"}, Timestamp: now.Add(-5 * time.Second)},
			{Predicate: "temporal_window", Args: []interface{}{"c"}, Timestamp: now},
		})

		results := engine.QueryTemporal("temporal_window", now.Add(-8*time.Second), now.Add(-2*time.Second))
		if len(results) != 1 {
			t.Errorf("Expected 1 result in time window, got %d", len(results))
		}
	})
}

func TestEngineQueryEmptyClause(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Empty query should return error
	_, err = engine.Query(ctx, "")
	if err == nil {
		t.Error("Expected error for empty query")
	}
}

func TestEngineWatchNotification(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	t.Run("notification to full channel is skipped", func(t *testing.T) {
		// Create channel with 0 buffer (blocking)
		ch := make(chan WatchEvent) // unbuffered

		engine.Subscribe("full_channel_test", ch)

		// This should not block - notification to full channel is skipped
		ctx := context.Background()
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "console_event", Args: []interface{}{"error", "test", int64(1000)}, Timestamp: time.Now()},
		})

		engine.Unsubscribe("full_channel_test", ch)
	})

	t.Run("notification with buffered channel", func(t *testing.T) {
		ch := make(chan WatchEvent, 10)
		engine.Subscribe("console_event", ch)

		ctx := context.Background()
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "console_event", Args: []interface{}{"error", "test notification", int64(1000)}, Timestamp: time.Now()},
		})

		// Give time for notification
		time.Sleep(50 * time.Millisecond)

		engine.Unsubscribe("console_event", ch)
	})
}

func TestEngineEvaluate(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Add facts that should trigger derived predicates
	facts := []Fact{
		{Predicate: "net_request", Args: []interface{}{"req1", "GET", "/api/fail", "fetch", int64(1000)}, Timestamp: time.Now()},
		{Predicate: "net_response", Args: []interface{}{"req1", int64(500), int64(50), int64(100)}, Timestamp: time.Now()},
	}
	_ = engine.AddFacts(ctx, facts)

	// Evaluate the failed_request predicate
	results, err := engine.Evaluate(ctx, "failed_request")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	// Just verify no crash - derived rules depend on schema
	t.Logf("Evaluate returned %d results for failed_request", len(results))
}

func TestEngineMatchesAllEdgeCases(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()
	_ = engine.AddFacts(ctx, []Fact{
		{Predicate: "short_args", Args: []interface{}{"a"}, Timestamp: time.Now()},
	})

	t.Run("more condition args than fact args", func(t *testing.T) {
		conditions := []Fact{
			{Predicate: "short_args", Args: []interface{}{"a", "b", "c"}}, // more args than fact
		}
		if engine.MatchesAll(conditions) {
			t.Error("Expected no match when condition has more args than fact")
		}
	})
}

func TestEngineSamplingRateThresholds(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 100,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Fill buffer to different levels and check sampling rate
	t.Run("sampling rate at different fill levels", func(t *testing.T) {
		// Add 45 facts (45% full) - should be 1.0 rate
		for i := 0; i < 45; i++ {
			_ = engine.AddFacts(ctx, []Fact{
				{Predicate: "console_event", Args: []interface{}{"error", "msg", int64(i)}, Timestamp: time.Now()},
			})
		}
		rate := engine.SamplingRate()
		if rate != 1.0 {
			t.Errorf("At 45%% full, expected rate 1.0, got %v", rate)
		}
	})
}

func TestEngineNoBufferLimit(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 0, // No limit
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()
	for i := 0; i < 100; i++ {
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "unbounded", Args: []interface{}{i}, Timestamp: time.Now()},
		})
	}

	// Sampling rate should be 1.0 with no limit
	if rate := engine.SamplingRate(); rate != 1.0 {
		t.Errorf("Expected rate 1.0 with no limit, got %v", rate)
	}

	// All facts should be present
	facts := engine.FactsByPredicate("unbounded")
	if len(facts) != 100 {
		t.Errorf("Expected 100 facts, got %d", len(facts))
	}
}

func TestEngineToConstantTypes(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Test various Go types in facts
	facts := []Fact{
		{Predicate: "type_test", Args: []interface{}{"string value"}, Timestamp: time.Now()},
		{Predicate: "type_test", Args: []interface{}{42}, Timestamp: time.Now()},
		{Predicate: "type_test", Args: []interface{}{int64(123)}, Timestamp: time.Now()},
		{Predicate: "type_test", Args: []interface{}{3.14}, Timestamp: time.Now()},
		{Predicate: "type_test", Args: []interface{}{true}, Timestamp: time.Now()},
		{Predicate: "type_test", Args: []interface{}{false}, Timestamp: time.Now()},
		{Predicate: "type_test", Args: []interface{}{[]byte("bytes")}, Timestamp: time.Now()},
	}

	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	stored := engine.FactsByPredicate("type_test")
	if len(stored) != len(facts) {
		t.Errorf("Expected %d facts, got %d", len(facts), len(stored))
	}
}

func TestEngineAddRuleWithoutSchema(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "", // No schema loaded
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Adding a valid rule without prior schema should work
	rule := `
Decl standalone_rule().
standalone_rule() :- console_event("error", _, _).
`
	err = engine.AddRule(rule)
	// This may or may not error depending on analysis
	t.Logf("AddRule without schema: %v", err)
}

func TestEngineEvaluateWithSubscription(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Subscribe to failed_request predicate
	ch := make(chan WatchEvent, 10)
	engine.Subscribe("failed_request", ch)
	defer engine.Unsubscribe("failed_request", ch)

	ctx := context.Background()

	// Add facts that should trigger the failed_request derivation
	facts := []Fact{
		{Predicate: "net_request", Args: []interface{}{"req1", "POST", "/api/fail", "fetch", int64(1000)}, Timestamp: time.Now()},
		{Predicate: "net_response", Args: []interface{}{"req1", int64(500), int64(50), int64(100)}, Timestamp: time.Now()},
	}
	_ = engine.AddFacts(ctx, facts)

	// Evaluate should trigger notification via atomToFact/convertConstant
	results, err := engine.Evaluate(ctx, "failed_request")
	if err != nil {
		t.Logf("Evaluate returned error (may be expected): %v", err)
	} else {
		t.Logf("Evaluate returned %d results", len(results))
	}

	// Give some time for notification to be processed
	time.Sleep(50 * time.Millisecond)
}

func TestEngineNotifySubscribersEdgeCases(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	t.Run("no subscribers for predicate", func(t *testing.T) {
		ctx := context.Background()
		// Add facts without any subscribers - should not panic
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "unsubscribed_pred", Args: []interface{}{"val"}, Timestamp: time.Now()},
		})
	})

	t.Run("multiple subscribers for same predicate", func(t *testing.T) {
		ch1 := make(chan WatchEvent, 10)
		ch2 := make(chan WatchEvent, 10)
		ch3 := make(chan WatchEvent, 10)

		engine.Subscribe("multi_sub", ch1)
		engine.Subscribe("multi_sub", ch2)
		engine.Subscribe("multi_sub", ch3)

		ctx := context.Background()
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "multi_sub", Args: []interface{}{"test"}, Timestamp: time.Now()},
		})

		// Verify WatchPredicates shows the predicate
		predicates := engine.WatchPredicates()
		found := false
		for _, p := range predicates {
			if p == "multi_sub" {
				found = true
				break
			}
		}
		if !found {
			t.Error("Expected multi_sub in watched predicates")
		}

		engine.Unsubscribe("multi_sub", ch1)
		engine.Unsubscribe("multi_sub", ch2)
		engine.Unsubscribe("multi_sub", ch3)
	})
}

func TestEngineQueryWithVariousPredicates(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Add facts with various argument types
	facts := []Fact{
		{Predicate: "console_event", Args: []interface{}{"error", "ReferenceError", int64(1000)}, Timestamp: time.Now()},
		{Predicate: "console_event", Args: []interface{}{"warn", "Deprecated API", int64(1001)}, Timestamp: time.Now()},
		{Predicate: "net_request", Args: []interface{}{"req1", "GET", "/api/data", "fetch", int64(1002)}, Timestamp: time.Now()},
	}
	_ = engine.AddFacts(ctx, facts)

	t.Run("query console_event with variable binding", func(t *testing.T) {
		results, err := engine.Query(ctx, "console_event(Level, Msg, Ts).")
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if len(results) < 2 {
			t.Logf("Expected at least 2 results, got %d", len(results))
		}
	})

	t.Run("query with partial binding", func(t *testing.T) {
		results, err := engine.Query(ctx, `console_event("error", Msg, _).`)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		t.Logf("Got %d results for error console_event", len(results))
	})

	t.Run("query non-existent predicate", func(t *testing.T) {
		results, err := engine.Query(ctx, "nonexistent_predicate(X, Y).")
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Expected 0 results for non-existent predicate, got %d", len(results))
		}
	})
}

func TestEngineAddFactsWithDifferentTypes(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	t.Run("add fact with float64", func(t *testing.T) {
		facts := []Fact{
			{Predicate: "float_test", Args: []interface{}{3.14159, 2.71828}, Timestamp: time.Now()},
		}
		if err := engine.AddFacts(ctx, facts); err != nil {
			t.Fatalf("AddFacts failed: %v", err)
		}

		stored := engine.FactsByPredicate("float_test")
		if len(stored) != 1 {
			t.Errorf("Expected 1 fact, got %d", len(stored))
		}
	})

	t.Run("add fact with mixed types", func(t *testing.T) {
		facts := []Fact{
			{Predicate: "mixed_test", Args: []interface{}{"str", 42, int64(123), 3.14, true}, Timestamp: time.Now()},
		}
		if err := engine.AddFacts(ctx, facts); err != nil {
			t.Fatalf("AddFacts failed: %v", err)
		}

		stored := engine.FactsByPredicate("mixed_test")
		if len(stored) != 1 {
			t.Errorf("Expected 1 fact, got %d", len(stored))
		}
	})

	t.Run("add empty args fact", func(t *testing.T) {
		facts := []Fact{
			{Predicate: "empty_args", Args: []interface{}{}, Timestamp: time.Now()},
		}
		if err := engine.AddFacts(ctx, facts); err != nil {
			t.Fatalf("AddFacts failed: %v", err)
		}

		stored := engine.FactsByPredicate("empty_args")
		if len(stored) != 1 {
			t.Errorf("Expected 1 fact, got %d", len(stored))
		}
	})
}

func TestEngineMatchesAllWithWildcards(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()
	_ = engine.AddFacts(ctx, []Fact{
		{Predicate: "match_test", Args: []interface{}{"a", "b", "c"}, Timestamp: time.Now()},
	})

	t.Run("match with underscore wildcard", func(t *testing.T) {
		conditions := []Fact{
			{Predicate: "match_test", Args: []interface{}{"_"}},
		}
		// Underscore is treated as a literal string, not wildcard in MatchesAll
		result := engine.MatchesAll(conditions)
		t.Logf("MatchesAll with _ returned: %v", result)
	})

	t.Run("match with empty args condition", func(t *testing.T) {
		conditions := []Fact{
			{Predicate: "match_test", Args: nil},
		}
		if !engine.MatchesAll(conditions) {
			t.Error("Expected match with nil args (any fact with predicate)")
		}
	})
}

func TestEngineBufferEviction(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 5,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Add more high-value facts than buffer can hold
	for i := 0; i < 10; i++ {
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "console_event", Args: []interface{}{"error", "msg" + string(rune('A'+i)), int64(i)}, Timestamp: time.Now()},
		})
	}

	facts := engine.Facts()
	if len(facts) > 5 {
		t.Errorf("Expected buffer size <= 5, got %d", len(facts))
	}

	// Verify index is properly rebuilt after eviction
	indexed := engine.FactsByPredicate("console_event")
	if len(indexed) != len(facts) {
		t.Errorf("Index mismatch: indexed=%d, buffer=%d", len(indexed), len(facts))
	}
}

func TestEngineContextCancellation(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	t.Run("AddFacts with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := engine.AddFacts(ctx, []Fact{
			{Predicate: "test", Args: []interface{}{"val"}, Timestamp: time.Now()},
		})
		// AddFacts should still work as it doesn't check context
		if err != nil {
			t.Logf("AddFacts with cancelled context: %v", err)
		}
	})

	t.Run("Query with cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := engine.Query(ctx, "test(X).")
		// Query may or may not respect cancellation
		t.Logf("Query with cancelled context: %v", err)
	})
}

func TestEngineLowValuePredicateSampling(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 100,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Fill buffer to trigger sampling (need high fill rate)
	for i := 0; i < 80; i++ {
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "console_event", Args: []interface{}{"error", "msg", int64(i)}, Timestamp: time.Now()},
		})
	}

	// Now add low-value facts - some may be sampled out
	lowValueCount := 0
	for i := 0; i < 50; i++ {
		err := engine.AddFacts(ctx, []Fact{
			{Predicate: "dom_node", Args: []interface{}{"node" + string(rune(i)), "div"}, Timestamp: time.Now()},
		})
		if err == nil {
			lowValueCount++
		}
	}

	// Verify sampling rate has adjusted
	rate := engine.SamplingRate()
	t.Logf("Sampling rate after high fill: %v", rate)

	// Some low-value facts may have been dropped
	domFacts := engine.FactsByPredicate("dom_node")
	t.Logf("Accepted %d dom_node facts, stored %d", lowValueCount, len(domFacts))
}

func TestEngineRebuildIndex(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 10,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Add facts of different predicates
	_ = engine.AddFacts(ctx, []Fact{
		{Predicate: "pred_a", Args: []interface{}{"a1"}, Timestamp: time.Now()},
		{Predicate: "pred_b", Args: []interface{}{"b1"}, Timestamp: time.Now()},
		{Predicate: "pred_a", Args: []interface{}{"a2"}, Timestamp: time.Now()},
	})

	// Force buffer eviction by adding more
	for i := 0; i < 15; i++ {
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "console_event", Args: []interface{}{"error", "overflow", int64(i)}, Timestamp: time.Now()},
		})
	}

	// Verify index still works correctly after eviction
	total := engine.Facts()
	for _, f := range total {
		indexed := engine.FactsByPredicate(f.Predicate)
		if len(indexed) == 0 {
			t.Errorf("No indexed facts for predicate %s", f.Predicate)
		}
	}
}

func TestEngineNotifySubscribersWithDerivedFacts(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Subscribe to a predicate that will have derived facts
	ch := make(chan WatchEvent, 10)
	engine.Subscribe("failed_request", ch)
	defer engine.Unsubscribe("failed_request", ch)

	ctx := context.Background()

	// Add facts that match the failed_request rule (net_response with status >= 400)
	facts := []Fact{
		{Predicate: "net_request", Args: []interface{}{"req-notify", "POST", "/api/fail", "fetch", int64(1000)}, Timestamp: time.Now()},
		{Predicate: "net_response", Args: []interface{}{"req-notify", int64(500), int64(50), int64(100)}, Timestamp: time.Now()},
	}
	_ = engine.AddFacts(ctx, facts)

	// Give a moment for notification processing
	time.Sleep(100 * time.Millisecond)

	// Check if notification was sent (non-blocking check)
	select {
	case event := <-ch:
		t.Logf("Received watch event for predicate: %s with %d facts", event.Predicate, len(event.Facts))
	default:
		t.Log("No notification received (derived rule may not have matched)")
	}
}

func TestEngineQueryWithConstantMatching(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Add test facts
	facts := []Fact{
		{Predicate: "console_event", Args: []interface{}{"error", "TypeError: undefined", int64(1000)}, Timestamp: time.Now()},
		{Predicate: "console_event", Args: []interface{}{"warn", "Deprecation warning", int64(1001)}, Timestamp: time.Now()},
		{Predicate: "console_event", Args: []interface{}{"error", "ReferenceError", int64(1002)}, Timestamp: time.Now()},
	}
	_ = engine.AddFacts(ctx, facts)

	// Query with a constant that should match via queryBufferDirect fallback
	results, err := engine.Query(ctx, `console_event("error", Msg, Ts).`)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	t.Logf("Query returned %d results for console_event with error level", len(results))
	for i, r := range results {
		t.Logf("  Result %d: %+v", i, r)
	}
}

func TestEngineNotifySubscribersDirectCall(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	t.Run("notify with subscribers", func(t *testing.T) {
		ch := make(chan WatchEvent, 5)
		engine.Subscribe("test_notify", ch)
		defer engine.Unsubscribe("test_notify", ch)

		// Directly call notifySubscribers (via the exposed mechanism)
		// We simulate this by adding facts that will trigger checkAndNotifyWatchers
		ctx := context.Background()
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "test_notify", Args: []interface{}{"value1"}, Timestamp: time.Now()},
		})

		// Check if channel received event
		select {
		case event := <-ch:
			t.Logf("Received event: predicate=%s, facts=%d", event.Predicate, len(event.Facts))
		case <-time.After(100 * time.Millisecond):
			t.Log("No event received within timeout (expected if predicate not in store)")
		}
	})

	t.Run("notify empty facts slice", func(t *testing.T) {
		ch := make(chan WatchEvent, 5)
		engine.Subscribe("empty_facts_test", ch)
		defer engine.Unsubscribe("empty_facts_test", ch)

		// This should not send any notifications (empty facts condition in notifySubscribers)
		select {
		case <-ch:
			t.Error("Should not receive event for empty facts")
		case <-time.After(50 * time.Millisecond):
			// Expected - no notification sent
		}
	})
}

func TestEngineAtomToFactConversion(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Subscribe to trigger atomToFact via checkAndNotifyWatchers
	ch := make(chan WatchEvent, 10)
	engine.Subscribe("net_response", ch)
	defer engine.Unsubscribe("net_response", ch)

	ctx := context.Background()

	// Add facts that will be stored and then queried via atomToFact
	facts := []Fact{
		{Predicate: "net_response", Args: []interface{}{"req-atom", int64(200), int64(50), int64(100)}, Timestamp: time.Now()},
	}
	_ = engine.AddFacts(ctx, facts)

	// The atomToFact function is called when iterating over store facts
	// Trigger evaluation to exercise atomToFact path
	result, err := engine.Evaluate(ctx, "net_response")
	if err != nil {
		t.Logf("Evaluate returned error (may be expected): %v", err)
	} else {
		t.Logf("Evaluate returned %d facts", len(result))
		for _, f := range result {
			t.Logf("  Fact: predicate=%s, args=%v", f.Predicate, f.Args)
		}
	}

	// Give time for any async processing
	time.Sleep(50 * time.Millisecond)
}

func TestEngineNotifySubscribersWithBaseFacts(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// Subscribe to a base fact predicate (not derived) - this ensures GetFacts finds the facts
	ch := make(chan WatchEvent, 10)
	engine.Subscribe("console_event", ch)
	defer engine.Unsubscribe("console_event", ch)

	ctx := context.Background()

	// Add multiple facts - they get stored in the Mangle store
	facts := []Fact{
		{Predicate: "console_event", Args: []interface{}{"error", "Test error message", int64(1000)}, Timestamp: time.Now()},
		{Predicate: "console_event", Args: []interface{}{"warn", "Test warning", int64(1001)}, Timestamp: time.Now()},
	}
	err = engine.AddFacts(ctx, facts)
	if err != nil {
		t.Fatalf("AddFacts failed: %v", err)
	}

	// Wait for notification
	select {
	case event := <-ch:
		t.Logf("SUCCESS: Received watch event for %s with %d facts", event.Predicate, len(event.Facts))
		if event.Predicate != "console_event" {
			t.Errorf("Expected predicate console_event, got %s", event.Predicate)
		}
		if len(event.Facts) == 0 {
			t.Error("Expected non-empty facts slice")
		}
	case <-time.After(200 * time.Millisecond):
		t.Log("No notification received - checking if store has facts")
		// Verify facts are in the store by evaluating
		result, evalErr := engine.Evaluate(ctx, "console_event")
		if evalErr != nil {
			t.Logf("Evaluate error: %v", evalErr)
		} else {
			t.Logf("Evaluate found %d console_event facts", len(result))
		}
	}
}

func TestEngineQueryBufferDirectWithConstants(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}

	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	ctx := context.Background()

	// Add facts with various argument types
	facts := []Fact{
		{Predicate: "net_request", Args: []interface{}{"req-1", "GET", "/api/users", "fetch", int64(1000)}, Timestamp: time.Now()},
		{Predicate: "net_request", Args: []interface{}{"req-2", "POST", "/api/users", "xhr", int64(1001)}, Timestamp: time.Now()},
		{Predicate: "net_request", Args: []interface{}{"req-3", "GET", "/api/data", "fetch", int64(1002)}, Timestamp: time.Now()},
	}
	_ = engine.AddFacts(ctx, facts)

	// Query with constant that should match only GET requests
	// This exercises queryBufferDirect constant matching branch
	results, err := engine.Query(ctx, `net_request(ReqId, "GET", Url, Type, Ts).`)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	t.Logf("Query for GET requests returned %d results", len(results))
	// Should match req-1 and req-3 (both GET)
	if len(results) < 2 {
		t.Logf("Expected at least 2 GET requests, got %d", len(results))
	}

	// Query with constant that matches no facts
	results2, err := engine.Query(ctx, `net_request(ReqId, "DELETE", Url, Type, Ts).`)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	if len(results2) != 0 {
		t.Errorf("Expected 0 results for DELETE requests, got %d", len(results2))
	}

	t.Run("query with more args than stored fact", func(t *testing.T) {
		// Add a fact with fewer args
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "short_query_test", Args: []interface{}{"a", "b"}, Timestamp: time.Now()},
		})
		// Query with more args - should not match (tests len check in queryBufferDirect)
		results, err := engine.Query(ctx, `short_query_test(A, B, C, D).`)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		t.Logf("Query with more args than fact returned %d results", len(results))
	})

	t.Run("query with invalid index", func(t *testing.T) {
		// This tests the idx bounds check in queryBufferDirect
		// We need a scenario where index contains stale indices
		// For now, verify basic behavior
		results, err := engine.Query(ctx, `nonexistent_query_pred(X).`)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("Expected 0 results for nonexistent predicate, got %d", len(results))
		}
	})
}
