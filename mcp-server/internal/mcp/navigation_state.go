package mcp

import (
	"context"
	"fmt"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod/lib/input"
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

RETURNS:
- url: Current page URL
- title: Page title
- loading: true if still loading
- hasDialog: true if modal is open
- scrollY: Current scroll position

USE THIS FIRST TO:
- Verify navigation succeeded (check URL)
- Confirm page finished loading
- Detect if a modal/dialog appeared
- Check scroll position

THEN USE IF NEEDED:
- get-interactive-elements (if you need to interact)
- get-navigation-links (if you need links)
- screenshot (only for visual debugging)

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

// NavigateURLTool navigates an existing session to a new URL.
type NavigateURLTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *NavigateURLTool) Name() string { return "navigate-url" }
func (t *NavigateURLTool) Description() string {
	return `Go to a URL in an existing session.

USE THIS (not create-session) when:
- Navigating within the same browser session
- Need to preserve cookies/localStorage
- Following links or testing page flows

WAIT OPTIONS:
- load: Wait for DOMContentLoaded (default, fast)
- networkidle: Wait until no network activity for 500ms (thorough)
- none: Return immediately (for SPAs that load async)

EXAMPLE:
navigate-url(session_id, url: "https://app.com/dashboard", wait_until: "networkidle")

Emits navigation_event fact. Returns final URL (may differ due to redirects).`
}
func (t *NavigateURLTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session to navigate",
			},
			"url": map[string]interface{}{
				"type":        "string",
				"description": "URL to navigate to",
			},
			"wait_until": map[string]interface{}{
				"type":        "string",
				"description": "Wait condition: 'load' (DOMContentLoaded), 'networkidle' (no network for 500ms), or 'none' (return immediately). Default: 'load'",
				"enum":        []string{"load", "networkidle", "none"},
			},
		},
		"required": []string{"session_id", "url"},
	}
}
func (t *NavigateURLTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	url := getStringArg(args, "url")
	waitUntil := getStringArg(args, "wait_until")
	if waitUntil == "" {
		waitUntil = "load"
	}

	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}
	if url == "" {
		return map[string]interface{}{"success": false, "error": "url is required"}, nil
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("session not found: %s", sessionID)}, nil
	}

	// Check if we're already on the same URL - skip navigation to avoid hang
	// Rod/CDP doesn't emit navigation events for same-URL navigation, causing
	// WaitLoad() to wait indefinitely for an event that never fires.
	currentInfo, _ := page.Info()
	if currentInfo != nil && currentInfo.URL == url {
		return map[string]interface{}{
			"success":     true,
			"url":         url,
			"duration_ms": int64(0),
			"note":        "already on this URL, no navigation needed",
		}, nil
	}

	startTime := time.Now()

	// Navigate based on wait condition
	var err error
	switch waitUntil {
	case "none":
		err = page.Navigate(url)
	case "networkidle":
		wait := page.MustWaitRequestIdle()
		err = page.Navigate(url)
		if err == nil {
			wait()
		}
	default: // "load"
		err = page.Navigate(url)
		if err == nil {
			err = page.WaitLoad()
		}
	}

	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("navigation failed: %v", err),
		}, nil
	}

	duration := time.Since(startTime)

	// Get final URL (may differ from requested due to redirects)
	info, _ := page.Info()
	finalURL := url
	if info != nil {
		finalURL = info.URL
	}

	// Emit Mangle fact for navigation
	now := time.Now()
	_ = t.engine.AddFacts(ctx, []mangle.Fact{{
		Predicate: "navigation_event",
		Args:      []interface{}{sessionID, finalURL, now.UnixMilli()},
		Timestamp: now,
	}})

	return map[string]interface{}{
		"success":     true,
		"url":         finalURL,
		"duration_ms": duration.Milliseconds(),
	}, nil
}

// PressKeyTool presses a keyboard key in a session.
type PressKeyTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *PressKeyTool) Name() string { return "press-key" }
func (t *PressKeyTool) Description() string {
	return "Press a keyboard key in the active session. Useful for Enter to submit forms, Escape to close dialogs, Tab for focus navigation."
}
func (t *PressKeyTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session",
			},
			"key": map[string]interface{}{
				"type":        "string",
				"description": "Key to press: Enter, Tab, Escape, ArrowUp, ArrowDown, ArrowLeft, ArrowRight, Backspace, Delete, Space, or any single character",
			},
			"modifiers": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Modifier keys to hold: Ctrl, Alt, Shift, Meta",
			},
		},
		"required": []string{"session_id", "key"},
	}
}
func (t *PressKeyTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	key := getStringArg(args, "key")

	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}
	if key == "" {
		return map[string]interface{}{"success": false, "error": "key is required"}, nil
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("session not found: %s", sessionID)}, nil
	}

	// Map common key names to input.Key constants
	keyMap := map[string]input.Key{
		"Enter":      input.Enter,
		"Tab":        input.Tab,
		"Escape":     input.Escape,
		"Backspace":  input.Backspace,
		"Space":      input.Space,
		"Delete":     input.Delete,
		"ArrowUp":    input.ArrowUp,
		"ArrowDown":  input.ArrowDown,
		"ArrowLeft":  input.ArrowLeft,
		"ArrowRight": input.ArrowRight,
		"Home":       input.Home,
		"End":        input.End,
		"PageUp":     input.PageUp,
		"PageDown":   input.PageDown,
	}

	var inputKey input.Key
	if k, ok := keyMap[key]; ok {
		inputKey = k
	} else if len(key) == 1 {
		// Single character - convert to input.Key (which is based on rune)
		inputKey = input.Key(rune(key[0]))
	} else {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("unknown key: %s. Supported: Enter, Tab, Escape, Backspace, Space, Delete, ArrowUp/Down/Left/Right, Home, End, PageUp, PageDown, or single characters", key),
		}, nil
	}

	// Handle modifiers if provided - use Down/Up for modifier keys
	modifiers := args["modifiers"]
	var modifierKeys []input.Key
	if modifiers != nil {
		if modList, ok := modifiers.([]interface{}); ok {
			for _, mod := range modList {
				modStr, _ := mod.(string)
				switch modStr {
				case "Ctrl":
					modifierKeys = append(modifierKeys, input.ControlLeft)
				case "Alt":
					modifierKeys = append(modifierKeys, input.AltLeft)
				case "Shift":
					modifierKeys = append(modifierKeys, input.ShiftLeft)
				case "Meta":
					modifierKeys = append(modifierKeys, input.MetaLeft)
				}
			}
		}
	}

	// Press modifier keys down
	for _, modKey := range modifierKeys {
		if err := page.Keyboard.Press(modKey); err != nil {
			return map[string]interface{}{
				"success": false,
				"error":   fmt.Sprintf("modifier key press failed: %v", err),
			}, nil
		}
	}

	// Press the main key
	if err := page.Keyboard.Press(inputKey); err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("key press failed: %v", err),
		}, nil
	}

	// Release modifier keys
	for _, modKey := range modifierKeys {
		_ = page.Keyboard.Release(modKey)
	}

	// Emit Mangle fact
	now := time.Now()
	_ = t.engine.AddFacts(ctx, []mangle.Fact{{
		Predicate: "user_keypress",
		Args:      []interface{}{sessionID, key, now.UnixMilli()},
		Timestamp: now,
	}})

	return map[string]interface{}{
		"success": true,
		"key":     key,
	}, nil
}

type BrowserHistoryTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *BrowserHistoryTool) Name() string { return "browser-history" }
func (t *BrowserHistoryTool) Description() string {
	return `Control browser navigation history (back, forward, reload).

ACTIONS:
- back: Go to previous page (like clicking browser back button)
- forward: Go to next page (after going back)
- reload: Refresh current page

WHEN TO USE:
- Testing back button behavior
- Verifying form resubmission warnings
- Testing cache behavior on reload
- Navigation flow testing

USE navigate-url INSTEAD when you know the target URL.
Use browser-history for simulating user navigation patterns.

Emits history_navigation fact. Waits for page load before returning.`
}
func (t *BrowserHistoryTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session",
			},
			"action": map[string]interface{}{
				"type":        "string",
				"description": "History action: 'back', 'forward', or 'reload'",
				"enum":        []string{"back", "forward", "reload"},
			},
		},
		"required": []string{"session_id", "action"},
	}
}
func (t *BrowserHistoryTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	action := getStringArg(args, "action")

	if sessionID == "" || action == "" {
		return map[string]interface{}{"success": false, "error": "session_id and action are required"}, nil
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("session not found: %s", sessionID)}, nil
	}

	var err error
	switch action {
	case "back":
		err = page.NavigateBack()
	case "forward":
		err = page.NavigateForward()
	case "reload":
		err = page.Reload()
	default:
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("unknown action: %s", action)}, nil
	}

	if err != nil {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("%s failed: %v", action, err)}, nil
	}

	// Wait for page to load
	_ = page.WaitLoad()

	// Get new URL
	info, _ := page.Info()
	newURL := ""
	if info != nil {
		newURL = info.URL
	}

	// Emit Mangle fact
	now := time.Now()
	_ = t.engine.AddFacts(ctx, []mangle.Fact{{
		Predicate: "history_navigation",
		Args:      []interface{}{sessionID, action, newURL, now.UnixMilli()},
		Timestamp: now,
	}})

	return map[string]interface{}{
		"success": true,
		"action":  action,
		"url":     newURL,
	}, nil
}

