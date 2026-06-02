// Phase 5 — Tests for the api axios client T-7 short-circuit.
// Tracks: 05-VALIDATION.md 5-SVC-09; review QA P0-4 / T-7.
//
// Drives synthetic 401s through the registered response-error interceptor to
// prove the _skipAuthRefresh flag + /auth/* URL pattern short-circuit, and
// that a normal /account 401 still triggers exactly one refresh.

const mockUpdateTokens = jest.fn();
const mockLogout = jest.fn().mockResolvedValue(undefined);
const mockInitialize = jest.fn();
let mockTokens: any = {access_token: 't', refresh_token: 'r', expires_in: 60};

jest.mock('../../stores/authStore', () => ({
  useAuthStore: {
    getState: () => ({
      tokens: mockTokens,
      updateTokens: mockUpdateTokens,
      logout: mockLogout,
      initialize: mockInitialize,
    }),
  },
}));

// HARD-04: the refresh interceptor now reads the device fingerprint to send
// device_id on /auth/refresh. Mock it so the test does not hit native code.
jest.mock('../deviceFingerprint', () => ({
  getDeviceFingerprint: jest.fn().mockResolvedValue({
    device_id: 'dev_1',
    device_secret: 'sec_1',
    platform: 'ios',
    model: 'iPhone',
  }),
}));

import axios from 'axios';
import api from '../api';

// The interceptor's refresh call uses the top-level axios.post; spy on it.
const refreshSpy = jest.spyOn(axios, 'post');

/**
 * Pull the rejection handler that api.ts registered via
 * api.interceptors.response.use(onFulfilled, onRejected).
 */
function getResponseErrorHandler(): (error: any) => Promise<any> {
  const handlers: any[] = (api.interceptors.response as any).handlers;
  const entry = handlers.find(h => h && typeof h.rejected === 'function');
  return entry.rejected;
}

describe('axios interceptor T-7 short-circuit', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockTokens = {access_token: 't', refresh_token: 'r', expires_in: 60};
  });

  it('exports the api singleton', () => {
    expect(api).toBeDefined();
    expect(typeof api.get).toBe('function');
  });

  it('AxiosRequestConfig type now permits _skipAuthRefresh boolean', () => {
    const cfg: import('axios').AxiosRequestConfig = {_skipAuthRefresh: true};
    expect(cfg._skipAuthRefresh).toBe(true);
  });

  // QA P0-4 — _skipAuthRefresh flag short-circuits refresh.
  it('does NOT call refresh when _skipAuthRefresh=true', async () => {
    const onRejected = getResponseErrorHandler();
    const error = {
      response: {status: 401},
      config: {url: '/account', _skipAuthRefresh: true, headers: {}},
    };
    await expect(onRejected(error)).rejects.toBe(error);
    expect(refreshSpy).not.toHaveBeenCalled();
  });

  // QA P0-4 — /auth/* URL pattern short-circuits refresh.
  it('does NOT call refresh for an /auth/google 401', async () => {
    const onRejected = getResponseErrorHandler();
    const error = {
      response: {status: 401},
      config: {url: '/auth/google', headers: {}},
    };
    await expect(onRejected(error)).rejects.toBe(error);
    expect(refreshSpy).not.toHaveBeenCalled();
  });

  // QA P0-4 — a normal /account 401 with a valid refresh token refreshes once.
  it('runs refresh exactly once for an /account 401 with a refresh token', async () => {
    refreshSpy.mockResolvedValueOnce({
      data: {data: {access_token: 'NEW_AT', refresh_token: 'NEW_RT', expires_in: 300}},
    });
    const onRejected = getResponseErrorHandler();
    // After a successful refresh the interceptor re-issues the original
    // request via api(originalRequest). Supply a custom adapter on the config
    // so that retry resolves immediately without hitting the network.
    const retryAdapter = jest.fn().mockResolvedValue({
      data: {ok: true},
      status: 200,
      statusText: 'OK',
      headers: {},
      config: {},
    });
    const error: any = {
      response: {status: 401},
      config: {url: '/account', headers: {} as Record<string, string>, adapter: retryAdapter},
    };
    const result = await onRejected(error);
    expect(refreshSpy).toHaveBeenCalledTimes(1);
    const [refreshUrl, refreshBody] = refreshSpy.mock.calls[0];
    expect(String(refreshUrl)).toContain('/auth/refresh');
    // HARD-04 client side: refresh now carries device_id for backend binding.
    expect(refreshBody).toEqual({refresh_token: 'r', device_id: 'dev_1'});
    expect(mockUpdateTokens).toHaveBeenCalledWith({
      access_token: 'NEW_AT',
      refresh_token: 'NEW_RT',
      expires_in: 300,
    });
    // The retry carried the new bearer token.
    expect(error.config.headers.Authorization).toBe('Bearer NEW_AT');
    expect(retryAdapter).toHaveBeenCalledTimes(1);
    expect(result).toMatchObject({data: {ok: true}});
  });
});
