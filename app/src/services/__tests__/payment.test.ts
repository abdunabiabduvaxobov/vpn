// app/src/services/__tests__/payment.test.ts
// Phase 5 Wave 0 scaffold — Wave 2 fills in implementations.
// Tracks: 05-VALIDATION.md task 5-SVC-07, 5-SVC-08.

describe.skip('upgradeUrlForLocale', () => {
  it('returns https://risevpn.com/ru/pricing?return=app for locale starting with ru', () => {
    // Wave 2: import {upgradeUrlForLocale} from '../payment';
    // expect upgradeUrlForLocale('ru') to equal 'https://risevpn.com/ru/pricing?return=app'
  });

  it('returns https://risevpn.com/en/pricing?return=app otherwise', () => {
    // Wave 2: expect upgradeUrlForLocale('es') to equal 'https://risevpn.com/en/pricing?return=app'
  });
});

describe.skip('getInvoice', () => {
  it('GETs /invoices/:id without escalate by default', () => {
    // Wave 2: import {getInvoice} from '../payment';
    // expect axios.get to have been called with '/invoices/abc123' (no query string)
  });

  it('appends ?escalate=true when escalate=true is passed', () => {
    // Wave 2: expect axios.get to have been called with '/invoices/abc123?escalate=true'
  });
});
