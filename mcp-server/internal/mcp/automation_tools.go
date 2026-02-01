package mcp

import (
	"context"
	"fmt"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod/lib/input"
)

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// =============================================================================
// EXECUTE PLAN - Mangle-driven automation
// =============================================================================

// ExecutePlanTool executes a sequence of actions derived from Mangle rules.
// Claude defines intent via Mangle rules; this tool executes the resulting plan.
type ExecutePlanTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *ExecutePlanTool) Name() string { return "execute-plan" }
func (t *ExecutePlanTool) Description() string {
	return `Execute a Mangle-derived action plan. Define what you want with Mangle rules, then execute.

WORKFLOW:
1. Submit a Mangle rule that derives 'action(...)' facts
2. Call this tool to execute all derived actions in sequence

SUPPORTED ACTIONS (derived via Mangle rules):
- action("click", Ref)
- action("type", Ref, Value)
- action("navigate", URL)
- action("press", Key)
- action("wait", Milliseconds)
- action("screenshot", Name)

EXAMPLE:
Submit rule: action("type", "email", "user@example.com"). action("type", "password", "secret"). action("click", "login-btn").
Then call execute-plan to run all actions.

This is MASSIVELY more token-efficient than individual tool calls!`
}
func (t *ExecutePlanTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session",
			},
			"actions": map[string]interface{}{
				"type":        "array",
				"description": "Optional: Direct action list instead of Mangle-derived. Each action: {type: 'click'|'type'|'navigate'|'press'|'wait', ref: '...', value: '...'}",
				"items": map[string]interface{}{
					"type": "object",
				},
			},
			"predicate": map[string]interface{}{
				"type":        "string",
				"description": "Mangle predicate to query for action plan (alternative to 'actions' array)",
			},
			"stop_on_error": map[string]interface{}{
				"type":        "boolean",
				"description": "Stop execution on first error (default: true)",
			},
			"delay_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Delay between actions in milliseconds (default: 100)",
			},
		},
		"required": []string{"session_id"},
	}
}
func (t *ExecutePlanTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	stopOnError := getBoolArg(args, "stop_on_error", true)
	delayMs := getIntArg(args, "delay_ms", 100)

	if sessionID == "" {
		return map[string]interface{}{"success": false, "error": "session_id is required"}, nil
	}

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return map[string]interface{}{"success": false, "error": fmt.Sprintf("session not found: %s", sessionID)}, nil
	}

	// Get actions either from args or from Mangle-derived facts
	var actions []map[string]interface{}

	if actionsArg, ok := args["actions"]; ok {
		// Direct actions provided
		if actionsList, ok := actionsArg.([]interface{}); ok {
			for _, a := range actionsList {
				if actionMap, ok := a.(map[string]interface{}); ok {
					actions = append(actions, actionMap)
				}
			}
		}
	} else {
		// Query Mangle for derived action facts
		actionFacts := t.engine.FactsByPredicate("action")
		for _, f := range actionFacts {
			if len(f.Args) >= 1 {
				actionType := fmt.Sprintf("%v", f.Args[0])
				action := map[string]interface{}{"type": actionType}
				if len(f.Args) >= 2 {
					action["ref"] = fmt.Sprintf("%v", f.Args[1])
				}
				if len(f.Args) >= 3 {
					action["value"] = fmt.Sprintf("%v", f.Args[2])
				}
				actions = append(actions, action)
			}
		}
	}

	if len(actions) == 0 {
		return map[string]interface{}{
			"success":       true,
			"message":       "no actions to execute",
			"actions_count": 0,
		}, nil
	}

	// Execute each action
	results := make([]map[string]interface{}, 0)
	successCount := 0
	errorCount := 0

	for i, action := range actions {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		actionType := getStringFromMap(action, "type")
		ref := getStringFromMap(action, "ref")
		value := getStringFromMap(action, "value")

		result := map[string]interface{}{
			"index": i,
			"type":  actionType,
		}

		var actionErr error

		switch actionType {
		case "click":
			element, err := findElementByRef(page, ref)
			if err != nil {
				actionErr = fmt.Errorf("element not found: %s", ref)
			} else {
				actionErr = element.Click("left", 1)
			}
			result["ref"] = ref

		case "type":
			element, err := findElementByRef(page, ref)
			if err != nil {
				actionErr = fmt.Errorf("element not found: %s", ref)
			} else {
				if err := element.SelectAllText(); err == nil {
					_ = element.Input("")
				}
				actionErr = element.Input(value)
			}
			result["ref"] = ref
			result["value"] = value

		case "navigate":
			url := ref
			if url == "" {
				url = value
			}
			actionErr = page.Navigate(url)
			if actionErr == nil {
				actionErr = page.WaitLoad()
			}
			result["url"] = url

		case "press":
			key := ref
			if key == "" {
				key = value
			}
			keyMap := map[string]input.Key{
				"Enter": input.Enter, "Tab": input.Tab, "Escape": input.Escape,
			}
			if k, ok := keyMap[key]; ok {
				actionErr = page.Keyboard.Press(k)
			} else if len(key) == 1 {
				actionErr = page.Keyboard.Press(input.Key(rune(key[0])))
			} else {
				actionErr = fmt.Errorf("unknown key: %s", key)
			}
			result["key"] = key

		case "wait":
			ms := 1000
			if ref != "" {
				fmt.Sscanf(ref, "%d", &ms)
			} else if value != "" {
				fmt.Sscanf(value, "%d", &ms)
			}
			if err := sleepWithContext(ctx, time.Duration(ms)*time.Millisecond); err != nil {
				return nil, err
			}
			result["ms"] = ms

		case "screenshot":
			name := ref
			if name == "" {
				name = fmt.Sprintf("plan-screenshot-%d", i)
			}
			imgData, err := page.Screenshot(false, nil)
			if err != nil {
				actionErr = err
			} else {
				result["name"] = name
				result["size_bytes"] = len(imgData)
			}

		default:
			actionErr = fmt.Errorf("unknown action type: %s", actionType)
		}

		if actionErr != nil {
			result["success"] = false
			result["error"] = actionErr.Error()
			errorCount++

			if stopOnError {
				results = append(results, result)
				break
			}
		} else {
			result["success"] = true
			successCount++
		}

		results = append(results, result)

		// Delay between actions
		if delayMs > 0 && i < len(actions)-1 {
			if err := sleepWithContext(ctx, time.Duration(delayMs)*time.Millisecond); err != nil {
				return nil, err
			}
		}
	}

	// Emit summary fact
	now := time.Now()
	_ = t.engine.AddFacts(ctx, []mangle.Fact{{
		Predicate: "plan_executed",
		Args:      []interface{}{sessionID, len(actions), successCount, errorCount, now.UnixMilli()},
		Timestamp: now,
	}})

	return map[string]interface{}{
		"success":       errorCount == 0,
		"total_actions": len(actions),
		"succeeded":     successCount,
		"failed":        errorCount,
		"results":       results,
	}, nil
}

// =============================================================================
// WAIT FOR CONDITION - Mangle-powered waiting
// =============================================================================

// WaitForConditionTool waits until a Mangle condition is satisfied.
type WaitForConditionTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *WaitForConditionTool) Name() string { return "wait-for-condition" }
func (t *WaitForConditionTool) Description() string {
	return `Wait until a Mangle-defined condition is true, or until timeout.

EXAMPLES:
- Wait for navigation: predicate="navigation_event", match_args=["session-id", "/dashboard"]
- Wait for element: predicate="interactive", match_args=["submit-btn"]
- Wait for error: predicate="console_event", match_args=["error"]
- Wait for custom rule: Submit rule like 'ready() :- navigation_event(_, "/dashboard", _), dom_text(_, "Welcome").' then wait for predicate="ready"

This is token-efficient because you define the condition once, and the tool polls for you.`
}
func (t *WaitForConditionTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"predicate": map[string]interface{}{
				"type":        "string",
				"description": "Mangle predicate to wait for",
			},
			"match_args": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Optional: Arguments that must match (use '_' for wildcard)",
			},
			"timeout_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum wait time in milliseconds (default: 10000)",
			},
			"poll_interval_ms": map[string]interface{}{
				"type":        "integer",
				"description": "How often to check condition (default: 200)",
			},
		},
		"required": []string{"predicate"},
	}
}
func (t *WaitForConditionTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	predicate := getStringArg(args, "predicate")
	timeoutMs := getIntArg(args, "timeout_ms", 10000)
	pollIntervalMs := getIntArg(args, "poll_interval_ms", 200)

	if predicate == "" {
		return map[string]interface{}{"success": false, "error": "predicate is required"}, nil
	}

	// Parse match_args
	var matchArgs []string
	if matchArgsRaw, ok := args["match_args"]; ok {
		if argsList, ok := matchArgsRaw.([]interface{}); ok {
			for _, a := range argsList {
				matchArgs = append(matchArgs, fmt.Sprintf("%v", a))
			}
		}
	}

	startTime := time.Now()
	timeout := time.Duration(timeoutMs) * time.Millisecond
	pollInterval := time.Duration(pollIntervalMs) * time.Millisecond

	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}

	timeoutTimer := time.NewTimer(timeout)
	defer timeoutTimer.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		// Check if condition is satisfied
		facts := t.engine.FactsByPredicate(predicate)

		for _, f := range facts {
			if len(matchArgs) == 0 {
				// No specific args required, any fact satisfies
				return map[string]interface{}{
					"success":   true,
					"matched":   true,
					"fact":      f,
					"waited_ms": time.Since(startTime).Milliseconds(),
				}, nil
			}

			// Check if fact args match
			matches := true
			for i, expected := range matchArgs {
				if expected == "_" {
					continue // Wildcard
				}
				if i >= len(f.Args) || fmt.Sprintf("%v", f.Args[i]) != expected {
					matches = false
					break
				}
			}

			if matches {
				return map[string]interface{}{
					"success":   true,
					"matched":   true,
					"fact":      f,
					"waited_ms": time.Since(startTime).Milliseconds(),
				}, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeoutTimer.C:
			return map[string]interface{}{
				"success":   false,
				"matched":   false,
				"error":     "timeout waiting for condition",
				"waited_ms": time.Since(startTime).Milliseconds(),
			}, nil
		case <-ticker.C:
		}
	}
}

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
	return `Get all console errors with root cause analysis.

Returns:
- All console errors captured during the session
- For each error, any failed API requests that caused it (via Mangle causal reasoning)
- Failed requests that haven't been linked to console errors yet
- Slow API calls (>1 second) that might indicate performance issues

This uses Mangle's causal reasoning rules to automatically link:
- Console errors to failed API requests (status >= 400)
- API failures to their initiator requests (cascading failures)

EXAMPLE OUTPUT:
{
  "errors": [
    {
      "message": "Uncaught TypeError: Cannot read property 'map' of undefined",
      "timestamp": 1732481234567,
      "caused_by": {
        "request_id": "req-123",
        "url": "/api/users",
        "status": 500
      }
    }
  ],
  "failed_requests": [...],
  "slow_apis": [...],
  "docker_errors": [...],
  "backend_correlations": [...]
}

When Docker integration is enabled (docker.enabled=true in config):
- Queries backend container logs for errors around the same time window
- Correlates frontend API failures to backend exceptions
- Provides full-stack error tracing: Browser -> API -> Backend
- Uses Mangle rules for temporal correlation (within 3 second window)`
}
func (t *GetConsoleErrorsTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
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
	includeWarnings := getBoolArg(args, "include_warnings", false)
	includeAllLevels := getBoolArg(args, "include_all_levels", false)
	debugMode := getBoolArg(args, "debug", false)
	sinceMs := int64(getIntArg(args, "since_ms", 0))

	// Collect console errors (and optionally warnings)
	consoleEvents := t.engine.FactsByPredicate("console_event")
	errors := make([]map[string]interface{}, 0)

	for _, ev := range consoleEvents {
		if len(ev.Args) < 3 {
			continue
		}
		level := fmt.Sprintf("%v", ev.Args[0])
		message := fmt.Sprintf("%v", ev.Args[1])
		timestamp := int64(0)
		if ts, ok := ev.Args[2].(int64); ok {
			timestamp = ts
		} else if ts, ok := ev.Args[2].(float64); ok {
			timestamp = int64(ts)
		}

		// Filter by level (unless includeAllLevels is set)
		if !includeAllLevels {
			if level != "error" && !(includeWarnings && level == "warning") {
				continue
			}
		}

		// Filter by timestamp
		if sinceMs > 0 && timestamp < sinceMs {
			continue
		}

		errorEntry := map[string]interface{}{
			"level":     level,
			"message":   message,
			"timestamp": timestamp,
		}

		// Look for causal relationship using error_chain predicate
		// error_chain(ConsoleErr, ReqId, Url, Status) links errors to their causes
		errorChains := t.engine.FactsByPredicate("error_chain")
		for _, chain := range errorChains {
			if len(chain.Args) >= 4 {
				chainMsg := fmt.Sprintf("%v", chain.Args[0])
				if chainMsg == message {
					errorEntry["caused_by"] = map[string]interface{}{
						"request_id": fmt.Sprintf("%v", chain.Args[1]),
						"url":        fmt.Sprintf("%v", chain.Args[2]),
						"status":     chain.Args[3],
					}
					break
				}
			}
		}

		// If no error_chain, try caused_by directly
		if errorEntry["caused_by"] == nil {
			causedByFacts := t.engine.FactsByPredicate("caused_by")
			for _, cb := range causedByFacts {
				if len(cb.Args) >= 2 {
					cbMsg := fmt.Sprintf("%v", cb.Args[0])
					if cbMsg == message {
						reqId := fmt.Sprintf("%v", cb.Args[1])
						errorEntry["caused_by_request_id"] = reqId

						// Try to find the request details
						netRequests := t.engine.FactsByPredicate("net_request")
						for _, req := range netRequests {
							if len(req.Args) >= 3 && fmt.Sprintf("%v", req.Args[0]) == reqId {
								errorEntry["caused_by"] = map[string]interface{}{
									"request_id": reqId,
									"method":     fmt.Sprintf("%v", req.Args[1]),
									"url":        fmt.Sprintf("%v", req.Args[2]),
								}
								// Add status from net_response
								netResponses := t.engine.FactsByPredicate("net_response")
								for _, resp := range netResponses {
									if len(resp.Args) >= 2 && fmt.Sprintf("%v", resp.Args[0]) == reqId {
										if causedBy, ok := errorEntry["caused_by"].(map[string]interface{}); ok {
											causedBy["status"] = resp.Args[1]
										}
										break
									}
								}
								break
							}
						}
						break
					}
				}
			}
		}

		errors = append(errors, errorEntry)
	}

	// Collect failed requests (status >= 400)
	failedRequests := make([]map[string]interface{}, 0)
	failedFacts := t.engine.FactsByPredicate("failed_request")
	for _, f := range failedFacts {
		if len(f.Args) >= 3 {
			timestamp := int64(0)
			// Get timestamp from net_request
			reqId := fmt.Sprintf("%v", f.Args[0])
			netRequests := t.engine.FactsByPredicate("net_request")
			for _, req := range netRequests {
				if len(req.Args) >= 5 && fmt.Sprintf("%v", req.Args[0]) == reqId {
					if ts, ok := req.Args[4].(int64); ok {
						timestamp = ts
					} else if ts, ok := req.Args[4].(float64); ok {
						timestamp = int64(ts)
					}
					break
				}
			}

			if sinceMs > 0 && timestamp < sinceMs {
				continue
			}

			failedRequests = append(failedRequests, map[string]interface{}{
				"request_id": f.Args[0],
				"url":        fmt.Sprintf("%v", f.Args[1]),
				"status":     f.Args[2],
				"timestamp":  timestamp,
			})
		}
	}

	// Collect slow API calls (>1 second)
	slowApis := make([]map[string]interface{}, 0)
	slowFacts := t.engine.FactsByPredicate("slow_api")
	for _, s := range slowFacts {
		if len(s.Args) >= 3 {
			slowApis = append(slowApis, map[string]interface{}{
				"request_id": s.Args[0],
				"url":        fmt.Sprintf("%v", s.Args[1]),
				"duration":   s.Args[2],
			})
		}
	}

	// Collect cascading failures
	cascadingFailures := make([]map[string]interface{}, 0)
	cascadeFacts := t.engine.FactsByPredicate("cascading_failure")
	for _, c := range cascadeFacts {
		if len(c.Args) >= 2 {
			cascadingFailures = append(cascadingFailures, map[string]interface{}{
				"child_request_id":  c.Args[0],
				"parent_request_id": c.Args[1],
			})
		}
	}

	// ==========================================================================
	// Docker Log Integration (Full-Stack Error Correlation)
	// ==========================================================================
	// Only query Docker logs if there are actual errors or failed requests to correlate
	var dockerErrors []map[string]interface{}
	var backendCorrelations []map[string]interface{}
	var containerHealth map[string]interface{}
	var dockerQueryError string

	hasErrorsToCorrelate := len(errors) > 0 || len(failedRequests) > 0 || len(slowApis) > 0

	if t.dockerClient != nil && hasErrorsToCorrelate {
		// Determine time window - look back from earliest error
		var earliest time.Time
		if len(errors) > 0 {
			if ts, ok := errors[0]["timestamp"].(int64); ok && ts > 0 {
				earliest = time.UnixMilli(ts).Add(-5 * time.Second)
			}
		}
		if earliest.IsZero() && len(failedRequests) > 0 {
			if ts, ok := failedRequests[0]["timestamp"].(int64); ok && ts > 0 {
				earliest = time.UnixMilli(ts).Add(-5 * time.Second)
			}
		}
		if earliest.IsZero() {
			earliest = time.Now().Add(-30 * time.Second)
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
				dockerErrors = append(dockerErrors, map[string]interface{}{
					"container": log.Container,
					"level":     log.Level,
					"tag":       log.Tag,
					"message":   log.Message,
					"timestamp": log.Timestamp.UnixMilli(),
				})
			}

			// Push all docker_log facts to Mangle for correlation
			if len(dockerFacts) > 0 {
				_ = t.engine.AddFacts(ctx, dockerFacts)
			}

			// Query Mangle for backend correlations
			// api_backend_correlation(ReqId, Container, BackendMsg, TimeDelta)
			correlationFacts := t.engine.FactsByPredicate("api_backend_correlation")
			for _, cf := range correlationFacts {
				if len(cf.Args) >= 4 {
					backendCorrelations = append(backendCorrelations, map[string]interface{}{
						"request_id":    fmt.Sprintf("%v", cf.Args[0]),
						"container":     fmt.Sprintf("%v", cf.Args[1]),
						"backend_error": fmt.Sprintf("%v", cf.Args[2]),
						"time_delta_ms": cf.Args[3],
					})
				}
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
		"error_count":          len(errors),
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
			if len(ev.Args) >= 1 {
				level := fmt.Sprintf("%v", ev.Args[0])
				levelCounts[level]++
			}
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
			"level": map[string]interface{}{
				"type":        "string",
				"description": "Filter by level: 'error', 'warning', 'success', 'info', or 'all' (default: 'all')",
				"enum":        []string{"error", "warning", "success", "info", "all"},
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
	levelFilter := getStringArg(args, "level")
	if levelFilter == "" {
		levelFilter = "all"
	}
	sinceMs := int64(getIntArg(args, "since_ms", 0))
	includeCorrelations := getBoolArg(args, "include_correlations", true)
	limit := getIntArg(args, "limit", 0)

	// Collect all toast notifications
	toastFacts := t.engine.FactsByPredicate("toast_notification")
	toasts := make([]map[string]interface{}, 0)

	errorCount := 0
	warningCount := 0
	successCount := 0
	infoCount := 0

	for _, f := range toastFacts {
		if len(f.Args) < 4 {
			continue
		}
		text := fmt.Sprintf("%v", f.Args[0])
		level := fmt.Sprintf("%v", f.Args[1])
		source := fmt.Sprintf("%v", f.Args[2])
		timestamp := int64(0)
		if ts, ok := f.Args[3].(int64); ok {
			timestamp = ts
		} else if ts, ok := f.Args[3].(float64); ok {
			timestamp = int64(ts)
		}

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
			correlationFacts := t.engine.FactsByPredicate("toast_after_api_failure")
			for _, cf := range correlationFacts {
				if len(cf.Args) >= 5 {
					cfText := fmt.Sprintf("%v", cf.Args[0])
					if cfText == text {
						toastEntry["correlated_api_failure"] = map[string]interface{}{
							"request_id":    fmt.Sprintf("%v", cf.Args[1]),
							"url":           fmt.Sprintf("%v", cf.Args[2]),
							"status":        cf.Args[3],
							"time_delta_ms": cf.Args[4],
						}
						break
					}
				}
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
	repeatedFacts := t.engine.FactsByPredicate("repeated_toast_error")
	for _, rf := range repeatedFacts {
		if len(rf.Args) >= 1 {
			repeatedErrors = append(repeatedErrors, fmt.Sprintf("%v", rf.Args[0]))
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

	return result, nil
}

// DiagnosePageTool runs Mangle queries to find root causes of errors.
type DiagnosePageTool struct {
	engine *mangle.Engine
}

func (t *DiagnosePageTool) Name() string { return "diagnose-page" }
func (t *DiagnosePageTool) Description() string {
	return `Analyze the current page state for errors using Mangle logic.

WHAT IT DOES:
- Queries Mangle for "root_cause", "slow_api", and "failed_request" facts.
- Returns a concise summary of what went wrong.
- TOKEN EFFICIENT: Replaces dumping 100 log lines with a single structured report.

WHEN TO USE:
- After a navigation fails or page looks broken.
- When you suspect API errors or JS crashes.
- To get a "health check" of the current session.

Returns: {status: "ok"|"error", issues: [...], root_causes: [...]}`
}
func (t *DiagnosePageTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (t *DiagnosePageTool) Execute(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
	// 1. Check for root causes (high confidence)
	rootCauses, err := t.engine.Query(ctx, "root_cause(Msg, Source, Cause).")
	if err != nil {
		return nil, err
	}

	// 2. Check for failed requests
	failedReqs, err := t.engine.Query(ctx, "failed_request(Id, Url, Status).")
	if err != nil {
		return nil, err
	}

	// 3. Check for slow APIs
	slowApis, err := t.engine.Query(ctx, "slow_api(Id, Url, Duration).")
	if err != nil {
		return nil, err
	}

	status := "ok"
	if len(rootCauses) > 0 || len(failedReqs) > 0 {
		status = "error"
	} else if len(slowApis) > 0 {
		status = "warning"
	}

	return map[string]interface{}{
		"status":          status,
		"root_causes":     rootCauses,
		"failed_requests": failedReqs,
		"slow_apis":       slowApis,
	}, nil
}

// AwaitStableStateTool blocks until the page is considered "stable" (network idle + DOM settled).
type AwaitStableStateTool struct {
	engine *mangle.Engine
}

func (t *AwaitStableStateTool) Name() string { return "await-stable-state" }
func (t *AwaitStableStateTool) Description() string {
	return `Block until the page is stable (network idle and DOM settled).

WHAT IT DOES:
- Waits for "network idle" (no requests in last 500ms).
- Waits for "DOM idle" (no updates in last 200ms).
- Returns when both conditions are met OR timeout.

WHEN TO USE:
- After navigation or complex interactions.
- BEFORE taking a screenshot or extracting data.
- To avoid "flaky" tests caused by loading spinners.

Returns: {status: "stable"|"timeout", duration_ms}`
}
func (t *AwaitStableStateTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"timeout_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Max wait time (default 10000ms)",
			},
			"network_idle_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Required network idle window (default 500ms)",
			},
		},
	}
}
func (t *AwaitStableStateTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	timeout := time.Duration(getIntArg(args, "timeout_ms", 10000)) * time.Millisecond
	netIdle := time.Duration(getIntArg(args, "network_idle_ms", 500)) * time.Millisecond
	domIdle := 200 * time.Millisecond

	start := time.Now()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(timeout):
			return map[string]interface{}{
				"status":      "timeout",
				"duration_ms": time.Since(start).Milliseconds(),
			}, nil
		case <-ticker.C:
			// Check network idle
			// We query for ANY request in the last X ms. If count == 0, we are idle.
			netWindow := time.Now().Add(-netIdle)
			recentReqs := t.engine.QueryTemporal("net_request", netWindow, time.Now())

			// Check DOM idle
			domWindow := time.Now().Add(-domIdle)
			recentDom := t.engine.QueryTemporal("dom_updated", domWindow, time.Now())

			if len(recentReqs) == 0 && len(recentDom) == 0 {
				return map[string]interface{}{
					"status":      "stable",
					"duration_ms": time.Since(start).Milliseconds(),
				}, nil
			}
		}
	}
}
