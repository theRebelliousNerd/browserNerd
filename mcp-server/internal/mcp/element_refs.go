package mcp

import (
	"strings"
	"time"

	"browsernerd-mcp-server/internal/browser"

	"github.com/go-rod/rod"
)

// elementRefScript declares the one function that names interactive elements.
//
// Every observation that hands out refs (get-interactive-elements, screenshot overlays)
// and the resolver that turns a ref back into an element evaluate this same source. A
// ref is a pure function of the rendered page: duplicates are numbered over every
// candidate in document order, before any filter, limit or visibility option is applied.
// So an element carries the same ref in every observation, and a ref resolves to exactly
// the element it labelled instead of to a position counted over some other population.
//
// Candidates are the semantic controls __bnSelectors matches, plus the controls a page made
// clickable without saying so: an element with an onclick attribute, one whose React props
// carry onClick, or one whose own cursor is pointer (not merely inherited from its parent).
// Such an element is only a candidate outside every semantic control, so the icon inside a
// button never becomes a second target.
//
// It declares __bnSelectors, __bnMatchesFilter(record, filter), __bnAccessibleLabel(el),
// __bnClassify(record) -> {type, action}, __bnHref(el) and __bnElementRefs(), which returns
// one record per candidate: {el, ref, tag, classes, role, dataTestId, ariaLabel, elId,
// elName, isRowLike, rowIndex, rowKeyRaw, clickable, hydration}. clickable names why a
// non-semantic element is a candidate (onclick | react | pointer, empty for a semantic
// control). hydration is set only on a page with a React root: "owned" when React rendered
// the element; "foreign" when React did not but rendered an ancestor inside the root (a
// widget library's own DOM); "dead" when nothing between it and the root carries a React
// fiber or props, so React never attached to it; "unhydrated" on server-rendered React
// markup with no root or fiber at all well after load, a page whose JavaScript never ran.
// Embed it inside a function body.
//
// __bnAccessibleLabel names an element the way assistive technology does: aria-labelledby,
// then aria-label, then the <label> elements associated with it, then its own text, and only
// then title, placeholder and alt. A form control is therefore reported by its label ("Your
// name"), never by its placeholder example ("Jordan Lee").
const elementRefScript = `
		const __bnSelectors = {
			buttons: 'button, input[type="submit"], input[type="button"], input[type="reset"], input[type="image"], summary, [role="button"], [role="tab"], [role="menuitem"], [role="menuitemcheckbox"], [role="menuitemradio"], [role="treeitem"], [role="option"], [role="switch"], [role="checkbox"], [role="radio"], [role="row"][aria-rowindex], [role="row"][data-rowindex], [role="row"][data-row-id], [role="row"][data-row-key], [role="gridcell"][tabindex], tr[aria-rowindex], tr[data-rowindex], tr[data-row-id], tr[data-row-key], .ag-row, .MuiDataGrid-row',
			inputs: 'input:not([type="hidden"]):not([type="submit"]):not([type="button"]):not([type="reset"]):not([type="image"]), textarea, [contenteditable="true"], [contenteditable=""]',
			links: 'a[href], [role="link"]',
			selects: 'select, [role="combobox"], [role="listbox"]'
		};
		const __bnAllSelector = Object.values(__bnSelectors).join(', ');
		const __bnEncodeRefPart = (value) => encodeURIComponent(String(value ?? '')).substring(0, 120);
		const __bnMatchesFilter = (record, filter) =>
			filter === 'all' ||
			(filter === 'buttons' && !!record.clickable) ||
			record.el.matches(__bnSelectors[filter] || __bnAllSelector);
		const __bnClassify = ({ el, tag, role, isRowLike }) => {
			const r = String(role || '').toLowerCase();
			const typeable = tag === 'input' || tag === 'textarea' || el.isContentEditable;
			if (r === 'combobox') return { type: 'combobox', action: typeable ? 'type' : 'click' };
			if (r === 'option' || r === 'treeitem') return { type: 'option', action: 'click' };
			if (r === 'tab') return { type: 'tab', action: 'click' };
			if (r === 'menuitem' || r === 'menuitemcheckbox' || r === 'menuitemradio') return { type: 'menuitem', action: 'click' };
			if (r === 'checkbox' || r === 'switch') return { type: 'checkbox', action: 'toggle' };
			if (r === 'radio') return { type: 'radio', action: 'toggle' };
			if (r === 'listbox' && tag !== 'select') return { type: 'listbox', action: 'click' };
			if (tag === 'button' || tag === 'summary' || r === 'button' ||
			    (tag === 'input' && ['submit', 'button', 'reset', 'image'].includes(el.type))) {
				return { type: 'button', action: 'click' };
			}
			if (tag === 'a' || r === 'link') return { type: 'link', action: 'click' };
			if (tag === 'select') return { type: 'select', action: 'select' };
			if (isRowLike) return { type: 'row', action: 'click' };
			if (tag === 'input') {
				return (el.type === 'checkbox' || el.type === 'radio')
					? { type: el.type, action: 'toggle' }
					: { type: 'input', action: 'type' };
			}
			if (typeable) return { type: 'input', action: 'type' };
			return { type: 'clickable', action: 'click' };
		};
		const __bnHref = (el) => {
			const raw = el.getAttribute('href');
			if (!raw) return '';
			let href = raw;
			try {
				const url = new URL(raw, location.href);
				href = url.origin === location.origin ? url.pathname + url.search + url.hash : url.href;
			} catch (e) {}
			return href.length > 120 ? href.substring(0, 117) + '...' : href;
		};
		const __bnReactInfo = (el) => {
			let props = null, owned = false, container = false;
			for (const key of Object.keys(el)) {
				if (key.startsWith('__reactProps$') || key.startsWith('__reactEventHandlers$')) {
					props = el[key];
					owned = true;
				} else if (key.startsWith('__reactFiber$') || key.startsWith('__reactInternalInstance$')) {
					owned = true;
				} else if (key.startsWith('__reactContainer$') || key === '_reactRootContainer') {
					container = true;
				}
			}
			return { props, owned, container };
		};
		const __bnNonSemanticSkip = new Set(['html', 'head', 'body', 'script', 'style', 'noscript', 'template', 'link', 'meta', 'br', 'hr', 'option', 'optgroup']);
		const __bnCollapseText = (text) => String(text ?? '').replace(/\s+/g, ' ').trim();
		const __bnTextOf = (nodes) => nodes
			.map((node) => __bnCollapseText(node.innerText || node.textContent))
			.filter(Boolean)
			.join(' ');
		const __bnAccessibleLabel = (el) => {
			const labelledBy = __bnTextOf((el.getAttribute('aria-labelledby') || '')
				.split(/\s+/)
				.filter(Boolean)
				.map((id) => document.getElementById(id))
				.filter(Boolean));
			const associated = el.labels ? __bnTextOf(Array.from(el.labels)) : '';
			const label = labelledBy ||
				__bnCollapseText(el.getAttribute('aria-label')) ||
				associated ||
				__bnCollapseText(el.innerText) ||
				__bnCollapseText(el.getAttribute('title')) ||
				__bnCollapseText(el.getAttribute('placeholder')) ||
				__bnCollapseText(el.getAttribute('alt'));
			return label.length > 50 ? label.substring(0, 47) + '...' : label;
		};
		// __bnStyleBudget caps the computed-style reads one scan spends looking for
		// cursor:pointer, so a page of tens of thousands of elements still resolves a ref
		// quickly. Elements past it are still found through onclick and React props.
		const __bnStyleBudget = 10000;
		const __bnCandidates = () => {
			const semantic = Array.from(document.querySelectorAll(__bnAllSelector));
			const semanticSet = new Set(semantic);
			const onclick = new Set(document.querySelectorAll('[onclick]'));
			const extra = [];
			const clickable = new Map();
			const reactRoots = [];
			const root = document.body || document.documentElement;
			if (root) {
				if (__bnReactInfo(root).container) reactRoots.push(root);
				const all = root.querySelectorAll('*');
				let styleBudget = __bnStyleBudget;
				let inside = null;
				for (let k = 0; k < all.length; k++) {
					const node = all[k];
					// A semantic control's subtree is skipped: what sits inside a button is
					// part of that button's target, not a second one.
					if (inside) {
						if (inside.contains(node)) continue;
						inside = null;
					}
					if (semanticSet.has(node)) {
						inside = node;
						continue;
					}
					const info = __bnReactInfo(node);
					if (info.container) reactRoots.push(node);
					const tag = node.localName;
					if (__bnNonSemanticSkip.has(tag)) continue;
					if (node.namespaceURI === 'http://www.w3.org/2000/svg' && tag !== 'svg') continue;
					if (tag === 'label' && node.control) continue;
					let kind = '';
					if (onclick.has(node)) {
						kind = 'onclick';
					} else if (info.props && typeof info.props.onClick === 'function') {
						kind = 'react';
					} else if (styleBudget > 0) {
						styleBudget--;
						if (getComputedStyle(node).cursor === 'pointer') {
							const parent = node.parentElement;
							if (!parent || getComputedStyle(parent).cursor !== 'pointer') kind = 'pointer';
						}
					}
					if (kind) {
						extra.push(node);
						clickable.set(node, kind);
					}
				}
			}
			const ordered = [];
			let i = 0, j = 0;
			while (i < semantic.length || j < extra.length) {
				if (j >= extra.length) ordered.push(semantic[i++]);
				else if (i >= semantic.length) ordered.push(extra[j++]);
				else if (semantic[i].compareDocumentPosition(extra[j]) & Node.DOCUMENT_POSITION_FOLLOWING) ordered.push(semantic[i++]);
				else ordered.push(extra[j++]);
			}
			// Server-rendered React (Next.js) markup with no React root or fiber
			// anywhere, well after load, is a page whose JavaScript never ran: every
			// control on it is inert, and each is reported unhydrated.
			if (reactRoots.length === 0 && root && document.readyState === 'complete' &&
				performance.now() > 3000 &&
				(document.getElementById('__next') || document.getElementById('__NEXT_DATA__') ||
				 typeof self.__next_f !== 'undefined' || document.querySelector('[data-reactroot]'))) {
				return { ordered, clickable, reactRoots: [root], unhydrated: true };
			}
			return { ordered, clickable, reactRoots, unhydrated: false };
		};
		// __bnOwnedAncestor reports whether React rendered any element between el and its
		// root. A widget library (a code editor, a map, a chart) builds its own DOM inside a
		// container React rendered: that DOM carries no fiber, yet it works.
		const __bnOwnedAncestor = (el, rootEl) => {
			for (let p = el.parentElement; p && p !== rootEl; p = p.parentElement) {
				if (__bnReactInfo(p).owned) return true;
			}
			return false;
		};
		const __bnElementRefs = () => {
			const seenCounts = new Map();
			const records = [];
			const { ordered, clickable, reactRoots, unhydrated } = __bnCandidates();
			ordered.forEach((el, index) => {
				const dataTestId = el.getAttribute('data-testid') || el.getAttribute('data-test-id') || '';
				const ariaLabel = el.getAttribute('aria-label') || '';
				const elId = el.id || '';
				const elName = el.name || '';
				const role = el.getAttribute('role') || '';
				const tag = el.tagName.toLowerCase();
				const classes = Array.from(el.classList);
				const rowClassLike = classes.some((cls) => cls === 'ag-row' || cls === 'MuiDataGrid-row' || cls === 'grid-row' || cls === 'table-row');
				const isRowLike = role === 'row' || tag === 'tr' || rowClassLike;
				const rowIndex = el.getAttribute('aria-rowindex') ||
				                 el.getAttribute('data-rowindex') ||
				                 el.getAttribute('row-index') ||
				                 el.getAttribute('data-row-index') ||
				                 '';
				const rowKeyRaw = el.getAttribute('data-row-id') ||
				                  el.getAttribute('data-row-key') ||
				                  el.getAttribute('data-id') ||
				                  el.getAttribute('data-key') ||
				                  el.getAttribute('data-item-id') ||
				                  el.getAttribute('data-uid') ||
				                  '';

				let baseRef;
				if (dataTestId) {
					baseRef = 'testid:' + dataTestId;
				} else if (ariaLabel && ariaLabel.length < 50) {
					baseRef = 'aria:' + ariaLabel.replace(/[^a-zA-Z0-9_-]/g, '_').substring(0, 40);
				} else if (elId) {
					baseRef = elId;
				} else if (elName) {
					baseRef = elName;
				} else if (isRowLike && rowKeyRaw) {
					baseRef = 'rowkey:' + __bnEncodeRefPart(rowKeyRaw);
				} else if (isRowLike && rowIndex) {
					baseRef = 'row:' + __bnEncodeRefPart(rowIndex);
				} else {
					const classStr = classes.slice(0, 2).join('.');
					baseRef = classStr ? tag + '.' + classStr : tag + '[' + index + ']';
				}

				const dupCount = seenCounts.get(baseRef) || 0;
				seenCounts.set(baseRef, dupCount + 1);
				let hydration = '';
				const enclosing = reactRoots.find((r) => r !== el && r.contains(el));
				if (enclosing && unhydrated) {
					hydration = 'unhydrated';
				} else if (enclosing) {
					if (__bnReactInfo(el).owned) hydration = 'owned';
					else hydration = __bnOwnedAncestor(el, enclosing) ? 'foreign' : 'dead';
				}
				records.push({
					el,
					ref: dupCount > 0 ? baseRef + '_' + dupCount : baseRef,
					tag, classes, role, dataTestId, ariaLabel, elId, elName, isRowLike, rowIndex, rowKeyRaw,
					clickable: clickable.get(el) || '',
					hydration
				});
			});
			return records;
		};
`

// resolveGeneratedRef runs elementRefScript in the page and returns the element that
// carries ref right now. It reports false when no element carries it or the page cannot
// be evaluated, so the caller can fall back to fingerprint matching.
func resolveGeneratedRef(page *rod.Page, ref string, timeout time.Duration) (*rod.Element, bool) {
	if ref == "" {
		return nil, false
	}
	obj, err := page.Timeout(timeout).Evaluate(rod.Eval(`(ref) => {`+elementRefScript+`
		const match = __bnElementRefs().find((record) => record.ref === ref);
		return match ? match.el : null;
	}`, ref).ByObject())
	if err != nil || obj == nil || obj.ObjectID == "" {
		return nil, false
	}
	element, err := page.ElementFromObject(obj)
	if err != nil {
		return nil, false
	}
	return element, true
}

// refStillNamesFingerprint reports whether element is still the element fingerprinted
// when its ref was handed out. A ref numbers duplicates in document order, so a control
// inserted above it since the observation moves the name to a different element; tag and
// text catch that. Position is deliberately not compared: scrolling moves every element
// without renaming any.
func refStillNamesFingerprint(element *rod.Element, fp *browser.ElementFingerprint) bool {
	if fp == nil {
		return true
	}
	if fp.TagName != "" {
		tagName, err := element.Property("tagName")
		if err != nil || !strings.EqualFold(tagName.Str(), fp.TagName) {
			return false
		}
	}
	// An element named by a stable attribute keeps its identity when its text changes - a
	// toggle relabels itself on every click. Only a positionally named element is
	// identified by what it says.
	if fp.DataTestID != "" || fp.AriaLabel != "" || fp.ID != "" || fp.Name != "" {
		return true
	}
	if fp.TextContent != "" {
		text, err := element.Text()
		if err != nil {
			return false
		}
		if !strings.HasPrefix(strings.Join(strings.Fields(text), " "), fp.TextContent) {
			return false
		}
	}
	return true
}
