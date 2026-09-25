package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"

	"github.com/go-rod/rod"
)

// FillFormTool fills multiple form fields in a single call.
type FillFormTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
	redactor *security.Redactor
	// secrets are values resolved from value_env; they are never echoed.
	secrets map[string]bool
}

func (t *FillFormTool) Name() string { return "fill-form" }
func (t *FillFormTool) Description() string {
	return `Fill multiple form fields in a single call - much more efficient than individual interact() calls.

Each field is found by ref, label, text, or role+name, set to exactly its value
(text, number, date, email, textarea, contenteditable, checkbox, select or
custom combobox), and read back: a field that does not hold the value fails the
fill, and a failed field stops the submit.

EXAMPLE CALL:
{
  "session_id": "ABC123",
  "fields": [
    {"label": "Email", "value": "user@example.com"},
    {"label": "Quantity", "value": "7"}
  ],
  "submit_button": "Save"
}

SUBMIT OPTIONS:
- submit: true - Press Enter after last field
- submit_button: ref or visible text of the button to click after filling`
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
				"description": "Fields: [{ref|label|text|role+name, value}, ...]",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"ref":   map[string]interface{}{"type": "string", "description": "Element ref, name, or id"},
						"label": map[string]interface{}{"type": "string", "description": "The field's label"},
						"text":  map[string]interface{}{"type": "string", "description": "Visible text or accessible name"},
						"role":  map[string]interface{}{"type": "string"},
						"name":  map[string]interface{}{"type": "string"},
						"value": map[string]interface{}{"type": "string", "description": "Value the field must hold"},
					},
					"required": []string{"value"},
				},
			},
			"submit": map[string]interface{}{
				"type":        "boolean",
				"description": "Press Enter after filling the last field to submit the form",
			},
			"submit_button": map[string]interface{}{
				"type":        "string",
				"description": "Ref or visible text of the button to click after filling",
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
	page = page.Context(ctx)
	registry := t.sessions.Registry(sessionID)

	fieldsList, ok := args["fields"].([]interface{})
	if !ok || len(fieldsList) == 0 {
		return map[string]interface{}{"success": false, "error": "fields must be a non-empty array"}, nil
	}

	filledFields := make([]map[string]interface{}, 0, len(fieldsList))
	var failures []string
	var lastElement *rod.Element

	for i, fieldItem := range fieldsList {
		fieldMap, ok := fieldItem.(map[string]interface{})
		if !ok {
			failures = append(failures, fmt.Sprintf("fields[%d] is not an object", i))
			continue
		}
		loc := locatorFromMap(fieldMap)
		value, _ := scalarString(fieldMap["value"])
		entry := map[string]interface{}{"index": i}
		if loc.Ref != "" {
			entry["ref"] = loc.Ref
		} else {
			entry["field"] = loc.String()
		}
		fail := func(err error) {
			entry["error"] = err.Error()
			filledFields = append(filledFields, entry)
			failures = append(failures, fmt.Sprintf("fields[%d] %s: %v", i, loc, err))
		}
		if loc.empty() {
			fail(fmt.Errorf("needs ref, label, text or role"))
			continue
		}

		target, err := locateElement(ctx, page, registry, loc, true)
		if err != nil {
			fail(err)
			continue
		}
		if target.located != "" {
			entry["located"] = target.located
		}
		sensitive := t.secrets[value] || fieldIsSensitive(ctx, target.element, loc.Ref, t.redactor)
		write, err := setFieldValue(ctx, page, target.element, value, sensitive)
		if err != nil {
			fail(err)
			continue
		}
		entry["success"] = true
		if write.Note != "" {
			entry["note"] = write.Note
		}
		if write.Selected != "" {
			entry["selected"] = write.Selected
		}
		lastElement = target.element
		filledFields = append(filledFields, entry)

		now := time.Now()
		_ = t.engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "form_field_filled",
			Args:      []interface{}{sessionID, firstNonEmpty(loc.Ref, target.located), now.UnixMilli()},
			Timestamp: now,
		}})
	}

	if len(failures) > 0 {
		msg := failures[0]
		if len(failures) > 1 {
			msg += fmt.Sprintf(" (and %d more field(s) failed)", len(failures)-1)
		}
		if submit || submitButton != "" {
			msg += "; not submitted"
		}
		return map[string]interface{}{
			"success":       false,
			"error":         msg,
			"filled_count":  len(filledFields) - len(failures),
			"filled_fields": filledFields,
			"submitted":     false,
		}, nil
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
		button, err := locateElement(ctx, page, registry, elementLocator{Ref: submitButton}, false)
		if err != nil {
			// A submit_button that names no element may be the button's text.
			button, err = locateElement(ctx, page, registry, elementLocator{Text: submitButton}, false)
		}
		if err != nil {
			return map[string]interface{}{
				"success":       false,
				"error":         fmt.Sprintf("submit button %q not found as a ref or visible text: %v", submitButton, err),
				"filled_fields": filledFields,
			}, nil
		}
		if err := clickElement(ctx, page, button.element, 1); err != nil {
			return map[string]interface{}{
				"success":       false,
				"error":         fmt.Sprintf("submit click failed: %v", err),
				"filled_fields": filledFields,
			}, nil
		}
	} else if submit && lastElement != nil {
		if err := dispatchKeyChord(page, keyChord{key: namedKeys["enter"]}); err != nil {
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

// fieldDescriptorScript returns the attributes that say whether a field holds
// a credential.
const fieldDescriptorScript = `function () {
  return ['type', 'name', 'id', 'autocomplete', 'aria-label'].map((a) => this.getAttribute(a) || '').join(' ');
}`

// fieldIsSensitive reports a field whose value must never be echoed.
func fieldIsSensitive(ctx context.Context, el *rod.Element, ref string, redactor *security.Redactor) bool {
	descriptor := ref
	if res, err := el.Context(ctx).Timeout(2 * time.Second).Eval(fieldDescriptorScript); err == nil {
		descriptor += " " + res.Value.Str()
	}
	return mandatoryRedactor(redactor).RedactInputValue(descriptor, "probe") == security.Redacted
}

// fieldWrite is what a field holds after a write.
type fieldWrite struct {
	Actual   string
	Selected string
	Note     string
}

// nativeValueTypes are inputs whose value is set, not typed: their text entry
// is locale-dependent and segment-based.
var nativeValueTypes = map[string]string{
	"date":           "YYYY-MM-DD",
	"time":           "HH:MM",
	"datetime-local": "YYYY-MM-DDTHH:MM",
	"month":          "YYYY-MM",
	"week":           "YYYY-Www",
	"color":          "#rrggbb",
	"range":          "a number within the range",
}

// setFieldValue makes el hold value, whatever kind of control it is, and
// fails unless the page reads that value back.
func setFieldValue(ctx context.Context, page *rod.Page, el *rod.Element, value string, sensitive bool) (fieldWrite, error) {
	info, err := inspectControl(ctx, el)
	if err != nil {
		return fieldWrite{}, err
	}
	if info.Disabled {
		return fieldWrite{}, fmt.Errorf("the field is disabled")
	}
	if info.ReadOnly {
		return fieldWrite{}, fmt.Errorf("the field is read-only")
	}
	switch {
	case info.Tag == "SELECT" || (info.Tag != "INPUT" && info.Tag != "TEXTAREA" && !info.Editable && info.isCustomCombobox()):
		outcome, err := selectValue(ctx, page, el, value)
		if err != nil {
			return fieldWrite{}, err
		}
		return fieldWrite{Actual: outcome.Label, Selected: outcome.Label}, nil
	case info.Tag == "INPUT" && (info.Type == "checkbox" || info.Type == "radio"):
		return setChecked(ctx, page, el, info, value)
	case info.Tag == "INPUT" && info.Type == "file":
		return fieldWrite{}, fmt.Errorf("a file input is set with the upload op")
	case info.Tag == "INPUT" && nativeValueTypes[info.Type] != "":
		actual, err := setNativeValue(ctx, el, value)
		if err != nil {
			return fieldWrite{}, err
		}
		return verifyFieldValue(value, actual, sensitive, fmt.Sprintf("a %s input takes %s", info.Type, nativeValueTypes[info.Type]))
	case info.Tag == "INPUT" || info.Tag == "TEXTAREA":
		actual, err := typeIntoField(ctx, page, el, value)
		if err != nil {
			return fieldWrite{}, err
		}
		if actual == "" && value != "" {
			// Typing did not land at all (a field that never took focus); set
			// the value the way a framework does. A partial landing - maxlength,
			// a filter - is what a person would get, and stays a mismatch.
			if set, setErr := setNativeValue(ctx, el, value); setErr == nil {
				actual = set
			}
		}
		hint := ""
		if info.Type == "number" {
			hint = "a number input takes digits only"
		}
		return verifyFieldValue(value, actual, sensitive, hint)
	case info.Editable:
		actual, err := typeIntoEditable(ctx, page, el, value)
		if err != nil {
			return fieldWrite{}, err
		}
		return verifyFieldValue(value, actual, sensitive, "")
	}
	return fieldWrite{}, fmt.Errorf("%s is not an editable field", strings.ToLower(info.Tag))
}

// clearAndFocusScript focuses an input or textarea and empties it through the
// prototype value setter, so a framework that tracks the value (React) sees
// the change in the input event.
const clearAndFocusScript = `function () {
  const proto = this instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
  this.focus();
  if (this.value !== '') {
    setter.call(this, '');
    this.dispatchEvent(new Event('input', { bubbles: true }));
  }
  const root = this.getRootNode ? this.getRootNode() : document;
  return (root.activeElement || document.activeElement) === this;
}`

const setNativeValueScript = `function (v) {
  const proto = this instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  const setter = Object.getOwnPropertyDescriptor(proto, 'value').set;
  this.focus();
  setter.call(this, v);
  this.dispatchEvent(new Event('input', { bubbles: true }));
  this.dispatchEvent(new Event('change', { bubbles: true }));
  return this.value;
}`

// typeIntoField empties a text field and types value into it as text input,
// then returns what the field holds.
func typeIntoField(ctx context.Context, page *rod.Page, el *rod.Element, value string) (string, error) {
	res, err := el.Context(ctx).Timeout(3 * time.Second).Eval(clearAndFocusScript)
	if err != nil {
		return "", fmt.Errorf("could not focus the field: %w", err)
	}
	if value != "" && res.Value.Bool() {
		if err := page.Context(ctx).Timeout(5 * time.Second).InsertText(value); err != nil {
			return "", fmt.Errorf("typing failed: %w", err)
		}
	}
	return readFieldValue(ctx, el)
}

func readFieldValue(ctx context.Context, el *rod.Element) (string, error) {
	res, err := el.Context(ctx).Timeout(3 * time.Second).Eval(`function () { return this.isContentEditable && !('value' in this) ? (this.innerText || '').replace(/\n+$/, '') : String(this.value) }`)
	if err != nil {
		return "", fmt.Errorf("could not read the field back: %w", err)
	}
	return res.Value.Str(), nil
}

func setNativeValue(ctx context.Context, el *rod.Element, value string) (string, error) {
	res, err := el.Context(ctx).Timeout(3*time.Second).Eval(setNativeValueScript, value)
	if err != nil {
		return "", fmt.Errorf("could not set the value: %w", err)
	}
	return res.Value.Str(), nil
}

const selectEditableScript = `function () {
  this.focus();
  const range = document.createRange();
  range.selectNodeContents(this);
  const sel = window.getSelection();
  sel.removeAllRanges();
  sel.addRange(range);
  return true;
}`

// typeIntoEditable replaces a contenteditable's content with value.
func typeIntoEditable(ctx context.Context, page *rod.Page, el *rod.Element, value string) (string, error) {
	if _, err := el.Context(ctx).Timeout(3 * time.Second).Eval(selectEditableScript); err != nil {
		return "", fmt.Errorf("could not focus the field: %w", err)
	}
	p := page.Context(ctx).Timeout(5 * time.Second)
	var err error
	if value == "" {
		_, err = p.Eval(`() => document.execCommand('delete')`)
	} else {
		err = p.InsertText(value)
	}
	if err != nil {
		return "", fmt.Errorf("typing failed: %w", err)
	}
	actual, err := readFieldValue(ctx, el)
	if err != nil || actual == value {
		return actual, err
	}
	res, err := el.Context(ctx).Timeout(3*time.Second).Eval(`function (v) {
  this.textContent = v;
  this.dispatchEvent(new InputEvent('input', { bubbles: true, data: v, inputType: 'insertText' }));
  return (this.innerText || '').replace(/\n+$/, '');
}`, value)
	if err != nil {
		return actual, nil
	}
	return res.Value.Str(), nil
}

// setChecked puts a checkbox or radio in the state value names.
func setChecked(ctx context.Context, page *rod.Page, el *rod.Element, info controlInfo, value string) (fieldWrite, error) {
	want, ok := parseCheckedValue(value)
	if !ok {
		return fieldWrite{}, fmt.Errorf("a %s takes true or false (got %q)", info.Type, value)
	}
	if info.Checked != want {
		if info.Type == "radio" && !want {
			return fieldWrite{}, fmt.Errorf("a radio cannot be unchecked; check another option in its group")
		}
		if err := clickElement(ctx, page, el, 1); err != nil {
			return fieldWrite{}, err
		}
		after, err := inspectControl(ctx, el)
		if err != nil {
			return fieldWrite{}, err
		}
		if after.Checked != want {
			return fieldWrite{}, fmt.Errorf("the %s did not change to %v", info.Type, want)
		}
	}
	return fieldWrite{Actual: fmt.Sprint(want)}, nil
}

func parseCheckedValue(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "on", "yes", "1", "checked":
		return true, true
	case "false", "off", "no", "0", "unchecked", "":
		return false, true
	}
	return false, false
}

// verifyFieldValue accepts the read-back value when it is the value, or the
// same characters reformatted by the page (an input mask); anything else is
// a failed write, reported without the values when the field is sensitive.
func verifyFieldValue(expected, actual string, sensitive bool, hint string) (fieldWrite, error) {
	if actual == expected {
		return fieldWrite{Actual: actual}, nil
	}
	if loose := looseFieldValue(expected); loose != "" && loose == looseFieldValue(actual) {
		note := "the page reformatted the value"
		if !sensitive {
			note = fmt.Sprintf("the page shows the value as %q", actual)
		}
		return fieldWrite{Actual: actual, Note: note}, nil
	}
	if sensitive {
		return fieldWrite{}, fmt.Errorf("the field does not hold the value that was filled (values redacted)")
	}
	msg := fmt.Sprintf("the field holds %q after filling, expected %q", actual, expected)
	if hint != "" {
		msg += "; " + hint
	}
	return fieldWrite{}, fmt.Errorf("%s", msg)
}

func looseFieldValue(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
