// Phase 8 — Wave 0 contract test for HARD-15 (CODE-REVIEW APP-H-04).
// Pins the behavior of the event-driven `waitForDisconnected`, which
// replaces the old 100ms busy-wait poll in vpnStore.connect():
//   - resolves immediately when not in 'disconnecting'
//   - resolves the instant the store leaves 'disconnecting' (event-driven)
//   - resolves at the safety cap when no transition ever happens
// Tracks: 08-06-PLAN.md Task 1, 08-VALIDATION HARD-15.

// The store module imports the native VPN bridge at load time; stub it so
// the store can be required in a plain jest (node) environment.
jest.mock('../services/vpnBridge', () => ({
  __esModule: true,
  connect: jest.fn().mockResolvedValue(undefined),
  disconnect: jest.fn().mockResolvedValue(undefined),
  getStatus: jest.fn(),
  onStatusChanged: jest.fn(() => () => {}),
  onStatsUpdated: jest.fn(() => () => {}),
}));

import {useVpnStore, waitForDisconnected} from './vpnStore';

function resetStore() {
  useVpnStore.setState({connectionState: 'disconnected'});
}

describe('waitForDisconnected (HARD-15 — event-driven, no busy-wait)', () => {
  beforeEach(() => {
    jest.useRealTimers();
    resetStore();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it('resolves immediately when the store is not in "disconnecting"', async () => {
    useVpnStore.setState({connectionState: 'disconnected'});
    await expect(waitForDisconnected()).resolves.toBeUndefined();
  });

  it('resolves the instant the store leaves "disconnecting" (no polling)', async () => {
    useVpnStore.setState({connectionState: 'disconnecting'});

    const promise = waitForDisconnected(3000);

    // Drive a real state transition — the one-shot subscription should fire.
    useVpnStore.setState({connectionState: 'disconnected'});

    await expect(promise).resolves.toBeUndefined();
  });

  it('resolves when transitioning to any non-disconnecting state (e.g. connected)', async () => {
    useVpnStore.setState({connectionState: 'disconnecting'});

    const promise = waitForDisconnected(3000);
    useVpnStore.setState({connectionState: 'connected'});

    await expect(promise).resolves.toBeUndefined();
  });

  it('resolves at the safety cap when no transition ever happens', async () => {
    jest.useFakeTimers();
    useVpnStore.setState({connectionState: 'disconnecting'});

    let resolved = false;
    const promise = waitForDisconnected(3000).then(() => {
      resolved = true;
    });

    // Before the cap: still pending (proves it is NOT busy-waiting/resolving early).
    jest.advanceTimersByTime(2999);
    await Promise.resolve();
    expect(resolved).toBe(false);

    // At the cap: the safety timeout fires and resolves.
    jest.advanceTimersByTime(1);
    await promise;
    expect(resolved).toBe(true);
  });

  it('does not resolve early before the cap while still disconnecting', async () => {
    jest.useFakeTimers();
    useVpnStore.setState({connectionState: 'disconnecting'});

    let resolved = false;
    waitForDisconnected(3000).then(() => {
      resolved = true;
    });

    // Old busy-wait would have polled every 100ms; advancing 100ms here
    // must NOT resolve, since the state never changed.
    jest.advanceTimersByTime(100);
    await Promise.resolve();
    expect(resolved).toBe(false);
  });
});
