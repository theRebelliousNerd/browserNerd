package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"
)

// Session describes the public metadata for a tracked browser context.
type Session struct {
	ID         string    `json:"id"`
	BrowserID  string    `json:"browser_id,omitempty"`
	TargetID   string    `json:"target_id,omitempty"`
	URL        string    `json:"url,omitempty"`
	Title      string    `json:"title,omitempty"`
	Status     string    `json:"status,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastActive time.Time `json:"last_active"`
	Isolated   bool      `json:"isolated"`
}

// BrowserInstance describes a connected Chrome process or external endpoint.
type BrowserInstance struct {
	ID         string    `json:"id"`
	ControlURL string    `json:"control_url,omitempty"`
	Status     string    `json:"status"`
	Managed    bool      `json:"managed"`
	Default    bool      `json:"default"`
	CreatedAt  time.Time `json:"created_at"`
	LastActive time.Time `json:"last_active"`
	TabCount   int       `json:"tab_count"`
}

type browserRecord struct {
	meta       BrowserInstance
	browser    *rod.Browser
	controlURL string
}

type sessionRecord struct {
	meta         Session
	page         *rod.Page
	isolated     *rod.Browser
	registry     *ElementRegistry // Per-session element cache for reliable re-identification
	activity     *sessionRuntimeEvidenceBuffer
	streamCancel context.CancelFunc // Cancels long-lived background event ingestion for this session
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
	browser          *rod.Browser
	sessions         map[string]*sessionRecord
	controlURL       string // WebSocket URL for DevTools
	redactor         *security.Redactor
	browsers         map[string]*browserRecord
	defaultBrowserID string
	reaperCancel     context.CancelFunc
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
	}
}

// Start connects to an existing Chrome or launches a new one using Rod's launcher.
func (m *SessionManager) Start(_ context.Context) error {
	// If we already have a browser, verify it's still alive
	if m.browser != nil {
		// Try a simple operation to test connection health
		_, err := m.browser.Version()
		if err == nil {
			return nil // Browser is healthy, reuse it
		}
		// Browser is dead, clean up and reconnect
		log.Printf("Stale browser connection detected, reconnecting...")
		_ = m.browser.Close()
		m.browser = nil
		m.controlURL = ""
		// Clear all sessions since they're orphaned
		m.mu.Lock()
		m.clearSessionsLocked(false)
		m.browsers = make(map[string]*browserRecord)
		m.defaultBrowserID = ""
		m.mu.Unlock()
	}

	if err := m.loadSessions(); err != nil {
		return fmt.Errorf("load sessions: %w", err)
	}

	controlURL := m.cfg.DebuggerURL
	if controlURL == "" {
		var err error
		controlURL, err = m.launchChrome()
		if err != nil {
			return err
		}
	}

	// Browser lifetime must not depend on request-scoped tool contexts.
	// Use a long-lived background context so later tool calls can reuse the same connection.
	browser := rod.New().ControlURL(controlURL).Context(context.Background())
	if err := browser.Connect(); err != nil {
		return fmt.Errorf("connect to chrome: %w", err)
	}

	m.browser = browser
	m.controlURL = controlURL
	m.mu.Lock()
	browserID := uuid.NewString()
	now := time.Now()
	m.defaultBrowserID = browserID
	m.browsers[browserID] = &browserRecord{
		meta: BrowserInstance{
			ID:         browserID,
			ControlURL: m.redactor.SanitizeString(controlURL),
			Status:     "connected",
			Managed:    m.cfg.DebuggerURL == "",
			Default:    true,
			CreatedAt:  now,
			LastActive: now,
		},
		browser:    browser,
		controlURL: controlURL,
	}
	m.mu.Unlock()
	m.startIdleReaper()
	log.Printf("Browser connected at %s", controlURL)
	return nil
}

// ControlURL returns the WebSocket debugger URL for the connected browser.
func (m *SessionManager) ControlURL() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.controlURL
}

// IsConnected returns whether the browser is currently connected.
func (m *SessionManager) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.browser != nil
}

// Shutdown closes tracked pages and the underlying browser.
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
	browsers := make([]*rod.Browser, 0, len(m.browsers))
	for id, record := range m.browsers {
		if record != nil && record.browser != nil {
			browsers = append(browsers, record.browser)
		}
		delete(m.browsers, id)
	}
	m.browser = nil
	m.controlURL = ""
	m.defaultBrowserID = ""
	m.mu.Unlock()

	var err error
	for _, record := range sessionRecords {
		if closeErr := closeSessionResources(record); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	for _, browser := range browsers {
		if closeErr := browser.Close(); closeErr != nil && err == nil {
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

// ReifyReact walks the React Fiber tree and emits facts for components, props, and state.
func (m *SessionManager) ReifyReact(ctx context.Context, sessionID string) ([]mangle.Fact, error) {
	if m.engine == nil {
		return nil, errors.New("mangle engine not configured")
	}
	page, ok := m.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("unknown session: %s", sessionID)
	}

	res, err := page.Context(ctx).Evaluate(&rod.EvalOptions{
		JS: `
		() => {
			const root = document.querySelector('[data-reactroot]') || document.getElementById('root') || document.body;
			if (!root) return [];
			const fiberKey = Object.keys(root).find(k => k.startsWith('__reactFiber'));
			if (!fiberKey) return [];

			const sanitize = (v) => {
				if (v === null) return null;
				const t = typeof v;
				if (t === 'string' || t === 'number' || t === 'boolean') return v;
				return undefined;
			};

			const rootFiber = root[fiberKey];
			const stack = [{ fiber: rootFiber, parent: null }];
			const seen = new Set();
			const results = [];
			let counter = 0;

			while (stack.length) {
				const { fiber, parent } = stack.pop();
				if (!fiber || seen.has(fiber)) continue;
				seen.add(fiber);

				const id = fiber._debugID || ('fiber_' + (counter++));
				const name = (fiber.type && (fiber.type.displayName || fiber.type.name)) ||
							 (fiber.elementType && fiber.elementType.name) ||
							 'Anonymous';

				const props = {};
				if (fiber.memoizedProps && typeof fiber.memoizedProps === 'object') {
					for (const [k, v] of Object.entries(fiber.memoizedProps)) {
						const s = sanitize(v);
						if (s !== undefined) props[k] = s;
					}
				}

				const state = [];
				if (fiber.memoizedState !== undefined) {
					const ms = fiber.memoizedState;
					if (Array.isArray(ms)) {
						ms.forEach((v, i) => {
							const s = sanitize(v);
							if (s !== undefined) state.push([i, s]);
						});
					} else if (ms && typeof ms === 'object' && 'baseState' in ms) {
						const s = sanitize(ms.baseState);
						if (s !== undefined) state.push([0, s]);
					}
				}

				const domNodeId = fiber.stateNode && fiber.stateNode.id ? fiber.stateNode.id : null;
				results.push({ id, name, parent, props, state, domNodeId });

				if (fiber.child) stack.push({ fiber: fiber.child, parent: id });
				if (fiber.sibling) stack.push({ fiber: fiber.sibling, parent });
			}
			return results;
		}
		`,
		ByValue:      true,
		AwaitPromise: true,
	})
	if err != nil || res == nil {
		return nil, fmt.Errorf("react reification failed: %w", err)
	}

	raw, err := res.Value.MarshalJSON()
	if err != nil {
		return nil, fmt.Errorf("marshal reified tree: %w", err)
	}

	var nodes []struct {
		ID        string                 `json:"id"`
		Name      string                 `json:"name"`
		Parent    *string                `json:"parent"`
		Props     map[string]interface{} `json:"props"`
		State     [][]interface{}        `json:"state"`
		DomNodeID *string                `json:"domNodeId"`
	}
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return nil, fmt.Errorf("decode reified tree: %w", err)
	}

	facts := make([]mangle.Fact, 0, len(nodes)*4)
	now := time.Now()

	for _, n := range nodes {
		parent := ""
		if n.Parent != nil {
			parent = *n.Parent
		}
		facts = append(facts, mangle.Fact{
			Predicate: "react_component",
			Args:      []interface{}{sessionID, n.ID, n.Name, parent},
			Timestamp: now,
		})

		for k, v := range n.Props {
			facts = append(facts, mangle.Fact{
				Predicate: "react_prop",
				Args:      []interface{}{sessionID, n.ID, k, fmt.Sprintf("%v", v)},
				Timestamp: now,
			})
		}

		for _, entry := range n.State {
			if len(entry) != 2 {
				continue
			}
			facts = append(facts, mangle.Fact{
				Predicate: "react_state",
				Args:      []interface{}{sessionID, n.ID, entry[0], fmt.Sprintf("%v", entry[1])},
				Timestamp: now,
			})
		}

		if n.DomNodeID != nil && *n.DomNodeID != "" {
			facts = append(facts, mangle.Fact{
				Predicate: "dom_mapping",
				Args:      []interface{}{sessionID, n.ID, *n.DomNodeID},
				Timestamp: now,
			})
		}
	}

	if err := m.engine.AddFacts(ctx, facts); err != nil {
		return nil, err
	}
	return facts, nil
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

	dest, err := m.CreateTab(ctx, srcMeta.BrowserID, targetURL, true)
	if err != nil {
		return nil, fmt.Errorf("create forked session: %w", err)
	}

	destPage, ok := m.Page(dest.ID)
	if !ok {
		return dest, nil
	}

	// Restore cookies into the new context.
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
	if len(params) > 0 {
		_ = destPage.SetCookies(params)
	}

	// Restore local/session storage (best-effort).
	restoreStorage(destPage, localJSON, sessionJSON)
	dest.Status = "forked"
	m.UpdateMetadata(dest.ID, func(s Session) Session {
		s.Status = "forked"
		return s
	})

	_ = m.persistSessions()
	return dest, nil
}

// startEventStream wires Rod CDP events into the fact sink (console + network + navigation).
