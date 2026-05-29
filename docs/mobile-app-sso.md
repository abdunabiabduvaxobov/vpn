# RiseVPN Mobile App — SSO + Pro CTA (Phase 5)

**Version:** 2.2.0  
**Stack:** React Native 0.84, TypeScript, Zustand, `@invertase/react-native-apple-authentication` v2.5.1, `@react-native-google-signin/google-signin` v16  
**Backend contract:** [`docs/auth-sso-api.md`](./auth-sso-api.md) — not duplicated here.

---

## Overview

Phase 5 adds three things to the mobile app:

1. **Apple / Google / Guest sign-in** on `LoginScreen`.
2. **Informational-only upgrade flow** on `PaymentScreen` — no IAP, no prices; a single CTA opens `risevpn.com/<locale>/pricing` in the system browser (app-store compliant).
3. **Deep-link return path** — when the user completes payment on the web, `vpnapp://payment/success?invoiceId=<uuid>` returns them to the app and triggers a polling modal that verifies Pro status with the backend.

Token verification is entirely server-side (Phase 2). The mobile app obtains a raw `identityToken` / `idToken` from the OS and forwards it to the backend unchanged.

---

## Sign-in flow

### LoginScreen CTAs

`app/src/screens/LoginScreen.tsx`

Three buttons are rendered:

| CTA | Platform | Handler |
|-----|----------|---------|
| Continue with Apple | iOS only (`Platform.OS === 'ios'`) | `useAuthStore.signInWithApple` |
| Continue with Google | Both | `useAuthStore.signInWithGoogle` |
| Continue as Guest | Both | `useAuthStore.initialize` |

Only one button is busy at a time (`isBusy` state). User cancellation returns silently to `LoginScreen` (no alert). Any other error — network failure, backend 4xx/5xx, missing token — shows a generic non-fatal `Alert` and stays on `LoginScreen`.

On success all three paths call `navigation.reset({index: 0, routes: [{name: 'Home'}]})` — the login stack is cleared from history.

### Apple sign-in

`app/src/services/appleSignIn.ts`

Calls `appleAuth.performRequest` with scopes `FULL_NAME` and `EMAIL`. Apple returns `fullName` and `email` only on the first sign-in ever for that Apple ID — subsequent sign-ins return `null` for both. The service throws on cancellation (`error.code === appleAuth.Error.CANCELED`) so `LoginScreen.onApple` can distinguish cancel from real error.

### Google sign-in

`app/src/services/googleSignIn.ts`

`configureGoogleSignIn()` must be called at app boot (wired in `App.tsx`) before any sign-in attempt. It sets `webClientId` to the `PLACEHOLDER_WEB` constant — this is the JWT audience the backend validates against (`GOOGLE_CLIENT_ID_WEB`).

`signInWithGoogle()` defensively reads both the v16 `userInfo.data.idToken` shape and the pre-v13 `userInfo.idToken` shape.

### Guest-to-SSO promotion

`app/src/stores/authStore.ts` — `signInWithApple` / `signInWithGoogle`

The axios interceptor attaches the existing guest `Authorization: Bearer` header automatically before the `/auth/apple` or `/auth/google` call. The backend (Phase 2, D-06) detects this header and promotes the guest row in place — `users.id` is preserved and existing device bindings stay intact.

The SSO calls are flagged `{_skipAuthRefresh: true}` (line 147 / line 175 in `authStore.ts`) so a 401 response cannot trigger the token-refresh interceptor and create a recursive guest re-mint.

After a successful SSO call the store calls `fetchAccount()` to immediately populate `user` state from `/account`.

See [`docs/auth-sso-api.md`](./auth-sso-api.md#identity-and-account-linking-rules) for full account-linking rules (auto-link by verified email, private-relay exception, guest-with-existing-owner conflict).

---

## Pro CTA and deep-link return

### PaymentScreen

`app/src/screens/PaymentScreen.tsx`

Informational only. No prices, no IAP. The single upgrade CTA (`t('payment.upgrade.cta')`) calls `setSheetVisible(true)`, which opens `LeavingAppSheet` before any `Linking.openURL` call. Bypassing the interstitial is not possible through the normal tap path.

A tertiary "Already paid? Refresh" link calls `fetchAccount()` directly and shows a toast.

### LeavingAppSheet interstitial

`app/src/components/LeavingAppSheet.tsx`

A bottom-sheet modal (D-12) that presents two buttons: Continue (calls `Linking.openURL(url)` then `onDismiss`) and Cancel. The URL comes from `upgradeUrlForLocale`.

### Upgrade URL construction

`app/src/services/payment.ts` — `upgradeUrlForLocale(i18nLocale: string)`

```
ru / ru-RU / ru-UA → https://risevpn.com/ru/pricing?return=app
anything else      → https://risevpn.com/en/pricing?return=app
```

The `?return=app` query parameter is part of the URL that lands on the landing site — it is not used by the deep-link handler on return.

### Deep-link contract

Scheme registered on both platforms: `vpnapp://`

Return URL shape: `vpnapp://payment/success?invoiceId=<uuid>`

`app/src/services/deepLink.ts` — `parseInvoiceFromUrl(url)`

The parser:
1. Splits on the first `?` and **exact-matches** the base path `vpnapp://payment/success` — paths like `success-evil` or `successfully` return `null`.
2. Finds `invoiceId` anywhere in the query string — `?invoiceId=X` and `?status=ok&invoiceId=X` both work.
3. URL-decodes the value and validates it against `/^[0-9a-f-]{36}$/i` (UUID shape gate — not a security check, just garbage filtering).

`registerDeepLinkHandler()` handles both cold start (`Linking.getInitialURL()`) and warm foreground (`Linking.addEventListener('url')`). Both paths call `useAuthStore.getState().startActivatingPro(invoiceId)`.

Duplicate delivery protection: `startActivatingPro` (`authStore.ts` line 191–194) is a no-op if `isActivatingPro === true` and `pendingInvoiceId` equals the incoming ID — the OS can deliver the same deep link twice (cold-start `getInitialURL` + warm `url` event on the same launch).

### ActivatingProModal polling

`app/src/components/ActivatingProModal.tsx`

Mounts at the root level whenever `authStore.isActivatingPro` is `true`. Polling cadence (D-21):

| Polls | Interval | Escalation |
|-------|----------|-----------|
| 1–5 | 2 s | `GET /invoices/{id}` |
| 6–15 | 2 s | `GET /invoices/{id}?escalate=true` |
| >15 | — | "taking longer" state |

Total budget: 30 s (15 polls × 2 s). The `?escalate=true` flag tells the backend to force a lava.top reconciliation check.

Modal states:

| State | User-dismissable | Trigger |
|-------|-----------------|---------|
| `polling` | No | default on open |
| `success` | Auto-dismiss (3 s) | backend returns `status === 'paid'` |
| `failed` | Yes (Back to Account) | backend returns `status === 'failed'` |
| `takingLonger` | Yes (Close) | 15 polls exhausted without `paid` |

On `success`: `fetchAccount()` is called to refresh the `user.subscription_tier` in store before the modal auto-dismisses.

On `takingLonger`: a manual Refresh button makes one more `GET /invoices/{id}?escalate=true` call. A "Contact Support" link opens `https://t.me/flawlssr`.

The `invoiceId` from the deep link is **untrusted** (T-1). The modal only transitions to `success` after the backend confirms `status === 'paid'` — the URL value itself never grants Pro.

---

## Native configuration

### iOS

**Bundle ID:** `com.vpnapp`

**Entitlements** (`app/ios/VpnApp/VpnApp.entitlements`):

```xml
<key>com.apple.developer.applesignin</key>
<array><string>Default</string></array>
```

This entitlement is required; without it the Apple sign-in sheet will not appear.

**Info.plist** (`app/ios/VpnApp/Info.plist`) — `CFBundleURLTypes`:

| CFBundleURLName | Scheme | Purpose |
|----------------|--------|---------|
| `com.vpnapp.payment` | `vpnapp` | Payment-return deep link |
| `com.googleusercontent.apps.PLACEHOLDER_IOS` | `com.googleusercontent.apps.PLACEHOLDER_IOS` | Google Sign-In reversed client ID |

The `PLACEHOLDER_IOS` entry must be replaced with the real reversed iOS client ID before store upload (see Build caveats below).

### Android

**Package name:** `com.vpnapp`

**`AndroidManifest.xml`** — deep-link intent filter on `MainActivity`:

```xml
<intent-filter android:autoVerify="false">
    <action android:name="android.intent.action.VIEW" />
    <category android:name="android.intent.category.DEFAULT" />
    <category android:name="android.intent.category.BROWSABLE" />
    <data android:scheme="vpnapp" android:host="payment" />
</intent-filter>
```

`BROWSABLE` is required for Chrome to navigate deep links from a tap. The filter is scoped to `host="payment"` — only `vpnapp://payment/*` reaches the app.

**`strings.xml`** (`app/android/app/src/main/res/values/strings.xml`):

```xml
<string name="server_client_id">PLACEHOLDER_WEB.apps.googleusercontent.com</string>
```

This is the Google Web Client ID consumed by `GoogleSignin.configure({webClientId})` in JS. It must match `GOOGLE_CLIENT_ID_WEB` on the backend and the landing site.

---

## Build and release caveats

### WARNING: Placeholder OAuth credentials (DEF-05-CREDS)

SSO will NOT authenticate on a real device until these sentinels are replaced:

| Sentinel | Location | Replace with |
|----------|----------|-------------|
| `PLACEHOLDER_WEB` | `app/src/services/googleSignIn.ts`, `app/android/.../strings.xml` | Google Web Client ID (= backend `GOOGLE_CLIENT_ID_WEB`) |
| `PLACEHOLDER_IOS` | `app/src/services/googleSignIn.ts`, `app/ios/VpnApp/Info.plist` | Google iOS Client ID |
| `PLACEHOLDER_ANDROID` | `app/src/services/googleSignIn.ts` | Google Android Client ID (registered against SHA-1 below) |
| `PLACEHOLDER_APPLE_SERVICE_ID` | (native config) | Apple Service ID (= backend `APPLE_SERVICE_ID`) |

Pre-upload verification:

```sh
grep -rn "PLACEHOLDER_" app/ios app/android app/src
# Must return zero results before store upload.
```

Values that are already real and do not need replacement:
- Apple Bundle ID: `com.vpnapp`
- Android debug keystore SHA-1: `5E:8F:16:06:2E:A3:CD:2C:4A:0D:54:78:76:BA:A6:F3:8C:AB:F6:25`

The `PLACEHOLDER_*` sentinels are public OAuth client IDs (not secrets) — they are safe to commit and reviewable. The deferral is operator-authorized (decision 2026-05-29).

### WARNING: Android release build requires JDK 17

RN 0.84's `com.facebook.react.settings` Gradle plugin rejects JDK 25 (the current host default). You must set `JAVA_HOME` to a Temurin 17 JDK before running `bundleRelease`:

```sh
export JAVA_HOME=/Library/Java/JavaVirtualMachines/temurin-17.jdk/Contents/Home
cd app/android && ./gradlew bundleRelease
```

JDK 25 error: `Error resolving plugin [id: 'com.facebook.react.settings'] > 25.0.2`.

### WARNING: iOS pod install blocked (DEF-05-01-01)

`cd app/ios && pod install` fails with:

```
[!] Unable to find a target named 'VpnAppNetworkExtension' in project 'VpnApp.xcodeproj'
```

The `Podfile` declares a `VpnAppNetworkExtension` target (line 38) that does not exist in `project.pbxproj`. This is a pre-existing configuration mismatch. No `Podfile.lock` is committed. Resolve by either adding the missing Xcode target or guarding the Podfile block, then re-running `pod install` on a machine that builds iOS and committing the resulting `Podfile.lock`.

### Token storage (deferred to Phase 8 HARD-15)

Auth tokens are currently stored in `AsyncStorage` (plaintext, sandboxed per OS). Migration to Keychain (iOS) and `EncryptedSharedPreferences` (Android) is tracked as Phase 8 HARD-15. Do not store sensitive state in `AsyncStorage` in new code.

---

## Version sources

All four sources must stay in lockstep. A `version.test.ts` guard (`app/src/config/__tests__/version.test.ts`) enforces this — it will fail if `APP_VERSION` in `version.ts` does not equal `2.2.0`.

| Source | Current value |
|--------|--------------|
| `app/package.json` | `"version": "2.2.0"` |
| `app/src/config/version.ts` | `APP_VERSION = '2.2.0'` |
| `app/android/app/build.gradle` | `versionName "2.2.0"`, `versionCode 13` |
| `app/ios/VpnApp.xcodeproj/project.pbxproj` | `MARKETING_VERSION = 2.2.0`, `CURRENT_PROJECT_VERSION = 2` |

When bumping the version, update all four files and verify with:

```sh
cd app && npx jest --testPathPattern=version.test
```

---

*Phase: 05-mobile-sso-pro-cta*  
*Completed: 2026-05-29*  
*Backend contract: [`docs/auth-sso-api.md`](./auth-sso-api.md)*
