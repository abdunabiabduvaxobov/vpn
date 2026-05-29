// app/src/stores/__tests__/authStore.test.ts
// Phase 5 Wave 0 scaffold — Wave 2 fills in implementations.
// Tracks: 05-VALIDATION.md task 5-SVC-01, 5-SVC-03, 5-SVC-05.

describe.skip('signInWithApple action', () => {
  it('POSTs identity_token+device fingerprint to /auth/apple', () => {
    // Wave 2: expect api.post to have been called with '/auth/apple' and {identity_token, ...fingerprint}
  });

  it('preserves guest JWT in Authorization header (in-place promotion)', () => {
    // Wave 2: when a guest token exists, the /auth/apple call carries Authorization: Bearer <guest JWT>
  });

  it('catches CANCELED error silently', () => {
    // Wave 2: a {code: '1001'} cancellation must not throw to the UI nor show an Alert
  });
});

describe.skip('signInWithGoogle action', () => {
  it('POSTs id_token+device fingerprint to /auth/google', () => {
    // Wave 2: expect api.post to have been called with '/auth/google' and {id_token, ...fingerprint}
  });
});

describe.skip('startActivatingPro / stopActivatingPro', () => {
  it('sets pendingInvoiceId + isActivatingPro=true', () => {
    // Wave 2: startActivatingPro('abc123') sets state.pendingInvoiceId='abc123', state.isActivatingPro=true
  });

  it('clears them on stop', () => {
    // Wave 2: stopActivatingPro() resets pendingInvoiceId=null, isActivatingPro=false
  });
});
