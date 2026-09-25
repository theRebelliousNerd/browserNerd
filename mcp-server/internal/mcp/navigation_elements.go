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
// NAVIGATION / INTERACTION ELEMENT TOOLS
// =============================================================================

// GetInteractiveElementsTool extracts all actionable elements from the page.
// Returns a compact list of buttons, inputs, links, selects - ready for interaction.
type GetInteractiveElementsTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
	redactor *security.Redactor
}

func (t *GetInteractiveElementsTool) Name() string { return "get-interactive-elements" }
func (t *GetInteractiveElementsTool) Description() string {
	return `Discover all clickable/typeable elements on the page.

TOKEN COST: Medium (sparse rows; group=true collapses repeated controls into one row)

WHEN TO USE:
- Need to interact with forms, buttons, or inputs
- Discovering what actions are available
- Before using interact() tool

WHEN TO USE SOMETHING ELSE:
- Just need links/navigation -> get-navigation-links (lighter)
- Just need page status -> get-page-state (lightest)

EXAMPLE OUTPUT (group=true):
{
  "summary": {"total": 405, "returned": 4, "groups": 4, "types": {"button": 402, "input": 2, "link": 1}},
  "elements": [
    {"ref": "email", "type": "input", "label": "Email", "action": "type"},
    {"ref": "testid:save", "type": "button", "label": "Save", "action": "click"},
    {"ref": "button.row", "type": "button", "label": "Open", "action": "click", "count": 400, "refs": "button.row_1..button.row_399"},
    {"ref": "a[0]", "type": "link", "label": "Help", "action": "click", "href": "/help"}
  ]
}

FIELDS (sparse - only included when meaningful):
- ref: ID to use with interact tool (always present)
- type: button|input|link|select|combobox|option|tab|menuitem|checkbox|radio|row|listbox|clickable
  (clickable = a div/span the page made clickable via onclick, React onClick or cursor:pointer)
- label, action (click|type|select|toggle), value, href (links), checked, disabled
- dead: true - inside a React root but never rendered by React, so clicking it does nothing
- count + refs: group=true only; the other members of a repeated (type, label, action) group

summary.total is every matching element found; returned is how many rows came back.

OPTIONS:
- filter: 'all', 'buttons', 'inputs', 'links', 'selects'
- limit: Max rows (default: 50)
- group: Collapse repeated controls (default: false)
- verbose: Include fingerprint data (default: false, saves tokens)

Emits interactive() facts for Mangle reasoning (one per returned row), and dead_control() facts.`
}
func (t *GetInteractiveElementsTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session to query",
			},
			"filter": map[string]interface{}{
				"type":        "string",
				"description": "Filter by type: 'all', 'buttons', 'inputs', 'links', 'selects' (default: all)",
				"enum":        []string{"all", "buttons", "inputs", "links", "selects"},
			},
			"visible_only": map[string]interface{}{
				"type":        "boolean",
				"description": "Only return visible elements (default: true)",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Max rows to return (default: 50)",
			},
			"group": map[string]interface{}{
				"type":        "boolean",
				"description": "Collapse controls sharing type, label and action into one row with count and refs (default: false)",
			},
			"verbose": map[string]interface{}{
				"type":        "boolean",
				"description": "Include fingerprint data and alt_selectors (default: false, saves tokens)",
			},
		},
		"required": []string{"session_id"},
	}
}
func (t *GetInteractiveElementsTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}

	filter, err := normalizeInteractiveFilter(getStringArg(args, "filter"))
	if err != nil {
		return nil, err
	}
	visibleOnly := true
	if v, ok := args["visible_only"].(bool); ok {
		visibleOnly = v
	}
	limit := getIntArg(args, "limit", 50)
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	group := getBoolArg(args, "group", false)
	requestedVerbose := getBoolArg(args, "verbose", false)

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// Options travel as data: nothing an agent sends becomes script source.
	result, err := page.Eval(interactiveElementsScript, map[string]interface{}{
		"filter":      filter,
		"visibleOnly": visibleOnly,
		"limit":       limit,
		"group":       group,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to extract elements: %w", err)
	}
	data, ok := result.Value.Val().(map[string]interface{})
	if !ok {
		return result.Value.Val(), nil
	}
	t.recordElements(ctx, sessionID, data, visibleOnly, requestedVerbose)
	return data, nil
}

// recordElements emits the Mangle facts for every returned row, registers its
// fingerprint for the resolver, and strips the fingerprint from the row unless the
// caller asked for it. A grouped row stands for its whole group: facts describe its
// first member, and group_count records how many share its type, label and action.
func (t *GetInteractiveElementsTool) recordElements(ctx context.Context, sessionID string, data map[string]interface{}, visibleOnly, verbose bool) {
	elems, ok := data["elements"].([]interface{})
	if !ok {
		return
	}
	now := time.Now()
	facts := make([]mangle.Fact, 0, len(elems)*4)
	fingerprints := make([]*browser.ElementFingerprint, 0, len(elems))

	for _, e := range elems {
		elem, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		ref := getStringFromMap(elem, "ref")
		elemType := getStringFromMap(elem, "type")
		label := getStringFromMap(elem, "label")
		action := getStringFromMap(elem, "action")

		facts = append(facts, mangle.Fact{
			Predicate: "interactive",
			Args:      []interface{}{sessionID, ref, elemType, label, action},
			Timestamp: now,
		})

		enabled := "true"
		if disabled, ok := elem["disabled"].(bool); ok && disabled {
			enabled = "false"
		}
		facts = append(facts, mangle.Fact{
			Predicate: "element_enabled",
			Args:      []interface{}{sessionID, ref, enabled},
			Timestamp: now,
		})
		if visibleOnly {
			facts = append(facts, mangle.Fact{
				Predicate: "element_visible",
				Args:      []interface{}{sessionID, ref, "true"},
				Timestamp: now,
			})
		}
		if dead, ok := elem["dead"].(bool); ok && dead {
			facts = append(facts, mangle.Fact{
				Predicate: "dead_control",
				Args:      []interface{}{sessionID, ref, label},
				Timestamp: now,
			})
		}
		if count := asInt(elem["count"]); count > 1 {
			facts = append(facts, mangle.Fact{
				Predicate: "elem_attr",
				Args:      []interface{}{sessionID, ref, "group_count", fmt.Sprintf("%d", count)},
				Timestamp: now,
			})
		}

		if v := getStringFromMap(elem, "value"); v != "" {
			descriptor := strings.Join([]string{ref, elemType, label}, " ")
			if fpData, ok := elem["fingerprint"].(map[string]interface{}); ok {
				descriptor += " " + strings.Join([]string{
					getStringFromMap(fpData, "input_type"),
					getStringFromMap(fpData, "id"),
					getStringFromMap(fpData, "name"),
					getStringFromMap(fpData, "aria_label"),
				}, " ")
			}
			v = mandatoryRedactor(t.redactor).RedactInputValue(descriptor, v)
			facts = append(facts, mangle.Fact{
				Predicate: "element_value",
				Args:      []interface{}{sessionID, ref, v},
				Timestamp: now,
			})
		}

		fp := &browser.ElementFingerprint{
			Ref:         ref,
			GeneratedAt: now,
		}

		if fpData, ok := elem["fingerprint"].(map[string]interface{}); ok {
			fp.TagName = getStringFromMap(fpData, "tag_name")
			fp.ID = getStringFromMap(fpData, "id")
			fp.Name = getStringFromMap(fpData, "name")
			fp.AriaLabel = getStringFromMap(fpData, "aria_label")
			fp.DataTestID = getStringFromMap(fpData, "data_testid")
			fp.Role = getStringFromMap(fpData, "role")
			fp.RowKey = getStringFromMap(fpData, "row_key")
			fp.RowIndex = getStringFromMap(fpData, "row_index")
			fp.TextContent = getStringFromMap(fpData, "text_content")

			appendAttr := func(attrName, attrValue string) {
				if strings.TrimSpace(attrName) == "" || strings.TrimSpace(attrValue) == "" {
					return
				}
				facts = append(facts, mangle.Fact{
					Predicate: "elem_attr",
					Args:      []interface{}{sessionID, ref, attrName, attrValue},
					Timestamp: now,
				})
			}
			appendAttr("id", fp.ID)
			appendAttr("name", fp.Name)
			appendAttr("aria_label", fp.AriaLabel)
			appendAttr("data_testid", fp.DataTestID)
			appendAttr("role", fp.Role)
			appendAttr("row_key", fp.RowKey)
			appendAttr("row_index", fp.RowIndex)
			appendAttr("input_type", getStringFromMap(fpData, "input_type"))
			appendAttr("button_type", getStringFromMap(fpData, "button_type"))
			appendAttr("clickable", getStringFromMap(fpData, "clickable"))

			if classesRaw, ok := fpData["classes"].([]interface{}); ok {
				for i, c := range classesRaw {
					if i >= 5 {
						break
					}
					if classStr, ok := c.(string); ok {
						fp.Classes = append(fp.Classes, classStr)
						facts = append(facts, mangle.Fact{
							Predicate: "elem_class",
							Args:      []interface{}{sessionID, ref, classStr},
							Timestamp: now,
						})
					}
				}
			}

			if bbData, ok := fpData["bounding_box"].(map[string]interface{}); ok {
				fp.BoundingBox = make(map[string]float64)
				var x, y, w, h int
				for k, v := range bbData {
					if fv, ok := v.(float64); ok {
						fp.BoundingBox[k] = fv
						switch k {
						case "x":
							x = int(fv)
						case "y":
							y = int(fv)
						case "width":
							w = int(fv)
						case "height":
							h = int(fv)
						}
					}
				}
				facts = append(facts, mangle.Fact{
					Predicate: "elem_bbox",
					Args:      []interface{}{sessionID, ref, x, y, w, h},
					Timestamp: now,
				})
			}
		}

		if altSel, ok := elem["alt_selectors"].([]interface{}); ok {
			for _, s := range altSel {
				if str, ok := s.(string); ok {
					fp.AltSelectors = append(fp.AltSelectors, str)
				}
			}
		}

		fingerprints = append(fingerprints, fp)

		if !verbose {
			delete(elem, "fingerprint")
			delete(elem, "alt_selectors")
		}
	}

	if len(facts) > 0 && t.engine != nil {
		_ = t.engine.AddFacts(ctx, facts)
	}
	if registry := t.sessions.Registry(sessionID); registry != nil {
		registry.RegisterBatch(fingerprints)
	}
}

// interactiveElementsScript extracts interactive elements with sparse output. It takes
// {filter, visibleOnly, limit, group} as data. Refs, candidates and types come from
// elementRefScript, which screenshot overlays and the resolver share. summary.total counts
// every matching element; rows stop at limit.
const interactiveElementsScript = `(opts) => {` + elementRefScript + `
	const filter = opts.filter;
	const visibleOnly = !!opts.visibleOnly;
	const limit = opts.limit;
	const group = !!opts.group;

	const escapeCss = (value) => {
		const raw = String(value ?? '');
		if (window.CSS && typeof window.CSS.escape === 'function') {
			return window.CSS.escape(raw);
		}
		return raw.replace(/([ !"#$%&'()*+,./:;<=>?@[\\\]^{|}~])/g, '\\$1');
	};
	const escapeAttr = (value) => String(value ?? '')
		.replace(/\\/g, '\\\\')
		.replace(/"/g, '\\"');
	const isVisible = (el) => {
		const rect = el.getBoundingClientRect();
		const style = getComputedStyle(el);
		return !(rect.width === 0 || rect.height === 0 ||
			style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0');
	};

	const buildElement = (record, type, action, label, disabled, isDead) => {
		const { el, ref, tag, classes, role, dataTestId, ariaLabel, elId, elName, isRowLike, rowIndex, rowKeyRaw, clickable } = record;
		const elem = { ref, type, action };
		if (label) elem.label = label;

		// Never return a credential-bearing input value.
		if ((type === 'input' || type === 'select' || type === 'combobox') && el.value) {
			const descriptor = [
				el.type || '', el.name || '', el.id || '',
				el.autocomplete || '', label, ref
			].join(' ').toLowerCase();
			const sensitive = /password|passwd|secret|token|api.?key|one.?time.?code|cc.?number|cc.?csc|card.?number|cvv|cvc/.test(descriptor);
			const value = String(el.value);
			elem.value = sensitive ? '[REDACTED]' : (value.length > 200 ? value.substring(0, 197) + '...' : value);
		}
		if ((type === 'checkbox' || type === 'radio') && (el.checked || el.getAttribute('aria-checked') === 'true')) {
			elem.checked = true;
		}
		if (disabled) elem.disabled = true;
		if (isDead) elem.dead = true;
		if (type === 'link') {
			const href = __bnHref(el);
			if (href) elem.href = href;
		}

		// Internal fingerprint: Go turns it into facts and a registry entry, then strips it
		// unless the caller asked for verbose output.
		const rect = el.getBoundingClientRect();
		elem.fingerprint = {
			tag_name: tag,
			bounding_box: {
				x: Math.round(rect.x),
				y: Math.round(rect.y),
				width: Math.round(rect.width),
				height: Math.round(rect.height)
			}
		};
		if (elId) elem.fingerprint.id = elId;
		if (elName) elem.fingerprint.name = elName;
		if (ariaLabel) elem.fingerprint.aria_label = ariaLabel;
		if (dataTestId) elem.fingerprint.data_testid = dataTestId;
		if (role) elem.fingerprint.role = role;
		if (clickable) elem.fingerprint.clickable = clickable;
		if (rowIndex) elem.fingerprint.row_index = rowIndex;
		if (rowKeyRaw) elem.fingerprint.row_key = rowKeyRaw;
		const textContent = (el.innerText || '').replace(/\s+/g, ' ').trim().substring(0, 100);
		if (textContent) elem.fingerprint.text_content = textContent;
		if (classes.length > 0) elem.fingerprint.classes = classes.slice(0, 5);
		if (tag === 'input' && el.type) elem.fingerprint.input_type = el.type;
		// A submit button outside every form submits nothing, so it is reported as a plain
		// button: only a real form submit is a primary action.
		if (tag === 'button' || (tag === 'input' && ['submit', 'button', 'reset'].includes(el.type))) {
			elem.fingerprint.button_type = (el.type === 'submit' && !el.form) ? 'button' : (el.type || 'button');
		}

		const altSelectors = [];
		if (dataTestId) altSelectors.push('[data-testid="' + escapeAttr(dataTestId) + '"]');
		if (ariaLabel && ariaLabel.length < 100) altSelectors.push('[aria-label="' + escapeAttr(ariaLabel) + '"]');
		if (elId) altSelectors.push('#' + elId);
		if (elName) altSelectors.push('[name="' + escapeAttr(elName) + '"]');
		if (role) altSelectors.push('[role="' + escapeAttr(role) + '"]');
		if (isRowLike && rowIndex) {
			altSelectors.push('[role="row"][aria-rowindex="' + escapeAttr(rowIndex) + '"]');
			altSelectors.push('[role="row"][data-rowindex="' + escapeAttr(rowIndex) + '"]');
			altSelectors.push('tr[aria-rowindex="' + escapeAttr(rowIndex) + '"]');
			altSelectors.push('tr[data-rowindex="' + escapeAttr(rowIndex) + '"]');
			altSelectors.push('[row-index="' + escapeAttr(rowIndex) + '"]');
			altSelectors.push('[data-row-index="' + escapeAttr(rowIndex) + '"]');
		}
		if (isRowLike && rowKeyRaw) {
			altSelectors.push('[role="row"][data-row-id="' + escapeAttr(rowKeyRaw) + '"]');
			altSelectors.push('[role="row"][data-row-key="' + escapeAttr(rowKeyRaw) + '"]');
			altSelectors.push('tr[data-row-id="' + escapeAttr(rowKeyRaw) + '"]');
			altSelectors.push('tr[data-row-key="' + escapeAttr(rowKeyRaw) + '"]');
			altSelectors.push('[data-row-id="' + escapeAttr(rowKeyRaw) + '"]');
			altSelectors.push('[data-row-key="' + escapeAttr(rowKeyRaw) + '"]');
			altSelectors.push('[data-id="' + escapeAttr(rowKeyRaw) + '"]');
			altSelectors.push('[data-key="' + escapeAttr(rowKeyRaw) + '"]');
			altSelectors.push('[data-item-id="' + escapeAttr(rowKeyRaw) + '"]');
			altSelectors.push('[data-uid="' + escapeAttr(rowKeyRaw) + '"]');
		}
		if (classes.length > 0) {
			const classSelector = classes.slice(0, 3).map((cls) => '.' + escapeCss(cls)).join('');
			if (classSelector) altSelectors.push(tag + classSelector);
		}
		if (altSelectors.length > 0) elem.alt_selectors = Array.from(new Set(altSelectors));
		return elem;
	};

	// refRun names a group's other members: a numbered run as "first..last", anything
	// else as the first few refs and a count of the rest.
	const refRun = (members) => {
		const parse = (r) => {
			const m = /^(.*)_(\d+)$/.exec(r);
			return m ? [m[1], Number(m[2])] : [r, 0];
		};
		const [base, first] = parse(members[0]);
		const rest = members.slice(1);
		const consecutive = members.every((r, k) => {
			const [b, n] = parse(r);
			return b === base && n === first + k;
		});
		if (consecutive && rest.length > 2) return rest[0] + '..' + rest[rest.length - 1];
		const shown = rest.slice(0, 3).join(', ');
		return rest.length > 3 ? shown + ', +' + (rest.length - 3) + ' more' : shown;
	};

	const rows = [];
	const groups = new Map();
	const typeCount = {};
	const deadLabels = [];
	let total = 0, disabledCount = 0, unhydrated = false;

	__bnElementRefs().forEach((record) => {
		if (!__bnMatchesFilter(record, filter)) return;
		const el = record.el;
		if (visibleOnly && !isVisible(el)) return;
		const { type, action } = __bnClassify(record);
		const label = __bnAccessibleLabel(el);
		total++;
		typeCount[type] = (typeCount[type] || 0) + 1;
		const disabled = !!el.disabled || el.getAttribute('aria-disabled') === 'true';
		if (disabled) disabledCount++;
		// A link still navigates without React; every other control is inert if React
		// never attached to it.
		const isDead = (record.hydration === 'dead' || record.hydration === 'unhydrated') &&
			type !== 'link' && !el.hasAttribute('onclick');
		if (isDead) deadLabels.push(label || record.ref);
		if (record.hydration === 'unhydrated') unhydrated = true;

		if (group) {
			const key = type + '|' + action + '|' + (isDead ? 'dead|' : '') +
				(label ? label.toLowerCase() : record.ref.replace(/_\d+$/, ''));
			const existing = groups.get(key);
			if (existing) {
				existing.members.push(record.ref);
				return;
			}
			const entry = { members: [record.ref], elem: null };
			groups.set(key, entry);
			if (rows.length < limit) {
				entry.elem = buildElement(record, type, action, label, disabled, isDead);
				rows.push(entry);
			}
			return;
		}
		if (rows.length < limit) rows.push(buildElement(record, type, action, label, disabled, isDead));
	});

	const elements = group ? rows.map((entry) => {
		if (entry.members.length > 1) {
			entry.elem.count = entry.members.length;
			entry.elem.refs = refRun(entry.members);
		}
		return entry.elem;
	}) : rows;

	const summary = { total, returned: elements.length, types: typeCount };
	if (group) summary.groups = groups.size;
	if (disabledCount > 0) summary.disabled = disabledCount;
	const out = { summary, elements };
	if (deadLabels.length > 0) {
		out.dead_controls = { count: deadLabels.length, labels: Array.from(new Set(deadLabels)).slice(0, 10) };
		if (unhydrated) {
			out.dead_controls.page = 'React never attached to this page: its JavaScript did not run or failed before hydrating (check browser-reason for failed script requests or a failed WebSocket)';
		}
	}
	return out;
}`

func normalizeInteractiveFilter(raw string) (string, error) {
	filter := strings.ToLower(strings.TrimSpace(raw))
	if filter == "" {
		return "all", nil
	}
	switch filter {
	case "all", "buttons", "inputs", "links", "selects":
		return filter, nil
	default:
		return "", fmt.Errorf("invalid interactive element filter %q", raw)
	}
}

// DiscoverGridsTool identifies grid/table surfaces and row interaction strategies.
type DiscoverGridsTool struct {
	sessions *browser.SessionManager
}

func (t *DiscoverGridsTool) Name() string { return "discover-grids" }
func (t *DiscoverGridsTool) Description() string {
	return `Discover grid/table surfaces and robust row refs.
Returns grid type, row counts, preferred row strategy, and optional sample row refs.
Use returned refs with interact/browser-act.`
}
func (t *DiscoverGridsTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Session ID",
			},
			"max_grids": map[string]interface{}{
				"type":        "integer",
				"description": "Max grids (default 10)",
			},
			"sample_rows": map[string]interface{}{
				"type":        "integer",
				"description": "Sample rows per grid (default 3, max 10)",
			},
			"include_samples": map[string]interface{}{
				"type":        "boolean",
				"description": "Include sample row refs (default true)",
			},
		},
		"required": []string{"session_id"},
	}
}
func (t *DiscoverGridsTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}

	maxGrids := getIntArg(args, "max_grids", 10)
	if maxGrids <= 0 {
		maxGrids = 10
	}
	if maxGrids > 50 {
		maxGrids = 50
	}
	sampleRows := getIntArg(args, "sample_rows", 3)
	if sampleRows <= 0 {
		sampleRows = 3
	}
	if sampleRows > 10 {
		sampleRows = 10
	}
	includeSamples := getBoolArg(args, "include_samples", true)

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	js := fmt.Sprintf(`
	() => {
		const maxGrids = %d;
		const sampleRows = %d;
		const includeSamples = %v;

		const gridSelectors = [
			'[role="grid"]',
			'[role="treegrid"]',
			'[role="table"]',
			'table',
			'.ag-root',
			'.ag-center-cols-viewport',
			'.ag-body-viewport',
			'.MuiDataGrid-root',
			'.ReactVirtualized__Grid',
			'.ReactVirtualized__Table',
			'.rdg',
			'[data-grid]',
			'[data-testid*="grid"]',
			'[data-testid*="table"]'
		];
		const rowSelectors = [
			'[role="row"]',
			'tr',
			'.ag-row',
			'.MuiDataGrid-row',
			'[data-row-id]',
			'[data-row-key]',
			'[aria-rowindex]',
			'[data-rowindex]',
			'[row-index]',
			'[data-row-index]'
		];
		const rowKeyAttrs = ['data-row-id', 'data-row-key', 'data-id', 'data-key', 'data-item-id', 'data-uid'];
		const rowIndexAttrs = ['aria-rowindex', 'data-rowindex', 'row-index', 'data-row-index'];

		const escapeCss = (value) => {
			const raw = String(value ?? '');
			if (window.CSS && typeof window.CSS.escape === 'function') {
				return window.CSS.escape(raw);
			}
			return raw.replace(/([ !"#$%%&'()*+,./:;<=>?@[\\\]^{|}~])/g, '\\$1');
		};
		const encodeRefPart = (value) => encodeURIComponent(String(value ?? '')).substring(0, 120);
		const cleanText = (value) => String(value ?? '').replace(/\s+/g, ' ').trim();

		const isVisible = (el) => {
			if (!el) return false;
			const rect = el.getBoundingClientRect();
			const style = getComputedStyle(el);
			return !(rect.width === 0 || rect.height === 0 ||
				style.display === 'none' || style.visibility === 'hidden' || style.opacity === '0');
		};

		const detectGridType = (el) => {
			const cls = Array.from(el.classList || []);
			if (cls.includes('MuiDataGrid-root')) return 'mui-datagrid';
			if (cls.includes('ag-root') || cls.includes('ag-center-cols-viewport') || cls.includes('ag-body-viewport')) return 'ag-grid';
			if (cls.some((c) => c.startsWith('ReactVirtualized__'))) return 'react-virtualized';
			if (cls.includes('rdg')) return 'react-data-grid';
			const role = (el.getAttribute('role') || '').toLowerCase();
			if (role === 'grid' || role === 'treegrid') return 'aria-grid';
			if (role === 'table') return 'aria-table';
			if (el.tagName.toLowerCase() === 'table') return 'html-table';
			return 'generic-grid';
		};

		const readFirstAttr = (el, attrs) => {
			for (const attr of attrs) {
				const value = el.getAttribute(attr);
				if (value && String(value).trim() !== '') return [attr, String(value)];
			}
			return ['', ''];
		};

		const buildRowRef = (row, idx) => {
			const [keyAttr, keyVal] = readFirstAttr(row, rowKeyAttrs);
			if (keyVal) {
				return { ref: 'rowkey:' + encodeRefPart(keyVal), strategy: 'rowkey', key_attr: keyAttr };
			}

			const [indexAttr, indexVal] = readFirstAttr(row, rowIndexAttrs);
			if (indexVal) {
				return { ref: 'row:' + encodeRefPart(indexVal), strategy: 'rowindex', index_attr: indexAttr };
			}

			const dataTestId = row.getAttribute('data-testid') || row.getAttribute('data-test-id') || '';
			if (dataTestId) {
				return { ref: 'testid:' + dataTestId, strategy: 'testid' };
			}

			const rowId = row.id || row.getAttribute('id') || '';
			if (rowId) {
				return { ref: rowId, strategy: 'id_or_name' };
			}

			const rowName = row.getAttribute('name') || '';
			if (rowName) {
				return { ref: rowName, strategy: 'id_or_name' };
			}

			const tag = row.tagName.toLowerCase();
			const classes = Array.from(row.classList || []).slice(0, 2);
			if (classes.length > 0) {
				const classSelector = classes.map((cls) => escapeCss(cls)).join('.');
				return { ref: tag + '.' + classSelector, strategy: 'class_or_position' };
			}

			return { ref: tag + '[' + idx + ']', strategy: 'class_or_position' };
		};

		const selector = gridSelectors.join(', ');
		const allCandidates = Array.from(document.querySelectorAll(selector));

		const rootCandidates = [];
		const seenCandidates = new Set();
		for (const candidate of allCandidates) {
			if (rootCandidates.length >= maxGrids) break;
			if (seenCandidates.has(candidate)) continue;
			seenCandidates.add(candidate);

			const parentGrid = candidate.parentElement ? candidate.parentElement.closest(selector) : null;
			if (parentGrid) continue;
			rootCandidates.push(candidate);
		}

		const grids = [];
		for (let gridIdx = 0; gridIdx < rootCandidates.length; gridIdx++) {
			const gridEl = rootCandidates[gridIdx];
			const rowElementsRaw = Array.from(gridEl.querySelectorAll(rowSelectors.join(', ')));
			const seenRows = new Set();
			const rows = [];
			for (const row of rowElementsRaw) {
				if (seenRows.has(row)) continue;
				seenRows.add(row);
				rows.push(row);
			}
			if (rows.length === 0) continue;

			const visibleRows = rows.filter(isVisible);
			const probeRows = (visibleRows.length > 0 ? visibleRows : rows).slice(0, Math.max(sampleRows, 8));

			const keyAttrsSet = new Set();
			const indexAttrsSet = new Set();
			let hasTestId = false;
			let hasIdOrName = false;

			for (const row of probeRows) {
				const [keyAttr, keyVal] = readFirstAttr(row, rowKeyAttrs);
				if (keyVal) keyAttrsSet.add(keyAttr);

				const [indexAttr, indexVal] = readFirstAttr(row, rowIndexAttrs);
				if (indexVal) indexAttrsSet.add(indexAttr);

				const dataTestId = row.getAttribute('data-testid') || row.getAttribute('data-test-id') || '';
				if (dataTestId) hasTestId = true;

				const rowId = row.id || row.getAttribute('id') || '';
				const rowName = row.getAttribute('name') || '';
				if (rowId || rowName) hasIdOrName = true;
			}

			let preferredRowRef = 'class_or_position';
			if (keyAttrsSet.size > 0) preferredRowRef = 'rowkey';
			else if (indexAttrsSet.size > 0) preferredRowRef = 'rowindex';
			else if (hasTestId) preferredRowRef = 'testid';
			else if (hasIdOrName) preferredRowRef = 'id_or_name';

			const gridDataTestId = gridEl.getAttribute('data-testid') || gridEl.getAttribute('data-test-id') || '';
			const gridRole = gridEl.getAttribute('role') || '';
			const gridId = gridEl.id || '';
			let gridRef = '';
			if (gridDataTestId) gridRef = 'testid:' + gridDataTestId;
			else if (gridId) gridRef = gridId;
			else if (gridRole) gridRef = 'grid:' + gridRole + '_' + gridIdx;
			else gridRef = 'grid[' + gridIdx + ']';

			const style = getComputedStyle(gridEl);
			const overflowY = (style.overflowY || '').toLowerCase();
			const virtualized = (rows.length > visibleRows.length + 10) ||
				(gridEl.scrollHeight > (gridEl.clientHeight * 2)) ||
				((overflowY === 'auto' || overflowY === 'scroll') && visibleRows.length > 0 && rows.length <= visibleRows.length + 2 && indexAttrsSet.size > 0);

			const gridInfo = {
				grid_ref: gridRef,
				grid_type: detectGridType(gridEl),
				role: gridRole || null,
				row_count: rows.length,
				visible_row_count: visibleRows.length,
				virtualized,
				preferred_row_ref: preferredRowRef,
				row_key_attributes: Array.from(keyAttrsSet),
				row_index_attributes: Array.from(indexAttrsSet),
				grid_bbox: (() => {
					const rect = gridEl.getBoundingClientRect();
					return {
						x: Math.round(rect.x),
						y: Math.round(rect.y),
						width: Math.round(rect.width),
						height: Math.round(rect.height)
					};
				})()
			};

			if (includeSamples) {
				const samplePool = visibleRows.length > 0 ? visibleRows : rows;
				const sampleRowRefs = [];
				for (let i = 0; i < samplePool.length && sampleRowRefs.length < sampleRows; i++) {
					const row = samplePool[i];
					const built = buildRowRef(row, i);
					const rowText = cleanText(row.innerText || '').substring(0, 80);
					const sample = {
						ref: built.ref,
						strategy: built.strategy
					};
					if (built.key_attr) sample.key_attr = built.key_attr;
					if (built.index_attr) sample.index_attr = built.index_attr;
					if (rowText) sample.text_preview = rowText;
					sampleRowRefs.push(sample);
				}
				gridInfo.sample_row_refs = sampleRowRefs;
			}

			grids.push(gridInfo);
		}

		return {
			success: true,
			total_grids: grids.length,
			grids
		};
	}
	`, maxGrids, sampleRows, includeSamples)

	result, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("failed to discover grids: %w", err)
	}

	return result.Value.Val(), nil
}

// DiscoverHiddenContentTool finds and reports on collapsible/hidden content
type DiscoverHiddenContentTool struct {
	sessions *browser.SessionManager
}

func (t *DiscoverHiddenContentTool) Name() string { return "discover-hidden-content" }
func (t *DiscoverHiddenContentTool) Description() string {
	return `Discover what's inside collapsed accordions, hidden tabs, and disclosure widgets.

TOKEN COST: Medium

FINDS:
- <details> elements (collapsed/expanded state)
- Elements with aria-expanded attribute (accordion buttons)
- Hidden panels (display: none, visibility: hidden)
- Tab panels (role="tabpanel" with hidden state)
- Collapsible sections (common patterns: .collapse, .accordion, etc.)

RETURNS (sparse):
- type: details|aria-accordion|tab-panel|collapsible-div
- trigger: Text of the expand button/trigger
- state: collapsed|expanded|hidden
- ref: Element ref to expand it
- interactive_elements: Count (only if > 0)
- content_preview: First 100 chars (only if content exists)

OPTIONS:
- auto_expand: Automatically click all collapsed sections (default: false)

USE WHEN:
- Need to see what's in collapsed accordions
- Discovering options in settings panels
- Finding form fields in wizard steps`
}

func (t *DiscoverHiddenContentTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session ID (required)",
			},
			"auto_expand": map[string]interface{}{
				"type":        "boolean",
				"description": "Automatically expand all collapsible sections (default: false)",
			},
		},
		"required": []string{"session_id"},
	}
}

func (t *DiscoverHiddenContentTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}

	autoExpand := false
	if v, ok := args["auto_expand"].(bool); ok {
		autoExpand = v
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	js := fmt.Sprintf(`
	() => {
		const autoExpand = %v;
		const sections = [];

		// Pattern 1: <details> elements
		document.querySelectorAll('details').forEach((details, idx) => {
			const summary = details.querySelector('summary');
			const isOpen = details.hasAttribute('open');
			const contentEl = Array.from(details.children).find(el => el.tagName !== 'SUMMARY');
			const content = contentEl?.innerText?.trim()?.substring(0, 100) || '';
			const interactiveCount = details.querySelectorAll('button, a, input, select, textarea').length;

			const section = {
				type: 'details',
				trigger: summary?.innerText?.trim() || 'Details',
				state: isOpen ? 'expanded' : 'collapsed',
				ref: details.id || 'details-' + idx
			};
			if (content) section.content_preview = content;
			if (interactiveCount > 0) section.interactive_elements = interactiveCount;
			if (!isOpen) section.expandable = true;
			sections.push(section);

			if (autoExpand && !isOpen && summary) summary.click();
		});

		// Pattern 2: aria-expanded buttons
		document.querySelectorAll('[aria-expanded]').forEach((trigger, idx) => {
			const isExpanded = trigger.getAttribute('aria-expanded') === 'true';
			const controls = trigger.getAttribute('aria-controls');
			let panel = controls ? document.getElementById(controls) : null;
			const content = panel?.innerText?.trim()?.substring(0, 100) || '';
			const interactiveCount = panel?.querySelectorAll('button, a, input, select, textarea').length || 0;

			const section = {
				type: 'aria-accordion',
				trigger: trigger.innerText?.trim() || trigger.getAttribute('aria-label') || 'Accordion',
				state: isExpanded ? 'expanded' : 'collapsed',
				ref: trigger.id || 'accordion-' + idx
			};
			if (content) section.content_preview = content;
			if (interactiveCount > 0) section.interactive_elements = interactiveCount;
			if (!isExpanded && panel) section.expandable = true;
			sections.push(section);

			if (autoExpand && !isExpanded) trigger.click();
		});

		// Pattern 3: Hidden tab panels
		document.querySelectorAll('[role="tabpanel"]').forEach((panel, idx) => {
			const isHidden = panel.hidden || panel.getAttribute('aria-hidden') === 'true' || getComputedStyle(panel).display === 'none';
			if (!isHidden) return;

			const id = panel.id;
			const tab = document.querySelector('[aria-controls="' + id + '"]');
			const content = panel.innerText?.trim()?.substring(0, 100) || '';
			const interactiveCount = panel.querySelectorAll('button, a, input, select, textarea').length;

			const section = {
				type: 'tab-panel',
				trigger: tab?.innerText?.trim() || 'Tab ' + idx,
				state: 'hidden',
				ref: tab?.id || 'tab-' + idx
			};
			if (content) section.content_preview = content;
			if (interactiveCount > 0) section.interactive_elements = interactiveCount;
			if (tab) section.expandable = true;
			sections.push(section);

			if (autoExpand && tab) tab.click();
		});

		return {
			total: sections.length,
			auto_expanded: autoExpand,
			sections
		};
	}
	`, autoExpand)

	result, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("failed to discover hidden content: %w", err)
	}

	return result.Value.Val(), nil
}
