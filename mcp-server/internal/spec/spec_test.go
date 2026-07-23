package spec

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleSpec = `---
name: Login form
source: src/components/LoginForm.tsx
binding:
  - { kind: component, target: LoginForm }
  - { kind: route, target: /login }
invariants:
  - name: no-visible-errors
    query: "user_visible_error(S, _, _, _)"
    expect: absent
---

# Login form

Prose describing intent.

<!-- browsernerd:invariant name=submit-gated from:42 to:80 expect:present -->
Submit must stay disabled until email and password validate.
` + "```query" + `
submit_ok(S) :- form_ready(S).
` + "```" + `
<!-- browsernerd:end -->

More prose.

<!-- browsernerd:invariant name=other-file from:5 to:9 in:src/utils/valid.ts expect:absent -->
Validation must never throw.
` + "```query" + `
throws(S) :- console_event(S, "error", _, _).
` + "```" + `
<!-- browsernerd:end -->
`

func TestParseFrontmatterAndInline(t *testing.T) {
	spec, err := Parse("login.md", []byte(sampleSpec))
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if spec.Name != "Login form" {
		t.Errorf("name = %q, want 'Login form'", spec.Name)
	}
	if spec.Source != "src/components/LoginForm.tsx" {
		t.Errorf("source = %q", spec.Source)
	}
	if len(spec.Bindings) != 2 || spec.Bindings[0].Kind != "component" || spec.Bindings[1].Target != "/login" {
		t.Errorf("bindings = %+v", spec.Bindings)
	}

	// 1 frontmatter + 2 inline invariants.
	if len(spec.Invariants) != 3 {
		t.Fatalf("expected 3 invariants, got %d: %+v", len(spec.Invariants), spec.Invariants)
	}

	fm := spec.Invariants[0]
	if fm.Name != "no-visible-errors" || fm.Expect != "absent" || fm.Inline {
		t.Errorf("frontmatter invariant wrong: %+v", fm)
	}
	if fm.File != "src/components/LoginForm.tsx" {
		t.Errorf("frontmatter invariant should inherit source file, got %q", fm.File)
	}

	gated := spec.Invariants[1]
	if gated.Name != "submit-gated" || gated.From != 42 || gated.To != 80 || gated.Expect != "present" {
		t.Errorf("inline invariant meta wrong: %+v", gated)
	}
	if gated.Query != "submit_ok(S) :- form_ready(S)." {
		t.Errorf("inline query wrong: %q", gated.Query)
	}
	if gated.File != "src/components/LoginForm.tsx" {
		t.Errorf("inline invariant should default to spec source, got %q", gated.File)
	}

	other := spec.Invariants[2]
	if other.File != "src/utils/valid.ts" {
		t.Errorf("`in:` override not applied, got %q", other.File)
	}
}

func TestInvariantCovers(t *testing.T) {
	inv := Invariant{File: "src/components/LoginForm.tsx", From: 42, To: 80}

	cases := []struct {
		file     string
		from, to int
		want     bool
	}{
		{"src/components/LoginForm.tsx", 50, 60, true},  // inside
		{"src/components/LoginForm.tsx", 80, 90, true},  // overlaps end
		{"src/components/LoginForm.tsx", 10, 42, true},  // overlaps start
		{"src/components/LoginForm.tsx", 10, 20, false}, // before
		{"src/components/LoginForm.tsx", 90, 99, false}, // after
		{"LoginForm.tsx", 50, 60, true},                 // basename match
		{"other.tsx", 50, 60, false},                    // different file
		{"src/components/LoginForm.tsx", 0, 0, false},   // no range
	}
	for _, c := range cases {
		if got := inv.Covers(c.file, c.from, c.to); got != c.want {
			t.Errorf("Covers(%q, %d, %d) = %v, want %v", c.file, c.from, c.to, got, c.want)
		}
	}
}

func TestParseUnterminatedInvariant(t *testing.T) {
	bad := "# doc\n\n<!-- browsernerd:invariant name=x -->\nno close tag here\n"
	if _, err := Parse("bad.md", []byte(bad)); err == nil {
		t.Fatal("expected error for unterminated invariant block")
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.md"), []byte(sampleSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.md"), []byte("---\nname: B\n---\n# b\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	specs, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(specs) != 2 {
		t.Fatalf("expected 2 specs (recursive, .md only), got %d", len(specs))
	}
}

// TestParseCommittedExample guards the documented example spec from drifting
// out of sync with the parser.
func TestParseCommittedExample(t *testing.T) {
	path := filepath.Join("..", "..", "examples", "specs", "login-form.md")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	s, err := Parse(path, content)
	if err != nil {
		t.Fatalf("parse example: %v", err)
	}
	if s.Name != "Login form" {
		t.Errorf("name = %q", s.Name)
	}
	// 1 frontmatter + 3 inline invariants.
	if len(s.Invariants) != 4 {
		t.Fatalf("expected 4 invariants, got %d", len(s.Invariants))
	}
	// The `in:` override must retarget the validation invariant.
	var found bool
	for _, inv := range s.Invariants {
		if inv.Name == "validation-no-throw" {
			found = true
			if inv.File != "src/utils/validate.ts" {
				t.Errorf("in: override not applied: %q", inv.File)
			}
		}
	}
	if !found {
		t.Error("validation-no-throw invariant missing")
	}
}
