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

const PAYMENT_SUCCESS_BASE = 'vpnapp://payment/success';
// invoiceId must be a UUID shape (8-4-4-4-12 hex). The deep-link value is
// UNTRUSTED (T-1) — this is a cheap shape gate before the backend poll, not
// a security check. Anything that isn't a 36-char hex/dash string is rejected.
const UUID_SHAPE = /^[0-9a-f-]{36}$/i;

/**
 * Extracts the invoiceId from a vpnapp://payment/success?...invoiceId=X URL.
 *
 * Robustness contract (Phase 5 review WR-01/WR-02/L-1):
 *  - Splits the URL on the FIRST `?` and EXACT-matches the base path
 *    `vpnapp://payment/success` — `success-evil`, `successfully`,
 *    `success/../`, wrong scheme/host all return null.
 *  - Finds `invoiceId` ANYWHERE in the query string — supports both
 *    `?invoiceId=X` and `?status=ok&invoiceId=X`.
 *  - URL-decodes the value and validates it is a UUID shape; otherwise null.
 */
export function parseInvoiceFromUrl(url: string | null): string | null {
  if (!url) return null;

  // Split on the FIRST '?' so the base path is matched exactly, not as a
  // prefix that would also accept `success-evil` / `successfully`.
  const qIndex = url.indexOf('?');
  const base = qIndex === -1 ? url : url.slice(0, qIndex);
  const query = qIndex === -1 ? '' : url.slice(qIndex + 1);

  if (base !== PAYMENT_SUCCESS_BASE) return null;
  if (!query) return null;

  // Parse all key=value pairs (also drop any trailing #fragment on the value).
  let raw: string | null = null;
  for (const pair of query.split('&')) {
    const eq = pair.indexOf('=');
    if (eq === -1) continue;
    const key = pair.slice(0, eq);
    if (key !== 'invoiceId') continue;
    let value = pair.slice(eq + 1);
    const hash = value.indexOf('#');
    if (hash !== -1) value = value.slice(0, hash);
    raw = value;
    break;
  }
  if (raw === null) return null;

  let decoded: string;
  try {
    decoded = decodeURIComponent(raw);
  } catch {
    decoded = raw;
  }

  if (!UUID_SHAPE.test(decoded)) return null;
  return decoded;
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
