// Manual Jest mock for @react-native-google-signin/google-signin v16.
// Mirrors v16 response shape: userInfo.data.idToken (NOT userInfo.idToken pre-v13).

export const GoogleSignin = {
  configure: jest.fn(),
  hasPlayServices: jest.fn().mockResolvedValue(true),
  signIn: jest.fn().mockResolvedValue({
    type: 'success',
    data: {
      idToken: 'mock-google-id-token',
      serverAuthCode: null,
      scopes: ['email', 'profile'],
      user: {
        id: 'mock-google-sub',
        email: 'test@example.com',
        name: 'Test User',
        familyName: 'User',
        givenName: 'Test',
        photo: null,
      },
    },
  }),
  signOut: jest.fn().mockResolvedValue(undefined),
  revokeAccess: jest.fn().mockResolvedValue(undefined),
  isSignedIn: jest.fn().mockResolvedValue(false),
  getCurrentUser: jest.fn().mockResolvedValue(null),
};

export const statusCodes = {
  SIGN_IN_CANCELLED: 'SIGN_IN_CANCELLED',
  IN_PROGRESS: 'IN_PROGRESS',
  PLAY_SERVICES_NOT_AVAILABLE: 'PLAY_SERVICES_NOT_AVAILABLE',
};

export default {GoogleSignin, statusCodes};
