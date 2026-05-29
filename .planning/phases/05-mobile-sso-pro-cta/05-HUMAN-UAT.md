---
phase: 5
slug: mobile-sso-pro-cta
status: pending
created: 2026-05-26
---

# Phase 5 — Operator Prerequisites + Manual UAT

> Two-part checklist:
> 1. **BLOCKING prerequisites** (Wave-1 gate): operator MUST confirm these BEFORE Wave 1 starts. If any item is missing, the plan stops at the prereq gate.
> 2. **Manual UAT** (post-Wave-4): operator-run smoke tests on real Android device per D-19, plus iOS test-deferred capture per D-20.

---

## Part 1 — BLOCKING Prerequisites (Wave-1 gate)

Operator MUST confirm each item below by checking the box AND filling the value. If any "Required this phase" item is missing, ABORT — Wave 1 cannot proceed.

> ## ⚠️ PLACEHOLDER VALUES — MUST BE REPLACED BEFORE STORE UPLOAD
>
> **Operator decision (2026-05-29):** Phase 5 proceeds with **placeholder OAuth credentials**. Real Apple/Google Client IDs + Apple Service ID will be filled in at **store-upload time** (end-of-milestone release phase).
>
> The four `PLACEHOLDER_*` sentinels below are wired into native config so the build compiles and the integration is correct — but **SSO sign-in will NOT authenticate until they are replaced with real values**. Before any store upload, run:
> ```
> grep -rn "PLACEHOLDER_" app/ios app/android app/src
> ```
> Every hit MUST be replaced. The Apple Bundle ID (`com.vpnapp`) and Android debug SHA-1 are **real** and need no change.
>
> **Tracked obligation:** see `DEF-05-CREDS` in `deferred-items.md`.

### Required this phase

- [x] **Apple Bundle ID**: registered in Apple Developer Portal with "Sign in with Apple" capability enabled.
    - Value: `com.vpnapp` (REAL — matches Android `applicationId`. Operator must register + enable "Sign in with Apple" in Apple Developer Portal before iOS sign-in works at store time.)
    - Verified via: Apple Developer Portal → Identifiers → Bundle IDs → search for the value above, confirm "Sign In with Apple" appears in Capabilities list.
    - **Open Q #1 from RESEARCH.md:** Current iOS Bundle ID in `app/ios/VpnApp.xcodeproj/project.pbxproj` is the RN template placeholder (`org.reactjs.native.example.*`). Wave 1 Task 1 MUST replace both occurrences (line 274 + line 303) with `com.vpnapp`.

- [x] **Apple Service ID**: registered (already needed for Phase 2 — re-confirm operator has it).
    - Value: `PLACEHOLDER_APPLE_SERVICE_ID` (deferred — reuse the real `APPLE_SERVICE_ID` from landing `.env` at store time. Not wired into iOS native config; native Sign-in-with-Apple uses the Bundle ID entitlement.)

- [x] **Google OAuth Web Client ID** (the audience that backend validates — already issued for Phase 2 landing).
    - Value: `PLACEHOLDER_WEB.apps.googleusercontent.com` (deferred — reuse the real `GOOGLE_CLIENT_ID_WEB` from landing `.env`. MUST be identical across web + mobile; it is the backend JWT audience. Wired into `android/.../strings.xml` `server_client_id` + `GoogleSignin.configure({webClientId})`.)

- [x] **Google OAuth iOS Client ID** (new this phase — tied to iOS Bundle ID above).
    - Value: `PLACEHOLDER_IOS.apps.googleusercontent.com` (deferred — create in Google Cloud Console at store time, iOS client tied to `com.vpnapp`.)
    - Reversed-client-id wired into Info.plist as: `com.googleusercontent.apps.PLACEHOLDER_IOS`

- [x] **Google OAuth Android Client ID** (new this phase — tied to `com.vpnapp` + debug keystore SHA-1).
    - Value: `PLACEHOLDER_ANDROID.apps.googleusercontent.com` (deferred — create in Google Cloud Console at store time, Android client tied to `com.vpnapp` + the SHA-1 below. Not directly referenced in config; Android uses `webClientId` + SHA-1 registration.)

- [x] **Android debug keystore SHA-1** registered with the Android OAuth client.
    - Extract via: `keytool -list -v -keystore app/android/app/debug.keystore -alias androiddebugkey -storepass android -keypass android | grep SHA1`
    - Value: `5E:8F:16:06:2E:A3:CD:2C:4A:0D:54:78:76:BA:A6:F3:8C:AB:F6:25` (REAL — extracted from the committed debug.keystore. Operator must register this with the Android OAuth client at store time.)

- [x] **Server `MIN_APP_VERSION` env value** (so backend doesn't reject 2.2.0 as below-min). Confirm operator has scheduled the bump to `2.2.0` on the production API AT THE SAME TIME as the mobile release (per RESEARCH.md Open Q #3).
    - Current value: `PLACEHOLDER — confirm at release time` (deferred to store-upload coordination)
    - Will bump to `2.2.0` when: `simultaneous with mobile store release (end-of-milestone release phase)`

### NOT required this phase (deferred to end-of-milestone release phase per D-21)

- [ ] Apple App Store Connect API key (`.p8` + key ID + issuer)
- [ ] Play Console service account JSON
- [ ] Production Android release keystore (a phase-internal keystore is acceptable for Wave 4 smoke — generate and document its location)
- [ ] Apple external-link entitlement form approval

---

## Part 2 — Manual UAT (post-Wave-4)

> Operator runs these on a physical Android device per D-19; iOS items are deferred per D-20.

### Android smoke (REQUIRED for phase completion per D-19)

- [ ] **Build signed `.aab`**: `cd app/android && ./gradlew bundleRelease` → produces `app/android/app/build/outputs/bundle/release/app-release.aab`.
- [ ] **Install on operator's Android device** (via `bundletool` → APK or `assembleRelease`).
- [ ] **Google sign-in works**: launch app → Account → Sign in to sync Pro → Google → complete Google sheet → land on Home with `auth_provider: 'google'`.
- [ ] **Guest → Google upgrade preserves users.id**: verify admin panel `SELECT * FROM users WHERE auth_provider='google'` shows ONE row with the ORIGINAL guest's `users.id` (not a new row).
- [ ] **PaymentScreen informational layout**: navigate to Payment → confirm (a) no price text anywhere on screen, (b) exactly one primary CTA reading "Upgrade to Pro at risevpn.com", (c) tertiary "Already paid? Refresh" text link below.
- [ ] **Interstitial → browser handoff**: tap CTA → confirm "You're leaving the app" sheet appears → tap Continue → confirm Chrome opens to `https://risevpn.com/en/pricing?return=app` (or `/ru/` if device language is Russian).
- [ ] **Deep-link receive**: in Chrome address bar type `vpnapp://payment/success?invoiceId=test123` → confirm app foregrounds and Activating-Pro modal appears. (Polling will fail with 404 since `test123` is not a real invoice — modal stays in polling state until 30s timeout switches to "takingLonger" state.)

### iOS smoke (DEFERRED per D-20)

- [ ] iOS test plan recorded; runtime verification deferred to end-of-milestone release phase.
- [ ] iOS code compiles cleanly: `cd app/ios && pod install && xcodebuild -workspace VpnApp.xcworkspace -scheme VpnApp -configuration Debug -sdk iphonesimulator build` (CI-style; runs on macOS only).

### Release-prep (post-smoke)

- [ ] `app/package.json` version === `2.2.0` (grep `'"version": "2.2.0"'`)
- [ ] `app/src/config/version.ts` `APP_VERSION` === `'2.2.0'`
- [ ] `app/android/app/build.gradle` `versionName "2.2.0"` + `versionCode 13`
- [ ] `app/ios/VpnApp.xcodeproj/project.pbxproj` `MARKETING_VERSION = 2.2.0;` + `CURRENT_PROJECT_VERSION = 2;`
- [ ] `cd app && npm test` exits 0 (all suites green including `version.test`)
- [ ] `cd app && npx tsc --noEmit` exits 0

---

## Sign-off

- [ ] Operator confirms Part 1 prereqs filled — date: `____________________`
- [ ] Operator confirms Part 2 UAT passed — date: `____________________`
- [ ] Phase 5 complete — date: `____________________`
