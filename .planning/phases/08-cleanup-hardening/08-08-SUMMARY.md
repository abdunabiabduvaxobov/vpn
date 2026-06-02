---
phase: 08-cleanup-hardening
plan: 08
subsystem: ci
tags: [security, ci, govulncheck, branch-protection, HARD-09]
requires: []
provides:
  - "PR-triggered govulncheck gate for server/api + server/tunnel (HARD-09)"
  - "branch-protection runbook + deliberate-vuln proof procedure (SC#3)"
affects:
  - .github/workflows/ci.yml
  - docs/ci/govulncheck-branch-protection.md
tech-stack:
  added:
    - "golang/govulncheck-action@v1 (official, exits non-zero on findings — true blocking)"
  patterns:
    - "one govulncheck job per Go module (api + tunnel) scanned with its own go.mod"
    - "PR-triggered CI workflow scoped to paths: server/** (first non-deploy workflow in repo)"
key-files:
  created:
    - .github/workflows/ci.yml
    - docs/ci/govulncheck-branch-protection.md
  modified: []
decisions:
  - "govulncheck-action@v1 needs no fail flag — non-zero exit on findings is its default (A2 confirmed against research-cited README)"
  - "Two jobs (govulncheck-api, govulncheck-tunnel) instead of one matrix — each module scanned with its own go.mod/go.sum graph; tunnel's xtls/xray-core is the highest-churn advisory risk"
  - "No-silencing/upgrade-to-suppress policy documented in workflow header — golang/govulncheck-action has no built-in mute flag"
metrics:
  duration: ~6m
  completed: 2026-06-02
status: in-repo-complete-pending-human-action
---

# Phase 8 Plan 08: govulncheck Blocking CI Gate Summary

A PR-triggered `govulncheck` CI gate (HARD-09 / S11-2) that fails on a vulnerable, reachable Go dependency in either `server/api` or `server/tunnel`, plus a documented one-time GitHub branch-protection toggle + deliberate-vuln proof that makes the red check actually merge-blocking (SC#3 "unmergeable").

## What Shipped (in-repo)

- **`.github/workflows/ci.yml`** — new `pull_request`-triggered workflow scoped to `paths: ['server/**']`, with two jobs:
  - `govulncheck-api` → `golang/govulncheck-action@v1`, `work-dir: server/api`, `go-version-file: server/api/go.mod` (Go 1.25).
  - `govulncheck-tunnel` → same action, `work-dir: server/tunnel`, `go-version-file: server/tunnel/go.mod` (imports xtls/xray-core).
  - Header comment documents the blocking semantics, the required manual branch-protection step, and the no-silencing/upgrade-to-suppress policy.
- **`docs/ci/govulncheck-branch-protection.md`** — runbook with Part 1 (enable required status checks `govulncheck-api` + `govulncheck-tunnel` on `main`), Part 2 (deliberate-vuln PR proof that the merge button is blocked), Part 3 (proof-recording table for the operator to fill in).

## Verification

| Check | Result |
|-------|--------|
| `ci.yml` exists, has `govulncheck-action`, `pull_request`, `server/api/go.mod`, `server/tunnel/go.mod` | PASS (grep) |
| `actionlint .github/workflows/ci.yml` | PASS (no errors) |
| `docs/ci/govulncheck-branch-protection.md` exists, mentions "branch protection" | PASS (grep) |

## Deviations from Plan

None — plan executed exactly as written. Task 1's optional verification of the action's fail-on-findings default (assumption A2) was confirmed against the research-cited `golang/govulncheck-action` README (§4.5, line 300/487/522 of 08-RESEARCH.md): the official action exits non-zero on findings by default, so no explicit failing flag was required.

## Human Action Required (Task 2 — checkpoint:human-action, gate=blocking)

The codebase cannot perform a GitHub repo-settings change. The following one-time manual step is required to make the gate merge-blocking (otherwise it ships advisory-only and SC#3's "unmergeable" wording is not satisfied):

1. **GitHub → Settings → Branches → Branch protection rules** for `main`: enable **"Require status checks to pass before merging"** and mark **`govulncheck-api`** and **`govulncheck-tunnel`** as required.
   - The checks only appear as selectable after they have run once on a PR, so open a small `server/**` PR (or the deliberate-vuln PR below) first if needed.
2. **Prove it blocks (deliberate-vuln PR):** add a dependency with a known GO advisory (e.g. an old `golang.org/x/...` version flagged by <https://pkg.go.dev/vuln/>) to `server/api`, ensure it is imported/reachable, `go mod tidy`, push the PR, and confirm `govulncheck-api` turns red and the merge button is blocked.
3. **Revert/close** the deliberate-vuln PR (do not merge).
4. **Record the outcome** (PR URL / screenshot) in the Part 3 table of `docs/ci/govulncheck-branch-protection.md`.

**Resume signal:** the operator types "approved" once branch protection is enabled and the deliberate-vuln PR was confirmed blocked, or describes what failed.

## Threat Coverage

- **T-08-09** (Tampering/Elevation, vulnerable dep in a PR) — mitigated by the govulncheck gate; fully enforced once the required-check toggle is on.
- **T-08-09b** (Repudiation, advisory-only silent non-block) — mitigated by the documented branch-protection runbook + deliberate-vuln proof.

## Self-Check: PASSED

- FOUND: .github/workflows/ci.yml
- FOUND: docs/ci/govulncheck-branch-protection.md
- FOUND commit: fed48f0 (ci.yml)
- FOUND commit: 2a7dbbe (branch-protection runbook)
