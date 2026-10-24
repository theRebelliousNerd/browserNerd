package browser

// NotificationClassifierJS is shared by event capture and settled DOM reads.
// ARIA roles describe announcements; only toast-specific markers identify toasts.
const NotificationClassifierJS = `
  const bnLiveSelector = '[role="alert"],[role="alertdialog"],[role="status"],[role="log"],[aria-live]:not([aria-live="off"])';
  const bnToastSelector = '[data-sonner-toast],[data-toast],[data-toast-id],.Toastify__toast,.chakra-toast,.ant-notification-notice,.ant-message-notice,.MuiSnackbar-root,.MuiSnackbarContent-root,.notistack-Snackbar';
  const bnToastMarker = (el) => {
    if (!el || el.nodeType !== 1) return false;
    if (el.matches(bnToastSelector)) return true;
    const parts = [el.getAttribute('class') || '', el.id || '', el.getAttribute('data-testid') || ''];
    return parts.some(s => /(^|[\s_-])(toast|snackbar|notistack)([\s_-]|$)/i.test(s));
  };
  const bnToastRoot = (el) => {
    let root = null;
    for (let cur = el, depth = 0; cur && depth < 6; cur = cur.parentElement, depth++) {
      if (bnToastMarker(cur)) root = cur;
    }
    return root;
  };
  const bnRegionRole = (el) => el.getAttribute('role') || (el.getAttribute('aria-live') === 'assertive' ? 'alert' : 'status');
  const bnContainsToast = (el) => Array.from(el.querySelectorAll(bnToastSelector + ',[class*="toast" i],[class*="snackbar" i]')).some(bnToastMarker);
  const bnVisible = (el) => {
    const r = el.getBoundingClientRect(), cs = getComputedStyle(el);
    return el.isConnected && r.width > 0 && r.height > 0 && cs.display !== 'none' && cs.visibility !== 'hidden' && cs.opacity !== '0';
  };
`
