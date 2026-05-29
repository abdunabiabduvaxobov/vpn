// Phase 5 — Google Sign-In service wrapper.
// Wraps @react-native-google-signin/google-signin v16. Reads idToken from
// userInfo.data.idToken (v16 wrapped shape — pre-v13 was userInfo.idToken).

import {
  GoogleSignin,
  statusCodes,
} from '@react-native-google-signin/google-signin';

// Google OAuth client IDs. Values committed from operator-furnished
// Apple Developer / Google Cloud Console registration (T-6: client IDs
// are PUBLIC values scoped by SHA-1 + Bundle ID — not secrets).
//
// Operator decision (2026-05-29, DEF-05-CREDS): Phase 5 ships with the
// PLACEHOLDER_* sentinels wired into native config + here. Real Web/iOS
// client IDs are filled at store-upload time. The Web client ID is the
// backend JWT AUDIENCE and MUST match the landing site's GOOGLE_CLIENT_ID_WEB
// + android/.../strings.xml server_client_id. Pre-upload check:
//   grep -rn "PLACEHOLDER_" app/ios app/android app/src
const WEB_CLIENT_ID = 'PLACEHOLDER_WEB.apps.googleusercontent.com';
const IOS_CLIENT_ID = 'PLACEHOLDER_IOS.apps.googleusercontent.com';

export interface GoogleSignInResult {
  idToken: string;
}

/**
 * Configures the Google Sign-In native SDK at app boot. MUST be called
 * before any signInWithGoogle invocation. Wired in App.tsx (Wave 3).
 *
 * webClientId is the AUDIENCE that appears in the resulting idToken.aud
 * claim and is what backend Phase 2 D-21 validates against.
 */
export function configureGoogleSignIn(): void {
  GoogleSignin.configure({
    webClientId: WEB_CLIENT_ID,
    iosClientId: IOS_CLIENT_ID,
    offlineAccess: false,
    scopes: ['email', 'profile'],
  });
}

/**
 * Invokes the native Google sign-in sheet. v16 wraps the response in
 * `data` — defensively reads both v16 and pre-v13 shapes.
 */
export async function signInWithGoogle(): Promise<GoogleSignInResult> {
  await GoogleSignin.hasPlayServices({showPlayServicesUpdateDialog: true});
  const userInfo = await GoogleSignin.signIn();
  // v16: userInfo.data.idToken; pre-v13: userInfo.idToken. Defensive read.
  const idToken =
    (userInfo as any).data?.idToken ?? (userInfo as any).idToken;
  if (!idToken) {
    throw new Error('Google sign-in did not return an idToken');
  }
  return {idToken};
}

export {statusCodes};
