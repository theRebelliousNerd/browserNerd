package browser

import (
	"path/filepath"
	"sort"
	"strings"
)

func scoreRepoTraceFrontendMatch(relPath, endpoint, method, window string, seeds repoTraceSeedSet) (float64, []string) {
	confidence := 0.0
	reasons := make([]string, 0, 4)
	if score, reason := repoTracePathMatch(endpoint, seeds.apiPaths); score > 0 {
		confidence += score
		reasons = append(reasons, reason)
	}
	if score, reason := repoTracePathMatch(endpoint, seeds.pagePaths); score > 0 {
		confidence += score * 0.4
		reasons = append(reasons, "page-context:"+reason)
	}
	if score, reason := repoTraceTokenMatch(relPath, seeds.pageTokens); score > 0 {
		confidence += score
		reasons = append(reasons, reason)
	}
	if score, reason := repoTraceTokenMatch(relPath, seeds.componentTokens); score > 0 {
		confidence += score
		reasons = append(reasons, "component-context:"+reason)
	}
	if score, reason := repoTraceTokenMatch(window, seeds.fieldTokens); score > 0 {
		confidence += score * 0.8
		reasons = append(reasons, "payload-context:"+reason)
	}
	if method != "" {
		confidence += 0.05
		reasons = append(reasons, "http-method-present")
	}
	if containsRepoTraceRequestKeyword(strings.ToLower(window)) {
		confidence += 0.05
	}
	return minRepoTraceConfidence(confidence), uniqueOrderedStrings(reasons)
}

func scoreRepoTraceBackendMatch(relPath, routePath, method, window string, seeds repoTraceSeedSet) (float64, []string) {
	confidence := 0.0
	reasons := make([]string, 0, 4)
	if score, reason := repoTracePathMatch(routePath, seeds.apiPaths); score > 0 {
		confidence += score + 0.05
		reasons = append(reasons, reason)
	}
	if score, reason := repoTracePathMatch(routePath, seeds.pagePaths); score > 0 {
		confidence += score * 0.4
		reasons = append(reasons, "page-context:"+reason)
	}
	if score, reason := repoTraceTokenMatch(relPath, seeds.pageTokens); score > 0 {
		confidence += score
		reasons = append(reasons, reason)
	}
	if score, reason := repoTraceTokenMatch(window, seeds.fieldTokens); score > 0 {
		confidence += score
		reasons = append(reasons, "payload-context:"+reason)
	}
	if summarizeRepoTraceAuth(window) != "" {
		confidence += 0.05
		reasons = append(reasons, "auth-guard-present")
	}
	if summarizeRepoTracePayload(window) != "" {
		confidence += 0.05
		reasons = append(reasons, "payload-shape-present")
	}
	if method != "" {
		confidence += 0.05
	}
	return minRepoTraceConfidence(confidence), uniqueOrderedStrings(reasons)
}

func extractRepoTraceFrontendCall(window string) (method, endpoint, requestCall string) {
	if matches := repoTraceAxiosMethodRe.FindStringSubmatch(window); len(matches) == 3 {
		return strings.ToUpper(matches[1]), matches[2], "axios." + strings.ToLower(matches[1])
	}
	if matches := repoTraceClientMethodRe.FindStringSubmatch(window); len(matches) == 3 {
		return strings.ToUpper(matches[1]), matches[2], "client." + strings.ToLower(matches[1])
	}
	if matches := repoTraceSWRRe.FindStringSubmatch(window); len(matches) == 2 {
		return "GET", matches[1], "useSWR"
	}
	if matches := repoTraceFetchRe.FindStringSubmatch(window); len(matches) == 2 {
		method = "GET"
		if methodMatches := repoTraceMethodOptionRe.FindStringSubmatch(window); len(methodMatches) == 2 {
			method = strings.ToUpper(methodMatches[1])
		}
		return method, matches[1], "fetch"
	}
	if matches := repoTraceConfigURLRe.FindStringSubmatch(window); len(matches) == 2 {
		if methodMatches := repoTraceMethodOptionRe.FindStringSubmatch(window); len(methodMatches) == 2 {
			method = strings.ToUpper(methodMatches[1])
		}
		return method, matches[1], "config.url"
	}
	return "", "", ""
}

func extractRepoTraceBackendRoute(window string) (method, routePath string) {
	if matches := repoTraceExpressRouteRe.FindStringSubmatch(window); len(matches) == 3 {
		return strings.ToUpper(matches[1]), matches[2]
	}
	if matches := repoTraceDecoratorRouteRe.FindStringSubmatch(window); len(matches) == 3 {
		return strings.ToUpper(matches[1]), matches[2]
	}
	if matches := repoTraceGoMethodRouteRe.FindStringSubmatch(window); len(matches) == 3 {
		return strings.ToUpper(matches[1]), matches[2]
	}
	if matches := repoTraceHandleFuncRe.FindStringSubmatch(window); len(matches) == 2 {
		return "", matches[1]
	}
	return "", ""
}

func summarizeRepoTraceAuth(window string) string {
	matches := repoTraceAuthRe.FindAllString(window, -1)
	if len(matches) == 0 {
		return ""
	}
	return strings.Join(uniqueOrderedStrings(matches), ",")
}

func summarizeRepoTracePayload(window string) string {
	fields := make([]string, 0, 6)
	for _, match := range repoTraceReqBodyFieldRe.FindAllStringSubmatch(window, -1) {
		if len(match) == 2 {
			fields = append(fields, match[1])
		}
	}
	for _, match := range repoTraceBodyFieldRe.FindAllStringSubmatch(window, -1) {
		if len(match) == 2 {
			fields = append(fields, match[1])
		}
	}
	for _, match := range repoTraceFormValueRe.FindAllStringSubmatch(window, -1) {
		if len(match) == 2 {
			fields = append(fields, match[1])
		}
	}
	fields = uniqueOrderedStrings(fields)
	if len(fields) > 0 {
		if len(fields) > 4 {
			fields = fields[:4]
		}
		return "fields:" + strings.Join(fields, ",")
	}

	schemas := make([]string, 0, 4)
	for _, match := range repoTraceSchemaRe.FindAllStringSubmatch(window, -1) {
		if len(match) == 2 {
			schemas = append(schemas, match[1])
		}
	}
	schemas = uniqueOrderedStrings(schemas)
	if len(schemas) > 0 {
		if len(schemas) > 3 {
			schemas = schemas[:3]
		}
		return "schema:" + strings.Join(schemas, ",")
	}

	return ""
}

func inferRepoTraceRouteFromFilePath(relPath string) (string, string) {
	normalized := filepath.ToSlash(relPath)
	switch {
	case strings.Contains(normalized, "/app/api/") && strings.HasSuffix(normalized, "/route.ts"):
		return normalizeRepoTracePath(strings.TrimSuffix(after(normalized, "/app"), "/route.ts")), ""
	case strings.Contains(normalized, "/app/api/") && strings.HasSuffix(normalized, "/route.js"):
		return normalizeRepoTracePath(strings.TrimSuffix(after(normalized, "/app"), "/route.js")), ""
	case strings.Contains(normalized, "/pages/api/") && strings.HasSuffix(normalized, ".ts"):
		return normalizeRepoTracePath(strings.TrimSuffix(after(normalized, "/pages"), ".ts")), ""
	case strings.Contains(normalized, "/pages/api/") && strings.HasSuffix(normalized, ".js"):
		return normalizeRepoTracePath(strings.TrimSuffix(after(normalized, "/pages"), ".js")), ""
	default:
		return "", ""
	}
}

func correlateRepoTrace(frontend []RepoTraceFrontendSite, backend []RepoTraceBackendExpectation) []RepoTraceCorrelation {
	correlations := make([]RepoTraceCorrelation, 0, len(frontend))
	seen := make(map[string]bool)
	for _, front := range frontend {
		for _, back := range backend {
			confidence, reason := correlateRepoTracePair(front, back)
			if confidence <= 0 {
				continue
			}
			key := front.ID + "|" + back.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			correlations = append(correlations, RepoTraceCorrelation{
				FrontendID: front.ID,
				BackendID:  back.ID,
				Reason:     reason,
				Confidence: confidence,
			})
		}
	}
	sort.SliceStable(correlations, func(i, j int) bool {
		if correlations[i].Confidence == correlations[j].Confidence {
			if correlations[i].FrontendID == correlations[j].FrontendID {
				return correlations[i].BackendID < correlations[j].BackendID
			}
			return correlations[i].FrontendID < correlations[j].FrontendID
		}
		return correlations[i].Confidence > correlations[j].Confidence
	})
	return correlations
}

func correlateRepoTracePair(front RepoTraceFrontendSite, back RepoTraceBackendExpectation) (float64, string) {
	frontPath := skeletonizeRepoTracePath(front.Endpoint)
	backPath := skeletonizeRepoTracePath(back.RoutePath)
	if frontPath == "" || backPath == "" {
		return 0, ""
	}
	switch {
	case frontPath == backPath && methodsCompatible(front.Method, back.Method):
		return 0.95, "route skeleton and method matched"
	case frontPath == backPath:
		return 0.85, "route skeleton matched"
	case strings.HasSuffix(frontPath, backPath) || strings.HasSuffix(backPath, frontPath):
		return 0.75, "route suffix matched"
	case repoTraceSharedSegments(repoTracePathSegments(frontPath), repoTracePathSegments(backPath)) >= 2:
		return 0.6, "route segments overlapped"
	default:
		return 0, ""
	}
}

func buildRepoTraceEvidence(frontend []RepoTraceFrontendSite, backend []RepoTraceBackendExpectation) []RepoTraceEvidence {
	evidence := make([]RepoTraceEvidence, 0, len(frontend)+len(backend))
	for _, front := range frontend {
		evidence = append(evidence, RepoTraceEvidence{
			Handle:     front.EvidenceHandle,
			Kind:       "frontend_request_site",
			Summary:    front.Summary,
			FilePath:   front.FilePath,
			Line:       front.Line,
			Snippet:    compactRepoTraceSnippet(strings.Join(front.MatchReasons, "; ")),
			Confidence: front.Confidence,
		})
	}
	for _, back := range backend {
		evidence = append(evidence, RepoTraceEvidence{
			Handle:     back.EvidenceHandle,
			Kind:       "backend_expectation",
			Summary:    back.Summary,
			FilePath:   back.FilePath,
			Line:       back.Line,
			Snippet:    compactRepoTraceSnippet(strings.Join(back.MatchReasons, "; ")),
			Confidence: back.Confidence,
		})
	}
	return evidence
}
