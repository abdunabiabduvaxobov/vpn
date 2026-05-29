# Phase 5 — Deferred Items

Out-of-scope discoveries logged during execution. NOT fixed in the discovering plan.

## From 05-00 (Wave 0 test scaffolding)

### DEF-05-00-01 — Pre-existing `app/__tests__/App.test.tsx` is not runnable under Jest

- **Discovered during:** Plan 05-00, Task 2 (running the Wave-0 suite gate).
- **Status:** Pre-existing (fails identically on committed HEAD with the new Wave-0 stubs stashed away — verified via `git stash`). NOT caused by this plan.
- **Symptom cascade (each fix surfaces the next native-module gap):**
  1. `SyntaxError: Unexpected token 'export'` from `@react-navigation/native` v7 (untranspiled ESM in node_modules; RN preset's default `transformIgnorePatterns` does not allow-list it).
  2. After a `transformIgnorePatterns` override: `@react-native-community/netinfo: NativeModule.RNCNetInfo is null` (needs the library's shipped Jest mock).
  3. After mocking netinfo: `[@RNC/AsyncStorage]: NativeModule: AsyncStorage is null` — and likely further native modules (`vpnBridge`, etc.) after that.
- **Why deferred (scope boundary + fix-attempt limit):**
  - `App.test.tsx` is a pre-existing full-app render smoke test, NOT in plan 05-00's `<files>` list (Wave 0 = mocks + stub tests + UAT doc only).
  - Making it green requires an unbounded chain of native-module mocks + a Jest setup file — that is Wave-3 component-testing / Phase-8 (HARD-15 RNTL) infrastructure work, not Wave-0 scaffolding.
  - Hit the 3-attempt auto-fix limit chasing the cascade; stopped per deviation rules.
- **Impact on 05-00 gate:** None for the Wave-0 stubs. All 9 stub suites pass as skipped (`npm test -- --testPathIgnorePatterns='version.test|App.test'` exits 0). `version.test.ts` is the single intentional RED (turns green in Wave 4). The only other red is this pre-existing `App.test.tsx`.
- **Recommended owner:** Wave 3 (when real component tests need `react-test-renderer` to render screens) or Phase 8 HARD-15. Likely fix: add `setupFiles`/`setupFilesAfterEnv` with native-module mocks (netinfo shipped mock, `@react-native-async-storage/async-storage/jest/async-storage-mock`, a `vpnBridge` stub) + a `transformIgnorePatterns` allow-list for `@react-navigation/*` and other RN-ecosystem ESM packages.

## From orchestrator (Phase 5 execution kickoff)

### DEF-05-CREDS — Placeholder OAuth credentials MUST be replaced before store upload

- **Decided by:** Operator on 2026-05-29 — "just leave placeholders for those, we will fill them in while uploading to the store in the end."
- **Status:** Intentional deferral. Phase 5 executes with placeholder OAuth secrets so the native wiring is complete and the build compiles; real values get filled at the end-of-milestone release phase.
- **Real values already set (no action needed):**
  - Apple Bundle ID = `com.vpnapp` (matches Android `applicationId`).
  - Android debug keystore SHA-1 = `5E:8F:16:06:2E:A3:CD:2C:4A:0D:54:78:76:BA:A6:F3:8C:AB:F6:25`.
- **Placeholders that MUST be replaced before store upload (greppable sentinel `PLACEHOLDER_`):**
  - `PLACEHOLDER_WEB.apps.googleusercontent.com` → real Google Web Client ID (= landing `GOOGLE_CLIENT_ID_WEB`; backend JWT audience — must be identical web+mobile). Wired into `android/app/src/main/res/values/strings.xml` `server_client_id` + `GoogleSignin.configure({webClientId})`.
  - `PLACEHOLDER_IOS` → real Google iOS Client ID. Wired into `ios/VpnApp/Info.plist` reversed-client-id (`com.googleusercontent.apps.PLACEHOLDER_IOS`).
  - `PLACEHOLDER_ANDROID.apps.googleusercontent.com` → real Google Android Client ID (registered against `com.vpnapp` + SHA-1 above).
  - `PLACEHOLDER_APPLE_SERVICE_ID` → real Apple Service ID (= landing `APPLE_SERVICE_ID`).
- **Pre-upload verification command:** `grep -rn "PLACEHOLDER_" app/ios app/android app/src` — every hit must be replaced.
- **Also at release time:** confirm server `MIN_APP_VERSION` bumps to `2.2.0` simultaneous with the mobile store release (RESEARCH.md Open Q #3).
- **Recommended owner:** end-of-milestone release phase (store upload).

## From 05-01 (Wave 1 native config)

### DEF-05-01-01 — `pod install` fails: Podfile references missing `VpnAppNetworkExtension` target

- **Discovered during:** Plan 05-01, Task 2 (`cd app/ios && pod install`).
- **Symptom:** `[!] Unable to find a target named 'VpnAppNetworkExtension' in project 'VpnApp.xcodeproj', did find 'VpnApp'.` (non-zero exit; no `Podfile.lock` produced).
- **Status:** PRE-EXISTING project-config mismatch, NOT caused by this plan. Verified:
  - `git show HEAD:app/ios/Podfile` already declares `target 'VpnAppNetworkExtension' do` (line 38) — predates this plan.
  - `app/ios/VpnApp.xcodeproj/project.pbxproj` contains **zero** references to `VpnAppNetworkExtension` (the target was never added to the Xcode project).
  - No `app/ios/Podfile.lock` is tracked on HEAD — `pod install` has never succeeded in this checkout, so the plan's expectation of a committed `Podfile.lock` was never met by the prior native setup.
- **Impact on 05-01:** The npm packages ARE installed + pinned (`@invertase/react-native-apple-authentication@2.5.1`, `@react-native-google-signin/google-signin@16.1.2`) and React Native auto-link config + Codegen recognized them (`pod install` Codegen phase generated `RNGoogleSignInCGen` before failing on the missing target). All native config files (Info.plist, entitlements, AppDelegate, pbxproj Bundle ID, AndroidManifest, strings.xml) are completed — the durable deliverable. Only the iOS `Podfile.lock` regeneration is blocked.
- **Two paths to resolve (release phase, on a machine that builds iOS):**
  1. Add the `VpnAppNetworkExtension` target to `VpnApp.xcodeproj` (the NetworkExtension that runs the Go tunnel — referenced in CLAUDE.md + entitlements `packet-tunnel-provider`), OR
  2. Remove/guard the `VpnAppNetworkExtension` block in `app/ios/Podfile` if that target is built outside CocoaPods.
  Then re-run `cd app/ios && pod install` and commit the resulting `Podfile.lock` (verify it names `RNAppleAuthentication` + `GoogleSignIn`).
- **Recommended owner:** end-of-milestone release phase (iOS build), or whoever owns the iOS NetworkExtension target wiring.

### DEF-05-01-02 — Pre-existing high-severity prod npm vulnerabilities (axios, picomatch)

- **Discovered during:** Plan 05-01, Task 2 (`npm audit --omit=dev`).
- **Symptom:** `npm audit --omit=dev` reports 2 high prod vulnerabilities: `axios` (multiple advisories incl. SSRF / prototype-pollution) and `picomatch` (ReDoS / method-injection, transitive via `tinyglobby` + build tooling).
- **Status:** PRE-EXISTING, NOT introduced by the two new SSO packages. Verified:
  - `axios@^1.13.6` is a direct dependency present on committed HEAD `app/package.json` before this plan.
  - `picomatch` appears 10× in the HEAD `package-lock.json` (pre-existing transitive dep).
  - Scoped analysis confirms **zero** vulnerabilities (high, critical, or otherwise) attributable to `@invertase/react-native-apple-authentication` or `@react-native-google-signin/google-signin` — the T-6 supply-chain gate PASSES for the new packages.
- **Why deferred (SCOPE BOUNDARY):** Only issues directly caused by this task's changes are auto-fixed. axios/picomatch are unrelated pre-existing dependency advisories; bumping axios is an API-client change (touches `app/src/services/api.ts` behavior) out of this plan's native-config scope.
- **Recommended owner:** a dedicated dependency-hardening pass (likely Phase 8 hardening). Suggested fix: bump `axios` to the latest patched 1.x and re-resolve `picomatch` via `npm audit fix` / lockfile dedupe, then re-run the mobile test + tsc suite.
