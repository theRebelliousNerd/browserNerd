package browser

import (
	"context"
	"errors"
	"fmt"
	"log"
	"runtime"
	"strings"
	"time"

	"browsernerd-mcp-server/internal/config"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"
)

// ListBrowsers returns connected browser instances with current tab counts.
func (m *SessionManager) ListBrowsers() []BrowserInstance {
	m.mu.RLock()
	defer m.mu.RUnlock()
	counts := make(map[string]int)
	for _, session := range m.sessions {
		if session != nil {
			counts[session.meta.BrowserID]++
		}
	}
	out := make([]BrowserInstance, 0, len(m.browsers))
	for id, record := range m.browsers {
		if record == nil {
			continue
		}
		meta := record.meta
		meta.TabCount = counts[id]
		out = append(out, meta)
	}
	return out
}

// LaunchAdditional starts a separate managed Chrome process.
func (m *SessionManager) LaunchAdditional(_ context.Context) (*BrowserInstance, error) {
	m.mu.RLock()
	browserCount := len(m.browsers)
	m.mu.RUnlock()
	if browserCount >= m.cfg.GetMaxBrowsers() {
		return nil, fmt.Errorf("browser limit reached (%d)", m.cfg.GetMaxBrowsers())
	}
	controlURL, err := m.launchChrome()
	if err != nil {
		return nil, err
	}
	browser := rod.New().ControlURL(controlURL).Context(context.Background())
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("connect additional chrome: %w", err)
	}

	now := time.Now()
	meta := BrowserInstance{
		ID:         uuid.NewString(),
		ControlURL: m.redactor.SanitizeString(controlURL),
		Status:     "connected",
		Managed:    true,
		CreatedAt:  now,
		LastActive: now,
	}
	m.mu.Lock()
	m.browsers[meta.ID] = &browserRecord{meta: meta, browser: browser, controlURL: controlURL}
	m.mu.Unlock()
	m.enableDownloads(browser, "")
	m.listenForDownloads(browser)
	return &meta, nil
}

// openTabCountLocked counts the sessions that hold an open page - the tabs the
// max_tabs bound exists to limit. A detached session (reloaded from the session
// store after a restart, or left behind when its browser went away) owns no
// tab, so it never blocks opening one; counting it let a long-lived workspace
// reach the limit with no browser running at all. Callers hold m.mu.
func (m *SessionManager) openTabCountLocked() int {
	open := 0
	for _, record := range m.sessions {
		if record.page != nil {
			open++
		}
	}
	return open
}

// CreateTab opens a tab in a selected browser. Shared-context tabs are the
// default; isolated=true creates a fresh incognito context.
func (m *SessionManager) CreateTab(ctx context.Context, browserID, rawURL string, isolated bool) (*Session, error) {
	return m.openTab(ctx, browserID, rawURL, isolated, nil)
}

// openTab opens the tab at about:blank, starts capture, runs prepare (cookies,
// storage) and only then navigates, so the page load is captured from its
// first request and runs with the prepared state.
func (m *SessionManager) openTab(_ context.Context, browserID, rawURL string, isolated bool, prepare func(*rod.Page)) (*Session, error) {
	m.mu.RLock()
	if m.openTabCountLocked() >= m.cfg.GetMaxTabs() {
		m.mu.RUnlock()
		return nil, fmt.Errorf("tab limit reached (%d)", m.cfg.GetMaxTabs())
	}
	record, resolvedID := m.browserRecordLocked(browserID)
	m.mu.RUnlock()
	if record == nil || record.browser == nil {
		return nil, errors.New("browser not connected")
	}
	if strings.TrimSpace(rawURL) == "" {
		rawURL = "about:blank"
	}

	targetBrowser := record.browser
	var isolatedBrowser *rod.Browser
	if isolated {
		var err error
		targetBrowser, err = record.browser.Incognito()
		if err != nil {
			return nil, fmt.Errorf("create isolated browser context: %w", err)
		}
		isolatedBrowser = targetBrowser
		// An incognito context is this server's own, even in an attached browser.
		m.enableDownloads(record.browser, targetBrowser.BrowserContextID)
		m.listenForDownloads(record.browser)
	}
	page, err := targetBrowser.Page(proto.TargetCreateTarget{URL: "about:blank"})
	if err != nil {
		return nil, fmt.Errorf("create tab: %w", err)
	}
	if err := (proto.EmulationSetDeviceMetricsOverride{
		Width:             m.cfg.GetViewportWidth(),
		Height:            m.cfg.GetViewportHeight(),
		DeviceScaleFactor: 1.0,
		Mobile:            false,
	}).Call(page); err != nil {
		log.Printf("warning: failed to set viewport: %v", err)
	}

	now := time.Now()
	meta := Session{
		ID:         uuid.NewString(),
		BrowserID:  resolvedID,
		TargetID:   string(page.TargetID),
		URL:        rawURL,
		Status:     "active",
		CreatedAt:  now,
		LastActive: now,
		Isolated:   isolated,
	}
	streamCtx, streamCancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.sessions[meta.ID] = &sessionRecord{
		meta:         meta,
		page:         page,
		isolated:     isolatedBrowser,
		registry:     NewElementRegistry(),
		activity:     newSessionRuntimeEvidenceBuffer(),
		streamCancel: streamCancel,
	}
	if browserRecord := m.browsers[resolvedID]; browserRecord != nil {
		browserRecord.meta.LastActive = now
	}
	m.mu.Unlock()
	m.startEventStream(streamCtx, meta.ID, page)
	if prepare != nil {
		prepare(page)
	}
	if rawURL != "about:blank" {
		// A failed load still leaves an open tab; page_load_failed and
		// net_loading_failed carry why.
		_ = page.Timeout(m.cfg.NavigationTimeout()).Navigate(rawURL)
		waitDocumentParsed(page, m.cfg.NavigationTimeout())
	}
	_ = m.persistSessions()
	return &meta, nil
}

// AttachToBrowser binds a target in a selected browser instance.
func (m *SessionManager) AttachToBrowser(_ context.Context, browserID, targetID string) (*Session, error) {
	m.mu.RLock()
	if m.openTabCountLocked() >= m.cfg.GetMaxTabs() {
		m.mu.RUnlock()
		return nil, fmt.Errorf("tab limit reached (%d)", m.cfg.GetMaxTabs())
	}
	record, resolvedID := m.browserRecordLocked(browserID)
	m.mu.RUnlock()
	if record == nil || record.browser == nil {
		return nil, errors.New("browser not connected")
	}
	page, err := record.browser.PageFromTarget(proto.TargetTargetID(targetID))
	if err != nil {
		return nil, fmt.Errorf("attach to target %s: %w", targetID, err)
	}
	now := time.Now()
	meta := Session{
		ID:         uuid.NewString(),
		BrowserID:  resolvedID,
		TargetID:   targetID,
		Status:     "attached",
		CreatedAt:  now,
		LastActive: now,
	}
	streamCtx, streamCancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.sessions[meta.ID] = &sessionRecord{
		meta:         meta,
		page:         page,
		registry:     NewElementRegistry(),
		activity:     newSessionRuntimeEvidenceBuffer(),
		streamCancel: streamCancel,
	}
	m.mu.Unlock()
	m.startEventStream(streamCtx, meta.ID, page)
	if m.engine != nil && m.cfg.EnableDOMIngestion && !strings.EqualFold(m.cfg.EventLoggingLevel, "minimal") {
		// An attached tab has already loaded: snapshot the DOM it shows now.
		_ = m.captureDOMFacts(streamCtx, meta.ID, page)
	}
	_ = m.persistSessions()
	return &meta, nil
}

// FocusSession activates a tab without changing the session used by other tools.
func (m *SessionManager) FocusSession(sessionID string) error {
	page, ok := m.Page(sessionID)
	if !ok {
		return fmt.Errorf("unknown session: %s", sessionID)
	}
	if _, err := page.Activate(); err != nil {
		return fmt.Errorf("activate tab: %w", err)
	}
	m.UpdateMetadata(sessionID, func(session Session) Session {
		session.Status = "active"
		session.LastActive = time.Now()
		return session
	})
	return nil
}

// CloseSession idempotently stops ingestion and closes one tab.
func (m *SessionManager) CloseSession(sessionID string) (bool, error) {
	m.mu.Lock()
	record, ok := m.sessions[sessionID]
	if !ok {
		m.mu.Unlock()
		return false, nil
	}
	delete(m.sessions, sessionID)
	if record.streamCancel != nil {
		record.streamCancel()
	}
	m.mu.Unlock()
	err := closeSessionResources(record)
	_ = m.persistSessions()
	return true, err
}

// CloseBrowser closes one instance and all tabs belonging to it.
func (m *SessionManager) CloseBrowser(browserID string) (bool, error) {
	m.mu.Lock()
	record, ok := m.browsers[browserID]
	if !ok {
		m.mu.Unlock()
		return false, nil
	}
	sessionRecords := make([]*sessionRecord, 0)
	for sessionID, session := range m.sessions {
		if session == nil || session.meta.BrowserID != browserID {
			continue
		}
		if session.streamCancel != nil {
			session.streamCancel()
		}
		sessionRecords = append(sessionRecords, session)
		delete(m.sessions, sessionID)
	}
	delete(m.browsers, browserID)
	if browserID == m.defaultBrowserID {
		m.defaultBrowserID = ""
		m.browser = nil
		m.controlURL = ""
		for id, candidate := range m.browsers {
			m.defaultBrowserID = id
			m.browser = candidate.browser
			m.controlURL = candidate.controlURL
			candidate.meta.Default = true
			break
		}
	}
	m.mu.Unlock()
	var err error
	for _, session := range sessionRecords {
		if closeErr := closeSessionResources(session); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	if record.browser != nil {
		if closeErr := record.browser.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	_ = m.persistSessions()
	return true, err
}

func (m *SessionManager) browserRecordLocked(browserID string) (*browserRecord, string) {
	if browserID == "" {
		browserID = m.defaultBrowserID
	}
	if record := m.browsers[browserID]; record != nil {
		return record, browserID
	}
	// Compatibility for tests or embedders that set the historical browser field.
	if browserID == "" && m.browser != nil {
		return &browserRecord{browser: m.browser}, ""
	}
	return nil, browserID
}

// ChromeBinary names the browser executable a managed launch runs:
// browser.launch[0] when configured, else the installed Chrome, Chromium or
// Edge that Rod finds on this platform. Empty means none is installed and Rod
// downloads its pinned Chromium build on first launch.
func ChromeBinary(cfg config.BrowserConfig) string {
	if len(cfg.Launch) > 0 {
		return cfg.Launch[0]
	}
	if path, ok := launcher.LookPath(); ok {
		return path
	}
	return ""
}

// newLauncher builds a Chrome launcher for ChromeBinary.
//
// Rod's leakless guard kills Chrome when this server dies without shutting it
// down, so a crashed or killed server leaves no orphaned browser. Leakless execs
// a helper binary it drops in the temp directory, and Windows Defender
// quarantines that helper ("the file contains a virus"), so on Windows the
// guard is off and shutdown-browser and session teardown own Chrome's lifetime.
func (m *SessionManager) newLauncher() *launcher.Launcher {
	launch := launcher.New().Headless(m.cfg.IsHeadless()).Leakless(runtime.GOOS != "windows")
	if bin := ChromeBinary(m.cfg); bin != "" {
		launch = launch.Bin(bin)
	}
	return launch
}

func (m *SessionManager) launchChrome() (string, error) {
	launch := m.newLauncher()
	flagStart := 0
	if len(m.cfg.Launch) > 0 {
		flagStart = 1
	}
	for _, rawFlag := range m.cfg.Launch[flagStart:] {
		flagString := strings.TrimLeft(rawFlag, "-")
		name, value, hasValue := strings.Cut(flagString, "=")
		if hasValue {
			launch = launch.Set(flags.Flag(name), value)
		} else {
			launch = launch.Set(flags.Flag(name))
		}
	}
	controlURL, err := launch.Launch()
	if err == nil {
		return controlURL, nil
	}
	alternate, fallbackErr := m.newLauncher().Launch()
	if fallbackErr != nil {
		return "", fmt.Errorf("launch chrome: %w (fallback: %v)", err, fallbackErr)
	}
	return alternate, nil
}

func (m *SessionManager) startIdleReaper() {
	timeout := m.cfg.GetIdleTabTimeout()
	if timeout <= 0 {
		return
	}
	m.mu.Lock()
	if m.reaperCancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.reaperCancel = cancel
	m.mu.Unlock()

	interval := timeout / 2
	if interval < time.Second {
		interval = time.Second
	}
	if interval > time.Minute {
		interval = time.Minute
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				m.reapIdleTabs(now, timeout)
			}
		}
	}()
}

func (m *SessionManager) reapIdleTabs(now time.Time, timeout time.Duration) {
	m.mu.RLock()
	stale := make([]string, 0)
	for id, record := range m.sessions {
		if record != nil && now.Sub(record.meta.LastActive) >= timeout {
			stale = append(stale, id)
		}
	}
	m.mu.RUnlock()
	for _, id := range stale {
		_, _ = m.CloseSession(id)
	}
}
