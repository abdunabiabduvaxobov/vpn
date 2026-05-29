// app/src/services/__tests__/googleSignIn.test.ts
// Phase 5 Wave 0 scaffold — Wave 2 fills in implementations.
// Tracks: 05-VALIDATION.md task 5-SVC-03, 5-SVC-04.

describe.skip('configureGoogleSignIn', () => {
  it('calls GoogleSignin.configure once at app boot with webClientId', () => {
    // Wave 2: import configureGoogleSignIn from '../googleSignIn';
    // jest.mock('@react-native-google-signin/google-signin');
    // expect GoogleSignin.configure to have been called once with {webClientId: ...}
  });
});

describe.skip('signInWithGoogle', () => {
  it('returns idToken from userInfo.data.idToken (v16 shape)', () => {
    // Wave 2: import signInWithGoogle from '../googleSignIn';
    // expect await signInWithGoogle() to resolve with idToken: 'mock-google-id-token'
    // (v16 shape: response.data.idToken, NOT response.idToken)
  });
});
