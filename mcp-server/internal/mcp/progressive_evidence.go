package mcp

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/mangle"
)

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

func detectContradictions(evidence *browserEvidence) []map[string]interface{} {
	contradictions := make([]map[string]interface{}, 0)
	if evidence == nil {
		return contradictions
	}

	failedRequestCount := 0
	for _, request := range evidence.calls {
		if request != nil && request.failed() {
			failedRequestCount++
		}
	}
	if failedRequestCount == 0 {
		return contradictions
	}

	successToastCount := 0
	for _, toast := range evidence.toasts {
		if toast.level == "success" {
			successToastCount++
		}
	}

	if successToastCount > 0 {
		contradictions = append(contradictions, map[string]interface{}{
			"type":                    "success_toast_with_failed_requests",
			"failed_request_count":    failedRequestCount,
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
