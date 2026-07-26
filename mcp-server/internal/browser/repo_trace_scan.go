package browser

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/config"

	"github.com/google/uuid"
)

func traceRepositoryWorkspace(ctx context.Context, sessionID string, browserCtx RepoTraceBrowserContext, traceCfg config.RepoTraceConfig) (*RepoTraceReport, error) {
	browserCtx = normalizeRepoTraceBrowserContext(browserCtx, traceCfg.MaxSeedHints, traceCfg.MaxNavigationHints, traceCfg.MaxControlHints)

	rootDir, err := resolveRepoTraceRoot(traceCfg.RootDir)
	if err != nil {
		return nil, err
	}

	searchRoots := resolveRepoTraceSearchRoots(rootDir, traceCfg.SearchRoots)
	seeds := buildRepoTraceSeedSet(browserCtx, traceCfg.MaxSeedHints)
	scanner := &repoTraceScanner{
		rootDir:     rootDir,
		searchRoots: searchRoots,
		cfg:         traceCfg,
		seeds:       seeds,
	}
	if err := scanner.scan(ctx); err != nil {
		return nil, err
	}

	traceID := uuid.NewString()
	generatedAt := time.Now()

	sortRepoTraceFrontend(scanner.frontendSites)
	sortRepoTraceBackend(scanner.backendRoutes)

	frontend := limitFrontendMatches(scanner.frontendSites, traceCfg.MaxFrontendMatches)
	backend := limitBackendMatches(scanner.backendRoutes, traceCfg.MaxBackendMatches)
	assignRepoTraceFrontendIDs(traceID, frontend)
	assignRepoTraceBackendIDs(traceID, backend)

	hazardSummary := summarizeRepoTraceHazards(browserCtx)
	auditPlan := buildRepoTraceAuditPlan(browserCtx, traceCfg.MaxPlanSteps)
	correlations := correlateRepoTrace(frontend, backend)
	evidence := buildRepoTraceEvidence(frontend, backend)
	handles := make([]string, 0, len(evidence))
	for _, ev := range evidence {
		handles = append(handles, ev.Handle)
	}

	report := &RepoTraceReport{
		TraceID:         traceID,
		SessionID:       sessionID,
		GeneratedAt:     generatedAt,
		RootDir:         rootDir,
		SearchRoots:     searchRoots,
		BrowserContext:  browserCtx,
		HazardSummary:   hazardSummary,
		AuditPlan:       auditPlan,
		Seeds:           seeds.seeds,
		FrontendSites:   frontend,
		BackendMatches:  backend,
		Correlations:    correlations,
		Evidence:        evidence,
		EvidenceHandles: handles,
		Warnings:        append([]string(nil), scanner.warnings...),
		Stats: RepoTraceStats{
			FilesScanned:                 scanner.filesScanned,
			FrontendMatches:              len(frontend),
			BackendMatches:               len(backend),
			CorrelationsFound:            len(correlations),
			NavigationLinks:              hazardSummary.NavigationLinks,
			InternalNavigationLinks:      hazardSummary.InternalNavigationLinks,
			InternalNavigationControls:   hazardSummary.InternalNavigationControls,
			RevealControls:               hazardSummary.RevealControls,
			WriteCapableForms:            hazardSummary.WriteCapableForms,
			WriteCapableControls:         hazardSummary.WriteCapableControls,
			DestructiveControls:          hazardSummary.DestructiveControls,
			AuthSensitiveControls:        hazardSummary.AuthSensitiveControls,
			AuthSensitiveNavigationLinks: hazardSummary.AuthSensitiveNavigationLinks,
			AuditPlanSteps:               len(auditPlan),
		},
	}
	report.Facts = buildRepoTraceFacts(report)

	return report, nil
}

func (s *repoTraceScanner) scan(ctx context.Context) error {
	for _, root := range s.searchRoots {
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Stat(root)
		if err != nil {
			s.warnings = append(s.warnings, fmt.Sprintf("repo trace skipped missing root: %s", root))
			continue
		}
		if !info.IsDir() {
			s.warnings = append(s.warnings, fmt.Sprintf("repo trace skipped non-directory root: %s", root))
			continue
		}

		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				s.warnings = append(s.warnings, fmt.Sprintf("repo trace walk error at %s: %v", path, walkErr))
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.IsDir() {
				if shouldSkipRepoTraceDir(d.Name(), s.cfg.IgnoreDirs) {
					return filepath.SkipDir
				}
				return nil
			}
			if s.filesScanned >= s.cfg.MaxFiles {
				return errRepoTraceFileLimit
			}
			if !repoTraceSourceExtensions[strings.ToLower(filepath.Ext(path))] {
				return nil
			}

			info, err := d.Info()
			if err != nil {
				return nil
			}
			if info.Size() <= 0 || info.Size() > s.cfg.MaxFileBytes {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				s.warnings = append(s.warnings, fmt.Sprintf("repo trace skipped unreadable file %s: %v", path, err))
				return nil
			}

			s.filesScanned++
			relPath, err := filepath.Rel(s.rootDir, path)
			if err != nil {
				relPath = path
			}
			relPath = filepath.ToSlash(relPath)

			s.frontendSites = append(s.frontendSites, extractRepoTraceFrontendSites(relPath, content, s.seeds)...)
			s.backendRoutes = append(s.backendRoutes, extractRepoTraceBackendRoutes(relPath, content, s.seeds)...)
			return nil
		})
		if errors.Is(err, errRepoTraceFileLimit) {
			s.warnings = append(s.warnings, fmt.Sprintf("repo trace hit file scan limit (%d files)", s.cfg.MaxFiles))
			return nil
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func extractRepoTraceFrontendSites(relPath string, content []byte, seeds repoTraceSeedSet) []RepoTraceFrontendSite {
	lines := strings.Split(string(content), "\n")
	matches := make([]RepoTraceFrontendSite, 0, 4)
	seen := make(map[string]bool)

	for i, line := range lines {
		if !containsRepoTraceRequestKeyword(strings.ToLower(line)) {
			continue
		}

		window := repoTraceLineWindow(lines, i, 1, 5)
		method, endpoint, requestCall := extractRepoTraceFrontendCall(window)
		endpoint = normalizeRepoTracePath(endpoint)
		if endpoint == "" || !looksLikeRepoTracePath(endpoint) {
			continue
		}

		confidence, reasons := scoreRepoTraceFrontendMatch(relPath, endpoint, method, window, seeds)
		if confidence < 0.25 {
			continue
		}

		key := fmt.Sprintf("%s|%d|%s|%s", relPath, i+1, method, endpoint)
		if seen[key] {
			continue
		}
		seen[key] = true

		summaryMethod := method
		if summaryMethod == "" {
			summaryMethod = "REQUEST"
		}
		matches = append(matches, RepoTraceFrontendSite{
			FilePath:     relPath,
			Line:         i + 1,
			Method:       method,
			Endpoint:     endpoint,
			RequestCall:  requestCall,
			Confidence:   confidence,
			Summary:      fmt.Sprintf("%s %s via %s", summaryMethod, endpoint, requestCall),
			MatchReasons: reasons,
		})
	}

	return matches
}

func extractRepoTraceBackendRoutes(relPath string, content []byte, seeds repoTraceSeedSet) []RepoTraceBackendExpectation {
	lines := strings.Split(string(content), "\n")
	matches := make([]RepoTraceBackendExpectation, 0, 4)
	seen := make(map[string]bool)

	if inferredPath, inferredMethod := inferRepoTraceRouteFromFilePath(relPath); inferredPath != "" {
		confidence, reasons := scoreRepoTraceBackendMatch(relPath, inferredPath, inferredMethod, relPath, seeds)
		if confidence >= 0.25 {
			key := fmt.Sprintf("%s|%s|%s|%d", relPath, inferredMethod, inferredPath, 1)
			seen[key] = true
			matches = append(matches, RepoTraceBackendExpectation{
				FilePath:     relPath,
				Line:         1,
				Method:       inferredMethod,
				RoutePath:    inferredPath,
				Confidence:   confidence,
				Summary:      fmt.Sprintf("file-route %s %s", fallbackRepoTraceMethod(inferredMethod), inferredPath),
				MatchReasons: append([]string{"route inferred from file path"}, reasons...),
			})
		}
	}

	for i, line := range lines {
		if !containsRepoTraceRouteKeyword(strings.ToLower(line)) {
			continue
		}
		window := repoTraceLineWindow(lines, i, 2, 4)
		method, routePath := extractRepoTraceBackendRoute(window)
		routePath = normalizeRepoTracePath(routePath)
		if routePath == "" || !looksLikeRepoTracePath(routePath) {
			continue
		}

		confidence, reasons := scoreRepoTraceBackendMatch(relPath, routePath, method, window, seeds)
		if confidence < 0.25 {
			continue
		}

		authExpectation := summarizeRepoTraceAuth(window)
		payloadExpectation := summarizeRepoTracePayload(window)
		summary := fmt.Sprintf("%s %s", fallbackRepoTraceMethod(method), routePath)
		if authExpectation != "" {
			summary += " auth=" + authExpectation
		}
		if payloadExpectation != "" {
			summary += " payload=" + payloadExpectation
		}

		key := fmt.Sprintf("%s|%d|%s|%s", relPath, i+1, method, routePath)
		if seen[key] {
			continue
		}
		seen[key] = true

		matches = append(matches, RepoTraceBackendExpectation{
			FilePath:           relPath,
			Line:               i + 1,
			Method:             method,
			RoutePath:          routePath,
			AuthExpectation:    authExpectation,
			PayloadExpectation: payloadExpectation,
			Confidence:         confidence,
			Summary:            summary,
			MatchReasons:       reasons,
		})
	}

	return matches
}

func resolveRepoTraceConfig(base config.RepoTraceConfig, opts *RepoTraceOptions) config.RepoTraceConfig {
	cfg := base
	if !cfg.Enabled &&
		cfg.RootDir == "" &&
		len(cfg.SearchRoots) == 0 &&
		len(cfg.IgnoreDirs) == 0 &&
		cfg.MaxFiles == 0 &&
		cfg.MaxFileBytes == 0 &&
		cfg.MaxSeedHints == 0 &&
		cfg.MaxFrontendMatches == 0 &&
		cfg.MaxBackendMatches == 0 {
		cfg.Enabled = true
	}

	if opts != nil {
		if strings.TrimSpace(opts.RootDir) != "" {
			cfg.RootDir = opts.RootDir
		}
		if len(opts.SearchRoots) > 0 {
			cfg.SearchRoots = append([]string(nil), opts.SearchRoots...)
		}
		if len(opts.IgnoreDirs) > 0 {
			cfg.IgnoreDirs = append([]string(nil), opts.IgnoreDirs...)
		}
		if opts.MaxFiles > 0 {
			cfg.MaxFiles = opts.MaxFiles
		}
		if opts.MaxFileBytes > 0 {
			cfg.MaxFileBytes = opts.MaxFileBytes
		}
		if opts.MaxSeedHints > 0 {
			cfg.MaxSeedHints = opts.MaxSeedHints
		}
		if opts.MaxNavigationHints > 0 {
			cfg.MaxNavigationHints = opts.MaxNavigationHints
		}
		if opts.MaxControlHints > 0 {
			cfg.MaxControlHints = opts.MaxControlHints
		}
		if opts.MaxPlanSteps > 0 {
			cfg.MaxPlanSteps = opts.MaxPlanSteps
		}
		if opts.MaxFrontendMatches > 0 {
			cfg.MaxFrontendMatches = opts.MaxFrontendMatches
		}
		if opts.MaxBackendMatches > 0 {
			cfg.MaxBackendMatches = opts.MaxBackendMatches
		}
	}

	if len(cfg.SearchRoots) == 0 {
		cfg.SearchRoots = []string{"."}
	}
	if len(cfg.IgnoreDirs) == 0 {
		cfg.IgnoreDirs = append([]string(nil), defaultRepoTraceIgnoreDirs...)
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = defaultRepoTraceMaxFiles
	}
	if cfg.MaxFileBytes <= 0 {
		cfg.MaxFileBytes = defaultRepoTraceMaxFileBytes
	}
	if cfg.MaxSeedHints <= 0 {
		cfg.MaxSeedHints = defaultRepoTraceMaxSeedHints
	}
	if cfg.MaxNavigationHints <= 0 {
		cfg.MaxNavigationHints = defaultRepoTraceMaxNavigationHints
	}
	if cfg.MaxControlHints <= 0 {
		cfg.MaxControlHints = defaultRepoTraceMaxControlHints
	}
	if cfg.MaxPlanSteps <= 0 {
		cfg.MaxPlanSteps = defaultRepoTraceMaxPlanSteps
	}
	if cfg.MaxFrontendMatches <= 0 {
		cfg.MaxFrontendMatches = defaultRepoTraceMaxFrontendMatches
	}
	if cfg.MaxBackendMatches <= 0 {
		cfg.MaxBackendMatches = defaultRepoTraceMaxBackendMatches
	}

	return cfg
}

func resolveRepoTraceRoot(root string) (string, error) {
	if strings.TrimSpace(root) != "" {
		absPath, err := filepath.Abs(root)
		if err != nil {
			return "", fmt.Errorf("resolve repo trace root: %w", err)
		}
		return absPath, nil
	}

	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get working directory for repo trace: %w", err)
	}

	workspaceDir, err := config.DiscoverWorkspace(cwd)
	if err != nil {
		return "", fmt.Errorf("discover workspace for repo trace: %w", err)
	}
	if workspaceDir != "" {
		return workspaceDir, nil
	}
	return cwd, nil
}

func resolveRepoTraceSearchRoots(rootDir string, searchRoots []string) []string {
	roots := make([]string, 0, len(searchRoots))
	seen := make(map[string]bool)
	if len(searchRoots) == 0 {
		return []string{rootDir}
	}
	for _, root := range searchRoots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		resolved := root
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(rootDir, root)
		}
		resolved = filepath.Clean(resolved)
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		roots = append(roots, resolved)
	}
	if len(roots) == 0 {
		return []string{rootDir}
	}
	return roots
}
