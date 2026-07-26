package mcp

import (
	"context"
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"browsernerd-mcp-server/internal/mangle"
)

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
		"browser-audit",
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
	return filterRowsSinceScoped(rows, timestampFields, sinceMs, true)
}

func filterRowsSinceScoped(
	rows []map[string]interface{},
	timestampFields []string,
	sinceMs int64,
	requireTimestamp bool,
) []map[string]interface{} {
	if sinceMs <= 0 || len(rows) == 0 {
		return rows
	}
	filtered := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		ts, hasTimestamp := rowTimestampMs(row, timestampFields)
		if !hasTimestamp && !requireTimestamp {
			filtered = append(filtered, row)
			continue
		}
		if hasTimestamp && ts >= sinceMs {
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
