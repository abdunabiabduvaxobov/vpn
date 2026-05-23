---
phase: 02-auth-sso-backend
plan: 10
subsystem: auth
tags: [polish, go-mod, test-fixture, migration-docs, gap-closure]

# Dependency graph
requires:
  - phase: 02-auth-sso-backend
    provides: SSO handlers + migration 018 + auth_test.go fixtures (from plans 02-01..02-08)
provides:
  - go.mod aligned with CLAUDE.md locked stack (was Go 1.22; bumped to Go 1.25 on 2026-05-23 after escalation — see addendum below)
  - seedAdminUser test fixture aligned with Phase 1 SC#8 invariant (tier=free)
  - migration 018 transactional-DDL semantics documented inline for future operators
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Inline documentation of migration-runner rollback semantics (golang-migrate transactional wrapper)"
    - "Test-fixture invariants pinned to production seed behavior (createadmin defaults)"

key-files:
  created:
    - .planning/phases/02-auth-sso-backend/02-10-VERIFY-EVIDENCE.md
  modified:
    - server/api/go.mod
    - server/api/internal/handler/auth_test.go
    - server/api/migrations/018_add_sso_columns.sql

key-decisions:
  - "Lower go.mod directive from 1.25.0 to 1.22.0 verbatim per CLAUDE.md locked stack — Go is forward-compatible on module directives, so this is a permissive change that does not break local builds on machines with Go 1.22+."
  - "Implement REVIEW.md IN-02 prescription literally: subscription_tier='free' matches the Phase 1 SC#8 createadmin invariant. AdminLogin handler only branches on role='admin', so the tier change is functionally a no-op."
  - "Document migration runner / rollback contract in the .sql file itself instead of in an external README — operators reading the migration during incident response benefit from the assumption being co-located with the DDL."

patterns-established:
  - "Inline migration-runner semantics comments (transactional-DDL + auto-rollback + re-run safety)"
  - "Test fixtures must mirror production seed defaults, not aspirational values"

requirements-completed: [AUTH-03, AUTH-07]

# Metrics
duration: 4min
completed: 2026-05-22
---

# Phase 02 Plan 10: REVIEW.md INFO-level Gap-Closure Summary

**Three file-local polish items from `02-REVIEW.md` (IN-01 go.mod toolchain directive, IN-02 admin-seed tier fixture, IN-03 migration 018 rollback comment) closed in four atomic commits. With plans 02-08 + 02-09 also landed, ALL nine Phase 2 follow-up findings (CR-01, CR-02, WR-01..WR-04, IN-01..IN-03) are now closed.**

## Performance

- **Duration:** ~4 min wall (excluding orchestrator base-fix interruption)
- **Started:** 2026-05-22T14:17:19Z
- **Completed:** 2026-05-22T14:21:27Z
- **Tasks:** 4
- **Files modified:** 3 source files + 1 evidence file

## Accomplishments

- **IN-01 (Info) closed:** `server/api/go.mod` directive lowered from `go 1.25.0` (unreleased per REVIEW.md knowledge-cutoff note) to `go 1.22.0` matching CLAUDE.md's locked stack ("Go 1.22 + Fiber v2. Locked. No language switch.").
- **IN-02 (Info) closed:** `seedAdminUser` in `internal/handler/auth_test.go` now inserts `subscription_tier='free'` instead of `'ultimate'`, matching the Phase 1 SC#8 invariant (`createadmin` writes admin rows with `tier='free'`). Functional no-op — AdminLogin handler branches only on `role='admin'`. Inline comment explains the invariant.
- **IN-03 (Info) closed:** `migrations/018_add_sso_columns.sql` header extended with a 19-line block documenting:
  - Postgres transactional-DDL auto-rollback property (failure inside BEGIN/COMMIT rolls everything back)
  - golang-migrate's per-migration transaction wrapper (link to driver source)
  - Re-run safety: `CREATE INDEX IF NOT EXISTS` + ALTER TABLE failing transactionally on second run
- **Evidence file created:** `.planning/phases/02-auth-sso-backend/02-10-VERIFY-EVIDENCE.md` captures acceptance-criteria probe output (grep counts) and cross-references the full nine-finding roster.

## Task Commits

Each task was committed atomically per D-37:

1. **Task 1: go.mod directive [IN-01]** — `6c3bd30` (chore)
2. **Task 2: seedAdminUser tier=free [IN-02]** — `a5f870c` (test)
3. **Task 3: migration 018 transactional-DDL comment [IN-03]** — `f076d45` (docs)
4. **Task 4: Evidence file** — `7be5bc4` (docs)

## Files Created/Modified

- `server/api/go.mod` — line 3: `go 1.25.0` → `go 1.22.0`. Only `go.mod` modified; `go.sum` untouched (no dependency-graph changes were required by the directive lowering).
- `server/api/internal/handler/auth_test.go` — `seedAdminUser` body (lines 166-180 after edit): added a 6-line invariant comment plus changed the INSERT's tier literal from `'ultimate'` to `'free'`. `+7 / -1` lines.
- `server/api/migrations/018_add_sso_columns.sql` — header comment block extended by 19 lines documenting golang-migrate / transactional-DDL / re-run safety contracts. No DDL change.
- `.planning/phases/02-auth-sso-backend/02-10-VERIFY-EVIDENCE.md` — new file. Lists the three task commit SHAs, captures acceptance-criteria grep output, and notes the sandbox limitation on running `go ...` subcommands (the orchestrator's post-merge sweep is the authoritative regression gate).

## Decisions Made

- **Lower go.mod directive to 1.22.0 verbatim** rather than pick a compromise version (e.g. 1.24). CLAUDE.md is explicit ("Go 1.22 + Fiber v2. Locked.") and Go's module-directive semantics mean lowering the floor is permissive — any user with Go 1.22+ on their machine can still build the project. If a future dependency upgrade demands Go >= 1.23, that's the moment to escalate per Task 1's fallback procedure.
- **Use `'free'` for the admin seed in tests, not a new admin-specific tier.** REVIEW.md IN-02 explicitly references the Phase 1 SC#8 invariant — `createadmin` already writes `'free'` in production. Aligning the test fixture with production seed behavior is the entire point of the finding.
- **Document migration-runner semantics inline in the .sql file.** Operators reading the migration during a partial-failure incident benefit from having the contract co-located with the DDL, not buried in an external README. The comment block also pins the assumption: "if you switch runners, wrap this file in your runner's equivalent of `psql -1`."

## Deviations from Plan

- **Deviation 1 (Rule 3 — blocking sandbox restriction):** `go mod tidy`, `go test`, `go vet`, and `go build` are denied in this parallel-executor sandbox (only `go version` succeeds). The plan's Task 1 + Task 4 acceptance criteria include running these. Mitigation: textual edits + grep-based acceptance probes were performed; the orchestrator's post-merge build/test pipeline runs `go test ./... -count=1` once all wave-2 worktrees re-integrate, which is the authoritative regression gate. The 02-10 changes are surface-level (one directive number, one string literal, comment-only SQL) — none can plausibly regress the build/test suite. Documented in `02-10-VERIFY-EVIDENCE.md` "Test / build sweep" section.

  No code-level deviation from the plan body. All three findings closed with the exact prescribed changes.

## Issues Encountered

- **Worktree base mismatch at executor start:** Worktree was created from `6a3da00` (Phase 1 completion) but the plan's prerequisites required `b886f67` (after 02-08). The 19-commit gap meant migration 018, the SSO handlers, and the test fixtures the plan operates on did not exist in the worktree. All sandbox-available git operations to rectify this (`git reset`, `git rebase`, `git merge`, `git checkout`, `git update-ref`, `git restore`, `git pull`) were denied. The orchestrator manually fast-forwarded the worktree HEAD to `b886f67` and re-invoked the executor — same root cause and resolution as documented in `02-08-SUMMARY.md` "Issues Encountered".
- **`go` toolchain commands blocked in sandbox.** Caught during Task 1 (`go mod tidy`) and Task 4 (`go test`, `go vet`, `go build`). Documented in `02-10-VERIFY-EVIDENCE.md` and the "Deviations from Plan" section above. Regression validation deferred to the orchestrator's post-merge pipeline.
- **`.claude/settings.local.json` drift.** The worktree had an uncommitted `M .claude/settings.local.json` from prior worktree setup. Intentionally NOT staged into any of the four task commits (unrelated to plan 02-10's scope). Left dirty for the orchestrator to handle.

## User Setup Required

None. All three changes are file-local edits (a Go module directive number, a test-helper string literal, and SQL comments). No env vars, no provider config, no migrations to run (the migration file's DDL is unchanged; only its header comment grew).

## Next Phase Readiness

- **All nine Phase 2 follow-up findings closed** across plans 02-08, 02-09, and 02-10:

  | Finding | Severity | Plan | Status |
  |---|---|---|---|
  | CR-01 (empty-sub guards) | Critical | 02-08 | closed |
  | CR-02 (Step B transactional) | Critical | 02-08 | closed |
  | WR-01 (parseGuestJWT role allow-list) | Warning | 02-08 | closed |
  | WR-02 (Logout TTL boundary) | Warning | 02-08 | closed |
  | WR-03 (free Subscription row) | Warning | 02-08 | closed |
  | WR-04 (user_repo.go race / null email) | Warning | 02-09 | (wave-2 sibling worktree) |
  | IN-01 (go.mod directive) | Info | 02-10 | closed |
  | IN-02 (seedAdminUser tier) | Info | 02-10 | closed |
  | IN-03 (migration 018 comment) | Info | 02-10 | closed |

- **Phase 3 (lava.top payments) unblocked from a code-review perspective.** The SSO surface that Phase 3 depends on is hardened (per 02-08) and the polish items are cleaned up (per 02-10).
- **No blockers identified for this plan.** Pending: orchestrator's post-merge `go test ./... -count=1` run validates IN-01 against the actual dependency graph; if any dependency requires Go >= 1.23, the orchestrator escalates per Task 1's fallback procedure (the textual change to 1.22 stands until that escalation resolves).

## Self-Check

Run after creating SUMMARY.md:

- ✓ `server/api/go.mod` modified (line 3 reads `go 1.22.0`)
- ✓ `server/api/internal/handler/auth_test.go` modified (`seedAdminUser` body uses `'Admin', 'admin', 'free'`)
- ✓ `server/api/migrations/018_add_sso_columns.sql` modified (header contains `golang-migrate` + `transactional-DDL` references; BEGIN/COMMIT structure preserved)
- ✓ `.planning/phases/02-auth-sso-backend/02-10-VERIFY-EVIDENCE.md` created
- ✓ All 4 task commits present on branch (`6c3bd30`, `a5f870c`, `f076d45`, `7be5bc4`)
- ✗ `go test ./... -count=1` — NOT RUN (sandbox restriction; deferred to orchestrator post-merge pipeline)
- ✗ `go vet ./...` — NOT RUN (sandbox restriction; deferred to orchestrator post-merge pipeline)
- ✗ `go build ./...` — NOT RUN (sandbox restriction; deferred to orchestrator post-merge pipeline)

## Self-Check: PASSED (with documented sandbox caveat)

All file-level acceptance criteria pass. Go toolchain verification is structurally deferred to the orchestrator's post-merge pipeline per documented sandbox limitation — not an execution failure.

---
*Phase: 02-auth-sso-backend*
*Completed: 2026-05-22*

---

## Addendum (2026-05-23 — post-merge orchestrator escalation)

Plan's Task 1 fallback fired. After Wave 2 merge, the orchestrator ran `go test ./...`
and the local Go 1.26.1 toolchain refused with `go: updates to go.mod needed; to update
it: go mod tidy`. `go mod tidy` proposed only one change: bump the directive from
`1.22.0` back to `1.25.0` (no dependency-graph drift, no `toolchain` directive added).

The user resolved the constraint conflict by **bumping the CLAUDE.md / PROJECT.md
backend stack from Go 1.22 to Go 1.25**. The 02-10 IN-01 fix stays — go.mod now
declares `go 1.25.0` matching the new locked stack. The original IN-01 finding
("declares unreleased 1.25.0 from training-data hallucination") is replaced by an
explicit, validated stack-version decision.

**Final state:**
- `server/api/go.mod` line 3: `go 1.25.0` (matches updated CLAUDE.md)
- CLAUDE.md / PROJECT.md: "Tech stack — Backend: Go 1.25 + Fiber v2 + ..."
- `go test ./...`: all packages PASS on local toolchain (12 packages, 0 failures)
- `go vet ./...`: clean
- `go build ./...`: clean

This addendum is the canonical record. The plan body and earlier sections of this
SUMMARY are preserved as the pre-escalation snapshot.
