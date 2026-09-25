package mangle

import (
	"context"
	"fmt"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

func newTestEngine(t *testing.T, limit int) *Engine {
	t.Helper()
	engine, err := NewEngine(config.MangleConfig{Enable: true, FactBufferLimit: limit})
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return engine
}

// Under a polling app the buffer must shed the successful polls and keep
// every failure, its request, and the console error it caused.
func TestRetentionKeepsFailureEvidenceUnderPolling(t *testing.T) {
	engine := newTestEngine(t, 600)
	ctx := context.Background()
	polls := 1000
	for _, batch := range pollingWorkload(polls, time.Now().Add(-time.Hour)) {
		if err := engine.AddFacts(ctx, batch); err != nil {
			t.Fatal(err)
		}
	}
	if n := len(engine.Facts()); n > 600 {
		t.Fatalf("buffer over its limit: %d", n)
	}
	failures := map[string]bool{}
	for _, f := range engine.FactsByPredicate("net_response") {
		if argInt(f, 2) >= 500 {
			failures[argString(f, 1)] = true
		}
	}
	if len(failures) != polls/25 {
		t.Fatalf("expected all %d failed responses retained, got %d", polls/25, len(failures))
	}
	requests := map[string]bool{}
	for _, f := range engine.FactsByPredicate("net_request") {
		requests[argString(f, 1)] = true
	}
	for id := range failures {
		if !requests[id] {
			t.Fatalf("failed response %s kept without its request", id)
		}
	}
	if got := len(engine.FactsByPredicate("console_event")); got != polls/25 {
		t.Fatalf("expected %d console errors retained, got %d", polls/25, got)
	}
	stats := engine.Stats()
	if stats.Evicted["high"] != 0 || stats.Evicted["low"] == 0 {
		t.Fatalf("eviction must take low-signal facts only while any remain: %+v", stats.Evicted)
	}
	rows, err := engine.Query(ctx, "failed_request(S, R, U, St).")
	if err != nil || len(rows) != polls/25 {
		t.Fatalf("failed_request after eviction: %d rows, %v", len(rows), err)
	}
}

// A request's facts leave together: no response outlives its request.
func TestRetentionEvictsRequestsAsUnits(t *testing.T) {
	engine := newTestEngine(t, 300)
	ctx := context.Background()
	for _, batch := range pollingWorkload(400, time.Now().Add(-time.Hour)) {
		_ = engine.AddFacts(ctx, batch)
	}
	requests := map[string]bool{}
	for _, f := range engine.FactsByPredicate("net_request") {
		requests[argString(f, 1)] = true
	}
	for _, f := range engine.FactsByPredicate("net_response") {
		if !requests[argString(f, 1)] {
			t.Fatalf("response %s kept without its request", argString(f, 1))
		}
	}
	for _, f := range engine.FactsByPredicate("net_header") {
		if !requests[argString(f, 1)] {
			t.Fatalf("header of %s kept without its request", argString(f, 1))
		}
	}
}

// Trimming goes to 90% of the limit, so it runs once per limit/10 facts,
// not on every batch once the buffer is full.
func TestRetentionTrimsWithHysteresis(t *testing.T) {
	engine := newTestEngine(t, 100)
	ctx := context.Background()
	for i := 0; i < 1000; i++ {
		_ = engine.AddFacts(ctx, []Fact{{Predicate: "console_event", Args: []interface{}{"s1", "log", fmt.Sprintf("m%d", i), int64(i)}, Timestamp: time.Now()}})
	}
	stats := engine.Stats()
	if stats.Trims > 1000/10+1 {
		t.Fatalf("expected about one trim per 10 facts, got %d trims", stats.Trims)
	}
	if stats.Facts > 100 || stats.Facts < 90 {
		t.Fatalf("buffer should sit between the trim target and the limit, has %d", stats.Facts)
	}
}

// current_url is where a session is: a navigation replaces the session's
// previous URL instead of adding a second answer.
func TestCurrentURLKeepsLatestPerSession(t *testing.T) {
	engine := newTestEngine(t, 1000)
	ctx := context.Background()
	now := time.Now()
	for i, url := range []string{"http://app/a", "http://app/b", "http://app/c"} {
		_ = engine.AddFacts(ctx, []Fact{{Predicate: "current_url", Args: []interface{}{"s1", url}, Timestamp: now.Add(time.Duration(i) * time.Millisecond)}})
	}
	_ = engine.AddFacts(ctx, []Fact{{Predicate: "current_url", Args: []interface{}{"s2", "http://app/x"}, Timestamp: now}})
	rows, err := engine.Query(ctx, "current_url(S, U).")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range rows {
		got[fmt.Sprintf("%v", row["S"])] = fmt.Sprintf("%v", row["U"])
	}
	if len(rows) != 2 || got["s1"] != "http://app/c" || got["s2"] != "http://app/x" {
		t.Fatalf("expected the latest URL per session, got %v", rows)
	}
}

// A failing poll that logs a console error every 400 ms: every error is
// linked to the failure just before it, and a runtime rule still submits.
// Joined on the session alone, the correlation built errors x failures rows
// and hit the created-fact limit, which failed every submit_rule.
func TestCausedByUnderFailingPollAndSubmitRule(t *testing.T) {
	engine := newTestEngine(t, 20000)
	ctx := context.Background()
	start := time.Now().Add(-time.Hour)
	const polls = 400
	for i := 0; i < polls; i++ {
		ts := start.Add(time.Duration(i) * 400 * time.Millisecond)
		id := fmt.Sprintf("req-%d", i)
		done := ts.Add(1500 * time.Millisecond) // a slow failure: response 1.5 s after the request
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "net_request", Args: []interface{}{"s1", id, "POST", "http://app/api/save", "", ts.UnixMilli()}, Timestamp: ts},
			{Predicate: "net_response", Args: []interface{}{"s1", id, int64(500), int64(1500), int64(1500)}, Timestamp: done},
			{Predicate: "net_http_error", Args: []interface{}{"s1", id, "http://app/api/save", int64(500), "Fetch", done.UnixMilli()}, Timestamp: done},
			{Predicate: "console_event", Args: []interface{}{"s1", "error", "Save failed", done.UnixMilli() + 3}, Timestamp: done.Add(3 * time.Millisecond)},
		})
	}
	rows, err := engine.Query(ctx, "caused_by(S, Msg, R).")
	if err != nil {
		t.Fatalf("caused_by under load: %v", err)
	}
	if len(rows) != polls {
		t.Fatalf("expected each error linked to exactly its own failure (%d rows), got %d", polls, len(rows))
	}
	if err := engine.AddRule(`Decl saves_failed(SessionId, Url).
saves_failed(S, U) :- failed_request(S, _, U, Status), Status >= 500.`); err != nil {
		t.Fatalf("submit_rule under load: %v", err)
	}
	rows, err = engine.Query(ctx, "saves_failed(S, U).")
	if err != nil || len(rows) != 1 {
		t.Fatalf("runtime rule: %v, %v", rows, err)
	}
}

// A 404 on a page asset is not a failed app request; a 404 from the API is.
func TestFailedRequestIgnoresAsset4xx(t *testing.T) {
	engine := newTestEngine(t, 1000)
	ctx := context.Background()
	now := time.Now()
	add := func(id, url string, status int64, resourceType string) {
		_ = engine.AddFacts(ctx, []Fact{
			{Predicate: "net_request", Args: []interface{}{"s1", id, "GET", url, "", now.UnixMilli()}, Timestamp: now},
			{Predicate: "net_response", Args: []interface{}{"s1", id, status, int64(1), int64(1)}, Timestamp: now},
			{Predicate: "net_http_error", Args: []interface{}{"s1", id, url, status, resourceType, now.UnixMilli()}, Timestamp: now},
		})
	}
	add("fav", "http://app/favicon.ico", 404, "Other")
	add("img", "http://app/logo.png", 404, "Image")
	add("api", "http://app/api/missing", 404, "Fetch")
	add("chunk", "http://app/_next/chunk.js", 404, "Script")
	add("boom", "http://app/logo2.png", 503, "Image")
	rows, err := engine.Query(ctx, "failed_request(S, R, U, St).")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, row := range rows {
		got[fmt.Sprintf("%v", row["R"])] = true
	}
	if len(rows) != 3 || !got["api"] || !got["chunk"] || !got["boom"] {
		t.Fatalf("expected api, chunk and boom, got %v", rows)
	}
}
