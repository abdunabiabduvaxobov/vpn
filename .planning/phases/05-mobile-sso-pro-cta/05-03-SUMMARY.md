---
phase: 05-mobile-sso-pro-cta
plan: 03
subsystem: mobile-sso-pro-cta-ui
tags: [ui, login, payment, informational-cta, deep-link-modal, polling, interstitial, account-sync, i18n, wave-3]
requires:
  - "05-01 native SSO infra (libs pinned, iOS Bundle ID, vpnapp:// routing, PLACEHOLDER_* OAuth sentinels)"
  - "05-02 service layer + authStore (signInWithApple/Google, getInvoice, upgradeUrlForLocale, pendingInvoiceId/isActivatingPro, start/stopActivatingPro, registerDeepLinkHandler, configureGoogleSignIn)"
provides:
  - "LoginScreen — navigable Stack.Screen with Apple (iOS-only) + Google + Guest CTAs; silent cancellation (D-02/D-05)"
  - "PaymentScreen rewrite — informational layout, single 'Upgrade to Pro at risevpn.com' CTA, zero prices, NO IAP, tertiary 'Already paid? Refresh' (D-13/D-14/D-15)"
  - "LeavingAppSheet — D-12 interstitial Modal shown BEFORE Linking.openURL"
  - "ActivatingProModal — root-level polling overlay, 2000ms cadence, ?escalate=true from poll #6, 30s/15-poll timeout → takingLonger (D-07/D-08/D-10/D-11)"
  - "AccountScreen 'Sign in to sync Pro' card — guest-only, routes to Login (D-03)"
  - "App.tsx wiring — configureGoogleSignIn() + registerDeepLinkHandler() at boot + <ActivatingProModal/> inside NavigationContainer"
  - "HomeScreen D-09 comment — foreground safety-net covered by existing fetchAccount()"
  - "EN + RU i18n key namespaces (login.*, payment.upgrade.*, payment.activating.*, payment.takingLonger.*, account.signInToSync.*); stale payment.* keys deleted"
affects:
  - "Wave 4 release — version bump (version.test stays RED until APP_VERSION → 2.2.0)"
  - "Operator UAT (05-HUMAN-UAT.md) — live SSO + deep-link smoke once PLACEHOLDER_* creds filled"
  - "Phase 8 Stripe cleanup — mobile PaymentScreen no longer has any Stripe/Telegram-CTA path"
tech-stack:
  added: []
  patterns:
    - "RN core Modal for both interstitial (slide-from-bottom) and polling overlay (fade) — no sheet/animation dep added"
    - "cancelledRef + recursive setTimeout polling loop (not setInterval) so each tick awaits its getInvoice before scheduling the next"
    - "Pitfall-4 auth gate: ActivatingProModal effect early-returns until isAuthenticated, re-runs via dep array once guest JWT mints"
    - "act()-wrapped react-test-renderer renders (React 19 / RN 0.84 reportGlobalError requires it)"
    - "jest.mock factory variable prefixed mock* (mockStoreState) to satisfy babel-plugin-jest-hoist scope rule"
key-files:
  created:
    - app/src/screens/LoginScreen.tsx
    - app/src/components/LeavingAppSheet.tsx
    - app/src/components/ActivatingProModal.tsx
  modified:
    - app/App.tsx
    - app/src/navigation/RootNavigator.tsx
    - app/src/screens/PaymentScreen.tsx
    - app/src/screens/AccountScreen.tsx
    - app/src/screens/HomeScreen.tsx
    - app/src/i18n/en.json
    - app/src/i18n/ru.json
    - app/src/screens/__tests__/LoginScreen.test.tsx
    - app/src/screens/__tests__/PaymentScreen.test.tsx
    - app/src/components/__tests__/ActivatingProModal.test.tsx
decisions:
  - "App-test deferred (DEF-05-00-01): the ESM transform fix greened the @react-navigation SyntaxError, but App.tsx → HomeScreen → vpnBridge does new NativeEventEmitter(NativeModules.VpnModule) which is null under jest — deeper native cascade. Per phase notes' ~2-attempt budget + 'leave deferred if it risks destabilizing the suite', reverted jest.config/setup changes to keep the 9 gate suites green."
  - "Removed the flat payment.upgrade ('Upgrade') i18n string and reused payment.upgrade as the new nested namespace (UI-SPEC §Removed copy explicitly reassigns it; grep confirmed zero source callers)."
  - "PaymentScreen Pro-user 'Manage subscription' link intentionally NOT wired this plan — D-14 footnote: hide unless GET /subscription/manage-url is confirmed; left as documented comment, free-user path is the launch-critical surface."
metrics:
  tasks: 3
  files: 14
  commits: 3
  duration: "~12m"
  completed: 2026-05-29
---

# Phase 5 Plan 03: Mobile SSO + Pro CTA UI Summary

Landed the entire user-visible surface of Phase 5: a navigable `LoginScreen` (Apple iOS-only + Google + Guest, silent cancellation), an App-store-compliant `PaymentScreen` rewrite (informational only — single "Upgrade to Pro at risevpn.com" CTA, **zero prices, no IAP, no Telegram CTA**), the D-12 `LeavingAppSheet` interstitial, the D-07/D-08/D-10/D-11 `ActivatingProModal` root-level polling overlay (2000ms × 5 → `?escalate=true` → 30s/15-poll timeout), the D-03 AccountScreen "Sign in to sync Pro" guest card, the `App.tsx` boot wiring (Google config + deep-link handler + root modal), the D-09 HomeScreen foreground-safety-net comment, and all new EN + RU i18n keys with stale `payment.*` keys removed. The three Wave-0 UI stub suites (`LoginScreen`, `PaymentScreen`, `ActivatingProModal`) are now real, passing tests.

## What Was Built

### Task 1 — i18n + Login route + LoginScreen (commit `fc84a4d`)
- `en.json` + `ru.json`: added `login.*`, `payment.upgrade.*` (incl. `leaving.*`), `payment.activating.*`, `payment.takingLonger.*`, `account.signInToSync.*`. RU values = EN placeholders per UI-SPEC scope (operator translates separately). Removed the old flat `payment.upgrade` string to make room for the nested namespace.
- `RootNavigator.tsx`: `Login: undefined` added to `RootStackParamList`; `<Stack.Screen name="Login">` registered (D-02 navigable, NOT a route-gate).
- `LoginScreen.tsx`: three vertically-stacked CTAs — Apple (`Platform.OS === 'ios'` guard), Google, Guest (transparent + border). Silent cancellation: catches `appleAuth.Error.CANCELED` / `statusCodes.SIGN_IN_CANCELLED`, no Alert (per-provider toasts deferred). `goHome()` resets nav to Home on success (D-05 silent transition).
- `LoginScreen.test.tsx`: 2 passing — 3 CTAs on iOS, 2 on Android (Apple hidden).

### Task 2 — LeavingAppSheet + ActivatingProModal + App.tsx + HomeScreen (commit `9b48764`)
- `LeavingAppSheet.tsx`: full-screen slide-from-bottom `Modal`; title/body/Continue(primary)/Cancel(outline). Continue → `Linking.openURL(url)` then dismiss. The ONLY place `Linking.openURL` for the upgrade flow is invoked (CTA tap can't bypass it).
- `ActivatingProModal.tsx`: mounts on `isActivatingPro`. Constants `POLL_INTERVAL_MS=2000`, `MAX_POLLS=15`, `ESCALATE_AFTER=5`. States: polling (spinner, no dismiss), success (`fetchAccount` → 3s display → `stopActivatingPro`), failed (`navigate('Account')`), takingLonger (Refresh + Telegram `https://t.me/flawlssr` + Close). Pitfall-4 auth gate: early-returns until `isAuthenticated`, re-runs via dep array. `cancelledRef` + recursive `setTimeout` (not `setInterval`).
- `App.tsx`: `configureGoogleSignIn()` + `registerDeepLinkHandler()` in the boot `useEffect` (with cleanup unsubscribe); `<ActivatingProModal/>` rendered inside `<NavigationContainer>` (needs `useNavigation`).
- `HomeScreen.tsx`: D-09 comment block documenting that the existing `AppState.active` → `fetchAccount()` already covers the web-paid-then-foreground safety-net (no functional change).
- `ActivatingProModal.test.tsx`: 4 passing — 2000ms cadence, escalate after poll #5, paid→fetchAccount+stopActivatingPro, hidden when not activating.

### Task 3 — PaymentScreen rewrite + AccountScreen sync card + stale-key cleanup (commit `2369199`)
- `PaymentScreen.tsx`: full rewrite. Current-plan card (Free limits or "Your current plan: Pro"), "Pro includes" feature list (free users), single CTA "Upgrade to Pro at risevpn.com" → opens `LeavingAppSheet` (never direct `Linking.openURL`), tertiary "Already paid? Refresh" → `fetchAccount()` + toast. Deleted: 3-plan hardcoded cards, `SUPPORT_TELEGRAM`/`openTelegram`, all price strings.
- `AccountScreen.tsx`: "Sign in to sync Pro" card above the Subscription card, gated on `!user?.auth_provider || user.auth_provider === 'guest'`. Apple (iOS-only) + Google buttons → `navigation.navigate('Login')`. Card unmounts once `auth_provider` flips (D-05).
- Stale i18n removal: deleted `payment.{title,subtitle,mostPopular,currentPlan,contactSupport,yourId,idMissing,telegramMessage,telegramError,howItWorksTitle,howItWorksBody,errorTitle,errorMessage,disclaimer,plans.*}` from both locales; retargeted RootNavigator Payment title to `payment.upgrade.title`.
- `PaymentScreen.test.tsx`: 4 passing — locked CTA copy, zero `$\d`/`€\d`/`/mo`, refresh link present, no Telegram.

## Verification Results

```
$ cd app && npm test -- --testPathIgnorePatterns='version.test|App.test'
Test Suites: 9 passed, 9 total
Tests:       44 passed, 44 total
exit 0
  # 6 service/store suites (Wave 2) + 3 UI suites (this plan) all green;
  # the 3 former describe.skip UI stubs are now real passing assertions.

$ cd app && npx tsc --noEmit
TSC EXIT: 0   # fully clean

$ grep -rE "t\('payment\.(title|subtitle|mostPopular|...|plans\.)" app/src --include='*.ts*' | grep -v __tests__ | grep -v i18n
  → ZERO stale callers

$ grep -E '"(telegramMessage|mostPopular|howItWorksTitle|telegramError)"' app/src/i18n/en.json app/src/i18n/ru.json
  → ZERO stale keys

$ grep "ActivatingProModal\|configureGoogleSignIn\|registerDeepLinkHandler" app/App.tsx  → all present
$ grep "Login: undefined" app/src/navigation/RootNavigator.tsx  → present
```

`version.test.ts` stays intentionally RED until Wave 4 bumps `APP_VERSION` to `2.2.0` (excluded from the gate by design). `App.test.tsx` stays deferred — see Deferred Issues.

## Compliance Note (App Store / Play)

PaymentScreen has **no price text anywhere** (no `$`, no `€`, no `/mo`, no `4.99`/`9.99` — verified by grep against both the component and the `payment.*` i18n) and **no IAP code path**. The single CTA reads exactly "Upgrade to Pro at risevpn.com" and routes through the `LeavingAppSheet` interstitial before any browser handoff. `onUpgradeTap` only sets `sheetVisible=true`; `Linking.openURL` lives solely inside `LeavingAppSheet.onContinue`, so the interstitial cannot be bypassed (Apple-sheet-bypass threat mitigated structurally).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] react-test-renderer renders required `act()` wrapping (React 19 / RN 0.84)**
- **Found during:** Task 1 (LoginScreen test) and Task 3 (PaymentScreen test).
- **Issue:** The plan's verbatim tests did `TestRenderer.create(<Screen/>)` un-wrapped. Under this repo's React 19 + react-test-renderer, the synchronous render triggers a `reportGlobalError` path that calls `window.dispatchEvent` (absent in the node test env), crashing the suite with `TypeError: window.dispatchEvent is not a function` and "update not wrapped in act(...)".
- **Fix:** Wrapped each `TestRenderer.create` in `act(() => { ... })` (LoginScreen + PaymentScreen tests). Same pattern the plan already used for the ActivatingProModal test.
- **Files modified:** `app/src/screens/__tests__/LoginScreen.test.tsx`, `app/src/screens/__tests__/PaymentScreen.test.tsx`.
- **Commits:** `fc84a4d` (Login), `2369199` (Payment).

**2. [Rule 1 - Bug] jest.mock factory referenced an out-of-scope variable (`storeState`)**
- **Found during:** Task 2 (ActivatingProModal test suite failed to RUN).
- **Issue:** The plan's verbatim test declared `let storeState` and referenced it inside the `jest.mock('../../stores/authStore', ...)` factory. `babel-plugin-jest-hoist` rejects out-of-scope variable access in a hoisted mock factory unless the name is prefixed `mock` (case-insensitive). Error: `The module factory of jest.mock() is not allowed to reference any out-of-scope variables. Invalid variable access: storeState`.
- **Fix:** Renamed `storeState` → `mockStoreState` throughout the test (standard jest idiom). No behavior change.
- **Files modified:** `app/src/components/__tests__/ActivatingProModal.test.tsx`.
- **Commit:** `9b48764`.

### Sentinel Note (intentional)
`PLACEHOLDER_WEB` / `PLACEHOLDER_IOS` OAuth sentinels in `googleSignIn.ts` (Wave 1/2, DEF-05-CREDS) are untouched and intact — this plan adds no real client IDs.

## Deferred Issues

**App.test.tsx (DEF-05-00-01) — deferred, NOT fixed (2-attempt budget reached).**
- Attempt 1 (transform): extended `transformIgnorePatterns` to transpile `@react-navigation` + `react-native-safe-area-context` and added a `jest.setup.js` mocking AsyncStorage/NetInfo/yandex-mobile-ads. This resolved the original `export { createStaticNavigation }` ESM SyntaxError.
- Attempt 2 (native cascade): App.tsx → HomeScreen → `useVpnConnection` → `src/services/vpnBridge.ts` does `new NativeEventEmitter(NativeModules.VpnModule)` at import time, and `VpnModule` is null under jest → `Invariant Violation: new NativeEventEmitter() requires a non-null argument`. Greening this needs mocking the app's own native VPN bridge (+ likely the ads native module), a deeper multi-module mock chain.
- **Decision:** per the phase notes' explicit "do NOT spend more than ~2 attempts" + "leave deferred if it risks destabilizing the suite," I **reverted** the `jest.config.js` + `jest.setup.js` changes so the 9 gate suites + tsc stay clean and unchanged. App.test remains the same pre-existing RED it was at plan start. A future owner can green it by mocking `NativeModules.VpnModule` (NativeEventEmitter-compatible) alongside the transform/setup chain.

## Authentication Gates
None. (Live SSO won't authenticate until `PLACEHOLDER_*` client IDs are replaced at store upload — DEF-05-CREDS — a deferred operator credential task, not an execution-time gate.)

## Known Stubs
None introduced by this plan. The only `PLACEHOLDER_*` sentinels remain in `googleSignIn.ts` / native config (Wave 1/2, operator-authorized DEF-05-CREDS) and were not touched. The Pro-user "Manage subscription" link is intentionally a documented comment (D-14: hide until `GET /subscription/manage-url` is confirmed) — not a UI stub on the launch-critical free-user path.

## Threat Flags
No new security-relevant surface beyond the plan's `<threat_model>`. T-1 (deep-link spoofing) mitigated: modal renders 'success' only after backend `getInvoice` returns `status==='paid'`. Apple-sheet-bypass mitigated structurally (CTA only toggles sheet visibility; `Linking.openURL` is inside `LeavingAppSheet`). T-2/T-3 (token/auth-code) preserved: LoginScreen never logs or surfaces tokens; cancellation is silent. T-6: no new optional deps added (RN core primitives only).

## Self-Check: PASSED
- All 3 created files + 8 modified production/i18n files + 3 filled test files verified present on disk.
- All 3 task commits (`fc84a4d`, `9b48764`, `2369199`) verified in git history.
- STATE.md / ROADMAP.md NOT modified (orchestrator owns those writes, per objective).
