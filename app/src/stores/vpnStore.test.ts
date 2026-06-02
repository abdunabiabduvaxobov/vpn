// HARD-15 (D-23) — RED test for the event-driven `waitForDisconnected` helper
// that replaces the 100ms busy-wait poll in vpnStore.ts:78-93.
//
// Contract:
//   1. Resolves when the store transitions 'disconnecting' -> 'disconnected'
//      (driven by a zustand subscription, NOT a setTimeout poll loop).
//   2. Resolves at the timeout cap when the transition never happens — no
//      infinite hang, no busy-wait.
//
// RED now: `waitForDisconnected` is not exported from vpnStore yet. The import
// below resolves to `undefined`, so the guard test fails until HARD-15 lands.
// When it lands, remove the guard's `t.fail` path and the two behavioural specs
// will exercise the real implementation.

// vpnBridge pulls in NativeModules/NativeEventEmitter; stub it so the store
// module imports cleanly under jsdom/node.
jest.mock('../services/vpnBridge', () => ({
  __esModule: true,
  connectVpn: jest.fn().mockResolvedValue(undefined),
  disconnectVpn: jest.fn().mockResolvedValue(undefined),
  getStatus: jest.fn().mockResolvedValue({state: 'disconnected'}),
  subscribeToStatus: jest.fn().mockReturnValue(() => {}),
  subscribeToStats: jest.fn().mockReturnValue(() => {}),
}));

// Import the store namespace so we can reach the (future) waitForDisconnected
// export without a hard ESM binding error if it is absent.
import * as vpnStoreModule from './vpnStore';
import {useVpnStore} from './vpnStore';

type WaitForDisconnected = (timeoutMs?: number) => Promise<void>;

const waitForDisconnected: WaitForDisconnected | undefined = (
  vpnStoreModule as unknown as {waitForDisconnected?: WaitForDisconnected}
).waitForDisconnected;

beforeEach(() => {
  useVpnStore.setState({connectionState: 'disconnected'});
});

describe('waitForDisconnected (HARD-15)', () => {
  it('is exported from the vpn store', () => {
    // RED until HARD-15 exports it.
    expect(typeof waitForDisconnected).toBe('function');
  });

  it('resolves when state transitions disconnecting -> disconnected', async () => {
    if (typeof waitForDisconnected !== 'function') {
      throw new Error(
        'HARD-15: waitForDisconnected is not exported yet — implement the event-driven wait',
      );
    }

    useVpnStore.setState({connectionState: 'disconnecting'});
    const waiter = waitForDisconnected(3000);

    // Flip to disconnected on the next tick; the subscription must resolve the
    // promise (no busy-wait polling).
    setTimeout(() => {
      useVpnStore.setState({connectionState: 'disconnected'});
    }, 10);

    await expect(waiter).resolves.toBeUndefined();
  });

  it('resolves at the timeout cap when no transition occurs', async () => {
    if (typeof waitForDisconnected !== 'function') {
      throw new Error(
        'HARD-15: waitForDisconnected is not exported yet — implement the timeout cap',
      );
    }

    jest.useFakeTimers();
    try {
      useVpnStore.setState({connectionState: 'disconnecting'});
      const waiter = waitForDisconnected(3000);
      // Never transition; advance past the cap. Must resolve, not hang.
      jest.advanceTimersByTime(3000);
      await expect(waiter).resolves.toBeUndefined();
    } finally {
      jest.useRealTimers();
    }
  });
});
