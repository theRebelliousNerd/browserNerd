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
