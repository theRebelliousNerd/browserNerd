package mcp

import (
	"strings"

	"browsernerd-mcp-server/internal/mangle"
)

// currentPageStartMs uses the latest main document's identity, so a reload
// never borrows another document's request, even when both share a URL.
func currentPageStartMs(engine *mangle.Engine, sessionID string) int64 {
	if engine == nil {
		return 0
	}
	navMs, navURL := int64(0), ""
	for _, f := range engine.FactsByPredicate("navigation_event") {
		if factInSession(f, sessionID) {
			if ts := factTime(f, 2); ts >= navMs {
				navMs, navURL = ts, factString(f, 1)
			}
		}
	}
	if navMs == 0 {
		return 0
	}
	contextMs, kind, loader := int64(0), "", ""
	for _, f := range engine.FactsByPredicate("navigation_context") {
		if !factInSession(f, sessionID) || factString(f, 1) != navURL {
			continue
		}
		if ts := factTime(f, 4); ts <= navMs && ts >= contextMs {
			contextMs, kind, loader = ts, factString(f, 2), factString(f, 3)
		}
	}
	if kind == "same_document" {
		return contextMs
	}
	contexts := map[string]mangle.Fact{}
	for _, f := range engine.FactsByPredicate("net_request_context") {
		if factInSession(f, sessionID) {
			contexts[factString(f, 1)] = f
		}
	}
	previousMs := int64(0)
	for _, f := range engine.FactsByPredicate("navigation_event") {
		if factInSession(f, sessionID) {
			if ts := factTime(f, 2); ts < navMs && ts > previousMs {
				previousMs = ts
			}
		}
	}
	latest, requestID := int64(0), ""
	for _, f := range engine.FactsByPredicate("net_request") {
		if !factInSession(f, sessionID) || withoutFragment(factString(f, 3)) != withoutFragment(navURL) {
			continue
		}
		ts := factTime(f, 5)
		if ts <= 0 || ts > navMs || ts <= latest {
			continue
		}
		rc, hasContext := contexts[factString(f, 1)]
		if hasContext && factString(rc, 2) != "Document" {
			continue
		}
		if kind == "document" {
			if !hasContext || loader == "" || factString(rc, 4) != loader || ts > contextMs {
				continue
			}
		} else if navMs-ts > documentRequestLeadMs || ts < previousMs {
			continue
		}
		latest, requestID = ts, factString(f, 1)
	}
	if latest > 0 {
		// Redirects retain one request id. Include its initial request, whose
		// start is also the joined network call's start, before the final URL.
		for _, f := range engine.FactsByPredicate("net_request") {
			if factInSession(f, sessionID) && factString(f, 1) == requestID {
				if ts := factTime(f, 5); ts > 0 && ts < latest {
					latest = ts
				}
			}
		}
		return latest
	}
	return navMs
}

func withoutFragment(raw string) string {
	return strings.SplitN(raw, "#", 2)[0]
}

// Evidence without a document loader has a bounded request lead.
const documentRequestLeadMs = 30000
