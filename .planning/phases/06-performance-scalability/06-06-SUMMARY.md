---
phase: 06-performance-scalability
plan: 06
subsystem: backend-api
tags: [PERF-07, ctx-propagation, gorm, pgx, dos-mitigation, refactor]
requires:
  - "Waves 1-4 merged (cache wiring c.Locals(\"user\"), per-job scheduler, heartbeat flush, RETURNING-id downgrades) — this plan absorbed those final signatures"
provides:
  - "ctx-first signatures on every exported repository function + db.WithContext(ctx) internally"
  - "end-to-end ctx propagation: client TCP -> Fiber -> GORM -> pgx -> Postgres CancelRequest on disconnect"
  - "time-bounded scheduler cleanup passes (30s per-pass timeout)"
affects:
  - "every repository file, every handler, both middleware ctx readers, the scheduler, the bot, and the createadmin CLI"
tech-stack:
  added: []
  patterns:
    - "ctx context.Context is the FIRST parameter of every exported repo function"
    - "db.WithContext(ctx) once at the top of multi-statement bodies (db = db.WithContext(ctx)); inline on single-statement bodies"
    - "transactional functions: db.WithContext(ctx).Transaction(func(tx){...}) so the tx inherits the statement ctx"
    - "scheduler/bot have no Fiber ctx: scheduler uses context.WithTimeout(context.Background(), 30s) per pass; bot passes its goroutine ctx; CLI uses context.Background()"
key-files:
  modified:
    - server/api/internal/repository/user_repo.go
    - server/api/internal/repository/connection_repo.go
    - server/api/internal/repository/server_repo.go
    - server/api/internal/repository/plan_repo.go
    - server/api/internal/repository/session_repo.go
    - server/api/internal/repository/admin_repo.go
    - server/api/internal/repository/expiry_repo.go
    - server/api/internal/repository/recovery_repo.go
    - server/api/internal/repository/webhook_event_repo.go
    - server/api/internal/repository/subscription_repo.go
    - server/api/internal/repository/device_repo.go
    - server/api/internal/repository/link_code_repo.go
    - server/api/internal/repository/audit_repo.go
    - server/api/internal/repository/invoice_repo.go
    - server/api/internal/handler/auth.go
    - server/api/internal/handler/servers.go
    - server/api/internal/handler/health.go
    - server/api/internal/handler/connection.go
    - server/api/internal/handler/devices.go
    - server/api/internal/handler/payment.go
    - server/api/internal/handler/webhook_lava.go
    - server/api/internal/handler/telegram.go
    - server/api/internal/handler/admin.go
    - server/api/internal/handler/plans_admin.go
    - server/api/internal/handler/plans_public.go
    - server/api/internal/middleware/auth.go
    - server/api/internal/middleware/audit.go
    - server/api/internal/middleware/admin.go
    - server/api/internal/scheduler/scheduler.go
    - server/api/internal/bot/recovery.go
    - server/api/cmd/createadmin/main.go
decisions:
  - "invoice_repo.go was threaded even though omitted from the plan's files_modified list — the Task 1 acceptance criterion (zero exported funcs taking db first) and the module build both require it (deviation Rule 3)"
  - "cmd/createadmin threaded with context.Background() — a CLI call site outside the plan's file list that would otherwise break go build (deviation Rule 3)"
  - "all repository + handler test call sites updated to pass context.Background() so go test ./... compiles (deviation Rule 3 — a stale test call site is a compile error in the test package)"
  - "cache.BustUserCache / cache.FlushHeartbeats kept context.Background() — they are cache (not repository) functions and are deliberate fire-and-forget side effects that must survive request completion"
  - "two direct GORM ops in webhook_lava (handleLavaRecurringFailed tx, handleLavaSubscriptionCancelled update) were given db.WithContext(ctx) for cancellation consistency with the PERF-07 goal, even though they are not repository functions"
metrics:
  duration: ~45m
  completed: 2026-05-30
  tasks: 2
  files: 40
  commits: 2
---

# Phase 6 Plan 6: Thread ctx Through Repositories Summary

Compiler-guided mechanical refactor (research Fork 1) that makes `ctx context.Context` the FIRST parameter of every exported repository function and calls `db.WithContext(ctx)` once inside each body, then updates every call site to pass a real ctx (handlers `c.Context()`, scheduler per-pass `context.WithTimeout(Background, 30s)`, bot `botCtx`, CLI `Background()`). A cancelled ctx now flows GORM -> pgx v5 -> a Postgres `CancelRequest`, so a client disconnect aborts the in-flight query and releases its pool connection — closing the pool-exhaustion DoS surface in audit §4.1 (PERF-07 / D-08).

## What Was Built

### Task 1 — ctx-first repository signatures (commit 20be627)
- 14 repository source files threaded: user, connection, server, plan, session, admin, expiry, recovery, webhook_event, subscription, device, link_code, audit, **invoice**.
- ~107 exported query functions: `func Xxx(ctx context.Context, db *gorm.DB, ...)`, first statement `db.WithContext(ctx)` (or `db = db.WithContext(ctx)` for multi-statement bodies so all statements share the context-bound session).
- 7 transactional functions converted to `db.WithContext(ctx).Transaction(...)`: SetUserPlan, SoftDeletePlan, ReplacePlanServers, ReplaceOffer (plan_repo), ConsumeLinkCode (link_code), PerformRestore (recovery), PromoteGuestToSSO (user).
- Internal fan-out re-threaded: DowngradeExpiredPlans -> FindSystemPlanID; UpdatePlan -> FindPlanByID; UpdatePlanOffer -> findOfferByID.
- No SQL, no return shapes, no behavior changed — pure ctx threading.

### Task 2 — every call site passes a real ctx (commit f4f05d1)
- 11 handler files + 3 middleware readers pass `c.Context()`.
- Helper functions that fan out to repo calls were given a ctx parameter and thread it through (handler: storeRefreshSession, resolveSSOUser, findUserByProviderID, all 5 lava webhook handlers + planIDFromContract; bot: writeAudit, sendStatus). Their callers pass `c.Context()` / `ctx` accordingly.
- Scheduler: each `runCleanup` and `runExpiryDowngrade` pass derives `ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)` (Pitfall 4) so a wedged cleanup query can't hang the 1-min / 10-min ticker.
- Bot: `handleCallback` now uses its goroutine `ctx` (the prior `_ = ctx` discard removed) for PerformRestore + writeAudit; handleLink/handleRestore/sendStatus thread the same ctx.
- CLI `cmd/createadmin` threaded with `context.Background()`.
- All repository + handler test call sites updated to pass `context.Background()` (added `"context"` import to 9 test files) so the suite compiles.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] invoice_repo.go threaded (omitted from plan file list)**
- **Found during:** Task 1
- **Issue:** `invoice_repo.go` has 5 exported `func Xxx(db *gorm.DB, ...)` functions but was not in the plan's `files_modified` list. Task 1's acceptance criterion is `grep "func [A-Z][A-Za-z]*(db \*gorm.DB" returns 0`, and the module would not build with mixed signatures.
- **Fix:** Threaded ctx through all 5 invoice repo functions and their call sites in payment.go / webhook_lava.go.
- **Files modified:** server/api/internal/repository/invoice_repo.go (+ payment.go, webhook_lava.go call sites)
- **Commit:** 20be627 / f4f05d1

**2. [Rule 3 - Blocking] cmd/createadmin threaded**
- **Found during:** Task 2 call-site sweep
- **Issue:** The createadmin CLI calls `repository.FindUserByEmailHash(db, ...)` and `repository.CreateUser(db, ...)` — a call site outside the plan's file list that would break `go build ./...`.
- **Fix:** Added `"context"` import and passed `context.Background()` (no request ctx in a one-shot CLI).
- **Files modified:** server/api/cmd/createadmin/main.go
- **Commit:** f4f05d1

**3. [Rule 3 - Blocking] all test call sites updated**
- **Found during:** Task 2 (acceptance criterion `go test ./... -short`)
- **Issue:** 11 test files called repo functions with the old `db`-first signature; left unchanged they are compile errors in the `repository_test` / `handler` test packages.
- **Fix:** Inserted `context.Background()` as the first arg at every test call site; added `"context"` import to the 9 test files that lacked it.
- **Files modified:** connection_repo_test.go, invoice_repo_test.go, plan_repo_test.go, subscription_repo_test.go, webhook_event_repo_test.go, user_repo_sso_test.go, user_repo_subscription_test.go, expiry_repo_test.go (repository); payment_test.go (handler)
- **Commit:** f4f05d1

**4. [Rule 2 - Correctness] two direct GORM ops in webhook_lava given WithContext(ctx)**
- **Found during:** Task 2
- **Issue:** `handleLavaRecurringFailed`'s `db.Transaction(...)` and `handleLavaSubscriptionCancelled`'s `db.Model(...).Updates(...)` are direct GORM ops (not repository functions) but represent the same DoS-relevant in-flight-query surface PERF-07 targets.
- **Fix:** `db.WithContext(ctx).Transaction(...)` and `db.WithContext(ctx).Model(...)` so a disconnect cancels them too.
- **Files modified:** server/api/internal/handler/webhook_lava.go
- **Commit:** f4f05d1

## Action Needed from the Orchestrator (validation)

The worktree sandbox **denied the `go` toolchain** (`go build` and `go test` both rejected with a permission error). Per the plan's `<validation_note>`, the refactor was made complete by exhaustive grep-based call-site tracing instead, and the orchestrator must run the authoritative post-merge validation:

```
cd server/api && go build ./...
cd server/api && go test ./... -short
cd server/api && go test ./internal/repository/... -run TestCtxCancelAbortsQuery   # D-09(c), needs Docker for testcontainers
```

Grep-based completeness evidence (all returned ZERO hits at hand-off):
- `grep -rn "func [A-Z][A-Za-z]*(db \*gorm.DB" server/api/internal/repository/` -> 0 (no exported func takes db first)
- `grep -rn "repository\.[A-Z][A-Za-z]*(db\b|...(tx\b|...(r\.db\b|...(testDB\b" server/ --include="*.go"` (production + tests + CLI) -> 0
- `grep -rn "repository\.[A-Za-z]*(nil," server/api/internal/` -> 0 (no nil ctx)
- `grep -rn "db.WithContext(ctx)" server/api/internal/repository/*.go` -> hits across all 14 repo files
- `grep -n "WithContext(ctx).Transaction" .../repository/*.go` -> SetUserPlan, SoftDeletePlan, ReplacePlanServers, ReplaceOffer, ConsumeLinkCode, PerformRestore, PromoteGuestToSSO
- handlers pass `c.Context()` -> multiple hits in all 11 handler files
- scheduler -> `context.WithTimeout(context.Background(), 30*time.Second)` in runCleanup + runExpiryDowngrade

## Verification Status

- [x] Every exported repo function takes ctx first + calls db.WithContext(ctx) (grep-verified)
- [x] Transactions use db.WithContext(ctx).Transaction (grep-verified)
- [x] Every call site (handlers/middleware/scheduler/bot/CLI/tests) passes a real ctx; no nil (grep-verified)
- [x] Scheduler/bot pass Background-timeout / botCtx, never a Fiber ctx they don't have
- [ ] `go build ./...` green — DEFERRED to orchestrator (toolchain denied in sandbox)
- [ ] `go test ./... -short` green — DEFERRED to orchestrator
- [ ] `TestCtxCancelAbortsQuery` green — DEFERRED to orchestrator (needs Docker)

## Known Stubs

None. This is a pure mechanical refactor — no UI, no data-source wiring, no placeholders introduced.

## Self-Check: PASSED

- FOUND: .planning/phases/06-performance-scalability/06-06-SUMMARY.md
- FOUND: 20be627 (Task 1 — ctx-first repo signatures)
- FOUND: f4f05d1 (Task 2 — call-site threading)
- Sample verify: invoice_repo.go contains 5 `ctx context.Context` parameters
