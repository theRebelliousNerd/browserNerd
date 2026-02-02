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
			{
				Predicate: "dom_attr",
				Args:      []interface{}{"loader-1", "id", "loading-overlay"},
				Timestamp: time.Now(),
			},
			{
				Predicate: "dom_attr",
				Args:      []interface{}{"spin-1", "class", "loading-spinner"},
				Timestamp: time.Now(),
			},
		}
		if err := engine.AddFacts(ctx, facts); err != nil {
			t.Fatal(err)
		}

		results, err := engine.Evaluate(ctx, "screen_blocked")
		if err != nil {
			t.Fatal(err)
		}

		if len(results) < 4 {
			t.Errorf("expected at least 4 blocked elements, got %d", len(results))
		}
	})

	t.Run("Macro: interaction_blocked", func(t *testing.T) {
		facts := []Fact{
			{
				Predicate: "current_url",
				Args:      []interface{}{"session-1", "https://example.com"},
				Timestamp: time.Now(),
			},
			{
				Predicate: "dom_attr",
				Args:      []interface{}{"blocker", "class", "modal"},
				Timestamp: time.Now(),
			},
		}
		if err := engine.AddFacts(ctx, facts); err != nil {
			t.Fatal(err)
		}

		results, err := engine.Evaluate(ctx, "interaction_blocked")
		if err != nil {
			t.Fatal(err)
		}

		if len(results) == 0 {
			t.Errorf("expected interaction_blocked to be derived")
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
			{
				Predicate: "dom_attr",
				Args:      []interface{}{"content-2", "role", "main"},
				Timestamp: time.Now(),
			},
			{
				Predicate: "dom_attr",
				Args:      []interface{}{"content-3", "class", "main-content"},
				Timestamp: time.Now(),
			},
		}
		if err := engine.AddFacts(ctx, facts); err != nil {
			t.Fatal(err)
		}

		results, err := engine.Evaluate(ctx, "is_main_content")
		if err != nil {
			t.Fatal(err)
		}

		if len(results) < 4 {
			t.Errorf("expected at least 4 main content areas, got %d", len(results))
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
			{
				Predicate: "interactive",
				Args:      []interface{}{"btn-3", "button", "Go", "click"},
				Timestamp: time.Now(),
			},
			{
				Predicate: "dom_attr",
				Args:      []interface{}{"btn-3", "id", "submit-button"},
				Timestamp: time.Now(),
			},
		}
		if err := engine.AddFacts(ctx, facts); err != nil {
			t.Fatal(err)
		}

		results, err := engine.Evaluate(ctx, "primary_action")
		if err != nil {
			t.Fatal(err)
		}

		if len(results) < 3 {
			t.Errorf("expected at least 3 primary actions, got %d", len(results))
		}
	})
}