package browser

import (
	"testing"
	"time"

	"browsernerd-mcp-server/internal/config"
)

// A navigation between calls is one no call saw: it is reported until a call
// attends the tab again, and a navigation a call made while attending is not.
func TestUnattendedNavigationsAreTheOnesAfterTheLastAttendedCall(t *testing.T) {
	manager := NewSessionManager(config.BrowserConfig{}, nil)
	manager.sessions["tab"] = &sessionRecord{meta: Session{ID: "tab", URL: "http://app/login"}, activity: newSessionRuntimeEvidenceBuffer()}
	base := time.Now().Add(-time.Minute)

	manager.recordNavigationEvent("tab", "http://app/dashboard", base)
	if moves := manager.UnattendedNavigations("tab"); moves != nil {
		t.Fatalf("before any call attended the tab there is nothing to call unattended: %+v", moves)
	}

	manager.MarkAttended("tab", base.Add(time.Second))
	manager.recordNavigationEvent("tab", "http://app/projects", base.Add(20*time.Second))
	manager.recordNavigationEvent("tab", "http://app/projects/42", base.Add(21*time.Second))
	moves := manager.UnattendedNavigations("tab")
	if len(moves) != 2 || moves[0].FromURL != "http://app/dashboard" || moves[1].ToURL != "http://app/projects/42" {
		t.Fatalf("both moves after the attended call, oldest first: %+v", moves)
	}

	manager.MarkAttended("tab", base.Add(30*time.Second))
	if moves := manager.UnattendedNavigations("tab"); len(moves) != 0 {
		t.Fatalf("a call that attended the tab has seen it: %+v", moves)
	}
	if moves := manager.UnattendedNavigations("missing"); moves != nil {
		t.Fatalf("an unknown session has no moves: %+v", moves)
	}
}
