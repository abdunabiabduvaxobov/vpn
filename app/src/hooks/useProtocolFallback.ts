import {useCallback, useEffect} from 'react';
import NetInfo from '@react-native-community/netinfo';
import {useVpnStore, waitForDisconnected} from '../stores/vpnStore';
import {useServerStore} from '../stores/serverStore';
import {useSettingsStore} from '../stores/settingsStore';
import api from '../services/api';
import type {ServerConfig} from '../types/api';
import {
  MAX_RECONNECT_ATTEMPTS,
  MAX_PROTOCOL_FALLBACKS,
  getBackoffDelay,
  buildProtocolQueue,
  useConnectionSlot,
  type VpnRefs,
} from './vpnConnectionShared';

/**
 * Owns the protocol-fallback + auto-reconnect + network-recovery slice of
 * the VPN lifecycle, extracted from the former 591-line useVpnConnection
 * (HARD-15 / CODE-REVIEW APP-M-04). Behavior-preserving except for the
 * APP-H-03 fix below.
 *
 * Returns `tryNextProtocol` so the lifecycle slice can reference it (it
 * does not, today, but the symmetry keeps the composition explicit).
 */
export function useProtocolFallback(refs: VpnRefs) {
  const connectionState = useVpnStore(s => s.connectionState);
  const connectionId = useVpnStore(s => s.connectionId);
  const storeConnect = useVpnStore(s => s.connect);
  const storeDisconnect = useVpnStore(s => s.disconnect);
  const setReconnectAttempt = useVpnStore(s => s.setReconnectAttempt);

  const {selectedServer} = useServerStore();
  const {autoReconnect, protocol: userProtocol} = useSettingsStore();
  const {reserveConnection, unregisterConnection} = useConnectionSlot();

  const currentServer = useVpnStore(s => s.currentServer);

  const {fallbackRef, reconnectTimerRef, isManualDisconnectRef, prevStateRef} =
    refs;

  // Try connecting with the next protocol in the fallback queue.
  const tryNextProtocol = useCallback(async () => {
    const fb = fallbackRef.current;
    if (
      !fb.config ||
      fb.index >= fb.queue.length ||
      fb.index >= MAX_PROTOCOL_FALLBACKS
    ) {
      // All protocols exhausted.
      useVpnStore.setState({
        connectionState: 'error',
        error: 'All protocols blocked',
      });
      return;
    }

    const nextProtocol = fb.queue[fb.index];
    console.log(
      `[VPN Connection] Switching to protocol: ${nextProtocol} (${
        fb.index + 1
      }/${fb.queue.length})`,
    );

    // APP-H-03 fix: route protocol switches through the proper disconnect
    // cleanup instead of a direct setState that bypasses it. If a tunnel is
    // still up or tearing down when we switch protocols, tear it down and
    // wait for the store to settle out of 'disconnecting' (event-driven,
    // capped) BEFORE bringing the new protocol up — otherwise the new
    // connect stacks on top of an un-cleaned native tunnel. This is the one
    // allowed behavior change in this plan (the audit calls for it); it is
    // minimal and only fires when a live/closing tunnel exists.
    const stateBeforeSwitch = useVpnStore.getState().connectionState;
    if (
      stateBeforeSwitch === 'connected' ||
      stateBeforeSwitch === 'disconnecting'
    ) {
      await storeDisconnect();
      await waitForDisconnected();
    }

    useVpnStore.setState({connectionState: 'switching_protocol'});

    const server = selectedServer || currentServer;
    if (!server) return;

    try {
      // Ensure we have a connection slot reserved.
      if (!useVpnStore.getState().connectionId) {
        const reserved = await reserveConnection(server.id);
        if (!reserved) {
          useVpnStore.setState({
            connectionState: 'error',
            error: 'Device limit reached',
          });
          return;
        }
      }

      // Re-fetch config in case server updated priority hints.
      const {data} = await api.get<{data: ServerConfig}>(
        `/servers/${server.id}/config`,
      );
      fb.config = data.data;

      // Rebuild queue from fresh config (server may have changed priorities).
      const freshQueue = buildProtocolQueue(data.data, userProtocol);
      const currentProtocolInFresh = freshQueue.indexOf(nextProtocol);
      if (currentProtocolInFresh >= 0) {
        fb.queue = freshQueue;
        fb.index = currentProtocolInFresh;
      }

      // Override the protocol in the config for the Go tunnel.
      const configWithProtocol = {
        ...data.data,
        protocol: nextProtocol,
      };

      await storeConnect(server, configWithProtocol);
      fb.attemptPerProtocol = 0;
    } catch (err) {
      console.error(
        `[VPN Connection] Failed with protocol ${nextProtocol}:`,
        err,
      );
      // Move to next protocol.
      fb.index++;
      fb.attemptPerProtocol = 0;
      // Small delay before trying next protocol.
      if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = setTimeout(() => tryNextProtocol(), 1000);
    }
  }, [
    selectedServer,
    currentServer,
    storeConnect,
    storeDisconnect,
    userProtocol,
    reserveConnection,
    fallbackRef,
    reconnectTimerRef,
  ]);

  // Watch for state transitions to handle auto-reconnect.
  useEffect(() => {
    const prevState = prevStateRef.current;

    // On transition to connected: reset fallback state (connection was
    // already reserved before tunnel connect).
    if (prevState !== 'connected' && connectionState === 'connected') {
      fallbackRef.current.attemptPerProtocol = 0;
    }

    // On transition from ANY non-terminal state to disconnected:
    // unregister + maybe reconnect. Including 'connecting' in the prev set
    // means every failed first-time connect also releases its slot, so the
    // server-side count stays honest.
    if (
      (prevState === 'connected' ||
        prevState === 'connecting' ||
        prevState === 'reconnecting' ||
        prevState === 'switching_protocol') &&
      connectionState === 'disconnected'
    ) {
      // Unregister the connection.
      if (connectionId) {
        unregisterConnection(connectionId);
      }

      // Auto-reconnect with protocol fallback.
      if (
        !isManualDisconnectRef.current &&
        autoReconnect &&
        (selectedServer || currentServer)
      ) {
        const fb = fallbackRef.current;
        fb.attemptPerProtocol++;

        if (fb.attemptPerProtocol >= MAX_RECONNECT_ATTEMPTS) {
          // Current protocol is failing — try next one.
          fb.index++;
          fb.attemptPerProtocol = 0;

          if (
            fb.index < fb.queue.length &&
            fb.index < MAX_PROTOCOL_FALLBACKS
          ) {
            reconnectTimerRef.current = setTimeout(
              () => tryNextProtocol(),
              1000,
            );
          } else {
            useVpnStore.setState({
              connectionState: 'error',
              error: 'All protocols blocked',
            });
          }
        } else {
          // Retry same protocol with backoff.
          const delay = getBackoffDelay(fb.attemptPerProtocol);
          setReconnectAttempt(fb.attemptPerProtocol);
          useVpnStore.setState({connectionState: 'reconnecting'});

          reconnectTimerRef.current = setTimeout(async () => {
            const server = selectedServer || currentServer;
            if (!server || !fb.config) return;
            try {
              // Ensure we have a connection slot reserved for the reconnect.
              if (!useVpnStore.getState().connectionId) {
                const reserved = await reserveConnection(server.id);
                if (!reserved) {
                  useVpnStore.setState({
                    connectionState: 'error',
                    error: 'Device limit reached',
                  });
                  return;
                }
              }
              const configWithProtocol = {
                ...fb.config,
                protocol: fb.queue[fb.index] || fb.config.protocol,
              };
              await storeConnect(server, configWithProtocol);
            } catch (err) {
              console.error('[VPN Connection] Reconnect attempt failed:', err);
            }
          }, delay);
        }
      }
    }

    prevStateRef.current = connectionState;
  }, [
    connectionState,
    connectionId,
    autoReconnect,
    currentServer,
    selectedServer,
    unregisterConnection,
    reserveConnection,
    setReconnectAttempt,
    storeConnect,
    tryNextProtocol,
    fallbackRef,
    reconnectTimerRef,
    isManualDisconnectRef,
    prevStateRef,
  ]);

  // Cleanup reconnect timer on unmount.
  useEffect(() => {
    return () => {
      if (reconnectTimerRef.current) {
        clearTimeout(reconnectTimerRef.current);
      }
    };
  }, [reconnectTimerRef]);

  // Detect network recovery and trigger reconnection when VPN was active.
  useEffect(() => {
    if (connectionState === 'connected') {
      refs.wasConnectedRef.current = true;
    } else if (
      connectionState === 'disconnected' ||
      connectionState === 'error'
    ) {
      // Reset only on manual disconnect (auto-reconnect keeps it true).
      if (isManualDisconnectRef.current) {
        refs.wasConnectedRef.current = false;
      }
    }
  }, [connectionState, isManualDisconnectRef, refs.wasConnectedRef]);

  useEffect(() => {
    const unsubscribe = NetInfo.addEventListener(state => {
      const isConnected =
        state.isConnected && state.isInternetReachable !== false;
      const vpnState = useVpnStore.getState().connectionState;

      // Network came back while VPN is in error/disconnected state and
      // wasn't manually disconnected. Skip if auto-reconnect is already
      // handling it (reconnecting/connecting/switching_protocol).
      if (
        isConnected &&
        refs.wasConnectedRef.current &&
        !isManualDisconnectRef.current &&
        autoReconnect &&
        (vpnState === 'error' || vpnState === 'disconnected') &&
        (selectedServer || currentServer)
      ) {
        console.log('[VPN Connection] Network recovered — attempting reconnect');
        refs.wasConnectedRef.current = false; // prevent duplicate triggers

        // Small delay to let the network stabilize.
        if (reconnectTimerRef.current) clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = setTimeout(async () => {
          // Re-check state — auto-reconnect may have already started.
          const currentState = useVpnStore.getState().connectionState;
          if (
            currentState === 'connecting' ||
            currentState === 'reconnecting' ||
            currentState === 'connected' ||
            currentState === 'switching_protocol'
          ) {
            return;
          }

          const server = selectedServer || currentServer;
          if (!server) return;

          try {
            const {data} = await api.get<{data: ServerConfig}>(
              `/servers/${server.id}/config`,
            );
            const config = data.data;
            const queue = buildProtocolQueue(config, userProtocol);
            fallbackRef.current = {queue, index: 0, attemptPerProtocol: 0, config};

            const reservedId = await reserveConnection(server.id);
            if (!reservedId) {
              useVpnStore.setState({
                connectionState: 'error',
                error: 'Device limit reached',
              });
              return;
            }

            useVpnStore.setState({connectionState: 'reconnecting'});
            setReconnectAttempt(1);

            const configWithProtocol = {
              ...config,
              protocol: queue[0] || config.protocol,
            };
            await storeConnect(server, configWithProtocol);
          } catch (err) {
            console.error(
              '[VPN Connection] Network recovery reconnect failed:',
              err,
            );
          }
        }, 2000);
      }
    });

    return () => unsubscribe();
  }, [
    autoReconnect,
    selectedServer,
    currentServer,
    userProtocol,
    reserveConnection,
    storeConnect,
    setReconnectAttempt,
    fallbackRef,
    reconnectTimerRef,
    isManualDisconnectRef,
    refs.wasConnectedRef,
  ]);

  return {tryNextProtocol, unregisterConnection};
}
