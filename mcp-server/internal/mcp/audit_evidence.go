package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

func buildAuditExecutionFacts(run *persistedAuditRun, completed, skipped []map[string]interface{}, ts time.Time) []mangle.Fact {
	if run == nil {
		return nil
	}

	facts := []mangle.Fact{{
		Predicate: "audit_plan_state",
		Args:      []interface{}{run.SessionID, run.AuditID, "execute", coalesceAuditRunStatus(run.Status, "resume_ready"), ts.UnixMilli()},
		Timestamp: ts,
	}, {
		Predicate: "scoped_audit_run",
		Args:      []interface{}{run.SessionID, run.AuditID, "contract_audit", ts.UnixMilli()},
		Timestamp: ts,
	}}
	for _, item := range completed {
		stepID := strings.TrimSpace(fmt.Sprintf("%v", item["id"]))
		facts = append(facts, mangle.Fact{
			Predicate: "audit_plan_step_completed",
			Args:      []interface{}{run.SessionID, run.AuditID, stepID, ts.UnixMilli()},
			Timestamp: ts,
		})
		if step := auditStepByID(run.Plan, stepID); step != nil {
			actionKind, targetRef, pageRoute, apiRoute, requestID := auditActionIdentityForStep(step, run.CurrentPath)
			facts = append(facts, mangle.Fact{
				Predicate: "scoped_audit_run_completed_action",
				Args:      []interface{}{run.SessionID, run.AuditID, actionKind, targetRef, pageRoute, apiRoute, requestID, ts.UnixMilli()},
				Timestamp: ts,
			})
		}
	}
	for _, item := range skipped {
		stepID := strings.TrimSpace(fmt.Sprintf("%v", item["id"]))
		reason := strings.TrimSpace(fmt.Sprintf("%v", item["reason"]))
		facts = append(facts, mangle.Fact{
			Predicate: "audit_plan_step_skipped",
			Args:      []interface{}{run.SessionID, run.AuditID, stepID, reason, ts.UnixMilli()},
			Timestamp: ts,
		})
		if step := auditStepByID(run.Plan, stepID); step != nil {
			actionKind, targetRef, pageRoute, apiRoute, requestID := auditActionIdentityForStep(step, run.CurrentPath)
			facts = append(facts, mangle.Fact{
				Predicate: "scoped_audit_run_skipped_action",
				Args:      []interface{}{run.SessionID, run.AuditID, actionKind, targetRef, pageRoute, apiRoute, requestID, reason, ts.UnixMilli()},
				Timestamp: ts,
			})
		}
	}
	return facts
}

func auditStepByID(plan []map[string]interface{}, stepID string) map[string]interface{} {
	for _, step := range plan {
		if auditStringValue(step["id"]) == stepID {
			return step
		}
	}
	return nil
}

func countPendingAuditSteps(plan []map[string]interface{}) int {
	count := 0
	for _, step := range plan {
		status := strings.TrimSpace(fmt.Sprintf("%v", step["status"]))
		if status == "" || status == "ready" || status == "deferred" {
			count++
		}
	}
	return count
}

func captureAuditStepEvidence(engine *mangle.Engine, sessionID string, since time.Time) map[string]interface{} {
	if engine == nil {
		return nil
	}

	sinceMs := since.UnixMilli()
	evidence := map[string]interface{}{}

	if currentURLRows := queryToRows(context.Background(), engine, fmt.Sprintf(`current_url(%q, Url).`, sessionID)); len(currentURLRows) > 0 {
		evidence["current_url"] = currentURLRows[0]["Url"]
	}

	requestIDs := make([]string, 0, 8)
	for _, fact := range engine.FactsByPredicate("net_request") {
		if len(fact.Args) < 6 || fmt.Sprintf("%v", fact.Args[0]) != sessionID {
			continue
		}
		if fact.Timestamp.UnixMilli() >= sinceMs {
			requestIDs = append(requestIDs, strings.TrimSpace(fmt.Sprintf("%v", fact.Args[1])))
		}
	}
	if len(requestIDs) > 0 {
		evidence["request_ids"] = uniqueStrings(requestIDs)
	}

	toasts := make([]map[string]interface{}, 0, 4)
	for _, fact := range engine.FactsByPredicate("toast_notification") {
		if len(fact.Args) < 5 || fmt.Sprintf("%v", fact.Args[0]) != sessionID {
			continue
		}
		if fact.Timestamp.UnixMilli() >= sinceMs {
			toasts = append(toasts, map[string]interface{}{
				"text":      fact.Args[1],
				"level":     fact.Args[2],
				"source":    fact.Args[3],
				"timestamp": fact.Args[4],
			})
		}
	}
	if len(toasts) > 0 {
		evidence["toasts"] = toasts
	}

	consoleEvents := make([]map[string]interface{}, 0, 4)
	for _, fact := range engine.FactsByPredicate("console_event") {
		if len(fact.Args) < 4 || fmt.Sprintf("%v", fact.Args[0]) != sessionID {
			continue
		}
		if fact.Timestamp.UnixMilli() >= sinceMs {
			consoleEvents = append(consoleEvents, map[string]interface{}{
				"level":     fact.Args[1],
				"message":   fact.Args[2],
				"timestamp": fact.Args[3],
			})
		}
	}
	if len(consoleEvents) > 0 {
		evidence["console_events"] = consoleEvents
	}

	if len(evidence) == 0 {
		return nil
	}
	return evidence
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func (t *BrowserAuditTool) resolveInteractiveRefForStep(ctx context.Context, sessionID string, step map[string]interface{}) (string, error) {
	if t.sessions == nil {
		return "", fmt.Errorf("interactive resolution requires an active session manager")
	}
	if ref := auditStringValue(step["ref"]); ref != "" && !strings.HasPrefix(ref, "/") {
		return ref, nil
	}

	result, err := (&GetInteractiveElementsTool{sessions: t.sessions, engine: t.engine}).Execute(ctx, map[string]interface{}{
		"session_id":   sessionID,
		"filter":       "all",
		"visible_only": true,
	})
	if err != nil {
		return "", err
	}
	resultMap, _ := result.(map[string]interface{})
	elements := toMapSlice(resultMap["elements"])
	label := strings.ToLower(strings.TrimSpace(auditStringValue(step["label"])))
	target := strings.ToLower(strings.TrimSpace(auditStringValue(step["ref"])))
	for _, element := range elements {
		elementRef := strings.TrimSpace(fmt.Sprintf("%v", element["ref"]))
		elementLabel := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", element["label"])))
		if elementRef == "" {
			continue
		}
		if label != "" && elementLabel == label {
			return elementRef, nil
		}
		if target != "" && elementLabel == target {
			return elementRef, nil
		}
	}
	return "", fmt.Errorf("unable to resolve interactive ref for step %q", auditStringValue(step["id"]))
}

func updateAuditRunLocation(currentURL, currentPath string, evidence map[string]interface{}) (string, string) {
	if evidence == nil {
		return currentURL, currentPath
	}
	if routeChange, ok := evidence["route_change"].(map[string]interface{}); ok {
		if toURL := strings.TrimSpace(fmt.Sprintf("%v", routeChange["to_url"])); toURL != "" {
			return toURL, canonicalAuditPath(toURL)
		}
	}
	if updatedURL := strings.TrimSpace(fmt.Sprintf("%v", evidence["current_url"])); updatedURL != "" {
		return updatedURL, canonicalAuditPath(updatedURL)
	}
	return currentURL, currentPath
}

func appendRecursiveAuditSteps(plan []map[string]interface{}, sessionID string, report *browser.RepoTraceReport) []map[string]interface{} {
	if report == nil || len(report.AuditPlan) == 0 {
		return plan
	}
	existing := make(map[string]bool, len(plan))
	for _, step := range plan {
		existing[auditPlanStepKey(step)] = true
	}
	currentRoute := canonicalAuditPath(report.BrowserContext.CurrentPath)

	for _, traceStep := range report.AuditPlan {
		if traceStep.Kind == "capture_context" {
			continue
		}

		switch traceStep.Kind {
		case "map_navigation":
			link, hasLink := findAuditNavigationLink(report.BrowserContext.NavigationLinks, traceStep)
			targetRoute := canonicalAuditPath(firstNonEmpty(traceStep.Path, link.Path))
			targetLabel := firstNonEmpty(traceStep.Target, link.Label, targetRoute, link.Href)
			if hasLink && targetRoute != "" && link.LinkType == "internal" && targetRoute != currentRoute {
				plan, existing = appendUniqueAuditPlanStep(plan, existing, map[string]interface{}{
					"id":               "navigate-" + auditPlanSlug(targetRoute),
					"title":            fmt.Sprintf("Navigate to %s", targetLabel),
					"action":           "navigate_page_route",
					"ref":              targetRoute,
					"label":            targetLabel,
					"route":            targetRoute,
					"path":             targetRoute,
					"status":           "deferred",
					"risky":            false,
					"mutability_class": "non_mutating",
					"hazard_class":     "non_mutating_navigation",
					"severity":         traceNavigationSeverity(link.AuthSensitive),
					"reason":           traceStep.Summary,
					"outputs": []string{
						"audit:" + sessionID + ":page_context",
						"audit:" + sessionID + ":fact_summary",
						"audit:" + sessionID + ":search_specs",
					},
				})
				plan, existing = appendUniqueAuditPlanStep(plan, existing, map[string]interface{}{
					"id":               "observe-" + auditPlanSlug(targetRoute),
					"title":            fmt.Sprintf("Observe route state for %s", targetLabel),
					"tool":             "browser-observe",
					"action":           "reveal_page_context",
					"ref":              targetLabel,
					"label":            targetLabel,
					"route":            targetRoute,
					"path":             targetRoute,
					"status":           "deferred",
					"risky":            false,
					"mutability_class": "non_mutating",
					"hazard_class":     "non_mutating_reveal",
					"severity":         traceNavigationSeverity(link.AuthSensitive),
					"reason":           fmt.Sprintf("Capture route state after navigating to %s.", targetLabel),
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
				continue
			}

			plan, existing = appendUniqueAuditPlanStep(plan, existing, map[string]interface{}{
				"id":               "review-navigation-" + auditPlanSlug(targetLabel),
				"title":            fmt.Sprintf("Review navigation target %s", targetLabel),
				"action":           "reveal_navigation_target",
				"ref":              firstNonEmpty(targetRoute, link.Href, targetLabel),
				"label":            targetLabel,
				"route":            currentRoute,
				"path":             targetRoute,
				"status":           "deferred",
				"risky":            false,
				"mutability_class": "non_mutating",
				"hazard_class":     "non_mutating_reveal",
				"severity":         traceNavigationSeverity(link.AuthSensitive),
				"reason":           traceStep.Summary,
				"outputs": []string{
					"audit:" + sessionID + ":page_context",
				},
			})
		case "inspect_form":
			form, _ := findAuditFormHint(report.BrowserContext.Forms, traceStep)
			apiRoute, route := splitAuditTracePath(firstNonEmpty(traceStep.Path, form.Action), currentRoute)
			targetLabel := firstNonEmpty(strings.Join(form.SubmitLabels, " / "), traceStep.Target, form.Action, apiRoute, route)
			plan, existing = appendUniqueAuditPlanStep(plan, existing, map[string]interface{}{
				"id":               "review-form-" + auditPlanSlug(targetLabel),
				"title":            firstNonEmpty(traceStep.Summary, fmt.Sprintf("Review form %s", targetLabel)),
				"tool":             "browser-observe",
				"action":           "reveal_form_contract",
				"ref":              targetLabel,
				"label":            targetLabel,
				"route":            route,
				"api_route":        apiRoute,
				"path":             firstNonEmpty(apiRoute, route),
				"status":           "deferred",
				"risky":            false,
				"mutability_class": "non_mutating",
				"hazard_class":     "non_mutating_reveal",
				"severity":         firstAuditSeverity(traceStep.HazardTypes),
				"reason":           appendAuditTraceDetails(traceStep.Summary, "fields", form.Fields),
				"args": map[string]interface{}{
					"session_id":          sessionID,
					"mode":                auditObserveModeForTraceStep(traceStep),
					"view":                "compact",
					"include_action_plan": true,
				},
				"outputs": []string{
					"audit:" + sessionID + ":page_context",
					"audit:" + sessionID + ":fact_summary",
					"audit:" + sessionID + ":contract_findings",
					"audit:" + sessionID + ":failed_requests",
				},
			})
		case "review_control":
			control, _ := findAuditControlHint(report.BrowserContext.Controls, traceStep)
			apiRoute, route := splitAuditTracePath(firstNonEmpty(traceStep.Path, control.Path, control.FormAction), currentRoute)
			targetLabel := firstNonEmpty(control.Label, control.Name, traceStep.Target, apiRoute, route)
			plan, existing = appendUniqueAuditPlanStep(plan, existing, map[string]interface{}{
				"id":               "review-control-" + auditPlanSlug(targetLabel),
				"title":            firstNonEmpty(traceStep.Summary, fmt.Sprintf("Review control %s", targetLabel)),
				"tool":             "browser-observe",
				"action":           "reveal_control_surface",
				"ref":              targetLabel,
				"label":            targetLabel,
				"route":            route,
				"api_route":        apiRoute,
				"path":             firstNonEmpty(apiRoute, route),
				"status":           "deferred",
				"risky":            false,
				"mutability_class": "non_mutating",
				"hazard_class":     "non_mutating_reveal",
				"severity":         firstAuditSeverity(traceStep.HazardTypes),
				"reason":           appendAuditTraceDetails(traceStep.Summary, "signals", control.HazardReasons),
				"args": map[string]interface{}{
					"session_id":          sessionID,
					"mode":                auditObserveModeForTraceStep(traceStep),
					"view":                "compact",
					"include_action_plan": true,
				},
				"outputs": []string{
					"audit:" + sessionID + ":page_context",
					"audit:" + sessionID + ":fact_summary",
					"audit:" + sessionID + ":contract_findings",
				},
			})
		}
	}
	return plan
}

func appendUniqueAuditPlanStep(plan []map[string]interface{}, existing map[string]bool, step map[string]interface{}) ([]map[string]interface{}, map[string]bool) {
	key := auditPlanStepKey(step)
	if key == "" || existing[key] {
		return plan, existing
	}
	existing[key] = true
	return appendAuditPlanStep(plan, step), existing
}

func auditPlanStepKey(step map[string]interface{}) string {
	return strings.Join([]string{
		auditStringValue(step["tool"]),
		auditStringValue(step["action"]),
		auditStringValue(step["ref"]),
		auditStringValue(step["route"]),
		auditStringValue(step["api_route"]),
		auditStringValue(step["path"]),
	}, "|")
}

func mergeAuditHazardSets(base, extra []map[string]interface{}) []map[string]interface{} {
	merged := make([]map[string]interface{}, 0, len(base)+len(extra))
	seen := make(map[string]bool)
	appendSet := func(items []map[string]interface{}) {
		for _, item := range items {
			key := strings.Join([]string{
				strings.TrimSpace(fmt.Sprintf("%v", item["hazard"])),
				strings.TrimSpace(fmt.Sprintf("%v", item["route"])),
				strings.TrimSpace(fmt.Sprintf("%v", item["source"])),
				strings.TrimSpace(fmt.Sprintf("%v", item["message"])),
			}, "|")
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, item)
		}
	}
	appendSet(base)
	appendSet(extra)
	return merged
}

func buildRecursiveAuditHazards(plan []map[string]interface{}, report *browser.RepoTraceReport) []map[string]interface{} {
	if report == nil {
		return nil
	}
	reviewStep := findAuditPlanStepNumberByAction(plan, "reveal_form_contract", "reveal_control_surface", "reveal_navigation_target")
	if reviewStep == 0 {
		reviewStep = 1
	}
	navStep := findAuditPlanStepNumberByAction(plan, "navigate_page_route")
	if navStep == 0 {
		navStep = reviewStep
	}
	hazards := make([]map[string]interface{}, 0, 4)
	if report.HazardSummary.WriteCapableForms > 0 || report.HazardSummary.WriteCapableControls > 0 {
		hazards = append(hazards, map[string]interface{}{
			"hazard":   "repo_trace_write_surfaces",
			"severity": "medium",
			"step":     reviewStep,
			"source":   "repo_trace",
			"message":  fmt.Sprintf("Recursive discover planning found %d write-capable forms and %d write-capable controls to review without activation.", report.HazardSummary.WriteCapableForms, report.HazardSummary.WriteCapableControls),
			"risky":    false,
		})
	}
	if report.HazardSummary.DestructiveForms > 0 || report.HazardSummary.DestructiveControls > 0 {
		hazards = append(hazards, map[string]interface{}{
			"hazard":   "repo_trace_destructive_surfaces",
			"severity": "high",
			"step":     reviewStep,
			"source":   "repo_trace",
			"message":  fmt.Sprintf("Recursive discover planning found %d destructive forms and %d destructive controls, so the plan stays review-only until explicitly approved.", report.HazardSummary.DestructiveForms, report.HazardSummary.DestructiveControls),
			"risky":    false,
		})
	}
	if report.HazardSummary.AuthSensitiveForms > 0 || report.HazardSummary.AuthSensitiveControls > 0 || report.HazardSummary.AuthSensitiveNavigationLinks > 0 {
		hazards = append(hazards, map[string]interface{}{
			"hazard":   "repo_trace_auth_sensitive_surfaces",
			"severity": "medium",
			"step":     navStep,
			"source":   "repo_trace",
			"message":  fmt.Sprintf("Recursive discover planning found %d auth-sensitive forms, %d auth-sensitive controls, and %d auth-sensitive navigation links.", report.HazardSummary.AuthSensitiveForms, report.HazardSummary.AuthSensitiveControls, report.HazardSummary.AuthSensitiveNavigationLinks),
			"risky":    false,
		})
	}
	return hazards
}

func traceNavigationSeverity(authSensitive bool) string {
	if authSensitive {
		return "medium"
	}
	return "low"
}

func appendAuditTraceDetails(summary, label string, values []string) string {
	values = uniqueStrings(values)
	if len(values) == 0 {
		return summary
	}
	return fmt.Sprintf("%s %s=%s.", strings.TrimSpace(summary), label, strings.Join(values, ", "))
}

func findAuditNavigationLink(links []browser.RepoTraceNavigationLink, traceStep browser.RepoTracePlanStep) (browser.RepoTraceNavigationLink, bool) {
	targetPath := canonicalAuditPath(traceStep.Path)
	targetLabel := strings.TrimSpace(traceStep.Target)
	for _, link := range links {
		if targetPath != "" && canonicalAuditPath(link.Path) == targetPath {
			return link, true
		}
	}
	for _, link := range links {
		if targetLabel != "" && strings.EqualFold(strings.TrimSpace(link.Label), targetLabel) {
			return link, true
		}
	}
	return browser.RepoTraceNavigationLink{}, false
}

func findAuditFormHint(forms []browser.RepoTraceFormHint, traceStep browser.RepoTracePlanStep) (browser.RepoTraceFormHint, bool) {
	targetPath := canonicalAuditPath(traceStep.Path)
	method := strings.ToUpper(strings.TrimSpace(traceStep.Method))
	for _, form := range forms {
		formPath := canonicalAuditPath(firstNonEmpty(form.Action, targetPath))
		if targetPath != "" && formPath != targetPath {
			continue
		}
		if method != "" && strings.ToUpper(strings.TrimSpace(form.Method)) != method {
			continue
		}
		return form, true
	}
	return browser.RepoTraceFormHint{}, false
}

func findAuditControlHint(controls []browser.RepoTraceControlHint, traceStep browser.RepoTracePlanStep) (browser.RepoTraceControlHint, bool) {
	targetPath := canonicalAuditPath(traceStep.Path)
	targetLabel := strings.TrimSpace(traceStep.Target)
	for _, control := range controls {
		controlPath := canonicalAuditPath(firstNonEmpty(control.Path, control.FormAction, control.Href))
		if targetPath != "" && controlPath == targetPath {
			return control, true
		}
		if targetLabel != "" && (strings.EqualFold(strings.TrimSpace(control.Label), targetLabel) || strings.EqualFold(strings.TrimSpace(control.Name), targetLabel)) {
			return control, true
		}
	}
	return browser.RepoTraceControlHint{}, false
}

func splitAuditTracePath(rawPath, currentRoute string) (string, string) {
	path := canonicalAuditPath(rawPath)
	route := currentRoute
	if path == "" {
		return "", route
	}
	if strings.HasPrefix(path, "/api/") {
		return path, route
	}
	return "", path
}

func auditPlanSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "step"
	}
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", ":", "-", ".", "-", "#", "-", "?", "-", "&", "-", "=", "-", "\"", "", "'", "")
	value = replacer.Replace(value)
	value = strings.Trim(value, "-")
	if value == "" {
		return "step"
	}
	return value
}
