package spec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSourcesUsesGenericFrontmatterAndIndexOrdering(t *testing.T) {
	root := t.TempDir()
	specDir := filepath.Join(root, "docs", "specs")
	indexDir := filepath.Join(root, "docs", "indexes")
	if err := os.MkdirAll(specDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(indexDir, 0o755); err != nil {
		t.Fatal(err)
	}

	account := `---
title: Account Security
doc_type: feature-spec
subsystem: frontend
read_when: Editing the account route or authentication forms
tags: [account, authentication]
---
# Account Security

The account page must never expose credentials in logs.
`
	if err := os.WriteFile(filepath.Join(specDir, "account.md"), []byte(account), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(indexDir, "specs.md"),
		[]byte("[Account](../specs/account.md)\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	docs, errs := LoadSources([]Source{{
		Name:    "product",
		Roots:   []string{specDir},
		Indexes: []string{filepath.Join(indexDir, "specs.md")},
	}}, LoadOptions{MaxFiles: 10, MaxFileBytes: 1 << 20})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(docs) != 1 {
		t.Fatalf("expected one deduplicated document, got %d", len(docs))
	}
	if docs[0].Name != "Account Security" || docs[0].Corpus != "product" {
		t.Fatalf("unexpected document metadata: %+v", docs[0])
	}
	if docs[0].ReadWhen == "" || docs[0].Subsystem != "frontend" {
		t.Fatalf("generic metadata was not parsed: %+v", docs[0])
	}
}

func TestMatchSpecsRanksRouteAndTerms(t *testing.T) {
	docs := []Spec{
		{
			Name:      "Account",
			Title:     "Account Settings",
			Path:      "account.md",
			Corpus:    "product",
			ReadWhen:  "Working on the account route",
			Subsystem: "frontend",
			Bindings:  []Binding{{Kind: "route", Target: "/account"}},
			Body:      "Password changes require a confirmation toast and a successful API response.",
		},
		{Name: "Billing", Title: "Billing", Path: "billing.md", Corpus: "product", Body: "Invoices and payments."},
	}

	matches := MatchSpecs(docs, MatchInput{
		Route: "/account/security",
		Terms: []string{"password"},
		Max:   4,
	}, 400)
	if len(matches) != 1 {
		t.Fatalf("expected one relevant match, got %+v", matches)
	}
	if matches[0].Name != "Account" || matches[0].Score < 100 {
		t.Fatalf("route binding was not prioritized: %+v", matches[0])
	}
	if matches[0].Excerpt == "" {
		t.Fatal("expected a compact excerpt")
	}
}

func TestLoadSourcesHonorsFileBounds(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "large.md"), make([]byte, 128), 0o600); err != nil {
		t.Fatal(err)
	}

	docs, errs := LoadSources(
		[]Source{{Name: "bounded", Roots: []string{root}}},
		LoadOptions{MaxFiles: 2, MaxFileBytes: 64},
	)
	if len(docs) != 0 || len(errs) == 0 {
		t.Fatalf("expected oversized file to be rejected, docs=%d errs=%v", len(docs), errs)
	}
}
