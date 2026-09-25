package mcp

import (
	"context"
	"fmt"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

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

EXAMPLE OUTPUT:
{
  "success": true,
  "url": "https://github.com/anthropics",
  "title": "Anthropic - GitHub",
  "wait_used": "load"
}

EXAMPLE (with redirect):
{
  "success": true,
  "url": "https://www.example.com/",
  "title": "Example Domain",
  "wait_used": "networkidle",
  "redirected_from": "http://example.com"
}

Emits navigation_event fact for Mangle reasoning.`
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
			"timeout_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Upper bound for navigating and waiting (default 30000)",
			},
		},
		"required": []string{"session_id", "url"},
	}
}

// defaultNavigateTimeout bounds a navigation and its wait.
const defaultNavigateTimeout = 30 * time.Second

func (t *NavigateURLTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	url := getStringArg(args, "url")
	waitUntil := getStringArg(args, "wait_until")
	if waitUntil == "" {
		waitUntil = "load"
	}
	timeout := defaultNavigateTimeout
	if ms := getIntArg(args, "timeout_ms", 0); ms > 0 {
		timeout = time.Duration(ms) * time.Millisecond
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
		t.emitNavigationFacts(ctx, sessionID, url, false)
		return map[string]interface{}{
			"success":     true,
			"url":         url,
			"duration_ms": int64(0),
			"note":        "already on this URL, no navigation needed",
		}, nil
	}

	startTime := time.Now()
	boundCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	bounded := page.Context(boundCtx)

	// Navigate based on wait condition
	var err error
	warning := ""
	switch waitUntil {
	case "none":
		err = bounded.Navigate(url)
	case "networkidle":
		wait := bounded.WaitRequestIdle(500*time.Millisecond, nil, nil, nil)
		err = bounded.Navigate(url)
		if err == nil {
			wait()
			if boundCtx.Err() != nil {
				warning = fmt.Sprintf("the network never went quiet for 500 ms within %s; the page may poll", timeout)
			}
		}
	default: // "load"
		err = bounded.Navigate(url)
		if err == nil {
			if err = bounded.WaitLoad(); err != nil && boundCtx.Err() != nil {
				err = fmt.Errorf("the load event did not fire within %s", timeout)
			}
		}
	}

	if err != nil {
		if boundCtx.Err() != nil && ctx.Err() == nil && waitUntil != "load" {
			err = fmt.Errorf("no response within %s", timeout)
		}
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

	t.emitNavigationFacts(ctx, sessionID, finalURL, true)

	result := map[string]interface{}{
		"success":     true,
		"url":         finalURL,
		"duration_ms": duration.Milliseconds(),
	}
	if warning != "" {
		result["warning"] = warning
	}
	return result, nil
}

func (t *NavigateURLTool) emitNavigationFacts(ctx context.Context, sessionID, url string, navigated bool) {
	if t.engine == nil {
		return
	}
	now := time.Now()
	facts := []mangle.Fact{{
		Predicate: "current_url",
		Args:      []interface{}{sessionID, url},
		Timestamp: now,
	}}
	if navigated {
		facts = append([]mangle.Fact{{
			Predicate: "navigation_event",
			Args:      []interface{}{sessionID, url, now.UnixMilli()},
			Timestamp: now,
		}}, facts...)
	}
	_ = t.engine.AddFacts(ctx, facts)
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
				"description": "Named key (Enter, Tab, Escape, ArrowDown, F5, ...), a single character, or a chord: Control+a, Shift+Tab, Meta+k, Control+Shift+P",
			},
			"modifiers": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Modifier keys to hold: Control, Alt, Shift, Meta",
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

	chord, err := parseKeyChord(key, toStringSlice(args["modifiers"]))
	if err != nil {
		return map[string]interface{}{"success": false, "error": err.Error()}, nil
	}
	if err := dispatchKeyChord(page.Context(ctx).Timeout(5*time.Second), chord); err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("key %s failed: %v", chord.label, err),
		}, nil
	}

	// Emit Mangle fact
	now := time.Now()
	if t.engine != nil {
		_ = t.engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "user_keypress",
			Args:      []interface{}{sessionID, chord.label, now.UnixMilli()},
			Timestamp: now,
		}})
	}

	return map[string]interface{}{
		"success": true,
		"key":     chord.label,
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

	timeout := defaultNavigateTimeout
	if ms := getIntArg(args, "timeout_ms", 0); ms > 0 {
		timeout = time.Duration(ms) * time.Millisecond
	}
	boundCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	bounded := page.Context(boundCtx)

	var err error
	switch action {
	case "back":
		err = bounded.NavigateBack()
	case "forward":
		err = bounded.NavigateForward()
	case "reload":
		err = bounded.Reload()
	default:
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("unknown action: %s", action)}, nil
	}

	if err != nil {
		if boundCtx.Err() != nil && ctx.Err() == nil {
			err = fmt.Errorf("no navigation within %s", timeout)
		}
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("%s failed: %v", action, err)}, nil
	}

	// Wait for page to load
	_ = bounded.WaitLoad()

	// Get new URL
	info, _ := page.Info()
	newURL := ""
	if info != nil {
		newURL = info.URL
	}

	// Emit Mangle fact
	now := time.Now()
	if t.engine != nil {
		_ = t.engine.AddFacts(ctx, []mangle.Fact{{
			Predicate: "history_navigation",
			Args:      []interface{}{sessionID, action, newURL, now.UnixMilli()},
			Timestamp: now,
		}})
	}

	return map[string]interface{}{
		"success": true,
		"action":  action,
		"url":     newURL,
	}, nil
}
