package mcp

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/mangle"
)

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
	_, hasExplicitTimeWindow := args["time_window_ms"]
	strictTimeScope := sinceNavigation || hasExplicitTimeWindow || topic == "what_changed_since"
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
		rootCauses = filterRowsSinceScoped(rootCauses, []string{"Ts", "Timestamp"}, effectiveSinceMs, strictTimeScope)
		failedReqs = filterRowsSinceScoped(failedReqs, []string{"ReqTs", "Timestamp"}, effectiveSinceMs, strictTimeScope)
		slowApis = filterRowsSinceScoped(slowApis, []string{"ReqTs", "Timestamp"}, effectiveSinceMs, strictTimeScope)
		userVisibleErrors = filterRowsSinceScoped(userVisibleErrors, []string{"Timestamp", "Ts"}, effectiveSinceMs, strictTimeScope)
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
