package mcp

import (
	"context"
	"fmt"

	"browsernerd-mcp-server/internal/browser"
)

// =============================================================================
// NAVIGATION / STATE TOOLS
// =============================================================================

// GetPageStateTool returns compact page state info.
type GetPageStateTool struct {
	sessions *browser.SessionManager
}

func (t *GetPageStateTool) Name() string { return "get-page-state" }
func (t *GetPageStateTool) Description() string {
	return `Quick status check of the current page.

TOKEN COST: Low (use this FIRST before heavier tools)

USE THIS FIRST TO:
- Verify navigation succeeded (check URL)
- Confirm page finished loading
- Detect if a modal/dialog appeared
- Check scroll position

THEN USE IF NEEDED:
- get-interactive-elements (if you need to interact)
- get-navigation-links (if you need links)
- screenshot (only for visual debugging)

EXAMPLE OUTPUT:
{
  "url": "https://github.com/anthropics/claude-code",
  "title": "GitHub - anthropics/claude-code",
  "loading": false,
  "activeElement": "search-input",
  "hasDialog": false,
  "scrollY": 0,
  "viewportHeight": 900
}

EXAMPLE (with modal open):
{
  "url": "https://app.example.com/settings",
  "title": "Settings",
  "loading": false,
  "hasDialog": true,
  "scrollY": 250,
  "viewportHeight": 900
}

AVOID: Taking screenshots just to check page state.`
}
func (t *GetPageStateTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session",
			},
		},
		"required": []string{"session_id"},
	}
}
func (t *GetPageStateTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	js := `
	() => {
		const activeEl = document.activeElement;
		let activeRef = null;
		if (activeEl && activeEl !== document.body) {
			activeRef = activeEl.id || activeEl.name || activeEl.tagName.toLowerCase();
		}

		// Check for common dialog/modal patterns
		const hasDialog = !!(
			document.querySelector('dialog[open]') ||
			document.querySelector('[role="dialog"]') ||
			document.querySelector('[role="alertdialog"]') ||
			document.querySelector('.modal.show') ||
			document.querySelector('[data-state="open"][role="dialog"]')
		);

		return {
			url: window.location.href,
			title: document.title,
			loading: document.readyState !== 'complete',
			activeElement: activeRef,
			hasDialog: hasDialog,
			scrollY: window.scrollY,
			viewportHeight: window.innerHeight
		};
	}
	`

	result, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("failed to get page state: %w", err)
	}

	return result.Value.Val(), nil
}
