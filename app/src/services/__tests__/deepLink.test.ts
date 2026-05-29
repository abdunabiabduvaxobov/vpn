// Phase 5 — Tests for deepLink service.
// Tracks: 05-VALIDATION.md 5-SVC-06.

import {parseInvoiceFromUrl, registerDeepLinkHandler} from '../deepLink';
import {Linking} from 'react-native';

jest.mock('react-native', () => ({
  Linking: {
    addEventListener: jest.fn(),
    getInitialURL: jest.fn(),
    removeEventListener: jest.fn(),
  },
}));

jest.mock('../../stores/authStore', () => ({
  useAuthStore: {
    getState: jest.fn(() => ({startActivatingPro: jest.fn()})),
  },
}));

describe('parseInvoiceFromUrl', () => {
  it('extracts invoiceId from canonical URL', () => {
    expect(
      parseInvoiceFromUrl('vpnapp://payment/success?invoiceId=abc123'),
    ).toBe('abc123');
  });
  it('URL-decodes encoded values', () => {
    expect(
      parseInvoiceFromUrl('vpnapp://payment/success?invoiceId=enc%20oded'),
    ).toBe('enc oded');
  });
  it('returns null for wrong scheme', () => {
    expect(
      parseInvoiceFromUrl('https://risevpn.com/pay/success?invoiceId=X'),
    ).toBeNull();
  });
  it('returns null when invoiceId param is missing', () => {
    expect(parseInvoiceFromUrl('vpnapp://payment/success')).toBeNull();
  });
  it('returns null for wrong path (not /payment/success)', () => {
    expect(parseInvoiceFromUrl('vpnapp://payment/cancel?invoiceId=X')).toBeNull();
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
});
