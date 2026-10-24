package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"

	"gopkg.in/yaml.v3"
)

// BrowserTestTool is the progressive, script-free test authoring and execution
// surface. Fixtures use browser-act operations and Mangle assertions.
type BrowserTestTool struct {
	sessions                *browser.SessionManager
	engine                  *mangle.Engine
	disableUnsafeJavaScript bool
	// redactor and pathPolicy are handed to the replay, so a fixture's steps
	// meet the same redaction and upload confinement as browser-act's.
	redactor   *security.Redactor
	pathPolicy *security.PathPolicy
}

func (t *BrowserTestTool) Name() string { return "browser-test" }

func (t *BrowserTestTool) Description() string {
	return `Create, inspect, or run declarative browser tests without a Python harness.

A fixture is {name, session_id, operations[], assertions[]}: operations are
browser-act operations (actions[] is read as the same list) and assertions are
{name, query, expect: present|absent, scope}. Unknown keys, invalid operations
and a fixture with neither operations nor assertions are refused before
anything runs. Create captures privacy-safe steps from a session or normalizes
an inline test. Run replays the operations and returns compact causal
diagnosis on failure. An action test's assertions default to scope fresh: what
the run caused, plus state that still holds (the tab's current URL), never an
event from before the run. A fixture with operations and no assertions is a
replay: status replayed, never passed.
Initial session_id may be omitted when session_create/attach (or explicit-source
fork/target focus) precedes page operations. Receipts and assertions use the
final replay session; query variable S binds that session, never another tab.

Credential fields use value_env, resolved only while running and never written
back into the fixture; an unset or empty variable stops the run before the
first step, naming the variable.

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
			"session_id": map[string]interface{}{"type": "string", "description": "Initial session; omit for a session-establishing fixture"},
			"name":       map[string]interface{}{"type": "string"},
			"test": map[string]interface{}{
				"type":        "object",
				"description": "Fixture {name, session_id, operations[], assertions[]}",
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
		result["summary"] = fmt.Sprintf("Created declarative fixture with %v browser-act operation(s)", result["operation_count"])
		return result, nil
	}
	spec, err := parseTestSpec(args)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	operations, issues := validateTestOperations(spec.Operations)
	if len(issues) > 0 {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("fixture operations are invalid: op %d %s: %s", issues[0].Index, issues[0].Type, issues[0].Message),
			"issues":  issues,
		}, nil
	}
	spec.Operations = operations
	encoded, err := yaml.Marshal(spec)
	if err != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("encode test: %v", err)}, nil
	}
	result := map[string]interface{}{
		"success":         true,
		"operation":       "create",
		"summary":         fmt.Sprintf("Normalized %d operation(s) and %d assertion(s)", len(spec.Operations), len(spec.Assertions)),
		"test":            spec,
		"test_yaml":       string(encoded),
		"operation_count": len(spec.Operations),
		"assertion_count": len(spec.Assertions),
	}
	if len(spec.Assertions) == 0 {
		result["warning"] = noAssertionsWarning
	}
	return result, nil
}

// noAssertionsWarning is what create and inspect say about a steps-only
// fixture: run replays it and reports replayed, not passed.
const noAssertionsWarning = "no assertions: run only replays the steps and reports status replayed, not passed; add {name, query, expect} to check what they caused"

func (t *BrowserTestTool) inspect(args map[string]interface{}) (interface{}, error) {
	spec, err := parseTestSpec(args)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	_, issues := validateTestOperations(spec.Operations)
	result := map[string]interface{}{
		"success":         len(issues) == 0,
		"operation":       "inspect",
		"name":            spec.Name,
		"session_id":      spec.SessionID,
		"operation_count": len(spec.Operations),
		"assertion_count": len(spec.Assertions),
		"operations":      spec.Operations,
		"assertions":      spec.Assertions,
	}
	if len(issues) > 0 {
		result["error"] = fmt.Sprintf("op %d %s: %s", issues[0].Index, issues[0].Type, issues[0].Message)
		result["issues"] = issues
	}
	if len(spec.Assertions) == 0 {
		result["warning"] = noAssertionsWarning
	}
	return result, nil
}

func (t *BrowserTestTool) run(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	result, err := (&RunTestTool{
		sessions:                t.sessions,
		engine:                  t.engine,
		disableUnsafeJavaScript: t.disableUnsafeJavaScript,
		redactor:                t.redactor,
		pathPolicy:              t.pathPolicy,
	}).Execute(ctx, args)
	if err != nil {
		return nil, err
	}
	resultMap := result.(map[string]interface{})
	resultMap["operation"] = "run"
	if replayOnly, _ := resultMap["replay_only"].(bool); replayOnly {
		replayed, _ := resultMap["replayed"].(bool)
		resultMap["success"] = replayed
		resultMap["status"] = ternaryStatus(replayed, "replayed", "failed")
		resultMap["summary"] = fmt.Sprintf("Replay %s; no assertions were checked", ternaryStatus(replayed, "completed", "failed"))
	} else {
		passed, _ := resultMap["passed"].(bool)
		resultMap["success"] = passed
		resultMap["status"] = ternaryStatus(passed, "passed", "failed")
		resultMap["summary"] = fmt.Sprintf("Declarative test %s", resultMap["status"])
	}
	sessionID, _ := resultMap["session_id"].(string)
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
