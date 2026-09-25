package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"

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

// captureDOMFacts snapshots a limited DOM view into facts to keep context light.
func (m *SessionManager) captureDOMFacts(ctx context.Context, sessionID string, page *rod.Page) error {
	if m.engine == nil {
		return nil
	}

	const maxNodes = 200
	script := fmt.Sprintf(`
	() => {
		const nodes = Array.from(document.querySelectorAll('*')).slice(0, %d);
		return nodes.map((el, idx) => {
			const attrs = {};
			for (const { name, value } of Array.from(el.attributes || [])) {
				attrs[name] = value;
			}
			const rect = el.getBoundingClientRect();
			const style = window.getComputedStyle(el);
			const isVisible = style.display !== 'none' && style.visibility !== 'hidden' && style.opacity !== '0' && rect.width > 0 && rect.height > 0;
			
			return {
				id: el.id || ('node_' + idx),
				tag: el.tagName,
				text: (el.innerText || '').slice(0, 256),
				parent: el.parentElement && (el.parentElement.id || el.parentElement.tagName || 'root'),
				attrs,
				layout: {
					x: rect.x,
					y: rect.y,
					width: rect.width,
					height: rect.height,
					visible: isVisible
				}
			};
		});
	}
	`, maxNodes)

	res, err := page.Context(ctx).Evaluate(&rod.EvalOptions{
		JS:           script,
		ByValue:      true,
		AwaitPromise: true,
	})
	if err != nil || res == nil {
		return err
	}

	raw, err := res.Value.MarshalJSON()
	if err != nil {
		return err
	}

	var nodes []struct {
		ID     string            `json:"id"`
		Tag    string            `json:"tag"`
		Text   string            `json:"text"`
		Parent string            `json:"parent"`
		Attrs  map[string]string `json:"attrs"`
		Layout struct {
			X       float64 `json:"x"`
			Y       float64 `json:"y"`
			Width   float64 `json:"width"`
			Height  float64 `json:"height"`
			Visible bool    `json:"visible"`
		} `json:"layout"`
	}
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return err
	}

	now := time.Now()
	facts := make([]mangle.Fact, 0, len(nodes)*3)
	for _, n := range nodes {
		facts = append(facts, mangle.Fact{
			Predicate: "dom_node",
			Args:      []interface{}{sessionID, n.ID, n.Tag, m.redactor.SanitizeString(n.Text), n.Parent},
			Timestamp: now,
		})
		if n.Text != "" {
			facts = append(facts, mangle.Fact{
				Predicate: "dom_text",
				Args:      []interface{}{sessionID, n.ID, m.redactor.SanitizeString(n.Text)},
				Timestamp: now,
			})
		}
		sensitiveInput := sensitiveDOMInput(n.Attrs)
		for k, v := range n.Attrs {
			if sensitiveInput && strings.EqualFold(k, "value") {
				v = security.Redacted
			} else {
				v = m.redactor.RedactHeader(k, v)
			}
			facts = append(facts, mangle.Fact{
				Predicate: "dom_attr",
				Args:      []interface{}{sessionID, n.ID, k, v},
				Timestamp: now,
			})
		}
		// Add layout fact
		facts = append(facts, mangle.Fact{
			Predicate: "dom_layout",
			Args:      []interface{}{sessionID, n.ID, n.Layout.X, n.Layout.Y, n.Layout.Width, n.Layout.Height, fmt.Sprintf("%v", n.Layout.Visible)},
			Timestamp: now,
		})
	}
	facts = append(facts, mangle.Fact{
		Predicate: "dom_updated",
		Args:      []interface{}{sessionID, now.UnixMilli()},
		Timestamp: now,
	})
	return m.engine.AddFacts(ctx, facts)
}

// SnapshotDOM triggers a one-off DOM capture for the given session.
func (m *SessionManager) SnapshotDOM(ctx context.Context, sessionID string) error {
	page, ok := m.Page(sessionID)
	if !ok {
		return fmt.Errorf("unknown session: %s", sessionID)
	}
	return m.captureDOMFacts(ctx, sessionID, page)
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
