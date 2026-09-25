package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

// Repeated controls collapse into one candidate carrying their count, dead controls
// drop out, and the ranking puts a unique form submit ahead of a row button repeated
// 400 times and ahead of a destructive control.
func TestGroupAndRankActionCandidates(t *testing.T) {
	raw := []map[string]interface{}{
		{"action": "click", "ref": "button.browse", "label": "Browse nodes", "priority": 80, "reason": "enabled_button", "count": 400},
		{"action": "click", "ref": "button.browse_1", "label": "Browse nodes", "priority": 80, "reason": "enabled_button"},
		{"action": "click", "ref": "del", "label": "Delete", "priority": 80, "reason": "enabled_button"},
		{"action": "click", "ref": "dead-btn", "label": "Dead action", "priority": 80, "reason": "enabled_button", "dead": true},
		{"action": "click", "ref": "testid:save", "label": "Save", "priority": 100, "reason": "primary_action"},
		{"action": "click", "ref": "failbtn", "label": "Trigger failure", "priority": 80, "reason": "enabled_button"},
		{"action": "press_escape", "ref": "", "label": "", "priority": 110, "reason": "modal"},
	}
	ranked := rankActionCandidates(groupActionCandidates(raw))

	labels := make([]string, 0, len(ranked))
	for _, c := range ranked {
		labels = append(labels, fmt.Sprintf("%v", c["label"]))
		if c["ref"] == "dead-btn" {
			t.Fatalf("dead control survived ranking: %v", c)
		}
	}
	want := []string{"", "Save", "Trigger failure", "Delete", "Browse nodes"}
	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Fatalf("ranking = %q, want %q", labels, want)
	}
	browse := ranked[len(ranked)-1]
	if asInt(browse["count"]) != 400 || browse["ref"] != "button.browse" {
		t.Fatalf("Browse nodes group = %v, want count 400 headed by button.browse", browse)
	}
}

// browser-reason reads candidates straight from facts, so the repeat count and dead
// flags that get-interactive-elements records reach its ranking too.
func TestQueryActionCandidatesUsesGroupAndDeadFacts(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	ctx := context.Background()
	now := time.Now()
	s := "s-rank"
	facts := []mangle.Fact{
		{Predicate: "interactive", Args: []interface{}{s, "button.browse", "button", "Browse nodes", "click"}, Timestamp: now},
		{Predicate: "element_enabled", Args: []interface{}{s, "button.browse", "true"}, Timestamp: now},
		{Predicate: "elem_attr", Args: []interface{}{s, "button.browse", "group_count", "400"}, Timestamp: now},
		{Predicate: "interactive", Args: []interface{}{s, "dead-btn", "button", "Dead action", "click"}, Timestamp: now},
		{Predicate: "element_enabled", Args: []interface{}{s, "dead-btn", "true"}, Timestamp: now},
		{Predicate: "dead_control", Args: []interface{}{s, "dead-btn", "Dead action"}, Timestamp: now},
		{Predicate: "interactive", Args: []interface{}{s, "testid:save", "button", "Save", "click"}, Timestamp: now},
		{Predicate: "element_enabled", Args: []interface{}{s, "testid:save", "true"}, Timestamp: now},
		{Predicate: "elem_attr", Args: []interface{}{s, "testid:save", "button_type", "submit"}, Timestamp: now},
		{Predicate: "interactive", Args: []interface{}{s, "card1", "clickable", "Open card", "click"}, Timestamp: now},
		{Predicate: "interactive", Args: []interface{}{s, "combo", "combobox", "Pick a color", "click"}, Timestamp: now},
		{Predicate: "element_enabled", Args: []interface{}{s, "combo", "true"}, Timestamp: now},
	}
	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatal(err)
	}

	candidates := queryActionCandidates(ctx, engine, s, 10)
	if len(candidates) == 0 || candidates[0]["ref"] != "testid:save" {
		t.Fatalf("Save should lead: %v", candidates)
	}
	seen := map[string]map[string]interface{}{}
	for _, c := range candidates {
		seen[fmt.Sprintf("%v", c["ref"])] = c
	}
	if _, ok := seen["dead-btn"]; ok {
		t.Fatalf("dead control offered: %v", candidates)
	}
	if asInt(seen["button.browse"]["count"]) != 400 {
		t.Fatalf("group count not carried: %v", seen["button.browse"])
	}
	// Module 75 makes the widget and non-semantic types plannable.
	if seen["card1"] == nil || seen["combo"] == nil {
		t.Fatalf("clickable card or combobox missing from candidates: %v", candidates)
	}
}

// A recommendation is one ready-to-send browser-act call and a short reason; controls
// offering the same choice yield one recommendation.
func TestBuildActionPlanRecommendationsIsCompactAndDeduplicated(t *testing.T) {
	candidates := []map[string]interface{}{
		{"action": "click", "ref": "testid:save", "label": "Save", "priority": 100, "reason": "primary_action"},
		{"action": "click", "ref": "button.row", "label": "Open", "priority": 80, "reason": "enabled_button", "count": 12},
		{"action": "click", "ref": "button.row_1", "label": "Open", "priority": 80, "reason": "enabled_button"},
	}
	recs := buildActionPlanRecommendations(candidates, 5, "s-1", "")
	if len(recs) != 2 {
		t.Fatalf("expected 2 recommendations, got %d: %v", len(recs), recs)
	}
	for _, rec := range recs {
		for _, key := range []string{"candidate", "requires_user_input"} {
			if _, ok := rec[key]; ok {
				t.Fatalf("recommendation carries %s: %v", key, rec)
			}
		}
		args := asMap(rec["args"])
		if len(args) != 2 || args["session_id"] != "s-1" {
			t.Fatalf("recommendation args should be session_id and operations only: %v", args)
		}
	}
	if recs[0]["reason"] != "primary_action: Save" {
		t.Fatalf("reason = %v", recs[0]["reason"])
	}
	if recs[1]["reason"] != "enabled_button: Open (1 of 12 alike)" {
		t.Fatalf("reason = %v", recs[1]["reason"])
	}
}

func TestRefRunSummary(t *testing.T) {
	cases := []struct {
		members []string
		want    string
	}{
		{[]string{"button.browse", "button.browse_1", "button.browse_2", "button.browse_3"}, "button.browse_1..button.browse_3"},
		{[]string{"row_4", "row_5", "row_6", "row_7"}, "row_5..row_7"},
		{[]string{"a", "b", "c", "d", "e"}, "b, c, d, +1 more"},
		{[]string{"x", "x_1"}, "x_1"},
		{[]string{"only"}, ""},
	}
	for _, c := range cases {
		if got := refRunSummary(c.members); got != c.want {
			t.Errorf("refRunSummary(%v) = %q, want %q", c.members, got, c.want)
		}
	}
	if got := numberRanges([]int{3, 4, 5, 9, 11, 12}); got != "3-5,9,11-12" {
		t.Errorf("numberRanges = %q", got)
	}
}

// Annotation numbers every distinct control before it spends slots on repeats, and
// numbers the picks in document order.
func TestSelectAnnotationBoxesPrefersDistinctControls(t *testing.T) {
	boxes := []BoundingBox{}
	add := func(label, ref string) {
		boxes = append(boxes, BoundingBox{Index: len(boxes), Type: "button", Label: label, Ref: ref})
	}
	add("Home", "a[0]")
	for i := 0; i < 10; i++ {
		ref := "button.browse"
		if i > 0 {
			ref = fmt.Sprintf("button.browse_%d", i)
		}
		add("Browse nodes", ref)
	}
	add("Save", "testid:save")
	add("Delete", "del")

	picked := selectAnnotationBoxes(boxes, 5)
	if len(picked) != 5 {
		t.Fatalf("picked %d boxes", len(picked))
	}
	got := make([]string, 0, len(picked))
	for i, box := range picked {
		if box.Index != i {
			t.Fatalf("box %d numbered %d", i, box.Index)
		}
		got = append(got, box.Ref)
	}
	want := "a[0]|button.browse|button.browse_1|testid:save|del"
	if strings.Join(got, "|") != want {
		t.Fatalf("picked %q, want %q", strings.Join(got, "|"), want)
	}

	rows := groupAnnotationBoxes(picked)
	if len(rows) != 4 {
		t.Fatalf("grouped rows = %v", rows)
	}
	if rows[1]["n"] != "1-2" || asInt(rows[1]["count"]) != 2 || rows[1]["refs"] != "button.browse_1" {
		t.Fatalf("Browse nodes row = %v", rows[1])
	}
}

// Sessions mode counts detached records truthfully and lists them only on request.
func TestSessionListingSeparatesDetachedRecords(t *testing.T) {
	now := time.Now()
	all := []browser.Session{{ID: "live", URL: "http://app/", Status: "active", LastActive: now}}
	for i := 0; i < 36; i++ {
		all = append(all, browser.Session{ID: fmt.Sprintf("old-%d", i), Status: "detached", LastActive: now.Add(-time.Duration(i+1) * time.Hour)})
	}

	data, summary, truncated := sessionListing(all, false, "compact", 20)
	rows := toMapSlice(data["sessions"])
	if len(rows) != 1 || rows[0]["id"] != "live" || truncated {
		t.Fatalf("default listing = %v (truncated %v)", rows, truncated)
	}
	if asInt(data["active_count"]) != 1 || asInt(data["detached_count"]) != 36 {
		t.Fatalf("counts = %v", data)
	}
	if !strings.Contains(summary, "1 active") || !strings.Contains(summary, "36 detached") {
		t.Fatalf("summary = %q", summary)
	}

	data, _, truncated = sessionListing(all, true, "compact", 20)
	rows = toMapSlice(data["sessions"])
	if len(rows) != 20 || !truncated || rows[0]["id"] != "live" {
		t.Fatalf("include_detached listing: %d rows, truncated %v, first %v", len(rows), truncated, rows[0])
	}

	data, _, _ = sessionListing(all, false, "summary", 20)
	if _, listed := data["sessions"]; listed {
		t.Fatalf("summary view listed sessions: %v", data)
	}
}

// The health digest counts only the current page load: failures from before the
// latest navigation are gone, the load's own failures count even when browser-act
// records its navigation after the load finished.
func TestObserveHealthDigestScopesToCurrentLoad(t *testing.T) {
	engine := testMangleEngineForProgressive(t)
	ctx := context.Background()
	s := "s-health"
	load := time.Now().Add(-10 * time.Second)
	at := func(offset time.Duration) time.Time { return load.Add(offset) }
	facts := []mangle.Fact{
		{Predicate: "net_request", Args: []interface{}{s, "old", "GET", "http://app/api/old", "", at(-2 * time.Minute).UnixMilli()}, Timestamp: at(-2 * time.Minute)},
		{Predicate: "net_response", Args: []interface{}{s, "old", 500, 1, 1}, Timestamp: at(-2 * time.Minute)},
		{Predicate: "navigation_event", Args: []interface{}{s, "http://app/", load.UnixMilli()}, Timestamp: load},
		{Predicate: "net_request", Args: []interface{}{s, "r1", "GET", "http://app/api/missing?x=1", "", at(time.Second).UnixMilli()}, Timestamp: at(time.Second)},
		{Predicate: "net_response", Args: []interface{}{s, "r1", 404, 1, 1}, Timestamp: at(time.Second)},
		{Predicate: "net_response", Args: []interface{}{s, "r2", 200, 1, 1}, Timestamp: at(time.Second)},
		{Predicate: "console_event", Args: []interface{}{s, "error", "boom", at(2 * time.Second).UnixMilli()}, Timestamp: at(2 * time.Second)},
		{Predicate: "console_event", Args: []interface{}{s, "warning", "meh", at(2 * time.Second).UnixMilli()}, Timestamp: at(2 * time.Second)},
		{Predicate: "net_response", Args: []interface{}{"other-session", "r9", 500, 1, 1}, Timestamp: at(time.Second)},
		// browser-act records its navigation once the load has finished.
		{Predicate: "navigation_event", Args: []interface{}{s, "http://app/index.html", at(4 * time.Second).UnixMilli()}, Timestamp: at(4 * time.Second)},
	}
	if err := engine.AddFacts(ctx, facts); err != nil {
		t.Fatal(err)
	}
	health := observeHealthDigest(engine, s)
	if asInt(health["failed_requests"]) != 1 || asInt(health["console_errors"]) != 1 {
		t.Fatalf("health = %v", health)
	}
	if recent := fmt.Sprintf("%v", health["recent_failures"]); recent != "[404 /api/missing?x=1]" {
		t.Fatalf("recent failures = %s", recent)
	}
	if observeHealthDigest(engine, "quiet-session") != nil {
		t.Fatal("a session with no failures should have no health digest")
	}
}

// Filtering grouped rows keeps their counts in the totals and admits the widget types.
func TestFilterInteractiveDataCountsGroupedRows(t *testing.T) {
	data := map[string]interface{}{
		"summary": map[string]interface{}{"total": 404, "returned": 4, "groups": 4},
		"elements": []interface{}{
			map[string]interface{}{"ref": "button.browse", "type": "button", "label": "Browse", "count": 400},
			map[string]interface{}{"ref": "card1", "type": "clickable", "label": "Card"},
			map[string]interface{}{"ref": "combo", "type": "combobox", "label": "Pick"},
			map[string]interface{}{"ref": "name", "type": "input", "label": "Name"},
		},
	}
	buttons := filterInteractiveData(data, "buttons")
	summary := asMap(buttons["summary"])
	if asInt(summary["total"]) != 401 || asInt(summary["returned"]) != 2 || asInt(summary["groups"]) != 2 {
		t.Fatalf("buttons summary = %v", summary)
	}
	selects := filterInteractiveData(data, "selects")
	if len(toAnySlice(selects["elements"])) != 1 {
		t.Fatalf("selects should hold the combobox: %v", selects["elements"])
	}

	compact := compactObserveInteractive(data, 2)
	if asInt(asMap(compact["summary"])["returned"]) != 2 || compact["truncated"] != true {
		t.Fatalf("compact interactive = %v", compact)
	}
}

// browser-observe's description and schema are sent with every tools/list, so they are
// budgeted like any response.
func TestObserveToolDefinitionBudget(t *testing.T) {
	tool := &BrowserObserveTool{}
	schema := responseTokens(t, tool.InputSchema())
	description := len(tool.Description()) / 4
	t.Logf("browser-observe definition: description ~%d tokens, schema ~%d tokens", description, schema)
	if description+schema > 1100 {
		t.Fatalf("browser-observe definition costs ~%d tokens, budget 1100", description+schema)
	}
	for _, mode := range []string{"text", "interactive", "screenshot", "sessions"} {
		if !strings.Contains(tool.Description(), mode) {
			t.Fatalf("description does not say when to use mode %s", mode)
		}
	}
}

// The composite content digest stays within its character budget.
func TestContentDigestBudget(t *testing.T) {
	headings := []interface{}{}
	for i := 0; i < 20; i++ {
		headings = append(headings, fmt.Sprintf("## Section heading number %d with some words", i))
	}
	digest := contentDigest(map[string]interface{}{
		"live":           []interface{}{"status: ready", "alert: Could not save"},
		"headings":       headings,
		"total_headings": 20,
	})
	size := 0
	for _, key := range []string{"live", "headings"} {
		for _, item := range toAnySlice(digest[key]) {
			size += len(fmt.Sprintf("%v", item))
		}
	}
	if size > contentDigestChars {
		t.Fatalf("digest holds %d chars, budget %d", size, contentDigestChars)
	}
	if len(toAnySlice(digest["live"])) != 2 || digest["more"] != "mode:text" {
		t.Fatalf("digest = %v", digest)
	}
}
