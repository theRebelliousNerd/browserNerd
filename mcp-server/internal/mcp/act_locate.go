package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ysmood/gson"
)

// elementLocator names one element: by a ref from browser-observe, by what a
// person sees (text, label, role+name), or by a viewport point.
type elementLocator struct {
	Ref      string
	Text     string
	Label    string
	Role     string
	Name     string
	Index    int
	HasIndex bool
	Exact    bool
	X, Y     float64
	HasPoint bool
}

func locatorFromMap(m map[string]interface{}) elementLocator {
	loc := elementLocator{
		Ref:   strings.TrimSpace(getStringFromMap(m, "ref")),
		Text:  getStringFromMap(m, "text"),
		Label: getStringFromMap(m, "label"),
		Role:  strings.TrimSpace(getStringFromMap(m, "role")),
		Name:  getStringFromMap(m, "name"),
		Exact: getBoolArg(m, "exact", false),
	}
	if n, ok := numberValue(m["index"]); ok {
		loc.Index, loc.HasIndex = int(n), true
	}
	x, hasX := numberValue(m["x"])
	y, hasY := numberValue(m["y"])
	if hasX && hasY {
		loc.X, loc.Y, loc.HasPoint = x, y, true
	}
	return loc
}

func (l elementLocator) empty() bool {
	return l.Ref == "" && l.Text == "" && l.Label == "" && l.Role == "" && !l.HasPoint
}

// String names the locator the way a caller wrote it, for messages.
func (l elementLocator) String() string {
	switch {
	case l.Ref != "":
		return fmt.Sprintf("ref %q", l.Ref)
	case l.Text != "":
		return fmt.Sprintf("text %q", l.Text)
	case l.Label != "":
		return fmt.Sprintf("label %q", l.Label)
	case l.Role != "" && l.Name != "":
		return fmt.Sprintf("role %s name %q", l.Role, l.Name)
	case l.Role != "":
		return fmt.Sprintf("role %s", l.Role)
	case l.HasPoint:
		return fmt.Sprintf("point (%.0f, %.0f)", l.X, l.Y)
	}
	return "no locator"
}

// locateScript finds the element a text, label or role locator names. It
// ranks exact matches over case-insensitive ones over contains matches,
// keeps only visible elements, prefers enabled and interactive ones, and
// collapses an element with the ancestors that merely wrap its text. The
// winner is parked on window.__bnLocated for the caller to take by object.
const locateScript = `(q) => {
  const norm = (s) => (s == null ? '' : String(s)).replace(/\s+/g, ' ').trim();
  const controlSel = 'input:not([type=hidden]),textarea,select,[contenteditable=""],[contenteditable="true"],[role=textbox],[role=combobox],[role=searchbox],[role=spinbutton],[role=checkbox],[role=radio],[role=switch],[role=slider],[role=listbox]';
  const interactiveSel = 'a[href],button,input:not([type=hidden]),select,textarea,summary,label,[role=button],[role=link],[role=menuitem],[role=menuitemcheckbox],[role=menuitemradio],[role=option],[role=tab],[role=checkbox],[role=radio],[role=switch],[role=combobox],[role=treeitem],[onclick],[tabindex]:not([tabindex="-1"]),[contenteditable=""],[contenteditable="true"]';
  const visible = (el) => {
    const r = el.getBoundingClientRect();
    if (r.width <= 0 || r.height <= 0) return false;
    const cs = getComputedStyle(el);
    return cs.visibility !== 'hidden' && cs.display !== 'none' && cs.opacity !== '0';
  };
  const disabled = (el) => el.disabled === true || el.getAttribute('aria-disabled') === 'true';
  const interactive = (el) => el.matches(interactiveSel) || getComputedStyle(el).cursor === 'pointer';
  const implicitRole = (el) => {
    const tag = el.tagName.toLowerCase();
    const type = (el.getAttribute('type') || 'text').toLowerCase();
    if (tag === 'button' || (tag === 'input' && ['button', 'submit', 'reset', 'image'].includes(type))) return 'button';
    if (tag === 'a' && el.hasAttribute('href')) return 'link';
    if (tag === 'input') {
      if (type === 'checkbox') return 'checkbox';
      if (type === 'radio') return 'radio';
      if (type === 'range') return 'slider';
      if (type === 'number') return 'spinbutton';
      if (type === 'search') return 'searchbox';
      return 'textbox';
    }
    if (tag === 'textarea') return 'textbox';
    if (tag === 'select') return el.multiple || el.size > 1 ? 'listbox' : 'combobox';
    if (/^h[1-6]$/.test(tag)) return 'heading';
    if (tag === 'img') return 'img';
    if (tag === 'li') return 'listitem';
    if (tag === 'nav') return 'navigation';
    if (tag === 'dialog') return 'dialog';
    return '';
  };
  const roleOf = (el) => (el.getAttribute('role') || implicitRole(el)).toLowerCase().split(/\s+/)[0];
  const labelText = (el) => {
    const by = el.getAttribute('aria-labelledby');
    if (by) {
      const t = norm(by.split(/\s+/).map((id) => document.getElementById(id)).filter(Boolean).map((n) => n.innerText || n.textContent).join(' '));
      if (t) return t;
    }
    const aria = norm(el.getAttribute('aria-label'));
    if (aria) return aria;
    if (el.labels && el.labels.length) {
      const t = norm(Array.from(el.labels).map((l) => l.innerText || l.textContent).join(' '));
      if (t) return t;
    }
    return '';
  };
  const accName = (el) => {
    const lt = labelText(el);
    if (lt) return lt;
    const tag = el.tagName;
    if (tag === 'INPUT') {
      const type = (el.type || '').toLowerCase();
      if (['button', 'submit', 'reset'].includes(type)) return norm(el.value) || (type === 'submit' ? 'Submit' : '');
      if (type === 'image') return norm(el.alt);
      return norm(el.placeholder) || norm(el.title);
    }
    if (tag === 'TEXTAREA' || tag === 'SELECT') return norm(el.placeholder) || norm(el.title);
    if (tag === 'IMG') return norm(el.alt);
    return norm(el.innerText || el.textContent).slice(0, 300) || norm(el.title);
  };
  const describe = (el) => {
    const tag = el.tagName.toLowerCase();
    const id = el.id ? '#' + el.id : '';
    const name = accName(el).slice(0, 40);
    return tag + id + (name ? ' "' + name + '"' : '');
  };

  const all = [];
  const collect = (root) => {
    for (const el of root.querySelectorAll('*')) {
      all.push(el);
      if (el.shadowRoot) collect(el.shadowRoot);
    }
  };
  collect(document);

  const wanted = norm(q.mode === 'role' ? q.name : q.text);
  const wantedLower = wanted.toLowerCase();
  const tierOf = (name) => {
    if (!wanted) return 0;
    if (name === wanted) return 0;
    const lower = name.toLowerCase();
    if (lower === wantedLower) return 1;
    if (!q.exact && lower.includes(wantedLower)) return 2;
    return -1;
  };

  let matches = [];
  for (const el of all) {
    const isControl = el.matches(controlSel);
    if (q.controls && !isControl) continue;
    let name;
    if (q.mode === 'role') {
      if (roleOf(el) !== q.role.toLowerCase()) continue;
      name = accName(el);
    } else if (q.mode === 'label') {
      if (!isControl) continue;
      name = labelText(el) || norm(el.getAttribute('placeholder'));
    } else {
      if (!isControl && !el.matches('input,img,[aria-label],[aria-labelledby]')) {
        if (!norm(el.textContent).toLowerCase().includes(wantedLower)) continue;
      }
      name = accName(el);
    }
    const tier = tierOf(name);
    if (tier < 0) continue;
    matches.push({ el, tier });
  }
  if (!matches.length) {
    return { ok: false, error: 'nothing on the page matches' };
  }
  // Rank among what a person can see: a hidden exact match (a mobile-only
  // button) must not hide the visible "Search Ctrl K" a contains match finds.
  const seen = matches.filter((m) => visible(m.el));
  if (!seen.length) {
    return { ok: false, error: matches.length + ' element(s) match but none is visible', candidates: matches.slice(0, 3).map((m) => describe(m.el)) };
  }
  const best = Math.min(...seen.map((m) => m.tier));
  let found = seen.filter((m) => m.tier === best).map((m) => m.el);
  const enabled = found.filter((el) => !disabled(el));
  if (enabled.length) found = enabled;
  const clickable = found.filter(interactive);
  if (clickable.length) {
    found = clickable.filter((el) => !clickable.some((other) => other !== el && el.contains(other)));
  } else {
    found = found.filter((el) => !found.some((other) => other !== el && el.contains(other)));
  }
  let pick = null;
  if (q.hasIndex) {
    if (q.index < 0 || q.index >= found.length) {
      return { ok: false, error: 'index ' + q.index + ' is out of range; ' + found.length + ' match(es)', count: found.length, candidates: found.slice(0, 3).map(describe) };
    }
    pick = found[q.index];
  } else if (found.length === 1) {
    pick = found[0];
  } else {
    return { ok: false, ambiguous: true, count: found.length, candidates: found.slice(0, 3).map(describe) };
  }
  window.__bnLocated = pick;
  return { ok: true, located: describe(pick), disabled: disabled(pick), count: found.length };
}`

type locateReport struct {
	OK         bool     `json:"ok"`
	Error      string   `json:"error"`
	Ambiguous  bool     `json:"ambiguous"`
	Count      int      `json:"count"`
	Candidates []string `json:"candidates"`
	Located    string   `json:"located"`
	Disabled   bool     `json:"disabled"`
}

// locatedElement is what a locator resolved to.
type locatedElement struct {
	element  *rod.Element
	located  string // human description, set for text/label/role locators
	disabled bool
}

// locateElement resolves a ref, text, label or role locator on page. controls
// restricts text matching to form controls (type, select, toggle, fill).
func locateElement(ctx context.Context, page *rod.Page, registry *browser.ElementRegistry, loc elementLocator, controls bool) (locatedElement, error) {
	if loc.Ref != "" {
		el, err := findElementByRefWithRegistry(page.Context(ctx), loc.Ref, registry)
		if err != nil {
			return locatedElement{}, err
		}
		return locatedElement{element: el}, nil
	}
	mode := "text"
	switch {
	case loc.Role != "":
		mode = "role"
	case loc.Label != "":
		mode = "label"
	}
	query := map[string]interface{}{
		"mode":     mode,
		"text":     firstNonEmpty(loc.Text, loc.Label),
		"role":     loc.Role,
		"name":     loc.Name,
		"exact":    loc.Exact,
		"index":    loc.Index,
		"hasIndex": loc.HasIndex,
		"controls": controls && mode == "text",
	}
	bounded := page.Context(ctx).Timeout(5 * time.Second)
	res, err := bounded.Evaluate(rod.Eval(locateScript, query))
	if err != nil {
		return locatedElement{}, fmt.Errorf("locate %s: %w", loc, err)
	}
	var report locateReport
	if err := json.Unmarshal([]byte(res.Value.JSON("", "")), &report); err != nil {
		return locatedElement{}, fmt.Errorf("locate %s: unreadable result: %w", loc, err)
	}
	if !report.OK {
		return locatedElement{}, locateFailure(loc, report)
	}
	el, err := bounded.ElementByJS(rod.Eval(`() => { const el = window.__bnLocated; delete window.__bnLocated; return el; }`))
	if err != nil {
		return locatedElement{}, fmt.Errorf("locate %s: %w", loc, err)
	}
	return locatedElement{element: el, located: report.Located, disabled: report.Disabled}, nil
}

func locateFailure(loc elementLocator, report locateReport) error {
	list := ""
	if len(report.Candidates) > 0 {
		parts := make([]string, 0, len(report.Candidates))
		for i, c := range report.Candidates {
			parts = append(parts, fmt.Sprintf("[%d] %s", i, c))
		}
		list = ": " + strings.Join(parts, ", ")
	}
	if report.Ambiguous {
		return fmt.Errorf("%s matches %d visible elements%s; add index (0-based), use role+name, or a ref from browser-observe", loc, report.Count, list)
	}
	return fmt.Errorf("%s: %s%s", loc, report.Error, list)
}

// clickTargetScript scrolls the element to the middle of the viewport and
// returns a point on it that a click would actually hit, or what covers it.
const clickTargetScript = `function (scroll) {
  const describe = (el) => {
    if (!el) return 'nothing';
    const cls = (typeof el.className === 'string' ? el.className : '').trim().split(/\s+/).filter(Boolean).slice(0, 2).join('.');
    return el.tagName.toLowerCase() + (el.id ? '#' + el.id : '') + (cls ? '.' + cls : '');
  };
  if (!this.isConnected) return { error: 'element is detached' };
  const style = getComputedStyle(this);
  if (style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0') return { error: 'element is not visible' };
  if (scroll) this.scrollIntoView({ block: 'center', inline: 'center', behavior: 'instant' });
  const rects = Array.from(this.getClientRects()).filter((r) => r.width > 0 && r.height > 0);
  if (!rects.length) return { error: 'element has no visible box' };
  const vw = window.innerWidth, vh = window.innerHeight;
  let covering = null;
  for (const r of rects) {
    const points = [
      [r.left + r.width / 2, r.top + r.height / 2],
      [r.left + Math.min(4, r.width / 2), r.top + Math.min(4, r.height / 2)],
      [r.right - Math.min(4, r.width / 2), r.bottom - Math.min(4, r.height / 2)],
    ];
    for (const [px, py] of points) {
      const x = Math.min(Math.max(px, 0), vw - 1), y = Math.min(Math.max(py, 0), vh - 1);
      const hit = document.elementFromPoint(x, y);
      if (hit && (hit === this || this.contains(hit))) return { x, y, top: window.self === window.top };
      if (!covering && hit) covering = hit;
    }
  }
  return { error: 'covered', by: describe(covering) };
}`

// clickElement clicks el the way a person does - a real pointer press and
// release at a point that hits it - with every step bound to ctx. It does not
// wait on animation frames, so a background tab or a page blocked by a dialog
// fails within the bound instead of hanging.
func clickElement(ctx context.Context, page *rod.Page, el *rod.Element, clickCount int) error {
	x, y, err := prepareElementPointer(ctx, page, el)
	if err != nil {
		return err
	}
	return pressAndRelease(page.Context(ctx), x, y, clickCount)
}

// hoverElement moves the pointer onto el.
func hoverElement(ctx context.Context, page *rod.Page, el *rod.Element) error {
	_, _, err := prepareElementPointer(ctx, page, el)
	return err
}

// hoverAt moves the pointer to a viewport point.
func hoverAt(page *rod.Page, x, y float64) error {
	return proto.InputDispatchMouseEvent{Type: proto.InputDispatchMouseEventTypeMouseMoved, X: x, Y: y}.Call(page)
}

// clickDispatchTimeout bounds the press and release. A click whose handler
// opens a JavaScript dialog does not complete until the dialog is answered.
const clickDispatchTimeout = 5 * time.Second

// dispatchClick presses and releases the left button at a viewport point.
func dispatchClick(page *rod.Page, x, y float64, clickCount int) error {
	bounded := page.Timeout(clickDispatchTimeout)
	if err := hoverAt(bounded, x, y); err != nil {
		return err
	}
	return pressAndRelease(bounded, x, y, clickCount)
}

// pressAndRelease sends one native press after the caller has placed the pointer.
func pressAndRelease(page *rod.Page, x, y float64, clickCount int) error {
	if clickCount < 1 {
		clickCount = 1
	}
	bounded := page.Timeout(clickDispatchTimeout)
	err := proto.InputDispatchMouseEvent{
		Type: proto.InputDispatchMouseEventTypeMousePressed, X: x, Y: y,
		Button: proto.InputMouseButtonLeft, Buttons: gson.Int(1), ClickCount: clickCount,
	}.Call(bounded)
	if err == nil {
		err = proto.InputDispatchMouseEvent{
			Type: proto.InputDispatchMouseEventTypeMouseReleased, X: x, Y: y,
			Button: proto.InputMouseButtonLeft, Buttons: gson.Int(0), ClickCount: clickCount,
		}.Call(bounded)
	}
	if err != nil && errors.Is(err, context.DeadlineExceeded) && page.GetContext().Err() == nil {
		return fmt.Errorf("the click did not complete within %s; a JavaScript dialog or a busy page is blocking it", clickDispatchTimeout)
	}
	return err
}

// describePointScript names the element at a viewport point.
const describePointScript = `(x, y) => {
  const el = document.elementFromPoint(x, y);
  if (!el) return { error: 'no element at (' + x + ', ' + y + '); the viewport is ' + window.innerWidth + 'x' + window.innerHeight };
  const text = (el.innerText || el.textContent || '').replace(/\s+/g, ' ').trim().slice(0, 40);
  return { located: el.tagName.toLowerCase() + (el.id ? '#' + el.id : '') + (text ? ' "' + text + '"' : '') };
}`

// describePoint names what sits at a viewport point, or explains why nothing does.
func describePoint(ctx context.Context, page *rod.Page, x, y float64) (string, error) {
	res, err := page.Context(ctx).Timeout(3*time.Second).Eval(describePointScript, x, y)
	if err != nil {
		return "", err
	}
	if msg := res.Value.Get("error"); !msg.Nil() {
		return "", errors.New(msg.Str())
	}
	return res.Value.Get("located").Str(), nil
}
