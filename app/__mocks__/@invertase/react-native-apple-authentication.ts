// Manual Jest mock for @invertase/react-native-apple-authentication.
// Mirrors real library v2.5.1 API surface used by app/src/services/appleSignIn.ts
// (and consumed in unit tests). DO NOT import from production code paths.

export const appleAuth = {
  performRequest: jest.fn().mockResolvedValue({
    identityToken: 'mock-apple-id-token',
    authorizationCode: 'mock-apple-auth-code',
    user: 'mock-apple-sub',
    fullName: {givenName: 'Test', familyName: 'User'},
    email: 'test@example.com',
    nonce: 'mock-nonce',
    realUserStatus: 1,
    state: null,
  }),
  Operation: {
    IMPLICIT: 0,
    LOGIN: 1,
    REFRESH: 2,
    LOGOUT: 3,
  },
  Scope: {
    FULL_NAME: 0,
    EMAIL: 1,
  },
  Error: {
    UNKNOWN: '1000',
    CANCELED: '1001',
    INVALID_RESPONSE: '1002',
    NOT_HANDLED: '1003',
    FAILED: '1004',
  },
};

export type AppleRequestResponse = {
  identityToken: string | null;
  authorizationCode: string | null;
  user: string;
  fullName: {givenName?: string; familyName?: string} | null;
  email: string | null;
  nonce: string;
  realUserStatus: number;
  state: string | null;
};

export default {appleAuth};
