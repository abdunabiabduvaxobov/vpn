---
phase: 3
plan: 09
subsystem: backend/scheduler+repository
tags: [scheduler, cron, expiry, downgrade, lava, plans, D-19, D-26, PAY-09, ADR-19.10]
dependency-graph:
  requires:
    - 03-01 (migrations-models-stripe-cleanup) — Plan / Subscription / User models with PlanID
    - 03-03 (plan-repo) — FindSystemPlanID
    - 03-06 (webhook-handler-ip-allowlist) — D-19 LITERAL contract (subscriptions.is_active=false on recurring.failed)
  provides:
    - repository.DowngradeExpiredPlans(db) (int64, error) — idempotent SQL plan-downgrade
    - scheduler.runExpiryDowngrade — every ~10 minutes (every 10th 1-minute tick)
    - scheduler.scheduler.expiryTickCount field
  affects:
    - 03-11 (docs-sandbox-smoke) — end-to-end smoke includes the lapse → cron → downgrade path
    - Phase 6 PERF-06 — RUN_SCHEDULER env gate will eventually wrap this for multi-replica
    - Phase 7 ADMIN-06 — admin UI showing "downgraded N users" log line data source
tech-stack:
  added: []
  patterns:
    - "Two-step driver-agnostic flip: Pluck userIDs via JOIN then UPDATE by IN-list (SQLite-test compatible; Postgres-portable)"
    - "Tick-counter modulo gating: increment per ticker fire, run gated job on N==0 (avoids a second time.Ticker and second goroutine)"
    - "Idempotent UPDATE: WHERE u.plan_id != system_plan_id ensures second-run is a no-op without explicit dedup state"
    - "D-19 cross-plan coordination: WHERE clause INTENTIONALLY omits s.is_active=TRUE (this comment is load-bearing — webhook flipped is_active before expires_at lapses)"
key-files:
  created:
    - server/api/internal/repository/expiry_repo.go (78 lines, 1 exported func)
    - server/api/internal/repository/expiry_repo_test.go (118 lines, 3 tests)
  modified:
    - server/api/internal/scheduler/scheduler.go (+27 / -3 lines — expiryTickCount field, every-10-tick gate, runExpiryDowngrade helper)
decisions:
  - "Tick-counter modulo gating over a second time.Ticker — keeps the goroutine and Stop() semantics single, costs one int field, fires within ±60s of the desired 10-min cadence which is well within D-26 tolerance."
  - "Two-step Pluck→Updates query over a single UPDATE…FROM — Postgres supports the latter natively but SQLite (used in all repo tests) does not have correlated UPDATE FROM semantics; the two-step approach lets the same code path run in unit tests and production without driver branching."
  - "Rule 1 deviation: T01 verbatim test for the BLOCKER #1 case used `IsActive: false` in db.Create which GORM omits per the documented 03-03 SUMMARY deviation #2 trap. Switched to insert-with-IsActive=true then explicit Update so the row actually lands with is_active=0 (matches the production state after handleLavaRecurringFailed's db.Updates map literal — see 03-06 SUMMARY deviation #1)."
metrics:
  duration_seconds: 228
  duration_human: "~4 minutes"
  tasks_total: 2
  tasks_complete: 2
  commits: 2
  files_created: 2
  files_modified: 1
  completed_date: "2026-05-23"
  completed_at: "2026-05-23T20:29:48Z"
  tests_added: 3
  tests_passing: 3
---

# Phase 3 Plan 09: expiry-cron Summary

**One-liner:** Landed the D-26 / ADR §19.10 cron — `repository.DowngradeExpiredPlans` flips `users.plan_id` back to the system plan + `subscription_tier='free'` when `subscriptions.expires_at < now()` regardless of `is_active` state (D-19 literal coordination with plan 03-06), wired into the existing 1-minute scheduler loop via a tick counter so the SQL runs every ~10 minutes, with three SQLite-backed tests proving (a) the happy-path lapse-flip is correct and idempotent, (b) recurring-failed users (`subscriptions.is_active=false` + lapsed `expires_at`) still get downgraded — BLOCKER #1 closed end-to-end, and (c) the cron is a safe no-op on an empty/cold DB.

## What Shipped

### Task 03-09-T01 — `internal/repository/expiry_repo.go` + tests (commit `1b3a9fb`)

`DowngradeExpiredPlans(db *gorm.DB) (int64, error)`:

- Resolves `system_plan_id` once via `repository.FindSystemPlanID(db)` (returns its error if no system plan seeded — fail-safe).
- Two-step driver-agnostic flip:
  1. `Pluck("users.id", &userIDs)` from a `users JOIN subscriptions` query filtered on `users.plan_id != system_plan_id AND s.expires_at IS NOT NULL AND s.expires_at < ?`.
  2. `Updates(map[string]interface{}{"plan_id": system_plan_id, "subscription_tier": "free"})` against the plucked IDs.
- Returns `result.RowsAffected` for logging.
- **WHERE clause intentionally OMITS `s.is_active=TRUE`** — D-19 BLOCKER #1 coordination. A user whose recurring payment failed has `subscriptions.is_active=false` (flipped in plan 03-06 T03's `handleLavaRecurringFailed` per the D-19 literal reading) AND `expires_at` in the past. The cron MUST find them — filtering on `is_active` would leave them on Pro forever. Codified by both the doc comment and the `TestRunExpiryDowngrade_FindsUsersRegardlessOfSubActive` test.
- Idempotent by virtue of `users.plan_id != system_plan_id`: the second run finds no matches because the first run already flipped them.

3 tests:

| Test | Validation |
|------|-----------|
| `TestDowngradeExpiredPlans_FlipsLapsedUsers` | PAY-09 — User A (pro + lapsed) flips to free; User B (pro + future) stays; User C (already free) untouched. Second call returns 0 rows (D-26 idempotency). |
| `TestRunExpiryDowngrade_FindsUsersRegardlessOfSubActive` | BLOCKER #1 D-19 — User D with `subscriptions.is_active=FALSE` + `expires_at` in past STILL gets downgraded. This is the regression test that locks in the coordination contract with plan 03-06. |
| `TestDowngradeExpiredPlans_EmptyTable_NoOp` | D-26 — empty users table returns (0, nil) without error. |

### Task 03-09-T02 — `internal/scheduler/scheduler.go` (commit `b888bef`)

Three surgical edits to the existing scheduler:

1. **`scheduler` struct** gains `expiryTickCount int` field (line 22).
2. **Goroutine loop** (lines 52-64) bumps `s.expiryTickCount++` after each `runCleanup(...)` call, then `if s.expiryTickCount%10 == 0 { runExpiryDowngrade(db, logger) }`. With the existing 1-minute `cleanupInterval`, this fires every 10 minutes — matches D-26 verbatim.
3. **`runExpiryDowngrade(db, logger)` helper** added at the bottom (lines 156-165). Calls `repository.DowngradeExpiredPlans(db)`; logs `Error` on failure (returns; does NOT panic — scheduler keeps running), logs `Info("expired plans downgraded to system plan", count)` on success.

Existing cleanup pipeline (sessions, stale connections, stale devices, link codes, **HOTFIX-01 `DowngradeExpiredSubscriptions`** which downgrades only `users.subscription_tier`) is **unchanged** — this plan adds the plans-catalog `users.plan_id` flip as an additional, gated-every-10-min step on top. The two downgrade helpers operate on different columns and converge correctly: HOTFIX-01 sets tier='free' every minute (worst-case 60s drift); this plan additionally moves `plan_id` to system every 10 minutes (worst-case 10-minute drift on plan_id reads). Mobile/landing surfaces read `plan_id` via JWT claims (refreshed every 5 min) so the practical observed drift is bounded by token lifetime regardless.

## Verification

**Plan-level success criteria (all 5):**

| # | Criterion | Result |
|---|---|---|
| 1 | `cd server/api && go build ./...` exits 0 | **PASS** |
| 2 | `cd server/api && go test ./... -count=1 -timeout=300s` exits 0 | **PASS** (all 11 packages green, total ≈48s) |
| 3 | `TestDowngradeExpiredPlans_FlipsLapsedUsers` + `TestRunExpiryDowngrade_FindsUsersRegardlessOfSubActive` pass (PAY-09 + BLOCKER #1 D-19) | **PASS** |
| 4 | Scheduler tick-counter gates downgrade to every ~10 minutes | **PASS** (`grep expiryTickCount%10 scheduler.go` → 1 hit at line 58) |
| 5 | Existing scheduler pipeline (HOTFIX-01 + others) intact | **PASS** (all 4 existing scheduler tests still pass — `TestScheduler_StopBeforeStart_IsNoop`, `_StartStop_DoesNotPanic`, `_CleansExpiredSessionsOnStart`, `_StartTwice_IsNoop`) |

**Per-task acceptance grep results:**

```
T01:
  grep -c "FindSystemPlanID"                expiry_repo.go        → 1  (used in line 50)
  grep -c "expires_at < ?"                  expiry_repo.go        → 1  (line 62)
  awk '/Where\(/,/\)/' expiry_repo.go | grep -c "is_active"        → 0  (BLOCKER #1 fix verified inside the GORM Where call)
  grep -c "Coordinated with plan 03-06"     expiry_repo.go        → 1  (line 36 — cross-link comment)
  grep -c "TestDowngradeExpiredPlans_FlipsLapsedUsers"  test       → 1
  grep -c "TestRunExpiryDowngrade_FindsUsersRegardlessOfSubActive" test → 1
  grep -c "second call must return 0"       test                   → 1  (idempotency)
  go test -run "TestDowngradeExpiredPlans|TestRunExpiryDowngrade"  → ok 0.487s (3 tests PASS)

T02:
  grep -c "expiryTickCount"                 scheduler.go          → 3  (decl + increment + modulo — exceeds floor of 2)
  grep -c "runExpiryDowngrade"              scheduler.go          → 3  (comment header + call site + func decl — exceeds floor of 2)
  grep -c "expiryTickCount%10"              scheduler.go          → 1  (line 58)
  grep -c "DowngradeExpiredPlans"           scheduler.go          → 1  (line 157)
  go build ./...                                                  → exit 0
  go test ./internal/scheduler/... -count=1                       → ok 0.633s (4 tests PASS)

Final plan-verification negatives:
  awk '/Where\(/,/\)/' expiry_repo.go | grep -c "is_active"        → 0  ✓ (cron does NOT filter on is_active)
```

**Full test suite:** `cd server/api && go test ./... -count=1 -timeout=300s` — all packages PASS:

```
ok   vpnapp/server/api/cmd/createadmin                5.015s
ok   vpnapp/server/api/internal/auth/apple            2.958s
ok   vpnapp/server/api/internal/auth/google           0.719s
ok   vpnapp/server/api/internal/cache                 9.724s
ok   vpnapp/server/api/internal/config                2.637s
ok   vpnapp/server/api/internal/handler               4.514s
ok   vpnapp/server/api/internal/lava                  2.296s
ok   vpnapp/server/api/internal/middleware            5.920s
ok   vpnapp/server/api/internal/recovery              3.309s
ok   vpnapp/server/api/internal/repository            2.712s
ok   vpnapp/server/api/internal/scheduler             2.859s
ok   vpnapp/server/api/migrations                     5.104s
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Test infrastructure bug] BLOCKER #1 regression test rewritten to use insert-then-Update for `is_active=false`**

- **Found during:** T01 — the plan's verbatim test body for `TestRunExpiryDowngrade_FindsUsersRegardlessOfSubActive` calls `db.Create(&model.Subscription{... IsActive: false, ExpiresAt: &yesterday})`. The 03-03 SUMMARY deviation #2 and 03-06 SUMMARY deviation #1 both call out the same trap: GORM's struct-based `Create` OMITS Go zero-value bool fields from the INSERT statement, and the SQLite test DDL has `is_active INTEGER NOT NULL DEFAULT 1`. So the verbatim test would store the row with `is_active=1` and (a) the assertion would still pass for the wrong reason on the current implementation (because the WHERE clause doesn't filter on is_active anyway, so a phantom is_active=1 row matches just like an actual is_active=0 row would), but (b) it wouldn't actually be a regression test for the BLOCKER #1 fix — a future change reintroducing `s.is_active = TRUE` to the WHERE clause would NOT be caught.
- **Fix:** Insert the row with `IsActive: true` first, then issue an explicit `db.Model(...).Where("id = ?", subID).Update("is_active", false)` — a map-key-explicit Update bypasses the zero-value omission. The row now genuinely has `is_active=0` in the table, and the test would correctly fail if the WHERE clause regained the `is_active=TRUE` filter.
- **Why this matters:** Without this fix, the BLOCKER #1 regression test is a placebo. The 03-06 production code uses `db.Updates(map[string]interface{}{"is_active": false, ...})` in `handleLavaRecurringFailed` for exactly this reason; the test now reproduces that real production state. Cross-plan consistency between webhook (03-06) + repo test (03-09) is now intact.
- **Files modified:** `server/api/internal/repository/expiry_repo_test.go` (only — T01 commit)
- **Commit:** `1b3a9fb`

### Deferred Issues

None — all in-scope work landed clean.

Downstream deferrals carried forward (already noted in 03-06 SUMMARY, unchanged by this plan):

- **`RUN_SCHEDULER` env gate** for multi-replica deployments — owned by **Phase 6 PERF-06**. The current single-replica v2.2.0 deployment runs the scheduler in the only API replica.
- **Admin UI for cron metrics** — owned by **Phase 7 ADMIN-06**. The `logger.Info("expired plans downgraded to system plan", zap.Int64("count", N))` log line is the data source.
- **Coordinated test asserting both `is_active=false` AND `plan_id` flip across web webhook + cron transitions** (end-to-end) — owned by **Plan 03-11 (docs-sandbox-smoke)** for the integration-level smoke. Repo-level coordination is proven in `TestRunExpiryDowngrade_FindsUsersRegardlessOfSubActive`.

## Threat Model Compliance

All 4 STRIDE entries in the plan's `<threat_model>` map to in-code mitigations:

| Threat ID | Mitigation in code |
|-----------|--------------------|
| T-03-70 (DoS: scheduler floods DB) | **Accepted per plan** — every 10 minutes, one Pluck + at most one UPDATE bounded by O(lapsed_users). At single-VM scale (current deployment) this is trivial. |
| T-03-71 (Tampering: downgrades paying user mid-period) | WHERE clause requires `s.expires_at IS NOT NULL AND s.expires_at < now()`. A paying user's `expires_at` was extended into the future by `handleLavaPaymentSuccess` / `handleLavaRecurringSuccess` (plan 03-06). They are never matched. Mid-cancellation users still inside their paid period also have `expires_at` in the future. Verified by `TestDowngradeExpiredPlans_FlipsLapsedUsers` (User B: pro + future expires_at, unchanged). |
| T-03-72 (Repudiation: user claims wrong downgrade) | `runExpiryDowngrade` logs `Info("expired plans downgraded to system plan", count=N)` on every non-zero run; `Error("expiry downgrade failed", err)` on failure. Logs are the forensic evidence; Phase 7 ADMIN-06 surfaces them in the UI. |
| T-03-73 (Tampering: multi-replica race) | **Accepted per plan** — single-replica v2.2.0. Phase 6 introduces `RUN_SCHEDULER` env gate. Even today, the operation is idempotent (`u.plan_id != system_plan_id` filter); concurrent UPDATEs converge on the same final state. |

ASVS L1 scoped (background job, no external surface). No L2 controls needed.

## Threat Flags

None. This plan introduces:

- **0 new HTTP endpoints** (scheduler is in-process)
- **0 new outbound network calls** (pure DB read+write)
- **0 new auth paths**
- **0 new schema** (uses existing `users`, `subscriptions`, `plans` tables from migration 019, already threat-modeled in 03-01)
- **0 new trust boundaries**

The single new function (`DowngradeExpiredPlans`) operates on tables already trust-boundaried by migration 019. The single new scheduler helper (`runExpiryDowngrade`) delegates to that function.

## Known Stubs

None. The `runExpiryDowngrade` helper has real implementation; `DowngradeExpiredPlans` has real SQL with real WHERE clauses and real RowsAffected accounting. No hardcoded empty arrays, no placeholder strings, no TODO/FIXME markers.

## Commits

| Task | Hash | Type | Message |
|------|------|------|---------|
| T01 | `1b3a9fb` | feat | add DowngradeExpiredPlans idempotent ADR §19.10 SQL |
| T02 | `b888bef` | feat | wire runExpiryDowngrade into scheduler every ~10 minutes |

## Downstream Consumers

- **Plan 03-11 (docs-sandbox-smoke)** will validate the full lapse → cron → downgrade path end-to-end against the lava sandbox (sandbox subscription that expires mid-smoke; cron forces tier=free; mobile next-foreground reflects downgrade).
- **Phase 6 PERF-06** will introduce the `RUN_SCHEDULER` env gate around `scheduler.Start(...)` so only one replica runs the background loop in a multi-replica deployment.
- **Phase 7 ADMIN-06** will surface the `expired plans downgraded to system plan` log line as a webhook/scheduler admin observable in the admin web UI.

## Self-Check: PASSED

Files exist:
- FOUND: server/api/internal/repository/expiry_repo.go
- FOUND: server/api/internal/repository/expiry_repo_test.go
- FOUND: server/api/internal/scheduler/scheduler.go (modified)
- FOUND: .planning/phases/03-lava-top-plans-catalog/03-09-expiry-cron-SUMMARY.md (this file)

Commits exist (verified via `git log --oneline 5768583..HEAD`):
- FOUND: 1b3a9fb (T01 expiry_repo.go + tests)
- FOUND: b888bef (T02 scheduler.go modification)

Verification:
- `cd server/api && go build ./...` → exit 0 — PASS
- `cd server/api && go test ./... -count=1 -timeout=300s` → all packages PASS (no regressions in any package)
- `cd server/api && go test ./internal/repository/ -run "TestDowngradeExpiredPlans|TestRunExpiryDowngrade" -count=1 -v` → 3 tests PASS (0.487s)
- `cd server/api && go test ./internal/scheduler/... -count=1 -v` → 4 tests PASS (0.633s)
- BLOCKER #1 negative grep: `awk '/Where\(/,/\)/' expiry_repo.go | grep -c "is_active"` → 0 — PASS
- All 5 plan-level success criteria — PASS
