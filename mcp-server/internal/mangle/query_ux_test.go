package mangle

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func seedRequests(t *testing.T, engine *Engine) {
	t.Helper()
	now := time.Now()
	_ = engine.AddFacts(context.Background(), []Fact{
		{Predicate: "net_request", Args: []interface{}{"s1", "r1", "GET", "http://app/api/ok", "", now.UnixMilli()}, Timestamp: now},
		{Predicate: "net_response", Args: []interface{}{"s1", "r1", int64(200), int64(5), int64(9)}, Timestamp: now},
		{Predicate: "net_request", Args: []interface{}{"s1", "r2", "POST", "http://app/api/save", "", now.UnixMilli()}, Timestamp: now},
		{Predicate: "net_response", Args: []interface{}{"s1", "r2", int64(500), int64(7), int64(12)}, Timestamp: now},
	})
}

// A conjunction joins predicates without writing a rule, and returns every
// variable it binds.
func TestQueryConjunction(t *testing.T) {
	engine := newTestEngine(t, 1000)
	seedRequests(t, engine)
	rows, err := engine.Query(context.Background(), `net_request(S, R, M, U, I, T), net_response(S, R, 500, L, D)`)
	if err != nil {
		t.Fatalf("conjunction: %v", err)
	}
	if len(rows) != 1 || rows[0]["U"] != "http://app/api/save" || rows[0]["M"] != "POST" || fmt.Sprintf("%v", rows[0]["D"]) != "12" {
		t.Fatalf("expected the failed POST joined to its URL, got %v", rows)
	}
	rows, err = engine.Query(context.Background(), `net_request(S, R, _, U, _, _), net_response(S, R, St, _, _), St >= 400.`)
	if err != nil || len(rows) != 1 || fmt.Sprintf("%v", rows[0]["St"]) != "500" {
		t.Fatalf("conjunction with a comparison: %v, %v", rows, err)
	}
	if _, err := engine.Query(context.Background(), `net_request(S, R, M, U, I), net_response(S, R, 500, L, D)`); err == nil || !strings.Contains(err.Error(), "net_request(SessionId, Id, Method, Url, InitiatorId, StartTime)") {
		t.Fatalf("a wrong arity inside a conjunction must name the signature, got %v", err)
	}
	// A comma inside a string is not a conjunction.
	if _, err := engine.Query(context.Background(), `console_event(S, "error", "a, b", T)`); err != nil {
		t.Fatalf("comma in a string literal: %v", err)
	}
}

// A wrong-arity query names the declared signature instead of returning 0 rows.
func TestQueryArityErrorNamesSignature(t *testing.T) {
	engine := newTestEngine(t, 1000)
	seedRequests(t, engine)
	_, err := engine.Query(context.Background(), `net_response(R, 500, U, T).`)
	if err == nil {
		t.Fatal("expected an arity error")
	}
	for _, want := range []string{"net_response takes 5 arguments, not 4", "net_response(SessionId, Id, Status, Latency, Duration)", "builtin:00_core_events"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error must contain %q: %v", want, err)
		}
	}
	if _, err := engine.Evaluate(context.Background(), "failed_requst"); err == nil || !strings.Contains(err.Error(), "did you mean failed_request(") {
		t.Fatalf("evaluate of a misspelled predicate: %v", err)
	}
}

// The schema catalog lists predicates with argument names and module,
// filtered by substring.
func TestSchemaCatalog(t *testing.T) {
	engine := newTestEngine(t, 1000)
	seedRequests(t, engine)
	_ = engine.AddFacts(context.Background(), []Fact{{Predicate: "custom_marker", Args: []interface{}{"s1", "x"}, Timestamp: time.Now()}})
	all := engine.Schema("")
	if len(all) < 100 {
		t.Fatalf("expected the whole program, got %d predicates", len(all))
	}
	found := map[string]PredicateInfo{}
	for _, p := range engine.Schema("net_") {
		if !strings.Contains(p.Name, "net_") {
			t.Fatalf("filter leaked %s", p.Name)
		}
		found[p.Name] = p
	}
	resp := found["net_response"]
	if resp.Signature != "net_response(SessionId, Id, Status, Latency, Duration)" || resp.Module != "builtin:00_core_events" || resp.Kind != "base" || resp.Facts != 2 {
		t.Fatalf("net_response entry: %+v", resp)
	}
	failed := engine.Schema("failed_request")
	derived := false
	for _, p := range failed {
		if p.Name == "failed_request" && p.Kind == "derived" && p.Module == "builtin:10_causal_temporal" {
			derived = true
		}
	}
	if !derived {
		t.Fatalf("failed_request must be listed as derived from 10_causal_temporal: %+v", failed)
	}
	custom := engine.Schema("custom_marker")
	if len(custom) != 1 || custom[0].Module != "pushed facts" || custom[0].Arity != 2 {
		t.Fatalf("pushed predicate entry: %+v", custom)
	}
	for _, p := range all {
		if strings.HasPrefix(p.Name, "mt_") || strings.HasPrefix(p.Name, "__") {
			t.Fatalf("internal predicate listed: %s", p.Name)
		}
	}
}

// A submitted rule that references a misspelled predicate is rejected with a
// suggestion instead of silently never firing.
func TestSubmitRuleRejectsUnknownPremise(t *testing.T) {
	engine := newTestEngine(t, 1000)
	err := engine.AddRule(`Decl broken(S).
broken(S) :- net_respons(S, _, _, _, _).`)
	if err == nil || !strings.Contains(err.Error(), "did you mean net_response(") {
		t.Fatalf("expected a did-you-mean for the rule body, got %v", err)
	}
	if err := engine.AddRule(`Decl helper(S).
helper(S) :- net_response(S, _, 500, _, _).
uses_helper(S) :- helper(S).`); err != nil {
		t.Fatalf("predicates defined in the same submission are known: %v", err)
	}
}

func TestStatsReportsEngineShape(t *testing.T) {
	engine := newTestEngine(t, 1000)
	seedRequests(t, engine)
	if _, err := engine.Query(context.Background(), "failed_request(S, R, U, St)."); err != nil {
		t.Fatal(err)
	}
	stats := engine.Stats()
	if stats.Facts != 4 || stats.Limit != 1000 || stats.TrimTarget != 900 || !stats.Ready {
		t.Fatalf("occupancy: %+v", stats)
	}
	if stats.Rules < 100 || stats.Predicates < 100 || len(stats.Modules) < 9 || stats.Evaluations == 0 || stats.CachedDerivations == 0 {
		t.Fatalf("program shape: %+v", stats)
	}
	if stats.FactsByClass["high"] != 2 {
		t.Fatalf("the failed request and its response are high-signal: %+v", stats.FactsByClass)
	}
}
