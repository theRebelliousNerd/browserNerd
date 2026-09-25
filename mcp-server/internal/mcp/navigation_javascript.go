package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

// =============================================================================
// JAVASCRIPT / FORM TOOLS
// =============================================================================

// EvaluateJSTool executes arbitrary JavaScript in the page context.
type EvaluateJSTool struct {
	sessions                *browser.SessionManager
	engine                  *mangle.Engine
	redactor                *security.Redactor
	disableUnsafeJavaScript bool
}

func (t *EvaluateJSTool) Name() string { return "evaluate-js" }
func (t *EvaluateJSTool) Description() string {
	return `Execute JavaScript in the browser context for advanced operations.

SECURITY DEFAULT:
Disabled unless a trusted operator sets security.allow_unsafe_javascript=true.
The progressive disclosure gate below still applies after it is enabled.

PROGRESSIVE DISCLOSURE GATE:
This tool is intentionally gated so agents prefer structured tools first.
Use one of:
- gate_reason="explicit_user_intent" (manual override; requires approved_by_handle from browser-reason)
- gate_reason="low_confidence" (after browser-reason signals uncertainty)
- gate_reason="contradiction_detected" (after browser-reason detects conflicts)
- gate_reason="no_matching_tool" (structured tools are insufficient)
- approved_by_handle="<evidence handle from browser-reason>"

WHEN TO USE (escape hatch for complex scenarios):
- Extracting data not available via other tools
- Complex DOM manipulations
- Triggering JavaScript events
- Reading application state (Redux, etc.)
- Workarounds when standard tools don't fit

SCRIPT FORMATS:
- Expression: "document.title"
- IIFE: "() => { return document.querySelectorAll('li').length; }"
- With element: "el => el.getBoundingClientRect()" (pass element_ref)

EXAMPLE OUTPUT (simple expression):
Input: script="document.title"
{"success": true, "result": "GitHub - Dashboard"}

EXAMPLE OUTPUT (IIFE returning object):
Input: script="() => ({count: document.querySelectorAll('a').length, ready: document.readyState})"
{"success": true, "result": {"count": 42, "ready": "complete"}}

EXAMPLE OUTPUT (with element_ref):
Input: script="el => el.textContent", element_ref="btn-0"
{"success": true, "result": "Submit Form"}

EXAMPLE OUTPUT (error):
{"success": false, "error": "ReferenceError: foo is not defined", "error_type": "script"}

PREFER THESE TOOLS INSTEAD when possible:
- get-interactive-elements for finding elements
- interact for clicking/typing
- fill-form for forms
- get-page-state for basic info

evaluate-js is powerful but harder to debug. Use sparingly.`
}
func (t *EvaluateJSTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session",
			},
			"script": map[string]interface{}{
				"type":        "string",
				"description": "JavaScript code to execute. Can be an expression or an IIFE: () => { return value; }",
			},
			"element_ref": map[string]interface{}{
				"type":        "string",
				"description": "Optional element ref - script receives the element as first argument",
			},
			"timeout_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Max execution time in milliseconds (default 5000, max 60000)",
				"default":     5000,
			},
			"gate_reason": map[string]interface{}{
				"type":        "string",
				"description": "Progressive disclosure gate reason (explicit_user_intent requires approved_by_handle)",
				"enum":        []string{"explicit_user_intent", "low_confidence", "contradiction_detected", "no_matching_tool"},
			},
			"approved_by_handle": map[string]interface{}{
				"type":        "string",
				"description": "Evidence handle from browser-reason that authorizes JS fallback",
			},
			"result_mode": map[string]interface{}{
				"type":        "string",
				"description": "Result shape: scalar|compact_json|raw (default compact_json)",
				"enum":        []string{"scalar", "compact_json", "raw"},
			},
			"max_result_bytes": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum bytes allowed in the result payload before truncation (default 8192)",
			},
		},
		"required": []string{"session_id", "script"},
	}
}
func (t *EvaluateJSTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	script := getStringArg(args, "script")
	elementRef := getStringArg(args, "element_ref")
	timeoutMs := getIntArg(args, "timeout_ms", 5000)
	gateReason := getStringArg(args, "gate_reason")
	approvedHandle := getStringArg(args, "approved_by_handle")
	resultMode := getStringArg(args, "result_mode")
	if resultMode == "" {
		resultMode = "compact_json"
	}
	maxResultBytes := getIntArg(args, "max_result_bytes", 8192)
	if maxResultBytes < 256 {
		maxResultBytes = 256
	}
	if maxResultBytes > 262144 {
		maxResultBytes = 262144
	}

	// Clamp timeout to reasonable range
	if timeoutMs < 100 {
		timeoutMs = 100
	}
	if timeoutMs > 60000 {
		timeoutMs = 60000
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond

	if sessionID == "" || script == "" {
		return map[string]interface{}{"success": false, "error": "session_id and script are required", "error_type": "validation"}, nil
	}
	if t.disableUnsafeJavaScript {
		return map[string]interface{}{
			"success":     false,
			"disabled":    true,
			"error":       "evaluate-js is disabled by default because arbitrary page scripts can read credentials",
			"error_type":  "security",
			"remediation": "use structured tools or explicitly enable security.allow_unsafe_javascript in a trusted config",
		}, nil
	}

	if ok, reason := t.evaluateJSGateOpen(sessionID, gateReason, approvedHandle, t.Name()); !ok {
		return map[string]interface{}{
			"success":            false,
			"gated":              true,
			"error":              reason,
			"recommended_tool":   "browser-reason",
			"required_reasons":   []string{"explicit_user_intent", "low_confidence", "contradiction_detected", "no_matching_tool"},
			"approved_by_handle": approvedHandle,
		}, nil
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("session not found: %s", sessionID), "error_type": "session"}, nil
	}

	var result interface{}
	var err error
	var errorType string

	// Get element registry for fingerprint-based lookup
	registry := t.sessions.Registry(sessionID)
	scriptToEval := normalizeEvalScriptForRod(script, elementRef != "")

	if elementRef != "" {
		// Execute on specific element
		element, findErr := findElementByRefWithRegistry(page, elementRef, registry)
		if findErr != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("element not found: %s", elementRef), "error_type": "element"}, nil
		}
		evalResult, evalErr := element.Timeout(timeout).Eval(scriptToEval)
		if evalErr != nil {
			err = evalErr
			errorType = classifyJSError(evalErr)
		} else {
			result = evalResult.Value.Val()
		}
	} else {
		// Execute on page with timeout
		evalResult, evalErr := page.Timeout(timeout).Eval(scriptToEval)
		if evalErr != nil {
			err = evalErr
			errorType = classifyJSError(evalErr)
		} else {
			result = evalResult.Value.Val()
		}
	}

	if err != nil {
		errMsg := formatJSError(err)
		return map[string]interface{}{
			"success":    false,
			"error":      errMsg,
			"error_type": errorType,
			"timeout_ms": timeoutMs,
		}, nil
	}

	redactor := t.redactor
	if redactor == nil {
		redactor = security.NewRedactor(nil)
	}
	finalResult, truncated, resultBytes := shapeEvaluateJSResult(redactor.Sanitize(result), resultMode, maxResultBytes)

	// Emit Mangle fact
	now := time.Now()
	if t.engine != nil {
		_ = t.engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "js_evaluated",
			Args:      []interface{}{sessionID, len(script), now.UnixMilli()},
			Timestamp: now,
		}})
	}

	return map[string]interface{}{
		"success":      true,
		"result":       finalResult,
		"result_mode":  resultMode,
		"truncated":    truncated,
		"result_bytes": resultBytes,
	}, nil
}

func (t *EvaluateJSTool) evaluateJSGateOpen(sessionID, gateReason, approvedHandle, toolName string) (bool, string) {
	validReasons := map[string]bool{
		"explicit_user_intent":   true,
		"low_confidence":         true,
		"contradiction_detected": true,
		"no_matching_tool":       true,
	}

	if gateReason != "" && !validReasons[gateReason] {
		return false, fmt.Sprintf("invalid gate_reason: %s", gateReason)
	}

	if gateReason == "explicit_user_intent" && approvedHandle == "" {
		return false, `gate_reason "explicit_user_intent" requires approved_by_handle from browser-reason`
	}

	if approvedHandle != "" {
		if hasRecentGateFact(t.engine, "disclosure_handle", sessionID, approvedHandle, jsGateTTL) {
			return true, ""
		}
		return false, fmt.Sprintf("approved_by_handle not found or expired: %s", approvedHandle)
	}

	if gateReason == "" {
		if strings.TrimSpace(toolName) == "" {
			toolName = "evaluate-js"
		}
		return false, fmt.Sprintf("%s is gated; provide gate_reason or approved_by_handle", toolName)
	}

	if hasRecentGateFact(t.engine, "js_gate_open", sessionID, gateReason, jsGateTTL) {
		return true, ""
	}

	return false, fmt.Sprintf("gate_reason %q is not currently authorized; call browser-reason first", gateReason)
}

func normalizeEvalScriptForRod(script string, withElement bool) string {
	trimmed := strings.TrimSpace(script)
	if trimmed == "" {
		return trimmed
	}
	if isFunctionLikeJavaScript(trimmed) {
		return trimmed
	}
	if withElement {
		return fmt.Sprintf("el => (%s)", trimmed)
	}
	return fmt.Sprintf("() => (%s)", trimmed)
}

func isFunctionLikeJavaScript(script string) bool {
	trimmed := strings.TrimSpace(strings.ToLower(script))
	if strings.HasPrefix(trimmed, "function") || strings.HasPrefix(trimmed, "async function") {
		return true
	}
	return strings.Contains(script, "=>")
}

func shapeEvaluateJSResult(result interface{}, mode string, maxBytes int) (interface{}, bool, int) {
	payload := fmt.Sprintf("%v", result)
	switch mode {
	case "scalar":
		switch v := result.(type) {
		case string:
			if len(v) > maxBytes {
				return v[:maxBytes], true, len(v)
			}
			return v, false, len(v)
		case float64, bool, int, int64, nil:
			return result, false, len(payload)
		default:
			return map[string]interface{}{
				"type": "non_scalar",
				"note": "result hidden in scalar mode; use compact_json or raw",
			}, true, len(payload)
		}
	case "raw":
		if len(payload) <= maxBytes {
			return result, false, len(payload)
		}
		return map[string]interface{}{
			"type":    fmt.Sprintf("%T", result),
			"preview": payload[:maxBytes],
		}, true, len(payload)
	default: // compact_json
		if len(payload) <= maxBytes {
			return result, false, len(payload)
		}
		return map[string]interface{}{
			"type":       fmt.Sprintf("%T", result),
			"preview":    payload[:maxBytes],
			"truncation": "result exceeded max_result_bytes",
		}, true, len(payload)
	}
}
