package security

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactorSanitizesCredentialsRecursively(t *testing.T) {
	redactor := NewRedactor(nil)
	input := map[string]interface{}{
		"headers": map[string]string{
			"Authorization": "Bearer live-access-token",
			"Cookie":        "session=cleartext",
			"Content-Type":  "application/json",
		},
		"password": "cleartext-password",
		"url":      "https://example.test/login?access_token=abc123&mode=test",
		"nested": []interface{}{
			map[string]interface{}{"selector": "#user-password", "value": "hunter2"},
		},
	}

	sanitized := redactor.Sanitize(input)
	encoded, err := json.Marshal(sanitized)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, secret := range []string{"live-access-token", "session=cleartext", "cleartext-password", "abc123", "hunter2"} {
		if strings.Contains(text, secret) {
			t.Fatalf("sanitized output retained secret %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "application/json") || !strings.Contains(text, "mode=test") {
		t.Fatalf("sanitized output removed safe values: %s", text)
	}
}

func TestRedactorSanitizeStringPreservesAuthorizationScheme(t *testing.T) {
	redactor := NewRedactor(nil)
	got := redactor.RedactHeader("Authorization", "Bearer abc.def.ghi")
	if got != "Bearer "+Redacted {
		t.Fatalf("unexpected authorization redaction: %q", got)
	}

	logLine := `request password=secret Bearer another-secret`
	got = redactor.SanitizeString(logLine)
	if strings.Contains(got, "secret") || strings.Contains(got, "another-secret") {
		t.Fatalf("log redaction retained secret: %q", got)
	}
}

func TestRedactorHandlesNilAndScalarValues(t *testing.T) {
	redactor := NewRedactor(nil)
	if got := redactor.Sanitize(nil); got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
	if got := redactor.Sanitize(42); got != 42 {
		t.Fatalf("expected scalar to remain unchanged, got %#v", got)
	}
}

func TestRedactorRedactsSensitiveBrowserInputs(t *testing.T) {
	redactor := NewRedactor(nil)
	for _, descriptor := range []string{
		"type=password name=login",
		"autocomplete=current-password",
		"ref=input-one-time-code",
		"label=Card CVV",
	} {
		if got := redactor.RedactInputValue(descriptor, "cleartext"); got != Redacted {
			t.Fatalf("expected descriptor %q to redact, got %q", descriptor, got)
		}
	}
	if got := redactor.RedactInputValue("name=display_name", "Steve"); got != "Steve" {
		t.Fatalf("expected safe browser input to remain, got %q", got)
	}
}

func TestRedactorSanitizesStructuredNativeLogRaw(t *testing.T) {
	redactor := NewRedactor([]string{"session_credential"})
	raw := `{"timestamp":"2026-10-22T12:00:00Z","level":"ERROR","message":"GET /items failed","password":"json-password-fixture","api_key":"json-key-fixture","context":{"token":"nested-token-fixture","safe":"keep"},"session_credential":{"parts":["custom-secret-fixture"]}}`
	payload := map[string]interface{}{
		"rows": []interface{}{map[string]interface{}{
			"source": "server_log", "log_source": "api", "raw": raw,
		}},
	}
	safe := redactor.Sanitize(payload).(map[string]interface{})
	rows := safe["rows"].([]interface{})
	result := rows[0].(map[string]interface{})["raw"].(string)
	for _, fixture := range []string{"json-password-fixture", "json-key-fixture", "nested-token-fixture", "custom-secret-fixture"} {
		if strings.Contains(result, fixture) {
			t.Fatalf("native JSON raw retained %q: %s", fixture, result)
		}
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(result), &decoded); err != nil {
		t.Fatalf("sanitized raw must remain JSON: %v, %s", err, result)
	}
	if decoded["message"] != "GET /items failed" || decoded["password"] != Redacted || decoded["session_credential"] != Redacted || decoded["context"].(map[string]interface{})["safe"] != "keep" {
		t.Fatalf("sensitive values or safe evidence changed incorrectly: %s", result)
	}
}

func TestRedactorJSONFragmentsQuotesEscapesAndScalarValues(t *testing.T) {
	redactor := NewRedactor([]string{"custom_credential"})
	cases := []struct {
		input, want string
	}{
		{`prefix {"password":"fragment-secret","safe":42} suffix`, `prefix {"password":"[REDACTED]","safe":42} suffix`},
		{`prefix {"pass\u0077ord":"escaped-key-fixture","safe":"keep"} suffix`, `prefix {"pass\u0077ord":"[REDACTED]","safe":"keep"} suffix`},
		{`{"password":"escaped-quote\"and-slash\\fixture","safe":"quote\" remains"}`, `{"password":"[REDACTED]","safe":"quote\" remains"}`},
		{`{"custom_credential":[{"private":"array-secret"}],"safe":true}`, `{"custom_credential":"[REDACTED]","safe":true}`},
		{`event {"token":12345,"count":3} tail`, `event {"token":"[REDACTED]","count":3} tail`},
		{`{"password":"clipped-fixture`, `{"password":"[REDACTED]"`},
		{`{"password":{"nested":"clipped-fixture"`, `{"password":"[REDACTED]"`},
		{`"ordinary JSON scalar"`, `"ordinary JSON scalar"`},
		{`""`, `""`},
		{`123`, `123`},
		{`[true,false,null]`, `[true,false,null]`},
		{`{"safe":{"items":[1,2,3]},"message":"failure"}`, `{"safe":{"items":[1,2,3]},"message":"failure"}`},
	}
	for _, tc := range cases {
		if result := redactor.SanitizeString(tc.input); result != tc.want {
			t.Errorf("SanitizeString(%q) = %q, want %q", tc.input, result, tc.want)
		}
	}
}

func TestRedactorJSONScanIsIterativeAndHandlesMalformedFragments(t *testing.T) {
	redactor := NewRedactor(nil)
	input := `prefix {"password":` + strings.Repeat("[", 20000) + `"deep-secret-fixture"` + strings.Repeat("]", 20000) + `,"message":"keep"} suffix`
	result := redactor.SanitizeString(input)
	if result != `prefix {"password":"[REDACTED]","message":"keep"} suffix` {
		t.Fatalf("deep JSON must redact without recursive decoding: %s", result)
	}
	for _, input := range []string{`{"password":`, `{"password":,}`, `{"password":}`, `{"password":"\\`, `{"bad\q":"safe","password":"fixture-secret"}`} {
		if result := redactor.SanitizeString(input); strings.Contains(result, "fixture-secret") {
			t.Fatalf("malformed fragment retained credential: %q", result)
		}
	}
}
