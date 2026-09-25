package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

// controlInfo is what a field is, read before choosing how to set it.
type controlInfo struct {
	Tag      string `json:"tag"`
	Type     string `json:"type"`
	Role     string `json:"role"`
	Editable bool   `json:"editable"`
	HasPopup string `json:"haspopup"`
	Expanded string `json:"expanded"`
	Disabled bool   `json:"disabled"`
	ReadOnly bool   `json:"readonly"`
	Visible  bool   `json:"visible"`
	Checked  bool   `json:"checked"`
}

const inspectControlScript = `function () {
  const r = this.getBoundingClientRect();
  const cs = getComputedStyle(this);
  return {
    tag: this.tagName,
    type: (this.getAttribute('type') || '').toLowerCase(),
    role: (this.getAttribute('role') || '').toLowerCase(),
    editable: !!this.isContentEditable,
    haspopup: (this.getAttribute('aria-haspopup') || '').toLowerCase(),
    expanded: this.getAttribute('aria-expanded') || '',
    disabled: this.disabled === true || this.getAttribute('aria-disabled') === 'true',
    readonly: this.readOnly === true || this.getAttribute('aria-readonly') === 'true',
    visible: r.width > 1 && r.height > 1 && cs.visibility !== 'hidden' && cs.display !== 'none',
    checked: this.checked === true,
  };
}`

func inspectControl(ctx context.Context, el *rod.Element) (controlInfo, error) {
	var info controlInfo
	res, err := el.Context(ctx).Timeout(3 * time.Second).Eval(inspectControlScript)
	if err != nil {
		return info, fmt.Errorf("could not read the element: %w", err)
	}
	if err := res.Value.Unmarshal(&info); err != nil {
		return info, fmt.Errorf("could not read the element: %w", err)
	}
	return info, nil
}

// isCustomCombobox reports a widget that opens a popup of options instead of
// holding a value itself.
func (c controlInfo) isCustomCombobox() bool {
	if c.Tag == "SELECT" {
		return false
	}
	if c.Role == "combobox" || c.Role == "listbox" {
		return true
	}
	switch c.HasPopup {
	case "listbox", "true", "menu", "tree", "grid", "dialog":
		return c.Tag == "BUTTON" || c.Role == "button" || c.Expanded != ""
	}
	return false
}

// selectOutcome is the option a select verified as chosen.
type selectOutcome struct {
	Label      string
	Value      string
	VerifiedBy string
}

const selectNativeScript = `function (q) {
  const norm = (s) => (s == null ? '' : String(s)).replace(/\s+/g, ' ').trim();
  const opts = Array.from(this.options || []);
  const want = norm(q), lower = want.toLowerCase();
  let pick = opts.find((o) => o.value === q) || opts.find((o) => norm(o.text) === want) || opts.find((o) => norm(o.text).toLowerCase() === lower);
  if (!pick && lower) {
    const partial = opts.filter((o) => norm(o.text).toLowerCase().includes(lower));
    if (partial.length === 1) pick = partial[0];
    else if (partial.length > 1) return { ok: false, error: 'ambiguous', labels: partial.slice(0, 6).map((o) => norm(o.text) || o.value) };
  }
  if (!pick) return { ok: false, error: 'no_match', labels: opts.slice(0, 10).map((o) => norm(o.text) || o.value) };
  if (pick.disabled) return { ok: false, error: 'disabled', labels: [norm(pick.text)] };
  const setter = Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, 'value').set;
  setter.call(this, pick.value);
  this.dispatchEvent(new Event('input', { bubbles: true }));
  this.dispatchEvent(new Event('change', { bubbles: true }));
  const chosen = this.options[this.selectedIndex];
  return { ok: this.value === pick.value, value: this.value, label: norm(chosen ? chosen.text : '') };
}`

// selectNative sets a <select> by option value or visible label and reads
// the selection back.
func selectNative(ctx context.Context, el *rod.Element, value string) (selectOutcome, error) {
	res, err := el.Context(ctx).Timeout(3*time.Second).Eval(selectNativeScript, value)
	if err != nil {
		return selectOutcome{}, fmt.Errorf("select failed: %w", err)
	}
	v := res.Value
	if !v.Get("ok").Bool() {
		labels := jsonStrings(v.Get("labels").Arr())
		switch v.Get("error").Str() {
		case "ambiguous":
			return selectOutcome{}, fmt.Errorf("option %q matches several: %s; give the full label or the option value", value, strings.Join(labels, ", "))
		case "disabled":
			return selectOutcome{}, fmt.Errorf("option %q is disabled", value)
		case "no_match":
			return selectOutcome{}, fmt.Errorf("no option %q; options: %s", value, strings.Join(labels, ", "))
		}
		return selectOutcome{}, fmt.Errorf("the select did not keep option %q", value)
	}
	return selectOutcome{Label: v.Get("label").Str(), Value: v.Get("value").Str(), VerifiedBy: "select_value"}, nil
}

// findTriggerScript finds the visible trigger that drives a hidden native
// select, the shape Radix and similar libraries render for form submission.
const findTriggerScript = `function () {
  let scope = this.parentElement;
  for (let depth = 0; scope && depth < 3; depth++, scope = scope.parentElement) {
    const trigger = Array.from(scope.querySelectorAll('[role=combobox],button[aria-haspopup],button[aria-expanded]')).find((el) => el !== this);
    if (trigger) return trigger;
  }
  return null;
}`

// findOptionScript looks for the option a custom combobox's popup offers.
const findOptionScript = `function (query) {
  const norm = (s) => (s == null ? '' : String(s)).replace(/\s+/g, ' ').trim();
  const visible = (el) => { const r = el.getBoundingClientRect(); const cs = getComputedStyle(el); return r.width > 0 && r.height > 0 && cs.visibility !== 'hidden' && cs.display !== 'none'; };
  const optionSel = '[role=option],[role=menuitem],[role=menuitemradio],[role=treeitem],[cmdk-item]';
  const scopes = [];
  for (const attr of ['aria-controls', 'aria-owns']) {
    for (const id of (this.getAttribute(attr) || '').split(/\s+/)) {
      const node = id && document.getElementById(id);
      if (node) scopes.push(node);
    }
  }
  let options = [];
  for (const s of scopes) options.push(...s.querySelectorAll(optionSel));
  if (!options.some(visible)) {
    for (const s of scopes) if ((s.getAttribute('role') || '') === 'listbox') options.push(...Array.from(s.children));
  }
  if (!options.some(visible)) options = Array.from(document.querySelectorAll(optionSel));
  options = options.filter(visible);
  if (!options.length) return { state: 'no_options' };
  const want = norm(query), lower = want.toLowerCase();
  const label = (o) => norm(o.getAttribute('aria-label') || o.innerText || o.textContent);
  const valueOf = (o) => o.getAttribute('data-value') || o.getAttribute('value') || '';
  let pick = options.filter((o) => label(o) === want);
  if (!pick.length) pick = options.filter((o) => label(o).toLowerCase() === lower);
  if (!pick.length) pick = options.filter((o) => valueOf(o) === query);
  if (!pick.length) {
    pick = options.filter((o) => label(o).toLowerCase().includes(lower));
    if (pick.length > 1) return { state: 'ambiguous', labels: pick.slice(0, 6).map(label) };
  }
  if (!pick.length) return { state: 'no_match', labels: options.slice(0, 8).map(label) };
  window.__bnLocated = pick[0];
  return { state: 'ok', label: label(pick[0]), value: valueOf(pick[0]) };
}`

// verifySelectionScript confirms a custom combobox now shows the option.
const verifySelectionScript = `function (want) {
  const norm = (s) => (s == null ? '' : String(s)).replace(/\s+/g, ' ').trim().toLowerCase();
  const label = norm(want.label);
  const shown = norm(this.tagName === 'INPUT' ? this.value : (this.innerText || this.textContent));
  if (label && shown.includes(label)) return 'trigger_text';
  let scope = this.parentElement;
  for (let depth = 0; scope && depth < 3; depth++, scope = scope.parentElement) {
    const native = scope.querySelector('select');
    if (native && native !== this && native.selectedIndex >= 0) {
      const opt = native.options[native.selectedIndex];
      if ((want.value && native.value === want.value) || (label && norm(opt.text) === label)) return 'native_select';
    }
  }
  for (const id of (this.getAttribute('aria-controls') || '').split(/\s+/)) {
    const list = id && document.getElementById(id);
    const sel = list && list.querySelector('[aria-selected=true]');
    if (sel && norm(sel.innerText || sel.textContent) === label) return 'aria_selected';
  }
  return '';
}`

const shownValueScript = `function () {
  const t = this.tagName === 'INPUT' ? this.value : (this.innerText || this.textContent);
  return (t || '').replace(/\s+/g, ' ').trim().slice(0, 60);
}`

// selectValue chooses value on a native select, a hidden native select behind
// a custom trigger, or a custom combobox, and succeeds only when the page
// shows that option as selected.
func selectValue(ctx context.Context, page *rod.Page, el *rod.Element, value string) (selectOutcome, error) {
	info, err := inspectControl(ctx, el)
	if err != nil {
		return selectOutcome{}, err
	}
	if info.Disabled {
		return selectOutcome{}, fmt.Errorf("the control is disabled")
	}
	if info.Tag == "SELECT" {
		if info.Visible {
			return selectNative(ctx, el, value)
		}
		trigger, triggerErr := elementFromScript(ctx, el, findTriggerScript)
		if triggerErr != nil || trigger == nil {
			// No visible widget drives it; setting the hidden select is all there is.
			outcome, err := selectNative(ctx, el, value)
			if err == nil {
				outcome.VerifiedBy = "hidden_select_value"
			}
			return outcome, err
		}
		return selectCustom(ctx, page, trigger, value)
	}
	if info.isCustomCombobox() {
		return selectCustom(ctx, page, el, value)
	}
	return selectOutcome{}, fmt.Errorf("%s is not a select or combobox; click it, then click the option by text", strings.ToLower(info.Tag))
}

// selectCustom opens a custom combobox, clicks the option whose text matches,
// and verifies the widget shows it.
func selectCustom(ctx context.Context, page *rod.Page, trigger *rod.Element, value string) (selectOutcome, error) {
	info, err := inspectControl(ctx, trigger)
	if err != nil {
		return selectOutcome{}, err
	}
	if info.Tag == "INPUT" {
		// An editable combobox filters its options as the user types.
		if _, err := typeIntoField(ctx, page, trigger, value); err != nil {
			return selectOutcome{}, err
		}
	} else if info.Expanded != "true" {
		if err := clickElement(ctx, page, trigger, 1); err != nil {
			return selectOutcome{}, fmt.Errorf("could not open the combobox: %w", err)
		}
	}

	closePopup := func() { _ = dispatchKeyChord(page.Context(ctx), keyChord{key: namedKeys["escape"]}) }
	deadline := time.Now().Add(3 * time.Second)
	var found map[string]interface{}
	for {
		res, err := trigger.Context(ctx).Timeout(2*time.Second).Eval(findOptionScript, value)
		if err != nil {
			closePopup()
			return selectOutcome{}, fmt.Errorf("could not read the options: %w", err)
		}
		state := res.Value.Get("state").Str()
		if state == "ok" {
			found = map[string]interface{}{"label": res.Value.Get("label").Str(), "value": res.Value.Get("value").Str()}
			break
		}
		if time.Now().Before(deadline) && state != "ambiguous" {
			if err := sleepWithContext(ctx, 100*time.Millisecond); err != nil {
				return selectOutcome{}, err
			}
			continue
		}
		closePopup()
		labels := strings.Join(jsonStrings(res.Value.Get("labels").Arr()), ", ")
		switch state {
		case "no_options":
			return selectOutcome{}, fmt.Errorf("the combobox opened no visible options")
		case "ambiguous":
			return selectOutcome{}, fmt.Errorf("option %q matches several: %s; give the full label", value, labels)
		}
		return selectOutcome{}, fmt.Errorf("no option %q; options: %s", value, labels)
	}

	option, err := page.Context(ctx).Timeout(2 * time.Second).ElementByJS(rod.Eval(`() => { const el = window.__bnLocated; delete window.__bnLocated; return el; }`))
	if err != nil {
		closePopup()
		return selectOutcome{}, fmt.Errorf("could not take the option: %w", err)
	}
	if err := clickElement(ctx, page, option, 1); err != nil {
		closePopup()
		return selectOutcome{}, fmt.Errorf("could not click option %q: %w", found["label"], err)
	}

	outcome := selectOutcome{Label: fmt.Sprint(found["label"]), Value: fmt.Sprint(found["value"])}
	verifyUntil := time.Now().Add(1500 * time.Millisecond)
	for {
		res, err := trigger.Context(ctx).Timeout(2*time.Second).Eval(verifySelectionScript, found)
		if err == nil && res.Value.Str() != "" {
			outcome.VerifiedBy = res.Value.Str()
			return outcome, nil
		}
		if time.Now().After(verifyUntil) {
			break
		}
		if err := sleepWithContext(ctx, 100*time.Millisecond); err != nil {
			return selectOutcome{}, err
		}
	}
	shown := ""
	if res, err := trigger.Context(ctx).Timeout(2 * time.Second).Eval(shownValueScript); err == nil {
		shown = res.Value.Str()
	}
	return selectOutcome{}, fmt.Errorf("clicked option %q but the combobox shows %q", outcome.Label, shown)
}

// elementFromScript runs script with el as this and returns the element it
// returns, or nil when it returns null. It does not retry.
func elementFromScript(ctx context.Context, el *rod.Element, script string) (*rod.Element, error) {
	obj, err := el.Context(ctx).Timeout(2 * time.Second).Evaluate(rod.Eval(script).ByObject())
	if err != nil {
		return nil, err
	}
	if obj == nil || obj.Subtype != proto.RuntimeRemoteObjectSubtypeNode {
		return nil, nil
	}
	return el.Page().Context(ctx).ElementFromObject(obj)
}

func jsonStrings(items []gson.JSON) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.Str())
	}
	return out
}
