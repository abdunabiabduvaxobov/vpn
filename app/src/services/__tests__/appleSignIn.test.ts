// app/src/services/__tests__/appleSignIn.test.ts
// Phase 5 Wave 0 scaffold — Wave 2 fills in implementations.
// Tracks: 05-VALIDATION.md task 5-SVC-01, 5-SVC-02.

describe.skip('signInWithApple', () => {
  it('returns identityToken on success', () => {
    // Wave 2: import signInWithApple from '../appleSignIn';
    // jest.mock('@invertase/react-native-apple-authentication');
    // expect await signInWithApple() to resolve with identityToken: 'mock-apple-id-token'
  });

  it('throws on Apple sheet cancellation with code 1001', () => {
    // Wave 2: override appleAuth.performRequest to reject with {code: '1001'}
    // expect await signInWithApple() to throw with code === appleAuth.Error.CANCELED
  });
});
