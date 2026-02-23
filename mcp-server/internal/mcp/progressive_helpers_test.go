package mcp

import (
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// asInt
// ---------------------------------------------------------------------------

func TestAsInt(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want int
	}{
		{"int", 42, 42},
		{"int8", int8(7), 7},
		{"int16", int16(300), 300},
		{"int32", int32(100000), 100000},
		{"int64", int64(9999999), 9999999},
		{"float32", float32(3.9), 3},
		{"float64", float64(7.7), 7},
		{"string integer", "42", 42},
		{"string float", "3.14", 3},
		{"empty string", "", 0},
		{"nil", nil, 0},
		{"bool true", true, 0},
		{"bool false", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := asInt(tt.in)
			if got != tt.want {
				t.Errorf("asInt(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// asInt64
// ---------------------------------------------------------------------------

func TestAsInt64(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want int64
	}{
		{"int", 42, 42},
		{"int8", int8(7), 7},
		{"int16", int16(300), 300},
		{"int32", int32(100000), 100000},
		{"int64", int64(9999999), 9999999},
		{"float32", float32(3.9), 3},
		{"float64", float64(7.7), 7},
		{"string integer", "42", 42},
		{"string float", "3.14", 3},
		{"empty string", "", 0},
		{"nil", nil, 0},
		{"bool true", true, 0},
		{"bool false", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := asInt64(tt.in)
			if got != tt.want {
				t.Errorf("asInt64(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildObserveSummary
// ---------------------------------------------------------------------------

func TestBuildObserveSummary(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]interface{}
		contains []string // each must appear in result
		excludes []string // each must NOT appear in result
		exact    string   // if non-empty, exact match
	}{
		{
			name:  "empty map",
			data:  map[string]interface{}{},
			exact: "observation complete",
		},
		{
			name: "state loading true",
			data: map[string]interface{}{
				"state": map[string]interface{}{"loading": true},
			},
			contains: []string{"loading=true"},
		},
		{
			name: "diagnostics error",
			data: map[string]interface{}{
				"diagnostics": map[string]interface{}{"status": "error"},
			},
			contains: []string{"diag=error"},
		},
		{
			name: "diagnostics ok excluded",
			data: map[string]interface{}{
				"diagnostics": map[string]interface{}{"status": "ok"},
			},
			exact: "observation complete",
		},
		{
			name: "toasts error_count",
			data: map[string]interface{}{
				"toasts": map[string]interface{}{"error_count": 3},
			},
			contains: []string{"toast_err=3"},
		},
		{
			name: "nav_counts total",
			data: map[string]interface{}{
				"nav_counts": map[string]interface{}{"total": 15},
			},
			contains: []string{"links=15"},
		},
		{
			name: "nav nested counts",
			data: map[string]interface{}{
				"nav": map[string]interface{}{
					"counts": map[string]interface{}{"total": 9},
				},
			},
			contains: []string{"links="},
		},
		{
			name: "interactive_summary total",
			data: map[string]interface{}{
				"interactive_summary": map[string]interface{}{"total": 8},
			},
			contains: []string{"interactive=8"},
		},
		{
			name: "interactive nested summary",
			data: map[string]interface{}{
				"interactive": map[string]interface{}{
					"summary": map[string]interface{}{"total": 12},
				},
			},
			contains: []string{"interactive="},
		},
		{
			name: "action_candidate_count",
			data: map[string]interface{}{
				"action_candidate_count": 5,
			},
			contains: []string{"candidates=5"},
		},
		{
			name: "action_candidates as []map",
			data: map[string]interface{}{
				"action_candidates": []map[string]interface{}{
					{"action": "click"},
					{"action": "type"},
				},
			},
			contains: []string{"candidates="},
		},
		{
			name: "full composite",
			data: map[string]interface{}{
				"state":               map[string]interface{}{"loading": false},
				"diagnostics":         map[string]interface{}{"status": "warning"},
				"toasts":              map[string]interface{}{"error_count": 1},
				"nav_counts":          map[string]interface{}{"total": 10},
				"interactive_summary": map[string]interface{}{"total": 5},
			},
			contains: []string{"loading=false", "diag=warning", "toast_err=1", "links=10", "interactive=5"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildObserveSummary(tt.data)
			if tt.exact != "" {
				if got != tt.exact {
					t.Fatalf("buildObserveSummary() = %q, want exact %q", got, tt.exact)
				}
				return
			}
			for _, c := range tt.contains {
				if !strings.Contains(got, c) {
					t.Errorf("buildObserveSummary() = %q, expected to contain %q", got, c)
				}
			}
			for _, e := range tt.excludes {
				if strings.Contains(got, e) {
					t.Errorf("buildObserveSummary() = %q, expected NOT to contain %q", got, e)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildMangleSummary
// ---------------------------------------------------------------------------

func TestBuildMangleSummary(t *testing.T) {
	tests := []struct {
		name string
		op   string
		data map[string]interface{}
		want string
	}{
		{
			name: "query with []map results",
			op:   "query",
			data: map[string]interface{}{
				"results": []map[string]interface{}{{"a": 1}, {"b": 2}},
			},
			want: "query returned 2 result(s)",
		},
		{
			name: "query with []interface results",
			op:   "query",
			data: map[string]interface{}{
				"results": []interface{}{"x", "y", "z"},
			},
			want: "query returned 3 result(s)",
		},
		{
			name: "query no results",
			op:   "query",
			data: map[string]interface{}{},
			want: "query completed",
		},
		{
			name: "read with facts",
			op:   "read",
			data: map[string]interface{}{
				"facts": []interface{}{"f1", "f2"},
			},
			want: "read 2 fact(s)",
		},
		{
			name: "read with count",
			op:   "read",
			data: map[string]interface{}{
				"count": 7,
			},
			want: "read 7 fact(s)",
		},
		{
			name: "push accepted 5",
			op:   "push",
			data: map[string]interface{}{
				"accepted": 5,
			},
			want: "pushed 5 fact(s)",
		},
		{
			name: "submit_rule success",
			op:   "submit_rule",
			data: map[string]interface{}{
				"success": true,
			},
			want: "rule submitted",
		},
		{
			name: "submit_rule failure",
			op:   "submit_rule",
			data: map[string]interface{}{
				"success": false,
			},
			want: "rule submission failed",
		},
		{
			name: "evaluate with results",
			op:   "evaluate",
			data: map[string]interface{}{
				"results": []interface{}{1, 2, 3},
			},
			want: "evaluated 3 result(s)",
		},
		{
			name: "temporal with results",
			op:   "temporal",
			data: map[string]interface{}{
				"results": []interface{}{"a"},
			},
			want: "temporal query returned 1 result(s)",
		},
		{
			name: "subscribe matched",
			op:   "subscribe",
			data: map[string]interface{}{
				"matched": true,
			},
			want: "subscription matched",
		},
		{
			name: "subscribe not matched",
			op:   "subscribe",
			data: map[string]interface{}{},
			want: "subscription completed",
		},
		{
			name: "await_fact matched",
			op:   "await_fact",
			data: map[string]interface{}{
				"matched": true,
			},
			want: "fact matched",
		},
		{
			name: "await_fact not matched",
			op:   "await_fact",
			data: map[string]interface{}{},
			want: "await completed",
		},
		{
			name: "await_conditions all_matched",
			op:   "await_conditions",
			data: map[string]interface{}{
				"all_matched": true,
			},
			want: "all conditions matched",
		},
		{
			name: "await_conditions not matched",
			op:   "await_conditions",
			data: map[string]interface{}{},
			want: "await conditions completed",
		},
		{
			name: "unknown operation",
			op:   "unknown_op",
			data: map[string]interface{}{},
			want: "unknown_op completed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMangleSummary(tt.op, tt.data)
			if got != tt.want {
				t.Errorf("buildMangleSummary(%q, ...) = %q, want %q", tt.op, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// suggestObserveNextStep
// ---------------------------------------------------------------------------

func TestSuggestObserveNextStep(t *testing.T) {
	sid := "test-session"

	tests := []struct {
		name            string
		data            map[string]interface{}
		mode            string
		view            string
		recommendations []map[string]interface{}
		wantTool        string
		wantContains    string // substring in args
	}{
		{
			name: "loading true suggests await_stable",
			data: map[string]interface{}{
				"state": map[string]interface{}{"loading": true},
			},
			wantTool:     "browser-act",
			wantContains: "await_stable",
		},
		{
			name: "diagnostics error suggests why_failed",
			data: map[string]interface{}{
				"diagnostics": map[string]interface{}{"status": "error"},
			},
			wantTool:     "browser-reason",
			wantContains: "why_failed",
		},
		{
			name: "diagnostics warning suggests health",
			data: map[string]interface{}{
				"diagnostics": map[string]interface{}{"status": "warning"},
			},
			wantTool:     "browser-reason",
			wantContains: "health",
		},
		{
			name: "recommendations used first",
			data: map[string]interface{}{},
			recommendations: []map[string]interface{}{
				{
					"tool":   "browser-act",
					"args":   map[string]interface{}{"operations": []string{"click"}},
					"reason": "do it",
				},
			},
			wantTool: "browser-act",
		},
		{
			name: "interactive total > 0 suggests next_best_action",
			data: map[string]interface{}{
				"interactive": map[string]interface{}{
					"summary": map[string]interface{}{"total": 5},
				},
			},
			wantTool:     "browser-reason",
			wantContains: "next_best_action",
		},
		{
			name: "interactive total 0 suggests hidden mode",
			data: map[string]interface{}{
				"interactive": map[string]interface{}{
					"summary": map[string]interface{}{"total": 0},
				},
			},
			wantTool:     "browser-observe",
			wantContains: "hidden",
		},
		{
			name: "interactive_summary total > 0",
			data: map[string]interface{}{
				"interactive_summary": map[string]interface{}{"total": 3},
			},
			wantTool:     "browser-reason",
			wantContains: "next_best_action",
		},
		{
			name: "interactive_summary total 0",
			data: map[string]interface{}{
				"interactive_summary": map[string]interface{}{"total": 0},
			},
			wantTool:     "browser-observe",
			wantContains: "hidden",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := suggestObserveNextStep(sid, tt.data, tt.mode, tt.view, tt.recommendations)
			if got == nil {
				t.Fatal("suggestObserveNextStep returned nil")
			}
			tool, _ := got["tool"].(string)
			if tool != tt.wantTool {
				t.Errorf("tool = %q, want %q", tool, tt.wantTool)
			}
			if tt.wantContains != "" {
				args, _ := got["args"].(map[string]interface{})
				if !argsContains(args, tt.wantContains) {
					t.Errorf("args %v does not contain %q", args, tt.wantContains)
				}
			}
		})
	}
}

// argsContains checks whether the serialized args map contains the given substring.
func argsContains(args map[string]interface{}, substr string) bool {
	// Walk keys and string values for a simple substring check.
	for k, v := range args {
		if strings.Contains(strings.ToLower(k), substr) {
			return true
		}
		switch val := v.(type) {
		case string:
			if strings.Contains(strings.ToLower(val), substr) {
				return true
			}
		case []map[string]interface{}:
			for _, inner := range val {
				for _, iv := range inner {
					if s, ok := iv.(string); ok && strings.Contains(strings.ToLower(s), substr) {
						return true
					}
				}
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// compactInteractiveData
// ---------------------------------------------------------------------------

func TestCompactInteractiveData(t *testing.T) {
	tests := []struct {
		name         string
		data         map[string]interface{}
		maxItems     int
		wantTrunc    bool
		wantElemLen  int
		wantSummary  bool
	}{
		{
			name: "elements within limit",
			data: map[string]interface{}{
				"summary":  "ok",
				"elements": []interface{}{"a", "b"},
			},
			maxItems:    5,
			wantTrunc:   false,
			wantElemLen: 2,
			wantSummary: true,
		},
		{
			name: "elements exceed limit",
			data: map[string]interface{}{
				"summary":  "big",
				"elements": []interface{}{"a", "b", "c", "d", "e"},
			},
			maxItems:    3,
			wantTrunc:   true,
			wantElemLen: 3,
			wantSummary: true,
		},
		{
			name:        "no elements",
			data:        map[string]interface{}{},
			maxItems:    5,
			wantTrunc:   false,
			wantElemLen: -1, // no "elements" key
			wantSummary: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compactInteractiveData(tt.data, tt.maxItems)
			if tt.wantSummary {
				if _, ok := got["summary"]; !ok {
					t.Error("expected summary key")
				}
			}
			if tt.wantElemLen >= 0 {
				elems, ok := got["elements"].([]interface{})
				if !ok {
					t.Fatalf("expected elements as []interface{}")
				}
				if len(elems) != tt.wantElemLen {
					t.Errorf("elements len = %d, want %d", len(elems), tt.wantElemLen)
				}
				trunc, _ := got["truncated"].(bool)
				if trunc != tt.wantTrunc {
					t.Errorf("truncated = %v, want %v", trunc, tt.wantTrunc)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// compactHiddenData
// ---------------------------------------------------------------------------

func TestCompactHiddenData(t *testing.T) {
	tests := []struct {
		name      string
		data      map[string]interface{}
		maxItems  int
		wantCount int
		wantTrunc bool
	}{
		{
			name: "with hidden_elements and summary",
			data: map[string]interface{}{
				"hidden_elements": []interface{}{"a", "b", "c"},
				"summary":         "3 hidden",
			},
			maxItems:  2,
			wantCount: 3,
			wantTrunc: true,
		},
		{
			name: "within limit",
			data: map[string]interface{}{
				"hidden_elements": []interface{}{"a"},
			},
			maxItems:  5,
			wantCount: 1,
			wantTrunc: false,
		},
		{
			name:      "empty data",
			data:      map[string]interface{}{},
			maxItems:  5,
			wantCount: -1,
			wantTrunc: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compactHiddenData(tt.data, tt.maxItems)
			if tt.wantCount >= 0 {
				count, _ := got["count"].(int)
				if count != tt.wantCount {
					t.Errorf("count = %d, want %d", count, tt.wantCount)
				}
				trunc, _ := got["truncated"].(bool)
				if trunc != tt.wantTrunc {
					t.Errorf("truncated = %v, want %v", trunc, tt.wantTrunc)
				}
			}
			if _, hasSummary := tt.data["summary"]; hasSummary {
				if _, ok := got["summary"]; !ok {
					t.Error("expected summary to be preserved")
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// compactToastData
// ---------------------------------------------------------------------------

func TestCompactToastData(t *testing.T) {
	tests := []struct {
		name           string
		data           map[string]interface{}
		maxItems       int
		wantStatus     bool
		wantSummary    bool
		wantErrCount   bool
		wantToastCount int
		wantRepeated   bool
	}{
		{
			name: "status and summary preserved",
			data: map[string]interface{}{
				"status":  "active",
				"summary": "2 toasts",
			},
			maxItems:    5,
			wantStatus:  true,
			wantSummary: true,
		},
		{
			name: "error_count preserved",
			data: map[string]interface{}{
				"error_count": 3,
			},
			maxItems:     5,
			wantErrCount: true,
		},
		{
			name: "toasts as []map",
			data: map[string]interface{}{
				"toasts": []map[string]interface{}{
					{"text": "a"}, {"text": "b"}, {"text": "c"},
					{"text": "d"}, {"text": "e"}, {"text": "f"},
				},
			},
			maxItems:       3,
			wantToastCount: 6,
		},
		{
			name: "toasts as []interface",
			data: map[string]interface{}{
				"toasts": []interface{}{
					map[string]interface{}{"text": "a"},
					map[string]interface{}{"text": "b"},
				},
			},
			maxItems:       5,
			wantToastCount: 2,
		},
		{
			name: "repeated_errors",
			data: map[string]interface{}{
				"repeated_errors": []interface{}{"err1", "err2"},
			},
			maxItems:     5,
			wantRepeated: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := compactToastData(tt.data, tt.maxItems)
			if tt.wantStatus {
				if _, ok := got["status"]; !ok {
					t.Error("expected status key")
				}
			}
			if tt.wantSummary {
				if _, ok := got["summary"]; !ok {
					t.Error("expected summary key")
				}
			}
			if tt.wantErrCount {
				if _, ok := got["error_count"]; !ok {
					t.Error("expected error_count key")
				}
			}
			if tt.wantToastCount > 0 {
				tc, _ := got["toast_count"].(int)
				if tc != tt.wantToastCount {
					t.Errorf("toast_count = %d, want %d", tc, tt.wantToastCount)
				}
			}
			if tt.wantRepeated {
				if _, ok := got["repeated_errors"]; !ok {
					t.Error("expected repeated_errors key")
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// countHiddenElements
// ---------------------------------------------------------------------------

func TestCountHiddenElements(t *testing.T) {
	tests := []struct {
		name string
		data map[string]interface{}
		want int
	}{
		{"nil map", nil, 0},
		{"empty map", map[string]interface{}{}, 0},
		{"with elements", map[string]interface{}{
			"hidden_elements": []interface{}{"a", "b", "c"},
		}, 3},
		{"wrong type", map[string]interface{}{
			"hidden_elements": "not a slice",
		}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.data == nil {
				tt.data = map[string]interface{}{}
			}
			got := countHiddenElements(tt.data)
			if got != tt.want {
				t.Errorf("countHiddenElements() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// severityLabel
// ---------------------------------------------------------------------------

func TestSeverityLabel(t *testing.T) {
	tests := []struct {
		score int
		want  string
	}{
		{95, "critical"},
		{90, "critical"},
		{80, "high"},
		{75, "high"},
		{60, "medium"},
		{55, "medium"},
		{30, "low"},
		{0, "low"},
	}
	for _, tt := range tests {
		t.Run(strconv.Itoa(tt.score), func(t *testing.T) {
			got := severityLabel(tt.score)
			if got != tt.want {
				t.Errorf("severityLabel(%d) = %q, want %q", tt.score, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ternaryStatus
// ---------------------------------------------------------------------------

func TestTernaryStatus(t *testing.T) {
	if got := ternaryStatus(true, "yes", "no"); got != "yes" {
		t.Errorf("ternaryStatus(true) = %q, want yes", got)
	}
	if got := ternaryStatus(false, "yes", "no"); got != "no" {
		t.Errorf("ternaryStatus(false) = %q, want no", got)
	}
}

// ---------------------------------------------------------------------------
// suggestInputValue
// ---------------------------------------------------------------------------

func TestSuggestInputValue(t *testing.T) {
	tests := []struct {
		label string
		want  string
	}{
		{"Email Address", "user@example.com"},
		{"email", "user@example.com"},
		{"Password", "<password>"},
		{"Phone Number", "<phone>"},
		{"Full Name", "<name>"},
		{"other_field", "<value>"},
		{"description", "<value>"},
	}
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			got := suggestInputValue(tt.label)
			if got != tt.want {
				t.Errorf("suggestInputValue(%q) = %q, want %q", tt.label, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// toMapSlice
// ---------------------------------------------------------------------------

func TestToMapSlice(t *testing.T) {
	tests := []struct {
		name    string
		in      interface{}
		wantLen int
	}{
		{"nil", nil, 0},
		{"typed []map", []map[string]interface{}{{"a": 1}}, 1},
		{"[]interface with maps", []interface{}{
			map[string]interface{}{"x": 1},
			map[string]interface{}{"y": 2},
		}, 2},
		{"[]interface mixed", []interface{}{
			map[string]interface{}{"x": 1},
			"not a map",
		}, 1},
		{"non-slice", "hello", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toMapSlice(tt.in)
			if len(got) != tt.wantLen {
				t.Errorf("toMapSlice() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// toStringSlice
// ---------------------------------------------------------------------------

func TestToStringSlice(t *testing.T) {
	tests := []struct {
		name    string
		in      interface{}
		wantLen int
	}{
		{"nil", nil, 0},
		{"typed []string", []string{"a", "b"}, 2},
		{"[]interface with strings", []interface{}{"x", "y"}, 2},
		{"[]interface with empty", []interface{}{"x", ""}, 1}, // empty trimmed out
		{"non-slice", 42, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toStringSlice(tt.in)
			if len(got) != tt.wantLen {
				t.Errorf("toStringSlice() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// extractTimestamp
// ---------------------------------------------------------------------------

func TestExtractTimestamp(t *testing.T) {
	tests := []struct {
		name string
		row  map[string]interface{}
		keys []string
		want int64
	}{
		{
			name: "key present",
			row:  map[string]interface{}{"ts": int64(1000)},
			keys: []string{"ts"},
			want: 1000,
		},
		{
			name: "key missing",
			row:  map[string]interface{}{"other": 1},
			keys: []string{"ts"},
			want: 0,
		},
		{
			name: "fallback key",
			row:  map[string]interface{}{"ts2": int64(500)},
			keys: []string{"ts1", "ts2"},
			want: 500,
		},
		{
			name: "first matching key wins",
			row:  map[string]interface{}{"ts1": int64(100), "ts2": int64(200)},
			keys: []string{"ts1", "ts2"},
			want: 100,
		},
		{
			name: "zero value skipped",
			row:  map[string]interface{}{"ts1": int64(0), "ts2": int64(300)},
			keys: []string{"ts1", "ts2"},
			want: 300,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTimestamp(tt.row, tt.keys...)
			if got != tt.want {
				t.Errorf("extractTimestamp() = %d, want %d", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// filterInteractiveData
// ---------------------------------------------------------------------------

func TestFilterInteractiveData(t *testing.T) {
	mkElem := func(typ string) interface{} {
		return map[string]interface{}{"type": typ, "ref": "r_" + typ}
	}
	allElems := []interface{}{
		mkElem("button"), mkElem("input"), mkElem("link"),
		mkElem("select"), mkElem("checkbox"), mkElem("radio"),
	}
	base := map[string]interface{}{
		"elements": allElems,
		"summary":  map[string]interface{}{"total": 6},
		"extra":    "kept",
	}

	tests := []struct {
		name       string
		filter     string
		wantCount  int
		returnsAll bool // if true, result should equal input
	}{
		{"empty filter", "", -1, true},
		{"all filter", "all", -1, true},
		{"unknown filter", "widgets", -1, true},
		{"buttons", "buttons", 1, false},
		{"inputs", "inputs", 3, false},  // input + checkbox + radio
		{"links", "links", 1, false},
		{"selects", "selects", 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterInteractiveData(base, tt.filter)
			if tt.returnsAll {
				// Should return original data reference
				if got["extra"] != "kept" {
					t.Error("expected original data returned")
				}
				return
			}
			elems, ok := got["elements"].([]interface{})
			if !ok {
				t.Fatal("expected elements in result")
			}
			if len(elems) != tt.wantCount {
				t.Errorf("filtered elements len = %d, want %d", len(elems), tt.wantCount)
			}
			summary, ok := got["summary"].(map[string]interface{})
			if !ok {
				t.Fatal("expected summary map")
			}
			if total, _ := summary["total"].(int); total != tt.wantCount {
				t.Errorf("summary total = %d, want %d", total, tt.wantCount)
			}
			// extra key should still be present
			if got["extra"] != "kept" {
				t.Error("expected extra key preserved")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildRefSet
// ---------------------------------------------------------------------------

func TestBuildRefSet(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]interface{}
		wantLen int
	}{
		{"nil", nil, 0},
		{"empty elements", map[string]interface{}{"elements": []interface{}{}}, 0},
		{
			"elements with refs",
			map[string]interface{}{
				"elements": []interface{}{
					map[string]interface{}{"ref": "btn1"},
					map[string]interface{}{"ref": "btn2"},
					map[string]interface{}{"ref": ""},
					map[string]interface{}{"other": "no ref"},
				},
			},
			2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildRefSet(tt.data)
			if len(got) != tt.wantLen {
				t.Errorf("buildRefSet() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// buildHrefSet
// ---------------------------------------------------------------------------

func TestBuildHrefSet(t *testing.T) {
	tests := []struct {
		name    string
		data    map[string]interface{}
		wantLen int
	}{
		{"nil", nil, 0},
		{"empty", map[string]interface{}{}, 0},
		{
			"areas with hrefs",
			map[string]interface{}{
				"nav":  map[string]interface{}{"home": "/", "about": "/about"},
				"side": map[string]interface{}{"settings": "/settings"},
				"main": map[string]interface{}{"link": "/main"},
				"foot": map[string]interface{}{"privacy": "/privacy"},
			},
			5,
		},
		{
			"ignores non-area keys",
			map[string]interface{}{
				"other": map[string]interface{}{"x": "/x"},
				"nav":   map[string]interface{}{"y": "/y"},
			},
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildHrefSet(tt.data)
			if len(got) != tt.wantLen {
				t.Errorf("buildHrefSet() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// filterActionCandidates
// ---------------------------------------------------------------------------

func TestFilterActionCandidates(t *testing.T) {
	tests := []struct {
		name         string
		candidates   []map[string]interface{}
		allowedRefs  map[string]bool
		allowedHrefs map[string]bool
		wantLen      int
	}{
		{
			name:       "empty candidates",
			candidates: []map[string]interface{}{},
			wantLen:    0,
		},
		{
			name: "global action kept (no ref, no label)",
			candidates: []map[string]interface{}{
				{"action": "scroll_down", "ref": "", "label": ""},
			},
			wantLen: 1,
		},
		{
			name: "navigate with allowed href",
			candidates: []map[string]interface{}{
				{"action": "navigate", "label": "/about", "ref": ""},
			},
			allowedHrefs: map[string]bool{"/about": true},
			wantLen:      1,
		},
		{
			name: "navigate with allowed ref",
			candidates: []map[string]interface{}{
				{"action": "navigate", "label": "", "ref": "btn1"},
			},
			allowedRefs: map[string]bool{"btn1": true},
			wantLen:     1,
		},
		{
			name: "default action with allowed ref",
			candidates: []map[string]interface{}{
				{"action": "click", "ref": "btn1", "label": "Submit"},
			},
			allowedRefs: map[string]bool{"btn1": true},
			wantLen:     1,
		},
		{
			name: "filtered out - ref not allowed",
			candidates: []map[string]interface{}{
				{"action": "click", "ref": "btn_unknown", "label": "Submit"},
			},
			allowedRefs: map[string]bool{"btn1": true},
			wantLen:     0,
		},
		{
			name: "mixed - some pass some fail",
			candidates: []map[string]interface{}{
				{"action": "scroll_down", "ref": "", "label": ""},     // global - kept
				{"action": "click", "ref": "btn1", "label": "OK"},     // allowed ref - kept
				{"action": "click", "ref": "btn_bad", "label": "Bad"}, // not allowed - dropped
			},
			allowedRefs: map[string]bool{"btn1": true},
			wantLen:     2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterActionCandidates(tt.candidates, tt.allowedRefs, tt.allowedHrefs)
			if len(got) != tt.wantLen {
				t.Errorf("filterActionCandidates() len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// limitAnySlice / limitMapSlice
// ---------------------------------------------------------------------------

func TestLimitAnySlice(t *testing.T) {
	items := []interface{}{"a", "b", "c", "d", "e"}
	tests := []struct {
		name    string
		max     int
		wantLen int
	}{
		{"under limit", 10, 5},
		{"exact limit", 5, 5},
		{"over limit truncates", 3, 3},
		{"zero max returns all", 0, 5},
		{"negative max returns all", -1, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := limitAnySlice(items, tt.max)
			if len(got) != tt.wantLen {
				t.Errorf("limitAnySlice(len=5, %d) len = %d, want %d", tt.max, len(got), tt.wantLen)
			}
		})
	}
}

func TestLimitMapSlice(t *testing.T) {
	items := []map[string]interface{}{{"a": 1}, {"b": 2}, {"c": 3}}
	tests := []struct {
		name    string
		max     int
		wantLen int
	}{
		{"under limit", 10, 3},
		{"exact limit", 3, 3},
		{"over limit truncates", 2, 2},
		{"zero max returns all", 0, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := limitMapSlice(items, tt.max)
			if len(got) != tt.wantLen {
				t.Errorf("limitMapSlice(len=3, %d) len = %d, want %d", tt.max, len(got), tt.wantLen)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// safeTraceFragment
// ---------------------------------------------------------------------------

func TestSafeTraceFragment(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fallback string
		want     string
	}{
		{"normal string", "hello_world", "fb", "hello_world"},
		{"special chars replaced", "hello world!@#", "fb", "hello_world"},
		{"empty uses fallback", "", "fb", "fb"},
		{"whitespace only uses fallback", "   ", "fb", "fb"},
		{"dashes preserved", "my-trace-id", "fb", "my-trace-id"},
		{"all special chars", "!!!###", "fallback", "fallback"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safeTraceFragment(tt.input, tt.fallback)
			if got != tt.want {
				t.Errorf("safeTraceFragment(%q, %q) = %q, want %q", tt.input, tt.fallback, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// truncateMangleData
// ---------------------------------------------------------------------------

func TestTruncateMangleData(t *testing.T) {
	t.Run("[]interface truncation", func(t *testing.T) {
		data := map[string]interface{}{
			"results": []interface{}{1, 2, 3, 4, 5},
			"meta":    "unchanged",
		}
		got := truncateMangleData(data, 3)
		results, ok := got["results"].([]interface{})
		if !ok {
			t.Fatal("expected results as []interface{}")
		}
		if len(results) != 3 {
			t.Errorf("results len = %d, want 3", len(results))
		}
		if trunc, ok := got["results_truncated"].(bool); !ok || !trunc {
			t.Error("expected results_truncated = true")
		}
		if got["meta"] != "unchanged" {
			t.Error("expected meta unchanged")
		}
	})

	t.Run("[]map truncation", func(t *testing.T) {
		data := map[string]interface{}{
			"facts": []map[string]interface{}{{"a": 1}, {"b": 2}, {"c": 3}},
		}
		got := truncateMangleData(data, 2)
		facts, ok := got["facts"].([]map[string]interface{})
		if !ok {
			t.Fatal("expected facts as []map")
		}
		if len(facts) != 2 {
			t.Errorf("facts len = %d, want 2", len(facts))
		}
		if trunc, ok := got["facts_truncated"].(bool); !ok || !trunc {
			t.Error("expected facts_truncated = true")
		}
	})

	t.Run("non-slice passthrough", func(t *testing.T) {
		data := map[string]interface{}{
			"status": "ok",
			"count":  42,
		}
		got := truncateMangleData(data, 1)
		if got["status"] != "ok" {
			t.Error("expected status preserved")
		}
		if got["count"] != 42 {
			t.Error("expected count preserved")
		}
	})

	t.Run("within limit no truncation flag", func(t *testing.T) {
		data := map[string]interface{}{
			"results": []interface{}{1, 2},
		}
		got := truncateMangleData(data, 5)
		if _, ok := got["results_truncated"]; ok {
			t.Error("expected no results_truncated key when within limit")
		}
	})
}
