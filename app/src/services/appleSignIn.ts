// Phase 5 — Apple Sign-In service wrapper.
// Wraps @invertase/react-native-apple-authentication. Mobile forwards
// the raw identityToken to backend POST /auth/apple — backend Phase 2
// D-19 validates JWKs + iss + aud + exp. No mobile-side crypto.

import {
  appleAuth,
  AppleRequestResponse,
} from '@invertase/react-native-apple-authentication';

export interface AppleSignInResult {
  identityToken: string;
  authorizationCode: string | null;
  user: string;
  // givenName/familyName are `string | null` to match the real
  // @invertase AppleRequestResponseFullName shape (the native sheet
  // returns null — not undefined — when a name component is absent).
  fullName: {givenName?: string | null; familyName?: string | null} | null;
  email: string | null;
}

/**
 * Invokes the native Apple sign-in sheet. Throws on cancellation
 * (error.code === appleAuth.Error.CANCELED) so callers can branch on it.
 * Throws a generic Error if Apple returns a result without identityToken.
 *
 * IMPORTANT: Apple returns fullName + email ONLY on the FIRST sign-in
 * attempt per Apple ID. Subsequent sign-ins return null for these fields.
 * Backend Phase 2 D-19 caches the first-attempt values; mobile just
 * forwards whatever Apple returns.
 */
export async function signInWithApple(): Promise<AppleSignInResult> {
  const response: AppleRequestResponse = await appleAuth.performRequest({
    requestedOperation: appleAuth.Operation.LOGIN,
    requestedScopes: [appleAuth.Scope.FULL_NAME, appleAuth.Scope.EMAIL],
  });

  if (!response.identityToken) {
    throw new Error('Apple sign-in did not return an identityToken');
  }

  return {
    identityToken: response.identityToken,
    authorizationCode: response.authorizationCode,
    user: response.user,
    fullName: response.fullName,
    email: response.email,
  };
}

export {appleAuth};
