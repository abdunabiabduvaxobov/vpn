// Phase 5 — Tests for authStore SSO + Activating-Pro extensions.
// Tracks: 05-VALIDATION.md 5-SVC-01, 5-SVC-03, 5-SVC-05.

import {useAuthStore} from '../authStore';

jest.mock('@react-native-async-storage/async-storage', () => ({
  setItem: jest.fn().mockResolvedValue(undefined),
  getItem: jest.fn().mockResolvedValue(null),
  removeItem: jest.fn().mockResolvedValue(undefined),
}));

// HARD-16: tokens now persist via react-native-keychain (secureTokenStore).
// Mock the native module so the store's get/set/clear resolve in jsdom.
jest.mock('react-native-keychain', () => ({
  setGenericPassword: jest.fn().mockResolvedValue(true),
  getGenericPassword: jest.fn().mockResolvedValue(false),
  resetGenericPassword: jest.fn().mockResolvedValue(true),
}));

jest.mock('../../services/api', () => ({
  __esModule: true,
  default: {
    post: jest.fn(),
    get: jest.fn(),
  },
}));

jest.mock('../../services/appleSignIn', () => ({
  signInWithApple: jest.fn().mockResolvedValue({
    identityToken: 'apple-id-token',
    authorizationCode: 'apple-auth-code',
    user: 'apple-sub',
    fullName: {givenName: 'Test', familyName: 'User'},
    email: 'test@example.com',
  }),
  appleAuth: {Error: {CANCELED: '1001'}},
}));

jest.mock('../../services/googleSignIn', () => ({
  signInWithGoogle: jest.fn().mockResolvedValue({idToken: 'google-id-token'}),
  statusCodes: {SIGN_IN_CANCELLED: 'SIGN_IN_CANCELLED'},
}));

jest.mock('../../services/deviceFingerprint', () => ({
  getDeviceFingerprint: jest.fn().mockResolvedValue({
    device_id: 'dev_1',
    device_secret: 'sec_1',
    platform: 'ios',
  }),
}));

import api from '../../services/api';

function resetStore() {
  useAuthStore.setState({
    user: null,
    tokens: null,
    isAuthenticated: false,
    isLoading: false,
    pendingInvoiceId: null,
    isActivatingPro: false,
  });
}

describe('signInWithApple action', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    resetStore();
    (api.post as jest.Mock).mockResolvedValue({
      data: {data: {access_token: 'AT', refresh_token: 'RT', expires_in: 300}},
    });
    (api.get as jest.Mock).mockResolvedValue({
      data: {data: {id: 'u1', full_name: 'T U', subscription_tier: 'free', subscription_expires_at: null, created_at: '', auth_provider: 'apple'}},
    });
  });

  it('POSTs identity_token + device fingerprint + _skipAuthRefresh to /auth/apple', async () => {
    await useAuthStore.getState().signInWithApple();
    expect(api.post).toHaveBeenCalledWith(
      '/auth/apple',
      expect.objectContaining({
        identity_token: 'apple-id-token',
        authorization_code: 'apple-auth-code',
        device_id: 'dev_1',
        device_secret: 'sec_1',
        platform: 'ios',
        full_name: 'Test User',
        email: 'test@example.com',
      }),
      expect.objectContaining({_skipAuthRefresh: true}),
    );
  });

  it('sets tokens + isAuthenticated and calls fetchAccount on success', async () => {
    await useAuthStore.getState().signInWithApple();
    const state = useAuthStore.getState();
    expect(state.tokens?.access_token).toBe('AT');
    expect(state.isAuthenticated).toBe(true);
    expect(state.isLoading).toBe(false);
    expect(api.get).toHaveBeenCalledWith('/account');
  });

  it('rethrows cancellation (code 1001) so LoginScreen can return silently', async () => {
    const {signInWithApple: applePerform} = require('../../services/appleSignIn');
    applePerform.mockRejectedValueOnce(Object.assign(new Error('cancelled'), {code: '1001'}));
    await expect(useAuthStore.getState().signInWithApple()).rejects.toMatchObject({code: '1001'});
    expect(useAuthStore.getState().isLoading).toBe(false);
  });

  // QA P0-1 / SC2 mechanism — D-06 in-place promotion: the guest token MUST
  // still be in the store (so the request interceptor attaches it) at the
  // moment the /auth/apple POST fires. It must NOT be cleared beforehand.
  it('does NOT clear the guest token before the /auth/apple POST', async () => {
    const AsyncStorage = require('@react-native-async-storage/async-storage');
    // Seed a guest session.
    const guestTokens = {access_token: 'GUEST_AT', refresh_token: 'GUEST_RT', expires_in: 300};
    useAuthStore.setState({tokens: guestTokens, isAuthenticated: true});

    let tokensAtPostTime: any = 'unset';
    let removeCalledBeforePost = false;
    (api.post as jest.Mock).mockImplementation(async () => {
      tokensAtPostTime = useAuthStore.getState().tokens;
      removeCalledBeforePost = (AsyncStorage.removeItem as jest.Mock).mock.calls.length > 0;
      return {data: {data: {access_token: 'AT', refresh_token: 'RT', expires_in: 300}}};
    });

    await useAuthStore.getState().signInWithApple();

    // The guest token was present (un-cleared) when /auth/apple was POSTed.
    expect(tokensAtPostTime).toEqual(guestTokens);
    expect(removeCalledBeforePost).toBe(false);
  });
});

describe('signInWithGoogle action', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    resetStore();
    (api.post as jest.Mock).mockResolvedValue({
      data: {data: {access_token: 'AT', refresh_token: 'RT', expires_in: 300}},
    });
    (api.get as jest.Mock).mockResolvedValue({
      data: {data: {id: 'u1', full_name: 'T U', subscription_tier: 'free', subscription_expires_at: null, created_at: '', auth_provider: 'google'}},
    });
  });

  it('POSTs id_token + device fingerprint + _skipAuthRefresh to /auth/google', async () => {
    await useAuthStore.getState().signInWithGoogle();
    expect(api.post).toHaveBeenCalledWith(
      '/auth/google',
      expect.objectContaining({
        id_token: 'google-id-token',
        device_id: 'dev_1',
        device_secret: 'sec_1',
        platform: 'ios',
      }),
      expect.objectContaining({_skipAuthRefresh: true}),
    );
  });
});

describe('HARD-16 secure-storage persistence', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    resetStore();
  });

  // SC#5: signing in writes the token pair to the Keychain (secureTokenStore),
  // NOT to AsyncStorage.
  it('signInWithApple persists tokens to Keychain, never to AsyncStorage', async () => {
    const Keychain = require('react-native-keychain');
    const AsyncStorage = require('@react-native-async-storage/async-storage');
    (api.post as jest.Mock).mockResolvedValue({
      data: {data: {access_token: 'AT', refresh_token: 'RT', expires_in: 300}},
    });
    (api.get as jest.Mock).mockResolvedValue({data: {data: {id: 'u1'}}});

    await useAuthStore.getState().signInWithApple();

    expect(Keychain.setGenericPassword).toHaveBeenCalledWith(
      'risevpn-auth',
      JSON.stringify({access_token: 'AT', refresh_token: 'RT', expires_in: 300}),
      {service: 'risevpn.auth'},
    );
    // No token ever goes to AsyncStorage.
    expect(AsyncStorage.setItem).not.toHaveBeenCalledWith(
      'auth-tokens',
      expect.anything(),
    );
  });

  // D-12 one-time wipe: initialize() purges the legacy AsyncStorage 'auth-tokens'
  // key so an upgrade-in-place user ends with no token there (SC#5 literal lock),
  // then mints a guest whose tokens land in the Keychain.
  it('initialize wipes legacy AsyncStorage token and writes guest tokens to Keychain', async () => {
    const Keychain = require('react-native-keychain');
    const AsyncStorage = require('@react-native-async-storage/async-storage');
    (Keychain.getGenericPassword as jest.Mock).mockResolvedValueOnce(false);
    (api.post as jest.Mock).mockResolvedValue({
      data: {data: {access_token: 'GAT', refresh_token: 'GRT', expires_in: 300}},
    });

    await useAuthStore.getState().initialize();

    expect(AsyncStorage.removeItem).toHaveBeenCalledWith('auth-tokens');
    expect(AsyncStorage.setItem).not.toHaveBeenCalledWith(
      'auth-tokens',
      expect.anything(),
    );
    expect(Keychain.setGenericPassword).toHaveBeenCalledWith(
      'risevpn-auth',
      JSON.stringify({access_token: 'GAT', refresh_token: 'GRT', expires_in: 300}),
      {service: 'risevpn.auth'},
    );
    expect(useAuthStore.getState().tokens?.access_token).toBe('GAT');
  });

  // logout clears the Keychain entry (not AsyncStorage).
  it('logout resets the Keychain entry', async () => {
    const Keychain = require('react-native-keychain');
    useAuthStore.setState({
      tokens: {access_token: 'AT', refresh_token: 'RT', expires_in: 300},
      isAuthenticated: true,
    });
    await useAuthStore.getState().logout();
    expect(Keychain.resetGenericPassword).toHaveBeenCalledWith({
      service: 'risevpn.auth',
    });
    expect(useAuthStore.getState().isAuthenticated).toBe(false);
  });
});

describe('startActivatingPro / stopActivatingPro', () => {
  beforeEach(() => resetStore());

  it('startActivatingPro sets pendingInvoiceId + isActivatingPro=true', () => {
    useAuthStore.getState().startActivatingPro('inv_X');
    const s = useAuthStore.getState();
    expect(s.pendingInvoiceId).toBe('inv_X');
    expect(s.isActivatingPro).toBe(true);
  });

  it('stopActivatingPro clears both fields', () => {
    useAuthStore.getState().startActivatingPro('inv_X');
    useAuthStore.getState().stopActivatingPro();
    const s = useAuthStore.getState();
    expect(s.pendingInvoiceId).toBeNull();
    expect(s.isActivatingPro).toBe(false);
  });

  // FIX 5 (L-3) — a duplicate delivery for the SAME invoice while already
  // activating is a no-op (prevents a second overlapping polling loop).
  it('startActivatingPro is a no-op for a duplicate of the active invoice', () => {
    useAuthStore.getState().startActivatingPro('inv_X');
    let setCount = 0;
    const unsub = useAuthStore.subscribe(() => {
      setCount += 1;
    });
    useAuthStore.getState().startActivatingPro('inv_X'); // duplicate
    unsub();
    expect(setCount).toBe(0); // no state write → no modal restart
    const s = useAuthStore.getState();
    expect(s.pendingInvoiceId).toBe('inv_X');
    expect(s.isActivatingPro).toBe(true);
  });

  // FIX 5 (L-3) — a DIFFERENT invoice still re-arms the modal (not deduped).
  it('startActivatingPro re-arms for a different invoice id', () => {
    useAuthStore.getState().startActivatingPro('inv_X');
    useAuthStore.getState().startActivatingPro('inv_Y');
    const s = useAuthStore.getState();
    expect(s.pendingInvoiceId).toBe('inv_Y');
    expect(s.isActivatingPro).toBe(true);
  });
});
