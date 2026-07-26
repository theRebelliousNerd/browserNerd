package mcp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"

	"gopkg.in/yaml.v3"
)

// GenerateTestTool synthesizes a draft TestSpec from privacy-safe action facts.
// The emitted operations use browser-act's native command vocabulary.
type GenerateTestTool struct {
	engine *mangle.Engine
}

func (t *GenerateTestTool) Name() string { return "generate-test" }

func (t *GenerateTestTool) Description() string {
	return `Turn a recorded interaction into a draft run-test spec.

Reads the session's action facts (navigation_event, input_event, click_event)
in timestamp order and emits them as a replayable actions[] list, plus a couple
of safe health assertions (no visible errors, no failed requests) to edit.

TOKEN COST: Low. Emits a compact YAML test, not a transcript.

WORKFLOW:
1. Drive the browser through the flow you want to capture.
2. generate-test to get a draft spec.
3. Edit the assertions to describe success, then feed it to run-test.

Returns: {name, actions_count, test, test_yaml}.`
}

func (t *GenerateTestTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id":         map[string]interface{}{"type": "string", "description": "Session whose actions to capture. If omitted, all sessions' actions are used."},
			"name":               map[string]interface{}{"type": "string", "description": "Name for the generated test (default 'recorded test')."},
			"include_assertions": map[string]interface{}{"type": "boolean", "description": "Include suggested health assertions (default true)."},
		},
	}
}

// timedAction pairs a synthesized action with the timestamp it occurred at.
type timedAction struct {
	ts     time.Time
	action map[string]interface{}
}

func (t *GenerateTestTool) Execute(_ context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	name := getStringArg(args, "name")
	if name == "" {
		name = "recorded test"
	}

	var timed []timedAction

	for _, f := range t.engine.FactsByPredicate("navigation_event") {
		if url, ok := stringArgAt(f, 1, sessionID); ok {
			timed = append(timed, timedAction{f.Timestamp, map[string]interface{}{"type": "navigate", "url": url}})
		}
	}
	for _, f := range t.engine.FactsByPredicate("input_event") {
		if node, ok := stringArgAt(f, 1, sessionID); ok {
			action := map[string]interface{}{"type": "interact", "action": "type", "ref": node}
			if len(f.Args) >= 3 {
				value := fmt.Sprintf("%v", f.Args[2])
				if value == security.Redacted {
					action["value_env"] = fixtureEnvironmentName(node)
				} else {
					action["value"] = value
				}
			}
			timed = append(timed, timedAction{f.Timestamp, action})
		}
	}
	for _, f := range t.engine.FactsByPredicate("click_event") {
		if node, ok := stringArgAt(f, 1, sessionID); ok {
			timed = append(timed, timedAction{f.Timestamp, map[string]interface{}{"type": "interact", "action": "click", "ref": node}})
		}
	}

	sort.SliceStable(timed, func(i, j int) bool { return timed[i].ts.Before(timed[j].ts) })

	actions := make([]map[string]interface{}, 0, len(timed))
	for _, ta := range timed {
		actions = append(actions, ta.action)
	}

	spec := TestSpec{
		Name:      name,
		SessionID: sessionID,
		Actions:   actions,
	}
	if getBoolArg(args, "include_assertions", true) {
		spec.Assertions = []TestAssertion{
			{Name: "no visible errors", Query: "user_visible_error(S, _, _, _)", Expect: "absent"},
			{Name: "no failed requests", Query: "failed_request(S, _, _, _)", Expect: "absent"},
		}
	}

	yamlBytes, err := yaml.Marshal(spec)
	if err != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("encode test: %v", err)}, nil
	}

	return map[string]interface{}{
		"name":          name,
		"actions_count": len(actions),
		"test":          spec,
		"test_yaml":     string(yamlBytes),
		"note":          "draft - edit the assertions to describe success before running",
	}, nil
}

// stringArgAt returns the string value of arg index i, honoring an optional
// session filter on arg 0. ok is false when the session does not match or the
// index is out of range.
func stringArgAt(f mangle.Fact, i int, sessionID string) (string, bool) {
	if sessionID != "" {
		if len(f.Args) == 0 || fmt.Sprintf("%v", f.Args[0]) != sessionID {
			return "", false
		}
	}
	if i >= len(f.Args) {
		return "", false
	}
	return fmt.Sprintf("%v", f.Args[i]), true
}

func fixtureEnvironmentName(ref string) string {
	var normalized strings.Builder
	for _, r := range strings.ToUpper(ref) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			normalized.WriteRune(r)
		} else if normalized.Len() > 0 {
			normalized.WriteByte('_')
		}
	}
	name := strings.Trim(normalized.String(), "_")
	if name == "" {
		name = "SECRET"
	}
	return "BROWSERNERD_TEST_" + name
}
