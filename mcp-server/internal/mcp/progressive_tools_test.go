package mcp

import (
	"context"
	"strings"
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

	t.Run("denies explicit user intent without approved handle", func(t *testing.T) {
		tool := &EvaluateJSTool{}
		ok, msg := tool.evaluateJSGateOpen("s1", "explicit_user_intent", "", tool.Name())
		if ok {
			t.Fatalf("expected explicit_user_intent gate to be denied without handle")
		}
		if !strings.Contains(msg, "requires approved_by_handle") {
			t.Fatalf("expected explicit handle guidance, got: %s", msg)
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

		ok, msg := tool.evaluateJSGateOpen("s2", "", "reason:s2:root_causes", tool.Name())
		if !ok {
			t.Fatalf("expected approved handle to pass, got: %s", msg)
		}
	})

	t.Run("allows explicit user intent with approved handle", func(t *testing.T) {
		engine := testMangleEngineForProgressive(t)
		tool := &EvaluateJSTool{engine: engine}
		ctx := context.Background()

		now := time.Now()
		_ = engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "disclosure_handle",
			Args:      []interface{}{"s2b", "reason:s2b:root_causes", "reason", now.UnixMilli()},
			Timestamp: now,
		}})

		ok, msg := tool.evaluateJSGateOpen("s2b", "explicit_user_intent", "reason:s2b:root_causes", tool.Name())
		if !ok {
			t.Fatalf("expected explicit_user_intent with approved handle to pass, got: %s", msg)
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

		ok, msg := tool.evaluateJSGateOpen("s3", "low_confidence", "", tool.Name())
		if !ok {
			t.Fatalf("expected low_confidence gate to pass, got: %s", msg)
		}
	})

	t.Run("gate error uses requested tool name", func(t *testing.T) {
		tool := &EvaluateJSTool{}
		ok, msg := tool.evaluateJSGateOpen("s4", "", "", "snapshot-dom")
		if ok {
			t.Fatalf("expected snapshot-dom gate check to fail without gate metadata")
		}
		if !strings.Contains(msg, "snapshot-dom is gated") {
			t.Fatalf("expected tool-specific gated message, got: %s", msg)
		}
	})
}

func TestEvaluateJSScriptNormalization(t *testing.T) {
	t.Run("keeps function script unchanged", func(t *testing.T) {
		got := normalizeEvalScriptForRod("() => document.title", false)
		if got != "() => document.title" {
			t.Fatalf("expected script to remain unchanged, got: %s", got)
		}
	})

	t.Run("wraps page expressions as zero-arg function", func(t *testing.T) {
		got := normalizeEvalScriptForRod("document.title", false)
		want := "() => (document.title)"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("wraps element expressions with el parameter", func(t *testing.T) {
		got := normalizeEvalScriptForRod("el.textContent", true)
		want := "el => (el.textContent)"
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
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
			Args:      []interface{}{"s-reason", "req-1", "/api/test", 500},
			Timestamp: now,
		},
		{
			Predicate: "toast_notification",
			Args:      []interface{}{"s-reason", "Saved successfully", "success", "ui", now.UnixMilli()},
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

func TestObserveIntentDefaults(t *testing.T) {
	cfg, ok := resolveObserveIntentDefaults("find_actions")
	if !ok {
		t.Fatal("expected find_actions intent to resolve")
	}
	if cfg.mode != "interactive" {
		t.Fatalf("expected mode=interactive, got %s", cfg.mode)
	}
	if cfg.view != "compact" {
		t.Fatalf("expected view=compact, got %s", cfg.view)
	}
	if cfg.maxItems <= 0 {
		t.Fatalf("expected positive maxItems, got %d", cfg.maxItems)
	}
}

func TestReasonIntentDefaults(t *testing.T) {
	cfg, ok := resolveReasonIntentDefaults("act_now")
	if !ok {
		t.Fatal("expected act_now intent to resolve")
	}
	if cfg.topic != "next_best_action" {
		t.Fatalf("expected next_best_action topic, got %s", cfg.topic)
	}
	if cfg.view != "compact" {
		t.Fatalf("expected compact view, got %s", cfg.view)
	}
}

func TestMangleActionCandidatesAndRecommendations(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	ctx := context.Background()
	now := time.Now()

	_ = engine.AddFacts(ctx, []mangle.Fact{
		{Predicate: "interactive", Args: []interface{}{"s-intent", "cta-1", "button", "Start", "click"}, Timestamp: now},
		{Predicate: "interactive", Args: []interface{}{"s-intent", "link-1", "link", "Learn more", "click"}, Timestamp: now},
		{Predicate: "element_enabled", Args: []interface{}{"s-intent", "cta-1", "true"}, Timestamp: now},
		{Predicate: "interaction_blocked", Args: []interface{}{"s-intent", "modal"}, Timestamp: now},
	})

	candidates := queryActionCandidates(ctx, engine, "s-intent", 20)
	if len(candidates) == 0 {
		t.Fatal("expected action candidates from mangle rules")
	}

	foundCTA := false
	foundEscape := false
	foundLink := false
	for _, c := range candidates {
		if c["ref"] == "cta-1" && c["action"] == "click" {
			foundCTA = true
		}
		if c["ref"] == "link-1" && c["action"] == "click" {
			foundLink = true
		}
		if c["action"] == "press_escape" {
			foundEscape = true
		}
	}

	if !foundCTA {
		t.Fatal("expected click candidate for cta-1")
	}
	if !foundLink {
		t.Fatal("expected click candidate for link-1")
	}
	if !foundEscape {
		t.Fatal("expected press_escape global action candidate")
	}

	recs := buildActionPlanRecommendations(candidates, 5, "s-intent", "https://example.com")
	if len(recs) == 0 {
		t.Fatal("expected action plan recommendations")
	}
	firstArgs, ok := recs[0]["args"].(map[string]interface{})
	if !ok {
		t.Fatal("expected args map on recommendation")
	}
	if firstArgs["session_id"] != "s-intent" {
		t.Fatalf("expected session_id=s-intent in recommendation args, got %v", firstArgs["session_id"])
	}
	if firstArgs["operations"] == nil {
		t.Fatal("expected operations in recommendation args")
	}
}

func TestBrowserReasonIntentAppliesDefaults(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	tool := &BrowserReasonTool{engine: engine}
	ctx := context.Background()

	result, err := tool.Execute(ctx, map[string]interface{}{
		"session_id": "s-act-now",
		"intent":     "act_now",
		"view":       "summary",
	})
	if err != nil {
		t.Fatalf("browser-reason execute failed: %v", err)
	}

	resultMap := result.(map[string]interface{})
	if resultMap["intent"] != "act_now" {
		t.Fatalf("expected intent=act_now, got %v", resultMap["intent"])
	}
	if resultMap["topic"] != "next_best_action" {
		t.Fatalf("expected topic=next_best_action, got %v", resultMap["topic"])
	}
}

func TestBuildActionPlanRecommendationsHandlesFormActions(t *testing.T) {
	candidates := []map[string]interface{}{
		{"action": "type", "ref": "email-input", "label": "Work Email", "priority": 90, "reason": "enabled_input"},
		{"action": "select", "ref": "country-select", "label": "Country", "priority": 80, "reason": "enabled_select"},
		{"action": "toggle", "ref": "terms-checkbox", "label": "Accept terms", "priority": 70, "reason": "toggle_control"},
	}

	recs := buildActionPlanRecommendations(candidates, 5, "s-form", "")
	if len(recs) != 3 {
		t.Fatalf("expected 3 recommendations, got %d", len(recs))
	}

	typeRec := recs[0]
	typeArgs, ok := typeRec["args"].(map[string]interface{})
	if !ok {
		t.Fatal("expected args map for type recommendation")
	}
	if typeArgs["session_id"] != "s-form" {
		t.Fatalf("expected session_id=s-form in recommendation args, got %v", typeArgs["session_id"])
	}
	typeOps, ok := typeArgs["operations"].([]map[string]interface{})
	if !ok || len(typeOps) != 1 {
		t.Fatal("expected single operation for type recommendation")
	}
	if typeOps[0]["action"] != "type" {
		t.Fatalf("expected type action, got %v", typeOps[0]["action"])
	}
	if typeOps[0]["value"] == "" {
		t.Fatal("expected suggested value for type action")
	}

	selectRec := recs[1]
	if requiresInput, _ := selectRec["requires_user_input"].(bool); !requiresInput {
		t.Fatal("expected select recommendation to require user input")
	}

	toggleRec := recs[2]
	toggleArgs, ok := toggleRec["args"].(map[string]interface{})
	if !ok {
		t.Fatal("expected args map for toggle recommendation")
	}
	toggleOps, ok := toggleArgs["operations"].([]map[string]interface{})
	if !ok || len(toggleOps) != 1 {
		t.Fatal("expected single operation for toggle recommendation")
	}
	if toggleOps[0]["action"] != "toggle" {
		t.Fatalf("expected toggle action, got %v", toggleOps[0]["action"])
	}
}

func TestBuildActionPlanRecommendationsHandlesNavigate(t *testing.T) {
	candidates := []map[string]interface{}{
		{"action": "navigate", "ref": "a1", "label": "/about", "priority": 58, "reason": "internal_nav_link"},
	}

	recs := buildActionPlanRecommendations(candidates, 5, "s-nav", "https://symbiogen.ai")
	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}
	args, ok := recs[0]["args"].(map[string]interface{})
	if !ok {
		t.Fatal("expected args map for navigate recommendation")
	}
	if args["session_id"] != "s-nav" {
		t.Fatalf("expected session_id=s-nav, got %v", args["session_id"])
	}
	ops, ok := args["operations"].([]map[string]interface{})
	if !ok || len(ops) != 1 {
		t.Fatal("expected single operation for navigate recommendation")
	}
	if ops[0]["type"] != "navigate" {
		t.Fatalf("expected navigate operation type, got %v", ops[0]["type"])
	}
	if ops[0]["url"] != "https://symbiogen.ai/about" {
		t.Fatalf("expected absolute url, got %v", ops[0]["url"])
	}
}

func TestFilterRowsSince(t *testing.T) {
	rows := []map[string]interface{}{
		{"ReqId": "old", "ReqTs": int64(1000)},
		{"ReqId": "new", "ReqTs": int64(5000)},
		{"ReqId": "unknown"},
	}

	filtered := filterRowsSince(rows, []string{"ReqTs"}, 3000)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 rows after filtering, got %d", len(filtered))
	}

	ids := map[string]bool{}
	for _, row := range filtered {
		ids[row["ReqId"].(string)] = true
	}
	if !ids["new"] {
		t.Fatal("expected row with ReqId=new")
	}
	if !ids["unknown"] {
		t.Fatal("expected row without timestamp to be retained")
	}
}

func TestBuildReasonChangeFeedSortsNewestFirst(t *testing.T) {
	changes := buildReasonChangeFeed(
		[]map[string]interface{}{{"Source": "backend", "Cause": "traceback", "Ts": int64(2000)}},
		[]map[string]interface{}{{"ReqId": "r1", "Url": "/api/a", "Status": 500, "ReqTs": int64(1000)}},
		[]map[string]interface{}{{"ReqId": "r2", "Url": "/api/b", "Duration": 2200, "ReqTs": int64(3000)}},
		[]map[string]interface{}{{"Source": "toast", "Message": "Failed save", "Timestamp": int64(2500)}},
		nil,
		10,
	)
	if len(changes) < 3 {
		t.Fatalf("expected multiple change events, got %d", len(changes))
	}
	firstType := changes[0]["type"]
	if firstType != "slow_api" {
		t.Fatalf("expected newest change to be slow_api, got %v", firstType)
	}
}
