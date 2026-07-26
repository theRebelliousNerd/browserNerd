package mcp

import (
	"fmt"
	"strings"

	"browsernerd-mcp-server/internal/browser"
)

func normalizeAuditPhase(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "discover", "execute", "report", "resume":
		return strings.ToLower(strings.TrimSpace(phase))
	default:
		return "report"
	}
}

func buildAuditDiscoverSummary(pageContext, factSummary map[string]interface{}, planSteps, hazardCount int) string {
	route := strings.TrimSpace(fmt.Sprintf("%v", pageContext["route_path"]))
	if route == "" {
		route = strings.TrimSpace(fmt.Sprintf("%v", pageContext["current_url"]))
	}
	if route == "" {
		route = "unknown_route"
	}
	summary := fmt.Sprintf(
		"phase=discover route=%s plan_steps=%d hazards=%d failed_requests=%d search_specs=%d",
		route,
		planSteps,
		hazardCount,
		asInt(factSummary["failed_requests"]),
		asInt(factSummary["search_specs"]),
	)
	if traceSteps := asInt(factSummary["repo_trace_plan_steps"]); traceSteps > 0 {
		summary += fmt.Sprintf(" trace_steps=%d", traceSteps)
	}
	return summary
}

func buildAuditRepoTraceDiscoverData(report *browser.RepoTraceReport) map[string]interface{} {
	if report == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"trace_id": report.TraceID,
		"browser_context": map[string]interface{}{
			"current_url":      report.BrowserContext.CurrentURL,
			"current_path":     report.BrowserContext.CurrentPath,
			"title":            report.BrowserContext.Title,
			"headings":         report.BrowserContext.Headings,
			"navigation_links": report.BrowserContext.NavigationLinks,
			"controls":         report.BrowserContext.Controls,
			"forms":            report.BrowserContext.Forms,
			"requests":         report.BrowserContext.Requests,
		},
		"hazard_summary":   report.HazardSummary,
		"audit_plan":       report.AuditPlan,
		"frontend_sites":   report.FrontendSites,
		"backend_matches":  report.BackendMatches,
		"correlations":     report.Correlations,
		"warnings":         report.Warnings,
		"stats":            report.Stats,
		"evidence_handles": report.EvidenceHandles,
	}
}

func buildAuditDiscoverHandles(sessionID string, includeRepoTrace bool) []string {
	handles := []string{
		"audit:" + sessionID + ":page_context",
		"audit:" + sessionID + ":fact_summary",
		"audit:" + sessionID + ":search_specs",
		"audit:" + sessionID + ":audit_plan",
		"audit:" + sessionID + ":audit_hazards",
	}
	if includeRepoTrace {
		handles = append(handles, "audit:"+sessionID+":repo_trace")
	}
	return handles
}

func buildAuditReportHandles(sessionID string, includeRepoMatches, includeRepoTrace bool) []string {
	handles := append([]string{}, buildAuditDiscoverHandles(sessionID, includeRepoTrace)...)
	handles = append(handles,
		"audit:"+sessionID+":contract_findings",
		"audit:"+sessionID+":failed_requests",
		"audit:"+sessionID+":api_correlations",
		"audit:"+sessionID+":full_stack_errors",
		"audit:"+sessionID+":user_visible_errors",
		"audit:"+sessionID+":mangle_contracts",
	)
	if includeRepoMatches {
		handles = append(handles, "audit:"+sessionID+":repo_matches")
	}
	if includeRepoTrace {
		handles = append(handles, "audit:"+sessionID+":repo_trace")
	}
	return handles
}

func resolveAuditResumeHandles(args map[string]interface{}, sessionID string, includeRepoMatches, includeRepoTrace bool) []string {
	handles := toStringSlice(args["expand_handles"])
	resumeHandle := strings.TrimSpace(getStringArg(args, "resume_handle"))
	if resumeHandle != "" {
		found := false
		for _, handle := range handles {
			if handle == resumeHandle {
				found = true
				break
			}
		}
		if !found {
			handles = append([]string{resumeHandle}, handles...)
		}
	}
	if len(handles) > 0 {
		return handles
	}

	fallback := []string{
		"audit:" + sessionID + ":page_context",
		"audit:" + sessionID + ":fact_summary",
		"audit:" + sessionID + ":audit_plan",
		"audit:" + sessionID + ":audit_hazards",
		"audit:" + sessionID + ":contract_findings",
		"audit:" + sessionID + ":failed_requests",
		"audit:" + sessionID + ":mangle_contracts",
	}
	if includeRepoMatches {
		fallback = append(fallback, "audit:"+sessionID+":repo_matches")
	}
	if includeRepoTrace {
		fallback = append(fallback, "audit:"+sessionID+":repo_trace")
	}
	return fallback
}

func auditHazardsRequireApproval(hazards []map[string]interface{}) bool {
	for _, hazard := range hazards {
		severity := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", hazard["severity"])))
		if severity == "medium" || severity == "high" || severity == "critical" {
			return true
		}
		if risky, ok := hazard["risky"].(bool); ok && risky {
			return true
		}
	}
	return false
}

func auditSearchSpecsToRows(specs []auditRepoSearchSpec) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(specs))
	for _, spec := range specs {
		rows = append(rows, map[string]interface{}{
			"key":       spec.key,
			"kind":      spec.kind,
			"literal":   spec.literal,
			"pattern":   spec.display,
			"path_hint": spec.pathHint,
		})
	}
	return rows
}

func appendAuditPlanStep(plan []map[string]interface{}, step map[string]interface{}) []map[string]interface{} {
	if step == nil {
		return plan
	}
	step["step"] = len(plan) + 1
	plan = append(plan, step)
	return plan
}

func appendAuditFailedRequestPlanSteps(plan []map[string]interface{}, sessionID, currentPath string, failedRequests []map[string]interface{}) []map[string]interface{} {
	seen := make(map[string]bool)
	for idx, request := range failedRequests {
		if idx >= 3 {
			break
		}
		reqID := strings.TrimSpace(fmt.Sprintf("%v", request["ReqId"]))
		method := strings.TrimSpace(fmt.Sprintf("%v", request["Method"]))
		route := canonicalAuditPath(strings.TrimSpace(fmt.Sprintf("%v", request["Path"])))
		status := asInt(request["Status"])
		if route == "" {
			route = canonicalAuditPath(strings.TrimSpace(fmt.Sprintf("%v", request["Url"])))
		}
		if reqID == "" || route == "" {
			continue
		}
		key := method + "|" + route + "|" + reqID
		if seen[key] {
			continue
		}
		seen[key] = true
		action := "reveal_contract_diff"
		if status == 401 || status == 403 {
			action = "reveal_request_auth"
		} else if strings.EqualFold(method, "POST") || strings.EqualFold(method, "PUT") || strings.EqualFold(method, "PATCH") || strings.EqualFold(method, "DELETE") {
			action = "reveal_request_payload"
		}
		plan = appendAuditPlanStep(plan, map[string]interface{}{
			"id":         "request-" + reqID,
			"title":      fmt.Sprintf("Inspect failing %s %s request", strings.ToUpper(firstNonEmpty(method, "REQUEST")), route),
			"tool":       "browser-mangle",
			"action":     action,
			"ref":        reqID,
			"label":      route,
			"route":      currentPath,
			"api_route":  route,
			"request_id": reqID,
			"status":     "ready",
			"risky":      false,
			"reason":     fmt.Sprintf("Observed %s %s with status %d from the current session.", strings.ToUpper(firstNonEmpty(method, "request")), route, status),
			"args": map[string]interface{}{
				"operation": "query",
				"query":     fmt.Sprintf(`failed_request(%q, %q, Url, Status).`, sessionID, reqID),
			},
			"outputs": auditDefaultHandlesForStep(sessionID, map[string]interface{}{}, action),
		})
	}
	return plan
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func findAuditPlanStepNumberByAction(plan []map[string]interface{}, actions ...string) int {
	if len(actions) == 0 {
		return 0
	}
	allowed := make(map[string]bool, len(actions))
	for _, action := range actions {
		allowed[action] = true
	}
	for _, step := range plan {
		if allowed[auditStringValue(step["action"])] {
			return asInt(step["step"])
		}
	}
	return 0
}

func findAuditPlanStepNumberByIDPrefix(plan []map[string]interface{}, prefix string) int {
	for _, step := range plan {
		if strings.HasPrefix(auditStringValue(step["id"]), prefix) {
			return asInt(step["step"])
		}
	}
	return 0
}

func auditObserveModeForTraceStep(step browser.RepoTracePlanStep) string {
	switch step.Kind {
	case "map_navigation":
		return "nav"
	case "inspect_form", "review_control", "reveal_surface":
		return "interactive"
	default:
		return "state"
	}
}

func mapAuditMutabilityClass(mode string, hazardTypes []string) string {
	for _, hazard := range hazardTypes {
		if hazard == "write_capable" || hazard == "likely_destructive" {
			return "mutating"
		}
	}
	if strings.Contains(strings.ToLower(mode), "confirm") || strings.Contains(strings.ToLower(mode), "inspect") {
		return "mutating"
	}
	return "non_mutating"
}

func firstAuditHazardClass(hazardTypes []string) string {
	for _, hazard := range hazardTypes {
		switch hazard {
		case "likely_destructive":
			return "destructive_hazard"
		case "write_capable":
			return "write_hazard"
		case "auth_sensitive":
			return "auth_sensitive"
		}
	}
	return "non_mutating_reveal"
}

func firstAuditSeverity(hazardTypes []string) string {
	for _, hazard := range hazardTypes {
		switch hazard {
		case "likely_destructive":
			return "critical"
		case "write_capable":
			return "high"
		case "auth_sensitive":
			return "medium"
		}
	}
	return "low"
}

func buildAuditPlanSkeleton(sessionID, repoRoot, currentPath string, includeRepoMatches bool, maxRepoMatches int, failedRequests []map[string]interface{}, repoTraceReport *browser.RepoTraceReport) []map[string]interface{} {
	plan := []map[string]interface{}{}
	plan = appendAuditPlanStep(plan, map[string]interface{}{
		"id":     "observe_context",
		"title":  "Refresh browser context with browser-observe",
		"tool":   "browser-observe",
		"action": "reveal_page_context",
		"ref":    firstNonEmpty(currentPath, "page_context"),
		"label":  "page_context",
		"route":  currentPath,
		"status": "ready",
		"risky":  false,
		"reason": "Safe observation keeps the audit anchored to the current route, visible failure signals, and current interactive surfaces.",
		"args": map[string]interface{}{
			"session_id":          sessionID,
			"mode":                "composite",
			"view":                "compact",
			"include_action_plan": true,
			"include_diagnostics": true,
		},
		"outputs": []string{
			"audit:" + sessionID + ":page_context",
			"audit:" + sessionID + ":fact_summary",
			"audit:" + sessionID + ":search_specs",
		},
	})
	plan = appendAuditFailedRequestPlanSteps(plan, sessionID, currentPath, failedRequests)
	plan = appendRecursiveAuditSteps(plan, sessionID, repoTraceReport)
	plan = appendAuditPlanStep(plan, map[string]interface{}{
		"id":        "report_contracts",
		"title":     "Run the contract report over the current session",
		"tool":      "browser-audit",
		"action":    "reveal_contract_report",
		"ref":       "contract_findings",
		"label":     "contract_findings",
		"route":     currentPath,
		"status":    "deferred",
		"risky":     includeRepoMatches,
		"hazard_id": "scoped_fact_emission",
		"reason":    "This step scans repo_root, correlates backend failures, and may emit scoped contract facts.",
		"args": map[string]interface{}{
			"session_id":           sessionID,
			"repo_root":            repoRoot,
			"phase":                "report",
			"view":                 "compact",
			"include_repo_matches": includeRepoMatches,
			"max_repo_matches":     maxRepoMatches,
		},
		"outputs": []string{
			"audit:" + sessionID + ":contract_findings",
			"audit:" + sessionID + ":failed_requests",
			"audit:" + sessionID + ":api_correlations",
			"audit:" + sessionID + ":full_stack_errors",
			"audit:" + sessionID + ":user_visible_errors",
			"audit:" + sessionID + ":mangle_contracts",
		},
	})
	plan = appendAuditPlanStep(plan, map[string]interface{}{
		"id":     "reason_findings",
		"title":  "Explain the top contract break with browser-reason",
		"tool":   "browser-reason",
		"action": "reveal_contract_reasoning",
		"ref":    "why_failed",
		"label":  "why_failed",
		"route":  currentPath,
		"status": "deferred",
		"risky":  false,
		"reason": "Reasoning helps confirm whether the report points to backend breakage, auth failures, or response-shape drift.",
		"args": map[string]interface{}{
			"session_id": sessionID,
			"topic":      "why_failed",
			"view":       "compact",
		},
	})
	plan = appendAuditPlanStep(plan, map[string]interface{}{
		"id":     "resume_focus",
		"title":  "Resume with only the sections you still need",
		"tool":   "browser-audit",
		"action": "reveal_compact_resume",
		"ref":    "resume",
		"label":  "resume",
		"route":  currentPath,
		"status": "deferred",
		"risky":  false,
		"reason": "Resume narrows the audit to selected evidence handles instead of re-reading the full report.",
		"args": map[string]interface{}{
			"session_id": sessionID,
			"repo_root":  repoRoot,
			"phase":      "resume",
			"view":       "compact",
		},
		"outputs": []string{
			"audit:" + sessionID + ":audit_plan",
			"audit:" + sessionID + ":audit_hazards",
			"audit:" + sessionID + ":contract_findings",
		},
	})
	return plan
}

func buildAuditHazards(
	currentURL string,
	searchSpecCount int,
	failedRequestCount int,
	userVisibleErrorCount int,
	includeRepoMatches bool,
	sessionsAvailable bool,
	plan []map[string]interface{},
	repoTraceReport *browser.RepoTraceReport,
) []map[string]interface{} {
	hazards := make([]map[string]interface{}, 0, 6)
	requestStep := findAuditPlanStepNumberByAction(plan, "reveal_request_auth", "reveal_request_payload", "reveal_contract_diff")
	if requestStep == 0 {
		requestStep = 2
	}
	reportStep := findAuditPlanStepNumberByIDPrefix(plan, "report_contracts")
	if reportStep == 0 {
		reportStep = requestStep
	}
	reasonStep := findAuditPlanStepNumberByIDPrefix(plan, "reason_findings")
	if reasonStep == 0 {
		reasonStep = reportStep + 1
	}

	if strings.TrimSpace(currentURL) == "" {
		hazards = append(hazards, map[string]interface{}{
			"hazard":   "missing_route_context",
			"severity": "medium",
			"step":     1,
			"source":   "browser_context",
			"message":  "No current URL was resolved for the session, so repo targeting may be weaker.",
			"risky":    false,
		})
	}

	if failedRequestCount == 0 {
		hazards = append(hazards, map[string]interface{}{
			"hazard":   "no_failed_requests_in_scope",
			"severity": "low",
			"step":     requestStep,
			"source":   "audit_window",
			"message":  "No failed requests were found in the current audit window, so the report may lean on weaker browser signals.",
			"risky":    false,
		})
	}

	if searchSpecCount == 0 {
		hazards = append(hazards, map[string]interface{}{
			"hazard":   "low_specificity_search",
			"severity": "medium",
			"step":     reportStep,
			"source":   "repo_search",
			"message":  "No deterministic route or API search specs were derived yet, so repo matches may be noisy.",
			"risky":    false,
		})
	}

	if includeRepoMatches {
		hazards = append(hazards, map[string]interface{}{
			"hazard":   "repo_scan_cost",
			"severity": "low",
			"step":     reportStep,
			"source":   "repo_search",
			"message":  "The report phase may scan repo_root to locate route and endpoint references, which can add latency on large repositories.",
			"risky":    false,
		})
	}

	if includeRepoMatches && !sessionsAvailable {
		hazards = append(hazards, map[string]interface{}{
			"hazard":   "repo_trace_unavailable",
			"severity": "medium",
			"step":     reportStep,
			"source":   "repo_trace",
			"message":  "Scoped repository trace correlation is unavailable because no session manager is attached.",
			"risky":    false,
		})
	}

	if includeRepoMatches && sessionsAvailable {
		hazards = append(hazards, map[string]interface{}{
			"hazard":   "scoped_fact_emission",
			"severity": "medium",
			"step":     reportStep,
			"source":   "repo_trace",
			"message":  "The report phase may emit scoped_* contract facts into the Mangle engine while building repo correlations.",
			"risky":    true,
		})
	}

	if userVisibleErrorCount == 0 {
		hazards = append(hazards, map[string]interface{}{
			"hazard":   "no_user_visible_error_signal",
			"severity": "low",
			"step":     reasonStep,
			"source":   "reasoning",
			"message":  "No user-visible errors were recorded, so response-shape hints may be limited.",
			"risky":    false,
		})
	}

	return mergeAuditHazardSets(hazards, buildRecursiveAuditHazards(plan, repoTraceReport))
}
