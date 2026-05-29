---
phase: 05-mobile-sso-pro-cta
plan: 02
subsystem: mobile-sso-service-layer
tags: [sso, apple-signin, google-signin, deep-link, payment, axios-interceptor, authstore, zustand, wave-2]
requires:
  - "05-00 Wave-0 Jest mocks + describe.skip stub tests (shape-exact to the SSO libs)"
  - "05-01 native SSO infra (libs installed + pinned, iOS Bundle ID, vpnapp:// routing, server_client_id)"
provides:
  - "appleSignIn.signInWithApple() — wraps @invertase appleAuth.performRequest (LOGIN + FULL_NAME + EMAIL); throws on null identityToken"
  - "googleSignIn.signInWithGoogle() + configureGoogleSignIn() — v16 userInfo.data.idToken read; PLACEHOLDER_* webClientId/iosClientId"
  - "deepLink.parseInvoiceFromUrl() (UNTRUSTED invoiceId, T-1) + registerDeepLinkHandler() (cold-start + warm url events -> startActivatingPro)"
  - "payment.upgradeUrlForLocale() (D-16 ru/en) + payment.getInvoice() (D-21 escalate); Stripe-era helpers DELETED"
  - "api.ts T-7 dual short-circuit (_skipAuthRefresh flag + /auth/* URL pattern) + AxiosRequestConfig module augmentation"
  - "authStore.signInWithApple/signInWithGoogle (D-06 in-place promotion) + pendingInvoiceId/isActivatingPro + start/stopActivatingPro"
  - "User type extended: auth_provider, email, email_verified (Phase 2 D-11 columns)"
affects:
  - "Wave 3 UI (LoginScreen, PaymentScreen rewrite, ActivatingProModal) — imports these service symbols + authStore actions/fields"
  - "Wave 3 App.tsx boot — must call configureGoogleSignIn() + registerDeepLinkHandler()"
  - "Phase 8 Stripe cleanup — mobile payment.ts no longer imports createCheckoutSession/cancelSubscription (coordinate via canonical refs)"
tech-stack:
  added: []
  patterns:
    - "Thin native-SDK service wrappers — forward token to backend, never persist (T-2)"
    - "Dual-defense axios 401 short-circuit: request-config flag + URL-pattern (T-7)"
    - "axios module augmentation (declare module 'axios') to type the _skipAuthRefresh request-config field"
    - "Zustand store extension mirroring the existing linkWithCode action shape for the two SSO actions"
    - "Deep-link invoiceId treated as UNTRUSTED — verification deferred to backend /invoices/:id polling (Wave 3)"
key-files:
  created:
    - app/src/services/appleSignIn.ts
    - app/src/services/googleSignIn.ts
    - app/src/services/deepLink.ts
  modified:
    - app/src/services/payment.ts
    - app/src/services/api.ts
    - app/src/stores/authStore.ts
    - app/src/types/api.ts
    - app/src/services/__tests__/appleSignIn.test.ts
    - app/src/services/__tests__/googleSignIn.test.ts
    - app/src/services/__tests__/deepLink.test.ts
    - app/src/services/__tests__/payment.test.ts
    - app/src/services/__tests__/api.test.ts
    - app/src/stores/__tests__/authStore.test.ts
decisions:
  - "googleSignIn.ts inlines PLACEHOLDER_WEB/PLACEHOLDER_IOS client IDs (DEF-05-CREDS operator-authorized sentinels) rather than inventing fake IDs — matches the native-config sentinels from Wave 1"
  - "AppleSignInResult.fullName widened to {givenName?: string|null; familyName?: string|null} to match the REAL @invertase AppleRequestResponseFullName (nullable), not the mock's non-null shape — fixes a latent tsc error in the plan's verbatim content"
  - "authStore extended in-place (D-CD) rather than a separate paymentReturnStore — two fields + two actions"
metrics:
  tasks: 3
  files: 13
  commits: 3
  duration: "~5m"
  completed: 2026-05-29
---

# Phase 5 Plan 02: Mobile SSO Service Layer Summary

Implemented the entire Phase 5 service-layer stack that bridges the Wave-1 native SDKs to the Wave-3 UI: three new service files (`appleSignIn.ts`, `googleSignIn.ts`, `deepLink.ts`), a `payment.ts` rewrite (dropped the Stripe-era `createCheckoutSession`/`cancelSubscription`, added locale-aware `upgradeUrlForLocale` + invoice-polling `getInvoice`), a T-7 dual short-circuit in the axios `api.ts` 401 interceptor (`_skipAuthRefresh` config flag + `/auth/*` URL pattern, with `AxiosRequestConfig` module augmentation), an `authStore` extension (`signInWithApple` + `signInWithGoogle` D-06 in-place-promotion actions plus `pendingInvoiceId`/`isActivatingPro` Activating-Pro modal state with `start`/`stopActivatingPro`), and a `User` type extension (`auth_provider`, `email`, `email_verified`). All six Wave-0 `describe.skip` stub suites for these surfaces were filled with real passing assertions.

## What Was Built

### Task 1 — Apple + Google sign-in service wrappers (commit `bc3a3d1`)
- `appleSignIn.ts` — `signInWithApple()` calls `appleAuth.performRequest({Operation.LOGIN, [Scope.FULL_NAME, Scope.EMAIL]})`, throws `Error('Apple sign-in did not return an identityToken')` on null token, returns `{identityToken, authorizationCode, user, fullName, email}`. Re-exports `appleAuth`. Exports `AppleSignInResult`.
- `googleSignIn.ts` — `configureGoogleSignIn()` calls `GoogleSignin.configure({webClientId, iosClientId, offlineAccess: false, scopes: ['email','profile']})`; `signInWithGoogle()` calls `hasPlayServices({showPlayServicesUpdateDialog: true})` then `signIn()`, reads `userInfo.data.idToken` (v16) with `?? userInfo.idToken` defensive fallback, throws on null. Re-exports `statusCodes`. Web/iOS client IDs are the operator-authorized `PLACEHOLDER_WEB`/`PLACEHOLDER_IOS` sentinels (DEF-05-CREDS).
- Filled `appleSignIn.test.ts` (4 tests) + `googleSignIn.test.ts` (4 tests) — 8 passing.

### Task 2 — deepLink + payment rewrite + api T-7 patch (commit `3c42a70`)
- `deepLink.ts` — `parseInvoiceFromUrl(url)` returns the URL-decoded `invoiceId` only for `vpnapp://payment/success?invoiceId=X`; returns `null` for wrong scheme, wrong path (`/payment/cancel`), missing query, or null input. `registerDeepLinkHandler()` queries `Linking.getInitialURL()` (cold start, with a `.catch` for the Android-reject case) AND subscribes to `Linking.addEventListener('url', ...)` (warm), each dispatching to `useAuthStore.getState().startActivatingPro(invoiceId)`; returns an unsubscribe that calls `sub.remove()`. T-1: the `invoiceId` is treated as UNTRUSTED.
- `payment.ts` rewrite — **deleted** `createCheckoutSession` + `cancelSubscription` + `CheckoutSession` (Stripe-era, D-14). **Added** `upgradeUrlForLocale(locale)` → `https://risevpn.com/{ru|en}/pricing?return=app` using `.toLowerCase().startsWith('ru')` (handles `ru-RU` regional variants per A10; ES→EN fallback); `getInvoice(id, escalate=false)` GETs `/invoices/:id` (+`?escalate=true` when escalate), returns `data.data`. Exports `Invoice`.
- `api.ts` patch — added `declare module 'axios'` augmentation typing `_skipAuthRefresh?: boolean` + `_retry?: boolean` on `AxiosRequestConfig`. The 401 interceptor now computes `skipAuthRefresh = originalRequest._skipAuthRefresh === true || requestUrl.startsWith('/auth/')` and the refresh branch only runs when `status === 401 && !_retry && !skipAuthRefresh`. The refresh logic itself (token rotation, queue, logout+initialize fallback) is unchanged — only the entry condition was widened.
- Filled `deepLink.test.ts` (8 tests) + `payment.test.ts` (10 tests) + `api.test.ts` (2 tests) — 20 passing.

### Task 3 — authStore extension + User type (commit `a6a7502`)
- `types/api.ts` — `User` gains `auth_provider?: 'guest'|'apple'|'google'`, `email?: string`, `email_verified?: boolean`.
- `authStore.ts` diff:
  - **Imports:** `performAppleSignIn`/`performGoogleSignIn` aliases + `ApiResponse` type.
  - **AuthState interface:** `+ pendingInvoiceId: string | null`, `+ isActivatingPro: boolean`, `+ signInWithApple`, `+ signInWithGoogle`, `+ startActivatingPro`, `+ stopActivatingPro`.
  - **Initial state:** `pendingInvoiceId: null`, `isActivatingPro: false`.
  - **signInWithApple():** invokes service-level Apple sign-in → `getDeviceFingerprint()` → joins `fullName` from `givenName`/`familyName` → `api.post('/auth/apple', {identity_token, authorization_code, device_id, device_secret, platform, full_name?, email?}, {_skipAuthRefresh: true})` → persists tokens to AsyncStorage → `set({tokens, isAuthenticated: true, isLoading: false, user: null})` → `fetchAccount()`. Does NOT clear the guest token first (D-06 in-place promotion — the request interceptor attaches the guest JWT). Re-throws so the Wave-3 LoginScreen can branch on cancellation (code 1001).
  - **signInWithGoogle():** same flow against `/auth/google` with `{id_token, device_id, device_secret, platform}`.
  - **startActivatingPro(id)/stopActivatingPro():** set/clear `pendingInvoiceId` + `isActivatingPro`.
- Filled `authStore.test.ts` (6 tests) — passing.

## api.ts T-7 Diff Summary

```
+ declare module 'axios' { export interface AxiosRequestConfig { _skipAuthRefresh?: boolean; _retry?: boolean; } }
...
  const originalRequest = error.config;
+ const requestUrl: string = originalRequest.url || '';
+ const skipAuthRefresh = originalRequest._skipAuthRefresh === true || requestUrl.startsWith('/auth/');
- if (error.response?.status === 401 && !originalRequest._retry) {
+ if (error.response?.status === 401 && !originalRequest._retry && !skipAuthRefresh) {
```
Either guard alone suffices; both together close the T-7 gap fully (request-flag belt + URL-pattern braces). Refresh body untouched.

## Verification Results

```
$ cd app && npm test -- --testPathIgnorePatterns='version.test|App.test'
Test Suites: 3 skipped, 6 passed, 6 of 9 total
Tests:       10 skipped, 34 passed, 44 total
exit 0
  # 6 passed = the 6 Wave-2 surfaces filled by this plan
  # 3 skipped = Wave-3 UI stubs (LoginScreen, PaymentScreen, ActivatingProModal) — Plan 03 scope

$ cd app && npx tsc --noEmit
0 errors (exit 0)   # FULL clean — see "Note on the anticipated PaymentScreen transient" below

$ grep -c "createCheckoutSession\|cancelSubscription" app/src/services/payment.ts  → 0
$ grep -rc "_skipAuthRefresh" app/src/services/api.ts app/src/stores/authStore.ts  → 3 + 3 (>=4 across files)
$ grep -c "auth_provider" app/src/types/api.ts                                     → 1
```

`version.test.ts` stays intentionally RED (Wave 4 bumps `APP_VERSION` to `2.2.0`); `App.test.tsx` stays pre-existing-RED (DEF-05-00-01). Both excluded from the gate by design.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `AppleSignInResult.fullName` type mismatch with the real @invertase library**
- **Found during:** Task 2 (`npx tsc --noEmit` after creating the service files).
- **Issue:** The plan's verbatim `appleSignIn.ts` declared `fullName: {givenName?: string; familyName?: string} | null`, but the REAL installed `@invertase/react-native-apple-authentication@2.5.1` `AppleRequestResponse.fullName` is `AppleRequestResponseFullName | null` where `givenName`/`familyName` are `string | null` (not `string | undefined`). The Wave-0 mock used the non-null shape, so the bug only surfaced against the real library types. Result: `error TS2322` on the `return {... fullName: response.fullName ...}` assignment — would have failed the Task 3 `tsc --noEmit` acceptance gate.
- **Fix:** Widened the interface to `fullName: {givenName?: string | null; familyName?: string | null} | null`. The authStore action's `${givenName ?? ''} ${familyName ?? ''}` join already handles both `null` and `undefined`, so no downstream change.
- **Files modified:** `app/src/services/appleSignIn.ts`.
- **Commit:** `3c42a70` (committed with Task 2 since it was discovered there).

### Sentinel Note (intentional, operator-authorized)

The plan's `<WEB_OAUTH_CLIENT_ID>`/`<IOS_OAUTH_CLIENT_ID>` template placeholders were replaced with the operator-authorized `PLACEHOLDER_WEB.apps.googleusercontent.com` / `PLACEHOLDER_IOS.apps.googleusercontent.com` sentinels (DEF-05-CREDS, the same values wired into the Wave-1 native config). The plan's automated checks (`! grep '<WEB_OAUTH_CLIENT_ID>'`, `! grep '<IOS_OAUTH_CLIENT_ID>'`) PASS because those template strings are absent; the authorized sentinels stay greppable for store-time replacement.

### Note on the anticipated PaymentScreen transient

The plan + threat-model `Stale Stripe-era imports` row anticipated that `app/src/screens/PaymentScreen.tsx` imports `createCheckoutSession` and would therefore throw a transient `tsc` error after the payment.ts rewrite (resolved in Wave 3). **In fact PaymentScreen.tsx does NOT import either deleted helper** — it uses a Telegram-CTA flow (`useSubscription` + `Linking.openURL`), so deleting the Stripe helpers broke nothing. `npx tsc --noEmit` is therefore FULLY clean (exit 0), not just clean-excluding-PaymentScreen. No transient error materialized.

## Authentication Gates

None. (Live SSO authentication will not succeed until the `PLACEHOLDER_*` client IDs are replaced at store upload — DEF-05-CREDS — but that is a deferred operator credential task, not an execution-time auth gate.)

## Known Stubs

| Stub | File | Line(s) | Reason / resolved by |
|------|------|---------|----------------------|
| `WEB_CLIENT_ID = 'PLACEHOLDER_WEB.apps.googleusercontent.com'` | `app/src/services/googleSignIn.ts` | ~19 | Operator-authorized deferred Google Web Client ID (DEF-05-CREDS). Backend JWT audience. Filled at store upload. NOT a defect — the wrapper logic is complete and tested. |
| `IOS_CLIENT_ID = 'PLACEHOLDER_IOS.apps.googleusercontent.com'` | `app/src/services/googleSignIn.ts` | ~20 | Operator-authorized deferred Google iOS Client ID (DEF-05-CREDS). Filled at store upload. |

These match the Wave-1 native-config sentinels; pre-upload check `grep -rn "PLACEHOLDER_" app/ios app/android app/src` enumerates all of them. No production-logic stubs were introduced — all behaviors are implemented and pass real Jest assertions.

## Threat Flags

No new security-relevant surface beyond the plan's `<threat_model>`. The three new files implement exactly the T-1 (deep-link untrusted invoiceId), T-2 (tokens never persisted), T-3 (auth code forwarded, never logged/persisted), and T-7 (dual 401 short-circuit) mitigations the register assigned to this wave.

## Self-Check: PASSED

- All 3 created files + 4 modified production files + 6 filled test files verified present on disk.
- All 3 task commits (`bc3a3d1`, `3c42a70`, `a6a7502`) verified in git history.
- STATE.md / ROADMAP.md NOT modified (orchestrator owns those writes, per objective).
