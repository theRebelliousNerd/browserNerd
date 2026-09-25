package mcp

import (
	"context"
	"fmt"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// =============================================================================
// EXECUTE PLAN - Mangle-driven automation
// =============================================================================

// ExecutePlanTool executes a sequence of actions derived from Mangle rules.
// Claude defines intent via Mangle rules; this tool executes the resulting plan.
type ExecutePlanTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *ExecutePlanTool) Name() string { return "execute-plan" }
func (t *ExecutePlanTool) Description() string {
	return `Execute multiple actions in a single tool call.

TOKEN COST: Very Low (1 call replaces N individual calls)

PREFER THIS OVER: Multiple interact() calls for multi-step workflows.

SUPPORTED ACTIONS:
- action("click", Ref)
- action("type", Ref, Value)
- action("select", Ref, Value)
- action("navigate", URL)
- action("press", Key)
- action("wait", Milliseconds)
- action("screenshot", Name)

EXAMPLE (login form - 1 call instead of 3):
execute-plan(actions: [
  {type: "type", ref: "email", value: "user@example.com"},
  {type: "type", ref: "password", value: "secret"},
  {type: "click", ref: "login-btn"}
])

WHEN TO USE:
- Form fills (multiple inputs + submit)
- Navigation sequences
- Any workflow with 2+ actions

Returns: {succeeded, failed, results[]}`
}
func (t *ExecutePlanTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session",
			},
			"actions": map[string]interface{}{
				"type":        "array",
				"description": "Optional: Direct action list instead of Mangle-derived. Each action: {type: 'click'|'type'|'select'|'navigate'|'press'|'wait'|'screenshot', ref: '...', value: '...'}",
				"items": map[string]interface{}{
					"type": "object",
				},
			},
			"predicate": map[string]interface{}{
				"type":        "string",
				"description": "Mangle predicate to query for action plan (alternative to 'actions' array)",
			},
			"stop_on_error": map[string]interface{}{
				"type":        "boolean",
				"description": "Stop execution on first error (default: true)",
			},
			"delay_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Delay between actions in milliseconds (default: 100)",
			},
		},
		"required": []string{"session_id"},
	}
}
func (t *ExecutePlanTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	predicate := getStringArg(args, "predicate")
	stopOnError := getBoolArg(args, "stop_on_error", true)
	delayMs := getIntArg(args, "delay_ms", 100)
	if predicate == "" {
		predicate = "action"
	}

	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("session not found: %s", sessionID)}, nil
	}
	page = page.Context(ctx)
	registry := t.sessions.Registry(sessionID)

	// Get actions either from args or from Mangle-derived facts
	var actions []map[string]interface{}

	if actionsArg, ok := args["actions"]; ok {
		// Direct actions provided
		if actionsList, ok := actionsArg.([]interface{}); ok {
			for _, a := range actionsList {
				if actionMap, ok := a.(map[string]interface{}); ok {
					actions = append(actions, actionMap)
				}
			}
		}
	} else {
		// Query Mangle for derived action facts.
		actionFacts, err := t.engine.Evaluate(ctx, predicate)
		if err != nil {
			return map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("failed to evaluate predicate %q: %v", predicate, err),
			}, nil
		}

		// Backward-compatibility fallback: "action" may be pushed as raw facts directly.
		if predicate == "action" && len(actionFacts) == 0 {
			actionFacts = t.engine.FactsByPredicate("action")
		}

		for _, f := range actionFacts {
			if len(f.Args) >= 1 {
				actionType := fmt.Sprintf("%v", f.Args[0])
				action := map[string]interface{}{"type": actionType}
				if len(f.Args) >= 2 {
					action["ref"] = fmt.Sprintf("%v", f.Args[1])
				}
				if len(f.Args) >= 3 {
					action["value"] = fmt.Sprintf("%v", f.Args[2])
				}
				actions = append(actions, action)
			}
		}
	}

	if len(actions) == 0 {
		return map[string]interface{}{
			"success":       true,
			"message":       "no actions to execute",
			"total_actions": 0,
			"executed":      0,
			"succeeded":     0,
			"failed":        0,
			"results":       []map[string]interface{}{},
		}, nil
	}

	// Execute each action
	results := make([]map[string]interface{}, 0)
	successCount := 0
	errorCount := 0

	for i, action := range actions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		actionType := getStringFromMap(action, "type")
		ref := getStringFromMap(action, "ref")
		value := getStringFromMap(action, "value")

		result := map[string]interface{}{
			"index": i,
			"type":  actionType,
		}

		var actionErr error

		switch actionType {
		case "click":
			element, err := findElementByRefWithRegistry(page, ref, registry)
			if err != nil {
				actionErr = fmt.Errorf("element not found: %s", ref)
			} else {
				actionErr = clickElement(ctx, page, element, 1)
			}
			result["ref"] = ref

		case "type":
			element, err := findElementByRefWithRegistry(page, ref, registry)
			sensitive := true
			if err != nil {
				actionErr = fmt.Errorf("element not found: %s", ref)
			} else {
				sensitive = fieldIsSensitive(ctx, element, ref, nil)
				_, actionErr = setFieldValue(ctx, page, element, value, sensitive)
			}
			result["ref"] = ref
			result["value"] = value
			if sensitive {
				result["value"] = security.Redacted
			}

		case "select":
			element, err := findElementByRefWithRegistry(page, ref, registry)
			if err != nil {
				actionErr = fmt.Errorf("element not found: %s", ref)
			} else {
				_, actionErr = selectValue(ctx, page, element, value)
			}
			result["ref"] = ref
			result["value"] = value

		case "navigate":
			url := ref
			if url == "" {
				url = value
			}
			// Skip navigation if already on same URL to avoid WaitLoad hang
			currentInfo, _ := page.Info()
			if currentInfo != nil && currentInfo.URL == url {
				result["url"] = url
				result["skipped"] = "already on this URL"
			} else {
				bounded := page.Timeout(defaultNavigateTimeout)
				actionErr = bounded.Navigate(url)
				if actionErr == nil {
					actionErr = bounded.WaitLoad()
				}
				result["url"] = url
			}

		case "press":
			key := ref
			if key == "" {
				key = value
			}
			chord, err := parseKeyChord(key, nil)
			if err != nil {
				actionErr = err
			} else {
				actionErr = dispatchKeyChord(page.Timeout(5*time.Second), chord)
			}
			result["key"] = key

		case "wait":
			ms := 1000
			if ref != "" {
				fmt.Sscanf(ref, "%d", &ms)
			} else if value != "" {
				fmt.Sscanf(value, "%d", &ms)
			}
			if err := sleepWithContext(ctx, time.Duration(ms)*time.Millisecond); err != nil {
				return nil, err
			}
			result["ms"] = ms

		case "screenshot":
			name := ref
			if name == "" {
				name = fmt.Sprintf("plan-screenshot-%d", i)
			}
			imgData, err := page.Screenshot(false, nil)
			if err != nil {
				actionErr = err
			} else {
				result["name"] = name
				result["size_bytes"] = len(imgData)
			}

		default:
			actionErr = fmt.Errorf("unknown action type: %s", actionType)
		}

		if actionErr != nil {
			result["success"] = false
			result["error"] = actionErr.Error()
			errorCount++

			if stopOnError {
				results = append(results, result)
				break
			}
		} else {
			result["success"] = true
			successCount++
		}

		results = append(results, result)

		// Delay between actions
		if delayMs > 0 && i < len(actions)-1 {
			if err := sleepWithContext(ctx, time.Duration(delayMs)*time.Millisecond); err != nil {
				return nil, err
			}
		}
	}

	// Emit summary fact
	now := time.Now()
	_ = t.engine.AddFacts(ctx, []mangle.Fact{{
		Predicate: "plan_executed",
		Args:      []interface{}{sessionID, len(actions), successCount, errorCount, now.UnixMilli()},
		Timestamp: now,
	}})

	return map[string]interface{}{
		"success":       errorCount == 0,
		"total_actions": len(actions),
		"executed":      len(results),
		"succeeded":     successCount,
		"failed":        errorCount,
		"results":       results,
	}, nil
}

// =============================================================================
// WAIT FOR CONDITION - Mangle-powered waiting
// =============================================================================

// WaitForConditionTool waits until a Mangle condition is satisfied.
type WaitForConditionTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *WaitForConditionTool) Name() string { return "wait-for-condition" }
func (t *WaitForConditionTool) Description() string {
	return `Wait until a Mangle-defined condition is true, or until timeout.

EXAMPLES:
- Wait for navigation: predicate="navigation_event", match_args=["session-id", "/dashboard"]
- Wait for element: predicate="interactive", match_args=["submit-btn"]
- Wait for error: predicate="console_event", match_args=["error"]
- Wait for custom rule: Submit rule like 'ready() :- navigation_event(_, "/dashboard", _), dom_text(_, "Welcome").' then wait for predicate="ready"

This is token-efficient because you define the condition once, and the tool polls for you.`
}
func (t *WaitForConditionTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"predicate": map[string]interface{}{
				"type":        "string",
				"description": "Mangle predicate to wait for",
			},
			"match_args": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Optional: Arguments that must match (use '_' for wildcard)",
			},
			"timeout_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum wait time in milliseconds (default: 10000)",
			},
			"poll_interval_ms": map[string]interface{}{
				"type":        "integer",
				"description": "How often to check condition (default: 200)",
			},
		},
		"required": []string{"predicate"},
	}
}
func (t *WaitForConditionTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	predicate := getStringArg(args, "predicate")
	timeoutMs := getIntArg(args, "timeout_ms", 10000)
	pollIntervalMs := getIntArg(args, "poll_interval_ms", 200)

	if predicate == "" {
		return map[string]interface{}{"success": false, "error": "predicate is required"}, nil
	}

	// Parse match_args
	var matchArgs []string
	if matchArgsRaw, ok := args["match_args"]; ok {
		if argsList, ok := matchArgsRaw.([]interface{}); ok {
			for _, a := range argsList {
				matchArgs = append(matchArgs, fmt.Sprintf("%v", a))
			}
		}
	}

	startTime := time.Now()
	timeout := time.Duration(timeoutMs) * time.Millisecond
	pollInterval := time.Duration(pollIntervalMs) * time.Millisecond

	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}

	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		// Check if condition is satisfied
		facts, source := t.resolvePredicateFacts(ctx, predicate)

		for _, f := range facts {
			if len(matchArgs) == 0 {
				// No specific args required, any fact satisfies
				return map[string]interface{}{
					"success":   true,
					"matched":   true,
					"fact":      f,
					"source":    source,
					"waited_ms": time.Since(startTime).Milliseconds(),
				}, nil
			}

			// Check if fact args match
			matches := true
			for i, expected := range matchArgs {
				if expected == "_" {
					continue // Wildcard
				}
				if i >= len(f.Args) || fmt.Sprintf("%v", f.Args[i]) != expected {
					matches = false
					break
				}
			}

			if matches {
				return map[string]interface{}{
					"success":   true,
					"matched":   true,
					"fact":      f,
					"source":    source,
					"waited_ms": time.Since(startTime).Milliseconds(),
				}, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeoutTimer.C:
			return map[string]interface{}{
				"success":   false,
				"matched":   false,
				"error":     "timeout waiting for condition",
				"waited_ms": time.Since(startTime).Milliseconds(),
			}, nil
		case <-ticker.C:
		}
	}
}

func (t *WaitForConditionTool) resolvePredicateFacts(ctx context.Context, predicate string) ([]mangle.Fact, string) {
	facts := t.engine.FactsByPredicate(predicate)
	if len(facts) > 0 {
		return facts, "fact_buffer"
	}

	// Derived predicates are not always present in the temporal buffer.
	// Fall back to program evaluation so submit-rule + wait-for-condition works.
	derived, err := t.engine.Evaluate(ctx, predicate)
	if err != nil || len(derived) == 0 {
		return facts, "fact_buffer"
	}
	return derived, "derived_eval"
}

// AwaitStableStateTool blocks until the page is considered "stable" (network idle + DOM settled).
type AwaitStableStateTool struct {
	engine *mangle.Engine
}

func (t *AwaitStableStateTool) Name() string { return "await-stable-state" }
func (t *AwaitStableStateTool) Description() string {
	return `Wait for page to settle before extraction/screenshot.

TOKEN COST: Low (single call, blocks until ready)

WAITS FOR:
- Network idle: No requests in last 500ms
- DOM idle: No mutations in last 200ms

WHEN TO USE:
- After navigation before extracting data
- Before screenshot to avoid loading spinners
- After complex interactions (form submits, filters)

PREFER navigate-url(wait_until: "networkidle") when navigating.
Use this tool after interactions that don't trigger navigation.

Returns: {status: "stable"|"timeout", duration_ms}`
}
func (t *AwaitStableStateTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"timeout_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Max wait time (default 10000ms)",
			},
			"network_idle_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Required network idle window (default 500ms)",
			},
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Only this tab's requests and DOM updates count",
			},
		},
	}
}
func (t *AwaitStableStateTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	timeout := time.Duration(getIntArg(args, "timeout_ms", 10000)) * time.Millisecond
	netIdle := time.Duration(getIntArg(args, "network_idle_ms", 500)) * time.Millisecond
	domIdle := 200 * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	if netIdle <= 0 {
		netIdle = 500 * time.Millisecond
	}

	// Another tab that polls must not keep this one from settling: with a
	// session, only its own requests and DOM updates count.
	sessionID := getStringArg(args, "session_id")
	inSession := func(facts []mangle.Fact) int {
		if sessionID == "" {
			return len(facts)
		}
		n := 0
		for _, f := range facts {
			if len(f.Args) > 0 && fmt.Sprint(f.Args[0]) == sessionID {
				n++
			}
		}
		return n
	}

	start := time.Now()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeoutTimer.C:
			return map[string]interface{}{
				"status":      "timeout",
				"duration_ms": time.Since(start).Milliseconds(),
				"warning":     fmt.Sprintf("the page never went quiet (no request for %s and no DOM change for %s) within %s; it may poll", netIdle, domIdle, timeout),
			}, nil
		case <-ticker.C:
			// Check network idle
			// We query for ANY request in the last X ms. If count == 0, we are idle.
			netWindow := time.Now().Add(-netIdle)
			recentReqs := t.engine.QueryTemporal("net_request", netWindow, time.Now())

			// Check DOM idle
			domWindow := time.Now().Add(-domIdle)
			recentDom := t.engine.QueryTemporal("dom_updated", domWindow, time.Now())

			if inSession(recentReqs) == 0 && inSession(recentDom) == 0 {
				return map[string]interface{}{
					"status":      "stable",
					"duration_ms": time.Since(start).Milliseconds(),
				}, nil
			}
		}
	}
}
