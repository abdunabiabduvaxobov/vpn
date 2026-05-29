// Phase 5 — Tests for deepLink service.
// Tracks: 05-VALIDATION.md 5-SVC-06; review FIX 1 (WR-01/WR-02/L-1) + QA P0-2.

import {parseInvoiceFromUrl, registerDeepLinkHandler} from '../deepLink';
import {Linking} from 'react-native';

jest.mock('react-native', () => ({
  Linking: {
    addEventListener: jest.fn(),
    getInitialURL: jest.fn(),
    removeEventListener: jest.fn(),
  },
}));

const mockStartActivatingPro = jest.fn();
jest.mock('../../stores/authStore', () => ({
  useAuthStore: {
    getState: jest.fn(() => ({startActivatingPro: mockStartActivatingPro})),
  },
}));

// A valid UUID-shape invoiceId (8-4-4-4-12 hex).
const UUID = '550e8400-e29b-41d4-a716-446655440000';

describe('parseInvoiceFromUrl', () => {
  it('extracts invoiceId from canonical URL', () => {
    expect(
      parseInvoiceFromUrl(`vpnapp://payment/success?invoiceId=${UUID}`),
    ).toBe(UUID);
  });

  // FIX 1b — invoiceId is found anywhere in the query string, not just first.
  it('extracts invoiceId when it is NOT the first query param', () => {
    expect(
      parseInvoiceFromUrl(`vpnapp://payment/success?status=ok&invoiceId=${UUID}`),
    ).toBe(UUID);
  });

  it('extracts invoiceId after a utm_source param', () => {
    expect(
      parseInvoiceFromUrl(
        `vpnapp://payment/success?utm_source=lava&invoiceId=${UUID}`,
      ),
    ).toBe(UUID);
  });

  it('URL-decodes encoded values that still resolve to a UUID shape', () => {
    // %2D is '-' — decodes back to a valid UUID shape.
    const encoded = UUID.replace(/-/g, '%2D');
    expect(
      parseInvoiceFromUrl(`vpnapp://payment/success?invoiceId=${encoded}`),
    ).toBe(UUID);
  });

  it('strips a trailing #fragment from the value', () => {
    expect(
      parseInvoiceFromUrl(`vpnapp://payment/success?invoiceId=${UUID}#frag`),
    ).toBe(UUID);
  });

  // FIX 1a — EXACT base-path match (reject prefix-y look-alikes).
  it('returns null for success-evil path', () => {
    expect(
      parseInvoiceFromUrl(`vpnapp://payment/success-evil?invoiceId=${UUID}`),
    ).toBeNull();
  });

  it('returns null for successfully path', () => {
    expect(
      parseInvoiceFromUrl(`vpnapp://payment/successfully?invoiceId=${UUID}`),
    ).toBeNull();
  });

  it('returns null for a success/.. traversal path', () => {
    expect(
      parseInvoiceFromUrl(`vpnapp://payment/success/..?invoiceId=${UUID}`),
    ).toBeNull();
  });

  it('returns null for wrong scheme', () => {
    expect(
      parseInvoiceFromUrl(`https://risevpn.com/pay/success?invoiceId=${UUID}`),
    ).toBeNull();
  });

  it('returns null for wrong host', () => {
    expect(
      parseInvoiceFromUrl(`vpnapp://other/success?invoiceId=${UUID}`),
    ).toBeNull();
  });

  it('returns null when invoiceId param is missing', () => {
    expect(parseInvoiceFromUrl('vpnapp://payment/success')).toBeNull();
  });

  it('returns null when query has params but no invoiceId', () => {
    expect(parseInvoiceFromUrl('vpnapp://payment/success?status=ok')).toBeNull();
  });

  it('returns null for wrong path (not /payment/success)', () => {
    expect(
      parseInvoiceFromUrl(`vpnapp://payment/cancel?invoiceId=${UUID}`),
    ).toBeNull();
  });

  // FIX 1c — non-UUID invoiceId rejected.
  it('returns null for a non-UUID invoiceId', () => {
    expect(
      parseInvoiceFromUrl('vpnapp://payment/success?invoiceId=abc123'),
    ).toBeNull();
  });

  it('returns null for an invoiceId with disallowed characters', () => {
    expect(
      parseInvoiceFromUrl(
        'vpnapp://payment/success?invoiceId=DROP%20TABLE%20invoices',
      ),
    ).toBeNull();
  });

  it('returns null for null input', () => {
    expect(parseInvoiceFromUrl(null)).toBeNull();
  });
});

describe('registerDeepLinkHandler', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    (Linking.getInitialURL as jest.Mock).mockResolvedValue(null);
    (Linking.addEventListener as jest.Mock).mockReturnValue({
      remove: jest.fn(),
    });
  });

  it('subscribes to url events AND queries getInitialURL', () => {
    const unsubscribe = registerDeepLinkHandler();
    expect(Linking.getInitialURL).toHaveBeenCalledTimes(1);
    expect(Linking.addEventListener).toHaveBeenCalledWith(
      'url',
      expect.any(Function),
    );
    expect(typeof unsubscribe).toBe('function');
  });

  it('returns an unsubscribe function that removes the listener', () => {
    const removeMock = jest.fn();
    (Linking.addEventListener as jest.Mock).mockReturnValue({
      remove: removeMock,
    });
    const unsubscribe = registerDeepLinkHandler();
    unsubscribe();
    expect(removeMock).toHaveBeenCalled();
  });

  // QA P0-2 — cold-start dispatch wiring.
  it('dispatches startActivatingPro on a valid cold-start URL', async () => {
    (Linking.getInitialURL as jest.Mock).mockResolvedValue(
      `vpnapp://payment/success?invoiceId=${UUID}`,
    );
    registerDeepLinkHandler();
    // Flush the getInitialURL().then microtask chain.
    await Promise.resolve();
    await Promise.resolve();
    expect(mockStartActivatingPro).toHaveBeenCalledWith(UUID);
  });

  it('does NOT dispatch on a malformed cold-start URL', async () => {
    (Linking.getInitialURL as jest.Mock).mockResolvedValue(
      'vpnapp://payment/success-evil?invoiceId=abc123',
    );
    registerDeepLinkHandler();
    await Promise.resolve();
    await Promise.resolve();
    expect(mockStartActivatingPro).not.toHaveBeenCalled();
  });

  // QA P0-2 — warm 'url' event dispatch wiring.
  it('dispatches startActivatingPro when the url event fires with a valid URL', () => {
    let urlCallback: ((e: {url: string}) => void) | undefined;
    (Linking.addEventListener as jest.Mock).mockImplementation((_evt, cb) => {
      urlCallback = cb;
      return {remove: jest.fn()};
    });
    registerDeepLinkHandler();
    expect(urlCallback).toBeDefined();
    urlCallback!({url: `vpnapp://payment/success?status=ok&invoiceId=${UUID}`});
    expect(mockStartActivatingPro).toHaveBeenCalledWith(UUID);
  });

  it('does NOT dispatch when the url event fires with a malformed URL', () => {
    let urlCallback: ((e: {url: string}) => void) | undefined;
    (Linking.addEventListener as jest.Mock).mockImplementation((_evt, cb) => {
      urlCallback = cb;
      return {remove: jest.fn()};
    });
    registerDeepLinkHandler();
    urlCallback!({url: 'vpnapp://payment/cancel?invoiceId=' + UUID});
    expect(mockStartActivatingPro).not.toHaveBeenCalled();
  });
});
