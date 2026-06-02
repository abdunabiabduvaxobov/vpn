import {create} from 'zustand';
import * as vpnBridge from '../services/vpnBridge';
import type {ConnectionState, TunnelStatus, TrafficStats} from '../types/vpn';
import type {Server, ServerConfig} from '../types/api';

// Module-level timeout handle — kept outside Zustand state to avoid type hacks
let disconnectTimeout: ReturnType<typeof setTimeout> | null = null;

function clearDisconnectTimeout() {
  if (disconnectTimeout) {
    clearTimeout(disconnectTimeout);
    disconnectTimeout = null;
  }
}

interface VpnState {
  connectionState: ConnectionState;
  currentServer: Server | null;
  serverConfig: ServerConfig | null;
  connectedAt: Date | null;
  bytesUp: number;
  bytesDown: number;
  speedUp: number;
  speedDown: number;
  error: string | null;
  reconnectAttempt: number;
  connectionId: string | null;
  // True while connect() is in flight — prevents re-entrant calls
  _connecting: boolean;

  connect: (server: Server, config: ServerConfig) => Promise<void>;
  disconnect: () => Promise<void>;
  updateStatus: (status: TunnelStatus) => void;
  updateStats: (stats: TrafficStats) => void;
  clearError: () => void;
  setReconnectAttempt: (attempt: number) => void;
  setConnectionId: (id: string | null) => void;
}

export const useVpnStore = create<VpnState>((set, get) => ({
  connectionState: 'disconnected',
  currentServer: null,
  serverConfig: null,
  connectedAt: null,
  bytesUp: 0,
  bytesDown: 0,
  speedUp: 0,
  speedDown: 0,
  error: null,
  reconnectAttempt: 0,
  connectionId: null,
  _connecting: false,

  connect: async (server: Server, config: ServerConfig) => {
    let {connectionState, _connecting} = get();

    // Block if a connect() call is already in flight (re-entry guard)
    // or the tunnel is already up. We deliberately do NOT block on
    // connectionState === 'connecting' here: the useVpnConnection hook
    // sets that state preemptively at the top of its own connect()
    // flow to give the button immediate visual feedback while the
    // server-config fetch and slot reservation are still running. If
    // we blocked on 'connecting' the preemptive update would race
    // itself and stall the whole flow. _connecting is still the
    // authoritative re-entry flag.
    if (_connecting || connectionState === 'connected') {
      console.log('[VPN Store] connect blocked: _connecting=', _connecting, 'state=', connectionState);
      return;
    }

    // If the previous disconnect is still in flight on the native side,
    // wait for it to finish before starting a new connect. Otherwise the
    // fresh connect() races the still-pending native disconnect and the
    // native side typically loses the race — leaving the user with a
    // failed first tap and a "works on second tap" experience.
    //
    // Event-driven (HARD-15 / CODE-REVIEW APP-H-04): instead of polling
    // connectionState every 100ms, await waitForDisconnected() which
    // resolves the instant the store leaves 'disconnecting' (via a
    // one-shot store subscription), with the SAME 3s safety cap as the
    // old poll. The store's own disconnect safety timeout is 5s so we
    // always finish inside that window.
    if (connectionState === 'disconnecting') {
      console.log('[VPN Store] connect waiting for disconnecting to finish');
      await waitForDisconnected();
      connectionState = get().connectionState;
      // If we're still stuck in disconnecting after the wait, force it.
      if (connectionState === 'disconnecting') {
        console.warn('[VPN Store] disconnecting state stuck — forcing disconnected');
        set({connectionState: 'disconnected'});
      }
    }

    // Clear any stale disconnect timeout from a previous disconnect
    clearDisconnectTimeout();

    set({
      _connecting: true,
      connectionState: 'connecting',
      currentServer: server,
      serverConfig: config,
      error: null,
    });

    try {
      // Call native module via VPN bridge → Android VpnService / iOS NEVPNManager
      // The promise resolves AFTER TUN + tun2socks are fully set up (step10).
      //
      // Safety timeout: if the native promise doesn't resolve or reject
      // within 15 seconds, race it against a rejection so the UI doesn't
      // get stuck on 'connecting' forever. The native tunnel might actually
      // be connected (xray logs show traffic flowing while JS thinks we're
      // still connecting) — the timeout surfaces this as an error and the
      // user can retry, at which point the native side is already up and
      // the second attempt resolves instantly.
      const CONNECT_TIMEOUT_MS = 15_000;
      const timeoutPromise = new Promise<never>((_, reject) => {
        setTimeout(() => reject(new Error('Connection timed out')), CONNECT_TIMEOUT_MS);
      });
      await Promise.race([vpnBridge.connect(config), timeoutPromise]);

      // Promise resolved = VPN fully connected (TUN + tun2socks ready).
      set({
        _connecting: false,
        connectionState: 'connected',
        connectedAt: new Date(),
        reconnectAttempt: 0,
      });
    } catch (err) {
      const errorMsg = err instanceof Error ? err.message : 'Connection failed';
      console.error('[VPN Store] connect error:', errorMsg);
      // Report error to API for debugging (fire-and-forget)
      try {
        const api = (await import('../services/api')).default;
        api.post('/debug/error', { error: errorMsg, action: 'connect' }).catch(() => {});
      } catch {}
      set({
        _connecting: false,
        connectionState: 'error',
        error: errorMsg,
      });
    }
  },

  disconnect: async () => {
    const {connectionState} = get();
    if (connectionState === 'disconnected') {
      return;
    }

    // Clear any previous disconnect timeout (handles double-disconnect)
    clearDisconnectTimeout();

    set({connectionState: 'disconnecting'});

    try {
      await vpnBridge.disconnect();

      // Don't eagerly set 'disconnected' — wait for native status broadcast.
      // Use a safety timeout: if native doesn't confirm within 5s, force it.
      disconnectTimeout = setTimeout(() => {
        if (get().connectionState === 'disconnecting') {
          console.warn('[VPN Store] Native disconnect timeout — forcing state');
          set({
            connectionState: 'disconnected',
            connectedAt: null,
            bytesUp: 0,
            bytesDown: 0,
            speedUp: 0,
            speedDown: 0,
            reconnectAttempt: 0,
          });
        }
        disconnectTimeout = null;
      }, 5000);
    } catch (err) {
      set({
        connectionState: 'error',
        error: err instanceof Error ? err.message : 'Disconnect failed',
      });
    }
  },

  // Called by the native module event listener when tunnel status changes
  updateStatus: (status: TunnelStatus) => {
    // Report errors to API for debugging
    if (status.state === 'error' && status.error) {
      import('../services/api').then(m => {
        m.default.post('/debug/error', {
          error: status.error,
          action: 'status_event',
          state: status.state,
        }).catch(() => {});
      }).catch(() => {});
    }

    // While connect() is in flight, do NOT overwrite connectionState from
    // native callbacks — the connect() promise flow owns the state.
    // Only update traffic stats and timing info.
    if (get()._connecting) {
      set({
        bytesUp: status.bytes_up,
        bytesDown: status.bytes_down,
        connectedAt: status.connected_at > 0 ? new Date(status.connected_at * 1000) : null,
      });
      return;
    }

    const current = get().connectionState;

    // Guard 1: preemptive 'connecting' window. useVpnConnection.connect()
    // sets state to 'connecting' BEFORE vpnStore.connect() runs (so the
    // button turns yellow immediately). During this API-prep window,
    // _connecting is false, so the guard above doesn't fire. Native
    // events arriving during this window are stale echoes from the
    // previous session's teardown (or a status poll on startup) and
    // must not overwrite the preemptive state — otherwise the UI
    // flickers back to 'disconnected' / "Press to connect" for 1-2s
    // in the middle of a tap the user already committed to.
    // Only 'error' punches through (a real native failure we must
    // surface even during prep).
    if (current === 'connecting' && !get()._connecting && status.state !== 'error') {
      set({
        bytesUp: status.bytes_up,
        bytesDown: status.bytes_down,
        connectedAt: status.connected_at > 0 ? new Date(status.connected_at * 1000) : null,
      });
      return;
    }

    // Guard 2: stale events after connection is established. After
    // connect() resolves and state reaches 'connected', the native
    // side may still fire delayed 'connecting' events (or even
    // another 'connected' event). Only real state changes matter:
    // 'disconnected' (tunnel dropped) and 'error' (tunnel crashed).
    // Everything else is an echo that would cause a visible flicker
    // in the UI (connected → connecting → connected in ~100ms).
    if (current === 'connected' && status.state !== 'disconnected' && status.state !== 'error') {
      set({
        bytesUp: status.bytes_up,
        bytesDown: status.bytes_down,
        connectedAt: status.connected_at > 0 ? new Date(status.connected_at * 1000) : null,
      });
      return;
    }

    // If native confirms 'disconnected', clear stats and cancel safety timeout
    if (status.state === 'disconnected') {
      clearDisconnectTimeout();
      set({
        connectionState: 'disconnected',
        connectedAt: null,
        bytesUp: 0,
        bytesDown: 0,
        speedUp: 0,
        speedDown: 0,
        reconnectAttempt: 0,
        error: null,
      });
      return;
    }

    set({
      connectionState: status.state,
      bytesUp: status.bytes_up,
      bytesDown: status.bytes_down,
      connectedAt: status.connected_at > 0 ? new Date(status.connected_at * 1000) : null,
      error: status.error || null,
    });
  },

  updateStats: (stats: TrafficStats) => {
    set({
      bytesUp: stats.bytes_up,
      bytesDown: stats.bytes_down,
      speedUp: stats.speed_up_bps,
      speedDown: stats.speed_down_bps,
    });
  },

  clearError: () => set({error: null}),

  setReconnectAttempt: (attempt) => set({reconnectAttempt: attempt}),

  setConnectionId: (id) => set({connectionId: id}),
}));

/**
 * Event-driven replacement for the old 100ms busy-wait poll in connect()
 * (HARD-15 / CODE-REVIEW APP-H-04). Resolves the instant the store leaves
 * the 'disconnecting' state via a one-shot store subscription, or after
 * `timeoutMs` as a safety cap — preserving the old 3s cap and the
 * force-to-disconnected fallback that connect() applies afterwards.
 *
 * Observable behavior is identical to the previous poll loop; the only
 * difference is no CPU-spinning setTimeout(…, 100) cycle.
 */
export function waitForDisconnected(timeoutMs = 3000): Promise<void> {
  return new Promise<void>(resolve => {
    // Already settled — nothing to wait for.
    if (useVpnStore.getState().connectionState !== 'disconnecting') {
      resolve();
      return;
    }

    let settled = false;
    const finish = () => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      unsub();
      resolve();
    };

    // Resolve the moment we transition out of 'disconnecting'.
    const unsub = useVpnStore.subscribe(state => {
      if (state.connectionState !== 'disconnecting') {
        finish();
      }
    });

    // Safety cap — preserves the old 3s upper bound so connect() can
    // force-to-disconnected if the native side never confirms.
    const timer = setTimeout(finish, timeoutMs);
  });
}
