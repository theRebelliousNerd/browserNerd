package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// probeEval runs a script in the probe page.
func probeEval(t *testing.T, p *liveProbe, script string) {
	t.Helper()
	page, ok := p.manager.Page(p.session)
	if !ok {
		t.Fatalf("probe session %s has no page", p.session)
	}
	if _, err := page.Context(p.ctx).Eval(script); err != nil {
		t.Fatalf("probe eval %q: %v", script, err)
	}
}

// waitForProbeFacts polls the engine until pred holds or the deadline passes.
func waitForProbeFacts(t *testing.T, p *liveProbe, what string, deadline time.Duration, pred func() bool) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if pred() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", deadline, what)
}

func probeRequestCount(p *liveProbe, urlPart string) int {
	n := 0
	for _, f := range p.engine.FactsByPredicate("net_request") {
		if len(f.Args) >= 4 && f.Args[0] == p.session && strings.Contains(f.Args[3].(string), urlPart) {
			n++
		}
	}
	return n
}

func probeConsoleHas(p *liveProbe, text string) bool {
	for _, f := range p.engine.FactsByPredicate("console_event") {
		if len(f.Args) >= 3 && f.Args[0] == p.session {
			if msg, _ := f.Args[2].(string); strings.Contains(msg, text) {
				return true
			}
		}
	}
	return false
}

// driveProbeFailure lets the poll build a storm, fetches the 404 after load,
// and clicks "Trigger failure" so the 500, its console error and its toast
// all land in the event stream.
func driveProbeFailure(t *testing.T, p *liveProbe) {
	t.Helper()
	waitForProbeFacts(t, p, "10 polls", 15*time.Second, func() bool { return probeRequestCount(p, "/api/poll") >= 10 })
	probeEval(t, p, `() => { fetch('/api/missing'); }`)
	probeEval(t, p, `() => { document.getElementById('failbtn').click(); }`)
	waitForProbeFacts(t, p, "the save-failed console error", 10*time.Second, func() bool { return probeConsoleHas(p, "Save failed") })
	// Let the toast poller (500 ms) drain.
	time.Sleep(1200 * time.Millisecond)
}

func logReasonResult(t *testing.T, label string, result interface{}) {
	t.Helper()
	raw, _ := json.Marshal(result)
	t.Logf("%s: ~%d tokens\n%s", label, len(raw)/4, string(raw))
}

// reasonTokensBeforeDiagnosis is the baseline this scenario (failure
// triggered, then an interactive observe) cost at commit 9a2acab6b, measured
// 2026-09-24; the test logs the current cost beside it.
var reasonTokensBeforeDiagnosis = map[string]int{"why_failed": 2470, "health": 2469}

// TestLiveReasonProbe pins browser-reason's budgets on the probe page after
// "Trigger failure": why_failed compact <= 600 tokens naming the 500 and its
// console error, health compact <= 300, network summary <= 250 with the poll
// flagged as a storm. An interactive observe runs first, so the page's 400
// identical buttons are in the engine and must not leak into a diagnosis.
func TestLiveReasonProbe(t *testing.T) {
	p := startLiveProbe(t)
	driveProbeFailure(t, p)
	observe := &BrowserObserveTool{sessions: p.manager, engine: p.engine}
	if _, err := observe.Execute(p.ctx, map[string]interface{}{"session_id": p.session, "mode": "interactive"}); err != nil {
		t.Fatalf("observe: %v", err)
	}
	reason := &BrowserReasonTool{engine: p.engine}
	run := func(args map[string]interface{}) map[string]interface{} {
		args["session_id"] = p.session
		res, err := reason.Execute(p.ctx, args)
		if err != nil {
			t.Fatalf("browser-reason %v: %v", args, err)
		}
		return res.(map[string]interface{})
	}

	why := run(map[string]interface{}{"topic": "why_failed"})
	logReasonResult(t, "why_failed compact", why)
	whyTokens := responseTokens(t, why)
	t.Logf("why_failed compact: before ~%d tokens, after ~%d", reasonTokensBeforeDiagnosis["why_failed"], whyTokens)
	if whyTokens > 600 {
		t.Fatalf("why_failed compact costs ~%d tokens, budget 600", whyTokens)
	}
	verdict := fmt.Sprintf("%v", why["verdict"])
	if !strings.Contains(verdict, "/api/fail -> 500") || !strings.Contains(verdict, "console error") {
		t.Fatalf("verdict must name the 500 and the console error it caused, got %q", verdict)
	}
	if len(p.engine.FactsByPredicate("net_failure_body")) > 0 {
		if !strings.Contains(verdict, "database unavailable") {
			t.Fatalf("the failure body was captured, so the verdict must quote it, got %q", verdict)
		}
	} else {
		t.Log("net_failure_body is not captured by this build; the body branch is proven by TestReasonWhyFailedNamesTheCausalChain")
	}
	for _, noise := range []string{"Browse nodes", "action_candidates", "recommendations"} {
		raw, _ := json.Marshal(why)
		if strings.Contains(string(raw), noise) {
			t.Fatalf("a diagnosis must not carry %q", noise)
		}
	}
	if !strings.Contains(strings.Join(toStringSlice(why["issues"]), "\n"), "storm GET /api/poll?x=") {
		t.Fatalf("the poll storm must be an issue, got %v", why["issues"])
	}

	health := run(map[string]interface{}{"topic": "health"})
	logReasonResult(t, "health compact", health)
	healthTokens := responseTokens(t, health)
	t.Logf("health compact: before ~%d tokens, after ~%d", reasonTokensBeforeDiagnosis["health"], healthTokens)
	if healthTokens > 300 {
		t.Fatalf("health compact costs ~%d tokens, budget 300", healthTokens)
	}

	network := run(map[string]interface{}{"topic": "network", "view": "summary"})
	logReasonResult(t, "network summary", network)
	if n := responseTokens(t, network); n > 250 {
		t.Fatalf("network summary costs ~%d tokens, budget 250", n)
	}
	if !strings.Contains(strings.Join(toStringSlice(network["top"]), "\n"), "storm GET /api/poll?x=") {
		t.Fatalf("network summary must flag /api/poll as a storm, got %v", network["top"])
	}
	logReasonResult(t, "network compact", run(map[string]interface{}{"topic": "network"}))
	logReasonResult(t, "network expand /api/fail", run(map[string]interface{}{"topic": "network", "expand": "/api/fail"}))
}
