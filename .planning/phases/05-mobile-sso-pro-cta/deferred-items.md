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
