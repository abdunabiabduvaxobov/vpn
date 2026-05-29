// Phase 5 D-14 — informational PaymentScreen (App-store compliant).
//
// NO in-app purchase, NO prices, NO buy button. A single CTA reading
// "Upgrade to Pro at risevpn.com" opens the LeavingAppSheet interstitial
// (D-12), which then hands off to the locale-aware /pricing URL in the
// system browser. The tertiary "Already paid? Refresh" link (D-15) just
// re-fetches /account.

import React, {useState} from 'react';
import {
  SafeAreaView,
  ScrollView,
  View,
  Text,
  TouchableOpacity,
  StyleSheet,
  ToastAndroid,
  Platform,
  Alert,
} from 'react-native';
import {useTranslation} from 'react-i18next';
import i18n from 'i18next';
import {colors, typography, spacing, borderRadius} from '../theme';
import {useAuthStore} from '../stores/authStore';
import {upgradeUrlForLocale} from '../services/payment';
import {LeavingAppSheet} from '../components/LeavingAppSheet';

function showToast(msg: string) {
  if (Platform.OS === 'android') {
    ToastAndroid.show(msg, ToastAndroid.SHORT);
  } else {
    Alert.alert(msg);
  }
}

export function PaymentScreen() {
  const {t} = useTranslation();
  const user = useAuthStore(s => s.user);
  const fetchAccount = useAuthStore(s => s.fetchAccount);
  const [sheetVisible, setSheetVisible] = useState(false);

  const isPro = user?.subscription_tier && user.subscription_tier !== 'free';
  const upgradeUrl = upgradeUrlForLocale(i18n.language || 'en');

  const onUpgradeTap = () => {
    // D-12: ALWAYS open interstitial BEFORE Linking.openURL. Never bypass.
    setSheetVisible(true);
  };

  const onRefresh = async () => {
    const before = user?.subscription_tier;
    await fetchAccount();
    const after = useAuthStore.getState().user?.subscription_tier;
    if (after && after !== 'free' && before === 'free') {
      showToast(t('payment.upgrade.refreshUpgraded'));
    } else {
      showToast(t('payment.upgrade.refreshNoChange'));
    }
  };

  return (
    <SafeAreaView style={styles.safe}>
      <ScrollView contentContainerStyle={styles.scroll}>
        {/* Current-plan card (always rendered) */}
        <View style={styles.card}>
          <Text style={styles.cardLabel}>{t('payment.upgrade.currentPlanLabel')}</Text>
          <Text style={styles.cardTitle}>
            {isPro ? t('payment.upgrade.proActiveTitle') : 'Free'}
          </Text>
          {!isPro && (
            <View style={styles.limitsList}>
              <Text style={styles.limit}>• {t('payment.upgrade.freeLimits.devices')}</Text>
              <Text style={styles.limit}>• {t('payment.upgrade.freeLimits.data')}</Text>
              <Text style={styles.limit}>• {t('payment.upgrade.freeLimits.servers')}</Text>
            </View>
          )}
          {/* Pro users: manage-subscription link (Phase 4 D-16). If the
              GET /subscription/manage-url endpoint isn't available, the
              link silently stays hidden (D-14 footnote). */}
        </View>

        {!isPro && (
          <>
            {/* "Pro includes" feature list */}
            <View style={styles.card}>
              <Text style={styles.featuresHeader}>{t('payment.upgrade.proIncludesHeading')}</Text>
              <View style={styles.featuresList}>
                <Text style={styles.feature}>✓ {t('payment.upgrade.proFeatures.speed')}</Text>
                <Text style={styles.feature}>✓ {t('payment.upgrade.proFeatures.servers')}</Text>
                <Text style={styles.feature}>✓ {t('payment.upgrade.proFeatures.devices')}</Text>
                <Text style={styles.feature}>✓ {t('payment.upgrade.proFeatures.ads')}</Text>
              </View>
            </View>

            {/* Single CTA — D-13 locked copy, no price text */}
            <TouchableOpacity
              style={styles.ctaButton}
              onPress={onUpgradeTap}
              activeOpacity={0.85}
              accessibilityLabel={t('payment.upgrade.cta')}>
              <Text style={styles.ctaText}>{t('payment.upgrade.cta')}</Text>
            </TouchableOpacity>

            {/* Tertiary refresh link — D-15 non-prominent */}
            <TouchableOpacity
              onPress={onRefresh}
              style={styles.refreshRow}
              accessibilityLabel={t('payment.upgrade.alreadyPaid')}>
              <Text style={styles.refreshText}>{t('payment.upgrade.alreadyPaid')}</Text>
            </TouchableOpacity>
          </>
        )}
      </ScrollView>

      <LeavingAppSheet
        visible={sheetVisible}
        url={upgradeUrl}
        onDismiss={() => setSheetVisible(false)}
      />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safe: {flex: 1, backgroundColor: colors.background},
  scroll: {
    padding: spacing.lg,
    paddingBottom: spacing.xxl,
  },
  card: {
    backgroundColor: colors.surface,
    borderRadius: borderRadius.lg,
    padding: spacing.lg,
    marginBottom: spacing.md,
    borderWidth: 1,
    borderColor: colors.border,
  },
  cardLabel: {
    ...typography.caption,
    color: colors.textMuted,
    textTransform: 'uppercase',
    letterSpacing: 1,
    marginBottom: spacing.xs,
  },
  cardTitle: {
    ...typography.h2,
    color: colors.textPrimary,
    marginBottom: spacing.md,
  },
  limitsList: {gap: spacing.xs},
  limit: {...typography.body, color: colors.textSecondary},
  featuresHeader: {
    ...typography.h3,
    color: colors.textPrimary,
    marginBottom: spacing.md,
  },
  featuresList: {gap: spacing.sm},
  feature: {...typography.body, color: colors.textPrimary},
  ctaButton: {
    backgroundColor: colors.primary,
    borderRadius: borderRadius.sm,
    paddingVertical: spacing.md,
    alignItems: 'center',
    marginTop: spacing.sm,
    minHeight: 48,
    justifyContent: 'center',
  },
  ctaText: {...typography.bodyBold, color: colors.textPrimary},
  refreshRow: {paddingVertical: spacing.md, alignItems: 'center'},
  refreshText: {...typography.caption, color: colors.primary},
});

export default PaymentScreen;
