package mcp

import (
	"context"
	"fmt"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/mangle"
)

// =============================================================================
// HYPER TOKEN-EFFICIENT NAVIGATION LINKS
// =============================================================================

// GetNavigationLinksTool extracts site navigation with minimal token usage.
// Designed for efficient site exploration - groups links by area, classifies
// internal/external, and emits Mangle facts for reasoning.
type GetNavigationLinksTool struct {
	sessions *browser.SessionManager
	engine   *mangle.Engine
}

func (t *GetNavigationLinksTool) Name() string { return "get-navigation-links" }
func (t *GetNavigationLinksTool) Description() string {
	return `Hyper token-efficient navigation link extraction.

Returns site navigation grouped by area (header, sidebar, main, footer) with:
- href destinations (internal/external/anchor)
- Compact ref for interact tool
- Link text (truncated for efficiency)

Also emits Mangle facts: nav_link(Ref, Href, Area, Internal) for reasoning.

OUTPUT FORMAT (ultra-compact):
{
  "url": "https://example.com/page",
  "nav": {"Dashboard": "/dashboard", "Settings": "/settings"},
  "main": {"Article 1": "/article/1", "External": "https://other.com"},
  "footer": {"Privacy": "/privacy", "Terms": "/terms"},
  "counts": {"total": 15, "internal": 12, "external": 3}
}

Use this instead of get-interactive-elements when you only need navigation.`
}

func (t *GetNavigationLinksTool) InputSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Target session ID (required)",
			},
			"internal_only": map[string]interface{}{
				"type":        "boolean",
				"description": "Only return internal links (same origin). Default: false",
			},
			"max_per_area": map[string]interface{}{
				"type":        "integer",
				"description": "Max links per area (default: 20). Use 0 for unlimited.",
			},
			"emit_facts": map[string]interface{}{
				"type":        "boolean",
				"description": "Emit nav_link facts to Mangle buffer (default: true)",
			},
		},
		"required": []string{"session_id"},
	}
}

func (t *GetNavigationLinksTool) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getStringArg(args, "session_id")
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}

	internalOnly := getBoolArg(args, "internal_only", false)
	maxPerArea := getIntArg(args, "max_per_area", 20)
	emitFacts := getBoolArg(args, "emit_facts", true)

	page, ok := t.sessions.Page(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	// Ultra-efficient JavaScript extraction
	js := fmt.Sprintf(`
	() => {
		const internalOnly = %v;
		const maxPerArea = %d;
		const origin = location.origin;
		const currentPath = location.pathname;

		// Area detection - find which navigation region an element is in
		function getArea(el) {
			let node = el;
			while (node && node !== document.body) {
				const tag = node.tagName?.toLowerCase();
				const role = node.getAttribute?.('role');
				const id = node.id?.toLowerCase() || '';
				const cls = node.className?.toLowerCase?.() || '';

				// Header/Nav detection
				if (tag === 'header' || tag === 'nav' || role === 'navigation' ||
				    id.includes('nav') || id.includes('header') || id.includes('menu') ||
				    cls.includes('nav') || cls.includes('header') || cls.includes('menu')) {
					return 'nav';
				}
				// Sidebar detection
				if (tag === 'aside' || role === 'complementary' ||
				    id.includes('sidebar') || id.includes('side-nav') ||
				    cls.includes('sidebar') || cls.includes('side-nav')) {
					return 'side';
				}
				// Footer detection
				if (tag === 'footer' || role === 'contentinfo' ||
				    id.includes('footer') || cls.includes('footer')) {
					return 'foot';
				}
				node = node.parentElement;
			}
			return 'main';
		}

		// Compact ref generator
		function getRef(el, idx) {
			if (el.id) return el.id;
			if (el.name) return el.name;
			// Use data-testid if available (common in React apps)
			const testId = el.getAttribute('data-testid');
			if (testId) return testId;
			// Minimal fallback
			return 'a' + idx;
		}

		// Truncate text efficiently
		function getText(el) {
			const text = (el.textContent || el.innerText || '').trim().replace(/\s+/g, ' ');
			return text.length > 30 ? text.slice(0, 27) + '...' : text;
		}

		// Classify href
		function classifyHref(href) {
			if (!href || href === '#' || href.startsWith('javascript:')) return null;
			if (href.startsWith('#')) return { type: 'anchor', href: href, internal: true };

			try {
				const url = new URL(href, origin);
				const isInternal = url.origin === origin;
				// Normalize internal paths
				const normalized = isInternal ? url.pathname + url.search + url.hash : url.href;
				return { type: isInternal ? 'internal' : 'external', href: normalized, internal: isInternal };
			} catch {
				return { type: 'relative', href: href, internal: true };
			}
		}

		const areas = { nav: {}, side: {}, main: {}, foot: {} };
		const counts = { nav: 0, side: 0, main: 0, foot: 0 };
		const refs = []; // For Mangle facts
		let total = 0, internal = 0, external = 0;

		document.querySelectorAll('a[href]').forEach((el, idx) => {
			// Skip invisible
			const rect = el.getBoundingClientRect();
			if (rect.width === 0 || rect.height === 0) return;
			const style = getComputedStyle(el);
			if (style.display === 'none' || style.visibility === 'hidden') return;

			const classified = classifyHref(el.href);
			if (!classified) return;
			if (internalOnly && !classified.internal) return;

			const area = getArea(el);

			// Respect max per area
			if (maxPerArea > 0 && counts[area] >= maxPerArea) return;

			const text = getText(el);
			if (!text) return; // Skip empty links

			const ref = getRef(el, idx);

			// Store compactly: text -> href
			areas[area][text] = classified.href;
			counts[area]++;
			total++;
			if (classified.internal) internal++; else external++;

			// Collect for Mangle facts
			refs.push({ ref, href: classified.href, area, internal: classified.internal });
		});

		return {
			url: location.href,
			path: currentPath,
			nav: Object.keys(areas.nav).length ? areas.nav : undefined,
			side: Object.keys(areas.side).length ? areas.side : undefined,
			main: Object.keys(areas.main).length ? areas.main : undefined,
			foot: Object.keys(areas.foot).length ? areas.foot : undefined,
			counts: { total, internal, external },
			_refs: refs // Internal use for Mangle facts
		};
	}
	`, internalOnly, maxPerArea)

	var result map[string]interface{}
	evalResult, err := page.Eval(js)
	if err != nil {
		return nil, fmt.Errorf("failed to extract navigation: %w", err)
	}
	if err := evalResult.Value.Unmarshal(&result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal navigation result: %w", err)
	}

	// Emit Mangle facts for reasoning
	if emitFacts && t.engine != nil {
		if refs, ok := result["_refs"].([]interface{}); ok {
			now := time.Now()
			facts := make([]mangle.Fact, 0, len(refs))

			for _, r := range refs {
				if refMap, ok := r.(map[string]interface{}); ok {
					ref := fmt.Sprintf("%v", refMap["ref"])
					href := fmt.Sprintf("%v", refMap["href"])
					area := fmt.Sprintf("%v", refMap["area"])
					isInternal := "false"
					if internal, ok := refMap["internal"].(bool); ok && internal {
						isInternal = "true"
					}

					facts = append(facts, mangle.Fact{
						Predicate: "nav_link",
						Args:      []interface{}{ref, href, area, isInternal},
						Timestamp: now,
					})
				}
			}

			if len(facts) > 0 {
				_ = t.engine.AddFacts(ctx, facts)
			}
		}
	}

	// Remove internal _refs from output (not needed by caller)
	delete(result, "_refs")

	// Clean up undefined areas for even more compact output
	for _, area := range []string{"nav", "side", "main", "foot"} {
		if result[area] == nil {
			delete(result, area)
		}
	}

	return result, nil
}

