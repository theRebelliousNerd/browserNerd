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
	// doctor builds the server self-diagnosis for mode "doctor".
	doctor func(ctx context.Context) map[string]interface{}
}

// observePlanningGroups is how many control groups composite observe extracts (and
// emits facts for) when it plans actions, however few rows it returns.
const observePlanningGroups = 60

func (t *BrowserObserveTool) Name() string { return "browser-observe" }
func (t *BrowserObserveTool) Description() string {
	return `See a page, small by default; drill down with mode.
- composite (default): title, headings, status/alert text, health digest, controls grouped
  (400 identical buttons = 1 row with count + refs), ranked browser-act calls.
- text: read the page (headings, landmarks, live regions, text blocks); text_query filters,
  max_chars caps. Prefer it to a screenshot for reading.
- interactive: controls only; refs feed browser-act.
- screenshot: saves an image, returns path + size. annotate numbers controls in view; ref
  captures one element.
- storage: signed out / 401? cookies and web-storage keys with flags and expiry, and when
  each JWT (even one nested in a JSON state blob) expires. Never values.
- sessions: live tabs. doctor: BrowserNERD's own health (stale binary, wedged calls, env).
  state|nav|hidden|grids|react|dom_snapshot: narrow slices.
view: summary < compact (default) < full.`
}

func (t *BrowserObserveTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Session (omit for sessions/doctor)",
			},
			"intent": map[string]interface{}{
				"type":        "string",
				"description": "Preset for omitted knobs",
				"enum":        []string{"quick_status", "find_actions", "map_navigation", "hidden_content", "deep_audit", "check_sessions", "visual_check", "grid_hunt"},
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"description": "Default composite",
				"enum":        []string{"composite", "text", "interactive", "screenshot", "storage", "sessions", "state", "nav", "hidden", "grids", "react", "dom_snapshot", "doctor"},
			},
			"view": map[string]interface{}{
				"type":        "string",
				"description": "Default compact",
				"enum":        []string{"summary", "compact", "full"},
			},
			"max_items": map[string]interface{}{
				"type":        "integer",
				"description": "List rows (default 20; annotate 30)",
			},
			"text_query": map[string]interface{}{
				"type":        "string",
				"description": "text: only blocks containing this term",
			},
			"max_chars": map[string]interface{}{
				"type":        "integer",
				"description": "text: char budget (default 1500)",
			},
			"filter": map[string]interface{}{
				"type":        "string",
				"description": "Control kind",
				"enum":        []string{"all", "buttons", "inputs", "links", "selects"},
			},
			"visible_only": map[string]interface{}{
				"type":        "boolean",
				"description": "Default true",
			},
			"annotate": map[string]interface{}{
				"type":        "boolean",
				"description": "screenshot: number the controls in view",
			},
			"ref": map[string]interface{}{
				"type":        "string",
				"description": "screenshot: this element only",
			},
			"full_page": map[string]interface{}{
				"type":        "boolean",
				"description": "screenshot: whole page",
			},
			"format": map[string]interface{}{
				"type":        "string",
				"description": "screenshot format",
				"enum":        []string{"png", "jpeg"},
			},
			"quality": map[string]interface{}{
				"type":        "integer",
				"description": "screenshot jpeg quality 1-100 (default 90)",
			},
			"save_path": map[string]interface{}{
				"type":        "string",
				"description": "screenshot: file path",
			},
			"include_detached": map[string]interface{}{
				"type":        "boolean",
				"description": "sessions: also list stale detached records",
			},
			"internal_only": map[string]interface{}{
				"type":        "boolean",
				"description": "nav: same-origin only",
			},
			"emit_facts": map[string]interface{}{
				"type":        "boolean",
				"description": "Default true",
			},
			"include_action_plan": map[string]interface{}{
				"type":        "boolean",
				"description": "Ranked browser-act calls (default true)",
			},
			"include_diagnostics": map[string]interface{}{
				"type":        "boolean",
				"description": "Full diagnostics and toasts (default false)",
			},
			"max_recommendations": map[string]interface{}{
				"type":        "integer",
				"description": "Default 3",
			},
			"max_grids": map[string]interface{}{
				"type":        "integer",
				"description": "grids: max grids",
			},
			"sample_rows": map[string]interface{}{
				"type":        "integer",
				"description": "grids: rows sampled (default 3)",
			},
			"include_samples": map[string]interface{}{
				"type":        "boolean",
				"description": "grids: sample refs (default true)",
			},
			"include_specs": map[string]interface{}{
				"type":        "boolean",
				"description": "Attach route spec excerpts (default false)",
			},
			"spec_terms": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Terms ranking spec excerpts",
			},
		},
	}
}

// withIntent records the intent preset on a response only when the caller used one;
// without one the fields carry nothing an agent needs.
func withIntent(response map[string]interface{}, hasIntent bool, intent string, applied bool) map[string]interface{} {
	if hasIntent {
		response["intent"] = intent
		response["intent_applied"] = applied
	}
	return response
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
	case "doctor":
		report := map[string]interface{}{}
		if t.doctor != nil {
			report = t.doctor(ctx)
		}
		return map[string]interface{}{
			"success": true,
			"status":  "ok",
			"mode":    mode,
			"view":    view,
			"summary": "BrowserNERD self-diagnosis",
			"data":    report,
		}, nil

	case "sessions":
		return withIntent(t.observeSessions(ctx, args, view, maxItems), hasIntent, intent, intentApplied), nil

	case "storage":
		if sessionID == "" {
			return map[string]interface{}{"success": false, "error": "session_id is required for storage mode"}, nil
		}
		response, err := t.observeStorage(ctx, sessionID, view)
		if err != nil {
			return nil, err
		}
		return withIntent(response, hasIntent, intent, intentApplied), nil

	case "text":
		if sessionID == "" {
			return map[string]interface{}{"success": false, "error": "session_id is required for text mode"}, nil
		}
		response, err := t.observeText(ctx, sessionID, args, view)
		if err != nil {
			return nil, err
		}
		return withIntent(response, hasIntent, intent, intentApplied), nil

	case "screenshot":
		if sessionID == "" {
			return map[string]interface{}{"success": false, "error": "session_id is required for screenshot mode"}, nil
		}
		delegate := &ScreenshotTool{sessions: t.sessions, engine: t.engine, pathPolicy: t.pathPolicy}
		delegateArgs := map[string]interface{}{
			"session_id": sessionID,
		}
		for _, key := range []string{"full_page", "save_path", "format", "quality", "annotate"} {
			if v, ok := args[key]; ok {
				delegateArgs[key] = v
			}
		}
		if ref := getStringArg(args, "ref"); ref != "" {
			delegateArgs["element_ref"] = ref
		}
		if argHasInt(args, "max_items") {
			delegateArgs["max_items"] = maxItems
		}
		res, err := delegate.Execute(ctx, delegateArgs)
		if err != nil {
			return nil, err
		}
		resMap := asMap(res)
		if ok, _ := resMap["success"].(bool); !ok {
			return withIntent(map[string]interface{}{
				"success": false,
				"status":  "error",
				"mode":    mode,
				"error":   resMap["error"],
			}, hasIntent, intent, intentApplied), nil
		}
		delete(resMap, "success")
		summary := "screenshot saved"
		if n := asInt(resMap["annotated"]); n > 0 {
			summary = fmt.Sprintf("screenshot saved with %d numbered control(s)", n)
		}
		handles := []string{"observe:" + sessionID + ":screenshot"}
		emitDisclosureFacts(ctx, t.engine, sessionID, handles, "observe")
		return withIntent(map[string]interface{}{
			"success":          true,
			"status":           "ok",
			"mode":             mode,
			"view":             view,
			"summary":          summary,
			"data":             resMap,
			"evidence_handles": handles,
			"truncated":        resMap["truncated"] == true,
		}, hasIntent, intent, intentApplied), nil

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

		response := withIntent(map[string]interface{}{
			"success":          true,
			"status":           "ok",
			"mode":             mode,
			"view":             view,
			"summary":          fmt.Sprintf("%d grid surface(s) detected", totalGrids),
			"evidence_handles": handles,
			"truncated":        false,
			"next_step":        nextStep,
		}, hasIntent, intent, intentApplied)
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
		response := withIntent(map[string]interface{}{
			"success":          true,
			"status":           "ok",
			"mode":             mode,
			"view":             view,
			"evidence_handles": handles,
			"truncated":        false,
		}, hasIntent, intent, intentApplied)
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
		response := withIntent(map[string]interface{}{
			"success":          true,
			"status":           "ok",
			"mode":             mode,
			"view":             view,
			"evidence_handles": handles,
			"truncated":        false,
		}, hasIntent, intent, intentApplied)
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
	contentData := map[string]interface{}{}
	var health map[string]interface{}

	fetchState := mode == "state" || mode == "composite"
	fetchNav := mode == "nav" || mode == "composite"
	fetchInteractive := mode == "interactive" || mode == "composite"
	fetchHidden := mode == "hidden" || (mode == "composite" && view == "full")
	fetchContent := mode == "composite" && view != "summary"
	fetchHealth := mode == "composite" || mode == "state"
	fetchDiagnostics := includeDiagnostics

	// Planning reads more control groups than the output shows, so a primary control
	// below the fold still reaches the plan; grouping keeps that extraction small.
	planningLimit := maxItems
	planningFilter := filter
	if includeActionPlan {
		planningLimit = maxInt(maxItems, observePlanningGroups)
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
			"group":        true,
			"verbose":      view == "full",
		})
		if err != nil {
			return nil, err
		}
		interactivePlanningData = asMap(res)
		interactiveData = filterInteractiveData(interactivePlanningData, filter)
	}

	if fetchContent {
		if text, err := readPageText(ctx, t.sessions, sessionID, "", contentDigestChars, true); err == nil {
			contentData = contentDigest(text)
		}
	}

	if fetchHealth {
		health = observeHealthDigest(t.engine, sessionID)
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
	if includeActionPlan && t.engine != nil && (fetchInteractive || fetchNav) {
		// Query more than we plan to return so we can filter stale candidates safely.
		raw := queryActionCandidatesRaw(ctx, t.engine, sessionID, maxInt(planningLimit*4, 300))

		// Keep only candidates that match the *current* snapshot (prevents stale actions
		// from prior pages), then collapse repeats and rank for the next decision.
		current := filterActionCandidates(raw, buildRefSet(interactivePlanningData), buildHrefSet(navData))
		annotateCandidatesFromRows(current, interactivePlanningData)
		actionCandidates = rankActionCandidates(groupActionCandidates(current))

		currentURL := getStringFromMap(stateData, "url")
		if currentURL == "" {
			currentURL = resolveCurrentURL(ctx, t.engine, sessionID)
		}
		recommendations = buildActionPlanRecommendations(actionCandidates, maxRecommendations, sessionID, originFromURL(currentURL))
		if view == "full" {
			handles = append(handles, "observe:"+sessionID+":action_candidates")
		}
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

	if len(health) > 0 {
		data["health"] = health
	}
	if len(contentData) > 0 {
		data["content"] = contentData
	}

	switch view {
	case "summary":
		if fetchState {
			data["state"] = compactObserveState(stateData)
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
			data["state"] = compactObserveState(stateData)
		}
		// Composite carries links as interactive rows with their href; the area map is
		// mode nav.
		if fetchNav && mode == "nav" {
			data["nav"] = navData
		}
		if fetchInteractive {
			data["interactive"] = compactObserveInteractive(interactiveData, maxItems)
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
		if includeActionPlan && len(recommendations) > 0 {
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

	if getBoolArg(args, "include_specs", false) {
		if matches := specContextForSession(t.specsCfg, t.sessions, sessionID, specTermsFromRaw(args["spec_terms"])); len(matches) > 0 {
			data["spec_context"] = matches
			handles = append(handles, "observe:"+sessionID+":spec_context")
		}
	}
	summary := observeSummaryLine(stateData, interactiveData, navData, health, len(actionCandidates))
	emitDisclosureFacts(ctx, t.engine, sessionID, handles, "observe")

	return withIntent(map[string]interface{}{
		"success":          true,
		"status":           "ok",
		"mode":             mode,
		"view":             view,
		"summary":          summary,
		"data":             data,
		"next_step":        suggestObserveNextStep(sessionID, data, mode, view, recommendations),
		"evidence_handles": handles,
		"truncated":        false,
	}, hasIntent, intent, intentApplied), nil
}
