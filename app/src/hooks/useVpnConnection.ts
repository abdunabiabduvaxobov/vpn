import {useRef} from 'react';
import {useVpnStore} from '../stores/vpnStore';
import {useConnectionStats} from './useConnectionStats';
import {useProtocolFallback} from './useProtocolFallback';
import {useVpnLifecycle} from './useVpnLifecycle';
import type {FallbackState, VpnRefs} from './vpnConnectionShared';

/**
 * Manages the VPN connection lifecycle with protocol fallback.
 *
 * HARD-15 / CODE-REVIEW APP-M-04: this hook was a 591-line monolith with
 * ~10 effects. It is now a THIN composition over three cohesive slices —
 *   - useVpnLifecycle    : connect / disconnect / toggle orchestration
 *   - useProtocolFallback: tryNextProtocol chain + auto-reconnect + netinfo
 *   - useConnectionStats : native event bridge + slot-leak guard + heartbeat
 * The shared mutable refs live here and are threaded into each slice so the
 * behavior is identical to the previous single-hook implementation. The
 * exported return shape is unchanged, so all call sites stay untouched.
 */
export function useVpnConnection() {
  const connectionState = useVpnStore(s => s.connectionState);
  const currentServer = useVpnStore(s => s.currentServer);
  const connectedAt = useVpnStore(s => s.connectedAt);
  const bytesUp = useVpnStore(s => s.bytesUp);
  const bytesDown = useVpnStore(s => s.bytesDown);
  const speedUp = useVpnStore(s => s.speedUp);
  const speedDown = useVpnStore(s => s.speedDown);
  const error = useVpnStore(s => s.error);
  const reconnectAttempt = useVpnStore(s => s.reconnectAttempt);
  const clearError = useVpnStore(s => s.clearError);

  // Shared mutable refs coordinated across the decomposed slices. Created
  // once here so all three sub-hooks see the same source of truth.
  const prevStateRef = useRef<string>(connectionState);
  const isManualDisconnectRef = useRef(false);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const fallbackRef = useRef<FallbackState>({
    queue: [],
    index: 0,
    attemptPerProtocol: 0,
    config: null,
  });
  const wasConnectedRef = useRef(false);

  const refs: VpnRefs = {
    prevStateRef,
    isManualDisconnectRef,
    reconnectTimerRef,
    fallbackRef,
    wasConnectedRef,
  };

  // Protocol fallback / auto-reconnect / network recovery. Exposes the
  // slot unregister helper used by the stats slice's error-path guard.
  const {unregisterConnection} = useProtocolFallback(refs);

  // Native event bridge + error-slot guard + heartbeat.
  useConnectionStats(unregisterConnection);

  // User-facing connect / disconnect / toggle.
  const {connect, disconnect, toggle} = useVpnLifecycle(refs);

  return {
    connectionState,
    currentServer,
    connectedAt,
    bytesUp,
    bytesDown,
    speedUp,
    speedDown,
    error,
    reconnectAttempt,
    connect,
    disconnect,
    toggle,
    clearError,
  };
}
