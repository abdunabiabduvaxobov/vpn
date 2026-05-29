// Phase 5 — Tests for payment service.
// Tracks: 05-VALIDATION.md 5-SVC-07, 5-SVC-08.

import {upgradeUrlForLocale, getInvoice} from '../payment';
import api from '../api';
import * as paymentModule from '../payment';

jest.mock('../api', () => ({
  __esModule: true,
  default: {
    get: jest.fn(),
  },
}));

describe('upgradeUrlForLocale', () => {
  it('returns RU URL for ru locale', () => {
    expect(upgradeUrlForLocale('ru')).toBe(
      'https://risevpn.com/ru/pricing?return=app',
    );
  });
  it('returns RU URL for ru-RU regional variant', () => {
    expect(upgradeUrlForLocale('ru-RU')).toBe(
      'https://risevpn.com/ru/pricing?return=app',
    );
  });
  it('returns EN URL for en locale', () => {
    expect(upgradeUrlForLocale('en')).toBe(
      'https://risevpn.com/en/pricing?return=app',
    );
  });
  it('falls back to EN URL for unknown locales (es)', () => {
    expect(upgradeUrlForLocale('es')).toBe(
      'https://risevpn.com/en/pricing?return=app',
    );
  });
  it('handles empty input safely', () => {
    expect(upgradeUrlForLocale('')).toBe(
      'https://risevpn.com/en/pricing?return=app',
    );
  });
});

describe('getInvoice', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('GETs /invoices/:id without query string by default', async () => {
    (api.get as jest.Mock).mockResolvedValueOnce({
      data: {data: {id: 'inv_123', status: 'pending'}},
    });
    await getInvoice('inv_123');
    expect(api.get).toHaveBeenCalledWith('/invoices/inv_123');
  });

  it('appends ?escalate=true when escalate is true', async () => {
    (api.get as jest.Mock).mockResolvedValueOnce({
      data: {data: {id: 'inv_123', status: 'paid'}},
    });
    await getInvoice('inv_123', true);
    expect(api.get).toHaveBeenCalledWith('/invoices/inv_123?escalate=true');
  });

  it('returns the inner data field from ApiResponse', async () => {
    (api.get as jest.Mock).mockResolvedValueOnce({
      data: {data: {id: 'inv_x', status: 'paid'}},
    });
    const inv = await getInvoice('inv_x');
    expect(inv.id).toBe('inv_x');
    expect(inv.status).toBe('paid');
  });
});

describe('payment.ts no longer exports Stripe helpers', () => {
  it('does not export createCheckoutSession', () => {
    expect((paymentModule as any).createCheckoutSession).toBeUndefined();
  });
  it('does not export cancelSubscription', () => {
    expect((paymentModule as any).cancelSubscription).toBeUndefined();
  });
});
