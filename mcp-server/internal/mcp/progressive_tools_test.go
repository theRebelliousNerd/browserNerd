package mcp

import (
	"context"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

func testMangleEngineForProgressive(t *testing.T) *mangle.Engine {
	t.Helper()
	cfg := config.MangleConfig{
		Enable:          true,
		SchemaPath:      "../../schemas/browser.mg",
		FactBufferLimit: 2000,
	}
	engine, err := mangle.NewEngine(cfg)
	if err != nil {
		t.Fatalf("failed to create mangle engine: %v", err)
	}
	return engine
}

func TestProgressiveToolContracts(t *testing.T) {
	t.Run("browser-observe contract", func(t *testing.T) {
		tool := &BrowserObserveTool{}
		if tool.Name() != "browser-observe" {
			t.Fatalf("unexpected name: %s", tool.Name())
		}
		schema := tool.InputSchema()
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 || required[0] != "session_id" {
			t.Fatalf("browser-observe should require session_id")
		}
	})

	t.Run("browser-act contract", func(t *testing.T) {
		tool := &BrowserActTool{}
		if tool.Name() != "browser-act" {
			t.Fatalf("unexpected name: %s", tool.Name())
		}
		schema := tool.InputSchema()
		required, ok := schema["required"].([]string)
		if !ok || len(required) < 2 {
			t.Fatalf("browser-act should require session_id and operations")
		}
	})

	t.Run("browser-reason contract", func(t *testing.T) {
		tool := &BrowserReasonTool{}
		if tool.Name() != "browser-reason" {
			t.Fatalf("unexpected name: %s", tool.Name())
		}
		schema := tool.InputSchema()
		required, ok := schema["required"].([]string)
		if !ok || len(required) == 0 || required[0] != "session_id" {
			t.Fatalf("browser-reason should require session_id")
		}
	})
}

func TestEvaluateJSGating(t *testing.T) {
	t.Run("denies when gate metadata is missing", func(t *testing.T) {
		tool := &EvaluateJSTool{}
		result, err := tool.Execute(context.Background(), map[string]interface{}{
			"session_id": "s1",
			"script":     "document.title",
		})
		if err != nil {
			t.Fatalf("execute failed: %v", err)
		}
		resultMap := result.(map[string]interface{})
		if success, _ := resultMap["success"].(bool); success {
			t.Fatalf("expected success=false when gate is missing")
		}
		if gated, _ := resultMap["gated"].(bool); !gated {
			t.Fatalf("expected gated=true")
		}
	})

	t.Run("allows explicit user intent", func(t *testing.T) {
		tool := &EvaluateJSTool{}
		ok, msg := tool.evaluateJSGateOpen("s1", "explicit_user_intent", "")
		if !ok {
			t.Fatalf("expected explicit_user_intent gate to pass, got: %s", msg)
		}
	})

	t.Run("allows approved handle when recent handle fact exists", func(t *testing.T) {
		engine := testMangleEngineForProgressive(t)
		tool := &EvaluateJSTool{engine: engine}
		ctx := context.Background()

		now := time.Now()
		_ = engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "disclosure_handle",
			Args:      []interface{}{"s2", "reason:s2:root_causes", "reason", now.UnixMilli()},
			Timestamp: now,
		}})

		ok, msg := tool.evaluateJSGateOpen("s2", "", "reason:s2:root_causes")
		if !ok {
			t.Fatalf("expected approved handle to pass, got: %s", msg)
		}
	})

	t.Run("allows low_confidence only when gate fact exists", func(t *testing.T) {
		engine := testMangleEngineForProgressive(t)
		tool := &EvaluateJSTool{engine: engine}
		ctx := context.Background()

		now := time.Now()
		_ = engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "js_gate_open",
			Args:      []interface{}{"s3", "low_confidence", now.UnixMilli()},
			Timestamp: now,
		}})

		ok, msg := tool.evaluateJSGateOpen("s3", "low_confidence", "")
		if !ok {
			t.Fatalf("expected low_confidence gate to pass, got: %s", msg)
		}
	})
}

func TestBrowserReasonEmitsGateFacts(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	tool := &BrowserReasonTool{engine: engine}
	ctx := context.Background()

	now := time.Now()
	_ = engine.AddFacts(ctx, []mangle.Fact{
		{
			Predicate: "failed_request",
			Args:      []interface{}{"req-1", "/api/test", 500},
			Timestamp: now,
		},
		{
			Predicate: "toast_notification",
			Args:      []interface{}{"Saved successfully", "success", "ui", now.UnixMilli()},
			Timestamp: now,
		},
	})

	result, err := tool.Execute(ctx, map[string]interface{}{
		"session_id": "s-reason",
		"topic":      "health",
		"view":       "summary",
	})
	if err != nil {
		t.Fatalf("browser-reason execute failed: %v", err)
	}

	resultMap := result.(map[string]interface{})
	if status := resultMap["status"]; status != "error" {
		t.Fatalf("expected status=error, got %v", status)
	}
	if suggested, _ := resultMap["expansion_suggested"].(bool); !suggested {
		t.Fatalf("expected expansion_suggested=true")
	}

	if !hasRecentGateFact(engine, "js_gate_open", "s-reason", "low_confidence", jsGateTTL) {
		t.Fatalf("expected low_confidence gate fact")
	}
	if !hasRecentGateFact(engine, "js_gate_open", "s-reason", "contradiction_detected", jsGateTTL) {
		t.Fatalf("expected contradiction_detected gate fact")
	}
}
