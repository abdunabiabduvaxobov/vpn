---
phase: 05-mobile-sso-pro-cta
plan: 00
subsystem: mobile-test-scaffolding
tags: [jest, mocks, sso, test-scaffolding, wave-0, uat]
requires: []
provides:
  - "Jest manual mocks for @invertase/react-native-apple-authentication + @react-native-google-signin/google-signin"
  - "10 Wave-0 stub test files (9 describe.skip + 1 intentional-RED version guard)"
  - "05-HUMAN-UAT.md operator-prerequisites BLOCKING gate + Wave-4 manual smoke checklist"
affects:
  - "Wave 2 services (appleSignIn, googleSignIn, deepLink, payment, api) — consume the mocks + fill the stubs"
  - "Wave 3 UI (LoginScreen, PaymentScreen, ActivatingProModal) — fill the stubs"
  - "Wave 4 release — turns version.test.ts green by bumping APP_VERSION to 2.2.0"
tech-stack:
  added: []
  patterns:
    - "RN-preset Jest manual mocks under app/__mocks__/ (Jest-only, never bundled)"
    - "describe.skip stub suites keep the suite green before code lands (tests-first scaffolding)"
    - "single intentionally-RED guard test (version.test.ts) as a Wave-4 must-turn-green gate"
key-files:
  created:
    - app/__mocks__/@invertase/react-native-apple-authentication.ts
    - app/__mocks__/@react-native-google-signin/google-signin.ts
    - app/src/services/__tests__/appleSignIn.test.ts
    - app/src/services/__tests__/googleSignIn.test.ts
    - app/src/services/__tests__/deepLink.test.ts
    - app/src/services/__tests__/payment.test.ts
    - app/src/services/__tests__/api.test.ts
    - app/src/stores/__tests__/authStore.test.ts
    - app/src/screens/__tests__/LoginScreen.test.tsx
    - app/src/screens/__tests__/PaymentScreen.test.tsx
    - app/src/components/__tests__/ActivatingProModal.test.tsx
    - app/src/config/__tests__/version.test.ts
    - .planning/phases/05-mobile-sso-pro-cta/05-HUMAN-UAT.md
    - .planning/phases/05-mobile-sso-pro-cta/deferred-items.md
  modified: []
decisions:
  - "Kept app/jest.config.js bare (preset: 'react-native', no overrides) — stubs are config-independent and pass without changes"
  - "Pre-existing App.test.tsx failure deferred (not in plan scope; unbounded native-module mock cascade)"
metrics:
  tasks: 3
  files: 14
  commits: 3
  duration: "~3m"
  completed: 2026-05-29
---

# Phase 5 Plan 00: Wave-0 Test Scaffolding Summary

Established Phase 5 Wave-0 test scaffolding: two Jest manual mocks mirroring the real Apple (v2.5.1) and Google (v16) SSO native-library API surfaces, ten stub test files (nine `describe.skip` suites that keep the suite green plus one intentionally-RED `version.test.ts` guard for Wave 4), and the `05-HUMAN-UAT.md` BLOCKING operator-prerequisites gate that Wave 1 cannot bypass.

## What Was Built

### Task 1 — Jest manual mocks (commit `d069d35`)
- `app/__mocks__/@invertase/react-native-apple-authentication.ts` — exports `appleAuth.performRequest` (resolves the documented fixture), `Operation`/`Scope`/`Error` enums (`CANCELED: '1001'`), plus an `AppleRequestResponse` type. Mirrors real library v2.5.1.
- `app/__mocks__/@react-native-google-signin/google-signin.ts` — exports `GoogleSignin.{configure,hasPlayServices,signIn,signOut,...}` with the v16 `data`-wrapped `signIn` response shape (`response.data.idToken`, NOT pre-v13 `response.idToken`) and `statusCodes.SIGN_IN_CANCELLED`.
- Both are Jest-only (`app/__mocks__/`), auto-discovered by the RN preset, never bundled into production (threat T-6 mitigation: committed + reviewable + shape-exact so Wave 2 swaps mock→real with zero API drift).

### Task 2 — 10 Wave-0 stub test files (commit `d9d2c34`)
- 9 `describe.skip` stub suites with the EXACT `it(...)` strings the plan specified (so Wave 2/3 can grep and fill): `appleSignIn`, `googleSignIn`, `deepLink`, `payment`, `api` (`_skipAuthRefresh`), `authStore`, `LoginScreen`, `PaymentScreen`, `ActivatingProModal`.
- `version.test.ts` — single NON-skipped guard `expect(APP_VERSION).toBe('2.2.0')`. Intentionally RED now (`version.ts` still exports `'2.1.0'`); turns green when Wave 4 bumps it.
- Each file headers its `05-VALIDATION.md` task ID (16 `5-(SVC|UI|VER|W0)-NN` references across the 10 files).

### Task 3 — 05-HUMAN-UAT.md (commit `2165eed`)
- Part 1 BLOCKING Wave-1 gate with 7 required-this-phase prereqs (Apple Bundle ID + Service ID, Google Web/iOS/Android client IDs, Android debug-keystore SHA-1, `MIN_APP_VERSION` bump coordination). Flags Open Q #1: iOS Bundle ID placeholder at `project.pbxproj` lines 274 + 303 (`org.reactjs.native.example.*`) — confirmed still present — for Wave 1 replacement.
- Part 2 manual UAT: Android smoke (D-19, incl. deep-link `vpnapp://payment/success?invoiceId=test123`, locked CTA "Upgrade to Pro at risevpn.com", `https://risevpn.com/en/pricing?return=app`), iOS deferred (D-20), and release-prep version checks.
- "NOT required this phase" section defers the upload-track prereqs per D-21.
- Value slots left blank (`____________`) for the orchestrator/operator to populate.

## Initial Jest Run

```
$ cd app && npm test -- --testPathIgnorePatterns='version.test|App.test'
Test Suites: 9 skipped, 0 of 9 total
Tests:       30 skipped, 30 total
exit: 0

$ cd app && npm test -- --testPathPattern=version.test
exit: 1   # intentional RED — Wave 4 turns it green

$ cd app && npx tsc --noEmit
0 errors
```

## Wave 1 Gate

**Wave 1 CANNOT start** until the operator fills the BLOCKING prerequisites in `05-HUMAN-UAT.md` Part 1 (Required this phase). If any required-this-phase item is missing, the Wave-1 plan stops at the operator-prereqs gate and reports the missing item. The iOS Bundle ID placeholder in `project.pbxproj` (lines 274 + 303) is the highest-risk gap (RESEARCH.md Open Q #1).

## Deviations from Plan

### Auto-fixed Issues

None — no production code was fixed. The three tasks were executed exactly as the plan specified (mocks, stubs, and UAT doc use the EXACT contents/strings dictated).

### Out-of-Scope Discovery (deferred, NOT fixed)

**1. [Scope boundary] Pre-existing `app/__tests__/App.test.tsx` is not runnable under Jest**
- **Found during:** Task 2 (running the Wave-0 suite gate).
- **Issue:** `App.test.tsx` (a pre-existing full-app render smoke test, NOT in this plan's `<files>`) fails before any code: `SyntaxError: Unexpected token 'export'` from `@react-navigation/native` v7 ESM; after a `transformIgnorePatterns` override it cascades to `netinfo` and `AsyncStorage` native-module nulls. Verified pre-existing via `git stash` (fails identically on committed HEAD without the new stubs).
- **Decision:** Deferred per SCOPE BOUNDARY + FIX-ATTEMPT-LIMIT (hit 3 attempts chasing the native-module cascade). Fully greening it requires a `setupFiles` chain of native-module mocks — Wave 3 component-testing / Phase 8 HARD-15 (RNTL) work, not Wave-0 scaffolding. Reverted the exploratory jest.config.js change; the Wave-0 stubs pass against the bare config.
- **Files modified:** none (jest.config.js restored to original committed state).
- **Logged in:** `.planning/phases/05-mobile-sso-pro-cta/deferred-items.md` (DEF-05-00-01).

### Note on the bare `npm test` command

A literal bare `cd app && npm test` exits 1 because of (a) the intentional `version.test.ts` RED (by design until Wave 4) and (b) the pre-existing `App.test.tsx` failure (deferred above). The plan's real Wave-0 gate is `npm test -- --testPathIgnorePatterns=version.test`; the Wave-0 stub suites themselves are 100% green (`--testPathIgnorePatterns='version.test|App.test'` exits 0). No stub file authored by this plan fails.

## Authentication Gates

None.

## Known Stubs

All 10 test files are intentional Wave-0 stubs (this is a scaffolding plan):
- 9 `describe.skip` suites — bodies are comments naming what Wave 2/3 fills in. By design; tracked in `05-VALIDATION.md` per-task map. NOT a blocker — Wave 0's deliverable IS the empty scaffolds.
- `version.test.ts` — intentionally RED guard; resolved by Wave 4 (`APP_VERSION = '2.2.0'`). Documented in the file header and in `05-HUMAN-UAT.md` release-prep.

No production-code stubs were introduced (Wave 0 ships no production code).

## Self-Check: PASSED

All 15 created files verified present on disk; all 3 task commits (`d069d35`, `d9d2c34`, `2165eed`) verified in git history.
