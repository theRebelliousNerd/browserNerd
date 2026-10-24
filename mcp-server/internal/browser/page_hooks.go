package browser

// pageHookFunction installs the in-page trackers that feed click_event,
// input_event, state_change and toast facts through window.__browsernerdEvents,
// which the event poller drains. It is registered to run in every new document
// before the page's own scripts (so the first toast of a page load is seen) and
// evaluated once in the current document for a tab that was already loaded.
// It is idempotent, and it observes `document` itself because a new document
// has no <html> or <body> yet when it runs.
const pageHookFunction = `() => {
	const w = window;
	if (w.__browsernerdHooked) return true;
	w.__browsernerdHooked = true;
	w.__browsernerdEvents = [];

	const classOf = (el) => {
		if (!el) return '';
		if (typeof el.className === 'string') return el.className;
		return (el.getAttribute && el.getAttribute('class')) || '';
	};

	document.addEventListener('click', (ev) => {
		try {
			const target = ev.target || {};
			w.__browsernerdEvents.push({ type: 'click', id: target.id || '', ts: Date.now() });
		} catch (e) {}
	}, true);

	const inputValue = (target) => {
		const descriptor = [
			target.type || '',
			target.autocomplete || '',
			target.id || '',
			target.name || '',
			(target.getAttribute && target.getAttribute('aria-label')) || ''
		].join(' ').toLowerCase();
		const sensitive = /password|passwd|secret|token|api.?key|one.?time.?code|cc.?number|cc.?csc|card.?number|cvv|cvc/.test(descriptor);
		return sensitive ? '[REDACTED]' : (target.value || '');
	};

	// Input and change events capture safe values; credentials are redacted in-page.
	const onInput = (ev) => {
		try {
			const target = ev.target || {};
			w.__browsernerdEvents.push({ type: 'input', id: target.id || target.name || '', value: inputValue(target), ts: Date.now() });
		} catch (e) {}
	};
	document.addEventListener('input', onInput, true);
	document.addEventListener('change', onInput, true);

	// State changes exposed through data-state* attributes.
	new MutationObserver((mutations) => {
		mutations.forEach((m) => {
			if (m.type === 'attributes' && m.attributeName && m.attributeName.startsWith('data-state')) {
				const val = (m.target && m.target.getAttribute) ? (m.target.getAttribute(m.attributeName) || '') : '';
				w.__browsernerdEvents.push({ type: 'state', name: m.attributeName, value: val, ts: Date.now() });
			}
		});
	}).observe(document, { attributes: true, subtree: true });

	` + NotificationClassifierJS + `
	// Severity is separate from whether an announcement is a toast.
	const errorPatterns = /error|danger|fail|critical|destructive/i;
	const warningPatterns = /warning|warn|caution/i;
	const successPatterns = /success|done|complete|confirmed/i;
	const infoPatterns = /info|information|note/i;

	const detectToastLevel = (el) => {
		const dataType = (el.getAttribute('data-type') || el.getAttribute('data-status') || el.getAttribute('data-variant') || '').toLowerCase();
		for (const descriptor of [dataType, classOf(el), (el.textContent || '').substring(0, 200)]) {
			if (errorPatterns.test(descriptor)) return 'error';
			if (warningPatterns.test(descriptor)) return 'warning';
			if (successPatterns.test(descriptor)) return 'success';
			if (infoPatterns.test(descriptor)) return 'info';
		}
		return 'info';
	};

	const extractToastSource = (el) => {
		const classes = classOf(el).toLowerCase();
		if (classes.includes('mui') || classes.includes('material')) return 'material-ui';
		if (classes.includes('chakra')) return 'chakra-ui';
		if (classes.includes('ant-')) return 'ant-design';
		if (el.hasAttribute('data-sonner-toast') || classes.includes('sonner') || classes.includes('toaster')) return 'shadcn';
		if (classes.includes('toastify')) return 'react-toastify';
		if (classes.includes('react-hot-toast')) return 'react-hot-toast';
		if (classes.includes('notistack')) return 'notistack';
		return 'native';
	};

	// A router announces each client-side navigation in an assertive live
	// region (Next.js, Gatsby): the page title, read aloud, not a toast.
	const routeAnnouncerSel = 'next-route-announcer, #__next-route-announcer__, #gatsby-announcer, [data-route-announcer]';
	const isRouteAnnouncement = (el, text) => {
		if (el.closest && el.closest(routeAnnouncerSel)) return true;
		const title = (document.title || '').trim();
		return title !== '' && text === title;
	};

	// A toast's title and description are separate blocks: innerText keeps
	// them apart ("Login failed Invalid credentials") where textContent runs
	// them together ("Login failedInvalid credentials").
	const toastText = (el) => {
		const raw = (el.isConnected && typeof el.innerText === 'string' && el.innerText) || el.textContent || '';
		return raw.replace(/\s+/g, ' ').trim().substring(0, 500);
	};
	// One notification is one toast for five seconds, whether it re-renders or
	// a mutation reports only its title or its description.
	const seenToasts = new Map();
	const toastKeyOf = (text) => text.replace(/\s+/g, '').toLowerCase().substring(0, 200);
	const seenToast = (key) => {
		const now = Date.now();
		for (const [seen, expires] of seenToasts) {
			if (expires <= now) { seenToasts.delete(seen); continue; }
			if (seen === key || (key.length >= 6 && (seen.startsWith(key) || seen.endsWith(key)))) return true;
		}
		seenToasts.set(key, now + 5000);
		return false;
	};
	const checkToast = (node) => {
		if (!node || node.nodeType !== 1) return;
		const toast = bnToastRoot(node);
		const el = toast || (node.matches(bnLiveSelector) ? node : null);
		if (!el || !bnVisible(el)) return;
		if (!toast && bnContainsToast(el)) return;
		const text = toastText(el);
		if (!text || isRouteAnnouncement(el, text)) return;
		const role = bnRegionRole(el);
		if (seenToast((toast ? 'toast:' : role + ':') + toastKeyOf(text))) return;
		w.__browsernerdEvents.push({
			type: toast ? 'toast' : 'live_region',
			role: role,
			text: text,
			level: detectToastLevel(el),
			source: extractToastSource(el),
			id: el.id || el.getAttribute('data-testid') || '',
			classes: classOf(el).substring(0, 200),
			ts: Date.now()
		});
	};

	// Reused announcement and toast regions report changed text. Repeated
	// identical text is deduplicated independently for each event kind.
	const interruptingRegion = (node) => {
		const el = node && (node.nodeType === 1 ? node : node.parentElement);
		if (!el || !el.closest) return null;
		return bnToastRoot(el) || el.closest(bnLiveSelector);
	};

	new MutationObserver((mutations) => {
		mutations.forEach((m) => {
			try {
				if (m.type === 'characterData') {
					const region = interruptingRegion(m.target);
					if (region) checkToast(region);
					return;
				}
				if (m.type !== 'childList') return;
				const region = interruptingRegion(m.target);
				if (region && (m.addedNodes.length || m.removedNodes.length)) checkToast(region);
				m.addedNodes.forEach((node) => {
					if (!node || node.nodeType !== 1) return;
					checkToast(node);
					// Toast containers often wrap the element that carries the role.
					if (node.querySelectorAll) {
						node.querySelectorAll(bnLiveSelector + ',' + bnToastSelector).forEach(checkToast);
					}
				});
			} catch (e) {}
		});
	}).observe(document, { childList: true, subtree: true, characterData: true });
	return true;
}`

// pageHookNewDocumentSource runs pageHookFunction in each new document.
const pageHookNewDocumentSource = "(" + pageHookFunction + ")();"
