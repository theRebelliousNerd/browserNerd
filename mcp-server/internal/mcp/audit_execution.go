package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

func (t *BrowserAuditTool) executeAuditPlanStep(ctx context.Context, run *persistedAuditRun, step map[string]interface{}, policy auditStepExecutionPolicy) (interface{}, error) {
	toolName := auditStringValue(step["tool"])
	args, _ := step["args"].(map[string]interface{})
	args = copyAuditArgs(args)
	if args["session_id"] == nil && run.SessionID != "" {
		args["session_id"] = run.SessionID
	}
	if args["repo_root"] == nil && run.RepoRoot != "" {
		args["repo_root"] = run.RepoRoot
	}

	if toolName == "" && policy.ActionKind != "" {
		return t.executeAuditActionStep(ctx, run, step, policy)
	}

	if policy.ActionKind == "map_navigation" || policy.ActionKind == "navigate_page_route" || policy.ActionKind == "navigate_history" {
		return t.executeAuditActionStep(ctx, run, step, policy)
	}

	switch toolName {
	case "browser-observe":
		return (&BrowserObserveTool{sessions: t.sessions, engine: t.engine}).Execute(ctx, args)
	case "browser-reason":
		return (&BrowserReasonTool{engine: t.engine}).Execute(ctx, args)
	case "browser-audit":
		if strings.TrimSpace(getStringArg(args, "audit_id")) == "" {
			args["audit_id"] = run.AuditID
		}
		return t.Execute(ctx, args)
	case "":
		return t.executeAuditActionStep(ctx, run, step, policy)
	default:
		if policy.ActionKind != "" {
			return t.executeAuditActionStep(ctx, run, step, policy)
		}
		return nil, fmt.Errorf("unsupported audit plan tool: %s", toolName)
	}
}

func (t *BrowserAuditTool) executeAuditActionStep(ctx context.Context, run *persistedAuditRun, step map[string]interface{}, policy auditStepExecutionPolicy) (map[string]interface{}, error) {
	handles := mergeAuditHandles(nil, auditDefaultHandlesForStep(run.SessionID, step, policy.ActionKind))
	summary := auditDefaultExecutionSummary(step, policy)

	switch policy.ActionKind {
	case "reveal_surface":
		if t.sessions == nil {
			return nil, fmt.Errorf("reveal_surface requires an active session manager")
		}
		ref, err := t.resolveInteractiveRefForStep(ctx, run.SessionID, step)
		if err != nil {
			return nil, err
		}
		res, err := (&InteractTool{sessions: t.sessions, engine: t.engine}).Execute(ctx, map[string]interface{}{
			"session_id": run.SessionID,
			"ref":        ref,
			"action":     "click",
		})
		if err != nil {
			return nil, err
		}
		result := map[string]interface{}{
			"summary":          fmt.Sprintf("revealed additional page context via %s", ref),
			"evidence_handles": handles,
		}
		if resultMap, ok := res.(map[string]interface{}); ok {
			for key, value := range resultMap {
				result[key] = value
			}
		}
		return result, nil
	case "map_navigation":
		if t.sessions == nil {
			return nil, fmt.Errorf("map_navigation requires an active session manager")
		}
		targetURL, err := resolveAuditNavigationURL(run.CurrentURL, auditStringValue(step["path"]), policy.PageRoute)
		if err != nil {
			return nil, err
		}
		res, err := (&NavigateURLTool{sessions: t.sessions, engine: t.engine}).Execute(ctx, map[string]interface{}{
			"session_id": run.SessionID,
			"url":        targetURL,
			"wait_until": "load",
		})
		if err != nil {
			return nil, err
		}
		result := map[string]interface{}{
			"summary":          fmt.Sprintf("navigated to %s for recursive audit discovery", targetURL),
			"evidence_handles": handles,
		}
		if resultMap, ok := res.(map[string]interface{}); ok {
			for key, value := range resultMap {
				result[key] = value
			}
		}
		return result, nil
	case "navigate_page_route":
		if t.sessions == nil {
			return nil, fmt.Errorf("navigate_page_route requires an active session manager")
		}
		targetURL, err := resolveAuditNavigationURL(run.CurrentURL, policy.TargetRef, policy.PageRoute)
		if err != nil {
			return nil, err
		}
		res, err := (&NavigateURLTool{sessions: t.sessions, engine: t.engine}).Execute(ctx, map[string]interface{}{
			"session_id": run.SessionID,
			"url":        targetURL,
			"wait_until": "load",
		})
		if err != nil {
			return nil, err
		}
		result := map[string]interface{}{
			"summary":          fmt.Sprintf("navigated to %s for audit replay", targetURL),
			"evidence_handles": handles,
		}
		if resultMap, ok := res.(map[string]interface{}); ok {
			for key, value := range resultMap {
				result[key] = value
			}
			if strings.TrimSpace(fmt.Sprintf("%v", resultMap["summary"])) == "" {
				result["summary"] = fmt.Sprintf("navigated to %s for audit replay", targetURL)
			}
		}
		return result, nil
	case "navigate_history":
		if t.sessions == nil {
			return nil, fmt.Errorf("navigate_history requires an active session manager")
		}
		action := strings.TrimSpace(policy.TargetRef)
		if action == "" {
			action = "back"
		}
		res, err := (&BrowserHistoryTool{sessions: t.sessions, engine: t.engine}).Execute(ctx, map[string]interface{}{
			"session_id": run.SessionID,
			"action":     action,
		})
		if err != nil {
			return nil, err
		}
		result := map[string]interface{}{
			"summary":          fmt.Sprintf("replayed history action %s for audit", action),
			"evidence_handles": handles,
		}
		if resultMap, ok := res.(map[string]interface{}); ok {
			for key, value := range resultMap {
				result[key] = value
			}
		}
		return result, nil
	default:
		return map[string]interface{}{
			"summary":          summary,
			"evidence_handles": handles,
			"evidence": map[string]interface{}{
				"action":     policy.ActionKind,
				"target_ref": policy.TargetRef,
				"request_id": policy.RequestID,
			},
		}, nil
	}
}

func resolveAuditStepExecutionPolicy(step map[string]interface{}, stepHazards []map[string]interface{}, currentPath string, allowances auditExecuteAllowances) auditStepExecutionPolicy {
	actionKind, targetRef, pageRoute, apiRoute, requestID := auditActionIdentityForStep(step, currentPath)
	mutabilityClass, hazardClass, severity := auditHazardProfileForStep(step, stepHazards, actionKind)
	policy := auditStepExecutionPolicy{
		ActionKind:      actionKind,
		TargetRef:       targetRef,
		PageRoute:       pageRoute,
		APIRoute:        apiRoute,
		RequestID:       requestID,
		MutabilityClass: mutabilityClass,
		HazardClass:     hazardClass,
		Severity:        severity,
	}

	switch hazardClass {
	case "destructive_hazard":
		if !allowances.Destructive && !allowances.Risky {
			policy.SkipReason = "requires explicit allow_destructive"
		}
	case "write_hazard":
		if !allowances.Mutating && !allowances.Risky {
			policy.SkipReason = "requires explicit allow_mutating"
		}
	case "non_mutating_navigation":
		if !allowances.Navigation && !allowances.Risky {
			policy.SkipReason = "requires explicit allow_navigation"
		}
	}

	if policy.SkipReason == "" {
		if gated, _ := step["gated"].(bool); gated {
			if !allowances.Risky {
				policy.SkipReason = "requires explicit allow_risky"
			}
		}
		if risky, _ := step["risky"].(bool); risky && !allowances.Risky {
			policy.SkipReason = "requires explicit allow_risky"
		}
	}

	return policy
}

func auditActionIdentityForStep(step map[string]interface{}, currentPath string) (string, string, string, string, string) {
	actionKind := auditStringValue(step["action"])
	if actionKind == "" {
		actionKind = defaultAuditActionKind(step)
	}
	targetRef := auditStringValue(step["ref"])
	if targetRef == "" {
		targetRef = auditStringValue(step["id"])
	}
	pageRoute := canonicalAuditPath(auditStringValue(step["route"]))
	if pageRoute == "" {
		pageRoute = canonicalAuditPath(currentPath)
	}
	apiRoute := canonicalAuditPath(auditStringValue(step["api_route"]))
	requestID := auditStringValue(step["request_id"])
	return actionKind, targetRef, pageRoute, apiRoute, requestID
}

func defaultAuditActionKind(step map[string]interface{}) string {
	toolName := auditStringValue(step["tool"])
	switch toolName {
	case "browser-observe":
		return "reveal_page_context"
	case "browser-reason":
		return "reveal_contract_reasoning"
	case "browser-audit":
		args, _ := step["args"].(map[string]interface{})
		switch normalizeAuditPhase(getStringArg(args, "phase")) {
		case "resume":
			return "reveal_compact_resume"
		case "discover":
			return "reveal_discover_plan"
		default:
			return "reveal_contract_report"
		}
	}

	stepID := auditStringValue(step["id"])
	if stepID == "" {
		return "reveal_audit_step"
	}
	return strings.ReplaceAll(stepID, "-", "_")
}

func auditHazardProfileForStep(step map[string]interface{}, stepHazards []map[string]interface{}, actionKind string) (string, string, string) {
	mutabilityClass := auditStringValue(step["mutability_class"])
	hazardClass := auditStringValue(step["hazard_class"])
	severity := strings.ToLower(auditStringValue(step["severity"]))

	for _, hazard := range stepHazards {
		if severity == "" || auditSeverityRank(auditStringValue(hazard["severity"])) > auditSeverityRank(severity) {
			severity = strings.ToLower(auditStringValue(hazard["severity"]))
		}
		if hazardClass == "" {
			switch strings.ToLower(auditStringValue(hazard["hazard"])) {
			case "write_action", "write_form":
				hazardClass = "write_hazard"
			case "destructive_action":
				hazardClass = "destructive_hazard"
			case "navigate_action":
				hazardClass = "non_mutating_navigation"
			}
		}
	}

	lowerAction := strings.ToLower(strings.TrimSpace(actionKind))
	switch {
	case hazardClass == "":
		switch {
		case lowerAction == "remove" || lowerAction == "destroy" || lowerAction == "purge" || strings.Contains(lowerAction, "delete"):
			hazardClass = "destructive_hazard"
			mutabilityClass = "mutating"
			if severity == "" {
				severity = "critical"
			}
		case lowerAction == "click" || lowerAction == "type" || lowerAction == "select" || lowerAction == "toggle" || lowerAction == "submit" || strings.Contains(lowerAction, "save"):
			hazardClass = "write_hazard"
			mutabilityClass = "mutating"
			if severity == "" {
				severity = "high"
			}
		case strings.HasPrefix(lowerAction, "navigate"):
			hazardClass = "non_mutating_navigation"
			mutabilityClass = "non_mutating"
			if severity == "" {
				severity = "medium"
			}
		case strings.HasPrefix(lowerAction, "reveal_"):
			hazardClass = "non_mutating_reveal"
			mutabilityClass = "non_mutating"
			if severity == "" {
				severity = "low"
			}
		}
	case mutabilityClass == "":
		switch hazardClass {
		case "destructive_hazard", "write_hazard":
			mutabilityClass = "mutating"
		default:
			mutabilityClass = "non_mutating"
		}
	}

	if severity == "" {
		if risky, _ := step["risky"].(bool); risky {
			severity = "high"
		} else {
			severity = "low"
		}
	}

	return mutabilityClass, hazardClass, severity
}

func auditHazardsForStep(hazards []map[string]interface{}, step map[string]interface{}) []map[string]interface{} {
	stepNum := asInt(step["step"])
	if stepNum == 0 {
		return nil
	}
	filtered := make([]map[string]interface{}, 0, 2)
	for _, hazard := range hazards {
		if asInt(hazard["step"]) == stepNum {
			filtered = append(filtered, hazard)
		}
	}
	return filtered
}

func auditExecutionRecordForStep(step map[string]interface{}, toolName string, policy auditStepExecutionPolicy, completed bool) map[string]interface{} {
	record := map[string]interface{}{
		"id":               auditStringValue(step["id"]),
		"tool":             toolName,
		"action":           policy.ActionKind,
		"mutability_class": policy.MutabilityClass,
		"hazard_class":     policy.HazardClass,
		"severity":         policy.Severity,
	}
	if completed {
		record["summary"] = step["result_summary"]
		if evidence := step["evidence"]; evidence != nil {
			record["evidence"] = evidence
		}
		if handles := toStringSlice(step["result_handles"]); len(handles) > 0 {
			record["evidence_handles"] = handles
		}
	} else {
		record["reason"] = strings.TrimSpace(fmt.Sprintf("%v", step["skip_reason"]))
	}
	return record
}

func auditExecutionStateFromPlan(plan []map[string]interface{}, status string) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(plan))
	for _, step := range plan {
		if auditStringValue(step["status"]) != status {
			continue
		}
		actionKind, _, _, _, _ := auditActionIdentityForStep(step, "")
		row := map[string]interface{}{
			"id":      auditStringValue(step["id"]),
			"tool":    auditStringValue(step["tool"]),
			"action":  actionKind,
			"summary": step["result_summary"],
		}
		if status == "skipped" {
			row["reason"] = step["skip_reason"]
		}
		if evidence := step["evidence"]; evidence != nil {
			row["evidence"] = evidence
		}
		if handles := toStringSlice(step["result_handles"]); len(handles) > 0 {
			row["evidence_handles"] = handles
		}
		rows = append(rows, row)
	}
	return rows
}

func auditExecutionNeedsApproval(plan []map[string]interface{}) bool {
	for _, step := range plan {
		status := auditStringValue(step["status"])
		if status == "" || status == "ready" || status == "deferred" {
			return true
		}
		if status == "skipped" && strings.HasPrefix(strings.TrimSpace(fmt.Sprintf("%v", step["skip_reason"])), "requires explicit allow_") {
			return true
		}
	}
	return false
}

func auditExecutionHandlesFromSteps(plan []map[string]interface{}) []string {
	handles := make([]string, 0, len(plan)*2)
	for _, step := range plan {
		handles = append(handles, toStringSlice(step["result_handles"])...)
	}
	return handles
}

func mergeAuditHandles(base, extra []string) []string {
	return uniqueStrings(append(append([]string{}, base...), extra...))
}

func copyAuditArgs(args map[string]interface{}) map[string]interface{} {
	if len(args) == 0 {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(args))
	for key, value := range args {
		out[key] = value
	}
	return out
}

func auditDefaultHandlesForStep(sessionID string, step map[string]interface{}, actionKind string) []string {
	if outputs := toStringSlice(step["outputs"]); len(outputs) > 0 {
		return outputs
	}
	switch strings.ToLower(strings.TrimSpace(actionKind)) {
	case "reveal_request_auth", "reveal_request_api_key", "reveal_request_payload":
		return []string{
			"audit:" + sessionID + ":failed_requests",
			"audit:" + sessionID + ":mangle_contracts",
		}
	case "reveal_frontend_contract":
		return []string{
			"audit:" + sessionID + ":repo_matches",
			"audit:" + sessionID + ":repo_trace",
		}
	case "reveal_contract_diff":
		return []string{
			"audit:" + sessionID + ":mangle_contracts",
			"audit:" + sessionID + ":contract_findings",
		}
	case "navigate_page_route", "navigate_history":
		return []string{
			"audit:" + sessionID + ":page_context",
			"audit:" + sessionID + ":contract_findings",
		}
	default:
		return []string{
			"audit:" + sessionID + ":contract_findings",
		}
	}
}

func resolveAuditNavigationURL(currentURL, targetRef, pageRoute string) (string, error) {
	target := strings.TrimSpace(targetRef)
	if target == "" {
		target = strings.TrimSpace(pageRoute)
	}
	if target == "" {
		return "", fmt.Errorf("navigate_page_route requires a target route")
	}
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target, nil
	}
	baseURL := strings.TrimSpace(currentURL)
	if baseURL == "" {
		return "", fmt.Errorf("navigate_page_route requires current_url to resolve relative route %q", target)
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse current_url %q: %w", currentURL, err)
	}
	ref, err := url.Parse(target)
	if err != nil {
		return "", fmt.Errorf("parse target route %q: %w", target, err)
	}
	return base.ResolveReference(ref).String(), nil
}

func auditDefaultExecutionSummary(step map[string]interface{}, policy auditStepExecutionPolicy) string {
	title := strings.TrimSpace(fmt.Sprintf("%v", step["title"]))
	if title != "" {
		return title
	}
	if policy.ActionKind != "" && policy.TargetRef != "" {
		return fmt.Sprintf("%s on %s", policy.ActionKind, policy.TargetRef)
	}
	if policy.ActionKind != "" {
		return policy.ActionKind
	}
	return strings.TrimSpace(fmt.Sprintf("%v", step["id"]))
}

func coalesceAuditRunStatus(status, fallback string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return fallback
	}
	return status
}

func auditSeverityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func auditStringValue(value interface{}) string {
	if value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	if text == "<nil>" {
		return ""
	}
	return text
}
