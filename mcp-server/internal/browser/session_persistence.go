package browser

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"browsernerd-mcp-server/internal/security"
)

func (m *SessionManager) persistSessions() error {
	if m.cfg.SessionStore == "" {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]Session, 0, len(m.sessions))
	for _, rec := range m.sessions {
		meta := rec.meta
		meta.URL = m.redactor.SanitizeString(meta.URL)
		meta.Title = m.redactor.SanitizeString(meta.Title)
		sessions = append(sessions, meta)
	}

	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return err
	}

	storeDir := filepath.Dir(m.cfg.SessionStore)
	if storeDir != "" && storeDir != "." {
		if err := security.EnsurePrivateDir(storeDir); err != nil {
			return err
		}
	}
	return security.WritePrivateFile(m.cfg.SessionStore, data)
}

// loadSessions loads persisted metadata (does not auto-attach to pages).
func (m *SessionManager) loadSessions() error {
	if m.cfg.SessionStore == "" {
		return nil
	}

	data, err := os.ReadFile(m.cfg.SessionStore)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var sessions []Session
	if err := json.Unmarshal(data, &sessions); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range sessions {
		// Mark as detached; a caller can use attach-session to bind to a live target.
		s.Status = "detached"
		m.sessions[s.ID] = &sessionRecord{
			meta:     s,
			page:     nil,
			registry: NewElementRegistry(),
			activity: newSessionRuntimeEvidenceBuffer(),
		}
	}
	return nil
}

func coalesceNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func redactInputValue(redactor *security.Redactor, id, value string) string {
	if value == security.Redacted {
		return value
	}
	if redactor != nil && redactor.IsSensitiveKey(id) {
		return security.Redacted
	}
	descriptor := strings.ToLower(id)
	for _, marker := range []string{"password", "passwd", "secret", "token", "api-key", "apikey", "cc-number", "card-number", "cvv", "cvc"} {
		if strings.Contains(descriptor, marker) {
			return security.Redacted
		}
	}
	return redactor.SanitizeString(value)
}

func sensitiveDOMInput(attrs map[string]string) bool {
	descriptor := strings.ToLower(strings.Join([]string{
		attrs["type"], attrs["autocomplete"], attrs["id"], attrs["name"], attrs["aria-label"],
	}, " "))
	for _, marker := range []string{"password", "passwd", "secret", "token", "api-key", "one-time-code", "cc-number", "cc-csc", "card-number", "cvv", "cvc"} {
		if strings.Contains(descriptor, marker) {
			return true
		}
	}
	return false
}

// isInternalScript returns true if the URL is an internal browser script (not app code).
func isInternalScript(url string) bool {
	// Filter out browser extensions, devtools, and internal protocols
	internalPrefixes := []string{
		"chrome://",
		"chrome-extension://",
		"devtools://",
		"about:",
		"data:",
		"blob:",
	}
	for _, prefix := range internalPrefixes {
		if strings.HasPrefix(url, prefix) {
			return true
		}
	}
	return false
}
