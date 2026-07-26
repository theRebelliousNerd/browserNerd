package mcp

import (
	"context"
	"fmt"
	"strings"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

// BrowserObserveTool provides progressive-disclosure page observation.
// This is a consolidated tool that wraps existing observe/extract tools.
type BrowserObserveTool struct {
	sessions   *browser.SessionManager
	engine     *mangle.Engine
	pathPolicy *security.PathPolicy
	specsCfg   config.SpecsConfig
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
			"include_specs": map[string]interface{}{
				"type":        "boolean",
				"description": "Attach compact configured spec context for the current route (default true)",
			},
			"spec_terms": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Optional feature or requirement terms used to rank spec context",
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
		delegate := &ScreenshotTool{sessions: t.sessions, engine: t.engine, pathPolicy: t.pathPolicy}
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

	if getBoolArg(args, "include_specs", true) {
		if matches := specContextForSession(t.specsCfg, t.sessions, sessionID, specTermsFromRaw(args["spec_terms"])); len(matches) > 0 {
			data["spec_context"] = matches
			handles = append(handles, "observe:"+sessionID+":spec_context")
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
