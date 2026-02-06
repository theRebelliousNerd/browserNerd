package mcp

import (
	"context"
	"fmt"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
)

// =============================================================================
// JAVASCRIPT / FORM TOOLS
// =============================================================================

// EvaluateJSTool executes arbitrary JavaScript in the page context.
type EvaluateJSTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *EvaluateJSTool) Name() string { return "evaluate-js" }
func (t *EvaluateJSTool) Description() string {
	return `Execute JavaScript in the browser context for advanced operations.

PROGRESSIVE DISCLOSURE GATE:
This tool is intentionally gated so agents prefer structured tools first.
Use one of:
- gate_reason="explicit_user_intent" (manual override)
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
				"description": "Progressive disclosure gate reason",
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

	if ok, reason := t.evaluateJSGateOpen(sessionID, gateReason, approvedHandle); !ok {
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

	if elementRef != "" {
		// Execute on specific element
		element, findErr := findElementByRefWithRegistry(page, elementRef, registry)
		if findErr != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("element not found: %s", elementRef), "error_type": "element"}, nil
		}
		evalResult, evalErr := element.Timeout(timeout).Eval(script)
		if evalErr != nil {
			err = evalErr
			errorType = classifyJSError(evalErr)
		} else {
			result = evalResult.Value.Val()
		}
	} else {
		// Execute on page with timeout
		evalResult, evalErr := page.Timeout(timeout).Eval(script)
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

	finalResult, truncated, resultBytes := shapeEvaluateJSResult(result, resultMode, maxResultBytes)

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

func (t *EvaluateJSTool) evaluateJSGateOpen(sessionID, gateReason, approvedHandle string) (bool, string) {
	validReasons := map[string]bool{
		"explicit_user_intent":   true,
		"low_confidence":         true,
		"contradiction_detected": true,
		"no_matching_tool":       true,
	}

	if approvedHandle == "" && gateReason == "" {
		return false, "evaluate-js is gated; provide gate_reason or approved_by_handle"
	}

	if approvedHandle != "" {
		if hasRecentGateFact(t.engine, "disclosure_handle", sessionID, approvedHandle, jsGateTTL) {
			return true, ""
		}
		return false, fmt.Sprintf("approved_by_handle not found or expired: %s", approvedHandle)
	}

	if !validReasons[gateReason] {
		return false, fmt.Sprintf("invalid gate_reason: %s", gateReason)
	}

	if gateReason == "explicit_user_intent" {
		return true, ""
	}

	if hasRecentGateFact(t.engine, "js_gate_open", sessionID, gateReason, jsGateTTL) {
		return true, ""
	}

	return false, fmt.Sprintf("gate_reason %q is not currently authorized; call browser-reason first", gateReason)
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

// FillFormTool fills multiple form fields in a single call.
type FillFormTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *FillFormTool) Name() string { return "fill-form" }
func (t *FillFormTool) Description() string {
	return `Fill multiple form fields in a single call - much more efficient than individual interact() calls.

USE THIS WHEN:
- Filling login forms, signup forms, or any multi-field form
- Need to submit after filling

TOKEN COST: Low (batches multiple field operations)

EXAMPLE CALL:
{
  "session_id": "ABC123",
  "fields": [
    {"ref": "input-0", "value": "user@example.com"},
    {"ref": "input-1", "value": "secretpassword"}
  ],
  "submit_button": "btn-2"
}

EXAMPLE OUTPUT:
{
  "success": true,
  "filled": 2,
  "submitted": true,
  "results": [
    {"ref": "input-0", "success": true},
    {"ref": "input-1", "success": true}
  ]
}

SUBMIT OPTIONS:
- submit: true - Press Enter after last field
- submit_button: "btn-ref" - Click specific button after filling

Fields identified by: ref (from get-interactive-elements), element name, or id.`
}
func (t *FillFormTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session",
			},
			"fields": map[string]interface{}{
				"type":        "array",
				"description": "Array of field definitions: [{ref: 'field-id', value: 'text'}, ...]",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"ref": map[string]interface{}{
							"type":        "string",
							"description": "Element ref, name, or id",
						},
						"value": map[string]interface{}{
							"type":        "string",
							"description": "Value to enter",
						},
					},
					"required": []string{"ref", "value"},
				},
			},
			"submit": map[string]interface{}{
				"type":        "boolean",
				"description": "Press Enter after filling the last field to submit the form",
			},
			"submit_button": map[string]interface{}{
				"type":        "string",
				"description": "Click this button ref after filling fields (alternative to submit: true)",
			},
		},
		"required": []string{"session_id", "fields"},
	}
}
func (t *FillFormTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	submit := getBoolArg(args, "submit", false)
	submitButton := getStringArg(args, "submit_button")

	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("session not found: %s", sessionID)}, nil
	}

	// Get element registry for fingerprint-based lookup
	registry := t.sessions.Registry(sessionID)

	// Parse fields array
	fieldsArg, ok := args["fields"]
	if !ok {
		return map[string]interface{}{"success": false, "error": "fields array is required"}, nil
	}

	fieldsList, ok := fieldsArg.([]interface{})
	if !ok {
		return map[string]interface{}{"success": false, "error": "fields must be an array"}, nil
	}

	filledFields := make([]map[string]interface{}, 0)
	var lastElement *rod.Element

	for i, fieldItem := range fieldsList {
		fieldMap, ok := fieldItem.(map[string]interface{})
		if !ok {
			continue
		}

		ref := getStringFromMap(fieldMap, "ref")
		value := getStringFromMap(fieldMap, "value")

		if ref == "" {
			continue
		}

		element, err := findElementByRefWithRegistry(page, ref, registry)
		if err != nil {
			filledFields = append(filledFields, map[string]interface{}{
				"ref":   ref,
				"error": err.Error(),
			})
			continue
		}

		// Clear and type using Rod's native Input
		if err := element.SelectAllText(); err == nil {
			_ = element.Input("")
		}
		if err := element.Input(value); err != nil {
			filledFields = append(filledFields, map[string]interface{}{
				"ref":   ref,
				"error": fmt.Sprintf("input failed: %v", err),
			})
			continue
		}

		lastElement = element
		filledFields = append(filledFields, map[string]interface{}{
			"ref":     ref,
			"success": true,
			"index":   i,
		})

		// Emit fact for each field
		now := time.Now()
		_ = t.engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "form_field_filled",
			Args:      []interface{}{sessionID, ref, now.UnixMilli()},
			Timestamp: now,
		}})
	}

	// Handle submission
	if submit || submitButton != "" {
		// Capture URL before submit for login success detection
		// This enables universal login_succeeded rule to work on any site
		preSubmitURL := ""
		if info, err := page.Info(); err == nil && info != nil {
			preSubmitURL = info.URL
		}
		preSubmitTime := time.Now()
		_ = t.engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "url_before_submit",
			Args:      []interface{}{sessionID, preSubmitURL, preSubmitTime.UnixMilli()},
			Timestamp: preSubmitTime,
		}})
	}

	if submitButton != "" {
		// Click submit button
		button, err := findElementByRefWithRegistry(page, submitButton, registry)
		if err != nil {
			return map[string]interface{}{
				"success":       false,
				"error":         fmt.Sprintf("submit button not found: %s (%v)", submitButton, err),
				"filled_fields": filledFields,
			}, nil
		}
		if err := button.Click("left", 1); err != nil {
			return map[string]interface{}{
				"success":       false,
				"error":         fmt.Sprintf("submit click failed: %v", err),
				"filled_fields": filledFields,
			}, nil
		}
	} else if submit && lastElement != nil {
		// Press Enter on last field
		if err := page.Keyboard.Press(input.Enter); err != nil {
			return map[string]interface{}{
				"success":       false,
				"error":         fmt.Sprintf("submit (Enter) failed: %v", err),
				"filled_fields": filledFields,
			}, nil
		}
	}

	// Emit form submission fact
	if submit || submitButton != "" {
		now := time.Now()
		_ = t.engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "form_submitted",
			Args:      []interface{}{sessionID, len(filledFields), now.UnixMilli()},
			Timestamp: now,
		}})
	}

	return map[string]interface{}{
		"success":       true,
		"filled_count":  len(filledFields),
		"filled_fields": filledFields,
		"submitted":     submit || submitButton != "",
	}, nil
}
