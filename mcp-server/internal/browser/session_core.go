package browser

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"
)

// Session describes the public metadata for a tracked browser context.
type Session struct {
	ID              string    `json:"id"`
	BrowserID       string    `json:"browser_id,omitempty"`
	TargetID        string    `json:"target_id,omitempty"`
	URL             string    `json:"url,omitempty"`
	Title           string    `json:"title,omitempty"`
	Status          string    `json:"status,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	LastActive      time.Time `json:"last_active"`
	Isolated        bool      `json:"isolated"`
	IsolatedDisplay bool      `json:"isolated_display,omitempty"`
	// Viewport is the device the tab emulates after a viewport operation; nil
	// means the configured default.
	Viewport *Viewport `json:"viewport,omitempty"`
}

// BrowserInstance describes a connected Chrome process or external endpoint.
type BrowserInstance struct {
	ID              string    `json:"id"`
	ControlURL      string    `json:"control_url,omitempty"`
	Status          string    `json:"status"`
	Managed         bool      `json:"managed"`
	IsolatedDisplay bool      `json:"isolated_display,omitempty"`
	Default         bool      `json:"default"`
	CreatedAt       time.Time `json:"created_at"`
	LastActive      time.Time `json:"last_active"`
	TabCount        int       `json:"tab_count"`
}

type browserRecord struct {
	meta        BrowserInstance
	browser     *rod.Browser
	controlURL  string
	pid         int
	userDataDir string
	tempDir     bool
	display     *managedDisplay
}

type sessionRecord struct {
	meta         Session
	page         *rod.Page
	isolated     *rod.Browser
	registry     *ElementRegistry // Per-session element cache for reliable re-identification
	activity     *sessionRuntimeEvidenceBuffer
	net          *networkActivity   // Requests in flight; nil without an event stream
	streamCancel context.CancelFunc // Cancels long-lived background event ingestion for this session
	// attendedMs is when the last tool call that acted on or observed this
	// tab ended; a navigation after it happened while no call was looking.
	attendedMs int64
}

type eventThrottler struct {
	interval time.Duration
	mu       sync.Mutex
	last     map[string]time.Time
}

func newEventThrottler(ms int) *eventThrottler {
	if ms <= 0 {
		return nil
	}
	return &eventThrottler{
		interval: time.Duration(ms) * time.Millisecond,
		last:     make(map[string]time.Time),
	}
}

func (t *eventThrottler) Allow(key string) bool {
	if t == nil {
		return true
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if last, ok := t.last[key]; ok {
		if now.Sub(last) < t.interval {
			return false
		}
	}
	t.last[key] = now
	return true
}

// ElementFingerprint captures identifying properties of an element for reliable re-identification.
// This enables detection of stale element references when the DOM changes.
// Uses omitempty for sparse JSON serialization to reduce token bloat.
type ElementFingerprint struct {
	Ref          string             `json:"ref"`                     // Generated reference string
	TagName      string             `json:"tag_name"`                // Lowercase tag name (button, input, etc.)
	ID           string             `json:"id,omitempty"`            // Element ID attribute (if any)
	Name         string             `json:"name,omitempty"`          // Name attribute (if any)
	Classes      []string           `json:"classes,omitempty"`       // CSS class list
	TextContent  string             `json:"text_content,omitempty"`  // First 100 chars of text content
	AriaLabel    string             `json:"aria_label,omitempty"`    // aria-label attribute
	DataTestID   string             `json:"data_testid,omitempty"`   // data-testid attribute
	Role         string             `json:"role,omitempty"`          // ARIA role attribute
	RowKey       string             `json:"row_key,omitempty"`       // Grid row key/id (if detected)
	RowIndex     string             `json:"row_index,omitempty"`     // Grid row index (if detected)
	BoundingBox  map[string]float64 `json:"bounding_box,omitempty"`  // x, y, width, height
	AltSelectors []string           `json:"alt_selectors,omitempty"` // Alternative CSS selectors for fallback
	GeneratedAt  time.Time          `json:"generated_at,omitempty"`  // When the element was discovered
}

// ElementRegistry provides a per-session cache of discovered elements with fingerprints.
// This enables reliable element re-identification even when DOM changes occur.
type ElementRegistry struct {
	mu           sync.RWMutex
	elements     map[string]*ElementFingerprint // ref -> fingerprint
	generationID int                            // Increments on each full discovery or navigation
	lastCleared  time.Time                      // When the registry was last cleared
}

// NewElementRegistry creates a new empty element registry.
func NewElementRegistry() *ElementRegistry {
	return &ElementRegistry{
		elements:    make(map[string]*ElementFingerprint),
		lastCleared: time.Now(),
	}
}

// Register adds or updates an element fingerprint in the registry.
func (r *ElementRegistry) Register(fp *ElementFingerprint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.elements[fp.Ref] = fp
}

// RegisterBatch adds multiple fingerprints and increments the generation ID.
func (r *ElementRegistry) RegisterBatch(fps []*ElementFingerprint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generationID++
	for _, fp := range fps {
		r.elements[fp.Ref] = fp
	}
}

// Get retrieves a fingerprint by ref, returning nil if not found.
func (r *ElementRegistry) Get(ref string) *ElementFingerprint {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.elements[ref]
}

// Clear removes all elements and increments the generation ID.
// Called on navigation to invalidate all stale references.
func (r *ElementRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.elements = make(map[string]*ElementFingerprint)
	r.generationID++
	r.lastCleared = time.Now()
}

// GenerationID returns the current generation, useful for staleness detection.
func (r *ElementRegistry) GenerationID() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.generationID
}

// Count returns the number of registered elements.
func (r *ElementRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.elements)
}

// IncrementGeneration marks all cached elements as potentially stale without clearing them.
// Called on DOM updates to indicate that element positions/properties may have changed.
// This is lighter than Clear() - elements remain usable but staleness detection becomes active.
func (r *ElementRegistry) IncrementGeneration() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.generationID++
}

// SessionManager owns the detached Chrome instance and tracks active sessions.
type SessionManager struct {
	cfg              config.BrowserConfig
	engine           EngineSink
	mu               sync.RWMutex
	startMu          sync.Mutex
	browser          *rod.Browser
	sessions         map[string]*sessionRecord
	controlURL       string // WebSocket URL for DevTools
	redactor         *security.Redactor
	browsers         map[string]*browserRecord
	defaultBrowserID string
	reaperCancel     context.CancelFunc
	dialogs          dialogBook
	downloads        downloadBook
	querier          ProcessQuerier
	reapedOrphans    int
}

// EngineSink defines the minimal interface we need from the logic layer.
type EngineSink interface {
	AddFacts(ctx context.Context, facts []mangle.Fact) error
}

func NewSessionManager(cfg config.BrowserConfig, sink EngineSink) *SessionManager {
	return NewSessionManagerWithSecurity(cfg, sink, security.NewRedactor(nil))
}

// NewSessionManagerWithSecurity creates a manager with explicit persistence redaction.
func NewSessionManagerWithSecurity(cfg config.BrowserConfig, sink EngineSink, redactor *security.Redactor) *SessionManager {
	if redactor == nil {
		// Browser facts are queryable and may be persisted later. Keep this
		// boundary safe even when diagnostic recorder redaction is disabled.
		redactor = security.NewRedactor(nil)
	}
	return &SessionManager{
		cfg:      cfg,
		engine:   sink,
		sessions: make(map[string]*sessionRecord),
		redactor: redactor,
		browsers: make(map[string]*browserRecord),
		querier:  DefaultProcessQuerier(),
	}
}

// Start connects to an existing Chrome or launches a new one using Rod's launcher.
func (m *SessionManager) Start(ctx context.Context) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	for {
		health := m.ConnectionHealth(ctx)
		if health.Connected {
			return nil
		}
		if health.State == "not_started" {
			break
		}
		if err := m.retireClosedDefault(health); err != nil {
			return err
		}
	}

	if err := m.loadSessions(); err != nil {
		return fmt.Errorf("load sessions: %w", err)
	}

	launched, err := m.startBrowser()
	if err != nil {
		return err
	}

	// Browser lifetime must not depend on request-scoped tool contexts.
	// Use a long-lived background context so later tool calls can reuse the same connection.
	controlURL := launched.controlURL
	browser := rod.New().ControlURL(controlURL).Context(context.Background())
	if err := browser.Connect(); err != nil {
		m.killLaunchedChrome(launched)
		return fmt.Errorf("connect to chrome: %w", err)
	}

	m.mu.Lock()
	m.browser = browser
	m.controlURL = controlURL
	browserID := uuid.NewString()
	now := time.Now()
	m.defaultBrowserID = browserID
	record := &browserRecord{
		meta: BrowserInstance{
			ID:              browserID,
			ControlURL:      m.redactor.SanitizeString(controlURL),
			Status:          "connected",
			Managed:         m.cfg.DebuggerURL == "",
			IsolatedDisplay: launched.display != nil,
			Default:         true,
			CreatedAt:       now,
			LastActive:      now,
		},
		browser:    browser,
		controlURL: controlURL,
	}
	record.pid = launched.pid
	record.userDataDir = launched.userDataDir
	record.tempDir = launched.tempDir
	record.display = launched.display
	m.browsers[browserID] = record
	m.mu.Unlock()
	if m.cfg.DebuggerURL == "" {
		// Only a browser this server launched is redirected to the download
		// directory; an attached everyday Chrome keeps its own behavior.
		m.enableDownloads(browser, "")
		m.listenForDownloads(browser)
	}
	m.startIdleReaper()
	log.Printf("Browser connected at %s", controlURL)
	return nil
}

// startBrowser launches a managed Chrome, or names the browser debugger_url
// attaches to (no process or profile of ours). A debugger_url naming only the
// endpoint ("ws://localhost:9222", "127.0.0.1:9222") is resolved to the
// browser's /devtools/browser/<id> socket, the only URL Chrome accepts a CDP
// connection on.
func (m *SessionManager) startBrowser() (*launchedChrome, error) {
	if err := m.cfg.ValidateDisplayIsolation(); err != nil {
		return nil, err
	}
	if m.cfg.DebuggerURL == "" {
		return m.launchChrome()
	}
	if strings.Contains(m.cfg.DebuggerURL, "/devtools/") {
		return &launchedChrome{controlURL: m.cfg.DebuggerURL}, nil
	}
	resolved, err := launcher.ResolveURL(m.cfg.DebuggerURL)
	if err != nil {
		return nil, fmt.Errorf("resolve debugger_url %s: %w", m.redactor.SanitizeString(m.cfg.DebuggerURL), err)
	}
	return &launchedChrome{controlURL: resolved}, nil
}

// ControlURL returns the WebSocket debugger URL for the connected browser.
func (m *SessionManager) ControlURL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.controlURL
}

// IsConnected verifies the default CDP transport within a bounded probe.
func (m *SessionManager) IsConnected() bool {
	return m.ConnectionHealth(context.Background()).Connected
}

// Shutdown closes tracked pages, closes managed browsers, terminates the
// Chrome process trees this server launched and deletes their temporary
// profiles. An attached browser (debugger_url) is left running.
func (m *SessionManager) Shutdown(_ context.Context) error {
	m.mu.Lock()
	sessionRecords := make([]*sessionRecord, 0, len(m.sessions))
	for id, record := range m.sessions {
		if record != nil {
			if record.streamCancel != nil {
				record.streamCancel()
			}
			sessionRecords = append(sessionRecords, record)
		}
		delete(m.sessions, id)
	}
	if m.reaperCancel != nil {
		m.reaperCancel()
		m.reaperCancel = nil
	}
	records := make([]*browserRecord, 0, len(m.browsers))
	for id, record := range m.browsers {
		if record != nil {
			records = append(records, record)
		}
		delete(m.browsers, id)
	}
	m.browser = nil
	m.controlURL = ""
	m.defaultBrowserID = ""
	if m.querier == nil {
		m.querier = DefaultProcessQuerier()
	}
	q := m.querier
	m.mu.Unlock()

	var err error
	for _, record := range sessionRecords {
		if closeErr := closeSessionResources(record); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	for _, record := range records {
		if closeErr := teardownBrowser(q, record); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	log.Printf("Browser shutdown complete")
	return err
}

func closeSessionResources(record *sessionRecord) error {
	if record == nil {
		return nil
	}
	var err error
	if record.page != nil {
		err = record.page.Close()
	}
	if record.isolated != nil {
		if closeErr := record.isolated.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

// clearSessionsLocked cancels session streams and optionally closes page handles.
// Caller must hold m.mu.
func (m *SessionManager) clearSessionsLocked(closePages bool) {
	for id, record := range m.sessions {
		if record != nil {
			if record.streamCancel != nil {
				record.streamCancel()
			}
			if closePages && record.page != nil {
				_ = record.page.Close()
			}
		}
		delete(m.sessions, id)
	}
}

// List returns lightweight metadata for all known sessions.
func (m *SessionManager) List() []Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make([]Session, 0, len(m.sessions))
	for _, record := range m.sessions {
		results = append(results, record.meta)
	}
	return results
}

// CreateSession opens a shared-context tab by default.
func (m *SessionManager) CreateSession(ctx context.Context, url string) (*Session, error) {
	return m.CreateTab(ctx, "", url, !m.cfg.IsMultiTabDefault())
}

// Attach attempts to bind to an existing target by TargetID.
func (m *SessionManager) Attach(ctx context.Context, targetID string) (*Session, error) {
	return m.AttachToBrowser(ctx, "", targetID)
}

// Page returns the underlying Rod page for a session when present.
func (m *SessionManager) Page(sessionID string) (*rod.Page, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.sessions[sessionID]
	if !ok || rec.page == nil {
		return nil, false
	}
	rec.meta.LastActive = time.Now()
	return rec.page, true
}

// Registry returns the element registry for a session.
// Returns nil if session doesn't exist.
func (m *SessionManager) Registry(sessionID string) *ElementRegistry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.sessions[sessionID]
	if !ok || rec.registry == nil {
		return nil
	}
	return rec.registry
}

// UpdateMetadata allows tools to refresh metadata (e.g., URL/title after navigation).
func (m *SessionManager) UpdateMetadata(sessionID string, updater func(Session) Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.sessions[sessionID]
	if !ok {
		return
	}
	rec.meta = updater(rec.meta)
	rec.meta.LastActive = time.Now()
}

// GetSession returns the current session metadata when available.
func (m *SessionManager) GetSession(sessionID string) (Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.sessions[sessionID]
	if !ok {
		return Session{}, false
	}
	return rec.meta, true
}

// ReifyReact returns a bounded structured Fiber subtree, with optional fact submission.
func (m *SessionManager) ReifyReact(ctx context.Context, sessionID string, root *rod.Element, options SnapshotOptions) (*StructuredSnapshot, error) {
	return m.captureStructured(ctx, sessionID, root, options, true)
}

func (m *SessionManager) recordNavigationEvent(sessionID, toURL string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record, ok := m.sessions[sessionID]
	if !ok || record == nil {
		return
	}
	if record.activity == nil {
		record.activity = newSessionRuntimeEvidenceBuffer()
	}

	record.activity.recordNavigation(record.meta.URL, toURL, at)
	record.meta.URL = toURL
	record.meta.LastActive = at
}

func (m *SessionManager) recordToastEvent(sessionID, text, level, source string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record, ok := m.sessions[sessionID]
	if !ok || record == nil {
		return
	}
	if record.activity == nil {
		record.activity = newSessionRuntimeEvidenceBuffer()
	}
	record.activity.recordToast(text, level, source, at)
}

func (m *SessionManager) recordConsoleErrorEvent(sessionID, level, message string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record, ok := m.sessions[sessionID]
	if !ok || record == nil {
		return
	}
	if record.activity == nil {
		record.activity = newSessionRuntimeEvidenceBuffer()
	}
	record.activity.recordConsoleError(level, message, at)
}

func (m *SessionManager) recordRequestEvent(sessionID, requestID, rawURL string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	record, ok := m.sessions[sessionID]
	if !ok || record == nil {
		return
	}
	if record.activity == nil {
		record.activity = newSessionRuntimeEvidenceBuffer()
	}
	record.activity.recordRequest(requestID, rawURL, at)
}

// ForkSession clones cookies + storage from an existing session into a new incognito context.
func (m *SessionManager) ForkSession(ctx context.Context, sessionID, url string) (*Session, error) {
	srcPage, ok := m.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("unknown session: %s", sessionID)
	}

	srcMeta, _ := m.GetSession(sessionID)

	// Snapshot cookies
	cookiesRes, err := proto.NetworkGetCookies{}.Call(srcPage)
	if err != nil {
		return nil, fmt.Errorf("get cookies: %w", err)
	}

	// Snapshot storage (best-effort)
	localJSON := snapshotStorage(srcPage, "localStorage")
	sessionJSON := snapshotStorage(srcPage, "sessionStorage")

	targetURL := url
	if targetURL == "" {
		targetURL = srcMeta.URL
		if targetURL == "" {
			targetURL = "about:blank"
		}
	}

	params := make([]*proto.NetworkCookieParam, 0, len(cookiesRes.Cookies))
	for _, c := range cookiesRes.Cookies {
		params = append(params, &proto.NetworkCookieParam{
			Name:     c.Name,
			Value:    c.Value,
			Domain:   c.Domain,
			Path:     c.Path,
			Expires:  c.Expires,
			HTTPOnly: c.HTTPOnly,
			Secure:   c.Secure,
			SameSite: c.SameSite,
			Priority: c.Priority,
		})
	}

	// The fork's first page load must already carry the source's cookies and
	// storage, so both are in place before it navigates: cookies set on the
	// fresh context, storage written by a script that runs before the
	// document's own scripts on the source's origin.
	var restoreScript proto.PageScriptIdentifier
	prepare := func(page *rod.Page) {
		if len(params) > 0 {
			_ = page.SetCookies(params)
		}
		if script := storageRestoreScript(srcMeta.URL, localJSON, sessionJSON); script != "" {
			if res, err := (proto.PageAddScriptToEvaluateOnNewDocument{Source: script}).Call(page); err == nil {
				restoreScript = res.Identifier
			}
		}
	}
	dest, err := m.openTab(ctx, srcMeta.BrowserID, targetURL, true, prepare)
	if err != nil {
		return nil, fmt.Errorf("create forked session: %w", err)
	}
	if destPage, ok := m.Page(dest.ID); ok && restoreScript != "" {
		_ = proto.PageRemoveScriptToEvaluateOnNewDocument{Identifier: restoreScript}.Call(destPage)
	}
	dest.Status = "forked"
	m.UpdateMetadata(dest.ID, func(s Session) Session {
		s.Status = "forked"
		return s
	})

	_ = m.persistSessions()
	return dest, nil
}

// startEventStream wires Rod CDP events into the fact sink (console + network + navigation).
