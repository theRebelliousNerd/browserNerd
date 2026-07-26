package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"

	"gopkg.in/yaml.v3"
)

// BrowserTestTool is the progressive, script-free test authoring and execution
// surface. Fixtures use browser-act operations and Mangle assertions.
type BrowserTestTool struct {
	sessions                *browser.SessionManager
	engine                  *mangle.Engine
	disableUnsafeJavaScript bool
}

func (t *BrowserTestTool) Name() string { return "browser-test" }

func (t *BrowserTestTool) Description() string {
	return `Create, inspect, or run declarative browser tests without a Python harness.

Fixtures use the same operations accepted by browser-act plus bounded Mangle
assertions. Create can capture privacy-safe actions from a session or normalize
an inline test. Run replays operations, evaluates fresh facts by default, and
returns compact causal diagnosis on failure.

Credential fields use value_env, which is resolved only while running and is
not written back into the portable fixture.

Operations:
  create  capture a session or normalize test/test_yaml
  inspect validate and summarize test/test_yaml
  run     replay and assert test/test_yaml

The returned test_yaml is a portable artifact; save it in the target repo with
the coding agent's normal file workflow.`
}

func (t *BrowserTestTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type": "string",
				"enum": []string{"create", "inspect", "run"},
			},
			"session_id": map[string]interface{}{"type": "string"},
			"name":       map[string]interface{}{"type": "string"},
			"test": map[string]interface{}{
				"type":        "object",
				"description": "Declarative fixture with browser-act operations and Mangle assertions",
			},
			"test_yaml": map[string]interface{}{"type": "string"},
			"include_assertions": map[string]interface{}{
				"type":        "boolean",
				"description": "For captured tests, add safe baseline assertions (default true)",
			},
			"stop_on_error": map[string]interface{}{"type": "boolean"},
			"diagnose_on_failure": map[string]interface{}{
				"type":        "boolean",
				"description": "For run, attach causal facts on failure (default true)",
			},
			"view": map[string]interface{}{
				"type": "string",
				"enum": []string{"summary", "compact", "full"},
			},
		},
	}
}

func (t *BrowserTestTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	operation := strings.ToLower(getStringArg(args, "operation"))
	if operation == "" {
		operation = "run"
	}
	switch operation {
	case "create":
		return t.create(ctx, args)
	case "inspect":
		return t.inspect(args)
	case "run":
		return t.run(ctx, args)
	default:
		return map[string]interface{}{"success": false, "error": "operation must be create, inspect, or run"}, nil
	}
}

func (t *BrowserTestTool) create(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if args["test"] == nil && getStringArg(args, "test_yaml") == "" {
		generated, err := (&GenerateTestTool{engine: t.engine}).Execute(ctx, args)
		if err != nil {
			return nil, err
		}
		result := generated.(map[string]interface{})
		result["success"] = true
		result["operation"] = "create"
		result["summary"] = fmt.Sprintf("Created declarative fixture with %v browser-act operation(s)", result["actions_count"])
		return result, nil
	}
	spec, err := parseTestSpec(args)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	encoded, err := yaml.Marshal(spec)
	if err != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("encode test: %v", err)}, nil
	}
	return map[string]interface{}{
		"success":         true,
		"operation":       "create",
		"summary":         fmt.Sprintf("Normalized %d operation(s) and %d assertion(s)", len(spec.Actions), len(spec.Assertions)),
		"test":            spec,
		"test_yaml":       string(encoded),
		"action_count":    len(spec.Actions),
		"assertion_count": len(spec.Assertions),
	}, nil
}

func (t *BrowserTestTool) inspect(args map[string]interface{}) (interface{}, error) {
	spec, err := parseTestSpec(args)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	return map[string]interface{}{
		"success":         true,
		"operation":       "inspect",
		"name":            spec.Name,
		"session_id":      spec.SessionID,
		"action_count":    len(spec.Actions),
		"assertion_count": len(spec.Assertions),
		"actions":         spec.Actions,
		"assertions":      spec.Assertions,
	}, nil
}

func (t *BrowserTestTool) run(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	result, err := (&RunTestTool{
		sessions:                t.sessions,
		engine:                  t.engine,
		disableUnsafeJavaScript: t.disableUnsafeJavaScript,
	}).Execute(ctx, args)
	if err != nil {
		return nil, err
	}
	resultMap := result.(map[string]interface{})
	resultMap["operation"] = "run"
	passed, _ := resultMap["passed"].(bool)
	resultMap["success"] = passed
	resultMap["status"] = ternaryStatus(passed, "passed", "failed")
	resultMap["summary"] = fmt.Sprintf("Declarative test %s", resultMap["status"])
	sessionID := getStringArg(args, "session_id")
	handle := fmt.Sprintf("test:%s:%d", sessionID, time.Now().UnixMilli())
	resultMap["evidence_handles"] = []string{handle}
	emitDisclosureFacts(ctx, t.engine, sessionID, []string{handle}, "test")

	view := normalizeProgressiveView(getStringArg(args, "view"))
	if view == "summary" {
		delete(resultMap, "replay")
		delete(resultMap, "diagnosis")
		if assertions, ok := resultMap["assertions"].([]map[string]interface{}); ok {
			for _, assertion := range assertions {
				delete(assertion, "sample")
			}
		}
	}
	return resultMap, nil
}
