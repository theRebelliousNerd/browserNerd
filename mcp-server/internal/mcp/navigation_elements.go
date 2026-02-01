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

CALL THIS to understand what you can interact with:
- Buttons (including [role="button"])
- Inputs (text, checkbox, radio, etc.)
- Links (<a> tags)
- Selects (dropdowns)

RETURNS for each element:
- ref: ID to use with interact tool
- type: button|input|link|select|checkbox|radio
- label: Human-readable text
- action: Suggested action (click|type|select|toggle)

WORKFLOW:
1. get-interactive-elements -> See what's available
2. interact(ref, action, value) -> Act on specific element

USE get-navigation-links INSTEAD if you only need links (more token-efficient).

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

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// JavaScript to extract interactive elements
	js := fmt.Sprintf(`
	() => {
		const filter = '%s';
		const visibleOnly = %v;
		const limit = %d;

		// Selectors for interactive elements
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

			// Skip hidden elements if visibleOnly
			if (visibleOnly) {
				const rect = el.getBoundingClientRect();
				const style = getComputedStyle(el);
				if (rect.width === 0 || rect.height === 0 ||
				    style.display === 'none' || style.visibility === 'hidden' ||
				    style.opacity === '0') {
					return;
				}
			}

			// Capture fingerprint data for reliable re-identification
			const dataTestId = el.getAttribute('data-testid') || el.getAttribute('data-test-id') || '';
			const ariaLabel = el.getAttribute('aria-label') || '';
			const elId = el.id || '';
			const elName = el.name || '';
			const role = el.getAttribute('role') || '';
			const tag = el.tagName.toLowerCase();
			const classes = Array.from(el.classList);
			const textContent = (el.innerText?.trim()?.substring(0, 100) || '');
			const rect = el.getBoundingClientRect();
			const boundingBox = {
				x: Math.round(rect.x),
				y: Math.round(rect.y),
				width: Math.round(rect.width),
				height: Math.round(rect.height)
			};

			// Generate ref with priority: data-testid > aria-label > id > name > generated
			let ref;
			if (dataTestId) {
				// Most stable - explicit test ID
				ref = 'testid:' + dataTestId;
			} else if (ariaLabel && ariaLabel.length < 50) {
				// Accessibility label - usually stable
				ref = 'aria:' + ariaLabel.replace(/[^a-zA-Z0-9_-]/g, '_').substring(0, 40);
			} else if (elId) {
				// Element ID
				ref = elId;
			} else if (elName) {
				// Form element name
				ref = elName;
			} else {
				// Fallback: tag + first 2 classes or index
				const classStr = classes.slice(0, 2).join('.');
				ref = classStr ? tag + '.' + classStr : tag + '[' + idx + ']';
			}

			// Avoid duplicates by appending index
			if (seen.has(ref)) {
				ref = ref + '_' + idx;
			}
			seen.add(ref);

			// Determine element type and action
			let type, action;

			if (tag === 'button' || el.type === 'submit' || el.type === 'button' || el.getAttribute('role') === 'button') {
				type = 'button';
				action = 'click';
			} else if (tag === 'a') {
				type = 'link';
				action = 'click';
			} else if (tag === 'select' || el.getAttribute('role') === 'combobox' || el.getAttribute('role') === 'listbox') {
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

			// Get label (prefer aria-label, then text content, then placeholder)
			let label = el.getAttribute('aria-label') ||
			           el.innerText?.trim()?.substring(0, 50) ||
			           el.placeholder ||
			           el.title ||
			           el.alt ||
			           '';

			// Clean up label
			label = label.replace(/\s+/g, ' ').trim();
			if (label.length > 50) label = label.substring(0, 47) + '...';

			// Build alternative selectors for fallback re-identification
			const altSelectors = [];
			if (dataTestId) {
				altSelectors.push('[data-testid="' + dataTestId + '"]');
			}
			if (ariaLabel && ariaLabel.length < 100) {
				altSelectors.push('[aria-label="' + ariaLabel.replace(/"/g, '\\"') + '"]');
			}
			if (elId) {
				altSelectors.push('#' + elId);
			}
			if (elName) {
				altSelectors.push('[name="' + elName + '"]');
			}
			if (role) {
				altSelectors.push('[role="' + role + '"]');
			}
			// Add class-based selector as last resort
			if (classes.length > 0) {
				altSelectors.push(tag + '.' + classes.slice(0, 3).join('.'));
			}

			elements.push({
				ref: ref,
				type: type,
				label: label,
				action: action,
				value: el.value || '',
				enabled: !el.disabled,
				checked: el.checked || false,
				// Alternative selectors for fallback if primary ref fails
				alt_selectors: altSelectors.slice(0, 4), // Top 4 alternatives
				// Fingerprint data for reliable re-identification
				fingerprint: {
					tag_name: tag,
					id: elId,
					name: elName,
					classes: classes.slice(0, 5), // First 5 classes
					text_content: textContent,
					aria_label: ariaLabel,
					data_testid: dataTestId,
					role: role,
					bounding_box: boundingBox
				}
			});
		});

		// Categorize elements for progressive disclosure index
		const categories = {
			navigation: [],
			settings: [],
			form_controls: [],
			action_buttons: [],
			file_upload: [],
			disabled: []
		};

		const typeCount = { buttons: 0, inputs: 0, links: 0, selects: 0, checkboxes: 0, radios: 0 };
		let enabledCount = 0;
		let disabledCount = 0;

		elements.forEach((el, idx) => {
			// Count by type
			if (el.type === 'button') typeCount.buttons++;
			else if (el.type === 'input') typeCount.inputs++;
			else if (el.type === 'link') typeCount.links++;
			else if (el.type === 'select') typeCount.selects++;
			else if (el.type === 'checkbox') typeCount.checkboxes++;
			else if (el.type === 'radio') typeCount.radios++;

			// Count by state
			if (el.enabled) enabledCount++;
			else disabledCount++;

			// Categorize by purpose
			const label = (el.label || '').toLowerCase();

			// Navigation keywords
			if (label.match(/\b(home|studio|presentations|research|workflow|trace|reviews|nav|menu|dashboard)\b/)) {
				categories.navigation.push(idx);
			}

			// Settings/configuration keywords
			if (label.match(/\b(settings|config|preferences|advanced|clarity|options)\b/)) {
				categories.settings.push(idx);
			}

			// Form control keywords (dropdowns, inputs with specific purposes)
			if (label.match(/\b(length|mood|audience|industry|style|formality|energy|template)\b/) ||
			    el.type === 'select' && !categories.settings.includes(idx)) {
				categories.form_controls.push(idx);
			}

			// Action button keywords
			if (label.match(/\b(save|submit|reset|start|create|cancel|continue|next|back|delete|edit)\b/)) {
				categories.action_buttons.push(idx);
			}

			// File upload keywords
			if (label.match(/\b(select files|select folder|upload|attach|browse)\b/)) {
				categories.file_upload.push(idx);
			}

			// Disabled elements
			if (!el.enabled) {
				categories.disabled.push(idx);
			}
		});

		// Build summary with quick index
		const summary = {
			total_elements: elements.length,
			by_type: typeCount,
			by_category: {},
			by_state: {
				enabled: enabledCount,
				disabled: disabledCount
			}
		};

		// Add category info with counts and element indices
		Object.keys(categories).forEach(cat => {
			if (categories[cat].length > 0) {
				summary.by_category[cat] = {
					count: categories[cat].length,
					indices: categories[cat]
				};
			}
		});

		return {
			summary: summary,
			url: window.location.href,
			title: document.title,
			count: elements.length,
			elements: elements
		};
	}
	`, filter, visibleOnly, limit)

	result, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("failed to extract elements: %w", err)
	}

	// Emit Mangle facts for the interactive elements (for RCA later)
	// Also register elements in the session's ElementRegistry for reliable re-finding
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

					// Create Mangle fact
					facts = append(facts, mangle.Fact{
						Predicate: "interactive",
						Args:      []interface{}{ref, elemType, label, action},
						Timestamp: now,
					})

					// Extract fingerprint data for registry
					fp := &browser.ElementFingerprint{
						Ref:         ref,
						GeneratedAt: now,
					}

					// Parse fingerprint data from JavaScript result
					if fpData, ok := elem["fingerprint"].(map[string]interface{}); ok {
						fp.TagName = getStringFromMap(fpData, "tag_name")
						fp.ID = getStringFromMap(fpData, "id")
						fp.Name = getStringFromMap(fpData, "name")
						fp.TextContent = getStringFromMap(fpData, "text_content")
						fp.AriaLabel = getStringFromMap(fpData, "aria_label")
						fp.DataTestID = getStringFromMap(fpData, "data_testid")
						fp.Role = getStringFromMap(fpData, "role")

						// Extract classes array
						if classesRaw, ok := fpData["classes"].([]interface{}); ok {
							for _, c := range classesRaw {
								if classStr, ok := c.(string); ok {
									fp.Classes = append(fp.Classes, classStr)
								}
							}
						}

						// Extract bounding box
						if bbData, ok := fpData["bounding_box"].(map[string]interface{}); ok {
							fp.BoundingBox = make(map[string]float64)
							for k, v := range bbData {
								if fv, ok := v.(float64); ok {
									fp.BoundingBox[k] = fv
								}
							}
						}
					}

					// Extract alt_selectors for fallback element lookup
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

			// Add Mangle facts
			if len(facts) > 0 {
				_ = t.engine.AddFacts(ctx, facts)
			}

			// Register elements in session registry
			if registry := t.sessions.Registry(sessionID); registry != nil {
				registry.RegisterBatch(fingerprints)
			}
		}
	}

	return result.Value.Val(), nil
}

// DiscoverHiddenContentTool finds and reports on collapsible/hidden content (accordions, details, tabs, etc.)
type DiscoverHiddenContentTool struct {
	sessions *browser.SessionManager
}

func (t *DiscoverHiddenContentTool) Name() string { return "discover-hidden-content" }
func (t *DiscoverHiddenContentTool) Description() string {
	return `Discover what's inside collapsed accordions, hidden tabs, and disclosure widgets.

FINDS:
- <details> elements (collapsed/expanded state)
- Elements with aria-expanded attribute (accordion buttons)
- Hidden panels (display: none, visibility: hidden)
- Tab panels (role="tabpanel" with hidden state)
- Collapsible sections (common patterns: .collapse, .accordion, etc.)

FOR EACH HIDDEN SECTION:
- Label/trigger text
- Current state (collapsed/expanded)
- Preview of hidden content (text snippet)
- Count of interactive elements inside
- Element ref to expand it

OPTIONS:
- auto_expand: Automatically click all collapsed sections to reveal content (default: false)

USE WHEN:
- You need to see what's in collapsed accordions without manually expanding
- Discovering all available options in a settings panel
- Finding form fields hidden in wizard steps
- Understanding full page structure including hidden content`
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
		const hiddenSections = [];

		// Pattern 1: <details> elements
		document.querySelectorAll('details').forEach((details, idx) => {
			const summary = details.querySelector('summary');
			const isOpen = details.hasAttribute('open');

			// Get content preview (text from first 100 chars)
			let content = '';
			const contentEl = Array.from(details.children).find(el => el.tagName !== 'SUMMARY');
			if (contentEl) {
				content = contentEl.innerText?.trim()?.substring(0, 100) || '';
			}

			// Count interactive elements inside
			const interactiveCount = details.querySelectorAll('button, a, input, select, textarea').length;

			hiddenSections.push({
				type: 'details',
				trigger: summary?.innerText?.trim() || 'Details',
				state: isOpen ? 'expanded' : 'collapsed',
				content_preview: content,
				interactive_elements: interactiveCount,
				ref: details.id || 'details-' + idx,
				expandable: !isOpen
			});

			// Auto-expand if requested
			if (autoExpand && !isOpen && summary) {
				summary.click();
			}
		});

		// Pattern 2: aria-expanded buttons (accordion triggers)
		document.querySelectorAll('[aria-expanded]').forEach((trigger, idx) => {
			const isExpanded = trigger.getAttribute('aria-expanded') === 'true';
			const controls = trigger.getAttribute('aria-controls');

			// Find the controlled panel
			let panel = null;
			let content = '';
			let interactiveCount = 0;

			if (controls) {
				panel = document.getElementById(controls);
				if (panel) {
					content = panel.innerText?.trim()?.substring(0, 100) || '';
					interactiveCount = panel.querySelectorAll('button, a, input, select, textarea').length;
				}
			}

			hiddenSections.push({
				type: 'aria-accordion',
				trigger: trigger.innerText?.trim() || trigger.getAttribute('aria-label') || 'Accordion',
				state: isExpanded ? 'expanded' : 'collapsed',
				content_preview: content,
				interactive_elements: interactiveCount,
				ref: trigger.id || 'accordion-trigger-' + idx,
				expandable: !isExpanded && !!panel
			});

			// Auto-expand if requested
			if (autoExpand && !isExpanded) {
				trigger.click();
			}
		});

		// Pattern 3: Hidden tab panels
		document.querySelectorAll('[role="tabpanel"]').forEach((panel, idx) => {
			const isHidden = panel.hidden ||
			                 panel.getAttribute('aria-hidden') === 'true' ||
			                 getComputedStyle(panel).display === 'none';

			if (isHidden) {
				const id = panel.id;
				const tab = document.querySelector('[aria-controls="' + id + '"]');

				const content = panel.innerText?.trim()?.substring(0, 100) || '';
				const interactiveCount = panel.querySelectorAll('button, a, input, select, textarea').length;

				hiddenSections.push({
					type: 'tab-panel',
					trigger: tab?.innerText?.trim() || 'Tab ' + idx,
					state: 'hidden',
					content_preview: content,
					interactive_elements: interactiveCount,
					ref: tab?.id || 'tab-' + idx,
					expandable: !!tab
				});

				// Auto-expand if requested
				if (autoExpand && tab) {
					tab.click();
				}
			}
		});

		// Pattern 4: Common hidden divs with collapsible class names
		const collapsibleSelectors = [
			'.collapse:not(.show)',
			'.accordion-collapse:not(.show)',
			'[data-collapsed="true"]',
			'[data-state="closed"]'
		];

		collapsibleSelectors.forEach(selector => {
			document.querySelectorAll(selector).forEach((panel, idx) => {
				const trigger = document.querySelector('[data-target="#' + panel.id + '"], [aria-controls="' + panel.id + '"]');

				const content = panel.innerText?.trim()?.substring(0, 100) || '';
				const interactiveCount = panel.querySelectorAll('button, a, input, select, textarea').length;

				hiddenSections.push({
					type: 'collapsible-div',
					trigger: trigger?.innerText?.trim() || 'Collapsible',
					state: 'collapsed',
					content_preview: content,
					interactive_elements: interactiveCount,
					ref: trigger?.id || panel.id || 'collapsible-' + idx,
					expandable: !!trigger
				});

				// Auto-expand if requested
				if (autoExpand && trigger) {
					trigger.click();
				}
			});
		});

		return {
			url: window.location.href,
			title: document.title,
			hidden_sections_found: hiddenSections.length,
			auto_expanded: autoExpand,
			sections: hiddenSections
		};
	}
	`, autoExpand)

	result, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("failed to discover hidden content: %w", err)
	}

	return result.Value.Val(), nil
}

// InteractTool performs actions on elements identified by ref using Rod's native methods.
// This ensures proper event triggering for React and other framework-managed inputs.
type InteractTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *InteractTool) Name() string { return "interact" }
func (t *InteractTool) Description() string {
	return `Perform actions on page elements (click, type, select, toggle, clear).

GET REFS FROM: get-interactive-elements (run it first to discover elements)

ACTIONS:
- click: Click button/link (uses real mouse events)
- type: Enter text in input (clears first, triggers React onChange)
- select: Choose dropdown option (by value or text)
- toggle: Check/uncheck checkbox or radio
- clear: Clear input field

EXAMPLE:
interact(session_id, ref: "email-input", action: "type", value: "user@test.com")
interact(session_id, ref: "submit-btn", action: "click")

FOR FORMS: Use fill-form instead - it's more token-efficient for multiple fields.

Emits user_click/user_type/user_select facts for Mangle.`
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
				"description": "Element ref from get-interactive-elements (id, name, or selector)",
			},
			"action": map[string]interface{}{
				"type":        "string",
				"description": "Action to perform: click, type, select, toggle, clear",
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

	// Get element registry for fingerprint-based lookup
	registry := t.sessions.Registry(sessionID)

	// Find element using multi-strategy search with registry support
	element, err := findElementByRefWithRegistry(page, ref, registry)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	// Validate fingerprint and collect warnings about stale references
	var validationWarnings []string
	if registry != nil {
		if fp := registry.Get(ref); fp != nil {
			validation := validateFingerprint(element, fp)
			if len(validation.Changes) > 0 {
				validationWarnings = validation.Changes
			}
		}
	}

	// Check visibility
	visible, err := element.Visible()
	if err != nil || !visible {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Element not visible: %s", ref),
		}, nil
	}

	// Perform action using Rod's native methods for proper event triggering
	var resultValue string
	var resultChecked bool

	switch action {
	case "click":
		// Use Rod's native click which simulates real mouse events
		if err := element.Click("left", 1); err != nil {
			return map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("Click failed: %v", err),
			}, nil
		}

	case "type":
		// Clear existing value first, then use Rod's Input which simulates keyboard
		if err := element.SelectAllText(); err == nil {
			_ = element.Input("")
		}
		if err := element.Input(value); err != nil {
			return map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("Type failed: %v", err),
			}, nil
		}
		if submit {
			// Press Enter key using Rod's native keyboard simulation
			if err := page.Keyboard.Press('\r'); err != nil {
				return map[string]interface{}{
					"success": false,
					"error":   fmt.Sprintf("Submit (Enter) failed: %v", err),
				}, nil
			}
		}
		// Get final value - Property returns gson.JSON, use Str() for string
		if propVal, err := element.Property("value"); err == nil {
			resultValue = propVal.Str()
		}

	case "select":
		// For native select elements, use Rod's Select method
		tagNameProp, _ := element.Property("tagName")
		tagName := tagNameProp.Str()
		if tagName == "SELECT" {
			if err := element.Select([]string{value}, true, "value"); err != nil {
				// Try by text if value doesn't work
				if err := element.Select([]string{value}, true, "text"); err != nil {
					return map[string]interface{}{
						"success": false,
						"error":   fmt.Sprintf("Option not found: %s", value),
					}, nil
				}
			}
		} else {
			// For custom dropdowns, click to open
			if err := element.Click("left", 1); err != nil {
				return map[string]interface{}{
					"success": false,
					"error":   fmt.Sprintf("Select click failed: %v", err),
				}, nil
			}
		}
		if propVal, err := element.Property("value"); err == nil {
			resultValue = propVal.Str()
		}

	case "toggle":
		// Click to toggle checkbox/radio
		if err := element.Click("left", 1); err != nil {
			return map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("Toggle failed: %v", err),
			}, nil
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

	// Emit Mangle fact for the interaction
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

	result := map[string]interface{}{
		"success": true,
		"ref":     ref,
		"action":  action,
		"value":   resultValue,
		"checked": resultChecked,
	}

	// Add stale reference warnings if element properties changed since discovery
	if len(validationWarnings) > 0 {
		result["warning"] = "Element found but properties changed since discovery"
		result["changes"] = validationWarnings
	}

	return result, nil
}

