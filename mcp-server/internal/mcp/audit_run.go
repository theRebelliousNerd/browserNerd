package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/security"
)

func persistAuditRun(repoRoot string, run persistedAuditRun) error {
	auditDir := filepath.Join(repoRoot, ".browsernerd", "data", "audits")
	if err := security.EnsurePrivateDir(auditDir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return err
	}
	if err := security.WritePrivateFile(filepath.Join(auditDir, run.AuditID+".json"), data); err != nil {
		return err
	}
	return security.WritePrivateFile(filepath.Join(auditDir, "latest-"+run.SessionID+".json"), data)
}

func loadPersistedAuditRun(repoRoot, sessionID, auditID string) (*persistedAuditRun, error) {
	auditDir := filepath.Join(repoRoot, ".browsernerd", "data", "audits")
	target := auditID
	if target == "" {
		target = "latest-" + sessionID
	}
	path := filepath.Join(auditDir, target+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load audit run %s: %w", target, err)
	}
	var run persistedAuditRun
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("decode audit run %s: %w", target, err)
	}
	return &run, nil
}

func renderPersistedAuditRun(run *persistedAuditRun, view string, maxItems int) map[string]interface{} {
	response := map[string]interface{}{
		"success":           true,
		"audit_id":          run.AuditID,
		"phase":             "resume",
		"session_id":        run.SessionID,
		"repo_root":         run.RepoRoot,
		"approval_required": run.ApprovalRequired,
		"summary":           fmt.Sprintf("resumed audit %s with %d planned steps and %d hazards", run.AuditID, len(run.Plan), len(run.Hazards)),
		"evidence_handles":  run.Handles,
		"view":              view,
	}
	data := map[string]interface{}{
		"audit_plan":        run.Plan,
		"audit_hazards":     run.Hazards,
		"contract_findings": run.Findings,
	}
	switch view {
	case "summary":
		response["counts"] = map[string]interface{}{
			"plan_steps": len(run.Plan),
			"hazards":    len(run.Hazards),
			"findings":   len(run.Findings),
		}
	case "compact":
		response["data"] = truncateAuditData(data, maxItems)
	default:
		response["data"] = data
	}
	return response
}

type auditStepExecutionPolicy struct {
	ActionKind      string
	TargetRef       string
	PageRoute       string
	APIRoute        string
	RequestID       string
	MutabilityClass string
	HazardClass     string
	Severity        string
	SkipReason      string
}

func (t *BrowserAuditTool) executePersistedAuditRun(ctx context.Context, run *persistedAuditRun, view string, maxItems int, allowances auditExecuteAllowances) (map[string]interface{}, error) {
	if run == nil {
		return nil, fmt.Errorf("audit run is required")
	}
	if run.SessionID == "" {
		return nil, fmt.Errorf("persisted audit run %s is missing session_id", run.AuditID)
	}

	completed := make([]map[string]interface{}, 0, len(run.Plan))
	skipped := make([]map[string]interface{}, 0, len(run.Plan))
	rediscoveryAttempts := 0
	rediscoveredSteps := 0
	planExpanded := false
	now := time.Now()

	if t.engine != nil {
		if err := t.engine.AddFacts(ctx, buildAuditPlanFacts(run.SessionID, run.AuditID, run.Phase, coalesceAuditRunStatus(run.Status, "loaded"), run.Plan, run.Hazards)); err != nil {
			return nil, fmt.Errorf("seed audit plan facts %s: %w", run.AuditID, err)
		}
	}

	for stepIndex := 0; stepIndex < len(run.Plan); stepIndex++ {
		step := run.Plan[stepIndex]
		stepID := auditStringValue(step["id"])
		toolName := auditStringValue(step["tool"])
		if stepID == "" && toolName == "" && strings.TrimSpace(fmt.Sprintf("%v", step["action"])) == "" {
			continue
		}
		if stepStatus := strings.TrimSpace(fmt.Sprintf("%v", step["status"])); stepStatus == "completed" || stepStatus == "skipped" {
			continue
		}

		policy := resolveAuditStepExecutionPolicy(step, auditHazardsForStep(run.Hazards, step), run.CurrentPath, allowances)
		if policy.SkipReason != "" {
			step["status"] = "skipped"
			step["skip_reason"] = policy.SkipReason
			skipped = append(skipped, auditExecutionRecordForStep(step, toolName, policy, false))
			continue
		}

		stepStartedAt := time.Now()
		execResult, err := t.executeAuditPlanStep(ctx, run, step, policy)
		if err != nil {
			step["status"] = "skipped"
			step["skip_reason"] = err.Error()
			skipped = append(skipped, auditExecutionRecordForStep(step, toolName, policy, false))
			continue
		}

		stepFinishedAt := time.Now()
		step["status"] = "completed"
		step["completed_at"] = stepFinishedAt.UnixMilli()

		stepEvidence := captureAuditStepEvidence(t.engine, run.SessionID, stepStartedAt)
		resultMap, _ := execResult.(map[string]interface{})
		if resultMap != nil {
			if summary := strings.TrimSpace(fmt.Sprintf("%v", resultMap["summary"])); summary != "" {
				step["result_summary"] = summary
			}
			if handles := uniqueStrings(toStringSlice(resultMap["evidence_handles"])); len(handles) > 0 {
				step["result_handles"] = handles
			}
			if evidence, ok := resultMap["evidence"].(map[string]interface{}); ok && len(evidence) > 0 {
				if len(stepEvidence) == 0 {
					stepEvidence = map[string]interface{}{}
				}
				for key, value := range evidence {
					stepEvidence[key] = value
				}
			}
			mergeAuditExecutionResultIntoRun(run, resultMap)
		}
		if handles := uniqueStrings(toStringSlice(step["result_handles"])); len(handles) == 0 {
			if outputs := uniqueStrings(toStringSlice(step["outputs"])); len(outputs) > 0 {
				step["result_handles"] = outputs
			}
		}
		if strings.TrimSpace(fmt.Sprintf("%v", step["result_summary"])) == "" {
			step["result_summary"] = auditDefaultExecutionSummary(step, policy)
		}
		run.CurrentURL, run.CurrentPath = updateAuditRunLocation(run.CurrentURL, run.CurrentPath, stepEvidence)
		if auditStepShouldRediscover(step, policy) && rediscoveryAttempts < defaultAuditExecuteRediscoveryPasses {
			rediscoveryAttempts++
			appendedSteps, warnings, err := t.appendRediscoveredAuditPlan(ctx, run, step)
			if len(warnings) > 0 {
				if len(stepEvidence) == 0 {
					stepEvidence = map[string]interface{}{}
				}
				stepEvidence["rediscovery_warnings"] = append([]string{}, warnings...)
			}
			if err != nil {
				if len(stepEvidence) == 0 {
					stepEvidence = map[string]interface{}{}
				}
				stepEvidence["rediscovery_error"] = err.Error()
			} else if len(appendedSteps) > 0 {
				planExpanded = true
				rediscoveredSteps += len(appendedSteps)
				if len(stepEvidence) == 0 {
					stepEvidence = map[string]interface{}{}
				}
				stepEvidence["rediscovered_steps"] = auditPlanStepIDs(appendedSteps)
			}
		}
		if len(stepEvidence) > 0 {
			step["evidence"] = stepEvidence
		}
		completed = append(completed, auditExecutionRecordForStep(step, toolName, policy, true))
	}

	run.Phase = "execute"
	run.GeneratedAt = now.UnixMilli()
	run.Status = "complete"
	run.ApprovalRequired = auditExecutionNeedsApproval(run.Plan)
	if countPendingAuditSteps(run.Plan) > 0 || run.ApprovalRequired {
		run.Status = "resume_ready"
	}
	run.CompletedSteps = auditExecutionStateFromPlan(run.Plan, "completed")
	run.SkippedSteps = auditExecutionStateFromPlan(run.Plan, "skipped")
	run.Handles = mergeAuditHandles(run.Handles, auditExecutionHandlesFromSteps(run.Plan))

	if err := persistAuditRun(run.RepoRoot, *run); err != nil {
		return nil, fmt.Errorf("persist execute audit run %s: %w", run.AuditID, err)
	}
	if t.engine != nil {
		if planExpanded {
			if err := t.engine.AddFacts(ctx, buildAuditPlanFacts(run.SessionID, run.AuditID, "execute", coalesceAuditRunStatus(run.Status, "resume_ready"), run.Plan, run.Hazards)); err != nil {
				return nil, fmt.Errorf("emit expanded audit plan facts %s: %w", run.AuditID, err)
			}
		}
		if err := t.engine.AddFacts(ctx, buildAuditExecutionFacts(run, completed, skipped, now)); err != nil {
			return nil, fmt.Errorf("emit execute audit facts %s: %w", run.AuditID, err)
		}
	}

	summary := fmt.Sprintf("phase=execute status=%s completed=%d skipped=%d remaining=%d", run.Status, len(completed), len(skipped), countPendingAuditSteps(run.Plan))
	if rediscoveredSteps > 0 {
		summary += fmt.Sprintf(" rediscovered=%d", rediscoveredSteps)
	}

	response := map[string]interface{}{
		"success":           true,
		"audit_id":          run.AuditID,
		"phase":             "execute",
		"status":            run.Status,
		"session_id":        run.SessionID,
		"repo_root":         run.RepoRoot,
		"summary":           summary,
		"completed_steps":   completed,
		"skipped_steps":     skipped,
		"approval_required": run.ApprovalRequired,
		"evidence_handles":  run.Handles,
		"view":              view,
	}
	if rediscoveredSteps > 0 {
		response["rediscovered_steps"] = rediscoveredSteps
	}
	data := map[string]interface{}{
		"audit_plan":        run.Plan,
		"audit_hazards":     run.Hazards,
		"completed_steps":   run.CompletedSteps,
		"skipped_steps":     run.SkippedSteps,
		"contract_findings": run.Findings,
	}
	switch view {
	case "summary":
		response["counts"] = map[string]interface{}{
			"completed": len(run.CompletedSteps),
			"skipped":   len(run.SkippedSteps),
			"pending":   countPendingAuditSteps(run.Plan),
		}
	case "compact":
		response["data"] = truncateAuditData(data, maxItems)
	default:
		response["data"] = data
	}

	return response, nil
}

func (t *BrowserAuditTool) appendRediscoveredAuditPlan(ctx context.Context, run *persistedAuditRun, step map[string]interface{}) ([]map[string]interface{}, []string, error) {
	if run == nil {
		return nil, nil, fmt.Errorf("audit run is required")
	}

	snapshot, err := t.buildAuditDiscoverSnapshot(ctx, run.SessionID, run.RepoRoot, auditDiscoverOptionsFromRun(run))
	if err != nil {
		return nil, nil, err
	}

	if snapshot.CurrentURL != "" {
		run.CurrentURL = snapshot.CurrentURL
	}
	if snapshot.CurrentPath != "" {
		run.CurrentPath = snapshot.CurrentPath
	}
	run.Handles = mergeAuditHandles(run.Handles, snapshot.ReportHandles)
	discoveredPlan := snapshot.Plan
	if !t.canExecuteAuditObserve(run.SessionID) {
		discoveredPlan = filterRediscoveredAuditPlanForExecution(discoveredPlan)
	}
	var appended []map[string]interface{}
	run.Plan, run.Hazards, appended = appendRediscoveredAuditPlanEntries(run.Plan, run.Hazards, discoveredPlan, snapshot.Hazards, auditStringValue(step["id"]))
	return appended, snapshot.Warnings, nil
}

func (t *BrowserAuditTool) buildAuditDiscoverSnapshot(ctx context.Context, sessionID, repoRoot string, opts auditDiscoverOptions) (auditDiscoverSnapshot, error) {
	snapshot := auditDiscoverSnapshot{}
	if t.engine == nil {
		return snapshot, fmt.Errorf("mangle engine is not available")
	}

	maxRepoMatches := opts.MaxRepoMatches
	if maxRepoMatches <= 0 {
		maxRepoMatches = defaultAuditMaxRepoMatches
	}

	sinceMs := int64(0)
	if opts.TimeWindowMs > 0 {
		sinceMs = time.Now().UnixMilli() - int64(opts.TimeWindowMs)
	}
	navigationSinceMs := int64(0)
	if opts.SinceNavigation {
		navigationSinceMs = latestNavigationTimestamp(ctx, t.engine, sessionID)
	}
	effectiveSinceMs := sinceMs
	if navigationSinceMs > effectiveSinceMs {
		effectiveSinceMs = navigationSinceMs
	}

	currentURL := resolveCurrentURL(ctx, t.engine, sessionID)
	if currentURL == "" && t.sessions != nil {
		if session, ok := t.sessions.GetSession(sessionID); ok {
			currentURL = strings.TrimSpace(session.URL)
		}
	}

	pageContext := buildAuditPageContext(t.sessions, sessionID, currentURL, repoRoot)
	failedRequests := queryAuditFailedRequests(ctx, t.engine, sessionID)
	apiCorrelations := queryToRows(ctx, t.engine, fmt.Sprintf("api_backend_correlation(%q, ReqId, Url, Status, BackendMsg, TimeDelta).", sessionID))
	fullStackErrors := queryToRows(ctx, t.engine, fmt.Sprintf("full_stack_error(%q, ConsoleMsg, ReqId, Url, BackendMsg).", sessionID))
	userVisibleErrors := queryToRows(ctx, t.engine, fmt.Sprintf("user_visible_error(%q, Source, Message, Timestamp).", sessionID))

	if effectiveSinceMs > 0 {
		failedRequests = filterRowsSince(failedRequests, []string{"ReqTs", "StartTime", "Timestamp"}, effectiveSinceMs)
		userVisibleErrors = filterRowsSince(userVisibleErrors, []string{"Timestamp", "Ts"}, effectiveSinceMs)
	}
	userVisibleErrors = dedupeUserVisibleErrors(userVisibleErrors)

	allowedReqIDs := make(map[string]bool, len(failedRequests))
	for _, row := range failedRequests {
		reqID := strings.TrimSpace(fmt.Sprintf("%v", row["ReqId"]))
		if reqID != "" {
			allowedReqIDs[reqID] = true
		}
	}
	apiCorrelations = filterAuditRowsByRequestID(apiCorrelations, allowedReqIDs)
	fullStackErrors = filterAuditRowsByRequestID(fullStackErrors, allowedReqIDs)

	searchSpecs := buildAuditRepoSearchSpecs(currentURL, failedRequests)
	searchSpecRows := auditSearchSpecsToRows(searchSpecs)
	discoverRepoTrace, warnings := t.traceAuditDiscoverContext(ctx, sessionID, repoRoot, opts.IncludeRepoMatches, maxRepoMatches)
	if discoverRepoTrace != nil {
		pageContext["repo_trace_id"] = discoverRepoTrace.TraceID
		pageContext["repo_trace_available"] = true
		pageContext["repo_trace_frontend_matches"] = len(discoverRepoTrace.FrontendSites)
		pageContext["repo_trace_backend_matches"] = len(discoverRepoTrace.BackendMatches)
		pageContext["repo_trace_correlations"] = len(discoverRepoTrace.Correlations)
		pageContext["repo_trace_plan_steps"] = len(discoverRepoTrace.AuditPlan)
		pageContext["repo_trace_navigation_links"] = len(discoverRepoTrace.BrowserContext.NavigationLinks)
		pageContext["repo_trace_controls"] = len(discoverRepoTrace.BrowserContext.Controls)
		pageContext["repo_trace_forms"] = len(discoverRepoTrace.BrowserContext.Forms)
	} else if len(warnings) > 0 {
		pageContext["repo_trace_available"] = false
	}

	currentPath := canonicalAuditPath(currentURL)
	plan := buildAuditPlanSkeleton(sessionID, repoRoot, currentPath, opts.IncludeRepoMatches, maxRepoMatches, failedRequests, discoverRepoTrace)
	hazards := buildAuditHazards(
		currentURL,
		len(searchSpecs),
		len(failedRequests),
		len(userVisibleErrors),
		opts.IncludeRepoMatches,
		t.sessions != nil,
		plan,
		discoverRepoTrace,
	)

	factSummary := map[string]interface{}{
		"failed_requests":     len(failedRequests),
		"api_correlations":    len(apiCorrelations),
		"full_stack_errors":   len(fullStackErrors),
		"user_visible_errors": len(userVisibleErrors),
		"audit_plan":          len(plan),
		"audit_hazards":       len(hazards),
		"search_specs":        len(searchSpecs),
		"repo_scan_planned":   opts.IncludeRepoMatches,
		"repo_trace_planned":  opts.IncludeRepoMatches && t.sessions != nil,
	}
	if discoverRepoTrace != nil {
		factSummary["repo_trace_plan_steps"] = len(discoverRepoTrace.AuditPlan)
		factSummary["repo_trace_navigation_links"] = len(discoverRepoTrace.BrowserContext.NavigationLinks)
		factSummary["repo_trace_controls"] = len(discoverRepoTrace.BrowserContext.Controls)
		factSummary["repo_trace_forms"] = len(discoverRepoTrace.BrowserContext.Forms)
	}

	snapshot = auditDiscoverSnapshot{
		CurrentURL:        currentURL,
		CurrentPath:       currentPath,
		PageContext:       pageContext,
		FactSummary:       factSummary,
		SearchSpecRows:    searchSpecRows,
		Plan:              plan,
		Hazards:           hazards,
		DiscoverHandles:   buildAuditDiscoverHandles(sessionID, discoverRepoTrace != nil),
		ReportHandles:     buildAuditReportHandles(sessionID, opts.IncludeRepoMatches, discoverRepoTrace != nil),
		Warnings:          warnings,
		NavigationSinceMs: navigationSinceMs,
		EffectiveSinceMs:  effectiveSinceMs,
		ApprovalRequired:  auditHazardsRequireApproval(hazards),
	}
	return snapshot, nil
}

func auditDiscoverOptionsFromRun(run *persistedAuditRun) auditDiscoverOptions {
	opts := auditDiscoverOptions{
		IncludeRepoMatches: true,
		MaxRepoMatches:     defaultAuditMaxRepoMatches,
		TimeWindowMs:       defaultReasonTimeWindowMs,
		SinceNavigation:    true,
	}
	if run == nil {
		return opts
	}

	for _, step := range run.Plan {
		if auditStringValue(step["tool"]) != "browser-audit" {
			continue
		}
		args, _ := step["args"].(map[string]interface{})
		if normalizeAuditPhase(getStringArg(args, "phase")) != "report" {
			continue
		}
		if argPresent(args, "include_repo_matches") {
			opts.IncludeRepoMatches = getBoolArg(args, "include_repo_matches", opts.IncludeRepoMatches)
		}
		if argHasInt(args, "max_repo_matches") {
			opts.MaxRepoMatches = getIntArg(args, "max_repo_matches", opts.MaxRepoMatches)
		}
		if argHasInt(args, "time_window_ms") {
			opts.TimeWindowMs = getIntArg(args, "time_window_ms", opts.TimeWindowMs)
		}
		if argPresent(args, "since_navigation") {
			opts.SinceNavigation = getBoolArg(args, "since_navigation", opts.SinceNavigation)
		}
		return opts
	}

	return opts
}

func auditStepShouldRediscover(step map[string]interface{}, policy auditStepExecutionPolicy) bool {
	toolName := auditStringValue(step["tool"])
	actionKind := strings.ToLower(strings.TrimSpace(policy.ActionKind))
	if toolName == "browser-observe" {
		return true
	}
	if actionKind == "map_navigation" || strings.HasPrefix(actionKind, "navigate") {
		return true
	}
	return strings.HasPrefix(actionKind, "reveal_")
}

func mergeAuditExecutionResultIntoRun(run *persistedAuditRun, resultMap map[string]interface{}) {
	if run == nil || len(resultMap) == 0 {
		return
	}

	if handles := toStringSlice(resultMap["evidence_handles"]); len(handles) > 0 {
		run.Handles = mergeAuditHandles(run.Handles, handles)
	}
	if findings := toMapSlice(resultMap["top_findings"]); len(findings) > 0 {
		run.Findings = findings
	}

	data, _ := resultMap["data"].(map[string]interface{})
	if len(data) == 0 {
		return
	}
	if findings := toMapSlice(data["contract_findings"]); len(findings) > 0 {
		run.Findings = findings
	}
	if pageContext, ok := data["page_context"].(map[string]interface{}); ok {
		if currentURL := strings.TrimSpace(fmt.Sprintf("%v", pageContext["current_url"])); currentURL != "" {
			run.CurrentURL = currentURL
			run.CurrentPath = canonicalAuditPath(currentURL)
		}
		return
	}
	if state, ok := data["state"].(map[string]interface{}); ok {
		if currentURL := strings.TrimSpace(fmt.Sprintf("%v", state["url"])); currentURL != "" {
			run.CurrentURL = currentURL
			run.CurrentPath = canonicalAuditPath(currentURL)
		}
	}
}

func appendRediscoveredAuditPlanEntries(plan, hazards, discoveredPlan, discoveredHazards []map[string]interface{}, sourceStepID string) ([]map[string]interface{}, []map[string]interface{}, []map[string]interface{}) {
	if len(discoveredPlan) == 0 {
		return plan, hazards, nil
	}

	nextStep := len(plan) + 1
	existingKeys := make(map[string]bool, len(plan))
	existingIDs := make(map[string]bool, len(plan))
	for _, step := range plan {
		existingKeys[auditPlanStepKey(step)] = true
		if stepID := auditStringValue(step["id"]); stepID != "" {
			existingIDs[stepID] = true
		}
	}

	stepNumberMap := make(map[int]int, len(discoveredPlan))
	appended := make([]map[string]interface{}, 0, minInt(len(discoveredPlan), defaultAuditExecutePlanAppendLimit))
	for _, step := range discoveredPlan {
		key := auditPlanStepKey(step)
		if key == "" || existingKeys[key] || len(appended) >= defaultAuditExecutePlanAppendLimit {
			continue
		}

		clonedStep := cloneAuditMap(step)
		originalStepNum := asInt(clonedStep["step"])
		clonedStep["step"] = nextStep
		clonedStep["id"] = uniqueAuditPlanStepID(existingIDs, auditStringValue(clonedStep["id"]), nextStep)
		if sourceStepID != "" {
			clonedStep["rediscovered_after_step"] = sourceStepID
		}

		existingKeys[key] = true
		existingIDs[auditStringValue(clonedStep["id"])] = true
		stepNumberMap[originalStepNum] = nextStep
		plan = append(plan, clonedStep)
		appended = append(appended, clonedStep)
		nextStep++
	}

	if len(stepNumberMap) == 0 || len(discoveredHazards) == 0 {
		return plan, hazards, appended
	}

	remappedHazards := make([]map[string]interface{}, 0, len(stepNumberMap))
	for _, hazard := range discoveredHazards {
		newStep, ok := stepNumberMap[asInt(hazard["step"])]
		if !ok {
			continue
		}
		clonedHazard := cloneAuditMap(hazard)
		clonedHazard["step"] = newStep
		if sourceStepID != "" {
			clonedHazard["rediscovered_after_step"] = sourceStepID
		}
		remappedHazards = append(remappedHazards, clonedHazard)
	}

	return plan, mergeAuditHazardSets(hazards, remappedHazards), appended
}

func uniqueAuditPlanStepID(existing map[string]bool, baseID string, stepNum int) string {
	baseID = strings.TrimSpace(baseID)
	if baseID == "" {
		baseID = fmt.Sprintf("rediscovered-step-%d", stepNum)
	}
	if !existing[baseID] {
		return baseID
	}
	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s-r%d", baseID, suffix)
		if !existing[candidate] {
			return candidate
		}
	}
}

func cloneAuditMap(src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return map[string]interface{}{}
	}
	dst := make(map[string]interface{}, len(src))
	for key, value := range src {
		switch typed := value.(type) {
		case map[string]interface{}:
			dst[key] = cloneAuditMap(typed)
		case []string:
			dst[key] = append([]string{}, typed...)
		case []interface{}:
			dst[key] = append([]interface{}{}, typed...)
		default:
			dst[key] = value
		}
	}
	return dst
}

func auditPlanStepIDs(steps []map[string]interface{}) []string {
	ids := make([]string, 0, len(steps))
	for _, step := range steps {
		if stepID := auditStringValue(step["id"]); stepID != "" {
			ids = append(ids, stepID)
		}
	}
	return ids
}

func (t *BrowserAuditTool) canExecuteAuditObserve(sessionID string) bool {
	if t.sessions == nil {
		return false
	}
	_, ok := t.sessions.Page(sessionID)
	return ok
}

func filterRediscoveredAuditPlanForExecution(plan []map[string]interface{}) []map[string]interface{} {
	filtered := make([]map[string]interface{}, 0, len(plan))
	for _, step := range plan {
		if auditStringValue(step["tool"]) == "browser-observe" {
			continue
		}
		filtered = append(filtered, step)
	}
	return filtered
}
