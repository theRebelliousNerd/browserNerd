package mangle

import (
	"context"
	"testing"
	"time"
)

// QuerySince reads the facts a run can claim: its own events, and states that
// still hold however old their fact is.
func TestQuerySinceKeepsCurrentStateAndDropsEarlierEvents(t *testing.T) {
	engine := newTestEngine(t, 1000)
	ctx := context.Background()
	before := time.Now().Add(-time.Minute)
	if err := engine.AddFacts(ctx, []Fact{
		{Predicate: "current_url", Args: []interface{}{"s", "http://app/login"}, Timestamp: before},
		{Predicate: "net_request", Args: []interface{}{"s", "old", "GET", "http://app/api", "fetch", before.UnixMilli()}, Timestamp: before},
		{Predicate: "net_response", Args: []interface{}{"s", "old", int64(500), int64(5), int64(9)}, Timestamp: before},
	}); err != nil {
		t.Fatal(err)
	}
	since := time.Now()

	count := func(query string, view bool) int {
		t.Helper()
		var rows []QueryResult
		var err error
		if view {
			rows, err = engine.QuerySince(ctx, query, since)
		} else {
			rows, err = engine.Query(ctx, query)
		}
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		return len(rows)
	}

	if got := count(`current_url(S, "http://app/login")`, true); got != 1 {
		t.Fatalf("the tab is still on /login, so the run view has its URL; got %d rows", got)
	}
	if got := count(`failed_request(S, R, U, St)`, false); got != 1 {
		t.Fatalf("the whole buffer has the earlier failure; got %d rows", got)
	}
	if got := count(`failed_request(S, R, U, St)`, true); got != 0 {
		t.Fatalf("a failure from before the run is not the run's; got %d rows", got)
	}
	if got := count(`on_login(S) :- current_url(S, U), :string:contains(U, "/login")`, true); got != 1 {
		t.Fatalf("a query with a body reads the same view; got %d rows", got)
	}

	now := since.Add(time.Millisecond)
	if err := engine.AddFacts(ctx, []Fact{
		{Predicate: "net_request", Args: []interface{}{"s", "new", "POST", "http://app/api/login", "fetch", now.UnixMilli()}, Timestamp: now},
		{Predicate: "net_response", Args: []interface{}{"s", "new", int64(502), int64(5), int64(9)}, Timestamp: now},
		{Predicate: "current_url", Args: []interface{}{"s", "http://app/dashboard"}, Timestamp: now},
	}); err != nil {
		t.Fatal(err)
	}
	if got := count(`failed_request(S, R, U, St)`, true); got != 1 {
		t.Fatalf("the run's own failure is in its view; got %d rows", got)
	}
	if got := count(`current_url(S, "http://app/login")`, true); got != 0 {
		t.Fatalf("a superseded URL is not current; got %d rows", got)
	}
	if _, err := engine.QuerySince(ctx, `current_url(S, U)`, time.Time{}); err == nil {
		t.Fatal("QuerySince without a start time must be refused")
	}
}

// A fact stamped exactly at the run's start belongs to before the run: on a
// clock that advances in coarse ticks, the last event before a run and the
// run's start read the same time.
func TestQuerySinceCountsATieAsBeforeTheRun(t *testing.T) {
	engine := newTestEngine(t, 1000)
	ctx := context.Background()
	since := time.Now()
	if err := engine.AddFacts(ctx, []Fact{
		{Predicate: "console_event", Args: []interface{}{"s", "error", "boom", since.UnixMilli()}, Timestamp: since},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := engine.QuerySince(ctx, `console_event(S, "error", M, T)`, since)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("a fact stamped at since is not the run's: %+v", rows)
	}
}
