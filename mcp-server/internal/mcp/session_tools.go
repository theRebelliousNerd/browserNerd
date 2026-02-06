package mcp

import (
	"context"
	"fmt"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

type ListSessionsTool struct {
	sessions *browser.SessionManager
}

func (t *ListSessionsTool) Name() string { return "list-sessions" }
func (t *ListSessionsTool) Description() string {
	return `List all active browser sessions managed by the detached Rod instance.

USE THIS FIRST to discover existing sessions before creating new ones.
Returns session IDs needed for all other browser interaction tools.

WHEN TO USE:
- At the start of automation to see what's available
- After creating sessions to confirm they exist
- Before closing sessions to get accurate IDs

EXAMPLE OUTPUT:
{
  "sessions": [
    {"id": "ABC123DEF456", "url": "https://github.com/dashboard", "title": "GitHub Dashboard"},
    {"id": "XYZ789GHI012", "url": "https://docs.python.org/3/", "title": "Python Docs"}
  ]
}

Empty if no sessions: {"sessions": []}`
}
func (t *ListSessionsTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (t *ListSessionsTool) Execute(_ context.Context, _ map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{"sessions": t.sessions.List()}, nil
}

type CreateSessionTool struct {
	sessions *browser.SessionManager
}

func (t *CreateSessionTool) Name() string { return "create-session" }
func (t *CreateSessionTool) Description() string {
	return `Create a new browser session for automation.

PREREQUISITE: Browser must be running (use launch-browser first if needed).

WHEN TO USE:
- Starting fresh automation without prior state
- Need isolated sessions (incognito mode)
- Testing multiple user flows in parallel

WORKFLOW:
1. launch-browser (if not running)
2. create-session (with optional starting URL)
3. Use returned session_id for all interaction tools

EXAMPLE OUTPUT:
{
  "session": {
    "id": "ABC123DEF456",
    "url": "https://example.com",
    "title": "Example Domain"
  }
}

Use the "id" field for all subsequent tool calls (navigate-url, get-interactive-elements, interact, etc.)`
}
func (t *CreateSessionTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "Optional URL to navigate after opening the session",
			},
		},
	}
}
func (t *CreateSessionTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	url := getStringArg(args, "url")
	if url == "" {
		url = "about:blank"
	}

	sess, err := t.sessions.CreateSession(ctx, url)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{"session": sess}, nil
}

type AttachSessionTool struct {
	sessions *browser.SessionManager
}

func (t *AttachSessionTool) Name() string { return "attach-session" }
func (t *AttachSessionTool) Description() string {
	return `Attach to an existing Chrome tab/window by its CDP TargetID.

USE INSTEAD OF create-session when:
- Connecting to a manually opened browser tab
- Resuming automation on an existing page
- Attaching to a tab opened by another process

HOW TO GET target_id:
- From Chrome DevTools Protocol directly
- From chrome://inspect page
- From prior automation that created tabs

Returns: {session: {id, url, title}} for use with other tools.`
}
func (t *AttachSessionTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"target_id": map[string]interface{}{
				"type":        "string",
				"description": "CDP TargetID to attach",
			},
		},
		"required": []string{"target_id"},
	}
}
func (t *AttachSessionTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	targetID := getStringArg(args, "target_id")
	if targetID == "" {
		return nil, fmt.Errorf("target_id is required")
	}

	sess, err := t.sessions.Attach(ctx, targetID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"session": sess}, nil
}

// ForkSessionTool clones an existing session's cookies + storage into a fresh incognito context.
type ForkSessionTool struct {
	sessions *browser.SessionManager
}

func (t *ForkSessionTool) Name() string { return "fork-session" }
func (t *ForkSessionTool) Description() string {
	return `Clone an existing session's auth state (cookies, localStorage) into a new tab.

WHEN TO USE:
- Testing logged-in user flows without re-authenticating
- Running parallel tests that need same auth state
- Exploring different paths from same starting point

EXAMPLE WORKFLOW:
1. create-session -> login to app -> get session_id
2. fork-session(session_id) -> new session with same auth
3. Now you have 2 independent sessions, both logged in

Returns: {forked_from, session: {id, url, title}}`
}
func (t *ForkSessionTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Existing session to fork",
			},
			"url": map[string]interface{}{
				"type":        "string",
				"description": "Optional URL override for the forked session",
			},
		},
		"required": []string{"session_id"},
	}
}
func (t *ForkSessionTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}

	url := getStringArg(args, "url")
	sess, err := t.sessions.ForkSession(ctx, sessionID, url)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"forked_from": sessionID,
		"session":     sess,
	}, nil
}

type ReifyReactTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *ReifyReactTool) Name() string { return "reify-react" }
func (t *ReifyReactTool) Description() string {
	return `Extract React component tree structure into Mangle facts for analysis.

PROGRESSIVE DISCLOSURE GATE:
This deep extraction tool is intentionally gated.
Use one of:
- gate_reason="explicit_user_intent" with approved_by_handle
- gate_reason="low_confidence" after browser-reason
- gate_reason="contradiction_detected" after browser-reason
- gate_reason="no_matching_tool" after browser-reason
- approved_by_handle from browser-reason evidence handles

WHAT IT DOES:
- Traverses React's internal Fiber tree
- Emits facts about component hierarchy, props, and state
- Enables logic-based reasoning about React app structure

WHEN TO USE:
- Debugging React component state
- Understanding component relationships
- Writing Mangle rules that depend on React structure
- Verifying component props match expectations

EMITTED FACTS:
- react_component(SessionId, ComponentName, ParentRef)
- react_prop(SessionId, ComponentRef, PropName, PropValue)
- react_state(SessionId, ComponentRef, StateKey, StateValue)

NOTE: Only works on React 16+ apps with accessible Fiber tree.`
}
func (t *ReifyReactTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session to introspect",
			},
			"gate_reason": map[string]interface{}{
				"type":        "string",
				"description": "Progressive disclosure gate reason (explicit_user_intent requires approved_by_handle)",
				"enum":        []string{"explicit_user_intent", "low_confidence", "contradiction_detected", "no_matching_tool"},
			},
			"approved_by_handle": map[string]interface{}{
				"type":        "string",
				"description": "Evidence handle from browser-reason that authorizes deep extraction fallback",
			},
		},
		"required": []string{"session_id"},
	}
}
func (t *ReifyReactTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	gateReason := getStringArg(args, "gate_reason")
	approvedHandle := getStringArg(args, "approved_by_handle")

	gateTool := &EvaluateJSTool{engine: t.engine}
	if ok, reason := gateTool.evaluateJSGateOpen(sessionID, gateReason, approvedHandle, t.Name()); !ok {
		return map[string]interface{}{
			"success":            false,
			"gated":              true,
			"error":              reason,
			"recommended_tool":   "browser-reason",
			"required_reasons":   []string{"explicit_user_intent", "low_confidence", "contradiction_detected", "no_matching_tool"},
			"approved_by_handle": approvedHandle,
		}, nil
	}

	facts, err := t.sessions.ReifyReact(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success":    true,
		"session_id": sessionID,
		"facts":      len(facts),
	}, nil
}

type SnapshotDOMTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *SnapshotDOMTool) Name() string { return "snapshot-dom" }
func (t *SnapshotDOMTool) Description() string {
	return `Capture current DOM structure as Mangle facts for logic-based analysis.

PROGRESSIVE DISCLOSURE GATE:
This deep extraction tool is intentionally gated.
Use one of:
- gate_reason="explicit_user_intent" with approved_by_handle
- gate_reason="low_confidence" after browser-reason
- gate_reason="contradiction_detected" after browser-reason
- gate_reason="no_matching_tool" after browser-reason
- approved_by_handle from browser-reason evidence handles

WHEN TO USE:
- Before writing Mangle rules that query DOM structure
- Debugging layout/structure issues
- Verifying DOM state after interactions
- Building assertions about page structure

EMITTED FACTS:
- dom_node(SessionId, NodeRef, TagName, ParentRef)
- dom_attr(SessionId, NodeRef, AttrName, AttrValue)
- dom_text(SessionId, NodeRef, TextContent)

PREFER get-interactive-elements for:
- Finding clickable elements
- Form automation
- Navigation link discovery

Use snapshot-dom when you need deep DOM structure analysis.`
}
func (t *SnapshotDOMTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session to snapshot",
			},
			"gate_reason": map[string]interface{}{
				"type":        "string",
				"description": "Progressive disclosure gate reason (explicit_user_intent requires approved_by_handle)",
				"enum":        []string{"explicit_user_intent", "low_confidence", "contradiction_detected", "no_matching_tool"},
			},
			"approved_by_handle": map[string]interface{}{
				"type":        "string",
				"description": "Evidence handle from browser-reason that authorizes deep extraction fallback",
			},
		},
		"required": []string{"session_id"},
	}
}
func (t *SnapshotDOMTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	gateReason := getStringArg(args, "gate_reason")
	approvedHandle := getStringArg(args, "approved_by_handle")

	gateTool := &EvaluateJSTool{engine: t.engine}
	if ok, reason := gateTool.evaluateJSGateOpen(sessionID, gateReason, approvedHandle, t.Name()); !ok {
		return map[string]interface{}{
			"success":            false,
			"gated":              true,
			"error":              reason,
			"recommended_tool":   "browser-reason",
			"required_reasons":   []string{"explicit_user_intent", "low_confidence", "contradiction_detected", "no_matching_tool"},
			"approved_by_handle": approvedHandle,
		}, nil
	}
	if err := t.sessions.SnapshotDOM(ctx, sessionID); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"success":    true,
		"session_id": sessionID,
		"status":     "captured",
	}, nil
}

// LaunchBrowserTool starts Chrome using the configured launch command.
type LaunchBrowserTool struct {
	sessions *browser.SessionManager
}

func (t *LaunchBrowserTool) Name() string { return "launch-browser" }
func (t *LaunchBrowserTool) Description() string {
	return `Start a Chrome browser instance for automation.

CALL THIS FIRST before any browser automation.

WHAT IT DOES:
- Launches Chrome with DevTools Protocol enabled
- Configures based on server settings (headless, user data dir, etc.)
- Returns control URL for debugging

WHEN TO USE:
- Starting a new automation session
- After shutdown-browser to restart
- Idempotent: safe to call if already running

TYPICAL WORKFLOW:
1. launch-browser         -> Start Chrome
2. create-session         -> Open a tab
3. navigate-url/interact  -> Automate
4. shutdown-browser       -> Cleanup (optional)

EXAMPLE OUTPUT (fresh start):
{
  "status": "started",
  "control_url": "ws://127.0.0.1:9222/devtools/browser/abc123"
}

EXAMPLE OUTPUT (already running):
{
  "status": "already_connected",
  "control_url": "ws://127.0.0.1:9222/devtools/browser/abc123"
}`
}
func (t *LaunchBrowserTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (t *LaunchBrowserTool) Execute(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
	if t.sessions.IsConnected() {
		return map[string]interface{}{
			"status":      "already_connected",
			"control_url": t.sessions.ControlURL(),
		}, nil
	}

	if err := t.sessions.Start(ctx); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"status":      "started",
		"control_url": t.sessions.ControlURL(),
	}, nil
}

// ShutdownBrowserTool stops the managed Chrome instance and clears sessions.
type ShutdownBrowserTool struct {
	sessions *browser.SessionManager
}

func (t *ShutdownBrowserTool) Name() string { return "shutdown-browser" }
func (t *ShutdownBrowserTool) Description() string {
	return `Stop the Chrome browser and clean up all sessions.

WHEN TO USE:
- End of automation to release resources
- Before restarting with different settings
- Cleanup after test failures

WHAT IT DOES:
- Closes all tracked sessions
- Terminates Chrome process
- Clears session state (NOT Mangle facts)

NOTE: Mangle fact buffer persists after shutdown.
Use this when you're done with browser automation.

EXAMPLE OUTPUT:
{
  "status": "shutdown_complete",
  "sessions_closed": 2
}`
}
func (t *ShutdownBrowserTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (t *ShutdownBrowserTool) Execute(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
	if err := t.sessions.Shutdown(ctx); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"status": "stopped",
	}, nil
}
