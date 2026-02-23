package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/recorder"
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
	return `Progressive page observation with token-aware modes.
Returns state/navigation/interactive data plus refs for browser-act.
Use mode for explicit slices (state, nav, interactive, hidden, grids, composite, sessions, screenshot, react, dom_snapshot).`
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
				"enum":        []string{"quick_status", "find_actions", "map_navigation", "hidden_content", "deep_audit", "check_sessions", "visual_check", "grid_hunt"},
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"description": "Observation mode",
				"enum":        []string{"state", "nav", "interactive", "hidden", "grids", "composite", "sessions", "screenshot", "react", "dom_snapshot"},
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
			"max_grids": map[string]interface{}{
				"type":        "integer",
				"description": "Grids mode: max grids (default max_items)",
			},
			"sample_rows": map[string]interface{}{
				"type":        "integer",
				"description": "Grids mode: sample rows (default 3, max 10)",
			},
			"include_samples": map[string]interface{}{
				"type":        "boolean",
				"description": "Grids mode: include sample refs (default true; summary defaults false)",
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
			sessions := toAnySlice(resMap["sessions"])
			response["summary"] = fmt.Sprintf("%d active session(s)", len(sessions))
			response["data"] = map[string]interface{}{"session_count": len(sessions)}
		case "compact":
			sessions := toAnySlice(resMap["sessions"])
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

	case "grids":
		if sessionID == "" {
			return map[string]interface{}{"success": false, "error": "session_id is required for grids mode"}, nil
		}
		delegate := &DiscoverGridsTool{sessions: t.sessions}
		delegateArgs := map[string]interface{}{
			"session_id": sessionID,
			"max_grids":  maxItems,
		}
		if argHasInt(args, "max_grids") {
			delegateArgs["max_grids"] = getIntArg(args, "max_grids", maxItems)
		}
		if argHasInt(args, "sample_rows") {
			delegateArgs["sample_rows"] = getIntArg(args, "sample_rows", 3)
		}
		if argPresent(args, "include_samples") {
			delegateArgs["include_samples"] = getBoolArg(args, "include_samples", true)
		} else {
			delegateArgs["include_samples"] = view != "summary"
		}

		res, err := delegate.Execute(ctx, delegateArgs)
		if err != nil {
			return nil, err
		}
		resMap := asMap(res)
		grids, _ := resMap["grids"].([]interface{})
		totalGrids := asInt(resMap["total_grids"])
		if totalGrids <= 0 {
			totalGrids = len(grids)
		}

		nextStep := map[string]interface{}{
			"tool": "browser-observe",
			"args": map[string]interface{}{
				"session_id": sessionID,
				"mode":       "interactive",
				"view":       "compact",
			},
		}
		if len(grids) > 0 {
			if firstGrid, ok := grids[0].(map[string]interface{}); ok {
				if rowRefs, ok := firstGrid["sample_row_refs"].([]interface{}); ok && len(rowRefs) > 0 {
					if firstRow, ok := rowRefs[0].(map[string]interface{}); ok {
						ref := getStringFromMap(firstRow, "ref")
						if ref != "" {
							nextStep = map[string]interface{}{
								"tool": "browser-act",
								"args": map[string]interface{}{
									"session_id": sessionID,
									"operations": []map[string]interface{}{
										{
											"type":   "interact",
											"action": "click",
											"ref":    ref,
										},
									},
								},
								"reason": "Try the first sampled row ref to validate row targeting",
							}
						}
					}
				}
			}
		}

		handles := []string{"observe:" + sessionID + ":grids"}
		emitDisclosureFacts(ctx, t.engine, sessionID, handles, "observe")

		response := map[string]interface{}{
			"success":          true,
			"status":           "ok",
			"intent":           ternaryStatus(hasIntent, intent, "custom"),
			"intent_applied":   intentApplied,
			"mode":             mode,
			"view":             view,
			"summary":          fmt.Sprintf("%d grid surface(s) detected", totalGrids),
			"evidence_handles": handles,
			"truncated":        false,
			"next_step":        nextStep,
		}
		switch view {
		case "summary":
			response["data"] = map[string]interface{}{
				"total_grids": totalGrids,
			}
		case "compact":
			response["data"] = map[string]interface{}{
				"total_grids": totalGrids,
				"grids":       limitAnySlice(grids, maxItems),
			}
		default:
			response["data"] = resMap
		}
		return response, nil

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
		diagView := "compact"
		if view == "summary" {
			diagView = "summary"
		} else if view == "full" {
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

	topErrors := buildObserveTopErrors(sessionID, diagnosticsData, toastData, maxInt(6, minInt(maxItems, 20)))
	investigationItems := buildInvestigationItems(sessionID, topErrors, minInt(maxItems, 8))
	if len(topErrors) > 0 {
		handles = append(handles, "observe:"+sessionID+":top_errors")
	}
	if len(investigationItems) > 0 {
		handles = append(handles, "observe:"+sessionID+":investigation_items")
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
		if len(topErrors) > 0 {
			data["top_errors"] = limitMapSlice(topErrors, minInt(3, maxItems))
		}
		if len(investigationItems) > 0 {
			data["investigation_items"] = limitMapSlice(investigationItems, minInt(3, maxItems))
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
		if len(topErrors) > 0 {
			data["top_errors"] = limitMapSlice(topErrors, minInt(maxItems, 10))
		}
		if len(investigationItems) > 0 {
			data["investigation_items"] = limitMapSlice(investigationItems, minInt(maxItems, 8))
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
		if len(topErrors) > 0 {
			data["top_errors"] = topErrors
		}
		if len(investigationItems) > 0 {
			data["investigation_items"] = investigationItems
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
	return `Perform browser actions -- navigate, click, type, manage sessions, wait, run JS.

Pass an operations array; they execute in sequence. Use browser-observe first to get ref IDs.

Operation types:
  Session:  session_create (url), session_attach (target_id), session_fork (clone auth state)
  Navigate: navigate (url), history (back/forward/reload)
  Interact: click, type, select, toggle -- requires ref from browser-observe
  Forms:    fill -- batch multiple fields [{ref, value}] + optional submit
  Keyboard: key -- e.g. "Enter", "Tab", "Control+a"
  Waiting:  await_stable (idle detection), await_fact, await_conditions, wait, sleep
  Advanced: js (eval JavaScript, gated), plan (Mangle-derived action sequence)

Options: stop_on_error (default true), view (summary|compact|full).

Use browser-observe to understand the page, browser-reason if something goes wrong.`
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
				"session_id": getStringFromMap(op, "source_session_id"),
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
	return `Diagnose browser problems -- health checks, root cause analysis, blocking issues.

Use when something goes wrong or you need guidance on what to do next.
Analyzes Mangle facts (network, console, DOM) with optional Docker log correlation.

Topics:
  health:             Page health score with error/warning counts
  next_best_action:   Ranked recommendations for what to do next
  blocking_issue:     What prevents progress (modals, auth walls, errors)
  why_failed:         Root cause analysis with causal chains
  what_changed_since: Diff facts since a timestamp (pass time_window_ms)

Intents (presets that apply topic + view defaults): triage, act_now, debug_failure, unblock

Views: summary (verdict + counts), compact (default, verdict + key evidence), full (all evidence).

Results include evidence handles for drill-down -- pass them back via expand_handles to dig deeper.
Use browser-mangle instead for raw Mangle queries; use browser-observe to re-check page state.

Use since_navigation=true to scope results to new errors after the latest navigation event
for the current route.`
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
			"since_navigation": map[string]interface{}{
				"type":        "boolean",
				"description": "When true, scope errors to events after latest navigation_event(SessionId, Url, Timestamp)",
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
	sinceNavigation := getBoolArg(args, "since_navigation", false)
	sinceMs := int64(0)
	if timeWindowMs > 0 {
		sinceMs = time.Now().UnixMilli() - int64(timeWindowMs)
	}
	navigationSinceMs := int64(0)
	if sinceNavigation {
		navigationSinceMs = latestNavigationTimestamp(ctx, t.engine, sessionID)
	}
	effectiveSinceMs := sinceMs
	if navigationSinceMs > effectiveSinceMs {
		effectiveSinceMs = navigationSinceMs
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

	if effectiveSinceMs > 0 {
		rootCauses = filterRowsSince(rootCauses, []string{"Ts", "Timestamp"}, effectiveSinceMs)
		failedReqs = filterRowsSince(failedReqs, []string{"ReqTs", "Timestamp"}, effectiveSinceMs)
		slowApis = filterRowsSince(slowApis, []string{"ReqTs", "Timestamp"}, effectiveSinceMs)
		userVisibleErrors = filterRowsSince(userVisibleErrors, []string{"Timestamp", "Ts"}, effectiveSinceMs)
	}
	userVisibleErrors = dedupeUserVisibleErrors(userVisibleErrors)

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
	topErrors := buildReasonTopErrors(sessionID, failedReqs, rootCauses, userVisibleErrors, blockingIssues, slowApis, maxInt(6, minInt(maxItems, 20)))
	investigationItems := buildInvestigationItems(sessionID, topErrors, minInt(maxItems, 10))

	data := map[string]interface{}{
		"top_errors":          topErrors,
		"investigation_items": investigationItems,
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
		"reason:" + sessionID + ":top_errors",
		"reason:" + sessionID + ":investigation_items",
		"reason:" + sessionID + ":failed_requests",
		"reason:" + sessionID + ":root_causes",
		"reason:" + sessionID + ":slow_apis",
		"reason:" + sessionID + ":blocking_issues",
		"reason:" + sessionID + ":user_visible_errors",
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
		"top_errors":          limitMapSlice(topErrors, minInt(5, maxItems)),
		"investigation_items": limitMapSlice(investigationItems, minInt(5, maxItems)),
		"evidence_handles":    handles,
		"expansion_suggested": confidence < 0.70 || len(contradictions) > 0,
		"view":                view,
		"time_window_ms":      timeWindowMs,
		"since_navigation":    sinceNavigation,
		"navigation_since_ms": navigationSinceMs,
		"effective_since_ms":  effectiveSinceMs,
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
	if toasts, ok := data["toasts"].([]map[string]interface{}); ok && len(toasts) > 0 {
		limit := minInt(maxItems, 5)
		out["toasts"] = limitMapSlice(toasts, limit)
		out["toast_count"] = len(toasts)
		out["truncated"] = len(toasts) > limit
	} else if toasts, ok := data["toasts"].([]interface{}); ok && len(toasts) > 0 {
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

type rankedErrorItem struct {
	payload   map[string]interface{}
	severity  int
	timestamp int64
	dedupeKey string
}

func buildObserveTopErrors(sessionID string, diagnosticsData, toastData map[string]interface{}, maxItems int) []map[string]interface{} {
	items := make([]rankedErrorItem, 0, 16)

	toasts := toMapSlice(toastData["toasts"])
	for _, toast := range toasts {
		level := strings.ToLower(strings.TrimSpace(getStringFromMap(toast, "level")))
		if level != "error" && level != "warning" {
			continue
		}
		score := 70
		if level == "error" {
			score = 90
		}
		message := strings.TrimSpace(getStringFromMap(toast, "text"))
		if message == "" {
			message = strings.TrimSpace(getStringFromMap(toast, "summary"))
		}
		if message == "" {
			message = "toast notification"
		}
		source := strings.TrimSpace(getStringFromMap(toast, "source"))
		ts := asInt64(toast["timestamp"])
		items = appendRankedError(items, rankedErrorItem{
			severity:  score,
			timestamp: ts,
			dedupeKey: "toast|" + level + "|" + message,
			payload: map[string]interface{}{
				"kind":            "toast_" + level,
				"message":         message,
				"source":          ternaryStatus(source != "", source, "toast"),
				"severity":        severityLabel(score),
				"severity_score":  score,
				"timestamp":       ts,
				"evidence_handle": "observe:" + sessionID + ":toasts",
			},
		})
	}

	repeated := toStringSlice(toastData["repeated_errors"])
	for _, msg := range repeated {
		if strings.TrimSpace(msg) == "" {
			continue
		}
		score := 85
		items = appendRankedError(items, rankedErrorItem{
			severity:  score,
			timestamp: 0,
			dedupeKey: "toast_repeated|" + msg,
			payload: map[string]interface{}{
				"kind":            "toast_repeated_error",
				"message":         msg,
				"source":          "toast",
				"severity":        severityLabel(score),
				"severity_score":  score,
				"evidence_handle": "observe:" + sessionID + ":toasts",
			},
		})
	}

	for _, row := range toMapSlice(diagnosticsData["failed_requests"]) {
		status := asInt(row["status"])
		score := 75
		if status >= 500 {
			score = 95
		}
		url := strings.TrimSpace(fmt.Sprintf("%v", row["url"]))
		reqID := strings.TrimSpace(fmt.Sprintf("%v", row["id"]))
		ts := asInt64(row["timestamp"])
		msg := fmt.Sprintf("HTTP %d %s", status, url)
		if url == "" {
			msg = fmt.Sprintf("HTTP %d failed request", status)
		}
		items = appendRankedError(items, rankedErrorItem{
			severity:  score,
			timestamp: ts,
			dedupeKey: "failed_request|" + reqID + "|" + url + "|" + strconv.Itoa(status),
			payload: map[string]interface{}{
				"kind":            "failed_request",
				"message":         msg,
				"url":             url,
				"request_id":      reqID,
				"status":          status,
				"severity":        severityLabel(score),
				"severity_score":  score,
				"timestamp":       ts,
				"evidence_handle": "observe:" + sessionID + ":diagnostics",
			},
		})
	}

	for _, row := range toMapSlice(diagnosticsData["root_causes"]) {
		cause := strings.TrimSpace(fmt.Sprintf("%v", row["Cause"]))
		msg := strings.TrimSpace(fmt.Sprintf("%v", row["Msg"]))
		if msg == "" {
			msg = cause
		}
		if msg == "" {
			msg = "root cause detected"
		}
		source := strings.TrimSpace(fmt.Sprintf("%v", row["Source"]))
		score := 88
		items = appendRankedError(items, rankedErrorItem{
			severity:  score,
			timestamp: extractTimestamp(row, "Ts", "Timestamp"),
			dedupeKey: "root_cause|" + source + "|" + msg + "|" + cause,
			payload: map[string]interface{}{
				"kind":            "root_cause",
				"message":         msg,
				"cause":           cause,
				"source":          source,
				"severity":        severityLabel(score),
				"severity_score":  score,
				"timestamp":       extractTimestamp(row, "Ts", "Timestamp"),
				"evidence_handle": "observe:" + sessionID + ":diagnostics",
			},
		})
	}

	for _, row := range toMapSlice(diagnosticsData["slow_apis"]) {
		duration := asInt(row["duration"])
		score := 55
		if duration >= 3000 {
			score = 65
		}
		url := strings.TrimSpace(fmt.Sprintf("%v", row["url"]))
		reqID := strings.TrimSpace(fmt.Sprintf("%v", row["id"]))
		msg := fmt.Sprintf("Slow API %dms %s", duration, url)
		if url == "" {
			msg = fmt.Sprintf("Slow API %dms", duration)
		}
		items = appendRankedError(items, rankedErrorItem{
			severity:  score,
			timestamp: extractTimestamp(row, "ReqTs", "Timestamp"),
			dedupeKey: "slow_api|" + reqID + "|" + url + "|" + strconv.Itoa(duration),
			payload: map[string]interface{}{
				"kind":            "slow_api",
				"message":         msg,
				"url":             url,
				"request_id":      reqID,
				"duration_ms":     duration,
				"severity":        severityLabel(score),
				"severity_score":  score,
				"timestamp":       extractTimestamp(row, "ReqTs", "Timestamp"),
				"evidence_handle": "observe:" + sessionID + ":diagnostics",
			},
		})
	}

	return finalizeRankedErrors(items, maxItems)
}

func buildReasonTopErrors(
	sessionID string,
	failedReqs []map[string]interface{},
	rootCauses []map[string]interface{},
	userVisibleErrors []map[string]interface{},
	blockingIssues []map[string]interface{},
	slowApis []map[string]interface{},
	maxItems int,
) []map[string]interface{} {
	items := make([]rankedErrorItem, 0, 24)

	for _, row := range failedReqs {
		status := asInt(row["Status"])
		score := 78
		if status >= 500 {
			score = 96
		}
		url := strings.TrimSpace(fmt.Sprintf("%v", row["Url"]))
		reqID := strings.TrimSpace(fmt.Sprintf("%v", row["ReqId"]))
		msg := fmt.Sprintf("HTTP %d %s", status, url)
		if url == "" {
			msg = fmt.Sprintf("HTTP %d failed request", status)
		}
		ts := extractTimestamp(row, "ReqTs", "Timestamp")
		items = appendRankedError(items, rankedErrorItem{
			severity:  score,
			timestamp: ts,
			dedupeKey: "failed_request|" + reqID + "|" + url + "|" + strconv.Itoa(status),
			payload: map[string]interface{}{
				"kind":            "failed_request",
				"message":         msg,
				"url":             url,
				"request_id":      reqID,
				"status":          status,
				"severity":        severityLabel(score),
				"severity_score":  score,
				"timestamp":       ts,
				"evidence_handle": "reason:" + sessionID + ":failed_requests",
			},
		})
	}

	for _, row := range rootCauses {
		cause := strings.TrimSpace(fmt.Sprintf("%v", row["Cause"]))
		msg := strings.TrimSpace(fmt.Sprintf("%v", row["ConsoleMsg"]))
		if msg == "" {
			msg = cause
		}
		if msg == "" {
			msg = "root cause detected"
		}
		source := strings.TrimSpace(fmt.Sprintf("%v", row["Source"]))
		ts := extractTimestamp(row, "Ts", "Timestamp")
		score := 90
		items = appendRankedError(items, rankedErrorItem{
			severity:  score,
			timestamp: ts,
			dedupeKey: "root_cause|" + source + "|" + msg + "|" + cause,
			payload: map[string]interface{}{
				"kind":            "root_cause",
				"message":         msg,
				"cause":           cause,
				"source":          source,
				"severity":        severityLabel(score),
				"severity_score":  score,
				"timestamp":       ts,
				"evidence_handle": "reason:" + sessionID + ":root_causes",
			},
		})
	}

	for _, row := range userVisibleErrors {
		source := strings.TrimSpace(fmt.Sprintf("%v", row["Source"]))
		msg := strings.TrimSpace(fmt.Sprintf("%v", row["Message"]))
		if msg == "" {
			msg = "user-visible error"
		}
		ts := extractTimestamp(row, "Timestamp", "Ts")
		kind, score := classifyUserVisibleError(source, msg)
		fingerprint := normalizeErrorFingerprint(msg)
		if fingerprint == "" {
			fingerprint = strings.ToLower(strings.TrimSpace(msg))
		}
		items = appendRankedError(items, rankedErrorItem{
			severity:  score,
			timestamp: ts,
			dedupeKey: "user_visible|" + strings.ToLower(strings.TrimSpace(source)) + "|" + fingerprint,
			payload: map[string]interface{}{
				"kind":            kind,
				"message":         msg,
				"source":          source,
				"severity":        severityLabel(score),
				"severity_score":  score,
				"timestamp":       ts,
				"evidence_handle": "reason:" + sessionID + ":user_visible_errors",
			},
		})
	}

	for _, row := range blockingIssues {
		reason := strings.TrimSpace(fmt.Sprintf("%v", row["Reason"]))
		if reason == "" {
			reason = "interaction blocked"
		}
		score := 82
		items = appendRankedError(items, rankedErrorItem{
			severity:  score,
			timestamp: extractTimestamp(row, "Timestamp", "Ts"),
			dedupeKey: "blocking|" + reason,
			payload: map[string]interface{}{
				"kind":            "blocking_issue",
				"message":         reason,
				"severity":        severityLabel(score),
				"severity_score":  score,
				"timestamp":       extractTimestamp(row, "Timestamp", "Ts"),
				"evidence_handle": "reason:" + sessionID + ":blocking_issues",
			},
		})
	}

	for _, row := range slowApis {
		duration := asInt(row["Duration"])
		score := 50
		if duration >= 3000 {
			score = 64
		}
		url := strings.TrimSpace(fmt.Sprintf("%v", row["Url"]))
		reqID := strings.TrimSpace(fmt.Sprintf("%v", row["ReqId"]))
		msg := fmt.Sprintf("Slow API %dms %s", duration, url)
		if url == "" {
			msg = fmt.Sprintf("Slow API %dms", duration)
		}
		ts := extractTimestamp(row, "ReqTs", "Timestamp")
		items = appendRankedError(items, rankedErrorItem{
			severity:  score,
			timestamp: ts,
			dedupeKey: "slow_api|" + reqID + "|" + url + "|" + strconv.Itoa(duration),
			payload: map[string]interface{}{
				"kind":            "slow_api",
				"message":         msg,
				"url":             url,
				"request_id":      reqID,
				"duration_ms":     duration,
				"severity":        severityLabel(score),
				"severity_score":  score,
				"timestamp":       ts,
				"evidence_handle": "reason:" + sessionID + ":slow_apis",
			},
		})
	}

	return finalizeRankedErrors(items, maxItems)
}

func buildInvestigationItems(sessionID string, topErrors []map[string]interface{}, maxItems int) []map[string]interface{} {
	if maxItems <= 0 {
		maxItems = 5
	}
	items := make([]map[string]interface{}, 0, minInt(len(topErrors), maxItems))
	for idx, errRow := range topErrors {
		if idx >= maxItems {
			break
		}
		message := strings.TrimSpace(fmt.Sprintf("%v", errRow["message"]))
		if message == "" {
			message = "investigate issue"
		}
		evidenceHandle := strings.TrimSpace(fmt.Sprintf("%v", errRow["evidence_handle"]))
		items = append(items, map[string]interface{}{
			"priority":        idx + 1,
			"severity":        errRow["severity"],
			"kind":            errRow["kind"],
			"issue":           message,
			"evidence_handle": evidenceHandle,
			"next_step":       buildInvestigationStep(sessionID, errRow),
		})
	}
	return items
}

func buildInvestigationStep(sessionID string, errRow map[string]interface{}) map[string]interface{} {
	kind := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", errRow["kind"])))
	switch kind {
	case "failed_request":
		return map[string]interface{}{
			"tool": "browser-mangle",
			"args": map[string]interface{}{
				"operation": "query",
				"query":     fmt.Sprintf("failed_request(%q, ReqId, Url, Status).", sessionID),
				"view":      "compact",
				"max_items": 20,
			},
		}
	case "root_cause":
		return map[string]interface{}{
			"tool": "browser-mangle",
			"args": map[string]interface{}{
				"operation": "query",
				"query":     fmt.Sprintf("root_cause_at(%q, ConsoleMsg, Source, Cause, Ts).", sessionID),
				"view":      "compact",
				"max_items": 20,
			},
		}
	case "compiler_error", "console_error":
		return map[string]interface{}{
			"tool": "browser-mangle",
			"args": map[string]interface{}{
				"operation": "query",
				"query":     fmt.Sprintf("console_event(%q, \"error\", Msg, Ts).", sessionID),
				"view":      "compact",
				"max_items": 20,
			},
		}
	case "user_visible_error":
		source := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", errRow["source"])))
		if source == "console" {
			return map[string]interface{}{
				"tool": "browser-mangle",
				"args": map[string]interface{}{
					"operation": "query",
					"query":     fmt.Sprintf("console_event(%q, \"error\", Msg, Ts).", sessionID),
					"view":      "compact",
					"max_items": 20,
				},
			}
		}
		fallthrough
	case "toast_error", "toast_warning", "toast_repeated_error":
		return map[string]interface{}{
			"tool": "browser-mangle",
			"args": map[string]interface{}{
				"operation": "query",
				"query":     fmt.Sprintf("toast_notification(%q, Text, Level, Source, Timestamp).", sessionID),
				"view":      "compact",
				"max_items": 20,
			},
		}
	case "blocking_issue":
		return map[string]interface{}{
			"tool": "browser-observe",
			"args": map[string]interface{}{
				"session_id":          sessionID,
				"mode":                "interactive",
				"view":                "compact",
				"include_diagnostics": true,
				"include_action_plan": true,
				"max_recommendations": 3,
				"max_items":           20,
			},
		}
	case "slow_api":
		return map[string]interface{}{
			"tool": "browser-mangle",
			"args": map[string]interface{}{
				"operation": "query",
				"query":     fmt.Sprintf("slow_api_at(%q, ReqId, Url, Duration, ReqTs).", sessionID),
				"view":      "compact",
				"max_items": 20,
			},
		}
	default:
		return map[string]interface{}{
			"tool": "browser-reason",
			"args": map[string]interface{}{
				"session_id":     sessionID,
				"topic":          "why_failed",
				"view":           "compact",
				"max_items":      20,
				"time_window_ms": defaultReasonTimeWindowMs,
			},
		}
	}
}

func appendRankedError(items []rankedErrorItem, item rankedErrorItem) []rankedErrorItem {
	if item.payload == nil {
		item.payload = map[string]interface{}{}
	}
	if item.payload["severity"] == nil {
		item.payload["severity"] = severityLabel(item.severity)
	}
	item.payload["severity_score"] = item.severity
	if item.payload["timestamp"] == nil && item.timestamp > 0 {
		item.payload["timestamp"] = item.timestamp
	}
	return append(items, item)
}

func finalizeRankedErrors(items []rankedErrorItem, maxItems int) []map[string]interface{} {
	if len(items) == 0 {
		return []map[string]interface{}{}
	}
	if maxItems <= 0 {
		maxItems = 10
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].severity != items[j].severity {
			return items[i].severity > items[j].severity
		}
		if items[i].timestamp != items[j].timestamp {
			return items[i].timestamp > items[j].timestamp
		}
		msgI := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", items[i].payload["message"])))
		msgJ := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", items[j].payload["message"])))
		return msgI < msgJ
	})

	seen := make(map[string]bool, len(items))
	out := make([]map[string]interface{}, 0, minInt(maxItems, len(items)))
	for _, item := range items {
		key := strings.TrimSpace(item.dedupeKey)
		if key != "" {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		out = append(out, item.payload)
		if len(out) >= maxItems {
			break
		}
	}
	return out
}

func severityLabel(score int) string {
	switch {
	case score >= 90:
		return "critical"
	case score >= 75:
		return "high"
	case score >= 55:
		return "medium"
	default:
		return "low"
	}
}

func normalizeErrorFingerprint(message string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(message)), " "))
}

func classifyUserVisibleError(source, message string) (string, int) {
	src := strings.ToLower(strings.TrimSpace(source))
	fp := normalizeErrorFingerprint(message)
	if fp == "" {
		fp = strings.ToLower(strings.TrimSpace(message))
	}

	compilerSignals := []string{
		"module not found",
		"can't resolve",
		"cannot resolve",
		"cannot find module",
		"failed to compile",
		"compilation failed",
		"import trace",
		"./src/",
		"webpack",
		"nextjs.org/docs/messages/module-not-found",
	}
	for _, signal := range compilerSignals {
		if strings.Contains(fp, signal) {
			return "compiler_error", 99
		}
	}

	switch src {
	case "console":
		return "console_error", 92
	case "toast":
		return "toast_error", 84
	default:
		return "user_visible_error", 88
	}
}

func dedupeUserVisibleErrors(rows []map[string]interface{}) []map[string]interface{} {
	if len(rows) == 0 {
		return []map[string]interface{}{}
	}

	type agg struct {
		source  string
		message string
		count   int
		firstTs int64
		lastTs  int64
	}

	byKey := make(map[string]*agg, len(rows))
	for _, row := range rows {
		source := strings.TrimSpace(fmt.Sprintf("%v", row["Source"]))
		if source == "" {
			source = strings.TrimSpace(fmt.Sprintf("%v", row["source"]))
		}
		message := strings.TrimSpace(fmt.Sprintf("%v", row["Message"]))
		if message == "" {
			message = strings.TrimSpace(fmt.Sprintf("%v", row["message"]))
		}
		if message == "" {
			continue
		}

		fp := normalizeErrorFingerprint(message)
		if fp == "" {
			continue
		}
		sourceKey := strings.ToLower(strings.TrimSpace(source))
		dedupeKey := sourceKey + "|" + fp
		ts := extractTimestamp(row, "Timestamp", "Ts")

		existing, ok := byKey[dedupeKey]
		if !ok {
			byKey[dedupeKey] = &agg{
				source:  source,
				message: message,
				count:   1,
				firstTs: ts,
				lastTs:  ts,
			}
			continue
		}

		existing.count++
		if ts > 0 && (existing.firstTs == 0 || ts < existing.firstTs) {
			existing.firstTs = ts
		}
		if ts > existing.lastTs {
			existing.lastTs = ts
		}
	}

	out := make([]map[string]interface{}, 0, len(byKey))
	for _, item := range byKey {
		row := map[string]interface{}{
			"Source":    item.source,
			"Message":   item.message,
			"Timestamp": item.lastTs,
			"Count":     item.count,
		}
		if item.firstTs > 0 {
			row["FirstTimestamp"] = item.firstTs
		}
		if item.lastTs > 0 {
			row["LastTimestamp"] = item.lastTs
		}
		out = append(out, row)
	}

	sort.SliceStable(out, func(i, j int) bool {
		tsI := extractTimestamp(out[i], "Timestamp", "LastTimestamp")
		tsJ := extractTimestamp(out[j], "Timestamp", "LastTimestamp")
		if tsI != tsJ {
			return tsI > tsJ
		}
		countI := asInt(out[i]["Count"])
		countJ := asInt(out[j]["Count"])
		if countI != countJ {
			return countI > countJ
		}
		msgI := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", out[i]["Message"])))
		msgJ := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", out[j]["Message"])))
		return msgI < msgJ
	})

	return out
}

func toMapSlice(value interface{}) []map[string]interface{} {
	if value == nil {
		return []map[string]interface{}{}
	}
	if rows, ok := value.([]map[string]interface{}); ok {
		return rows
	}
	rawRows, ok := value.([]interface{})
	if !ok {
		return []map[string]interface{}{}
	}
	rows := make([]map[string]interface{}, 0, len(rawRows))
	for _, raw := range rawRows {
		if row, ok := raw.(map[string]interface{}); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func toStringSlice(value interface{}) []string {
	if value == nil {
		return []string{}
	}
	if rows, ok := value.([]string); ok {
		return rows
	}
	rawRows, ok := value.([]interface{})
	if !ok {
		return []string{}
	}
	rows := make([]string, 0, len(rawRows))
	for _, raw := range rawRows {
		msg := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if msg != "" {
			rows = append(rows, msg)
		}
	}
	return rows
}

func extractTimestamp(row map[string]interface{}, keys ...string) int64 {
	for _, key := range keys {
		if key == "" {
			continue
		}
		if val, ok := row[key]; ok {
			ts := asInt64(val)
			if ts > 0 {
				return ts
			}
		}
	}
	return 0
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

func latestNavigationTimestamp(ctx context.Context, engine *mangle.Engine, sessionID string) int64 {
	if engine == nil || strings.TrimSpace(sessionID) == "" {
		return 0
	}
	rows := queryToRows(ctx, engine, fmt.Sprintf("navigation_event(%q, Url, Timestamp).", sessionID))
	latest := int64(0)
	for _, row := range rows {
		ts := extractTimestamp(row, "Timestamp", "Ts", "TNav")
		if ts > latest {
			latest = ts
		}
	}
	return latest
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
		case strings.Contains(handle, "top_errors"):
			selected["top_errors"] = true
		case strings.Contains(handle, "investigation_items"):
			selected["investigation_items"] = true
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
	case "grid_hunt":
		return observeIntentDefaults{
			mode:               "grids",
			view:               "compact",
			maxItems:           12,
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
		"discover-grids",
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

func toAnySlice(value interface{}) []interface{} {
	if value == nil {
		return []interface{}{}
	}
	if items, ok := value.([]interface{}); ok {
		return items
	}
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice {
		return []interface{}{}
	}
	items := make([]interface{}, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		items[i] = rv.Index(i).Interface()
	}
	return items
}

func limitMapSlice(items []map[string]interface{}, max int) []map[string]interface{} {
	if max <= 0 || len(items) <= max {
		return items
	}
	return items[:max]
}

// BrowserMangleTool consolidates all Mangle fact operations into one progressive-disclosure tool.
type BrowserMangleTool struct {
	engine           *mangle.Engine
	dockerClient     *docker.Client
	recorder         *recorder.Recorder
	defaultTraceDir  string
	defaultLogWindow time.Duration
}

func (t *BrowserMangleTool) Name() string { return "browser-mangle" }
func (t *BrowserMangleTool) Description() string {
	return `Query and manipulate the Mangle fact engine directly.

Use for raw fact access. Prefer browser-reason for high-level diagnostics.
Mangle stores browser facts (network, console, DOM, navigation) and derives new facts via rules.

Operations:
  query:            Execute Mangle query. E.g. query:"current_url(S, U)."
  read:             Read recent buffered facts (newest first, optional predicate_filter)
  temporal:         Query facts in a time window (after_ms / before_ms, epoch ms)
  evaluate:         Check if a derived predicate currently has matches
  push:             Add facts to the engine
  submit_rule:      Add a derivation rule (Datalog syntax)
  subscribe:        Watch for a predicate match with timeout
  await_fact:       Wait for a specific fact (predicate + args)
  await_conditions: Wait for multiple facts simultaneously (AND logic)
  export_flight:    Export raw JSONL evidence bundle (facts + optional Docker logs)

Common built-in predicates: current_url, net_request, net_response, console_event,
navigation_event, slow_api (derived), caused_by (derived), login_succeeded (derived).

Views: summary (~100 tokens), compact (default ~500), full (all rows).`
}

func (t *BrowserMangleTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"description": "Mangle operation to perform",
				"enum":        []string{"query", "temporal", "evaluate", "read", "submit_rule", "subscribe", "push", "await_fact", "await_conditions", "export_flight"},
			},
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session (recommended for export_flight and scoped reads)",
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
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Output path for export_flight (defaults to recorder trace_dir/flight-<ts>.jsonl)",
			},
			"include_server_logs": map[string]interface{}{
				"type":        "boolean",
				"description": "Include Docker server logs in export_flight (default false)",
			},
			"since_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Filter export_flight rows at/after this epoch ms (default now-log_window or now-5m)",
			},
			"until_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Filter export_flight rows at/before this epoch ms (default now)",
			},
			"max_rows": map[string]interface{}{
				"type":        "integer",
				"description": "Max rows written by export_flight (default 2000, newest rows kept when truncated)",
			},
		},
		"required": []string{"operation"},
	}
}

func (t *BrowserMangleTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	operation := strings.ToLower(getStringArg(args, "operation"))
	if operation == "" {
		return map[string]interface{}{"success": false, "error": "operation is required"}, nil
	}
	if operation == "export_flight" {
		return t.handleExportFlight(ctx, args), nil
	}
	if t.engine == nil {
		return map[string]interface{}{"success": false, "error": "mangle engine is not available"}, nil
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

func (t *BrowserMangleTool) handleExportFlight(ctx context.Context, args map[string]interface{}) map[string]interface{} {
	view := normalizeProgressiveView(getStringArg(args, "view"))
	sessionID := strings.TrimSpace(getStringArg(args, "session_id"))
	includeServerLogs := getBoolArg(args, "include_server_logs", false)

	untilMs := asInt64(args["until_ms"])
	if untilMs <= 0 {
		untilMs = time.Now().UnixMilli()
	}

	sinceMs := asInt64(args["since_ms"])
	if sinceMs <= 0 {
		window := t.defaultLogWindow
		if window <= 0 {
			window = 5 * time.Minute
		}
		sinceMs = untilMs - int64(window/time.Millisecond)
	}
	if sinceMs > untilMs {
		sinceMs = untilMs
	}

	maxRows := getIntArg(args, "max_rows", 2000)
	if maxRows <= 0 {
		maxRows = 2000
	}
	if maxRows > 100000 {
		maxRows = 100000
	}

	outPath, err := resolveFlightExportPath(getStringArg(args, "path"), t.defaultTraceDir, sessionID, untilMs)
	if err != nil {
		return map[string]interface{}{
			"success":   false,
			"operation": "export_flight",
			"error":     err.Error(),
		}
	}

	rows := make([]map[string]interface{}, 0, maxRows)
	factRows := collectFlightFactRows(t.engine, sessionID, sinceMs, untilMs)
	rows = append(rows, factRows...)

	logRows := []map[string]interface{}{}
	warnings := make([]string, 0, 1)
	if includeServerLogs {
		if t.dockerClient == nil {
			warnings = append(warnings, "docker log export requested but docker integration is disabled")
		} else {
			logs, logErr := t.dockerClient.QueryLogs(ctx, time.UnixMilli(sinceMs))
			if logErr != nil {
				warnings = append(warnings, "docker log export failed: "+logErr.Error())
			} else {
				logRows = collectFlightDockerRows(logs, sinceMs, untilMs)
				rows = append(rows, logRows...)
			}
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		ti := asInt64(rows[i]["ts"])
		tj := asInt64(rows[j]["ts"])
		if ti != tj {
			return ti < tj
		}
		si := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", rows[i]["source"])))
		sj := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", rows[j]["source"])))
		return si < sj
	})

	truncated := false
	if len(rows) > maxRows {
		rows = rows[len(rows)-maxRows:]
		truncated = true
	}

	if err := writeJSONLFile(outPath, rows); err != nil {
		return map[string]interface{}{
			"success":   false,
			"operation": "export_flight",
			"error":     fmt.Sprintf("write export file: %v", err),
		}
	}

	if t.recorder != nil {
		t.recorder.Log("export_flight", sessionID, map[string]interface{}{
			"path":                outPath,
			"rows_written":        len(rows),
			"fact_rows":           len(factRows),
			"docker_rows":         len(logRows),
			"include_server_logs": includeServerLogs,
			"since_ms":            sinceMs,
			"until_ms":            untilMs,
			"truncated":           truncated,
		})
	}

	summary := fmt.Sprintf("exported %d row(s) to %s", len(rows), outPath)
	if truncated {
		summary += " (truncated to newest rows)"
	}
	if sessionID != "" {
		summary += fmt.Sprintf(" [session=%s]", sessionID)
	}

	exportData := map[string]interface{}{
		"path":                outPath,
		"rows_written":        len(rows),
		"fact_rows":           len(factRows),
		"docker_rows":         len(logRows),
		"session_id":          sessionID,
		"include_server_logs": includeServerLogs,
		"since_ms":            sinceMs,
		"until_ms":            untilMs,
		"truncated":           truncated,
	}

	response := map[string]interface{}{
		"success":   true,
		"operation": "export_flight",
		"view":      view,
		"summary":   summary,
		"export":    exportData,
	}
	if len(warnings) > 0 {
		response["warnings"] = warnings
	}

	switch view {
	case "summary":
		// Keep summary token-light: no sample rows.
	case "compact":
		response["sample_rows"] = limitAnySlice(mapSliceToAny(rows), minInt(5, len(rows)))
	default:
		response["rows"] = rows
	}

	return response
}

func resolveFlightExportPath(rawPath, defaultTraceDir, sessionID string, untilMs int64) (string, error) {
	baseDir := strings.TrimSpace(defaultTraceDir)
	if baseDir == "" {
		baseDir = recorder.TraceDir
	}

	path := strings.TrimSpace(rawPath)
	if path == "" {
		path = filepath.Join(baseDir, fmt.Sprintf("flight_%s_%d.jsonl", safeTraceFragment(sessionID, "global"), untilMs))
	} else if !filepath.IsAbs(path) {
		path = filepath.Join(baseDir, path)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create export directory: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve export path: %w", err)
	}
	return absPath, nil
}

func collectFlightFactRows(engine *mangle.Engine, sessionID string, sinceMs, untilMs int64) []map[string]interface{} {
	if engine == nil {
		return []map[string]interface{}{}
	}

	facts := engine.Facts()
	rows := make([]map[string]interface{}, 0, len(facts))
	for _, f := range facts {
		if sessionID != "" {
			if len(f.Args) == 0 || fmt.Sprintf("%v", f.Args[0]) != sessionID {
				continue
			}
		}

		ts := f.Timestamp.UnixMilli()
		if ts > 0 {
			if sinceMs > 0 && ts < sinceMs {
				continue
			}
			if untilMs > 0 && ts > untilMs {
				continue
			}
		}

		row := map[string]interface{}{
			"ts":        ts,
			"source":    "mangle_fact",
			"predicate": f.Predicate,
			"args":      f.Args,
		}
		if len(f.Args) > 0 {
			row["session_id"] = fmt.Sprintf("%v", f.Args[0])
		}
		rows = append(rows, row)
	}
	return rows
}

func collectFlightDockerRows(logs []docker.LogEntry, sinceMs, untilMs int64) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(logs))
	for _, entry := range logs {
		ts := entry.Timestamp.UnixMilli()
		if sinceMs > 0 && ts < sinceMs {
			continue
		}
		if untilMs > 0 && ts > untilMs {
			continue
		}
		rows = append(rows, map[string]interface{}{
			"ts":        ts,
			"source":    "docker_log",
			"container": entry.Container,
			"level":     entry.Level,
			"tag":       entry.Tag,
			"message":   entry.Message,
			"raw":       entry.Raw,
		})
	}
	return rows
}

func writeJSONLFile(path string, rows []map[string]interface{}) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			return err
		}
	}
	return nil
}

func safeTraceFragment(input, fallback string) string {
	raw := strings.TrimSpace(input)
	if raw == "" {
		raw = fallback
	}
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('_')
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return fallback
	}
	return out
}

func mapSliceToAny(rows []map[string]interface{}) []interface{} {
	out := make([]interface{}, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	return out
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
