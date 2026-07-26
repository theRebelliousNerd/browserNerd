package browser

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	defaultStepRuntimeNavigationLimit = 8
	defaultStepRuntimeToastLimit      = 16
	defaultStepRuntimeConsoleLimit    = 16
	defaultStepRuntimeRequestLimit    = 32
)

// StepRuntimeEvidence captures a small, session-scoped view of the browser
// activity seen after a caller-defined checkpoint. It is intended for execute
// flows that need to decide whether a step visibly changed runtime state.
type StepRuntimeEvidence struct {
	SessionID          string               `json:"session_id"`
	SinceMs            int64                `json:"since_ms"`
	CapturedAtMs       int64                `json:"captured_at_ms"`
	Changed            bool                 `json:"changed"`
	RouteChange        *StepRouteChange     `json:"route_change,omitempty"`
	LatestToast        *StepToastEvidence   `json:"latest_toast,omitempty"`
	LatestConsoleError *StepConsoleEvidence `json:"latest_console_error,omitempty"`
	RequestIDsObserved []string             `json:"request_ids_observed,omitempty"`
}

// StepRouteChange describes the latest navigation observed after a checkpoint.
type StepRouteChange struct {
	FromURL     string `json:"from_url,omitempty"`
	ToURL       string `json:"to_url"`
	TimestampMs int64  `json:"timestamp_ms"`
}

// StepToastEvidence captures the latest toast-like notification seen after a
// checkpoint.
type StepToastEvidence struct {
	Text        string `json:"text"`
	Level       string `json:"level,omitempty"`
	Source      string `json:"source,omitempty"`
	TimestampMs int64  `json:"timestamp_ms"`
}

// StepConsoleEvidence captures the latest console error seen after a
// checkpoint.
type StepConsoleEvidence struct {
	Level       string `json:"level,omitempty"`
	Message     string `json:"message"`
	TimestampMs int64  `json:"timestamp_ms"`
}

type stepRequestObservation struct {
	RequestID   string
	URL         string
	TimestampMs int64
}

type sessionRuntimeEvidenceBuffer struct {
	mu            sync.RWMutex
	navigations   []StepRouteChange
	toasts        []StepToastEvidence
	consoleErrors []StepConsoleEvidence
	requests      []stepRequestObservation
}

func newSessionRuntimeEvidenceBuffer() *sessionRuntimeEvidenceBuffer {
	return &sessionRuntimeEvidenceBuffer{
		navigations:   make([]StepRouteChange, 0, defaultStepRuntimeNavigationLimit),
		toasts:        make([]StepToastEvidence, 0, defaultStepRuntimeToastLimit),
		consoleErrors: make([]StepConsoleEvidence, 0, defaultStepRuntimeConsoleLimit),
		requests:      make([]stepRequestObservation, 0, defaultStepRuntimeRequestLimit),
	}
}

func appendBounded[T any](items []T, item T, limit int) []T {
	items = append(items, item)
	if limit > 0 && len(items) > limit {
		items = append([]T(nil), items[len(items)-limit:]...)
	}
	return items
}

func (b *sessionRuntimeEvidenceBuffer) recordNavigation(fromURL, toURL string, ts time.Time) {
	if b == nil {
		return
	}
	fromURL = strings.TrimSpace(fromURL)
	toURL = strings.TrimSpace(toURL)
	if toURL == "" || toURL == fromURL {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.navigations = appendBounded(b.navigations, StepRouteChange{
		FromURL:     fromURL,
		ToURL:       toURL,
		TimestampMs: ts.UnixMilli(),
	}, defaultStepRuntimeNavigationLimit)
}

func (b *sessionRuntimeEvidenceBuffer) recordToast(text, level, source string, ts time.Time) {
	if b == nil {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.toasts = appendBounded(b.toasts, StepToastEvidence{
		Text:        text,
		Level:       strings.TrimSpace(level),
		Source:      strings.TrimSpace(source),
		TimestampMs: ts.UnixMilli(),
	}, defaultStepRuntimeToastLimit)
}

func (b *sessionRuntimeEvidenceBuffer) recordConsoleError(level, message string, ts time.Time) {
	if b == nil {
		return
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.consoleErrors = appendBounded(b.consoleErrors, StepConsoleEvidence{
		Level:       strings.TrimSpace(level),
		Message:     message,
		TimestampMs: ts.UnixMilli(),
	}, defaultStepRuntimeConsoleLimit)
}

func (b *sessionRuntimeEvidenceBuffer) recordRequest(requestID, rawURL string, ts time.Time) {
	if b == nil {
		return
	}
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	b.requests = appendBounded(b.requests, stepRequestObservation{
		RequestID:   requestID,
		URL:         strings.TrimSpace(rawURL),
		TimestampMs: ts.UnixMilli(),
	}, defaultStepRuntimeRequestLimit)
}

func (b *sessionRuntimeEvidenceBuffer) snapshotSince(sessionID string, since time.Time) StepRuntimeEvidence {
	sinceMs := since.UnixMilli()
	if since.IsZero() {
		sinceMs = 0
	}

	evidence := StepRuntimeEvidence{
		SessionID:    sessionID,
		SinceMs:      sinceMs,
		CapturedAtMs: time.Now().UnixMilli(),
	}
	if b == nil {
		return evidence
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	for i := len(b.navigations) - 1; i >= 0; i-- {
		if b.navigations[i].TimestampMs >= sinceMs {
			change := b.navigations[i]
			evidence.RouteChange = &change
			break
		}
	}

	for i := len(b.toasts) - 1; i >= 0; i-- {
		if b.toasts[i].TimestampMs >= sinceMs {
			toast := b.toasts[i]
			evidence.LatestToast = &toast
			break
		}
	}

	for i := len(b.consoleErrors) - 1; i >= 0; i-- {
		if b.consoleErrors[i].TimestampMs >= sinceMs {
			console := b.consoleErrors[i]
			evidence.LatestConsoleError = &console
			break
		}
	}

	requestIDs := make([]string, 0, defaultStepRuntimeRequestLimit)
	seen := make(map[string]bool, defaultStepRuntimeRequestLimit)
	for _, request := range b.requests {
		if request.TimestampMs < sinceMs {
			continue
		}
		if seen[request.RequestID] {
			continue
		}
		seen[request.RequestID] = true
		requestIDs = append(requestIDs, request.RequestID)
	}
	if len(requestIDs) > defaultStepRuntimeRequestLimit {
		requestIDs = append([]string(nil), requestIDs[len(requestIDs)-defaultStepRuntimeRequestLimit:]...)
	}
	evidence.RequestIDsObserved = requestIDs
	evidence.Changed = evidence.RouteChange != nil ||
		evidence.LatestToast != nil ||
		evidence.LatestConsoleError != nil ||
		len(evidence.RequestIDsObserved) > 0

	return evidence
}

// CaptureStepRuntimeEvidence returns the latest bounded runtime signals observed
// for a session after the provided checkpoint.
func (m *SessionManager) CaptureStepRuntimeEvidence(sessionID string, since time.Time) (StepRuntimeEvidence, error) {
	if strings.TrimSpace(sessionID) == "" {
		return StepRuntimeEvidence{}, fmt.Errorf("session_id is required")
	}

	sinceMs := since.UnixMilli()
	if since.IsZero() {
		sinceMs = 0
	}

	m.mu.RLock()
	record, ok := m.sessions[sessionID]
	m.mu.RUnlock()
	if !ok {
		return StepRuntimeEvidence{}, fmt.Errorf("unknown session: %s", sessionID)
	}

	if record == nil || record.activity == nil {
		return StepRuntimeEvidence{
			SessionID:    sessionID,
			SinceMs:      sinceMs,
			CapturedAtMs: time.Now().UnixMilli(),
		}, nil
	}

	return record.activity.snapshotSince(sessionID, since), nil
}
