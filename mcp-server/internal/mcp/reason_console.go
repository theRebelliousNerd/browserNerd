package mcp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"browsernerd-mcp-server/internal/security"
)

var (
	consoleUUIDPattern   = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	consoleHexIDPattern  = regexp.MustCompile(`(?i)\b[0-9a-f]*[0-9][0-9a-f]*\b`)
	consoleNumberPattern = regexp.MustCompile(`[0-9]+`)
)

// consoleFingerprint is the grouping key for a console message: whitespace
// collapsed, ids and numbers folded, so "status 500" and "status 404", or the
// same error for two record ids, count as one message.
func consoleFingerprint(message string) string {
	s := strings.Join(strings.Fields(message), " ")
	s = consoleUUIDPattern.ReplaceAllString(s, "{id}")
	s = consoleHexIDPattern.ReplaceAllStringFunc(s, func(tok string) string {
		if len(tok) >= 8 {
			return "{id}"
		}
		return tok
	})
	s = consoleNumberPattern.ReplaceAllString(s, "#")
	return strings.ToLower(s)
}

func consoleLevelRank(level string) int {
	switch level {
	case "error":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

// consoleGroup is one console message with every near-identical repeat.
type consoleGroup struct {
	level   string
	message string
	count   int
	firstMs int64
	lastMs  int64
}

// groupConsole folds records into groups ordered errors first, then warnings,
// then by count and recency.
func groupConsole(records []consoleRecord) []*consoleGroup {
	byKey := make(map[string]*consoleGroup)
	out := make([]*consoleGroup, 0)
	for _, r := range records {
		key := r.level + "|" + consoleFingerprint(r.message)
		g, ok := byKey[key]
		if !ok {
			g = &consoleGroup{level: r.level, message: r.message, firstMs: r.ts, lastMs: r.ts}
			byKey[key] = g
			out = append(out, g)
		}
		g.count++
		if r.ts > 0 && (g.firstMs == 0 || r.ts < g.firstMs) {
			g.firstMs = r.ts
		}
		if r.ts > g.lastMs {
			g.lastMs = r.ts
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := consoleLevelRank(out[i].level), consoleLevelRank(out[j].level); ri != rj {
			return ri < rj
		}
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].lastMs > out[j].lastMs
	})
	return out
}

func (g *consoleGroup) row() map[string]interface{} {
	row := map[string]interface{}{
		"level":    g.level,
		"message":  clipText(g.message, 240),
		"count":    g.count,
		"first_ms": g.firstMs,
	}
	if g.lastMs != g.firstMs {
		row["last_ms"] = g.lastMs
	}
	return row
}

func (g *consoleGroup) line() string {
	line := fmt.Sprintf("console %s %q", g.level, clipText(g.message, 120))
	if g.count > 1 {
		line += fmt.Sprintf(" x%d", g.count)
	}
	return line
}

// groupConsoleEntries folds get-console-errors entries (session_id, level,
// message, timestamp, caused_by) by session, level and fingerprint. Each group
// keeps its first entry's fields plus count, first_ms and last_ms, with
// timestamp set to the latest; a group adopts the first caused_by any of its
// entries carries. Errors come before warnings, then by count and recency.
// dropSession omits session_id when the caller already filtered to one.
func groupConsoleEntries(entries []map[string]interface{}, dropSession bool) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(entries))
	byKey := make(map[string]map[string]interface{})
	for _, e := range entries {
		level := normalizeConsoleLevel(fmt.Sprintf("%v", e["level"]))
		key := fmt.Sprintf("%v", e["session_id"]) + "|" + level + "|" + consoleFingerprint(fmt.Sprintf("%v", e["message"]))
		ts := asInt64(e["timestamp"])
		g, ok := byKey[key]
		if !ok {
			g = make(map[string]interface{}, len(e)+3)
			for k, v := range e {
				g[k] = v
			}
			if dropSession {
				delete(g, "session_id")
			}
			g["count"] = 0
			g["first_ms"] = ts
			g["last_ms"] = ts
			byKey[key] = g
			out = append(out, g)
		}
		g["count"] = asInt(g["count"]) + 1
		if first := asInt64(g["first_ms"]); ts > 0 && (first == 0 || ts < first) {
			g["first_ms"] = ts
		}
		if ts > asInt64(g["last_ms"]) {
			g["last_ms"] = ts
		}
		g["timestamp"] = g["last_ms"]
		if g["caused_by"] == nil && e["caused_by"] != nil {
			g["caused_by"] = e["caused_by"]
			if id, ok := e["caused_by_request_id"]; ok {
				g["caused_by_request_id"] = id
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		li := consoleLevelRank(normalizeConsoleLevel(fmt.Sprintf("%v", out[i]["level"])))
		lj := consoleLevelRank(normalizeConsoleLevel(fmt.Sprintf("%v", out[j]["level"])))
		if li != lj {
			return li < lj
		}
		if ci, cj := asInt(out[i]["count"]), asInt(out[j]["count"]); ci != cj {
			return ci > cj
		}
		return asInt64(out[i]["last_ms"]) > asInt64(out[j]["last_ms"])
	})
	return out
}

var snippetSecretFieldPattern = regexp.MustCompile(`(?i)("(?:[a-z0-9_-]*(?:password|passwd|secret|token|api[_-]?key|authorization|cookie|session)[a-z0-9_-]*)"\s*:\s*)"[^"]*"`)

var snippetRedactor = security.NewRedactor(nil)

// redactSnippet removes credentials from a response body or log line before it
// is returned, collapses whitespace, and clips it to max bytes.
func redactSnippet(s string, max int) string {
	s = snippetSecretFieldPattern.ReplaceAllString(s, `${1}"`+security.Redacted+`"`)
	s = snippetRedactor.SanitizeString(s)
	return clipText(s, max)
}

// bodyDetail extracts the one human line from a failure body: the detail,
// message, or error field of a JSON body, a FastAPI validation message, or the
// first words of a text body.
func bodyDetail(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err == nil {
		for _, key := range []string{"detail", "message", "error_description", "error", "title", "msg", "reason"} {
			switch v := parsed[key].(type) {
			case string:
				if strings.TrimSpace(v) != "" {
					return clipText(v, 100)
				}
			case []interface{}:
				if len(v) > 0 {
					if first, ok := v[0].(map[string]interface{}); ok {
						msg := strings.TrimSpace(fmt.Sprintf("%v", first["msg"]))
						if loc, ok := first["loc"].([]interface{}); ok && len(loc) > 0 {
							parts := make([]string, 0, len(loc))
							for _, p := range loc {
								parts = append(parts, fmt.Sprintf("%v", p))
							}
							msg = strings.Join(parts, ".") + ": " + msg
						}
						if msg != "" && msg != "<nil>" {
							return clipText(msg, 100)
						}
					}
				}
			case map[string]interface{}:
				if inner := strings.TrimSpace(fmt.Sprintf("%v", v["message"])); inner != "" && inner != "<nil>" {
					return clipText(inner, 100)
				}
			}
		}
		return ""
	}
	if strings.HasPrefix(body, "<") {
		return ""
	}
	return clipText(strings.SplitN(body, "\n", 2)[0], 80)
}
