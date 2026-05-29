// app/src/services/__tests__/deepLink.test.ts
// Phase 5 Wave 0 scaffold — Wave 2 fills in implementations.
// Tracks: 05-VALIDATION.md task 5-SVC-06.

describe.skip('parseInvoiceFromUrl', () => {
  it('extracts invoiceId from vpnapp://payment/success?invoiceId=X', () => {
    // Wave 2: import parseInvoiceFromUrl from '../deepLink';
    // expect parseInvoiceFromUrl('vpnapp://payment/success?invoiceId=abc123') to equal 'abc123'
  });

  it('returns null for wrong scheme', () => {
    // Wave 2: expect parseInvoiceFromUrl('https://payment/success?invoiceId=abc123') to be null
  });

  it('returns null for missing invoiceId param', () => {
    // Wave 2: expect parseInvoiceFromUrl('vpnapp://payment/success') to be null
  });

  it('decodeURIComponents the captured value', () => {
    // Wave 2: expect parseInvoiceFromUrl('vpnapp://payment/success?invoiceId=a%20b') to equal 'a b'
  });
});
