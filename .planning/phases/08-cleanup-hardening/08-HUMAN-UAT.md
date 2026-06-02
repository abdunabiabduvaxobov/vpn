---
status: partial
phase: 08-cleanup-hardening
source: [08-VERIFICATION.md]
started: 2026-06-02T00:00:00Z
updated: 2026-06-02T00:00:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. govulncheck merge-blocking branch protection (SC#3 / HARD-09 / plan 08-08)
expected: In GitHub → Settings → Branches → rule for `main`, "Require status checks to pass before merging" is enabled and `govulncheck-api` + `govulncheck-tunnel` are marked required. A deliberate-vuln PR (known-advisory dep, imported/reachable) turns the check red and the merge button is blocked; outcome recorded in the Part 3 table of `docs/ci/govulncheck-branch-protection.md`.
result: [pending]

### 2. On-device secure token storage + single coordinated re-login (SC#5 / HARD-16 / plan 08-09)
expected: After building the new app against the deployed 08-04 clean-break backend: iOS Keychain holds an entry for service `risevpn.auth` and `auth-tokens` is absent from the AsyncStorage manifest; Android encrypted-prefs XML present and `auth-tokens` absent from RKStorage; exactly ONE auth prompt on first launch of the new build; the captured `/auth/refresh` body contains `device_id` and a foreign `device_id` is rejected. Full procedure: `docs/manual-verification/08-keychain-asyncstorage.md`.
result: [pending]

## Summary

total: 2
passed: 0
issues: 0
pending: 2
skipped: 0
blocked: 0

## Gaps
