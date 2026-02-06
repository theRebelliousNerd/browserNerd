package mcp

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/mangle"
)

const (
	defaultProgressiveMaxItems = 20
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
		},
		"required": []string{"session_id"},
	}
}

func (t *BrowserObserveTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}
	mode := getStringArg(args, "mode")
	if mode == "" {
		mode = "composite"
	}
	view := normalizeProgressiveView(getStringArg(args, "view"))
	maxItems := getIntArg(args, "max_items", defaultProgressiveMaxItems)
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}

	filter := getStringArg(args, "filter")
	if filter == "" {
		filter = "all"
	}
	visibleOnly := getBoolArg(args, "visible_only", true)
	internalOnly := getBoolArg(args, "internal_only", false)
	emitFacts := getBoolArg(args, "emit_facts", true)

	stateTool := &GetPageStateTool{sessions: t.sessions}
	navTool := &GetNavigationLinksTool{sessions: t.sessions, engine: t.engine}
	interactiveTool := &GetInteractiveElementsTool{sessions: t.sessions, engine: t.engine}
	hiddenTool := &DiscoverHiddenContentTool{sessions: t.sessions}

	stateData := map[string]interface{}{}
	navData := map[string]interface{}{}
	interactiveData := map[string]interface{}{}
	hiddenData := map[string]interface{}{}

	fetchState := mode == "state" || mode == "composite"
	fetchNav := mode == "nav" || mode == "composite"
	fetchInteractive := mode == "interactive" || mode == "composite"
	fetchHidden := mode == "hidden" || (mode == "composite" && view == "full")

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
	}

	summary := buildObserveSummary(data)
	emitDisclosureFacts(ctx, t.engine, sessionID, handles, "observe")

	return map[string]interface{}{
		"success":          true,
		"status":           "ok",
		"mode":             mode,
		"view":             view,
		"summary":          summary,
		"data":             data,
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

	topic := getStringArg(args, "topic")
	if topic == "" {
		topic = "health"
	}
	view := normalizeProgressiveView(getStringArg(args, "view"))
	maxItems := getIntArg(args, "max_items", defaultProgressiveMaxItems)
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}

	rootCauses := queryToRows(ctx, t.engine, "root_cause(ConsoleMsg, Source, Cause).")
	failedReqs := queryToRows(ctx, t.engine, "failed_request(ReqId, Url, Status).")
	slowApis := queryToRows(ctx, t.engine, "slow_api(ReqId, Url, Duration).")
	blockingIssues := queryToRows(ctx, t.engine, "interaction_blocked(SessionId, Reason).")
	userVisibleErrors := queryToRows(ctx, t.engine, "user_visible_error(Source, Message, Timestamp).")

	contradictions := detectContradictions(t.engine)

	confidence := computeReasonConfidence(len(rootCauses), len(failedReqs), len(slowApis), len(contradictions), topic)
	status := "ok"
	if len(failedReqs) > 0 || len(contradictions) > 0 || len(rootCauses) > 0 {
		status = "error"
	} else if len(slowApis) > 0 || len(blockingIssues) > 0 {
		status = "warning"
	}

	recommendations := recommendNextActions(topic, status, len(failedReqs), len(rootCauses), len(contradictions), confidence)

	data := map[string]interface{}{
		"failed_requests":     failedReqs,
		"root_causes":         rootCauses,
		"slow_apis":           slowApis,
		"blocking_issues":     blockingIssues,
		"user_visible_errors": userVisibleErrors,
		"contradictions":      contradictions,
		"recommendations":     recommendations,
	}

	handles := []string{
		"reason:" + sessionID + ":failed_requests",
		"reason:" + sessionID + ":root_causes",
		"reason:" + sessionID + ":slow_apis",
		"reason:" + sessionID + ":blocking_issues",
		"reason:" + sessionID + ":contradictions",
		"reason:" + sessionID + ":recommendations",
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
		"topic":               topic,
		"status":              status,
		"confidence":          confidence,
		"summary":             buildReasonSummary(status, confidence, len(rootCauses), len(failedReqs), len(slowApis), len(contradictions)),
		"evidence_handles":    handles,
		"expansion_suggested": confidence < 0.70 || len(contradictions) > 0,
		"view":                view,
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

func buildObserveSummary(data map[string]interface{}) string {
	parts := make([]string, 0, 4)
	if state, ok := data["state"].(map[string]interface{}); ok {
		if loading, exists := state["loading"].(bool); exists {
			parts = append(parts, fmt.Sprintf("loading=%t", loading))
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

func detectContradictions(engine *mangle.Engine) []map[string]interface{} {
	contradictions := make([]map[string]interface{}, 0)
	failed := engine.FactsByPredicate("failed_request")
	toasts := engine.FactsByPredicate("toast_notification")
	if len(failed) == 0 || len(toasts) == 0 {
		return contradictions
	}

	successToastCount := 0
	for _, t := range toasts {
		if len(t.Args) < 2 {
			continue
		}
		level := fmt.Sprintf("%v", t.Args[1])
		if level == "success" {
			successToastCount++
		}
	}

	if successToastCount > 0 {
		contradictions = append(contradictions, map[string]interface{}{
			"type":                    "success_toast_with_failed_requests",
			"failed_request_count":    len(failed),
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

func recommendNextActions(topic, status string, failedReqs, rootCauses, contradictions int, confidence float64) []map[string]interface{} {
	_ = topic
	recs := make([]map[string]interface{}, 0, 3)
	if status == "ok" {
		recs = append(recs, map[string]interface{}{
			"tool":   "browser-observe",
			"reason": "No critical issues detected; continue with focused observation.",
			"args": map[string]interface{}{
				"mode": "interactive",
				"view": "compact",
			},
		})
	}
	if failedReqs > 0 || rootCauses > 0 {
		recs = append(recs, map[string]interface{}{
			"tool":   "browser-reason",
			"reason": "Expand failure evidence for targeted remediation.",
			"args": map[string]interface{}{
				"topic": "why_failed",
				"view":  "full",
			},
		})
	}
	if contradictions > 0 {
		recs = append(recs, map[string]interface{}{
			"tool":   "evaluate-js",
			"reason": "Contradiction detected; JS inspection is now permitted.",
			"args": map[string]interface{}{
				"gate_reason": "contradiction_detected",
			},
		})
	}
	if confidence < 0.70 {
		recs = append(recs, map[string]interface{}{
			"tool":   "evaluate-js",
			"reason": "Low confidence reasoning result; permit targeted JS fallback.",
			"args": map[string]interface{}{
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
		case strings.Contains(handle, "recommendations"):
			selected["recommendations"] = true
		case strings.Contains(handle, "user_visible_errors"):
			selected["user_visible_errors"] = true
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

func ternaryStatus(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
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
