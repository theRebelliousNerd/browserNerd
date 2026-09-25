package mcp

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"browsernerd-mcp-server/internal/mangle"
)

// pageLoadWindowMs bounds how far before the latest navigation fact the current page
// load may have begun. One load records several navigation facts: the frame commit the
// event stream sees, redirect hops, and the fact browser-act records once the load has
// finished, after the page's own requests have already failed.
const pageLoadWindowMs = 30000

// observeHealthDigest counts what went wrong on the current page from the facts the
// event stream already holds: responses with status >= 400, console errors, and
// JavaScript dialogs. It reads the fact buffer directly (no rule evaluation), scoped to
// the session and to the current page load, and returns nil when nothing went wrong.
// It is a digest, not a diagnosis: browser-reason explains causes.
func observeHealthDigest(engine *mangle.Engine, sessionID string) map[string]interface{} {
	if engine == nil || sessionID == "" {
		return nil
	}
	navs := make([]int64, 0, 4)
	latest := int64(0)
	for _, f := range engine.FactsByPredicate("navigation_event") {
		if len(f.Args) >= 3 && fmt.Sprintf("%v", f.Args[0]) == sessionID {
			ts := asInt64(f.Args[2])
			navs = append(navs, ts)
			if ts > latest {
				latest = ts
			}
		}
	}
	since := latest
	for _, ts := range navs {
		if ts >= latest-pageLoadWindowMs && ts < since {
			since = ts
		}
	}
	current := func(f mangle.Fact) bool {
		return len(f.Args) > 0 && fmt.Sprintf("%v", f.Args[0]) == sessionID && f.Timestamp.UnixMilli() >= since
	}

	urls := map[string]string{}
	for _, f := range engine.FactsByPredicate("net_request") {
		if len(f.Args) >= 4 && fmt.Sprintf("%v", f.Args[0]) == sessionID {
			urls[fmt.Sprintf("%v", f.Args[1])] = fmt.Sprintf("%v", f.Args[3])
		}
	}
	// A 4xx on a page asset (favicon, image, font, ...) is not a failure the
	// page shows; failed_request, browser-reason and act effects draw the same
	// line by the CDP resource type.
	assets := map[string]bool{}
	for _, f := range engine.FactsByPredicate("net_http_error") {
		if len(f.Args) >= 5 && fmt.Sprintf("%v", f.Args[0]) == sessionID && assetResourceTypes[fmt.Sprintf("%v", f.Args[4])] {
			assets[fmt.Sprintf("%v", f.Args[1])] = true
		}
	}
	type failure struct {
		status int
		url    string
		at     int64
	}
	failed := map[string]failure{}
	for _, f := range engine.FactsByPredicate("net_response") {
		if len(f.Args) < 3 || !current(f) {
			continue
		}
		status := asInt(f.Args[2])
		id := fmt.Sprintf("%v", f.Args[1])
		if status < 400 || (status < 500 && assets[id]) {
			continue
		}
		failed[id] = failure{status: status, url: urls[id], at: f.Timestamp.UnixMilli()}
	}
	consoleErrors := 0
	for _, f := range engine.FactsByPredicate("console_event") {
		if len(f.Args) >= 2 && current(f) && strings.EqualFold(fmt.Sprintf("%v", f.Args[1]), "error") {
			consoleErrors++
		}
	}
	dialogs := 0
	for _, f := range engine.FactsByPredicate("js_dialog") {
		if current(f) {
			dialogs++
		}
	}
	if len(failed) == 0 && consoleErrors == 0 && dialogs == 0 {
		return nil
	}

	out := map[string]interface{}{}
	if len(failed) > 0 {
		list := make([]failure, 0, len(failed))
		for _, f := range failed {
			list = append(list, f)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].at > list[j].at })
		recent := make([]string, 0, 3)
		for _, f := range list {
			if len(recent) == 3 {
				break
			}
			recent = append(recent, fmt.Sprintf("%d %s", f.status, shortRequestPath(f.url)))
		}
		out["failed_requests"] = len(failed)
		out["recent_failures"] = recent
	}
	if consoleErrors > 0 {
		out["console_errors"] = consoleErrors
	}
	if dialogs > 0 {
		out["dialogs"] = dialogs
	}
	return out
}

// shortRequestPath names a request by its path and query, clipped, so a digest line
// stays one short line.
func shortRequestPath(raw string) string {
	if raw == "" {
		return "(unknown url)"
	}
	path := raw
	if u, err := url.Parse(raw); err == nil && u.Path != "" {
		path = u.Path
		if u.RawQuery != "" {
			path += "?" + u.RawQuery
		}
	}
	if len(path) > 60 {
		path = path[:57] + "..."
	}
	return path
}

// healthSummary renders a health digest as a short clause for the observe summary.
func healthSummary(health map[string]interface{}) string {
	if len(health) == 0 {
		return ""
	}
	parts := make([]string, 0, 3)
	if n := asInt(health["failed_requests"]); n > 0 {
		parts = append(parts, fmt.Sprintf("%d failed request(s)", n))
	}
	if n := asInt(health["console_errors"]); n > 0 {
		parts = append(parts, fmt.Sprintf("%d console error(s)", n))
	}
	if n := asInt(health["dialogs"]); n > 0 {
		parts = append(parts, fmt.Sprintf("%d dialog(s)", n))
	}
	return strings.Join(parts, ", ")
}
