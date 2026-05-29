// app/src/screens/__tests__/LoginScreen.test.tsx
// Phase 5 Wave 0 scaffold — Wave 3 fills in implementations.
// Tracks: 05-VALIDATION.md task 5-UI-01.

describe.skip('LoginScreen renders', () => {
  it('shows Apple + Google + Guest CTAs on iOS', () => {
    // Wave 3: mock Platform.OS = 'ios'; render LoginScreen via react-test-renderer
    // expect three CTAs (Continue with Apple, Continue with Google, Continue as Guest)
  });

  it('hides Apple CTA on Android', () => {
    // Wave 3: mock Platform.OS = 'android'; render LoginScreen
    // expect Apple CTA absent; Google + Guest CTAs present
  });
});
