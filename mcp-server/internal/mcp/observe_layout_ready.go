package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-rod/rod"
)

// Readiness for the layout audit: wait until the page has finished loading,
// and say so honestly when it never does, so a clean verdict is never a
// verdict on skeleton cards (docs/specs/responsive-layout-audit.md,
// "Readiness").

const (
	layoutReadyDefaultMs  = 20000
	layoutReadyMaxMs      = 120000
	layoutReadyPoll       = 250 * time.Millisecond
	layoutNetQuiet        = 500 * time.Millisecond
	layoutReadyProbeLimit = 2 * time.Second
	layoutReadySettledRun = 2

	readinessReady        = "ready"
	readinessStillLoading = "still_loading"
	readinessNetworkBusy  = "network_busy"
	readinessUnknown      = "unknown"
)

// readinessRank orders states from best to worst for the top-level verdict.
var readinessRank = map[string]int{readinessReady: 0, readinessUnknown: 1, readinessNetworkBusy: 2, readinessStillLoading: 3}

// layoutReadiness is how settled the page was when a viewport was audited.
type layoutReadiness struct {
	State          string
	Waited         time.Duration
	Timeout        time.Duration
	Placeholders   int
	Samples        []string
	InFlight       int
	NetworkTracked bool
	ProbeErr       string
}

// layoutReadyTimeout reads ready_timeout_ms.
func layoutReadyTimeout(args map[string]interface{}) (time.Duration, error) {
	if _, ok := args["ready_timeout_ms"]; !ok {
		return layoutReadyDefaultMs * time.Millisecond, nil
	}
	n, ok := numberValue(args["ready_timeout_ms"])
	if !ok || n < 0 {
		return 0, fmt.Errorf("ready_timeout_ms must be a number of milliseconds, 0 or more")
	}
	if n > layoutReadyMaxMs {
		return 0, fmt.Errorf("ready_timeout_ms is capped at %d (got %.0f)", layoutReadyMaxMs, n)
	}
	return time.Duration(n) * time.Millisecond, nil
}

// placeholderScript counts the loading placeholders the page renders, from
// generic signals only: aria-busy, indeterminate progress, skeleton and
// shimmer classes, spinners, and "Loading…" live regions or standalone text.
// Only rendered placeholders count, and only the outermost of nested ones.
const placeholderScript = `() => {
	const cands = new Map();
	const add = (el, kind) => { if (el && !cands.has(el)) cands.set(el, kind); };
	const all = (sel, kind, keep) => document.querySelectorAll(sel).forEach((el) => { if (!keep || keep(el)) add(el, kind); });
	all('[aria-busy="true"]', 'aria-busy');
	all('[role="progressbar"]:not([aria-valuenow]), progress:not([value])', 'progress');
	all('.animate-pulse', 'pulse', (el) => { const r = el.getBoundingClientRect(); return r.width > 16 || r.height > 16; });
	all('.animate-spin', 'spinner');
	all('[class*="skeleton" i], [class*="shimmer" i], [data-loading="true"], [data-skeleton]', 'skeleton');
	const loading = /^\s*loading(?:\s*(?:\.{3}|…)|\s+[^.…\n]{1,40}(?:\.{3}|…))?\s*$/i;
	all('[role="status"], [aria-live]:not([aria-live="off"])', 'live-region', (el) => {
		const t = (el.textContent || '').trim();
		return t.length > 0 && t.length <= 60 && /\bloading\b/i.test(t);
	});
	if (document.body) {
		const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
		let node, steps = 0;
		while ((node = walker.nextNode()) && steps++ < 20000) {
			const text = node.nodeValue;
			if (!text || text.length > 80 || !loading.test(text)) continue;
			const el = node.parentElement;
			if (!el || /^(SCRIPT|STYLE|NOSCRIPT|TEMPLATE|TITLE|OPTION)$/.test(el.tagName)) continue;
			if ((el.textContent || '').trim().length > 60) continue;
			add(el, 'loading-text');
		}
	}
	const shown = (el) => {
		if (typeof el.checkVisibility === 'function' && !el.checkVisibility({ opacityProperty: true, visibilityProperty: true })) return false;
		const r = el.getBoundingClientRect();
		return r.width > 1 && r.height > 1;
	};
	const visible = [];
	cands.forEach((kind, el) => { if (shown(el)) visible.push(el); });
	const set = new Set(visible);
	const outer = visible.filter((el) => {
		for (let p = el.parentElement; p; p = p.parentElement) { if (set.has(p)) return false; }
		return true;
	});
	const name = (el) => {
		const tid = el.getAttribute('data-testid');
		if (tid) return '[data-testid="' + tid + '"]';
		if (el.id) return '#' + el.id;
		const label = el.getAttribute('aria-label');
		if (label) return el.tagName.toLowerCase() + '[aria-label="' + label.slice(0, 40) + '"]';
		const cls = (typeof el.className === 'string' ? el.className : '').trim().split(/\s+/).filter(Boolean).slice(0, 2);
		return el.tagName.toLowerCase() + (cls.length ? '.' + cls.join('.') : '');
	};
	return { count: outer.length, samples: outer.slice(0, 5).map((el) => name(el) + ' (' + cands.get(el) + ')') };
}`

type placeholderProbe struct {
	Count   int      `json:"count"`
	Samples []string `json:"samples"`
}

func probePlaceholders(ctx context.Context, page *rod.Page) (placeholderProbe, error) {
	var out placeholderProbe
	res, err := page.Context(ctx).Timeout(layoutReadyProbeLimit).Eval(placeholderScript)
	if err != nil {
		return out, err
	}
	raw, err := json.Marshal(res.Value.Val())
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("placeholder probe: %w", err)
	}
	return out, nil
}

// waitLayoutReady polls the page until it is settled - network quiet and no
// placeholder rendered on two polls in a row - or timeout passes, and
// classifies the last poll. A zero timeout polls once.
func (t *BrowserObserveTool) waitLayoutReady(ctx context.Context, sessionID string, page *rod.Page, timeout time.Duration) layoutReadiness {
	start := time.Now()
	deadline := start.Add(timeout)
	settled := 0
	for {
		probe, probeErr := probePlaceholders(ctx, page)
		net := t.sessions.NetworkState(sessionID)
		now := time.Now()
		r := layoutReadiness{
			Waited: now.Sub(start), Timeout: timeout,
			Placeholders: probe.Count, Samples: probe.Samples,
			InFlight: net.InFlight, NetworkTracked: net.Tracked,
		}
		quiet := !net.Tracked || net.Quiet(now, layoutNetQuiet)
		switch {
		case probeErr != nil:
			r.State = readinessUnknown
			r.ProbeErr = probeErr.Error()
			settled = 0
		case probe.Count > 0:
			r.State = readinessStillLoading
			settled = 0
		case !quiet:
			r.State = readinessNetworkBusy
			settled = 0
		default:
			r.State = readinessReady
			settled++
		}
		if settled >= layoutReadySettledRun || !now.Before(deadline) || ctx.Err() != nil {
			return r
		}
		wait := layoutReadyPoll
		if left := deadline.Sub(now); left < wait {
			wait = left
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

// warning is the line a caller reads when the audit did not see a settled
// page; "" when it did.
func (r layoutReadiness) warning() string {
	after := fmt.Sprintf("%.1fs", r.Waited.Seconds())
	switch r.State {
	case readinessStillLoading:
		return fmt.Sprintf("audited while %d loading placeholder(s) were visible after %s; results cover the loading state, not the page's content", r.Placeholders, after)
	case readinessNetworkBusy:
		return fmt.Sprintf("audited while the network was still busy after %s (%d request(s) in flight); content that arrives later can still move the layout", after, r.InFlight)
	case readinessUnknown:
		return fmt.Sprintf("could not read the page's loading state (%s); results may cover a loading frame", r.ProbeErr)
	}
	return ""
}

func (r layoutReadiness) data() map[string]interface{} {
	out := map[string]interface{}{
		"state":     r.State,
		"waited_ms": r.Waited.Milliseconds(),
		"network":   "tracked",
	}
	if !r.NetworkTracked {
		out["network"] = "untracked"
	}
	if r.Placeholders > 0 {
		out["placeholders"] = r.Placeholders
		out["samples"] = r.Samples
	}
	if r.InFlight > 0 {
		out["in_flight"] = r.InFlight
	}
	return out
}

// worstReadiness is the top-level verdict over every audited viewport.
func worstReadiness(blocks []*layoutBlock) string {
	worst := readinessReady
	for _, b := range blocks {
		if readinessRank[b.readiness.State] > readinessRank[worst] {
			worst = b.readiness.State
		}
	}
	return worst
}

// readinessWarnings is one line per viewport that was not ready.
func readinessWarnings(blocks []*layoutBlock) []string {
	var out []string
	for _, b := range blocks {
		if w := b.readiness.warning(); w != "" {
			if len(blocks) > 1 {
				w = strings.TrimSpace(b.name()) + ": " + w
			}
			out = append(out, w)
		}
	}
	return out
}
