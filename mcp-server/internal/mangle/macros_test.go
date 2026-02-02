package mangle

import (
	"context"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

func TestSemanticMacros(t *testing.T) {
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

	t.Run("Macro: screen_blocked", func(t *testing.T) {
		facts := []Fact{
			{
				Predicate: "dom_attr",
				Args:      []interface{}{"modal-1", "class", "modal-backdrop"},
				Timestamp: time.Now(),
			},
			{
				Predicate: "dom_node",
				Args:      []interface{}{"modal-1", "div", "", "body"},
				Timestamp: time.Now(),
			},
			{
				Predicate: "dom_attr",
				Args:      []interface{}{"dialog-1", "role", "dialog"},
				Timestamp: time.Now(),
			},
		}
		if err := engine.AddFacts(ctx, facts); err != nil {
			t.Fatal(err)
		}

		results, err := engine.Query(ctx, "screen_blocked(Id, Reason).")
		if err != nil {
			t.Fatal(err)
		}

		if len(results) < 2 {
			t.Errorf("expected at least 2 blocked elements, got %d", len(results))
		}
	})

	t.Run("Macro: is_main_content", func(t *testing.T) {
		facts := []Fact{
			{
				Predicate: "dom_node",
				Args:      []interface{}{"main-1", "main", "Article text", "body"},
				Timestamp: time.Now(),
			},
			{
				Predicate: "dom_attr",
				Args:      []interface{}{"content-1", "id", "main"},
				Timestamp: time.Now(),
			},
		}
		if err := engine.AddFacts(ctx, facts); err != nil {
			t.Fatal(err)
		}

		results, err := engine.Query(ctx, "is_main_content(Id).")
		if err != nil {
			t.Fatal(err)
		}

		if len(results) < 2 {
			t.Errorf("expected at least 2 main content areas, got %d", len(results))
		}
	})

	t.Run("Macro: primary_action", func(t *testing.T) {
		facts := []Fact{
			{
				Predicate: "interactive",
				Args:      []interface{}{"btn-1", "button", "Submit", "click"},
				Timestamp: time.Now(),
			},
			{
				Predicate: "dom_attr",
				Args:      []interface{}{"btn-1", "type", "submit"},
				Timestamp: time.Now(),
			},
			{
				Predicate: "interactive",
				Args:      []interface{}{"btn-2", "button", "Login", "click"},
				Timestamp: time.Now(),
			},
			{
				Predicate: "dom_attr",
				Args:      []interface{}{"btn-2", "class", "btn-primary"},
				Timestamp: time.Now(),
			},
		}
		if err := engine.AddFacts(ctx, facts); err != nil {
			t.Fatal(err)
		}

		results, err := engine.Query(ctx, "primary_action(Id, Label).")
		if err != nil {
			t.Fatal(err)
		}

		if len(results) < 2 {
			t.Errorf("expected at least 2 primary actions, got %d", len(results))
		}
	})
}
