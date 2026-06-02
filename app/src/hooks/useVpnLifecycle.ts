import {useCallback} from 'react';
import {useVpnStore} from '../stores/vpnStore';
import {useServerStore} from '../stores/serverStore';
import {useSettingsStore} from '../stores/settingsStore';
import {useInterstitialAd} from '../ads/useInterstitialAd';
import api from '../services/api';
import type {ServerConfig} from '../types/api';
import {
  buildProtocolQueue,
  useConnectionSlot,
  type VpnRefs,
} from './vpnConnectionShared';

/**
 * Owns the user-facing connect/disconnect/toggle orchestration slice of the
 * VPN lifecycle, extracted from the former 591-line useVpnConnection
 * (HARD-15 / CODE-REVIEW APP-M-04). Behavior-preserving: identical
 * preemptive 'connecting' flip, interstitial gate, slot reservation,
 * protocol-queue build, and failure-path slot release as before.
 */
export function useVpnLifecycle(refs: VpnRefs) {
  const connectionState = useVpnStore(s => s.connectionState);
  const storeConnect = useVpnStore(s => s.connect);
  const storeDisconnect = useVpnStore(s => s.disconnect);
  const setReconnectAttempt = useVpnStore(s => s.setReconnectAttempt);

  const {selectedServer} = useServerStore();
  const {protocol: userProtocol} = useSettingsStore();
  const {maybeShowInterstitial} = useInterstitialAd();
  const {reserveConnection, unregisterConnection, setConnectionId} =
    useConnectionSlot();

  const {isManualDisconnectRef, reconnectTimerRef, fallbackRef} = refs;

  const connect = useCallback(async () => {
    const server = selectedServer;
    if (!server) return;

    // Flip the UI to 'connecting' immediately so the button turns yellow +
    // shows the spinner the moment the user taps. vpnStore.connect() no
    // longer blocks on this state so the subsequent storeConnect call
    // proceeds normally.
    useVpnStore.setState({connectionState: 'connecting', error: null});

    // Show interstitial ad (free users, every Nth connect). Awaits until
    // dismissed, skipped, or times out — never blocks VPN.
    await maybeShowInterstitial();

    isManualDisconnectRef.current = false;

    // Defensive: release any lingering connection slot from a previous
    // failed attempt before reserving a new one.
    const lingeringId = useVpnStore.getState().connectionId;
    if (lingeringId) {
      try {
        await api.delete(`/connections/${lingeringId}`);
      } catch {
        // Best effort — the scheduler will eventually clean stale rows.
      }
      setConnectionId(null);
    }

    try {
      // 1. Fetch server config.
      const {data} = await api.get<{data: ServerConfig}>(
        `/servers/${server.id}/config`,
      );
      const config = data.data;

      // 2. Reserve connection slot BEFORE connecting tunnel.
      const reservedConnectionId = await reserveConnection(server.id);
      if (!reservedConnectionId) {
        useVpnStore.setState({
          connectionState: 'error',
          error: 'Device limit reached',
        });
        return;
      }

      // 3. Build protocol queue and connect tunnel.
      const queue = buildProtocolQueue(config, userProtocol);
      fallbackRef.current = {
        queue,
        index: 0,
        attemptPerProtocol: 0,
        config,
      };

      const primaryProtocol = queue[0] || config.protocol;
      const configWithProtocol = {
        ...config,
        protocol: primaryProtocol,
      };

      console.log(
        `[VPN Connection] Connecting with protocol: ${primaryProtocol}, queue: [${queue.join(
          ', ',
        )}]`,
      );

      try {
        await storeConnect(server, configWithProtocol);
      } catch (err) {
        // 4. Tunnel failed — release reservation.
        if (reservedConnectionId) {
          try {
            await api.delete(`/connections/${reservedConnectionId}`);
          } catch {}
          setConnectionId(null);
        }
        throw err;
      }
    } catch (err) {
      console.error('Failed to connect:', err);
      // Release any slot we managed to reserve before the failure.
      const stuckId = useVpnStore.getState().connectionId;
      if (stuckId) {
        try {
          await api.delete(`/connections/${stuckId}`);
        } catch {}
        setConnectionId(null);
      }
      useVpnStore.setState({
        connectionState: 'error',
        error: err instanceof Error ? err.message : 'Connection failed',
      });
    }
  }, [
    selectedServer,
    storeConnect,
    userProtocol,
    reserveConnection,
    setConnectionId,
    maybeShowInterstitial,
    isManualDisconnectRef,
    fallbackRef,
  ]);

  const disconnect = useCallback(async () => {
    // Mark as manual so the auto-reconnect logic is suppressed.
    isManualDisconnectRef.current = true;

    // Cancel any pending reconnect.
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
    setReconnectAttempt(0);
    fallbackRef.current = {queue: [], index: 0, attemptPerProtocol: 0, config: null};

    // Unregister BEFORE store clears tunnel — read fresh ID from store.
    const currentId = useVpnStore.getState().connectionId;
    if (currentId) {
      await unregisterConnection(currentId);
    }

    await storeDisconnect();
  }, [
    storeDisconnect,
    setReconnectAttempt,
    unregisterConnection,
    isManualDisconnectRef,
    reconnectTimerRef,
    fallbackRef,
  ]);

  // Toggle: connect if disconnected, disconnect if connected.
  const toggle = useCallback(async () => {
    if (connectionState === 'connected') {
      await disconnect();
    } else if (
      connectionState === 'disconnected' ||
      connectionState === 'error'
    ) {
      await connect();
    }
  }, [connectionState, connect, disconnect]);

  return {connect, disconnect, toggle};
}
