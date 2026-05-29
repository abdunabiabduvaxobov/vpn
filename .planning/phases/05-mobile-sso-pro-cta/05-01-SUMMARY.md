---
phase: 05-mobile-sso-pro-cta
plan: 01
subsystem: mobile-native-sso-infra
tags: [ios, android, sso, deep-link, native-config, apple-signin, google-signin, wave-1]
requires:
  - "05-00 Wave-0 Jest mocks + stubs (shape-exact to the libs installed here)"
provides:
  - "@invertase/react-native-apple-authentication@2.5.1 + @react-native-google-signin/google-signin@16.1.2 installed at EXACT pinned versions"
  - "iOS Bundle ID = com.vpnapp (real, Sign-in-with-Apple capable) replacing RN template placeholder"
  - "iOS Sign-in-with-Apple entitlement + CFBundleURLTypes (vpnapp:// + Google reversed-client-id)"
  - "iOS AppDelegate application(_:open:options:) forwarding to GIDSignIn then RCTLinkingManager"
  - "Android vpnapp://payment/* BROWSABLE intent-filter on MainActivity"
  - "Android strings.xml server_client_id resource (Google Web Client ID)"
affects:
  - "Wave 2 services (appleSignIn.ts, googleSignIn.ts, deepLink.ts) — native registration now in place so the JS wrappers can call the real native modules"
  - "Wave 3 UI (LoginScreen, deep-link modal) — vpnapp:// now reaches RN Linking on both platforms"
  - "end-of-milestone release phase — must replace PLACEHOLDER_* OAuth sentinels + re-run pod install after fixing VpnAppNetworkExtension target"
tech-stack:
  added:
    - "@invertase/react-native-apple-authentication@2.5.1 (exact pin, T-6)"
    - "@react-native-google-signin/google-signin@16.1.2 (exact pin, T-6)"
  patterns:
    - "Custom-scheme deep link vpnapp://payment/* scoped by android:host=payment + iOS CFBundleURLSchemes"
    - "AppDelegate URL handler: GIDSignIn.handle first, RCTLinkingManager fallback (no override — UIApplicationDelegate protocol base)"
    - "PLACEHOLDER_* greppable sentinels for operator-deferred OAuth creds (filled at store upload)"
key-files:
  created:
    - .planning/phases/05-mobile-sso-pro-cta/05-01-SUMMARY.md
  modified:
    - app/package.json
    - app/package-lock.json
    - app/ios/VpnApp.xcodeproj/project.pbxproj
    - app/ios/VpnApp/Info.plist
    - app/ios/VpnApp/VpnApp.entitlements
    - app/ios/VpnApp/AppDelegate.swift
    - app/android/app/src/main/AndroidManifest.xml
    - app/android/app/src/main/res/values/strings.xml
    - .planning/phases/05-mobile-sso-pro-cta/deferred-items.md
decisions:
  - "pod install blocked by pre-existing Podfile/xcodeproj VpnAppNetworkExtension mismatch — recorded as DEF-05-01-01, not a code error; npm deps pinned + lockfile committed regardless"
  - "2 high prod npm vulns (axios, picomatch) are pre-existing + out of scope — DEF-05-01-02; the two NEW SSO packages contribute zero high/critical (T-6 gate PASS)"
  - "AppDelegate URL handler declared WITHOUT override (base is UIApplicationDelegate protocol, not RCTAppDelegate) per RESEARCH §AppDelegate example"
metrics:
  tasks: 6
  files: 9
  commits: 6
  duration: "~8m"
  completed: 2026-05-29
---

# Phase 5 Plan 01: Native-Side SSO Infrastructure Summary

Landed all Phase 5 Wave-1 native infrastructure: installed the two SSO libraries at EXACT pinned versions (2.5.1 + 16.1.2), replaced the iOS RN-template Bundle-ID placeholder with the real `com.vpnapp`, added the Sign-in-with-Apple entitlement + `CFBundleURLTypes` (vpnapp:// + Google reversed-client-id) + an `application(_:open:options:)` URL handler in AppDelegate, and registered the Android `vpnapp://payment/*` BROWSABLE intent-filter + `server_client_id` string resource — all using the operator-authorized placeholder OAuth credentials (real Bundle ID + SHA-1; deferred Client IDs as greppable `PLACEHOLDER_*` sentinels).

## Operator Prerequisites (Task 1 — checkpoint resolved before execution)

Task 1 was a `checkpoint:human-action` gate. **It was already resolved by the operator** (commit `c08385f`) before this executor ran — all 7 "Required this phase" items in `05-HUMAN-UAT.md` Part 1 are `[x]` with values. The operator authorized proceeding with **placeholder OAuth credentials** (real values filled at store-upload time per `DEF-05-CREDS`). Values consumed by Tasks 3-6 (full detail lives in `05-HUMAN-UAT.md` Part 1):

| Prereq | Value used | Real / Placeholder |
|--------|-----------|--------------------|
| Apple Bundle ID | `com.vpnapp` | REAL (Task 3) |
| Google iOS Client ID | `PLACEHOLDER_IOS` → reversed `com.googleusercontent.apps.PLACEHOLDER_IOS` | PLACEHOLDER (Task 4 Info.plist) |
| Google Web Client ID | `PLACEHOLDER_WEB.apps.googleusercontent.com` | PLACEHOLDER (Task 6 strings.xml `server_client_id`) |
| Google Android Client ID | `PLACEHOLDER_ANDROID.apps.googleusercontent.com` | PLACEHOLDER (not wired into config; SHA-1 + webClientId registration) |
| Apple Service ID | `PLACEHOLDER_APPLE_SERVICE_ID` | PLACEHOLDER (not wired into iOS native; uses Bundle-ID entitlement) |
| Android debug SHA-1 | `5E:8F:16:06:2E:A3:CD:2C:4A:0D:54:78:76:BA:A6:F3:8C:AB:F6:25` | REAL (informational; registered server-side) |

## What Was Built

### Task 2 — SSO packages installed + pinned (commit `79cf8d8`)
- `npm install --save-exact` added `@invertase/react-native-apple-authentication@2.5.1` and `@react-native-google-signin/google-signin@16.1.2` to `app/package.json` `dependencies` — **EXACT versions, no caret/tilde** (T-6 supply-chain). `package-lock.json` regenerated + committed.
- `npm audit --omit=dev`: prod totals = `{critical:0, high:2, moderate:11, low:0}`. Scoped analysis confirms **zero** vulnerabilities attributable to either new SSO package → **T-6 supply-chain gate PASS**. The 2 high vulns (`axios`, `picomatch`) are pre-existing dependencies (verified on committed HEAD) — out of scope, logged as `DEF-05-01-02`.
- `pod install` was attempted but **failed on a pre-existing project misconfiguration** (Podfile declares a `VpnAppNetworkExtension` target absent from the xcodeproj; no `Podfile.lock` was ever committed on HEAD). React Native auto-link + Codegen DID recognize the new Google module (`RNGoogleSignInCGen` generated before the failure), so autolinking is wired correctly — only `Podfile.lock` regeneration is blocked. Logged as `DEF-05-01-01`.

### Task 3 — iOS Bundle ID (commit `18a33a2`)
- Replaced both `PRODUCT_BUNDLE_IDENTIFIER` occurrences (Debug line 274 + Release line 303) in `app/ios/VpnApp.xcodeproj/project.pbxproj`: `org.reactjs.native.example.$(PRODUCT_NAME:rfc1034identifier)` → `com.vpnapp`. No template placeholder remains. Resolves RESEARCH Open Q #1 — Sign-in-with-Apple cannot work with the RN template Bundle ID. (No separate test/extension target carried the placeholder — only the 2 main-target configs.)

### Task 4 — iOS entitlement + URL types (commit `3e23e22`)
- `VpnApp.entitlements`: added `com.apple.developer.applesignin = [Default]` alongside the existing `networking.networkextension` + `application-groups` keys (both preserved).
- `Info.plist`: added `CFBundleURLTypes` with two entries — (1) the `vpnapp` custom scheme for payment-return deep links, (2) the Google reversed-client-id `com.googleusercontent.apps.PLACEHOLDER_IOS` in both `CFBundleURLName` and `CFBundleURLSchemes`.
- `plutil -lint` clean on both files.

### Task 5 — AppDelegate URL handler (commit `93ab893`)
- Added `import GoogleSignIn`.
- Added `application(_:open:options:)` after `didFinishLaunchingWithOptions`. **No `override`** — the base is the `UIApplicationDelegate` protocol (this project's `AppDelegate` is `UIResponder, UIApplicationDelegate`, NOT a `RCTAppDelegate` subclass), so `override` would be rejected. Handler calls `GIDSignIn.sharedInstance.handle(url)` first (Google callback), then falls back to `RCTLinkingManager.application(app, open: url, options: options)` for `vpnapp://`.

### Task 6 — Android deep link + Web Client ID (commit `2f6a44f`)
- `AndroidManifest.xml`: added a VIEW intent-filter on `MainActivity` (after the preserved MAIN/LAUNCHER filter) with `BROWSABLE` category (CONTEXT Pitfall 3 — Chrome refuses link-tap navigation without it) scoped to `android:scheme="vpnapp" android:host="payment"` (T-1 spoofing scope-down — only `vpnapp://payment/*` reaches the app). `android:launchMode="singleTask"` preserved.
- `strings.xml`: appended `<string name="server_client_id">PLACEHOLDER_WEB.apps.googleusercontent.com</string>`, preserving the existing `app_name`.
- `xmllint --noout` clean on both files.

## Verification Results

```
$ cd app && npx tsc --noEmit
0 errors (exit 0)

$ cd app && npm test -- --testPathIgnorePatterns='version.test|App.test'
Test Suites: 9 skipped, 0 of 9 total
Tests:       30 skipped, 30 total
exit 0   # Wave-0 stub suite still green — no regression

$ grep "org.reactjs.native.example" app/ios/VpnApp.xcodeproj/project.pbxproj  → exit 1 (placeholder gone)
$ plutil -lint Info.plist + VpnApp.entitlements                              → both OK
$ xmllint --noout AndroidManifest.xml + strings.xml                          → both OK
$ grep server_client_id / BROWSABLE / vpnapp scheme / applesignin / GIDSignIn → all PASS
```

Known-RED files behave exactly as designed: `version.test.ts` still RED (`2.1.0` vs expected `2.2.0` — turns green in Wave 4); `App.test.tsx` pre-existing failure (`DEF-05-00-01`). Both intentional, excluded from the gate.

## Wave 2 Readiness

Wave 2 (Apple/Google sign-in service wrappers + deepLink.ts) can now run: the native modules are installed at the pinned versions the Wave-0 mocks mirror, the iOS Bundle ID is real (Sign-in-with-Apple capable), both platforms route `vpnapp://payment/*` to the RN Linking module, and the Google Web Client ID resource is in place for `GoogleSignin.configure({webClientId})`. The only carry-forward obligations are the deferred items below — neither blocks Wave 2/3 JS development (placeholders compile; real values only needed for live SSO at store upload).

## Deviations from Plan

### Auto-fixed Issues
None — no production-logic bugs were found. All edits were the exact native config the plan specified, using the operator-resolved checkpoint values.

### Environment/Config Deviations (recorded, not halted per environment notes)

**1. [Rule 3 - Blocking, pre-existing] `pod install` blocked by missing `VpnAppNetworkExtension` Xcode target**
- **Found during:** Task 2.
- **Issue:** `pod install` exits non-zero: `Unable to find a target named 'VpnAppNetworkExtension' in project 'VpnApp.xcodeproj'`. The Podfile (HEAD, line 38) declares this target but the xcodeproj contains zero references to it; no `Podfile.lock` was ever committed on HEAD.
- **Resolution:** Per environment notes, did NOT halt. npm deps remain pinned + `package-lock.json` committed; all native config edits completed + committed. `Podfile.lock` regeneration deferred to the iOS build environment.
- **Files modified:** none beyond the planned npm install.
- **Logged in:** `deferred-items.md` `DEF-05-01-01`.

**2. [Scope boundary] Pre-existing high-severity prod npm vulnerabilities (axios, picomatch)**
- **Found during:** Task 2 (`npm audit --omit=dev`).
- **Issue:** 2 high prod vulns — `axios` (direct dep, on HEAD before this plan) and `picomatch` (transitive, 10× in HEAD lockfile). Neither is the new SSO packages.
- **Resolution:** Out of scope (only auto-fix issues caused by this task's changes). The T-6 gate scopes to the two new packages — both clean.
- **Logged in:** `deferred-items.md` `DEF-05-01-02`.

### Sentinel deviation (intentional, operator-authorized)
The plan's literal template placeholders (`<IOS_OAUTH_CLIENT_ID>`, `YOUR_WEB_OAUTH_CLIENT_ID`) were replaced with the operator-authorized deferred sentinels `PLACEHOLDER_IOS` / `PLACEHOLDER_WEB` (the resolved-checkpoint values). The plan's automated checks (`! grep <IOS_OAUTH_CLIENT_ID>`, `! grep YOUR_WEB_OAUTH_CLIENT_ID`) all PASS because those template strings are absent; the authorized sentinels stay greppable for store-time replacement.

## Authentication Gates
None encountered during execution. (Task 1 was a `checkpoint:human-action` prereq gate, already resolved by the operator before this run.)

## Known Stubs

These are operator-authorized deferred OAuth credentials, NOT defects. Native wiring is complete and compiles; **live SSO sign-in will not authenticate until these are replaced at store upload** (tracked `DEF-05-CREDS`). Pre-upload command: `grep -rn "PLACEHOLDER_" app/ios app/android app/src`.

| Sentinel | File | Line | Reason / resolved by |
|----------|------|------|----------------------|
| `PLACEHOLDER_IOS` | `app/ios/VpnApp/Info.plist` | 84, 87 (+comment 79) | Google iOS Client ID — fill at store upload |
| `PLACEHOLDER_WEB` | `app/android/app/src/main/res/values/strings.xml` | 9 (+comment 7) | Google Web Client ID — fill at store upload |

Plus pod-install-blocked `Podfile.lock` (`DEF-05-01-01`) — durable native config is committed; lockfile regenerated at iOS build time.

## Self-Check: PASSED
- All 8 modified files + the new SUMMARY verified present on disk.
- All 6 task commits (`79cf8d8`, `18a33a2`, `3e23e22`, `93ab893`, `2f6a44f` + the pre-existing operator-auth `c08385f`) verified in git history.
- STATE.md / ROADMAP.md NOT modified (orchestrator owns those writes, per objective).
