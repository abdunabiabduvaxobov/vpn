import type {MutableRefObject} from 'react';
import {useCallback} from 'react';
import {useVpnStore} from '../stores/vpnStore';
import api from '../services/api';
import type {ServerConfig} from '../types/api';

// Shared tuning constants for the VPN connection lifecycle. Extracted
// verbatim from the former useVpnConnection so the lifecycle, protocol
// fallback, and reconnect slices stay in lock-step (HARD-15).
export const MAX_RECONNECT_ATTEMPTS = 3;
export const MAX_PROTOCOL_FALLBACKS = 3;
export const BASE_RECONNECT_DELAY_MS = 1000;
export const MAX_RECONNECT_DELAY_MS = 30_000;

export function getBackoffDelay(attempt: number): number {
  const delay = BASE_RECONNECT_DELAY_MS * Math.pow(2, attempt);
  return Math.min(delay, MAX_RECONNECT_DELAY_MS);
}

/**
 * Builds an ordered list of protocols to try, based on server capabilities
 * and priority hints from the API (geo-aware). Pure function — unchanged
 * from the original useVpnConnection.
 */
export function buildProtocolQueue(
  config: ServerConfig,
  userProtocol: string,
): string[] {
  // Start with server-provided priority (geo-aware) or a default.
  const priority = config.protocol_priority ?? [
    'vless-reality',
    'amneziawg',
    'vless-ws',
  ];

  // Filter to only include protocols the server actually supports.
  const available = priority.filter(p => {
    if (p === 'vless-reality' && config.reality) return true;
    if (p === 'vless-ws' && config.websocket) return true;
    if (p === 'amneziawg' && config.awg) return true;
    return false;
  });

  // If user selected a specific protocol (not "auto"), move it to the front.
  if (userProtocol !== 'auto' && available.includes(userProtocol)) {
    return [userProtocol, ...available.filter(p => p !== userProtocol)];
  }

  // Fallback: if no protocols matched (misconfigured server), use the
  // server's default.
  if (available.length === 0) {
    return [config.protocol];
  }

  return available;
}

// Mutable protocol-fallback bookkeeping shared between the lifecycle and
// fallback slices.
export interface FallbackState {
  queue: string[];
  index: number;
  attemptPerProtocol: number;
  config: ServerConfig | null;
}

// Bundle of shared mutable refs that the decomposed VPN hooks coordinate
// through. useVpnConnection owns these and threads them into each slice so
// behavior is identical to the original single-hook implementation.
export interface VpnRefs {
  prevStateRef: MutableRefObject<string>;
  isManualDisconnectRef: MutableRefObject<boolean>;
  reconnectTimerRef: MutableRefObject<ReturnType<typeof setTimeout> | null>;
  fallbackRef: MutableRefObject<FallbackState>;
  wasConnectedRef: MutableRefObject<boolean>;
}

// Hook exposing the backend slot reserve/unregister calls. Shared because
// both the lifecycle (initial connect) and fallback (reconnect / protocol
// switch) slices need them, and both must mutate the SAME connectionId in
// the store. Unchanged from the original useVpnConnection.
export function useConnectionSlot() {
  const setConnectionId = useVpnStore(s => s.setConnectionId);

  const reserveConnection = useCallback(
    async (serverId: string): Promise<string | null> => {
      try {
        const res = await api.post<{data: {id: string}}>('/connections', {
          server_id: serverId,
        });
        const id = res.data.data.id;
        setConnectionId(id);
        return id;
      } catch (err) {
        console.error(
          '[VPN Connection] Failed to reserve connection slot:',
          err,
        );
        return null;
      }
    },
    [setConnectionId],
  );

  const unregisterConnection = useCallback(
    async (id: string) => {
      try {
        await api.delete(`/connections/${id}`);
      } catch (err) {
        console.error('[VPN Connection] Failed to unregister connection:', err);
      } finally {
        setConnectionId(null);
      }
    },
    [setConnectionId],
  );

  return {reserveConnection, unregisterConnection, setConnectionId};
}
