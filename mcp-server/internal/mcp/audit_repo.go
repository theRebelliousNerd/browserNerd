package mcp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

func buildAuditPageContext(sessions *browser.SessionManager, sessionID, currentURL, repoRoot string) map[string]interface{} {
	context := map[string]interface{}{
		"session_id": sessionID,
		"repo_root":  repoRoot,
	}
	if currentURL != "" {
		context["current_url"] = currentURL
		if routePath := canonicalAuditPath(currentURL); routePath != "" {
			context["route_path"] = routePath
		}
	}

	if sessions == nil {
		context["session_found"] = false
		context["browser_connected"] = false
		context["active_session_count"] = 0
		return context
	}

	context["browser_connected"] = sessions.IsConnected()
	sessionList := sessions.List()
	context["active_session_count"] = len(sessionList)

	if session, ok := sessions.GetSession(sessionID); ok {
		context["session_found"] = true
		context["session_status"] = session.Status
		context["session_title"] = session.Title
		context["session_url"] = session.URL
		context["session_target_id"] = session.TargetID
		context["created_at"] = session.CreatedAt.UnixMilli()
		context["last_active"] = session.LastActive.UnixMilli()
		if currentURL == "" && strings.TrimSpace(session.URL) != "" {
			context["current_url"] = session.URL
			if routePath := canonicalAuditPath(session.URL); routePath != "" {
				context["route_path"] = routePath
			}
		}
		return context
	}

	context["session_found"] = false
	return context
}

func (t *BrowserAuditTool) traceAuditDiscoverContext(ctx context.Context, sessionID, repoRoot string, includeRepoMatches bool, maxRepoMatches int) (*browser.RepoTraceReport, []string) {
	if !includeRepoMatches || t.sessions == nil {
		return nil, nil
	}

	if _, ok := t.sessions.Page(sessionID); !ok {
		return nil, nil
	}

	traceReport, err := t.sessions.TraceRepositoryContext(ctx, sessionID, &browser.RepoTraceOptions{
		RootDir:            repoRoot,
		MaxNavigationHints: defaultAuditDiscoverMaxHints,
		MaxControlHints:    defaultAuditDiscoverMaxHints,
		MaxPlanSteps:       defaultAuditDiscoverMaxSteps,
		MaxFrontendMatches: maxRepoMatches,
		MaxBackendMatches:  maxRepoMatches,
	})
	if err != nil {
		return nil, []string{"repo trace unavailable during discover: " + err.Error()}
	}
	return traceReport, nil
}

func queryAuditFailedRequests(ctx context.Context, engine *mangle.Engine, sessionID string) []map[string]interface{} {
	if engine == nil || strings.TrimSpace(sessionID) == "" {
		return []map[string]interface{}{}
	}

	failedRows := queryToRows(ctx, engine, fmt.Sprintf("failed_request_at(%q, ReqId, Url, Status, ReqTs).", sessionID))
	if len(failedRows) == 0 {
		failedRows = queryToRows(ctx, engine, fmt.Sprintf("failed_request(%q, ReqId, Url, Status).", sessionID))
	}

	requestMetaRows := queryToRows(ctx, engine, fmt.Sprintf("net_request(%q, ReqId, Method, Url, InitiatorId, StartTime).", sessionID))
	requestMetaByID := make(map[string]map[string]interface{}, len(requestMetaRows))
	for _, row := range requestMetaRows {
		reqID := strings.TrimSpace(fmt.Sprintf("%v", row["ReqId"]))
		if reqID != "" {
			requestMetaByID[reqID] = row
		}
	}

	responseRows := queryToRows(ctx, engine, fmt.Sprintf("net_response(%q, ReqId, Status, Latency, Duration).", sessionID))
	responseByID := make(map[string]map[string]interface{}, len(responseRows))
	for _, row := range responseRows {
		reqID := strings.TrimSpace(fmt.Sprintf("%v", row["ReqId"]))
		if reqID != "" {
			responseByID[reqID] = row
		}
	}

	enriched := make([]map[string]interface{}, 0, len(failedRows))
	for _, row := range failedRows {
		copyRow := make(map[string]interface{}, len(row)+6)
		for key, value := range row {
			copyRow[key] = value
		}

		reqID := strings.TrimSpace(fmt.Sprintf("%v", copyRow["ReqId"]))
		if meta, ok := requestMetaByID[reqID]; ok {
			if copyRow["Method"] == nil {
				copyRow["Method"] = meta["Method"]
			}
			if copyRow["StartTime"] == nil {
				copyRow["StartTime"] = meta["StartTime"]
			}
		}
		if response, ok := responseByID[reqID]; ok {
			if copyRow["Latency"] == nil {
				copyRow["Latency"] = response["Latency"]
			}
			if copyRow["Duration"] == nil {
				copyRow["Duration"] = response["Duration"]
			}
			if copyRow["Status"] == nil {
				copyRow["Status"] = response["Status"]
			}
		}

		urlValue := strings.TrimSpace(fmt.Sprintf("%v", copyRow["Url"]))
		if urlValue != "" {
			copyRow["Path"] = canonicalAuditPath(urlValue)
			copyRow["PathPattern"] = buildAuditPathPatternDisplay(urlValue)
		}

		enriched = append(enriched, copyRow)
	}

	sort.SliceStable(enriched, func(i, j int) bool {
		tsI := extractTimestamp(enriched[i], "ReqTs", "StartTime", "Timestamp")
		tsJ := extractTimestamp(enriched[j], "ReqTs", "StartTime", "Timestamp")
		if tsI != tsJ {
			return tsI > tsJ
		}
		statusI := asInt(enriched[i]["Status"])
		statusJ := asInt(enriched[j]["Status"])
		return statusI > statusJ
	})

	return enriched
}

func filterAuditRowsByRequestID(rows []map[string]interface{}, allowed map[string]bool) []map[string]interface{} {
	if len(rows) == 0 || len(allowed) == 0 {
		if len(allowed) == 0 {
			return []map[string]interface{}{}
		}
		return rows
	}

	filtered := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		reqID := strings.TrimSpace(fmt.Sprintf("%v", row["ReqId"]))
		if reqID != "" && allowed[reqID] {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func buildAuditRepoSearchSpecs(currentURL string, failedRequests []map[string]interface{}) []auditRepoSearchSpec {
	byKey := make(map[string]auditRepoSearchSpec)

	addSpec := func(kind, rawPath string) {
		canonical := canonicalAuditPath(rawPath)
		if canonical == "" || canonical == "/" {
			return
		}
		regex, err := buildAuditPathRegex(canonical)
		if err != nil {
			return
		}

		key := canonical
		if kind == "page_route" {
			key = "route:" + canonical
		}

		if _, exists := byKey[key]; exists {
			return
		}

		byKey[key] = auditRepoSearchSpec{
			key:      key,
			kind:     kind,
			literal:  canonical,
			display:  buildAuditPathPatternDisplay(canonical),
			regex:    regex,
			pathHint: buildAuditPathHint(canonical),
		}
	}

	if currentURL != "" {
		addSpec("page_route", currentURL)
	}
	for _, row := range failedRequests {
		addSpec("api_contract", strings.TrimSpace(fmt.Sprintf("%v", row["Url"])))
	}

	specs := make([]auditRepoSearchSpec, 0, len(byKey))
	for _, spec := range byKey {
		specs = append(specs, spec)
	}

	sort.SliceStable(specs, func(i, j int) bool {
		if specs[i].kind != specs[j].kind {
			return specs[i].kind < specs[j].kind
		}
		return specs[i].key < specs[j].key
	})

	return specs
}

func buildAuditPathRegex(rawPath string) (*regexp.Regexp, error) {
	canonical := canonicalAuditPath(rawPath)
	if canonical == "" {
		return nil, fmt.Errorf("empty audit path")
	}

	segments := strings.Split(strings.Trim(canonical, "/"), "/")
	parts := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		if looksDynamicAuditSegment(segment) {
			parts = append(parts, "[^\"'`\\s/]+")
			continue
		}
		parts = append(parts, regexp.QuoteMeta(segment))
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("no searchable audit segments")
	}

	return regexp.Compile(`/` + strings.Join(parts, `/`))
}

func buildAuditPathPatternDisplay(rawPath string) string {
	canonical := canonicalAuditPath(rawPath)
	if canonical == "" {
		return ""
	}

	segments := strings.Split(strings.Trim(canonical, "/"), "/")
	displaySegments := make([]string, 0, len(segments))
	for _, segment := range segments {
		if segment == "" {
			continue
		}
		if looksDynamicAuditSegment(segment) {
			displaySegments = append(displaySegments, "{param}")
			continue
		}
		displaySegments = append(displaySegments, segment)
	}
	if len(displaySegments) == 0 {
		return canonical
	}
	return "/" + strings.Join(displaySegments, "/")
}

func buildAuditPathHint(rawPath string) string {
	canonical := canonicalAuditPath(rawPath)
	if canonical == "" {
		return ""
	}

	segments := strings.Split(strings.Trim(canonical, "/"), "/")
	hints := make([]string, 0, 2)
	for _, segment := range segments {
		if segment == "" || looksDynamicAuditSegment(segment) {
			continue
		}
		hints = append(hints, strings.ToLower(segment))
		if len(hints) >= 2 {
			break
		}
	}
	return strings.Join(hints, "/")
}

func looksDynamicAuditSegment(segment string) bool {
	segment = strings.TrimSpace(segment)
	if segment == "" {
		return false
	}

	lower := strings.ToLower(segment)
	if strings.HasPrefix(lower, ":") || strings.HasPrefix(lower, "[") || strings.Contains(lower, "${") {
		return true
	}
	if strings.HasPrefix(lower, "{") && strings.HasSuffix(lower, "}") {
		return true
	}
	if matched, _ := regexp.MatchString(`^v\d+$`, lower); matched {
		return false
	}
	if matched, _ := regexp.MatchString(`^\d+$`, lower); matched {
		return true
	}
	if matched, _ := regexp.MatchString(`^[0-9a-f]{8,}$`, lower); matched {
		return true
	}
	if matched, _ := regexp.MatchString(`^[0-9a-f]{8}-[0-9a-f-]{27,}$`, lower); matched {
		return true
	}
	if matched, _ := regexp.MatchString(`^[a-z]+-\d[\w-]*$`, lower); matched {
		return true
	}
	if matched, _ := regexp.MatchString(`^\w+-\w+-\w+-\w+$`, lower); matched && len(lower) >= 16 {
		return true
	}
	return false
}

func canonicalAuditPath(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}

	parsed, err := url.Parse(value)
	if err == nil && parsed != nil {
		if parsed.Path != "" {
			value = parsed.Path
		}
	}

	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx := strings.IndexAny(value, "?#"); idx >= 0 {
		value = value[:idx]
	}
	value = strings.ReplaceAll(value, `\`, `/`)
	if !strings.HasPrefix(value, "/") {
		value = "/" + strings.TrimLeft(value, "/")
	}
	cleaned := path.Clean(value)
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func findAuditRepoMatches(repoRoot string, specs []auditRepoSearchSpec, maxMatches int) ([]map[string]interface{}, []string) {
	if maxMatches <= 0 || len(specs) == 0 {
		return []map[string]interface{}{}, nil
	}

	matches := make([]map[string]interface{}, 0, maxMatches)
	seen := make(map[string]bool, maxMatches*2)
	skippedLargeFiles := 0

	err := filepath.WalkDir(repoRoot, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if shouldSkipAuditDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if len(matches) >= maxMatches {
			return errAuditRepoMatchLimit
		}
		if shouldSkipAuditFile(filePath) {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return nil
		}
		if info.Size() > auditRepoMaxFileBytes {
			skippedLargeFiles++
			return nil
		}

		file, err := os.Open(filePath)
		if err != nil {
			return nil
		}
		funcErr := func() error {
			defer file.Close()

			relPath := strings.ReplaceAll(strings.TrimPrefix(filePath, repoRoot), `\`, `/`)
			relPath = strings.TrimPrefix(relPath, "/")
			relLower := strings.ToLower(relPath)

			scanner := bufio.NewScanner(file)
			scanner.Buffer(make([]byte, 0, 64*1024), auditRepoMaxFileBytes)
			lineNumber := 0
			for scanner.Scan() {
				lineNumber++
				line := scanner.Text()
				for _, spec := range specs {
					if spec.regex == nil {
						continue
					}
					matchText := spec.regex.FindString(line)
					if matchText == "" {
						continue
					}

					dedupeKey := fmt.Sprintf("%s|%s|%d", spec.key, relPath, lineNumber)
					if seen[dedupeKey] {
						continue
					}
					seen[dedupeKey] = true

					score := 80
					matchType := "pattern"
					if spec.literal != "" && strings.Contains(line, spec.literal) {
						score = 96
						matchType = "exact"
					}
					if spec.pathHint != "" && strings.Contains(relLower, spec.pathHint) {
						score += 4
					}
					if spec.kind == "page_route" {
						score -= 10
					}

					matches = append(matches, map[string]interface{}{
						"contract_key": spec.key,
						"kind":         spec.kind,
						"pattern":      spec.display,
						"match_type":   matchType,
						"match":        matchText,
						"file":         relPath,
						"line":         lineNumber,
						"preview":      trimAuditPreview(line, 220),
						"score":        score,
					})

					if len(matches) >= maxMatches {
						return errAuditRepoMatchLimit
					}
				}
			}
			if scanErr := scanner.Err(); scanErr != nil {
				return nil
			}
			return nil
		}()
		if funcErr != nil {
			return funcErr
		}
		return nil
	})

	if err != nil && !errors.Is(err, errAuditRepoMatchLimit) {
		return []map[string]interface{}{}, []string{fmt.Sprintf("repo scan stopped early: %v", err)}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		scoreI := asInt(matches[i]["score"])
		scoreJ := asInt(matches[j]["score"])
		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		fileI := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", matches[i]["file"])))
		fileJ := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", matches[j]["file"])))
		if fileI != fileJ {
			return fileI < fileJ
		}
		return asInt(matches[i]["line"]) < asInt(matches[j]["line"])
	})

	warnings := []string{}
	if skippedLargeFiles > 0 {
		warnings = append(warnings, fmt.Sprintf("skipped %d large file(s) over %d bytes", skippedLargeFiles, auditRepoMaxFileBytes))
	}

	return limitMapSlice(matches, maxMatches), warnings
}

func shouldSkipAuditDir(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case ".git", ".next", ".turbo", "node_modules", "dist", "build", "coverage", "tmp", "vendor":
		return true
	default:
		return false
	}
}

func shouldSkipAuditFile(filePath string) bool {
	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".json", ".yaml", ".yml", ".graphql", ".gql":
		return false
	default:
		return true
	}
}

func trimAuditPreview(line string, maxLen int) string {
	preview := strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
	if maxLen <= 0 || len(preview) <= maxLen {
		return preview
	}
	return preview[:maxLen-3] + "..."
}
