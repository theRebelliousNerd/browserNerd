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

// InteractTool performs actions on elements
type InteractTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
	redactor *security.Redactor
	// secrets are values resolved from value_env; they are never echoed.
	secrets map[string]bool
}

func (t *InteractTool) Name() string { return "interact" }
func (t *InteractTool) Description() string {
	return `Perform an action on one element: click, type, select, toggle, clear, hover.

FIND THE ELEMENT BY ONE OF:
- ref: from get-interactive-elements / browser-observe
- text: visible text or accessible name ("Save"); exact match first, then contains
- label: a form control's label ("Quantity")
- role + name: ARIA role and accessible name (role "button", name "Save")
- x + y: viewport point (click and hover only)
When several elements match, the error lists them; add index (0-based).

ACTIONS:
- click / hover
- type: the field ends up holding exactly value (read back, or the op fails)
- select: native select by option value or label, or a custom combobox
  (opened, option clicked, selection verified)
- toggle: check/uncheck; clear: empty a field

Emits user_click/user_type/user_select/user_toggle facts for Mangle.`
}
func (t *InteractTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session",
			},
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: click, type, select, toggle, clear, hover",
				"enum":        interactActions,
			},
			"ref":   map[string]interface{}{"type": "string", "description": "Element ref from get-interactive-elements"},
			"text":  map[string]interface{}{"type": "string", "description": "Visible text or accessible name"},
			"label": map[string]interface{}{"type": "string", "description": "Form control label"},
			"role":  map[string]interface{}{"type": "string", "description": "ARIA role, with name"},
			"name":  map[string]interface{}{"type": "string", "description": "Accessible name for role"},
			"index": map[string]interface{}{"type": "integer", "description": "0-based pick among several matches"},
			"exact": map[string]interface{}{"type": "boolean", "description": "No contains fallback"},
			"x":     map[string]interface{}{"type": "number", "description": "Viewport x (click/hover)"},
			"y":     map[string]interface{}{"type": "number", "description": "Viewport y (click/hover)"},
			"value": map[string]interface{}{
				"type":        "string",
				"description": "Value for type/select actions",
			},
			"submit": map[string]interface{}{
				"type":        "boolean",
				"description": "Press Enter after typing (default: false)",
			},
		},
		"required": []string{"session_id", "action"},
	}
}
func (t *InteractTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	action := strings.ToLower(getStringArg(args, "action"))
	value, _ := scalarString(args["value"])
	submit := getBoolArg(args, "submit", false)
	loc := locatorFromMap(args)

	if sessionID == "" || action == "" {
		return nil, fmt.Errorf("session_id and action are required")
	}
	if loc.empty() {
		return map[string]interface{}{"success": false, "error": "interact needs ref, text, label, role(+name), or x/y"}, nil
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	page = page.Context(ctx)

	if loc.HasPoint {
		return t.pointAction(ctx, sessionID, action, loc)
	}

	registry := t.sessions.Registry(sessionID)
	controls := action == "type" || action == "select" || action == "toggle" || action == "clear"
	target, err := locateElement(ctx, page, registry, loc, controls)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	element := target.element

	var validationWarnings []string
	descriptor := loc.Ref
	if registry != nil && loc.Ref != "" {
		if fp := registry.Get(loc.Ref); fp != nil {
			validation := validateFingerprint(element, fp)
			if len(validation.Changes) > 0 {
				validationWarnings = validation.Changes
			}
			descriptor += " " + strings.Join([]string{
				fp.TagName, fp.ID, fp.Name, fp.AriaLabel, fp.DataTestID, fp.Role,
			}, " ")
		}
	}
	sensitiveValue := t.secrets[value] || fieldIsSensitive(ctx, element, descriptor, t.redactor)
	safeValue := mandatoryRedactor(t.redactor).SanitizeString(value)
	if sensitiveValue {
		safeValue = security.Redacted
	}
	subject := firstNonEmpty(loc.Ref, target.located)

	// A hidden native select is how custom select widgets keep their value;
	// select finds the visible trigger that drives it.
	if action != "select" {
		visible, err := element.Context(ctx).Timeout(3 * time.Second).Visible()
		if err != nil || !visible {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("element not visible: %s", loc)}, nil
		}
	}
	if target.disabled && (action == "click" || action == "toggle") {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("element is disabled: %s", subject)}, nil
	}

	result := map[string]interface{}{"success": true, "action": action}
	if loc.Ref != "" {
		result["ref"] = loc.Ref
	} else {
		result["located"] = target.located
	}

	switch action {
	case "click":
		if err := clickElement(ctx, page, element, 1); err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("click failed: %v", err)}, nil
		}

	case "hover":
		if err := hoverElement(ctx, page, element); err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("hover failed: %v", err)}, nil
		}

	case "type", "clear":
		if action == "clear" {
			value = ""
		}
		write, err := setFieldValue(ctx, page, element, value, sensitiveValue)
		if err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("%s failed: %v", action, err)}, nil
		}
		if submit {
			if err := dispatchKeyChord(page, keyChord{key: namedKeys["enter"]}); err != nil {
				return map[string]interface{}{"success": false, "error": fmt.Sprintf("submit failed: %v", err)}, nil
			}
		}
		if write.Note != "" {
			result["note"] = write.Note
		}
		if action == "type" && value != "" {
			if sensitiveValue {
				result["value"] = security.Redacted
			} else {
				result["value"] = mandatoryRedactor(t.redactor).SanitizeString(write.Actual)
			}
		}

	case "select":
		outcome, err := selectValue(ctx, page, element, value)
		if err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("select failed: %v", err)}, nil
		}
		result["selected"] = outcome.Label
		if outcome.Value != "" && outcome.Value != outcome.Label {
			result["value"] = outcome.Value
		}
		result["verified_by"] = outcome.VerifiedBy

	case "toggle":
		before, err := inspectControl(ctx, element)
		if err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("toggle failed: %v", err)}, nil
		}
		if err := clickElement(ctx, page, element, 1); err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("toggle failed: %v", err)}, nil
		}
		after, err := inspectControl(ctx, element)
		if err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("toggle failed: %v", err)}, nil
		}
		if after.Tag == "INPUT" && after.Type == "checkbox" && before.Checked == after.Checked {
			return map[string]interface{}{"success": false, "error": "toggle clicked, but the checkbox did not change state"}, nil
		}
		result["checked"] = after.Checked

	default:
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("unknown action %q; use %s", action, strings.Join(interactActions, ", "))}, nil
	}

	t.emitInteractFact(ctx, sessionID, action, subject, safeValue)
	if len(validationWarnings) > 0 {
		result["warning"] = "Element properties changed since discovery"
		result["changes"] = validationWarnings
	}
	return result, nil
}

// pointAction clicks or hovers a viewport point, naming what sits there.
func (t *InteractTool) pointAction(ctx context.Context, sessionID, action string, loc elementLocator) (interface{}, error) {
	page, _ := t.sessions.Page(sessionID)
	page = page.Context(ctx)
	if action != "click" && action != "hover" {
		return map[string]interface{}{"success": false, "error": "x/y locates only click and hover"}, nil
	}
	located, err := describePoint(ctx, page, loc.X, loc.Y)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	if action == "hover" {
		err = hoverAt(page, loc.X, loc.Y)
	} else {
		err = dispatchClick(page, loc.X, loc.Y, 1)
	}
	if err != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("%s at (%.0f, %.0f) failed: %v", action, loc.X, loc.Y, err)}, nil
	}
	if action == "click" {
		t.emitInteractFact(ctx, sessionID, action, located, "")
	}
	return map[string]interface{}{"success": true, "action": action, "located": located}, nil
}

func (t *InteractTool) emitInteractFact(ctx context.Context, sessionID, action, subject, safeValue string) {
	if t.engine == nil {
		return
	}
	now := time.Now()
	var predicate string
	var factArgs []interface{}
	switch action {
	case "click":
		predicate = "user_click"
		factArgs = []interface{}{sessionID, subject, now.UnixMilli()}
	case "type":
		predicate = "user_type"
		factArgs = []interface{}{sessionID, subject, safeValue, now.UnixMilli()}
	case "select":
		predicate = "user_select"
		factArgs = []interface{}{sessionID, subject, safeValue, now.UnixMilli()}
	case "toggle":
		predicate = "user_toggle"
		factArgs = []interface{}{sessionID, subject, now.UnixMilli()}
	default:
		return
	}
	_ = t.engine.AddFacts(ctx, []mangle.Fact{{Predicate: predicate, Args: factArgs, Timestamp: now}})
}

func mandatoryRedactor(redactor *security.Redactor) *security.Redactor {
	if redactor != nil {
		return redactor
	}
	return security.NewRedactor(nil)
}
