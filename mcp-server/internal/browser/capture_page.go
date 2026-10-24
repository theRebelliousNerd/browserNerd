package browser

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// onMainFrameNavigated records where the tab now is. Only the main frame
// moves the session: an iframe loading an ad or widget is not a navigation.
// A main frame committing Chrome's error page is a failed page load of the
// URL it could not reach - the state a tab is left in when a backend restarts
// under it.
func (m *SessionManager) onMainFrameNavigated(ctx context.Context, sessionID string, capture *captureState, frame *proto.PageFrame) {
	if frame == nil || frame.ParentID != "" {
		return
	}
	now := time.Now()
	if registry := m.Registry(sessionID); registry != nil {
		prevCount := registry.Count()
		registry.Clear()
		if prevCount > 0 {
			log.Printf("[session:%s] navigation cleared %d cached elements", sessionID, prevCount)
		}
	}
	url := m.redactor.SanitizeString(frame.URL)
	facts := navigationFacts(sessionID, url, now)
	facts = append(facts, mangle.Fact{
		Predicate: "navigation_context",
		Args:      []interface{}{sessionID, url, "document", string(frame.LoaderID), now.UnixMilli()},
		Timestamp: now,
	})
	if strings.HasPrefix(frame.URL, "chrome-error://") {
		unreachable := m.redactor.SanitizeString(frame.UnreachableURL)
		if unreachable == "" {
			unreachable = url
		}
		if capture.claimPageLoadFailure(unreachable, now) {
			facts = append(facts, pageLoadFailedFact(sessionID, unreachable, "chrome error page", now))
		}
	}
	m.addFacts(ctx, sessionID, "navigation", facts)
	m.recordNavigationEvent(sessionID, url, now)
}

// onSameDocumentNavigation records a client-side route change (pushState,
// replaceState, hash change). A single-page app changes routes this way, and
// without it current_url would stay on the URL the app was first loaded at.
func (m *SessionManager) onSameDocumentNavigation(ctx context.Context, sessionID string, capture *captureState, ev *proto.PageNavigatedWithinDocument) {
	if ev.FrameID != capture.mainFrame {
		return
	}
	now := time.Now()
	if registry := m.Registry(sessionID); registry != nil {
		registry.IncrementGeneration()
	}
	url := m.redactor.SanitizeString(ev.URL)
	facts := append(navigationFacts(sessionID, url, now), mangle.Fact{
		Predicate: "navigation_context",
		Args:      []interface{}{sessionID, url, "same_document", "", now.UnixMilli()},
		Timestamp: now,
	})
	m.addFacts(ctx, sessionID, "navigation", facts)
	m.recordNavigationEvent(sessionID, url, now)
}

func navigationFacts(sessionID, url string, now time.Time) []mangle.Fact {
	return []mangle.Fact{
		{
			Predicate: "navigation_event",
			Args:      []interface{}{sessionID, url, now.UnixMilli()},
			Timestamp: now,
		},
		{
			// current_url is where the session IS; the engine keeps only the
			// latest one per session.
			Predicate: "current_url",
			Args:      []interface{}{sessionID, url},
			Timestamp: now,
		},
	}
}

// onLogEntry records browser-generated warnings and errors: deprecations,
// interventions, security and CSP violations, and network messages that no
// request fact already carries. A network entry naming a WebSocket is how a
// refused WebSocket handshake reports itself.
func (m *SessionManager) onLogEntry(ctx context.Context, sessionID string, capture *captureState, entry *proto.LogLogEntry) {
	if entry == nil {
		return
	}
	level := string(entry.Level)
	if level != string(proto.LogLogEntryLevelError) && level != string(proto.LogLogEntryLevelWarning) {
		return
	}
	now := time.Now()
	text := m.redactor.SanitizeString(truncateUTF8(entry.Text, 2000))
	if entry.NetworkRequestID != "" {
		if _, isSocket := capture.socket(entry.NetworkRequestID); isSocket {
			m.onWebSocketEvent(ctx, sessionID, capture, entry.NetworkRequestID, "error", text, now)
			return
		}
		if entry.Source == proto.LogLogEntrySourceNetwork {
			// net_http_error / net_loading_failed already carry this request.
			return
		}
	}
	m.addFacts(ctx, sessionID, "browser_log", []mangle.Fact{{
		Predicate: "browser_log",
		Args:      []interface{}{sessionID, string(entry.Source), level, text, m.redactor.SanitizeString(entry.URL), now.UnixMilli()},
		Timestamp: now,
	}})
}

// onException records an uncaught exception or unhandled rejection as a
// console error. A render crash that empties a page often throws without any
// console call, and DevTools shows it in the console for the same reason.
func (m *SessionManager) onException(ctx context.Context, sessionID string, ev *proto.RuntimeExceptionThrown) {
	details := ev.ExceptionDetails
	if details == nil {
		return
	}
	message := details.Text
	if details.Exception != nil && details.Exception.Description != "" {
		message = details.Text + " " + details.Exception.Description
	}
	if details.URL != "" {
		message += fmt.Sprintf(" (%s:%d)", details.URL, details.LineNumber+1)
	}
	now := time.Now()
	message = m.redactor.SanitizeString(truncateUTF8(strings.TrimSpace(message), 2000))
	m.recordConsoleErrorEvent(sessionID, "error", message, now)
	m.addFacts(ctx, sessionID, "exception", []mangle.Fact{{
		Predicate: "console_event",
		Args:      []interface{}{sessionID, "error", message, now.UnixMilli()},
		Timestamp: now,
	}})
}

// waitDocumentParsed returns once the page has parsed its document
// (readyState interactive or complete), or after limit. Navigate returns when
// the navigation commits, before the document exists, so an operation run
// right after session_create could click a page whose handlers were not yet
// attached. The load event is not awaited: a page that long-polls never fires
// it.
func waitDocumentParsed(page *rod.Page, limit time.Duration) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		res, err := page.Timeout(2 * time.Second).Eval(`() => document.readyState`)
		if err == nil {
			if state := res.Value.Str(); state == "interactive" || state == "complete" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
}
