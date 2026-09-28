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

	// Toasts, snackbars, alerts and notifications, from any common UI library.
	const toastPatterns = /toast|notification|alert|snackbar|banner|message|notice|popup|notistack/i;
	const errorPatterns = /error|danger|fail|critical|destructive/i;
	const warningPatterns = /warning|warn|caution/i;
	const successPatterns = /success|done|complete|confirmed/i;
	const infoPatterns = /info|information|note/i;

	const detectToastLevel = (el) => {
		const role = (el.getAttribute('role') || '').toLowerCase();
		const ariaLive = (el.getAttribute('aria-live') || '').toLowerCase();
		const dataType = (el.getAttribute('data-type') || el.getAttribute('data-status') || el.getAttribute('data-variant') || '').toLowerCase();
		const combined = classOf(el).toLowerCase() + ' ' + role + ' ' + dataType;
		if (errorPatterns.test(combined)) return 'error';
		if (warningPatterns.test(combined)) return 'warning';
		if (successPatterns.test(combined)) return 'success';
		if (infoPatterns.test(combined)) return 'info';
		// role="alert" is implicitly assertive: an interruption, not a note.
		if (ariaLive === 'assertive' || role === 'alert' || role === 'alertdialog') return 'error';
		if (errorPatterns.test((el.textContent || '').substring(0, 200))) return 'error';
		return 'info';
	};

	const libraryClasses = ['muisnackbar', 'muialert', 'chakra-alert', 'chakra-toast', 'ant-notification', 'ant-message', 'ant-alert', 'sonner', 'toaster', 'toastify', 'react-hot-toast', 'notistack'];
	const isToastElement = (el) => {
		if (!el || el.nodeType !== 1) return false;
		const classes = classOf(el).toLowerCase();
		const role = el.getAttribute('role') || '';
		const ariaLive = el.getAttribute('aria-live') || '';
		const id = (el.id || '').toLowerCase();
		const dataTestId = el.getAttribute('data-testid') || '';
		if (toastPatterns.test(classes) || toastPatterns.test(id) || toastPatterns.test(dataTestId)) return true;
		if (role === 'alert' || role === 'alertdialog' || role === 'status') return true;
		if (ariaLive === 'polite' || ariaLive === 'assertive') return true;
		return libraryClasses.some((name) => classes.includes(name));
	};

	const extractToastSource = (el) => {
		const classes = classOf(el).toLowerCase();
		if (classes.includes('mui') || classes.includes('material')) return 'material-ui';
		if (classes.includes('chakra')) return 'chakra-ui';
		if (classes.includes('ant-')) return 'ant-design';
		if (classes.includes('sonner') || classes.includes('toaster')) return 'shadcn';
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
	const checkToast = (el) => {
		if (!isToastElement(el)) return;
		const text = toastText(el);
		if (!text || isRouteAnnouncement(el, text)) return;
		if (seenToast(toastKeyOf(text))) return;
		w.__browsernerdEvents.push({
			type: 'toast',
			text: text,
			level: detectToastLevel(el),
			source: extractToastSource(el),
			id: el.id || el.getAttribute('data-testid') || '',
			classes: classOf(el).substring(0, 200),
			ts: Date.now()
		});
	};

	// A region that interrupts (role="alert", an assertive live region, a
	// toast-library container) reports text written into it after it exists:
	// most apps and toast libraries reuse one live region and only change its
	// text. A polite status region reports only when it is added, because
	// its text changes too often to be an alert.
	const interruptingRegion = (node) => {
		const el = node && (node.nodeType === 1 ? node : node.parentElement);
		if (!el || !el.closest) return null;
		const region = el.closest('[role="alert"], [role="alertdialog"], [aria-live="assertive"]');
		if (region) return region;
		let cur = el;
		for (let depth = 0; cur && depth < 4; depth++, cur = cur.parentElement) {
			const classes = classOf(cur).toLowerCase();
			if (/toast|snackbar|notistack|sonner/.test(classes) || libraryClasses.some((name) => classes.includes(name))) return cur;
		}
		return null;
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
						node.querySelectorAll('[role="alert"], [role="status"], [aria-live]').forEach(checkToast);
					}
				});
			} catch (e) {}
		});
	}).observe(document, { childList: true, subtree: true, characterData: true });
	return true;
}`

// pageHookNewDocumentSource runs pageHookFunction in each new document.
const pageHookNewDocumentSource = "(" + pageHookFunction + ")();"
