---
phase: 05-mobile-sso-pro-cta
type: review-fix
based_on: 05-REVIEW.md (2026-05-29 deep review — 0 critical, 5 warning, 6 info)
created: 2026-05-29
fixes_applied: 6
fixes_deferred: 5
commits: 7
final_tsc: pass
final_suite: 10 suites / 75 tests passing (App.test pre-existing-RED, excluded)
placeholder_sentinels: intact (DEF-05-CREDS)
state_roadmap_modified: false
---

# Phase 5 Review Fix Report

Gap-closure pass applying the high-value findings from the Phase 5 code review
(`05-REVIEW.md`) + the QA P0 test gaps to the mobile app, each locked with
regression tests. Every fix is an atomic commit (normal commits, hooks on).
STATE.md / ROADMAP.md were NOT modified. The `PLACEHOLDER_*` OAuth sentinels
(DEF-05-CREDS) remain intact.

## Summary

| # | Fix | Review ref | Commit | Status |
|---|-----|-----------|--------|--------|
| 1 | deep-link parser hardening | WR-01 + WR-02 + L-1 | `8affd1e` | Applied |
| 2 | guest CTA awaits real auth | WR-04 | `44f5d61` | Applied |
| 3 | SSO error handling + onGoogle catch | WR-05 | `3f2a2b5` | Applied |
| 4 | ActivatingProModal timer cleanup | WR-03 + perf-Low | `bcb7a58` | Applied |
| 5 | duplicate deep-link dedup | L-3 / perf-Low | `7418ea5` | Applied |
| — | QA P0-4 interceptor lock-in tests | QA P0-4 / T-7 | `caee123` | Applied |
| 6 | dependency advisory (`npm audit fix`) | dep advisory | `fb1fd7c` | Applied (non-breaking) |

Baseline before fixes: 10 suites / 45 tests. After fixes: 10 suites / **75 tests**
(+30 regression tests). `npx tsc --noEmit` exit 0 throughout.

---

## Fixes Applied

### FIX 1 — Deep-link parser (WR-01 + WR-02 + L-1) — launch-critical — `8affd1e`

**File:** `app/src/services/deepLink.ts` (+ `__tests__/deepLink.test.ts`)

Rewrote `parseInvoiceFromUrl`:
- **(a) WR-02 + L-1** — splits the URL on the FIRST `?` and EXACT-matches the
  base path `vpnapp://payment/success`. Look-alikes `success-evil`,
  `successfully`, `success/..`, wrong scheme, and wrong host now all return
  `null` (previously a `startsWith` prefix match accepted them).
- **(b) WR-01** — parses ALL `key=value` pairs and finds `invoiceId` anywhere
  in the query string. Both `?invoiceId=X` and `?status=ok&invoiceId=X` /
  `?utm_source=lava&invoiceId=X` now resolve (previously only the FIRST param
  matched, silently breaking the launch-critical "Pro unlocks immediately" path
  on any URL shape with a leading param).
- **(c)** — URL-decodes the value, strips any trailing `#fragment`, and
  validates a UUID shape (`/^[0-9a-f-]{36}$/i`); returns `null` otherwise. The
  invoiceId remains UNTRUSTED (T-1) — this is a cheap shape gate before the
  backend `getInvoice` poll, not a security check.

**Tests (deepLink.test.ts → 22 cases):** any-position invoiceId (`?status=ok&…`,
`?utm_source=…`), success-evil / successfully / success/.. → null, wrong host →
null, non-UUID / disallowed-char invoiceId → null, and the dispatch wiring
(QA P0-2): valid cold-start `getInitialURL` → `startActivatingPro(uuid)`;
captured `addEventListener('url')` callback with a valid URL → dispatch;
malformed cold-start and warm URLs → NOT dispatched.

> Note: pre-existing tests that used non-UUID ids (`abc123`, `enc%20oded`) were
> updated to valid UUID-shape fixtures to match the new (c) contract — this is
> intended tightening, not a regression.

### FIX 2 — Guest CTA awaits real auth completion (WR-04) — `44f5d61`

**Files:** `app/src/stores/authStore.ts`, `app/src/screens/LoginScreen.tsx`

- `initialize` changed from `() => void` (fire-and-forget IIFE) to
  `async (): Promise<void>` that settles once guest auth has completed (tokens
  set or failed). The App-boot call site (`App.tsx`) and the api.ts refresh-
  fallback call sites invoke it fire-and-forget and ignore the return value, so
  they are unaffected. The separate `settingsStore.initialize` is untouched.
- `LoginScreen.onGuest` now awaits `initialize()` and gates `goHome()` on the
  **live** store `isAuthenticated` (the reactive closure value was stale inside
  the async handler). On failure (e.g. offline) it surfaces a non-fatal
  `Alert(t('login.signInFailed'))` and stays on LoginScreen instead of landing
  on Home unauthenticated.

**Tests (LoginScreen.test.tsx):** guest path awaits `initialize()` when not yet
authenticated; guest auth failure → Alert + no navigation.

### FIX 3 — SSO error handling + onGoogle catch (WR-05) — `3f2a2b5`

**Files:** `app/src/screens/LoginScreen.tsx` (+ `__tests__/LoginScreen.test.tsx`)

- `onApple` / `onGoogle` stay SILENT on user cancellation
  (`appleAuth.Error.CANCELED` / `statusCodes.SIGN_IN_CANCELLED`) but now surface
  a non-fatal `Alert(t('login.signInFailed'))` for ANY non-cancellation error
  (network, backend 4xx/5xx, missing token). Neither path navigates Home on
  error.
- `onGoogle` previously had NO non-cancellation catch — a real error escaped the
  handler as an unhandled rejection. It now catches and surfaces.
- The i18n key `login.signInFailed` (plus `login.appleCancelled` /
  `login.googleCancelled`) already existed in both `en.json` and `ru.json`, so
  no new i18n keys were required.

**Tests:** Apple error → Alert; Apple cancellation → silent; Google error →
Alert; Google cancellation → silent.

### FIX 4 — ActivatingProModal timer cleanup (WR-03 + perf-Low) — `bcb7a58`

**File:** `app/src/components/ActivatingProModal.tsx` (+ its test)

- Tracks the 3s success `setTimeout` in `successTimerRef` and `clearTimeout`s
  it in the effect cleanup, so it can no longer fire `setModalState` /
  `stopActivatingPro` on an unmounted component (set-state-after-unmount).
- Guards the polling-loop success-timer callback and the `onRefresh` success
  branch with `cancelledRef` (the `onRefresh` branch previously had no guard at
  all, and added the same tracked/cleared timer).

**Tests:** `status==='failed'` → failed state + polling stops; advancing through
the 15-poll budget → `takingLonger` + no 16th poll; unmount mid-poll → no
further `getInvoice` and no unmounted-setState `console.error` warning; unmount
within the 3s success window → `stopActivatingPro` NOT fired after unmount.

### FIX 5 — Duplicate deep-link dedup (L-3 / perf-Low) — `7418ea5`

**File:** `app/src/stores/authStore.ts` (+ its test)

- `startActivatingPro(invoiceId)` no-ops when
  `isActivatingPro === true && pendingInvoiceId === invoiceId`, preventing two
  overlapping polling loops when the OS delivers the same deep link twice
  (cold-start `getInitialURL` + a warm `url` event for the same launch). A
  DIFFERENT invoice id still re-arms the modal.

**Tests:** duplicate-id call produces ZERO state writes (verified via
`store.subscribe`); different-id call re-arms; plus the QA P0-1 mechanism test.

### QA P0-1 — guest token not cleared before /auth/apple POST — `7418ea5`

**File:** `app/src/stores/__tests__/authStore.test.ts`

Added a test asserting that with a guest token seeded in the store,
`signInWithApple()` still has that guest token present (un-cleared) at the
moment the `/auth/apple` POST fires, and `AsyncStorage.removeItem` was NOT
called beforehand. This locks the D-06 in-place-promotion mechanism (SC2) the
review called out as the key continuity invariant — no code change needed, the
behavior was already correct; this regression-locks it.

### QA P0-4 — api 401 interceptor T-7 short-circuit lock-in — `caee123`

**File:** `app/src/services/__tests__/api.test.ts`

Drives a synthetic 401 through the registered response-error interceptor:
- `config._skipAuthRefresh=true` → `/auth/refresh` NOT called.
- `config.url='/auth/google'` → no refresh.
- `config.url='/account'` + valid refresh token → exactly one `/auth/refresh`
  call with the correct body, `updateTokens` called with the rotated tokens,
  and the retried request carries `Authorization: Bearer NEW_AT`.

Test-only commit (no production change) — locks the existing T-7 behavior.

### FIX 6 — Dependency advisory (`npm audit fix`, no --force) — `fb1fd7c`

**File:** `app/package-lock.json` (package.json UNCHANGED)

Ran `cd app && npm audit fix` (no `--force`). It applied non-breaking transitive
dependency bumps only — `package.json` dependency ranges were NOT changed.

- **Cleared** the runtime `follow-redirects` moderate advisory
  (GHSA-r4q5-vmmm-2653 — Authorization-header leak to cross-domain redirect
  targets via axios), plus the high-severity `lodash` (GHSA-r5fr-rjxr-66jc,
  GHSA-f23m-r3pf-42rh) and `picomatch` (GHSA-3v7f-55p6-f55p, GHSA-c2c7-rcm5-vvqj)
  advisories.
- Advisory count: **15 → 7**.
- The remaining **7 moderate** advisories (`fast-xml-parser` pulled in by
  `@react-native-community/cli-*` and a `qs`/`body-parser` dev chain) require
  `npm audit fix --force`, which would install
  `@react-native-community/cli@20.1.3` "outside the stated dependency range" — a
  BREAKING/major RN CLI change. Per the instructions, `--force` was NOT run;
  these are dev-only (build tooling, not shipped in the app bundle) and are left
  for Phase 8 hardening.
- Re-verified `npx tsc --noEmit` exit 0 and the full suite (75 tests) green
  after the lockfile change — nothing broke.

---

## Deferred — NOT fixed (recorded with rationale)

These were explicitly out of scope for this pass; recorded here so they are not
lost. None were changed.

| Ref | Item | Rationale / owner |
|-----|------|-------------------|
| **M-2** | `android:usesCleartextTraffic="true"` app-wide | Phase 8 hardening — needs a `network_security_config.xml` to scope cleartext to the local dev API only. Pre-existing VPN/local-dev setting, not introduced by Phase 5. |
| **L-2** | `QUERY_ALL_PACKAGES` permission | Play Console data-safety documentation task. Pre-existing; not a code defect. |
| **L-4** | AsyncStorage plaintext tokens | Phase 8 HARD-15 — already tracked; project ships `react-native-mmkv` as the migration target for encrypted token storage. |
| **QA P1/P2** | Test gaps beyond the P0 lock-in tests | Follow-up — not implemented now per scope. The P0 tests (P0-1..P0-4) covering the fixed code paths are all in place. |

### Review info items intentionally NOT actioned (out of this pass's scope)

- **IN-01** (`signInWithGoogle` `as any` cast), **IN-05** (`as any` error casts in
  AccountScreen), **IN-06** (loose `isPro` truthy) — cosmetic typing nits, no
  behavior impact.
- **IN-02 / IN-03** (`Invoice.status` union omits backend reconciliation states;
  `expired` keeps polling instead of terminating) — robustness nits bounded by
  the 15-poll budget. Not in the FIX 1–6 list; left for a future contract-sync
  pass against the backend Phase 2/4 invoice state machine. The modal is
  poll-count-bounded so the worst case is a `takingLonger` after ~30s.
- **IN-04** (poll budget is count-based, comment says "30s") — documentation
  accuracy nit; the escalate transition is correct and tested.

---

## Verification

```
$ cd app && npx tsc --noEmit
exit 0   # fully clean

$ cd app && npm test -- --testPathIgnorePatterns='App.test'
Test Suites: 10 passed, 10 total
Tests:       75 passed, 75 total   (was 45 at baseline; +30 regression tests)
exit 0

$ grep -rn "PLACEHOLDER_" app/src
  googleSignIn.ts: PLACEHOLDER_WEB / PLACEHOLDER_IOS sentinels intact (DEF-05-CREDS)

$ git diff --name-only 04e38ac..HEAD | grep -E "STATE.md|ROADMAP.md"
  (none — STATE.md / ROADMAP.md NOT modified)
```

`App.test.tsx` remains pre-existing-RED (DEF-05-00-01 — native VPN bridge
`NativeEventEmitter` is null under jest) and is excluded from the gate by
design, as in the original Phase 5 plans.

## Commits

| Commit | Message |
|--------|---------|
| `8affd1e` | fix(05): harden deep-link parser — exact path, any-position invoiceId, UUID gate |
| `44f5d61` | fix(05): make guest CTA await real auth completion before navigating |
| `3f2a2b5` | fix(05): surface non-fatal SSO errors, add onGoogle catch (WR-05) |
| `bcb7a58` | fix(05): clear ActivatingProModal success timer on unmount (WR-03) |
| `7418ea5` | fix(05): dedup duplicate deep-link delivery in startActivatingPro (L-3) |
| `caee123` | test(05): lock the api 401 interceptor T-7 short-circuit (QA P0-4) |
| `fb1fd7c` | chore(05): npm audit fix (no --force) — clear follow-redirects + lodash/picomatch |
