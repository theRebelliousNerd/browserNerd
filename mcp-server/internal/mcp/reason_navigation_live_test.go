package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLiveReasonImmediateHealthyReloadScopesCurrentDocument(t *testing.T) {
	var healthy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/records" {
			w.Header().Set("Content-Type", "application/json")
			if !healthy.Load() {
				w.WriteHeader(502)
			}
			fmt.Fprint(w, `{"items":[]}`)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><title>Records fixture</title><h1>Records</h1>
<p id="state">Loading</p><script>fetch('/api/records?name=selected&page=1').then(r=>document.getElementById('state').textContent=r.ok?'Available':'Unavailable')</script>
<button id="route" onclick="fetch(location.href).then(()=>history.replaceState({},'',location.href))">Refresh route state</button>`)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	act, sid := liveDataPage(t, ctx, "<!doctype html><title>Neutral fixture</title>")
	run := func(op map[string]interface{}) {
		t.Helper()
		result, err := act.Execute(ctx, map[string]interface{}{"session_id": sid, "operations": ops(op, map[string]interface{}{"type": "await_stable"}), "settle_ms": 0})
		if err != nil || asMap(result)["success"] != true {
			t.Fatalf("action failed: %v %v", result, err)
		}
	}
	run(map[string]interface{}{"type": "navigate", "url": server.URL + "/records"})
	reason := &BrowserReasonTool{engine: act.engine}
	before := runReason(t, reason, map[string]interface{}{"session_id": sid, "topic": "network"})
	if before["status"] != "error" {
		t.Fatalf("fixture failed to capture initial 502: %+v", before)
	}
	healthy.Store(true)
	run(map[string]interface{}{"type": "history", "action": "reload"})
	page, _ := act.sessions.Page(sid)
	state, err := page.Eval(`() => document.getElementById('state').textContent`)
	if err != nil || state.Value.Str() != "Available" {
		t.Fatalf("healthy document did not render: %v %v", state, err)
	}
	for _, topic := range []string{"network", "health", "why_failed", "blocking_issue"} {
		result := runReason(t, reason, map[string]interface{}{"session_id": sid, "topic": topic, "since_navigation": true})
		if result["status"] != "ok" {
			t.Errorf("%s retained a previous document's 502: %+v", topic, result)
		}
	}
	wide := runReason(t, reason, map[string]interface{}{"session_id": sid, "topic": "network", "since_navigation": false, "expand": "/api/records"})
	calls := toMapSlice(wide["calls"])
	if len(calls) != 2 || calls[0]["before_navigation"] != true || calls[1]["before_navigation"] != nil || wide["status"] != "warning" {
		t.Fatalf("history did not retain accurate old and current calls: %+v", wide)
	}
	if !strings.Contains(fmt.Sprint(wide["groups"]), "name=[OMITTED]&page=[OMITTED]") {
		t.Fatalf("live grouping did not mark omitted query values: %+v", wide)
	}
	// A fetch to the document URL followed by same-URL replaceState is a route
	// event, not another document request that can backdate the route boundary.
	run(map[string]interface{}{"type": "click", "ref": "route"})
	var latest int64
	for _, f := range act.engine.FactsByPredicate("navigation_context") {
		if factInSession(f, sid) && factString(f, 2) == "same_document" && factTime(f, 4) > latest {
			latest = factTime(f, 4)
		}
	}
	if latest == 0 || currentPageStartMs(act.engine, sid) != latest {
		t.Fatalf("same-document route borrowed a fetch or document boundary: latest=%d got=%d", latest, currentPageStartMs(act.engine, sid))
	}
}
