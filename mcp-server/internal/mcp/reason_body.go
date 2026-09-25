package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"

	"github.com/go-rod/rod/lib/proto"
)

// The page rendered empty, yet the call behind it answered 200: the answer is
// in the body, and no failure fact carries it. network + expand + body fetches
// the latest matching responses from Chrome's network buffer and returns each
// as its SHAPE - keys, array lengths, short scalar samples - so an empty list,
// a null field or an unexpected envelope reads in one line (items: array[0])
// without paying for the payload.

const (
	maxBodiesPerCall  = 3
	maxBodyParseBytes = 2 << 20
	bodyShapeMaxDepth = 3
	bodyShapeMaxKeys  = 12
	bodyScalarChars   = 60
	bodyTextChars     = 300
)

// attachBodies fetches and summarizes the bodies of the most recent rows in
// response["calls"] (network topic with expand).
func attachBodies(ctx context.Context, sessions *browser.SessionManager, sessionID string, response map[string]interface{}) {
	rows, _ := response["calls"].([]map[string]interface{})
	if len(rows) == 0 {
		return
	}
	if sessions == nil {
		response["body_note"] = "bodies unavailable: no browser session manager"
		return
	}
	page, ok := sessions.Page(sessionID)
	if !ok || page == nil {
		response["body_note"] = "bodies unavailable: session has no open tab"
		return
	}
	order := make([]int, len(rows))
	for i := range rows {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return asInt64(rows[order[a]]["at_ms"]) > asInt64(rows[order[b]]["at_ms"])
	})
	fetched := 0
	for _, i := range order {
		if fetched == maxBodiesPerCall {
			break
		}
		id, _ := rows[i]["id"].(string)
		if id == "" {
			continue
		}
		fetched++
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		res, err := proto.NetworkGetResponseBody{RequestID: proto.NetworkRequestID(id)}.Call(page.Context(callCtx))
		cancel()
		if err != nil {
			rows[i]["body_unavailable"] = bodyUnavailableReason(err)
			continue
		}
		raw := res.Body
		if res.Base64Encoded {
			decoded, decErr := base64.StdEncoding.DecodeString(raw)
			if decErr != nil || !looksLikeText(decoded) {
				rows[i]["body_shape"] = fmt.Sprintf("binary (%d bytes)", len(decoded))
				continue
			}
			raw = string(decoded)
		}
		rows[i]["body_shape"] = summarizeBody(raw)
	}
}

// bodyUnavailableReason explains a failed body read in words an agent can act on.
func bodyUnavailableReason(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "No resource with given identifier") || strings.Contains(msg, "No data found") {
		return "evicted from Chrome's network buffer; repeat the action and expand again right after"
	}
	return clipText(msg, 120)
}

// summarizeBody returns a JSON body's shape, or a text body's first words.
func summarizeBody(body string) interface{} {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return "empty body"
	}
	if len(trimmed) <= maxBodyParseBytes && (trimmed[0] == '{' || trimmed[0] == '[') {
		var v interface{}
		if err := json.Unmarshal([]byte(trimmed), &v); err == nil {
			return jsonShape(v, 0)
		}
	}
	return redactSnippet(strings.Join(strings.Fields(trimmed), " "), bodyTextChars)
}

// jsonShape keeps a JSON value's structure and drops its bulk: objects keep
// up to bodyShapeMaxKeys keys (sorted), arrays become their length plus the
// shape of the first element, strings are clipped and redacted, and anything
// deeper than bodyShapeMaxDepth is named by its keys only.
func jsonShape(v interface{}, depth int) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if depth >= bodyShapeMaxDepth {
			if len(keys) > bodyShapeMaxKeys {
				keys = append(keys[:bodyShapeMaxKeys], fmt.Sprintf("+%d", len(x)-bodyShapeMaxKeys))
			}
			return "object{" + strings.Join(keys, ",") + "}"
		}
		out := make(map[string]interface{}, minInt(len(keys), bodyShapeMaxKeys)+1)
		for i, k := range keys {
			if i == bodyShapeMaxKeys {
				out["..."] = fmt.Sprintf("+%d keys", len(keys)-bodyShapeMaxKeys)
				break
			}
			if isSecretBodyKey(k) {
				out[k] = "[REDACTED]"
				continue
			}
			out[k] = jsonShape(x[k], depth+1)
		}
		return out
	case []interface{}:
		if len(x) == 0 {
			return "array[0]"
		}
		if depth >= bodyShapeMaxDepth {
			return fmt.Sprintf("array[%d]", len(x))
		}
		return map[string]interface{}{
			"array": len(x),
			"first": jsonShape(x[0], depth+1),
		}
	case string:
		return redactSnippet(x, bodyScalarChars)
	default:
		return x
	}
}

// isSecretBodyKey names a JSON key whose value must never be echoed, even
// clipped: tokens, passwords, keys and cookies.
func isSecretBodyKey(key string) bool {
	k := strings.ToLower(key)
	for _, marker := range []string{"token", "password", "passwd", "secret", "api_key", "apikey", "authorization", "cookie", "session_key", "private_key"} {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}

func looksLikeText(b []byte) bool {
	n := len(b)
	if n > 512 {
		n = 512
	}
	for _, c := range b[:n] {
		if c == 0 {
			return false
		}
	}
	return true
}
