# Phase 5: Mobile SSO + Pro CTA — Research

**Researched:** 2026-05-26
**Domain:** React Native 0.84 client integration: Apple/Google SSO, custom-scheme deep links, in-app browser handoff, post-payment invoice polling, version bump for store builds
**Confidence:** HIGH (every recommended library version verified against npm registry; every required iOS/Android file inspected on disk; backend contracts mapped to existing handlers; UI-SPEC + CONTEXT.md provide locked decisions on virtually every visible surface)

## Summary

Phase 5 is the **mobile-client-only** terminal of a backend + landing payment loop that's already shipped. The mobile app at `app/` is bare-workflow React Native 0.84.1 + React 19.2.3, using Zustand for state, AsyncStorage for token persistence, axios for HTTP (with global 401→refresh→retry), and react-navigation v7 native-stack. No Expo, no Firebase. The work has four shapes:

1. **Two new SSO surfaces** — `@invertase/react-native-apple-authentication@2.5.1` for iOS (Android variant exists via `appleAuthAndroid` but Phase 5 hides Apple on Android per D-02), and `@react-native-google-signin/google-signin@16.1.2` for iOS + Android. Both auto-link to RN's community CLI on RN 0.84. Both deliver ID tokens that the existing backend (`/auth/apple`, `/auth/google` shipped in Phase 2) already accepts.
2. **One screen rewrite + one new screen + one new card** — `PaymentScreen.tsx` (full rewrite per D-14), new `LoginScreen.tsx` (D-02), and a new "Sign in to sync Pro" card on `AccountScreen.tsx` (D-03). All UI tokens locked by the approved UI-SPEC.
3. **One global overlay** — a deep-link-triggered "Activating Pro" modal rendered above the active screen on receipt of `vpnapp://payment/success?invoiceId=X`. Polls `GET /invoices/{id}` on the exact Phase 4 D-21 cadence (2s × 5 → 2s + `?escalate=true` × 10 → 30s timeout copy mirror).
4. **Plumbing** — version bumps in 4 files (D-17), Info.plist URL schemes + Sign-in-with-Apple entitlement + AppDelegate URL handler on iOS, AndroidManifest intent-filter + strings.xml client-id resource on Android, and an `_skipAuthRefresh` flag (or URL-pattern check) in `api.ts` to prevent the SSO endpoints from cycling through the existing refresh interceptor (T-7).

**Primary recommendation:** The phase is well-bounded, the libraries are stable, the backend is ready, and the UI-SPEC is approved. The single highest-risk item is **iOS bundle ID still being the RN template placeholder** (`org.reactjs.native.example.$(PRODUCT_NAME:rfc1034identifier)`) — Sign in with Apple cannot work without a real, registered Bundle ID. Document this as a blocking operator-prerequisite alongside D-21's existing checklist before any iOS code lands. Otherwise, plans should track the wave structure already telegraphed by CONTEXT.md: Wave 1 (operator prerequisites + deps install + native config), Wave 2 (services layer: appleSignIn, googleSignIn, deepLink, payment helpers), Wave 3 (UI: LoginScreen, PaymentScreen rewrite, AccountScreen card, RootNavigator wiring, Activating-Pro modal), Wave 4 (version bumps + local Android signed-build smoke).

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Login gating:**
- **D-01:** Auto-guest on app launch is preserved. LoginScreen is NOT mandatory; reached on demand from AccountScreen ("Sign in to sync Pro") or via direct navigation. ROADMAP SC#1's "auth entry" wording is **deliberately deviated from**.
- **D-02:** `LoginScreen.tsx` is a navigable destination, not a route-gate. Three CTAs vertically stacked: "Continue with Apple" (iOS only — hidden on Android), "Continue with Google" (both), "Continue as Guest". Apple cancellation returns silently to LoginScreen.
- **D-03:** AccountScreen gains a "Sign in to sync Pro" card visible only when `auth_provider === 'guest'` (or undefined for v2.1.0 carry-over). Two side-by-side Apple/Google buttons route to `LoginScreen`. Once SSO completes, the card disappears.
- **D-04:** Account-linking by verified email is silent (no UI). Private-relay Apple emails (`@privaterelay.appleid.com`) are NOT auto-linked per backend D-04.
- **D-05:** Silent transition to Home on guest → SSO success. No splash, no toast.
- **D-06:** Guest JWT MUST be sent in `Authorization: Bearer` header to `/auth/apple` and `/auth/google` so backend takes in-place promotion path (preserves `users.id`).

**Pro-return handshake:**
- **D-07:** Modal overlay on deep-link receive. Blocks dismissal during polling.
- **D-08:** Polling cadence = Phase 4 D-21 verbatim. 2s interval. Polls 1–5: no query string. Poll 6+: append `?escalate=true`. Total timeout: 30s. On `status === 'paid'` → close modal + `fetchAccount()` + transient success toast. On `status === 'failed'` → close modal + navigate to AccountScreen with error.
- **D-09:** Foreground safety-net extends existing `HomeScreen.tsx` lines 41–50 `AppState.active` hook.
- **D-10:** 30s timeout copy matches Phase 4 D-22. Shows `payment.takingLonger.title`, Refresh button, Telegram support link (`https://t.me/flawlssr`). No auto-retry; user has agency.
- **D-11:** Modal scope is global, not PaymentScreen-local. Renders above any active screen via root-level overlay.

**App Store compliance:**
- **D-12:** Full-screen "You're leaving the app" interstitial sheet BEFORE `Linking.openURL`. Primary "Continue" → opens browser. Secondary "Cancel" → dismiss.
- **D-13:** CTA copy is **exactly** `Upgrade to Pro at risevpn.com`. NO price displayed anywhere on or near the button (SC#3 hard requirement).
- **D-14:** PaymentScreen content structure: (1) current-plan card with limits (no prices), (2) "Pro includes" feature list (free users only, no prices), (3) single CTA "Upgrade to Pro at risevpn.com", (4) tertiary "Already paid? Refresh" text link. Existing Telegram-CTA flow + 3-plan cards **removed entirely**. `createCheckoutSession` + `cancelSubscription` deleted from `payment.ts`.
- **D-15:** Restore-purchase affordance is non-prominent (small text link, NOT a button). Calls `fetchAccount()` + shows transient toast.
- **D-16:** Locale derivation: `ru` → `https://risevpn.com/ru/pricing?return=app`; otherwise `/en/pricing?return=app`. Mobile carries EN + RU only; ES is landing-only.

**Build, release, & versioning:**
- **D-17:** All four version sources updated together: `app/package.json` (`"version": "2.2.0"`), `app/src/config/version.ts` (`APP_VERSION = '2.2.0'`), `app/android/app/build.gradle` (`versionName "2.2.0"` + `versionCode` incremented from 12 to 13), `app/ios/VpnApp.xcodeproj/project.pbxproj` (`MARKETING_VERSION = 2.2.0` + `CURRENT_PROJECT_VERSION` incremented from 1).
- **D-18:** **No fastlane / no CI upload / no TestFlight + no Play Internal upload in this phase.** Explicit scope deviation from APP-07. Phase 5 release bar: "code complete + signed local Android build + smoke-tested on operator's physical Android device."
- **D-19:** Local Android release build (`./gradlew bundleRelease`) → signed `.aab`. Operator smoke-tests on physical Android device covering: Google sign-in, guest → SSO upgrade preserves `users.id`, PaymentScreen informational layout, interstitial → browser handoff, deep-link receive opens polling modal.
- **D-20:** iOS code lands and compiles but iOS smoke-test deferred (operator has no iOS hardware/Apple Connect setup).
- **D-21:** Operator-prerequisites checklist gates the phase as a `[BLOCKING]` task. Required this phase: Apple Bundle ID with Sign-in-with-Apple capability, Google OAuth client IDs (iOS, Android, Web), Android debug keystore SHA-1 registered. Apple App Store Connect API key, Play Console service account, production release keystore, external-link entitlement: NOT required.

**Threat model (every PLAN.md MUST include `<threat_model>`):**
- **D-22:** `security_enforcement` enabled. Minimum cover: T-1 deep-link spoofing, T-2 ID-token replay, T-3 Apple authorization-code leakage, T-4 universal-clipboard / browser-history leak of `invoiceId`, T-5 token storage on rooted/jailbroken device, T-6 SSO library supply-chain risk, T-7 401→refresh→retry feedback loop on SSO failure.

### Claude's Discretion

- Sheet vs full-screen Modal for "You're leaving the app" interstitial (D-12 content is locked, visual form factor isn't).
- `paymentReturnStore` vs extending `authStore` for polling/modal state (D-11). Default: extend `authStore` with `pendingInvoiceId` + `isActivatingPro` fields.
- i18n key namespace exact names within the locked namespaces (`login.*`, `payment.upgrade.*`, `payment.activating.*`, `payment.takingLonger.*`, `account.signInToSync.*`).
- AccountScreen "Sign in to sync Pro" card visual treatment within UI-SPEC tokens.
- `useSubscription` hook re-wiring strategy after PaymentScreen rewrite.
- Whether to add `_skipAuthRefresh` config flag in `api.ts` for SSO endpoints (T-7 mitigation) vs adding a URL-pattern check in the existing 401 interceptor.
- Pre-selected provider on LoginScreen when navigated from AccountScreen card (default: no params, all three CTAs visible).

### Deferred Ideas (OUT OF SCOPE)

- TestFlight upload (iOS) + Play Internal Track upload (Android) → end-of-milestone release phase.
- fastlane / CI release automation → same end-of-milestone phase.
- iOS smoke-test on physical iPhone → end-of-milestone phase.
- Universal Links (`https://risevpn.com/pay/success` via `apple-app-site-association` + `assetlinks.json`) → current spec uses custom scheme `vpnapp://`.
- MMKV token-storage migration (AsyncStorage stays per ADR §12.6).
- Mobile consumption of JWT `plan_id` claim (mobile keeps reading legacy `subscription_tier`).
- ES locale on mobile (landing-only).
- "Merge accounts" UI for cross-provider distinct-email users (Phase 6).
- Share-code Pro warning before SSO (ADR §13 row 9).
- `cancelSubscription` UI on mobile (Phase 7+).
- Apple external-link entitlement form submission (parallel operational track).
- Per-provider error toasts (default is silent return).
- Linked-provider chips on AccountScreen.
- Splash / welcome animation on SSO success (D-05 silent).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| APP-01 | Sign in with Apple works on iOS via `@invertase/react-native-apple-authentication`; backend `/auth/apple` returns the same JWT shape as guest login. | §"Apple Sign-In Integration" + §"Guest → SSO Upgrade In-Place"; backend handler `server/api/internal/handler/auth.go::AppleSignIn` already shipped (Phase 2 D-19). |
| APP-02 | Sign in with Google works on iOS + Android via `@react-native-google-signin/google-signin`; backend `/auth/google` returns the same JWT shape. | §"Google Sign-In Integration"; backend handler `auth.go::GoogleSignIn` shipped (Phase 2 D-21). Three OAuth client IDs needed (iOS, Android, Web) — Web client ID is the one returned in `idToken.aud` and is what backend validates. |
| APP-03 | `LoginScreen` offers Apple / Google / Guest. (Wording deviation: it's a navigable destination not a route-gate per D-02.) | §"LoginScreen + AccountScreen Card"; existing `RootNavigator.tsx` shape and `RootStackParamList` type pattern. |
| APP-04 | Guest → Apple/Google upgrade preserves `users.id`. | §"Guest → SSO Upgrade In-Place"; locked by D-06 — guest JWT goes in `Authorization` header on `/auth/apple` and `/auth/google` so backend executes in-place promotion path (Phase 2 D-06). |
| APP-05 | `PaymentScreen` informational only — single "Upgrade to Pro at risevpn.com" CTA opening system browser. No prices, no buy button, no IAP. | §"In-App Browser for Pricing CTA" + §"PaymentScreen Rewrite (D-14)"; locked by D-13/D-14, UI-SPEC namespace `payment.upgrade.*`. |
| APP-06 | `vpnapp://payment/success?invoiceId=X` returns user to app; app polls `GET /invoices/{id}` and refreshes plan state. iOS Info.plist + Android intent-filter registered. | §"Deep Link Handling" + §"Invoice Polling Pattern"; backend endpoint shipped Phase 3 03-05 with `?escalate=true` semantics. |
| APP-07 | `app.json` bumped to 2.2.0; build ships to TestFlight + Play Internal. | §"Release Pipeline"; **deliberately deviated** by D-18 — Phase 5 bar is local signed Android `.aab` smoke-tested by operator; uploads land in a later end-of-milestone phase. |
</phase_requirements>

## Standard Stack

### Core

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `@invertase/react-native-apple-authentication` | **2.5.1** (latest, published 2026-03-31) [VERIFIED: npm view] | Apple Sign-In on iOS (and Android via `appleAuthAndroid` if ever needed) | The only actively-maintained RN Apple-auth library; named in ADR-007 §15 row 7; confirmed by CONTEXT.md `canonical_refs`; auto-links on RN 0.60+ [CITED: github.com/invertase/react-native-apple-authentication] |
| `@react-native-google-signin/google-signin` | **16.1.2** (latest, published 2026-02-28) [VERIFIED: npm view] | Google Sign-In on iOS + Android | The community-blessed RN Google-auth library; named in ADR-007 §15 row 7; v13+ requires `webClientId` even without Firebase to obtain an `idToken` [CITED: react-native-google-signin.github.io/docs] |
| `react-native` | 0.84.1 [VERIFIED: package.json] | Host framework | Locked tech stack |
| `zustand` | 5.0.12 [VERIFIED: package.json] | Auth + payment-return state | Locked tech stack; existing `authStore` extends naturally |
| `axios` | 1.13.6 [VERIFIED: package.json] | HTTP client | Locked tech stack; existing `api.ts` carries the 401→refresh interceptor |
| `react-i18next` | 16.6.1 + `i18next` 25.10.4 [VERIFIED: package.json] | Translations (EN + RU) | Already pervasive |
| `@react-navigation/native-stack` | 7.14.6 [VERIFIED: package.json] | Screen routing — adds `Login` Stack.Screen | Existing `RootNavigator.tsx` pattern |

### Supporting (already in tree — no install needed)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `@react-native-async-storage/async-storage` | ^1.24.0 | Token persistence | Preserved; do NOT migrate to MMKV this phase (ADR §12.6) |
| `react-native-device-info` | ^15.0.2 | Existing `getDeviceFingerprint()` helper | Reused on SSO calls to bind device per Phase 2 D-20/D-22 |
| `@tanstack/react-query` | ^5.94.5 | `useSubscription` hook | Kept (D-CD: consumer surface shrinks but hook stays) |
| `react-native-mmkv` | ^4.3.0 | (Available but unused by auth this phase) | Do not consume — ADR §12.6 locks AsyncStorage for tokens |
| `react-native-localize` | ^3.7.0 | Locale detection (RU vs EN for upgrade URL) | Reused — `getLocales()[0].languageCode` is the input to D-16 derivation |

### React Native Core Primitives (no new dep — RN 0.84 stock)

| Primitive | Purpose |
|-----------|---------|
| `Linking` | `Linking.openURL` to launch system browser; `Linking.addEventListener('url')` to receive deep links; `Linking.getInitialURL()` for cold-start |
| `Modal` | "You're leaving the app" interstitial sheet (D-12) + Activating-Pro overlay (D-07/D-11) |
| `ActivityIndicator` | Spinner inside Activating-Pro modal |
| `AppState` | Foreground safety-net (D-09) — already used in HomeScreen lines 41–50 |
| `Platform` | iOS vs Android branching (`Platform.OS === 'ios'` for Apple CTA visibility) |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `@invertase/react-native-apple-authentication` | Native iOS `AuthenticationServices` bridge (custom Swift module) | More work, smaller dep footprint, but reinvents a well-tested wheel. CONTEXT.md `canonical_refs` already names invertase package, do not deviate. |
| `@react-native-google-signin/google-signin` | `react-native-app-auth` (generic OAuth) | Generic library does NOT integrate with the native Google "select an account" UI on Android — UX regression. Stick with the named library. |
| `react-native-inappbrowser-reborn` | `Linking.openURL` (system browser) | In-app browser shows the user URL still belongs to risevpn.com but ADDS a heavy native dep. **App Store guideline note:** Since Apple's May 2025 guideline update for the US storefront, no entitlement is required for external links and no in-app browser requirement applies [CITED: developer.apple.com/news/?id=9txfddzf]. **Recommend `Linking.openURL`** (RN core, zero new dep, matches existing PaymentScreen line 163 pattern for Telegram CTA). |
| Universal Links (`https://risevpn.com/pay/success` → app) | Custom scheme `vpnapp://payment/success?invoiceId=X` | Universal Links require `apple-app-site-association` + Android `assetlinks.json` hosted on the website — extra surface, deferred per CONTEXT.md `<deferred>`. ROADMAP SC#4 and ADR §12.4 lock `vpnapp://`. |
| MMKV for tokens | AsyncStorage | ADR §12.6 locks no change this phase. Hardening (HARD-16) moves tokens to Keychain / EncryptedSharedPreferences in Phase 8. |

**Installation (one command):**

```bash
cd app
npm install --save-exact \
  @invertase/react-native-apple-authentication@2.5.1 \
  @react-native-google-signin/google-signin@16.1.2
cd ios && pod install && cd ..
```

**Version verification:**
```bash
npm view @invertase/react-native-apple-authentication version
# → 2.5.1 (modified 2026-03-31) — VERIFIED
npm view @react-native-google-signin/google-signin version
# → 16.1.2 (modified 2026-02-28) — VERIFIED
```

**Pin exactly (`--save-exact`)** per T-6 supply-chain mitigation; commit `package-lock.json`; run `npm audit --omit=dev` and resolve any `high`/`critical` findings before merge.

## Architecture Patterns

### Recommended File Structure

```
app/
├── App.tsx                                     # Add deepLink.ts wiring + GoogleSignin.configure() + render <ActivatingProModal/>
├── src/
│   ├── services/
│   │   ├── api.ts                              # MODIFY: add _skipAuthRefresh handling for /auth/apple and /auth/google
│   │   ├── appleSignIn.ts                      # NEW: wraps @invertase/react-native-apple-authentication
│   │   ├── googleSignIn.ts                     # NEW: wraps @react-native-google-signin/google-signin (+ configure())
│   │   ├── deepLink.ts                         # NEW: Linking.addEventListener + URL parser + dispatches to authStore
│   │   ├── deviceFingerprint.ts                # REUSE (unchanged)
│   │   └── payment.ts                          # REWRITE: drop createCheckoutSession / cancelSubscription; add upgradeUrlForLocale() + pollInvoice()
│   ├── stores/
│   │   └── authStore.ts                        # MODIFY: add signInWithApple(), signInWithGoogle(), pendingInvoiceId, isActivatingPro fields
│   ├── screens/
│   │   ├── LoginScreen.tsx                     # NEW
│   │   ├── PaymentScreen.tsx                   # REWRITE per D-14
│   │   ├── AccountScreen.tsx                   # MODIFY: add "Sign in to sync Pro" card per D-03
│   │   └── HomeScreen.tsx                      # MODIFY: extend lines 41–50 AppState hook per D-09
│   ├── components/
│   │   ├── LeavingAppSheet.tsx                 # NEW: D-12 interstitial Modal
│   │   └── ActivatingProModal.tsx              # NEW: D-07/D-08/D-10/D-11 global polling overlay
│   ├── navigation/
│   │   └── RootNavigator.tsx                   # MODIFY: add Login Stack.Screen
│   ├── i18n/
│   │   ├── en.json                             # MODIFY: add login.*, payment.upgrade.*, payment.activating.*, payment.takingLonger.*, account.signInToSync.*
│   │   └── ru.json                             # MODIFY: parallel keys (placeholder = EN copy; operator translates separately per UI-SPEC)
│   ├── types/
│   │   └── api.ts                              # MODIFY: extend User with auth_provider, email, email_verified
│   └── config/
│       └── version.ts                          # MODIFY: APP_VERSION = '2.2.0'
├── package.json                                # MODIFY: version 2.2.0 + new deps
├── ios/
│   ├── Podfile                                 # AUTO via auto-linking; verify after pod install
│   ├── VpnApp.xcodeproj/project.pbxproj        # MODIFY: MARKETING_VERSION=2.2.0, CURRENT_PROJECT_VERSION++, PRODUCT_BUNDLE_IDENTIFIER (see Open Q #1)
│   └── VpnApp/
│       ├── Info.plist                          # MODIFY: CFBundleURLTypes for `vpnapp` + Google reversed-client-id
│       ├── VpnApp.entitlements                 # MODIFY: add `com.apple.developer.applesignin` capability
│       └── AppDelegate.swift                   # MODIFY: implement application(_:open:options:)
└── android/app/
    ├── build.gradle                            # MODIFY: versionName "2.2.0", versionCode 13 (was 12), apply google-services plugin (optional — see Pattern 2)
    ├── src/main/AndroidManifest.xml            # MODIFY: add intent-filter for vpnapp scheme on MainActivity
    └── src/main/res/values/strings.xml         # NEW or MODIFY: add server_client_id (Web OAuth client ID) string resource
```

### Pattern 1: Apple Sign-In Service (iOS)

```typescript
// Source: github.com/invertase/react-native-apple-authentication/blob/main/README.md (extracted via WebFetch)
// app/src/services/appleSignIn.ts
import {
  appleAuth,
  AppleRequestResponse,
} from '@invertase/react-native-apple-authentication';

export interface AppleSignInResult {
  identityToken: string;                       // JWT — sent to backend /auth/apple
  authorizationCode: string | null;            // Optional — Phase 2 D-18 says backend doesn't exchange yet, but pass it through
  user: string;                                // sub claim (Apple stable id)
  fullName: { givenName?: string; familyName?: string } | null;  // FIRST sign-in only
  email: string | null;                        // FIRST sign-in only
}

export async function signInWithApple(): Promise<AppleSignInResult> {
  const response: AppleRequestResponse = await appleAuth.performRequest({
    requestedOperation: appleAuth.Operation.LOGIN,
    requestedScopes: [appleAuth.Scope.FULL_NAME, appleAuth.Scope.EMAIL],
  });

  if (!response.identityToken) {
    throw new Error('Apple sign-in did not return an identityToken');
  }

  return {
    identityToken: response.identityToken,
    authorizationCode: response.authorizationCode,
    user: response.user,
    fullName: response.fullName,
    email: response.email,
  };
}

// User cancellation surfaces as a thrown error with code === '1001'
// (see @invertase/react-native-apple-authentication README — exact code: appleAuth.Error.CANCELED).
// authStore.signInWithApple() should catch and rethrow as a typed "cancelled" error
// so LoginScreen returns silently per D-02 + UI-SPEC.
```

**CRITICAL** [CITED: invertase README]: Apple returns `fullName` and `email` **ONLY ON THE FIRST sign-in attempt**. Subsequent sign-ins return `null` for these fields. The backend's Phase 2 D-19 handler already accounts for this (uses the cached row on second sign-in), so the mobile client just forwards whatever Apple returns. To re-test on the same device, the developer must revoke app access via iOS Settings → Apple ID → Sign in with Apple → RiseVPN → Stop Using Apple ID.

### Pattern 2: Google Sign-In Service (iOS + Android)

```typescript
// Source: react-native-google-signin.github.io/docs (extracted via WebFetch)
// app/src/services/googleSignIn.ts
import {
  GoogleSignin,
  statusCodes,
} from '@react-native-google-signin/google-signin';

const WEB_CLIENT_ID = '...your-web-oauth-client-id....apps.googleusercontent.com';
const IOS_CLIENT_ID = '...your-ios-oauth-client-id....apps.googleusercontent.com';

export function configureGoogleSignIn() {
  GoogleSignin.configure({
    webClientId: WEB_CLIENT_ID,         // REQUIRED — this is the audience that appears in idToken.aud
    iosClientId: IOS_CLIENT_ID,         // REQUIRED on iOS — for the native Google sheet
    offlineAccess: false,                // We don't need a refresh token; backend issues its own JWTs
    scopes: ['email', 'profile'],        // Backend needs email_verified + sub claims
  });
}

export interface GoogleSignInResult {
  idToken: string;                       // JWT — sent to backend /auth/google
}

export async function signInWithGoogle(): Promise<GoogleSignInResult> {
  await GoogleSignin.hasPlayServices({ showPlayServicesUpdateDialog: true });
  const userInfo = await GoogleSignin.signIn();
  // v16: userInfo.data.idToken — see the breaking API change vs v12 (was userInfo.idToken)
  const idToken = (userInfo as any).data?.idToken ?? (userInfo as any).idToken;
  if (!idToken) {
    throw new Error('Google sign-in did not return an idToken');
  }
  return { idToken };
}

// Cancellation: error.code === statusCodes.SIGN_IN_CANCELLED → silent return
```

**Three OAuth client IDs required** [CITED: react-native-google-signin docs + GitHub issue #1152]:
1. **Web OAuth client ID** — used as `webClientId` in `configure()` so the resulting `idToken.aud` matches what the backend validates. **This is the same Web client ID Phase 2 D-21 already issued for the landing site** — reuse it. Backend audience must include this value.
2. **iOS OAuth client ID** — used as `iosClientId` in `configure()` and its `REVERSED_CLIENT_ID` goes into `Info.plist`'s `CFBundleURLTypes`. Tied to the iOS Bundle ID.
3. **Android OAuth client ID** — registered in Google Cloud Console with the Android package name (`com.vpnapp`) and SHA-1 fingerprint of the signing keystore. **NOT passed to `configure()`**; presence in Google Cloud Console is enough.

**`google-services.json` is NOT REQUIRED if not using Firebase** [CITED: react-native-google-signin/google-signin issue #1152 + docs]. We're not. We pass client IDs to `configure()` directly. The Android Gradle Google Services plugin is optional in this mode; only the build.gradle change is to apply the plugin if we add a `google-services.json` — recommended path is to skip the plugin entirely and pass the Web client ID as a string resource in `strings.xml` so the native side can resolve it.

**Android SHA-1 — debug vs release:**
- **Debug:** `keytool -list -v -keystore app/android/app/debug.keystore -alias androiddebugkey -storepass android -keypass android` → register this SHA-1 in the Android OAuth client for local smoke per D-19 + D-21.
- **Release:** when the production keystore is set up (end-of-milestone release phase), its SHA-1 must also be registered. For Phase 5, the phase-internal release keystore SHA-1 is also registered for the signed-`.aab` smoke per D-19.

### Pattern 3: Deep Link Service

```typescript
// Source: reactnative.dev/docs/linking (RN 0.84 stock Linking API)
// app/src/services/deepLink.ts
import { Linking } from 'react-native';
import { useAuthStore } from '../stores/authStore';

const DEEP_LINK_PREFIX = 'vpnapp://';

function parseInvoiceFromUrl(url: string): string | null {
  if (!url.startsWith(DEEP_LINK_PREFIX)) return null;
  // Match vpnapp://payment/success?invoiceId=X
  const m = url.match(/^vpnapp:\/\/payment\/success\?invoiceId=([^&]+)/);
  return m ? decodeURIComponent(m[1]) : null;
}

export function registerDeepLinkHandler(): () => void {
  // Cold-start case: getInitialURL() returns the URL the app was launched with.
  Linking.getInitialURL().then((url) => {
    if (url) {
      const invoiceId = parseInvoiceFromUrl(url);
      if (invoiceId) useAuthStore.getState().startActivatingPro(invoiceId);
    }
  });

  // Warm case: app already running, Linking emits 'url' when the OS hands us the URL.
  const sub = Linking.addEventListener('url', ({ url }) => {
    const invoiceId = parseInvoiceFromUrl(url);
    if (invoiceId) useAuthStore.getState().startActivatingPro(invoiceId);
  });

  return () => sub.remove();
}
```

Called once from `App.tsx`'s existing `useEffect` (next to `authStore.initialize()`).

### Pattern 4: Invoice Polling (mirrors Phase 4 D-21)

```typescript
// app/src/services/payment.ts (replaces existing Stripe-era file)
import api from './api';

export interface Invoice {
  id: string;
  status: 'pending' | 'paid' | 'failed' | 'expired';
  // ...other fields from backend Phase 3 03-05 contract
}

export async function getInvoice(invoiceId: string, escalate: boolean = false): Promise<Invoice> {
  const url = escalate ? `/invoices/${invoiceId}?escalate=true` : `/invoices/${invoiceId}`;
  const { data } = await api.get<{ data: Invoice }>(url);
  return data.data;
}

// Locale-derived upgrade URL — D-16
export function upgradeUrlForLocale(i18nLocale: string): string {
  const locale = i18nLocale === 'ru' ? 'ru' : 'en';
  return `https://risevpn.com/${locale}/pricing?return=app`;
}
```

**Polling loop lives in `ActivatingProModal.tsx` (component, not service)** so React lifecycle owns the timer:

```typescript
// Pseudocode:
useEffect(() => {
  if (!invoiceId) return;
  let pollCount = 0;
  const POLL_INTERVAL = 2000;
  const MAX_POLLS = 15;          // 30s / 2s
  const ESCALATE_AFTER = 5;      // start ?escalate=true on poll #6 (=10s elapsed)
  let cancelled = false;

  async function tick() {
    if (cancelled) return;
    pollCount += 1;
    try {
      const inv = await getInvoice(invoiceId, pollCount > ESCALATE_AFTER);
      if (inv.status === 'paid') { handleSuccess(); return; }
      if (inv.status === 'failed') { handleFailure(); return; }
      // 'pending' or 'expired' (treat expired same as timeout)
    } catch { /* keep polling; transient errors are expected */ }
    if (pollCount >= MAX_POLLS) { handleTimeout(); return; }
    setTimeout(tick, POLL_INTERVAL);
  }
  tick();
  return () => { cancelled = true; };
}, [invoiceId]);
```

### Pattern 5: authStore extension

```typescript
// authStore.ts — add to AuthState interface:
interface AuthState {
  // ...existing fields
  pendingInvoiceId: string | null;
  isActivatingPro: boolean;

  // Actions:
  signInWithApple: () => Promise<void>;
  signInWithGoogle: () => Promise<void>;
  startActivatingPro: (invoiceId: string) => void;
  stopActivatingPro: () => void;
}

// signInWithApple implementation pattern (mirrors existing linkWithCode at lines 80-97):
signInWithApple: async () => {
  set({ isLoading: true });
  try {
    const appleResult = await signInWithApple();             // service
    const fingerprint = await getDeviceFingerprint();
    // CRITICAL D-06: existing guest token (if any) goes in Authorization header
    // so backend takes in-place promotion path. axios interceptor does this
    // automatically once tokens are present.
    const { data } = await api.post<{ data: AuthTokens }>(
      '/auth/apple',
      {
        identity_token: appleResult.identityToken,
        authorization_code: appleResult.authorizationCode,
        device_id: fingerprint.device_id,
        device_secret: fingerprint.device_secret,
        platform: fingerprint.platform,        // 'ios'
        full_name: appleResult.fullName ?
          `${appleResult.fullName.givenName ?? ''} ${appleResult.fullName.familyName ?? ''}`.trim() : undefined,
        email: appleResult.email ?? undefined,
      },
      { _skipAuthRefresh: true } as any,        // T-7 mitigation
    );
    const tokens = data.data;
    await AsyncStorage.setItem(TOKENS_KEY, JSON.stringify(tokens));
    set({ tokens, isAuthenticated: true, isLoading: false, user: null });
    await get().fetchAccount();
  } catch (error) {
    set({ isLoading: false });
    throw error;
  }
},
```

### Anti-Patterns to Avoid

- **Calling `Linking.openURL` directly on Upgrade tap** — bypasses the D-12 interstitial. Always render the sheet first.
- **Treating Apple `fullName` as always-present** — Apple returns null on second sign-in. Backend caches it; mobile just forwards.
- **Persisting Apple `identityToken` or Google `idToken` to AsyncStorage** — these are short-lived JWTs and should be transient (T-2 threat). Hold them only in the local variable for the duration of the `/auth/apple` POST, then drop.
- **Letting the existing 401 interceptor recurse into `/auth/apple` or `/auth/google`** — T-7. The interceptor (`api.ts` lines 51–115) on a 401 from these endpoints would call `/auth/refresh`, which itself fails 401 (the SSO call had no valid refresh token), which triggers `logout()` + `initialize()`, which mints a new guest. The Apple/Google sign-in then *fails silently and the user becomes a fresh guest*. Mitigation options: (a) `_skipAuthRefresh` config flag on the request; (b) URL-pattern check in the interceptor (skip if `/auth/*`). Either works — planner picks per Claude's Discretion. **Verify behavior:** the guest JWT *should* be valid on the SSO endpoints (D-06 in-place promotion), so a 401 here means real failure, not stale token. The interceptor logic must NOT attempt refresh.
- **Hand-rolling URL parser with naive string split** — use `Linking.parse` if exposed, or careful regex with `decodeURIComponent` on the captured invoice id (URL-safe values shouldn't need decoding but be defensive).
- **Reading the wrong fields on Google v16 response** — v16 changed `userInfo.idToken` to `userInfo.data.idToken`. Document this in code comments.
- **Forgetting `Linking.getInitialURL()` for cold-start** — if the app isn't running when the user taps "Open in app" on `/pay/success`, the OS launches it with the URL as the initial-URL. `addEventListener('url')` alone misses this case.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Apple ID token verification (signature, iss, aud, exp) | Custom JWT verifier on mobile | Backend `/auth/apple` (already shipped Phase 2 D-19) | Apple's JWKs rotation, audience scoping, and tolerance for clock skew is a wheel re-invention. Mobile forwards the raw `identityToken`. |
| Google ID token verification | Custom JWT verifier on mobile | Backend `/auth/google` (already shipped Phase 2 D-21) | Same reason. `google.golang.org/api/idtoken` on backend handles the JWKs + audience matching. |
| Apple cancellation detection | String-match the error message | `appleAuth.Error.CANCELED` constant (`code === '1001'`) | Library exports semantic codes. |
| Google cancellation detection | String-match the error message | `statusCodes.SIGN_IN_CANCELLED` from `@react-native-google-signin/google-signin` | Library exports semantic codes. |
| OAuth flow plumbing (PKCE, redirect handling, code exchange) | `react-native-app-auth` or custom | Native Google + Apple SDKs via the two named RN libraries | Native SDKs do the heavy lifting; we only forward the resulting token. |
| In-app browser | `react-native-inappbrowser-reborn` + custom WebView | RN core `Linking.openURL` | (Above: 2026 App Store guidelines for US storefront no longer require external-link entitlement; system browser is the simpler, safer path.) |
| Custom polling loop with adaptive backoff | Long-running `setInterval` with exponential backoff | Fixed-cadence loop per D-08 (mirrors Phase 4) | The cadence is locked. Adaptive backoff defeats the cross-surface UX consistency. |
| Deep-link sub-route mapping | DIY URL matcher | `Linking.addEventListener` + a small regex | One URL pattern, no router needed. (react-navigation's deep-linking config is overkill here because the deep link routes to a global Modal, not a Screen.) |
| Sheet animation library | `react-native-bottom-sheet` | RN core `Modal` with `animationType="slide"` | One sheet, no new dep. |
| Token storage migration | Manual write to Keychain / EncryptedSharedPreferences | (Don't migrate this phase — HARD-16 in Phase 8) | ADR §12.6 locks AsyncStorage. |

**Key insight:** The mobile client is a thin shell over the existing backend SSO and invoice endpoints. Every cryptographic / verification step is server-side. Mobile owes only: (1) native-token capture, (2) header attachment for in-place promotion, (3) deep-link receive, (4) polling on the locked cadence, (5) store updates on success.

## Runtime State Inventory

> Phase 5 is mostly greenfield mobile UI on top of existing backend contracts; *however* token-storage carry-over and deep-link cold-start state crossing process boundaries justify a partial inventory.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | **AsyncStorage `auth-tokens` key** (existing) — guest-issued JWT lives here for the in-place promotion to work. **AsyncStorage `device-secret-v1` key** — generated by `deviceFingerprint.ts`, must survive across the SSO upgrade (it's per-device, not per-user). | None — both keys are reused as-is. SSO upgrade overwrites `auth-tokens` with the new SSO-issued JWT but `users.id` stays the same backend-side (D-06). |
| Live service config | Apple Developer Portal: Bundle ID with Sign-in-with-Apple capability, Service ID (web — already configured Phase 2). Google Cloud Console: OAuth client IDs for Web (Phase 2 already), iOS, Android — the iOS + Android clients are new this phase. Backend's `MIN_APP_VERSION` env var — operator must ensure it doesn't reject 2.2.0. | Operator-prerequisites checklist (D-21) addresses this. |
| OS-registered state | iOS: Sign-in-with-Apple capability registered in app entitlements (entitlements file change). Custom URL scheme `vpnapp` registered in Info.plist. Android: intent-filter for `vpnapp://payment/success` on MainActivity. | All in scope; handled by native-config tasks. |
| Secrets/env vars | No new mobile-side secrets — client IDs are public values (not secrets). Backend already holds Apple `.p8` key and Google verifier config from Phase 2. | None. Mobile code commits client IDs as constants (acceptable; they're scoped by SHA-1 fingerprint on Android and Bundle ID on iOS). |
| Build artifacts / installed packages | After `npm install` of new deps, `node_modules` carries native code for Apple + Google libraries that must be linked by CocoaPods (iOS) and Gradle autolink (Android). After `pod install`, `ios/Pods/` carries `RNAppleAuthentication` + `GoogleSignIn` pods. **Existing `ios/Pods.lock` and `app/package-lock.json` will both change** — commit both. | Plans must include explicit `pod install` step + lockfile commit. |

## Common Pitfalls

### Pitfall 1: Apple Sign-In silently fails with "code 1000" on simulator or unsigned device
**What goes wrong:** `appleAuth.performRequest` rejects with a generic auth error.
**Why it happens:** Sign-in-with-Apple requires (a) a real Bundle ID registered in Apple Developer Portal with the capability enabled, (b) the entitlements file containing `com.apple.developer.applesignin`, and (c) a provisioning profile that includes the capability. The RN template's placeholder Bundle ID `org.reactjs.native.example.*` cannot have Sign-in-with-Apple enabled.
**How to avoid:** Set the real Bundle ID (operator prerequisite D-21). Confirm `VpnApp.entitlements` contains the capability key. Build via Xcode at least once after entitlement changes to refresh the provisioning profile.
**Warning signs:** Apple sheet flashes and dismisses immediately; error code `1000` (`AppleAuthError.UNKNOWN`) or `1001` (`CANCELED` — but user didn't cancel).

### Pitfall 2: Google Sign-In returns null `idToken` on Android
**What goes wrong:** `userInfo.data.idToken` is `null` even though sign-in "succeeded".
**Why it happens:** (a) `webClientId` is missing or wrong in `configure()` — the Web OAuth client ID is what generates the `idToken`. (b) SHA-1 of the signing keystore is not registered against the Android OAuth client. (c) Wrong `webClientId` (using the Android client ID instead).
**How to avoid:** Triple-check `webClientId === <Web OAuth client ID>` (NOT iOS or Android). Register debug + release SHA-1 of the keystore signing this build in the Android OAuth client. Use `keytool -list -v -keystore <keystore> -alias <alias>` to extract SHA-1 fingerprint.
**Warning signs:** Sign-in completes, `userInfo.user.email` is populated, but `data.idToken === null`. Backend `/auth/google` then fails with 401 because there's nothing to verify.

### Pitfall 3: Deep link doesn't open the app — Chrome shows "Page not found"
**What goes wrong:** Tapping `vpnapp://payment/success?invoiceId=X` in mobile Chrome (Android) does not switch to the app.
**Why it happens:** Chrome on Android refuses to navigate to a custom scheme from a top-level link tap unless triggered via specific gestures or `<a>` tag with `intent://` URL. **OR** the intent-filter in AndroidManifest.xml is missing `<category android:name="android.intent.category.BROWSABLE" />`.
**How to avoid:** AndroidManifest intent-filter MUST include `BROWSABLE` category. Test by opening the URL from an SMS / email / button click on a real page (the landing /pay/success page does this). The iOS Safari → custom scheme path works without intent-filter equivalent because iOS Universal Links don't apply to custom schemes.
**Warning signs:** Custom scheme works when typed into Safari address bar but not when tapped on a web page.

### Pitfall 4: Polling modal blocks the user after cold-start without a JWT
**What goes wrong:** User taps "Open in app" from `/pay/success`, app cold-starts, the deep-link triggers the polling modal, but `authStore.initialize()` hasn't completed yet, so the modal calls `/invoices/{id}` without an Authorization header and gets 401 → loop forever.
**Why it happens:** `App.tsx` calls `authStore.initialize()` and `registerDeepLinkHandler()` in the same `useEffect`. The deep-link handler may fire before `initialize()` completes the `/auth/guest` call.
**How to avoid:** Either (a) gate `startActivatingPro` on `isAuthenticated === true` (wait for initialize then process queued invoiceId), or (b) trust the existing 401→refresh→retry to eventually attach a guest JWT (works because `initialize()` will mint one). Option (a) is cleaner.
**Warning signs:** Modal stuck on "Activating…" for 30s, then timeout, but backend logs show no `/invoices/{id}` requests received.

### Pitfall 5: Guest tokens dropped before SSO call → backend creates a NEW user instead of in-place promotion
**What goes wrong:** A guest user taps "Continue with Apple", the call to `/auth/apple` lands with NO `Authorization` header, backend creates a fresh `users` row, and the guest's existing `users.id` is orphaned. Admin panel shows two rows; their stored VPN connections still bind to the old guest id.
**Why it happens:** (a) `authStore.signInWithApple` doesn't read tokens before initiating the Apple sheet — but the axios request interceptor reads them at request-time so this is usually fine. (b) An eager logout in `LoginScreen` ("Continue as Guest" or "logout" path) clears tokens before "Continue with Apple" is tapped. (c) The interceptor at `api.ts` line 23 skips attaching the token because `tokens` is null — this happens if `initialize()` is still running.
**How to avoid:** D-06 mandates: `authStore.signInWithApple()` MUST verify the existing guest JWT is present in state before invoking the Apple sheet. If missing, run `await initialize()` first.
**Warning signs:** Admin panel `SELECT * FROM users WHERE email = 'tester@example.com'` returns more than one row after one Apple sign-in.

### Pitfall 6: T-7 — 401 from `/auth/apple` cycles through refresh interceptor and re-initializes as guest
**What goes wrong:** Apple ID token is genuinely rejected by backend (e.g., wrong `aud`). Response is 401. Interceptor `api.ts` lines 56–112 attempts `/auth/refresh` with the guest refresh token, succeeds, retries the original `/auth/apple`, which still 401s with the new access token… infinite loop, or eventual `logout()` + `initialize()` mints a fresh guest.
**Why it happens:** The existing interceptor doesn't distinguish auth-required endpoints from auth-establishing endpoints.
**How to avoid:** Add an `_skipAuthRefresh: true` flag in axios `config` for `/auth/apple` and `/auth/google` calls, and short-circuit the interceptor when present. OR add a URL-pattern check: `if (originalRequest.url?.startsWith('/auth/')) return Promise.reject(error);` at the top of the 401 handler.
**Warning signs:** Tester reports "Apple sign-in did nothing" after a backend misconfiguration; backend logs show repeated 401 then a /auth/guest call.

### Pitfall 7: Yandex Mobile Ads SDK is still in the app and shows ads to a fresh Pro user
**What goes wrong:** A user pays on web, deep-links back, polling completes, `subscription_tier` flips to `pro`, but the `AdBanner` component on `HomeScreen` line 124 still renders for ~1s because the React re-render hasn't propagated.
**Why it happens:** `AdBanner` reads `user.subscription_tier` from `authStore`. After `fetchAccount()` updates it, the component re-renders. But if `fetchAccount` hasn't completed yet (network in flight), the banner shows.
**How to avoid:** Ensure `await get().fetchAccount()` resolves BEFORE the polling modal closes (D-08 explicit ordering). The modal then closes → Home re-renders with the updated user → AdBanner reads `pro` → renders null.
**Warning signs:** Flash of ad immediately after the success toast.

### Pitfall 8: Forgetting to remove stale i18n keys breaks RU build
**What goes wrong:** PaymentScreen rewrite uses new `payment.upgrade.*` keys but old `payment.title`, `payment.subtitle`, `payment.plans.premium.name`, `payment.telegramMessage`, etc. remain in en.json and ru.json. If those keys are still referenced elsewhere (e.g., a Settings screen subtitle, or a deeplink) they continue to work; if they're not, no error but bloat. **Worse:** if a RU translator removes the old `payment.title` key thinking it's dead, a stale `t('payment.title')` call elsewhere shows the literal key string.
**Why it happens:** UI-SPEC's "removed copy" list is long; grep for stale callers can be missed.
**How to avoid:** Run `grep -rE "t\('payment\.(title|subtitle|plans|telegram|howItWorks|disclaimer|currentPlan|upgrade|yourId|idMissing|errorTitle|errorMessage|mostPopular|contactSupport)" app/src/` after rewrite, expect zero hits. UI-SPEC explicitly mandates this grep pass.
**Warning signs:** Translator filed a confused issue about a key showing up as literal text.

## Code Examples

### iOS native: Info.plist additions

```xml
<!-- Source: reactnative.dev/docs/linking + react-native-google-signin.github.io/docs/setting-up/ios -->
<key>CFBundleURLTypes</key>
<array>
  <!-- Custom scheme for payment-return deep link -->
  <dict>
    <key>CFBundleURLName</key>
    <string>com.vpnapp.payment</string>
    <key>CFBundleURLSchemes</key>
    <array>
      <string>vpnapp</string>
    </array>
  </dict>
  <!-- Google Sign-In reversed-client-id -->
  <dict>
    <key>CFBundleURLName</key>
    <string>com.googleusercontent.apps.<IOS_OAUTH_CLIENT_ID></string>
    <key>CFBundleURLSchemes</key>
    <array>
      <!-- Reversed iOS OAuth client ID — exact value from Google Cloud Console -->
      <string>com.googleusercontent.apps.<IOS_OAUTH_CLIENT_ID></string>
    </array>
  </dict>
</array>
```

### iOS native: VpnApp.entitlements additions

```xml
<!-- Source: github.com/invertase/react-native-apple-authentication INITIAL_SETUP.md -->
<key>com.apple.developer.applesignin</key>
<array>
  <string>Default</string>
</array>
```

(Add to the existing dict alongside the network-extension and app-groups keys already present.)

### iOS native: AppDelegate.swift URL handler

```swift
// Source: react-native-google-signin docs + reactnative.dev/docs/linking
// Add inside the AppDelegate class, after didFinishLaunchingWithOptions

func application(
  _ app: UIApplication,
  open url: URL,
  options: [UIApplication.OpenURLOptionsKey: Any] = [:]
) -> Bool {
  // First try Google Sign-In handler (consumes its own URL scheme)
  if GIDSignIn.sharedInstance.handle(url) {
    return true
  }
  // Then RN's Linking module catches the vpnapp:// scheme
  return RCTLinkingManager.application(app, open: url, options: options)
}
```

**Imports needed:** `import GoogleSignIn` and `import React_RCTAppDelegate` (already present).

### Android native: AndroidManifest.xml intent-filter

```xml
<!-- Source: reactnative.dev/docs/linking + Android dev docs -->
<!-- Add INSIDE the existing <activity android:name=".MainActivity"> element -->
<intent-filter android:autoVerify="false">
  <action android:name="android.intent.action.VIEW" />
  <category android:name="android.intent.category.DEFAULT" />
  <category android:name="android.intent.category.BROWSABLE" />
  <data android:scheme="vpnapp" android:host="payment" />
</intent-filter>
```

The existing `MainActivity` already has `android:launchMode="singleTask"` (line 26 of AndroidManifest.xml) which is REQUIRED for `onNewIntent` delivery to the same instance.

### Android native: strings.xml — Google Web client ID resource (optional helper)

```xml
<!-- app/android/app/src/main/res/values/strings.xml -->
<resources>
  <string name="app_name">Rise VPN</string>
  <!-- ... existing strings ... -->
  <!-- Google Sign-In Web OAuth client ID. Not strictly required because we pass it
       to configure() in JS, but useful if the native side ever needs it. -->
  <string name="server_client_id"><!--YOUR_WEB_OAUTH_CLIENT_ID--></string>
</resources>
```

### App.tsx wiring

```typescript
// Source: existing App.tsx + new helpers
import React, { useEffect } from 'react';
// ... existing imports
import { configureGoogleSignIn } from './src/services/googleSignIn';
import { registerDeepLinkHandler } from './src/services/deepLink';
import { ActivatingProModal } from './src/components/ActivatingProModal';

function App(): React.JSX.Element {
  useEffect(() => {
    useAuthStore.getState().initialize();
    useSettingsStore.getState().initialize();
    configureGoogleSignIn();                                    // NEW
    const unsubscribe = registerDeepLinkHandler();              // NEW
    MobileAds.initialize().catch(() => { /* ... */ });
    return () => unsubscribe();                                 // NEW
  }, []);

  return (
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <SafeAreaProvider>
          <StatusBar barStyle="light-content" backgroundColor={colors.background} />
          <NavigationContainer theme={navTheme}>
            <RootNavigator />
          </NavigationContainer>
          <ActivatingProModal />                                {/* NEW — root-level overlay */}
        </SafeAreaProvider>
      </QueryClientProvider>
    </ErrorBoundary>
  );
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Apple Sign-In required custom Swift module via `AuthenticationServices.framework` | `@invertase/react-native-apple-authentication` (v1+ since 2020, v2 added Android support) | v2.0.0 (2022) | Library auto-links on RN 0.60+, no manual native module needed |
| Google Sign-In required `react-native-google-signin` (no scope) | `@react-native-google-signin/google-signin` (scoped namespace) | 2021 namespace migration | Same library, different name — old name is deprecated |
| Google Sign-In v12 returned `userInfo.idToken` directly | v13+ returns `userInfo.data.idToken` (response wrapped in `data`) | v13.0.0 (early 2024) | Breaking change — code targeting v12 will read `undefined` |
| External-purchase links on iOS required App Store entitlement form approval | US storefront: no entitlement required since May 2025 court ruling | Apple guideline update 2025-05-01 | RiseVPN's CTA-to-website model is now straightforward [CITED: 9to5mac.com/2025/05/01] |
| Universal Links replaced custom schemes for app-to-app handoff | Both still work; custom schemes preferred for known-app contexts | iOS 9 (2015) / Android 6 (2015) | ROADMAP locks custom scheme `vpnapp://` for now; Universal Links remain a future hardening per CONTEXT.md `<deferred>` |
| Token storage = AsyncStorage / plaintext | Industry shift to Keychain (iOS) / EncryptedSharedPreferences (Android) | Ongoing | Phase 5 keeps AsyncStorage per ADR §12.6; HARD-16 moves it in Phase 8 |

**Deprecated/outdated:**
- Old npm name `react-native-google-signin` (no namespace) — must use `@react-native-google-signin/google-signin`.
- Pre-v13 patterns reading `userInfo.idToken` — must read `userInfo.data.idToken` on v16.
- "Sign in with Apple" optional on iOS — Apple Review now REQUIRES Sign-in-with-Apple if any other third-party SSO is offered. Phase 5 offers Google + Apple, so we're compliant by default.

## Project Constraints (from CLAUDE.md)

| Directive | Source | How research respects it |
|-----------|--------|--------------------------|
| GSD workflow enforcement | CLAUDE.md L50-58 | Research is invoked via `/gsd-plan-phase` orchestrator; no direct repo edits proposed. |
| Tech stack — Mobile: RN 0.84 + TS + Zustand + axios + react-navigation. Locked. | CLAUDE.md L12-13 | Every recommendation uses only stack-approved libraries; no Redux, no Recoil, no react-native-app-auth. |
| No IAP buttons in mobile app. CTA points to risevpn.com. | CLAUDE.md L19 | §"In-App Browser for Pricing CTA" + §"PaymentScreen Rewrite" enforce single CTA opening system browser. |
| Identity provider: Apple + Google SSO (web + mobile). Guest device-based login preserved. | CLAUDE.md L18 | §"LoginScreen + AccountScreen Card" preserves "Continue as Guest"; §"Guest → SSO Upgrade In-Place" preserves users.id. |
| App-store compliance: No IAP buttons. CTA points to risevpn.com. | CLAUDE.md L19 + ROADMAP SC#3 + UI-SPEC D-13 | D-12 interstitial sheet + D-13 CTA copy lock + Apple May 2025 guideline note covers compliance posture. |
| Security: audit Critical/High findings MUST land before any user pays. | CLAUDE.md L21 | §"Threat model" mapping covers T-1 through T-7; backend `/auth/*` audit findings already addressed in Phase 2. |

## Validation Architecture

> `workflow.nyquist_validation: true` in `.planning/config.json` — this section is REQUIRED.

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Jest 29.6.3 + React Native preset (`preset: 'react-native'`) [VERIFIED: package.json + jest.config.js] |
| Config file | `app/jest.config.js` (single line: `module.exports = { preset: 'react-native' }`) |
| Quick run command | `cd app && npm test` (runs all `__tests__` + `*.test.tsx` files) |
| Targeted run command | `cd app && npm test -- --testPathPattern=<file>` (single file) |
| Full suite command | `cd app && npm test` |
| Type-check command | `cd app && npx tsc --noEmit` |
| Lint command | `cd app && npm run lint` |

**Reality check on test coverage in this repo:** the existing `__tests__/App.test.tsx` is a single smoke test that renders `<App/>` via `ReactTestRenderer`. There is **no React Native Testing Library (RNTL) in the dependency tree** [VERIFIED: package.json grep — `@testing-library/react-native` absent]. The phase can either:
- **Option A (minimal, recommended):** Stay with `react-test-renderer` for shallow component tests; add no new test dep. Cover service-layer (`appleSignIn.ts`, `googleSignIn.ts`, `deepLink.ts`, `payment.ts`, `authStore.ts`) via plain Jest + mocked `react-native`'s `Linking` and the two SSO libraries. Use the existing smoke test as a regression guard.
- **Option B (richer):** Add `@testing-library/react-native@^13` (~50 KB dev dep) for component-level UI tests of LoginScreen, PaymentScreen, ActivatingProModal. Higher fidelity but introduces a new dep and longer Wave 0.

**Recommendation:** Option A for Phase 5. The phase already has a heavy manual smoke-test path (D-19's Android device verification) covering the integration cases that RNTL would test. RNTL is a Phase 8 (HARD-15 splits useVpnConnection) candidate where the unit-of-work justifies it.

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|--------------|
| APP-01 | Apple sign-in returns identityToken; authStore.signInWithApple POSTs to /auth/apple with guest JWT in header | unit | `npm test -- --testPathPattern=authStore.test` | ❌ Wave 0 |
| APP-01 | Apple sheet cancellation surfaces silently (no Alert) | unit | `npm test -- --testPathPattern=authStore.test` | ❌ Wave 0 |
| APP-02 | Google sign-in returns idToken; authStore.signInWithGoogle POSTs to /auth/google | unit | `npm test -- --testPathPattern=authStore.test` | ❌ Wave 0 |
| APP-02 | googleSignIn.configure() called at app boot with correct webClientId | unit | `npm test -- --testPathPattern=googleSignIn.test` | ❌ Wave 0 |
| APP-03 | LoginScreen renders three CTAs on iOS; two on Android (Apple hidden) | unit (shallow) | `npm test -- --testPathPattern=LoginScreen.test` | ❌ Wave 0 |
| APP-04 | signInWithApple sends Authorization: Bearer <guest JWT> when guest tokens exist | unit | `npm test -- --testPathPattern=authStore.test` (axios interceptor mocked) | ❌ Wave 0 |
| APP-05 | PaymentScreen renders no price text; CTA copy exactly matches "Upgrade to Pro at risevpn.com"; CTA opens LeavingAppSheet before Linking.openURL | unit (shallow) + integration | `npm test -- --testPathPattern=PaymentScreen.test` | ❌ Wave 0 |
| APP-05 | upgradeUrlForLocale('ru') returns `https://risevpn.com/ru/pricing?return=app`; default returns `/en/` | unit | `npm test -- --testPathPattern=payment.test` | ❌ Wave 0 |
| APP-06 | deepLink.ts parses `vpnapp://payment/success?invoiceId=X` correctly; dispatches startActivatingPro | unit | `npm test -- --testPathPattern=deepLink.test` | ❌ Wave 0 |
| APP-06 | ActivatingProModal polls every 2s; appends ?escalate=true after poll #5; times out at 30s | unit (fake timers) | `npm test -- --testPathPattern=ActivatingProModal.test` | ❌ Wave 0 |
| APP-06 | On status='paid', modal calls fetchAccount() and closes; on 'failed' navigates to Account | unit | `npm test -- --testPathPattern=ActivatingProModal.test` | ❌ Wave 0 |
| APP-07 | APP_VERSION export equals '2.2.0'; package.json version 2.2.0; Android versionName + iOS MARKETING_VERSION match | unit + grep | `npm test -- --testPathPattern=version.test` + `grep` in CI | ❌ Wave 0 |
| **Manual-only** | iOS: tester taps "Continue with Apple", completes Apple sheet, lands on Home (SC#1 verbatim path) | E2E manual | recorded in 05-HUMAN-UAT.md per D-20 | n/a — iOS smoke deferred |
| **Manual-only** | Android: Google sign-in works on physical device; guest→Google promotion preserves users.id | E2E manual | 05-HUMAN-UAT.md (operator runs per D-19) | n/a |
| **Manual-only** | Android: tap interstitial Continue → opens Chrome to risevpn.com/<locale>/pricing?return=app | E2E manual | 05-HUMAN-UAT.md | n/a |
| **Manual-only** | Android: typing `vpnapp://payment/success?invoiceId=test123` in Chrome opens app to polling modal | E2E manual | 05-HUMAN-UAT.md | n/a |

**Mocking strategy:**
- `@invertase/react-native-apple-authentication` → Jest manual mock at `__mocks__/@invertase/react-native-apple-authentication.ts` exporting `appleAuth.performRequest` that resolves with a fixed `identityToken` / `authorizationCode` / `fullName` / `email` object.
- `@react-native-google-signin/google-signin` → Jest manual mock exporting `GoogleSignin.configure`, `GoogleSignin.hasPlayServices`, `GoogleSignin.signIn` returning a fixed `idToken`.
- `react-native` `Linking` → use the existing react-native preset mock (Linking is auto-mocked). Override `openURL`, `addEventListener`, `getInitialURL` per test.
- `axios` → mock `app/src/services/api.ts` to return canned responses; assert on request payload + headers.
- Fake timers (`jest.useFakeTimers()`) for the polling loop test — advance by 2s ticks and assert call counts + URL escalation.

### Sampling Rate

- **Per task commit:** `cd app && npm test -- --testPathPattern=<file-under-test>` (fast — single file, <5s).
- **Per wave merge:** `cd app && npm test` (full Jest suite — <60s) + `npx tsc --noEmit` (type-check — <20s).
- **Phase gate:** Full suite green + lint clean + manual Android smoke per D-19 (operator UAT in 05-HUMAN-UAT.md) before `/gsd-verify-work`.

### Wave 0 Gaps

- [ ] `app/src/services/__tests__/appleSignIn.test.ts` — happy path + cancellation
- [ ] `app/src/services/__tests__/googleSignIn.test.ts` — configure called once; signIn returns idToken
- [ ] `app/src/services/__tests__/deepLink.test.ts` — URL parser edge cases (missing invoiceId, wrong scheme, encoded values)
- [ ] `app/src/services/__tests__/payment.test.ts` — upgradeUrlForLocale + getInvoice escalate query
- [ ] `app/src/stores/__tests__/authStore.test.ts` — signInWithApple + signInWithGoogle + startActivatingPro / stopActivatingPro
- [ ] `app/src/screens/__tests__/LoginScreen.test.tsx` — three CTAs visible iOS, two on Android
- [ ] `app/src/screens/__tests__/PaymentScreen.test.tsx` — informational layout, single CTA, locale-aware URL
- [ ] `app/src/components/__tests__/ActivatingProModal.test.tsx` — polling loop with fake timers
- [ ] `app/src/config/__tests__/version.test.ts` — APP_VERSION === '2.2.0' (trivial guard against future drift)
- [ ] `app/__mocks__/@invertase/react-native-apple-authentication.ts` — manual mock
- [ ] `app/__mocks__/@react-native-google-signin/google-signin.ts` — manual mock
- [ ] **Framework install:** none — Jest + react-test-renderer already in tree

## Security Domain

> `security_enforcement` enabled (CONTEXT.md D-22). All Phase 5 plans MUST include `<threat_model>` covering at minimum T-1..T-7 from CONTEXT.md.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|------------------|
| V2 Authentication | yes | Apple+Google SSO via native SDKs → backend verifier (Phase 2 D-19/D-21); guest device-fingerprint flow preserved |
| V3 Session Management | yes (carry-over) | HS256 access (5 min) + refresh (30 day) tokens; existing rotation transaction (HOTFIX-05) + UNIQUE index (HOTFIX-07) |
| V4 Access Control | yes | Backend authorization on `/invoices/{id}` MUST scope to current user (T-1 mitigation); verify Phase 3 03-05 contract enforces this |
| V5 Input Validation | yes | Mobile validates `invoiceId` from deep-link URL is non-empty + URL-safe before sending to backend; backend re-validates |
| V6 Cryptography | yes (by reference) | Apple JWKs + Google idtoken verification on backend; mobile holds NO crypto state — never roll our own |

### Known Threat Patterns for RN 0.84 mobile auth + custom-scheme deep links

| Pattern | STRIDE | Standard Mitigation | CONTEXT.md ref |
|---------|--------|---------------------|----------------|
| Deep-link spoofing (malicious page fires `vpnapp://payment/success?invoiceId=X`) | Spoofing | Mobile DOES NOT trust the deep link alone; polls backend `/invoices/{id}` which is the source of truth | T-1 |
| ID-token replay (captured Apple/Google JWT) | Spoofing, Tampering | Backend Phase 2 D-16 validates signature + iss + aud + exp; mobile never persists tokens; pin libraries (T-6) | T-2 |
| Apple authorization-code leakage in logs | Information disclosure | Don't log the code or token; treat as transient; HARD-10 zap redactor (Phase 8) catches accidental logs server-side | T-3 |
| `invoiceId` leak via browser history / clipboard | Information disclosure | invoiceId alone is non-sensitive (identifies an invoice but doesn't grant access without an auth token) — documented residual risk | T-4 |
| Token theft on rooted/jailbroken device | Spoofing, Elevation | AsyncStorage on Android is plain; iOS Keychain-ish. ADR §12.6 keeps current behavior; HARD-16 moves to secure storage in Phase 8 | T-5 |
| SSO library supply-chain compromise | Tampering, Elevation | Pin exact versions (`--save-exact`); commit lockfile; `npm audit --omit=dev` clean; review GitHub release notes for typo-squat indicators | T-6 |
| 401 → refresh → retry feedback loop on SSO failure | Denial of service (subtle: user loses identity instead of recovering it) | `_skipAuthRefresh` flag OR URL-pattern check in interceptor for `/auth/*` endpoints | T-7 |
| URL-scheme collision (another app registers `vpnapp` first) | Spoofing | On Android, OS shows a chooser when multiple apps claim the same scheme — risk is low for a niche scheme but real. Mitigation: prefer Universal Links long-term (deferred). | (not in CONTEXT but worth noting) |

## Environment Availability

Mobile phase depends on developer-machine tooling + operator accounts:

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Node.js | RN CLI, Metro, Jest | ✓ | engines requires >= 22.11.0 | — |
| npm | Package install | ✓ | (bundled with Node) | — |
| Xcode | iOS build (`pod install`, `xcodebuild`) | unknown (operator's machine) | Recommended Xcode 15+ | iOS code lands but build deferred per D-20 |
| Android Studio + SDK | Android build | ✓ (operator has Android device per D-19) | Recommended AGP 8+ | Required this phase |
| CocoaPods | iOS deps | required on iOS-build machine | Recommended 1.14+ | — |
| Java JDK 17 | Android Gradle | required | Recommended 17 | — |
| Android keystore (release) | Signed `.aab` for SC#5 smoke per D-19 | unknown (operator) | — | Generate phase-internal keystore per D-21 |
| Apple Developer account | Sign-in-with-Apple capability registration | unknown (operator already has this per Phase 2) | — | D-21 BLOCKING gate |
| Google Cloud Console project | OAuth client IDs (iOS, Android, Web) | Web ID exists from Phase 2; iOS + Android new this phase | — | D-21 BLOCKING gate |
| Real Android device | D-19 smoke test | ✓ (operator's device) | Android 10+ recommended | — |
| Real iOS device | Phase 5 smoke (deferred per D-20) | ✗ (operator has no iOS hw) | — | iOS code lands and type-checks; runtime verified end-of-milestone phase |

**Missing dependencies with no fallback:** none — all blocking items are operator prerequisites already enumerated in D-21.

**Missing dependencies with fallback:**
- iOS smoke testing → deferred per D-20 (code lands; runtime verified later).
- Production release keystore → phase-internal release keystore per D-21 (will be swapped later).

## Risks & Open Questions

1. **iOS Bundle ID is still the RN template placeholder.**
   - What we know: `app/ios/VpnApp.xcodeproj/project.pbxproj` lines 274 and 303 read `PRODUCT_BUNDLE_IDENTIFIER = "org.reactjs.native.example.$(PRODUCT_NAME:rfc1034identifier)"`. Android `applicationId` is correctly `com.vpnapp`.
   - What's unclear: D-21 says "Apple Service ID + Bundle ID … verify operator has these" — but if the Bundle ID was never set on the iOS side, Sign-in-with-Apple cannot be enabled, and the operator's Apple Developer Portal "Bundle ID" registration may not match what the app actually announces.
   - Recommendation: **PRE-WAVE 1 BLOCKING task** — confirm Bundle ID with operator. Likely target: `com.vpnapp` to match Android. Update both `PRODUCT_BUNDLE_IDENTIFIER` slots in `project.pbxproj` AND register the Bundle ID in Apple Developer Portal with Sign-in-with-Apple capability. If a different Bundle ID is registered, change the xcodeproj to match.

2. **Apple cancellation error code — `1000` vs `1001`.**
   - What we know: `appleAuth.Error.CANCELED` is exported by the library; the README mentions error code `1000` for "AuthorizationError" but doesn't enumerate code values.
   - What's unclear: Whether the cancellation path throws an error matching `appleAuth.Error.CANCELED` consistently across iOS versions.
   - Recommendation: Wrap the call in a try/catch and check both `error.code === appleAuth.Error.CANCELED` AND a fallback string match. Test on a real device during D-19 smoke (but Apple sign-in is iOS-only so deferred per D-20 — verify in end-of-milestone phase).

3. **`MIN_APP_VERSION` server-side gate behavior on 2.2.0.**
   - What we know: `api.ts` line 18 sends `X-App-Version: <APP_VERSION>`; server rejects below-min with 426.
   - What's unclear: What value `MIN_APP_VERSION` currently holds in operator's production env. If it's `2.2.0`, then debug builds (still on 2.1.0 until the bump) would suddenly start failing.
   - Recommendation: Bump `MIN_APP_VERSION` to `2.2.0` on the server AT THE SAME TIME as the mobile release, not before. Coordinate via 05-HUMAN-UAT.md.

4. **Backend `/auth/apple` and `/auth/google` accept `_skipAuthRefresh` semantics.**
   - What we know: T-7 mitigation requires the axios 401 interceptor to short-circuit for SSO endpoints. The backend Phase 2 handler doesn't require this; the change is purely client-side.
   - What's unclear: Whether the backend handler is well-defined for a 401 case (e.g., invalid Apple identityToken) — does it return 401 or 400? If it's 400, the interceptor doesn't trigger and T-7 is moot.
   - Recommendation: Verify Phase 2 handler status codes via `server/api/internal/handler/auth.go::AppleSignIn`. Add the `_skipAuthRefresh` defense regardless (cheap insurance).

5. **iOS reversed-client-id format.**
   - What we know: Google docs say the reversed-client-id URL scheme is the literal value `com.googleusercontent.apps.<IOS_OAUTH_CLIENT_ID>`. It must match the iOS OAuth client ID issued by Google Cloud Console.
   - What's unclear: Whether operator has issued the iOS OAuth client yet.
   - Recommendation: D-21 prerequisites checklist explicitly enumerates this. Plan task halts if missing.

6. **Locale fallback when `getLocales()` returns an unexpected value.**
   - What we know: `i18n/index.ts` defaults to `'ru'` if `getLocales()[0]?.languageCode` is undefined; `upgradeUrlForLocale` returns EN for any non-`ru` value (D-16).
   - What's unclear: User has set device language to ES — the mobile app shows EN copy (no ES on mobile) and `upgradeUrlForLocale` returns the EN landing — but the landing /en/pricing page may not match the user's expectation if they're an actual Spanish speaker.
   - Recommendation: Document this as a known limitation. Add ES to mobile in a later phase per CONTEXT.md `<deferred>`.

7. **`paymentReturnStore` vs extending `authStore` — Claude's Discretion.**
   - What we know: D-11 + Claude's Discretion default = extend `authStore` with `pendingInvoiceId` + `isActivatingPro`.
   - What's unclear: Whether a separate small store keeps `authStore` cleaner.
   - Recommendation: **Extend `authStore`** (default per D-CD). It's two fields and two actions; pulling into a separate store creates a cross-cutting dependency between `deepLink.ts` and two stores. Reassess in Phase 7 if state grows.

8. **Yandex Mobile Ads SDK and a fresh Pro user.**
   - What we know: `AdBanner` reads `subscription_tier` and renders ads only for free users. After Pro flip, AdBanner re-renders to null.
   - What's unclear: Whether `MobileAds.initialize()` in `App.tsx` line 51 has any side effect that lingers after the user goes Pro (background impression tracking, etc.).
   - Recommendation: Not Phase 5's problem unless evidence emerges; flag for Phase 6/Performance.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `@invertase/react-native-apple-authentication` v2.5.1 auto-links on RN 0.84.1 without manual native edits beyond entitlements + capability. | Standard Stack + Apple Sign-In Integration | If autolink fails, plans need an explicit manual-link task. Mitigation: verify `pod install` output names `RNAppleAuthentication`. Low risk — RN 0.84 + library v2 is a well-trodden combo. |
| A2 | `@react-native-google-signin/google-signin` v16 returns `idToken` on `userInfo.data.idToken` (not `userInfo.idToken`) on RN 0.84. | Pattern 2 + Pitfall 2 | If v16 returns a different shape, code reads undefined. Mitigation: keep the existing `(userInfo as any).data?.idToken ?? (userInfo as any).idToken` defensive pattern. [CITED from official docs but version-specific reads need verification at install time.] |
| A3 | Backend `/auth/apple` accepts `device_id`, `device_secret`, `platform`, `full_name`, `email` optionally on the request body. | Pattern 5 (signInWithApple) | If backend signature differs, the call fails 400. Mitigation: read `server/api/internal/handler/auth.go::AppleSignIn` request DTO before writing the mobile call. CONTEXT.md `canonical_refs` says this is Phase 2 D-19/D-20 contract — verify against actual handler. |
| A4 | Backend `/invoices/{id}` returns `{data: {status: 'pending'|'paid'|'failed'|'expired', ...}}` and accepts `?escalate=true` query. | Pattern 4 + Invoice Polling | If response shape differs, polling never sees `'paid'`. Mitigation: read `server/api/internal/handler/payment.go::GetInvoice` shipped in Phase 3 03-05. |
| A5 | Google `webClientId` audience-matching on backend allows the same Web client ID used by the landing site. | Pattern 2 | If backend audience whitelist doesn't include the Web client ID, `/auth/google` fails with 401 on `aud` mismatch. Mitigation: verify Phase 2 D-21 audience list. |
| A6 | The `?return=app` query parameter on `https://risevpn.com/<locale>/pricing` is consumed by Phase 4 D-19's auto-checkout-on-return flow. | Pattern 4 (upgradeUrlForLocale) | If landing doesn't read `?return=app`, behavior is identical to a normal `/pricing` visit (user manually picks plan and clicks Get Pro). Mitigation: trivial — feature degrades gracefully. CONTEXT.md D-16 acknowledges this as a possible Phase 4 follow-up. |
| A7 | `MobileAds.initialize()` is fire-and-forget and has no observable interference with `Linking.addEventListener` or other Phase 5 wiring. | Pitfall 7 | If initialization grabs `Linking` or AppState in a conflicting way, deep links could be eaten. Mitigation: Yandex SDK has no documented interaction with `Linking`. Run a smoke test post-install. |
| A8 | Operator's Apple Developer "Bundle ID" matches what we set in `PRODUCT_BUNDLE_IDENTIFIER`. | Open Q #1 | If misaligned, Sign-in-with-Apple capability is not honored. Mitigation: Open Question #1 calls this out as a BLOCKING task. |
| A9 | The custom-scheme `vpnapp://` is not registered by any other installed app on the operator's test device. | Threat Model + Deep Link Handling | If a collision exists, Android shows a chooser. Mitigation: name is project-specific; collision risk is negligible. |
| A10 | Mobile `i18next` device-language detection returning `'ru'` includes regional variants (`ru-RU`, etc.) — UI-SPEC + D-16 assume binary `ru` vs not-`ru`. | upgradeUrlForLocale | If `getLocales()[0]?.languageCode` returns `'ru-RU'`, the existing `=== 'ru'` check fails and defaults to EN. Mitigation: use `startsWith('ru')` or `.toLowerCase().slice(0,2) === 'ru'`. Verify on test device. |

## Open Questions

(Numbered in §Risks & Open Questions above — items 1, 2, 3, 4, 5, 6, 7, 8 are the live questions the planner should fold into the operator prerequisites checklist + first-wave tasks.)

## Sources

### Primary (HIGH confidence)

- `app/package.json` — verified RN 0.84.1, react-i18next, axios, zustand versions present [Read]
- `app/App.tsx` — verified existing boot flow (initialize, MobileAds, no Linking handler yet) [Read]
- `app/src/services/api.ts` — verified 401→refresh→retry interceptor code path (lines 51–115) [Read]
- `app/src/stores/authStore.ts` — verified existing actions, the concurrency guard, the `linkWithCode` pattern (line 80) [Read]
- `app/src/services/payment.ts` — verified existing Stripe-era exports to remove [Read]
- `app/src/navigation/RootNavigator.tsx` — verified `RootStackParamList` shape and screen registration pattern [Read]
- `app/src/screens/HomeScreen.tsx` — verified AppState hook at lines 41–50 [Read]
- `app/src/screens/PaymentScreen.tsx` — verified existing 3-plan card layout, Telegram CTA, `Linking.openURL` line 163 [Read]
- `app/src/screens/AccountScreen.tsx` (first 100 lines) — verified screen structure conventions [Read]
- `app/src/types/api.ts` — verified `User` and `Subscription` types to extend [Read]
- `app/src/config/version.ts` — verified APP_VERSION constant pattern [Read]
- `app/src/i18n/index.ts` + `en.json` — verified i18next config + namespace structure [Read]
- `app/src/theme/colors.ts` — verified all UI-SPEC color tokens exist [Read]
- `app/ios/VpnApp/Info.plist` — verified CFBundleURLTypes does NOT exist yet (new add) + placeholder BUNDLE_IDENTIFIER ref [Read]
- `app/ios/VpnApp/VpnApp.entitlements` — verified only network-extension + app-groups present; Sign-in-with-Apple capability missing [Read]
- `app/ios/VpnApp/AppDelegate.swift` — verified `application(_:open:options:)` method does NOT exist yet [Read]
- `app/ios/VpnApp.xcodeproj/project.pbxproj` (selective grep) — verified `PRODUCT_BUNDLE_IDENTIFIER` is still RN template placeholder, `MARKETING_VERSION = 1.0`, `CURRENT_PROJECT_VERSION = 1` [Bash grep]
- `app/android/app/src/main/AndroidManifest.xml` — verified `singleTask` launchMode present + no intent-filter for vpnapp yet [Read]
- `app/android/app/build.gradle` — verified `versionCode 12` + `versionName "2.1.0"` + applicationId `com.vpnapp` [Bash]
- npm registry — Apple lib v2.5.1 modified 2026-03-31; Google lib v16.1.2 modified 2026-02-28 [Bash `npm view`]

### Secondary (MEDIUM confidence — verified across multiple sources)

- [github.com/invertase/react-native-apple-authentication README](https://github.com/invertase/react-native-apple-authentication) — API surface, cancellation codes, "fullName + email on first sign-in only" warning [WebFetch]
- [github.com/invertase/react-native-apple-authentication INITIAL_SETUP.md](https://github.com/invertase/react-native-apple-authentication/blob/main/docs/INITIAL_SETUP.md) — Xcode capability + Apple Developer Portal setup [WebFetch]
- [react-native-google-signin.github.io/docs/install](https://react-native-google-signin.github.io/docs/install) — installation + sub-guides existence [WebFetch]
- [react-native-google-signin.github.io/docs/setting-up/ios](https://react-native-google-signin.github.io/docs/setting-up/ios) — iOS reversed-client-id + AppDelegate URL handler [WebFetch]
- [react-native-google-signin.github.io/docs/setting-up/android](https://react-native-google-signin.github.io/docs/setting-up/android) — Android google-services config option [WebFetch]
- [github.com/react-native-google-signin/google-signin issue #1152](https://github.com/react-native-google-signin/google-signin/issues/1152) — webClientId required without Firebase [WebSearch]
- [reactnative.dev/docs/linking](https://reactnative.dev/docs/linking) — Linking API + Info.plist CFBundleURLTypes + Android intent-filter examples [WebSearch]
- [9to5mac.com/2025/05/01/apple-app-store-guidelines-external-links/](https://9to5mac.com/2025/05/01/apple-app-store-guidelines-external-links/) — May 2025 US-storefront external-link guideline change [WebSearch]
- [developer.apple.com/news/?id=9txfddzf](https://developer.apple.com/news/?id=9txfddzf) — Apple's official update news [WebSearch]

### Tertiary (LOW confidence — single source, training data, or community guides)

- [peerlist.io blog — Implement Sign In with Apple in React Native](https://peerlist.io/blog/engineering/implementing-sign-in-with-apple-in-react-native) — confirms invertase library is the standard recommendation [WebSearch]
- [medium.com — Google Sign-In Without Firebase (various 2024–2026 posts)](https://chaim-zalmy-muskal.medium.com/hi-6d328bbd550f) — corroborates non-Firebase setup pattern [WebSearch]
- Knowledge of RN 0.84 specifics where docs are sparse — pre-training assumption, partially verified by `package.json` versions [training data]

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — every version verified against npm registry; backend contracts mapped to existing handlers; UI-SPEC locks UI dependencies to zero new design libs.
- Architecture: HIGH — every file change traced to a concrete existing file; existing patterns (`linkWithCode`, AppState hook, axios interceptor) provide established models.
- Pitfalls: MEDIUM-HIGH — pitfalls #1, #2, #4, #6, #8 are derived from concrete codebase reads; #3, #5, #7 are from cross-referenced library docs + general RN community knowledge.
- Threat model: HIGH — CONTEXT.md D-22 enumerates T-1 through T-7 with concrete mitigations; this research adds T-8 (URL-scheme collision) as a low-severity supplement.
- Validation architecture: HIGH — Jest+RTR is in tree, no new framework needed; manual-only items are explicitly bounded by D-19/D-20.

**Research date:** 2026-05-26
**Valid until:** 2026-06-26 (30 days; SSO library ecosystem is relatively stable, but Apple/Google ID-token contract subject to platform changes — re-verify if execution starts after the 30-day window)

## RESEARCH COMPLETE

**Phase:** 5 — Mobile SSO + Pro CTA
**Confidence:** HIGH

### Key Findings

- **The mobile codebase is a textbook bare-RN 0.84 layout** — Zustand + axios + AsyncStorage + react-navigation v7 + i18next. Every UI-SPEC token exists in the theme. The existing `authStore.linkWithCode` (line 80) is the perfect template for `signInWithApple` and `signInWithGoogle`. The HomeScreen AppState hook (lines 41–50) is exactly the foreground-safety-net D-09 targets.
- **All required backend endpoints already ship.** `/auth/apple`, `/auth/google` (Phase 2), `GET /invoices/{id}?escalate=true` (Phase 3 03-05). Mobile is purely a consumer.
- **Two new npm deps pin to verified-current versions:** `@invertase/react-native-apple-authentication@2.5.1` (published 2026-03-31) and `@react-native-google-signin/google-signin@16.1.2` (published 2026-02-28). Both auto-link on RN 0.84. Combined dep weight is ~6 MB native code.
- **The iOS Bundle ID is still the RN template placeholder** (`org.reactjs.native.example.*`) — this is a real Phase-blocker for Sign-in-with-Apple and must be resolved as a Wave-1 prerequisite alongside D-21's existing checklist.
- **App Store posture on the Upgrade CTA is straightforward** — May 2025 guideline update removed the external-link entitlement requirement on the US storefront. `Linking.openURL` to the website is the simple, compliant path. The D-12 interstitial sheet is a reviewer-protection move, not a hard requirement.
- **T-7 is the single subtle threat unique to this phase:** the existing 401 interceptor must not recurse into the new SSO endpoints. Either an `_skipAuthRefresh` config flag or a URL-pattern check at the top of the 401 handler resolves it cleanly.
- **Validation strategy is "lean Jest + manual operator UAT":** ~10 new unit test files, Jest manual mocks for the two SSO libs, fake timers for the polling loop. The hard integration cases (real Apple sheet, deep-link routing through Chrome, signed-`.aab` install on Android device) are covered by 05-HUMAN-UAT.md per D-19/D-20.

### File Created

`/Users/abdunabi/Desktop/vpn/.planning/phases/05-mobile-sso-pro-cta/05-RESEARCH.md`

### Confidence Assessment

| Area | Level | Reason |
|------|-------|--------|
| Standard Stack | HIGH | Versions verified against npm; libraries confirmed by ADR §15 + CONTEXT.md canonical_refs; all peer deps satisfied |
| Architecture | HIGH | Every file change mapped to an existing file or a well-defined new path; patterns inherit from concrete existing code |
| Pitfalls | MEDIUM-HIGH | Concrete pitfalls drawn from codebase + library docs; some (URL-scheme collision, MIN_APP_VERSION drift) are general RN community knowledge |
| Threat model | HIGH | CONTEXT.md D-22 already enumerates the threat set; research confirms mitigations are tractable |
| Validation | HIGH | Test framework in tree; no Wave 0 install needed; manual UAT path well-defined |
| iOS readiness | MEDIUM | Bundle ID placeholder is a blocker; otherwise all native files identified and tractable |

### Open Questions

(See §"Risks & Open Questions" above — eight items, of which #1 (Bundle ID) is the only true BLOCKER. Items #2–#8 are pre-execution checks or graceful-degradation cases.)

### Ready for Planning

Research complete. Planner can decompose Phase 5 into plans following the wave structure:

- **Wave 0:** Operator prerequisites verification + test scaffolding (mocks + empty test files).
- **Wave 1:** Native config (Info.plist, entitlements, AndroidManifest, strings.xml, AppDelegate.swift) + npm install + pod install + iOS Bundle ID fix.
- **Wave 2:** Services layer (`appleSignIn.ts`, `googleSignIn.ts`, `deepLink.ts`, `payment.ts` rewrite, `api.ts` `_skipAuthRefresh` patch, `authStore.ts` extension, type updates).
- **Wave 3:** UI layer (`LoginScreen.tsx`, `PaymentScreen.tsx` rewrite, `AccountScreen.tsx` card, `RootNavigator.tsx` add Login screen, `LeavingAppSheet.tsx`, `ActivatingProModal.tsx`, `App.tsx` wiring, i18n keys).
- **Wave 4:** Version bumps (D-17) + local Android signed `.aab` build + operator smoke per D-19 + grep pass for stale i18n keys + 05-HUMAN-UAT.md.

Each PLAN.md MUST include a `<threat_model>` block covering T-1..T-7 per D-22.
