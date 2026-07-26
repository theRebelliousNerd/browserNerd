package browser

import (
	"fmt"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

func TestCaptureStepRuntimeEvidenceIncludesStepSignals(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{}, nil)
	sessionID := "session-step-evidence"
	manager.sessions[sessionID] = &sessionRecord{
		meta: Session{
			ID:  sessionID,
			URL: "https://app.example.com/start",
		},
		registry: NewElementRegistry(),
		activity: newSessionRuntimeEvidenceBuffer(),
	}

	since := time.UnixMilli(1000)
	manager.recordRequestEvent(sessionID, "req-before", "https://api.example.com/before", time.UnixMilli(950))
	manager.recordConsoleErrorEvent(sessionID, "error", "stale failure", time.UnixMilli(975))

	manager.recordRequestEvent(sessionID, "req-1", "https://api.example.com/account", time.UnixMilli(1100))
	manager.recordRequestEvent(sessionID, "req-2", "https://api.example.com/toast", time.UnixMilli(1110))
	manager.recordRequestEvent(sessionID, "req-1", "https://api.example.com/account", time.UnixMilli(1120))
	manager.recordToastEvent(sessionID, "Profile saved", "success", "shadcn", time.UnixMilli(1130))
	manager.recordConsoleErrorEvent(sessionID, "error", "Cannot read properties of undefined", time.UnixMilli(1140))
	manager.recordNavigationEvent(sessionID, "https://app.example.com/dashboard", time.UnixMilli(1150))

	evidence, err := manager.CaptureStepRuntimeEvidence(sessionID, since)
	if err != nil {
		t.Fatalf("CaptureStepRuntimeEvidence returned error: %v", err)
	}

	if !evidence.Changed {
		t.Fatal("expected Changed=true when new runtime activity was recorded")
	}
	if evidence.RouteChange == nil {
		t.Fatal("expected route change evidence")
	}
	if evidence.RouteChange.FromURL != "https://app.example.com/start" {
		t.Fatalf("expected route from start page, got %q", evidence.RouteChange.FromURL)
	}
	if evidence.RouteChange.ToURL != "https://app.example.com/dashboard" {
		t.Fatalf("expected route to dashboard, got %q", evidence.RouteChange.ToURL)
	}
	if evidence.LatestToast == nil || evidence.LatestToast.Text != "Profile saved" {
		t.Fatalf("expected latest toast evidence, got %+v", evidence.LatestToast)
	}
	if evidence.LatestConsoleError == nil || evidence.LatestConsoleError.Message != "Cannot read properties of undefined" {
		t.Fatalf("expected latest console error evidence, got %+v", evidence.LatestConsoleError)
	}
	if len(evidence.RequestIDsObserved) != 2 {
		t.Fatalf("expected two request ids after checkpoint, got %v", evidence.RequestIDsObserved)
	}
	if evidence.RequestIDsObserved[0] != "req-1" || evidence.RequestIDsObserved[1] != "req-2" {
		t.Fatalf("unexpected request ids after checkpoint: %v", evidence.RequestIDsObserved)
	}
}

func TestCaptureStepRuntimeEvidenceIsSessionScoped(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{}, nil)
	manager.sessions["session-a"] = &sessionRecord{
		meta:     Session{ID: "session-a", URL: "https://app.example.com/a"},
		registry: NewElementRegistry(),
		activity: newSessionRuntimeEvidenceBuffer(),
	}
	manager.sessions["session-b"] = &sessionRecord{
		meta:     Session{ID: "session-b", URL: "https://app.example.com/b"},
		registry: NewElementRegistry(),
		activity: newSessionRuntimeEvidenceBuffer(),
	}

	since := time.UnixMilli(2000)
	manager.recordToastEvent("session-b", "Other session toast", "error", "native", time.UnixMilli(2100))
	manager.recordRequestEvent("session-a", "req-a", "https://api.example.com/a", time.UnixMilli(2110))
	manager.recordRequestEvent("session-b", "req-b", "https://api.example.com/b", time.UnixMilli(2120))

	evidence, err := manager.CaptureStepRuntimeEvidence("session-a", since)
	if err != nil {
		t.Fatalf("CaptureStepRuntimeEvidence returned error: %v", err)
	}

	if !evidence.Changed {
		t.Fatal("expected Changed=true for session-a")
	}
	if evidence.LatestToast != nil {
		t.Fatalf("expected no toast leakage from other session, got %+v", evidence.LatestToast)
	}
	if len(evidence.RequestIDsObserved) != 1 || evidence.RequestIDsObserved[0] != "req-a" {
		t.Fatalf("expected only session-a request ids, got %v", evidence.RequestIDsObserved)
	}
}

func TestCaptureStepRuntimeEvidenceIsBounded(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{}, nil)
	sessionID := "session-bounded"
	manager.sessions[sessionID] = &sessionRecord{
		meta:     Session{ID: sessionID, URL: "https://app.example.com/bounded"},
		registry: NewElementRegistry(),
		activity: newSessionRuntimeEvidenceBuffer(),
	}

	for i := 0; i < defaultStepRuntimeRequestLimit+8; i++ {
		manager.recordRequestEvent(
			sessionID,
			fmt.Sprintf("req-%02d", i),
			fmt.Sprintf("https://api.example.com/%02d", i),
			time.UnixMilli(int64(3000+i)),
		)
	}

	evidence, err := manager.CaptureStepRuntimeEvidence(sessionID, time.Time{})
	if err != nil {
		t.Fatalf("CaptureStepRuntimeEvidence returned error: %v", err)
	}

	if len(evidence.RequestIDsObserved) != defaultStepRuntimeRequestLimit {
		t.Fatalf("expected %d bounded request ids, got %d", defaultStepRuntimeRequestLimit, len(evidence.RequestIDsObserved))
	}
	if evidence.RequestIDsObserved[0] != "req-08" {
		t.Fatalf("expected oldest retained request id req-08, got %q", evidence.RequestIDsObserved[0])
	}
	last := evidence.RequestIDsObserved[len(evidence.RequestIDsObserved)-1]
	if last != "req-39" {
		t.Fatalf("expected newest retained request id req-39, got %q", last)
	}
}

func TestCaptureStepRuntimeEvidenceUnknownSession(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{}, nil)

	if _, err := manager.CaptureStepRuntimeEvidence("missing-session", time.Now()); err == nil {
		t.Fatal("expected unknown session error")
	}
}
