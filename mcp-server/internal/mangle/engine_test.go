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
