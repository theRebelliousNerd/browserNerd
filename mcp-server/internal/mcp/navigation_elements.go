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

TOKEN COST: Medium (returns compact element list with sparse JSON - empty fields omitted)

WHEN TO USE:
- Need to interact with forms, buttons, or inputs
- Discovering what actions are available
- Before using interact() tool

WHEN TO USE SOMETHING ELSE:
- Just need links/navigation -> get-navigation-links (lighter)
- Just need page status -> get-page-state (lightest)

EXAMPLE OUTPUT:
{
  "summary": {"total": 5, "types": {"button": 2, "input": 2, "link": 1}},
  "elements": [
    {"ref": "btn-0", "type": "button", "label": "Sign In", "action": "click"},
    {"ref": "input-1", "type": "input", "label": "Email", "action": "type"},
    {"ref": "input-2", "type": "input", "label": "Password", "action": "type", "value": ""},
    {"ref": "chk-3", "type": "checkbox", "label": "Remember me", "action": "toggle"},
    {"ref": "link-4", "type": "link", "label": "Forgot password?", "action": "click"}
  ]
}

SPARSE FIELDS (only included when non-empty):
- ref: ID to use with interact tool (always present)
- type: button|input|link|select|checkbox|radio (always present)
- label: Human-readable text
- action: Suggested action (click|type|select|toggle)
- value: Current value (inputs only)
- checked: true (checkboxes/radios only when checked)
- disabled: true (only when disabled)

OPTIONS:
- filter: 'all', 'buttons', 'inputs', 'links', 'selects'
- limit: Max elements (default: 50)
- verbose: Include fingerprint data (default: false, saves tokens)

Emits interactive() facts for Mangle reasoning.`
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
				"description": "Max elements to return (default: 50)",
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
	requestedVerbose := getBoolArg(args, "verbose", false)
	// Always extract richer metadata internally so Mangle can reason on it without
	// bloating the tool output. We'll strip verbose fields before returning when
	// requestedVerbose=false.
	internalVerbose := true

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// JavaScript to extract interactive elements with sparse output
	js := fmt.Sprintf(`
	(filter) => {
		const visibleOnly = %v;
		const limit = %d;
		const verbose = %v;

		const escapeCss = (value) => {
			const raw = String(value ?? '');
			if (window.CSS && typeof window.CSS.escape === 'function') {
				return window.CSS.escape(raw);
			}
			return raw.replace(/([ !"#$%%&'()*+,./:;<=>?@[\\\]^{|}~])/g, '\\$1');
		};

		const escapeAttr = (value) => String(value ?? '')
			.replace(/\\/g, '\\\\')
			.replace(/"/g, '\\"');

		const encodeRefPart = (value) => encodeURIComponent(String(value ?? '')).substring(0, 120);

		const selectors = {
			buttons: 'button, input[type="submit"], input[type="button"], [role="button"], [role="tab"], [role="menuitem"], [role="option"], [role="switch"], [role="checkbox"], [role="radio"], [role="row"][aria-rowindex], [role="row"][data-rowindex], [role="row"][data-row-id], [role="row"][data-row-key], [role="gridcell"][tabindex], tr[aria-rowindex], tr[data-rowindex], tr[data-row-id], tr[data-row-key], .ag-row, .MuiDataGrid-row',
			inputs: 'input:not([type="hidden"]):not([type="submit"]):not([type="button"]), textarea, [contenteditable="true"]',
			links: 'a[href]',
			selects: 'select, [role="combobox"], [role="listbox"]'
		};

		let selector;
		if (filter === 'all') {
			selector = Object.values(selectors).join(', ');
		} else {
			selector = selectors[filter] || Object.values(selectors).join(', ');
		}

		const elements = [];
		const seenCounts = new Map();

		document.querySelectorAll(selector).forEach((el, idx) => {
			if (elements.length >= limit) return;

			if (visibleOnly) {
				const rect = el.getBoundingClientRect();
				const style = getComputedStyle(el);
				if (rect.width === 0 || rect.height === 0 ||
				    style.display === 'none' || style.visibility === 'hidden' ||
				    style.opacity === '0') {
					return;
				}
			}

			const dataTestId = el.getAttribute('data-testid') || el.getAttribute('data-test-id') || '';
			const ariaLabel = el.getAttribute('aria-label') || '';
			const elId = el.id || '';
			const elName = el.name || '';
			const role = el.getAttribute('role') || '';
			const tag = el.tagName.toLowerCase();
			const classes = Array.from(el.classList);
			const rowClassLike = classes.some((cls) => cls === 'ag-row' || cls === 'MuiDataGrid-row' || cls === 'grid-row' || cls === 'table-row');
			const isRowLike = role === 'row' || tag === 'tr' || rowClassLike;
			const rowIndex = el.getAttribute('aria-rowindex') ||
			                 el.getAttribute('data-rowindex') ||
			                 el.getAttribute('row-index') ||
			                 el.getAttribute('data-row-index') ||
			                 '';
			const rowKeyRaw = el.getAttribute('data-row-id') ||
			                  el.getAttribute('data-row-key') ||
			                  el.getAttribute('data-id') ||
			                  el.getAttribute('data-key') ||
			                  el.getAttribute('data-item-id') ||
			                  el.getAttribute('data-uid') ||
			                  '';

			// Generate ref
			let ref;
			if (dataTestId) {
				ref = 'testid:' + dataTestId;
			} else if (ariaLabel && ariaLabel.length < 50) {
				ref = 'aria:' + ariaLabel.replace(/[^a-zA-Z0-9_-]/g, '_').substring(0, 40);
			} else if (elId) {
				ref = elId;
			} else if (elName) {
				ref = elName;
			} else if (isRowLike && rowKeyRaw) {
				ref = 'rowkey:' + encodeRefPart(rowKeyRaw);
			} else if (isRowLike && rowIndex) {
				ref = 'row:' + encodeRefPart(rowIndex);
			} else {
				const classStr = classes.slice(0, 2).join('.');
				ref = classStr ? tag + '.' + classStr : tag + '[' + idx + ']';
			}

			const baseRef = ref;
			const dupCount = seenCounts.get(baseRef) || 0;
			if (dupCount > 0) {
				ref = baseRef + '_' + dupCount;
			}
			seenCounts.set(baseRef, dupCount + 1);

			// Determine type and action
			let type, action;
			if (tag === 'button' || el.type === 'submit' || el.type === 'button' || role === 'button') {
				type = 'button';
				action = 'click';
			} else if (role === 'checkbox' || role === 'radio' || role === 'switch') {
				type = role === 'switch' ? 'checkbox' : role;
				action = 'toggle';
			} else if (tag === 'a') {
				type = 'link';
				action = 'click';
			} else if (tag === 'select' || role === 'combobox' || role === 'listbox') {
				type = 'select';
				action = 'select';
			} else if (isRowLike) {
				type = 'row';
				action = 'click';
			} else if (tag === 'input') {
				const inputType = el.type || 'text';
				if (inputType === 'checkbox' || inputType === 'radio') {
					type = inputType;
					action = 'toggle';
				} else {
					type = 'input';
					action = 'type';
				}
			} else if (tag === 'textarea' || el.contentEditable === 'true') {
				type = 'input';
				action = 'type';
			} else {
				type = 'clickable';
				action = 'click';
			}

			// Get label
			let label = el.getAttribute('aria-label') ||
			           el.innerText?.trim()?.substring(0, 50) ||
			           el.placeholder ||
			           el.title ||
			           el.alt ||
			           '';
			label = label.replace(/\\s+/g, ' ').trim();
			if (label.length > 50) label = label.substring(0, 47) + '...';

			// Build SPARSE element object - only include non-empty/meaningful fields
			const elem = { ref, type, action };

			// Only include label if non-empty
			if (label) elem.label = label;

			// Never return a credential-bearing input value.
			if ((type === 'input' || type === 'select') && el.value) {
				const descriptor = [
					el.type || '', el.name || '', el.id || '',
					el.autocomplete || '', label, ref
				].join(' ').toLowerCase();
				const sensitive = /password|passwd|secret|token|api.?key|one.?time.?code|cc.?number|cc.?csc|card.?number|cvv|cvc/.test(descriptor);
				elem.value = sensitive ? '[REDACTED]' : el.value;
			}

			// Only include checked for checkboxes/radios
			if ((type === 'checkbox' || type === 'radio') && el.checked) {
				elem.checked = true;
			}

			// Only include disabled if true
			if (el.disabled) {
				elem.disabled = true;
			}

			// Only include verbose data if requested
			if (verbose) {
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
				// Only add non-empty fingerprint fields
				if (elId) elem.fingerprint.id = elId;
				if (elName) elem.fingerprint.name = elName;
				if (ariaLabel) elem.fingerprint.aria_label = ariaLabel;
				if (dataTestId) elem.fingerprint.data_testid = dataTestId;
				if (role) elem.fingerprint.role = role;
				if (rowIndex) elem.fingerprint.row_index = rowIndex;
				if (rowKeyRaw) elem.fingerprint.row_key = rowKeyRaw;
				const textContent = (el.innerText || '').replace(/\s+/g, ' ').trim().substring(0, 100);
				if (textContent) elem.fingerprint.text_content = textContent;
				if (classes.length > 0) elem.fingerprint.classes = classes.slice(0, 5);
				if (tag === 'input' && el.type) elem.fingerprint.input_type = el.type;
				if (tag === 'button' && el.type) elem.fingerprint.button_type = el.type;
				if (tag === 'input' && (el.type === 'submit' || el.type === 'button' || el.type === 'reset')) {
					elem.fingerprint.button_type = el.type;
				}

				// Build alt_selectors only in verbose mode
				const altSelectors = [];
				if (dataTestId) altSelectors.push('[data-testid="' + escapeAttr(dataTestId) + '"]');
				if (ariaLabel && ariaLabel.length < 100) altSelectors.push('[aria-label="' + escapeAttr(ariaLabel) + '"]');
				if (elId) altSelectors.push('#' + elId);
				if (elName) altSelectors.push('[name="' + escapeAttr(elName) + '"]');
				if (role) {
					altSelectors.push('[role="' + escapeAttr(role) + '"]');
				}
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
					if (classSelector) {
						altSelectors.push(tag + classSelector);
					}
				}
				if (altSelectors.length > 0) elem.alt_selectors = Array.from(new Set(altSelectors));
			}

			elements.push(elem);
		});

		// Build compact summary
		const typeCount = {};
		let disabledCount = 0;
		elements.forEach(el => {
			typeCount[el.type] = (typeCount[el.type] || 0) + 1;
			if (el.disabled) disabledCount++;
		});

		const summary = {
			total: elements.length,
			types: typeCount
		};
		if (disabledCount > 0) summary.disabled = disabledCount;

		return {
			summary,
			elements
		};
	}
	`, visibleOnly, limit, internalVerbose)

	result, err := page.Eval(js, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to extract elements: %w", err)
	}

	// Emit Mangle facts and register fingerprints (always, for interact tool)
	if data, ok := result.Value.Val().(map[string]interface{}); ok {
		if elems, ok := data["elements"].([]interface{}); ok {
			now := time.Now()
			// We can emit multiple facts per element (enabled state, bbox, attrs, etc.).
			facts := make([]mangle.Fact, 0, len(elems)*4)
			fingerprints := make([]*browser.ElementFingerprint, 0, len(elems))

			for _, e := range elems {
				if elem, ok := e.(map[string]interface{}); ok {
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
					if disabledRaw, ok := elem["disabled"]; ok {
						if disabled, ok := disabledRaw.(bool); ok && disabled {
							enabled = "false"
						}
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

					// Maintain the original token-efficient output behavior.
					// When requestedVerbose=false, strip internal verbose fields before returning.
					if !requestedVerbose {
						delete(elem, "fingerprint")
						delete(elem, "alt_selectors")
					}
				}
			}

			if len(facts) > 0 {
				_ = t.engine.AddFacts(ctx, facts)
			}

			if registry := t.sessions.Registry(sessionID); registry != nil {
				registry.RegisterBatch(fingerprints)
			}
		}
	}

	return result.Value.Val(), nil
}

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

// InteractTool performs actions on elements
type InteractTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
	redactor *security.Redactor
}

func (t *InteractTool) Name() string { return "interact" }
func (t *InteractTool) Description() string {
	return `Perform actions on page elements (click, type, select, toggle, clear).

TOKEN COST: Low (single action)

GET REFS FROM: get-interactive-elements (run it first to get element refs)

ACTIONS:
- click: Click button/link
- type: Enter text in input (clears first)
- select: Choose dropdown option by visible text
- toggle: Check/uncheck checkbox or radio
- clear: Clear input field

EXAMPLE OUTPUT (click):
{"success": true, "ref": "btn-0", "action": "click"}

EXAMPLE OUTPUT (type):
{"success": true, "ref": "input-1", "action": "type", "value": "user@example.com"}

EXAMPLE OUTPUT (toggle checkbox):
{"success": true, "ref": "chk-3", "action": "toggle", "checked": true}

EXAMPLE OUTPUT (select dropdown):
{"success": true, "ref": "select-5", "action": "select", "value": "Option 2"}

FOR MULTIPLE FIELDS: Use fill-form instead (more efficient).

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
			"ref": map[string]interface{}{
				"type":        "string",
				"description": "Element ref from get-interactive-elements",
			},
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action: click, type, select, toggle, clear",
				"enum":        []string{"click", "type", "select", "toggle", "clear"},
			},
			"value": map[string]interface{}{
				"type":        "string",
				"description": "Value for type/select actions",
			},
			"submit": map[string]interface{}{
				"type":        "boolean",
				"description": "Press Enter after typing (default: false)",
			},
		},
		"required": []string{"session_id", "ref", "action"},
	}
}
func (t *InteractTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	ref := getStringArg(args, "ref")
	action := getStringArg(args, "action")
	value := getStringArg(args, "value")
	submit := false
	if v, ok := args["submit"].(bool); ok {
		submit = v
	}

	if sessionID == "" || ref == "" || action == "" {
		return nil, fmt.Errorf("session_id, ref, and action are required")
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	registry := t.sessions.Registry(sessionID)
	element, err := findElementByRefWithRegistry(page, ref, registry)
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}

	var validationWarnings []string
	descriptor := ref
	if registry != nil {
		if fp := registry.Get(ref); fp != nil {
			validation := validateFingerprint(element, fp)
			if len(validation.Changes) > 0 {
				validationWarnings = validation.Changes
			}
			descriptor += " " + strings.Join([]string{
				fp.TagName, fp.ID, fp.Name, fp.AriaLabel, fp.DataTestID, fp.Role,
			}, " ")
		}
	}
	for _, attribute := range []string{"type", "name", "id", "autocomplete", "aria-label"} {
		if attrValue, attrErr := element.Attribute(attribute); attrErr == nil && attrValue != nil {
			descriptor += " " + *attrValue
		}
	}
	safeValue := mandatoryRedactor(t.redactor).RedactInputValue(descriptor, value)
	sensitiveValue := safeValue == security.Redacted

	visible, err := element.Visible()
	if err != nil || !visible {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("Element not visible: %s", ref)}, nil
	}

	var resultValue string
	var resultChecked bool

	switch action {
	case "click":
		if err := element.Timeout(5*time.Second).Click("left", 1); err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("Click failed: %v", err)}, nil
		}

	case "type":
		if err := element.Timeout(5 * time.Second).SelectAllText(); err == nil {
			_ = element.Timeout(5 * time.Second).Input("")
		}
		if err := element.Timeout(5 * time.Second).Input(value); err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("Type failed: %v", err)}, nil
		}
		if submit {
			if err := page.Keyboard.Press('\r'); err != nil {
				return map[string]interface{}{"success": false, "error": fmt.Sprintf("Submit failed: %v", err)}, nil
			}
		}
		if propVal, err := element.Property("value"); err == nil {
			resultValue = propVal.Str()
		}

	case "select":
		tagNameProp, _ := element.Property("tagName")
		tagName := tagNameProp.Str()
		if tagName == "SELECT" {
			if err := selectOption(element, value); err != nil {
				return map[string]interface{}{"success": false, "error": fmt.Sprintf("Option not found: %s", value)}, nil
			}
		} else {
			if err := element.Timeout(5*time.Second).Click("left", 1); err != nil {
				return map[string]interface{}{"success": false, "error": fmt.Sprintf("Select click failed: %v", err)}, nil
			}
		}
		if propVal, err := element.Property("value"); err == nil {
			resultValue = propVal.Str()
		}

	case "toggle":
		if err := element.Timeout(5*time.Second).Click("left", 1); err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("Toggle failed: %v", err)}, nil
		}
		if checkedProp, err := element.Property("checked"); err == nil {
			resultChecked = checkedProp.Bool()
		}

	case "clear":
		if err := element.Timeout(5 * time.Second).SelectAllText(); err == nil {
			_ = element.Timeout(5 * time.Second).Input("")
		}
		resultValue = ""
	}

	// Emit Mangle fact
	now := time.Now()
	var predicate string
	var factArgs []interface{}
	switch action {
	case "click":
		predicate = "user_click"
		factArgs = []interface{}{sessionID, ref, now.UnixMilli()}
	case "type":
		predicate = "user_type"
		factArgs = []interface{}{sessionID, ref, safeValue, now.UnixMilli()}
	case "select":
		predicate = "user_select"
		factArgs = []interface{}{sessionID, ref, safeValue, now.UnixMilli()}
	case "toggle":
		predicate = "user_toggle"
		factArgs = []interface{}{sessionID, ref, now.UnixMilli()}
	}
	if predicate != "" && t.engine != nil {
		_ = t.engine.AddFacts(ctx, []mangle.Fact{{Predicate: predicate, Args: factArgs, Timestamp: now}})
	}

	// Build sparse result
	result := map[string]interface{}{"success": true, "ref": ref, "action": action}
	if resultValue != "" {
		if sensitiveValue {
			result["value"] = security.Redacted
		} else {
			result["value"] = mandatoryRedactor(t.redactor).SanitizeString(resultValue)
		}
	}
	if action == "toggle" {
		result["checked"] = resultChecked
	}
	if len(validationWarnings) > 0 {
		result["warning"] = "Element properties changed since discovery"
		result["changes"] = validationWarnings
	}

	return result, nil
}

func mandatoryRedactor(redactor *security.Redactor) *security.Redactor {
	if redactor != nil {
		return redactor
	}
	return security.NewRedactor(nil)
}
