package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/go-rod/rod"
)

const connectionProbeBound = 2 * time.Second

// ConnectionHealth is a fresh default-instance transport assessment. It
// grants no authority to close an instance or to replay a page operation.
type ConnectionHealth struct {
	BrowserID string `json:"browser_id,omitempty"`
	Connected bool   `json:"connected"`
	State     string `json:"state"`
	Reason    string `json:"reason,omitempty"`
	Error     string `json:"error,omitempty"`
	browser   *rod.Browser
}

func (m *SessionManager) ConnectionHealth(ctx context.Context) ConnectionHealth {
	m.mu.RLock()
	b, id := m.browser, m.defaultBrowserID
	m.mu.RUnlock()
	h := ConnectionHealth{BrowserID: id, State: "not_started", Reason: "browser_not_started", browser: b}
	if b == nil {
		return h
	}
	probeCtx, cancel := context.WithTimeout(ctx, connectionProbeBound)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := b.Context(probeCtx).Version(); result <- err }()
	var err error
	select {
	case err = <-result:
	case <-probeCtx.Done():
		err = probeCtx.Err()
	}
	m.mu.RLock()
	same := m.browser == b && m.defaultBrowserID == id
	m.mu.RUnlock()
	if !same {
		h.State = "unverified"
		h.Reason = "cdp_connection_changed"
		return h
	}
	if err == nil {
		h.Connected = true
		h.State = "reachable"
		h.Reason = "cdp_version_verified"
		return h
	}
	h.State = "unverified"
	h.Error = err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		h.Reason = "cdp_probe_timeout"
	case errors.Is(err, context.Canceled):
		h.Reason = "cdp_probe_canceled"
	case errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		h.State = "unreachable"
		h.Reason = "cdp_transport_closed"
	default:
		h.Reason = "cdp_probe_failed"
	}
	return h
}

// retireClosedDefault is only entered by explicit Start. It retires one
// proved-exited, owned default; unknown transport/PID state never authorizes
// cleanup. Clearing the exited PID prevents a signal to a reused PID.
func (m *SessionManager) retireClosedDefault(h ConnectionHealth) error {
	if h.State != "unreachable" || h.Reason != "cdp_transport_closed" {
		return fmt.Errorf("browser %s transport unverified (%s): %s; no lifecycle changes", h.BrowserID, h.Reason, h.Error)
	}
	m.mu.Lock()
	record := m.browsers[h.BrowserID]
	refuse := func(reason string) error {
		m.mu.Unlock()
		return fmt.Errorf("browser %s recovery refused: %s; use explicit owned-session cleanup", h.BrowserID, reason)
	}
	if record == nil || m.defaultBrowserID != h.BrowserID || record.browser != h.browser || m.browser != h.browser {
		return refuse("cdp_connection_changed")
	}
	if !record.meta.Managed || record.pid <= 0 || !record.tempDir || !isBrowserNERDUserDataDir(record.userDataDir) || profileOwner(record.userDataDir) != os.Getpid() {
		return refuse("browser_process_ownership_unverified")
	}
	for _, s := range m.sessions {
		if s != nil && s.meta.BrowserID == h.BrowserID && s.page != nil {
			return refuse("browser_has_open_contexts")
		}
	}
	exited, err := ownedProcessExited(record.pid)
	if err != nil {
		return refuse("browser_process_liveness_unverified: " + err.Error())
	}
	if !exited {
		return refuse("browser_process_still_live_or_pid_reused")
	}
	record.pid = 0
	record.browser = nil
	m.mu.Unlock()
	_, err = m.CloseBrowser(h.BrowserID)
	if err != nil {
		return fmt.Errorf("retire exited owned browser %s: %w", h.BrowserID, err)
	}
	return nil
}
