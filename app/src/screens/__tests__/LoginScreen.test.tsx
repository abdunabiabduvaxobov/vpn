import React from 'react';
import TestRenderer, {act} from 'react-test-renderer';
import {Platform} from 'react-native';
import {LoginScreen} from '../LoginScreen';

jest.mock('@react-navigation/native', () => ({
  useNavigation: () => ({reset: jest.fn()}),
}));

jest.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => {
      const map: Record<string, string> = {
        'login.title': 'Sign in to RiseVPN',
        'login.subtitle': 'Sign in so your Pro plan follows you to every device.',
        'login.continueWithApple': 'Continue with Apple',
        'login.continueWithGoogle': 'Continue with Google',
        'login.continueAsGuest': 'Continue as Guest',
        'login.guestHint': 'You can sign in later from Account.',
      };
      return map[key] ?? key;
    },
  }),
}));

jest.mock('../../stores/authStore', () => ({
  useAuthStore: (selector: any) =>
    selector({
      signInWithApple: jest.fn(),
      signInWithGoogle: jest.fn(),
      initialize: jest.fn(),
      isAuthenticated: true,
    }),
}));

jest.mock('../../services/appleSignIn', () => ({
  appleAuth: {Error: {CANCELED: '1001'}},
}));

jest.mock('../../services/googleSignIn', () => ({
  statusCodes: {SIGN_IN_CANCELLED: 'SIGN_IN_CANCELLED'},
}));

describe('LoginScreen', () => {
  it('shows Apple + Google + Guest CTAs on iOS', () => {
    Platform.OS = 'ios';
    let tree: TestRenderer.ReactTestRenderer;
    act(() => {
      tree = TestRenderer.create(<LoginScreen />);
    });
    const text = JSON.stringify(tree!.toJSON());
    expect(text).toContain('Continue with Apple');
    expect(text).toContain('Continue with Google');
    expect(text).toContain('Continue as Guest');
  });

  it('hides Apple CTA on Android', () => {
    Platform.OS = 'android';
    let tree: TestRenderer.ReactTestRenderer;
    act(() => {
      tree = TestRenderer.create(<LoginScreen />);
    });
    const text = JSON.stringify(tree!.toJSON());
    expect(text).not.toContain('Continue with Apple');
    expect(text).toContain('Continue with Google');
    expect(text).toContain('Continue as Guest');
  });
});
