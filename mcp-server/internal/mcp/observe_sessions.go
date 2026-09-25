package mcp

import (
	"context"
	"fmt"
	"sort"

	"browsernerd-mcp-server/internal/browser"
)

// sessionIsDetached reports whether a session is a record reloaded from an earlier
// server run with no live tab behind it. Nothing can be observed or driven through it
// until attach-session binds it to a target again.
func sessionIsDetached(s browser.Session) bool {
	return s.Status == "detached"
}

// compactSessionRow is the part of a session an agent needs to pick one.
func compactSessionRow(s browser.Session) map[string]interface{} {
	row := map[string]interface{}{
		"id":       s.ID,
		"url":      s.URL,
		"status":   s.Status,
		"isolated": s.Isolated,
	}
	if s.Title != "" {
		row["title"] = s.Title
	}
	return row
}

// sessionListing lists sessions for mode "sessions": live ones by default, most
// recently active first, with truthful active and detached counts. Detached records
// are listed only when includeDetached is set.
func sessionListing(all []browser.Session, includeDetached bool, view string, maxItems int) (data map[string]interface{}, summary string, truncated bool) {
	sorted := make([]browser.Session, len(all))
	copy(sorted, all)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].LastActive.After(sorted[j].LastActive) })

	listed := make([]browser.Session, 0, len(sorted))
	active, detached := 0, 0
	for _, s := range sorted {
		if sessionIsDetached(s) {
			detached++
			if !includeDetached {
				continue
			}
		} else {
			active++
		}
		listed = append(listed, s)
	}

	summary = fmt.Sprintf("%d active session(s)", active)
	if detached > 0 {
		summary += fmt.Sprintf(", %d detached", detached)
		if !includeDetached {
			summary += " (include_detached:true lists them)"
		}
	}

	data = map[string]interface{}{
		"active_count":   active,
		"detached_count": detached,
	}
	switch view {
	case "summary":
	case "full":
		data["sessions"] = listed
	default:
		rows := make([]map[string]interface{}, 0, len(listed))
		for _, s := range listed {
			if len(rows) >= maxItems {
				truncated = true
				break
			}
			rows = append(rows, compactSessionRow(s))
		}
		data["sessions"] = rows
	}
	return data, summary, truncated
}

// observeSessions is browser-observe mode "sessions".
func (t *BrowserObserveTool) observeSessions(ctx context.Context, args map[string]interface{}, view string, maxItems int) map[string]interface{} {
	data, summary, truncated := sessionListing(t.sessions.List(), getBoolArg(args, "include_detached", false), view, maxItems)
	if view == "full" {
		data["browsers"] = t.sessions.ListBrowsers()
	}
	handles := []string{"observe:sessions"}
	emitDisclosureFacts(ctx, t.engine, "", handles, "observe")
	return map[string]interface{}{
		"success":          true,
		"status":           "ok",
		"mode":             "sessions",
		"view":             view,
		"summary":          summary,
		"data":             data,
		"evidence_handles": handles,
		"truncated":        truncated,
	}
}
