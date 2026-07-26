package browser

import (
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"

	"browsernerd-mcp-server/internal/mangle"
)

func buildRepoTraceFacts(report *RepoTraceReport) []mangle.Fact {
	now := report.GeneratedAt
	facts := make([]mangle.Fact, 0, 1+len(report.Seeds)+len(report.FrontendSites)+len(report.BackendMatches)+len(report.Correlations)+len(report.Evidence))
	facts = append(facts, mangle.Fact{
		Predicate: "repo_trace_run",
		Args: []interface{}{
			report.SessionID, report.TraceID, report.RootDir, report.BrowserContext.CurrentURL,
			len(report.FrontendSites), len(report.BackendMatches), len(report.Correlations), now.UnixMilli(),
		},
		Timestamp: now,
	})

	for _, seed := range report.Seeds {
		facts = append(facts, mangle.Fact{
			Predicate: "repo_trace_seed",
			Args:      []interface{}{report.SessionID, report.TraceID, seed.Kind, seed.Value, seed.Source, now.UnixMilli()},
			Timestamp: now,
		})
	}
	for _, front := range report.FrontendSites {
		facts = append(facts, mangle.Fact{
			Predicate: "repo_trace_frontend_request_site",
			Args: []interface{}{
				report.SessionID, report.TraceID, front.ID, front.Method, front.Endpoint, front.FilePath,
				front.Line, front.Confidence, front.EvidenceHandle, now.UnixMilli(),
			},
			Timestamp: now,
		})
	}
	for _, back := range report.BackendMatches {
		facts = append(facts, mangle.Fact{
			Predicate: "repo_trace_backend_expectation",
			Args: []interface{}{
				report.SessionID, report.TraceID, back.ID, back.Method, back.RoutePath, back.AuthExpectation,
				back.PayloadExpectation, back.FilePath, back.Line, back.Confidence, back.EvidenceHandle, now.UnixMilli(),
			},
			Timestamp: now,
		})
	}
	for _, edge := range report.Correlations {
		facts = append(facts, mangle.Fact{
			Predicate: "repo_trace_correlation",
			Args: []interface{}{
				report.SessionID, report.TraceID, edge.FrontendID, edge.BackendID, edge.Reason, edge.Confidence, now.UnixMilli(),
			},
			Timestamp: now,
		})
	}
	for _, ev := range report.Evidence {
		facts = append(facts, mangle.Fact{
			Predicate: "repo_trace_evidence",
			Args: []interface{}{
				report.SessionID, report.TraceID, ev.Handle, ev.Kind, ev.Summary, ev.FilePath, ev.Line, ev.Confidence, now.UnixMilli(),
			},
			Timestamp: now,
		})
	}
	return facts
}

func sortRepoTraceFrontend(sites []RepoTraceFrontendSite) {
	sort.SliceStable(sites, func(i, j int) bool {
		if sites[i].Confidence == sites[j].Confidence {
			if sites[i].FilePath == sites[j].FilePath {
				return sites[i].Line < sites[j].Line
			}
			return sites[i].FilePath < sites[j].FilePath
		}
		return sites[i].Confidence > sites[j].Confidence
	})
}

func sortRepoTraceBackend(routes []RepoTraceBackendExpectation) {
	sort.SliceStable(routes, func(i, j int) bool {
		if routes[i].Confidence == routes[j].Confidence {
			if routes[i].FilePath == routes[j].FilePath {
				return routes[i].Line < routes[j].Line
			}
			return routes[i].FilePath < routes[j].FilePath
		}
		return routes[i].Confidence > routes[j].Confidence
	})
}

func limitFrontendMatches(sites []RepoTraceFrontendSite, max int) []RepoTraceFrontendSite {
	if max <= 0 || len(sites) <= max {
		return sites
	}
	return append([]RepoTraceFrontendSite(nil), sites[:max]...)
}

func limitBackendMatches(routes []RepoTraceBackendExpectation, max int) []RepoTraceBackendExpectation {
	if max <= 0 || len(routes) <= max {
		return routes
	}
	return append([]RepoTraceBackendExpectation(nil), routes[:max]...)
}

func assignRepoTraceFrontendIDs(traceID string, sites []RepoTraceFrontendSite) {
	for i := range sites {
		sites[i].ID = fmt.Sprintf("front-%d", i+1)
		sites[i].EvidenceHandle = fmt.Sprintf("trace:%s:frontend:%d", traceID, i+1)
	}
}

func assignRepoTraceBackendIDs(traceID string, routes []RepoTraceBackendExpectation) {
	for i := range routes {
		routes[i].ID = fmt.Sprintf("back-%d", i+1)
		routes[i].EvidenceHandle = fmt.Sprintf("trace:%s:backend:%d", traceID, i+1)
	}
}

func containsRepoTraceRequestKeyword(lower string) bool {
	return strings.Contains(lower, "fetch(") ||
		strings.Contains(lower, "axios.") ||
		strings.Contains(lower, "axios(") ||
		strings.Contains(lower, "client.") ||
		strings.Contains(lower, "request(") ||
		strings.Contains(lower, "useswr(")
}

func containsRepoTraceRouteKeyword(lower string) bool {
	return strings.Contains(lower, "router.") ||
		strings.Contains(lower, "app.") ||
		strings.Contains(lower, "handlefunc(") ||
		strings.Contains(lower, "@get(") ||
		strings.Contains(lower, "@post(") ||
		strings.Contains(lower, "@patch(") ||
		strings.Contains(lower, "@put(") ||
		strings.Contains(lower, "@delete(")
}

func normalizeRepoTracePath(raw string) string {
	raw = strings.TrimSpace(strings.Trim(raw, "\"'`"))
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, "\\", "/")
	if idx := strings.IndexAny(raw, "?#"); idx >= 0 {
		raw = raw[:idx]
	}
	if parsed, err := url.Parse(raw); err == nil && parsed.Path != "" && (parsed.Scheme != "" || parsed.Host != "") {
		raw = parsed.Path
	}
	raw = repoTraceTemplateExprRe.ReplaceAllString(raw, ":param")
	raw = repoTraceBracketParamRe.ReplaceAllString(raw, ":param")
	if strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
		return ""
	}
	if !strings.HasPrefix(raw, "/") {
		if strings.Contains(raw, "/api/") {
			raw = raw[strings.Index(raw, "/api/"):]
		} else if strings.HasPrefix(strings.ToLower(raw), "api/") {
			raw = "/" + raw
		} else {
			return ""
		}
	}
	for strings.Contains(raw, "//") {
		raw = strings.ReplaceAll(raw, "//", "/")
	}
	if len(raw) > 1 {
		raw = strings.TrimRight(raw, "/")
	}
	return raw
}

func skeletonizeRepoTracePath(path string) string {
	path = normalizeRepoTracePath(path)
	if path == "" {
		return ""
	}
	segments := repoTracePathSegments(path)
	for i, segment := range segments {
		if looksDynamicRepoTraceSegment(segment) {
			segments[i] = ":param"
		}
	}
	if len(segments) == 0 {
		return "/"
	}
	return "/" + strings.Join(segments, "/")
}

func repoTracePathSegments(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return nil
	}
	raw := strings.Split(trimmed, "/")
	out := make([]string, 0, len(raw))
	for _, segment := range raw {
		segment = strings.TrimSpace(strings.ToLower(segment))
		if segment == "" {
			continue
		}
		out = append(out, segment)
	}
	return out
}

func repoTraceSharedSegments(a, b []string) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	set := make(map[string]bool, len(a))
	for _, segment := range a {
		set[segment] = true
	}
	count := 0
	seen := make(map[string]bool)
	for _, segment := range b {
		if set[segment] && !seen[segment] {
			seen[segment] = true
			count++
		}
	}
	return count
}

func looksDynamicRepoTraceSegment(segment string) bool {
	segment = strings.TrimSpace(strings.ToLower(segment))
	if segment == "" {
		return false
	}
	if strings.HasPrefix(segment, ":") || strings.Contains(segment, ":param") {
		return true
	}
	if strings.Contains(segment, "[") || strings.Contains(segment, "]") || strings.Contains(segment, "${") {
		return true
	}
	if repoTraceHexLikeRe.MatchString(segment) {
		return true
	}
	digits := 0
	for _, r := range segment {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits == len(segment) && digits > 0
}

func looksLikeRepoTracePath(path string) bool {
	if path == "" || path == "/" {
		return false
	}
	if repoTraceAssetExtensions[strings.ToLower(filepath.Ext(path))] {
		return false
	}
	return strings.HasPrefix(path, "/")
}

func shouldSkipRepoTraceDir(name string, ignoreDirs []string) bool {
	if len(ignoreDirs) == 0 {
		ignoreDirs = defaultRepoTraceIgnoreDirs
	}
	for _, ignored := range ignoreDirs {
		if strings.EqualFold(name, ignored) {
			return true
		}
	}
	return false
}

func addRepoTraceTokens(target map[string]bool, text string) {
	if len(target) == 0 || strings.TrimSpace(text) == "" {
		return
	}
	normalized := strings.ToLower(strings.ReplaceAll(text, "/", " "))
	for _, token := range repoTraceWordRe.FindAllString(normalized, -1) {
		if len(token) < 3 || repoTraceStopTokens[token] {
			continue
		}
		target[token] = true
	}
}

func repoTracePathMatch(candidate string, seeds []string) (float64, string) {
	if candidate == "" || len(seeds) == 0 {
		return 0, ""
	}
	candidate = normalizeRepoTracePath(candidate)
	candidateSkeleton := skeletonizeRepoTracePath(candidate)
	candidateSegments := repoTracePathSegments(candidateSkeleton)

	bestScore := 0.0
	bestReason := ""
	for _, seed := range seeds {
		seedPath := normalizeRepoTracePath(seed)
		if seedPath == "" {
			continue
		}
		seedSkeleton := skeletonizeRepoTracePath(seedPath)
		switch {
		case candidate == seedPath || candidateSkeleton == seedSkeleton:
			return 0.65, "route matched browser/network path"
		case strings.HasPrefix(candidateSkeleton, seedSkeleton) || strings.HasPrefix(seedSkeleton, candidateSkeleton):
			if bestScore < 0.4 {
				bestScore = 0.4
				bestReason = "route shared path prefix with browser context"
			}
		default:
			shared := repoTraceSharedSegments(candidateSegments, repoTracePathSegments(seedSkeleton))
			if shared >= 2 && bestScore < 0.3 {
				bestScore = 0.3
				bestReason = "route shared multiple path segments with browser context"
			}
		}
	}
	return bestScore, bestReason
}

func repoTraceTokenMatch(text string, tokens map[string]bool) (float64, string) {
	if len(tokens) == 0 || strings.TrimSpace(text) == "" {
		return 0, ""
	}
	normalized := strings.ToLower(text)
	hits := 0
	for token := range tokens {
		if token != "" && strings.Contains(normalized, token) {
			hits++
		}
	}
	switch {
	case hits >= 3:
		return 0.2, "multiple repo/context tokens matched"
	case hits == 2:
		return 0.15, "two repo/context tokens matched"
	case hits == 1:
		return 0.08, "one repo/context token matched"
	default:
		return 0, ""
	}
}

func repoTraceKeywordMatch(text string, keywords []string) string {
	if strings.TrimSpace(text) == "" || len(keywords) == 0 {
		return ""
	}
	normalized := strings.ToLower(repoTraceWhitespaceRe.ReplaceAllString(text, " "))
	for _, keyword := range keywords {
		if strings.Contains(normalized, keyword) {
			return keyword
		}
	}
	return ""
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

func uniqueOrderedStrings(values []string) []string {
	if len(values) == 0 {
		return values
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

func repoTraceLineWindow(lines []string, idx, before, after int) string {
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

func methodsCompatible(a, b string) bool {
	a = strings.ToUpper(strings.TrimSpace(a))
	b = strings.ToUpper(strings.TrimSpace(b))
	return a == "" || b == "" || a == b
}

func compactRepoTraceSnippet(text string) string {
	trimmed := strings.TrimSpace(repoTraceWhitespaceRe.ReplaceAllString(text, " "))
	if len(trimmed) <= repoTraceSnippetLimit {
		return trimmed
	}
	return trimmed[:repoTraceSnippetLimit] + "..."
}

func fallbackRepoTraceMethod(method string) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		return "ROUTE"
	}
	return method
}

func minRepoTraceConfidence(value float64) float64 {
	if value > 0.99 {
		return 0.99
	}
	return value
}

func after(value, needle string) string {
	idx := strings.Index(value, needle)
	if idx < 0 {
		return value
	}
	return value[idx+len(needle):]
}
