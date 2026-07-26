package mcp

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/spec"
)

const specDoc = `---
name: Login form
source: src/components/LoginForm.tsx
binding:
  - { kind: component, target: LoginForm }
  - { kind: route, target: /login }
invariants:
  - name: no-failed-requests
    query: "failed_request(S, _, _, _)"
    expect: absent
---

# Login form

<!-- browsernerd:invariant name=submit-region from:42 to:80 expect:present -->
Submit region invariant.
` + "```query" + `
slow_api(S, _, _, _)
` + "```" + `
<!-- browsernerd:end -->
`

func writeSpecDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "login.md"), []byte(specDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGetSpecsTool_DeliveryByLineRange(t *testing.T) {
	dir := writeSpecDir(t)
	tool := &GetSpecsTool{engine: setupTestEngine(t)}

	// Editing lines 50-60 of LoginForm.tsx should deliver the 42-80 invariant,
	// but NOT the frontmatter one (which has no line range).
	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"dir":  dir,
		"file": "src/components/LoginForm.tsx",
		"from": 50,
		"to":   60,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	m := res.(map[string]interface{})
	invs := m["invariants"].([]map[string]interface{})
	if len(invs) != 1 || invs[0]["name"] != "submit-region" {
		t.Fatalf("expected only submit-region invariant, got %+v", invs)
	}
}

func TestGetSpecsTool_DeliveryByComponent(t *testing.T) {
	dir := writeSpecDir(t)
	tool := &GetSpecsTool{engine: setupTestEngine(t)}

	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"dir":       dir,
		"component": "LoginForm",
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	m := res.(map[string]interface{})
	invs := m["invariants"].([]map[string]interface{})
	// Component binding matches the spec -> both invariants delivered.
	if len(invs) != 2 {
		t.Fatalf("expected 2 invariants for component, got %d: %+v", len(invs), invs)
	}
}

func TestCheckSpecsTool_Violation(t *testing.T) {
	dir := writeSpecDir(t)
	engine := setupTestEngine(t)

	// A failed request violates the "no-failed-requests" invariant.
	now := time.Now()
	if err := engine.AddFacts(context.Background(), []mangle.Fact{
		{Predicate: "net_request", Args: []interface{}{testSessionID, "r1", "GET", "https://api/x", "fetch", now.UnixMilli()}, Timestamp: now},
		{Predicate: "net_response", Args: []interface{}{testSessionID, "r1", int64(500), int64(50), int64(100)}, Timestamp: now},
	}); err != nil {
		t.Fatal(err)
	}

	tool := &CheckSpecsTool{engine: engine}
	res, err := tool.Execute(context.Background(), map[string]interface{}{"dir": dir})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	m := res.(map[string]interface{})
	if passed, _ := m["passed"].(bool); passed {
		t.Fatalf("expected spec check to fail, got: %+v", m)
	}
	violations := m["violations"].([]map[string]interface{})
	found := false
	for _, v := range violations {
		if v["name"] == "Login form/no-failed-requests" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the no-failed-requests violation among %+v", violations)
	}
	diag, ok := m["diagnosis"].(map[string]interface{})
	if !ok || diag["failed_request"] == nil {
		t.Fatalf("expected failed_request in diagnosis, got %+v", m["diagnosis"])
	}
}

func TestCheckSpecsTool_ScopedByLineRange(t *testing.T) {
	dir := writeSpecDir(t)
	engine := setupTestEngine(t)
	tool := &CheckSpecsTool{engine: engine}

	// Scope to lines 42-80: only the submit-region invariant (slow_api present)
	// is checked. No slow_api facts -> it's violated, and the frontmatter
	// invariant is out of scope.
	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"dir":  dir,
		"file": "src/components/LoginForm.tsx",
		"from": 42,
		"to":   80,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	m := res.(map[string]interface{})
	if checked := m["checked"].(int); checked != 1 {
		t.Fatalf("expected exactly 1 invariant checked in range, got %d", checked)
	}
}

func TestGetSpecsTool_ResolvesBindings(t *testing.T) {
	dir := writeSpecDir(t)
	engine := setupTestEngine(t)
	now := time.Now()
	if err := engine.AddFacts(context.Background(), []mangle.Fact{
		{Predicate: "react_component", Args: []interface{}{testSessionID, "f1", "LoginForm", "root"}, Timestamp: now},
		{Predicate: "dom_mapping", Args: []interface{}{testSessionID, "f1", "node-42"}, Timestamp: now},
	}); err != nil {
		t.Fatal(err)
	}

	tool := &GetSpecsTool{engine: engine}
	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"dir":        dir,
		"component":  "LoginForm",
		"session_id": testSessionID,
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	m := res.(map[string]interface{})
	rb, ok := m["resolved_bindings"].([]map[string]interface{})
	if !ok || len(rb) == 0 {
		t.Fatalf("expected resolved_bindings, got %+v", m["resolved_bindings"])
	}
	bindings := rb[0]["bindings"].([]bindingResolution)
	var componentPresent bool
	for _, b := range bindings {
		if b.Kind == "component" && b.Present && len(b.Nodes) == 1 && b.Nodes[0] == "node-42" {
			componentPresent = true
		}
	}
	if !componentPresent {
		t.Fatalf("expected LoginForm resolved to node-42, got %+v", bindings)
	}
}

func TestGetSpecsToolLoadsConfiguredGenericCorpus(t *testing.T) {
	root := t.TempDir()
	doc := `---
title: Checkout Experience
doc_type: feature-spec
subsystem: frontend
read_when: Working on checkout
binding:
  - { kind: route, target: /checkout }
---
# Checkout Experience

The checkout page displays a confirmation after payment succeeds.
`
	if err := os.WriteFile(filepath.Join(root, "checkout.md"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	enabled := true
	tool := &GetSpecsTool{
		engine: setupTestEngine(t),
		specsCfg: config.SpecsConfig{
			Enabled:         &enabled,
			Sources:         []config.SpecSourceConfig{{Name: "product", Roots: []string{root}}},
			MaxFiles:        10,
			MaxFileBytes:    1 << 20,
			MaxResults:      4,
			MaxExcerptBytes: 400,
		},
	}

	res, err := tool.Execute(context.Background(), map[string]interface{}{"route": "/checkout/review"})
	if err != nil {
		t.Fatal(err)
	}
	docs, ok := res.(map[string]interface{})["documents"].([]spec.Match)
	if !ok || len(docs) != 1 || docs[0].Title != "Checkout Experience" {
		t.Fatalf("expected configured generic spec match, got %+v", res)
	}
}

func TestBrowserActAttachesConfiguredSpecContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, "checkout.md"),
		[]byte("# Checkout\n\nPayment confirmation requirements."),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	enabled := true
	cfg := config.SpecsConfig{
		Enabled:         &enabled,
		Sources:         []config.SpecSourceConfig{{Name: "product", Roots: []string{root}}},
		MaxFiles:        10,
		MaxFileBytes:    1 << 20,
		MaxResults:      4,
		MaxExcerptBytes: 300,
	}
	tool := &BrowserActTool{
		sessions: browser.NewSessionManager(config.BrowserConfig{}, nil),
		engine:   setupTestEngine(t),
		specsCfg: cfg,
	}
	res, err := tool.Execute(context.Background(), map[string]interface{}{
		"operations": []interface{}{map[string]interface{}{"type": "sleep", "duration_ms": 0}},
		"spec_terms": []interface{}{"checkout", "payment"},
	})
	if err != nil {
		t.Fatal(err)
	}
	matches, ok := res.(map[string]interface{})["spec_context"].([]spec.Match)
	if !ok || len(matches) != 1 {
		t.Fatalf("expected post-action spec context, got %+v", res)
	}
}
