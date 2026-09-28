package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"browsernerd-mcp-server/internal/correlation"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// startEventStream wires a session's CDP events into facts. Every domain is
// enabled and every subscription made BEFORE it returns, so a caller that
// opened the tab at about:blank and navigates afterwards captures the page
// load from its first request - the document, its load-time fetches, and the
// console output of its first scripts. Only the draining runs in background.
//
// Dialogs are answered even without an engine: an unanswered dialog blocks
// the renderer and every later call on the tab.
func (m *SessionManager) startEventStream(ctx context.Context, sessionID string, page *rod.Page) {
	waitDialog := page.Context(ctx).EachEvent(func(ev *proto.PageJavascriptDialogOpening) {
		m.handleDialog(ctx, sessionID, page, ev)
	})
	waits := []func(){waitDialog}

	var capture *captureState
	if m.engine != nil {
		mainFrame := proto.PageFrameID(page.TargetID)
		if tree, err := (proto.PageGetFrameTree{}).Call(page); err == nil && tree.FrameTree != nil && tree.FrameTree.Frame != nil {
			mainFrame = tree.FrameTree.Frame.ID
		}
		capture = newCaptureState(mainFrame)

		// The trackers run in every new document before its own scripts, and
		// once now for a tab that is already showing a page.
		if _, err := (proto.PageAddScriptToEvaluateOnNewDocument{Source: pageHookNewDocumentSource}).Call(page); err != nil {
			log.Printf("[session:%s] install page hooks: %v", sessionID, err)
		}
		_, _ = page.Context(ctx).Evaluate(&rod.EvalOptions{JS: pageHookFunction, ByValue: true, AwaitPromise: true})

		waits = append(waits, m.subscribePageEvents(ctx, sessionID, page, capture), m.subscribeRuntimeEvents(ctx, sessionID, page, capture))
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[session:%s] event stream panic recovered: %v\n%s", sessionID, r, debug.Stack())
			}
		}()
		var wg sync.WaitGroup
		run := func(name string, fn func()) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[session:%s] %s panic recovered: %v\n%s", sessionID, name, r, debug.Stack())
					}
				}()
				fn()
			}()
		}
		for i, wait := range waits {
			run(fmt.Sprintf("event subscription %d", i), wait)
		}
		if capture != nil {
			run("failure body fetcher", func() { m.runFailureBodyFetcher(ctx, sessionID, page, capture) })
			run("event poller", func() { m.pollPageEvents(ctx, sessionID, page) })
		}
		wg.Wait()
	}()
}

// subscribePageEvents covers where the tab is: main-frame navigations,
// same-document route changes, error pages, and the load that triggers a DOM
// snapshot.
func (m *SessionManager) subscribePageEvents(ctx context.Context, sessionID string, page *rod.Page, capture *captureState) func() {
	level := strings.ToLower(m.cfg.EventLoggingLevel)
	captureDOM := m.cfg.EnableDOMIngestion && level != "minimal"
	throttler := newEventThrottler(m.cfg.EventThrottleMs)
	return page.Context(ctx).EachEvent(
		func(ev *proto.PageFrameNavigated) {
			m.onMainFrameNavigated(ctx, sessionID, capture, ev.Frame)
		},
		func(ev *proto.PageNavigatedWithinDocument) {
			m.onSameDocumentNavigation(ctx, sessionID, capture, ev)
		},
		func(*proto.PageLoadEventFired) {
			if captureDOM && throttler.Allow("dom_load") {
				if err := m.captureDOMFacts(ctx, sessionID, page); err != nil {
					log.Printf("[session:%s] DOM capture error: %v", sessionID, err)
				}
			}
		},
		func(*proto.DOMDocumentUpdated) {
			// Cached element refs may be stale once the document changes.
			if registry := m.Registry(sessionID); registry != nil {
				registry.IncrementGeneration()
			}
			if captureDOM && throttler.Allow("dom_update") {
				if err := m.captureDOMFacts(ctx, sessionID, page); err != nil {
					log.Printf("[session:%s] DOM capture error: %v", sessionID, err)
				}
			}
		},
	)
}

// subscribeRuntimeEvents covers what the page does: console output, uncaught
// exceptions, browser log entries, HTTP traffic and its failures, and
// WebSocket lifecycles. Frame payloads are never captured.
func (m *SessionManager) subscribeRuntimeEvents(ctx context.Context, sessionID string, page *rod.Page, capture *captureState) func() {
	level := strings.ToLower(m.cfg.EventLoggingLevel)
	captureHeaders := m.cfg.EnableHeaderIngestion && level != "minimal"
	consoleErrorsOnly := level == "minimal"
	throttler := newEventThrottler(m.cfg.EventThrottleMs)
	return page.Context(ctx).EachEvent(
		func(ev *proto.RuntimeConsoleAPICalled) {
			isProblem := ev.Type == proto.RuntimeConsoleAPICalledTypeError || ev.Type == proto.RuntimeConsoleAPICalledTypeWarning
			if consoleErrorsOnly && !isProblem {
				return
			}
			// Errors and warnings are never throttled away.
			if !isProblem && !throttler.Allow("console") {
				return
			}
			now := time.Now()
			msg := m.redactor.SanitizeString(stringifyConsoleArgs(ev.Args))
			if ev.Type == proto.RuntimeConsoleAPICalledTypeError {
				m.recordConsoleErrorEvent(sessionID, string(ev.Type), msg, now)
			}
			m.addFacts(ctx, sessionID, "console", []mangle.Fact{{
				Predicate: "console_event",
				Args:      []interface{}{sessionID, string(ev.Type), msg, now.UnixMilli()},
				Timestamp: now,
			}})
		},
		func(ev *proto.RuntimeExceptionThrown) {
			m.onException(ctx, sessionID, ev)
		},
		func(ev *proto.LogEntryAdded) {
			m.onLogEntry(ctx, sessionID, capture, ev.Entry)
		},
		func(ev *proto.NetworkRequestWillBeSent) {
			m.onRequestWillBeSent(ctx, sessionID, capture, captureHeaders, ev)
		},
		func(ev *proto.NetworkResponseReceived) {
			m.onResponseReceived(ctx, sessionID, capture, captureHeaders, ev)
		},
		func(ev *proto.NetworkLoadingFinished) {
			m.onLoadingFinished(capture, ev)
		},
		func(ev *proto.NetworkLoadingFailed) {
			m.onLoadingFailed(ctx, sessionID, capture, ev, time.Now())
		},
		func(ev *proto.NetworkWebSocketCreated) {
			m.onWebSocketCreated(ctx, sessionID, capture, ev, time.Now())
		},
		func(ev *proto.NetworkWebSocketWillSendHandshakeRequest) {
			m.onWebSocketEvent(ctx, sessionID, capture, ev.RequestID, "handshake", "", time.Now())
		},
		func(ev *proto.NetworkWebSocketHandshakeResponseReceived) {
			m.onWebSocketHandshake(ctx, sessionID, capture, ev, time.Now())
		},
		func(ev *proto.NetworkWebSocketFrameError) {
			m.onWebSocketEvent(ctx, sessionID, capture, ev.RequestID, "error", ev.ErrorMessage, time.Now())
		},
		func(ev *proto.NetworkWebSocketClosed) {
			m.onWebSocketEvent(ctx, sessionID, capture, ev.RequestID, "closed", "", time.Now())
		},
	)
}

func (m *SessionManager) onRequestWillBeSent(ctx context.Context, sessionID string, capture *captureState, captureHeaders bool, ev *proto.NetworkRequestWillBeSent) {
	now := time.Now()
	if ev.Request == nil {
		return
	}
	initiatorType := ""
	initiatorID := ""
	initiatorScript := ""
	initiatorLineNo := 0

	// Initiator extraction for cascading failure detection.
	if ev.Initiator != nil {
		initiatorType = string(ev.Initiator.Type)
		// Priority 1: a request chain (fetch triggered by another request).
		if ev.Initiator.RequestID != "" {
			initiatorID = string(ev.Initiator.RequestID)
		}
		// Priority 2: a URL initiator (redirect or prefetch).
		if initiatorID == "" && ev.Initiator.URL != "" {
			initiatorID = ev.Initiator.URL
		}
		// Priority 3: the most specific non-internal script on the call stack.
		if ev.Initiator.Stack != nil && len(ev.Initiator.Stack.CallFrames) > 0 {
			frame := ev.Initiator.Stack.CallFrames[0]
			initiatorScript = frame.URL
			if initiatorScript == "" {
				initiatorScript = string(frame.ScriptID)
			}
			initiatorLineNo = frame.LineNumber
			for _, f := range ev.Initiator.Stack.CallFrames {
				if f.URL != "" && !isInternalScript(f.URL) {
					initiatorScript = f.URL
					initiatorLineNo = f.LineNumber
					break
				}
			}
		}
	}

	initiatorRef := coalesceNonEmpty(initiatorID, initiatorScript)
	if initiatorLineNo > 0 && initiatorScript != "" {
		initiatorRef = fmt.Sprintf("%s:%d", initiatorScript, initiatorLineNo)
	}
	initiatorRef = m.redactor.SanitizeString(initiatorRef)
	safeURL := m.redactor.SanitizeString(ev.Request.URL)
	m.recordRequestEvent(sessionID, string(ev.RequestID), safeURL, now)
	capture.trackRequest(ev.RequestID, &requestInfo{
		url:          safeURL,
		resourceType: string(ev.Type),
		mainDocument: ev.Type == proto.NetworkResourceTypeDocument && ev.FrameID == capture.mainFrame,
		sentAt:       ev.Timestamp,
	})

	facts := []mangle.Fact{{
		Predicate: "net_request",
		Args:      []interface{}{sessionID, string(ev.RequestID), ev.Request.Method, safeURL, initiatorRef, now.UnixMilli()},
		Timestamp: now,
	}}
	if initiatorType != "" || initiatorRef != "" {
		facts = append(facts, mangle.Fact{
			Predicate: "request_initiator",
			Args:      []interface{}{sessionID, string(ev.RequestID), initiatorType, initiatorRef},
			Timestamp: now,
		})
	}
	if captureHeaders {
		facts = append(facts, m.headerFacts(sessionID, string(ev.RequestID), "req", ev.Request.Headers, now)...)
	}
	if strings.TrimSpace(ev.Request.PostData) != "" {
		facts = append(facts, extractRequestPayloadFacts(sessionID, string(ev.RequestID), ev.Request.PostData, ev.Request.Headers, now)...)
	}
	m.addFacts(ctx, sessionID, "net_request", facts)
}

func (m *SessionManager) onResponseReceived(ctx context.Context, sessionID string, capture *captureState, captureHeaders bool, ev *proto.NetworkResponseReceived) {
	if ev.Response == nil {
		return
	}
	now := time.Now()
	latency, duration := responseTimings(capture, ev)
	facts := []mangle.Fact{{
		Predicate: "net_response",
		Args:      []interface{}{sessionID, string(ev.RequestID), ev.Response.Status, latency, duration},
		Timestamp: now,
	}}
	if captureHeaders {
		facts = append(facts, m.headerFacts(sessionID, string(ev.RequestID), "res", ev.Response.Headers, now)...)
	}
	m.addFacts(ctx, sessionID, "net_response", facts)
	m.onHTTPErrorResponse(ctx, sessionID, capture, ev, now)
}

// responseTimings returns net_response's Latency and Duration in whole
// milliseconds: Latency is the server's wait (request sent to first response
// byte), Duration the whole time from the request being issued to its
// response headers arriving, queueing and connection setup included.
func responseTimings(capture *captureState, ev *proto.NetworkResponseReceived) (latency, duration int64) {
	if timing := ev.Response.Timing; timing != nil && timing.ReceiveHeadersEnd > 0 {
		wait := timing.ReceiveHeadersEnd - timing.SendEnd
		if timing.SendEnd <= 0 {
			wait = timing.ReceiveHeadersEnd
		}
		if wait > 0 {
			latency = int64(wait)
		}
	}
	if info, ok := capture.request(ev.RequestID); ok && info.sentAt > 0 && ev.Timestamp >= info.sentAt {
		duration = int64(float64(ev.Timestamp-info.sentAt) * 1000)
	}
	if duration == 0 && ev.Response.Timing != nil && ev.Response.Timing.ReceiveHeadersEnd > 0 {
		duration = int64(ev.Response.Timing.ReceiveHeadersEnd)
	}
	return latency, duration
}

// headerFacts normalizes headers (lowercased keys, redacted credentials) and
// extracts correlation keys (request/correlation/trace ids).
func (m *SessionManager) headerFacts(sessionID, requestID, kind string, headers proto.NetworkHeaders, now time.Time) []mangle.Fact {
	facts := make([]mangle.Fact, 0, len(headers))
	for k, v := range headers {
		key := strings.ToLower(k)
		raw := fmt.Sprintf("%v", v)
		facts = append(facts, mangle.Fact{
			Predicate: "net_header",
			Args:      []interface{}{sessionID, requestID, kind, key, m.redactor.RedactHeader(key, raw)},
			Timestamp: now,
		})
		for _, corr := range correlation.FromHeader(key, raw) {
			facts = append(facts, mangle.Fact{
				Predicate: "net_correlation_key",
				Args:      []interface{}{sessionID, requestID, corr.Type, corr.Value},
				Timestamp: now,
			})
		}
	}
	return facts
}

// pollPageEvents drains the in-page tracker buffer into click, input, state
// and toast facts every 500 ms. It backs off on evaluation failures and stops
// after twenty in a row (the tab is gone or wedged).
func (m *SessionManager) pollPageEvents(ctx context.Context, sessionID string, page *rod.Page) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	consecutiveErrors := 0
	nextAttempt := time.Time{}

	for {
		select {
		case <-ctx.Done():
			return
		case tickTime := <-ticker.C:
			if tickTime.Before(nextAttempt) {
				continue
			}
			if err := m.drainPageEvents(ctx, sessionID, page); err != nil {
				consecutiveErrors++
				if consecutiveErrors >= 20 {
					log.Printf("[session:%s] stopping event poller after %d consecutive evaluation failures", sessionID, consecutiveErrors)
					return
				}
				nextAttempt = tickTime.Add(eventPollBackoff(consecutiveErrors))
				continue
			}
			consecutiveErrors = 0
			nextAttempt = time.Time{}
		}
	}
}

// DrainPageEvents turns what the page's trackers buffered since the last poll
// into facts now. browser-act calls it before reading what a batch caused: a
// toast raised after the last 500 ms poll would otherwise reach the facts only
// after the digest was read. The in-page buffer is taken and reset in one
// evaluation, so a drain and the poller never deliver an event twice.
func (m *SessionManager) DrainPageEvents(ctx context.Context, sessionID string) error {
	page, ok := m.Page(sessionID)
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}
	return m.drainPageEvents(ctx, sessionID, page)
}

// drainPageEvents takes the in-page tracker buffer once and adds its facts.
func (m *SessionManager) drainPageEvents(ctx context.Context, sessionID string, page *rod.Page) error {
	res, err := page.Context(ctx).Evaluate(&rod.EvalOptions{
		JS: `() => {
			const buf = Array.isArray(window.__browsernerdEvents) ? window.__browsernerdEvents : [];
			window.__browsernerdEvents = [];
			return buf;
		}`,
		ByValue:      true,
		AwaitPromise: true,
	})
	if err != nil {
		return err
	}
	if res == nil {
		return fmt.Errorf("page event drain returned nothing")
	}
	if res.Value.Nil() {
		return nil
	}
	raw, err := res.Value.MarshalJSON()
	if err != nil {
		return nil
	}
	var events []struct {
		Type    string  `json:"type"`
		ID      string  `json:"id"`
		Name    string  `json:"name"`
		Value   string  `json:"value"`
		Text    string  `json:"text"`
		Level   string  `json:"level"`
		Source  string  `json:"source"`
		Classes string  `json:"classes"`
		TS      float64 `json:"ts"`
	}
	if err := json.Unmarshal(raw, &events); err != nil {
		return nil
	}
	if len(events) == 0 {
		return nil
	}
	facts := make([]mangle.Fact, 0, len(events))
	for _, ev := range events {
		ts := time.UnixMilli(int64(ev.TS))
		switch ev.Type {
		case "click":
			facts = append(facts, mangle.Fact{
				Predicate: "click_event",
				Args:      []interface{}{sessionID, ev.ID, ts.UnixMilli()},
				Timestamp: ts,
			})
		case "input":
			facts = append(facts, mangle.Fact{
				Predicate: "input_event",
				Args:      []interface{}{sessionID, ev.ID, redactInputValue(m.redactor, ev.ID, ev.Value), ts.UnixMilli()},
				Timestamp: ts,
			})
		case "state":
			facts = append(facts, mangle.Fact{
				Predicate: "state_change",
				Args:      []interface{}{sessionID, ev.Name, ev.Value, ts.UnixMilli()},
				Timestamp: ts,
			})
		case "toast":
			safeText := m.redactor.SanitizeString(ev.Text)
			m.recordToastEvent(sessionID, safeText, ev.Level, ev.Source, ts)
			facts = append(facts, mangle.Fact{
				Predicate: "toast_notification",
				Args:      []interface{}{sessionID, safeText, ev.Level, ev.Source, ts.UnixMilli()},
				Timestamp: ts,
			})
			switch ev.Level {
			case "error":
				facts = append(facts, mangle.Fact{
					Predicate: "error_toast",
					Args:      []interface{}{sessionID, safeText, ev.Source, ts.UnixMilli()},
					Timestamp: ts,
				})
			case "warning":
				facts = append(facts, mangle.Fact{
					Predicate: "warning_toast",
					Args:      []interface{}{sessionID, safeText, ev.Source, ts.UnixMilli()},
					Timestamp: ts,
				})
			}
		}
	}
	m.addFacts(ctx, sessionID, "click/state/toast", facts)
	return nil
}
