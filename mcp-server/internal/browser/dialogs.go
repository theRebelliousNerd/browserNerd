package browser

import (
	"context"
	"log"
	"sync"
	"time"

	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// DialogRecord is one JavaScript alert/confirm/prompt/beforeunload a page
// opened, and what answered it. A dialog is never left open: an open dialog
// blocks the renderer, and every later call on the tab hangs behind it.
type DialogRecord struct {
	SessionID     string    `json:"session_id"`
	Type          string    `json:"type"` // alert | confirm | prompt | beforeunload
	Message       string    `json:"message"`
	DefaultPrompt string    `json:"default_prompt,omitempty"`
	URL           string    `json:"url,omitempty"`
	Accepted      bool      `json:"accepted"`
	HandledBy     string    `json:"handled_by"` // armed | policy
	OpenedAt      time.Time `json:"opened_at"`
}

// dialogArm is a one-shot answer for the next dialog a session opens.
type dialogArm struct {
	accept     bool
	promptText string
	armedAt    time.Time
}

// dialogBook holds armed answers and the recent dialog history per session.
type dialogBook struct {
	mu      sync.Mutex
	armed   map[string]dialogArm
	history map[string][]DialogRecord
}

const maxDialogHistory = 50

func (b *dialogBook) init() {
	if b.armed == nil {
		b.armed = make(map[string]dialogArm)
	}
	if b.history == nil {
		b.history = make(map[string][]DialogRecord)
	}
}

// ArmDialog sets the answer for the next dialog sessionID opens. It replaces
// any earlier unused arm, and is consumed by that one dialog.
func (m *SessionManager) ArmDialog(sessionID string, accept bool, promptText string) {
	m.dialogs.mu.Lock()
	defer m.dialogs.mu.Unlock()
	m.dialogs.init()
	m.dialogs.armed[sessionID] = dialogArm{accept: accept, promptText: promptText, armedAt: time.Now()}
}

// DisarmDialog drops an unused arm, so it cannot answer a later, unrelated dialog.
func (m *SessionManager) DisarmDialog(sessionID string) {
	m.dialogs.mu.Lock()
	defer m.dialogs.mu.Unlock()
	m.dialogs.init()
	delete(m.dialogs.armed, sessionID)
}

// DialogsSince returns the dialogs sessionID opened at or after since, oldest first.
func (m *SessionManager) DialogsSince(sessionID string, since time.Time) []DialogRecord {
	m.dialogs.mu.Lock()
	defer m.dialogs.mu.Unlock()
	m.dialogs.init()
	out := make([]DialogRecord, 0)
	for _, rec := range m.dialogs.history[sessionID] {
		if !rec.OpenedAt.Before(since) {
			out = append(out, rec)
		}
	}
	return out
}

// takeDialogAnswer consumes the armed answer for sessionID, or falls back to
// the configured policy. It is called from the dialog event handler.
func (m *SessionManager) takeDialogAnswer(sessionID string) (accept bool, promptText string, handledBy string) {
	m.dialogs.mu.Lock()
	defer m.dialogs.mu.Unlock()
	m.dialogs.init()
	if arm, ok := m.dialogs.armed[sessionID]; ok {
		delete(m.dialogs.armed, sessionID)
		return arm.accept, arm.promptText, "armed"
	}
	return m.cfg.GetDialogPolicy() == "accept", "", "policy"
}

// handleDialog answers a dialog the moment it opens - with the armed answer,
// else the configured policy - records it, and emits js_dialog. It runs on its
// own event subscription, so no other capture work can delay the answer.
func (m *SessionManager) handleDialog(ctx context.Context, sessionID string, page *rod.Page, ev *proto.PageJavascriptDialogOpening) {
	now := time.Now()
	accept, promptText, handledBy := m.takeDialogAnswer(sessionID)
	if err := (proto.PageHandleJavaScriptDialog{Accept: accept, PromptText: promptText}).Call(page); err != nil {
		log.Printf("[session:%s] answering %s dialog failed: %v", sessionID, ev.Type, err)
	}
	rec := DialogRecord{
		SessionID:     sessionID,
		Type:          string(ev.Type),
		Message:       m.redactor.SanitizeString(ev.Message),
		DefaultPrompt: m.redactor.SanitizeString(ev.DefaultPrompt),
		URL:           m.redactor.SanitizeString(ev.URL),
		Accepted:      accept,
		HandledBy:     handledBy,
		OpenedAt:      now,
	}
	m.recordDialog(rec)
	if m.engine == nil {
		return
	}
	m.addFacts(ctx, sessionID, "js_dialog", []mangle.Fact{{
		Predicate: "js_dialog",
		Args:      []interface{}{sessionID, rec.Type, rec.Message, boolFactString(accept), handledBy, now.UnixMilli()},
		Timestamp: now,
	}})
}

// recordDialog appends rec to the session's bounded history.
func (m *SessionManager) recordDialog(rec DialogRecord) {
	m.dialogs.mu.Lock()
	defer m.dialogs.mu.Unlock()
	m.dialogs.init()
	h := append(m.dialogs.history[rec.SessionID], rec)
	if len(h) > maxDialogHistory {
		h = h[len(h)-maxDialogHistory:]
	}
	m.dialogs.history[rec.SessionID] = h
}
