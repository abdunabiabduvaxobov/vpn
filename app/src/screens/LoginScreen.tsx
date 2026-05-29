import React, {useState} from 'react';
import {
  SafeAreaView,
  ScrollView,
  Text,
  TouchableOpacity,
  StyleSheet,
  Platform,
  ActivityIndicator,
  Alert,
} from 'react-native';
import {useTranslation} from 'react-i18next';
import {useNavigation} from '@react-navigation/native';
import type {NativeStackNavigationProp} from '@react-navigation/native-stack';
import {colors, typography, spacing, borderRadius} from '../theme';
import {useAuthStore} from '../stores/authStore';
import {appleAuth} from '../services/appleSignIn';
import {statusCodes} from '../services/googleSignIn';
import type {RootStackParamList} from '../navigation/RootNavigator';

type NavProp = NativeStackNavigationProp<RootStackParamList, 'Login'>;

export function LoginScreen() {
  const {t} = useTranslation();
  const navigation = useNavigation<NavProp>();
  const [isBusy, setIsBusy] = useState<'apple' | 'google' | 'guest' | null>(null);
  const ssoAppleAction = useAuthStore(s => s.signInWithApple);
  const ssoGoogleAction = useAuthStore(s => s.signInWithGoogle);
  const initialize = useAuthStore(s => s.initialize);

  // D-05: silent transition — pop the login flow and land on Home.
  const goHome = () => navigation.reset({index: 0, routes: [{name: 'Home'}]});

  const onApple = async () => {
    setIsBusy('apple');
    try {
      await ssoAppleAction();
      goHome(); // D-05: silent transition
    } catch (e: any) {
      // D-02: cancellation returns silently to LoginScreen.
      if (e?.code === appleAuth.Error.CANCELED) return;
      // Other errors — also return silently per UI-SPEC interaction contract
      // (no Alert). Per-provider toasts are deferred (CONTEXT.md <deferred>).
    } finally {
      setIsBusy(null);
    }
  };

  const onGoogle = async () => {
    setIsBusy('google');
    try {
      await ssoGoogleAction();
      goHome();
    } catch (e: any) {
      if (e?.code === statusCodes.SIGN_IN_CANCELLED) return;
    } finally {
      setIsBusy(null);
    }
  };

  const onGuest = async () => {
    setIsBusy('guest');
    try {
      // WR-04: initialize() now returns a Promise that settles once guest auth
      // has completed (tokens set or failed), so awaiting it is meaningful —
      // goHome() only runs after the guest session actually exists. Read the
      // LIVE store state (not the reactive `isAuthenticated` closure value,
      // which is stale within this async handler) to gate navigation.
      if (!useAuthStore.getState().isAuthenticated) {
        await initialize();
      }
      if (useAuthStore.getState().isAuthenticated) {
        goHome();
      } else {
        // Guest login failed (e.g. offline). Surface a non-fatal error and
        // stay on LoginScreen instead of landing on Home unauthenticated.
        Alert.alert(t('login.signInFailed'));
      }
    } finally {
      setIsBusy(null);
    }
  };

  return (
    <SafeAreaView style={styles.safe}>
      <ScrollView contentContainerStyle={styles.scroll}>
        <Text style={styles.title}>{t('login.title')}</Text>
        <Text style={styles.subtitle}>{t('login.subtitle')}</Text>

        {Platform.OS === 'ios' && (
          <TouchableOpacity
            style={[styles.cta, styles.ctaPrimary]}
            onPress={onApple}
            disabled={isBusy !== null}
            activeOpacity={0.85}
            accessibilityLabel={t('login.continueWithApple')}>
            {isBusy === 'apple' ? (
              <ActivityIndicator color={colors.textPrimary} />
            ) : (
              <Text style={styles.ctaText}>{t('login.continueWithApple')}</Text>
            )}
          </TouchableOpacity>
        )}

        <TouchableOpacity
          style={[styles.cta, styles.ctaPrimary]}
          onPress={onGoogle}
          disabled={isBusy !== null}
          activeOpacity={0.85}
          accessibilityLabel={t('login.continueWithGoogle')}>
          {isBusy === 'google' ? (
            <ActivityIndicator color={colors.textPrimary} />
          ) : (
            <Text style={styles.ctaText}>{t('login.continueWithGoogle')}</Text>
          )}
        </TouchableOpacity>

        <TouchableOpacity
          style={[styles.cta, styles.ctaGuest]}
          onPress={onGuest}
          disabled={isBusy !== null}
          activeOpacity={0.85}
          accessibilityLabel={t('login.continueAsGuest')}>
          {isBusy === 'guest' ? (
            <ActivityIndicator color={colors.textPrimary} />
          ) : (
            <Text style={styles.ctaTextGuest}>{t('login.continueAsGuest')}</Text>
          )}
        </TouchableOpacity>

        <Text style={styles.guestHint}>{t('login.guestHint')}</Text>
      </ScrollView>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safe: {flex: 1, backgroundColor: colors.background},
  scroll: {
    flexGrow: 1,
    justifyContent: 'center',
    paddingHorizontal: spacing.lg,
    paddingBottom: spacing.xxl,
  },
  title: {...typography.h1, color: colors.textPrimary, marginBottom: spacing.sm},
  subtitle: {
    ...typography.body,
    color: colors.textSecondary,
    marginBottom: spacing.xl,
  },
  cta: {
    paddingVertical: spacing.md,
    borderRadius: borderRadius.sm,
    marginBottom: spacing.md,
    alignItems: 'center',
    minHeight: 48,
    justifyContent: 'center',
  },
  ctaPrimary: {backgroundColor: colors.primary},
  ctaGuest: {
    backgroundColor: 'transparent',
    borderWidth: 1,
    borderColor: colors.border,
  },
  ctaText: {...typography.bodyBold, color: colors.textPrimary},
  ctaTextGuest: {...typography.bodyBold, color: colors.textSecondary},
  guestHint: {
    ...typography.caption,
    color: colors.textMuted,
    textAlign: 'center',
    marginTop: spacing.sm,
  },
});

export default LoginScreen;
