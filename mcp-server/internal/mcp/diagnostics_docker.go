package mcp

import (
	"context"
	"fmt"

	"browsernerd-mcp-server/internal/correlation"
	"browsernerd-mcp-server/internal/logs"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"
)

// Docker predicates retain container semantics. Native entries are reported
// through the shared diagnosis without being relabeled as container evidence.
func (t *GetConsoleErrorsTool) addDockerDiagnostics(ctx context.Context, result map[string]interface{}, entries []logs.Entry, sessionID string) {
	redactor := t.redactor
	if redactor == nil {
		redactor = security.NewRedactor(nil)
	}
	var containerEntries []logs.Entry
	var facts []mangle.Fact
	errors := make([]map[string]interface{}, 0)
	for _, entry := range entries {
		if !t.dockerClient.OwnsSource(entry.Source) || entry.Timestamp.IsZero() {
			continue
		}
		entry.Source = redactor.SanitizeString(entry.Source)
		entry.Tag = redactor.SanitizeString(entry.Tag)
		entry.Message = redactor.SanitizeString(entry.Message)
		containerEntries = append(containerEntries, entry)
		if entry.Level != "ERROR" && entry.Level != "WARNING" && entry.Level != "CRITICAL" {
			continue
		}
		stamp := entry.Timestamp.UnixMilli()
		facts = append(facts, mangle.Fact{Predicate: "docker_log", Args: []interface{}{entry.Source, entry.Level, entry.Tag, entry.Message, stamp}, Timestamp: entry.Timestamp})
		row := map[string]interface{}{"container": entry.Source, "level": entry.Level, "tag": entry.Tag, "message": entry.Message, "timestamp": stamp}
		keys := make([]map[string]string, 0)
		for _, key := range correlation.FromMessage(entry.Message) {
			facts = append(facts, mangle.Fact{Predicate: "docker_log_correlation", Args: []interface{}{entry.Source, key.Type, key.Value, entry.Message, stamp}, Timestamp: entry.Timestamp})
			keys = append(keys, map[string]string{"key_type": key.Type, "key_value": key.Value})
		}
		if len(keys) > 0 {
			row["correlation_keys"] = keys
		}
		errors = append(errors, row)
	}
	result["docker_errors"] = errors
	result["docker_error_count"] = len(errors)
	result["container_health"] = t.dockerClient.AnalyzeHealth(containerEntries)
	if len(facts) == 0 {
		return
	}
	if err := t.engine.AddFacts(ctx, facts); err != nil {
		result["docker_fact_error"] = redactor.SanitizeString(err.Error())
		return
	}
	keyed, err := t.engine.Evaluate(ctx, "api_backend_correlation")
	if err != nil {
		result["docker_correlation_error"] = redactor.SanitizeString(err.Error())
		return
	}
	correlated, _ := result["backend_correlations"].([]map[string]interface{})
	for _, fact := range keyed {
		if len(fact.Args) < 6 || (sessionID != "" && fmt.Sprint(fact.Args[0]) != sessionID) {
			continue
		}
		correlated = append(correlated, map[string]interface{}{"session_id": fact.Args[0], "request_id": fact.Args[1], "url": fact.Args[2], "status": fact.Args[3], "backend_error": fact.Args[4], "time_delta_ms": fact.Args[5], "mode": "keyed"})
	}
	result["backend_correlations"] = correlated
}
