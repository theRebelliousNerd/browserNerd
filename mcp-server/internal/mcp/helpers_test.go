package mcp

import (
	"errors"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

func TestMatchFact(t *testing.T) {
	facts := []mangle.Fact{
		{Predicate: "test", Args: []interface{}{"arg1", "arg2", 123}, Timestamp: time.Now()},
		{Predicate: "test", Args: []interface{}{"other", "values"}, Timestamp: time.Now()},
	}

	tests := []struct {
		name     string
		facts    []mangle.Fact
		wantArgs []interface{}
		expected bool
	}{
		{
			name:     "empty want args with facts",
			facts:    facts,
			wantArgs: nil,
			expected: true,
		},
		{
			name:     "empty want args with empty facts",
			facts:    nil,
			wantArgs: nil,
			expected: false,
		},
		{
			name:     "matching first arg",
			facts:    facts,
			wantArgs: []interface{}{"arg1"},
			expected: true,
		},
		{
			name:     "matching multiple args",
			facts:    facts,
			wantArgs: []interface{}{"arg1", "arg2"},
			expected: true,
		},
		{
			name:     "matching all args",
			facts:    facts,
			wantArgs: []interface{}{"arg1", "arg2", 123},
			expected: true,
		},
		{
			name:     "non-matching args",
			facts:    facts,
			wantArgs: []interface{}{"nonexistent"},
			expected: false,
		},
		{
			name:     "want more args than fact has",
			facts:    facts,
			wantArgs: []interface{}{"arg1", "arg2", 123, "extra"},
			expected: false,
		},
		{
			name:     "matching other fact",
			facts:    facts,
			wantArgs: []interface{}{"other", "values"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchFact(tt.facts, tt.wantArgs)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestSanitizeAriaLabel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple text", "Submit", "Submit"},
		{"with spaces", "Submit Form", "Submit_Form"},
		{"with special chars", "Save & Continue!", "Save___Continue_"},
		{"with unicode", "Hello World", "Hello_World"},
		{"long text truncated", "This is a very long aria label that should be truncated at forty characters", "This_is_a_very_long_aria_label_that_shou"},
		{"alphanumeric", "btn123", "btn123"},
		{"with hyphens", "my-button", "my-button"},
		{"with underscores", "my_button", "my_button"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeAriaLabel(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestEscapeAttributeValue(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple text", "hello", "hello"},
		{"with quotes", `say "hello"`, `say \"hello\"`},
		{"with backslash", `path\to\file`, `path\\to\\file`},
		{"mixed", `"quoted\\path"`, `\"quoted\\\\path\"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeAttributeValue(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestEscapeCSSSelector(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"simple text", "button", "button"},
		{"with dot", "btn.primary", `btn\.primary`},
		{"with hash", "btn#submit", `btn\#submit`},
		{"with colon", "ns:element", `ns\:element`},
		{"with brackets", "data[key]", `data\[key\]`},
		{"with space", "my button", `my\ button`},
		{"complex", "btn.class#id[attr]", `btn\.class\#id\[attr\]`},
		{"with slash", "path/to", `path\/to`},
		{"with special chars", "a>b+c~d", `a\>b\+c\~d`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeCSSSelector(tt.input)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGetStringArg(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		expected string
	}{
		{
			name:     "string value",
			args:     map[string]interface{}{"key": "value"},
			key:      "key",
			expected: "value",
		},
		{
			name:     "missing key",
			args:     map[string]interface{}{"other": "value"},
			key:      "key",
			expected: "",
		},
		{
			name:     "int value converted to string",
			args:     map[string]interface{}{"key": 123},
			key:      "key",
			expected: "123",
		},
		{
			name:     "nil map",
			args:     nil,
			key:      "key",
			expected: "",
		},
		{
			name:     "bool value converted to string",
			args:     map[string]interface{}{"key": true},
			key:      "key",
			expected: "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStringArg(tt.args, tt.key)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGetIntArg(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		fallback int
		expected int
	}{
		{
			name:     "int value",
			args:     map[string]interface{}{"key": 42},
			key:      "key",
			fallback: 0,
			expected: 42,
		},
		{
			name:     "int64 value",
			args:     map[string]interface{}{"key": int64(100)},
			key:      "key",
			fallback: 0,
			expected: 100,
		},
		{
			name:     "float64 value",
			args:     map[string]interface{}{"key": float64(3.14)},
			key:      "key",
			fallback: 0,
			expected: 3,
		},
		{
			name:     "missing key uses fallback",
			args:     map[string]interface{}{"other": 123},
			key:      "key",
			fallback: 99,
			expected: 99,
		},
		{
			name:     "string value uses fallback",
			args:     map[string]interface{}{"key": "not a number"},
			key:      "key",
			fallback: 50,
			expected: 50,
		},
		{
			name:     "nil map uses fallback",
			args:     nil,
			key:      "key",
			fallback: 25,
			expected: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getIntArg(tt.args, tt.key, tt.fallback)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestGetBoolArg(t *testing.T) {
	tests := []struct {
		name     string
		args     map[string]interface{}
		key      string
		fallback bool
		expected bool
	}{
		{
			name:     "true value",
			args:     map[string]interface{}{"key": true},
			key:      "key",
			fallback: false,
			expected: true,
		},
		{
			name:     "false value",
			args:     map[string]interface{}{"key": false},
			key:      "key",
			fallback: true,
			expected: false,
		},
		{
			name:     "missing key uses fallback true",
			args:     map[string]interface{}{"other": true},
			key:      "key",
			fallback: true,
			expected: true,
		},
		{
			name:     "missing key uses fallback false",
			args:     map[string]interface{}{"other": false},
			key:      "key",
			fallback: false,
			expected: false,
		},
		{
			name:     "non-bool value uses fallback",
			args:     map[string]interface{}{"key": "true"},
			key:      "key",
			fallback: false,
			expected: false,
		},
		{
			name:     "nil map uses fallback",
			args:     nil,
			key:      "key",
			fallback: true,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getBoolArg(tt.args, tt.key, tt.fallback)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestClassifyJSError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: "",
		},
		{
			name:     "timeout error",
			err:      errors.New("context deadline exceeded"),
			expected: "timeout",
		},
		{
			name:     "timeout keyword",
			err:      errors.New("operation timeout"),
			expected: "timeout",
		},
		{
			name:     "syntax error",
			err:      errors.New("SyntaxError: Unexpected token"),
			expected: "syntax",
		},
		{
			name:     "unexpected token",
			err:      errors.New("Unexpected token '}' at line 5"),
			expected: "syntax",
		},
		{
			name:     "reference error",
			err:      errors.New("ReferenceError: foo is not defined"),
			expected: "runtime",
		},
		{
			name:     "type error",
			err:      errors.New("TypeError: Cannot read property 'map' of undefined"),
			expected: "runtime",
		},
		{
			name:     "is not defined",
			err:      errors.New("myVar is not defined"),
			expected: "runtime",
		},
		{
			name:     "is not a function",
			err:      errors.New("foo is not a function"),
			expected: "runtime",
		},
		{
			name:     "cannot read property",
			err:      errors.New("Cannot read property 'x' of null"),
			expected: "runtime",
		},
		{
			name:     "cannot read properties",
			err:      errors.New("Cannot read properties of undefined"),
			expected: "runtime",
		},
		{
			name:     "promise error",
			err:      errors.New("Promise rejection"),
			expected: "async",
		},
		{
			name:     "async error",
			err:      errors.New("async function failed"),
			expected: "async",
		},
		{
			name:     "security error",
			err:      errors.New("SecurityError: blocked by CSP"),
			expected: "security",
		},
		{
			name:     "cross-origin error",
			err:      errors.New("cross-origin request blocked"),
			expected: "security",
		},
		{
			name:     "unknown error",
			err:      errors.New("some random error"),
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyJSError(tt.err)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestFormatJSError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: "",
		},
		{
			name:     "reference error",
			err:      errors.New("runtime error: ReferenceError: foo is not defined"),
			expected: "ReferenceError:foo is not defined",
		},
		{
			name:     "type error",
			err:      errors.New("CDP: TypeError: x is not a function"),
			expected: "TypeError:x is not a function",
		},
		{
			name:     "syntax error",
			err:      errors.New("eval failed: SyntaxError: Unexpected token"),
			expected: "SyntaxError:Unexpected token",
		},
		{
			name:     "timeout",
			err:      errors.New("context deadline exceeded"),
			expected: "Script execution timed out",
		},
		{
			name:     "short error unchanged",
			err:      errors.New("short error"),
			expected: "short error",
		},
		{
			name: "long error truncated",
			err: errors.New("this is a very long error message that exceeds the maximum allowed " +
				"length of two hundred characters and should be truncated at the end to " +
				"prevent extremely long error messages from being displayed in their entirety " +
				"which would be unreadable"),
			expected: "this is a very long error message that exceeds the maximum allowed " +
				"length of two hundred characters and should be truncated at the end to " +
				"prevent extremely long error messages from being displayed ...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatJSError(tt.err)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestValidateFingerprint(t *testing.T) {
	// Test with nil fingerprint
	t.Run("nil fingerprint", func(t *testing.T) {
		result := validateFingerprint(nil, nil)
		if !result.Valid {
			t.Error("expected valid for nil fingerprint")
		}
		if result.Score != 1.0 {
			t.Errorf("expected score 1.0, got %v", result.Score)
		}
	})

	// Note: validateFingerprint requires a non-nil rod.Element to call methods on
	// Testing with nil element would cause a panic, so we skip that scenario
	// The function is designed to be called with actual DOM elements
}

func TestFingerprintValidationResult(t *testing.T) {
	t.Run("struct fields", func(t *testing.T) {
		result := FingerprintValidationResult{
			Valid:   true,
			Changes: []string{"field1: changed", "field2: changed"},
			Score:   0.8,
		}

		if !result.Valid {
			t.Error("expected Valid to be true")
		}
		if len(result.Changes) != 2 {
			t.Errorf("expected 2 changes, got %d", len(result.Changes))
		}
		if result.Score != 0.8 {
			t.Errorf("expected score 0.8, got %v", result.Score)
		}
	})
}

func TestElementRegistry(t *testing.T) {
	t.Run("new registry", func(t *testing.T) {
		reg := browser.NewElementRegistry()
		if reg == nil {
			t.Fatal("expected non-nil registry")
		}
		if reg.Count() != 0 {
			t.Errorf("expected empty registry, got %d elements", reg.Count())
		}
	})

	t.Run("register and get", func(t *testing.T) {
		reg := browser.NewElementRegistry()
		fp := &browser.ElementFingerprint{
			Ref:     "test-ref",
			TagName: "button",
			ID:      "submit-btn",
		}
		reg.Register(fp)

		if reg.Count() != 1 {
			t.Errorf("expected 1 element, got %d", reg.Count())
		}

		retrieved := reg.Get("test-ref")
		if retrieved == nil {
			t.Fatal("expected to retrieve fingerprint")
		}
		if retrieved.TagName != "button" {
			t.Errorf("expected tagname 'button', got %q", retrieved.TagName)
		}
	})

	t.Run("get non-existent", func(t *testing.T) {
		reg := browser.NewElementRegistry()
		result := reg.Get("nonexistent")
		if result != nil {
			t.Error("expected nil for non-existent ref")
		}
	})

	t.Run("register batch", func(t *testing.T) {
		reg := browser.NewElementRegistry()
		fps := []*browser.ElementFingerprint{
			{Ref: "ref1", TagName: "button"},
			{Ref: "ref2", TagName: "input"},
			{Ref: "ref3", TagName: "a"},
		}
		initialGen := reg.GenerationID()
		reg.RegisterBatch(fps)

		if reg.Count() != 3 {
			t.Errorf("expected 3 elements, got %d", reg.Count())
		}
		if reg.GenerationID() != initialGen+1 {
			t.Error("expected generation ID to increment")
		}
	})

	t.Run("clear", func(t *testing.T) {
		reg := browser.NewElementRegistry()
		reg.Register(&browser.ElementFingerprint{Ref: "test", TagName: "div"})
		initialGen := reg.GenerationID()

		reg.Clear()

		if reg.Count() != 0 {
			t.Errorf("expected 0 elements after clear, got %d", reg.Count())
		}
		if reg.GenerationID() != initialGen+1 {
			t.Error("expected generation ID to increment after clear")
		}
	})

	t.Run("increment generation", func(t *testing.T) {
		reg := browser.NewElementRegistry()
		reg.Register(&browser.ElementFingerprint{Ref: "test", TagName: "div"})
		initialGen := reg.GenerationID()
		initialCount := reg.Count()

		reg.IncrementGeneration()

		if reg.GenerationID() != initialGen+1 {
			t.Error("expected generation ID to increment")
		}
		if reg.Count() != initialCount {
			t.Error("expected count to remain unchanged")
		}
	})
}
