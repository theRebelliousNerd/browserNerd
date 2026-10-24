package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

func eventPollBackoff(consecutiveErrors int) time.Duration {
	if consecutiveErrors <= 0 {
		return 0
	}
	backoff := 500 * time.Millisecond
	for i := 1; i < consecutiveErrors && backoff < 5*time.Second; i++ {
		backoff *= 2
	}
	if backoff > 5*time.Second {
		return 5 * time.Second
	}
	return backoff
}

func stringifyConsoleArgs(args []*proto.RuntimeRemoteObject) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if a == nil {
			continue
		}
		if !a.Value.Nil() {
			parts = append(parts, a.Value.String())
			continue
		}
		if a.Description != "" {
			parts = append(parts, a.Description)
		}
	}
	return strings.Join(parts, " ")
}

// captureDOMFacts records bounded automatic page evidence; explicit inspection returns its own rows.
func (m *SessionManager) captureDOMFacts(ctx context.Context, sessionID string, page *rod.Page) error {
	_, err := m.captureStructuredPage(ctx, sessionID, page, nil, SnapshotOptions{EmitFacts: true}, false)
	return err
}

// SnapshotDOM returns bounded direct DOM evidence independently from fact retention.
func (m *SessionManager) SnapshotDOM(ctx context.Context, sessionID string, root *rod.Element, options SnapshotOptions) (*StructuredSnapshot, error) {
	return m.captureStructured(ctx, sessionID, root, options, false)
}

func snapshotStorage(page *rod.Page, store string) string {
	jsFunc := fmt.Sprintf(`() => {
		try {
			const out = {};
			for (const key of Object.keys(%s)) {
				out[key] = %s.getItem(key);
			}
			return JSON.stringify(out);
		} catch (e) {
			return "{}";
		}
	}`, store, store)

	res, err := page.Evaluate(&rod.EvalOptions{
		JS:           jsFunc,
		ByValue:      true,
		AwaitPromise: true,
	})
	if err != nil || res == nil || res.Value.Nil() {
		return "{}"
	}
	return res.Value.String()
}

// storageRestoreScript returns a new-document script that writes the snapshot
// storage before the page's own scripts run, only on the origin the snapshot
// was taken from. It returns "" when there is nothing to restore.
func storageRestoreScript(sourceURL, localJSON, sessionJSON string) string {
	if (localJSON == "" || localJSON == "{}") && (sessionJSON == "" || sessionJSON == "{}") {
		return ""
	}
	parsed, err := url.Parse(sourceURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	origin, _ := json.Marshal(parsed.Scheme + "://" + parsed.Host)
	local, _ := json.Marshal(localJSON)
	session, _ := json.Marshal(sessionJSON)
	return fmt.Sprintf(`(() => {
	if (location.origin !== %s) return;
	try { Object.entries(JSON.parse(%s || "{}")).forEach(([k, v]) => localStorage.setItem(k, v)); } catch (e) {}
	try { Object.entries(JSON.parse(%s || "{}")).forEach(([k, v]) => sessionStorage.setItem(k, v)); } catch (e) {}
})();`, origin, local, session)
}

func extractRequestPayloadFacts(sessionID, requestID, postData string, headers proto.NetworkHeaders, ts time.Time) []mangle.Fact {
	postData = strings.TrimSpace(postData)
	if postData == "" {
		return nil
	}

	contentType := ""
	for key, value := range headers {
		if strings.EqualFold(key, "content-type") {
			contentType = strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", value)))
			break
		}
	}

	fields := map[string]string{}
	switch {
	case strings.Contains(contentType, "application/json") || strings.HasPrefix(postData, "{"):
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(postData), &payload); err == nil {
			for key, value := range payload {
				fields[key] = inferPayloadValueKind(value)
			}
		}
	case strings.Contains(contentType, "application/x-www-form-urlencoded"):
		if values, err := url.ParseQuery(postData); err == nil {
			for key := range values {
				fields[key] = "string"
			}
		}
	}

	if len(fields) == 0 {
		return nil
	}

	facts := make([]mangle.Fact, 0, len(fields))
	for key, kind := range fields {
		facts = append(facts, mangle.Fact{
			Predicate: "request_payload_field",
			Args:      []interface{}{sessionID, requestID, key, kind},
			Timestamp: ts,
		})
	}
	return facts
}

func inferPayloadValueKind(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "number"
	case string:
		return "string"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		_ = typed
		return "unknown"
	}
}

// persistSessions writes session metadata to disk for continuity across restarts.
