import React, {useEffect} from 'react';
import {StatusBar} from 'react-native';
import {SafeAreaProvider} from 'react-native-safe-area-context';
import {NavigationContainer} from '@react-navigation/native';
import {QueryClient, QueryClientProvider} from '@tanstack/react-query';
import {MobileAds} from 'yandex-mobile-ads';

import {RootNavigator} from './src/navigation/RootNavigator';
import {useAuthStore} from './src/stores/authStore';
import {useSettingsStore} from './src/stores/settingsStore';
import {ErrorBoundary} from './src/components/ErrorBoundary';
import {ActivatingProModal} from './src/components/ActivatingProModal';
import {configureGoogleSignIn} from './src/services/googleSignIn';
import {registerDeepLinkHandler} from './src/services/deepLink';
import {colors} from './src/theme';

// Import i18n to initialize translations
import './src/i18n';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 2,
      staleTime: 30_000,
    },
  },
});

const navTheme = {
  dark: true,
  colors: {
    primary: colors.primary,
    background: colors.background,
    card: colors.surface,
    text: colors.textPrimary,
    border: colors.border,
    notification: colors.error,
  },
  fonts: {
    regular: {fontFamily: 'System', fontWeight: '400' as const},
    medium: {fontFamily: 'System', fontWeight: '500' as const},
    bold: {fontFamily: 'System', fontWeight: '700' as const},
    heavy: {fontFamily: 'System', fontWeight: '900' as const},
  },
};

function App(): React.JSX.Element {
  // Restore persisted state on app launch
  useEffect(() => {
    useAuthStore.getState().initialize();
    useSettingsStore.getState().initialize();

    // Phase 5: Google Sign-In must be configured before the first sign-in
    // attempt; register the vpnapp:// deep-link handler so payment-return
    // links surface the Activating-Pro modal (cold start + warm foreground).
    configureGoogleSignIn();
    const unsubscribeDeepLink = registerDeepLinkHandler();

    // Initialize Yandex Ads SDK (fire-and-forget, failure is non-fatal)
    MobileAds.initialize().catch(() => {
      console.warn('[Ads] SDK initialization failed');
    });

    return () => {
      unsubscribeDeepLink();
    };
  }, []);

  return (
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <SafeAreaProvider>
          <StatusBar barStyle="light-content" backgroundColor={colors.background} />
          <NavigationContainer theme={navTheme}>
            <RootNavigator />
            <ActivatingProModal />
          </NavigationContainer>
        </SafeAreaProvider>
      </QueryClientProvider>
    </ErrorBoundary>
  );
}

export default App;
