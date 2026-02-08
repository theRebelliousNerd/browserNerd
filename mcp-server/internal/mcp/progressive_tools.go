package mcp

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/mangle"
)

const (
	defaultProgressiveMaxItems = 20
	defaultObserveMaxRecs      = 3
	defaultReasonMaxRecs       = 4
	defaultReasonTimeWindowMs  = 300000
	jsGateTTL                  = 10 * time.Minute
)

// BrowserObserveTool provides progressive-disclosure page observation.
// This is a consolidated tool that wraps existing observe/extract tools.
type BrowserObserveTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *BrowserObserveTool) Name() string { return "browser-observe" }
func (t *BrowserObserveTool) Description() string {
	return `Observe the browser -- page state, elements, sessions, screenshots, React trees, DOM.

USE THIS TOOL to understand what is on the page before acting. Start here.

QUICK START (use intent for common tasks):
  intent:"quick_status"    -> Is the page loaded? Any errors?
  intent:"find_actions"    -> What can I click/type/select?
  intent:"map_navigation"  -> Where can I navigate from here?
  intent:"check_sessions"  -> What tabs are open? (no session_id needed)
  intent:"visual_check"    -> Screenshot the current page
  intent:"deep_audit"      -> Everything: state + nav + interactive + diagnostics

MODES (explicit control -- override intent defaults):
  state:        Page URL, title, loading status, dialog info
  nav:          Navigation links grouped by region (header, sidebar, main, footer) + counts
  interactive:  Clickable elements (buttons, inputs, links, selects) with ref IDs for browser-act
  hidden:       Elements outside the viewport or display:none
  composite:    state + nav + interactive combined (default)
  sessions:     List all active browser tabs (no session_id required)
  screenshot:   Capture page image (params: full_page, format, save_path)
  react:        Extract React Fiber component tree (requires disclosure handle)
  dom_snapshot: Snapshot full DOM as Mangle facts (requires disclosure handle)

VIEWS (control output size):
  summary: Minimal -- counts and handles only (~200 tokens). Start here.
  compact: Practical -- enough detail for decisions (~500-1500 tokens). Default.
  full:    Complete -- all data, for debugging (~2000+ tokens).

EXAMPLES:
  {session_id:"sess-1", intent:"quick_status"}
  {session_id:"sess-1", mode:"interactive", view:"compact"}
  {session_id:"sess-1", mode:"screenshot", full_page:true}
  {intent:"check_sessions"}

WHEN TO USE EACH MODE:
  "I need to understand this page"         -> mode:"composite" or intent:"quick_status"
  "What can I click?"                      -> mode:"interactive" or intent:"find_actions"
  "Where can I navigate?"                  -> mode:"nav" or intent:"map_navigation"
  "What tabs are open?"                    -> mode:"sessions" (no session_id needed)
  "I need to see the page visually"        -> mode:"screenshot"
  "I need React component state/props"     -> mode:"react" (gated -- needs handle)
  "I need raw DOM structure as facts"      -> mode:"dom_snapshot" (gated -- needs handle)

NEXT STEP: Use ref IDs from interactive mode in browser-act to click/type/select.`
}

func (t *BrowserObserveTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session",
			},
			"intent": map[string]interface{}{
				"type":        "string",
				"description": "Token-aware intent preset that applies progressive defaults when explicit knobs are omitted",
				"enum":        []string{"quick_status", "find_actions", "map_navigation", "hidden_content", "deep_audit", "check_sessions", "visual_check"},
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"description": "Observation mode",
				"enum":        []string{"state", "nav", "interactive", "hidden", "composite", "sessions", "screenshot", "react", "dom_snapshot"},
			},
			"full_page": map[string]interface{}{
				"type":        "boolean",
				"description": "For screenshot mode: capture full scrollable page (default false)",
			},
			"save_path": map[string]interface{}{
				"type":        "string",
				"description": "For screenshot mode: save screenshot to this file path",
			},
			"format": map[string]interface{}{
				"type":        "string",
				"description": "For screenshot mode: image format (png|jpeg)",
				"enum":        []string{"png", "jpeg"},
			},
			"view": map[string]interface{}{
				"type":        "string",
				"description": "Disclosure depth: summary|compact|full",
				"enum":        []string{"summary", "compact", "full"},
			},
			"max_items": map[string]interface{}{
				"type":        "integer",
				"description": "Max number of items for list outputs (default 20)",
			},
			"filter": map[string]interface{}{
				"type":        "string",
				"description": "Interactive filter: all|buttons|inputs|links|selects",
				"enum":        []string{"all", "buttons", "inputs", "links", "selects"},
			},
			"visible_only": map[string]interface{}{
				"type":        "boolean",
				"description": "Only visible interactive elements (default true)",
			},
			"internal_only": map[string]interface{}{
				"type":        "boolean",
				"description": "For nav mode: only internal links",
			},
			"emit_facts": map[string]interface{}{
				"type":        "boolean",
				"description": "Emit derived facts where supported (default true)",
			},
			"include_action_plan": map[string]interface{}{
				"type":        "boolean",
				"description": "Include Mangle-derived action candidates and browser-act recommendations (default true)",
			},
			"include_diagnostics": map[string]interface{}{
				"type":        "boolean",
				"description": "Include lightweight health signals (diagnose-page + toast counts) (default false; enabled by some intents)",
			},
			"max_recommendations": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum recommendation rows to return (default 3)",
			},
		},
	}
}

func (t *BrowserObserveTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	intent := normalizeObserveIntent(getStringArg(args, "intent"))
	intentCfg, hasIntent := resolveObserveIntentDefaults(intent)

	mode := strings.ToLower(getStringArg(args, "mode"))
	view := normalizeProgressiveView(getStringArg(args, "view"))
	maxItems := getIntArg(args, "max_items", defaultProgressiveMaxItems)
	filter := strings.ToLower(getStringArg(args, "filter"))
	visibleOnly := getBoolArg(args, "visible_only", true)
	internalOnly := getBoolArg(args, "internal_only", false)
	emitFacts := getBoolArg(args, "emit_facts", true)
	includeActionPlan := getBoolArg(args, "include_action_plan", true)
	includeDiagnostics := getBoolArg(args, "include_diagnostics", false)
	maxRecommendations := getIntArg(args, "max_recommendations", defaultObserveMaxRecs)
	if maxRecommendations <= 0 {
		maxRecommendations = defaultObserveMaxRecs
	}

	intentApplied := false
	if hasIntent {
		if !argHasNonEmptyString(args, "mode") && intentCfg.mode != "" {
			mode = intentCfg.mode
			intentApplied = true
		}
		if !argHasNonEmptyString(args, "view") && intentCfg.view != "" {
			view = intentCfg.view
			intentApplied = true
		}
		if !argHasInt(args, "max_items") && intentCfg.maxItems > 0 {
			maxItems = intentCfg.maxItems
			intentApplied = true
		}
		if !argHasNonEmptyString(args, "filter") && intentCfg.filter != "" {
			filter = intentCfg.filter
			intentApplied = true
		}
		if !argPresent(args, "visible_only") {
			visibleOnly = intentCfg.visibleOnly
			intentApplied = true
		}
		if !argPresent(args, "internal_only") {
			internalOnly = intentCfg.internalOnly
			intentApplied = true
		}
		if !argPresent(args, "include_action_plan") {
			includeActionPlan = intentCfg.includeActionPlan
			intentApplied = true
		}
		if !argPresent(args, "include_diagnostics") {
			includeDiagnostics = intentCfg.includeDiagnostics
			intentApplied = true
		}
		if !argHasInt(args, "max_recommendations") && intentCfg.maxRecommendations > 0 {
			maxRecommendations = intentCfg.maxRecommendations
			intentApplied = true
		}
	}

	if mode == "" {
		mode = "composite"
	}
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}
	if filter == "" {
		filter = "all"
	}

	// Handle new delegating modes that return early
	switch mode {
	case "sessions":
		delegate := &ListSessionsTool{sessions: t.sessions}
		res, err := delegate.Execute(ctx, map[string]interface{}{})
		if err != nil {
			return nil, err
		}
		resMap := asMap(res)
		handles := []string{"observe:sessions"}
		emitDisclosureFacts(ctx, t.engine, "", handles, "observe")
		response := map[string]interface{}{
			"success":          true,
			"status":           "ok",
			"intent":           ternaryStatus(hasIntent, intent, "custom"),
			"intent_applied":   intentApplied,
			"mode":             mode,
			"view":             view,
			"evidence_handles": handles,
			"truncated":        false,
		}
		switch view {
		case "summary":
			sessions, _ := resMap["sessions"].([]interface{})
			response["summary"] = fmt.Sprintf("%d active session(s)", len(sessions))
			response["data"] = map[string]interface{}{"session_count": len(sessions)}
		case "compact":
			sessions, _ := resMap["sessions"].([]interface{})
			response["summary"] = fmt.Sprintf("%d active session(s)", len(sessions))
			response["data"] = map[string]interface{}{"sessions": limitAnySlice(sessions, maxItems)}
		default:
			response["data"] = resMap
		}
		return response, nil

	case "screenshot":
		if sessionID == "" {
			return map[string]interface{}{"success": false, "error": "session_id is required for screenshot mode"}, nil
		}
		delegate := &ScreenshotTool{sessions: t.sessions, engine: t.engine}
		delegateArgs := map[string]interface{}{
			"session_id": sessionID,
		}
		if v, ok := args["full_page"]; ok {
			delegateArgs["full_page"] = v
		}
		if v, ok := args["save_path"]; ok {
			delegateArgs["save_path"] = v
		}
		if v, ok := args["format"]; ok {
			delegateArgs["format"] = v
		}
		res, err := delegate.Execute(ctx, delegateArgs)
		if err != nil {
			return nil, err
		}
		handles := []string{"observe:" + sessionID + ":screenshot"}
		emitDisclosureFacts(ctx, t.engine, sessionID, handles, "observe")
		return map[string]interface{}{
			"success":          true,
			"status":           "ok",
			"intent":           ternaryStatus(hasIntent, intent, "custom"),
			"intent_applied":   intentApplied,
			"mode":             mode,
			"view":             view,
			"summary":          "screenshot captured",
			"data":             res,
			"evidence_handles": handles,
			"truncated":        false,
		}, nil

	case "react":
		if sessionID == "" {
			return map[string]interface{}{"success": false, "error": "session_id is required for react mode"}, nil
		}
		delegate := &ReifyReactTool{sessions: t.sessions, engine: t.engine}
		res, err := delegate.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
		})
		if err != nil {
			return nil, err
		}
		resMap := asMap(res)
		handles := []string{"observe:" + sessionID + ":react"}
		emitDisclosureFacts(ctx, t.engine, sessionID, handles, "observe")
		response := map[string]interface{}{
			"success":          true,
			"status":           "ok",
			"intent":           ternaryStatus(hasIntent, intent, "custom"),
			"intent_applied":   intentApplied,
			"mode":             mode,
			"view":             view,
			"evidence_handles": handles,
			"truncated":        false,
		}
		switch view {
		case "summary":
			componentCount := asInt(resMap["component_count"])
			response["summary"] = fmt.Sprintf("React tree: %d component(s)", componentCount)
			response["data"] = map[string]interface{}{
				"component_count": componentCount,
				"success":         resMap["success"],
			}
		case "compact":
			componentCount := asInt(resMap["component_count"])
			response["summary"] = fmt.Sprintf("React tree: %d component(s)", componentCount)
			response["data"] = resMap
		default:
			response["data"] = resMap
		}
		return response, nil

	case "dom_snapshot":
		if sessionID == "" {
			return map[string]interface{}{"success": false, "error": "session_id is required for dom_snapshot mode"}, nil
		}
		delegate := &SnapshotDOMTool{sessions: t.sessions, engine: t.engine}
		res, err := delegate.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
		})
		if err != nil {
			return nil, err
		}
		resMap := asMap(res)
		handles := []string{"observe:" + sessionID + ":dom_snapshot"}
		emitDisclosureFacts(ctx, t.engine, sessionID, handles, "observe")
		response := map[string]interface{}{
			"success":          true,
			"status":           "ok",
			"intent":           ternaryStatus(hasIntent, intent, "custom"),
			"intent_applied":   intentApplied,
			"mode":             mode,
			"view":             view,
			"evidence_handles": handles,
			"truncated":        false,
		}
		switch view {
		case "summary":
			nodeCount := asInt(resMap["node_count"])
			response["summary"] = fmt.Sprintf("DOM snapshot: %d node(s)", nodeCount)
			response["data"] = map[string]interface{}{
				"node_count": nodeCount,
				"success":    resMap["success"],
			}
		case "compact":
			nodeCount := asInt(resMap["node_count"])
			response["summary"] = fmt.Sprintf("DOM snapshot: %d node(s)", nodeCount)
			response["data"] = resMap
		default:
			response["data"] = resMap
		}
		return response, nil
	}

	// Require session_id for original modes
	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}

	stateTool := &GetPageStateTool{sessions: t.sessions}
	navTool := &GetNavigationLinksTool{sessions: t.sessions, engine: t.engine}
	interactiveTool := &GetInteractiveElementsTool{sessions: t.sessions, engine: t.engine}
	hiddenTool := &DiscoverHiddenContentTool{sessions: t.sessions}

	stateData := map[string]interface{}{}
	navData := map[string]interface{}{}
	interactiveData := map[string]interface{}{}
	interactivePlanningData := map[string]interface{}{}
	hiddenData := map[string]interface{}{}
	diagnosticsData := map[string]interface{}{}
	toastData := map[string]interface{}{}

	fetchState := mode == "state" || mode == "composite"
	fetchNav := mode == "nav" || mode == "composite"
	fetchInteractive := mode == "interactive" || mode == "composite"
	fetchHidden := mode == "hidden" || (mode == "composite" && view == "full")
	fetchDiagnostics := includeDiagnostics

	// Planning snapshot: when action planning is enabled, prefer a wider interactive extraction
	// than the returned output to avoid missing primary CTAs (while keeping output token-light).
	planningLimit := maxItems
	planningFilter := filter
	if includeActionPlan {
		planningLimit = maxInt(maxItems, 80)
		// Action planning benefits from seeing all interactive elements even if output filter is narrower.
		planningFilter = "all"
	}

	if fetchState {
		res, err := stateTool.Execute(ctx, map[string]interface{}{"session_id": sessionID})
		if err != nil {
			return nil, err
		}
		stateData = asMap(res)
	}

	if fetchNav {
		res, err := navTool.Execute(ctx, map[string]interface{}{
			"session_id":    sessionID,
			"internal_only": internalOnly,
			"max_per_area":  maxInt(maxItems, 20),
			"emit_facts":    emitFacts,
		})
		if err != nil {
			return nil, err
		}
		navData = asMap(res)
	}

	if fetchInteractive {
		res, err := interactiveTool.Execute(ctx, map[string]interface{}{
			"session_id":   sessionID,
			"filter":       planningFilter,
			"visible_only": visibleOnly,
			"limit":        planningLimit,
			"verbose":      view == "full",
		})
		if err != nil {
			return nil, err
		}
		interactivePlanningData = asMap(res)
		if fetchInteractive {
			interactiveData = filterInteractiveData(interactivePlanningData, filter)
		}
	}

	if fetchHidden {
		res, err := hiddenTool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
		})
		if err != nil {
			return nil, err
		}
		hiddenData = asMap(res)
	}

	if fetchDiagnostics {
		diagTool := &DiagnosePageTool{engine: t.engine}
		diagView := "summary"
		if view == "full" {
			diagView = "full"
		}
		res, err := diagTool.Execute(ctx, map[string]interface{}{
			"session_id": sessionID,
			"view":       diagView,
			"max_items":  minInt(maxItems, 20),
		})
		if err == nil {
			diagnosticsData = asMap(res)
		} else {
			diagnosticsData = map[string]interface{}{"status": "error", "summary": err.Error()}
		}

		if t.engine != nil {
			toastTool := &GetToastNotificationsTool{engine: t.engine}
			toastView := "summary"
			if view == "compact" {
				toastView = "compact"
			} else if view == "full" {
				toastView = "full"
			}
			toastLimit := minInt(maxItems, 10)
			level := "all"
			if view != "full" {
				level = "error"
			}
			toastRes, tErr := toastTool.Execute(ctx, map[string]interface{}{
				"session_id":           sessionID,
				"level":                level,
				"view":                 toastView,
				"include_correlations": view == "full",
				"limit":                toastLimit,
			})
			if tErr == nil {
				toastData = asMap(toastRes)
			} else {
				toastData = map[string]interface{}{"status": "error", "summary": tErr.Error()}
			}
		} else {
			toastData = map[string]interface{}{"status": "unavailable", "summary": "mangle engine unavailable"}
		}
	}

	handles := make([]string, 0, 4)
	data := map[string]interface{}{}

	if fetchState {
		handles = append(handles, "observe:"+sessionID+":state")
	}
	if fetchNav {
		handles = append(handles, "observe:"+sessionID+":nav")
	}
	if fetchInteractive {
		handles = append(handles, "observe:"+sessionID+":interactive")
	}
	if fetchHidden {
		handles = append(handles, "observe:"+sessionID+":hidden")
	}
	if fetchDiagnostics {
		handles = append(handles, "observe:"+sessionID+":diagnostics")
		handles = append(handles, "observe:"+sessionID+":toasts")
	}

	actionCandidates := []map[string]interface{}{}
	recommendations := []map[string]interface{}{}
	if includeActionPlan && t.engine != nil && (fetchInteractive || fetchNav || mode == "composite") {
		// Query more than we plan to return so we can filter stale candidates safely.
		queryLimit := maxInt(planningLimit*4, 300)
		actionCandidatesRaw := queryActionCandidates(ctx, t.engine, sessionID, queryLimit)

		// Filter to candidates that match the *current* observe snapshot (prevents stale actions from prior pages).
		allowedRefs := buildRefSet(interactivePlanningData)
		allowedHrefs := buildHrefSet(navData)
		actionCandidates = filterActionCandidates(actionCandidatesRaw, allowedRefs, allowedHrefs)

		currentURL := getStringFromMap(stateData, "url")
		if currentURL == "" {
			currentURL = resolveCurrentURL(ctx, t.engine, sessionID)
		}
		recommendations = buildActionPlanRecommendations(actionCandidates, maxRecommendations, sessionID, originFromURL(currentURL))
		handles = append(handles, "observe:"+sessionID+":action_candidates")
		handles = append(handles, "observe:"+sessionID+":recommendations")
	}

	switch view {
	case "summary":
		if fetchState {
			data["state"] = map[string]interface{}{
				"url":       getStringFromMap(stateData, "url"),
				"title":     getStringFromMap(stateData, "title"),
				"loading":   stateData["loading"],
				"hasDialog": stateData["hasDialog"],
			}
		}
		if fetchNav {
			if counts, ok := navData["counts"].(map[string]interface{}); ok {
				data["nav_counts"] = counts
			}
		}
		if fetchInteractive {
			if summary, ok := interactiveData["summary"].(map[string]interface{}); ok {
				data["interactive_summary"] = summary
			}
		}
		if fetchHidden {
			data["hidden_count"] = countHiddenElements(hiddenData)
		}
		if fetchDiagnostics {
			data["diagnostics"] = map[string]interface{}{
				"status":  getStringFromMap(diagnosticsData, "status"),
				"counts":  diagnosticsData["counts"],
				"summary": diagnosticsData["summary"],
			}
			data["toasts"] = map[string]interface{}{
				"error_count":   toastData["error_count"],
				"warning_count": toastData["warning_count"],
				"success_count": toastData["success_count"],
				"info_count":    toastData["info_count"],
				"summary":       toastData["summary"],
			}
		}
		if includeActionPlan {
			data["action_candidate_count"] = len(actionCandidates)
			data["recommendation_count"] = len(recommendations)
		}
	case "compact":
		if fetchState {
			data["state"] = stateData
		}
		if fetchNav {
			data["nav"] = navData
		}
		if fetchInteractive {
			data["interactive"] = compactInteractiveData(interactiveData, maxItems)
		}
		if fetchHidden {
			data["hidden"] = compactHiddenData(hiddenData, maxItems)
		}
		if fetchDiagnostics {
			data["diagnostics"] = map[string]interface{}{
				"status":  getStringFromMap(diagnosticsData, "status"),
				"counts":  diagnosticsData["counts"],
				"summary": diagnosticsData["summary"],
			}
			data["toasts"] = compactToastData(toastData, maxItems)
		}
		if includeActionPlan {
			data["action_candidates"] = limitMapSlice(actionCandidates, maxItems)
			data["recommendations"] = recommendations
		}
	default: // full
		if fetchState {
			data["state"] = stateData
		}
		if fetchNav {
			data["nav"] = navData
		}
		if fetchInteractive {
			data["interactive"] = interactiveData
		}
		if fetchHidden {
			data["hidden"] = hiddenData
		}
		if fetchDiagnostics {
			data["diagnostics"] = diagnosticsData
			data["toasts"] = toastData
		}
		if includeActionPlan {
			data["action_candidates"] = actionCandidates
			data["recommendations"] = recommendations
		}
	}

	summary := buildObserveSummary(data)
	emitDisclosureFacts(ctx, t.engine, sessionID, handles, "observe")

	return map[string]interface{}{
		"success":          true,
		"status":           "ok",
		"intent":           ternaryStatus(hasIntent, intent, "custom"),
		"intent_applied":   intentApplied,
		"mode":             mode,
		"view":             view,
		"summary":          summary,
		"data":             data,
		"next_step":        suggestObserveNextStep(sessionID, data, mode, view, recommendations),
		"evidence_handles": handles,
		"truncated":        false,
	}, nil
}

// BrowserActTool consolidates browser actions with progressive-disclosure results.
type BrowserActTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *BrowserActTool) Name() string { return "browser-act" }
func (t *BrowserActTool) Description() string {
	return `Do things in the browser -- navigate, click, type, manage sessions, wait, run JS.

USE THIS TOOL to perform actions. Use browser-observe first to get ref IDs for elements.

OPERATIONS (pass as array -- multiple ops execute in sequence):

  Session management (no session_id needed):
    {type:"session_create", url:"https://example.com"}         -> Open new tab
    {type:"session_attach", target_id:"TARGET-ID"}             -> Attach to existing tab
    {type:"session_fork", source_session_id:"s1", url:"..."}   -> Clone tab with cookies/auth

  Navigation:
    {type:"navigate", url:"https://example.com"}               -> Go to URL
    {type:"history", action:"back"}                            -> back, forward, reload

  Interaction (use ref from browser-observe interactive mode):
    {type:"interact", ref:"btn-submit", action:"click"}                          -> Click element
    {type:"interact", ref:"input-email", action:"type", value:"user@test.com"}   -> Type into input
    {type:"interact", ref:"select-country", action:"select", value:"US"}         -> Select dropdown
    {type:"interact", ref:"checkbox-agree", action:"toggle"}                     -> Toggle checkbox

  Forms (batch fill multiple fields at once):
    {type:"fill", fields:[{ref:"input-email",value:"a@b.com"},{ref:"input-pass",value:"x"}]}

  Keyboard:
    {type:"key", key:"Enter"}                                  -> Press key
    {type:"key", key:"Control+a"}                              -> Key combination

  Waiting:
    {type:"await_stable", timeout_ms:10000}                    -> Wait for no network/DOM activity
    {type:"await_fact", predicate:"login_succeeded", args:["sess-1"], timeout_ms:15000}
    {type:"await_conditions", conditions:[{predicate:"p1",args:["a"]}], timeout_ms:10000}
    {type:"wait", predicate:"current_url", match_args:{"Url":"/dashboard"}, timeout_ms:10000}
    {type:"sleep", ms:2000}                                    -> Hard pause (avoid if possible)

  Advanced:
    {type:"js", script:"document.title", timeout_ms:5000}      -> Eval JS (gated)
    {type:"plan", actions:[...], predicate:"done()", delay_ms:500} -> Mangle-derived plan

MULTI-STEP EXAMPLE (login flow):
  {session_id:"sess-1", operations:[
    {type:"interact", ref:"input-email", action:"type", value:"user@co.com"},
    {type:"interact", ref:"input-pass", action:"type", value:"secret"},
    {type:"interact", ref:"btn-login", action:"click"},
    {type:"await_stable", timeout_ms:5000}
  ]}

OPTIONS:
  stop_on_error: true (default) -- halt sequence on first failure
  view: "compact" (default) -- summary|compact|full controls result detail

WHEN TO USE vs OTHER TOOLS:
  "I need to click/type/navigate"          -> browser-act
  "I need to understand the page first"    -> browser-observe
  "Something went wrong, why?"             -> browser-reason
  "I need to query Mangle facts directly"  -> browser-mangle`
}

func (t *BrowserActTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session",
			},
			"operations": map[string]interface{}{
				"type":        "array",
				"description": "Action operations to execute",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"type": map[string]interface{}{
							"type": "string",
							"enum": []string{"navigate", "interact", "fill", "key", "history", "sleep", "session_create", "session_attach", "session_fork", "wait", "await_stable", "await_fact", "await_conditions", "js", "plan"},
						},
					},
					"required": []string{"type"},
				},
			},
			"stop_on_error": map[string]interface{}{
				"type":        "boolean",
				"description": "Stop at first failed operation (default true)",
			},
			"view": map[string]interface{}{
				"type":        "string",
				"description": "summary|compact|full",
				"enum":        []string{"summary", "compact", "full"},
			},
			"max_items": map[string]interface{}{
				"type":        "integer",
				"description": "Max operation results returned in compact mode (default 20)",
			},
		},
		"required": []string{"operations"},
	}
}

func (t *BrowserActTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")

	rawOps, ok := args["operations"].([]interface{})
	if !ok || len(rawOps) == 0 {
		return map[string]interface{}{"success": false, "error": "operations must be a non-empty array"}, nil
	}

	stopOnError := getBoolArg(args, "stop_on_error", true)
	view := normalizeProgressiveView(getStringArg(args, "view"))
	maxItems := getIntArg(args, "max_items", defaultProgressiveMaxItems)
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}

	navTool := &NavigateURLTool{sessions: t.sessions, engine: t.engine}
	interactTool := &InteractTool{sessions: t.sessions, engine: t.engine}
	fillTool := &FillFormTool{sessions: t.sessions, engine: t.engine}
	keyTool := &PressKeyTool{sessions: t.sessions, engine: t.engine}
	historyTool := &BrowserHistoryTool{sessions: t.sessions, engine: t.engine}

	results := make([]map[string]interface{}, 0, len(rawOps))
	succeeded := 0
	failed := 0

	for idx, raw := range rawOps {
		op, ok := raw.(map[string]interface{})
		if !ok {
			results = append(results, map[string]interface{}{
				"index":   idx,
				"type":    "unknown",
				"success": false,
				"error":   "operation must be an object",
			})
			failed++
			if stopOnError {
				break
			}
			continue
		}

		opType := strings.ToLower(getStringFromMap(op, "type"))
		entry := map[string]interface{}{
			"index": idx,
			"type":  opType,
		}

		var (
			opResult interface{}
			err      error
		)

		switch opType {
		case "navigate":
			url := getStringFromMap(op, "url")
			waitUntil := getStringFromMap(op, "wait_until")
			opResult, err = navTool.Execute(ctx, map[string]interface{}{
				"session_id": sessionID,
				"url":        url,
				"wait_until": waitUntil,
			})
		case "interact":
			opResult, err = interactTool.Execute(ctx, map[string]interface{}{
				"session_id": sessionID,
				"ref":        getStringFromMap(op, "ref"),
				"action":     getStringFromMap(op, "action"),
				"value":      getStringFromMap(op, "value"),
				"submit":     op["submit"],
			})
		case "fill":
			opResult, err = fillTool.Execute(ctx, map[string]interface{}{
				"session_id":    sessionID,
				"fields":        op["fields"],
				"submit":        op["submit"],
				"submit_button": getStringFromMap(op, "submit_button"),
			})
		case "key":
			opResult, err = keyTool.Execute(ctx, map[string]interface{}{
				"session_id": sessionID,
				"key":        getStringFromMap(op, "key"),
				"modifiers":  op["modifiers"],
			})
		case "history":
			opResult, err = historyTool.Execute(ctx, map[string]interface{}{
				"session_id": sessionID,
				"action":     getStringFromMap(op, "action"),
			})
		case "sleep":
			ms := getIntArg(op, "duration_ms", 250)
			if ms < 0 {
				ms = 0
			}
			err = sleepWithContext(ctx, time.Duration(ms)*time.Millisecond)
			opResult = map[string]interface{}{"success": err == nil, "slept_ms": ms}

		case "session_create":
			createTool := &CreateSessionTool{sessions: t.sessions}
			opResult, err = createTool.Execute(ctx, map[string]interface{}{
				"url": getStringFromMap(op, "url"),
			})

		case "session_attach":
			attachTool := &AttachSessionTool{sessions: t.sessions}
			opResult, err = attachTool.Execute(ctx, map[string]interface{}{
				"target_id": getStringFromMap(op, "target_id"),
			})

		case "session_fork":
			forkTool := &ForkSessionTool{sessions: t.sessions}
			forkArgs := map[string]interface{}{
				"source_session_id": getStringFromMap(op, "source_session_id"),
			}
			if u := getStringFromMap(op, "url"); u != "" {
				forkArgs["url"] = u
			}
			opResult, err = forkTool.Execute(ctx, forkArgs)

		case "wait":
			waitTool := &WaitForConditionTool{sessions: t.sessions, engine: t.engine}
			waitArgs := map[string]interface{}{
				"predicate": getStringFromMap(op, "predicate"),
			}
			if v, ok := op["match_args"]; ok {
				waitArgs["match_args"] = v
			}
			if v, ok := op["timeout_ms"]; ok {
				waitArgs["timeout_ms"] = v
			}
			opResult, err = waitTool.Execute(ctx, waitArgs)

		case "await_stable":
			stableTool := &AwaitStableStateTool{engine: t.engine}
			stableArgs := map[string]interface{}{}
			if v, ok := op["timeout_ms"]; ok {
				stableArgs["timeout_ms"] = v
			}
			opResult, err = stableTool.Execute(ctx, stableArgs)

		case "await_fact":
			awaitTool := &AwaitFactTool{engine: t.engine}
			awaitArgs := map[string]interface{}{
				"predicate": getStringFromMap(op, "predicate"),
			}
			if v, ok := op["args"]; ok {
				awaitArgs["args"] = v
			}
			if v, ok := op["timeout_ms"]; ok {
				awaitArgs["timeout_ms"] = v
			}
			opResult, err = awaitTool.Execute(ctx, awaitArgs)

		case "await_conditions":
			condTool := &AwaitConditionsTool{engine: t.engine}
			condArgs := map[string]interface{}{}
			if v, ok := op["conditions"]; ok {
				condArgs["conditions"] = v
			}
			if v, ok := op["timeout_ms"]; ok {
				condArgs["timeout_ms"] = v
			}
			opResult, err = condTool.Execute(ctx, condArgs)

		case "js":
			jsTool := &EvaluateJSTool{sessions: t.sessions, engine: t.engine}
			jsArgs := map[string]interface{}{
				"session_id": sessionID,
				"script":     getStringFromMap(op, "script"),
			}
			if v, ok := op["timeout_ms"]; ok {
				jsArgs["timeout_ms"] = v
			}
			if v, ok := op["gate_reason"]; ok {
				jsArgs["gate_reason"] = v
			}
			if v, ok := op["approved_by_handle"]; ok {
				jsArgs["approved_by_handle"] = v
			}
			opResult, err = jsTool.Execute(ctx, jsArgs)

		case "plan":
			planTool := &ExecutePlanTool{sessions: t.sessions, engine: t.engine}
			planArgs := map[string]interface{}{
				"session_id": sessionID,
			}
			if v, ok := op["actions"]; ok {
				planArgs["actions"] = v
			}
			if v, ok := op["predicate"]; ok {
				planArgs["predicate"] = v
			}
			if v, ok := op["delay_ms"]; ok {
				planArgs["delay_ms"] = v
			}
			opResult, err = planTool.Execute(ctx, planArgs)

		default:
			err = fmt.Errorf("unknown operation type: %s", opType)
		}

		success := err == nil
		if resultMap, ok := opResult.(map[string]interface{}); ok {
			if s, exists := resultMap["success"].(bool); exists {
				success = success && s
			}
		}

		if err != nil {
			entry["error"] = err.Error()
		}
		entry["success"] = success
		entry["result"] = opResult

		results = append(results, entry)
		if success {
			succeeded++
		} else {
			failed++
			if stopOnError {
				break
			}
		}
	}

	now := time.Now().UnixMilli()
	handle := fmt.Sprintf("act:%s:%d", sessionID, now)
	emitDisclosureFacts(ctx, t.engine, sessionID, []string{handle}, "act")

	response := map[string]interface{}{
		"success":          failed == 0,
		"status":           ternaryStatus(failed == 0, "ok", "error"),
		"summary":          fmt.Sprintf("Executed %d operation(s): %d succeeded, %d failed", len(results), succeeded, failed),
		"counts":           map[string]interface{}{"total": len(results), "succeeded": succeeded, "failed": failed},
		"evidence_handles": []string{handle},
		"view":             view,
	}

	switch view {
	case "summary":
		// no per-operation payload
	case "compact":
		compact := make([]map[string]interface{}, 0, len(results))
		for _, r := range results {
			compact = append(compact, map[string]interface{}{
				"index":   r["index"],
				"type":    r["type"],
				"success": r["success"],
				"error":   r["error"],
			})
		}
		response["results"] = limitMapSlice(compact, maxItems)
		response["truncated"] = len(compact) > maxItems
	default:
		response["results"] = results
		response["truncated"] = false
	}

	return response, nil
}

// BrowserReasonTool performs Mangle-first reasoning with progressive disclosure.
type BrowserReasonTool struct {
	engine       *mangle.Engine
	dockerClient *docker.Client
}

func (t *BrowserReasonTool) Name() string { return "browser-reason" }
func (t *BrowserReasonTool) Description() string {
	return `Diagnose browser problems -- health checks, root cause analysis, blocking issues, recommendations.

USE THIS TOOL when something goes wrong or you need guidance on what to do next.
Uses Mangle causal reasoning over collected facts (network, console, DOM, Docker logs).

TOPICS:
  health:             Overall page health score with error/warning counts.
                      "Is this page healthy? Any errors?"
  next_best_action:   Ranked recommendations for what to do next.
                      "What should I do now?"
  blocking_issue:     Identify what prevents progress (modals, auth walls, errors).
                      "Why can't I interact with the page?"
  why_failed:         Root cause analysis of failures with causal chains.
                      "The last action failed -- why?"
  what_changed_since: Diff facts since a timestamp to detect state changes.
                      "What changed after I clicked that button?"

INTENTS (shortcuts that set topic + view defaults):
  triage:        -> topic:"health", view:"summary"    Quick health check
  act_now:       -> topic:"next_best_action"           What should I do?
  debug_failure: -> topic:"why_failed", view:"full"    Deep failure analysis
  unblock:       -> topic:"blocking_issue"             Find what's blocking me

VIEWS:
  summary: Verdict + counts only. Includes handles for drill-down.
  compact: Verdict + key evidence. Default -- good for decisions.
  full:    Everything -- all root causes, causal chains, recommendations.

EVIDENCE HANDLES:
  Results include handles like "failed_requests", "root_causes", "slow_apis".
  Pass these back via handles:["failed_requests"] to expand specific sections.

EXAMPLES:
  {session_id:"sess-1", intent:"triage"}
  {session_id:"sess-1", topic:"why_failed", view:"full"}
  {session_id:"sess-1", topic:"health", handles:["failed_requests","slow_apis"]}
  {session_id:"sess-1", topic:"what_changed_since", since_ms:1706000000000}

WHEN TO USE vs OTHER TOOLS:
  "Page looks broken"                      -> browser-reason topic:"health"
  "What should I do next?"                 -> browser-reason topic:"next_best_action"
  "Something is blocking the page"         -> browser-reason topic:"blocking_issue"
  "My click didn't work"                   -> browser-reason topic:"why_failed"
  "I need to query Mangle facts directly"  -> browser-mangle (not browser-reason)

INCLUDES DOCKER LOG CORRELATION:
  When Docker integration is enabled, failed API requests are automatically correlated
  with backend container logs, providing full-stack error chains.`
}

func (t *BrowserReasonTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Session context for gating and handles",
			},
			"intent": map[string]interface{}{
				"type":        "string",
				"description": "Reasoning preset that applies topic/view defaults when explicit knobs are omitted",
				"enum":        []string{"triage", "act_now", "debug_failure", "unblock"},
			},
			"topic": map[string]interface{}{
				"type":        "string",
				"description": "Reasoning topic",
				"enum":        []string{"health", "next_best_action", "blocking_issue", "why_failed", "what_changed_since"},
			},
			"view": map[string]interface{}{
				"type":        "string",
				"description": "summary|compact|full",
				"enum":        []string{"summary", "compact", "full"},
			},
			"max_items": map[string]interface{}{
				"type":        "integer",
				"description": "Max rows per section (default 20)",
			},
			"expand_handles": map[string]interface{}{
				"type":        "array",
				"description": "Only expand matching evidence handles",
				"items":       map[string]interface{}{"type": "string"},
			},
			"include_action_plan": map[string]interface{}{
				"type":        "boolean",
				"description": "Include Mangle-derived browser-act operation recommendations (default true)",
			},
			"max_recommendations": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum recommendation rows to return (default 4)",
			},
			"time_window_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Only include evidence newer than now-window (default 300000; set 0 for all history)",
			},
		},
		"required": []string{"session_id"},
	}
}

func (t *BrowserReasonTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if t.engine == nil {
		return map[string]interface{}{"success": false, "error": "mangle engine is not available"}, nil
	}

	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}

	intent := normalizeReasonIntent(getStringArg(args, "intent"))
	intentCfg, hasIntent := resolveReasonIntentDefaults(intent)
	topic := strings.ToLower(getStringArg(args, "topic"))
	view := normalizeProgressiveView(getStringArg(args, "view"))
	if hasIntent {
		if !argHasNonEmptyString(args, "topic") && intentCfg.topic != "" {
			topic = intentCfg.topic
		}
		if !argHasNonEmptyString(args, "view") && intentCfg.view != "" {
			view = intentCfg.view
		}
	}
	if topic == "" {
		topic = "health"
	}
	maxItems := getIntArg(args, "max_items", defaultProgressiveMaxItems)
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}
	includeActionPlan := getBoolArg(args, "include_action_plan", true)
	maxRecommendations := getIntArg(args, "max_recommendations", defaultReasonMaxRecs)
	if maxRecommendations <= 0 {
		maxRecommendations = defaultReasonMaxRecs
	}
	timeWindowMs := getIntArg(args, "time_window_ms", defaultReasonTimeWindowMs)
	if timeWindowMs < 0 {
		timeWindowMs = 0
	}
	if timeWindowMs > 86400000 {
		timeWindowMs = 86400000
	}
	sinceMs := int64(0)
	if timeWindowMs > 0 {
		sinceMs = time.Now().UnixMilli() - int64(timeWindowMs)
	}

	rootCauses := queryToRows(ctx, t.engine, fmt.Sprintf("root_cause_at(%q, ConsoleMsg, Source, Cause, Ts).", sessionID))
	if len(rootCauses) == 0 {
		rootCauses = queryToRows(ctx, t.engine, fmt.Sprintf("root_cause(%q, ConsoleMsg, Source, Cause).", sessionID))
	}
	failedReqs := queryToRows(ctx, t.engine, fmt.Sprintf("failed_request_at(%q, ReqId, Url, Status, ReqTs).", sessionID))
	if len(failedReqs) == 0 {
		failedReqs = queryToRows(ctx, t.engine, fmt.Sprintf("failed_request(%q, ReqId, Url, Status).", sessionID))
	}
	slowApis := queryToRows(ctx, t.engine, fmt.Sprintf("slow_api_at(%q, ReqId, Url, Duration, ReqTs).", sessionID))
	if len(slowApis) == 0 {
		slowApis = queryToRows(ctx, t.engine, fmt.Sprintf("slow_api(%q, ReqId, Url, Duration).", sessionID))
	}
	blockingIssues := filterRowsByField(queryToRows(ctx, t.engine, "interaction_blocked(SessionId, Reason)."), "SessionId", sessionID)
	userVisibleErrors := queryToRows(ctx, t.engine, fmt.Sprintf("user_visible_error(%q, Source, Message, Timestamp).", sessionID))
	actionCandidates := queryActionCandidates(ctx, t.engine, sessionID, maxItems)

	if sinceMs > 0 {
		rootCauses = filterRowsSince(rootCauses, []string{"Ts", "Timestamp"}, sinceMs)
		failedReqs = filterRowsSince(failedReqs, []string{"ReqTs", "Timestamp"}, sinceMs)
		slowApis = filterRowsSince(slowApis, []string{"ReqTs", "Timestamp"}, sinceMs)
		userVisibleErrors = filterRowsSince(userVisibleErrors, []string{"Timestamp", "Ts"}, sinceMs)
	}

	contradictions := detectContradictions(ctx, t.engine, sessionID)

	confidence := computeReasonConfidence(len(rootCauses), len(failedReqs), len(slowApis), len(contradictions), topic)
	status := "ok"
	if len(failedReqs) > 0 || len(contradictions) > 0 || len(rootCauses) > 0 {
		status = "error"
	} else if len(slowApis) > 0 || len(blockingIssues) > 0 {
		status = "warning"
	}

	baseOrigin := originFromURL(resolveCurrentURL(ctx, t.engine, sessionID))
	recommendations := recommendNextActions(sessionID, topic, status, len(failedReqs), len(rootCauses), len(contradictions), confidence)
	if includeActionPlan {
		recommendations = append(buildActionPlanRecommendations(actionCandidates, maxRecommendations, sessionID, baseOrigin), recommendations...)
	}
	recommendations = limitMapSlice(recommendations, maxRecommendations)

	data := map[string]interface{}{
		"failed_requests":     failedReqs,
		"root_causes":         rootCauses,
		"slow_apis":           slowApis,
		"blocking_issues":     blockingIssues,
		"user_visible_errors": userVisibleErrors,
		"contradictions":      contradictions,
		"action_candidates":   actionCandidates,
		"recommendations":     recommendations,
	}
	if topic == "what_changed_since" {
		data["changes"] = buildReasonChangeFeed(rootCauses, failedReqs, slowApis, userVisibleErrors, blockingIssues, maxItems)
	}

	handles := []string{
		"reason:" + sessionID + ":failed_requests",
		"reason:" + sessionID + ":root_causes",
		"reason:" + sessionID + ":slow_apis",
		"reason:" + sessionID + ":blocking_issues",
		"reason:" + sessionID + ":contradictions",
		"reason:" + sessionID + ":action_candidates",
		"reason:" + sessionID + ":recommendations",
	}
	if topic == "what_changed_since" {
		handles = append(handles, "reason:"+sessionID+":changes")
	}
	selectedData := applyHandleFilter(data, args["expand_handles"])

	emitFacts := []mangle.Fact{
		{
			Predicate: "confidence_score",
			Args:      []interface{}{sessionID, topic, int(math.Round(confidence * 100.0)), time.Now().UnixMilli()},
			Timestamp: time.Now(),
		},
	}
	if confidence < 0.70 {
		emitFacts = append(emitFacts, mangle.Fact{
			Predicate: "js_gate_open",
			Args:      []interface{}{sessionID, "low_confidence", time.Now().UnixMilli()},
			Timestamp: time.Now(),
		})
	}
	if len(contradictions) > 0 {
		emitFacts = append(emitFacts, mangle.Fact{
			Predicate: "js_gate_open",
			Args:      []interface{}{sessionID, "contradiction_detected", time.Now().UnixMilli()},
			Timestamp: time.Now(),
		})
	}
	if len(recommendations) == 0 {
		emitFacts = append(emitFacts, mangle.Fact{
			Predicate: "js_gate_open",
			Args:      []interface{}{sessionID, "no_matching_tool", time.Now().UnixMilli()},
			Timestamp: time.Now(),
		})
	}
	if len(emitFacts) > 0 {
		_ = t.engine.AddFacts(ctx, emitFacts)
	}
	emitDisclosureFacts(ctx, t.engine, sessionID, handles, "reason")

	response := map[string]interface{}{
		"success":             true,
		"intent":              ternaryStatus(hasIntent, intent, "custom"),
		"topic":               topic,
		"status":              status,
		"confidence":          confidence,
		"summary":             buildReasonSummary(status, confidence, len(rootCauses), len(failedReqs), len(slowApis), len(contradictions)),
		"evidence_handles":    handles,
		"expansion_suggested": confidence < 0.70 || len(contradictions) > 0,
		"view":                view,
		"time_window_ms":      timeWindowMs,
	}

	switch view {
	case "summary":
		response["counts"] = map[string]interface{}{
			"root_causes":     len(rootCauses),
			"failed_requests": len(failedReqs),
			"slow_apis":       len(slowApis),
			"blocking_issues": len(blockingIssues),
			"contradictions":  len(contradictions),
		}
	case "compact":
		response["data"] = truncateReasonData(selectedData, maxItems)
	default:
		response["data"] = selectedData
	}

	return response, nil
}

func normalizeProgressiveView(view string) string {
	switch strings.ToLower(view) {
	case "summary", "compact", "full":
		return strings.ToLower(view)
	default:
		return "compact"
	}
}

func asMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

func countHiddenElements(hiddenData map[string]interface{}) int {
	elems, ok := hiddenData["hidden_elements"].([]interface{})
	if !ok {
		return 0
	}
	return len(elems)
}

func compactInteractiveData(data map[string]interface{}, maxItems int) map[string]interface{} {
	out := map[string]interface{}{}
	if summary, ok := data["summary"]; ok {
		out["summary"] = summary
	}
	if elems, ok := data["elements"].([]interface{}); ok {
		out["elements"] = limitAnySlice(elems, maxItems)
		out["truncated"] = len(elems) > maxItems
	}
	return out
}

func compactHiddenData(data map[string]interface{}, maxItems int) map[string]interface{} {
	out := map[string]interface{}{}
	if elems, ok := data["hidden_elements"].([]interface{}); ok {
		out["hidden_elements"] = limitAnySlice(elems, maxItems)
		out["count"] = len(elems)
		out["truncated"] = len(elems) > maxItems
	}
	if summary, ok := data["summary"]; ok {
		out["summary"] = summary
	}
	return out
}

func compactToastData(data map[string]interface{}, maxItems int) map[string]interface{} {
	out := map[string]interface{}{}
	if status, ok := data["status"]; ok && status != nil {
		out["status"] = status
	}
	if summary, ok := data["summary"]; ok && summary != nil {
		out["summary"] = summary
	}

	for _, k := range []string{"error_count", "warning_count", "success_count", "info_count"} {
		if v, ok := data[k]; ok && v != nil {
			out[k] = v
		}
	}

	if reps, ok := data["repeated_errors"].([]interface{}); ok && len(reps) > 0 {
		out["repeated_errors"] = limitAnySlice(reps, minInt(maxItems, 5))
	}

	// Include a small sample of toasts only if present.
	if toasts, ok := data["toasts"].([]interface{}); ok && len(toasts) > 0 {
		limit := minInt(maxItems, 5)
		out["toasts"] = limitAnySlice(toasts, limit)
		out["toast_count"] = len(toasts)
		out["truncated"] = len(toasts) > limit
	}

	return out
}

func buildObserveSummary(data map[string]interface{}) string {
	parts := make([]string, 0, 4)
	if state, ok := data["state"].(map[string]interface{}); ok {
		if loading, exists := state["loading"].(bool); exists {
			parts = append(parts, fmt.Sprintf("loading=%t", loading))
		}
	}
	if diag, ok := data["diagnostics"].(map[string]interface{}); ok {
		status := strings.TrimSpace(getStringFromMap(diag, "status"))
		if status != "" && status != "ok" {
			parts = append(parts, "diag="+status)
		}
	}
	if toasts, ok := data["toasts"].(map[string]interface{}); ok {
		if errCount := asInt(toasts["error_count"]); errCount > 0 {
			parts = append(parts, fmt.Sprintf("toast_err=%d", errCount))
		}
	}
	if navCounts, ok := data["nav_counts"].(map[string]interface{}); ok {
		if total, exists := navCounts["total"]; exists {
			parts = append(parts, fmt.Sprintf("links=%v", total))
		}
	} else if nav, ok := data["nav"].(map[string]interface{}); ok {
		if counts, ok := nav["counts"].(map[string]interface{}); ok {
			if total, exists := counts["total"]; exists {
				parts = append(parts, fmt.Sprintf("links=%v", total))
			}
		}
	}
	if interSummary, ok := data["interactive_summary"].(map[string]interface{}); ok {
		if total, exists := interSummary["total"]; exists {
			parts = append(parts, fmt.Sprintf("interactive=%v", total))
		}
	} else if inter, ok := data["interactive"].(map[string]interface{}); ok {
		if summary, ok := inter["summary"].(map[string]interface{}); ok {
			if total, exists := summary["total"]; exists {
				parts = append(parts, fmt.Sprintf("interactive=%v", total))
			}
		}
	}
	if candidateCount := asInt(data["action_candidate_count"]); candidateCount > 0 {
		parts = append(parts, fmt.Sprintf("candidates=%d", candidateCount))
	} else if candidates, ok := data["action_candidates"].([]map[string]interface{}); ok {
		parts = append(parts, fmt.Sprintf("candidates=%d", len(candidates)))
	} else if candidatesAny, ok := data["action_candidates"].([]interface{}); ok && len(candidatesAny) > 0 {
		parts = append(parts, fmt.Sprintf("candidates=%d", len(candidatesAny)))
	}
	if len(parts) == 0 {
		return "observation complete"
	}
	return "observation: " + strings.Join(parts, ", ")
}

func queryToRows(ctx context.Context, engine *mangle.Engine, query string) []map[string]interface{} {
	results, err := engine.Query(ctx, query)
	if err != nil {
		return []map[string]interface{}{}
	}
	rows := make([]map[string]interface{}, 0, len(results))
	for _, r := range results {
		row := make(map[string]interface{}, len(r))
		for k, v := range r {
			row[k] = v
		}
		rows = append(rows, row)
	}
	return rows
}

func detectContradictions(ctx context.Context, engine *mangle.Engine, sessionID string) []map[string]interface{} {
	contradictions := make([]map[string]interface{}, 0)
	if engine == nil || sessionID == "" {
		return contradictions
	}

	// failed_request is derived; query the store, scoped to the session.
	failedRows := queryToRows(ctx, engine, fmt.Sprintf("failed_request(%q, ReqId, Url, Status).", sessionID))
	if len(failedRows) == 0 {
		return contradictions
	}

	// toast_notification is a base event stored in the temporal buffer.
	toasts := engine.FactsByPredicate("toast_notification")
	successToastCount := 0
	for _, t := range toasts {
		// toast_notification(SessionId, Text, Level, Source, Timestamp)
		if len(t.Args) < 3 {
			continue
		}
		if fmt.Sprintf("%v", t.Args[0]) != sessionID {
			continue
		}
		level := fmt.Sprintf("%v", t.Args[2])
		if level == "success" {
			successToastCount++
		}
	}

	if successToastCount > 0 {
		contradictions = append(contradictions, map[string]interface{}{
			"type":                    "success_toast_with_failed_requests",
			"failed_request_count":    len(failedRows),
			"success_toast_count":     successToastCount,
			"confidence_impact_delta": -0.25,
		})
	}

	return contradictions
}

func computeReasonConfidence(rootCauses, failedReqs, slowApis, contradictions int, topic string) float64 {
	score := 0.95
	if failedReqs > 0 {
		score = 0.80
	}
	if rootCauses > 0 {
		score += 0.08
	}
	if slowApis > 0 && failedReqs == 0 {
		score -= 0.10
	}
	if contradictions > 0 {
		score -= 0.25
	}
	if topic == "next_best_action" && rootCauses == 0 && failedReqs == 0 && contradictions == 0 {
		score -= 0.20
	}
	return math.Max(0.10, math.Min(0.99, score))
}

func recommendNextActions(sessionID, topic, status string, failedReqs, rootCauses, contradictions int, confidence float64) []map[string]interface{} {
	_ = topic
	recs := make([]map[string]interface{}, 0, 3)
	if status == "ok" {
		recs = append(recs, map[string]interface{}{
			"tool":   "browser-observe",
			"reason": "No critical issues detected; continue with focused observation.",
			"args": map[string]interface{}{
				"session_id": sessionID,
				"mode":       "interactive",
				"view":       "compact",
			},
		})
	}
	if failedReqs > 0 || rootCauses > 0 {
		recs = append(recs, map[string]interface{}{
			"tool":   "browser-reason",
			"reason": "Expand failure evidence for targeted remediation.",
			"args": map[string]interface{}{
				"session_id": sessionID,
				"topic":      "why_failed",
				"view":       "full",
			},
		})
	}
	if contradictions > 0 {
		recs = append(recs, map[string]interface{}{
			"tool":   "browser-act",
			"reason": "Contradiction detected; JS inspection is now permitted.",
			"args": map[string]interface{}{
				"session_id": sessionID,
				"operations": []map[string]interface{}{
					{"type": "js", "gate_reason": "contradiction_detected"},
				},
			},
		})
	}
	if confidence < 0.70 {
		recs = append(recs, map[string]interface{}{
			"tool":   "browser-act",
			"reason": "Low confidence reasoning result; permit targeted JS fallback.",
			"args": map[string]interface{}{
				"session_id": sessionID,
				"operations": []map[string]interface{}{
					{"type": "js", "gate_reason": "low_confidence"},
				},
			},
		})
	}
	return recs
}

func buildReasonSummary(status string, confidence float64, rootCauses, failedReqs, slowApis, contradictions int) string {
	return fmt.Sprintf(
		"status=%s confidence=%.2f root_causes=%d failed_requests=%d slow_apis=%d contradictions=%d",
		status,
		confidence,
		rootCauses,
		failedReqs,
		slowApis,
		contradictions,
	)
}

func truncateReasonData(data map[string]interface{}, maxItems int) map[string]interface{} {
	out := make(map[string]interface{}, len(data))
	for k, v := range data {
		switch rows := v.(type) {
		case []map[string]interface{}:
			out[k] = limitMapSlice(rows, maxItems)
		default:
			out[k] = v
		}
	}
	return out
}

func applyHandleFilter(data map[string]interface{}, rawHandles interface{}) map[string]interface{} {
	raw, ok := rawHandles.([]interface{})
	if !ok || len(raw) == 0 {
		return data
	}

	selected := make(map[string]bool)
	for _, h := range raw {
		handle := strings.ToLower(fmt.Sprintf("%v", h))
		switch {
		case strings.Contains(handle, "failed_requests"):
			selected["failed_requests"] = true
		case strings.Contains(handle, "root_causes"):
			selected["root_causes"] = true
		case strings.Contains(handle, "slow_apis"):
			selected["slow_apis"] = true
		case strings.Contains(handle, "blocking_issues"):
			selected["blocking_issues"] = true
		case strings.Contains(handle, "contradictions"):
			selected["contradictions"] = true
		case strings.Contains(handle, "action_candidates"):
			selected["action_candidates"] = true
		case strings.Contains(handle, "recommendations"):
			selected["recommendations"] = true
		case strings.Contains(handle, "user_visible_errors"):
			selected["user_visible_errors"] = true
		case strings.Contains(handle, "changes"):
			selected["changes"] = true
		}
	}
	if len(selected) == 0 {
		return data
	}

	filtered := map[string]interface{}{}
	for k, v := range data {
		if selected[k] {
			filtered[k] = v
		}
	}
	return filtered
}

func emitDisclosureFacts(ctx context.Context, engine *mangle.Engine, sessionID string, handles []string, reason string) {
	if engine == nil || sessionID == "" || len(handles) == 0 {
		return
	}
	now := time.Now()
	facts := make([]mangle.Fact, 0, len(handles))
	for _, h := range handles {
		facts = append(facts, mangle.Fact{
			Predicate: "disclosure_handle",
			Args:      []interface{}{sessionID, h, reason, now.UnixMilli()},
			Timestamp: now,
		})
	}
	_ = engine.AddFacts(ctx, facts)
}

func hasRecentGateFact(engine *mangle.Engine, predicate, sessionID, matchValue string, ttl time.Duration) bool {
	if engine == nil {
		return false
	}
	facts := engine.FactsByPredicate(predicate)
	cutoff := time.Now().Add(-ttl)
	for i := len(facts) - 1; i >= 0; i-- {
		f := facts[i]
		if f.Timestamp.Before(cutoff) {
			continue
		}
		if len(f.Args) < 2 {
			continue
		}
		if fmt.Sprintf("%v", f.Args[0]) != sessionID {
			continue
		}
		if fmt.Sprintf("%v", f.Args[1]) == matchValue {
			return true
		}
	}
	return false
}

type observeIntentDefaults struct {
	mode               string
	view               string
	maxItems           int
	filter             string
	visibleOnly        bool
	internalOnly       bool
	includeActionPlan  bool
	includeDiagnostics bool
	maxRecommendations int
}

func normalizeObserveIntent(intent string) string {
	return strings.ToLower(strings.TrimSpace(intent))
}

func resolveObserveIntentDefaults(intent string) (observeIntentDefaults, bool) {
	switch intent {
	case "quick_status":
		return observeIntentDefaults{
			mode:               "state",
			view:               "summary",
			maxItems:           5,
			filter:             "all",
			visibleOnly:        true,
			internalOnly:       false,
			includeActionPlan:  false,
			includeDiagnostics: true,
			maxRecommendations: 2,
		}, true
	case "find_actions":
		return observeIntentDefaults{
			mode:               "interactive",
			view:               "compact",
			maxItems:           12,
			filter:             "all",
			visibleOnly:        true,
			internalOnly:       false,
			includeActionPlan:  true,
			includeDiagnostics: false,
			maxRecommendations: defaultObserveMaxRecs,
		}, true
	case "map_navigation":
		return observeIntentDefaults{
			mode:               "nav",
			view:               "compact",
			maxItems:           20,
			filter:             "all",
			visibleOnly:        true,
			internalOnly:       true,
			includeActionPlan:  false,
			includeDiagnostics: false,
			maxRecommendations: defaultObserveMaxRecs,
		}, true
	case "hidden_content":
		return observeIntentDefaults{
			mode:               "hidden",
			view:               "compact",
			maxItems:           20,
			filter:             "all",
			visibleOnly:        true,
			internalOnly:       false,
			includeActionPlan:  false,
			includeDiagnostics: false,
			maxRecommendations: defaultObserveMaxRecs,
		}, true
	case "deep_audit":
		return observeIntentDefaults{
			mode:               "composite",
			view:               "full",
			maxItems:           50,
			filter:             "all",
			visibleOnly:        true,
			internalOnly:       false,
			includeActionPlan:  true,
			includeDiagnostics: true,
			maxRecommendations: defaultReasonMaxRecs,
		}, true
	case "check_sessions":
		return observeIntentDefaults{
			mode:               "sessions",
			view:               "compact",
			maxItems:           20,
			filter:             "all",
			visibleOnly:        true,
			internalOnly:       false,
			includeActionPlan:  false,
			includeDiagnostics: false,
			maxRecommendations: 0,
		}, true
	case "visual_check":
		return observeIntentDefaults{
			mode:               "screenshot",
			view:               "compact",
			maxItems:           1,
			filter:             "all",
			visibleOnly:        true,
			internalOnly:       false,
			includeActionPlan:  false,
			includeDiagnostics: false,
			maxRecommendations: 0,
		}, true
	default:
		return observeIntentDefaults{}, false
	}
}

type reasonIntentDefaults struct {
	topic string
	view  string
}

func normalizeReasonIntent(intent string) string {
	return strings.ToLower(strings.TrimSpace(intent))
}

func resolveReasonIntentDefaults(intent string) (reasonIntentDefaults, bool) {
	switch intent {
	case "triage":
		return reasonIntentDefaults{topic: "health", view: "compact"}, true
	case "act_now":
		return reasonIntentDefaults{topic: "next_best_action", view: "compact"}, true
	case "debug_failure":
		return reasonIntentDefaults{topic: "why_failed", view: "full"}, true
	case "unblock":
		return reasonIntentDefaults{topic: "blocking_issue", view: "compact"}, true
	default:
		return reasonIntentDefaults{}, false
	}
}

func argPresent(args map[string]interface{}, key string) bool {
	_, ok := args[key]
	return ok
}

func argHasNonEmptyString(args map[string]interface{}, key string) bool {
	raw, ok := args[key]
	if !ok {
		return false
	}
	value, ok := raw.(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(value) != ""
}

func argHasInt(args map[string]interface{}, key string) bool {
	raw, ok := args[key]
	if !ok {
		return false
	}
	switch raw.(type) {
	case int, int8, int16, int32, int64, float32, float64:
		return true
	default:
		return false
	}
}

func suggestObserveNextStep(sessionID string, data map[string]interface{}, mode, view string, recommendations []map[string]interface{}) map[string]interface{} {
	mode = strings.ToLower(strings.TrimSpace(mode))
	view = strings.ToLower(strings.TrimSpace(view))
	if state, ok := data["state"].(map[string]interface{}); ok {
		if loading, exists := state["loading"].(bool); exists && loading {
			return map[string]interface{}{
				"tool": "browser-act",
				"args": map[string]interface{}{
					"session_id": sessionID,
					"operations": []map[string]interface{}{
						{"type": "await_stable", "timeout_ms": 10000},
					},
				},
			}
		}
	}

	if diag, ok := data["diagnostics"].(map[string]interface{}); ok {
		status := strings.TrimSpace(getStringFromMap(diag, "status"))
		if status == "error" {
			return map[string]interface{}{
				"tool": "browser-reason",
				"args": map[string]interface{}{
					"session_id": sessionID,
					"topic":      "why_failed",
					"view":       "compact",
				},
			}
		}
		if status == "warning" {
			return map[string]interface{}{
				"tool": "browser-reason",
				"args": map[string]interface{}{
					"session_id": sessionID,
					"topic":      "health",
					"view":       "compact",
				},
			}
		}
	}

	if len(recommendations) > 0 {
		first := recommendations[0]
		next := map[string]interface{}{}
		toolName := strings.TrimSpace(getStringFromMap(first, "tool"))
		if toolName != "" {
			next["tool"] = toolName
		}
		if args, ok := first["args"].(map[string]interface{}); ok {
			if toolRequiresSessionID(toolName) && sessionID != "" {
				args["session_id"] = sessionID
			}
			next["args"] = args
		}
		if reason, ok := first["reason"].(string); ok {
			next["reason"] = reason
		}
		if len(next) > 0 {
			return next
		}
	}

	if interactive, ok := data["interactive"].(map[string]interface{}); ok {
		if summary, ok := interactive["summary"].(map[string]interface{}); ok {
			if total := asInt(summary["total"]); total > 0 {
				return map[string]interface{}{
					"tool": "browser-reason",
					"args": map[string]interface{}{
						"session_id": sessionID,
						"topic":      "next_best_action",
						"view":       "compact",
					},
				}
			}
			// If we have *no* visible interactive elements, expand scope before falling back to JS.
			if asInt(summary["total"]) == 0 {
				return map[string]interface{}{
					"tool": "browser-observe",
					"args": map[string]interface{}{
						"session_id":    sessionID,
						"mode":          "hidden",
						"view":          "compact",
						"max_items":     20,
						"emit_facts":    true,
						"internal_only": false,
					},
				}
			}
		}
	}
	if interSummary, ok := data["interactive_summary"].(map[string]interface{}); ok {
		if total := asInt(interSummary["total"]); total > 0 {
			return map[string]interface{}{
				"tool": "browser-reason",
				"args": map[string]interface{}{
					"session_id": sessionID,
					"topic":      "next_best_action",
					"view":       "compact",
				},
			}
		}
		if asInt(interSummary["total"]) == 0 {
			return map[string]interface{}{
				"tool": "browser-observe",
				"args": map[string]interface{}{
					"session_id":    sessionID,
					"mode":          "hidden",
					"view":          "compact",
					"max_items":     20,
					"emit_facts":    true,
					"internal_only": false,
				},
			}
		}
	}

	if navCounts, ok := data["nav_counts"].(map[string]interface{}); ok {
		if total := asInt(navCounts["total"]); total > 0 {
			return map[string]interface{}{
				"tool": "browser-observe",
				"args": map[string]interface{}{
					"session_id": sessionID,
					"mode":       "interactive",
					"view":       "compact",
				},
			}
		}
	}
	if nav, ok := data["nav"].(map[string]interface{}); ok {
		if counts, ok := nav["counts"].(map[string]interface{}); ok {
			if total := asInt(counts["total"]); total > 0 {
				return map[string]interface{}{
					"tool": "browser-observe",
					"args": map[string]interface{}{
						"session_id": sessionID,
						"mode":       "interactive",
						"view":       "compact",
					},
				}
			}
		}
	}

	// If the caller didn't request composite, we likely just need more context.
	if mode != "" && mode != "composite" {
		return map[string]interface{}{
			"tool": "browser-observe",
			"args": map[string]interface{}{
				"session_id": sessionID,
				"mode":       "composite",
				"view":       "compact",
			},
		}
	}

	// If composite data still looks empty, a screenshot is often the cheapest way to understand what's happening.
	navTotal := -1
	if navCounts, ok := data["nav_counts"].(map[string]interface{}); ok {
		navTotal = asInt(navCounts["total"])
	} else if nav, ok := data["nav"].(map[string]interface{}); ok {
		if counts, ok := nav["counts"].(map[string]interface{}); ok {
			navTotal = asInt(counts["total"])
		}
	}
	interTotal := -1
	if interSummary, ok := data["interactive_summary"].(map[string]interface{}); ok {
		interTotal = asInt(interSummary["total"])
	} else if inter, ok := data["interactive"].(map[string]interface{}); ok {
		if summary, ok := inter["summary"].(map[string]interface{}); ok {
			interTotal = asInt(summary["total"])
		}
	}
	if navTotal == 0 && interTotal == 0 {
		return map[string]interface{}{
			"tool": "browser-observe",
			"args": map[string]interface{}{
				"session_id": sessionID,
				"mode":       "screenshot",
			},
		}
	}

	return map[string]interface{}{
		"tool": "browser-reason",
		"args": map[string]interface{}{
			"session_id": sessionID,
			"topic":      "next_best_action",
			"view":       "compact",
		},
	}
}

func toolRequiresSessionID(tool string) bool {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "attach-session",
		"browser-act",
		"browser-history",
		"browser-mangle",
		"browser-observe",
		"browser-reason",
		"create-session",
		"discover-hidden-content",
		"evaluate-js",
		"fill-form",
		"fork-session",
		"get-interactive-elements",
		"get-navigation-links",
		"get-page-state",
		"interact",
		"launch-browser",
		"list-sessions",
		"navigate-url",
		"press-key",
		"reify-react",
		"screenshot",
		"snapshot-dom":
		return true
	default:
		return false
	}
}

func resolveCurrentURL(ctx context.Context, engine *mangle.Engine, sessionID string) string {
	if engine == nil || sessionID == "" {
		return ""
	}
	rows := filterRowsByField(queryToRows(ctx, engine, "current_url(SessionId, Url)."), "SessionId", sessionID)
	if len(rows) == 0 {
		return ""
	}
	// Prefer the newest binding if there are multiple.
	return fmt.Sprintf("%v", rows[len(rows)-1]["Url"])
}

func originFromURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func filterRowsByField(rows []map[string]interface{}, field, expected string) []map[string]interface{} {
	if expected == "" {
		return rows
	}
	filtered := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		if fmt.Sprintf("%v", row[field]) == expected {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func filterRowsSince(rows []map[string]interface{}, timestampFields []string, sinceMs int64) []map[string]interface{} {
	if sinceMs <= 0 || len(rows) == 0 {
		return rows
	}
	filtered := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		ts, hasTimestamp := rowTimestampMs(row, timestampFields)
		if !hasTimestamp || ts >= sinceMs {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func rowTimestampMs(row map[string]interface{}, timestampFields []string) (int64, bool) {
	for _, field := range timestampFields {
		value, exists := row[field]
		if !exists {
			continue
		}
		ts := asInt64(value)
		if ts > 0 {
			return ts, true
		}
	}
	return 0, false
}

func buildReasonChangeFeed(
	rootCauses []map[string]interface{},
	failedReqs []map[string]interface{},
	slowApis []map[string]interface{},
	userVisibleErrors []map[string]interface{},
	blockingIssues []map[string]interface{},
	maxItems int,
) []map[string]interface{} {
	changes := make([]map[string]interface{}, 0, len(failedReqs)+len(slowApis)+len(userVisibleErrors)+len(blockingIssues)+len(rootCauses))

	for _, row := range failedReqs {
		changes = append(changes, map[string]interface{}{
			"type":      "failed_request",
			"key":       fmt.Sprintf("%v", row["ReqId"]),
			"detail":    fmt.Sprintf("%v (%v)", row["Url"], row["Status"]),
			"timestamp": asInt64(row["ReqTs"]),
		})
	}
	for _, row := range slowApis {
		changes = append(changes, map[string]interface{}{
			"type":      "slow_api",
			"key":       fmt.Sprintf("%v", row["ReqId"]),
			"detail":    fmt.Sprintf("%v (%vms)", row["Url"], row["Duration"]),
			"timestamp": asInt64(row["ReqTs"]),
		})
	}
	for _, row := range userVisibleErrors {
		changes = append(changes, map[string]interface{}{
			"type":      "user_visible_error",
			"key":       fmt.Sprintf("%v", row["Source"]),
			"detail":    fmt.Sprintf("%v", row["Message"]),
			"timestamp": asInt64(row["Timestamp"]),
		})
	}
	for _, row := range blockingIssues {
		changes = append(changes, map[string]interface{}{
			"type":      "blocking_issue",
			"key":       fmt.Sprintf("%v", row["SessionId"]),
			"detail":    fmt.Sprintf("%v", row["Reason"]),
			"timestamp": 0,
		})
	}
	for _, row := range rootCauses {
		changes = append(changes, map[string]interface{}{
			"type":      "root_cause",
			"key":       fmt.Sprintf("%v", row["Source"]),
			"detail":    fmt.Sprintf("%v", row["Cause"]),
			"timestamp": asInt64(row["Ts"]),
		})
	}

	sort.SliceStable(changes, func(i, j int) bool {
		return asInt64(changes[i]["timestamp"]) > asInt64(changes[j]["timestamp"])
	})

	return limitMapSlice(changes, maxItems)
}

func queryActionCandidates(ctx context.Context, engine *mangle.Engine, sessionID string, maxItems int) []map[string]interface{} {
	if engine == nil || strings.TrimSpace(sessionID) == "" {
		return []map[string]interface{}{}
	}

	type candidate struct {
		Action   string
		Ref      string
		Label    string
		Priority int
		Reason   string
		Source   string
	}

	best := make(map[string]candidate)

	dedupKey := func(isGlobal bool, action, ref, label string) string {
		a := strings.ToLower(strings.TrimSpace(action))
		r := strings.ToLower(strings.TrimSpace(ref))
		l := strings.ToLower(strings.TrimSpace(label))

		if isGlobal {
			if a == "" {
				return "global|unknown"
			}
			return "global|" + a
		}

		switch a {
		case "navigate":
			// For navigate, label holds the target href; multiple refs can point to the same href.
			if l != "" {
				return a + "|" + l
			}
			if r != "" {
				return a + "|" + r
			}
			return a + "|unknown"
		default:
			// For clicks/typing/toggling, ref is the stable key.
			if r != "" {
				return a + "|" + r
			}
			if l != "" {
				return a + "|" + l
			}
			if a != "" {
				return a + "|" + a
			}
			return "unknown|unknown"
		}
	}

	upsert := func(isGlobal bool, action, ref, label string, priority int, reason, source string) {
		key := dedupKey(isGlobal, action, ref, label)
		c := candidate{
			Action:   action,
			Ref:      ref,
			Label:    label,
			Priority: priority,
			Reason:   reason,
			Source:   source,
		}
		if prev, ok := best[key]; ok {
			// Keep the highest-priority candidate for the same semantic action.
			if c.Priority <= prev.Priority {
				return
			}
		}
		best[key] = c
	}

	rows := queryToRows(ctx, engine, fmt.Sprintf("action_candidate(%q, Ref, Label, Action, Priority, Reason).", sessionID))
	for _, row := range rows {
		upsert(false,
			fmt.Sprintf("%v", row["Action"]),
			fmt.Sprintf("%v", row["Ref"]),
			fmt.Sprintf("%v", row["Label"]),
			asInt(row["Priority"]),
			fmt.Sprintf("%v", row["Reason"]),
			"mangle",
		)
	}

	globalRows := queryToRows(ctx, engine, fmt.Sprintf("global_action(%q, Action, Priority, Reason).", sessionID))
	for _, row := range globalRows {
		upsert(true,
			fmt.Sprintf("%v", row["Action"]),
			"",
			"",
			asInt(row["Priority"]),
			fmt.Sprintf("%v", row["Reason"]),
			"mangle",
		)
	}

	candidates := make([]candidate, 0, len(best))
	for _, c := range best {
		candidates = append(candidates, c)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		ai := strings.ToLower(strings.TrimSpace(candidates[i].Action))
		aj := strings.ToLower(strings.TrimSpace(candidates[j].Action))
		if ai != aj {
			return ai < aj
		}
		li := strings.ToLower(strings.TrimSpace(candidates[i].Label))
		lj := strings.ToLower(strings.TrimSpace(candidates[j].Label))
		if li != lj {
			return li < lj
		}
		ri := strings.ToLower(strings.TrimSpace(candidates[i].Ref))
		rj := strings.ToLower(strings.TrimSpace(candidates[j].Ref))
		if ri != rj {
			return ri < rj
		}
		reasonI := strings.ToLower(strings.TrimSpace(candidates[i].Reason))
		reasonJ := strings.ToLower(strings.TrimSpace(candidates[j].Reason))
		return reasonI < reasonJ
	})

	out := make([]map[string]interface{}, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, map[string]interface{}{
			"action":   c.Action,
			"ref":      c.Ref,
			"label":    c.Label,
			"priority": c.Priority,
			"reason":   c.Reason,
			"source":   c.Source,
		})
	}

	return limitMapSlice(out, maxItems)
}

func buildActionPlanRecommendations(candidates []map[string]interface{}, max int, sessionID, baseOrigin string) []map[string]interface{} {
	if len(candidates) == 0 {
		return nil
	}
	recs := make([]map[string]interface{}, 0, len(candidates))

	for _, candidate := range candidates {
		action := strings.ToLower(fmt.Sprintf("%v", candidate["action"]))
		ref := fmt.Sprintf("%v", candidate["ref"])
		label := fmt.Sprintf("%v", candidate["label"])
		reason := fmt.Sprintf("%v", candidate["reason"])
		priority := asInt(candidate["priority"])

		var ops []map[string]interface{}
		requiresUserInput := false
		switch action {
		case "navigate":
			target := strings.TrimSpace(label)
			if target == "" {
				continue
			}
			if strings.HasPrefix(target, "/") && baseOrigin != "" {
				target = strings.TrimRight(baseOrigin, "/") + target
			}
			ops = []map[string]interface{}{
				{"type": "navigate", "url": target, "wait_until": "networkidle"},
			}
		case "click":
			if strings.TrimSpace(ref) == "" {
				continue
			}
			ops = []map[string]interface{}{
				{"type": "interact", "action": "click", "ref": ref},
			}
		case "press_escape":
			ops = []map[string]interface{}{
				{"type": "key", "key": "Escape"},
			}
		case "type":
			if strings.TrimSpace(ref) == "" {
				continue
			}
			suggested := suggestInputValue(label)
			if strings.HasPrefix(suggested, "<") {
				requiresUserInput = true
			}
			ops = []map[string]interface{}{
				{"type": "interact", "action": "type", "ref": ref, "value": suggested},
			}
		case "select":
			if strings.TrimSpace(ref) == "" {
				continue
			}
			requiresUserInput = true
			ops = []map[string]interface{}{
				{"type": "interact", "action": "select", "ref": ref, "value": "<option>"},
			}
		case "toggle":
			if strings.TrimSpace(ref) == "" {
				continue
			}
			ops = []map[string]interface{}{
				{"type": "interact", "action": "toggle", "ref": ref},
			}
		default:
			continue
		}

		recs = append(recs, map[string]interface{}{
			"tool": "browser-act",
			"reason": fmt.Sprintf(
				"Candidate action from Mangle (priority=%d, reason=%s, label=%s).",
				priority,
				reason,
				label,
			),
			"args": map[string]interface{}{
				"session_id":    sessionID,
				"operations":    ops,
				"stop_on_error": true,
				"view":          "compact",
			},
			"candidate":           candidate,
			"requires_user_input": requiresUserInput,
		})
	}

	return limitMapSlice(recs, max)
}

func asInt(v interface{}) int {
	switch value := v.(type) {
	case int:
		return value
	case int8:
		return int(value)
	case int16:
		return int(value)
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float32:
		return int(value)
	case float64:
		return int(value)
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0
		}
		if i, err := strconv.Atoi(trimmed); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return int(f)
		}
		return 0
	default:
		return 0
	}
}

func asInt64(v interface{}) int64 {
	switch value := v.(type) {
	case int:
		return int64(value)
	case int8:
		return int64(value)
	case int16:
		return int64(value)
	case int32:
		return int64(value)
	case int64:
		return value
	case float32:
		return int64(value)
	case float64:
		return int64(value)
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0
		}
		if i, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(trimmed, 64); err == nil {
			return int64(f)
		}
		return 0
	default:
		return 0
	}
}

func suggestInputValue(label string) string {
	lower := strings.ToLower(label)
	switch {
	case strings.Contains(lower, "email"):
		return "user@example.com"
	case strings.Contains(lower, "password"):
		return "<password>"
	case strings.Contains(lower, "phone"):
		return "<phone>"
	case strings.Contains(lower, "name"):
		return "<name>"
	default:
		return "<value>"
	}
}

func ternaryStatus(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func filterInteractiveData(data map[string]interface{}, filter string) map[string]interface{} {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" || filter == "all" {
		return data
	}

	allowed := map[string]bool{}
	switch filter {
	case "buttons":
		allowed["button"] = true
	case "inputs":
		allowed["input"] = true
		allowed["checkbox"] = true
		allowed["radio"] = true
	case "links":
		allowed["link"] = true
	case "selects":
		allowed["select"] = true
	default:
		return data
	}

	elems, ok := data["elements"].([]interface{})
	if !ok || len(elems) == 0 {
		return data
	}

	filtered := make([]interface{}, 0, len(elems))
	typeCount := map[string]int{}
	disabledCount := 0

	for _, e := range elems {
		elem, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(getStringFromMap(elem, "type")))
		if typ == "" || !allowed[typ] {
			continue
		}

		filtered = append(filtered, elem)
		typeCount[typ]++
		if disabled, ok := elem["disabled"].(bool); ok && disabled {
			disabledCount++
		}
	}

	summary := map[string]interface{}{
		"total": len(filtered),
		"types": map[string]interface{}{},
	}
	typesOut := summary["types"].(map[string]interface{})
	for k, v := range typeCount {
		typesOut[k] = v
	}
	if disabledCount > 0 {
		summary["disabled"] = disabledCount
	}

	out := map[string]interface{}{}
	for k, v := range data {
		if k == "summary" || k == "elements" {
			continue
		}
		out[k] = v
	}
	out["summary"] = summary
	out["elements"] = filtered
	return out
}

func buildRefSet(interactiveData map[string]interface{}) map[string]bool {
	out := map[string]bool{}
	if interactiveData == nil {
		return out
	}
	elems, ok := interactiveData["elements"].([]interface{})
	if !ok || len(elems) == 0 {
		return out
	}
	for _, e := range elems {
		elem, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		ref := strings.TrimSpace(getStringFromMap(elem, "ref"))
		if ref == "" {
			continue
		}
		out[ref] = true
	}
	return out
}

func buildHrefSet(navData map[string]interface{}) map[string]bool {
	out := map[string]bool{}
	if navData == nil {
		return out
	}
	for _, area := range []string{"nav", "side", "main", "foot"} {
		m, ok := navData[area].(map[string]interface{})
		if !ok || len(m) == 0 {
			continue
		}
		for _, v := range m {
			href := strings.TrimSpace(fmt.Sprintf("%v", v))
			if href == "" {
				continue
			}
			out[href] = true
		}
	}
	return out
}

func filterActionCandidates(candidates []map[string]interface{}, allowedRefs, allowedHrefs map[string]bool) []map[string]interface{} {
	if len(candidates) == 0 {
		return candidates
	}
	if allowedRefs == nil {
		allowedRefs = map[string]bool{}
	}
	if allowedHrefs == nil {
		allowedHrefs = map[string]bool{}
	}

	out := make([]map[string]interface{}, 0, len(candidates))
	for _, c := range candidates {
		action := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", c["action"])))
		ref := strings.TrimSpace(fmt.Sprintf("%v", c["ref"]))
		label := strings.TrimSpace(fmt.Sprintf("%v", c["label"]))

		// Global actions: keep.
		if ref == "" && label == "" {
			out = append(out, c)
			continue
		}

		switch action {
		case "navigate":
			// For navigate, label typically holds the target href.
			if label != "" && allowedHrefs[label] {
				out = append(out, c)
				continue
			}
			// Sometimes navigate candidates are produced via element ref.
			if ref != "" && allowedRefs[ref] {
				out = append(out, c)
				continue
			}
		default:
			if ref != "" && allowedRefs[ref] {
				out = append(out, c)
				continue
			}
		}
	}
	return out
}

func limitAnySlice(items []interface{}, max int) []interface{} {
	if max <= 0 || len(items) <= max {
		return items
	}
	return items[:max]
}

func limitMapSlice(items []map[string]interface{}, max int) []map[string]interface{} {
	if max <= 0 || len(items) <= max {
		return items
	}
	return items[:max]
}

// BrowserMangleTool consolidates all Mangle fact operations into one progressive-disclosure tool.
type BrowserMangleTool struct {
	engine *mangle.Engine
}

func (t *BrowserMangleTool) Name() string { return "browser-mangle" }
func (t *BrowserMangleTool) Description() string {
	return `Query and manipulate the Mangle fact engine directly -- facts, rules, queries, temporal reasoning.

USE THIS TOOL for direct Mangle access. Prefer browser-reason for high-level diagnostics.
Mangle is a logic programming engine that stores facts about the browser (network events,
console logs, DOM state, navigation) and derives new facts through rules.

OPERATIONS:

  Reading facts:
    query:     Execute a Mangle query string. Returns matching facts.
               {operation:"query", query:"current_url(S, U)."}
               {operation:"query", query:"slow_api(ReqId, Url, Duration)."}
               {operation:"query", query:"caused_by(ConsoleErr, ReqId)."}

    temporal:  Query facts within a time window (epoch milliseconds).
               {operation:"temporal", predicate:"net_request", after_ms:1706000000000}

    evaluate:  Evaluate a derived predicate (rules that combine base facts).
               {operation:"evaluate", predicate:"cascading_failure"}

    read:      Read recent buffered facts (newest first).
               {operation:"read", limit:50}
               {operation:"read", predicate_filter:"net_response"}

  Writing facts/rules:
    push:         Add new facts to the engine.
                  {operation:"push", facts:["test_fact(1,2,3).","status(ok)."]}

    submit_rule:  Add a Mangle rule (derives new facts from existing ones).
                  {operation:"submit_rule", rule:"my_rule(X) :- net_response(X,S,_,_), S >= 500."}

  Waiting:
    subscribe:        Watch for a predicate match with timeout.
                      {operation:"subscribe", predicate:"login_succeeded", timeout_ms:15000}

    await_fact:       Wait for a specific fact to appear.
                      {operation:"await_fact", predicate:"current_url", args:["sess-1","/dashboard"], timeout_ms:10000}

    await_conditions: Wait for multiple conditions simultaneously.
                      {operation:"await_conditions", conditions:[{predicate:"p1",args:["a"]}], timeout_ms:10000}

VIEWS:
  summary: Counts and status only (~100 tokens)
  compact: Truncated results, default (~500 tokens)
  full:    Complete results, all rows

COMMON PREDICATES (built-in from CDP events):
  current_url(SessionId, Url)                     Page URL per session
  net_request(Id, Method, Url, Initiator, Time)   HTTP requests
  net_response(Id, Status, Latency, Duration)     HTTP responses
  console_event(Level, Message, Time)             Console logs
  navigation_event(Session, Url, Time)            Page navigations
  slow_api(ReqId, Url, Duration)                  Requests > 1s (derived)
  caused_by(ConsoleErr, ReqId)                    Error causality (derived)
  login_succeeded(SessionId)                      Login detection (derived)

WHEN TO USE vs OTHER TOOLS:
  "I need raw fact data"                   -> browser-mangle
  "Is the page healthy? What went wrong?"  -> browser-reason (higher-level)
  "I want to wait for a fact during a flow"-> browser-act (await_fact/await_stable ops)`
}

func (t *BrowserMangleTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"description": "Mangle operation to perform",
				"enum":        []string{"query", "temporal", "evaluate", "read", "submit_rule", "subscribe", "push", "await_fact", "await_conditions"},
			},
			"view": map[string]interface{}{
				"type":        "string",
				"description": "Disclosure depth: summary|compact|full",
				"enum":        []string{"summary", "compact", "full"},
			},
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Mangle query string (for query operation)",
			},
			"predicate": map[string]interface{}{
				"type":        "string",
				"description": "Predicate name (for temporal/evaluate/subscribe/await_fact)",
			},
			"predicate_filter": map[string]interface{}{
				"type":        "string",
				"description": "Filter by predicate (for read operation)",
			},
			"rule": map[string]interface{}{
				"type":        "string",
				"description": "Mangle rule source (for submit_rule)",
			},
			"facts": map[string]interface{}{
				"type":        "array",
				"description": "Facts to push (for push operation)",
				"items":       map[string]interface{}{"type": "object"},
			},
			"args": map[string]interface{}{
				"type":        "array",
				"description": "Predicate arguments (for await_fact)",
				"items":       map[string]interface{}{"type": "string"},
			},
			"conditions": map[string]interface{}{
				"type":        "array",
				"description": "Conditions to await (for await_conditions)",
				"items":       map[string]interface{}{"type": "object"},
			},
			"after_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Start of time window in epoch ms (for temporal)",
			},
			"before_ms": map[string]interface{}{
				"type":        "integer",
				"description": "End of time window in epoch ms (for temporal)",
			},
			"timeout_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in milliseconds (for subscribe/await_fact/await_conditions)",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Max facts to return (for read)",
			},
			"max_items": map[string]interface{}{
				"type":        "integer",
				"description": "Max items in response (default 20)",
			},
		},
		"required": []string{"operation"},
	}
}

func (t *BrowserMangleTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if t.engine == nil {
		return map[string]interface{}{"success": false, "error": "mangle engine is not available"}, nil
	}

	operation := strings.ToLower(getStringArg(args, "operation"))
	if operation == "" {
		return map[string]interface{}{"success": false, "error": "operation is required"}, nil
	}

	view := normalizeProgressiveView(getStringArg(args, "view"))
	maxItems := getIntArg(args, "max_items", defaultProgressiveMaxItems)
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}

	var (
		opResult interface{}
		err      error
	)

	switch operation {
	case "query":
		delegate := &QueryFactsTool{engine: t.engine}
		opResult, err = delegate.Execute(ctx, map[string]interface{}{
			"query": getStringArg(args, "query"),
		})

	case "temporal":
		delegate := &QueryTemporalTool{engine: t.engine}
		delegateArgs := map[string]interface{}{
			"predicate": getStringArg(args, "predicate"),
		}
		if v, ok := args["after_ms"]; ok {
			delegateArgs["after_ms"] = v
		}
		if v, ok := args["before_ms"]; ok {
			delegateArgs["before_ms"] = v
		}
		opResult, err = delegate.Execute(ctx, delegateArgs)

	case "evaluate":
		delegate := &EvaluateRuleTool{engine: t.engine}
		opResult, err = delegate.Execute(ctx, map[string]interface{}{
			"predicate": getStringArg(args, "predicate"),
		})

	case "read":
		delegate := &ReadFactsTool{engine: t.engine}
		delegateArgs := map[string]interface{}{}
		if v, ok := args["limit"]; ok {
			delegateArgs["limit"] = v
		}
		if v, ok := args["predicate_filter"]; ok {
			delegateArgs["predicate_filter"] = v
		}
		opResult, err = delegate.Execute(ctx, delegateArgs)

	case "submit_rule":
		delegate := &SubmitRuleTool{engine: t.engine}
		opResult, err = delegate.Execute(ctx, map[string]interface{}{
			"rule": getStringArg(args, "rule"),
		})

	case "subscribe":
		delegate := &SubscribeRuleTool{engine: t.engine}
		delegateArgs := map[string]interface{}{
			"predicate": getStringArg(args, "predicate"),
		}
		if v, ok := args["timeout_ms"]; ok {
			delegateArgs["timeout_ms"] = v
		}
		opResult, err = delegate.Execute(ctx, delegateArgs)

	case "push":
		delegate := &PushFactsTool{engine: t.engine}
		opResult, err = delegate.Execute(ctx, map[string]interface{}{
			"facts": args["facts"],
		})

	case "await_fact":
		delegate := &AwaitFactTool{engine: t.engine}
		delegateArgs := map[string]interface{}{
			"predicate": getStringArg(args, "predicate"),
		}
		if v, ok := args["args"]; ok {
			delegateArgs["args"] = v
		}
		if v, ok := args["timeout_ms"]; ok {
			delegateArgs["timeout_ms"] = v
		}
		opResult, err = delegate.Execute(ctx, delegateArgs)

	case "await_conditions":
		delegate := &AwaitConditionsTool{engine: t.engine}
		delegateArgs := map[string]interface{}{}
		if v, ok := args["conditions"]; ok {
			delegateArgs["conditions"] = v
		}
		if v, ok := args["timeout_ms"]; ok {
			delegateArgs["timeout_ms"] = v
		}
		opResult, err = delegate.Execute(ctx, delegateArgs)

	default:
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("unknown mangle operation: %s", operation),
		}, nil
	}

	if err != nil {
		return map[string]interface{}{
			"success":   false,
			"operation": operation,
			"error":     err.Error(),
		}, nil
	}

	resultMap := asMap(opResult)
	handle := fmt.Sprintf("mangle:%s:%d", operation, time.Now().UnixMilli())
	handles := []string{handle}
	emitDisclosureFacts(ctx, t.engine, "", handles, "mangle")

	response := map[string]interface{}{
		"success":          true,
		"operation":        operation,
		"view":             view,
		"evidence_handles": handles,
	}

	switch view {
	case "summary":
		response["summary"] = buildMangleSummary(operation, resultMap)
	case "compact":
		response["data"] = truncateMangleData(resultMap, maxItems)
		response["summary"] = buildMangleSummary(operation, resultMap)
	default: // full
		response["data"] = resultMap
	}

	return response, nil
}

func buildMangleSummary(operation string, data map[string]interface{}) string {
	switch operation {
	case "query":
		if results, ok := data["results"].([]map[string]interface{}); ok {
			return fmt.Sprintf("query returned %d result(s)", len(results))
		}
		if results, ok := data["results"].([]interface{}); ok {
			return fmt.Sprintf("query returned %d result(s)", len(results))
		}
		return "query completed"
	case "read":
		if facts, ok := data["facts"].([]interface{}); ok {
			return fmt.Sprintf("read %d fact(s)", len(facts))
		}
		if count := asInt(data["count"]); count > 0 {
			return fmt.Sprintf("read %d fact(s)", count)
		}
		return "read completed"
	case "push":
		accepted := asInt(data["accepted"])
		return fmt.Sprintf("pushed %d fact(s)", accepted)
	case "submit_rule":
		if success, ok := data["success"].(bool); ok && success {
			return "rule submitted"
		}
		return "rule submission failed"
	case "evaluate":
		if results, ok := data["results"].([]interface{}); ok {
			return fmt.Sprintf("evaluated %d result(s)", len(results))
		}
		return "evaluation completed"
	case "temporal":
		if results, ok := data["results"].([]interface{}); ok {
			return fmt.Sprintf("temporal query returned %d result(s)", len(results))
		}
		return "temporal query completed"
	case "subscribe":
		if matched, ok := data["matched"].(bool); ok && matched {
			return "subscription matched"
		}
		return "subscription completed"
	case "await_fact":
		if matched, ok := data["matched"].(bool); ok && matched {
			return "fact matched"
		}
		return "await completed"
	case "await_conditions":
		if matched, ok := data["all_matched"].(bool); ok && matched {
			return "all conditions matched"
		}
		return "await conditions completed"
	default:
		return operation + " completed"
	}
}

func truncateMangleData(data map[string]interface{}, maxItems int) map[string]interface{} {
	out := make(map[string]interface{}, len(data))
	for k, v := range data {
		switch items := v.(type) {
		case []interface{}:
			out[k] = limitAnySlice(items, maxItems)
			if len(items) > maxItems {
				out[k+"_truncated"] = true
			}
		case []map[string]interface{}:
			out[k] = limitMapSlice(items, maxItems)
			if len(items) > maxItems {
				out[k+"_truncated"] = true
			}
		default:
			out[k] = v
		}
	}
	return out
}
