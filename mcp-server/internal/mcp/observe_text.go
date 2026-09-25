package mcp

import (
	"context"
	"fmt"
	"strings"

	"browsernerd-mcp-server/internal/browser"
)

const (
	defaultTextMaxChars = 1500
	maxTextMaxChars     = 20000
	contentDigestChars  = 300
)

// pageTextScript reads what a page says, cheaply and in reading order: the title, the
// h1-h3 outline, the landmark regions, the live regions (alerts, status lines, toasts),
// and the visible text blocks. It takes {query, maxChars, digest} as data.
//
// Blocks are deduplicated, and blocks that differ only in their digits ("Row 1: ...",
// "Row 2: ...") collapse into the first with a count, unless a query is given: a query
// is matched against every block, and a long matching block returns only the window
// around the match. Headings, live regions and blocks are spent from one character
// budget in that order; digest skips blocks entirely.
const pageTextScript = `(opts) => {
	const query = String(opts.query || '').toLowerCase();
	const maxChars = opts.maxChars;
	const digest = !!opts.digest;
	const collapse = (t) => String(t ?? '').replace(/\s+/g, ' ').trim();
	const clip = (t, n) => t.length > n ? t.substring(0, n - 3) + '...' : t;
	const visible = (el) => {
		if (!el || !el.getBoundingClientRect) return false;
		const rect = el.getBoundingClientRect();
		if (rect.width === 0 && rect.height === 0) return false;
		const style = getComputedStyle(el);
		return style.display !== 'none' && style.visibility !== 'hidden';
	};
	const matches = (t) => !query || t.toLowerCase().includes(query);
	const around = (t) => {
		if (!query || t.length <= 240) return clip(t, 400);
		const at = t.toLowerCase().indexOf(query);
		if (at < 0) return clip(t, 240);
		const start = Math.max(0, at - 100);
		const end = Math.min(t.length, at + query.length + 100);
		return (start > 0 ? '...' : '') + t.substring(start, end) + (end < t.length ? '...' : '');
	};

	let used = 0;
	let truncated = false;
	const spend = (t) => {
		if (used + t.length > maxChars) {
			truncated = true;
			return false;
		}
		used += t.length;
		return true;
	};

	const out = { title: document.title || '', url: location.href };

	const headings = [];
	const seenHeadings = new Set();
	document.querySelectorAll('h1, h2, h3, [role="heading"]').forEach((h) => {
		let level = Number((h.tagName.match(/^H(\d)$/) || [])[1] || h.getAttribute('aria-level') || 2);
		if (level > 3) return;
		if (!visible(h)) return;
		const text = clip(collapse(h.innerText), 100);
		if (!text || seenHeadings.has(text) || !matches(text)) return;
		seenHeadings.add(text);
		headings.push('#'.repeat(level) + ' ' + text);
	});

	const landmarkOf = (el) => {
		const role = (el.getAttribute('role') || '').toLowerCase();
		const tag = el.tagName.toLowerCase();
		const named = el.getAttribute('aria-label') || '';
		const sectioned = el.parentElement && el.parentElement.closest('article, aside, main, nav, section, [role="main"], [role="region"]');
		if (role) return role === 'region' && !named ? '' : role;
		if (tag === 'header') return sectioned ? '' : 'banner';
		if (tag === 'footer') return sectioned ? '' : 'contentinfo';
		if (tag === 'nav') return 'navigation';
		if (tag === 'main') return 'main';
		if (tag === 'aside') return 'complementary';
		if ((tag === 'section' || tag === 'form') && named) return tag === 'form' ? 'form' : 'region';
		return '';
	};
	const landmarks = [];
	document.querySelectorAll('header, nav, main, aside, footer, section[aria-label], form[aria-label], [role="banner"], [role="navigation"], [role="main"], [role="complementary"], [role="contentinfo"], [role="search"], [role="region"], [role="form"]').forEach((el) => {
		const name = landmarkOf(el);
		if (!name || !visible(el)) return;
		const label = collapse(el.getAttribute('aria-label'));
		const entry = label ? name + ': ' + clip(label, 40) : name;
		if (!landmarks.includes(entry)) landmarks.push(entry);
	});

	const live = [];
	const liveEls = [];
	document.querySelectorAll('[role="alert"], [role="alertdialog"], [role="status"], [role="log"], [aria-live]:not([aria-live="off"]), [class*="toast" i], [class*="snackbar" i], [data-sonner-toast]').forEach((el) => {
		if (liveEls.some((outer) => outer.contains(el))) return;
		if (!visible(el)) return;
		const text = clip(collapse(el.innerText), 200);
		if (!text || !matches(text)) return;
		liveEls.push(el);
		const role = (el.getAttribute('role') || (el.getAttribute('aria-live') === 'assertive' ? 'alert' : 'status')).toLowerCase();
		const entry = role + ': ' + text;
		if (!live.includes(entry)) live.push(entry);
	});

	out.headings = [];
	out.live = [];
	for (const entry of live) {
		if (!spend(entry)) break;
		out.live.push(entry);
	}
	for (const entry of headings) {
		if (!spend(entry)) break;
		out.headings.push(entry);
	}
	if (out.live.length === 0) delete out.live;
	if (out.headings.length === 0) delete out.headings;
	if (landmarks.length > 0) out.landmarks = landmarks.slice(0, 12);
	if (digest) {
		out.total_headings = headings.length;
		out.truncated = truncated;
		return out;
	}

	// Blocks: the outermost visible text containers outside navigation, controls and
	// live regions (already reported above).
	const blockSelector = 'p, li, td, th, dd, dt, blockquote, pre, figcaption, caption, h4, h5, h6, [role="cell"], [role="gridcell"], div, span, section, article';
	const skip = 'nav, [role="navigation"], button, select, option, textarea, script, style, noscript, template, [aria-hidden="true"], h1, h2, h3, [role="heading"], ' +
		'[role="button"], [role="combobox"], [role="listbox"], [role="option"], [role="tab"], [role="menuitem"], [role="checkbox"], [role="switch"], [role="radio"]';
	const blockTags = new Set(['p', 'li', 'td', 'th', 'dd', 'dt', 'blockquote', 'pre', 'figcaption', 'caption', 'h4', 'h5', 'h6']);
	const hasDirectText = (el) => Array.from(el.childNodes).some((n) => n.nodeType === 3 && n.textContent.trim() !== '');
	const hasBlockChild = (el) => Array.from(el.children).some((c) => {
		const d = getComputedStyle(c).display;
		return d === 'block' || d === 'flex' || d === 'grid' || d === 'list-item' || d === 'table';
	});
	const texts = [];
	const taken = [];
	document.querySelectorAll(blockSelector).forEach((el) => {
		const tag = el.tagName.toLowerCase();
		const role = (el.getAttribute('role') || '').toLowerCase();
		if (!blockTags.has(tag) && role !== 'cell' && role !== 'gridcell') {
			// A generic container counts only when it holds text directly and no blocks.
			if (!hasDirectText(el) || hasBlockChild(el)) return;
		}
		if (el.closest(skip)) return;
		if (liveEls.some((l) => l.contains(el))) return;
		if (taken.length > 0 && taken[taken.length - 1].contains(el)) return;
		if (!visible(el)) return;
		const text = collapse(el.innerText);
		if (text.length < 2) return;
		taken.push(el);
		texts.push(text);
	});

	const blocks = [];
	const seen = new Set();
	const similar = new Map();
	for (const text of texts) {
		if (seen.has(text)) continue;
		seen.add(text);
		if (query) {
			if (matches(text)) blocks.push({ text: around(text), n: 0 });
			continue;
		}
		const shape = text.replace(/\d+/g, '#');
		const first = similar.get(shape);
		if (first) {
			first.n++;
			continue;
		}
		const entry = { text: clip(text, 400), n: 0 };
		similar.set(shape, entry);
		blocks.push(entry);
	}
	out.blocks = [];
	for (const block of blocks) {
		const text = block.n > 0 ? block.text + ' [+' + block.n + ' similar]' : block.text;
		if (!spend(text)) break;
		out.blocks.push(text);
	}
	if (out.blocks.length === 0) delete out.blocks;
	out.total_blocks = texts.length;
	if (query) out.matched_blocks = blocks.length;
	out.chars = used;
	out.truncated = truncated;
	return out;
}`

// readPageText evaluates pageTextScript in the session's page.
func readPageText(ctx context.Context, sessions *browser.SessionManager, sessionID, query string, maxChars int, digest bool) (map[string]interface{}, error) {
	page, ok := sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if maxChars <= 0 {
		maxChars = defaultTextMaxChars
	}
	if maxChars > maxTextMaxChars {
		maxChars = maxTextMaxChars
	}
	result, err := page.Context(ctx).Eval(pageTextScript, map[string]interface{}{
		"query":    strings.TrimSpace(query),
		"maxChars": maxChars,
		"digest":   digest,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read page text: %w", err)
	}
	data, ok := result.Value.Val().(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("page text returned no data")
	}
	return data, nil
}

// observeText is browser-observe mode "text": what the page says, within max_chars.
func (t *BrowserObserveTool) observeText(ctx context.Context, sessionID string, args map[string]interface{}, view string) (map[string]interface{}, error) {
	query := getStringArg(args, "text_query")
	maxChars := getIntArg(args, "max_chars", defaultTextMaxChars)
	data, err := readPageText(ctx, t.sessions, sessionID, query, maxChars, view == "summary")
	if err != nil {
		return nil, err
	}
	if view != "full" {
		delete(data, "url")
	}
	headings := len(toAnySlice(data["headings"]))
	blocks := len(toAnySlice(data["blocks"]))
	summary := fmt.Sprintf("%d heading(s), %d of %d text block(s)", headings, blocks, asInt(data["total_blocks"]))
	if query != "" {
		summary = fmt.Sprintf("%d block(s) match %q", asInt(data["matched_blocks"]), query)
	}
	if truncated, _ := data["truncated"].(bool); truncated {
		summary += "; budget reached (raise max_chars or narrow with text_query)"
	}
	handles := []string{"observe:" + sessionID + ":text"}
	emitDisclosureFacts(ctx, t.engine, sessionID, handles, "observe")
	return map[string]interface{}{
		"success":          true,
		"status":           "ok",
		"mode":             "text",
		"view":             view,
		"summary":          summary,
		"data":             data,
		"evidence_handles": handles,
		"truncated":        data["truncated"],
	}, nil
}

// contentDigest condenses a page-text digest into what composite observe shows: the
// headings and the live-region lines, together at most contentDigestChars characters.
func contentDigest(text map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	budget := contentDigestChars
	take := func(key string, limit int) {
		items := make([]string, 0, limit)
		for _, raw := range toAnySlice(text[key]) {
			s := fmt.Sprintf("%v", raw)
			if len(items) >= limit || len(s) > budget {
				break
			}
			budget -= len(s)
			items = append(items, s)
		}
		if len(items) > 0 {
			out[key] = items
		}
	}
	take("live", 3)
	take("headings", 4)
	if total := asInt(text["total_headings"]); total > len(toAnySlice(out["headings"])) && len(out) > 0 {
		out["more"] = "mode:text"
	}
	return out
}
