package mcp

import (
	"fmt"
	"strings"
	"testing"
)

func TestReasonHealthyReloadLabelsHistoryWithoutChangingDefaults(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	p := newProbeFacts("history-labels")
	url := probeOrigin + "/records"
	p.request("old-document", "GET", url, 100, 200, 1)
	p.add("navigation_event", 120, url, p.at(120).UnixMilli())
	p.request("old-api", "GET", probeOrigin+"/api/records", 200, 502, 1)
	p.request("current-document", "GET", url, 1000, 200, 1)
	p.add("navigation_event", 1020, url, p.at(1020).UnixMilli())
	p.add("current_url", 1020, url)
	p.request("current-api", "GET", probeOrigin+"/api/records", 1100, 200, 1)
	loadFacts(t, engine, p)
	tool := &BrowserReasonTool{engine: engine}
	for _, view := range []string{"summary", "compact", "full"} {
		for _, topic := range []string{"health", "why_failed", "blocking_issue"} {
			for _, mode := range []string{"default", "history", "navigation"} {
				t.Run(view+"/"+topic+"/"+mode, func(t *testing.T) {
					args := map[string]interface{}{"session_id": p.sid, "topic": topic, "view": view}
					if mode != "default" {
						args["since_navigation"] = mode == "navigation"
					}
					res := runReason(t, tool, args)
					label := fmt.Sprint(res["scope"])
					if mode == "navigation" {
						if res["status"] != "ok" || label != "since the last navigation" || asInt64(res["scope_since_ms"]) != p.at(1000).UnixMilli() {
							t.Fatalf("healthy current document was not scoped explicitly: %+v", res)
						}
					} else if res["status"] != "error" || label != "in the last 300s of session history" || !strings.HasPrefix(fmt.Sprint(res["verdict"]), label+": GET /api/records -> 502") {
						t.Fatalf("historical failure was dropped or presented without its history scope: %+v", res)
					}
				})
			}
		}
	}
	wide := runReason(t, tool, map[string]interface{}{"session_id": p.sid, "topic": "network", "since_navigation": false, "expand": "/api/records"})
	calls := toMapSlice(wide["calls"])
	if wide["scope"] != "in the last 300s of session history" || wide["status"] != "warning" || len(calls) != 2 || calls[0]["before_navigation"] != true || calls[1]["before_navigation"] != nil {
		t.Fatalf("history labels changed request evidence or recovery: %+v", wide)
	}
}
