package logs

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var (
	timestampPrefix = regexp.MustCompile(`^\[?(\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:[ \t]?(?:Z|[+-]\d{2}:?\d{2}))?)\]?[ \t]?(.*)$`)
	levelPrefix     = regexp.MustCompile(`^(ERROR|WARNING|WARN|INFO|DEBUG|CRITICAL|FATAL|TRACE)(?:\s*[:|]\s*|\s+)(.*)$`)
	tagPrefix       = regexp.MustCompile(`^\[([A-Z_]+)\]\s*(.*)$`)
	pipeLevel       = regexp.MustCompile(`^\|\s*(ERROR|WARNING|WARN|INFO|DEBUG|CRITICAL|FATAL|TRACE)\s*\|\s*(.*)$`)
	webEvent        = regexp.MustCompile(`^-\s+(error|warn|event|wait|ready)\s+(.*)$`)
	exception       = regexp.MustCompile(`^(?:[\w.]+Error|[\w.]+Exception|Exception|Error|panic):`)
)

// Parse preserves measured timestamps. Standalone undated records have a zero
// timestamp and cannot be correlated; continuation lines inherit their parent.
func Parse(source, text string) []Entry {
	entries, _ := ParseContext(context.Background(), source, text)
	return entries
}

// ParseContext checks cancellation between records and preserves completed
// records alongside any cancellation error.
func ParseContext(ctx context.Context, source, text string) ([]Entry, error) {
	var entries []Entry
	traceback := false
	var message, original strings.Builder
	flush := func() {
		if len(entries) > 0 {
			entries[len(entries)-1].Message = message.String()
			entries[len(entries)-1].Raw = original.String()
		}
	}
	for text != "" {
		if err := ctx.Err(); err != nil {
			flush()
			return entries, err
		}
		var raw string
		raw, text, _ = strings.Cut(text, "\n")
		raw = strings.TrimSuffix(raw, "\r")
		if strings.TrimSpace(raw) == "" {
			continue
		}
		entry := parseLine(source, raw)
		trimmed := strings.TrimSpace(entry.Message)
		isTraceback := strings.HasPrefix(trimmed, "Traceback (most recent call last):")
		continuation := strings.HasPrefix(entry.Message, " ") || strings.HasPrefix(entry.Message, "\t") || strings.HasPrefix(trimmed, "File ") || exception.MatchString(trimmed)
		// Docker prefixes every physical traceback line with its own timestamp.
		// Retain the timestamp of the first line, alongside all original text.
		if len(entries) > 0 && ((traceback && (continuation || isTraceback)) || (entry.Timestamp.IsZero() && (continuation || isTraceback))) {
			last := &entries[len(entries)-1]
			message.WriteString("\n" + entry.Message)
			original.WriteString("\n" + raw)
			if isTraceback || traceback || exception.MatchString(trimmed) {
				last.Level, last.Tag = "ERROR", "TRACEBACK"
			}
			traceback = (traceback || isTraceback) && !exception.MatchString(trimmed)
			continue
		}
		traceback = isTraceback
		if isTraceback {
			entry.Level, entry.Tag = "ERROR", "TRACEBACK"
		}
		flush()
		entries = append(entries, entry)
		message.Reset()
		original.Reset()
		message.WriteString(entry.Message)
		original.WriteString(raw)
	}
	flush()
	return entries, ctx.Err()
}

func parseLine(source, raw string) Entry {
	e := Entry{Source: source, Raw: raw, Level: "INFO"}
	remaining := strings.TrimLeft(raw, "\r")
	if stamp := timestampPrefix.FindStringSubmatch(remaining); len(stamp) == 3 {
		e.Timestamp = parseTimestamp(stamp[1])
		remaining = stamp[2]
		// A transport timestamp takes precedence over a second application stamp.
		if nested := timestampPrefix.FindStringSubmatch(remaining); len(nested) == 3 {
			remaining = nested[2]
		}
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(remaining), &fields) == nil && fields != nil {
		if e.Timestamp.IsZero() {
			e.Timestamp = parseTimestamp(jsonString(fields, "timestamp", "time"))
		}
		e.Message = jsonString(fields, "message", "msg")
		if e.Message == "" {
			e.Message = remaining
		}
		e.Level = normalizeLevel(jsonString(fields, "level", "severity"))
		if e.Level == "" {
			e.Level = inferLevel(e.Message)
		}
		e.Tag = jsonString(fields, "tag")
		return e
	}
	if level := pipeLevel.FindStringSubmatch(strings.TrimSpace(remaining)); len(level) == 3 {
		e.Level, e.Message = normalizeLevel(level[1]), level[2]
	} else if level := levelPrefix.FindStringSubmatch(strings.TrimSpace(remaining)); len(level) == 3 {
		e.Level, e.Message = normalizeLevel(level[1]), level[2]
	} else if tag := tagPrefix.FindStringSubmatch(strings.TrimSpace(remaining)); len(tag) == 3 {
		e.Tag, e.Message = tag[1], tag[2]
		e.Level = inferLevel(tag[1] + " " + tag[2])
	} else if event := webEvent.FindStringSubmatch(strings.TrimSpace(remaining)); len(event) == 3 {
		e.Tag, e.Message = "NEXTJS", event[2]
		e.Level = inferLevel(event[1])
	} else {
		e.Message, e.Level = remaining, inferLevel(remaining)
	}
	return e
}

func jsonString(fields map[string]json.RawMessage, keys ...string) string {
	for _, key := range keys {
		var value string
		if json.Unmarshal(fields[key], &value) == nil && value != "" {
			return value
		}
	}
	return ""
}

func parseTimestamp(value string) time.Time {
	value = strings.Replace(value, ",", ".", 1)
	for _, layout := range []string{
		time.RFC3339Nano, "2006-01-02T15:04:05.999999999Z0700",
		"2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05.999999999Z0700",
		"2006-01-02 15:04:05.999999999 Z07:00", "2006-01-02 15:04:05.999999999 Z0700",
		"2006-01-02T15:04:05.999999999 Z07:00", "2006-01-02T15:04:05.999999999 Z0700",
		"2006-01-02T15:04:05.999999999", "2006-01-02 15:04:05.999999999",
	} {
		if stamp, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return stamp
		}
	}
	return time.Time{}
}

func normalizeLevel(level string) string {
	switch level = strings.ToUpper(level); level {
	case "WARN":
		return "WARNING"
	case "FATAL":
		return "CRITICAL"
	default:
		return level
	}
}

func inferLevel(message string) string {
	text := strings.ToLower(message)
	for _, word := range []string{"error", "exception", "failed", "failure", "traceback", "critical", "fatal", "panic", "crash", "segfault", "timeout", "refused", "denied"} {
		if strings.Contains(text, word) {
			return "ERROR"
		}
	}
	for _, word := range []string{"warn", "deprecated", "slow", "retry", "fallback", "degraded", "skipping", "missing"} {
		if strings.Contains(text, word) {
			return "WARNING"
		}
	}
	return "INFO"
}
