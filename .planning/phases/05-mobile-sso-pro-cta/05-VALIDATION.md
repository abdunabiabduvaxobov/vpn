---
phase: 5
slug: mobile-sso-pro-cta
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-26
---

# Phase 5 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Derived from `05-RESEARCH.md` §"Validation Architecture" (lines 691–760).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Jest 29.6.3 + `preset: 'react-native'` (already in tree) + `react-test-renderer` 19.2.0 |
| **Config file** | `app/jest.config.js` (existing, single-line preset) |
| **Quick run command** | `cd app && npm test -- --testPathPattern=<file>` |
| **Full suite command** | `cd app && npm test` |
| **Type-check command** | `cd app && npx tsc --noEmit` |
| **Lint command** | `cd app && npm run lint` |
| **Estimated runtime** | quick <5s · full <60s · tsc <20s |

**Strategy choice:** Option A from RESEARCH.md — stay with `react-test-renderer` for shallow component tests, no new RNTL dependency. RNTL is deferred to Phase 8 (HARD-15).

---

## Sampling Rate

- **After every task commit:** Run `cd app && npm test -- --testPathPattern=<file-under-test>` (<5s)
- **After every plan wave:** Run `cd app && npm test` (full suite, <60s) + `cd app && npx tsc --noEmit` (<20s)
- **Before `/gsd-verify-work`:** Full suite green + lint clean + manual Android smoke per D-19 in `05-HUMAN-UAT.md`
- **Max feedback latency:** 5 seconds (per-task) · 80 seconds (per-wave)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 5-W0-01 | 00 | 0 | APP-01..07 | — | Wave-0 test scaffolding | unit | `npm test` (empty stubs pass) | ❌ W0 | ⬜ pending |
| 5-W0-02 | 00 | 0 | APP-01 | T-6 | Apple SDK manual mock | unit | `npm test` | ❌ W0 | ⬜ pending |
| 5-W0-03 | 00 | 0 | APP-02 | T-6 | Google SDK manual mock | unit | `npm test` | ❌ W0 | ⬜ pending |
| 5-SVC-01 | service | 2 | APP-01 | T-2, T-7 | Apple sign-in returns identityToken; POSTs `/auth/apple` with guest JWT in `Authorization` header; `_skipAuthRefresh: true` | unit | `npm test -- --testPathPattern=authStore.test` | ❌ W0 | ⬜ pending |
| 5-SVC-02 | service | 2 | APP-01 | T-2 | Apple cancellation surfaces silently (no Alert) | unit | `npm test -- --testPathPattern=authStore.test` | ❌ W0 | ⬜ pending |
| 5-SVC-03 | service | 2 | APP-02 | T-2, T-7 | Google sign-in returns idToken; POSTs `/auth/google` with guest JWT in `Authorization` header | unit | `npm test -- --testPathPattern=authStore.test` | ❌ W0 | ⬜ pending |
| 5-SVC-04 | service | 2 | APP-02 | T-6 | `GoogleSignin.configure()` called at app boot with correct `webClientId` | unit | `npm test -- --testPathPattern=googleSignIn.test` | ❌ W0 | ⬜ pending |
| 5-SVC-05 | service | 2 | APP-04 | T-2 | `signInWithApple`/`signInWithGoogle` sends `Authorization: Bearer <guest JWT>` when guest tokens exist (in-place promotion) | unit | `npm test -- --testPathPattern=authStore.test` | ❌ W0 | ⬜ pending |
| 5-SVC-06 | service | 2 | APP-06 | T-1, T-5 | `deepLink.ts` parses `vpnapp://payment/success?invoiceId=X`; dispatches `startActivatingPro` | unit | `npm test -- --testPathPattern=deepLink.test` | ❌ W0 | ⬜ pending |
| 5-SVC-07 | service | 2 | APP-05 | T-3 | `upgradeUrlForLocale('ru')` → `https://risevpn.com/ru/pricing?return=app`; default returns `/en/` | unit | `npm test -- --testPathPattern=payment.test` | ❌ W0 | ⬜ pending |
| 5-SVC-08 | service | 2 | APP-06 | T-1 | `getInvoice(id)` calls `GET /invoices/{id}`; appends `?escalate=true` after threshold | unit | `npm test -- --testPathPattern=payment.test` | ❌ W0 | ⬜ pending |
| 5-SVC-09 | service | 2 | APP-01, APP-02 | T-7 | axios interceptor short-circuits 401→refresh for `/auth/*` (uses `_skipAuthRefresh` flag) | unit | `npm test -- --testPathPattern=api.test` | ❌ W0 | ⬜ pending |
| 5-UI-01 | ui | 3 | APP-03 | — | LoginScreen renders Apple+Google+Guest on iOS; Google+Guest on Android (Apple hidden) | unit (shallow) | `npm test -- --testPathPattern=LoginScreen.test` | ❌ W0 | ⬜ pending |
| 5-UI-02 | ui | 3 | APP-05 | T-1 | PaymentScreen renders no price text; CTA copy exactly "Upgrade to Pro at risevpn.com"; opens LeavingAppSheet before `Linking.openURL` | unit (shallow) | `npm test -- --testPathPattern=PaymentScreen.test` | ❌ W0 | ⬜ pending |
| 5-UI-03 | ui | 3 | APP-06 | T-1 | ActivatingProModal polls every 2s; escalates after poll #5; times out at 30s | unit (fake timers) | `npm test -- --testPathPattern=ActivatingProModal.test` | ❌ W0 | ⬜ pending |
| 5-UI-04 | ui | 3 | APP-06 | — | On `status='paid'`, modal calls `fetchAccount()` and closes; on `'failed'` navigates to Account | unit | `npm test -- --testPathPattern=ActivatingProModal.test` | ❌ W0 | ⬜ pending |
| 5-VER-01 | release | 4 | APP-07 | — | `APP_VERSION === '2.2.0'`; package.json version 2.2.0; Android `versionName` + iOS `MARKETING_VERSION` aligned | unit + grep | `npm test -- --testPathPattern=version.test` + grep | ❌ W0 | ⬜ pending |
| 5-VER-02 | release | 4 | APP-07 | T-6 | Android signed `.aab` builds successfully (smoke) | manual + CLI | `cd app/android && ./gradlew bundleRelease` | n/a | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

**Mocking strategy (RESEARCH.md §734-739):**
- `@invertase/react-native-apple-authentication` → Jest manual mock at `app/__mocks__/@invertase/react-native-apple-authentication.ts`
- `@react-native-google-signin/google-signin` → Jest manual mock at `app/__mocks__/@react-native-google-signin/google-signin.ts`
- `react-native` `Linking` → react-native preset auto-mock; override per-test
- `axios` via `app/src/services/api.ts` → mock at module boundary
- `jest.useFakeTimers()` for polling loop tests

---

## Wave 0 Requirements

- [ ] `app/src/services/__tests__/appleSignIn.test.ts` — happy path + cancellation
- [ ] `app/src/services/__tests__/googleSignIn.test.ts` — configure called once; signIn returns idToken
- [ ] `app/src/services/__tests__/deepLink.test.ts` — URL parser edge cases (missing invoiceId, wrong scheme, encoded values)
- [ ] `app/src/services/__tests__/payment.test.ts` — `upgradeUrlForLocale` + `getInvoice` escalate query
- [ ] `app/src/services/__tests__/api.test.ts` — `_skipAuthRefresh` short-circuits 401 interceptor
- [ ] `app/src/stores/__tests__/authStore.test.ts` — `signInWithApple` + `signInWithGoogle` + `startActivatingPro` / `stopActivatingPro`
- [ ] `app/src/screens/__tests__/LoginScreen.test.tsx` — three CTAs visible iOS, two on Android
- [ ] `app/src/screens/__tests__/PaymentScreen.test.tsx` — informational layout, single CTA, locale-aware URL
- [ ] `app/src/components/__tests__/ActivatingProModal.test.tsx` — polling loop with fake timers
- [ ] `app/src/config/__tests__/version.test.ts` — `APP_VERSION === '2.2.0'`
- [ ] `app/__mocks__/@invertase/react-native-apple-authentication.ts` — manual mock
- [ ] `app/__mocks__/@react-native-google-signin/google-signin.ts` — manual mock
- [ ] **Framework install:** none — Jest + react-test-renderer already in tree

---

## Manual-Only Verifications

> These cases cannot be exercised in Jest (real SSO sheets, system browser, signed Android build). Recorded in `05-HUMAN-UAT.md` per D-19/D-20.

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| iOS tester taps "Continue with Apple", completes Apple sheet, lands on Home (SC#1 verbatim) | APP-01 | Apple sheet is a system UI — cannot be invoked from Jest | Recorded in 05-HUMAN-UAT.md; deferred per D-20 (no iOS hw on operator's machine) |
| Android: Google sign-in works on physical device; guest → Google promotion preserves `users.id` | APP-02, APP-04 | Real Google Play Services + real device required | Operator runs per D-19; verify admin panel shows one user row with `auth_provider: google` |
| Android: tap interstitial "Continue" → opens Chrome to `risevpn.com/<locale>/pricing?return=app` | APP-05 | System browser launch | 05-HUMAN-UAT.md |
| Android: typing `vpnapp://payment/success?invoiceId=test123` in Chrome opens app to ActivatingProModal | APP-06 | Cross-app deep-link round trip | 05-HUMAN-UAT.md |
| Android: signed `.aab` uploads to Play Internal Track | APP-07 | Play Console upload | 05-HUMAN-UAT.md |
| iOS: TestFlight build uploaded (deferred) | APP-07 | Xcode + Apple Developer membership | Deferred to end-of-milestone phase per D-20 |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (12 stubs + 2 mocks)
- [ ] No watch-mode flags (Jest runs single-pass)
- [ ] Feedback latency < 5s per-task / < 80s per-wave
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
