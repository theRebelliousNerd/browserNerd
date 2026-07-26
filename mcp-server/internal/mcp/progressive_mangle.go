package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/recorder"
	"browsernerd-mcp-server/internal/security"
)

// BrowserMangleTool consolidates all Mangle fact operations into one progressive-disclosure tool.
type BrowserMangleTool struct {
	engine           *mangle.Engine
	dockerClient     *docker.Client
	recorder         *recorder.Recorder
	redactor         *security.Redactor
	pathPolicy       *security.PathPolicy
	defaultTraceDir  string
	defaultLogWindow time.Duration
}

func (t *BrowserMangleTool) Name() string { return "browser-mangle" }
func (t *BrowserMangleTool) Description() string {
	return `Query and manipulate the Mangle fact engine directly.

Use for raw fact access. Prefer browser-reason for high-level diagnostics.
Mangle stores browser facts (network, console, DOM, navigation) and derives new facts via rules.

Operations:
  query:            Execute Mangle query. E.g. query:"current_url(S, U)."
  read:             Read recent buffered facts (newest first, optional predicate_filter)
  temporal:         Query facts in a time window (after_ms / before_ms, epoch ms)
  evaluate:         Check if a derived predicate currently has matches
  push:             Add facts to the engine
  submit_rule:      Add a derivation rule (Datalog syntax)
  subscribe:        Watch for a predicate match with timeout
  await_fact:       Wait for a specific fact (predicate + args)
  await_conditions: Wait for multiple facts simultaneously (AND logic)
  export_flight:    Export raw JSONL evidence bundle (facts + optional Docker logs)

Common built-in predicates: current_url, net_request, net_response, console_event,
navigation_event, slow_api (derived), caused_by (derived), login_succeeded (derived).

Views: summary (~100 tokens), compact (default ~500), full (all rows).`
}

func (t *BrowserMangleTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"operation": map[string]interface{}{
				"type":        "string",
				"description": "Mangle operation to perform",
				"enum":        []string{"query", "temporal", "evaluate", "read", "submit_rule", "subscribe", "push", "await_fact", "await_conditions", "export_flight"},
			},
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session (recommended for export_flight and scoped reads)",
			},
			"view": map[string]interface{}{
				"type":        "string",
				"description": "Disclosure depth: summary|compact|full",
				"enum":        []string{"summary", "compact", "full"},
			},
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Mangle query string (for query operation)",
			},
			"predicate": map[string]interface{}{
				"type":        "string",
				"description": "Predicate name (for temporal/evaluate/subscribe/await_fact)",
			},
			"predicate_filter": map[string]interface{}{
				"type":        "string",
				"description": "Filter by predicate (for read operation)",
			},
			"rule": map[string]interface{}{
				"type":        "string",
				"description": "Mangle rule source (for submit_rule)",
			},
			"facts": map[string]interface{}{
				"type":        "array",
				"description": "Facts to push (for push operation)",
				"items":       map[string]interface{}{"type": "object"},
			},
			"args": map[string]interface{}{
				"type":        "array",
				"description": "Predicate arguments (for await_fact)",
				"items":       map[string]interface{}{"type": "string"},
			},
			"conditions": map[string]interface{}{
				"type":        "array",
				"description": "Conditions to await (for await_conditions)",
				"items":       map[string]interface{}{"type": "object"},
			},
			"after_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Start of time window in epoch ms (for temporal)",
			},
			"before_ms": map[string]interface{}{
				"type":        "integer",
				"description": "End of time window in epoch ms (for temporal)",
			},
			"timeout_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Timeout in milliseconds (for subscribe/await_fact/await_conditions)",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Max facts to return (for read)",
			},
			"max_items": map[string]interface{}{
				"type":        "integer",
				"description": "Max items in response (default 20)",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Output path for export_flight (defaults to recorder trace_dir/flight-<ts>.jsonl)",
			},
			"include_server_logs": map[string]interface{}{
				"type":        "boolean",
				"description": "Include Docker server logs in export_flight (default false)",
			},
			"since_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Filter export_flight rows at/after this epoch ms (default now-log_window or now-5m)",
			},
			"until_ms": map[string]interface{}{
				"type":        "integer",
				"description": "Filter export_flight rows at/before this epoch ms (default now)",
			},
			"max_rows": map[string]interface{}{
				"type":        "integer",
				"description": "Max rows written by export_flight (default 2000, newest rows kept when truncated)",
			},
		},
		"required": []string{"operation"},
	}
}

func (t *BrowserMangleTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	operation := strings.ToLower(getStringArg(args, "operation"))
	if operation == "" {
		return map[string]interface{}{"success": false, "error": "operation is required"}, nil
	}
	if operation == "export_flight" {
		return t.handleExportFlight(ctx, args), nil
	}
	if t.engine == nil {
		return map[string]interface{}{"success": false, "error": "mangle engine is not available"}, nil
	}

	view := normalizeProgressiveView(getStringArg(args, "view"))
	maxItems := getIntArg(args, "max_items", defaultProgressiveMaxItems)
	if maxItems <= 0 {
		maxItems = defaultProgressiveMaxItems
	}

	var (
		opResult interface{}
		err      error
	)

	switch operation {
	case "query":
		delegate := &QueryFactsTool{engine: t.engine}
		opResult, err = delegate.Execute(ctx, map[string]interface{}{
			"query": getStringArg(args, "query"),
		})

	case "temporal":
		delegate := &QueryTemporalTool{engine: t.engine}
		delegateArgs := map[string]interface{}{
			"predicate": getStringArg(args, "predicate"),
		}
		if v, ok := args["after_ms"]; ok {
			delegateArgs["after_ms"] = v
		}
		if v, ok := args["before_ms"]; ok {
			delegateArgs["before_ms"] = v
		}
		opResult, err = delegate.Execute(ctx, delegateArgs)

	case "evaluate":
		delegate := &EvaluateRuleTool{engine: t.engine}
		opResult, err = delegate.Execute(ctx, map[string]interface{}{
			"predicate": getStringArg(args, "predicate"),
		})

	case "read":
		delegate := &ReadFactsTool{engine: t.engine}
		delegateArgs := map[string]interface{}{}
		if v, ok := args["limit"]; ok {
			delegateArgs["limit"] = v
		}
		if v, ok := args["predicate_filter"]; ok {
			delegateArgs["predicate_filter"] = v
		}
		opResult, err = delegate.Execute(ctx, delegateArgs)

	case "submit_rule":
		delegate := &SubmitRuleTool{engine: t.engine}
		opResult, err = delegate.Execute(ctx, map[string]interface{}{
			"rule": getStringArg(args, "rule"),
		})

	case "subscribe":
		delegate := &SubscribeRuleTool{engine: t.engine}
		delegateArgs := map[string]interface{}{
			"predicate": getStringArg(args, "predicate"),
		}
		if v, ok := args["timeout_ms"]; ok {
			delegateArgs["timeout_ms"] = v
		}
		opResult, err = delegate.Execute(ctx, delegateArgs)

	case "push":
		delegate := &PushFactsTool{engine: t.engine}
		opResult, err = delegate.Execute(ctx, map[string]interface{}{
			"facts": args["facts"],
		})

	case "await_fact":
		delegate := &AwaitFactTool{engine: t.engine}
		delegateArgs := map[string]interface{}{
			"predicate": getStringArg(args, "predicate"),
		}
		if v, ok := args["args"]; ok {
			delegateArgs["args"] = v
		}
		if v, ok := args["timeout_ms"]; ok {
			delegateArgs["timeout_ms"] = v
		}
		opResult, err = delegate.Execute(ctx, delegateArgs)

	case "await_conditions":
		delegate := &AwaitConditionsTool{engine: t.engine}
		delegateArgs := map[string]interface{}{}
		if v, ok := args["conditions"]; ok {
			delegateArgs["conditions"] = v
		}
		if v, ok := args["timeout_ms"]; ok {
			delegateArgs["timeout_ms"] = v
		}
		opResult, err = delegate.Execute(ctx, delegateArgs)

	default:
		return map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("unknown mangle operation: %s", operation),
		}, nil
	}

	if err != nil {
		return map[string]interface{}{
			"success":   false,
			"operation": operation,
			"error":     err.Error(),
		}, nil
	}

	resultMap := asMap(opResult)
	handle := fmt.Sprintf("mangle:%s:%d", operation, time.Now().UnixMilli())
	handles := []string{handle}
	emitDisclosureFacts(ctx, t.engine, "", handles, "mangle")

	response := map[string]interface{}{
		"success":          true,
		"operation":        operation,
		"view":             view,
		"evidence_handles": handles,
	}

	switch view {
	case "summary":
		response["summary"] = buildMangleSummary(operation, resultMap)
	case "compact":
		response["data"] = truncateMangleData(resultMap, maxItems)
		response["summary"] = buildMangleSummary(operation, resultMap)
	default: // full
		response["data"] = resultMap
	}

	return response, nil
}

func (t *BrowserMangleTool) handleExportFlight(ctx context.Context, args map[string]interface{}) map[string]interface{} {
	view := normalizeProgressiveView(getStringArg(args, "view"))
	sessionID := strings.TrimSpace(getStringArg(args, "session_id"))
	includeServerLogs := getBoolArg(args, "include_server_logs", false)

	untilMs := asInt64(args["until_ms"])
	if untilMs <= 0 {
		untilMs = time.Now().UnixMilli()
	}

	sinceMs := asInt64(args["since_ms"])
	if sinceMs <= 0 {
		window := t.defaultLogWindow
		if window <= 0 {
			window = 5 * time.Minute
		}
		sinceMs = untilMs - int64(window/time.Millisecond)
	}
	if sinceMs > untilMs {
		sinceMs = untilMs
	}

	maxRows := getIntArg(args, "max_rows", 2000)
	if maxRows <= 0 {
		maxRows = 2000
	}
	if maxRows > 100000 {
		maxRows = 100000
	}

	outPath, err := resolveFlightExportPath(t.pathPolicy, getStringArg(args, "path"), t.defaultTraceDir, sessionID, untilMs)
	if err != nil {
		return map[string]interface{}{
			"success":   false,
			"operation": "export_flight",
			"error":     err.Error(),
		}
	}

	rows := make([]map[string]interface{}, 0, maxRows)
	factRows := collectFlightFactRows(t.engine, sessionID, sinceMs, untilMs)
	rows = append(rows, factRows...)

	logRows := []map[string]interface{}{}
	warnings := make([]string, 0, 1)
	if includeServerLogs {
		if t.dockerClient == nil {
			warnings = append(warnings, "docker log export requested but docker integration is disabled")
		} else {
			logs, logErr := t.dockerClient.QueryLogs(ctx, time.UnixMilli(sinceMs))
			if logErr != nil {
				warnings = append(warnings, "docker log export failed: "+logErr.Error())
			} else {
				logRows = collectFlightDockerRows(logs, sinceMs, untilMs)
				rows = append(rows, logRows...)
			}
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		ti := asInt64(rows[i]["ts"])
		tj := asInt64(rows[j]["ts"])
		if ti != tj {
			return ti < tj
		}
		si := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", rows[i]["source"])))
		sj := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", rows[j]["source"])))
		return si < sj
	})

	truncated := false
	if len(rows) > maxRows {
		rows = rows[len(rows)-maxRows:]
		truncated = true
	}
	rows = sanitizeFlightRows(rows, t.redactor)

	if err := writeJSONLFile(outPath, rows); err != nil {
		return map[string]interface{}{
			"success":   false,
			"operation": "export_flight",
			"error":     fmt.Sprintf("write export file: %v", err),
		}
	}

	if t.recorder != nil {
		t.recorder.Log("export_flight", sessionID, map[string]interface{}{
			"path":                outPath,
			"rows_written":        len(rows),
			"fact_rows":           len(factRows),
			"docker_rows":         len(logRows),
			"include_server_logs": includeServerLogs,
			"since_ms":            sinceMs,
			"until_ms":            untilMs,
			"truncated":           truncated,
		})
	}

	summary := fmt.Sprintf("exported %d row(s) to %s", len(rows), outPath)
	if truncated {
		summary += " (truncated to newest rows)"
	}
	if sessionID != "" {
		summary += fmt.Sprintf(" [session=%s]", sessionID)
	}

	exportData := map[string]interface{}{
		"path":                outPath,
		"rows_written":        len(rows),
		"fact_rows":           len(factRows),
		"docker_rows":         len(logRows),
		"session_id":          sessionID,
		"include_server_logs": includeServerLogs,
		"since_ms":            sinceMs,
		"until_ms":            untilMs,
		"truncated":           truncated,
	}

	response := map[string]interface{}{
		"success":   true,
		"operation": "export_flight",
		"view":      view,
		"summary":   summary,
		"export":    exportData,
	}
	if len(warnings) > 0 {
		response["warnings"] = warnings
	}

	switch view {
	case "summary":
		// Keep summary token-light: no sample rows.
	case "compact":
		response["sample_rows"] = limitAnySlice(mapSliceToAny(rows), minInt(5, len(rows)))
	default:
		response["rows"] = rows
	}

	return response
}

func resolveFlightExportPath(policy *security.PathPolicy, rawPath, defaultTraceDir, sessionID string, untilMs int64) (string, error) {
	baseDir := strings.TrimSpace(defaultTraceDir)
	if baseDir == "" {
		baseDir = recorder.TraceDir
	}

	if policy == nil {
		return "", errors.New("export write path policy is not configured")
	}
	filename := fmt.Sprintf("flight_%s_%d.jsonl", safeTraceFragment(sessionID, "global"), untilMs)
	path, err := policy.ResolveForWrite(strings.TrimSpace(rawPath), baseDir, filename)
	if err != nil {
		return "", err
	}
	if err := security.EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return "", fmt.Errorf("create export directory: %w", err)
	}
	return path, nil
}

func collectFlightFactRows(engine *mangle.Engine, sessionID string, sinceMs, untilMs int64) []map[string]interface{} {
	if engine == nil {
		return []map[string]interface{}{}
	}

	facts := engine.Facts()
	rows := make([]map[string]interface{}, 0, len(facts))
	for _, f := range facts {
		if sessionID != "" {
			if len(f.Args) == 0 || fmt.Sprintf("%v", f.Args[0]) != sessionID {
				continue
			}
		}

		ts := f.Timestamp.UnixMilli()
		if ts > 0 {
			if sinceMs > 0 && ts < sinceMs {
				continue
			}
			if untilMs > 0 && ts > untilMs {
				continue
			}
		}

		row := map[string]interface{}{
			"ts":        ts,
			"source":    "mangle_fact",
			"predicate": f.Predicate,
			"args":      f.Args,
		}
		if len(f.Args) > 0 {
			row["session_id"] = fmt.Sprintf("%v", f.Args[0])
		}
		rows = append(rows, row)
	}
	return rows
}

func collectFlightDockerRows(logs []docker.LogEntry, sinceMs, untilMs int64) []map[string]interface{} {
	rows := make([]map[string]interface{}, 0, len(logs))
	for _, entry := range logs {
		ts := entry.Timestamp.UnixMilli()
		if sinceMs > 0 && ts < sinceMs {
			continue
		}
		if untilMs > 0 && ts > untilMs {
			continue
		}
		rows = append(rows, map[string]interface{}{
			"ts":        ts,
			"source":    "docker_log",
			"container": entry.Container,
			"level":     entry.Level,
			"tag":       entry.Tag,
			"message":   entry.Message,
			"raw":       entry.Raw,
		})
	}
	return rows
}

func writeJSONLFile(path string, rows []map[string]interface{}) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return err
	}

	encoder := json.NewEncoder(file)
	for _, row := range rows {
		if err := encoder.Encode(row); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeFlightRows(rows []map[string]interface{}, redactor *security.Redactor) []map[string]interface{} {
	if redactor == nil {
		return rows
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		sanitized, ok := redactor.Sanitize(row).(map[string]interface{})
		if !ok {
			continue
		}
		out = append(out, sanitized)
	}
	return out
}

func safeTraceFragment(input, fallback string) string {
	raw := strings.TrimSpace(input)
	if raw == "" {
		raw = fallback
	}
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('_')
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return fallback
	}
	return out
}

func mapSliceToAny(rows []map[string]interface{}) []interface{} {
	out := make([]interface{}, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	return out
}

func buildMangleSummary(operation string, data map[string]interface{}) string {
	switch operation {
	case "query":
		if results, ok := data["results"].([]map[string]interface{}); ok {
			return fmt.Sprintf("query returned %d result(s)", len(results))
		}
		if results, ok := data["results"].([]interface{}); ok {
			return fmt.Sprintf("query returned %d result(s)", len(results))
		}
		return "query completed"
	case "read":
		if facts, ok := data["facts"].([]interface{}); ok {
			return fmt.Sprintf("read %d fact(s)", len(facts))
		}
		if count := asInt(data["count"]); count > 0 {
			return fmt.Sprintf("read %d fact(s)", count)
		}
		return "read completed"
	case "push":
		accepted := asInt(data["accepted"])
		return fmt.Sprintf("pushed %d fact(s)", accepted)
	case "submit_rule":
		if success, ok := data["success"].(bool); ok && success {
			return "rule submitted"
		}
		return "rule submission failed"
	case "evaluate":
		if results, ok := data["results"].([]interface{}); ok {
			return fmt.Sprintf("evaluated %d result(s)", len(results))
		}
		return "evaluation completed"
	case "temporal":
		if results, ok := data["results"].([]interface{}); ok {
			return fmt.Sprintf("temporal query returned %d result(s)", len(results))
		}
		return "temporal query completed"
	case "subscribe":
		if matched, ok := data["matched"].(bool); ok && matched {
			return "subscription matched"
		}
		return "subscription completed"
	case "await_fact":
		if matched, ok := data["matched"].(bool); ok && matched {
			return "fact matched"
		}
		return "await completed"
	case "await_conditions":
		if matched, ok := data["all_matched"].(bool); ok && matched {
			return "all conditions matched"
		}
		return "await conditions completed"
	default:
		return operation + " completed"
	}
}

func truncateMangleData(data map[string]interface{}, maxItems int) map[string]interface{} {
	out := make(map[string]interface{}, len(data))
	for k, v := range data {
		switch items := v.(type) {
		case []interface{}:
			out[k] = limitAnySlice(items, maxItems)
			if len(items) > maxItems {
				out[k+"_truncated"] = true
			}
		case []map[string]interface{}:
			out[k] = limitMapSlice(items, maxItems)
			if len(items) > maxItems {
				out[k+"_truncated"] = true
			}
		default:
			out[k] = v
		}
	}
	return out
}
