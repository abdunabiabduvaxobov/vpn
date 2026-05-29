// Phase 5 — Tests for appleSignIn service.
// Tracks: 05-VALIDATION.md 5-SVC-01, 5-SVC-02.

import {signInWithApple} from '../appleSignIn';
import {appleAuth} from '@invertase/react-native-apple-authentication';

jest.mock('@invertase/react-native-apple-authentication');

describe('signInWithApple', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  it('returns identityToken + authorizationCode + user + fullName + email on success', async () => {
    const result = await signInWithApple();
    expect(result.identityToken).toBe('mock-apple-id-token');
    expect(result.authorizationCode).toBe('mock-apple-auth-code');
    expect(result.user).toBe('mock-apple-sub');
    expect(result.fullName).toEqual({givenName: 'Test', familyName: 'User'});
    expect(result.email).toBe('test@example.com');
  });

  it('invokes appleAuth.performRequest with LOGIN + FULL_NAME + EMAIL scopes', async () => {
    await signInWithApple();
    expect(appleAuth.performRequest).toHaveBeenCalledWith({
      requestedOperation: appleAuth.Operation.LOGIN,
      requestedScopes: [appleAuth.Scope.FULL_NAME, appleAuth.Scope.EMAIL],
    });
  });

  it('re-throws cancellation (code 1001) from the native sheet', async () => {
    (appleAuth.performRequest as jest.Mock).mockRejectedValueOnce(
      Object.assign(new Error('User cancelled'), {code: '1001'}),
    );
    await expect(signInWithApple()).rejects.toMatchObject({code: '1001'});
    expect(appleAuth.Error.CANCELED).toBe('1001');
  });

  it('throws when identityToken is null', async () => {
    (appleAuth.performRequest as jest.Mock).mockResolvedValueOnce({
      identityToken: null,
      authorizationCode: null,
      user: 'sub',
      fullName: null,
      email: null,
    });
    await expect(signInWithApple()).rejects.toThrow(
      /did not return an identityToken/,
    );
  });
});
