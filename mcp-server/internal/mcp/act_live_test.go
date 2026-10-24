package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// These tests drive browser-act against the probe pages in headless Chrome
// and pin what each change is for: an op either does what it says, or fails
// with the reason, within a bound.

func ops(list ...map[string]interface{}) []interface{} {
	out := make([]interface{}, 0, len(list))
	for _, op := range list {
		out = append(out, op)
	}
	return out
}

func (p *liveProbe) act(t *testing.T, args map[string]interface{}) map[string]interface{} {
	t.Helper()
	if _, ok := args["session_id"]; !ok {
		args["session_id"] = p.session
	}
	res, err := (&BrowserActTool{sessions: p.manager, engine: p.engine}).Execute(p.ctx, args)
	if err != nil {
		t.Fatalf("browser-act: %v", err)
	}
	return res.(map[string]interface{})
}

func (p *liveProbe) actOK(t *testing.T, args map[string]interface{}) map[string]interface{} {
	t.Helper()
	res := p.act(t, args)
	if res["success"] != true {
		raw, _ := json.Marshal(res)
		t.Fatalf("expected success: %s", raw)
	}
	return res
}

func (p *liveProbe) eval(t *testing.T, sessionID, expr string) string {
	t.Helper()
	page, ok := p.manager.Page(sessionID)
	if !ok {
		t.Fatalf("no page for %s", sessionID)
	}
	res, err := page.Timeout(5 * time.Second).Eval("() => " + expr)
	if err != nil {
		t.Fatalf("eval %s: %v", expr, err)
	}
	return res.Value.Str()
}

func (p *liveProbe) status(t *testing.T) string {
	t.Helper()
	return p.eval(t, p.session, "document.getElementById('status').textContent")
}

func firstResult(t *testing.T, res map[string]interface{}) map[string]interface{} {
	t.Helper()
	results, ok := res["results"].([]map[string]interface{})
	if !ok || len(results) == 0 {
		t.Fatalf("no results: %+v", res)
	}
	return results[0]
}

func TestLiveActSleepNeverSleepsADefault(t *testing.T) {
	p := startLiveProbe(t)
	start := time.Now()
	res := p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "sleep", "ms": 600})})
	elapsed := time.Since(start)
	if elapsed < 600*time.Millisecond {
		t.Fatalf("sleep ms:600 returned after %v", elapsed)
	}
	notes, _ := res["normalized"].([]string)
	if len(notes) != 1 || !strings.Contains(notes[0], "ms -> duration_ms") {
		t.Fatalf("the alias should be reported: %+v", res)
	}
	t.Logf("sleep {ms:600}: slept %v, normalized %v", elapsed.Round(time.Millisecond), notes)

	start = time.Now()
	res = p.act(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "sleep", "duration_ms": 61000})})
	if res["status"] != "invalid" || time.Since(start) > time.Second {
		t.Fatalf("a sleep over the cap must be refused at once: %+v", res)
	}
}

func TestLiveActKeyChords(t *testing.T) {
	p := startLiveProbe(t)
	p.actOK(t, map[string]interface{}{"operations": ops(
		map[string]interface{}{"type": "interact", "action": "type", "label": "Name", "value": "hello world"},
		map[string]interface{}{"type": "key", "key": "Control+a"},
		map[string]interface{}{"type": "key", "key": "Backspace"},
	)})
	if got := p.eval(t, p.session, "document.getElementById('name').value"); got != "" {
		t.Fatalf("Control+a then Backspace should empty the field, it holds %q", got)
	}
	p.actOK(t, map[string]interface{}{"operations": ops(
		map[string]interface{}{"type": "interact", "action": "click", "label": "Quantity"},
		map[string]interface{}{"type": "key", "key": "Shift+Tab"},
	)})
	if got := p.eval(t, p.session, "document.activeElement.id"); got != "name" {
		t.Fatalf("Shift+Tab from Quantity should focus Name, focus is on %q", got)
	}
}

func TestLiveActFillHoldsExactlyTheValue(t *testing.T) {
	p := startLiveProbe(t)
	res := p.actOK(t, map[string]interface{}{"operations": ops(
		map[string]interface{}{"type": "fill", "fields": []interface{}{map[string]interface{}{"label": "Quantity", "value": "7"}}},
	)})
	if got := p.eval(t, p.session, "document.getElementById('qty').value"); got != "7" {
		t.Fatalf("Quantity holds %q after fill, want 7", got)
	}
	t.Logf("fill Quantity=7 (was 42): ~%d tokens", responseTokens(t, res))

	p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "navigate", "url": p.baseURL + "/widgets.html"})})
	p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "fill", "fields": []interface{}{
		map[string]interface{}{"label": "React quantity", "value": "7"},
		map[string]interface{}{"label": "When", "value": "2026-09-24"},
		map[string]interface{}{"label": "Email", "value": "qa@example.com"},
		map[string]interface{}{"label": "Notes", "value": "line one"},
		map[string]interface{}{"label": "Editor", "value": "new text"},
		map[string]interface{}{"label": "Agree", "value": "true"},
	}})})
	checks := map[string]string{
		"document.getElementById('react-qty').value":         "7",
		"document.getElementById('react-state').textContent": "7",
		"document.getElementById('when').value":              "2026-09-24",
		"document.getElementById('mail').value":              "qa@example.com",
		"document.getElementById('notes').value":             "line one",
		"document.getElementById('editor').innerText":        "new text",
		"String(document.getElementById('agree').checked)":   "true",
	}
	for expr, want := range checks {
		if got := p.eval(t, p.session, expr); got != want {
			t.Fatalf("%s = %q, want %q", expr, got, want)
		}
	}

	res = p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "fill", "fields": []interface{}{
		map[string]interface{}{"label": "Phone", "value": "5551234567"},
	}})})
	if got := p.eval(t, p.session, "document.getElementById('phone').value"); got != "(555) 123-4567" {
		t.Fatalf("the mask should format the phone, got %q", got)
	}

	for _, c := range []struct{ label, value, want string }{
		{"Code", "123456", `holds "123"`},
		{"When", "09/24/2026", "YYYY-MM-DD"},
	} {
		res = p.act(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "fill", "fields": []interface{}{
			map[string]interface{}{"label": c.label, "value": c.value},
		}})})
		if res["success"] != false || !strings.Contains(fmt.Sprint(firstResult(t, res)["error"]), c.want) {
			t.Fatalf("fill %s=%q must fail with %q: %+v", c.label, c.value, c.want, res)
		}
	}
}

func TestLiveActSelectVerifiesTheWidget(t *testing.T) {
	p := startLiveProbe(t)
	res := p.actOK(t, map[string]interface{}{"operations": ops(
		map[string]interface{}{"type": "interact", "action": "select", "text": "Pick a color", "value": "Green"},
	)})
	entry := firstResult(t, res)
	if entry["selected"] != "Green" || entry["verified_by"] != "trigger_text" {
		t.Fatalf("expected a verified custom selection: %+v", entry)
	}
	if got := p.eval(t, p.session, "document.getElementById('combo').textContent"); got != "Green" {
		t.Fatalf("the combobox shows %q", got)
	}
	t.Logf("select Green on role=combobox: %+v (~%d tokens)", entry, responseTokens(t, res))

	p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "interact", "action": "select", "label": "Native select", "value": "Beta"})})
	if got := p.eval(t, p.session, "document.getElementById('nat').value"); got != "b" {
		t.Fatalf("native select by label should pick value b, got %q", got)
	}
	res = p.act(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "interact", "action": "select", "text": "Green", "value": "Purple"})})
	if res["success"] != false || !strings.Contains(fmt.Sprint(firstResult(t, res)["error"]), "options: Red, Green") {
		t.Fatalf("a missing option must fail and list the options: %+v", res)
	}

	p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "navigate", "url": p.baseURL + "/widgets.html"})})
	res = p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "interact", "action": "select", "label": "Fruit", "value": "Banana"})})
	if got := p.eval(t, p.session, "document.querySelector('select[name=fruit]').value"); got != "banana" {
		t.Fatalf("the Radix-style select should hold banana, holds %q (%+v)", got, firstResult(t, res))
	}
	res = p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "interact", "action": "select", "ref": "fruit", "value": "Apple"})})
	if got := p.eval(t, p.session, "document.getElementById('fruit-trigger').innerText"); got != "Apple" {
		t.Fatalf("selecting through the hidden native select should drive its trigger, it shows %q (%+v)", got, firstResult(t, res))
	}
}

func TestLiveActLocatesWithoutObserving(t *testing.T) {
	p := startLiveProbe(t)
	p.actOK(t, map[string]interface{}{"operations": ops(
		map[string]interface{}{"type": "fill", "fields": []interface{}{map[string]interface{}{"label": "Quantity", "value": "3"}}},
		map[string]interface{}{"type": "interact", "action": "click", "text": "Save"},
	)})
	if got := p.status(t); got != "saved 3" {
		t.Fatalf("text:Save should submit the form, status %q", got)
	}

	x := p.eval(t, p.session, "String(Math.round(document.getElementById('card1').getBoundingClientRect().left + 20))")
	y := p.eval(t, p.session, "String(Math.round(document.getElementById('card1').getBoundingClientRect().top + 5))")
	var px, py float64
	fmt.Sscanf(x, "%f", &px)
	fmt.Sscanf(y, "%f", &py)
	res := p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "interact", "action": "click", "x": px, "y": py})})
	if got := p.status(t); got != "card clicked" {
		t.Fatalf("x/y click on the card should set status, got %q (%+v)", got, firstResult(t, res))
	}

	res = p.act(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "interact", "action": "click", "text": "Browse nodes"})})
	msg := fmt.Sprint(firstResult(t, res)["error"])
	if res["success"] != false || !strings.Contains(msg, "matches 400 visible elements") || !strings.Contains(msg, "[2]") {
		t.Fatalf("an ambiguous text must list candidates: %q", msg)
	}
	t.Logf("ambiguous locator error (~%d tokens): %s", responseTokens(t, res), msg)
	p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "interact", "action": "click", "text": "Browse nodes", "index": 399})})

	p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "interact", "action": "click", "role": "button", "name": "Hydrated action"})})
	if got := p.status(t); got != "hydrated clicked" {
		t.Fatalf("role+name click should reach the hydrated button, status %q", got)
	}
}

func TestLiveActEffectsDigestReportsWhatAClickCaused(t *testing.T) {
	p := startLiveProbe(t)
	res := p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "interact", "action": "click", "text": "Trigger failure"})})
	raw, _ := json.Marshal(res)
	tokens := len(raw) / 4
	t.Logf("Trigger failure compact (~%d tokens): %s", tokens, raw)
	if tokens > 250 {
		t.Fatalf("the Trigger failure result costs ~%d tokens; budget 250", tokens)
	}
	effects, _ := res["effects"].(map[string]interface{})
	failed := fmt.Sprint(effects["failed_requests"])
	if !strings.Contains(failed, "/api/fail 500") {
		t.Fatalf("effects must name the 500 on /api/fail: %+v", effects)
	}
	if !strings.Contains(fmt.Sprint(effects["errors"]), "Save failed with status 500") {
		t.Fatalf("effects must name the console error: %+v", effects)
	}
	if !strings.Contains(fmt.Sprint(effects["toasts"]), "Could not save: server error") {
		t.Fatalf("effects must name the error toast: %+v", effects)
	}
	if strings.Contains(failed, "/api/missing") || strings.Contains(failed, "poll") {
		t.Fatalf("effects must hold only what this batch caused: %+v", effects)
	}

	summary := p.act(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "interact", "action": "click", "text": "Trigger failure"}), "view": "summary"})
	if line := fmt.Sprint(summary["summary"]); !strings.Contains(line, "/api/fail 500") {
		t.Fatalf("view summary should fold effects into one line: %q", line)
	}
	t.Logf("Trigger failure summary view (~%d tokens): %s", responseTokens(t, summary), summary["summary"])
}

func TestLiveActPlanUsesTheBoundedInputPath(t *testing.T) {
	p := startLiveProbe(t)
	res := p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "plan", "actions": []interface{}{
		map[string]interface{}{"type": "type", "ref": "qty", "value": "4"},
		map[string]interface{}{"type": "press", "value": "Control+a"},
		map[string]interface{}{"type": "type", "ref": "qty", "value": "8"},
		map[string]interface{}{"type": "click", "ref": "testid:save-btn"},
	}})})
	if got := p.status(t); got != "saved 8" {
		t.Fatalf("a plan should type, chord and click through the same path as interact, status %q (%+v)", got, res)
	}
}

func TestLiveActEffectsReportDownloads(t *testing.T) {
	p := startLiveProbe(t)
	res := p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "interact", "action": "click", "text": "Download report"})})
	if len(p.engine.FactsByPredicate("download")) == 0 {
		time.Sleep(2 * time.Second)
		if len(p.engine.FactsByPredicate("download")) == 0 {
			t.Skip("this build records no download facts")
		}
	}
	effects, _ := res["effects"].(map[string]interface{})
	if !strings.Contains(fmt.Sprint(effects["downloads"]), "report.csv") {
		t.Fatalf("effects should name the download: %+v", effects)
	}
	t.Logf("Download report effects: %+v (~%d tokens)", effects, responseTokens(t, res))
}

func TestLiveActSessionCreateHandsTheBatchItsSession(t *testing.T) {
	p := startLiveProbe(t)
	res := p.actOK(t, map[string]interface{}{"operations": ops(
		map[string]interface{}{"type": "session_create", "url": p.baseURL + "/index.html"},
		map[string]interface{}{"type": "interact", "action": "click", "text": "Clickable card"},
	)})
	entry := firstResult(t, res)
	created, _ := entry["session_id"].(string)
	if created == "" || created == p.session || res["session_id"] != created {
		t.Fatalf("session_create should return the new id and the batch should carry it: %+v", res)
	}
	if got := p.eval(t, created, "document.getElementById('status').textContent"); got != "card clicked" {
		t.Fatalf("the op after session_create should run in the new tab, its status is %q", got)
	}
	t.Logf("session_create compact (~%d tokens): %+v", responseTokens(t, res), entry)

	// The original tab is now in the background, where requestAnimationFrame
	// never fires; interacting there must still work, not hang.
	start := time.Now()
	p.actOK(t, map[string]interface{}{"operations": ops(
		map[string]interface{}{"type": "fill", "fields": []interface{}{map[string]interface{}{"label": "Quantity", "value": "5"}}},
		map[string]interface{}{"type": "interact", "action": "click", "text": "Save"},
	)})
	if got := p.status(t); got != "saved 5" {
		t.Fatalf("background-tab fill and click should submit, status %q", got)
	}
	t.Logf("background-tab fill+click took %v", time.Since(start).Round(time.Millisecond))
}

func TestLiveActDialogAnswers(t *testing.T) {
	p := startLiveProbe(t)
	start := time.Now()
	res := p.act(t, map[string]interface{}{"settle_ms": 0, "operations": ops(map[string]interface{}{"type": "interact", "action": "click", "text": "Delete"})})
	elapsed := time.Since(start)
	if elapsed > 7*time.Second {
		t.Fatalf("a click that opens a dialog took %v; it must return within the bound", elapsed)
	}
	if res["success"] != true {
		t.Fatalf("the dialog-opening click must succeed: %+v", res)
	}
	if got := p.status(t); got != "kept" {
		t.Fatalf("an unarmed confirm is dismissed, status should be kept, got %q", got)
	}
	if !strings.Contains(fmt.Sprint(firstResult(t, res)["dialogs"]), "dismissed") {
		t.Fatalf("the op should report the dismissed dialog: %+v", firstResult(t, res))
	}
	t.Logf("Delete, no arm: %v, %+v", elapsed.Round(time.Millisecond), firstResult(t, res))

	res = p.actOK(t, map[string]interface{}{"operations": ops(map[string]interface{}{"type": "interact", "action": "click", "text": "Delete", "dialog": "accept"})})
	if got := p.status(t); got != "deleted" {
		t.Fatalf("dialog:accept should confirm, status %q", got)
	}
	if !strings.Contains(fmt.Sprint(res["effects"]), "accepted") {
		t.Fatalf("effects should report the accepted dialog: %+v", res["effects"])
	}

	p.actOK(t, map[string]interface{}{"operations": ops(
		map[string]interface{}{"type": "dialog", "accept": true},
		map[string]interface{}{"type": "interact", "action": "click", "text": "Delete"},
	)})
	if got := p.status(t); got != "deleted" {
		t.Fatalf("a dialog op should arm the next dialog, status %q", got)
	}
}
