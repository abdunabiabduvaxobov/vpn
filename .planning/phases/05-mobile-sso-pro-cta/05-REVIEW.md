---
phase: 05-mobile-sso-pro-cta
reviewed: 2026-05-29T14:54:46Z
depth: deep
files_reviewed: 20
files_reviewed_list:
  - app/App.tsx
  - app/src/services/api.ts
  - app/src/services/appleSignIn.ts
  - app/src/services/googleSignIn.ts
  - app/src/services/deepLink.ts
  - app/src/services/payment.ts
  - app/src/stores/authStore.ts
  - app/src/types/api.ts
  - app/src/config/version.ts
  - app/src/components/ActivatingProModal.tsx
  - app/src/components/LeavingAppSheet.tsx
  - app/src/screens/LoginScreen.tsx
  - app/src/screens/PaymentScreen.tsx
  - app/src/screens/AccountScreen.tsx
  - app/src/screens/HomeScreen.tsx
  - app/src/navigation/RootNavigator.tsx
  - app/ios/VpnApp/Info.plist
  - app/ios/VpnApp/AppDelegate.swift
  - app/android/app/src/main/AndroidManifest.xml
  - app/android/app/src/main/res/values/strings.xml
findings:
  critical: 0
  warning: 5
  info: 6
  total: 11
status: issues_found
---

# Phase 5: Code Review Report

**Reviewed:** 2026-05-29T14:54:46Z
**Depth:** deep
**Files Reviewed:** 20
**Status:** issues_found

## Summary

Reviewed the Phase 5 mobile SSO + Pro-CTA execution diff (`588cf15..HEAD`, `app/` only): the SSO service wrappers (`appleSignIn`, `googleSignIn`), the deep-link handler, the `payment.ts` rewrite, the `api.ts` `_skipAuthRefresh` interceptor patch, the `authStore` in-place guest-promotion actions, the `ActivatingProModal` polling overlay, the rewritten compliance-safe `PaymentScreen`, `LoginScreen`, `AccountScreen`, `App.tsx` wiring, and the iOS/Android native config + version bump. Test files were read for behavioral intent but are out of review scope.

Overall assessment: the feature is well-structured and the headline security properties hold. **No Critical issues.** The two designed-in trust boundaries are sound: the deep-link `invoiceId` is treated as untrusted and Pro only flips after the backend `getInvoice` returns `status === 'paid'` (T-1), and the SSO ID-tokens are forwarded raw to the backend with no mobile-side crypto and never persisted/logged (T-2/T-3). The `_skipAuthRefresh` dual short-circuit (config flag + `/auth/*` URL pattern) cannot be abused to escalate privilege — it only *disables* refresh-on-401, and `/auth/refresh` self-matches so it cannot recurse. No real OAuth secret was committed; the only `PLACEHOLDER_*` strings are the operator-authorized deferred sentinels (DEF-05-CREDS) and a test-fixture `device_secret: 'sec_1'` confined to spec files.

The concerns that warrant attention are correctness/robustness, not security: a deep-link parser that fails on legitimate multi-param URLs and accepts non-`success` path prefixes, a `setTimeout` in the polling modal that is not cleared on unmount (set-state-after-unmount), and a misleading `await` on a fire-and-forget `initialize()`.

Version bump is consistent across `version.ts` (2.2.0), `package.json` (2.2.0), and Android `build.gradle` (versionName 2.2.0 / versionCode 13).

## Warnings

### WR-01: Deep-link parser requires `invoiceId` to be the FIRST query parameter

**File:** `app/src/services/deepLink.ts:25`
**Issue:** The extraction regex `/\?invoiceId=([^&]+)/` anchors on `?invoiceId=`, so it only matches when `invoiceId` is the *first* query parameter immediately after `?`. A legitimate payment-return URL where the backend or lava.top appends any parameter before it — e.g. `vpnapp://payment/success?status=ok&invoiceId=X` or `vpnapp://payment/success?utm_source=lava&invoiceId=X` — returns `null`, so `startActivatingPro` is never dispatched and the Activating-Pro modal never appears even though the user paid. This is the launch-critical "Pro unlocks immediately" path silently breaking on a URL shape the mobile app does not control. Verified by reproduction: `vpnapp://payment/success?foo=1&invoiceId=X` => `null`.
**Fix:** Match the parameter regardless of position by allowing a `?` or `&` delimiter before the key:
```ts
const m = url.match(/[?&]invoiceId=([^&#]+)/);
```
(The `#` exclusion also stops a trailing fragment from being captured into the id.)

### WR-02: Deep-link path check uses prefix match, not exact-segment match

**File:** `app/src/services/deepLink.ts:24`
**Issue:** `url.startsWith(PAYMENT_SUCCESS_PATH)` (where `PAYMENT_SUCCESS_PATH = 'vpnapp://payment/success'`) treats any path that *begins with* `success` as a payment-success link. Verified: both `vpnapp://payment/success-evil?invoiceId=HACK` and `vpnapp://payment/successfully?invoiceId=HACK` parse and return an invoiceId, dispatching `startActivatingPro`. Because the invoiceId is untrusted and the backend `getInvoice` is the source of truth (T-1), this is not a privilege-escalation hole — at worst it surfaces the polling modal for a bogus id that the backend rejects. But it is a robustness gap that contradicts the stated intent ("Scoped to host=payment so only vpnapp://payment/* reaches the app") and could spuriously pop the modal.
**Fix:** Require the path to be exactly `success` (followed by `?`, `#`, or end-of-string) rather than any string starting with `success`:
```ts
const PAYMENT_SUCCESS_PREFIX = 'vpnapp://payment/success';
if (!url.startsWith(DEEP_LINK_SCHEME)) return null;
const rest = url.slice(PAYMENT_SUCCESS_PREFIX.length);
if (!url.startsWith(PAYMENT_SUCCESS_PREFIX) || (rest && rest[0] !== '?' && rest[0] !== '#')) {
  return null;
}
```

### WR-03: `setTimeout` in success path is not cleared on unmount (set-state-after-unmount)

**File:** `app/src/components/ActivatingProModal.tsx:73-76, 114-117`
**Issue:** On `status === 'paid'` the effect schedules a 3000ms `setTimeout` that calls `stopActivatingPro()` and `setModalState('polling')`. The effect's cleanup function (line 96-98) only sets `cancelledRef.current = true` — it never `clearTimeout`s this pending timer. If the modal unmounts within that 3s window (e.g. the navigator tears it down, or `isActivatingPro` is flipped elsewhere), the timer still fires `setModalState` on an unmounted component (a React state-update-on-unmounted-component warning and a wasted `stopActivatingPro` call). The same un-cleared `setTimeout` exists in the `onRefresh` handler (line 114-117), which additionally has no `cancelledRef` guard at all. `cancelledRef` guards the polling `tick` chain but not these success timers.
**Fix:** Track the timeout id in a ref and clear it in the effect cleanup, e.g.:
```ts
const successTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
// on paid:
successTimerRef.current = setTimeout(() => { stopActivatingPro(); setModalState('polling'); }, 3000);
// cleanup:
return () => {
  cancelledRef.current = true;
  if (successTimerRef.current) clearTimeout(successTimerRef.current);
};
```
Apply the same ref/clear to the `onRefresh` success branch.

### WR-04: `LoginScreen.onGuest` awaits a non-async, fire-and-forget `initialize()`

**File:** `app/src/screens/LoginScreen.tsx:64-67` (and `app/src/stores/authStore.ts:62-92`)
**Issue:** `initialize` is declared `() => void` and runs its work inside an un-awaited async IIFE (`(async () => { ... })()`), returning `undefined`. `LoginScreen.onGuest` does `await initialize()`, which resolves on the next microtask regardless of whether `/auth/guest` has returned. `goHome()` therefore fires before tokens are set, so the app navigates to Home (and Account's `!isAuthenticated` loading branch) while the guest session is still being minted. It works in practice because `isAuthenticated` is reactive, but the `await` is misleading — a reviewer/maintainer will reasonably assume guest auth has completed by the time `goHome()` runs. If guest login fails (offline), the user lands on Home unauthenticated with no feedback.
**Fix:** Either make `initialize` return the promise (`initialize: (): Promise<void> => { ... return (async () => {...})(); }`) so the `await` is meaningful, or drop the `await` in `onGuest` and remove the `finally { setIsBusy(null) }` reliance on completion (since it no longer reflects auth completion). Returning the promise is preferable so `setIsBusy('guest')` accurately reflects in-flight state.

### WR-05: Guest sign-in path swallows all errors with no user feedback

**File:** `app/src/screens/LoginScreen.tsx:42-46, 54-58`
**Issue:** `onApple` catches the Apple cancellation code and then silently swallows *all other* errors (network failure, backend 4xx/5xx, missing identityToken) with an empty branch — there is no toast, no Alert, no state change. `onGoogle` is worse: it only `return`s on `statusCodes.SIGN_IN_CANCELLED` and has no other catch handling at all, so a non-cancellation Google error propagates out of the `try` into the `finally` and then is an unhandled rejection from the `onGoogle` async handler (it is re-thrown by `authStore.signInWithGoogle`). The summary documents per-provider toasts as intentionally deferred, so this is a known UX gap rather than a defect — but on the launch-critical "sign in once" path, a hard failure (e.g. backend down, or the still-placeholder OAuth client IDs rejecting tokens) leaves the user tapping a button that appears to do nothing, with no diagnostic. Note this interacts with DEF-05-CREDS: until real client IDs are filled, *every* real sign-in fails into this silent branch.
**Fix:** At minimum, distinguish cancellation from failure and surface a generic error toast on the non-cancellation path (mirroring `PaymentScreen.showToast`), and add an explicit catch in `onGoogle` for non-cancellation errors so nothing escapes the handler:
```ts
} catch (e: any) {
  if (e?.code === statusCodes.SIGN_IN_CANCELLED) return;
  showToast(t('login.signInFailed'));
}
```

## Info

### IN-01: `signInWithGoogle` defensive read casts through `any`, losing type safety

**File:** `app/src/services/googleSignIn.ts:51-52`
**Issue:** `(userInfo as any).data?.idToken ?? (userInfo as any).idToken` discards the SDK's typed response to support both v16 and pre-v13 shapes. The project is pinned to `@react-native-google-signin/google-signin` v16, so the pre-v13 fallback is dead-ish defensive code, and the `as any` suppresses any future SDK type drift that a typed read would catch.
**Fix:** Read the v16 typed shape directly (`userInfo.data?.idToken`) and keep the fallback only if a genuine version-straddling concern exists; if kept, narrow via a typed helper instead of `as any`.

### IN-02: `Invoice.status` union omits backend reconciliation states

**File:** `app/src/services/payment.ts:13`
**Issue:** `status: 'pending' | 'paid' | 'failed' | 'expired'`. The modal treats anything not `paid`/`failed` as "keep polling" (including `expired`, which is terminal-ish). If the backend ever returns a status outside this union (e.g. `cancelled`, `refunded`, `processing`), TS won't flag it and the modal will silently poll until the 15-poll budget elapses into `takingLonger`. Low impact given the modal is poll-count-bounded, but the contract should match the backend Phase 2/4 invoice state machine.
**Fix:** Confirm the union against the backend invoice status enum and add any missing terminal states, treating terminal-non-paid states as `failed` in the modal.

### IN-03: `expired` invoice status keeps polling instead of terminating

**File:** `app/src/components/ActivatingProModal.tsx:79-83`
**Issue:** Only `failed` short-circuits to the failed state; `expired` falls into the `// 'pending' or 'expired' — keep polling` branch and polls until `MAX_POLLS`, then shows `takingLonger`. For an expired payment URL the user will never succeed, so the 30s wait + "taking longer" framing is misleading (it implies success is imminent).
**Fix:** Treat `expired` the same as `failed` (immediate failed state), or add a distinct terminal message.

### IN-04: Polling budget is poll-count-based, not wall-clock — comment says "30s timeout"

**File:** `app/src/components/ActivatingProModal.tsx:27-28, 87-91`
**Issue:** `MAX_POLLS = 15` with a 2000ms interval is documented as a "30s budget," but the real elapsed time is `15 * (network round-trip + 2000ms)`, i.e. strictly greater than 30s and dependent on backend latency (each `tick` awaits `getInvoice` before scheduling the next `setTimeout`). The escalate transition (`pollCount > ESCALATE_AFTER`, so poll #6 onward) is correct and matches the tests. Not a bug, but the "30s" framing in the comment and the D-21 cadence is inaccurate under real latency.
**Fix:** Either gate on `Date.now()` elapsed against a 30s wall-clock budget, or update the comment to "~30s + N round-trips" to set correct expectations.

### IN-05: `as any` error casts in AccountScreen handlers

**File:** `app/src/screens/AccountScreen.tsx:262, 293`
**Issue:** `const anyErr = err as {response?: {data?: {error?: string}}}` is a manual structural cast on caught errors. This is consistent with existing pre-Phase-5 patterns in the file (not new to this phase) and is low-risk, but it bypasses axios's typed error surface.
**Fix:** Optional — use `axios.isAxiosError(err)` to narrow before reading `err.response?.data?.error`. Low priority; flagged only for consistency if the team standardizes error handling.

### IN-06: PaymentScreen `isPro` derives a loose truthy value from a typed union

**File:** `app/src/screens/PaymentScreen.tsx:42`
**Issue:** `const isPro = user?.subscription_tier && user.subscription_tier !== 'free'` evaluates to `undefined` when `user` is null and a string-or-boolean otherwise; it is used in JSX conditionals where that is harmless. Minor readability/typing nit — the value is `string | boolean | undefined` rather than a clean `boolean`.
**Fix:** `const isPro = !!user && user.subscription_tier !== 'free';` for a strict boolean.

## Security Notes (reviewed — no findings)

- **`_skipAuthRefresh` interceptor (`api.ts:60-140`):** Cannot be abused. The flag only *suppresses* the 401→refresh→retry cycle; it grants no access. The `/auth/*` URL pattern self-includes `/auth/refresh`, so a refresh 401 cannot recurse into another refresh. No token is logged. The refresh body is unchanged from prior phases. No leak path identified.
- **Deep-link `invoiceId` (T-1):** Untrusted by design; Pro only unlocks after backend `getInvoice` returns `paid`. Spoofing a deep link cannot grant Pro (WR-02 robustness aside).
- **SSO token handling (T-2/T-3):** `identityToken`/`idToken`/`authorizationCode` are forwarded raw to the backend and never persisted to AsyncStorage or logged; only the resulting `AuthTokens` are stored. Mobile does no crypto, matching the documented server-side-verification contract.
- **In-place guest promotion (`authStore.signInWithApple/Google`):** Does not clear the guest token before the `/auth/*` call, so the request interceptor attaches the guest JWT (D-06). `users.id` continuity is the backend's responsibility; mobile correctly does `set({user: null})` then `fetchAccount()` to re-pull the (possibly-promoted) identity. No client-side id handling that could corrupt continuity.
- **Committed credentials:** No real secret committed. `PLACEHOLDER_IOS/WEB/ANDROID/APPLE_SERVICE_ID` are operator-authorized deferred sentinels (DEF-05-CREDS). `device_secret: 'sec_1'` occurrences are test fixtures in `*.test.ts` only. Google client IDs are public values by design (T-6).
- **iOS ATS / Android cleartext:** `usesCleartextTraffic="true"` (Android) and `NSAllowsLocalNetworking` (iOS) are pre-existing VPN/local-dev settings, not introduced by this phase; `NSAllowsArbitraryLoads` remains `false`. Out of phase scope.

---

_Reviewed: 2026-05-29T14:54:46Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
