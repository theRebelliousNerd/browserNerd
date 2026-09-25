package mcp

import (
	"context"
	"fmt"
	"time"

	"browsernerd-mcp-server/internal/correlation"
	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/mangle"
)

// =============================================================================
// GET CONSOLE ERRORS - Diagnostic tool for error analysis
// =============================================================================

// GetConsoleErrorsTool provides a diagnostic report of all console errors with causal analysis.
type GetConsoleErrorsTool struct {
	engine       *mangle.Engine
	dockerClient *docker.Client
}

func (t *GetConsoleErrorsTool) Name() string { return "get-console-errors" }
func (t *GetConsoleErrorsTool) Description() string {
	return `Full error report with causal analysis and Docker correlation.

TOKEN COST: Medium-High (detailed data, use diagnose-page first)

HIERARCHY:
1. diagnose-page -> Quick health check (use first)
2. get-console-errors -> Full details (use when diagnose shows errors)
3. get-toast-notifications -> User-visible errors only

RETURNS:
- Console errors grouped by near-identical message (numbers and ids folded), errors before
  warnings, each with count, first_ms, last_ms and API correlation (the request that caused it)
- Failed requests (status >= 400)
- Slow APIs (>1 second response)
- Docker backend correlations (if enabled)

WHEN TO USE:
- After diagnose-page shows status: "error"
- For detailed debugging of specific failures
- Full-stack tracing (frontend -> API -> backend)

WHEN NOT TO USE:
- Quick health checks -> use diagnose-page
- Just checking if page loaded -> use get-page-state

Returns: {error_count (events), group_count, errors[] (groups), failed_requests[], slow_apis[], docker_errors[]}`
}
func (t *GetConsoleErrorsTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional: Filter to a specific session_id to avoid cross-session contamination",
			},
			"include_warnings": map[string]interface{}{
				"type":        "boolean",
				"description": "Include console warnings in addition to errors (default: false)",
			},
			"include_all_levels": map[string]interface{}{
				"type":        "boolean",
				"description": "Include ALL console events (log, info, debug, etc.) for diagnostics",
			},
			"since_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Only include events after this timestamp (epoch ms). Default: all events.",
			},
			"debug": map[string]interface{}{
				"type":        "boolean",
				"description": "Include diagnostic info about what's in the fact buffer",
			},
		},
	}
}
func (t *GetConsoleErrorsTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	includeWarnings := getBoolArg(args, "include_warnings", false)
	includeAllLevels := getBoolArg(args, "include_all_levels", false)
	debugMode := getBoolArg(args, "debug", false)
	sinceMs := int64(getIntArg(args, "since_ms", 0))

	toInt64 := func(v interface{}) int64 {
		switch t := v.(type) {
		case int:
			return int64(t)
		case int64:
			return t
		case float64:
			return int64(t)
		case float32:
			return int64(t)
		default:
			return 0
		}
	}

	// Index raw facts for fast lookups.
	type reqInfo struct {
		Method    string
		URL       string
		Timestamp int64
	}

	consoleEvents := t.engine.FactsByPredicate("console_event")
	netRequests := t.engine.FactsByPredicate("net_request")
	netResponses := t.engine.FactsByPredicate("net_response")

	reqIndex := make(map[string]map[string]reqInfo) // session -> reqId -> info
	respIndex := make(map[string]map[string]any)    // session -> reqId -> status

	for _, req := range netRequests {
		if len(req.Args) < 6 {
			continue
		}
		sess := fmt.Sprintf("%v", req.Args[0])
		if sessionID != "" && sess != sessionID {
			continue
		}
		reqID := fmt.Sprintf("%v", req.Args[1])
		if reqID == "" {
			continue
		}
		if _, ok := reqIndex[sess]; !ok {
			reqIndex[sess] = make(map[string]reqInfo)
		}
		reqIndex[sess][reqID] = reqInfo{
			Method:    fmt.Sprintf("%v", req.Args[2]),
			URL:       fmt.Sprintf("%v", req.Args[3]),
			Timestamp: toInt64(req.Args[5]),
		}
	}

	for _, resp := range netResponses {
		if len(resp.Args) < 3 {
			continue
		}
		sess := fmt.Sprintf("%v", resp.Args[0])
		if sessionID != "" && sess != sessionID {
			continue
		}
		reqID := fmt.Sprintf("%v", resp.Args[1])
		if reqID == "" {
			continue
		}
		if _, ok := respIndex[sess]; !ok {
			respIndex[sess] = make(map[string]any)
		}
		respIndex[sess][reqID] = resp.Args[2]
	}

	// Derived facts (must come from the Mangle store, not the temporal buffer).
	errorChainFacts, _ := t.engine.Evaluate(ctx, "error_chain")
	causedByFacts, _ := t.engine.Evaluate(ctx, "caused_by")
	failedFacts, _ := t.engine.Evaluate(ctx, "failed_request")
	slowFacts, _ := t.engine.Evaluate(ctx, "slow_api")
	cascadeFacts, _ := t.engine.Evaluate(ctx, "cascading_failure")

	type chainInfo struct {
		ReqID  string
		URL    string
		Status interface{}
	}

	errorChains := make(map[string]map[string]chainInfo) // session -> consoleMsg -> info
	for _, chain := range errorChainFacts {
		if len(chain.Args) < 5 {
			continue
		}
		sess := fmt.Sprintf("%v", chain.Args[0])
		if sessionID != "" && sess != sessionID {
			continue
		}
		msg := fmt.Sprintf("%v", chain.Args[1])
		if msg == "" {
			continue
		}
		if _, ok := errorChains[sess]; !ok {
			errorChains[sess] = make(map[string]chainInfo)
		}
		errorChains[sess][msg] = chainInfo{
			ReqID:  fmt.Sprintf("%v", chain.Args[2]),
			URL:    fmt.Sprintf("%v", chain.Args[3]),
			Status: chain.Args[4],
		}
	}

	causedBy := make(map[string]map[string]string) // session -> consoleMsg -> reqId
	for _, cb := range causedByFacts {
		if len(cb.Args) < 3 {
			continue
		}
		sess := fmt.Sprintf("%v", cb.Args[0])
		if sessionID != "" && sess != sessionID {
			continue
		}
		msg := fmt.Sprintf("%v", cb.Args[1])
		if msg == "" {
			continue
		}
		if _, ok := causedBy[sess]; !ok {
			causedBy[sess] = make(map[string]string)
		}
		causedBy[sess][msg] = fmt.Sprintf("%v", cb.Args[2])
	}

	// Collect console errors (and optionally warnings).
	errors := make([]map[string]interface{}, 0)
	for _, ev := range consoleEvents {
		if len(ev.Args) < 4 {
			continue
		}
		evSession := fmt.Sprintf("%v", ev.Args[0])
		if sessionID != "" && evSession != sessionID {
			continue
		}
		level := fmt.Sprintf("%v", ev.Args[1])
		message := fmt.Sprintf("%v", ev.Args[2])
		timestamp := toInt64(ev.Args[3])

		if !includeAllLevels {
			if level != "error" && !(includeWarnings && level == "warning") {
				continue
			}
		}

		if sinceMs > 0 && timestamp > 0 && timestamp < sinceMs {
			continue
		}

		errorEntry := map[string]interface{}{
			"session_id": evSession,
			"level":      level,
			"message":    message,
			"timestamp":  timestamp,
		}

		if bySession, ok := errorChains[evSession]; ok {
			if info, ok := bySession[message]; ok && info.ReqID != "" {
				errorEntry["caused_by"] = map[string]interface{}{
					"request_id": info.ReqID,
					"url":        info.URL,
					"status":     info.Status,
				}
			}
		}

		// If no error_chain, try caused_by + raw request/response details.
		if errorEntry["caused_by"] == nil {
			if bySession, ok := causedBy[evSession]; ok {
				if reqID, ok := bySession[message]; ok && reqID != "" {
					errorEntry["caused_by_request_id"] = reqID

					cause := map[string]interface{}{
						"request_id": reqID,
					}
					if req, ok := reqIndex[evSession][reqID]; ok {
						cause["method"] = req.Method
						cause["url"] = req.URL
					}
					if status, ok := respIndex[evSession][reqID]; ok {
						cause["status"] = status
					}
					errorEntry["caused_by"] = cause
				}
			}
		}

		errors = append(errors, errorEntry)
	}
	eventCount := len(errors)
	errors = groupConsoleEntries(errors, sessionID != "")

	// Collect failed requests (status >= 400).
	failedRequests := make([]map[string]interface{}, 0)
	for _, f := range failedFacts {
		if len(f.Args) < 4 {
			continue
		}
		sess := fmt.Sprintf("%v", f.Args[0])
		if sessionID != "" && sess != sessionID {
			continue
		}
		reqID := fmt.Sprintf("%v", f.Args[1])
		url := fmt.Sprintf("%v", f.Args[2])
		status := f.Args[3]
		timestamp := int64(0)
		if req, ok := reqIndex[sess][reqID]; ok {
			timestamp = req.Timestamp
		}
		if sinceMs > 0 && timestamp > 0 && timestamp < sinceMs {
			continue
		}
		failedRequests = append(failedRequests, map[string]interface{}{
			"session_id": sess,
			"request_id": reqID,
			"url":        url,
			"status":     status,
			"timestamp":  timestamp,
		})
	}

	// Collect slow API calls (>1 second).
	slowApis := make([]map[string]interface{}, 0)
	for _, s := range slowFacts {
		if len(s.Args) < 4 {
			continue
		}
		sess := fmt.Sprintf("%v", s.Args[0])
		if sessionID != "" && sess != sessionID {
			continue
		}
		slowApis = append(slowApis, map[string]interface{}{
			"session_id": sess,
			"request_id": fmt.Sprintf("%v", s.Args[1]),
			"url":        fmt.Sprintf("%v", s.Args[2]),
			"duration":   s.Args[3],
		})
	}

	// Collect cascading failures.
	cascadingFailures := make([]map[string]interface{}, 0)
	for _, c := range cascadeFacts {
		if len(c.Args) < 3 {
			continue
		}
		sess := fmt.Sprintf("%v", c.Args[0])
		if sessionID != "" && sess != sessionID {
			continue
		}
		cascadingFailures = append(cascadingFailures, map[string]interface{}{
			"session_id":        sess,
			"child_request_id":  fmt.Sprintf("%v", c.Args[1]),
			"parent_request_id": fmt.Sprintf("%v", c.Args[2]),
		})
	}

	// ==========================================================================
	// Docker Log Integration (Full-Stack Error Correlation)
	// ==========================================================================
	// Only query Docker logs if there are actual errors or failed requests to correlate
	var dockerErrors []map[string]interface{}
	var backendCorrelations []map[string]interface{}
	var containerHealth map[string]interface{}
	var dockerQueryError string

	hasErrorsToCorrelate := eventCount > 0 || len(failedRequests) > 0 || len(slowApis) > 0

	if t.dockerClient != nil && hasErrorsToCorrelate {
		// Determine time window - look back from earliest observed timestamp.
		earliestMs := int64(0)
		for _, e := range errors {
			ts, _ := e["first_ms"].(int64)
			if ts <= 0 {
				continue
			}
			if earliestMs == 0 || ts < earliestMs {
				earliestMs = ts
			}
		}
		for _, fr := range failedRequests {
			ts, _ := fr["timestamp"].(int64)
			if ts <= 0 {
				continue
			}
			if earliestMs == 0 || ts < earliestMs {
				earliestMs = ts
			}
		}
		earliest := time.Now().Add(-30 * time.Second)
		if earliestMs > 0 {
			earliest = time.UnixMilli(earliestMs).Add(-5 * time.Second)
		}

		// Query Docker logs
		logs, err := t.dockerClient.QueryLogs(ctx, earliest)
		if err != nil {
			dockerQueryError = err.Error()
		}
		if len(logs) > 0 {
			// Filter to errors and warnings
			errorLogs := t.dockerClient.FilterErrors(logs)

			// Build facts for Mangle and response entries
			var dockerFacts []mangle.Fact
			for _, log := range errorLogs {
				dockerFacts = append(dockerFacts, mangle.Fact{
					Predicate: "docker_log",
					Args: []interface{}{
						log.Container,
						log.Level,
						log.Tag,
						log.Message,
						log.Timestamp.UnixMilli(),
					},
					Timestamp: log.Timestamp,
				})

				// Build docker error entry for response
				errorEntry := map[string]interface{}{
					"container": log.Container,
					"level":     log.Level,
					"tag":       log.Tag,
					"message":   log.Message,
					"timestamp": log.Timestamp.UnixMilli(),
				}

				correlationKeys := correlation.FromMessage(log.Message)
				if len(correlationKeys) > 0 {
					keyEntries := make([]map[string]string, 0, len(correlationKeys))
					for _, key := range correlationKeys {
						dockerFacts = append(dockerFacts, mangle.Fact{
							Predicate: "docker_log_correlation",
							Args: []interface{}{
								log.Container,
								key.Type,
								key.Value,
								log.Message,
								log.Timestamp.UnixMilli(),
							},
							Timestamp: log.Timestamp,
						})
						keyEntries = append(keyEntries, map[string]string{
							"key_type":  key.Type,
							"key_value": key.Value,
						})
					}
					errorEntry["correlation_keys"] = keyEntries
				}

				dockerErrors = append(dockerErrors, errorEntry)
			}

			// Push all docker_log facts to Mangle for correlation
			if len(dockerFacts) > 0 {
				_ = t.engine.AddFacts(ctx, dockerFacts)
			}

			// Query Mangle for backend correlations
			// api_backend_correlation(SessionId, ReqId, Url, Status, BackendMsg, TimeDelta)
			correlationFacts, _ := t.engine.Evaluate(ctx, "api_backend_correlation")
			for _, cf := range correlationFacts {
				if len(cf.Args) < 6 {
					continue
				}
				sess := fmt.Sprintf("%v", cf.Args[0])
				if sessionID != "" && sess != sessionID {
					continue
				}
				backendCorrelations = append(backendCorrelations, map[string]interface{}{
					"session_id":    sess,
					"request_id":    fmt.Sprintf("%v", cf.Args[1]),
					"url":           fmt.Sprintf("%v", cf.Args[2]),
					"status":        cf.Args[3],
					"backend_error": fmt.Sprintf("%v", cf.Args[4]),
					"time_delta_ms": cf.Args[5],
					"container":     "backend",
					"mode":          "keyed",
				})
			}

			// Get container health analysis
			health := t.dockerClient.AnalyzeHealth(logs)
			containerHealth = make(map[string]interface{})
			for name, h := range health {
				containerHealth[name] = map[string]interface{}{
					"status":        h.Status,
					"error_count":   h.ErrorCount,
					"warning_count": h.WarningCount,
				}
			}
		}
	}

	result := map[string]interface{}{
		"success":              true,
		"error_count":          eventCount,
		"group_count":          len(errors),
		"errors":               errors,
		"failed_request_count": len(failedRequests),
		"failed_requests":      failedRequests,
		"slow_api_count":       len(slowApis),
		"slow_apis":            slowApis,
		"cascading_failures":   cascadingFailures,
	}

	// Add Docker integration results only if enabled AND we queried for correlations
	if t.dockerClient != nil {
		result["docker_enabled"] = true
		// Only include Docker data if we actually queried (had errors to correlate)
		if hasErrorsToCorrelate {
			result["docker_error_count"] = len(dockerErrors)
			result["docker_errors"] = dockerErrors
			result["backend_correlations"] = backendCorrelations
			result["container_health"] = containerHealth
			if dockerQueryError != "" {
				result["docker_query_error"] = dockerQueryError
			}

			// Summary message
			if len(backendCorrelations) > 0 {
				result["correlation_summary"] = fmt.Sprintf(
					"%d frontend failures linked to %d backend errors across %d containers",
					len(backendCorrelations), len(dockerErrors), len(containerHealth))
			}
		}
	} else {
		result["docker_enabled"] = false
	}

	// Add debug info showing what's in the buffer
	if debugMode {
		// Count console events by level
		levelCounts := make(map[string]int)
		for _, ev := range consoleEvents {
			if len(ev.Args) < 2 {
				continue
			}
			evSession := fmt.Sprintf("%v", ev.Args[0])
			if sessionID != "" && evSession != sessionID {
				continue
			}
			level := fmt.Sprintf("%v", ev.Args[1])
			levelCounts[level]++
		}

		// Count all predicates in the buffer
		predicateCounts := make(map[string]int)
		allFacts := t.engine.Facts()
		for _, f := range allFacts {
			predicateCounts[f.Predicate]++
		}

		result["debug"] = map[string]interface{}{
			"total_facts_in_buffer":   len(allFacts),
			"console_events_by_level": levelCounts,
			"facts_by_predicate":      predicateCounts,
			"session_filter":          sessionID,
			"note": "Server-side logs (Next.js RSC/SSR) are NOT captured by CDP. " +
				"Only client-side JavaScript console calls are visible. " +
				"Events must occur AFTER session is created to be captured.",
		}
	}

	return result, nil
}

// =============================================================================
// GET TOAST NOTIFICATIONS - Instant error overlay detection
// =============================================================================

// GetToastNotificationsTool provides instant visibility into user-visible toast errors.
type GetToastNotificationsTool struct {
	engine *mangle.Engine
}

func (t *GetToastNotificationsTool) Name() string { return "get-toast-notifications" }
func (t *GetToastNotificationsTool) Description() string {
	return `Get all toast/notification overlays detected on the page.

WHAT IT DOES:
- Captures toast notifications from popular UI libraries (Material-UI, Chakra, Ant Design, shadcn, react-toastify, etc.)
- Detects error, warning, success, and info level toasts
- Correlates error toasts with failed API requests
- Provides instant visibility into user-visible errors (faster than console.error)

DETECTION METHODS:
- MutationObserver watches for dynamically added toast elements
- Detects role="alert", role="status", aria-live attributes
- Recognizes common CSS class patterns (toast, notification, snackbar, etc.)

EXAMPLE OUTPUT:
{
  "toasts": [
    {
      "text": "Failed to save changes",
      "level": "error",
      "source": "shadcn",
      "timestamp": 1732481234567,
      "correlated_api_failure": {
        "request_id": "req-123",
        "url": "/api/save",
        "status": 500
      }
    }
  ],
  "error_count": 1,
  "warning_count": 0,
  "repeated_errors": ["Failed to save changes"]
}

WHEN TO USE:
- After any user action that might trigger an error
- To detect form validation errors shown via toast
- To verify success messages appear after operations
- For debugging - error toasts often appear before console.error`
}
func (t *GetToastNotificationsTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional: filter toasts to a specific session_id (recommended for token efficiency).",
			},
			"level": map[string]interface{}{
				"type":        "string",
				"description": "Filter by level: 'error', 'warning', 'success', 'info', or 'all' (default: 'all')",
				"enum":        []string{"error", "warning", "success", "info", "all"},
			},
			"view": map[string]interface{}{
				"type":        "string",
				"description": "summary|compact|full (default: compact). summary omits toast rows; compact returns a small sample; full returns all (subject to limit).",
				"enum":        []string{"summary", "compact", "full"},
			},
			"since_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Only include toasts after this timestamp (epoch ms). Default: all.",
			},
			"include_correlations": map[string]interface{}{
				"type":        "boolean",
				"description": "Include API failure correlations for error toasts (default: true)",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of toasts to return (default: all)",
			},
		},
	}
}
func (t *GetToastNotificationsTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionFilter := getStringArg(args, "session_id")
	levelFilter := getStringArg(args, "level")
	if levelFilter == "" {
		levelFilter = "all"
	}
	view := normalizeProgressiveView(getStringArg(args, "view"))
	sinceMs := int64(getIntArg(args, "since_ms", 0))
	includeCorrelations := getBoolArg(args, "include_correlations", view == "full")
	limit := getIntArg(args, "limit", 0)
	if limit <= 0 && view != "full" {
		limit = 10
	}

	// Collect toast notifications from the temporal buffer. (These are user-visible UI overlays
	// captured via DOM mutation observation; they are high-signal and usually low-volume.)
	toastFacts := t.engine.FactsByPredicate("toast_notification")
	toasts := make([]map[string]interface{}, 0)

	errorCount := 0
	warningCount := 0
	successCount := 0
	infoCount := 0

	// Correlation predicates are derived and are not stored in the temporal buffer.
	// We query the Mangle store directly once per tool invocation.
	correlationByToastKey := make(map[string]map[string]interface{})
	if includeCorrelations {
		var correlationRows []map[string]interface{}
		if sessionFilter != "" {
			correlationRows = queryToRows(ctx, t.engine, fmt.Sprintf(
				"toast_after_api_failure(%q, ToastText, ReqId, Url, Status, TimeDelta).",
				sessionFilter,
			))
		} else {
			correlationRows = queryToRows(ctx, t.engine, "toast_after_api_failure(SessionId, ToastText, ReqId, Url, Status, TimeDelta).")
		}
		for _, row := range correlationRows {
			rowSession := sessionFilter
			if rowSession == "" {
				rowSession = fmt.Sprintf("%v", row["SessionId"])
			}
			if rowSession == "" {
				continue
			}
			toastText := fmt.Sprintf("%v", row["ToastText"])
			if toastText == "" {
				continue
			}
			// First match wins; multiple correlations for the same toast text are unlikely.
			key := rowSession + "|" + toastText
			if _, exists := correlationByToastKey[key]; exists {
				continue
			}
			correlationByToastKey[key] = map[string]interface{}{
				"request_id":    fmt.Sprintf("%v", row["ReqId"]),
				"url":           fmt.Sprintf("%v", row["Url"]),
				"status":        row["Status"],
				"time_delta_ms": row["TimeDelta"],
			}
		}
	}

	for _, f := range toastFacts {
		// toast_notification(SessionId, Text, Level, Source, Timestamp)
		if len(f.Args) < 5 {
			continue
		}
		sessionID := fmt.Sprintf("%v", f.Args[0])
		if sessionFilter != "" && sessionID != sessionFilter {
			continue
		}
		text := fmt.Sprintf("%v", f.Args[1])
		level := fmt.Sprintf("%v", f.Args[2])
		source := fmt.Sprintf("%v", f.Args[3])
		timestamp := asInt64(f.Args[4])

		// Filter by timestamp
		if sinceMs > 0 && timestamp < sinceMs {
			continue
		}

		// Filter by level
		if levelFilter != "all" && level != levelFilter {
			continue
		}

		// Count by level
		switch level {
		case "error":
			errorCount++
		case "warning":
			warningCount++
		case "success":
			successCount++
		case "info":
			infoCount++
		}

		toastEntry := map[string]interface{}{
			"text":      text,
			"level":     level,
			"source":    source,
			"timestamp": timestamp,
		}

		// Look for API correlation for error toasts
		if includeCorrelations && level == "error" {
			if corr, ok := correlationByToastKey[sessionID+"|"+text]; ok {
				toastEntry["correlated_api_failure"] = corr
			}
		}

		toasts = append(toasts, toastEntry)
	}

	// Apply limit if specified
	if limit > 0 && len(toasts) > limit {
		toasts = toasts[:limit]
	}

	// Find repeated error messages
	repeatedErrors := make([]string, 0)
	if sessionFilter != "" {
		rows := queryToRows(ctx, t.engine, fmt.Sprintf("repeated_toast_error(%q, Msg).", sessionFilter))
		for _, row := range rows {
			repeatedErrors = append(repeatedErrors, fmt.Sprintf("%v", row["Msg"]))
		}
	} else {
		rows := queryToRows(ctx, t.engine, "repeated_toast_error(SessionId, Msg).")
		for _, row := range rows {
			repeatedErrors = append(repeatedErrors, fmt.Sprintf("%v", row["Msg"]))
		}
	}

	result := map[string]interface{}{
		"success":         true,
		"toast_count":     len(toasts),
		"toasts":          toasts,
		"error_count":     errorCount,
		"warning_count":   warningCount,
		"success_count":   successCount,
		"info_count":      infoCount,
		"repeated_errors": repeatedErrors,
	}

	// Add summary for quick reading
	if errorCount > 0 {
		result["status"] = "errors_detected"
		result["summary"] = fmt.Sprintf("%d error toast(s) detected - user has seen error messages", errorCount)
	} else if warningCount > 0 {
		result["status"] = "warnings_detected"
		result["summary"] = fmt.Sprintf("%d warning toast(s) detected", warningCount)
	} else if len(toasts) > 0 {
		result["status"] = "ok"
		result["summary"] = fmt.Sprintf("%d toast(s) captured, no errors", len(toasts))
	} else {
		result["status"] = "no_toasts"
		result["summary"] = "No toast notifications detected yet"
	}

	// Progressive disclosure: omit the toast rows unless requested.
	if view == "summary" {
		delete(result, "toasts")
		if errs, ok := result["repeated_errors"].([]string); ok && len(errs) > 5 {
			result["repeated_errors"] = errs[:5]
		}
	} else if view == "compact" {
		if errs, ok := result["repeated_errors"].([]string); ok && len(errs) > 10 {
			result["repeated_errors"] = errs[:10]
		}
	}

	return result, nil
}

// DiagnosePageTool runs Mangle queries to find root causes of errors.
type DiagnosePageTool struct {
	engine *mangle.Engine
}

func (t *DiagnosePageTool) Name() string { return "diagnose-page" }
func (t *DiagnosePageTool) Description() string {
	return `Quick health check - find errors without reading raw logs.

TOKEN COST: Very Low (structured summary, not raw data)

PREFER THIS OVER: get-console-errors when you just need a yes/no on errors.

RETURNS:
- status: "ok" | "warning" | "error"
- root_causes: Causal analysis of what went wrong
- failed_requests: API calls that returned errors
- slow_apis: Performance bottlenecks

WHEN TO USE:
- After navigation to verify page loaded correctly
- Quick sanity check before interacting
- Debugging when something seems broken

USE get-console-errors INSTEAD when you need full error details.

Returns: {status, root_causes[], failed_requests[], slow_apis[]}`
}
func (t *DiagnosePageTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional: scope diagnostics to a session_id (recommended).",
			},
			"view": map[string]interface{}{
				"type":        "string",
				"description": "summary|compact|full (default: summary). summary returns counts only; compact returns limited rows; full returns all rows.",
				"enum":        []string{"summary", "compact", "full"},
			},
			"max_items": map[string]interface{}{
				"type":        "integer",
				"description": "Max rows returned per category in compact view (default 20).",
			},
		},
	}
}
func (t *DiagnosePageTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	if t.engine == nil {
		return map[string]interface{}{
			"status":          "error",
			"root_causes":     []map[string]interface{}{},
			"failed_requests": []map[string]interface{}{},
			"slow_apis":       []map[string]interface{}{},
			"summary":         "mangle engine unavailable",
		}, nil
	}

	view := normalizeProgressiveView(getStringArg(args, "view"))
	if !argHasNonEmptyString(args, "view") {
		view = "summary"
	}
	maxItems := getIntArg(args, "max_items", defaultProgressiveMaxItems)
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}

	// Keep this resilient: query errors should degrade to empty rows rather
	// than causing an empty or failed tool payload.
	sessionFilter := getStringArg(args, "session_id")
	var rootCauses []map[string]interface{}
	var failedReqs []map[string]interface{}
	var slowApis []map[string]interface{}
	if sessionFilter != "" {
		rootCauses = queryToRows(ctx, t.engine, fmt.Sprintf("root_cause(%q, Msg, Source, Cause).", sessionFilter))
		failedReqs = queryToRows(ctx, t.engine, fmt.Sprintf("failed_request(%q, Id, Url, Status).", sessionFilter))
		slowApis = queryToRows(ctx, t.engine, fmt.Sprintf("slow_api(%q, Id, Url, Duration).", sessionFilter))
	} else {
		rootCauses = queryToRows(ctx, t.engine, "root_cause(SessionId, Msg, Source, Cause).")
		failedReqs = queryToRows(ctx, t.engine, "failed_request(SessionId, Id, Url, Status).")
		slowApis = queryToRows(ctx, t.engine, "slow_api(SessionId, Id, Url, Duration).")
	}

	status := "ok"
	if len(rootCauses) > 0 || len(failedReqs) > 0 {
		status = "error"
	} else if len(slowApis) > 0 {
		status = "warning"
	}

	counts := map[string]interface{}{
		"root_causes":     len(rootCauses),
		"failed_requests": len(failedReqs),
		"slow_apis":       len(slowApis),
	}
	summary := fmt.Sprintf(
		"status=%s root_causes=%d failed_requests=%d slow_apis=%d",
		status,
		len(rootCauses),
		len(failedReqs),
		len(slowApis),
	)

	switch view {
	case "summary":
		return map[string]interface{}{
			"status":  status,
			"counts":  counts,
			"summary": summary,
		}, nil
	case "compact":
		return map[string]interface{}{
			"status":          status,
			"counts":          counts,
			"summary":         summary,
			"root_causes":     limitMapSlice(rootCauses, maxItems),
			"failed_requests": limitMapSlice(failedReqs, maxItems),
			"slow_apis":       limitMapSlice(slowApis, maxItems),
			"truncated":       len(rootCauses) > maxItems || len(failedReqs) > maxItems || len(slowApis) > maxItems,
		}, nil
	default:
		return map[string]interface{}{
			"status":          status,
			"root_causes":     rootCauses,
			"failed_requests": failedReqs,
			"slow_apis":       slowApis,
			"counts":          counts,
			"summary":         summary,
		}, nil
	}
}
