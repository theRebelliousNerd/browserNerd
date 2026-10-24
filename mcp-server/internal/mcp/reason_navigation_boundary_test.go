package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestReasonQueryGroupingMarksOmittedValues(t *testing.T) {
	cases := map[string]string{
		"/api/records?name=chosen&page=1&size=20": "/api/records?name=[OMITTED]&page=[OMITTED]&size=[OMITTED]",
		"/api/records?name=&page&name=chosen":     "/api/records?name=[OMITTED]&page=[OMITTED]",
		"/api/records?access_token=credential":    "/api/records?access_token=[OMITTED]",
		"/api/records?label%26page=chosen":        "/api/records?label%26page=[OMITTED]",
	}
	for input, want := range cases {
		if got := normalizeRequestURL(probeOrigin+input, probeOrigin); got != want {
			t.Errorf("query grouping = %q, want %q", got, want)
		}
	}
	engine := testMangleEngineForProgressive(t)
	p := newProbeFacts("query-values")
	p.add("current_url", 0, probeOrigin+"/records")
	p.request("failed", "GET", probeOrigin+"/api/records?name=chosen", 1000, 502, 1)
	p.request("other", "GET", probeOrigin+"/api/records?name=different", 2000, 200, 1)
	loadFacts(t, engine, p)
	tool := &BrowserReasonTool{engine: engine}
	res := runReason(t, tool, map[string]interface{}{"session_id": p.sid, "topic": "network", "view": "full"})
	groups := toMapSlice(res["groups"])
	if res["status"] != "error" || len(groups) != 1 || groups[0]["recovered"] != nil {
		t.Fatalf("different query values recovered a failed exact URL: %+v", res)
	}
	raw, _ := json.Marshal(res)
	if !strings.Contains(string(raw), "[OMITTED]") || strings.Contains(string(raw), "chosen") || strings.Contains(string(raw), "different") {
		t.Fatalf("group display did not omit values explicitly: %s", raw)
	}
	p.request("retry", "GET", probeOrigin+"/api/records?name=chosen", 3000, 200, 1)
	loadFacts(t, engine, p)
	res = runReason(t, tool, map[string]interface{}{"session_id": p.sid, "topic": "network", "view": "full"})
	if groups = toMapSlice(res["groups"]); res["status"] != "warning" || asInt(groups[0]["recovered"]) != 1 {
		t.Fatalf("matching exact URL did not recover: %+v", res)
	}
}

func TestReasonRapidReloadUsesLatestRequestBoundary(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	p := newProbeFacts("rapid-reload")
	url := probeOrigin + "/records"
	p.request("first-doc", "GET", url, 100, 200, 1)
	p.add("navigation_event", 120, url, p.at(120).UnixMilli())
	p.request("first-api", "GET", probeOrigin+"/api/records?size=20", 200, 502, 1)
	p.request("reload-doc", "GET", url, 500, 200, 1)
	p.add("navigation_event", 520, url, p.at(520).UnixMilli())
	p.add("current_url", 520, url)
	p.request("reload-api", "GET", probeOrigin+"/api/records?size=20", 600, 200, 1)
	loadFacts(t, engine, p)
	tool := &BrowserReasonTool{engine: engine}
	if got := currentPageStartMs(engine, p.sid); got != p.at(500).UnixMilli() {
		t.Errorf("reload boundary = %d, want %d", got, p.at(500).UnixMilli())
	}
	for _, topic := range []string{"network", "health", "why_failed", "blocking_issue"} {
		t.Run(topic, func(t *testing.T) {
			res := runReason(t, tool, map[string]interface{}{"session_id": p.sid, "topic": topic, "since_navigation": true})
			if res["status"] != "ok" {
				t.Errorf("healthy reloaded page retains an earlier failure: %+v", res)
			}
		})
	}
	// Topic defaults retain their documented lookback; the explicit scope
	// controls whether this diagnosis includes the preceding document.
	for _, topic := range []string{"health", "why_failed", "blocking_issue"} {
		res := runReason(t, tool, map[string]interface{}{"session_id": p.sid, "topic": topic})
		if res["status"] != "error" {
			t.Errorf("%s default lost its historical evidence: %+v", topic, res)
		}
	}
	wide := runReason(t, tool, map[string]interface{}{"session_id": p.sid, "topic": "network", "since_navigation": false, "expand": "/api/records"})
	calls := toMapSlice(wide["calls"])
	if len(calls) != 2 || calls[0]["before_navigation"] != true || calls[1]["before_navigation"] != nil || wide["status"] != "warning" {
		t.Fatalf("explicit history lost prior call or mislabeled current call: %s", fmt.Sprint(wide))
	}
}
