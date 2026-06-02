---
phase: 08-cleanup-hardening
plan: 09
subsystem: mobile-auth
tags: [security, secure-storage, keychain, device-binding, HARD-16, SC5]
requires: ["08-01", "08-04", "08-06"]
provides:
  - "auth tokens at rest in iOS Keychain / Android EncryptedSharedPreferences (not AsyncStorage)"
  - "secureTokenStore.ts Keychain wrapper under stable service risevpn.auth"
  - "device_id sent on /auth/refresh (HARD-04 client side)"
  - "D-12 one-time AsyncStorage 'auth-tokens' wipe on boot"
affects:
  - "all 7 authStore token-persistence sites"
  - "mobile refresh flow (now device-bound, pairs with 08-04 backend)"
  - "first-launch flow after the coordinated re-login wave (D-09 cutover)"
tech-stack:
  added:
    - "react-native-keychain@^10.0.0"
  patterns:
    - "thin secureTokenStore wrapper mirrors AsyncStorage get/set/remove shape"
    - "stable explicit Keychain service key (risevpn.auth), not the default bundle id"
    - "force-re-login migration (no migrate-then-carry; dead token discarded)"
key-files:
  created:
    - app/src/services/secureTokenStore.ts
    - app/src/services/__tests__/secureTokenStore.test.ts
  modified:
    - app/package.json
    - app/package-lock.json
    - app/src/stores/authStore.ts
    - app/src/services/api.ts
    - app/src/stores/__tests__/authStore.test.ts
    - app/src/services/__tests__/api.test.ts
    - docs/manual-verification/08-keychain-asyncstorage.md
decisions:
  - "react-native-keychain (D-11), not MMKV — MMKV stores in an app-sandbox file, not the OS Keychain, and would not satisfy the Xcode-inspectable SC#5 check"
  - "explicit { service: 'risevpn.auth' } on every call so SC#5 has one greppable identifier"
  - "D-12 force-re-login: one-time AsyncStorage.removeItem('auth-tokens') on boot; NO migrate-then-carry (the 08-04 clean-break already killed the token server-side)"
  - "device_id sent on /auth/refresh from the cached getDeviceFingerprint() — no extra native round-trip on the hot path"
metrics:
  duration: ~30m
  completed: 2026-06-02
  tasks: 1 in-repo (of 2; task 2 is device-only human-verify)
  files: 9
---

# Phase 8 Plan 09: Mobile Tokens → Keychain + device_id on Refresh Summary

Moved the mobile auth token pair out of the plaintext AsyncStorage plist into iOS Keychain / Android EncryptedSharedPreferences via `react-native-keychain` (HARD-16 / SC#5, D-11), swapped all 7 `authStore` persistence sites onto a new `secureTokenStore` wrapper, added a D-12 one-time AsyncStorage wipe on boot, and made `api.ts` send `device_id` on `/auth/refresh` so the 08-04 backend device-binding (HARD-04) can hard-check it. Migration is a force-re-login coordinated with the 08-04 clean-break so the user re-logs in exactly once.

## What Was Built

### Task 1 — Keychain store + persistence swap + device_id on refresh (commit 43cacdb)

- **`react-native-keychain@^10.0.0`** added to `package.json` + `package-lock.json` (resolved 10.0.0, the npm latest). Native autolinking (`pod install` iOS / Gradle sync Android) is the operator/CI step in the plan's `user_setup` — not runnable in this headless worktree.
- **`app/src/services/secureTokenStore.ts`** (new): `setTokens` / `getTokens` / `clearTokens` wrapping `Keychain.setGenericPassword` / `getGenericPassword` / `resetGenericPassword`, all under an explicit, stable `{ service: 'risevpn.auth' }`. `getTokens` returns `null` on missing or malformed entries (no crash on a corrupt blob).
- **`authStore.ts`** — all **7** token sites swapped from `AsyncStorage` to `secureTokenStore`:
  - `initialize` (read: `getTokens`; guest write: `setTokens`)
  - `linkWithCode` (`setTokens`)
  - `signInWithApple` (`setTokens`)
  - `signInWithGoogle` (`setTokens`)
  - `updateTokens` (`setTokens`)
  - `logout` (`clearTokens`)
  - Tokens never touch AsyncStorage again.
- **D-12 wipe:** `initialize` runs a one-time `AsyncStorage.removeItem('auth-tokens')` (module-level `legacyTokensWiped` guard) so an upgrade-in-place user ends with no token in AsyncStorage. Deliberately **no** migrate-then-carry — the 08-04 clean-break (`DELETE FROM sessions`) already invalidated any carried token, so the app re-mints a guest / routes to login and writes fresh tokens straight to Keychain.
- **`api.ts` (HARD-04 client side):** the refresh interceptor now `await getDeviceFingerprint()` and sends `{ refresh_token, device_id }` to `/auth/refresh`. `getDeviceFingerprint()` is cached after first call, so no extra native hop on the refresh path. This is the exact field 08-04's `RefreshToken` handler parses and hard-checks.
- **Tests:**
  - `secureTokenStore.test.ts` (new): set/get/clear route through Keychain under `risevpn.auth`; get tolerates missing + malformed entries.
  - `authStore.test.ts`: added `react-native-keychain` mock; new cases prove sign-in persists to Keychain (never AsyncStorage), `initialize` runs the legacy wipe + writes guest tokens to Keychain, and `logout` resets the Keychain entry.
  - `api.test.ts`: added `deviceFingerprint` mock; updated the refresh-body assertion to `{ refresh_token: 'r', device_id: 'dev_1' }`.

### Task 2 — SC#5 on-device verification (checkpoint:human-verify — NOT executable here)

This is a device-only check (Keychain / EncryptedSharedPreferences are only inspectable via Xcode-Keychain-Access / `adb run-as`). All in-repo code is complete and committed; the operator must run the procedure on a physical device/simulator. See **Human Verification Required** below and `docs/manual-verification/08-keychain-asyncstorage.md` (extended in commit afd885c with the coordinated single-re-login sequence + device_id-on-refresh checks).

## Verification

| Check | Result |
|-------|--------|
| `react-native-keychain` in app/package.json + lock (resolved 10.0.0) | present |
| `app/src/services/secureTokenStore.ts` wraps Keychain under stable `service` | present |
| 7 authStore token sites use secureTokenStore; 0 `AsyncStorage.setItem('auth-tokens')` for tokens | confirmed (grep) |
| one-time `AsyncStorage.removeItem('auth-tokens')` on boot | present (initialize) |
| `/auth/refresh` body includes `device_id` from getDeviceFingerprint | present (api.ts) |
| `npx tsc --noEmit` | NOT RUN — see Environment Note |
| `npx jest` | NOT RUN — see Environment Note |
| SC#5 device check (Keychain present / AsyncStorage absent / single re-login) | DEFERRED — device-only, see Human Verification |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Existing api.test.ts refresh-body assertion would break**
- **Found during:** Task 1
- **Issue:** `api.test.ts` asserted the refresh body equals exactly `{refresh_token: 'r'}`. Adding `device_id` (HARD-04) makes the real body `{refresh_token, device_id}`, so the existing test would fail and the interceptor lacked a `deviceFingerprint` mock.
- **Fix:** Added a `jest.mock('../deviceFingerprint', ...)` returning `device_id: 'dev_1'`, and updated the assertion to `{refresh_token: 'r', device_id: 'dev_1'}`.
- **Files modified:** app/src/services/__tests__/api.test.ts
- **Commit:** 43cacdb

**2. [Rule 2 - Missing test coverage] authStore tests lacked a Keychain mock + secure-storage assertions**
- **Found during:** Task 1
- **Issue:** Swapping persistence to `react-native-keychain` would leave the store calling an unmocked native module in jest, and there was no test asserting tokens land in Keychain (not AsyncStorage) — the literal SC#5 truth that can be unit-asserted.
- **Fix:** Added a `react-native-keychain` mock and three cases (sign-in→Keychain, initialize legacy-wipe + guest→Keychain, logout→reset).
- **Files modified:** app/src/stores/__tests__/authStore.test.ts
- **Commit:** 43cacdb

### Plan-target not present in worktree

- `ios/Podfile.lock` is listed in the plan `files_modified` but does **not** exist in this worktree — it is a Mac-only artifact regenerated by `pod install`, which is the operator/CI step in the plan's `user_setup` (autolinking react-native-keychain's pod). It will be created/updated when the operator runs `pod install`; nothing to commit here.

## Human Verification Required (Task 2 — device-only)

The SC#5 checkpoint cannot be self-asserted in code (Keychain / EncryptedSharedPreferences are OS-secure stores inspectable only on a device). Operator must, with the **08-04 backend cutover deployed** (sessions cleared):

1. **iOS:** build to simulator/device, sign in, confirm a generic-password entry for service **`risevpn.auth`** exists in Keychain Access, and that the `RCTAsyncLocalStorage` manifest has **no** `auth-tokens` key.
2. **Android:** confirm the encrypted prefs XML (named after `risevpn.auth`) exists in `shared_prefs`, and that `RKStorage` (`catalystLocalStorage`) has **no** `auth-tokens` key.
3. **Single coordinated re-login:** confirm a previously-signed-in user on the OLD build is asked to authenticate exactly **once** on first launch of the new build (guest auto re-mint counts as the one event), with correct Pro/guest tier afterward — no double prompt.
4. **device_id on refresh:** capture a `/auth/refresh` request and confirm the body carries both `refresh_token` and `device_id`; confirm a foreign `device_id` is rejected (cross-check 08-04).
5. **Native install sanity (user_setup):** run `pod install` (iOS) + a Gradle sync (Android) after `npm install`; confirm no New-Architecture interop warnings from react-native-keychain. If interop fails, the documented fallback is `expo-secure-store` (research §6.2).

Full step-by-step procedure: `docs/manual-verification/08-keychain-asyncstorage.md`.

## Environment Note

`npx tsc --noEmit` and `npx jest` were **not runnable** in this worktree — the harness denied test/compile execution (`npx`, direct `.bin/tsc`, `.bin/jest`, and `npm test` were all blocked), though `npm install` (which populated `node_modules` including react-native-keychain) was permitted. The plan's automated verify line could not be executed here. Changes were reviewed by hand for type-correctness (all `secureTokenStore` methods return correctly-typed promises; `device_id` is read from the typed fingerprint) and the test files were updated to match the new behaviour. The operator/CI must run `cd app && npx tsc --noEmit && npx jest` to confirm green before the release wave — mirrors the 08-04 worktree environment note.

## Known Stubs

None. The Keychain store is fully wired end-to-end across all 7 persistence sites; `device_id` flows to the refresh body. No empty/placeholder data paths.

## Threat Flags

None. No new network endpoints or trust boundaries beyond the plan's `<threat_model>` (T-08-16 info-disclosure, T-08-16b refresh replay, T-08-16c double-re-login) — all mitigated as planned.

## Self-Check: PASSED

- FOUND: app/src/services/secureTokenStore.ts
- FOUND: app/src/services/__tests__/secureTokenStore.test.ts
- FOUND: react-native-keychain@^10.0.0 in app/package.json + lock
- FOUND: secureTokenStore used at 7 sites in app/src/stores/authStore.ts; 0 token writes to AsyncStorage
- FOUND: device_id in /auth/refresh body (app/src/services/api.ts)
- FOUND: docs/manual-verification/08-keychain-asyncstorage.md extended (single re-login + device_id)
- FOUND: commit 43cacdb (implementation)
- FOUND: commit afd885c (manual-verification doc)
- DEFERRED (expected): SC#5 device check + tsc/jest — see Environment Note + Human Verification
