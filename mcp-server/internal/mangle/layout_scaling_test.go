package mangle

import (
	"context"
	"fmt"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

// TestLayoutFactsDoNotDeriveQuadratically guards both shipped schemas against a
// rule that joins dom_layout with itself.
//
// Live 2026-09-24 elements_far_apart paired every laid-out element with every
// other: a login page's layout derived 10,140 facts, past the 10,000 derived
// fact ceiling, and from then on every fact insertion re-evaluated the program
// and failed. Every CDP event paid that quadratic evaluation, and page
// operations on the session stopped returning.
func TestLayoutFactsDoNotDeriveQuadratically(t *testing.T) {
	engine, err := NewEngine(config.MangleConfig{Enable: true, FactBufferLimit: 4096})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	facts := make([]Fact, 0, 120)
	for i := range 120 {
		facts = append(facts, Fact{
			Predicate: "dom_layout",
			Args:      []any{"s1", fmt.Sprintf("n%d", i), float64(i * 40), float64(i * 25), 100.0, 20.0, true},
			Timestamp: now,
		})
	}
	if err := engine.AddFacts(context.Background(), facts); err != nil {
		t.Fatalf("120 laid-out elements must insert without exhausting derivation: %v", err)
	}
	follow := []Fact{{Predicate: "console_event", Args: []any{"s1", "log", "ready", now.UnixMilli()}, Timestamp: now}}
	if err := engine.AddFacts(context.Background(), follow); err != nil {
		t.Fatalf("a later event must still insert: %v", err)
	}
}
