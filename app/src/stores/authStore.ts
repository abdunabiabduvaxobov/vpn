import {create} from 'zustand';
import AsyncStorage from '@react-native-async-storage/async-storage';
import api from '../services/api';
import {getDeviceFingerprint} from '../services/deviceFingerprint';
import {signInWithApple as performAppleSignIn} from '../services/appleSignIn';
import {signInWithGoogle as performGoogleSignIn} from '../services/googleSignIn';
import {secureTokenStore} from '../services/secureTokenStore';
import type {User, AuthTokens, ApiResponse} from '../types/api';

// HARD-16 / SC#5 (D-12): the auth token pair used to live here in AsyncStorage
// (plaintext app-sandbox file). It now persists via secureTokenStore (iOS
// Keychain / Android EncryptedSharedPreferences). This key is retained ONLY so
// the one-time boot cleanup below can purge any token an upgrade-in-place user
// still carries in AsyncStorage. Tokens are never WRITTEN here again.
const LEGACY_TOKENS_KEY = 'auth-tokens';

// D-12 one-time wipe guard — flips true after the first boot cleanup so the
// removeItem only runs once per process (it is idempotent regardless).
let legacyTokensWiped = false;

interface AuthState {
  user: User | null;
  tokens: AuthTokens | null;
  isAuthenticated: boolean;
  isLoading: boolean;

  // Phase 5: Activating-Pro modal state (D-CD: extend authStore rather
  // than a separate paymentReturnStore — two fields, two actions).
  pendingInvoiceId: string | null;
  isActivatingPro: boolean;

  // Actions
  initialize: () => Promise<void>;
  fetchAccount: () => Promise<void>;
  updateProfile: (name: string) => Promise<void>;
  updateTokens: (tokens: AuthTokens) => void;
  linkWithCode: (code: string) => Promise<void>;
  logout: () => Promise<void>;
  setLoading: (loading: boolean) => void;

  // Phase 5: SSO actions.
  signInWithApple: () => Promise<void>;
  signInWithGoogle: () => Promise<void>;

  // Phase 5: deep-link → polling-modal bridge.
  startActivatingPro: (invoiceId: string) => void;
  stopActivatingPro: () => void;
}

// Concurrency guard for initialize() — StrictMode double-mounts and rapid
// re-renders can otherwise call /auth/guest twice and orphan a server
// session. The first call sets this true and subsequent calls early-return.
let initializing = false;

export const useAuthStore = create<AuthState>((set, get) => ({
  user: null,
  tokens: null,
  isAuthenticated: false,
  isLoading: false,
  // Phase 5: Activating-Pro modal state.
  pendingInvoiceId: null,
  isActivatingPro: false,

  // Called once on app startup — restores tokens from storage or auto-creates a guest session.
  // The guest call sends the device fingerprint (id + secret) so the server
  // can return the existing user_id for this device across reinstalls and
  // across linked accounts via share codes.
  //
  // Guarded against concurrent invocation by the module-level `initializing`
  // flag — StrictMode double-mounts and resume-from-background can otherwise
  // call /auth/guest twice and orphan a server session.
  // Returns a Promise that resolves once guest auth has settled (tokens set
  // or failed) so callers that need to await completion (e.g. LoginScreen's
  // guest CTA, WR-04) can navigate only after the session is established. The
  // App-boot call site invokes this fire-and-forget; ignoring the returned
  // promise is harmless.
  initialize: async (): Promise<void> => {
    if (initializing) return;
    initializing = true;
    set({isLoading: true});
    try {
      // D-12 one-time wipe: purge any token an upgrade-in-place user still
      // carries in the old AsyncStorage 'auth-tokens' key. The backend
      // clean-break cutover (08-04) already cleared sessions server-side, so a
      // carried-forward token is dead anyway — we deliberately do NOT migrate
      // it into the Keychain (research §6.4: migrate-then-carry-forward gains
      // nothing because the token is invalid). This leaves AsyncStorage with no
      // auth token, satisfying the SC#5 literal lock, and the app routes to
      // login / re-mints a guest below, writing fresh tokens straight to
      // Keychain.
      if (!legacyTokensWiped) {
        legacyTokensWiped = true;
        try {
          await AsyncStorage.removeItem(LEGACY_TOKENS_KEY);
        } catch {
          // Cleanup is best-effort; a failure here does not block boot.
        }
      }

      // Tokens now live in the OS secure store (Keychain / EncryptedSharedPrefs).
      const tokens = await secureTokenStore.getTokens();
      if (tokens) {
        set({tokens, isAuthenticated: true, isLoading: false});
        return;
      }

      const fingerprint = await getDeviceFingerprint();
      const {data} = await api.post<{data: AuthTokens}>('/auth/guest', fingerprint);
      const guestTokens = data.data;
      await secureTokenStore.setTokens(guestTokens);
      set({tokens: guestTokens, isAuthenticated: true, isLoading: false});
    } catch {
      // Guest login failed (e.g. no network). The app stays unauthenticated
      // and will retry the next time initialize() is called.
      set({isLoading: false});
    } finally {
      initializing = false;
    }
  },

  // Redeem a 6-digit share code given by a friend (the plan owner). On
  // success, replaces the locally-stored tokens with ones for the owner's
  // user_id, so subsequent API calls behave as if this device had logged
  // into the owner's account directly.
  linkWithCode: async (code: string) => {
    set({isLoading: true});
    try {
      const fingerprint = await getDeviceFingerprint();
      const {data} = await api.post<{data: AuthTokens}>('/auth/link', {
        code,
        ...fingerprint,
      });
      const tokens = data.data;
      await secureTokenStore.setTokens(tokens);
      set({tokens, isAuthenticated: true, isLoading: false, user: null});
      // Re-fetch the account so the UI immediately reflects the new tier.
      await get().fetchAccount();
    } catch (error) {
      set({isLoading: false});
      throw error;
    }
  },

  // Phase 5 — Apple Sign-In. D-06: guest JWT MUST be sent in the request
  // Authorization header so backend takes the in-place promotion path.
  // The existing axios request interceptor reads tokens from state and
  // attaches Authorization automatically — we do NOT clear tokens before
  // invoking the Apple sheet. The /auth/apple call is flagged
  // {_skipAuthRefresh: true} so a 401 there cannot recurse the refresh
  // interceptor and re-mint a guest (T-7).
  signInWithApple: async () => {
    set({isLoading: true});
    try {
      const appleResult = await performAppleSignIn();
      const fingerprint = await getDeviceFingerprint();
      const fullName = appleResult.fullName
        ? `${appleResult.fullName.givenName ?? ''} ${appleResult.fullName.familyName ?? ''}`.trim()
        : undefined;
      const {data} = await api.post<ApiResponse<AuthTokens>>(
        '/auth/apple',
        {
          identity_token: appleResult.identityToken,
          authorization_code: appleResult.authorizationCode,
          device_id: fingerprint.device_id,
          device_secret: fingerprint.device_secret,
          platform: fingerprint.platform,
          full_name: fullName || undefined,
          email: appleResult.email ?? undefined,
        },
        {_skipAuthRefresh: true},
      );
      const tokens = data.data;
      await secureTokenStore.setTokens(tokens);
      set({tokens, isAuthenticated: true, isLoading: false, user: null});
      await get().fetchAccount();
    } catch (error) {
      set({isLoading: false});
      throw error; // re-throw so LoginScreen can detect cancellation
    }
  },

  // Phase 5 — Google Sign-In. Same D-06 in-place promotion path + T-7 flag.
  signInWithGoogle: async () => {
    set({isLoading: true});
    try {
      const googleResult = await performGoogleSignIn();
      const fingerprint = await getDeviceFingerprint();
      const {data} = await api.post<ApiResponse<AuthTokens>>(
        '/auth/google',
        {
          id_token: googleResult.idToken,
          device_id: fingerprint.device_id,
          device_secret: fingerprint.device_secret,
          platform: fingerprint.platform,
        },
        {_skipAuthRefresh: true},
      );
      const tokens = data.data;
      await secureTokenStore.setTokens(tokens);
      set({tokens, isAuthenticated: true, isLoading: false, user: null});
      await get().fetchAccount();
    } catch (error) {
      set({isLoading: false});
      throw error;
    }
  },

  // Phase 5 — Activating-Pro modal bridge. Called by deepLink.ts on
  // vpnapp://payment/success?invoiceId=X receive.
  //
  // L-3: the OS can deliver the same deep link twice (cold-start
  // getInitialURL + a warm 'url' event for the same launch). De-dup so a
  // duplicate delivery for the SAME invoice does not restart the modal and
  // spin up a second overlapping polling loop.
  startActivatingPro: (invoiceId: string) => {
    const {isActivatingPro, pendingInvoiceId} = get();
    if (isActivatingPro && pendingInvoiceId === invoiceId) return;
    set({pendingInvoiceId: invoiceId, isActivatingPro: true});
  },

  stopActivatingPro: () => {
    set({pendingInvoiceId: null, isActivatingPro: false});
  },

  fetchAccount: async () => {
    try {
      const {data} = await api.get<{data: User}>('/account');
      set({user: data.data});
    } catch {
      // Silently fail — user info is not critical
    }
  },

  updateProfile: async (name: string) => {
    const {data} = await api.patch<{data: {id: string; full_name: string}}>(
      '/account',
      {name},
    );
    set(state => ({
      user: state.user ? {...state.user, full_name: data.data.full_name} : null,
    }));
  },

  updateTokens: (tokens: AuthTokens) => {
    // Fire-and-forget persist into the secure store (Keychain / EncryptedSharedPrefs).
    void secureTokenStore.setTokens(tokens);
    set({tokens});
  },

  // Awaits the secure-store removal so a racing initialize() (e.g. caused
  // by app resume) cannot read the now-stale tokens before they are cleared.
  logout: async () => {
    try {
      await secureTokenStore.clearTokens();
    } catch {
      // Even if persistence fails, drop the in-memory tokens so the UI
      // immediately reflects the logged-out state.
    }
    set({user: null, tokens: null, isAuthenticated: false});
  },

  setLoading: (loading: boolean) => set({isLoading: loading}),
}));
