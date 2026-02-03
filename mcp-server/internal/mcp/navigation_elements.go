package mcp

import (
	"context"
	"fmt"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

// =============================================================================
// NAVIGATION / INTERACTION ELEMENT TOOLS
// =============================================================================

// GetInteractiveElementsTool extracts all actionable elements from the page.
// Returns a compact list of buttons, inputs, links, selects - ready for interaction.
type GetInteractiveElementsTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
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

	filter := getStringArg(args, "filter")
	if filter == "" {
		filter = "all"
	}
	visibleOnly := true
	if v, ok := args["visible_only"].(bool); ok {
		visibleOnly = v
	}
	limit := getIntArg(args, "limit", 50)
	verbose := getBoolArg(args, "verbose", false)

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// JavaScript to extract interactive elements with sparse output
	js := fmt.Sprintf(`
	() => {
		const filter = '%s';
		const visibleOnly = %v;
		const limit = %d;
		const verbose = %v;

		const selectors = {
			buttons: 'button, input[type="submit"], input[type="button"], [role="button"]',
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
		const seen = new Set();

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
			} else {
				const classStr = classes.slice(0, 2).join('.');
				ref = classStr ? tag + '.' + classStr : tag + '[' + idx + ']';
			}

			if (seen.has(ref)) {
				ref = ref + '_' + idx;
			}
			seen.add(ref);

			// Determine type and action
			let type, action;
			if (tag === 'button' || el.type === 'submit' || el.type === 'button' || role === 'button') {
				type = 'button';
				action = 'click';
			} else if (tag === 'a') {
				type = 'link';
				action = 'click';
			} else if (tag === 'select' || role === 'combobox' || role === 'listbox') {
				type = 'select';
				action = 'select';
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

			// Only include value for inputs with actual values
			if ((type === 'input' || type === 'select') && el.value) {
				elem.value = el.value;
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
				if (classes.length > 0) elem.fingerprint.classes = classes.slice(0, 5);

				// Build alt_selectors only in verbose mode
				const altSelectors = [];
				if (dataTestId) altSelectors.push('[data-testid="' + dataTestId + '"]');
				if (ariaLabel && ariaLabel.length < 100) altSelectors.push('[aria-label="' + ariaLabel.replace(/"/g, '\\\\"') + '"]');
				if (elId) altSelectors.push('#' + elId);
				if (elName) altSelectors.push('[name="' + elName + '"]');
				if (altSelectors.length > 0) elem.alt_selectors = altSelectors;
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
	`, filter, visibleOnly, limit, verbose)

	result, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("failed to extract elements: %w", err)
	}

	// Emit Mangle facts and register fingerprints (always, for interact tool)
	if data, ok := result.Value.Val().(map[string]interface{}); ok {
		if elems, ok := data["elements"].([]interface{}); ok {
			now := time.Now()
			facts := make([]mangle.Fact, 0, len(elems))
			fingerprints := make([]*browser.ElementFingerprint, 0, len(elems))

			for _, e := range elems {
				if elem, ok := e.(map[string]interface{}); ok {
					ref := getStringFromMap(elem, "ref")
					elemType := getStringFromMap(elem, "type")
					label := getStringFromMap(elem, "label")
					action := getStringFromMap(elem, "action")

					facts = append(facts, mangle.Fact{
						Predicate: "interactive",
						Args:      []interface{}{ref, elemType, label, action},
						Timestamp: now,
					})

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

						if classesRaw, ok := fpData["classes"].([]interface{}); ok {
							for _, c := range classesRaw {
								if classStr, ok := c.(string); ok {
									fp.Classes = append(fp.Classes, classStr)
								}
							}
						}

						if bbData, ok := fpData["bounding_box"].(map[string]interface{}); ok {
							fp.BoundingBox = make(map[string]float64)
							for k, v := range bbData {
								if fv, ok := v.(float64); ok {
									fp.BoundingBox[k] = fv
								}
							}
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
	if registry != nil {
		if fp := registry.Get(ref); fp != nil {
			validation := validateFingerprint(element, fp)
			if len(validation.Changes) > 0 {
				validationWarnings = validation.Changes
			}
		}
	}

	visible, err := element.Visible()
	if err != nil || !visible {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("Element not visible: %s", ref)}, nil
	}

	var resultValue string
	var resultChecked bool

	switch action {
	case "click":
		if err := element.Click("left", 1); err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("Click failed: %v", err)}, nil
		}

	case "type":
		if err := element.SelectAllText(); err == nil {
			_ = element.Input("")
		}
		if err := element.Input(value); err != nil {
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
			if err := element.Select([]string{value}, true, "value"); err != nil {
				if err := element.Select([]string{value}, true, "text"); err != nil {
					return map[string]interface{}{"success": false, "error": fmt.Sprintf("Option not found: %s", value)}, nil
				}
			}
		} else {
			if err := element.Click("left", 1); err != nil {
				return map[string]interface{}{"success": false, "error": fmt.Sprintf("Select click failed: %v", err)}, nil
			}
		}
		if propVal, err := element.Property("value"); err == nil {
			resultValue = propVal.Str()
		}

	case "toggle":
		if err := element.Click("left", 1); err != nil {
			return map[string]interface{}{"success": false, "error": fmt.Sprintf("Toggle failed: %v", err)}, nil
		}
		if checkedProp, err := element.Property("checked"); err == nil {
			resultChecked = checkedProp.Bool()
		}

	case "clear":
		if err := element.SelectAllText(); err == nil {
			_ = element.Input("")
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
		factArgs = []interface{}{ref, now.UnixMilli()}
	case "type":
		predicate = "user_type"
		factArgs = []interface{}{ref, value, now.UnixMilli()}
	case "select":
		predicate = "user_select"
		factArgs = []interface{}{ref, value, now.UnixMilli()}
	case "toggle":
		predicate = "user_toggle"
		factArgs = []interface{}{ref, now.UnixMilli()}
	}
	if predicate != "" {
		_ = t.engine.AddFacts(ctx, []mangle.Fact{{Predicate: predicate, Args: factArgs, Timestamp: now}})
	}

	// Build sparse result
	result := map[string]interface{}{"success": true, "ref": ref, "action": action}
	if resultValue != "" {
		result["value"] = resultValue
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
