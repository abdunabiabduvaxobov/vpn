import {useEffect} from 'react';
import {useVpnStore} from '../stores/vpnStore';
import * as vpnBridge from '../services/vpnBridge';
import api from '../services/api';

/**
 * Owns the "stats / timers / native event plumbing" slice of the VPN
 * connection lifecycle, extracted from the former 591-line
 * useVpnConnection (HARD-15 / CODE-REVIEW APP-M-04). Behavior-preserving:
 *
 *   1. Wires the native status/stats bridge listeners into the store.
 *   2. Releases a reserved slot whenever the tunnel lands in 'error'
 *      directly (without passing through 'disconnected').
 *   3. Sends the connection heartbeat every 60s while connected.
 *
 * No functional change — this is the same set of effects that previously
 * lived inline in useVpnConnection.
 */
export function useConnectionStats(
  unregisterConnection: (id: string) => Promise<void>,
): void {
  const connectionState = useVpnStore(s => s.connectionState);
  const connectionId = useVpnStore(s => s.connectionId);
  const updateStatus = useVpnStore(s => s.updateStatus);
  const updateStats = useVpnStore(s => s.updateStats);

  // Listen for native status change events.
  useEffect(() => {
    const unsubStatus = vpnBridge.onStatusChanged(updateStatus);
    const unsubStats = vpnBridge.onStatsUpdated(updateStats);

    return () => {
      unsubStatus();
      unsubStats();
    };
  }, [updateStatus, updateStats]);

  // Release any reserved slot whenever the tunnel lands in the 'error'
  // state. The transition watcher only releases slots on disconnect-edge
  // transitions, which misses the case where the native tunnel fires an
  // 'error' event directly without going through 'disconnected' first.
  // Without this the slot leaks to the server's active-connection count
  // and the next manual retry can hit the per-user device limit.
  useEffect(() => {
    if (connectionState === 'error' && connectionId) {
      unregisterConnection(connectionId);
    }
  }, [connectionState, connectionId, unregisterConnection]);

  // Send heartbeat every 60s while connected.
  useEffect(() => {
    if (connectionState !== 'connected' || !connectionId) return;

    const interval = setInterval(async () => {
      try {
        await api.patch(`/connections/${connectionId}/heartbeat`);
      } catch (err) {
        console.error('[VPN Connection] Heartbeat failed:', err);
      }
    }, 60_000);

    // Send initial heartbeat immediately on connect.
    api.patch(`/connections/${connectionId}/heartbeat`).catch(() => {});

    return () => clearInterval(interval);
  }, [connectionState, connectionId]);
}
