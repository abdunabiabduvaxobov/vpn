import React from 'react';
import TestRenderer, {act} from 'react-test-renderer';
import {Platform, Alert} from 'react-native';
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
        'login.signInFailed': "Couldn't sign in. Please try again.",
      };
      return map[key] ?? key;
    },
  }),
}));

// Mutable store snapshot so individual tests can drive the SSO/guest actions
// and the live getState() reads in the handlers. Prefixed `mock*` to satisfy
// babel-plugin-jest-hoist scope rules inside the jest.mock factory.
const mockState = {
  signInWithApple: jest.fn(),
  signInWithGoogle: jest.fn(),
  initialize: jest.fn(),
  isAuthenticated: true,
};
jest.mock('../../stores/authStore', () => {
  const store: any = (selector: any) => selector(mockState);
  store.getState = () => mockState;
  return {useAuthStore: store};
});

jest.mock('../../services/appleSignIn', () => ({
  appleAuth: {Error: {CANCELED: '1001'}},
}));
jest.mock('../../services/googleSignIn', () => ({
  statusCodes: {SIGN_IN_CANCELLED: 'SIGN_IN_CANCELLED'},
}));

/**
 * Walk the rendered tree and invoke the onPress of the TouchableOpacity whose
 * accessibilityLabel matches `label`.
 */
function pressByLabel(tree: TestRenderer.ReactTestRenderer, label: string) {
  const node = tree.root.findAll(
    n => n.props && n.props.accessibilityLabel === label && typeof n.props.onPress === 'function',
  )[0];
  return node.props.onPress();
}

describe('LoginScreen', () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockState.isAuthenticated = true;
    mockState.signInWithApple = jest.fn().mockResolvedValue(undefined);
    mockState.signInWithGoogle = jest.fn().mockResolvedValue(undefined);
    mockState.initialize = jest.fn().mockResolvedValue(undefined);
  });

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

  // FIX 3 (WR-05) — Apple non-cancellation error surfaces a non-fatal Alert.
  it('alerts on Apple non-cancellation error and does NOT silently swallow', async () => {
    Platform.OS = 'ios';
    const alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    mockState.signInWithApple = jest
      .fn()
      .mockRejectedValue(Object.assign(new Error('network'), {code: 'NETWORK'}));
    let tree: TestRenderer.ReactTestRenderer;
    await act(async () => {
      tree = TestRenderer.create(<LoginScreen />);
    });
    await act(async () => {
      await pressByLabel(tree!, 'Continue with Apple');
    });
    expect(alertSpy).toHaveBeenCalledWith("Couldn't sign in. Please try again.");
    alertSpy.mockRestore();
  });

  // FIX 3 (WR-05) — Apple cancellation stays silent (no Alert).
  it('stays silent on Apple user cancellation', async () => {
    Platform.OS = 'ios';
    const alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    mockState.signInWithApple = jest
      .fn()
      .mockRejectedValue(Object.assign(new Error('cancelled'), {code: '1001'}));
    let tree: TestRenderer.ReactTestRenderer;
    await act(async () => {
      tree = TestRenderer.create(<LoginScreen />);
    });
    await act(async () => {
      await pressByLabel(tree!, 'Continue with Apple');
    });
    expect(alertSpy).not.toHaveBeenCalled();
    alertSpy.mockRestore();
  });

  // FIX 3 (WR-05) — Google now has a non-cancellation catch (no unhandled reject).
  it('alerts on Google non-cancellation error', async () => {
    Platform.OS = 'android';
    const alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    mockState.signInWithGoogle = jest
      .fn()
      .mockRejectedValue(Object.assign(new Error('backend down'), {code: 'X'}));
    let tree: TestRenderer.ReactTestRenderer;
    await act(async () => {
      tree = TestRenderer.create(<LoginScreen />);
    });
    await act(async () => {
      await pressByLabel(tree!, 'Continue with Google');
    });
    expect(alertSpy).toHaveBeenCalledWith("Couldn't sign in. Please try again.");
    alertSpy.mockRestore();
  });

  it('stays silent on Google user cancellation', async () => {
    Platform.OS = 'android';
    const alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    mockState.signInWithGoogle = jest
      .fn()
      .mockRejectedValue(
        Object.assign(new Error('cancelled'), {code: 'SIGN_IN_CANCELLED'}),
      );
    let tree: TestRenderer.ReactTestRenderer;
    await act(async () => {
      tree = TestRenderer.create(<LoginScreen />);
    });
    await act(async () => {
      await pressByLabel(tree!, 'Continue with Google');
    });
    expect(alertSpy).not.toHaveBeenCalled();
    alertSpy.mockRestore();
  });

  // FIX 2 (WR-04) — guest CTA awaits initialize() and only then navigates when
  // the live store reports authenticated.
  it('awaits initialize() on guest path when not yet authenticated', async () => {
    Platform.OS = 'android';
    mockState.isAuthenticated = false;
    let resolved = false;
    mockState.initialize = jest.fn().mockImplementation(async () => {
      // Simulate guest auth completing — flip the live state.
      mockState.isAuthenticated = true;
      resolved = true;
    });
    let tree: TestRenderer.ReactTestRenderer;
    await act(async () => {
      tree = TestRenderer.create(<LoginScreen />);
    });
    await act(async () => {
      await pressByLabel(tree!, 'Continue as Guest');
    });
    expect(mockState.initialize).toHaveBeenCalledTimes(1);
    expect(resolved).toBe(true);
  });

  // FIX 2 (WR-04) — guest auth failure surfaces an Alert, does NOT navigate.
  it('alerts and stays put when guest auth fails (still unauthenticated)', async () => {
    Platform.OS = 'android';
    mockState.isAuthenticated = false;
    const alertSpy = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    mockState.initialize = jest.fn().mockResolvedValue(undefined); // stays unauth
    let tree: TestRenderer.ReactTestRenderer;
    await act(async () => {
      tree = TestRenderer.create(<LoginScreen />);
    });
    await act(async () => {
      await pressByLabel(tree!, 'Continue as Guest');
    });
    expect(alertSpy).toHaveBeenCalledWith("Couldn't sign in. Please try again.");
    alertSpy.mockRestore();
  });
});
