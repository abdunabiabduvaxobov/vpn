// Phase 5 — Tests for googleSignIn service.
// Tracks: 05-VALIDATION.md 5-SVC-03, 5-SVC-04.

import {configureGoogleSignIn, signInWithGoogle} from '../googleSignIn';
import {GoogleSignin} from '@react-native-google-signin/google-signin';

jest.mock('@react-native-google-signin/google-signin');

describe('configureGoogleSignIn', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('calls GoogleSignin.configure exactly once with webClientId + iosClientId + scopes', () => {
    configureGoogleSignIn();
    expect(GoogleSignin.configure).toHaveBeenCalledTimes(1);
    const call = (GoogleSignin.configure as jest.Mock).mock.calls[0][0];
    expect(call).toHaveProperty('webClientId');
    expect(call.webClientId).toMatch(/\.apps\.googleusercontent\.com$|^.+$/);
    expect(call).toHaveProperty('iosClientId');
    expect(call.offlineAccess).toBe(false);
    expect(call.scopes).toEqual(['email', 'profile']);
  });
});

describe('signInWithGoogle', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('returns idToken from userInfo.data.idToken (v16 shape)', async () => {
    const result = await signInWithGoogle();
    expect(result.idToken).toBe('mock-google-id-token');
  });

  it('calls hasPlayServices before signIn', async () => {
    await signInWithGoogle();
    expect(GoogleSignin.hasPlayServices).toHaveBeenCalledWith({
      showPlayServicesUpdateDialog: true,
    });
    expect(GoogleSignin.signIn).toHaveBeenCalledTimes(1);
  });

  it('throws when idToken is null', async () => {
    (GoogleSignin.signIn as jest.Mock).mockResolvedValueOnce({
      type: 'success',
      data: {idToken: null, user: {email: 'x@x.com'}},
    });
    await expect(signInWithGoogle()).rejects.toThrow(
      /did not return an idToken/,
    );
  });
});
