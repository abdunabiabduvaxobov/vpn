// Phase 5 — Payment helpers (Stripe-era helpers DELETED per D-14).
//
// - upgradeUrlForLocale: derives the locale-aware /pricing URL the
//   mobile app opens in the system browser (D-16).
// - getInvoice: polled by ActivatingProModal on the locked Phase 4 D-21
//   cadence (2s × 5 → 2s + ?escalate=true × 10 → 30s timeout).

import api from './api';
import type {ApiResponse} from '../types/api';

export interface Invoice {
  id: string;
  status: 'pending' | 'paid' | 'failed' | 'expired';
  amount?: number;
  currency?: string;
  plan_id?: string;
  offer_id?: string;
  created_at?: string;
  updated_at?: string;
}

/**
 * D-16: returns ru-prefixed URL for any locale starting with 'ru'
 * (handles ru, ru-RU, ru-UA, etc per RESEARCH.md A10); otherwise EN.
 * Mobile carries EN + RU only — ES locale falls back to EN.
 */
export function upgradeUrlForLocale(i18nLocale: string): string {
  const isRussian = (i18nLocale || '').toLowerCase().startsWith('ru');
  const lang = isRussian ? 'ru' : 'en';
  return `https://risevpn.com/${lang}/pricing?return=app`;
}

/**
 * Fetches a single invoice by id. Append ?escalate=true after poll #5
 * (10s elapsed) to force the backend → lava fallback reconciliation
 * per Phase 4 D-21 cadence.
 */
export async function getInvoice(
  invoiceId: string,
  escalate = false,
): Promise<Invoice> {
  const url = escalate
    ? `/invoices/${invoiceId}?escalate=true`
    : `/invoices/${invoiceId}`;
  const {data} = await api.get<ApiResponse<Invoice>>(url);
  return data.data;
}
