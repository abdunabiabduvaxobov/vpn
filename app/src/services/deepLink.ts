// Phase 5 — Deep-link handler for vpnapp://payment/success?invoiceId=X
//
// Receives the OS-delivered URL on both cold start (getInitialURL) and
// warm foreground (addEventListener('url')), parses the invoiceId, and
// dispatches to authStore.startActivatingPro which triggers the
// Activating-Pro modal overlay.
//
// T-1: invoiceId from the URL is UNTRUSTED — the modal verifies status
// via GET /invoices/:id (backend is the source of truth).

import {Linking} from 'react-native';
import {useAuthStore} from '../stores/authStore';

const DEEP_LINK_SCHEME = 'vpnapp://';
const PAYMENT_SUCCESS_PATH = 'vpnapp://payment/success';

/**
 * Extracts the invoiceId from a vpnapp://payment/success?invoiceId=X URL.
 * Returns null for any other scheme, path, or missing query parameter.
 * URL-decodes the captured value.
 */
export function parseInvoiceFromUrl(url: string | null): string | null {
  if (!url || !url.startsWith(DEEP_LINK_SCHEME)) return null;
  if (!url.startsWith(PAYMENT_SUCCESS_PATH)) return null;
  const m = url.match(/\?invoiceId=([^&]+)/);
  if (!m) return null;
  try {
    return decodeURIComponent(m[1]);
  } catch {
    return m[1];
  }
}

/**
 * Registers OS deep-link handlers. Returns an unsubscribe function.
 * MUST be called once at app boot from App.tsx.
 */
export function registerDeepLinkHandler(): () => void {
  // Cold-start: app was launched by the deep link.
  Linking.getInitialURL()
    .then((url) => {
      const invoiceId = parseInvoiceFromUrl(url);
      if (invoiceId) {
        useAuthStore.getState().startActivatingPro(invoiceId);
      }
    })
    .catch(() => {
      // Linking.getInitialURL() can reject on some Android versions; ignore.
    });

  // Warm: app already running.
  const sub = Linking.addEventListener('url', ({url}) => {
    const invoiceId = parseInvoiceFromUrl(url);
    if (invoiceId) {
      useAuthStore.getState().startActivatingPro(invoiceId);
    }
  });

  return () => sub.remove();
}
