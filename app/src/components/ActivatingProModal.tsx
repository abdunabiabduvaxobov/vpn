// Phase 5 D-07/D-08/D-10/D-11 — root-level Activating-Pro polling overlay.
//
// Mounts whenever authStore.isActivatingPro is true (set by deepLink.ts on
// vpnapp://payment/success?invoiceId=X). Polls GET /invoices/:id on the locked
// Phase 4 D-21 cadence: every 2000ms, ?escalate=true from poll #6, 30s budget
// (15 polls). T-1: pendingInvoiceId is UNTRUSTED — only flips to 'success'
// after the backend returns status === 'paid'.

import React, {useEffect, useState, useRef} from 'react';
import {
  Modal,
  View,
  Text,
  TouchableOpacity,
  ActivityIndicator,
  StyleSheet,
  Linking,
} from 'react-native';
import {useTranslation} from 'react-i18next';
import {useNavigation} from '@react-navigation/native';
import type {NativeStackNavigationProp} from '@react-navigation/native-stack';
import {useAuthStore} from '../stores/authStore';
import {getInvoice} from '../services/payment';
import {colors, typography, spacing, borderRadius} from '../theme';
import type {RootStackParamList} from '../navigation/RootNavigator';

const POLL_INTERVAL_MS = 2000;
const MAX_POLLS = 15; // 30s budget / 2s
const ESCALATE_AFTER = 5; // poll #6 onward sets ?escalate=true
const TELEGRAM_URL = 'https://t.me/flawlssr';

type ModalState = 'polling' | 'success' | 'failed' | 'takingLonger';
type NavProp = NativeStackNavigationProp<RootStackParamList>;

export function ActivatingProModal() {
  const {t} = useTranslation();
  const navigation = useNavigation<NavProp>();
  const pendingInvoiceId = useAuthStore(s => s.pendingInvoiceId);
  const isActivatingPro = useAuthStore(s => s.isActivatingPro);
  const isAuthenticated = useAuthStore(s => s.isAuthenticated);
  const fetchAccount = useAuthStore(s => s.fetchAccount);
  const stopActivatingPro = useAuthStore(s => s.stopActivatingPro);

  const [modalState, setModalState] = useState<ModalState>('polling');
  const cancelledRef = useRef(false);

  useEffect(() => {
    if (!isActivatingPro || !pendingInvoiceId) {
      // Reset on modal close.
      cancelledRef.current = true;
      setModalState('polling');
      return;
    }
    if (!isAuthenticated) {
      // Pitfall 4: wait for auth before polling. Once initialize() mints a
      // guest JWT and isAuthenticated flips, this effect re-runs.
      return;
    }

    cancelledRef.current = false;
    let pollCount = 0;

    const tick = async () => {
      if (cancelledRef.current) return;
      pollCount += 1;
      try {
        const inv = await getInvoice(pendingInvoiceId, pollCount > ESCALATE_AFTER);
        if (cancelledRef.current) return;
        if (inv.status === 'paid') {
          await fetchAccount();
          if (cancelledRef.current) return;
          setModalState('success');
          setTimeout(() => {
            stopActivatingPro();
            setModalState('polling');
          }, 3000);
          return;
        }
        if (inv.status === 'failed') {
          setModalState('failed');
          return;
        }
        // 'pending' or 'expired' — keep polling.
      } catch {
        // transient — keep polling.
      }
      if (pollCount >= MAX_POLLS) {
        if (!cancelledRef.current) setModalState('takingLonger');
        return;
      }
      setTimeout(tick, POLL_INTERVAL_MS);
    };

    tick();

    return () => {
      cancelledRef.current = true;
    };
  }, [isActivatingPro, pendingInvoiceId, isAuthenticated, fetchAccount, stopActivatingPro]);

  const onFailedBackToAccount = () => {
    stopActivatingPro();
    setModalState('polling');
    navigation.navigate('Account');
  };

  const onRefresh = async () => {
    if (!pendingInvoiceId) return;
    try {
      const inv = await getInvoice(pendingInvoiceId, true);
      await fetchAccount();
      if (inv.status === 'paid') {
        setModalState('success');
        setTimeout(() => {
          stopActivatingPro();
          setModalState('polling');
        }, 3000);
      }
    } catch {
      /* stay in takingLonger */
    }
  };

  const onContactSupport = () => {
    Linking.openURL(TELEGRAM_URL);
  };

  const onClose = () => {
    stopActivatingPro();
    setModalState('polling');
  };

  // Only render when activating.
  if (!isActivatingPro) return null;

  // D-07: block dismissal during polling; allow on takingLonger (Close) +
  // failed (Back) + success (auto-dismiss).
  const dismissable =
    modalState === 'takingLonger' || modalState === 'failed' || modalState === 'success';

  return (
    <Modal
      visible
      transparent
      animationType="fade"
      onRequestClose={dismissable ? onClose : () => {}}>
      <View style={styles.scrim}>
        <View style={styles.card}>
          {modalState === 'polling' && (
            <>
              <ActivityIndicator size="large" color={colors.primary} style={styles.spinner} />
              <Text style={styles.title}>{t('payment.activating.title')}</Text>
              <Text style={styles.body}>{t('payment.activating.subtitle')}</Text>
            </>
          )}
          {modalState === 'success' && (
            <>
              <Text style={[styles.title, {color: colors.success}]}>
                {t('payment.activating.successTitle')}
              </Text>
              <Text style={styles.body}>{t('payment.activating.successToast')}</Text>
            </>
          )}
          {modalState === 'failed' && (
            <>
              <Text style={styles.title}>{t('payment.activating.failedTitle')}</Text>
              <Text style={styles.body}>{t('payment.activating.failedBody')}</Text>
              <TouchableOpacity
                style={[styles.btn, styles.btnPrimary]}
                onPress={onFailedBackToAccount}>
                <Text style={styles.btnPrimaryText}>{t('payment.activating.failedRetry')}</Text>
              </TouchableOpacity>
            </>
          )}
          {modalState === 'takingLonger' && (
            <>
              <Text style={styles.title}>{t('payment.takingLonger.title')}</Text>
              <Text style={styles.body}>{t('payment.takingLonger.body')}</Text>
              <TouchableOpacity style={[styles.btn, styles.btnPrimary]} onPress={onRefresh}>
                <Text style={styles.btnPrimaryText}>{t('payment.takingLonger.refresh')}</Text>
              </TouchableOpacity>
              <TouchableOpacity onPress={onContactSupport} style={styles.linkRow}>
                <Text style={styles.linkText}>{t('payment.takingLonger.contactSupport')}</Text>
              </TouchableOpacity>
              <TouchableOpacity style={[styles.btn, styles.btnSecondary]} onPress={onClose}>
                <Text style={styles.btnSecondaryText}>{t('payment.takingLonger.close')}</Text>
              </TouchableOpacity>
            </>
          )}
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  scrim: {
    flex: 1,
    backgroundColor: colors.overlay,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: spacing.lg,
  },
  card: {
    backgroundColor: colors.surface,
    borderRadius: borderRadius.lg,
    padding: spacing.lg,
    alignItems: 'stretch',
    width: '100%',
    maxWidth: 420,
    borderWidth: 1,
    borderColor: colors.border,
  },
  spinner: {marginBottom: spacing.md},
  title: {
    ...typography.h2,
    color: colors.textPrimary,
    marginBottom: spacing.sm,
    textAlign: 'center',
  },
  body: {
    ...typography.body,
    color: colors.textSecondary,
    marginBottom: spacing.md,
    textAlign: 'center',
  },
  btn: {
    paddingVertical: spacing.md,
    borderRadius: borderRadius.sm,
    alignItems: 'center',
    minHeight: 48,
    justifyContent: 'center',
    marginTop: spacing.sm,
  },
  btnPrimary: {backgroundColor: colors.primary},
  btnPrimaryText: {...typography.bodyBold, color: colors.textPrimary},
  btnSecondary: {backgroundColor: 'transparent', borderWidth: 1, borderColor: colors.border},
  btnSecondaryText: {...typography.bodyBold, color: colors.textSecondary},
  linkRow: {paddingVertical: spacing.sm, alignItems: 'center'},
  linkText: {...typography.caption, color: colors.primary},
});

export default ActivatingProModal;
