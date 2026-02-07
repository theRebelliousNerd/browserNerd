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
	return `Observe browser context with progressive disclosure.

MODES:
- state: page url/title/loading/dialog
- nav: grouped navigation links + counts
- interactive: actionable elements + summary
- hidden: hidden/out-of-viewport content
- composite: state + nav + interactive in one call

VIEWS:
- summary: minimal output + handles
- compact: practical output for agent decisions
- full: expanded output for diagnostics

Designed to reduce token cost by returning only the detail level needed now.`
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
				"enum":        []string{"quick_status", "find_actions", "map_navigation", "hidden_content", "deep_audit"},
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"description": "Observation mode",
				"enum":        []string{"state", "nav", "interactive", "hidden", "composite"},
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
		"required": []string{"session_id"},
	}
}

func (t *BrowserObserveTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}
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

	stateTool := &GetPageStateTool{sessions: t.sessions}
	navTool := &GetNavigationLinksTool{sessions: t.sessions, engine: t.engine}
	interactiveTool := &GetInteractiveElementsTool{sessions: t.sessions, engine: t.engine}
	hiddenTool := &DiscoverHiddenContentTool{sessions: t.sessions}

	stateData := map[string]interface{}{}
	navData := map[string]interface{}{}
	interactiveData := map[string]interface{}{}
	hiddenData := map[string]interface{}{}
	diagnosticsData := map[string]interface{}{}
	toastData := map[string]interface{}{}

	fetchState := mode == "state" || mode == "composite"
	fetchNav := mode == "nav" || mode == "composite"
	fetchInteractive := mode == "interactive" || mode == "composite"
	fetchHidden := mode == "hidden" || (mode == "composite" && view == "full")
	fetchDiagnostics := includeDiagnostics

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
			"max_per_area":  maxItems,
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
			"filter":       filter,
			"visible_only": visibleOnly,
			"limit":        maxItems,
			"verbose":      view == "full",
		})
		if err != nil {
			return nil, err
		}
		interactiveData = asMap(res)
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
		actionCandidates = queryActionCandidates(ctx, t.engine, sessionID, maxItems)
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
	return `Execute browser actions through one consolidated interface.

Supported operation types:
- navigate
- interact
- fill
- key
- history
- sleep

Use operations[] to run multi-step flows with one tool call.`
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
							"enum": []string{"navigate", "interact", "fill", "key", "history", "sleep"},
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
		"required": []string{"session_id", "operations"},
	}
}

func (t *BrowserActTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}

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
	return `Reason over browser facts with progressive disclosure.

TOPICS:
- health
- next_best_action
- blocking_issue
- why_failed
- what_changed_since

Returns compact verdicts first, with evidence handles for deeper expansion.`
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
			"tool":   "evaluate-js",
			"reason": "Contradiction detected; JS inspection is now permitted.",
			"args": map[string]interface{}{
				"session_id":  sessionID,
				"gate_reason": "contradiction_detected",
			},
		})
	}
	if confidence < 0.70 {
		recs = append(recs, map[string]interface{}{
			"tool":   "evaluate-js",
			"reason": "Low confidence reasoning result; permit targeted JS fallback.",
			"args": map[string]interface{}{
				"session_id":  sessionID,
				"gate_reason": "low_confidence",
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
				"tool": "await-stable-state",
				"args": map[string]interface{}{"timeout_ms": 10000},
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
			"tool": "screenshot",
			"args": map[string]interface{}{
				"session_id": sessionID,
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
