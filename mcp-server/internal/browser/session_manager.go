package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
	"github.com/go-rod/rod/lib/proto"
	"github.com/google/uuid"
)

// Session describes the public metadata for a tracked browser context.
type Session struct {
	ID         string    `json:"id"`
	TargetID   string    `json:"target_id,omitempty"`
	URL        string    `json:"url,omitempty"`
	Title      string    `json:"title,omitempty"`
	Status     string    `json:"status,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastActive time.Time `json:"last_active"`
}

type sessionRecord struct {
	meta     Session
	page     *rod.Page
	registry *ElementRegistry // Per-session element cache for reliable re-identification
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
type ElementFingerprint struct {
	Ref          string             `json:"ref"`           // Generated reference string
	TagName      string             `json:"tag_name"`      // Lowercase tag name (button, input, etc.)
	ID           string             `json:"id"`            // Element ID attribute (if any)
	Name         string             `json:"name"`          // Name attribute (if any)
	Classes      []string           `json:"classes"`       // CSS class list
	TextContent  string             `json:"text_content"`  // First 100 chars of text content
	AriaLabel    string             `json:"aria_label"`    // aria-label attribute
	DataTestID   string             `json:"data_testid"`   // data-testid attribute
	Role         string             `json:"role"`          // ARIA role attribute
	BoundingBox  map[string]float64 `json:"bounding_box"`  // x, y, width, height
	AltSelectors []string           `json:"alt_selectors"` // Alternative CSS selectors for fallback
	GeneratedAt  time.Time          `json:"generated_at"`  // When the element was discovered
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
	cfg        config.BrowserConfig
	engine     EngineSink
	mu         sync.RWMutex
	browser    *rod.Browser
	sessions   map[string]*sessionRecord
	controlURL string // WebSocket URL for DevTools
}

// EngineSink defines the minimal interface we need from the logic layer.
type EngineSink interface {
	AddFacts(ctx context.Context, facts []mangle.Fact) error
}

func NewSessionManager(cfg config.BrowserConfig, sink EngineSink) *SessionManager {
	return &SessionManager{
		cfg:      cfg,
		engine:   sink,
		sessions: make(map[string]*sessionRecord),
	}
}

// Start connects to an existing Chrome or launches a new one using Rod's launcher.
func (m *SessionManager) Start(ctx context.Context) error {
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
		m.sessions = make(map[string]*sessionRecord)
		m.mu.Unlock()
	}

	if err := m.loadSessions(); err != nil {
		return fmt.Errorf("load sessions: %w", err)
	}

	controlURL := m.cfg.DebuggerURL
	if controlURL == "" && len(m.cfg.Launch) > 0 {
		bin := m.cfg.Launch[0]
		launch := launcher.New().Bin(bin).Headless(m.cfg.IsHeadless())
		if len(m.cfg.Launch) > 1 {
			for _, rawFlag := range m.cfg.Launch[1:] {
				flagStr := strings.TrimLeft(rawFlag, "-")
				name, val, hasVal := strings.Cut(flagStr, "=")
				if hasVal {
					launch = launch.Set(flags.Flag(name), val)
				} else {
					launch = launch.Set(flags.Flag(name))
				}
			}
		}
		url, err := launch.Launch()
		if err != nil {
			// Fallback: let Rod pick the port and defaults.
			fallback := launcher.New().Bin(bin).Headless(m.cfg.IsHeadless())
			if alt, altErr := fallback.Launch(); altErr == nil {
				controlURL = alt
			} else {
				return fmt.Errorf("launch chrome: %w (fallback: %v)", err, altErr)
			}
		} else {
			controlURL = url
		}
	}

	if controlURL == "" {
		return errors.New("no debugger_url or launch command provided")
	}

	browser := rod.New().ControlURL(controlURL).Context(ctx)
	if err := browser.Connect(); err != nil {
		return fmt.Errorf("connect to chrome: %w", err)
	}

	m.browser = browser
	m.controlURL = controlURL
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
func (m *SessionManager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, record := range m.sessions {
		if record.page != nil {
			_ = record.page.Close()
		}
		delete(m.sessions, id)
	}

	var err error
	if m.browser != nil {
		err = m.browser.Close()
		m.browser = nil
	}
	m.controlURL = ""
	log.Printf("Browser shutdown complete")
	return err
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

// CreateSession opens a new page (incognito context by default) and tracks it.
func (m *SessionManager) CreateSession(ctx context.Context, url string) (*Session, error) {
	if m.browser == nil {
		return nil, errors.New("browser not connected")
	}

	incognito, err := m.browser.Incognito()
	if err != nil {
		return nil, fmt.Errorf("incognito context: %w", err)
	}

	page, err := incognito.Page(proto.TargetCreateTarget{URL: url})
	if err != nil {
		return nil, fmt.Errorf("create page: %w", err)
	}

	// Set viewport dimensions from config (default 1920x1080)
	if err := (proto.EmulationSetDeviceMetricsOverride{
		Width:             m.cfg.GetViewportWidth(),
		Height:            m.cfg.GetViewportHeight(),
		DeviceScaleFactor: 1.0,
		Mobile:            false,
	}).Call(page); err != nil {
		log.Printf("warning: failed to set viewport: %v", err)
	}

	// Best-effort load; failures are not fatal for scaffolding.
	_ = page.Timeout(m.cfg.NavigationTimeout()).Navigate(url)

	meta := Session{
		ID:         uuid.NewString(),
		TargetID:   string(page.TargetID),
		URL:        url,
		Status:     "active",
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
	}

	m.mu.Lock()
	m.sessions[meta.ID] = &sessionRecord{meta: meta, page: page, registry: NewElementRegistry()}
	m.mu.Unlock()

	m.startEventStream(ctx, meta.ID, page)
	_ = m.persistSessions()

	return &meta, nil
}

// Attach attempts to bind to an existing target by TargetID.
func (m *SessionManager) Attach(ctx context.Context, targetID string) (*Session, error) {
	if m.browser == nil {
		return nil, errors.New("browser not connected")
	}

	page, err := m.browser.PageFromTarget(proto.TargetTargetID(targetID))
	if err != nil {
		return nil, fmt.Errorf("attach to target %s: %w", targetID, err)
	}

	meta := Session{
		ID:         uuid.NewString(),
		TargetID:   targetID,
		Status:     "attached",
		CreatedAt:  time.Now(),
		LastActive: time.Now(),
	}

	m.mu.Lock()
	m.sessions[meta.ID] = &sessionRecord{meta: meta, page: page, registry: NewElementRegistry()}
	m.mu.Unlock()

	m.startEventStream(ctx, meta.ID, page)
	_ = m.persistSessions()
	return &meta, nil
}

// Page returns the underlying Rod page for a session when present.
func (m *SessionManager) Page(sessionID string) (*rod.Page, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.sessions[sessionID]
	if !ok {
		return nil, false
	}
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
			Args:      []interface{}{n.ID, n.Name, parent},
			Timestamp: now,
		})

		for k, v := range n.Props {
			facts = append(facts, mangle.Fact{
				Predicate: "react_prop",
				Args:      []interface{}{n.ID, k, fmt.Sprintf("%v", v)},
				Timestamp: now,
			})
		}

		for _, entry := range n.State {
			if len(entry) != 2 {
				continue
			}
			facts = append(facts, mangle.Fact{
				Predicate: "react_state",
				Args:      []interface{}{n.ID, entry[0], fmt.Sprintf("%v", entry[1])},
				Timestamp: now,
			})
		}

		if n.DomNodeID != nil && *n.DomNodeID != "" {
			facts = append(facts, mangle.Fact{
				Predicate: "dom_mapping",
				Args:      []interface{}{n.ID, *n.DomNodeID},
				Timestamp: now,
			})
		}
	}

	if err := m.engine.AddFacts(ctx, facts); err != nil {
		return nil, err
	}
	return facts, nil
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

	dest, err := m.CreateSession(ctx, targetURL)
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
	m.UpdateMetadata(dest.ID, func(s Session) Session {
		s.Status = "forked"
		return s
	})

	_ = m.persistSessions()
	return dest, nil
}

// startEventStream wires Rod CDP events into the fact sink (console + network + navigation).
func (m *SessionManager) startEventStream(ctx context.Context, sessionID string, page *rod.Page) {
	if m.engine == nil {
		return
	}

	go func() {
		var wg sync.WaitGroup

		level := strings.ToLower(m.cfg.EventLoggingLevel)
		captureDOM := m.cfg.EnableDOMIngestion && level != "minimal"
		captureHeaders := m.cfg.EnableHeaderIngestion && level != "minimal"
		consoleErrorsOnly := level == "minimal"
		throttler := newEventThrottler(m.cfg.EventThrottleMs)

		// Optionally capture initial DOM snapshot.
		if captureDOM {
			_ = proto.DOMEnable{}.Call(page)
			_ = m.captureDOMFacts(ctx, sessionID, page)
		}

		// Install lightweight click/input/state trackers in the page context.
		_, _ = page.Context(ctx).Evaluate(&rod.EvalOptions{
			JS: `
			() => {
				const w = window;
				if (w.__browsernerdHooked) return true;
				w.__browsernerdHooked = true;
				w.__browsernerdEvents = [];

				// Click events (PRD Vector 2: Flight Recorder)
				document.addEventListener('click', (ev) => {
					try {
						const target = ev.target || {};
						const id = target.id || '';
						w.__browsernerdEvents.push({ type: 'click', id, ts: Date.now() });
					} catch (e) {}
				}, true);

				// Input events - capture value changes on form fields
				document.addEventListener('input', (ev) => {
					try {
						const target = ev.target || {};
						const id = target.id || target.name || '';
						const value = target.value || '';
						w.__browsernerdEvents.push({ type: 'input', id, value, ts: Date.now() });
					} catch (e) {}
				}, true);

				// Change events - capture final values on blur/submit
				document.addEventListener('change', (ev) => {
					try {
						const target = ev.target || {};
						const id = target.id || target.name || '';
						const value = target.value || '';
						w.__browsernerdEvents.push({ type: 'input', id, value, ts: Date.now() });
					} catch (e) {}
				}, true);

				// State change observation via data-* attributes
				const obs = new MutationObserver((mutations) => {
					mutations.forEach((m) => {
						if (m.type === 'attributes' && m.attributeName && m.attributeName.startsWith('data-state')) {
							const val = (m.target && m.target.getAttribute) ? (m.target.getAttribute(m.attributeName) || '') : '';
							w.__browsernerdEvents.push({ type: 'state', name: m.attributeName, value: val, ts: Date.now() });
						}
					});
				});
				obs.observe(document.documentElement || document.body, { attributes: true, subtree: true });

				// Toast/Notification detection via MutationObserver
				// Watches for dynamically added toast overlays, snackbars, alerts, notifications
				const toastPatterns = /toast|notification|alert|snackbar|banner|message|notice|popup|notistack/i;
				const errorPatterns = /error|danger|fail|critical|destructive/i;
				const warningPatterns = /warning|warn|caution/i;
				const successPatterns = /success|done|complete|confirmed/i;
				const infoPatterns = /info|information|note/i;

				const detectToastLevel = (el) => {
					const classes = (el.className || '').toLowerCase();
					const role = (el.getAttribute('role') || '').toLowerCase();
					const ariaLive = (el.getAttribute('aria-live') || '').toLowerCase();
					const dataType = (el.getAttribute('data-type') || el.getAttribute('data-status') || el.getAttribute('data-variant') || '').toLowerCase();
					const combined = classes + ' ' + role + ' ' + dataType;

					if (errorPatterns.test(combined)) return 'error';
					if (warningPatterns.test(combined)) return 'warning';
					if (successPatterns.test(combined)) return 'success';
					if (infoPatterns.test(combined)) return 'info';
					// Default based on aria-live urgency
					if (ariaLive === 'assertive') return 'error';
					if (ariaLive === 'polite') return 'info';
					return 'info';
				};

				const isToastElement = (el) => {
					if (!el || el.nodeType !== 1) return false;
					const classes = (el.className || '').toLowerCase();
					const role = (el.getAttribute && el.getAttribute('role')) || '';
					const ariaLive = (el.getAttribute && el.getAttribute('aria-live')) || '';
					const id = (el.id || '').toLowerCase();
					const dataTestId = (el.getAttribute && el.getAttribute('data-testid')) || '';

					// Check common toast patterns
					if (toastPatterns.test(classes)) return true;
					if (toastPatterns.test(id)) return true;
					if (toastPatterns.test(dataTestId)) return true;
					if (role === 'alert' || role === 'alertdialog' || role === 'status') return true;
					if (ariaLive === 'polite' || ariaLive === 'assertive') return true;

					// Check for common UI library patterns
					// Material-UI / MUI
					if (classes.includes('muisnackbar') || classes.includes('muialert')) return true;
					// Chakra UI
					if (classes.includes('chakra-alert') || classes.includes('chakra-toast')) return true;
					// Ant Design
					if (classes.includes('ant-notification') || classes.includes('ant-message') || classes.includes('ant-alert')) return true;
					// shadcn/ui / Radix
					if (classes.includes('sonner') || classes.includes('toaster')) return true;
					// react-toastify
					if (classes.includes('toastify')) return true;
					// react-hot-toast
					if (classes.includes('react-hot-toast')) return true;
					// notistack
					if (classes.includes('notistack')) return true;

					return false;
				};

				const extractToastSource = (el) => {
					const classes = (el.className || '').toLowerCase();
					if (classes.includes('mui') || classes.includes('material')) return 'material-ui';
					if (classes.includes('chakra')) return 'chakra-ui';
					if (classes.includes('ant-')) return 'ant-design';
					if (classes.includes('sonner') || classes.includes('toaster')) return 'shadcn';
					if (classes.includes('toastify')) return 'react-toastify';
					if (classes.includes('react-hot-toast')) return 'react-hot-toast';
					if (classes.includes('notistack')) return 'notistack';
					return 'native';
				};

				const seenToasts = new Set();

				const toastObs = new MutationObserver((mutations) => {
					mutations.forEach((m) => {
						if (m.type !== 'childList' || !m.addedNodes.length) return;
						m.addedNodes.forEach((node) => {
							// Check the node itself and its descendants
							const checkNode = (el) => {
								if (!el || el.nodeType !== 1) return;
								if (!isToastElement(el)) return;

								// Get visible text content
								const text = (el.textContent || el.innerText || '').trim().substring(0, 500);
								if (!text) return;

								// Deduplicate by text content (toasts often re-render)
								const toastKey = text.substring(0, 100);
								if (seenToasts.has(toastKey)) return;
								seenToasts.add(toastKey);

								// Clean up old entries after 5 seconds
								setTimeout(() => seenToasts.delete(toastKey), 5000);

								const level = detectToastLevel(el);
								const source = extractToastSource(el);
								const id = el.id || el.getAttribute('data-testid') || '';

								w.__browsernerdEvents.push({
									type: 'toast',
									text: text,
									level: level,
									source: source,
									id: id,
									classes: (el.className || '').substring(0, 200),
									ts: Date.now()
								});
							};

							checkNode(node);
							// Also check descendants (toast containers often wrap the actual alert)
							if (node.querySelectorAll) {
								node.querySelectorAll('[role="alert"], [role="status"], [aria-live]').forEach(checkNode);
							}
						});
					});
				});
				toastObs.observe(document.body || document.documentElement, { childList: true, subtree: true });
				return true;
			}
			`,
			ByValue:      true,
			AwaitPromise: true,
		})

		// Navigation - emit both navigation_event (timestamped) and current_url (stateful)
		waitNav := page.Context(ctx).EachEvent(func(ev *proto.PageFrameNavigated) {
			now := time.Now()

			// Clear element registry on navigation - refs become invalid when page changes
			if registry := m.Registry(sessionID); registry != nil {
				prevCount := registry.Count()
				registry.Clear()
				if prevCount > 0 {
					log.Printf("[session:%s] navigation cleared %d cached elements (new URL: %s)", sessionID, prevCount, ev.Frame.URL)
				}
			}

			facts := []mangle.Fact{
				{
					Predicate: "navigation_event",
					Args:      []interface{}{sessionID, ev.Frame.URL, now.UnixMilli()},
					Timestamp: now,
				},
				{
					// current_url is the stateful predicate for test assertions
					// It represents "where the session IS" not "where it navigated"
					Predicate: "current_url",
					Args:      []interface{}{sessionID, ev.Frame.URL},
					Timestamp: now,
				},
			}
			if err := m.engine.AddFacts(ctx, facts); err != nil {
				log.Printf("[session:%s] navigation fact error: %v", sessionID, err)
			}
			m.UpdateMetadata(sessionID, func(s Session) Session {
				s.URL = ev.Frame.URL
				s.LastActive = now
				return s
			})
		})

		// Console, network, and DOM streams
		waitRest := page.Context(ctx).EachEvent(
			func(ev *proto.RuntimeConsoleAPICalled) {
				if consoleErrorsOnly && ev.Type != proto.RuntimeConsoleAPICalledTypeError && ev.Type != proto.RuntimeConsoleAPICalledTypeWarning {
					return
				}
				if !throttler.Allow("console") {
					return
				}
				now := time.Now()
				msg := stringifyConsoleArgs(ev.Args)
				if err := m.engine.AddFacts(ctx, []mangle.Fact{{
					Predicate: "console_event",
					Args:      []interface{}{string(ev.Type), msg, now.UnixMilli()},
					Timestamp: now,
				}}); err != nil {
					log.Printf("[session:%s] console fact error: %v", sessionID, err)
				}
			},
			func(ev *proto.NetworkRequestWillBeSent) {
				if !throttler.Allow("net_request") {
					return
				}
				now := time.Now()
				initiatorType := ""
				initiatorID := ""
				initiatorScript := ""
				initiatorLineNo := 0

				// Enhanced initiator extraction for cascading failure detection (PRD Section 3.4)
				if ev.Initiator != nil {
					initiatorType = string(ev.Initiator.Type)

					// Priority 1: Direct request chain (fetch triggered by another request)
					if ev.Initiator.RequestID != "" {
						initiatorID = string(ev.Initiator.RequestID)
					}

					// Priority 2: URL-based initiator (redirect or prefetch)
					if initiatorID == "" && ev.Initiator.URL != "" {
						initiatorID = ev.Initiator.URL
					}

					// Priority 3: Script-based initiator with full call stack
					if ev.Initiator.Stack != nil && len(ev.Initiator.Stack.CallFrames) > 0 {
						frame := ev.Initiator.Stack.CallFrames[0]
						initiatorScript = frame.URL
						if initiatorScript == "" {
							initiatorScript = string(frame.ScriptID)
						}
						initiatorLineNo = frame.LineNumber

						// Walk up the call stack to find the most specific script
						for _, f := range ev.Initiator.Stack.CallFrames {
							if f.URL != "" && !isInternalScript(f.URL) {
								initiatorScript = f.URL
								initiatorLineNo = f.LineNumber
								break
							}
						}
					}
				}

				facts := []mangle.Fact{{
					Predicate: "net_request",
					Args:      []interface{}{string(ev.RequestID), ev.Request.Method, ev.Request.URL, initiatorType, now.UnixMilli()},
					Timestamp: now,
				}}

				// Emit request_initiator for cascading failure analysis
				if initiatorType != "" || initiatorID != "" || initiatorScript != "" {
					// Use parent RequestID if available, otherwise use script location
					parentRef := coalesceNonEmpty(initiatorID, initiatorScript)
					if initiatorLineNo > 0 && initiatorScript != "" {
						parentRef = fmt.Sprintf("%s:%d", initiatorScript, initiatorLineNo)
					}
					facts = append(facts, mangle.Fact{
						Predicate: "request_initiator",
						Args:      []interface{}{string(ev.RequestID), initiatorType, parentRef},
						Timestamp: now,
					})
				}

				if err := m.engine.AddFacts(ctx, facts); err != nil {
					log.Printf("[session:%s] net_request fact error: %v", sessionID, err)
				}

				if captureHeaders && ev.Request != nil {
					for k, v := range ev.Request.Headers {
						if err := m.engine.AddFacts(ctx, []mangle.Fact{{
							Predicate: "net_header",
							Args:      []interface{}{string(ev.RequestID), "req", strings.ToLower(k), fmt.Sprintf("%v", v)},
							Timestamp: now,
						}}); err != nil {
							log.Printf("[session:%s] net_header fact error: %v", sessionID, err)
						}
					}
				}
			},
			func(ev *proto.NetworkResponseReceived) {
				if !throttler.Allow("net_response") {
					return
				}
				now := time.Now()
				var latency, duration int64
				if ev.Response != nil && ev.Response.Timing != nil {
					// Convert CDP float64 timings (milliseconds) to int64 for Mangle arithmetic
					latency = int64(ev.Response.Timing.ReceiveHeadersEnd)
					duration = int64(ev.Response.Timing.ConnectEnd)
				}
				if err := m.engine.AddFacts(ctx, []mangle.Fact{{
					Predicate: "net_response",
					Args:      []interface{}{string(ev.RequestID), ev.Response.Status, latency, duration},
					Timestamp: now,
				}}); err != nil {
					log.Printf("[session:%s] net_response fact error: %v", sessionID, err)
				}

				if captureHeaders && ev.Response != nil {
					for k, v := range ev.Response.Headers {
						if err := m.engine.AddFacts(ctx, []mangle.Fact{{
							Predicate: "net_header",
							Args:      []interface{}{string(ev.RequestID), "res", strings.ToLower(k), fmt.Sprintf("%v", v)},
							Timestamp: now,
						}}); err != nil {
							log.Printf("[session:%s] res net_header fact error: %v", sessionID, err)
						}
					}
				}
			},
			func(ev *proto.DOMDocumentUpdated) {
				// Mark cached elements as potentially stale when DOM changes
				if registry := m.Registry(sessionID); registry != nil {
					registry.IncrementGeneration()
				}

				if !captureDOM {
					return
				}
				if !throttler.Allow("dom_update") {
					return
				}
				if err := m.captureDOMFacts(ctx, sessionID, page); err != nil {
					log.Printf("[session:%s] DOM capture error: %v", sessionID, err)
				}
			},
		)

		wg.Add(3)
		go func() {
			defer wg.Done()
			waitNav()
		}()
		go func() {
			defer wg.Done()
			waitRest()
		}()
		go func() {
			defer wg.Done()
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					res, err := page.Context(ctx).Evaluate(&rod.EvalOptions{
						JS: `
						() => {
							const buf = Array.isArray(window.__browsernerdEvents) ? window.__browsernerdEvents : [];
							window.__browsernerdEvents = [];
							return buf;
						}
						`,
						ByValue:      true,
						AwaitPromise: true,
					})
					if err != nil || res == nil {
						continue
					}
					if res.Value.Nil() {
						continue
					}
					raw, err := res.Value.MarshalJSON()
					if err != nil {
						continue
					}
					var events []struct {
						Type    string  `json:"type"`
						ID      string  `json:"id"`
						Name    string  `json:"name"`
						Value   string  `json:"value"`
						Text    string  `json:"text"`    // Toast: notification text
						Level   string  `json:"level"`   // Toast: error, warning, success, info
						Source  string  `json:"source"`  // Toast: UI library (material-ui, chakra-ui, etc.)
						Classes string  `json:"classes"` // Toast: CSS classes for debugging
						TS      float64 `json:"ts"`
					}
					if err := json.Unmarshal(raw, &events); err != nil {
						continue
					}

					facts := make([]mangle.Fact, 0, len(events))
					for _, ev := range events {
						ts := time.UnixMilli(int64(ev.TS))
						switch ev.Type {
						case "click":
							facts = append(facts, mangle.Fact{
								Predicate: "click_event",
								Args:      []interface{}{ev.ID, ts.UnixMilli()},
								Timestamp: ts,
							})
						case "input":
							// input_event(NodeId, Value, Timestamp) per PRD schema
							facts = append(facts, mangle.Fact{
								Predicate: "input_event",
								Args:      []interface{}{ev.ID, ev.Value, ts.UnixMilli()},
								Timestamp: ts,
							})
						case "state":
							facts = append(facts, mangle.Fact{
								Predicate: "state_change",
								Args:      []interface{}{ev.Name, ev.Value, ts.UnixMilli()},
								Timestamp: ts,
							})
						case "toast":
							// toast_notification(Text, Level, Source, Timestamp) for instant error overlay detection
							facts = append(facts, mangle.Fact{
								Predicate: "toast_notification",
								Args:      []interface{}{ev.Text, ev.Level, ev.Source, ts.UnixMilli()},
								Timestamp: ts,
							})
							// Also emit level-specific predicates for easy querying
							if ev.Level == "error" {
								facts = append(facts, mangle.Fact{
									Predicate: "error_toast",
									Args:      []interface{}{ev.Text, ev.Source, ts.UnixMilli()},
									Timestamp: ts,
								})
								log.Printf("[session:%s] ERROR TOAST DETECTED: %s (source: %s)", sessionID, ev.Text, ev.Source)
							} else if ev.Level == "warning" {
								facts = append(facts, mangle.Fact{
									Predicate: "warning_toast",
									Args:      []interface{}{ev.Text, ev.Source, ts.UnixMilli()},
									Timestamp: ts,
								})
							}
						}
					}
					if len(facts) > 0 {
						if err := m.engine.AddFacts(ctx, facts); err != nil {
							log.Printf("[session:%s] click/state/toast fact error: %v", sessionID, err)
						}
					}
				}
			}
		}()
		wg.Wait()
	}()
}

func stringifyConsoleArgs(args []*proto.RuntimeRemoteObject) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if a == nil {
			continue
		}
		if !a.Value.Nil() {
			parts = append(parts, a.Value.String())
			continue
		}
		if a.Description != "" {
			parts = append(parts, a.Description)
		}
	}
	return strings.Join(parts, " ")
}

// captureDOMFacts snapshots a limited DOM view into facts to keep context light.
func (m *SessionManager) captureDOMFacts(ctx context.Context, sessionID string, page *rod.Page) error {
	const maxNodes = 200
	script := fmt.Sprintf(`
	() => {
		const nodes = Array.from(document.querySelectorAll('*')).slice(0, %d);
		return nodes.map((el, idx) => {
			const attrs = {};
			for (const { name, value } of Array.from(el.attributes || [])) {
				attrs[name] = value;
			}
			const rect = el.getBoundingClientRect();
			const style = window.getComputedStyle(el);
			const isVisible = style.display !== 'none' && style.visibility !== 'hidden' && style.opacity !== '0' && rect.width > 0 && rect.height > 0;
			
			return {
				id: el.id || ('node_' + idx),
				tag: el.tagName,
				text: (el.innerText || '').slice(0, 256),
				parent: el.parentElement && (el.parentElement.id || el.parentElement.tagName || 'root'),
				attrs,
				layout: {
					x: rect.x,
					y: rect.y,
					width: rect.width,
					height: rect.height,
					visible: isVisible
				}
			};
		});
	}
	`, maxNodes)

	res, err := page.Context(ctx).Evaluate(&rod.EvalOptions{
		JS:           script,
		ByValue:      true,
		AwaitPromise: true,
	})
	if err != nil || res == nil {
		return err
	}

	raw, err := res.Value.MarshalJSON()
	if err != nil {
		return err
	}

	var nodes []struct {
		ID     string            `json:"id"`
		Tag    string            `json:"tag"`
		Text   string            `json:"text"`
		Parent string            `json:"parent"`
		Attrs  map[string]string `json:"attrs"`
		Layout struct {
			X       float64 `json:"x"`
			Y       float64 `json:"y"`
			Width   float64 `json:"width"`
			Height  float64 `json:"height"`
			Visible bool    `json:"visible"`
		} `json:"layout"`
	}
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return err
	}

	now := time.Now()
	facts := make([]mangle.Fact, 0, len(nodes)*3)
	for _, n := range nodes {
		facts = append(facts, mangle.Fact{
			Predicate: "dom_node",
			Args:      []interface{}{n.ID, n.Tag, n.Text, n.Parent},
			Timestamp: now,
		})
		if n.Text != "" {
			facts = append(facts, mangle.Fact{
				Predicate: "dom_text",
				Args:      []interface{}{n.ID, n.Text},
				Timestamp: now,
			})
		}
		for k, v := range n.Attrs {
			facts = append(facts, mangle.Fact{
				Predicate: "dom_attr",
				Args:      []interface{}{n.ID, k, v},
				Timestamp: now,
			})
		}
		// Add layout fact
		facts = append(facts, mangle.Fact{
			Predicate: "dom_layout",
			Args:      []interface{}{n.ID, n.Layout.X, n.Layout.Y, n.Layout.Width, n.Layout.Height, fmt.Sprintf("%v", n.Layout.Visible)},
			Timestamp: now,
		})
	}
	return m.engine.AddFacts(ctx, facts)
}

// SnapshotDOM triggers a one-off DOM capture for the given session.
func (m *SessionManager) SnapshotDOM(ctx context.Context, sessionID string) error {
	page, ok := m.Page(sessionID)
	if !ok {
		return fmt.Errorf("unknown session: %s", sessionID)
	}
	return m.captureDOMFacts(ctx, sessionID, page)
}

func snapshotStorage(page *rod.Page, store string) string {
	jsFunc := fmt.Sprintf(`() => {
		try {
			const out = {};
			for (const key of Object.keys(%s)) {
				out[key] = %s.getItem(key);
			}
			return JSON.stringify(out);
		} catch (e) {
			return "{}";
		}
	}`, store, store)

	res, err := page.Evaluate(&rod.EvalOptions{
		JS:           jsFunc,
		ByValue:      true,
		AwaitPromise: true,
	})
	if err != nil || res == nil || res.Value.Nil() {
		return "{}"
	}
	return res.Value.String()
}

func restoreStorage(page *rod.Page, localJSON, sessionJSON string) {
	_, _ = page.Evaluate(&rod.EvalOptions{
		JS: `
		(local, session) => {
			try {
				const l = JSON.parse(local || "{}");
				Object.entries(l).forEach(([k, v]) => localStorage.setItem(k, v));
			} catch (e) {}
			try {
				const s = JSON.parse(session || "{}");
				Object.entries(s).forEach(([k, v]) => sessionStorage.setItem(k, v));
			} catch (e) {}
		}
		`,
		JSArgs:       []interface{}{localJSON, sessionJSON},
		ByValue:      true,
		AwaitPromise: true,
		UserGesture:  true,
	})
}

// persistSessions writes session metadata to disk for continuity across restarts.
func (m *SessionManager) persistSessions() error {
	if m.cfg.SessionStore == "" {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]Session, 0, len(m.sessions))
	for _, rec := range m.sessions {
		sessions = append(sessions, rec.meta)
	}

	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(m.cfg.SessionStore), 0o755); err != nil {
		return err
	}
	return os.WriteFile(m.cfg.SessionStore, data, 0o644)
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
		m.sessions[s.ID] = &sessionRecord{meta: s, page: nil}
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
