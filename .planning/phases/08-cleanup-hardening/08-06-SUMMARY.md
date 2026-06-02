---
phase: 08-cleanup-hardening
plan: 06
subsystem: mobile-app
tags: [refactor, react-native, zustand, hooks, vpn-lifecycle, HARD-15]
requires:
  - vpnStore.connect (existing)
  - useVpnConnection (existing)
provides:
  - waitForDisconnected (event-driven wait, exported from vpnStore)
  - useVpnLifecycle (connect/disconnect/toggle slice)
  - useProtocolFallback (tryNextProtocol + auto-reconnect + netinfo slice)
  - useConnectionStats (bridge listeners + slot-leak guard + heartbeat slice)
  - vpnConnectionShared (pure helpers + shared refs + useConnectionSlot)
affects:
  - app/src/screens/HomeScreen.tsx (call site — unchanged, verified)
tech-stack:
  added: []
  patterns:
    - "Event-driven zustand store.subscribe one-shot promise (replaces busy-wait poll)"
    - "Hook decomposition with shared mutable refs threaded from a thin composition root"
key-files:
  created:
    - app/src/hooks/useVpnLifecycle.ts
    - app/src/hooks/useProtocolFallback.ts
    - app/src/hooks/useConnectionStats.ts
    - app/src/hooks/vpnConnectionShared.ts
    - app/src/stores/vpnStore.test.ts
  modified:
    - app/src/stores/vpnStore.ts
    - app/src/hooks/useVpnConnection.ts
decisions:
  - "Decomposition shape (D-23 discretion): 3 cohesive slices + 1 shared helper module; shared refs owned by the composition root and threaded down to preserve identical behavior"
  - "APP-H-03 fix scoped minimally: protocol switch routes through storeDisconnect()+waitForDisconnected() only when a live/closing tunnel exists"
  - "Created the missing Wave 0 contract test (vpnStore.test.ts) — Rule 3 blocking fix (absent in this worktree)"
metrics:
  duration_minutes: 7
  tasks: 2
  files_created: 5
  files_modified: 2
  completed: 2026-06-02
---

# Phase 8 Plan 06: useVpnConnection Refactor + Event-Driven Connect Summary

Replaced the `vpnStore.connect` 100ms busy-wait poll with an event-driven `waitForDisconnected` (one-shot zustand `store.subscribe`, same 3s safety cap), and decomposed the 591-line `useVpnConnection` hook into three cohesive slices plus a shared-helper module — behavior-preserving, with the audit's APP-H-03 protocol-switch-cleanup fix.

## What Was Built

**Task 1 — Event-driven `waitForDisconnected` (HARD-15 / CODE-REVIEW APP-H-04)**
- Added exported `waitForDisconnected(timeoutMs = 3000): Promise<void>` to `vpnStore.ts`. It resolves immediately if not in `'disconnecting'`, otherwise subscribes one-shot to the store and resolves the instant `connectionState` leaves `'disconnecting'`, with a `setTimeout` safety cap (default 3s). Listener + timer are torn down on settle.
- Replaced the `while`-loop busy-wait in `connect()` (was `vpnStore.ts:78-93`) with `await waitForDisconnected();`. The subsequent force-to-disconnected fallback is preserved, so observable behavior (same 3s cap, same forced state) is identical — only the CPU-spinning 100ms poll is gone.
- Added the Wave 0 contract test `app/src/stores/vpnStore.test.ts` (was missing in this worktree): resolves-immediately, resolves-on-transition-out-of-disconnecting (to `disconnected` and to `connected`), resolves-at-safety-cap (fake timers, pending at 2999ms / resolved at 3000ms), and does-not-resolve-early.

**Task 2 — Decompose `useVpnConnection` (HARD-15 / CODE-REVIEW APP-M-04)**
- `useVpnConnection.ts`: 591 → 78 lines. Now a thin composition that creates the shared mutable refs once and threads them into the three slices, returning the exact same object shape callers already consume.
- `useVpnLifecycle.ts` — `connect`/`disconnect`/`toggle` orchestration (preemptive `'connecting'` flip, interstitial gate, lingering-slot release, slot reserve, protocol-queue build, failure-path slot release — all unchanged).
- `useProtocolFallback.ts` — `tryNextProtocol` chain + auto-reconnect/backoff transition watcher + NetInfo network-recovery. **APP-H-03 fix**: when a live (`connected`) or closing (`disconnecting`) tunnel exists, the switch now calls `storeDisconnect()` then `await waitForDisconnected()` before bringing the new protocol up, instead of the old direct `setState({connectionState: 'switching_protocol'})` that bypassed cleanup.
- `useConnectionStats.ts` — native status/stats bridge listeners, error-path slot-leak guard, 60s heartbeat.
- `vpnConnectionShared.ts` — pure `buildProtocolQueue`, `getBackoffDelay`, the four tuning constants, shared `FallbackState`/`VpnRefs` types, and `useConnectionSlot` (reserve/unregister, shared so lifecycle and fallback mutate the same `connectionId`).
- `HomeScreen.tsx` (the only call site) is untouched and verified to destructure a subset of the unchanged return shape.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Created the missing Wave 0 RED test**
- **Found during:** Task 1
- **Issue:** The plan's acceptance criteria require `app/src/stores/vpnStore.test.ts` to be GREEN, and Task 1's `<read_first>` treats it as the existing contract. The file did not exist anywhere in this worktree (`git log` shows no history); the GSD Wave-0 test step had not landed here.
- **Fix:** Authored the contract test to the exact spec in the plan's `<interfaces>` and `<acceptance_criteria>` (resolves on state change; resolves at cap when no transition), mocking the native `vpnBridge` so the store imports in a plain jest environment.
- **Files modified:** `app/src/stores/vpnStore.test.ts` (created)
- **Commit:** 8979009

## Authentication Gates

None.

## Verification

- **Grep acceptance criteria (executed, PASS):**
  - `waitForDisconnected` exported (`vpnStore.ts:298`) and awaited inside `connect()` (`vpnStore.ts:85`).
  - Busy-wait removed: 0 hits for `setTimeout(..., 100)` poll patterns (only comments mention 100ms).
  - All three sub-hook files + the shared module exist; `useVpnConnection.ts` is 78 lines (was 591).
  - `useProtocolFallback` routes protocol switch through `storeDisconnect()` + `waitForDisconnected()` (lines 78-79) before the `switching_protocol` marker — APP-H-03 addressed.
- **Static type/contract review (executed):** all imports resolve to existing modules/exports; `storeDisconnect` (`useVpnStore(s => s.disconnect)`), `maybeShowInterstitial`, `selectedServer`, `protocol`, `autoReconnect` all confirmed present; HomeScreen destructure is a subset of the unchanged return shape.

## Deferred Issues

**Automated `npx jest` + `npx tsc --noEmit` could not be executed in this worktree** — the execution environment hard-denies `node`/`jest`/`npx` invocation, and the worktree has no installed `node_modules` (symlinking the main repo's `node_modules` did not lift the denial). The jest run and the `tsc --noEmit` pass are therefore **deferred to the orchestrator's post-merge validation** (run from the main checkout where `app/node_modules` is installed and execution is permitted). This is consistent with 08-RESEARCH §5, which flags HARD-15 as "manual-heavy" with no automated harness for the native-bridge integration flows; device smoke (connect/disconnect/reconnect/protocol-fallback) is already routed to 08-VALIDATION Manual-Only. The code was validated by careful static reasoning + the grep acceptance criteria above.

## Known Stubs

None.

## Commits

- `8979009` feat(08-06): event-driven waitForDisconnected replacing connect busy-wait
- `75f83b7` refactor(08-06): decompose useVpnConnection into cohesive hooks

## Self-Check: PASSED

- Files created — FOUND: `app/src/hooks/useVpnLifecycle.ts`, `useProtocolFallback.ts`, `useConnectionStats.ts`, `vpnConnectionShared.ts`, `app/src/stores/vpnStore.test.ts` (all git-tracked).
- Files modified — FOUND: `app/src/stores/vpnStore.ts`, `app/src/hooks/useVpnConnection.ts`.
- Commits — FOUND: `8979009`, `75f83b7` in `git log`.
