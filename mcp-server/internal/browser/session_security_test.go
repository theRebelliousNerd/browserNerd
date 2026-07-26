package browser

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/security"
)

func TestRedactInputValueProtectsCredentialFields(t *testing.T) {
	redactor := security.NewRedactor(nil)
	for _, id := range []string{"password", "new_password", "api-token", "cc-number", "card-cvv"} {
		if got := redactInputValue(redactor, id, "cleartext"); got != security.Redacted {
			t.Fatalf("expected %q to be redacted, got %q", id, got)
		}
	}
	if got := redactInputValue(redactor, "display_name", "Steve"); got != "Steve" {
		t.Fatalf("expected ordinary input to remain visible, got %q", got)
	}
}

func TestSensitiveDOMInput(t *testing.T) {
	if !sensitiveDOMInput(map[string]string{"type": "password", "value": "secret"}) {
		t.Fatal("expected password input to be sensitive")
	}
	if !sensitiveDOMInput(map[string]string{"autocomplete": "cc-number"}) {
		t.Fatal("expected payment input to be sensitive")
	}
	if sensitiveDOMInput(map[string]string{"type": "text", "name": "company"}) {
		t.Fatal("ordinary text input was marked sensitive")
	}
}

func TestEventPollBackoffIsBounded(t *testing.T) {
	if got := eventPollBackoff(1); got != 500*time.Millisecond {
		t.Fatalf("unexpected first backoff: %s", got)
	}
	if got := eventPollBackoff(20); got != 5*time.Second {
		t.Fatalf("expected bounded 5s backoff, got %s", got)
	}
}

func TestPersistSessionsUsesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "sessions.json")
	manager := NewSessionManager(config.BrowserConfig{SessionStore: path}, nil)
	now := time.Now()
	manager.sessions["session-1"] = &sessionRecord{
		meta: Session{
			ID:         "session-1",
			URL:        "https://example.test/?token=not-a-session-secret",
			CreatedAt:  now,
			LastActive: now,
		},
	}
	if err := manager.persistSessions(); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "session-1") {
		t.Fatalf("session metadata was not persisted: %s", content)
	}
	if strings.Contains(string(content), "not-a-session-secret") {
		t.Fatalf("session metadata retained URL credential: %s", content)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("expected session file mode 0600, got %o", info.Mode().Perm())
		}
	}
}
