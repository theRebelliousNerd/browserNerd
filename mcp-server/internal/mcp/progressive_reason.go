package mcp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/logs"
	"browsernerd-mcp-server/internal/mangle"
)

// BrowserReasonTool turns what the browser recorded into a diagnosis: a
// one-line verdict, the causal chain behind it, and one pointer to the next
// call worth making.
type BrowserReasonTool struct {
	engine      *mangle.Engine
	sessions    *browser.SessionManager // the tab whose response bodies network+body reads
	backendLogs logs.Source
	backend     backendCorrelator
}

var reasonTopics = []string{"why_failed", "health", "network", "blocking_issue", "next_best_action", "what_changed_since"}

func (t *BrowserReasonTool) Name() string { return "browser-reason" }
func (t *BrowserReasonTool) Description() string {
	return `Diagnose the page from what the browser recorded. Pick the topic by the question:
  why_failed: what broke, or what did my click break -> verdict, the failed request (status, body), the console error/toast/live-region error it caused, backend log lines, other issues
  health: is anything wrong -> scorecard (failed 5xx/4xx/network, console errors, storms, ws failures, dialogs, page load failures) and the top issue
  network: is something looping, which calls fail -> the page on screen's requests grouped by method+URL (data calls first, healthy chunks/fonts folded into assets), counts, statuses, storms, latest status of a failing group; earlier counts what came before the navigation (since_navigation=false includes it); expand=<url> lists that URL's calls; add body=true to see what the latest ones returned (JSON shape: items: array[0] means it answered empty)
  blocking_issue: why is the page empty or stuck -> load failure, dismissed dialog, overlay, login wall, failed data call
  next_best_action: what can I do here -> deduped action candidates
  what_changed_since: issues inside time_window_ms, newest first
Scope: since_action=true (since your last click/input), since_navigation=true (current route), time_window_ms (default 300000). Results name the effective latest bound; window-only diagnoses include session history. view: summary|compact(default)|full. next names the one call worth making next.`
}

func (t *BrowserReasonTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{"type": "string"},
			"topic": map[string]interface{}{
				"type":        "string",
				"description": "Question to answer (default health)",
				"enum":        reasonTopics,
			},
			"view": map[string]interface{}{
				"type": "string",
				"enum": []string{"summary", "compact", "full"},
			},
			"intent": map[string]interface{}{
				"type":        "string",
				"description": "Preset: triage=health, act_now=next_best_action, debug_failure=why_failed full, unblock=blocking_issue",
				"enum":        []string{"triage", "act_now", "debug_failure", "unblock"},
			},
			"since_action": map[string]interface{}{
				"type":        "boolean",
				"description": "Only evidence since your last click, input, key or submit",
			},
			"since_navigation": map[string]interface{}{
				"type":        "boolean",
				"description": "Only evidence since the latest navigation (default true for network, false otherwise)",
			},
			"time_window_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Only evidence newer than now minus this (default 300000; 0 = all)",
			},
			"expand": map[string]interface{}{
				"type":        "string",
				"description": "network: list the individual calls of groups whose URL contains this",
			},
			"body": map[string]interface{}{
				"type":        "boolean",
				"description": "network with expand: fetch the latest matching responses (up to 3) and return each body's shape",
			},
			"expand_handles": map[string]interface{}{
				"type":        "array",
				"description": "Evidence sections to return in data, e.g. reason:console",
				"items":       map[string]interface{}{"type": "string"},
			},
			"max_items": map[string]interface{}{
				"type":        "integer",
				"description": "Max rows per section (default 20)",
			},
			"include_action_plan": map[string]interface{}{
				"type":        "boolean",
				"description": "next_best_action: include ready browser-act operations (default true)",
			},
			"max_recommendations": map[string]interface{}{
				"type":        "integer",
				"description": "next_best_action: max actions (default 4)",
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
	topic := strings.ToLower(strings.TrimSpace(getStringArg(args, "topic")))
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
	known := false
	for _, name := range reasonTopics {
		if topic == name {
			known = true
		}
	}
	if !known {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("unknown topic %q; use one of %s", topic, strings.Join(reasonTopics, ", "))}, nil
	}
	maxItems := getIntArg(args, "max_items", defaultProgressiveMaxItems)
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}
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
	// The network topic answers for the page on screen unless told otherwise:
	// requests from before the latest navigation (the 502s of a backend
	// restart minutes ago) are another page's traffic.
	sinceNavigation := getBoolArg(args, "since_navigation", topic == "network")
	sinceAction := getBoolArg(args, "since_action", false)
	_, hasExplicitTimeWindow := args["time_window_ms"]

	effectiveSinceMs := int64(0)
	if timeWindowMs > 0 {
		effectiveSinceMs = time.Now().UnixMilli() - int64(timeWindowMs)
	}
	windowSinceMs := effectiveSinceMs
	pageStartMs := int64(0)
	if sinceNavigation || topic == "network" {
		pageStartMs = currentPageStartMs(t.engine, sessionID)
	}
	navigationSinceMs := int64(0)
	if sinceNavigation {
		navigationSinceMs = pageStartMs
		if navigationSinceMs > effectiveSinceMs {
			effectiveSinceMs = navigationSinceMs
		}
	}
	actionSinceMs := int64(0)
	if sinceAction {
		if ts := latestActionMs(t.engine, sessionID); ts > 0 {
			actionSinceMs = ts - actionScopeSlackMs
			if actionSinceMs > effectiveSinceMs {
				effectiveSinceMs = actionSinceMs
			}
		}
	}
	scope := reasonScope{
		sessionID: sessionID,
		sinceMs:   effectiveSinceMs,
		strict:    sinceNavigation || sinceAction || hasExplicitTimeWindow || topic == "what_changed_since",
	}
	label := reasonScopeLabel(effectiveSinceMs, actionSinceMs, navigationSinceMs, timeWindowMs)

	ev := collectBrowserEvidence(ctx, t.engine, scope)
	d := diagnose(ev)

	var response map[string]interface{}
	switch topic {
	case "network":
		response = renderNetwork(d, view, getStringArg(args, "expand"), maxItems, pageStartMs)
		if sinceNavigation && navigationSinceMs > windowSinceMs {
			wide := collectBrowserEvidence(ctx, t.engine, reasonScope{sessionID: sessionID, sinceMs: windowSinceMs, strict: hasExplicitTimeWindow})
			if earlier := trafficBefore(wide.calls, navigationSinceMs); earlier != nil {
				response["earlier"] = earlier
			}
		}
		if getBoolArg(args, "body", false) && getStringArg(args, "expand") != "" {
			attachBodies(ctx, t.sessions, sessionID, response)
		}
	case "health":
		response = renderHealth(d, view, label)
	case "blocking_issue":
		response = renderBlocking(d, view, label, maxItems)
	case "next_best_action":
		response = t.renderNextBestAction(ctx, d, view, label, sessionID, getBoolArg(args, "include_action_plan", true), maxRecommendations)
	case "what_changed_since":
		response = renderChanges(d, label, maxItems)
	default:
		response = t.renderWhyFailed(ctx, d, view, label, maxItems)
	}
	response["success"] = true
	response["topic"] = topic
	response["scope"] = label
	response["scope_since_ms"] = effectiveSinceMs
	if hasIntent {
		response["intent"] = intent
	}
	if sinceNavigation {
		response["navigation_since_ms"] = navigationSinceMs
	}
	if sinceAction {
		response["action_since_ms"] = actionSinceMs
	}

	causal := len(ev.backendRootCauses)
	failed := 0
	for _, fg := range d.failures {
		failed += len(fg.calls)
		if len(fg.effects) > 0 {
			causal++
		}
	}
	confidence := computeReasonConfidence(causal, failed, d.score.slow, len(ev.contradictions), topic)
	if topic != "network" {
		response["confidence"] = math.Round(confidence*100) / 100
	}
	if confidence < 0.70 || len(ev.contradictions) > 0 {
		response["expansion_suggested"] = true
	}

	handles := make([]string, 0, 8)
	for _, name := range reasonEvidenceSections {
		if d.section(name, 1) != nil {
			handles = append(handles, "reason:"+name)
		}
	}
	if view != "summary" && len(handles) > 0 {
		response["evidence_handles"] = handles
	}
	if selected := selectedReasonSections(args["expand_handles"]); len(selected) > 0 {
		data := make(map[string]interface{}, len(selected))
		for _, name := range selected {
			if name == "backend_logs" {
				if logs, ok := response["backend_logs"]; ok {
					data[name] = logs
				}
				continue
			}
			if rows := d.section(name, maxItems); rows != nil {
				data[name] = rows
			}
		}
		response["data"] = data
	}

	now := time.Now()
	grantReasons := make([]string, 0, 3)
	emitFacts := []mangle.Fact{{
		Predicate: "confidence_score",
		Args:      []interface{}{sessionID, topic, int(math.Round(confidence * 100.0)), now.UnixMilli()},
		Timestamp: now,
	}}
	if confidence < 0.70 {
		grantReasons = append(grantReasons, "low_confidence")
		emitFacts = append(emitFacts, mangle.Fact{Predicate: "js_gate_open", Args: []interface{}{sessionID, "low_confidence", now.UnixMilli()}, Timestamp: now})
	}
	if len(ev.contradictions) > 0 {
		grantReasons = append(grantReasons, "contradiction_detected")
		emitFacts = append(emitFacts, mangle.Fact{Predicate: "js_gate_open", Args: []interface{}{sessionID, "contradiction_detected", now.UnixMilli()}, Timestamp: now})
	}
	if response["next"] == nil {
		grantReasons = append(grantReasons, "no_matching_tool")
		emitFacts = append(emitFacts, mangle.Fact{Predicate: "js_gate_open", Args: []interface{}{sessionID, "no_matching_tool", now.UnixMilli()}, Timestamp: now})
	}
	expiry, issueErr := t.engine.PublishReasonGrants(ctx, sessionID, handles, grantReasons)
	response["gate_grants"] = reasonAuthorizationReceipt(expiry, issueErr, grantReasons)
	// Facts are observation evidence only; trims cannot revoke issued control state.
	_ = t.engine.AddFacts(ctx, emitFacts)
	emitDisclosureFacts(ctx, t.engine, sessionID, handles, "reason")
	return response, nil
}

func reasonAuthorizationReceipt(expiry time.Time, issueErr error, reasons []string) map[string]interface{} {
	if issueErr != nil {
		code := "authorization_publication_failed"
		switch {
		case errors.Is(issueErr, mangle.ErrGateGrantCapacity):
			code = "capacity_exhausted"
		case errors.Is(issueErr, mangle.ErrGateGrantEmpty):
			code = "no_eligible_grants"
		case errors.Is(issueErr, mangle.ErrGateGrantUnavailable):
			code = "authority_unavailable"
		case errors.Is(issueErr, context.Canceled), errors.Is(issueErr, context.DeadlineExceeded):
			code = "publication_cancelled"
		case errors.Is(issueErr, mangle.ErrGateGrantInvalid):
			code = "invalid_bundle"
		}
		return map[string]interface{}{"available": false, "reason": code}
	}
	receipt := map[string]interface{}{"available": true, "expires_at_ms": expiry.UnixMilli()}
	if len(reasons) > 0 {
		receipt["reasons"] = reasons
	}
	return receipt
}

// trafficBefore summarizes the calls a navigation-scoped read left out: how
// many started before startMs and how many of those failed.
func trafficBefore(calls []*netCall, startMs int64) map[string]interface{} {
	requests, failed := 0, 0
	for _, c := range calls {
		if c.startMs <= 0 || c.startMs >= startMs {
			continue
		}
		requests++
		if c.diagnosticFailure() {
			failed++
		}
	}
	if requests == 0 {
		return nil
	}
	return map[string]interface{}{
		"requests": requests,
		"failed":   failed,
		"note":     "requests from before the page on screen; since_navigation=false includes them",
	}
}

// summaryLine is the one-line count header: the status and every non-zero count.
func (d *diagnosis) summaryLine() string {
	parts := []string{"status=" + d.status}
	for _, kv := range []struct {
		k string
		v int
	}{
		{"page_load_failed", d.score.pageLoadFailed}, {"failed_5xx", d.score.failed5xx}, {"failed_4xx", d.score.failed4xx},
		{"failed_network", d.score.failedNetwork}, {"console_errors", d.score.consoleErrors}, {"toast_errors", d.score.toastErrors},
		{"live_region_errors", d.score.liveRegionErrors},
		{"dialogs", d.score.dialogs}, {"storms", d.score.storms}, {"ws_failures", d.score.wsFailures}, {"slow", d.score.slow},
		{"blocking", d.score.blocking},
	} {
		if kv.v > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", kv.k, kv.v))
		}
	}
	return strings.Join(parts, " ")
}

func (d *diagnosis) nextFor(is *reasonIssue) interface{} {
	if is != nil {
		if is.next != nil {
			return is.next
		}
		return nil
	}
	return map[string]interface{}{"tool": "browser-observe"}
}

// renderWhyFailed is the diagnosis: the verdict, the root cause behind it with
// its evidence, the backend log lines for the leading failed request, and
// every other issue as one line.
// correlationTarget names the URL and moment backend logs are asked about
// when the primary issue is a page that failed to load or a WebSocket that
// failed, not a failed request.
func correlationTarget(is *reasonIssue) *netCall {
	if is == nil || is.detail == nil {
		return nil
	}
	url := ""
	switch is.kind {
	case "page_load_failed":
		url, _ = is.detail["url"].(string)
	case "websocket":
		if row, ok := is.detail["websocket"].(map[string]interface{}); ok {
			url, _ = row["url"].(string)
		}
	}
	if url == "" {
		return nil
	}
	return &netCall{url: url, startMs: is.atMs, endMs: is.atMs}
}

func (t *BrowserReasonTool) renderWhyFailed(ctx context.Context, d *diagnosis, view, label string, maxItems int) map[string]interface{} {
	out := map[string]interface{}{
		"status":  d.status,
		"verdict": d.verdict(label),
		"summary": d.summaryLine(),
	}
	primary := d.primary()
	if next := d.nextFor(primary); next != nil {
		out["next"] = next
	}
	if view == "summary" {
		return out
	}

	var failureIssue *reasonIssue
	for _, is := range d.issues {
		if is.failure != nil {
			failureIssue = is
			break
		}
	}
	var backendLogs interface{}
	if failureIssue != nil {
		if src := t.backendSource(); src != nil {
			backendLogs = t.backend.linesFor(ctx, src, failureIssue.failure.latest())
		}
	} else if target := correlationTarget(primary); target != nil {
		// No request failed, but the page did not load or its WebSocket did
		// not connect: every browser signal can read clean while the truth sits
		// in the server log, so ask it about that URL at that moment.
		if src := t.backendSource(); src != nil {
			backendLogs = t.backend.linesFor(ctx, src, target)
			failureIssue = primary
		}
	}

	if primary != nil && primary.detail != nil {
		rootCause := make(map[string]interface{}, len(primary.detail)+1)
		for k, v := range primary.detail {
			rootCause[k] = v
		}
		if backendLogs != nil && failureIssue == primary {
			rootCause["backend_logs"] = backendLogs
		}
		out["root_cause"] = rootCause
	}
	if backendLogs != nil && failureIssue != primary {
		out["backend_logs"] = map[string]interface{}{"for": failureIssue.line, "lines": backendLogs}
	}

	maxLines := minInt(maxItems, 6)
	if view == "full" {
		maxLines = maxItems
	}
	lines, more := d.issueLines(maxLines, view == "full")
	if len(lines) > 0 {
		out["issues"] = lines
	}
	if more > 0 {
		out["more_issues"] = more
	}
	if view == "full" {
		data := make(map[string]interface{})
		for _, name := range reasonEvidenceSections {
			if rows := d.section(name, maxItems); rows != nil {
				data[name] = rows
			}
		}
		if len(data) > 0 {
			out["data"] = data
		}
	}
	return out
}

// renderHealth is the scorecard: every count, the top issue as the verdict,
// and the loudest console messages.
func renderHealth(d *diagnosis, view, label string) map[string]interface{} {
	out := map[string]interface{}{
		"status":  d.status,
		"verdict": d.verdict(label),
		"summary": d.summaryLine(),
	}
	if next := d.nextFor(d.primary()); next != nil {
		out["next"] = next
	}
	if view == "summary" {
		return out
	}
	out["scorecard"] = d.score.row()
	lines := make([]string, 0, 3)
	for _, g := range d.console {
		if len(lines) == 3 {
			break
		}
		lines = append(lines, g.line())
	}
	if len(lines) > 0 {
		out["console"] = lines
	}
	if view == "full" {
		out["issues"] = d.issueRows()
	}
	return out
}

// renderBlocking answers "why is the page empty or stuck": the issues that can
// hold a page back, worst first.
func renderBlocking(d *diagnosis, view, label string, maxItems int) map[string]interface{} {
	blockers := make([]*reasonIssue, 0)
	for _, is := range d.issues {
		if is.blocking != "" {
			blockers = append(blockers, is)
		}
	}
	out := map[string]interface{}{"status": d.status, "summary": d.summaryLine()}
	if len(blockers) == 0 {
		out["verdict"] = "nothing blocking recorded " + label
		if p := d.primary(); p != nil {
			out["top_issue"] = p.line
		}
		return out
	}
	out["verdict"] = scopedIssueVerdict(label, blockers[0].line)
	if blockers[0].next != nil {
		out["next"] = blockers[0].next
	}
	if view == "summary" {
		return out
	}
	if blockers[0].detail != nil {
		out["root_cause"] = blockers[0].detail
	}
	rest := make([]string, 0)
	for _, is := range blockers[1:] {
		if len(rest) >= minInt(maxItems, 6) && view != "full" {
			break
		}
		rest = append(rest, is.line)
	}
	if len(rest) > 0 {
		out["blocking"] = rest
	}
	return out
}

// renderNextBestAction lists the page's action candidates with repeats folded
// (400 identical buttons are one candidate with a count), led by the error to
// fix when there is one.
func (t *BrowserReasonTool) renderNextBestAction(ctx context.Context, d *diagnosis, view, label, sessionID string, includePlan bool, maxActions int) map[string]interface{} {
	out := map[string]interface{}{
		"status":  d.status,
		"verdict": d.verdict(label),
		"summary": d.summaryLine(),
	}
	candidates := queryActionCandidates(ctx, t.engine, sessionID, maxActions)
	if len(candidates) > maxActions {
		candidates = candidates[:maxActions]
	}
	actions := make([]map[string]interface{}, 0, len(candidates))
	if includePlan {
		for _, cand := range candidates {
			recs := buildActionPlanRecommendations([]map[string]interface{}{cand}, 1, sessionID, d.ev.origin)
			if len(recs) == 0 {
				continue
			}
			rec := recs[0]
			recArgs, _ := rec["args"].(map[string]interface{})
			row := map[string]interface{}{"label": cand["label"], "operations": recArgs["operations"]}
			if n := asInt(cand["count"]); n > 1 {
				row["count"] = n
			}
			if needs, _ := rec["requires_user_input"].(bool); needs {
				row["requires_user_input"] = true
			}
			actions = append(actions, row)
		}
	} else {
		for _, c := range candidates {
			row := map[string]interface{}{"action": c["action"], "ref": c["ref"], "label": c["label"]}
			if n := asInt(c["count"]); n > 1 {
				row["count"] = n
			}
			actions = append(actions, row)
		}
	}
	if view != "summary" && len(actions) > 0 {
		out["actions"] = actions
	}
	if p := d.primary(); p != nil && p.level == "error" && p.next != nil {
		out["next"] = p.next
	} else if includePlan && len(actions) > 0 {
		out["next"] = map[string]interface{}{"tool": "browser-act", "args": map[string]interface{}{"operations": actions[0]["operations"]}}
	}
	return out
}

// renderChanges is every issue inside the window, newest first.
func renderChanges(d *diagnosis, label string, maxItems int) map[string]interface{} {
	rows := d.issueRows()
	sort.SliceStable(rows, func(i, j int) bool { return asInt64(rows[i]["at_ms"]) > asInt64(rows[j]["at_ms"]) })
	out := map[string]interface{}{
		"status":  d.status,
		"verdict": d.verdict(label),
		"summary": d.summaryLine(),
		"changes": limitMapSlice(rows, maxItems),
	}
	if next := d.nextFor(d.primary()); next != nil {
		out["next"] = next
	}
	return out
}

// reasonEvidenceSections are the expandable evidence sections, in the order
// handles and full data list them.
var reasonEvidenceSections = []string{
	"page_load_failures", "failed_requests", "console", "toasts", "live_regions", "dialogs", "websockets",
	"storms", "slow_apis", "root_causes", "blocking_issues", "contradictions", "network",
}

// section returns one evidence section's rows, or nil when it is empty.
func (d *diagnosis) section(name string, maxItems int) interface{} {
	origin := d.ev.origin
	var rows []map[string]interface{}
	switch name {
	case "failed_requests":
		for _, fg := range d.failures {
			for _, c := range fg.calls {
				rows = append(rows, callRow(c, origin))
			}
		}
		sort.SliceStable(rows, func(i, j int) bool { return asInt64(rows[i]["at_ms"]) > asInt64(rows[j]["at_ms"]) })
	case "console":
		for _, g := range d.console {
			rows = append(rows, g.row())
		}
	case "toasts":
		for _, g := range d.toasts {
			rows = append(rows, g.row())
		}
	case "live_regions":
		for _, r := range d.ev.liveRegions {
			rows = append(rows, map[string]interface{}{"role": r.role, "level": r.level, "message": clipText(r.text, 240), "at_ms": r.ts})
		}
	case "dialogs":
		for _, dl := range d.ev.dialogs {
			rows = append(rows, map[string]interface{}{"type": dl.kind, "message": clipText(dl.message, 160), "accepted": dl.accepted, "handled_by": dl.handledBy, "at_ms": dl.ts})
		}
	case "websockets":
		for _, g := range d.wsGroups {
			rows = append(rows, g.row())
		}
	case "page_load_failures":
		for _, lf := range d.ev.loadFailures {
			rows = append(rows, map[string]interface{}{"url": lf.url, "error": lf.errorText, "at_ms": lf.ts})
		}
	case "storms":
		for _, g := range d.groups {
			if g.storm != nil {
				rows = append(rows, g.row())
			}
		}
	case "slow_apis":
		for _, c := range d.ev.calls {
			if c.durationMs > slowRequestMs && isAPIRequest(c.url) {
				rows = append(rows, callRow(c, origin))
			}
		}
	case "network":
		for _, g := range d.groups {
			rows = append(rows, g.row())
		}
	case "root_causes":
		rows = d.ev.backendRootCauses
	case "blocking_issues":
		rows = d.ev.blocking
	case "contradictions":
		rows = d.ev.contradictions
	case "issues":
		rows = d.issueRows()
	}
	if len(rows) == 0 {
		return nil
	}
	return limitMapSlice(rows, maxItems)
}

// selectedReasonSections maps expand_handles (full handles or bare section
// names) to section names.
func selectedReasonSections(raw interface{}) []string {
	handles := toStringSlice(raw)
	if len(handles) == 0 {
		return nil
	}
	all := append(append([]string{}, reasonEvidenceSections...), "issues", "backend_logs")
	out := make([]string, 0, len(handles))
	seen := make(map[string]bool)
	for _, h := range handles {
		h = strings.ToLower(strings.TrimSpace(h))
		for _, name := range all {
			if !seen[name] && (h == name || strings.HasSuffix(h, ":"+name)) {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}
