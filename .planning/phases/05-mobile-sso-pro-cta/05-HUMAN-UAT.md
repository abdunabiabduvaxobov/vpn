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

### Required this phase

- [ ] **Apple Bundle ID**: registered in Apple Developer Portal with "Sign in with Apple" capability enabled.
    - Value: `____________________` (likely `com.vpnapp` to match Android `applicationId`)
    - Verified via: Apple Developer Portal → Identifiers → Bundle IDs → search for the value above, confirm "Sign In with Apple" appears in Capabilities list.
    - **Open Q #1 from RESEARCH.md:** Current iOS Bundle ID in `app/ios/VpnApp.xcodeproj/project.pbxproj` is the RN template placeholder (`org.reactjs.native.example.*`). Wave 1 Task 1 MUST replace both occurrences (line 274 + line 303) with the confirmed value above.

- [ ] **Apple Service ID**: registered (already needed for Phase 2 — re-confirm operator has it).
    - Value: `____________________`

- [ ] **Google OAuth Web Client ID** (the audience that backend validates — already issued for Phase 2 landing).
    - Value: `____________________.apps.googleusercontent.com`

- [ ] **Google OAuth iOS Client ID** (new this phase — tied to iOS Bundle ID above).
    - Value: `____________________.apps.googleusercontent.com`
    - Reversed-client-id format for Info.plist: `com.googleusercontent.apps.<IOS_CLIENT_ID>`

- [ ] **Google OAuth Android Client ID** (new this phase — tied to `com.vpnapp` + debug keystore SHA-1).
    - Value: `____________________.apps.googleusercontent.com`

- [ ] **Android debug keystore SHA-1** registered with the Android OAuth client.
    - Extract via: `keytool -list -v -keystore app/android/app/debug.keystore -alias androiddebugkey -storepass android -keypass android | grep SHA1`
    - Value: `__:__:__:__:__:__:__:__:__:__:__:__:__:__:__:__:__:__:__:__`

- [ ] **Server `MIN_APP_VERSION` env value** (so backend doesn't reject 2.2.0 as below-min). Confirm operator has scheduled the bump to `2.2.0` on the production API AT THE SAME TIME as the mobile release (per RESEARCH.md Open Q #3).
    - Current value: `____________________`
    - Will bump to `2.2.0` when: `____________________`

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
