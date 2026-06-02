// HARD-16 / SC#5 (D-11) — auth tokens at rest in OS secure storage.
//
// Before HARD-16 the access/refresh pair was written to
// `AsyncStorage.setItem('auth-tokens', ...)` — a plaintext file in the app
// sandbox, readable by anyone who can reach the container (the S10 audit
// finding). This wrapper moves the pair into the platform secure store via
// `react-native-keychain` (D-11): iOS **Keychain** (SecItem) and Android
// **EncryptedSharedPreferences** (Keystore-backed AES). MMKV-with-encryption
// was explicitly rejected because it stores in an app-sandbox file, not the
// OS Keychain, and so would not satisfy the Xcode-inspectable SC#5 check.
//
// It deliberately mirrors the get/set/remove shape AsyncStorage offered so the
// 7 call sites in authStore.ts swap one-for-one.
//
// The `SERVICE` string is the load-bearing identifier SC#5 inspects: on iOS the
// Keychain entry is filed under this service; on Android it is the encrypted
// prefs file name. Keep it stable — changing it strands the old entry and forces
// a re-login.

import * as Keychain from 'react-native-keychain';
import type {AuthTokens} from '../types/api';

// Stable Keychain service key (SC#5 inspects this). A fixed string rather than
// the default (bundle id) so the entry is explicit and greppable in the manual
// verification procedure (docs/manual-verification/08-keychain-asyncstorage.md).
const SERVICE = 'risevpn.auth';

// Keychain stores a username/password pair; we only need the password slot for
// the JSON token blob, but a non-empty username is required for the entry.
const ACCOUNT = 'risevpn-auth';

/**
 * Persist the token pair into the OS secure store (iOS Keychain / Android
 * EncryptedSharedPreferences). The JSON pair is stored as the "password" under
 * a single, stable `service`.
 */
export async function setTokens(tokens: AuthTokens): Promise<void> {
  await Keychain.setGenericPassword(ACCOUNT, JSON.stringify(tokens), {
    service: SERVICE,
  });
}

/**
 * Read the token pair back from the secure store. Returns null when there is no
 * entry (fresh install / after logout) or when the stored value is unparseable.
 */
export async function getTokens(): Promise<AuthTokens | null> {
  try {
    const credentials = await Keychain.getGenericPassword({service: SERVICE});
    if (!credentials || !credentials.password) {
      return null;
    }
    return JSON.parse(credentials.password) as AuthTokens;
  } catch {
    // Corrupt/unreadable entry — treat as "no tokens" so the app re-auths
    // rather than crashing on a malformed Keychain blob.
    return null;
  }
}

/**
 * Remove the token pair from the secure store (logout / re-login cutover).
 */
export async function clearTokens(): Promise<void> {
  await Keychain.resetGenericPassword({service: SERVICE});
}

export const secureTokenStore = {
  setTokens,
  getTokens,
  clearTokens,
  SERVICE,
};

export default secureTokenStore;
