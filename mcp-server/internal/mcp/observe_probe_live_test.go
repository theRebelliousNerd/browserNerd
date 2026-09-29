package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/security"
)

// Token costs measured on the probe page at 9a2acab6b, before observation was grouped
// and made progressive. The MCP composite also carried ~1,800 tokens of spec excerpts
// (include_specs defaulted to true), which the harness engine has no specs for.
const (
	baselineCompositeTokens  = 1693
	baselineScreenshotTokens = 8236
	baselineSessionsTokens   = 134
)

// reloadProbe loads the probe page again with the event stream attached, so its
// load-time 404 and console output become facts. The harness opens the page before
// the stream is listening.
func reloadProbe(t *testing.T, p *liveProbe) {
	t.Helper()
	page, ok := p.manager.Page(p.session)
	if !ok {
		t.Fatal("probe session has no page")
	}
	if err := page.Navigate(p.baseURL + "/index.html"); err != nil {
		t.Fatalf("reload probe: %v", err)
	}
	if err := page.WaitLoad(); err != nil {
		t.Fatalf("wait for probe load: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)
}

func observeProbe(t *testing.T, p *liveProbe, args map[string]interface{}) map[string]interface{} {
	t.Helper()
	policy, err := security.NewPathPolicy(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	observe := &BrowserObserveTool{sessions: p.manager, engine: p.engine, pathPolicy: policy}
	res, err := observe.Execute(p.ctx, args)
	if err != nil {
		t.Fatalf("observe %v: %v", args, err)
	}
	out, ok := res.(map[string]interface{})
	if !ok {
		t.Fatalf("observe returned %T", res)
	}
	if success, _ := out["success"].(bool); !success {
		t.Fatalf("observe %v failed: %v", args, out)
	}
	return out
}

// probeStatus reads the probe's role=status line, which each probe control rewrites.
func probeStatus(t *testing.T, p *liveProbe) string {
	t.Helper()
	page, _ := p.manager.Page(p.session)
	res, err := page.Eval(`() => document.getElementById('status').textContent`)
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	return res.Value.Str()
}

func rowsByLabel(t *testing.T, interactive map[string]interface{}) map[string][]map[string]interface{} {
	t.Helper()
	out := map[string][]map[string]interface{}{}
	for _, raw := range toAnySlice(interactive["elements"]) {
		row, ok := raw.(map[string]interface{})
		if !ok {
			t.Fatalf("element row is %T", raw)
		}
		label := getStringFromMap(row, "label")
		out[label] = append(out[label], row)
	}
	return out
}

func mustJSON(t *testing.T, v interface{}) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// The default observation of the probe page fits the budget and still carries what
// the next decision needs: the form's Save button, the custom combobox, the clickable
// card, the 400 identical buttons as one counted row, the dead control, the status
// text, the health digest, and recommendations led by Save.
func TestLiveProbeObserveCompositeBudget(t *testing.T) {
	p := startLiveProbe(t)
	reloadProbe(t, p)

	out := observeProbe(t, p, map[string]interface{}{"session_id": p.session})
	tokens := responseTokens(t, out)
	t.Logf("composite compact: before ~%d tokens, after ~%d tokens", baselineCompositeTokens, tokens)
	if tokens > 900 {
		t.Fatalf("composite compact costs ~%d tokens, budget 900: %s", tokens, mustJSON(t, out))
	}

	data := asMap(out["data"])
	interactive := asMap(data["interactive"])
	summary := asMap(interactive["summary"])
	rows := rowsByLabel(t, interactive)

	save := rows["Save"]
	if len(save) != 1 || save[0]["ref"] != "testid:save-btn" || save[0]["type"] != "button" {
		t.Fatalf("Save button row wrong or missing: %v", save)
	}
	combo := rows["Pick a color"]
	if len(combo) != 1 || combo[0]["type"] != "combobox" || combo[0]["action"] != "click" {
		t.Fatalf("custom combobox should be one combobox row with action click: %v", combo)
	}
	card := rows["Clickable card (div onclick, no role)"]
	if len(card) != 1 || card[0]["type"] != "clickable" || card[0]["ref"] != "card1" {
		t.Fatalf("clickable card missing: %v", card)
	}
	browse := rows["Browse nodes"]
	if len(browse) != 1 || asInt(browse[0]["count"]) != 400 {
		t.Fatalf("400 Browse nodes buttons should be one row with count 400: %v", browse)
	}
	if refs := getStringFromMap(browse[0], "refs"); refs != "button.browse_1..button.browse_399" {
		t.Fatalf("Browse nodes refs = %q", refs)
	}

	// The total is every control found, not a planning cap: the sum of every row's count.
	counted := 0
	for _, raw := range toAnySlice(interactive["elements"]) {
		counted += maxInt(asInt(asMap(raw)["count"]), 1)
	}
	if total := asInt(summary["total"]); total != counted || total < 410 {
		t.Fatalf("summary.total %d, rows account for %d", total, counted)
	}
	if asInt(summary["groups"]) != len(toAnySlice(interactive["elements"])) {
		t.Fatalf("groups %v does not match %d rows", summary["groups"], len(toAnySlice(interactive["elements"])))
	}

	dead := asMap(interactive["dead_controls"])
	if asInt(dead["count"]) != 1 || !strings.Contains(mustJSON(t, dead["labels"]), "Dead action") {
		t.Fatalf("dead controls = %v, want only Dead action", dead)
	}
	if deadRow := rows["Dead action"]; len(deadRow) != 1 || deadRow[0]["dead"] != true {
		t.Fatalf("Dead action row not flagged: %v", deadRow)
	}
	if hydrated := rows["Hydrated action"]; len(hydrated) != 1 || hydrated[0]["dead"] != nil {
		t.Fatalf("Hydrated action wrongly flagged: %v", hydrated)
	}

	recs := toMapSlice(data["recommendations"])
	if len(recs) == 0 {
		t.Fatal("no recommendations")
	}
	for i, rec := range recs {
		if _, blob := rec["candidate"]; blob {
			t.Fatalf("recommendation %d still carries the candidate blob: %v", i, rec)
		}
		if strings.Contains(getStringFromMap(rec, "reason"), "Browse nodes") ||
			strings.Contains(getStringFromMap(rec, "reason"), "Dead action") {
			t.Fatalf("recommendation %d suggests a repeated or dead control: %v", i, rec)
		}
	}
	if first := mustJSON(t, recs[0]); !strings.Contains(first, "testid:save-btn") {
		t.Fatalf("first recommendation should click Save: %s", first)
	}
	if next := mustJSON(t, out["next_step"]); !strings.Contains(next, "testid:save-btn") {
		t.Fatalf("next_step should click Save: %s", next)
	}

	content := mustJSON(t, data["content"])
	if !strings.Contains(content, "status: ready") || !strings.Contains(content, "Probe Dashboard") {
		t.Fatalf("content digest missing heading or status text: %s", content)
	}
	if len(content) > contentDigestChars+120 {
		t.Fatalf("content digest is %d bytes", len(content))
	}
	health := asMap(data["health"])
	if asInt(health["failed_requests"]) < 1 || !strings.Contains(mustJSON(t, health), "404 /api/missing") {
		t.Fatalf("health digest missing the probe's 404: %v", data["health"])
	}
}

// Spec excerpts cost tokens on every call, so observe attaches them only when asked.
func TestLiveProbeObserveSpecsOnlyOnRequest(t *testing.T) {
	p := startLiveProbe(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "probe.md"), []byte("# Probe Dashboard\n\nThe probe dashboard shows revenue."), 0o600); err != nil {
		t.Fatal(err)
	}
	enabled := true
	observe := &BrowserObserveTool{sessions: p.manager, engine: p.engine, specsCfg: config.SpecsConfig{
		Enabled:         &enabled,
		Sources:         []config.SpecSourceConfig{{Name: "probe", Roots: []string{root}}},
		MaxFiles:        10,
		MaxFileBytes:    1 << 20,
		MaxResults:      4,
		MaxExcerptBytes: 300,
	}}
	run := func(args map[string]interface{}) map[string]interface{} {
		res, err := observe.Execute(p.ctx, args)
		if err != nil {
			t.Fatal(err)
		}
		return asMap(asMap(res)["data"])
	}
	if data := run(map[string]interface{}{"session_id": p.session, "spec_terms": []interface{}{"probe"}}); data["spec_context"] != nil {
		t.Fatalf("spec context attached by default: %v", data["spec_context"])
	}
	if data := run(map[string]interface{}{"session_id": p.session, "include_specs": true, "spec_terms": []interface{}{"probe"}}); data["spec_context"] == nil {
		t.Fatal("include_specs true attached no spec context")
	}
}

// A clickable div, a custom combobox and its options are reachable: observation hands
// out refs the browser-act click path resolves.
func TestLiveProbeNonSemanticControlsAreClickable(t *testing.T) {
	p := startLiveProbe(t)
	act := &BrowserActTool{sessions: p.manager, engine: p.engine}
	click := func(ref string) {
		t.Helper()
		res, err := act.Execute(p.ctx, map[string]interface{}{
			"session_id": p.session,
			"operations": []interface{}{map[string]interface{}{"type": "interact", "action": "click", "ref": ref}},
		})
		if err != nil {
			t.Fatalf("click %s: %v", ref, err)
		}
		if success, _ := asMap(res)["success"].(bool); !success {
			t.Fatalf("click %s failed: %v", ref, res)
		}
	}

	observeProbe(t, p, map[string]interface{}{"session_id": p.session, "mode": "interactive"})
	click("card1")
	if got := probeStatus(t, p); got != "card clicked" {
		t.Fatalf("clicking the card ref left status %q", got)
	}

	click("combo")
	out := observeProbe(t, p, map[string]interface{}{"session_id": p.session, "mode": "interactive", "filter": "selects"})
	rows := rowsByLabel(t, asMap(asMap(out["data"])["interactive"]))
	red := rows["Red"]
	if len(red) != 1 || red[0]["type"] != "option" || red[0]["action"] != "click" {
		t.Fatalf("an open combobox's options should be option rows: %v", rows)
	}
	click(getStringFromMap(red[0], "ref"))
	page, _ := p.manager.Page(p.session)
	res, err := page.Eval(`() => document.getElementById('combo').textContent`)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Value.Str(); got != "Red" {
		t.Fatalf("clicking the Red option ref left the combobox reading %q", got)
	}
}

// mode text reads the page within its budget: the revenue figure, the live status
// line, and one row standing for 60 near-identical paragraphs. text_query narrows to
// the matching block, and max_chars truncates honestly.
func TestLiveProbeObserveTextBudget(t *testing.T) {
	p := startLiveProbe(t)

	out := observeProbe(t, p, map[string]interface{}{"session_id": p.session, "mode": "text"})
	tokens := responseTokens(t, out)
	t.Logf("text: before (no text mode; a screenshot cost ~%d tokens), after ~%d tokens", baselineScreenshotTokens, tokens)
	if tokens > 700 {
		t.Fatalf("text mode costs ~%d tokens, budget 700: %s", tokens, mustJSON(t, out))
	}
	body := mustJSON(t, out["data"])
	for _, want := range []string{"$12,345", "status: ready", "# Probe Dashboard", "[+59 similar]"} {
		if !strings.Contains(body, want) {
			t.Fatalf("text mode missing %q: %s", want, body)
		}
	}

	query := observeProbe(t, p, map[string]interface{}{"session_id": p.session, "mode": "text", "text_query": "revenue"})
	blocks := toAnySlice(asMap(query["data"])["blocks"])
	if len(blocks) != 1 || !strings.Contains(fmt.Sprintf("%v", blocks[0]), "$12,345") {
		t.Fatalf("text_query revenue returned %v", blocks)
	}

	small := observeProbe(t, p, map[string]interface{}{"session_id": p.session, "mode": "text", "max_chars": 60})
	if truncated, _ := asMap(small["data"])["truncated"].(bool); !truncated {
		t.Fatalf("max_chars 60 should truncate: %v", small["data"])
	}
	if chars := asInt(asMap(small["data"])["chars"]); chars > 60 {
		t.Fatalf("max_chars 60 returned %d chars", chars)
	}
}

// A default screenshot returns a file and its size, nothing else. annotate numbers only
// what is in view, capped, with the repeated buttons grouped. ref captures one element.
// A tab in the background is brought forward and captured instead of hanging.
func TestLiveProbeObserveScreenshotBudget(t *testing.T) {
	p := startLiveProbe(t)

	plain := observeProbe(t, p, map[string]interface{}{"session_id": p.session, "mode": "screenshot"})
	tokens := responseTokens(t, plain)
	t.Logf("screenshot default: before ~%d tokens, after ~%d tokens", baselineScreenshotTokens, tokens)
	if tokens > 150 {
		t.Fatalf("default screenshot costs ~%d tokens, budget 150: %s", tokens, mustJSON(t, plain))
	}
	data := asMap(plain["data"])
	if _, listed := data["elements"]; listed {
		t.Fatalf("default screenshot listed elements: %v", data)
	}
	if asInt(data["width"]) <= 0 || asInt(data["height"]) <= 0 || asInt(data["size_bytes"]) <= 0 {
		t.Fatalf("screenshot dimensions missing: %v", data)
	}
	if _, err := os.Stat(getStringFromMap(data, "file_path")); err != nil {
		t.Fatalf("screenshot file not written: %v", err)
	}

	annotated := observeProbe(t, p, map[string]interface{}{"session_id": p.session, "mode": "screenshot", "annotate": true})
	tokens = responseTokens(t, annotated)
	t.Logf("screenshot annotate: before ~%d tokens (every element, max_items ignored), after ~%d tokens", baselineScreenshotTokens, tokens)
	if tokens > 1200 {
		t.Fatalf("annotated screenshot costs ~%d tokens, budget 1200: %s", tokens, mustJSON(t, annotated))
	}
	adata := asMap(annotated["data"])
	if n := asInt(adata["annotated"]); n == 0 || n > defaultAnnotateItems {
		t.Fatalf("annotated %d controls, want 1..%d", n, defaultAnnotateItems)
	}
	rows := rowsByLabel(t, adata)
	if save := rows["Save"]; len(save) != 1 {
		t.Fatalf("annotation lost the unique Save button: %v", rows)
	}
	if browse := rows["Browse nodes"]; len(browse) != 1 || asInt(browse[0]["count"]) < 2 {
		t.Fatalf("annotated Browse nodes should be one grouped row: %v", browse)
	}

	capped := observeProbe(t, p, map[string]interface{}{"session_id": p.session, "mode": "screenshot", "annotate": true, "max_items": 5})
	if n := asInt(asMap(capped["data"])["annotated"]); n != 5 {
		t.Fatalf("max_items 5 annotated %d", n)
	}

	element := observeProbe(t, p, map[string]interface{}{"session_id": p.session, "mode": "screenshot", "ref": "testid:save-btn"})
	edata := asMap(element["data"])
	if w := asInt(edata["width"]); w <= 0 || w >= asInt(data["width"]) {
		t.Fatalf("element screenshot width %d, page width %v", w, data["width"])
	}

	// Open a second tab so the probe tab is in the background, then capture the probe.
	second, err := p.manager.CreateSession(p.ctx, p.baseURL+"/second.html")
	if err != nil {
		t.Fatalf("second tab: %v", err)
	}
	if err := p.manager.FocusSession(second.ID); err != nil {
		t.Fatalf("focus second tab: %v", err)
	}
	started := time.Now()
	observeProbe(t, p, map[string]interface{}{"session_id": p.session, "mode": "screenshot"})
	if elapsed := time.Since(started); elapsed > screenshotCaptureTimeout+5*time.Second {
		t.Fatalf("background tab capture took %s", elapsed)
	}
}

// Sessions mode lists the one live tab compactly.
func TestLiveProbeObserveSessionsBudget(t *testing.T) {
	p := startLiveProbe(t)
	out := observeProbe(t, p, map[string]interface{}{"mode": "sessions"})
	tokens := responseTokens(t, out)
	t.Logf("sessions (1 active): before ~%d tokens, after ~%d tokens", baselineSessionsTokens, tokens)
	if tokens > 150 {
		t.Fatalf("sessions costs ~%d tokens, budget 150: %s", tokens, mustJSON(t, out))
	}
	data := asMap(out["data"])
	if asInt(data["active_count"]) != 1 || asInt(data["detached_count"]) != 0 {
		t.Fatalf("counts wrong: %v", data)
	}
	rows := toMapSlice(data["sessions"])
	if len(rows) != 1 || rows[0]["id"] != p.session {
		t.Fatalf("sessions rows = %v", rows)
	}
	for _, field := range []string{"browser_id", "target_id", "created_at", "last_active"} {
		if _, ok := rows[0][field]; ok {
			t.Fatalf("compact session row carries %s: %v", field, rows[0])
		}
	}
}

func TestObserveFilterHonouredAcrossModes(t *testing.T) {
	p := startLiveProbe(t)

	t.Run("composite mode honours filter buttons", func(t *testing.T) {
		out := observeProbe(t, p, map[string]interface{}{
			"session_id": p.session,
			"mode":       "composite",
			"view":       "full",
			"filter":     "buttons",
		})
		data := asMap(out["data"])
		summary := fmt.Sprintf("%v", out["summary"])

		// Summary must not mention links when filtered to buttons
		if strings.Contains(summary, "link(s)") {
			t.Errorf("summary should not mention links when filter=buttons, got: %q", summary)
		}

		// Nav data must not be returned when filter=buttons
		if nav, ok := data["nav"].(map[string]interface{}); ok && len(nav) > 0 {
			if counts, ok := nav["counts"].(map[string]interface{}); ok && asInt(counts["total"]) > 0 {
				t.Errorf("data.nav should not return links when filter=buttons, got: %v", nav)
			}
		}

		// All interactive elements must be button-like
		inter := asMap(data["interactive"])
		elems := toAnySlice(inter["elements"])
		if len(elems) == 0 {
			t.Fatalf("expected buttons, got 0 elements")
		}
		for _, el := range elems {
			elm := asMap(el)
			typ := fmt.Sprintf("%v", elm["type"])
			if typ != "button" && typ != "clickable" && typ != "tab" && typ != "menuitem" && typ != "option" && typ != "row" {
				t.Errorf("unexpected non-button type %q in filter=buttons: %v", typ, elm)
			}
		}

		// Recommendations and action candidates must not suggest link navigations or input typing
		for _, recRaw := range toAnySlice(data["recommendations"]) {
			rec := asMap(recRaw)
			reason := strings.ToLower(fmt.Sprintf("%v", rec["reason"]))
			if strings.Contains(reason, "navigate") || strings.Contains(reason, "home") || strings.Contains(reason, "second") {
				t.Errorf("recommendation should not suggest link navigation when filter=buttons: %v", rec)
			}
		}
	})

	t.Run("composite mode honours filter inputs without dead button leak", func(t *testing.T) {
		out := observeProbe(t, p, map[string]interface{}{
			"session_id": p.session,
			"mode":       "composite",
			"view":       "full",
			"filter":     "inputs",
		})
		data := asMap(out["data"])
		summary := fmt.Sprintf("%v", out["summary"])

		// Summary must not mention links
		if strings.Contains(summary, "link(s)") {
			t.Errorf("summary should not mention links when filter=inputs, got: %q", summary)
		}

		// The probe page has 1 dead button (dead-btn) and 0 dead inputs.
		// dead_controls must not leak the dead button when filtered to inputs.
		inter := asMap(data["interactive"])
		if dead, ok := inter["dead_controls"].(map[string]interface{}); ok && asInt(dead["count"]) > 0 {
			t.Errorf("filter=inputs must not report dead button in dead_controls, got: %v", dead)
		}
		if strings.Contains(summary, "dead control(s)") {
			t.Errorf("summary should not report dead controls when no inputs are dead, got: %q", summary)
		}

		// All elements must be input-like
		elems := toAnySlice(inter["elements"])
		if len(elems) == 0 {
			t.Fatalf("expected inputs, got 0 elements")
		}
		for _, el := range elems {
			elm := asMap(el)
			typ := fmt.Sprintf("%v", elm["type"])
			if typ != "input" && typ != "checkbox" && typ != "radio" && typ != "combobox" {
				t.Errorf("unexpected non-input type %q in filter=inputs: %v", typ, elm)
			}
		}
	})

	t.Run("interactive mode honours filter links", func(t *testing.T) {
		out := observeProbe(t, p, map[string]interface{}{
			"session_id": p.session,
			"mode":       "interactive",
			"view":       "compact",
			"filter":     "links",
		})
		data := asMap(out["data"])
		inter := asMap(data["interactive"])
		elems := toAnySlice(inter["elements"])
		if len(elems) == 0 {
			t.Fatalf("expected links, got 0 elements")
		}
		for _, el := range elems {
			elm := asMap(el)
			if typ := fmt.Sprintf("%v", elm["type"]); typ != "link" {
				t.Errorf("expected only link elements, got %s: %v", typ, elm)
			}
		}
	})

	t.Run("screenshot mode with annotate honours filter", func(t *testing.T) {
		out := observeProbe(t, p, map[string]interface{}{
			"session_id": p.session,
			"mode":       "screenshot",
			"annotate":   true,
			"filter":     "buttons",
		})
		data := asMap(out["data"])
		elems := toAnySlice(data["elements"])
		if len(elems) == 0 {
			t.Fatalf("expected annotated button elements, got 0")
		}
		for _, el := range elems {
			elm := asMap(el)
			typ := fmt.Sprintf("%v", elm["type"])
			if typ != "button" && typ != "clickable" && typ != "tab" && typ != "menuitem" && typ != "option" && typ != "row" {
				t.Errorf("screenshot annotate with filter=buttons must not annotate %s: %v", typ, elm)
			}
		}
	})

	t.Run("nav mode honours filter buttons by returning zero links", func(t *testing.T) {
		out := observeProbe(t, p, map[string]interface{}{
			"session_id": p.session,
			"mode":       "nav",
			"filter":     "buttons",
		})
		data := asMap(out["data"])
		if nav, ok := data["nav"].(map[string]interface{}); ok {
			if counts, ok := nav["counts"].(map[string]interface{}); ok && asInt(counts["total"]) > 0 {
				t.Errorf("nav mode with filter=buttons must not return links, got: %v", nav)
			}
		}
	})

	t.Run("composite mode honours filter selects", func(t *testing.T) {
		out := observeProbe(t, p, map[string]interface{}{
			"session_id": p.session,
			"mode":       "composite",
			"filter":     "selects",
		})
		data := asMap(out["data"])
		inter := asMap(data["interactive"])
		elems := toAnySlice(inter["elements"])
		if len(elems) != 2 {
			t.Fatalf("expected 2 select elements (native select + combobox), got %d: %v", len(elems), elems)
		}
		for _, el := range elems {
			elm := asMap(el)
			typ := fmt.Sprintf("%v", elm["type"])
			if typ != "select" && typ != "combobox" && typ != "listbox" && typ != "option" {
				t.Errorf("unexpected non-select type %q in filter=selects: %v", typ, elm)
			}
		}
	})

	t.Run("invalid filter refused loudly", func(t *testing.T) {
		tool := &BrowserObserveTool{sessions: p.manager, engine: p.engine}
		res, err := tool.Execute(p.ctx, map[string]interface{}{
			"session_id": p.session,
			"filter":     "invalid_kind",
		})
		if err != nil {
			// Refusal via Go error is acceptable
			return
		}
		resMap := asMap(res)
		if resMap["success"] == true {
			t.Fatalf("expected invalid filter to fail, got success: %v", resMap)
		}
		errStr := fmt.Sprintf("%v", resMap["error"])
		if !strings.Contains(errStr, "invalid") || !strings.Contains(errStr, "filter") {
			t.Fatalf("expected error message to state invalid filter, got: %v", resMap["error"])
		}
	})
}
