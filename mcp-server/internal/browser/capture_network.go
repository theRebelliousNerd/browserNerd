package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

const (
	// maxTrackedRequests bounds the per-session request and WebSocket maps
	// that correlate later CDP events (response, finish, failure) with the
	// request that started them.
	maxTrackedRequests = 4096
	// maxFailureBodyBytes bounds a net_failure_body snippet.
	maxFailureBodyBytes = 1024
	// maxFailureBodyFetchBytes skips bodies too large to be an error message.
	maxFailureBodyFetchBytes = 256 << 10
	// failureBodyQueue bounds pending body fetches; overflow is dropped.
	failureBodyQueue = 32
	// pageLoadFailDedupe collapses the loadingFailed and chrome-error
	// frameNavigated reports of one failed document load.
	pageLoadFailDedupe = 5 * time.Second
)

// requestInfo is what a later CDP event needs to know about its request.
type requestInfo struct {
	url          string
	resourceType string
	mainDocument bool
	sentAt       proto.MonotonicTime
	status       int
	mimeType     string
}

// captureState is one session's CDP correlation state. The nav, dialog and
// network subscriptions run on separate goroutines, so it is locked.
type captureState struct {
	mu            sync.Mutex
	mainFrame     proto.PageFrameID
	requests      map[proto.NetworkRequestID]*requestInfo
	requestOrder  []proto.NetworkRequestID
	sockets       map[proto.NetworkRequestID]string
	lastLoadFail  string
	lastLoadFailT time.Time
	bodyJobs      chan proto.NetworkRequestID
	// net counts requests in flight; it has its own lock and outlives the
	// correlation maps above, which keep error responses until their body
	// is fetched.
	net *networkActivity
}

func newCaptureState(mainFrame proto.PageFrameID) *captureState {
	return &captureState{
		net:       newNetworkActivity(),
		mainFrame: mainFrame,
		requests:  make(map[proto.NetworkRequestID]*requestInfo),
		sockets:   make(map[proto.NetworkRequestID]string),
		bodyJobs:  make(chan proto.NetworkRequestID, failureBodyQueue),
	}
}

func (c *captureState) trackRequest(id proto.NetworkRequestID, info *requestInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.requests[id]; !exists {
		c.requestOrder = append(c.requestOrder, id)
	}
	c.requests[id] = info
	for len(c.requestOrder) > maxTrackedRequests {
		delete(c.requests, c.requestOrder[0])
		c.requestOrder = c.requestOrder[1:]
	}
}

func (c *captureState) request(id proto.NetworkRequestID) (requestInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	info, ok := c.requests[id]
	if !ok {
		return requestInfo{}, false
	}
	return *info, true
}

func (c *captureState) recordResponse(id proto.NetworkRequestID, status int, mimeType string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if info, ok := c.requests[id]; ok {
		info.status = status
		info.mimeType = mimeType
	}
}

func (c *captureState) forgetRequest(id proto.NetworkRequestID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.requests, id)
}

func (c *captureState) openSocket(id proto.NetworkRequestID, url string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sockets) >= maxTrackedRequests {
		for key := range c.sockets {
			delete(c.sockets, key)
			break
		}
	}
	c.sockets[id] = url
}

func (c *captureState) socket(id proto.NetworkRequestID) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	url, ok := c.sockets[id]
	return url, ok
}

func (c *captureState) closeSocket(id proto.NetworkRequestID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sockets, id)
}

// claimPageLoadFailure reports whether a failure of url at now is new, and
// records it; the same document failing twice within the dedupe window is one
// failed load reported through two CDP events.
func (c *captureState) claimPageLoadFailure(url string, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastLoadFail == url && now.Sub(c.lastLoadFailT) < pageLoadFailDedupe {
		return false
	}
	c.lastLoadFail = url
	c.lastLoadFailT = now
	return true
}

func (m *SessionManager) addFacts(ctx context.Context, sessionID, kind string, facts []mangle.Fact) {
	if len(facts) == 0 {
		return
	}
	if err := m.engine.AddFacts(ctx, facts); err != nil {
		log.Printf("[session:%s] %s fact error: %v", sessionID, kind, err)
	}
}

// onHTTPErrorResponse records the response status and, for a status >= 400,
// the HTTP error with its URL and resource type at the time it arrived.
func (m *SessionManager) onHTTPErrorResponse(ctx context.Context, sessionID string, capture *captureState, ev *proto.NetworkResponseReceived, now time.Time) {
	if ev.Response == nil {
		return
	}
	status := ev.Response.Status
	capture.recordResponse(ev.RequestID, status, ev.Response.MIMEType)
	if status < 400 {
		return
	}
	safeURL := m.redactor.SanitizeString(ev.Response.URL)
	facts := []mangle.Fact{{
		Predicate: "net_http_error",
		Args:      []interface{}{sessionID, string(ev.RequestID), safeURL, status, string(ev.Type), now.UnixMilli()},
		Timestamp: now,
	}}
	if info, ok := capture.request(ev.RequestID); ok && info.mainDocument {
		if capture.claimPageLoadFailure(safeURL, now) {
			facts = append(facts, pageLoadFailedFact(sessionID, safeURL, fmt.Sprintf("HTTP %d %s", status, strings.TrimSpace(ev.Response.StatusText)), now))
		}
	}
	m.addFacts(ctx, sessionID, "net_http_error", facts)
}

// onLoadingFinished queues a body fetch for a finished text/JSON error response.
func (m *SessionManager) onLoadingFinished(capture *captureState, ev *proto.NetworkLoadingFinished) {
	info, ok := capture.request(ev.RequestID)
	if !ok {
		return
	}
	if info.status >= 400 && isTextualMIME(info.mimeType) && ev.EncodedDataLength <= maxFailureBodyFetchBytes {
		select {
		case capture.bodyJobs <- ev.RequestID:
		default:
		}
		return
	}
	capture.forgetRequest(ev.RequestID)
}

// onLoadingFailed records a network-level failure: a refused connection, a DNS
// error, a CORS block, an abort. A failed main-frame document is also a failed
// page load, and a failing WebSocket handshake is a WebSocket error.
func (m *SessionManager) onLoadingFailed(ctx context.Context, sessionID string, capture *captureState, ev *proto.NetworkLoadingFailed, now time.Time) {
	errorText := ev.ErrorText
	if ev.CorsErrorStatus != nil && ev.CorsErrorStatus.CorsError != "" {
		errorText += " (CORS " + string(ev.CorsErrorStatus.CorsError) + ")"
	}
	if ev.BlockedReason != "" {
		errorText += " (blocked " + string(ev.BlockedReason) + ")"
	}
	errorText = m.redactor.SanitizeString(errorText)

	if url, ok := capture.socket(ev.RequestID); ok {
		capture.closeSocket(ev.RequestID)
		m.addFacts(ctx, sessionID, "ws_event", []mangle.Fact{wsEventFact(sessionID, ev.RequestID, url, "error", errorText, now)})
		return
	}

	facts := []mangle.Fact{{
		Predicate: "net_loading_failed",
		Args:      []interface{}{sessionID, string(ev.RequestID), errorText, boolFactString(ev.Canceled), now.UnixMilli()},
		Timestamp: now,
	}}
	if info, ok := capture.request(ev.RequestID); ok && info.mainDocument && !ev.Canceled {
		if capture.claimPageLoadFailure(info.url, now) {
			facts = append(facts, pageLoadFailedFact(sessionID, info.url, errorText, now))
		}
	}
	capture.forgetRequest(ev.RequestID)
	m.addFacts(ctx, sessionID, "net_loading_failed", facts)
}

func (m *SessionManager) onWebSocketCreated(ctx context.Context, sessionID string, capture *captureState, ev *proto.NetworkWebSocketCreated, now time.Time) {
	url := m.redactor.SanitizeString(ev.URL)
	capture.openSocket(ev.RequestID, url)
	m.addFacts(ctx, sessionID, "ws_event", []mangle.Fact{wsEventFact(sessionID, ev.RequestID, url, "created", "", now)})
}

func (m *SessionManager) onWebSocketEvent(ctx context.Context, sessionID string, capture *captureState, id proto.NetworkRequestID, event, detail string, now time.Time) {
	url, ok := capture.socket(id)
	if !ok {
		return
	}
	if event == "closed" {
		capture.closeSocket(id)
	}
	m.addFacts(ctx, sessionID, "ws_event", []mangle.Fact{wsEventFact(sessionID, id, url, event, m.redactor.SanitizeString(detail), now)})
}

func (m *SessionManager) onWebSocketHandshake(ctx context.Context, sessionID string, capture *captureState, ev *proto.NetworkWebSocketHandshakeResponseReceived, now time.Time) {
	if ev.Response == nil {
		return
	}
	detail := strings.TrimSpace(fmt.Sprintf("%d %s", ev.Response.Status, ev.Response.StatusText))
	event := "open"
	if ev.Response.Status != 101 {
		event = "error"
	}
	m.onWebSocketEvent(ctx, sessionID, capture, ev.RequestID, event, detail, now)
}

func wsEventFact(sessionID string, id proto.NetworkRequestID, url, event, detail string, now time.Time) mangle.Fact {
	return mangle.Fact{
		Predicate: "ws_event",
		Args:      []interface{}{sessionID, string(id), url, event, detail, now.UnixMilli()},
		Timestamp: now,
	}
}

func pageLoadFailedFact(sessionID, url, errorText string, now time.Time) mangle.Fact {
	return mangle.Fact{
		Predicate: "page_load_failed",
		Args:      []interface{}{sessionID, url, errorText, now.UnixMilli()},
		Timestamp: now,
	}
}

func boolFactString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// runFailureBodyFetcher fetches queued error-response bodies one at a time,
// off the event loop, so a slow Network.getResponseBody never delays capture.
func (m *SessionManager) runFailureBodyFetcher(ctx context.Context, sessionID string, page *rod.Page, capture *captureState) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-capture.bodyJobs:
			info, ok := capture.request(id)
			capture.forgetRequest(id)
			if !ok {
				continue
			}
			res, err := proto.NetworkGetResponseBody{RequestID: id}.Call(page.Context(ctx))
			if err != nil || res == nil {
				continue
			}
			body := res.Body
			if res.Base64Encoded {
				decoded, decodeErr := base64.StdEncoding.DecodeString(body)
				if decodeErr != nil || !utf8.Valid(decoded) {
					continue
				}
				body = string(decoded)
			}
			snippet := failureBodySnippet(m.redactor, info.mimeType, body)
			if snippet == "" {
				continue
			}
			now := time.Now()
			m.addFacts(ctx, sessionID, "net_failure_body", []mangle.Fact{{
				Predicate: "net_failure_body",
				Args:      []interface{}{sessionID, string(id), snippet},
				Timestamp: now,
			}})
		}
	}
}

func isTextualMIME(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	return strings.HasPrefix(mimeType, "text/") ||
		strings.Contains(mimeType, "json") ||
		strings.Contains(mimeType, "xml")
}

var (
	htmlDropBlocks = regexp.MustCompile(`(?is)<(script|style|head)\b.*?</(script|style|head)>`)
	htmlTags       = regexp.MustCompile(`(?s)<[^>]*>`)
	whitespaceRun  = regexp.MustCompile(`\s+`)
)

// failureBodySnippet reduces an error response body to at most
// maxFailureBodyBytes of redacted text. JSON is re-encoded after key-based
// redaction; HTML is reduced to its visible text.
func failureBodySnippet(redactor *security.Redactor, mimeType, body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	lowerMIME := strings.ToLower(mimeType)
	switch {
	case strings.Contains(lowerMIME, "json") || strings.HasPrefix(body, "{") || strings.HasPrefix(body, "["):
		var decoded interface{}
		if err := json.Unmarshal([]byte(body), &decoded); err == nil {
			if redactor != nil {
				decoded = redactor.Sanitize(decoded)
			}
			if encoded, err := json.Marshal(decoded); err == nil {
				body = string(encoded)
			}
		}
	case strings.Contains(lowerMIME, "html"):
		body = htmlDropBlocks.ReplaceAllString(body, " ")
		body = htmlTags.ReplaceAllString(body, " ")
	}
	body = strings.TrimSpace(whitespaceRun.ReplaceAllString(body, " "))
	if redactor != nil {
		body = redactor.SanitizeString(body)
	}
	return truncateUTF8(body, maxFailureBodyBytes)
}

// truncateUTF8 cuts s to at most limit bytes without splitting a rune.
func truncateUTF8(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
