package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/docker"
	"browsernerd-mcp-server/internal/mangle"
)

// probeFacts builds the event stream the probe page produces, as facts with
// real timestamps: epoch-ms arguments and buffer timestamps that agree.
type probeFacts struct {
	sid   string
	base  time.Time
	facts []mangle.Fact
}

func newProbeFacts(sid string) *probeFacts {
	return &probeFacts{sid: sid, base: time.Now().Add(-40 * time.Second)}
}

func (p *probeFacts) at(ms int64) time.Time { return p.base.Add(time.Duration(ms) * time.Millisecond) }

func (p *probeFacts) add(predicate string, ms int64, args ...interface{}) {
	p.facts = append(p.facts, mangle.Fact{Predicate: predicate, Args: append([]interface{}{p.sid}, args...), Timestamp: p.at(ms)})
}

// request records a request at startMs answered with status after durMs; a
// zero status leaves it unanswered.
func (p *probeFacts) request(id, method, url string, startMs int64, status int, durMs int64) {
	p.add("net_request", startMs, id, method, url, "", p.at(startMs).UnixMilli())
	if status > 0 {
		p.add("net_response", startMs+durMs, id, status, int64(0), int64(0))
	}
}

const probeOrigin = "http://127.0.0.1:5173"

// probeFailureScenario is the probe page after "Trigger failure": a poll
// storm, a 404 with a body, the 500 with its JSON body, the console error and
// toast that follow it, a refused WebSocket, a slow call, and a warning.
func probeFailureScenario(sid string) *probeFacts {
	p := newProbeFacts(sid)
	p.add("navigation_event", 0, probeOrigin+"/index.html", p.at(0).UnixMilli())
	p.add("current_url", 0, probeOrigin+"/index.html")
	p.request("doc", "GET", probeOrigin+"/index.html", 0, 200, 8)
	for i := int64(0); i < 20; i++ {
		p.request(fmt.Sprintf("poll-%d", i), "GET", fmt.Sprintf("%s/api/poll?x=%d", probeOrigin, i), 500+i*400, 200, 3)
	}
	p.request("missing", "GET", probeOrigin+"/api/missing", 600, 404, 4)
	p.add("net_failure_body", 604, "missing", `{"error":"not found"}`)
	p.request("slow", "GET", probeOrigin+"/api/slow", 620, 200, 2503)
	p.request("favicon", "GET", probeOrigin+"/favicon.ico", 640, 404, 2)
	p.add("console_event", 700, "warning", "Deprecated prop used", p.at(700).UnixMilli())
	p.add("ws_event", 710, "ws-1", "ws://127.0.0.1:1/hmr", "created", "", p.at(710).UnixMilli())
	p.add("ws_event", 720, "ws-1", "ws://127.0.0.1:1/hmr", "error", "net::ERR_CONNECTION_REFUSED", p.at(720).UnixMilli())
	p.request("fail", "GET", probeOrigin+"/api/fail", 6000, 500, 5)
	p.add("net_failure_body", 6005, "fail", `{"error":"boom","detail":"database unavailable"}`)
	p.add("console_event", 6061, "error", "Save failed with status 500", p.at(6061).UnixMilli())
	p.add("toast_notification", 6066, "Could not save: server error", "info", "native", p.at(6066).UnixMilli())
	return p
}

func loadFacts(t *testing.T, engine *mangle.Engine, p *probeFacts) {
	t.Helper()
	if err := engine.AddFacts(context.Background(), p.facts); err != nil {
		t.Fatalf("add facts: %v", err)
	}
}

func runReason(t *testing.T, tool *BrowserReasonTool, args map[string]interface{}) map[string]interface{} {
	t.Helper()
	res, err := tool.Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("browser-reason: %v", err)
	}
	m := res.(map[string]interface{})
	if m["success"] != true {
		t.Fatalf("browser-reason failed: %v", m)
	}
	return m
}

func TestReasonWhyFailedNamesTheCausalChain(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	loadFacts(t, engine, probeFailureScenario("s-probe"))
	tool := &BrowserReasonTool{engine: engine}

	res := runReason(t, tool, map[string]interface{}{"session_id": "s-probe", "topic": "why_failed"})
	tokens := responseTokens(t, res)
	raw, _ := json.Marshal(res)
	t.Logf("why_failed compact (synthetic probe): ~%d tokens\n%s", tokens, raw)

	verdict := fmt.Sprintf("%v", res["verdict"])
	for _, want := range []string{"GET /api/fail -> 500", `"database unavailable"`, "console error 56 ms later", `toast "Could not save: server error"`} {
		if !strings.Contains(verdict, want) {
			t.Fatalf("verdict must contain %q, got %q", want, verdict)
		}
	}
	if tokens > 600 {
		t.Fatalf("why_failed compact costs ~%d tokens, budget 600", tokens)
	}
	rc, _ := res["root_cause"].(map[string]interface{})
	req, _ := rc["request"].(map[string]interface{})
	if req["status"] != 500 || !strings.Contains(fmt.Sprintf("%v", req["body"]), "database unavailable") {
		t.Fatalf("root cause must carry the 500 and its body, got %v", rc)
	}
	effects := toMapSlice(rc["effects"])
	if len(effects) != 2 || effects[0]["kind"] != "console_error" || effects[1]["kind"] != "toast" {
		t.Fatalf("expected the console error and the toast as effects, got %v", effects)
	}
	if asInt64(effects[0]["after_ms"]) != 56 {
		t.Fatalf("console error follows the response by 56 ms, got %v", effects[0]["after_ms"])
	}

	issues := strings.Join(toStringSlice(res["issues"]), "\n")
	for _, want := range []string{"GET /api/missing -> 404", "storm GET /api/poll?x= x20 every 400ms", "ws ws://127.0.0.1:1/hmr failed: net::ERR_CONNECTION_REFUSED", "slow GET /api/slow 2503ms"} {
		if !strings.Contains(issues, want) {
			t.Fatalf("issues must contain %q, got:\n%s", want, issues)
		}
	}
	if strings.Contains(issues, "favicon") || strings.Contains(issues, "Deprecated") {
		t.Fatalf("a favicon 404 and a console warning are not failures, got:\n%s", issues)
	}
	for _, dup := range []string{"top_errors", "investigation_items", "action_candidates", "recommendations", "data"} {
		if _, ok := res[dup]; ok {
			t.Fatalf("compact why_failed must not carry %q", dup)
		}
	}
	next, _ := res["next"].(map[string]interface{})
	nextArgs, _ := next["args"].(map[string]interface{})
	if next["tool"] != "browser-reason" || nextArgs["topic"] != "network" || nextArgs["expand"] != "/api/fail" {
		t.Fatalf("next must point at the failing URL's calls, got %v", next)
	}

	summary := runReason(t, tool, map[string]interface{}{"session_id": "s-probe", "topic": "why_failed", "view": "summary"})
	t.Logf("why_failed summary: ~%d tokens", responseTokens(t, summary))
	full := runReason(t, tool, map[string]interface{}{"session_id": "s-probe", "topic": "why_failed", "view": "full"})
	t.Logf("why_failed full: ~%d tokens", responseTokens(t, full))
	data, _ := full["data"].(map[string]interface{})
	for _, section := range []string{"failed_requests", "console", "toasts", "websockets", "storms", "slow_apis"} {
		if data[section] == nil {
			t.Fatalf("full view must carry the %s section, got keys %v", section, mapKeys(data))
		}
	}
}

func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestReasonHealthIsACompactScorecard(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	loadFacts(t, engine, probeFailureScenario("s-health"))
	tool := &BrowserReasonTool{engine: engine}

	res := runReason(t, tool, map[string]interface{}{"session_id": "s-health", "topic": "health"})
	tokens := responseTokens(t, res)
	raw, _ := json.Marshal(res)
	t.Logf("health compact (synthetic probe): ~%d tokens\n%s", tokens, raw)
	if tokens > 300 {
		t.Fatalf("health compact costs ~%d tokens, budget 300", tokens)
	}
	score, _ := res["scorecard"].(map[string]interface{})
	want := map[string]int{"failed_5xx": 1, "failed_4xx": 1, "console_errors": 1, "console_warnings": 1, "toast_errors": 1, "storms": 1, "ws_failures": 1, "slow": 1, "page_load_failed": 0, "dialogs": 0}
	for k, v := range want {
		if asInt(score[k]) != v {
			t.Fatalf("scorecard %s = %v, want %d (scorecard %v)", k, score[k], v, score)
		}
	}
	if !strings.Contains(fmt.Sprintf("%v", res["verdict"]), "/api/fail") {
		t.Fatalf("health must name the top issue, got %v", res["verdict"])
	}
}

func TestReasonNetworkTopicGroupsFlagsStormsAndExpands(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	p := probeFailureScenario("s-net")
	p.request("u1", "GET", probeOrigin+"/api/users/123/profile", 900, 200, 5)
	p.request("u2", "GET", probeOrigin+"/api/users/456/profile", 950, 200, 5)
	p.request("u3", "GET", probeOrigin+"/api/users/2f1c9a3e-9f0b-4b7e-8a51-1d2c3e4f5a6b/profile", 990, 200, 5)
	loadFacts(t, engine, p)
	tool := &BrowserReasonTool{engine: engine}

	summary := runReason(t, tool, map[string]interface{}{"session_id": "s-net", "topic": "network", "view": "summary"})
	tokens := responseTokens(t, summary)
	raw, _ := json.Marshal(summary)
	t.Logf("network summary (synthetic probe): ~%d tokens\n%s", tokens, raw)
	if tokens > 250 {
		t.Fatalf("network summary costs ~%d tokens, budget 250", tokens)
	}
	top := toStringSlice(summary["top"])
	if len(top) != 3 || !strings.HasPrefix(top[0], "500 GET /api/fail") {
		t.Fatalf("the failing group leads the top lines, got %v", top)
	}
	if !strings.Contains(strings.Join(top, "\n"), "storm GET /api/poll?x= x20 every 400ms") {
		t.Fatalf("the poll must be flagged as a storm, got %v", top)
	}

	compact := runReason(t, tool, map[string]interface{}{"session_id": "s-net", "topic": "network"})
	t.Logf("network compact: ~%d tokens", responseTokens(t, compact))
	groups := toMapSlice(compact["groups"])
	var users map[string]interface{}
	for _, g := range groups {
		if g["url"] == "/api/users/{id}/profile" {
			users = g
		}
	}
	if users == nil || asInt(users["count"]) != 3 {
		t.Fatalf("numeric and uuid segments must collapse into one group, got %v", groups)
	}
	if asInt(groups[0]["failures"]) == 0 {
		t.Fatalf("failures sort first, got %v", groups[0])
	}
	ws := toMapSlice(compact["websockets"])
	if len(ws) != 1 || asInt(ws[0]["failed"]) != 1 {
		t.Fatalf("the refused WebSocket must appear with its failure, got %v", ws)
	}

	expanded := runReason(t, tool, map[string]interface{}{"session_id": "s-net", "topic": "network", "expand": "/api/poll", "max_items": 5})
	calls := toMapSlice(expanded["calls"])
	if len(calls) != 5 || asInt(calls[0]["status"]) != 200 {
		t.Fatalf("expand must list the group's last max_items calls, got %v", calls)
	}
}

func TestReasonStuckTabNamesTheRecovery(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	p := newProbeFacts("s-stuck")
	p.add("navigation_event", 0, "http://localhost:3001/network", p.at(0).UnixMilli())
	p.add("page_load_failed", 5000, "http://localhost:3001/network", "net::ERR_CONNECTION_REFUSED", p.at(5000).UnixMilli())
	p.add("navigation_event", 5010, "chrome-error://chromewebdata/", p.at(5010).UnixMilli())
	p.add("current_url", 5010, "chrome-error://chromewebdata/")
	loadFacts(t, engine, p)
	tool := &BrowserReasonTool{engine: engine}

	res := runReason(t, tool, map[string]interface{}{"session_id": "s-stuck", "topic": "blocking_issue", "since_navigation": true})
	verdict := fmt.Sprintf("%v", res["verdict"])
	if !strings.Contains(verdict, "tab stuck on chrome-error") || !strings.Contains(verdict, "localhost:3001/network") || !strings.Contains(verdict, "ERR_CONNECTION_REFUSED") {
		t.Fatalf("verdict must say the tab is stuck and on what, got %q", verdict)
	}
	next, _ := res["next"].(map[string]interface{})
	ops := toMapSlice(asMap(next["args"])["operations"])
	if next["tool"] != "browser-act" || len(ops) != 2 || ops[0]["type"] != "session_close" || ops[1]["url"] != "http://localhost:3001/network" {
		t.Fatalf("next must be the recovery: close then recreate on the URL, got %v", next)
	}

	// A later successful navigation means the tab recovered.
	later := newProbeFacts("s-stuck")
	later.base = p.base
	later.add("navigation_event", 9000, "http://localhost:3001/network", later.at(9000).UnixMilli())
	later.add("current_url", 9000, "http://localhost:3001/network")
	loadFacts(t, engine, later)
	res = runReason(t, tool, map[string]interface{}{"session_id": "s-stuck", "topic": "why_failed"})
	if strings.Contains(fmt.Sprintf("%v", res["verdict"]), "stuck") {
		t.Fatalf("a recovered tab is not stuck, got %v", res["verdict"])
	}
}

func TestReasonSaysADialogWasAutoDismissed(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	p := newProbeFacts("s-dialog")
	p.add("click_event", 1000, "del", p.at(1000).UnixMilli())
	p.add("js_dialog", 1010, "confirm", "Delete everything?", false, "policy", p.at(1010).UnixMilli())
	loadFacts(t, engine, p)
	tool := &BrowserReasonTool{engine: engine}

	res := runReason(t, tool, map[string]interface{}{"session_id": "s-dialog", "topic": "why_failed", "since_action": true})
	verdict := fmt.Sprintf("%v", res["verdict"])
	if !strings.Contains(verdict, `confirm "Delete everything?" auto-dismissed by policy`) {
		t.Fatalf("verdict must say the confirm was dismissed, got %q", verdict)
	}
	if asInt64(res["action_since_ms"]) != p.at(1000).UnixMilli()-actionScopeSlackMs {
		t.Fatalf("since_action must scope to the click, got %v", res["action_since_ms"])
	}
	health := runReason(t, tool, map[string]interface{}{"session_id": "s-dialog", "topic": "health"})
	if asInt(asMap(health["scorecard"])["dialogs"]) != 1 {
		t.Fatalf("health must count the dialog, got %v", health["scorecard"])
	}
}

func TestReasonSinceActionDropsEarlierFailures(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	p := newProbeFacts("s-action")
	p.request("old", "GET", probeOrigin+"/api/old", 100, 500, 5)
	p.add("click_event", 3000, "btn", p.at(3000).UnixMilli())
	p.request("new", "POST", probeOrigin+"/api/save", 3020, 422, 5)
	p.add("net_failure_body", 3025, "new", `{"detail":[{"loc":["body","email"],"msg":"field required","type":"missing"}]}`)
	loadFacts(t, engine, p)
	tool := &BrowserReasonTool{engine: engine}

	res := runReason(t, tool, map[string]interface{}{"session_id": "s-action", "topic": "why_failed", "since_action": true})
	verdict := fmt.Sprintf("%v", res["verdict"])
	if !strings.Contains(verdict, `POST /api/save -> 422 "body.email: field required"`) {
		t.Fatalf("verdict must name what the click broke, got %q", verdict)
	}
	if strings.Contains(strings.Join(toStringSlice(res["issues"]), " "), "/api/old") {
		t.Fatalf("a failure before the click is out of scope, got %v", res["issues"])
	}
}

func TestReasonNetworkLevelFailures(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	p := newProbeFacts("s-netfail")
	p.request("refused", "GET", probeOrigin+"/api/data", 100, 0, 0)
	p.add("net_loading_failed", 130, "refused", "net::ERR_CONNECTION_REFUSED", false, p.at(130).UnixMilli())
	p.request("aborted", "GET", probeOrigin+"/api/search", 200, 0, 0)
	p.add("net_loading_failed", 210, "aborted", "net::ERR_ABORTED", true, p.at(210).UnixMilli())
	p.request("img", "GET", probeOrigin+"/static/logo.png", 220, 404, 1)
	p.request("stream", "GET", probeOrigin+"/api/stream", 300, 200, 10)
	p.add("net_loading_failed", 900, "stream", "net::ERR_HTTP2_PROTOCOL_ERROR", false, p.at(900).UnixMilli())
	loadFacts(t, engine, p)
	tool := &BrowserReasonTool{engine: engine}

	res := runReason(t, tool, map[string]interface{}{"session_id": "s-netfail", "topic": "why_failed"})
	all := append([]string{fmt.Sprintf("%v", res["verdict"])}, toStringSlice(res["issues"])...)
	if len(all) != 2 {
		t.Fatalf("exactly two failures: the refused call and the body that broke after its 200; a canceled fetch and a 404 image are not failures, got %v", all)
	}
	joined := strings.Join(all, "\n")
	for _, want := range []string{"GET /api/data -> net::ERR_CONNECTION_REFUSED", "GET /api/stream -> net::ERR_HTTP2_PROTOCOL_ERROR"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("expected %q, got %v", want, all)
		}
	}
	net := runReason(t, tool, map[string]interface{}{"session_id": "s-netfail", "topic": "network"})
	for _, g := range toMapSlice(net["groups"]) {
		statuses, _ := g["statuses"].(map[string]int)
		if g["url"] == "/api/search" && statuses["canceled"] != 1 {
			t.Fatalf("a canceled fetch counts as canceled, got %v", g)
		}
		if g["url"] == "/api/stream" && statuses["failed"] != 1 {
			t.Fatalf("a body that failed after its 200 counts as failed, got %v", g)
		}
	}
}

func TestReasonExpandHandlesReturnOnlyTheNamedSections(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	loadFacts(t, engine, probeFailureScenario("s-expand"))
	tool := &BrowserReasonTool{engine: engine}

	res := runReason(t, tool, map[string]interface{}{
		"session_id":     "s-expand",
		"topic":          "why_failed",
		"expand_handles": []interface{}{"reason:console", "reason:s-expand:failed_requests"},
	})
	handles := toStringSlice(res["evidence_handles"])
	if len(handles) == 0 || handles[0] != "reason:failed_requests" {
		t.Fatalf("handles name the non-empty sections, got %v", handles)
	}
	data := asMap(res["data"])
	if len(data) != 2 || data["console"] == nil || data["failed_requests"] == nil {
		t.Fatalf("expand_handles must return exactly the named sections, got keys %v", mapKeys(data))
	}
	failed := toMapSlice(data["failed_requests"])
	if len(failed) != 2 || failed[0]["url"] != "/api/fail" {
		t.Fatalf("failed_requests lists the 500 and the 404, newest first, got %v", failed)
	}
}

func TestReasonWhatChangedSinceListsIssuesNewestFirst(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	loadFacts(t, engine, probeFailureScenario("s-changes"))
	res := runReason(t, &BrowserReasonTool{engine: engine}, map[string]interface{}{"session_id": "s-changes", "topic": "what_changed_since", "time_window_ms": 120000})
	changes := toMapSlice(res["changes"])
	if len(changes) < 4 {
		t.Fatalf("expected the window's issues, got %v", changes)
	}
	for i := 1; i < len(changes); i++ {
		if asInt64(changes[i]["at_ms"]) > asInt64(changes[i-1]["at_ms"]) {
			t.Fatalf("changes must be newest first, got %v", changes)
		}
	}
}

func TestReasonRequiresAKnownTopic(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	res, _ := (&BrowserReasonTool{engine: engine}).Execute(context.Background(), map[string]interface{}{"session_id": "s", "topic": "bogus"})
	m := res.(map[string]interface{})
	if m["success"] != false || !strings.Contains(fmt.Sprintf("%v", m["error"]), "network") {
		t.Fatalf("an unknown topic must fail and list the topics, got %v", m)
	}
}

func TestReasonNextBestActionFoldsRepeatedControls(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	now := time.Now()
	facts := []mangle.Fact{
		{Predicate: "interactive", Args: []interface{}{"s-nba", "save", "button", "Save", "click"}, Timestamp: now},
		{Predicate: "element_enabled", Args: []interface{}{"s-nba", "save", "true"}, Timestamp: now},
	}
	for i := 0; i < 40; i++ {
		ref := fmt.Sprintf("browse-%d", i)
		facts = append(facts,
			mangle.Fact{Predicate: "interactive", Args: []interface{}{"s-nba", ref, "button", "Browse nodes", "click"}, Timestamp: now},
			mangle.Fact{Predicate: "element_enabled", Args: []interface{}{"s-nba", ref, "true"}, Timestamp: now},
		)
	}
	if err := engine.AddFacts(context.Background(), facts); err != nil {
		t.Fatal(err)
	}
	res := runReason(t, &BrowserReasonTool{engine: engine}, map[string]interface{}{"session_id": "s-nba", "topic": "next_best_action"})
	t.Logf("next_best_action compact: ~%d tokens", responseTokens(t, res))
	actions := toMapSlice(res["actions"])
	browse := 0
	for _, a := range actions {
		if a["label"] == "Browse nodes" {
			browse++
			if asInt(a["count"]) != 40 {
				t.Fatalf("40 identical buttons fold into one action with count 40, got %v", a)
			}
		}
	}
	if browse != 1 {
		t.Fatalf("expected one Browse nodes action, got %d in %v", browse, actions)
	}
}

// fakeLogs is an injected backend log source.
type fakeLogs struct {
	logs  []docker.LogEntry
	err   error
	calls int32
}

func (f *fakeLogs) LogsBetween(context.Context, time.Time, time.Time) ([]docker.LogEntry, error) {
	atomic.AddInt32(&f.calls, 1)
	return f.logs, f.err
}
func (f *fakeLogs) LogWindow() time.Duration { return 30 * time.Second }

func TestReasonAttachesBackendLogLinesForTheFailedRequest(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	p := probeFailureScenario("s-backend")
	loadFacts(t, engine, p)
	failAt := p.at(6005)
	src := &fakeLogs{logs: []docker.LogEntry{
		{Container: "cross-thread-backend", Timestamp: failAt.Add(-3 * time.Millisecond), Level: "ERROR", Tag: "TRACEBACK", Message: "Traceback (most recent call last):\n  File \"app.py\"\nOperationalError: database unavailable password=hunter2"},
		{Container: "cross-thread-backend", Timestamp: failAt, Level: "INFO", Message: `172.18.0.1 - "GET /api/fail HTTP/1.1" 500 Internal Server Error`},
		{Container: "cross-thread-backend", Timestamp: failAt.Add(10 * time.Second), Level: "INFO", Message: `"GET /api/poll HTTP/1.1" 200`},
	}}
	tool := &BrowserReasonTool{engine: engine, backendLogs: src}

	res := runReason(t, tool, map[string]interface{}{"session_id": "s-backend", "topic": "why_failed"})
	t.Logf("why_failed with backend lines: ~%d tokens", responseTokens(t, res))
	lines := toStringSlice(asMap(res["root_cause"])["backend_logs"])
	if len(lines) != 2 {
		t.Fatalf("expected the access line and the traceback, got %v", lines)
	}
	if !strings.Contains(lines[0], `"GET /api/fail HTTP/1.1" 500`) {
		t.Fatalf("the line naming the path comes first, got %v", lines)
	}
	if !strings.Contains(lines[1], "OperationalError: database unavailable") || strings.Contains(lines[1], "hunter2") {
		t.Fatalf("a traceback shows its exception line, redacted, got %q", lines[1])
	}
}

func TestReasonBackendUnavailableIsOneQuietNote(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	loadFacts(t, engine, probeFailureScenario("s-nodocker"))
	src := &fakeLogs{err: errors.New("docker logs cross-thread-backend: exit status 1: failed to connect to the docker API at npipe:////./pipe/dockerDesktopLinuxEngine")}
	tool := &BrowserReasonTool{engine: engine, backendLogs: src}

	for i := 0; i < 3; i++ {
		res := runReason(t, tool, map[string]interface{}{"session_id": "s-nodocker", "topic": "why_failed"})
		if note := asMap(res["root_cause"])["backend_logs"]; note != "unavailable: docker daemon not reachable" {
			t.Fatalf("an unreachable Docker is one short note, got %v", note)
		}
	}
	if n := atomic.LoadInt32(&src.calls); n != 1 {
		t.Fatalf("an unreachable Docker is asked once per cooldown, got %d calls", n)
	}
}

// TestReasonDeadDockerCostsUnderASecond runs the real client against a port
// nothing listens on.
func TestReasonDeadDockerCostsUnderASecond(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	loadFacts(t, engine, probeFailureScenario("s-dead"))
	tool := &BrowserReasonTool{engine: engine, dockerClient: docker.NewClient([]string{"cross-thread-backend", "cross-thread-frontend"}, 30*time.Second, "tcp://127.0.0.1:1")}

	start := time.Now()
	res := runReason(t, tool, map[string]interface{}{"session_id": "s-dead", "topic": "why_failed"})
	elapsed := time.Since(start)
	note := fmt.Sprintf("%v", asMap(res["root_cause"])["backend_logs"])
	t.Logf("why_failed with a dead Docker took %s, backend_logs=%q", elapsed, note)
	if !strings.HasPrefix(note, "unavailable: ") {
		t.Fatalf("expected an unavailable note, got %q", note)
	}
	if elapsed > time.Second {
		t.Fatalf("a dead Docker cost %s, want < 1s", elapsed)
	}
}

func TestNormalizeRequestURL(t *testing.T) {
	cases := map[string]string{
		probeOrigin + "/api/poll?x=1":                               "/api/poll?x=",
		probeOrigin + "/api/items/42?b=2&a=1":                       "/api/items/{id}?a=&b=",
		probeOrigin + "/api/n/2f1c9a3e-9f0b-4b7e-8a51-1d2c3e4f5a6b": "/api/n/{id}",
		"https://api.example.com/v1/orgs/9f8e7d6c5b4a3f2e1d0c":      "api.example.com/v1/orgs/{id}",
		"ws://127.0.0.1:1/hmr":                                      "ws://127.0.0.1:1/hmr",
		probeOrigin + "/":                                           "/",
	}
	for raw, want := range cases {
		if got := normalizeRequestURL(raw, probeOrigin); got != want {
			t.Errorf("normalizeRequestURL(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestDetectStorm(t *testing.T) {
	series := func(n int, gap int64) []int64 {
		out := make([]int64, n)
		for i := range out {
			out[i] = 1_000_000 + int64(i)*gap
		}
		return out
	}
	if s := detectStorm(series(12, 400)); s == nil || s.intervalMs != 400 || !s.fixed {
		t.Fatalf("12 calls every 400 ms is a storm at 400 ms, got %+v", s)
	}
	if s := detectStorm(series(6, 400)); s == nil {
		t.Fatal("6 calls at a steady 400 ms is a loop")
	}
	if s := detectStorm(series(6, 5000)); s != nil {
		t.Fatalf("a 5 s poll is not a storm, got %+v", s)
	}
	burst := []int64{0, 50, 90, 400, 410, 1200, 1300, 2600, 2610, 2700}
	for i := range burst {
		burst[i] += 1_000_000
	}
	if s := detectStorm(burst); s == nil || s.peak30s != 10 {
		t.Fatalf("10 irregular calls in 30 s is a storm, got %+v", s)
	}
	if s := detectStorm(series(4, 100)); s != nil {
		t.Fatalf("4 calls is too few, got %+v", s)
	}
}

func TestConsoleGroupingFoldsNearIdenticalMessages(t *testing.T) {
	groups := groupConsole([]consoleRecord{
		{level: "warning", message: "Deprecated prop used", ts: 1},
		{level: "error", message: "Save failed with status 500", ts: 2},
		{level: "error", message: "Save failed with status 404", ts: 5},
		{level: "error", message: "Node 2f1c9a3e-9f0b-4b7e-8a51-1d2c3e4f5a6b missing", ts: 3},
		{level: "error", message: "Node 9d8c7b6a-1111-2222-3333-444455556666 missing", ts: 4},
		{level: "error", message: "Node 2f1c9a3e-9f0b-4b7e-8a51-1d2c3e4f5a6b missing", ts: 6},
	})
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d", len(groups))
	}
	if groups[0].message != "Node 2f1c9a3e-9f0b-4b7e-8a51-1d2c3e4f5a6b missing" || groups[0].count != 3 || groups[0].firstMs != 3 || groups[0].lastMs != 6 {
		t.Fatalf("the most repeated error leads with its first and last time, got %+v", groups[0])
	}
	if groups[1].count != 2 || groups[2].level != "warning" {
		t.Fatalf("errors come before warnings, got %+v %+v", groups[1], groups[2])
	}
}

func TestGetConsoleErrorsGroupsRepeats(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	now := time.Now()
	var facts []mangle.Fact
	for i := 0; i < 5; i++ {
		facts = append(facts, mangle.Fact{Predicate: "console_event", Args: []interface{}{"s-group", "error", fmt.Sprintf("Request %d failed with status 500", 100+i), int64(1000 + i)}, Timestamp: now})
	}
	facts = append(facts,
		mangle.Fact{Predicate: "console_event", Args: []interface{}{"s-group", "warning", "Deprecated prop used", int64(999)}, Timestamp: now},
		mangle.Fact{Predicate: "console_event", Args: []interface{}{"s-group", "error", "TypeError: x is undefined", int64(2000)}, Timestamp: now},
	)
	if err := engine.AddFacts(context.Background(), facts); err != nil {
		t.Fatal(err)
	}
	res, err := (&GetConsoleErrorsTool{engine: engine}).Execute(context.Background(), map[string]interface{}{"session_id": "s-group", "include_warnings": true})
	if err != nil {
		t.Fatal(err)
	}
	m := res.(map[string]interface{})
	groups := m["errors"].([]map[string]interface{})
	if m["error_count"] != 7 || len(groups) != 3 {
		t.Fatalf("7 events fold into 3 groups, got error_count=%v groups=%d", m["error_count"], len(groups))
	}
	if asInt(groups[0]["count"]) != 5 || asInt64(groups[0]["first_ms"]) != 1000 || asInt64(groups[0]["last_ms"]) != 1004 {
		t.Fatalf("the repeated error leads with count and first/last, got %v", groups[0])
	}
	if groups[2]["level"] != "warning" {
		t.Fatalf("warnings come after errors, got %v", groups[2])
	}
	if _, ok := groups[0]["session_id"]; ok {
		t.Fatal("a session-filtered call does not repeat session_id per row")
	}
}

func TestBodyDetailAndRedaction(t *testing.T) {
	cases := map[string]string{
		`{"error":"boom","detail":"database unavailable"}`:                "database unavailable",
		`{"detail":[{"loc":["body","email"],"msg":"field required"}]}`:    "body.email: field required",
		`{"error":{"message":"quota exceeded"}}`:                          "quota exceeded",
		`{"message":"Not Found"}`:                                         "Not Found",
		"upstream connect error or disconnect/reset before headers\nmore": "upstream connect error or disconnect/reset before headers",
		"<!doctype html><html><body>502 Bad Gateway</body></html>":        "",
	}
	for body, want := range cases {
		if got := bodyDetail(body); got != want {
			t.Errorf("bodyDetail(%q) = %q, want %q", body, got, want)
		}
	}
	red := redactSnippet(`{"user":"a","password":"hunter2","access_token":"abc.def","note":"Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0In0.c2lnbmF0dXJlMTIz"}`, 400)
	for _, secret := range []string{"hunter2", "abc.def", "eyJhbGci"} {
		if strings.Contains(red, secret) {
			t.Fatalf("redactSnippet leaked %q: %s", secret, red)
		}
	}
	if !strings.Contains(red, `"user":"a"`) {
		t.Fatalf("redaction must keep non-secret fields, got %s", red)
	}
}
