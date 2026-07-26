package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

func buildContractAuditFindings(
	sessionID string,
	currentURL string,
	failedRequests []map[string]interface{},
	apiCorrelations []map[string]interface{},
	fullStackErrors []map[string]interface{},
	userVisibleErrors []map[string]interface{},
	repoMatches []map[string]interface{},
	maxItems int,
) []map[string]interface{} {
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}

	repoByKey := make(map[string][]map[string]interface{})
	for _, match := range repoMatches {
		key := strings.TrimSpace(fmt.Sprintf("%v", match["contract_key"]))
		if key == "" {
			continue
		}
		repoByKey[key] = append(repoByKey[key], match)
	}

	correlationByReqID := make(map[string]map[string]interface{}, len(apiCorrelations))
	for _, row := range apiCorrelations {
		reqID := strings.TrimSpace(fmt.Sprintf("%v", row["ReqId"]))
		if reqID != "" {
			correlationByReqID[reqID] = row
		}
	}

	fullStackByReqID := make(map[string]map[string]interface{}, len(fullStackErrors))
	for _, row := range fullStackErrors {
		reqID := strings.TrimSpace(fmt.Sprintf("%v", row["ReqId"]))
		if reqID != "" {
			fullStackByReqID[reqID] = row
		}
	}

	primaryVisibleError := ""
	if len(userVisibleErrors) > 0 {
		primaryVisibleError = strings.TrimSpace(fmt.Sprintf("%v", userVisibleErrors[0]["Message"]))
	}

	findings := make([]map[string]interface{}, 0, len(failedRequests))
	pageRoute := canonicalAuditPath(currentURL)

	for _, request := range failedRequests {
		reqID := strings.TrimSpace(fmt.Sprintf("%v", request["ReqId"]))
		endpoint := strings.TrimSpace(fmt.Sprintf("%v", request["Path"]))
		if endpoint == "" {
			endpoint = canonicalAuditPath(strings.TrimSpace(fmt.Sprintf("%v", request["Url"])))
		}
		status := asInt(request["Status"])
		method := strings.TrimSpace(fmt.Sprintf("%v", request["Method"]))
		if method == "" {
			method = "GET"
		}
		ts := extractTimestamp(request, "ReqTs", "StartTime", "Timestamp")
		correlation := correlationByReqID[reqID]
		fullStack := fullStackByReqID[reqID]
		matches := repoByKey[endpoint]

		classification := classifyAuditFinding(status, correlation, fullStack, matches, primaryVisibleError)
		candidateFiles := uniqueAuditFiles(matches, 3)

		finding := map[string]interface{}{
			"kind":             classification.kind,
			"message":          classification.message,
			"severity":         severityLabel(classification.severityScore),
			"severity_score":   classification.severityScore,
			"confidence":       classification.confidence,
			"request_id":       reqID,
			"method":           method,
			"url":              request["Url"],
			"endpoint":         endpoint,
			"endpoint_pattern": buildAuditPathPatternDisplay(endpoint),
			"status":           status,
			"timestamp":        ts,
			"repo_match_count": len(matches),
			"candidate_files":  candidateFiles,
			"page_route":       pageRoute,
			"evidence_handles": []string{
				"audit:" + sessionID + ":contract_findings",
				"audit:" + sessionID + ":failed_requests",
				"audit:" + sessionID + ":repo_matches",
			},
		}
		if len(matches) > 0 {
			finding["source_matches"] = limitMapSlice(matches, minInt(3, len(matches)))
		}
		if correlation != nil {
			finding["backend_message"] = correlation["BackendMsg"]
			finding["correlation_delta_ms"] = correlation["TimeDelta"]
		}
		if fullStack != nil {
			finding["console_message"] = fullStack["ConsoleMsg"]
			if finding["backend_message"] == nil {
				finding["backend_message"] = fullStack["BackendMsg"]
			}
		}
		if primaryVisibleError != "" {
			finding["user_visible_error"] = primaryVisibleError
		}

		findings = append(findings, finding)
	}

	sort.SliceStable(findings, func(i, j int) bool {
		scoreI := asInt(findings[i]["severity_score"])
		scoreJ := asInt(findings[j]["severity_score"])
		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		tsI := extractTimestamp(findings[i], "timestamp", "Timestamp")
		tsJ := extractTimestamp(findings[j], "timestamp", "Timestamp")
		if tsI != tsJ {
			return tsI > tsJ
		}
		return strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", findings[i]["message"]))) <
			strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", findings[j]["message"])))
	})

	return limitMapSlice(findings, maxItems)
}

type observedAuditRequest struct {
	ID      string
	Method  string
	Route   string
	RawURL  string
	Headers map[string]string
}

func queryObservedAuditRequests(engine *mangle.Engine, sessionID string, sinceMs int64) []observedAuditRequest {
	if engine == nil {
		return nil
	}

	headerFacts := engine.FactsByPredicate("net_header")
	headersByRequest := make(map[string]map[string]string)
	for _, fact := range headerFacts {
		if len(fact.Args) < 5 || fmt.Sprintf("%v", fact.Args[0]) != sessionID {
			continue
		}
		kind := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", fact.Args[2])))
		if kind != "req" && kind != "request" {
			continue
		}
		reqID := strings.TrimSpace(fmt.Sprintf("%v", fact.Args[1]))
		if reqID == "" {
			continue
		}
		if headersByRequest[reqID] == nil {
			headersByRequest[reqID] = make(map[string]string)
		}
		headersByRequest[reqID][strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", fact.Args[3])))] =
			strings.TrimSpace(fmt.Sprintf("%v", fact.Args[4]))
	}

	requestFacts := engine.FactsByPredicate("net_request")
	requests := make([]observedAuditRequest, 0, len(requestFacts))
	for _, fact := range requestFacts {
		if len(fact.Args) < 6 || fmt.Sprintf("%v", fact.Args[0]) != sessionID {
			continue
		}
		if sinceMs > 0 && fact.Timestamp.UnixMilli() < sinceMs {
			continue
		}
		reqID := strings.TrimSpace(fmt.Sprintf("%v", fact.Args[1]))
		method := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", fact.Args[2])))
		rawURL := strings.TrimSpace(fmt.Sprintf("%v", fact.Args[3]))
		route := canonicalAuditPath(rawURL)
		if reqID == "" || route == "" {
			continue
		}
		requests = append(requests, observedAuditRequest{
			ID:      reqID,
			Method:  method,
			Route:   route,
			RawURL:  rawURL,
			Headers: headersByRequest[reqID],
		})
	}
	return requests
}

func buildScopedContractFactsFromRepoTrace(repoRoot, sessionID, currentURL string, report *browser.RepoTraceReport, observedRequests []observedAuditRequest) []mangle.Fact {
	if report == nil {
		return nil
	}

	now := time.Now()
	pageRoute := canonicalAuditPath(currentURL)
	if pageRoute == "" {
		pageRoute = report.BrowserContext.CurrentPath
	}
	facts := make([]mangle.Fact, 0, len(report.FrontendSites)*4+len(report.BackendMatches)*3)
	seen := make(map[string]bool)

	for _, site := range report.FrontendSites {
		method := strings.ToUpper(strings.TrimSpace(site.Method))
		if method == "" {
			method = "GET"
		}
		route := canonicalAuditPath(site.Endpoint)
		if route == "" {
			continue
		}
		authMechanism, payloadFields := inspectFrontendContractSnippet(repoRoot, site)
		key := strings.Join([]string{"front", site.ID, pageRoute, route, method, authMechanism}, "|")
		if !seen[key] {
			facts = append(facts, mangle.Fact{
				Predicate: "scoped_frontend_api_contract",
				Args:      []interface{}{sessionID, site.ID, pageRoute, route, method, authMechanism},
				Timestamp: now,
			})
			seen[key] = true
		}
		for field := range payloadFields {
			key = strings.Join([]string{"front-payload", site.ID, route, method, field}, "|")
			if !seen[key] {
				facts = append(facts, mangle.Fact{
					Predicate: "scoped_frontend_payload_requirement",
					Args:      []interface{}{sessionID, site.ID, pageRoute, route, method, field, "required"},
					Timestamp: now,
				})
				seen[key] = true
			}
		}

		for _, request := range matchedAuditRequests(route, method, observedRequests) {
			key = strings.Join([]string{"action-link", site.ID, request.ID}, "|")
			if !seen[key] {
				facts = append(facts, mangle.Fact{
					Predicate: "scoped_action_request_link",
					Args:      []interface{}{sessionID, site.ID, request.ID},
					Timestamp: now,
				})
				seen[key] = true
			}
			for field := range payloadFields {
				key = strings.Join([]string{"request-payload", request.ID, field}, "|")
				if !seen[key] {
					facts = append(facts, mangle.Fact{
						Predicate: "scoped_request_payload_field",
						Args:      []interface{}{sessionID, request.ID, field, "static_trace"},
						Timestamp: now,
					})
					seen[key] = true
				}
			}
		}
	}

	for _, match := range report.BackendMatches {
		method := strings.ToUpper(strings.TrimSpace(match.Method))
		if method == "" {
			method = "GET"
		}
		route := canonicalAuditPath(match.RoutePath)
		if route == "" {
			continue
		}
		for _, mechanism := range parseBackendAuthExpectation(match.AuthExpectation) {
			key := strings.Join([]string{"back-auth", route, method, mechanism, match.FilePath, strconv.Itoa(match.Line)}, "|")
			if !seen[key] {
				facts = append(facts, mangle.Fact{
					Predicate: "scoped_backend_api_contract",
					Args:      []interface{}{sessionID, route, method, mechanism},
					Timestamp: now,
				})
				seen[key] = true
			}
		}
		for _, field := range parseBackendPayloadRequirement(match.PayloadExpectation) {
			key := strings.Join([]string{"back-payload", route, method, field}, "|")
			if !seen[key] {
				facts = append(facts, mangle.Fact{
					Predicate: "scoped_backend_payload_requirement",
					Args:      []interface{}{sessionID, route, method, field, "required"},
					Timestamp: now,
				})
				seen[key] = true
			}
		}
	}

	return facts
}

func matchedAuditRequests(route, method string, requests []observedAuditRequest) []observedAuditRequest {
	regex, _ := buildAuditPathRegex(route)
	matches := make([]observedAuditRequest, 0, 2)
	for _, request := range requests {
		if method != "" && request.Method != "" && method != request.Method {
			continue
		}
		if request.Route == route {
			matches = append(matches, request)
			continue
		}
		if regex != nil && regex.MatchString(request.Route) {
			matches = append(matches, request)
		}
	}
	return matches
}

func inspectFrontendContractSnippet(repoRoot string, site browser.RepoTraceFrontendSite) (string, map[string]string) {
	fullPath := filepath.Join(repoRoot, filepath.FromSlash(site.FilePath))
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "none", map[string]string{}
	}
	lines := strings.Split(string(content), "\n")
	window := auditLineWindow(lines, maxInt(site.Line-1, 0), 2, 6)
	auth := inferFrontendAuthMechanism(window)
	fields := extractPayloadFieldsFromSnippet(window)
	return auth, fields
}

func inferFrontendAuthMechanism(window string) string {
	lower := strings.ToLower(window)
	switch {
	case strings.Contains(lower, "x-api-key") || strings.Contains(lower, "apikey") || strings.Contains(lower, "api_key"):
		return "api_key"
	case strings.Contains(lower, "authorization") && strings.Contains(lower, "bearer"):
		return "jwt"
	case strings.Contains(lower, "authorization"):
		return "auth_header"
	default:
		return "none"
	}
}

func extractPayloadFieldsFromSnippet(window string) map[string]string {
	fields := make(map[string]string)
	match := auditPayloadObjectRe.FindStringSubmatch(window)
	if len(match) < 2 {
		return fields
	}
	for _, fieldMatch := range auditObjectFieldRe.FindAllStringSubmatch(match[1], -1) {
		if len(fieldMatch) >= 2 {
			field := strings.TrimSpace(fieldMatch[1])
			if field != "" {
				fields[field] = "present"
			}
		}
	}
	return fields
}

func parseBackendAuthExpectation(raw string) []string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(lower, "api_key") || strings.Contains(lower, "apikey") || strings.Contains(lower, "x-api-key"):
		return []string{"api_key"}
	case strings.Contains(lower, "auth") || strings.Contains(lower, "jwt") || strings.Contains(lower, "bearer"):
		return []string{"jwt"}
	default:
		return nil
	}
}

func parseBackendPayloadRequirement(raw string) []string {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "fields:") {
		return nil
	}
	fields := strings.Split(strings.TrimPrefix(raw, "fields:"), ",")
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}

func queryScopedContractAuditFindings(ctx context.Context, engine *mangle.Engine, sessionID string) []map[string]interface{} {
	if engine == nil {
		return nil
	}

	type scopedSpec struct {
		predicate string
		query     string
		severity  string
		message   func(map[string]interface{}) string
	}

	specs := []scopedSpec{
		{
			predicate: "missing_auth",
			query:     fmt.Sprintf(`scoped_missing_jwt_or_auth_header(%q, ActionRef, PageRoute, RequestId, ApiRoute, Method, ExpectedMechanism).`, sessionID),
			severity:  "high",
			message: func(row map[string]interface{}) string {
				return fmt.Sprintf("Observed request %v is missing expected %v for %v %v.", row["RequestId"], row["ExpectedMechanism"], row["Method"], row["ApiRoute"])
			},
		},
		{
			predicate: "missing_api_key",
			query:     fmt.Sprintf(`scoped_missing_api_key(%q, ActionRef, PageRoute, RequestId, ApiRoute, Method).`, sessionID),
			severity:  "high",
			message: func(row map[string]interface{}) string {
				return fmt.Sprintf("Observed request %v is missing an API key for %v %v.", row["RequestId"], row["Method"], row["ApiRoute"])
			},
		},
		{
			predicate: "auth_mismatch",
			query:     fmt.Sprintf(`scoped_auth_mechanism_mismatch(%q, ActionRef, PageRoute, RequestId, ApiRoute, Method, ObservedMechanism, ExpectedMechanism).`, sessionID),
			severity:  "high",
			message: func(row map[string]interface{}) string {
				return fmt.Sprintf("Observed auth mechanism %v does not satisfy expected %v for %v %v.", row["ObservedMechanism"], row["ExpectedMechanism"], row["Method"], row["ApiRoute"])
			},
		},
		{
			predicate: "payload_mismatch",
			query:     fmt.Sprintf(`scoped_payload_requirement_mismatch(%q, ActionRef, PageRoute, RequestId, ApiRoute, Method, Field, Requirement).`, sessionID),
			severity:  "medium",
			message: func(row map[string]interface{}) string {
				return fmt.Sprintf("Payload for request %v is missing required field %v on %v %v.", row["RequestId"], row["Field"], row["Method"], row["ApiRoute"])
			},
		},
		{
			predicate: "contract_gap",
			query:     fmt.Sprintf(`scoped_frontend_backend_contract_gap(%q, ActionRef, PageRoute, ApiRoute, Method, Aspect, FrontendValue, BackendValue).`, sessionID),
			severity:  "high",
			message: func(row map[string]interface{}) string {
				return fmt.Sprintf("Frontend/backend contract gap on %v %v (%v: %v -> %v).", row["Method"], row["ApiRoute"], row["Aspect"], row["FrontendValue"], row["BackendValue"])
			},
		},
	}

	findings := make([]map[string]interface{}, 0, 12)
	seen := make(map[string]bool)
	for _, spec := range specs {
		rows := queryToRows(ctx, engine, spec.query)
		for _, row := range rows {
			key := spec.predicate + "|" + fmt.Sprintf("%v", row["ActionRef"]) + "|" + fmt.Sprintf("%v", row["ApiRoute"]) + "|" + fmt.Sprintf("%v", row["RequestId"]) + "|" + fmt.Sprintf("%v", row["Aspect"]) + "|" + fmt.Sprintf("%v", row["Field"])
			if seen[key] {
				continue
			}
			seen[key] = true
			findings = append(findings, map[string]interface{}{
				"kind":           spec.predicate,
				"severity":       spec.severity,
				"message":        spec.message(row),
				"route":          row["ApiRoute"],
				"method":         row["Method"],
				"action_ref":     row["ActionRef"],
				"request_id":     row["RequestId"],
				"page_route":     row["PageRoute"],
				"mechanism":      row["ExpectedMechanism"],
				"field":          row["Field"],
				"aspect":         row["Aspect"],
				"frontend_value": row["FrontendValue"],
				"backend_value":  row["BackendValue"],
				"evidence_handles": []string{
					"audit:" + sessionID + ":mangle_contracts",
				},
			})
		}
	}
	return findings
}

func mergeAuditFindingSets(primary, secondary []map[string]interface{}, maxItems int) []map[string]interface{} {
	merged := make([]map[string]interface{}, 0, len(primary)+len(secondary))
	seen := make(map[string]bool)
	appendSet := func(items []map[string]interface{}) {
		for _, item := range items {
			key := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", item["kind"]))) + "|" +
				strings.TrimSpace(fmt.Sprintf("%v", item["route"])) + "|" +
				strings.TrimSpace(fmt.Sprintf("%v", item["method"])) + "|" +
				strings.TrimSpace(fmt.Sprintf("%v", item["request_id"])) + "|" +
				strings.TrimSpace(fmt.Sprintf("%v", item["field"]))
			if seen[key] {
				continue
			}
			seen[key] = true
			merged = append(merged, item)
		}
	}
	appendSet(primary)
	appendSet(secondary)
	if maxItems > 0 && len(merged) > maxItems {
		return merged[:maxItems]
	}
	return merged
}

func auditLineWindow(lines []string, idx, before, after int) string {
	start := idx - before
	if start < 0 {
		start = 0
	}
	end := idx + after + 1
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start:end], "\n")
}
