import React, {useEffect, useRef, useState} from 'react';
import {AppState, AppStateStatus, View, Text, StyleSheet, SafeAreaView, TouchableOpacity} from 'react-native';
import {useTranslation} from 'react-i18next';
import {useNavigation} from '@react-navigation/native';
import type {NativeStackNavigationProp} from '@react-navigation/native-stack';

import {ConnectButton} from '../components/ConnectButton';
import {SpeedIndicator} from '../components/SpeedIndicator';
import {StatusBadge} from '../components/StatusBadge';
import {useVpnConnection} from '../hooks/useVpnConnection';
import {useAuthStore} from '../stores/authStore';
import {useServerStore} from '../stores/serverStore';
import {useLayout} from '../hooks/useLayout';
import {AdBanner} from '../ads/AdBanner';
import {colors, typography, spacing} from '../theme';
import type {RootStackParamList} from '../navigation/RootNavigator';

type NavigationProp = NativeStackNavigationProp<RootStackParamList>;

export function HomeScreen() {
  const {t} = useTranslation();
  const navigation = useNavigation<NavigationProp>();
  const {
    connectionState,
    currentServer,
    connectedAt,
    bytesUp,
    bytesDown,
    speedUp,
    speedDown,
    toggle,
  } = useVpnConnection();
  const {selectedServer} = useServerStore();
  const {tabletContentStyle} = useLayout();
  const fetchAccount = useAuthStore(s => s.fetchAccount);
  const isAuthenticated = useAuthStore(s => s.isAuthenticated);

  // Refresh user data (subscription_tier) when app returns to foreground
  // so ad gating picks up changes made via the admin panel.
  //
  // Phase 5 D-09: foreground safety-net for users who paid on web but
  // closed the browser before tapping "Open in app". /account returns
  // the updated subscription_tier (Phase 2 contract), so the existing
  // fetchAccount() call below already covers this case — no additional
  // network call needed.
  const appStateRef = useRef<AppStateStatus>(AppState.currentState);
  useEffect(() => {
    if (isAuthenticated) fetchAccount();
    const sub = AppState.addEventListener('change', next => {
      if (appStateRef.current !== 'active' && next === 'active' && isAuthenticated) {
        fetchAccount();
      }
      appStateRef.current = next;
    });
    return () => sub.remove();
  }, [isAuthenticated, fetchAccount]);

  // Connection timer
  const [elapsed, setElapsed] = useState('00:00:00');

  useEffect(() => {
    if (connectionState !== 'connected' || !connectedAt) {
      setElapsed('00:00:00');
      return;
    }

    const interval = setInterval(() => {
      const diff = Math.floor((Date.now() - connectedAt.getTime()) / 1000);
      const hrs = Math.floor(diff / 3600).toString().padStart(2, '0');
      const mins = Math.floor((diff % 3600) / 60).toString().padStart(2, '0');
      const secs = (diff % 60).toString().padStart(2, '0');
      setElapsed(`${hrs}:${mins}:${secs}`);
    }, 1000);

    return () => clearInterval(interval);
  }, [connectionState, connectedAt]);

  const serverDisplay = selectedServer || currentServer;

  return (
    <SafeAreaView style={styles.container}>
      <View style={[styles.content, tabletContentStyle]}>
        {/* Status */}
        <View style={styles.statusSection}>
          <StatusBadge state={connectionState} />
          {connectionState === 'connected' && (
            <Text style={styles.timer}>{elapsed}</Text>
          )}
          {connectionState === 'disconnected' && (
            <Text style={styles.hint}>{t('connection.tapToConnect')}</Text>
          )}
        </View>

        {/* Connect Button */}
        <View style={styles.buttonSection}>
          <ConnectButton state={connectionState} onPress={toggle} />
        </View>

        {/* Speed Stats (visible when connected) */}
        {connectionState === 'connected' && (
          <SpeedIndicator
            speedUp={speedUp}
            speedDown={speedDown}
            bytesUp={bytesUp}
            bytesDown={bytesDown}
          />
        )}

        {/* Server Selection */}
        <TouchableOpacity
          style={styles.serverSelector}
          onPress={() => navigation.navigate('ServerList')}
          activeOpacity={0.7}>
          {serverDisplay ? (
            <View style={styles.serverInfo}>
              <Text style={styles.serverCity}>{serverDisplay.city}</Text>
              <Text style={styles.serverCountry}>{serverDisplay.country}</Text>
            </View>
          ) : (
            <Text style={styles.selectServerText}>
              {t('connection.selectServer')}
            </Text>
          )}
          <Text style={styles.chevron}>›</Text>
        </TouchableOpacity>
      </View>

      {/* Ad banner — only renders for free tier users */}
      <AdBanner />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.background,
  },
  content: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: spacing.lg,
  },
  statusSection: {
    alignItems: 'center',
    marginBottom: spacing.xl,
  },
  timer: {
    ...typography.h2,
    color: colors.textPrimary,
    marginTop: spacing.sm,
    fontVariant: ['tabular-nums'],
  },
  hint: {
    ...typography.caption,
    color: colors.textMuted,
    marginTop: spacing.sm,
  },
  buttonSection: {
    marginVertical: spacing.xl,
  },
  serverSelector: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.surface,
    borderRadius: 12,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.md,
    marginTop: spacing.xl,
    width: '100%',
    borderWidth: 1,
    borderColor: colors.border,
  },
  serverInfo: {
    flex: 1,
  },
  serverCity: {
    ...typography.bodyBold,
    color: colors.textPrimary,
  },
  serverCountry: {
    ...typography.caption,
    color: colors.textSecondary,
  },
  selectServerText: {
    ...typography.body,
    color: colors.textSecondary,
    flex: 1,
  },
  chevron: {
    fontSize: 24,
    color: colors.textMuted,
  },
});
