---
phase: 01-hotfix-audit-critical-fixes
plan: 07
subsystem: server/api/repository
tags: [hotfix, test-only, regression-guard, subscription-downgrade, scheduler, audit-crit-01]
dependency_graph:
  requires: [01-hotfix-audit-critical-fixes/06]
  provides: [regression-guard-downgrade-scheduler]
  affects: [server/api/internal/repository/user_repo_subscription_test.go]
tech_stack:
  added: []
  patterns:
    - "in-memory sqlite with user-registered NOW() function (mattn/go-sqlite3 ConnectHook + RegisterFunc) so production Postgres-flavored SQL runs verbatim in unit tests"
key_files:
  created:
    - server/api/internal/repository/user_repo_subscription_test.go
  modified: []
decisions:
  - "Test-only commit — the production code path (column + model field + repo read + scheduler call) is already correct on main; the WRITE half (webhook populates subscription_expires_at) is deferred to Phase 3 per D-07."
  - "Register a sqlite NOW() user-function returning the current time in GORM's sqlite ISO-8601 layout, so the production query `subscription_expires_at < NOW()` runs verbatim against sqlite — zero production code changed."
  - "Three tests cover the three branches of the WHERE clause: past-due Pro user (downgraded, expires_at preserved as audit trail), future-dated Pro user (left alone), NULL expires_at admin-comped user (left alone)."
  - "ZERO production code touched. payment.go is byte-identical to the phase base (f43b2be) across ALL 8 hotfix commits — D-07 invariant enforced PHASE-WIDE."
metrics:
  duration_seconds: 360
  tasks_completed: 2
  files_modified: 1
  commit_hash: 15073e4
  completed_date: 2026-05-22
requirements: [HOTFIX-01]
threat_refs: [T-1-01]
---

# Phase 1 Plan 7: HOTFIX-01 — Regression test for subscription downgrade Summary

Three sqlite-based regression tests guarding the existing scheduler path that downgrades expired Pro users to free. Production code is intentionally unchanged — the column + model field + repository read + scheduler wire are already correct on main; the webhook WRITE that populates `subscription_expires_at` is deferred to Phase 3 (lava.top) per CONTEXT.md D-07.

## What Changed

### `server/api/internal/repository/user_repo_subscription_test.go` (NEW, 241 lines)

Three regression tests around `repository.DowngradeExpiredSubscriptions`:

| Test | Setup | Expect |
|------|-------|--------|
| `TestDowngradeExpiredSubscriptions_DowngradesPastDueProUser` | Pro user, `expires_at = NOW() - 1h` | `count == 1`, tier becomes `"free"`, **expires_at preserved** as audit trail |
| `TestDowngradeExpiredSubscriptions_LeavesFutureProUserAlone` | Pro user, `expires_at = NOW() + 24h` | `count == 0`, tier stays `"pro"` |
| `TestDowngradeExpiredSubscriptions_IgnoresNullExpiresAt` | Pro user, `expires_at = NULL` (admin-comped) | `count == 0`, tier stays `"pro"`, expires_at stays NULL |

### Test-side sqlite shim

The production query embeds `NOW()` as a SQL literal:

```go
db.Where("subscription_tier <> ? AND subscription_expires_at IS NOT NULL AND subscription_expires_at < NOW()", "free").
   Update("subscription_tier", "free")
```

`NOW()` is Postgres-flavored — sqlite does not implement it. To exercise the production query verbatim without modifying the production signature, the test registers a custom sqlite3 driver (`sqlite3_with_now`) once via `sync.Once`:

```go
sql.Register(sqliteWithNowDriverName, &sqlite3.SQLiteDriver{
    ConnectHook: func(conn *sqlite3.SQLiteConn) error {
        now := func() string {
            return time.Now().Format("2006-01-02 15:04:05.999999999-07:00")
        }
        return conn.RegisterFunc("NOW", now, true)
    },
})
```

GORM's sqlite driver writes `time.Time` columns using the same ISO-8601 layout, so the lexicographic string comparison done by sqlite is equivalent to the chronological comparison Postgres performs at runtime.

## ZERO production code modified

This commit changes exactly ONE file: a new test file. No production `.go` file, no migration, no handler is touched.

```
$ git diff HEAD~1 HEAD --stat
 .../repository/user_repo_subscription_test.go      | 241 +++++++++++++++++++++
 1 file changed, 241 insertions(+)
```

## Verification

### Test results

```
$ cd server/api && go test ./internal/repository/... -v -count=1 -run TestDowngradeExpiredSubscriptions_
=== RUN   TestDowngradeExpiredSubscriptions_DowngradesPastDueProUser
--- PASS: TestDowngradeExpiredSubscriptions_DowngradesPastDueProUser (0.00s)
=== RUN   TestDowngradeExpiredSubscriptions_LeavesFutureProUserAlone
--- PASS: TestDowngradeExpiredSubscriptions_LeavesFutureProUserAlone (0.00s)
=== RUN   TestDowngradeExpiredSubscriptions_IgnoresNullExpiresAt
--- PASS: TestDowngradeExpiredSubscriptions_IgnoresNullExpiresAt (0.00s)
PASS
ok      vpnapp/server/api/internal/repository   0.745s
```

### Full repository suite — no regression

```
$ cd server/api && go test ./internal/repository/... -count=1
ok      vpnapp/server/api/internal/repository   0.373s
```

### D-07 invariant — payment.go byte-identical to phase base PHASE-WIDE

```
$ BASE=f43b2bee84946ed745b87d85635ce2226f984673   # phase base (= merge-base with main before any 01-* commit)
$ git diff $BASE..HEAD -- server/api/internal/handler/payment.go | wc -l
0
$ git diff $BASE..HEAD -- server/api/migrations/ | wc -l
0
```

`handler/payment.go` is unchanged from the phase base across ALL 8 hotfix commits (01-01..01-07 so far), and ZERO migrations have been added by HOTFIX-01 (the column was already in `001_initial.sql:12`).

### Smoke greps — existing scheduler path intact

```
$ grep -nE 'subscription_expires_at\s+TIMESTAMPTZ' server/api/migrations/001_initial.sql
12:    subscription_expires_at TIMESTAMPTZ,

$ grep -n 'DowngradeExpiredSubscriptions' server/api/internal/scheduler/scheduler.go
124:    expiredCount, err := repository.DowngradeExpiredSubscriptions(db)

$ grep -n 'DowngradeExpiredSubscriptions' server/api/internal/repository/user_repo.go
270:// DowngradeExpiredSubscriptions finds every user whose paid subscription
277:func DowngradeExpiredSubscriptions(db *gorm.DB) (int64, error)
```

The column is still in the initial schema, the scheduler still calls the function every minute, and the repository function signature is unchanged.

### Commit shape

```
$ git log -1 --format=%s
hotfix(01): regression test for subscription downgrade (column+scheduler already correct) [HOTFIX-01]

$ git diff HEAD~1 HEAD --name-only
server/api/internal/repository/user_repo_subscription_test.go
```

Subject matches the required regex `^hotfix\(01\): .*HOTFIX-01` and the commit changes exactly one file.

## Why this commit is test-only (reviewer FAQ)

> "Where's the fix? CRIT-01 says subscription never expires."

The fix is split across phases by design (CONTEXT.md D-07):

| Half | Status | Phase | File |
|------|--------|-------|------|
| READ — scheduler downgrades when `expires_at < NOW()` | already correct on main | (pre-existing) | `repository/user_repo.go:277-288`, `scheduler/scheduler.go:124` |
| WRITE — webhook populates `expires_at` on successful renewal | deferred | Phase 3 (lava.top webhook) | new file in Phase 3 |

The existing Stripe webhook at `handler/payment.go:271-294` is being DELETED entirely in Phase 8 (HARD-01) and has zero paying Stripe users today. Patching it now would be wasted work — Phase 3's lava.top webhook will write to the same column on every renewal.

These three regression tests guard the READ half while the WRITE half is built, so a future refactor that accidentally removes the column, breaks the WHERE clause, or unwires the scheduler will fail CI before it ships.

## Threat mitigation

| Threat | Mitigation in this commit |
|--------|--------------------------|
| T-1-01 (Repudiation — subscription never expires) | Regression tests over `DowngradeExpiredSubscriptions` cover the three branches of the WHERE clause (past-due → downgrade; future → skip; NULL → skip). A future refactor that breaks the scheduler read will fail CI. The WRITE half is deferred to Phase 3 per D-07. |

## Deviations from Plan

None — plan executed exactly as written. Task 1 (test stub with three SKIP functions) was written and verified, then Task 2 replaced the SKIP bodies with real implementations and committed atomically (per plan: "Do NOT commit. Task 2 commits the full diff atomically").

The plan flagged sqlite vs Postgres `NOW()` semantics as a known gotcha and proposed two fallback options. The implementation chose option (a) — register `NOW()` as a user-defined sqlite function via `mattn/go-sqlite3.ConnectHook` + `RegisterFunc`, returning a string in GORM-sqlite's ISO-8601 layout. This lets the production query run verbatim against sqlite without changing the production signature.

## Self-Check: PASSED

- File `server/api/internal/repository/user_repo_subscription_test.go` exists.
- Commit `15073e4` exists on `main` (`git log --oneline -3` shows it as HEAD).
- All three `TestDowngradeExpiredSubscriptions_*` tests PASS.
- D-07 invariant verified: `git diff f43b2be..HEAD -- server/api/internal/handler/payment.go | wc -l == 0`.
- D-07 invariant verified: `git diff f43b2be..HEAD -- server/api/migrations/ | wc -l == 0`.
- Full repository test suite green (no regression on existing subscription_repo_test, connection_repo_test, etc.).
