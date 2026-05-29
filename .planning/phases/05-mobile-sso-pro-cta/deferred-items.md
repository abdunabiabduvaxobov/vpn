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
