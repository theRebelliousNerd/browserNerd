package mcp

import (
	"context"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/spec"
)

func TestResolveComponentBinding(t *testing.T) {
	engine := setupTestEngine(t)
	now := time.Now()
	// react_component(S, FiberId, Name, ParentFiberId); dom_mapping(S, FiberId, NodeId)
	if err := engine.AddFacts(context.Background(), []mangle.Fact{
		{Predicate: "react_component", Args: []interface{}{testSessionID, "f1", "LoginForm", "root"}, Timestamp: now},
		{Predicate: "dom_mapping", Args: []interface{}{testSessionID, "f1", "node-42"}, Timestamp: now},
	}); err != nil {
		t.Fatal(err)
	}

	res := resolveBinding(context.Background(), engine, testSessionID, spec.Binding{Kind: "component", Target: "LoginForm"})
	if !res.Present {
		t.Fatalf("expected LoginForm present, got %+v", res)
	}
	if len(res.Nodes) != 1 || res.Nodes[0] != "node-42" {
		t.Fatalf("expected node-42, got %+v", res.Nodes)
	}

	absent := resolveBinding(context.Background(), engine, testSessionID, spec.Binding{Kind: "component", Target: "Nope"})
	if absent.Present {
		t.Fatalf("expected absent component, got %+v", absent)
	}
}

func TestResolveRouteBinding(t *testing.T) {
	engine := setupTestEngine(t)
	now := time.Now()
	if err := engine.AddFacts(context.Background(), []mangle.Fact{
		{Predicate: "current_url", Args: []interface{}{testSessionID, "https://app.example.com/login?next=/x"}, Timestamp: now},
	}); err != nil {
		t.Fatal(err)
	}

	if r := resolveBinding(context.Background(), engine, testSessionID, spec.Binding{Kind: "route", Target: "/login"}); !r.Present {
		t.Fatalf("expected /login to match, got %+v", r)
	}
	if r := resolveBinding(context.Background(), engine, testSessionID, spec.Binding{Kind: "route", Target: "/dashboard"}); r.Present {
		t.Fatalf("expected /dashboard not to match, got %+v", r)
	}
}

func TestResolveSelectorBinding(t *testing.T) {
	engine := setupTestEngine(t)
	now := time.Now()
	if err := engine.AddFacts(context.Background(), []mangle.Fact{
		{Predicate: "dom_attr", Args: []interface{}{testSessionID, "node-7", "data-testid", "submit-btn"}, Timestamp: now},
	}); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{"submit-btn", "[data-testid=submit-btn]"} {
		r := resolveBinding(context.Background(), engine, testSessionID, spec.Binding{Kind: "selector", Target: target})
		if !r.Present || len(r.Nodes) != 1 || r.Nodes[0] != "node-7" {
			t.Fatalf("selector %q: expected node-7, got %+v", target, r)
		}
	}
}

func TestRouteMatches(t *testing.T) {
	cases := []struct {
		url, target string
		want        bool
	}{
		{"https://x.com/login", "/login", true},
		{"https://x.com/login?a=b", "/login", true},
		{"https://x.com/app/login", "/login", true},
		{"https://x.com/dashboard", "/login", false},
		{"/login", "/login", true},
	}
	for _, c := range cases {
		if got := routeMatches(c.url, c.target); got != c.want {
			t.Errorf("routeMatches(%q,%q)=%v want %v", c.url, c.target, got, c.want)
		}
	}
}
