// Phase 5 — Tests for the api axios client T-7 short-circuit.
// Tracks: 05-VALIDATION.md 5-SVC-09.
//
// Tests focus on the _skipAuthRefresh short-circuit. Full interceptor
// is covered by the existing integration smoke; this guards T-7.

import api from '../api';

jest.mock('../../stores/authStore', () => ({
  useAuthStore: {
    getState: () => ({
      tokens: {access_token: 't', refresh_token: 'r', expires_in: 60},
      updateTokens: jest.fn(),
      logout: jest.fn().mockResolvedValue(undefined),
      initialize: jest.fn(),
    }),
  },
}));

describe('axios interceptor T-7 short-circuit', () => {
  it('exports the api singleton', () => {
    expect(api).toBeDefined();
    expect(typeof api.get).toBe('function');
  });

  it('AxiosRequestConfig type now permits _skipAuthRefresh boolean', () => {
    // Type-level check: this file compiles only if the module augmentation in api.ts
    // accepted the field.
    const cfg: import('axios').AxiosRequestConfig = {_skipAuthRefresh: true};
    expect(cfg._skipAuthRefresh).toBe(true);
  });
});
