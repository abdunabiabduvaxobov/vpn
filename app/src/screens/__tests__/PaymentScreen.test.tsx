// app/src/screens/__tests__/PaymentScreen.test.tsx
// Phase 5 Wave 0 scaffold — Wave 3 fills in implementations.
// Tracks: 05-VALIDATION.md task 5-UI-02.

describe.skip('PaymentScreen informational layout', () => {
  it('renders no $ price text anywhere', () => {
    // Wave 3: render PaymentScreen for a free user; assert no price/$ text in the tree
  });

  it('renders exactly one CTA with text Upgrade to Pro at risevpn.com', () => {
    // Wave 3: assert exactly one CTA reading "Upgrade to Pro at risevpn.com"
  });

  it('opens LeavingAppSheet before Linking.openURL on CTA tap', () => {
    // Wave 3: tap CTA → interstitial "You're leaving the app" appears BEFORE Linking.openURL fires
  });
});
