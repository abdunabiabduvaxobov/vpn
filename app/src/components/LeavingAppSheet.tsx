// Phase 5 D-12 — "You're leaving the app" interstitial.
// Appears BEFORE Linking.openURL on the PaymentScreen Upgrade CTA tap.
// App-store reviewer protection: one extra confirmation step before the
// external-browser handoff to risevpn.com/<locale>/pricing.

import React from 'react';
import {Modal, View, Text, TouchableOpacity, StyleSheet, Linking} from 'react-native';
import {useTranslation} from 'react-i18next';
import {colors, typography, spacing, borderRadius} from '../theme';

interface Props {
  visible: boolean;
  url: string;
  onDismiss: () => void;
}

export function LeavingAppSheet({visible, url, onDismiss}: Props) {
  const {t} = useTranslation();

  const onContinue = async () => {
    await Linking.openURL(url);
    onDismiss();
  };

  return (
    <Modal
      visible={visible}
      transparent
      animationType="slide"
      onRequestClose={onDismiss}>
      <View style={styles.scrim}>
        <View style={styles.sheet}>
          <Text style={styles.title}>{t('payment.upgrade.leaving.title')}</Text>
          <Text style={styles.body}>{t('payment.upgrade.leaving.body')}</Text>
          <TouchableOpacity
            style={[styles.btn, styles.btnPrimary]}
            onPress={onContinue}
            activeOpacity={0.85}
            accessibilityLabel={t('payment.upgrade.leaving.continue')}>
            <Text style={styles.btnPrimaryText}>{t('payment.upgrade.leaving.continue')}</Text>
          </TouchableOpacity>
          <TouchableOpacity
            style={[styles.btn, styles.btnSecondary]}
            onPress={onDismiss}
            activeOpacity={0.85}
            accessibilityLabel={t('payment.upgrade.leaving.cancel')}>
            <Text style={styles.btnSecondaryText}>{t('payment.upgrade.leaving.cancel')}</Text>
          </TouchableOpacity>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  scrim: {
    flex: 1,
    backgroundColor: colors.overlay,
    justifyContent: 'flex-end',
  },
  sheet: {
    backgroundColor: colors.surface,
    paddingHorizontal: spacing.lg,
    paddingTop: spacing.lg,
    paddingBottom: spacing.xxl,
    borderTopLeftRadius: borderRadius.xl,
    borderTopRightRadius: borderRadius.xl,
    borderTopWidth: 1,
    borderColor: colors.border,
  },
  title: {...typography.h3, color: colors.textPrimary, marginBottom: spacing.sm},
  body: {...typography.body, color: colors.textSecondary, marginBottom: spacing.lg},
  btn: {
    paddingVertical: spacing.md,
    borderRadius: borderRadius.sm,
    alignItems: 'center',
    minHeight: 48,
    justifyContent: 'center',
    marginBottom: spacing.sm,
  },
  btnPrimary: {backgroundColor: colors.primary},
  btnPrimaryText: {...typography.bodyBold, color: colors.textPrimary},
  btnSecondary: {
    backgroundColor: 'transparent',
    borderWidth: 1,
    borderColor: colors.border,
  },
  btnSecondaryText: {...typography.bodyBold, color: colors.textSecondary},
});

export default LeavingAppSheet;
