package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"browsernerd-mcp-server/internal/correlation"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/security"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

func (m *SessionManager) startEventStream(ctx context.Context, sessionID string, page *rod.Page) {
	if m.engine == nil {
		return
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[session:%s] event stream panic recovered: %v\n%s", sessionID, r, debug.Stack())
			}
		}()

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

				const inputValue = (target) => {
					const descriptor = [
						target.type || '',
						target.autocomplete || '',
						target.id || '',
						target.name || '',
						target.getAttribute?.('aria-label') || ''
					].join(' ').toLowerCase();
					const sensitive = /password|passwd|secret|token|api.?key|one.?time.?code|cc.?number|cc.?csc|card.?number|cvv|cvc/.test(descriptor);
					return sensitive ? '[REDACTED]' : (target.value || '');
				};

				// Input events capture safe value changes and redact credentials in-page.
				document.addEventListener('input', (ev) => {
					try {
						const target = ev.target || {};
						const id = target.id || target.name || '';
						const value = inputValue(target);
						w.__browsernerdEvents.push({ type: 'input', id, value, ts: Date.now() });
					} catch (e) {}
				}, true);

				// Change events - capture final values on blur/submit
				document.addEventListener('change', (ev) => {
					try {
						const target = ev.target || {};
						const id = target.id || target.name || '';
						const value = inputValue(target);
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
			m.recordNavigationEvent(sessionID, ev.Frame.URL, now)
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
				msg := m.redactor.SanitizeString(stringifyConsoleArgs(ev.Args))
				if ev.Type == proto.RuntimeConsoleAPICalledTypeError {
					m.recordConsoleErrorEvent(sessionID, string(ev.Type), msg, now)
				}
				if err := m.engine.AddFacts(ctx, []mangle.Fact{{
					Predicate: "console_event",
					Args:      []interface{}{sessionID, string(ev.Type), msg, now.UnixMilli()},
					Timestamp: now,
				}}); err != nil {
					log.Printf("[session:%s] console fact error: %v", sessionID, err)
				}
			},
			func(ev *proto.NetworkRequestWillBeSent) {
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

				// Store a best-effort initiator reference on the net_request fact itself.
				initiatorRef := coalesceNonEmpty(initiatorID, initiatorScript)
				if initiatorLineNo > 0 && initiatorScript != "" {
					initiatorRef = fmt.Sprintf("%s:%d", initiatorScript, initiatorLineNo)
				}
				safeURL := m.redactor.SanitizeString(ev.Request.URL)
				m.recordRequestEvent(sessionID, string(ev.RequestID), safeURL, now)

				facts := []mangle.Fact{{
					Predicate: "net_request",
					Args:      []interface{}{sessionID, string(ev.RequestID), ev.Request.Method, safeURL, m.redactor.SanitizeString(initiatorRef), now.UnixMilli()},
					Timestamp: now,
				}}

				// Emit request_initiator for cascading failure analysis
				if initiatorType != "" || initiatorID != "" || initiatorScript != "" {
					facts = append(facts, mangle.Fact{
						Predicate: "request_initiator",
						Args:      []interface{}{sessionID, string(ev.RequestID), initiatorType, initiatorRef},
						Timestamp: now,
					})
				}

				if captureHeaders && ev.Request != nil {
					for k, v := range ev.Request.Headers {
						key := strings.ToLower(k)
						value := m.redactor.RedactHeader(key, fmt.Sprintf("%v", v))
						facts = append(facts, mangle.Fact{
							Predicate: "net_header",
							Args:      []interface{}{sessionID, string(ev.RequestID), "req", key, value},
							Timestamp: now,
						})
						for _, corr := range correlation.FromHeader(key, fmt.Sprintf("%v", v)) {
							facts = append(facts, mangle.Fact{
								Predicate: "net_correlation_key",
								Args:      []interface{}{sessionID, string(ev.RequestID), corr.Type, corr.Value},
								Timestamp: now,
							})
						}
					}
				}

				if ev.Request != nil && strings.TrimSpace(ev.Request.PostData) != "" {
					facts = append(facts, extractRequestPayloadFacts(sessionID, string(ev.RequestID), ev.Request.PostData, ev.Request.Headers, now)...)
				}

				if err := m.engine.AddFacts(ctx, facts); err != nil {
					log.Printf("[session:%s] net_request fact error: %v", sessionID, err)
				}
			},
			func(ev *proto.NetworkResponseReceived) {
				now := time.Now()
				var latency, duration int64
				if ev.Response != nil && ev.Response.Timing != nil {
					// Convert CDP float64 timings (milliseconds) to int64 for Mangle arithmetic
					latency = int64(ev.Response.Timing.ReceiveHeadersEnd)
					duration = int64(ev.Response.Timing.ConnectEnd)
				}
				facts := []mangle.Fact{{
					Predicate: "net_response",
					Args:      []interface{}{sessionID, string(ev.RequestID), ev.Response.Status, latency, duration},
					Timestamp: now,
				}}

				if captureHeaders && ev.Response != nil {
					for k, v := range ev.Response.Headers {
						key := strings.ToLower(k)
						value := m.redactor.RedactHeader(key, fmt.Sprintf("%v", v))
						facts = append(facts, mangle.Fact{
							Predicate: "net_header",
							Args:      []interface{}{sessionID, string(ev.RequestID), "res", key, value},
							Timestamp: now,
						})
						for _, corr := range correlation.FromHeader(key, fmt.Sprintf("%v", v)) {
							facts = append(facts, mangle.Fact{
								Predicate: "net_correlation_key",
								Args:      []interface{}{sessionID, string(ev.RequestID), corr.Type, corr.Value},
								Timestamp: now,
							})
						}
					}
				}

				if err := m.engine.AddFacts(ctx, facts); err != nil {
					log.Printf("[session:%s] net_response fact error: %v", sessionID, err)
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
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[session:%s] waitNav panic recovered: %v\n%s", sessionID, r, debug.Stack())
				}
			}()
			waitNav()
		}()
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[session:%s] waitRest panic recovered: %v\n%s", sessionID, r, debug.Stack())
				}
			}()
			waitRest()
		}()
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[session:%s] event poller panic recovered: %v\n%s", sessionID, r, debug.Stack())
				}
			}()

			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			consecutiveErrors := 0
			nextAttempt := time.Time{}

			for {
				select {
				case <-ctx.Done():
					return
				case tickTime := <-ticker.C:
					if tickTime.Before(nextAttempt) {
						continue
					}
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
						consecutiveErrors++
						if consecutiveErrors >= 20 {
							log.Printf("[session:%s] stopping event poller after %d consecutive evaluation failures", sessionID, consecutiveErrors)
							return
						}
						nextAttempt = tickTime.Add(eventPollBackoff(consecutiveErrors))
						continue
					}
					consecutiveErrors = 0
					nextAttempt = time.Time{}
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
								Args:      []interface{}{sessionID, ev.ID, ts.UnixMilli()},
								Timestamp: ts,
							})
						case "input":
							// input_event(NodeId, Value, Timestamp) per PRD schema
							facts = append(facts, mangle.Fact{
								Predicate: "input_event",
								Args:      []interface{}{sessionID, ev.ID, redactInputValue(m.redactor, ev.ID, ev.Value), ts.UnixMilli()},
								Timestamp: ts,
							})
						case "state":
							facts = append(facts, mangle.Fact{
								Predicate: "state_change",
								Args:      []interface{}{sessionID, ev.Name, ev.Value, ts.UnixMilli()},
								Timestamp: ts,
							})
						case "toast":
							safeText := m.redactor.SanitizeString(ev.Text)
							m.recordToastEvent(sessionID, safeText, ev.Level, ev.Source, ts)
							// toast_notification(Text, Level, Source, Timestamp) for instant error overlay detection
							facts = append(facts, mangle.Fact{
								Predicate: "toast_notification",
								Args:      []interface{}{sessionID, safeText, ev.Level, ev.Source, ts.UnixMilli()},
								Timestamp: ts,
							})
							// Also emit level-specific predicates for easy querying
							if ev.Level == "error" {
								facts = append(facts, mangle.Fact{
									Predicate: "error_toast",
									Args:      []interface{}{sessionID, safeText, ev.Source, ts.UnixMilli()},
									Timestamp: ts,
								})
								log.Printf("[session:%s] ERROR TOAST DETECTED: %s (source: %s)", sessionID, safeText, ev.Source)
							} else if ev.Level == "warning" {
								facts = append(facts, mangle.Fact{
									Predicate: "warning_toast",
									Args:      []interface{}{sessionID, safeText, ev.Source, ts.UnixMilli()},
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

func eventPollBackoff(consecutiveErrors int) time.Duration {
	if consecutiveErrors <= 0 {
		return 0
	}
	backoff := 500 * time.Millisecond
	for i := 1; i < consecutiveErrors && backoff < 5*time.Second; i++ {
		backoff *= 2
	}
	if backoff > 5*time.Second {
		return 5 * time.Second
	}
	return backoff
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
	if m.engine == nil {
		return nil
	}

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
			Args:      []interface{}{sessionID, n.ID, n.Tag, m.redactor.SanitizeString(n.Text), n.Parent},
			Timestamp: now,
		})
		if n.Text != "" {
			facts = append(facts, mangle.Fact{
				Predicate: "dom_text",
				Args:      []interface{}{sessionID, n.ID, m.redactor.SanitizeString(n.Text)},
				Timestamp: now,
			})
		}
		sensitiveInput := sensitiveDOMInput(n.Attrs)
		for k, v := range n.Attrs {
			if sensitiveInput && strings.EqualFold(k, "value") {
				v = security.Redacted
			} else {
				v = m.redactor.RedactHeader(k, v)
			}
			facts = append(facts, mangle.Fact{
				Predicate: "dom_attr",
				Args:      []interface{}{sessionID, n.ID, k, v},
				Timestamp: now,
			})
		}
		// Add layout fact
		facts = append(facts, mangle.Fact{
			Predicate: "dom_layout",
			Args:      []interface{}{sessionID, n.ID, n.Layout.X, n.Layout.Y, n.Layout.Width, n.Layout.Height, fmt.Sprintf("%v", n.Layout.Visible)},
			Timestamp: now,
		})
	}
	facts = append(facts, mangle.Fact{
		Predicate: "dom_updated",
		Args:      []interface{}{sessionID, now.UnixMilli()},
		Timestamp: now,
	})
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

func extractRequestPayloadFacts(sessionID, requestID, postData string, headers proto.NetworkHeaders, ts time.Time) []mangle.Fact {
	postData = strings.TrimSpace(postData)
	if postData == "" {
		return nil
	}

	contentType := ""
	for key, value := range headers {
		if strings.EqualFold(key, "content-type") {
			contentType = strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", value)))
			break
		}
	}

	fields := map[string]string{}
	switch {
	case strings.Contains(contentType, "application/json") || strings.HasPrefix(postData, "{"):
		var payload map[string]interface{}
		if err := json.Unmarshal([]byte(postData), &payload); err == nil {
			for key, value := range payload {
				fields[key] = inferPayloadValueKind(value)
			}
		}
	case strings.Contains(contentType, "application/x-www-form-urlencoded"):
		if values, err := url.ParseQuery(postData); err == nil {
			for key := range values {
				fields[key] = "string"
			}
		}
	}

	if len(fields) == 0 {
		return nil
	}

	facts := make([]mangle.Fact, 0, len(fields))
	for key, kind := range fields {
		facts = append(facts, mangle.Fact{
			Predicate: "request_payload_field",
			Args:      []interface{}{sessionID, requestID, key, kind},
			Timestamp: ts,
		})
	}
	return facts
}

func inferPayloadValueKind(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return "number"
	case string:
		return "string"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	default:
		_ = typed
		return "unknown"
	}
}

// persistSessions writes session metadata to disk for continuity across restarts.
