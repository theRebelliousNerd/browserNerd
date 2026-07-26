package mangle

import (
	"context"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

func TestEngineRejectsOversizedRuleAndQuery(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 100,
		MaxRuleBytes:    32,
		MaxQueryBytes:   24,
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.AddRule(strings.Repeat("x", 33)); err == nil || !strings.Contains(err.Error(), "max_rule_bytes") {
		t.Fatalf("expected rule byte limit, got %v", err)
	}
	if _, err := engine.Query(context.Background(), strings.Repeat("q", 25)); err == nil || !strings.Contains(err.Error(), "max_query_bytes") {
		t.Fatalf("expected query byte limit, got %v", err)
	}
}

func TestEngineRejectsTooManyRuleClauses(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 100,
		MaxRuleClauses:  1,
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	rule := `
Decl first_guard().
Decl second_guard().
first_guard().
second_guard().
`
	if err := engine.AddRule(rule); err == nil || !strings.Contains(err.Error(), "clause limit") {
		t.Fatalf("expected clause limit, got %v", err)
	}
}

func TestEngineCapsQueryResults(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 100,
		MaxQueryResults: 2,
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 5; i++ {
		if err := engine.AddFacts(context.Background(), []Fact{{
			Predicate: "console_event",
			Args:      []interface{}{"session", "error", "message", int64(i)},
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
		}}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := engine.Query(context.Background(), "console_event(Session, Level, Message, Timestamp).")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected result cap of 2, got %d", len(rows))
	}
}

func TestWatchOnlyEmitsFactsDerivedAfterSubscription(t *testing.T) {
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 100,
	}
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now()
	oldFact := Fact{
		Predicate: "console_event",
		Args:      []interface{}{"session", "error", "historical", int64(1)},
		Timestamp: now,
	}
	if err := engine.AddFacts(ctx, []Fact{oldFact}); err != nil {
		t.Fatal(err)
	}

	ch := make(chan WatchEvent, 4)
	engine.Subscribe("console_event", ch)
	defer engine.Unsubscribe("console_event", ch)

	if err := engine.AddFacts(ctx, []Fact{{
		Predicate: "navigation_event",
		Args:      []interface{}{"session", "https://example.test", int64(2)},
		Timestamp: now.Add(time.Millisecond),
	}}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-ch:
		t.Fatalf("historical fact triggered watch: %+v", event)
	default:
	}

	if err := engine.AddFacts(ctx, []Fact{{
		Predicate: "console_event",
		Args:      []interface{}{"session", "error", "new", int64(3)},
		Timestamp: now.Add(2 * time.Millisecond),
	}}); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-ch:
		if len(event.Facts) != 1 || event.Facts[0].Args[2] != "new" {
			t.Fatalf("expected only newly observed fact, got %+v", event.Facts)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("expected watch event for newly added fact")
	}
}
