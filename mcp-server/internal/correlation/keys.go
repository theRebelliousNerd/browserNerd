package correlation

import (
	"regexp"
	"strings"
)

// Key represents a normalized correlation key.
type Key struct {
	Type  string
	Value string
}

var (
	traceparentPattern = regexp.MustCompile(`(?i)^\s*([0-9a-f]{2})-([0-9a-f]{32})-([0-9a-f]{16})-([0-9a-f]{2})\s*$`)
	cloudTracePattern  = regexp.MustCompile(`(?i)^\s*([0-9a-f]{32})(?:/[0-9]+)?(?:;o=\d+)?\s*$`)
	b3SinglePattern    = regexp.MustCompile(`(?i)^\s*([0-9a-f]{16,32})-[0-9a-f]{16}(?:-[01d](?:-[0-9a-f]{16})?)?\s*$`)

	requestIDPattern   = regexp.MustCompile(`(?i)\b(?:x-request-id|request[_-]?id)\b["']?\s*(?:=|:)\s*["']?([a-z0-9][a-z0-9._:/\-]{5,127})`)
	correlationPattern = regexp.MustCompile(`(?i)\b(?:x-correlation-id|correlation[_-]?id)\b["']?\s*(?:=|:)\s*["']?([a-z0-9][a-z0-9._:/\-]{5,127})`)
	traceIDPattern     = regexp.MustCompile(`(?i)\b(?:x-trace-id|trace[_-]?id|x-b3-traceid)\b["']?\s*(?:=|:)\s*["']?([0-9a-f]{16,64})`)
	traceparentMsgPat  = regexp.MustCompile(`(?i)\btraceparent\b["']?\s*(?:=|:)\s*["']?([0-9a-f]{2}-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2})`)
	cloudTraceMsgPat   = regexp.MustCompile(`(?i)\bx-cloud-trace-context\b["']?\s*(?:=|:)\s*["']?([0-9a-f]{32})(?:/[0-9]+)?`)
)

// FromHeader extracts normalized correlation keys from a network header pair.
func FromHeader(name, value string) []Key {
	headerName := strings.ToLower(strings.TrimSpace(name))
	headerValue := normalizeValue(value)
	if headerName == "" || headerValue == "" {
		return nil
	}

	keys := make([]Key, 0, 2)
	switch headerName {
	case "x-request-id", "request-id", "request_id":
		keys = append(keys, Key{Type: "request_id", Value: headerValue})
	case "x-correlation-id", "correlation-id", "correlation_id", "x-correlationid":
		keys = append(keys, Key{Type: "correlation_id", Value: headerValue})
	case "x-trace-id", "trace-id", "trace_id", "x-b3-traceid":
		keys = append(keys, Key{Type: "trace_id", Value: headerValue})
	case "traceparent":
		if traceID := traceIDFromTraceparent(headerValue); traceID != "" {
			keys = append(keys, Key{Type: "trace_id", Value: traceID})
		}
	case "x-cloud-trace-context":
		if traceID := traceIDFromCloudTrace(headerValue); traceID != "" {
			keys = append(keys, Key{Type: "trace_id", Value: traceID})
		}
	case "b3":
		if traceID := traceIDFromB3Single(headerValue); traceID != "" {
			keys = append(keys, Key{Type: "trace_id", Value: traceID})
		}
	}

	return dedupe(keys)
}

// FromMessage extracts normalized correlation keys from arbitrary log text.
func FromMessage(message string) []Key {
	msg := strings.ToLower(strings.TrimSpace(message))
	if msg == "" {
		return nil
	}

	keys := make([]Key, 0, 4)
	for _, match := range requestIDPattern.FindAllStringSubmatch(msg, -1) {
		if len(match) >= 2 {
			if value := normalizeValue(match[1]); value != "" {
				keys = append(keys, Key{Type: "request_id", Value: value})
			}
		}
	}
	for _, match := range correlationPattern.FindAllStringSubmatch(msg, -1) {
		if len(match) >= 2 {
			if value := normalizeValue(match[1]); value != "" {
				keys = append(keys, Key{Type: "correlation_id", Value: value})
			}
		}
	}
	for _, match := range traceIDPattern.FindAllStringSubmatch(msg, -1) {
		if len(match) >= 2 {
			if value := normalizeValue(match[1]); value != "" {
				keys = append(keys, Key{Type: "trace_id", Value: value})
			}
		}
	}
	for _, match := range traceparentMsgPat.FindAllStringSubmatch(msg, -1) {
		if len(match) >= 2 {
			if traceID := traceIDFromTraceparent(match[1]); traceID != "" {
				keys = append(keys, Key{Type: "trace_id", Value: traceID})
			}
		}
	}
	for _, match := range cloudTraceMsgPat.FindAllStringSubmatch(msg, -1) {
		if len(match) >= 2 {
			if traceID := traceIDFromCloudTrace(match[1]); traceID != "" {
				keys = append(keys, Key{Type: "trace_id", Value: traceID})
			}
		}
	}

	return dedupe(keys)
}

func traceIDFromTraceparent(value string) string {
	matches := traceparentPattern.FindStringSubmatch(value)
	if len(matches) != 5 {
		return ""
	}
	return normalizeValue(matches[2])
}

func traceIDFromCloudTrace(value string) string {
	matches := cloudTracePattern.FindStringSubmatch(value)
	if len(matches) != 2 {
		return ""
	}
	return normalizeValue(matches[1])
}

func traceIDFromB3Single(value string) string {
	matches := b3SinglePattern.FindStringSubmatch(value)
	if len(matches) != 2 {
		return ""
	}
	return normalizeValue(matches[1])
}

func normalizeValue(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	normalized = strings.Trim(normalized, "\"'`")
	normalized = strings.TrimRight(normalized, ".,;:)]}")
	return normalized
}

func dedupe(keys []Key) []Key {
	if len(keys) <= 1 {
		return keys
	}

	seen := make(map[string]struct{}, len(keys))
	uniq := make([]Key, 0, len(keys))
	for _, key := range keys {
		if key.Type == "" || key.Value == "" {
			continue
		}
		token := key.Type + ":" + key.Value
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		uniq = append(uniq, key)
	}
	return uniq
}
