package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

const (
	defaultAuditMaxRepoMatches           = 12
	defaultAuditDiscoverMaxSteps         = 16
	defaultAuditDiscoverMaxHints         = 8
	defaultAuditExecuteRediscoveryPasses = 4
	defaultAuditExecutePlanAppendLimit   = 12
	auditRepoMaxFileBytes                = 1 << 20
	auditResumeTTL                       = 30 * time.Minute
)

var (
	errAuditRepoMatchLimit = errors.New("audit repo match limit reached")
	auditPayloadObjectRe   = regexp.MustCompile(`(?is)(?:data|body)\s*:\s*(?:JSON\.stringify\()?\{([^{}]{0,800})\}`)
	auditObjectFieldRe     = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*:\s*([^,\n]+)`)
)

// BrowserAuditTool performs progressive frontend-to-API contract tracing.
type BrowserAuditTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

type persistedAuditRun struct {
	AuditID          string                   `json:"audit_id"`
	SessionID        string                   `json:"session_id"`
	Phase            string                   `json:"phase"`
	Status           string                   `json:"status,omitempty"`
	RepoRoot         string                   `json:"repo_root"`
	CurrentURL       string                   `json:"current_url,omitempty"`
	CurrentPath      string                   `json:"current_path,omitempty"`
	GeneratedAt      int64                    `json:"generated_at"`
	ApprovalRequired bool                     `json:"approval_required"`
	Hazards          []map[string]interface{} `json:"hazards,omitempty"`
	Plan             []map[string]interface{} `json:"plan,omitempty"`
	Findings         []map[string]interface{} `json:"findings,omitempty"`
	Handles          []string                 `json:"evidence_handles,omitempty"`
	CompletedSteps   []map[string]interface{} `json:"completed_steps,omitempty"`
	SkippedSteps     []map[string]interface{} `json:"skipped_steps,omitempty"`
}

type auditExecuteAllowances struct {
	Risky       bool
	Navigation  bool
	Mutating    bool
	Destructive bool
}

type auditDiscoverOptions struct {
	IncludeRepoMatches bool
	MaxRepoMatches     int
	TimeWindowMs       int
	SinceNavigation    bool
}

type auditDiscoverSnapshot struct {
	CurrentURL        string
	CurrentPath       string
	PageContext       map[string]interface{}
	FactSummary       map[string]interface{}
	SearchSpecRows    []map[string]interface{}
	Plan              []map[string]interface{}
	Hazards           []map[string]interface{}
	DiscoverHandles   []string
	ReportHandles     []string
	Warnings          []string
	NavigationSinceMs int64
	EffectiveSinceMs  int64
	ApprovalRequired  bool
}

func (t *BrowserAuditTool) Name() string { return "browser-audit" }

func (t *BrowserAuditTool) Description() string {
	return `Audit frontend-to-API contracts for the current browser session.

Use when a page is failing and you need to trace the active route to failing API calls,
backend correlation signals, and likely frontend source files under repo_root.

Phases:
  discover: Passive only. Builds a deterministic audit plan skeleton, hazard list, and evidence handles.
  execute:  Executes the discovered plan, skipping gated/risky steps unless explicitly allowed.
  report:   Runs the full audit, including repo matching and scoped contract synthesis.
  resume:   Continue from prior handles with focused sections instead of dumping the full audit again.

Views: summary (counts + page facts), compact (default, ranked findings + evidence),
full (all gathered facts, repo matches, and trace sections).

Results include evidence handles for progressive drill-down. Prefer browser-reason for
general triage and browser-mangle for raw ad-hoc queries.`
}

func (t *BrowserAuditTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target browser session to audit",
			},
			"repo_root": map[string]interface{}{
				"type":        "string",
				"description": "Frontend repository root to scan for contract references",
			},
			"phase": map[string]interface{}{
				"type":        "string",
				"description": "Audit phase: discover plans safely, execute allowed plan steps, report executes the contract trace, resume narrows follow-up sections",
				"enum":        []string{"discover", "execute", "report", "resume"},
			},
			"audit_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional caller-provided audit label echoed back in responses",
			},
			"resume_handle": map[string]interface{}{
				"type":        "string",
				"description": "Preferred browser-audit evidence handle to prioritize when phase=resume",
			},
			"view": map[string]interface{}{
				"type":        "string",
				"description": "summary|compact|full",
				"enum":        []string{"summary", "compact", "full"},
			},
			"max_items": map[string]interface{}{
				"type":        "integer",
				"description": "Max findings per section (default 20)",
			},
			"expand_handles": map[string]interface{}{
				"type":        "array",
				"description": "Only expand matching evidence handles; especially useful with phase=resume",
				"items":       map[string]interface{}{"type": "string"},
			},
			"time_window_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Only include evidence newer than now-window (default 300000; set 0 for all history)",
			},
			"since_navigation": map[string]interface{}{
				"type":        "boolean",
				"description": "When true, scope request/error evidence to events after the latest navigation_event",
			},
			"include_repo_matches": map[string]interface{}{
				"type":        "boolean",
				"description": "Scan repo_root for likely frontend contract references (default true)",
			},
			"include_live_observe": map[string]interface{}{
				"type":        "boolean",
				"description": "Run a live composite browser-observe pass before the report phase refreshes evidence (default true for report, false otherwise)",
			},
			"allow_risky": map[string]interface{}{
				"type":        "boolean",
				"description": "When phase=execute, allow steps marked risky by the plan (default false)",
			},
			"allow_navigation": map[string]interface{}{
				"type":        "boolean",
				"description": "When phase=execute, allow gated non-mutating navigation steps like navigate_page_route (default false)",
			},
			"allow_mutating": map[string]interface{}{
				"type":        "boolean",
				"description": "When phase=execute, allow mutating write-like actions such as click/type/submit or save-like steps (default false)",
			},
			"allow_destructive": map[string]interface{}{
				"type":        "boolean",
				"description": "When phase=execute, allow destructive delete/remove/purge actions (default false)",
			},
			"max_repo_matches": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum repo match rows to return (default 12)",
			},
		},
		"required": []string{"session_id", "repo_root"},
	}
}

func (t *BrowserAuditTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if t.engine == nil {
		return map[string]interface{}{"success": false, "error": "mangle engine is not available"}, nil
	}

	sessionID := strings.TrimSpace(getStringArg(args, "session_id"))
	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}

	repoRoot := strings.TrimSpace(getStringArg(args, "repo_root"))
	if repoRoot == "" {
		return map[string]interface{}{"success": false, "error": "repo_root is required"}, nil
	}

	absRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("resolve repo_root %q: %v", repoRoot, err),
		}, nil
	}

	info, err := os.Stat(absRepoRoot)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("repo_root %q is not accessible: %v", absRepoRoot, err),
		}, nil
	}
	if !info.IsDir() {
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("repo_root %q must be a directory", absRepoRoot),
		}, nil
	}

	view := normalizeProgressiveView(getStringArg(args, "view"))
	phase := normalizeAuditPhase(getStringArg(args, "phase"))
	auditIDArg := strings.TrimSpace(getStringArg(args, "audit_id"))
	maxItems := getIntArg(args, "max_items", defaultProgressiveMaxItems)
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}

	timeWindowMs := getIntArg(args, "time_window_ms", defaultReasonTimeWindowMs)
	if timeWindowMs < 0 {
		timeWindowMs = 0
	}
	if timeWindowMs > 86400000 {
		timeWindowMs = 86400000
	}

	sinceNavigation := getBoolArg(args, "since_navigation", true)
	includeRepoMatches := getBoolArg(args, "include_repo_matches", true)
	includeLiveObserve := getBoolArg(args, "include_live_observe", true)
	allowRisky := getBoolArg(args, "allow_risky", false)
	allowNavigation := getBoolArg(args, "allow_navigation", false)
	allowMutating := getBoolArg(args, "allow_mutating", false)
	allowDestructive := getBoolArg(args, "allow_destructive", false)
	maxRepoMatches := getIntArg(args, "max_repo_matches", defaultAuditMaxRepoMatches)
	if maxRepoMatches <= 0 {
		maxRepoMatches = defaultAuditMaxRepoMatches
	}

	if phase == "execute" {
		run, err := loadPersistedAuditRun(absRepoRoot, sessionID, auditIDArg)
		if err != nil {
			return map[string]interface{}{"success": false, "error": err.Error()}, nil
		}
		return t.executePersistedAuditRun(ctx, run, view, maxItems, auditExecuteAllowances{
			Risky:       allowRisky,
			Navigation:  allowNavigation,
			Mutating:    allowMutating,
			Destructive: allowDestructive,
		})
	}

	sinceMs := int64(0)
	if timeWindowMs > 0 {
		sinceMs = time.Now().UnixMilli() - int64(timeWindowMs)
	}
	navigationSinceMs := int64(0)
	if sinceNavigation {
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

	pageContext := buildAuditPageContext(t.sessions, sessionID, currentURL, absRepoRoot)
	runLiveObserve := includeLiveObserve && phase == "report" && t.sessions != nil
	if runLiveObserve {
		observeTool := &BrowserObserveTool{sessions: t.sessions, engine: t.engine}
		_, _ = observeTool.Execute(ctx, map[string]interface{}{
			"session_id":          sessionID,
			"mode":                "composite",
			"view":                "compact",
			"max_items":           maxItems,
			"include_action_plan": true,
			"include_diagnostics": true,
			"visible_only":        true,
		})
	}
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
	discoverRepoTrace, discoverWarnings := t.traceAuditDiscoverContext(ctx, sessionID, absRepoRoot, includeRepoMatches, maxRepoMatches)
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
	} else if len(discoverWarnings) > 0 {
		pageContext["repo_trace_available"] = false
	}
	plan := buildAuditPlanSkeleton(
		sessionID,
		absRepoRoot,
		canonicalAuditPath(currentURL),
		includeRepoMatches,
		maxRepoMatches,
		failedRequests,
		discoverRepoTrace,
	)
	hazards := buildAuditHazards(
		currentURL,
		len(searchSpecs),
		len(failedRequests),
		len(userVisibleErrors),
		includeRepoMatches,
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
		"repo_scan_planned":   includeRepoMatches,
		"repo_trace_planned":  includeRepoMatches && t.sessions != nil,
	}
	if discoverRepoTrace != nil {
		factSummary["repo_trace_plan_steps"] = len(discoverRepoTrace.AuditPlan)
		factSummary["repo_trace_navigation_links"] = len(discoverRepoTrace.BrowserContext.NavigationLinks)
		factSummary["repo_trace_controls"] = len(discoverRepoTrace.BrowserContext.Controls)
		factSummary["repo_trace_forms"] = len(discoverRepoTrace.BrowserContext.Forms)
	}
	discoverHandles := buildAuditDiscoverHandles(sessionID, discoverRepoTrace != nil)

	auditID := auditIDArg
	if auditID == "" {
		auditID = fmt.Sprintf("audit-%d", time.Now().UnixMilli())
	}

	if phase == "discover" {
		emitDisclosureFacts(ctx, t.engine, sessionID, discoverHandles, "audit")

		response := map[string]interface{}{
			"success":               true,
			"audit_id":              auditID,
			"phase":                 phase,
			"session_id":            sessionID,
			"repo_root":             absRepoRoot,
			"status":                "planned",
			"summary":               buildAuditDiscoverSummary(pageContext, factSummary, len(plan), len(hazards)),
			"audit_plan":            plan,
			"audit_hazards":         hazards,
			"evidence_handles":      discoverHandles,
			"report_handles":        buildAuditReportHandles(sessionID, includeRepoMatches, includeRepoMatches && t.sessions != nil),
			"view":                  view,
			"time_window_ms":        timeWindowMs,
			"since_navigation":      sinceNavigation,
			"navigation_since_ms":   navigationSinceMs,
			"effective_since_ms":    effectiveSinceMs,
			"approval_required":     auditHazardsRequireApproval(hazards),
			"risky_actions_skipped": true,
			"next_step": map[string]interface{}{
				"tool": "browser-audit",
				"args": map[string]interface{}{
					"session_id":           sessionID,
					"repo_root":            absRepoRoot,
					"phase":                "report",
					"view":                 "compact",
					"time_window_ms":       timeWindowMs,
					"since_navigation":     sinceNavigation,
					"include_repo_matches": includeRepoMatches,
					"max_repo_matches":     maxRepoMatches,
				},
			},
		}
		if len(discoverWarnings) > 0 {
			response["warnings"] = discoverWarnings
		}

		switch view {
		case "summary":
			response["counts"] = factSummary
			response["page_context"] = map[string]interface{}{
				"current_url":          pageContext["current_url"],
				"route_path":           pageContext["route_path"],
				"session_found":        pageContext["session_found"],
				"browser_connected":    pageContext["browser_connected"],
				"active_session_count": pageContext["active_session_count"],
			}
			if discoverRepoTrace != nil {
				response["repo_trace"] = buildAuditRepoTraceDiscoverData(discoverRepoTrace)
			}
		case "compact":
			response["data"] = map[string]interface{}{
				"page_context":  pageContext,
				"fact_summary":  factSummary,
				"search_specs":  limitMapSlice(searchSpecRows, maxItems),
				"audit_plan":    plan,
				"audit_hazards": hazards,
			}
			if discoverRepoTrace != nil {
				response["data"].(map[string]interface{})["repo_trace"] = buildAuditRepoTraceDiscoverData(discoverRepoTrace)
			}
		default:
			response["data"] = map[string]interface{}{
				"page_context":  pageContext,
				"fact_summary":  factSummary,
				"search_specs":  searchSpecRows,
				"audit_plan":    plan,
				"audit_hazards": hazards,
			}
			if discoverRepoTrace != nil {
				response["data"].(map[string]interface{})["repo_trace"] = buildAuditRepoTraceDiscoverData(discoverRepoTrace)
			}
		}

		run := persistedAuditRun{
			AuditID:          auditID,
			SessionID:        sessionID,
			Phase:            phase,
			Status:           "planned",
			RepoRoot:         absRepoRoot,
			CurrentURL:       currentURL,
			CurrentPath:      canonicalAuditPath(currentURL),
			GeneratedAt:      time.Now().UnixMilli(),
			ApprovalRequired: auditHazardsRequireApproval(hazards),
			Hazards:          hazards,
			Plan:             plan,
			Handles:          buildAuditReportHandles(sessionID, includeRepoMatches, includeRepoMatches && t.sessions != nil),
		}
		if err := persistAuditRun(absRepoRoot, run); err != nil {
			return nil, fmt.Errorf("persist discover audit run %s: %w", auditID, err)
		}
		if t.engine != nil {
			if err := t.engine.AddFacts(ctx, buildAuditPlanFacts(sessionID, auditID, phase, run.Status, plan, hazards)); err != nil {
				return nil, fmt.Errorf("emit discover audit plan facts %s: %w", auditID, err)
			}
		}

		return response, nil
	}

	repoMatches := []map[string]interface{}{}
	repoWarnings := []string{}
	mangleContractFindings := []map[string]interface{}{}
	var repoTraceReport *browser.RepoTraceReport
	if includeRepoMatches {
		repoMatches, repoWarnings = findAuditRepoMatches(absRepoRoot, searchSpecs, maxRepoMatches)
		if t.sessions != nil {
			traceReport, traceErr := t.sessions.TraceRepositoryContext(ctx, sessionID, &browser.RepoTraceOptions{
				RootDir:            absRepoRoot,
				MaxFrontendMatches: maxRepoMatches,
				MaxBackendMatches:  maxRepoMatches,
			})
			if traceErr != nil {
				repoWarnings = append(repoWarnings, "repo trace unavailable: "+traceErr.Error())
			} else if traceReport != nil {
				repoTraceReport = traceReport
				pageContext["repo_trace_id"] = traceReport.TraceID
				pageContext["repo_trace_frontend_matches"] = len(traceReport.FrontendSites)
				pageContext["repo_trace_backend_matches"] = len(traceReport.BackendMatches)
				pageContext["repo_trace_correlations"] = len(traceReport.Correlations)

				observedRequests := queryObservedAuditRequests(t.engine, sessionID, effectiveSinceMs)
				contractFacts := buildScopedContractFactsFromRepoTrace(absRepoRoot, sessionID, currentURL, traceReport, observedRequests)
				if len(contractFacts) > 0 {
					if err := t.engine.AddFacts(ctx, contractFacts); err != nil {
						repoWarnings = append(repoWarnings, "contract fact emission failed: "+err.Error())
					} else {
						mangleContractFindings = queryScopedContractAuditFindings(ctx, t.engine, sessionID)
					}
				}
			}
		}
	}
	if len(mangleContractFindings) == 0 {
		mangleContractFindings = queryScopedContractAuditFindings(ctx, t.engine, sessionID)
	}

	findings := buildContractAuditFindings(sessionID, currentURL, failedRequests, apiCorrelations, fullStackErrors, userVisibleErrors, repoMatches, maxItems)
	if len(mangleContractFindings) > 0 {
		findings = mergeAuditFindingSets(mangleContractFindings, findings, maxItems)
	}
	status := buildAuditStatus(findings)
	confidence := computeAuditConfidence(findings, pageContext)

	factSummary["repo_matches"] = len(repoMatches)
	factSummary["mangle_contracts"] = len(mangleContractFindings)

	data := map[string]interface{}{
		"page_context":        pageContext,
		"fact_summary":        factSummary,
		"search_specs":        searchSpecRows,
		"contract_findings":   findings,
		"audit_plan":          plan,
		"audit_hazards":       hazards,
		"mangle_contracts":    mangleContractFindings,
		"failed_requests":     failedRequests,
		"api_correlations":    apiCorrelations,
		"full_stack_errors":   fullStackErrors,
		"user_visible_errors": userVisibleErrors,
		"repo_matches":        repoMatches,
	}
	if repoTraceReport != nil {
		data["repo_trace"] = map[string]interface{}{
			"trace_id":        repoTraceReport.TraceID,
			"frontend_sites":  repoTraceReport.FrontendSites,
			"backend_matches": repoTraceReport.BackendMatches,
			"correlations":    repoTraceReport.Correlations,
		}
	}
	handles := buildAuditReportHandles(sessionID, includeRepoMatches, repoTraceReport != nil)
	effectiveHandles := args["expand_handles"]
	if phase == "resume" {
		effectiveHandles = resolveAuditResumeHandles(
			args,
			sessionID,
			includeRepoMatches,
			repoTraceReport != nil,
		)
	}
	selectedData := applyAuditHandleFilter(data, effectiveHandles)
	emitDisclosureFacts(ctx, t.engine, sessionID, handles, "audit")

	approvalRequired := auditHazardsRequireApproval(hazards)
	response := map[string]interface{}{
		"success":             true,
		"audit_id":            auditID,
		"phase":               phase,
		"session_id":          sessionID,
		"repo_root":           absRepoRoot,
		"status":              status,
		"confidence":          confidence,
		"summary":             buildAuditSummary(status, confidence, len(findings), len(failedRequests), len(apiCorrelations), len(repoMatches)),
		"top_findings":        limitMapSlice(findings, minInt(5, maxItems)),
		"evidence_handles":    handles,
		"view":                view,
		"time_window_ms":      timeWindowMs,
		"since_navigation":    sinceNavigation,
		"navigation_since_ms": navigationSinceMs,
		"effective_since_ms":  effectiveSinceMs,
		"approval_required":   approvalRequired,
		"next_step":           buildAuditNextStep(sessionID, findings),
	}
	if phase == "resume" {
		response["resume_handles"] = toStringSlice(effectiveHandles)
	}
	if len(repoWarnings) > 0 {
		response["warnings"] = repoWarnings
	}

	switch view {
	case "summary":
		response["counts"] = factSummary
		response["page_context"] = map[string]interface{}{
			"current_url":          pageContext["current_url"],
			"route_path":           pageContext["route_path"],
			"session_found":        pageContext["session_found"],
			"browser_connected":    pageContext["browser_connected"],
			"active_session_count": pageContext["active_session_count"],
		}
	case "compact":
		response["data"] = truncateAuditData(selectedData, maxItems)
	default:
		response["data"] = selectedData
	}

	runHandles := handles
	if phase == "resume" {
		if focused := toStringSlice(effectiveHandles); len(focused) > 0 {
			runHandles = focused
		}
	}
	run := persistedAuditRun{
		AuditID:          auditID,
		SessionID:        sessionID,
		Phase:            phase,
		Status:           status,
		RepoRoot:         absRepoRoot,
		CurrentURL:       currentURL,
		CurrentPath:      canonicalAuditPath(currentURL),
		GeneratedAt:      time.Now().UnixMilli(),
		ApprovalRequired: approvalRequired,
		Hazards:          hazards,
		Plan:             plan,
		Findings:         findings,
		Handles:          runHandles,
	}
	if err := persistAuditRun(absRepoRoot, run); err != nil {
		return nil, fmt.Errorf("persist %s audit run %s: %w", phase, auditID, err)
	}

	return response, nil
}

type auditRepoSearchSpec struct {
	key      string
	kind     string
	literal  string
	display  string
	regex    *regexp.Regexp
	pathHint string
}
