package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
)

// ---------------------------------------------------------------------------
// selectRecentSessionFacts (resources.go)
// ---------------------------------------------------------------------------

func TestSelectRecentSessionFacts(t *testing.T) {
	// Helper: create a real Mangle engine with the production schema.
	makeEngine := func(t *testing.T) *mangle.Engine {
		t.Helper()
		cfg := config.MangleConfig{
			Enable:          true,
			SchemaPath:      "../../schemas/browser.mg",
			FactBufferLimit: 2000,
		}
		engine, err := mangle.NewEngine(cfg)
		if err != nil {
			t.Fatalf("NewEngine: %v", err)
		}
		return engine
	}

	// pushDomNodes pushes N dom_node facts for the given sessionID.
	pushDomNodes := func(t *testing.T, engine *mangle.Engine, sessionID string, n int) {
		t.Helper()
		facts := make([]mangle.Fact, n)
		for i := 0; i < n; i++ {
			facts[i] = mangle.Fact{
				Predicate: "dom_node",
				Args:      []interface{}{sessionID, fmt.Sprintf("n%d", i), "div", fmt.Sprintf("text%d", i), "root"},
				Timestamp: time.Now().Add(time.Duration(i) * time.Millisecond),
			}
		}
		if err := engine.AddFacts(context.Background(), facts); err != nil {
			t.Fatalf("AddFacts: %v", err)
		}
	}

	t.Run("nil engine returns empty", func(t *testing.T) {
		got := selectRecentSessionFacts(nil, "sess1", "", 10)
		if len(got) != 0 {
			t.Fatalf("expected empty slice, got %d facts", len(got))
		}
	})

	t.Run("empty sessionID returns empty", func(t *testing.T) {
		engine := makeEngine(t)
		got := selectRecentSessionFacts(engine, "", "", 10)
		if len(got) != 0 {
			t.Fatalf("expected empty slice, got %d facts", len(got))
		}
	})

	t.Run("limit <= 0 returns empty", func(t *testing.T) {
		engine := makeEngine(t)
		got := selectRecentSessionFacts(engine, "sess1", "", 0)
		if len(got) != 0 {
			t.Fatalf("expected empty slice for limit=0, got %d", len(got))
		}
		got = selectRecentSessionFacts(engine, "sess1", "", -5)
		if len(got) != 0 {
			t.Fatalf("expected empty slice for limit=-5, got %d", len(got))
		}
	})

	t.Run("filters by sessionID", func(t *testing.T) {
		engine := makeEngine(t)
		pushDomNodes(t, engine, "sessA", 3)
		pushDomNodes(t, engine, "sessB", 2)

		got := selectRecentSessionFacts(engine, "sessA", "dom_node", 100)
		for _, f := range got {
			if fmt.Sprintf("%v", f.Args[0]) != "sessA" {
				t.Fatalf("expected all facts for sessA, got arg0=%v", f.Args[0])
			}
		}
		if len(got) != 3 {
			t.Fatalf("expected 3 facts for sessA, got %d", len(got))
		}
	})

	t.Run("filters by predicate", func(t *testing.T) {
		engine := makeEngine(t)
		pushDomNodes(t, engine, "sess1", 3)

		got := selectRecentSessionFacts(engine, "sess1", "dom_node", 100)
		if len(got) != 3 {
			t.Fatalf("expected 3 dom_node facts, got %d", len(got))
		}

		got = selectRecentSessionFacts(engine, "sess1", "nonexistent_predicate", 100)
		if len(got) != 0 {
			t.Fatalf("expected 0 facts for nonexistent predicate, got %d", len(got))
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		engine := makeEngine(t)
		pushDomNodes(t, engine, "sess1", 10)

		got := selectRecentSessionFacts(engine, "sess1", "dom_node", 3)
		if len(got) != 3 {
			t.Fatalf("expected 3 facts (limit), got %d", len(got))
		}
	})

	t.Run("results in chronological order", func(t *testing.T) {
		engine := makeEngine(t)
		pushDomNodes(t, engine, "sess1", 5)

		got := selectRecentSessionFacts(engine, "sess1", "dom_node", 100)
		if len(got) < 2 {
			t.Fatalf("need at least 2 facts to verify order, got %d", len(got))
		}
		// dom_node args[1] is NodeId: n0, n1, n2, ...
		for i := 1; i < len(got); i++ {
			prev := fmt.Sprintf("%v", got[i-1].Args[1])
			curr := fmt.Sprintf("%v", got[i].Args[1])
			if prev >= curr {
				t.Fatalf("expected chronological order, but got[%d].Args[1]=%s >= got[%d].Args[1]=%s",
					i-1, prev, i, curr)
			}
		}
	})

	t.Run("no predicate filter returns all session facts", func(t *testing.T) {
		engine := makeEngine(t)
		pushDomNodes(t, engine, "sess1", 4)

		got := selectRecentSessionFacts(engine, "sess1", "", 100)
		if len(got) < 4 {
			t.Fatalf("expected at least 4 facts with no predicate filter, got %d", len(got))
		}
	})
}

// ---------------------------------------------------------------------------
// argString (resources.go)
// ---------------------------------------------------------------------------

func TestArgString(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want string
	}{
		{"nil", nil, ""},
		{"string", "hello", "hello"},
		{"string slice first", []string{"a", "b"}, "a"},
		{"string slice empty", []string{}, ""},
		{"int via Sprintf", 42, "42"},
		{"bool via Sprintf", true, "true"},
		{"float via Sprintf", 3.14, "3.14"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := argString(tt.in)
			if got != tt.want {
				t.Errorf("argString(%#v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// min (resources.go)
// ---------------------------------------------------------------------------

func TestMin(t *testing.T) {
	tests := []struct {
		name string
		a, b int
		want int
	}{
		{"a < b", 2, 5, 2},
		{"a > b", 7, 3, 3},
		{"a == b", 4, 4, 4},
		{"negatives", -3, -1, -3},
		{"zero", 0, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := min(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("min(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// classifyJSError (helpers.go)
// ---------------------------------------------------------------------------

func TestClassifyJSError_Resources(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil error", nil, ""},
		{"context deadline exceeded", errors.New("context deadline exceeded"), "timeout"},
		{"SyntaxError", errors.New("SyntaxError: blah"), "syntax"},
		{"ReferenceError", errors.New("ReferenceError: x is not defined"), "runtime"},
		{"TypeError", errors.New("TypeError: Cannot read property"), "runtime"},
		{"Promise rejection", errors.New("Promise rejection"), "async"},
		{"SecurityError cross-origin", errors.New("SecurityError: cross-origin"), "security"},
		{"random error", errors.New("some random error"), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyJSError(tt.err)
			if got != tt.want {
				t.Errorf("classifyJSError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// formatJSError (helpers.go)
// ---------------------------------------------------------------------------

func TestFormatJSError_Resources(t *testing.T) {
	longMsg := strings.Repeat("x", 250)

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"ReferenceError extracted", errors.New("wrapper ReferenceError: x is not defined"), "ReferenceError:x is not defined"},
		{"TypeError extracted", errors.New("wrapper TypeError: Cannot read"), "TypeError:Cannot read"},
		{"SyntaxError extracted", errors.New("wrapper SyntaxError: Unexpected"), "SyntaxError:Unexpected"},
		{"timeout replaced", errors.New("context deadline exceeded"), "Script execution timed out"},
		{"long string truncated", errors.New(longMsg), longMsg[:197] + "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatJSError(tt.err)
			if got != tt.want {
				t.Errorf("formatJSError(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// looksLikeCSSSelector (helpers.go)
// ---------------------------------------------------------------------------

func TestLooksLikeCSSSelector_Resources(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"tag.class", "button.class1", true},
		{"tag.multi-class", "div.my-class.other", true},
		{".class-only", ".class-only", true},
		{"no dot", "noclass", false},
		{"uppercase tag", "UPPER.case", false},
		{"empty class part", "button.", false},
		{"special char in class", "button.Invalid!", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeCSSSelector(tt.input)
			if got != tt.want {
				t.Errorf("looksLikeCSSSelector(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// splitDuplicateSuffix (helpers.go)
// ---------------------------------------------------------------------------

func TestSplitDuplicateSuffix_Resources(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantBase  string
		wantIdx   int
		wantSplit bool
	}{
		{"class ref with _2", "button.class_2", "button.class", 2, true},
		{"no special char", "simple", "simple", 0, false},
		{"underscore no dot/bracket", "no_suffix", "no_suffix", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base, idx, split := splitDuplicateSuffix(tt.input)
			if base != tt.wantBase || idx != tt.wantIdx || split != tt.wantSplit {
				t.Errorf("splitDuplicateSuffix(%q) = (%q, %d, %v), want (%q, %d, %v)",
					tt.input, base, idx, split,
					tt.wantBase, tt.wantIdx, tt.wantSplit)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildEscapedClassSelector (helpers.go)
// ---------------------------------------------------------------------------

func TestBuildEscapedClassSelector_Resources(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantSel  string
		wantIdx  int
		wantDup  bool
		wantOK   bool
	}{
		{"div.class1.class2", "div.class1.class2", "div.class1.class2", 0, false, true},
		{"no dots rejected", "nodots", "", 0, false, false},
		{"empty string", "", "", 0, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel, idx, dup, ok := buildEscapedClassSelector(tt.input)
			if sel != tt.wantSel || idx != tt.wantIdx || dup != tt.wantDup || ok != tt.wantOK {
				t.Errorf("buildEscapedClassSelector(%q) = (%q, %d, %v, %v), want (%q, %d, %v, %v)",
					tt.input, sel, idx, dup, ok,
					tt.wantSel, tt.wantIdx, tt.wantDup, tt.wantOK)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseIndexedTagRef (helpers.go)
// ---------------------------------------------------------------------------

func TestParseIndexedTagRef_Resources(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantTag string
		wantIdx int
		wantOK  bool
	}{
		{"button[3]", "button[3]", "button", 3, true},
		{"div[0]", "div[0]", "div", 0, true},
		{"no index", "noindex", "", 0, false},
		{"empty tag [3]", "[3]", "", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag, idx, ok := parseIndexedTagRef(tt.input)
			if tag != tt.wantTag || idx != tt.wantIdx || ok != tt.wantOK {
				t.Errorf("parseIndexedTagRef(%q) = (%q, %d, %v), want (%q, %d, %v)",
					tt.input, tag, idx, ok,
					tt.wantTag, tt.wantIdx, tt.wantOK)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// getStringArg / getStringFromMap (helpers.go)
// ---------------------------------------------------------------------------

func TestGetStringArg_Resources(t *testing.T) {
	tests := []struct {
		name string
		args map[string]interface{}
		key  string
		want string
	}{
		{"string value", map[string]interface{}{"k": "val"}, "k", "val"},
		{"int value", map[string]interface{}{"k": 99}, "k", "99"},
		{"missing key", map[string]interface{}{"other": "v"}, "k", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getStringArg(tt.args, tt.key)
			if got != tt.want {
				t.Errorf("getStringArg(%v, %q) = %q, want %q", tt.args, tt.key, got, tt.want)
			}
			// getStringFromMap should produce the same result
			got2 := getStringFromMap(tt.args, tt.key)
			if got2 != tt.want {
				t.Errorf("getStringFromMap(%v, %q) = %q, want %q", tt.args, tt.key, got2, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// getIntArg (helpers.go)
// ---------------------------------------------------------------------------

func TestGetIntArg_Resources(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		fallback int
		want     int
	}{
		{"int", map[string]interface{}{"k": 42}, "k", 0, 42},
		{"int64", map[string]interface{}{"k": int64(100)}, "k", 0, 100},
		{"float64", map[string]interface{}{"k": float64(7.9)}, "k", 0, 7},
		{"missing key", map[string]interface{}{}, "k", 55, 55},
		{"string fallback", map[string]interface{}{"k": "notnum"}, "k", 10, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getIntArg(tt.args, tt.key, tt.fallback)
			if got != tt.want {
				t.Errorf("getIntArg = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// getBoolArg (helpers.go)
// ---------------------------------------------------------------------------

func TestGetBoolArg_Resources(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		fallback bool
		want     bool
	}{
		{"true", map[string]interface{}{"k": true}, "k", false, true},
		{"false", map[string]interface{}{"k": false}, "k", true, false},
		{"missing uses fallback", map[string]interface{}{}, "k", true, true},
		{"non-bool uses fallback", map[string]interface{}{"k": "yes"}, "k", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getBoolArg(tt.args, tt.key, tt.fallback)
			if got != tt.want {
				t.Errorf("getBoolArg = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// sanitizeAriaLabel (helpers.go)
// ---------------------------------------------------------------------------

func TestSanitizeAriaLabel_Resources(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"alphanumeric", "Submit", "Submit"},
		{"special chars become underscore", "Save & Continue!", "Save___Continue_"},
		{"truncated at 40", strings.Repeat("a", 50), strings.Repeat("a", 40)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeAriaLabel(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeAriaLabel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// escapeAttributeValue (helpers.go)
// ---------------------------------------------------------------------------

func TestEscapeAttributeValue_Resources(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"quotes escaped", `say "hi"`, `say \"hi\"`},
		{"backslashes escaped", `a\b`, `a\\b`},
		{"both", `"a\b"`, `\"a\\b\"`},
		{"plain", "hello", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeAttributeValue(tt.input)
			if got != tt.want {
				t.Errorf("escapeAttributeValue(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// escapeCSSSelector (helpers.go)
// ---------------------------------------------------------------------------

func TestEscapeCSSSelector_Resources(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"dot escaped", "a.b", `a\.b`},
		{"hash escaped", "a#b", `a\#b`},
		{"brackets escaped", "a[0]", `a\[0\]`},
		{"plain", "hello", "hello"},
		{"colon escaped", "ns:el", `ns\:el`},
		{"space escaped", "a b", `a\ b`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeCSSSelector(tt.input)
			if got != tt.want {
				t.Errorf("escapeCSSSelector(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildRowIndexSelectors (helpers.go)
// ---------------------------------------------------------------------------

func TestBuildRowIndexSelectors_Resources(t *testing.T) {
	t.Run("non-empty produces 12 selectors", func(t *testing.T) {
		sel := buildRowIndexSelectors("5")
		if len(sel) != 12 {
			t.Fatalf("expected 12 selectors, got %d", len(sel))
		}
		// Spot-check a known selector
		found := false
		for _, s := range sel {
			if s == `[role="row"][aria-rowindex="5"]` {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("missing expected aria-rowindex selector")
		}
	})

	t.Run("empty returns nil", func(t *testing.T) {
		sel := buildRowIndexSelectors("")
		if sel != nil {
			t.Fatalf("expected nil, got %v", sel)
		}
	})

	t.Run("whitespace only returns nil", func(t *testing.T) {
		sel := buildRowIndexSelectors("   ")
		if sel != nil {
			t.Fatalf("expected nil for whitespace, got %v", sel)
		}
	})
}

// ---------------------------------------------------------------------------
// buildRowKeySelectors (helpers.go)
// ---------------------------------------------------------------------------

func TestBuildRowKeySelectors_Resources(t *testing.T) {
	t.Run("non-empty produces 18 selectors", func(t *testing.T) {
		sel := buildRowKeySelectors("org-123")
		if len(sel) != 18 {
			t.Fatalf("expected 18 selectors, got %d", len(sel))
		}
		// Spot-check
		found := false
		for _, s := range sel {
			if s == `[role="row"][data-row-id="org-123"]` {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("missing expected data-row-id selector")
		}
	})

	t.Run("empty returns nil", func(t *testing.T) {
		sel := buildRowKeySelectors("")
		if sel != nil {
			t.Fatalf("expected nil, got %v", sel)
		}
	})

	t.Run("whitespace only returns nil", func(t *testing.T) {
		sel := buildRowKeySelectors("   ")
		if sel != nil {
			t.Fatalf("expected nil for whitespace, got %v", sel)
		}
	})
}

// ---------------------------------------------------------------------------
// buildSelectorFromFingerprintClasses (helpers.go)
// ---------------------------------------------------------------------------

func TestBuildSelectorFromFingerprintClasses(t *testing.T) {
	tests := []struct {
		name string
		fp   *browser.ElementFingerprint
		want string
	}{
		{
			name: "nil fingerprint",
			fp:   nil,
			want: "",
		},
		{
			name: "empty classes",
			fp:   &browser.ElementFingerprint{TagName: "div", Classes: []string{}},
			want: "",
		},
		{
			name: "empty tag",
			fp:   &browser.ElementFingerprint{TagName: "", Classes: []string{"cls"}},
			want: "",
		},
		{
			name: "tag with one class",
			fp:   &browser.ElementFingerprint{TagName: "button", Classes: []string{"primary"}},
			want: "button.primary",
		},
		{
			name: "tag with two classes",
			fp:   &browser.ElementFingerprint{TagName: "div", Classes: []string{"flex", "items-center"}},
			want: "div.flex.items-center",
		},
		{
			name: "tag with three classes",
			fp:   &browser.ElementFingerprint{TagName: "span", Classes: []string{"a", "b", "c"}},
			want: "span.a.b.c",
		},
		{
			name: "more than 3 classes truncated to 3",
			fp:   &browser.ElementFingerprint{TagName: "div", Classes: []string{"a", "b", "c", "d", "e"}},
			want: "div.a.b.c",
		},
		{
			name: "classes with special chars are escaped",
			fp:   &browser.ElementFingerprint{TagName: "div", Classes: []string{"hover:bg-blue"}},
			want: `div.hover\:bg-blue`,
		},
		{
			name: "empty class strings skipped",
			fp:   &browser.ElementFingerprint{TagName: "div", Classes: []string{"", "real"}},
			want: "div.real",
		},
		{
			name: "all empty classes",
			fp:   &browser.ElementFingerprint{TagName: "div", Classes: []string{"", " ", "  "}},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSelectorFromFingerprintClasses(tt.fp)
			if got != tt.want {
				t.Errorf("buildSelectorFromFingerprintClasses() = %q, want %q", got, tt.want)
			}
		})
	}
}
