package mcp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/mangle"
)

func buildAuditPlanFacts(sessionID, auditID, phase, status string, plan, hazards []map[string]interface{}) []mangle.Fact {
	now := time.Now()
	facts := []mangle.Fact{{
		Predicate: "audit_plan_state",
		Args:      []interface{}{sessionID, auditID, phase, status, now.UnixMilli()},
		Timestamp: now,
	}, {
		Predicate: "scoped_audit_run",
		Args:      []interface{}{sessionID, auditID, "contract_audit", now.UnixMilli()},
		Timestamp: now,
	}}

	for _, step := range plan {
		facts = append(facts, mangle.Fact{
			Predicate: "audit_plan_step",
			Args: []interface{}{
				sessionID,
				auditID,
				asInt(step["step"]),
				strings.TrimSpace(fmt.Sprintf("%v", step["id"])),
				strings.TrimSpace(fmt.Sprintf("%v", step["tool"])),
				fmt.Sprintf("%v", step["risky"]),
			},
			Timestamp: now,
		}, mangle.Fact{
			Predicate: "audit_discovered_action",
			Args: []interface{}{
				sessionID,
				auditID,
				asInt(step["step"]),
				strings.TrimSpace(fmt.Sprintf("%v", step["ref"])),
				strings.TrimSpace(fmt.Sprintf("%v", step["action"])),
				strings.TrimSpace(fmt.Sprintf("%v", step["label"])),
				strings.TrimSpace(fmt.Sprintf("%v", step["route"])),
			},
			Timestamp: now,
		}, mangle.Fact{
			Predicate: "scoped_audit_proposed_action",
			Args: []interface{}{
				sessionID,
				auditID,
				strings.TrimSpace(fmt.Sprintf("%v", step["action"])),
				strings.TrimSpace(fmt.Sprintf("%v", step["ref"])),
				strings.TrimSpace(fmt.Sprintf("%v", step["route"])),
				strings.TrimSpace(fmt.Sprintf("%v", step["api_route"])),
				strings.TrimSpace(fmt.Sprintf("%v", step["request_id"])),
				strings.TrimSpace(fmt.Sprintf("%v", step["reason"])),
			},
			Timestamp: now,
		})
	}

	for _, hazard := range hazards {
		facts = append(facts, mangle.Fact{
			Predicate: "audit_hazard_fact",
			Args: []interface{}{
				sessionID,
				auditID,
				asInt(hazard["step"]),
				strings.TrimSpace(fmt.Sprintf("%v", hazard["hazard"])),
				strings.TrimSpace(fmt.Sprintf("%v", hazard["severity"])),
			},
			Timestamp: now,
		})
	}

	return facts
}

type auditFindingClassification struct {
	kind          string
	message       string
	severityScore int
	confidence    float64
}

func classifyAuditFinding(
	status int,
	correlation map[string]interface{},
	fullStack map[string]interface{},
	matches []map[string]interface{},
	userVisibleError string,
) auditFindingClassification {
	switch {
	case fullStack != nil || correlation != nil:
		return auditFindingClassification{
			kind:          "backend_contract_break",
			message:       "API failure correlates to a backend exception visible from the browser session",
			severityScore: 97,
			confidence:    0.96,
		}
	case status == 404 && len(matches) > 0:
		return auditFindingClassification{
			kind:          "endpoint_drift",
			message:       "Frontend code still references an endpoint that now returns 404",
			severityScore: 92,
			confidence:    0.91,
		}
	case status == 404:
		return auditFindingClassification{
			kind:          "missing_endpoint",
			message:       "Observed 404 with no strong frontend reference match; endpoint may be missing or renamed",
			severityScore: 84,
			confidence:    0.74,
		}
	case status == 401 || status == 403:
		return auditFindingClassification{
			kind:          "auth_contract_block",
			message:       "API contract is blocked by an auth or permission failure",
			severityScore: 86,
			confidence:    0.87,
		}
	case looksLikeAuditResponseMismatch(userVisibleError):
		return auditFindingClassification{
			kind:          "response_shape_mismatch",
			message:       "Frontend error signature suggests a stale response shape or parsing contract",
			severityScore: 88,
			confidence:    0.82,
		}
	case status >= 500:
		return auditFindingClassification{
			kind:          "api_failure",
			message:       "The page is hitting a server-side API failure without a matched backend trace yet",
			severityScore: 83,
			confidence:    0.72,
		}
	default:
		return auditFindingClassification{
			kind:          "contract_signal",
			message:       "The session contains an API contract signal worth tracing further",
			severityScore: 72,
			confidence:    0.64,
		}
	}
}

func uniqueAuditFiles(matches []map[string]interface{}, max int) []string {
	if len(matches) == 0 {
		return []string{}
	}
	if max <= 0 {
		max = len(matches)
	}

	files := make([]string, 0, minInt(len(matches), max))
	seen := make(map[string]bool, max)
	for _, match := range matches {
		file := strings.TrimSpace(fmt.Sprintf("%v", match["file"]))
		if file == "" || seen[file] {
			continue
		}
		seen[file] = true
		files = append(files, file)
		if len(files) >= max {
			break
		}
	}
	return files
}

func looksLikeAuditResponseMismatch(message string) bool {
	fingerprint := normalizeErrorFingerprint(message)
	if fingerprint == "" {
		return false
	}

	signals := []string{
		"unexpected token",
		"cannot read properties",
		"is not iterable",
		"is not a function",
		"undefined",
		"json",
	}
	for _, signal := range signals {
		if strings.Contains(fingerprint, signal) {
			return true
		}
	}
	return false
}

func buildAuditStatus(findings []map[string]interface{}) string {
	if len(findings) == 0 {
		return "ok"
	}
	topScore := asInt(findings[0]["severity_score"])
	switch {
	case topScore >= 90:
		return "error"
	case topScore >= 75:
		return "warning"
	default:
		return "ok"
	}
}

func computeAuditConfidence(findings []map[string]interface{}, pageContext map[string]interface{}) float64 {
	base := 0.48
	if strings.TrimSpace(fmt.Sprintf("%v", pageContext["current_url"])) != "" {
		base += 0.12
	}
	if found, _ := pageContext["session_found"].(bool); found {
		base += 0.08
	}

	if len(findings) == 0 {
		if base > 0.88 {
			return 0.88
		}
		return base
	}

	total := 0.0
	for idx, finding := range findings {
		if idx >= 3 {
			break
		}
		total += auditConfidenceValue(finding["confidence"])
	}
	average := total / float64(minInt(len(findings), 3))
	score := (base + average) / 2
	if score < 0.10 {
		return 0.10
	}
	if score > 0.99 {
		return 0.99
	}
	return score
}

func auditConfidenceValue(raw interface{}) float64 {
	switch value := raw.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func buildAuditSummary(status string, confidence float64, findings, failedRequests, correlations, repoMatches int) string {
	return fmt.Sprintf(
		"status=%s confidence=%.2f findings=%d failed_requests=%d correlations=%d repo_matches=%d",
		status,
		confidence,
		findings,
		failedRequests,
		correlations,
		repoMatches,
	)
}

func buildAuditNextStep(sessionID string, findings []map[string]interface{}) map[string]interface{} {
	if len(findings) == 0 {
		return map[string]interface{}{
			"tool": "browser-reason",
			"args": map[string]interface{}{
				"session_id": sessionID,
				"topic":      "health",
				"view":       "compact",
			},
		}
	}

	kind := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", findings[0]["kind"])))
	switch kind {
	case "backend_contract_break":
		return map[string]interface{}{
			"tool": "browser-mangle",
			"args": map[string]interface{}{
				"operation": "query",
				"query":     fmt.Sprintf("api_backend_correlation(%q, ReqId, Url, Status, BackendMsg, TimeDelta).", sessionID),
				"view":      "compact",
				"max_items": 20,
			},
		}
	case "endpoint_drift", "missing_endpoint":
		return map[string]interface{}{
			"tool": "browser-mangle",
			"args": map[string]interface{}{
				"operation": "query",
				"query":     fmt.Sprintf("failed_request_at(%q, ReqId, Url, Status, ReqTs).", sessionID),
				"view":      "compact",
				"max_items": 20,
			},
		}
	default:
		return map[string]interface{}{
			"tool": "browser-reason",
			"args": map[string]interface{}{
				"session_id": sessionID,
				"topic":      "why_failed",
				"view":       "compact",
			},
		}
	}
}

func truncateAuditData(data map[string]interface{}, maxItems int) map[string]interface{} {
	out := make(map[string]interface{}, len(data))
	for key, value := range data {
		switch rows := value.(type) {
		case []map[string]interface{}:
			out[key] = limitMapSlice(rows, maxItems)
		default:
			out[key] = value
		}
	}
	return out
}

func applyAuditHandleFilter(data map[string]interface{}, rawHandles interface{}) map[string]interface{} {
	handles := toStringSlice(rawHandles)
	if len(handles) == 0 {
		return data
	}

	selected := map[string]bool{}
	for _, handle := range handles {
		value := strings.ToLower(strings.TrimSpace(handle))
		switch {
		case strings.Contains(value, "page_context"):
			selected["page_context"] = true
		case strings.Contains(value, "fact_summary"):
			selected["fact_summary"] = true
		case strings.Contains(value, "search_specs"):
			selected["search_specs"] = true
		case strings.Contains(value, "audit_plan"):
			selected["audit_plan"] = true
		case strings.Contains(value, "audit_hazards"):
			selected["audit_hazards"] = true
		case strings.Contains(value, "contract_findings"):
			selected["contract_findings"] = true
		case strings.Contains(value, "failed_requests"):
			selected["failed_requests"] = true
		case strings.Contains(value, "api_correlations"):
			selected["api_correlations"] = true
		case strings.Contains(value, "full_stack_errors"):
			selected["full_stack_errors"] = true
		case strings.Contains(value, "user_visible_errors"):
			selected["user_visible_errors"] = true
		case strings.Contains(value, "repo_matches"):
			selected["repo_matches"] = true
		case strings.Contains(value, "mangle_contracts"):
			selected["mangle_contracts"] = true
		case strings.Contains(value, "repo_trace"):
			selected["repo_trace"] = true
		}
	}

	if len(selected) == 0 {
		return data
	}

	filtered := make(map[string]interface{}, len(selected))
	for key, value := range data {
		if selected[key] {
			filtered[key] = value
		}
	}
	return filtered
}
