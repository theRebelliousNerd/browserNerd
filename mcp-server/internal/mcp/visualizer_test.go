package mcp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

func TestVisualizeFactsLogic(t *testing.T) {
	// Setup engine with schema
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 1000,
	}
	engine, err := mangle.NewEngine(cfg)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// Add sample interactive facts
	facts := []mangle.Fact{
		{
			Predicate: "interactive",
			Args:      []interface{}{"btn-1", "button", "Submit", "click"},
			Timestamp: time.Now(),
		},
		{
			Predicate: "dom_layout",
			Args:      []interface{}{"btn-1", 10.0, 20.0, 100.0, 50.0, "true"},
			Timestamp: time.Now(),
		},
	}
	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatal(err)
	}

	// Test the coordinate extraction logic (internal part of Execute)
	queryStr := "interactive(Ref, \"button\", _, _)."
	variable := "Ref"

	results, err := engine.Query(ctx, queryStr)
	if err != nil {
		t.Fatal(err)
	}

	targetIDs := make(map[string]bool)
	for _, res := range results {
		if val, ok := res[variable]; ok {
			t.Logf("Value type: %T, value: %v", val, val)
			targetIDs[fmt.Sprintf("%v", val)] = true
		}
	}

	if !targetIDs["btn-1"] {
		t.Errorf("expected btn-1 to be in targetIDs, got %v", targetIDs)
	}

	// Verify layout retrieval
	layoutQuery := "dom_layout(Id, X, Y, W, H, Vis)."
	layoutRes, err := engine.Query(ctx, layoutQuery)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, res := range layoutRes {
		id := fmt.Sprintf("%v", res["Id"])
		if id == "btn-1" {
			found = true
			x := toFloat(res["X"])
			if x != 10.0 {
				t.Errorf("expected X=10.0, got %v", x)
			}
		}
	}
	if !found {
		t.Errorf("did not find layout for btn-1")
	}
}

func toFloat(v interface{}) float64 {
	var f float64
	fmt.Sscanf(fmt.Sprintf("%v", v), "%f", &f)
	return f
}
