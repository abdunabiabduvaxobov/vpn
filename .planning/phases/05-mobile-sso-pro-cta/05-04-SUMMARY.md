---
phase: 05-mobile-sso-pro-cta
plan: 04
subsystem: release
tags: [versioning, android, gradle, aab, release-build, signing, jest, wave-4]

# Dependency graph
requires:
  - phase: 05-00
    provides: "Wave-0 version.test.ts intentional-RED guard (asserts APP_VERSION === '2.2.0')"
  - phase: 05-03
    provides: "Full mobile SSO + Pro CTA UI surface (Login/Payment/ActivatingProModal) — the code being shipped at 2.2.0"
provides:
  - "All 4 mobile version sources at 2.2.0 in lockstep (package.json, version.ts, build.gradle, ios pbxproj)"
  - "Wave-0 version.test flipped RED -> GREEN; full Jest suite (excl. deferred App.test) green at 10 suites / 45 tests"
  - "Signed Android release .aab (~155 MB) at app/android/app/build/outputs/bundle/release/app-release.aab, SHA-256 338c4819b8a78bc1d60f2a8a16d85458aaca1e1a4825172c1d38e45ead131131"
  - "Documented JDK-17 release-build toolchain (RN 0.84 Gradle plugin rejects host-default JDK 25)"
affects:
  - "End-of-milestone release phase — uploads to TestFlight + Play Internal Track (D-18 deferred), physical-device smoke (DEF-05-CREDS)"
  - "Backend MIN_APP_VERSION coordination — bump to 2.2.0 simultaneous with mobile store release"

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Single-source version constant (version.ts APP_VERSION) kept in lockstep with all 3 native version fields; version.test acts as the drift regression guard"
    - "Release .aab build requires JAVA_HOME pinned to JDK 17 (Temurin 17.0.18); host-default JDK 25 breaks com.facebook.react.settings plugin resolution"

key-files:
  created:
    - .planning/phases/05-mobile-sso-pro-cta/05-04-SUMMARY.md
  modified:
    - app/package.json
    - app/src/config/version.ts
    - app/android/app/build.gradle
    - app/ios/VpnApp.xcodeproj/project.pbxproj
    - .planning/phases/05-mobile-sso-pro-cta/05-HUMAN-UAT.md

key-decisions:
  - "Used the real vpn-upload.keystore (already present + gitignored) for the signed .aab rather than generating a phase-internal keystore — a fully Play-uploadable artifact, no D-21 fallback needed."
  - "Retried bundleRelease under JDK 17 after host-default JDK 25 broke RN 0.84's Gradle settings plugin (Rule 3 blocking-fix); recorded the toolchain requirement in 05-HUMAN-UAT.md."
  - "Task 3 (physical-device checkpoint:human-verify) recorded DEFERRED-PENDING per operator decision 2026-05-29 (DEF-05-CREDS) — NOT paused/blocked."

patterns-established:
  - "Version-drift guard: version.test.ts must stay green; any future version bump touches all 4 sources or the guard fails."

requirements-completed: [APP-07]

# Metrics
duration: 5min
completed: 2026-05-29
---

# Phase 5 Plan 04: Release Prep (Version Bump + Signed .aab) Summary

**All 4 mobile version sources bumped to 2.2.0 in lockstep, Wave-0 `version.test` flipped RED->GREEN, and a fully-signed ~155 MB Android release `.aab` produced via `./gradlew bundleRelease` (JDK 17); physical-device UAT recorded deferred-pending per operator decision.**

## Performance

- **Duration:** ~5 min
- **Started:** 2026-05-29T14:44:02Z
- **Completed:** 2026-05-29T14:49:20Z
- **Tasks:** 2 of 3 executed (Task 3 deferred-pending per operator)
- **Files modified:** 5

## Accomplishments
- Closed version drift across all 4 sources to `2.2.0` (package.json, `src/config/version.ts`, `build.gradle` versionName + versionCode 13, iOS pbxproj MARKETING_VERSION + CURRENT_PROJECT_VERSION 2 across Debug + Release).
- Turned Wave-0's intentionally-RED `version.test.ts` GREEN; full Jest suite (excl. pre-existing-broken `App.test.tsx`) passes — 10 suites / 45 tests, exit 0. `tsc --noEmit` exits 0.
- Produced a signed Android App Bundle (`app-release.aab`, 162,840,597 bytes / ~155 MB) signed with the real `vpn-upload` keystore; base manifest embeds versionName `2.2.0`. SHA-256 recorded in 05-HUMAN-UAT.md.
- Diagnosed + worked around an environment toolchain blocker: RN 0.84's Gradle settings plugin rejects host-default JDK 25; pinned `JAVA_HOME` to JDK 17 to build.

## 4-File Version Diff

| Source | Before | After |
|--------|--------|-------|
| `app/package.json` | `"version": "2.1.0"` | `"version": "2.2.0"` |
| `app/src/config/version.ts` | `APP_VERSION = '2.1.0'` | `APP_VERSION = '2.2.0'` |
| `app/android/app/build.gradle` | `versionCode 12` / `versionName "2.1.0"` | `versionCode 13` / `versionName "2.2.0"` |
| `app/ios/...project.pbxproj` | `CURRENT_PROJECT_VERSION = 1` / `MARKETING_VERSION = 1.0` (x2 Debug+Release) | `CURRENT_PROJECT_VERSION = 2` / `MARKETING_VERSION = 2.2.0` (x2 Debug+Release) |

Old values fully removed (grep for `2.1.0` in build.gradle and `1.0` in pbxproj both return nothing).

## Signed Artifact

- **Path:** `app/android/app/build/outputs/bundle/release/app-release.aab`
- **Size:** 162,840,597 bytes (~155 MB)
- **SHA-256:** `338c4819b8a78bc1d60f2a8a16d85458aaca1e1a4825172c1d38e45ead131131`
- **Signed with:** real `vpn-upload` keystore (`META-INF/VPN-UPLO.RSA` present in the bundle; SHA256withRSA, valid until 2053)
- **Embedded version:** base AndroidManifest contains `2.2.0`
- **Build log:** `BUILD SUCCESSFUL in 2m 12s`, ran through `:app:signReleaseBundle` -> `:app:bundleRelease`
- **NOT uploaded** to Play Console / TestFlight (D-18 explicit defer to end-of-milestone release phase).
- The `.aab` itself is gitignored (build output); the SHA-256 in 05-HUMAN-UAT.md is the durable record.

## Verification Results

```
$ cd app && npx jest --testPathPattern=version.test
PASS src/config/__tests__/version.test.ts  (1 passed)   # Wave-0 intentional RED is now GREEN

$ cd app && npm test -- --testPathIgnorePatterns='App.test'
Test Suites: 10 passed, 10 total
Tests:       45 passed, 45 total
exit 0

$ cd app && npx tsc --noEmit
TSC_EXIT=0   # fully clean

$ # all 4 sources confirmed at 2.2.0; old values absent
```

## Task Commits

1. **Task 1: Bump all 4 version sources to 2.2.0** - `b9d6178` (chore)
2. **Task 2: Produce signed .aab + record release-prep UAT status** - `65ae053` (chore)

**Plan metadata:** committed by the orchestrator with STATE.md/ROADMAP.md (this executor does NOT touch those per objective).

_Task 3 (physical-device checkpoint:human-verify) was NOT executed — recorded deferred-pending (see below)._

## Files Created/Modified
- `app/package.json` - version field 2.1.0 -> 2.2.0
- `app/src/config/version.ts` - APP_VERSION constant (X-App-Version request header) 2.1.0 -> 2.2.0
- `app/android/app/build.gradle` - versionName 2.2.0 + versionCode 13
- `app/ios/VpnApp.xcodeproj/project.pbxproj` - MARKETING_VERSION 2.2.0 + CURRENT_PROJECT_VERSION 2 (Debug + Release)
- `.planning/phases/05-mobile-sso-pro-cta/05-HUMAN-UAT.md` - ticked release-prep version checks + build-.aab item with path/size/SHA-256; physical-device smoke items annotated DEFERRED-PENDING
- `.planning/phases/05-mobile-sso-pro-cta/05-04-SUMMARY.md` - this file

## Decisions Made
- **Real upload keystore over phase-internal:** `app/vpn-upload.keystore` + `keystore.properties` were already configured (gitignored). Building with them yields a real Play-uploadable artifact, so the D-21 phase-internal-keystore fallback was unnecessary.
- **Deferred Task 3, did not block:** Per the executor's `<checkpoint_is_operator_deferred>` directive and operator decision 2026-05-29, physical-device install + smoke + store upload are deferred to the end-of-milestone release phase (DEF-05-CREDS). The Part 2 manual smoke items stay `pending`/`DEFERRED-PENDING`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Release build failed under host-default JDK 25 — retried with JDK 17**
- **Found during:** Task 2 (`./gradlew bundleRelease`)
- **Issue:** First invocation (host default `java` = OpenJDK 25.0.2) failed at settings-plugin resolution: `Error resolving plugin [id: 'com.facebook.react.settings'] > 25.0.2`. RN 0.84's React Native Gradle Plugin + AGP do not support JDK 25 (officially JDK 17).
- **Fix:** Re-ran with `JAVA_HOME=/Library/Java/JavaVirtualMachines/temurin-17.jdk/Contents/Home` (Temurin 17.0.18, already installed). Build succeeded.
- **Files modified:** none (toolchain-only; documented the requirement in 05-HUMAN-UAT.md so the operator re-runs with JDK 17).
- **Verification:** `BUILD SUCCESSFUL in 2m 12s`; signed `.aab` produced at the expected path.
- **Committed in:** no code change; the UAT-doc note is part of `65ae053`.

---

**Total deviations:** 1 auto-fixed (1 blocking).
**Impact on plan:** The fix was a toolchain selection, not a code/scope change. No scope creep. The plan's version bump + signed-artifact deliverables landed exactly as specified.

## Issues Encountered
- JDK-25-vs-RN-0.84 incompatibility (see Deviation 1) — resolved by pinning JDK 17.

## Deferred Items

**Task 3 — Physical-device manual UAT (checkpoint:human-verify) — DEFERRED-PENDING (DEF-05-CREDS).**
- Operator decision (2026-05-29): the signed-build install on a physical Android device, the 6-item smoke checklist (Google sign-in, guest->Google `users.id` preservation, PaymentScreen informational layout, interstitial->browser handoff, deep-link receive), and the store upload are ALL deferred to the end-of-milestone release phase.
- The autonomous deliverables (version bump + green version.test + attempted/successful signed `.aab`) are the durable, verifiable artifacts of this plan. The signed `.aab` is ready for the operator to install + smoke whenever the release phase runs.
- Live SSO sign-in additionally requires replacing the `PLACEHOLDER_*` OAuth client IDs (DEF-05-CREDS) — those sentinels remain intact and untouched by this plan.
- 05-HUMAN-UAT.md Part 2 "Operator confirms Part 2 UAT passed" and "Phase 5 complete" sign-off date lines are intentionally left blank — they are filled when the deferred device UAT actually runs.

## Phase 5 Closeout (Success Criteria)
- **SC#1 (Apple -> Home):** code complete (05-03 LoginScreen); live verification deferred (DEF-05-CREDS).
- **SC#2 (guest->SSO preserves id):** code complete (Phase 2 D-06 + 05-02 service layer); live verification deferred.
- **SC#3 (informational PaymentScreen):** code complete + unit-verified (05-03: zero prices, no IAP, single risevpn.com CTA).
- **SC#4 (deep-link -> modal):** code complete + unit-verified (05-03 ActivatingProModal).
- **SC#5 (2.2.0 + smoked):** version bump DONE + signed `.aab` DONE; **DELIBERATELY DEVIATED per D-18** — local signed build + (deferred) operator device-smoke instead of TestFlight/Play Internal upload. Uploads land in the end-of-milestone release phase.

## Stub / Threat Scan
- **Known Stubs:** None introduced by this plan. The `PLACEHOLDER_*` OAuth sentinels (DEF-05-CREDS, Wave 1/2) remain intact and untouched — operator-authorized deferral, resolved at store-upload time.
- **Threat Flags:** None. Wave 4 adds no new attack surface (build + version metadata only). Version-drift threat is mitigated (all 4 sources lockstep + `version.test` regression guard). The signed-artifact-tamper threat is mitigated (SHA-256 recorded for operator pre-install verification).

## User Setup Required
- **End-of-milestone release phase (DEF-05-CREDS):** replace all `PLACEHOLDER_*` OAuth client IDs, confirm backend `MIN_APP_VERSION` bump to 2.2.0 simultaneous with the store release, install `app-release.aab` on a physical Android device and run the 6-item Part 2 smoke, then upload to Play Internal + TestFlight.
- **Re-building the .aab:** must run with `JAVA_HOME` set to JDK 17 (Temurin 17.0.18) — host-default JDK 25 breaks the RN 0.84 Gradle plugin.

## Next Phase Readiness
- Codebase is in a shippable 2.2.0 state; a signed `.aab` is sitting in `build/outputs/` ready for device-smoke + upload.
- No blockers for the autonomous deliverables. The remaining work (device UAT, OAuth creds, store upload) is the deferred end-of-milestone release activity, already tracked under DEF-05-CREDS.

## Self-Check: PASSED
- All 4 modified version files + 05-HUMAN-UAT.md + 05-04-SUMMARY.md + the signed `app-release.aab` verified present on disk.
- Both task commits (`b9d6178`, `65ae053`) verified in git history.
- STATE.md / ROADMAP.md NOT modified (orchestrator owns those writes, per objective).

---
*Phase: 05-mobile-sso-pro-cta*
*Completed: 2026-05-29*
