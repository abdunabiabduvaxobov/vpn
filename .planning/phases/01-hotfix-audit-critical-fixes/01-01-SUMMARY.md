---
phase: 01-hotfix-audit-critical-fixes
plan: 01
subsystem: security
tags: [go, cli, bcrypt, golang.org/x/term, password-handling, hotfix]

# Dependency graph
requires: []
provides:
  - "createadmin CLI reads password from stdin (TTY echo-off via golang.org/x/term + bufio piped fallback)"
  - "Seeded admin row defaults to subscription_tier='free' (was the 'ultimate' bug)"
  - "Regression test scaffold for cmd/createadmin (TestCreateAdmin_RejectsPasswordFlag / SeedsFreeTier / AcceptsPipedStdin)"
  - "Extracted createAdminUser(db, emailHash, hash) helper — testable seam"
  - "Extracted readPassword(in, prompt) helper — testable stdin handler"
  - "Dockerfile docker-exec comment no longer leaks an `-password=...` example"
affects:
  - 01-08-PLAN (HOTFIX-08 env validation)
  - 01-09-PLAN (smoke step 9 verifies these behaviors on staging)
  - phase-2 (auth/SSO will reuse the password-via-stdin pattern if any new admin tooling lands)

# Tech tracking
tech-stack:
  added:
    - "golang.org/x/term v0.25.0 (promoted from indirect to direct require)"
  patterns:
    - "Password input reads from stdin only — argv never carries secrets (ASVS V6.2.3)"
    - "TTY-aware password helper: term.IsTerminal → term.ReadPassword (echo-off); else bufio.NewReader (piped, warned)"
    - "Helpers (createAdminUser, readPassword) extracted from main() to enable in-memory sqlite + os.Pipe tests"

key-files:
  created:
    - "server/api/cmd/createadmin/main_test.go"
  modified:
    - "server/api/cmd/createadmin/main.go"
    - "server/api/go.mod"
    - "server/api/go.sum"
    - "server/api/Dockerfile"

key-decisions:
  - "Use golang.org/x/term over raw bufio for TTY path (CONTEXT.md discretion; matches sudo's password UX)"
  - "Fall back to bufio.NewReader when stdin is not a TTY so CI/automation pipes work without exposing argv"
  - "Refactor main() to extract createAdminUser + readPassword instead of //go:embed grepping (cleaner tests)"
  - "Trim only trailing \\r\\n from piped stdin so passwords containing internal whitespace are preserved"

patterns-established:
  - "Pattern: secret input via stdin only — `func readPassword(in *os.File, prompt *os.File) (string, error)` with TTY-vs-pipe branching"
  - "Pattern: extract a testable createXxxUser helper from CLI main() so role/tier seeding can be asserted against in-memory sqlite"
  - "Pattern: CLI flag-rejection regression test via `exec.Command('go', 'run', ...)` capturing CombinedOutput"

requirements-completed: [HOTFIX-06]

# Metrics
duration: ~5min
completed: 2026-05-21
---

# Phase 01 Plan 01: HOTFIX-06 — createadmin reads password from stdin + seeds tier=free Summary

**Replaced the leaky `-password` argv flag with a stdin-based echo-off prompt (golang.org/x/term) plus a piped-fallback path, and fixed the hardcoded `subscription_tier="ultimate"` seed bug — all in one atomic commit on the working branch.**

## Performance

- **Duration:** ~5 min (~259s wall-clock)
- **Started:** 2026-05-21T23:22:03Z
- **Completed:** 2026-05-21T23:26:22Z
- **Tasks:** 2 / 2 (Task 1 stub + Task 2 implementation + commit, per the plan's atomic-commit invariant)
- **Files modified:** 5 (1 created, 4 modified)

## Accomplishments

- **HOTFIX-06 (S2-1) closed:** `-password` argv flag removed; password now read from stdin via `term.ReadPassword` (TTY) or `bufio.NewReader` (piped). `ps`, shell history, journald, and `docker inspect` no longer carry the operator's plaintext password.
- **ROADMAP §Phase 1 success criterion #8 (part B) closed:** seeded admin row defaults to `subscription_tier='free'` (was hardcoded to `"ultimate"`).
- **golang.org/x/term v0.25.0** promoted from indirect (transitive via crypto) to a direct require in `go.mod`.
- **Three regression tests added** to `cmd/createadmin/main_test.go` — all PASS, no SKIPs:
  - `TestCreateAdmin_RejectsPasswordFlag` — spawns `go run ./cmd/createadmin -password=...`, asserts non-zero exit + `flag provided but not defined: -password` in stderr.
  - `TestCreateAdmin_SeedsFreeTier` — exercises `createAdminUser` against an in-memory sqlite DB, asserts both the returned struct AND the persisted row have `subscription_tier='free'` and `role='admin'`.
  - `TestCreateAdmin_AcceptsPipedStdin` — exercises `readPassword` with `os.Pipe`, asserts the piped fallback returns the password (no `inappropriate ioctl for device` error).
- **Dockerfile comment** at line 13 updated: the docker-exec example no longer carries `-password=...`; new wording is `docker exec -it vpn-api ./createadmin -email=...` (password via stdin).

## Task Commits

Per plan D-01 (one atomic commit per hotfix) and the plan's Task 1 `<done>` clause, Tasks 1 and 2 land in a single atomic commit:

1. **Tasks 1 + 2 (HOTFIX-06: stdin prompt + free-tier seed + tests + Dockerfile comment)** — `63fde77` (hotfix)

**No metadata commit** (per the orchestrator's directive: STATE.md / ROADMAP.md / REQUIREMENTS.md are written by the parent agent after all worktree agents complete).

## Files Created/Modified

- **Created** `server/api/cmd/createadmin/main_test.go` — three regression tests covering argv rejection, free-tier seeding, and piped-stdin fallback (no SKIPs).
- **Modified** `server/api/cmd/createadmin/main.go` — deleted `-password` flag, added `readPassword(in, prompt)` helper (TTY/pipe branched), extracted `createAdminUser(db, emailHash, hash)` from main(), changed `SubscriptionTier: "ultimate"` → `"free"`.
- **Modified** `server/api/go.mod` — added `golang.org/x/term v0.25.0` to the direct require block. (Side-effect: `go mod tidy` correctly recategorized `github.com/go-telegram-bot-api/telegram-bot-api/v5` and `github.com/google/uuid` from indirect to direct — both were already imported by `internal/bot/recovery.go` and `internal/handler/auth.go`; this is housekeeping, not a new dependency.)
- **Modified** `server/api/go.sum` — checksums for `golang.org/x/term` v0.25.0.
- **Modified** `server/api/Dockerfile` (line 13 comment only) — removed the `-password=...` example from the docker-exec hint.

## Decisions Made

- **TTY-aware password helper rather than always-bufio:** kept the sudo-style echo-off UX for the interactive case (the primary operator path) while preserving CI/automation through the piped fallback (with a clearly-worded stderr warning). Plan-recommended.
- **Helpers extracted from main() rather than grepping source from a test:** the plan offered `//go:embed` of `main.go` + grep as a fallback for the free-tier assertion; chose the preferred refactor instead. `createAdminUser(db, emailHash, hash)` and `readPassword(in, prompt)` are now unit-testable in isolation and the tests assert the real INSERT path (with persisted-row re-read) rather than a textual check on source.
- **Trim only `\r\n` (not all whitespace) from piped stdin:** preserves passwords that contain leading or trailing spaces. `strings.TrimRight(line, "\r\n")` mirrors the behavior of `term.ReadPassword`, which strips only the newline.
- **`docker exec -it` (interactive TTY) in the Dockerfile comment:** required so the operator's stdin reaches the container as a TTY and the echo-off prompt fires (otherwise the container's stdin is non-TTY and we'd hit the piped-fallback path silently).

## Deviations from Plan

None of the implementation-level deviation rules fired. Two minor toolchain side-effects:

### Toolchain side-effects (not rule-driven deviations)

**1. `go get golang.org/x/term@latest` upgraded `go.mod`'s Go directive from 1.22.0 → 1.25.0**
- **Cause:** The local Go toolchain is 1.26.1; modern `go get` updates the Go directive to match the resolved minimum version of the new dep's go.mod. Latest term is v0.43.0 which declares `go 1.25`.
- **Why this matters:** CLAUDE.md locks the backend tech stack to **Go 1.22**. Promoting the go directive would break the constraint and silently let later packages depend on 1.25-only stdlib APIs.
- **Resolution:** `git checkout go.mod go.sum` to revert the toolchain churn, then re-rewrote `main.go` first (so the new imports are visible) and re-ran `go mod tidy`. With the imports in place, `tidy` resolved `golang.org/x/term v0.25.0` (the highest version still on go 1.22) and left `go 1.22.0` unchanged. **No Go version bump shipped.**

**2. `go mod tidy` recategorized two transitive-but-actually-direct dependencies**
- **Found:** `github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.5.1` and `github.com/google/uuid v1.5.0` migrated from the `// indirect` block to the direct require block.
- **Verification:** both packages are imported by first-party code (`internal/bot/recovery.go`, `internal/handler/auth.go`, etc.), so the previous `// indirect` annotation was simply stale. `tidy` is correcting the bookkeeping; this is not a new dependency or a behavior change.
- **Decision:** kept the correction in the commit (rolling it back would just leave `go.mod` lying about reality, and the next call to `tidy` from any developer would re-promote them).

---

**Total deviations:** 0 rule-driven (Rules 1-4 did not apply). 2 toolchain side-effects handled inline (Go directive reverted, `tidy` corrections kept).
**Impact on plan:** None. The commit's contract (files touched, behavior change, message format, test pass count) matches the plan's acceptance criteria verbatim.

## Issues Encountered

- **Worktree base mismatch.** This worktree's branch was based on the existing main HEAD (`035d069`) rather than the planning HEAD (`a1d9121`). The pre-execution `worktree_branch_check` flagged the divergence; resolved via `git reset --soft a1d91219c464d312a039f5fac3c7ae3812dc5037` followed by `git checkout HEAD -- .` to materialize the `.planning/` tree (which only exists on the planning branch) and the per-phase CONTEXT/RESEARCH/PLAN files. No commits were lost; the unrelated `.claude/settings.local.json` modification was preserved via `git stash` / `stash pop`.
- **No other issues.** Tests passed first time after the implementation landed. `go build ./...` against the entire module also passed (transitive impact zero).

## User Setup Required

None — HOTFIX-06 is a build-time-only change (CLI tool used at bootstrap). The new prompt fires interactively on next `createadmin` run; no environment variable or external service touched.

The plan-09 staging smoke checklist (step 9) re-verifies `./createadmin -password=anything` errors with `not defined` and that a fresh admin row has `subscription_tier='free'`. That smoke is owned by plan 09, not this plan.

## Verification Evidence

```
$ cd server/api && go test ./cmd/createadmin/... -v -count=1 -run TestCreateAdmin_
=== RUN   TestCreateAdmin_RejectsPasswordFlag
--- PASS: TestCreateAdmin_RejectsPasswordFlag (0.12s)
=== RUN   TestCreateAdmin_SeedsFreeTier
--- PASS: TestCreateAdmin_SeedsFreeTier (0.00s)
=== RUN   TestCreateAdmin_AcceptsPipedStdin
--- PASS: TestCreateAdmin_AcceptsPipedStdin (0.00s)
PASS
ok  vpnapp/server/api/cmd/createadmin  0.928s
```

```
$ git log -1 --format=%s
hotfix(01): createadmin reads password from stdin + seeds tier=free [HOTFIX-06]

$ git diff-tree --no-commit-id --name-only -r HEAD | sort
server/api/Dockerfile
server/api/cmd/createadmin/main.go
server/api/cmd/createadmin/main_test.go
server/api/go.mod
server/api/go.sum

$ grep -E '^\s*golang.org/x/term\s' server/api/go.mod | grep -v indirect
    golang.org/x/term v0.25.0

$ ! grep -q 'flag\.String("password"' server/api/cmd/createadmin/main.go && echo OK
OK

$ grep -q 'term\.ReadPassword' server/api/cmd/createadmin/main.go && echo OK
OK

$ grep -q 'SubscriptionTier: "free"' server/api/cmd/createadmin/main.go && echo OK
OK

$ ! grep -q 'SubscriptionTier: "ultimate"' server/api/cmd/createadmin/main.go && echo OK
OK
```

## Confirmation: Runbook / Docs `-password=` references

Searched `docs/`, `server/api/README.md`, and `server/api/Dockerfile`:

- `server/api/Dockerfile:13` — **updated in this commit**, no longer mentions `-password=...`.
- `docs/**` — `NO_HITS` (no references to update).
- `server/api/README.md` — does not exist (no file).

Post-edit re-grep returns `NO_HITS_REMAIN`. No further runbook updates needed.

## Next Plan Readiness

- **Plan 01-02 (HOTFIX-08 env validation framework)** is unblocked. It needs `internal/config/config.go` and `cmd/main.go` — both untouched by this plan.
- **Cross-plan invariant maintained:** `server/api/internal/handler/payment.go` was NOT touched (per D-07, that file is being deleted in Phase 8 and zero paying Stripe users exist; the plan-01 invariant `[ $(git diff base..HEAD -- payment.go | wc -l) -eq 0 ]` holds).
- **Test scaffold available for reuse:** `openSeedTestDB` in `cmd/createadmin/main_test.go` is local to the createadmin package today; if HOTFIX-01 / HOTFIX-02 tests want a shared in-memory users table they can promote it to `internal/repository/testdb.go` per VALIDATION.md Wave 0 optional item.
- **Operator-facing change:** any internal runbook calling `./createadmin -password=...` must be updated. The operator should be informed that bootstrapping the first admin now requires `docker exec -it vpn-api ./createadmin -email=admin@example.com` and a TTY-attached terminal session. (Plan 09 smoke step 9 will exercise this on staging.)

## Self-Check: PASSED

- **Files claimed exist:** all 5 paths verified present (`FOUND` for each).
- **Commit claimed exists:** `63fde77` is HEAD on the worktree branch; subject line matches `^hotfix\(01\): .*HOTFIX-06`.
- **Tests claimed pass:** re-ran `go test ./cmd/createadmin/...` from a clean shell; three `--- PASS` lines, zero `--- SKIP`, zero `--- FAIL`.
- **Module builds:** `go build ./...` (entire backend module) returns exit 0.
- **Smoke (plan success criterion #1):** `go run ./cmd/createadmin -password=anything` exits 2 with stderr `flag provided but not defined: -password`.

---
*Phase: 01-hotfix-audit-critical-fixes*
*Completed: 2026-05-21*
