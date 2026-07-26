package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

// BrowserActTool consolidates browser actions with progressive-disclosure results.
type BrowserActTool struct {
	sessions                *browser.SessionManager
	engine                  *mangle.Engine
	redactor                *security.Redactor
	specsCfg                config.SpecsConfig
	disableUnsafeJavaScript bool
}

func (t *BrowserActTool) Name() string { return "browser-act" }
func (t *BrowserActTool) Description() string {
	return `Perform browser actions -- navigate, click, type, manage sessions, wait, run JS.

Pass an operations array; they execute in sequence. Use browser-observe first to get ref IDs.

Operation types:
  Browser:  browser_launch (new_instance), browser_close (browser_id)
  Tabs:     session_create (url/browser_id/isolated), session_attach, session_focus, session_close, session_fork
  Navigate: navigate (url), history (back/forward/reload)
  Interact: click, type, select, toggle -- requires ref from browser-observe
  Forms:    fill -- batch multiple fields [{ref, value}] + optional submit
  Keyboard: key -- e.g. "Enter", "Tab", "Control+a"
  Waiting:  await_stable (idle detection), await_fact, await_conditions, wait, sleep
  Advanced: js (trusted-config only, then gated), plan (Mangle-derived action sequence)

Options: stop_on_error (default true), view (summary|compact|full).

Use browser-observe to understand the page, browser-reason if something goes wrong.`
}

func (t *BrowserActTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session",
			},
			"operations": map[string]interface{}{
				"type":        "array",
				"description": "Action operations to execute",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"type": map[string]interface{}{
							"type": "string",
							"enum": []string{"navigate", "interact", "fill", "key", "history", "sleep", "browser_launch", "browser_close", "session_create", "session_attach", "session_focus", "session_close", "session_fork", "wait", "await_stable", "await_fact", "await_conditions", "js", "plan"},
						},
					},
					"required": []string{"type"},
				},
			},
			"stop_on_error": map[string]interface{}{
				"type":        "boolean",
				"description": "Stop at first failed operation (default true)",
			},
			"view": map[string]interface{}{
				"type":        "string",
				"description": "summary|compact|full",
				"enum":        []string{"summary", "compact", "full"},
			},
			"max_items": map[string]interface{}{
				"type":        "integer",
				"description": "Max operation results returned in compact mode (default 20)",
			},
			"include_specs": map[string]interface{}{
				"type":        "boolean",
				"description": "Attach compact configured spec context after the operations (default true)",
			},
			"spec_terms": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Optional feature or requirement terms used to rank spec context",
			},
		},
		"required": []string{"operations"},
	}
}

func (t *BrowserActTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")

	rawOps, ok := args["operations"].([]interface{})
	if !ok || len(rawOps) == 0 {
		return map[string]interface{}{"success": false, "error": "operations must be a non-empty array"}, nil
	}

	stopOnError := getBoolArg(args, "stop_on_error", true)
	view := normalizeProgressiveView(getStringArg(args, "view"))
	maxItems := getIntArg(args, "max_items", defaultProgressiveMaxItems)
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}

	navTool := &NavigateURLTool{sessions: t.sessions, engine: t.engine}
	interactTool := &InteractTool{sessions: t.sessions, engine: t.engine, redactor: t.redactor}
	fillTool := &FillFormTool{sessions: t.sessions, engine: t.engine}
	keyTool := &PressKeyTool{sessions: t.sessions, engine: t.engine}
	historyTool := &BrowserHistoryTool{sessions: t.sessions, engine: t.engine}

	results := make([]map[string]interface{}, 0, len(rawOps))
	succeeded := 0
	failed := 0

	for idx, raw := range rawOps {
		op, ok := raw.(map[string]interface{})
		if !ok {
			results = append(results, map[string]interface{}{
				"index":   idx,
				"type":    "unknown",
				"success": false,
				"error":   "operation must be an object",
			})
			failed++
			if stopOnError {
				break
			}
			continue
		}

		opType := strings.ToLower(getStringFromMap(op, "type"))
		entry := map[string]interface{}{
			"index": idx,
			"type":  opType,
		}

		var (
			opResult interface{}
			err      error
		)

		switch opType {
		case "navigate":
			url := getStringFromMap(op, "url")
			waitUntil := getStringFromMap(op, "wait_until")
			opResult, err = navTool.Execute(ctx, map[string]interface{}{
				"session_id": sessionID,
				"url":        url,
				"wait_until": waitUntil,
			})
		case "interact":
			opResult, err = interactTool.Execute(ctx, map[string]interface{}{
				"session_id": sessionID,
				"ref":        getStringFromMap(op, "ref"),
				"action":     getStringFromMap(op, "action"),
				"value":      getStringFromMap(op, "value"),
				"submit":     op["submit"],
			})
		case "fill":
			opResult, err = fillTool.Execute(ctx, map[string]interface{}{
				"session_id":    sessionID,
				"fields":        op["fields"],
				"submit":        op["submit"],
				"submit_button": getStringFromMap(op, "submit_button"),
			})
		case "key":
			opResult, err = keyTool.Execute(ctx, map[string]interface{}{
				"session_id": sessionID,
				"key":        getStringFromMap(op, "key"),
				"modifiers":  op["modifiers"],
			})
		case "history":
			opResult, err = historyTool.Execute(ctx, map[string]interface{}{
				"session_id": sessionID,
				"action":     getStringFromMap(op, "action"),
			})
		case "sleep":
			ms := getIntArg(op, "duration_ms", 250)
			if ms < 0 {
				ms = 0
			}
			err = sleepWithContext(ctx, time.Duration(ms)*time.Millisecond)
			opResult = map[string]interface{}{"success": err == nil, "slept_ms": ms}

		case "session_create":
			createTool := &CreateSessionTool{sessions: t.sessions}
			opResult, err = createTool.Execute(ctx, map[string]interface{}{
				"url":        getStringFromMap(op, "url"),
				"browser_id": getStringFromMap(op, "browser_id"),
				"isolated":   getBoolArg(op, "isolated", false),
			})

		case "session_attach":
			attachTool := &AttachSessionTool{sessions: t.sessions}
			opResult, err = attachTool.Execute(ctx, map[string]interface{}{
				"target_id":  getStringFromMap(op, "target_id"),
				"browser_id": getStringFromMap(op, "browser_id"),
			})

		case "session_fork":
			forkTool := &ForkSessionTool{sessions: t.sessions}
			forkArgs := map[string]interface{}{
				"session_id": firstNonEmpty(getStringFromMap(op, "source_session_id"), sessionID),
			}
			if u := getStringFromMap(op, "url"); u != "" {
				forkArgs["url"] = u
			}
			opResult, err = forkTool.Execute(ctx, forkArgs)

		case "session_focus":
			focusTool := &FocusSessionTool{sessions: t.sessions}
			opResult, err = focusTool.Execute(ctx, map[string]interface{}{
				"session_id": firstNonEmpty(getStringFromMap(op, "session_id"), sessionID),
			})

		case "session_close":
			closeTool := &CloseSessionTool{sessions: t.sessions}
			opResult, err = closeTool.Execute(ctx, map[string]interface{}{
				"session_id": firstNonEmpty(getStringFromMap(op, "session_id"), sessionID),
			})

		case "browser_launch":
			launchTool := &LaunchBrowserTool{sessions: t.sessions}
			opResult, err = launchTool.Execute(ctx, map[string]interface{}{
				"new_instance": getBoolArg(op, "new_instance", true),
			})

		case "browser_close":
			shutdownTool := &ShutdownBrowserTool{sessions: t.sessions}
			opResult, err = shutdownTool.Execute(ctx, map[string]interface{}{
				"browser_id": getStringFromMap(op, "browser_id"),
			})

		case "wait":
			waitTool := &WaitForConditionTool{sessions: t.sessions, engine: t.engine}
			waitArgs := map[string]interface{}{
				"predicate": getStringFromMap(op, "predicate"),
			}
			if v, ok := op["match_args"]; ok {
				waitArgs["match_args"] = v
			}
			if v, ok := op["timeout_ms"]; ok {
				waitArgs["timeout_ms"] = v
			}
			opResult, err = waitTool.Execute(ctx, waitArgs)

		case "await_stable":
			stableTool := &AwaitStableStateTool{engine: t.engine}
			stableArgs := map[string]interface{}{}
			if v, ok := op["timeout_ms"]; ok {
				stableArgs["timeout_ms"] = v
			}
			opResult, err = stableTool.Execute(ctx, stableArgs)

		case "await_fact":
			awaitTool := &AwaitFactTool{engine: t.engine}
			awaitArgs := map[string]interface{}{
				"predicate": getStringFromMap(op, "predicate"),
			}
			if v, ok := op["args"]; ok {
				awaitArgs["args"] = v
			}
			if v, ok := op["timeout_ms"]; ok {
				awaitArgs["timeout_ms"] = v
			}
			opResult, err = awaitTool.Execute(ctx, awaitArgs)

		case "await_conditions":
			condTool := &AwaitConditionsTool{engine: t.engine}
			condArgs := map[string]interface{}{}
			if v, ok := op["conditions"]; ok {
				condArgs["conditions"] = v
			}
			if v, ok := op["timeout_ms"]; ok {
				condArgs["timeout_ms"] = v
			}
			opResult, err = condTool.Execute(ctx, condArgs)

		case "js":
			jsTool := &EvaluateJSTool{
				sessions:                t.sessions,
				engine:                  t.engine,
				redactor:                t.redactor,
				disableUnsafeJavaScript: t.disableUnsafeJavaScript,
			}
			jsArgs := map[string]interface{}{
				"session_id": sessionID,
				"script":     getStringFromMap(op, "script"),
			}
			if v, ok := op["timeout_ms"]; ok {
				jsArgs["timeout_ms"] = v
			}
			if v, ok := op["gate_reason"]; ok {
				jsArgs["gate_reason"] = v
			}
			if v, ok := op["approved_by_handle"]; ok {
				jsArgs["approved_by_handle"] = v
			}
			opResult, err = jsTool.Execute(ctx, jsArgs)

		case "plan":
			planTool := &ExecutePlanTool{sessions: t.sessions, engine: t.engine}
			planArgs := map[string]interface{}{
				"session_id": sessionID,
			}
			if v, ok := op["actions"]; ok {
				planArgs["actions"] = v
			}
			if v, ok := op["predicate"]; ok {
				planArgs["predicate"] = v
			}
			if v, ok := op["delay_ms"]; ok {
				planArgs["delay_ms"] = v
			}
			opResult, err = planTool.Execute(ctx, planArgs)

		default:
			err = fmt.Errorf("unknown operation type: %s", opType)
		}

		success := err == nil
		if resultMap, ok := opResult.(map[string]interface{}); ok {
			if s, exists := resultMap["success"].(bool); exists {
				success = success && s
			}
		}

		if err != nil {
			entry["error"] = err.Error()
		}
		entry["success"] = success
		entry["result"] = opResult

		results = append(results, entry)
		if success {
			succeeded++
		} else {
			failed++
			if stopOnError {
				break
			}
		}
	}

	now := time.Now().UnixMilli()
	handle := fmt.Sprintf("act:%s:%d", sessionID, now)
	emitDisclosureFacts(ctx, t.engine, sessionID, []string{handle}, "act")

	response := map[string]interface{}{
		"success":          failed == 0,
		"status":           ternaryStatus(failed == 0, "ok", "error"),
		"summary":          fmt.Sprintf("Executed %d operation(s): %d succeeded, %d failed", len(results), succeeded, failed),
		"counts":           map[string]interface{}{"total": len(results), "succeeded": succeeded, "failed": failed},
		"evidence_handles": []string{handle},
		"view":             view,
	}

	switch view {
	case "summary":
		// no per-operation payload
	case "compact":
		compact := make([]map[string]interface{}, 0, len(results))
		for _, r := range results {
			compact = append(compact, map[string]interface{}{
				"index":   r["index"],
				"type":    r["type"],
				"success": r["success"],
				"error":   r["error"],
			})
		}
		response["results"] = limitMapSlice(compact, maxItems)
		response["truncated"] = len(compact) > maxItems
	default:
		response["results"] = results
		response["truncated"] = false
	}
	if getBoolArg(args, "include_specs", true) {
		if matches := specContextForSession(t.specsCfg, t.sessions, sessionID, specTermsFromRaw(args["spec_terms"])); len(matches) > 0 {
			response["spec_context"] = matches
			response["evidence_handles"] = append(response["evidence_handles"].([]string), "act:"+sessionID+":spec_context")
		}
	}

	return response, nil
}
