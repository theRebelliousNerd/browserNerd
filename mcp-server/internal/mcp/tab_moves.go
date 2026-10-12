package mcp

import (
	"time"

	"browsernerd-mcp-server/internal/browser"
)

// A tab can move while no BrowserNERD call is looking at it: a redirect that
// lands after an act reported, the page navigating on its own, or a person
// using the visible browser window. The navigation is recorded as facts, but
// the next call must say so too; otherwise the caller believes the tab is
// still where its last call left it, and acts on the wrong page.

// tabMovedNote is what a call says about navigations nobody saw.
const tabMovedNote = "the tab navigated after the last BrowserNERD call on it ended: a redirect that landed late, the page navigating on its own, or someone using the browser window"

// unattendedMove describes the navigations sessionID made since the last act
// or observe on it ended, or nil when it made none.
func unattendedMove(sessions *browser.SessionManager, sessionID string) map[string]interface{} {
	if sessions == nil || sessionID == "" {
		return nil
	}
	moves := sessions.UnattendedNavigations(sessionID)
	if len(moves) == 0 {
		return nil
	}
	first, last := moves[0], moves[len(moves)-1]
	origin := originOf(last.ToURL)
	out := map[string]interface{}{
		"from":  shortURL(first.FromURL, origin),
		"to":    shortURL(last.ToURL, origin),
		"at_ms": last.TimestampMs,
		"note":  tabMovedNote,
	}
	if len(moves) > 1 {
		out["hops"] = len(moves)
	}
	return out
}

// markAttended records that a call has seen each session as it is now.
func markAttended(sessions *browser.SessionManager, sessionIDs ...string) {
	if sessions == nil {
		return
	}
	now := time.Now()
	for _, id := range sessionIDs {
		if id != "" {
			sessions.MarkAttended(id, now)
		}
	}
}
