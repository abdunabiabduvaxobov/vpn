---
phase: 07-admin-panel-overhaul
verified: 2026-06-01T00:00:00Z
status: human_needed
score: 8/8 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Full admin UI walkthrough (Task 4 of Plan 07-10)"
    expected: |
      1. Dashboard KPI bar shows paid_users, MRR, active_connections, signups today/week/month,
         churn_30d, failed_payments_30d and refreshes without a page reload (~60s).
      2. Users > UserDetail: Suspend (reason) → suspended user's next request is 403;
         Unsuspend restores. Force-cancel (refund+reason) → tier resets, audit row with
         reason visible. Force-disconnect → confirm dialog echoes user id; second click
         within 30s shows the 429 toast.
      3. Servers: Drain a server → non-admin GET /servers no longer lists it while admin
         still sees it (is_draining badge). Disconnect-all → confirm dialog echoes hostname.
      4. System > Deps-health tab: postgres/redis/lava green + tunnel server fresh badge.
         Feature flags: toggle maintenance_mode ON → non-admin request gets friendly 503,
         admin panel still reachable. Toggle OFF. Toggle signups_off → /auth/guest 503.
         Create broadcast → GET /api/v1/broadcasts returns it.
      5. Payments: webhook log shows status + redacted emails. Click DELIVERED event →
         payload modal. Replay (with reason) → status flips to REPLAYED, retried_count
         increments, user tier unchanged (no double grant).
    why_human: |
      These are visual, real-time, and cross-surface behaviors (mobile 403, browser UI
      interactions, confirm dialogs, toast timing). They require a live API with migration 024
      applied and at least one tunnel posting heartbeats. Cannot be verified via grep or
      static analysis. Defined as a blocking human-UAT checkpoint in Plan 07-10 Task 4.
  - test: "TestForceCancelWebhookRace (ADMIN-03 serialization proof on real Postgres)"
    expected: |
      After N=20 concurrent iterations of force-cancel vs payment.success on the same user,
      every final state is one of the two consistent outcomes (tier=free, contract inactive
      OR tier=pro, contract active) — never a hybrid.
    why_human: |
      Requires testcontainers Postgres (pg_advisory_xact_lock does not exist in SQLite).
      The test exists, compiles, and skips cleanly under -short (confirmed by go test
      ./integration/ -short). A Docker-backed CI run is needed for full proof (ADMIN-03).
  - test: "TestWebhookReplayIdempotent (ADMIN-06 replay idempotency on real Postgres)"
    expected: |
      Calling applyLavaEvent twice with the same payment.success payload yields a single
      tier grant; subscription_expires_at is identical on the second call; row status
      flips to REPLAYED with retried_count incremented.
    why_human: |
      Requires testcontainers Postgres (applyLavaEvent's success path takes
      pg_advisory_xact_lock via WithUserLock). Test exists, compiles, and skips cleanly
      under -short. A Docker-backed CI run is needed for full proof (ADMIN-06).
---

# Phase 7: Admin Panel Overhaul — Verification Report

**Phase Goal:** Admin panel overhaul — KPI dashboard, per-user controls with advisory locks, server controls, system controls, webhook log + replay, readyz/livez
**Verified:** 2026-06-01
**Status:** human_needed
**Re-verification:** No — initial verification

## Gate Results

| Gate | Command | Result |
|------|---------|--------|
| Backend build | `cd server/api && go build ./...` | PASS (no output = clean) |
| Backend tests -short | `cd server/api && go test ./... -short -count=1` | PASS (17 packages, 0 failures) |
| Frontend build | `cd admin-web && npm run build` | PASS (tsc -b + Vite bundle) |
| Integration tests -short | `cd server/api && go test ./integration/ -short` | SKIP (no Docker — expected, not a failure) |

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | ADMIN-01: GET /admin/stats returns 8 live KPIs (paid_users, mrr, active_connections, signups_today/week/month, churn_30d, failed_payments_30d) plus pre-existing fields | VERIFIED | `repository.GetDashboardKPIs` (admin_repo.go:185), `GetMRR` (admin_repo.go:273), `AdminGetStats` calls `GetDashboardKPIs` (admin.go:564); `cache:admin:mrr:<currency>` 5-min TTL (admin.go:548); `TestAdminGetStatsKPIs` PASS |
| 2 | ADMIN-02: Per-user controls — suspend/unsuspend (sessions revoked + 403 on next request), force-disconnect (throttled ≤1/user/30s → 429), force-cancel, audit/session/connection history | VERIFIED | `AdminSuspendUser/UnsuspendUser/DisconnectUser/CancelSubscription` all exist (admin_user_controls.go); routes wired in main.go:426-432; `TestAdminUserControls` PASS (6 subtests including 429 throttle) |
| 3 | ADMIN-03: Per-user advisory lock (`WithUserLock`) serializes admin force-cancel against lava webhook tier-grant — no hybrid state | VERIFIED (partial — Docker needed for race proof) | `WithUserLock` uses `pg_advisory_xact_lock(hashtextextended(?,0))` (lock.go:45,54); wired in `handleLavaPaymentSuccess` (webhook_lava.go:263), `handleLavaRecurringSuccess` (358), `handleLavaRecurringFailed` (462), `handleLavaSubscriptionCancelled` (507), and `AdminCancelSubscription` (admin_user_controls.go:330); all four webhook contract-mutating paths now take the lock (WR-02, WR-03 fixed). `TestForceCancelWebhookRace` compiles and skips cleanly — Docker CI required for live race proof |
| 4 | ADMIN-04: Server drain (is_draining filter removes server from non-admin GET /servers; cache-busted synchronously), force-disconnect by server_id, per-server health | VERIFIED | `ListActiveServers` adds `AND is_draining = ?` filter (server_repo.go:24); `AdminDrainServer/UndrainServer/DisconnectServer/ServerHealth` exist (admin_server_controls.go); routes wired in main.go:413-416; `TestServerDrainHidesFromPublic`, `TestServerDrainBustsCache`, `TestServerDrainForceDisconnects` all PASS |
| 5 | ADMIN-05: Feature flags (signups_off, payments_off, maintenance_mode) — maintenance 503s non-admins, exempts admin/auth-login/livez/readyz/internal; flags fronted by ~10s Redis cache; broadcast CRUD | VERIFIED | `Maintenance` middleware (maintenance.go:33) mounts at api.Use (main.go:262); exemption list confirmed by `TestMaintenanceMiddleware` (6 subtests including trailing-slash probe path — WR-01 fix confirmed); `GetFlag/BustFlag` (flags_cache.go:41,78); admin flag/broadcast routes (main.go:439-444); `RequireFlagOff` wraps `/auth/guest` + `/checkout` |
| 6 | ADMIN-06: Webhook log + idempotent replay — `applyLavaEvent` extracted, replay endpoint flips status to REPLAYED + bumps retried_count, replay takes same WithUserLock | VERIFIED (partial — Docker needed for idempotency proof) | `applyLavaEvent` (webhook_lava.go:170) called by live handler and replay; `AdminListWebhookEvents/AdminGetWebhookEvent/AdminReplayWebhookEvent` (admin_webhooks.go); `model.LavaWebhookEvent.Status/RetriedCount` (lava_webhook_event.go:28-32); routes wired in main.go:456-458; `TestWebhookReplayIdempotent` skips cleanly — Docker CI required for full idempotency proof |
| 7 | ADMIN-07: GET /livez returns 200 zero-I/O; GET /readyz returns 200 when DB+Redis+lava(cached)+tunnel-freshness all green, else 503 with status-word-only per-dep map | VERIFIED | `Livez()` (health.go:48, zero I/O confirmed), `Readyz(db,redis,lavaClient)` (health.go:67); routes wired in main.go:294-295; `TestReadyzLivez` PASS (4 subtests: livez-200, readyz-200-healthy, readyz-503-redis-down-no-leaked-errors, readyz-503-stale-tunnel); `InternalSecret` constant-time (internal_secret.go:24); `INTERNAL_HEARTBEAT_SECRET` in `RequireEnv` (config.go:272); tunnel `StartHeartbeat` exists (heartbeat.go:34) |
| 8 | ADMIN-08: Admin-only deps-health page shows live status of DB, Redis, lava, per-tunnel-server last_seen_at + freshness | VERIFIED | `AdminDepsHealth` (admin_system.go:250) wired at main.go:450; `ListServerHealth` (server_repo.go:76); reuses cached lava value from `GetLavaReachable` (health_cache.go:28); `TestDepsHealth` PASS (2 subtests: per-dep status + freshness, cached lava read) |

**Score: 8/8 truths verified** (2 carry a Docker-only proof leg routed to human verification; the implementation is complete and all non-Docker verification passes)

### Required Artifacts

| Artifact | Status | Evidence |
|----------|--------|---------|
| `server/api/migrations/024_admin_panel_overhaul.sql` | VERIFIED | Contains users.suspended_at/reason, vpn_servers.is_draining/last_seen_at, lava_webhook_events.status (backfilled), feature_flags (3 seeded flags), broadcast_messages, retried_count |
| `server/api/internal/testutil/pg.go` | VERIFIED | `StartPostgres(t)` at line 40; t.Skip on Short/no-Docker; applies all migrations |
| `server/api/internal/repository/lock.go` | VERIFIED | `WithUserLock` with `pg_advisory_xact_lock(hashtextextended(?,0))` at line 45 |
| `server/api/internal/repository/admin_repo.go` | VERIFIED | `GetDashboardKPIs` (line 185), `GetMRR` (line 273) |
| `server/api/internal/handler/health.go` | VERIFIED | `Livez` (48), `Readyz` (67), `HeartbeatServer` (169) |
| `server/api/internal/middleware/internal_secret.go` | VERIFIED | `InternalSecret` with `subtle.ConstantTimeCompare` at line 24 |
| `server/api/internal/middleware/maintenance.go` | VERIFIED | `Maintenance` (line 33); WR-01 trailing-slash fix confirmed by subtest |
| `server/api/internal/middleware/suspended.go` | VERIFIED | `SuspendedRequired` (line 28) |
| `server/api/internal/handler/admin_user_controls.go` | VERIFIED | Suspend/Unsuspend/Disconnect/CancelSubscription handlers, WithUserLock on cancel |
| `server/api/internal/handler/admin_server_controls.go` | VERIFIED | Drain/Undrain/DisconnectServer/ServerHealth handlers |
| `server/api/internal/handler/admin_system.go` | VERIFIED | `AdminDepsHealth` (line 250); feature-flag + broadcast CRUD handlers |
| `server/api/internal/handler/admin_webhooks.go` | VERIFIED | `AdminListWebhookEvents` (63), `AdminReplayWebhookEvent` (166) |
| `server/api/internal/handler/webhook_lava.go` | VERIFIED | `applyLavaEvent` (170); WithUserLock on all 4 contract-mutating paths |
| `server/api/internal/handler/broadcasts.go` | VERIFIED | Public `ListBroadcastsPublic` handler |
| `server/api/internal/cache/flags_cache.go` | VERIFIED | `GetFlag` (41), `BustFlag` (78), 10s TTL, fail-open |
| `server/api/internal/cache/health_cache.go` | VERIFIED | `GetLavaReachable/SetLavaReachable` (28/46), 60s TTL |
| `server/api/internal/model/user.go` | VERIFIED | `SuspendedAt/SuspendedReason` fields at line 53-54 |
| `server/api/internal/model/server.go` | VERIFIED | `IsDraining/LastSeenAt` fields at line 26-29 |
| `server/api/internal/model/lava_webhook_event.go` | VERIFIED | `Status/RetriedCount` fields at line 28-32 |
| `server/api/internal/model/feature_flag.go` | VERIFIED | Exists |
| `server/api/internal/model/broadcast.go` | VERIFIED | Exists |
| `server/api/internal/repository/server_repo.go` | VERIFIED | `is_draining = false` filter (line 24), `CountFreshServers` (35), `TouchServerHeartbeat` (49), `ListServerHealth` (76) |
| `server/api/internal/repository/feature_flag_repo.go` | VERIFIED | Exists |
| `server/api/internal/repository/broadcast_repo.go` | VERIFIED | Exists |
| `server/api/internal/config/config.go` | VERIFIED | `InternalHeartbeatSecret` + in `RequireEnv` (line 272) |
| `server/tunnel/internal/heartbeat.go` | VERIFIED | `StartHeartbeat` (line 34) — tunnel internal package compiles; cmd/tunnel link failure is pre-existing/unrelated |
| `admin-web/src/api/system.ts` | VERIFIED | `getDepsHealth` (line 35) and all flag/broadcast/broadcast functions |
| `admin-web/src/api/webhooks.ts` | VERIFIED | `replayWebhookEvent` (line 90) |
| `admin-web/src/pages/System.tsx` | VERIFIED | `getDepsHealth` + refetchInterval:15_000 (line 109-111) |
| `admin-web/src/pages/Payments.tsx` | VERIFIED | Replay wiring: 13 references to replayWebhookEvent |
| `admin-web/src/App.tsx` | VERIFIED | `/payments` (line 94), `/system` (line 102) lazy routes |
| `admin-web/src/components/layout/AdminLayout.tsx` | VERIFIED | "Платежи" (line 26), "Система" (line 27) nav items |
| `server/api/integration/admin_concurrency_test.go` | VERIFIED | `TestForceCancelWebhookRace` — compiles, skips cleanly without Docker |
| `server/api/integration/webhook_replay_test.go` | VERIFIED | `TestWebhookReplayIdempotent` — compiles, skips cleanly without Docker |

### Key Link Verification

| From | To | Via | Status |
|------|----|-----|--------|
| `admin.go::AdminGetStats` | `admin_repo.go::GetDashboardKPIs` | Direct call (line 564) | WIRED |
| `webhook_lava.go::handleLavaPaymentSuccess` | `lock.go::WithUserLock` | `repository.WithUserLock(ctx,db,inv.UserID,...)` (line 263) | WIRED |
| `webhook_lava.go::handleLavaRecurringSuccess` | `lock.go::WithUserLock` | Line 358 | WIRED |
| `webhook_lava.go::handleLavaRecurringFailed` | `lock.go::WithUserLock` | Line 462 (WR-03 fix) | WIRED |
| `webhook_lava.go::handleLavaSubscriptionCancelled` | `lock.go::WithUserLock` | Line 507 (WR-02 fix) | WIRED |
| `admin_user_controls.go::AdminCancelSubscription` | `lock.go::WithUserLock` | Line 330 | WIRED |
| `admin_webhooks.go::AdminReplayWebhookEvent` | `webhook_lava.go::applyLavaEvent` | `applyLavaEvent(c.Context(),db,redis,logger,*ev)` (grep confirmed) | WIRED |
| `admin_server_controls.go::AdminDrainServer` | `servers_cache.go::BustServersCache` | Cache-bust after write | WIRED |
| `server_repo.go::ListActiveServers` | drain filter | `WHERE is_active = ? AND is_draining = ?` (line 24) | WIRED |
| `cmd/main.go` | `middleware.Maintenance` | `api.Use(middleware.Maintenance(db, redisClient, logger))` (line 262) | WIRED |
| `cmd/main.go` | `middleware.SuspendedRequired` | `protected := api.Group("", authMiddleware, middleware.SuspendedRequired(db))` (line 349) | WIRED |
| `tunnel/internal/heartbeat.go::StartHeartbeat` | `api/cmd/main.go` (heartbeat route) | `POST /api/v1/internal/servers/:id/heartbeat` with `X-Internal-Secret` | WIRED |
| `admin-web/src/pages/Payments.tsx` | `admin-web/src/api/webhooks.ts::replayWebhookEvent` | `useMutation(replayWebhookEvent)` | WIRED |
| `admin-web/src/App.tsx` | `admin-web/src/pages/System.tsx` | `/system` lazy route (line 102) | WIRED |
| `admin-web/src/pages/System.tsx` | `admin-web/src/api/system.ts::getDepsHealth` | `queryFn: getDepsHealth` (line 110) | WIRED |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|-------------------|--------|
| `admin.go::AdminGetStats` | `stats` map | `repository.GetDashboardKPIs` → raw SQL COUNTs on DB | Yes — parameterized GORM Raw queries | FLOWING |
| `health.go::Readyz` | `deps` map | DB SELECT 1, Redis PING, `cache.GetLavaReachable`, `repository.CountFreshServers` | Yes — real probes with per-dep timeouts | FLOWING |
| `admin_system.go::AdminDepsHealth` | result struct | Same probes as readyz + `repository.ListServerHealth` SELECT | Yes | FLOWING |
| `admin_webhooks.go::AdminListWebhookEvents` | event list | `repository.ListWebhookEvents` DB query | Yes | FLOWING |
| `admin_user_controls.go::AdminSuspendUser` | `suspended_at` | `repository.UpdateUser` + `repository.DeleteUserSessions` + `cache.BustUserCache` | Yes — DB writes flow back to user | FLOWING |
| `admin-web/src/pages/Dashboard.tsx` | KPI cards | `useQuery(getAdminStats)` → `GET /admin/stats` | Yes — live backend | FLOWING |
| `admin-web/src/pages/System.tsx` | deps-health | `useQuery(getDepsHealth, refetchInterval:15_000)` → `GET /admin/system/deps-health` | Yes — live backend | FLOWING |
| `admin-web/src/pages/Payments.tsx` | webhook list | `useQuery(listWebhookEvents)` → `GET /admin/webhook-events` | Yes — live backend | FLOWING |

### Behavioral Spot-Checks

| Behavior | Verification Method | Result |
|----------|-------------------|--------|
| `go build ./...` (api) compiles clean | `cd server/api && go build ./...` | PASS |
| All 17 test packages pass -short | `cd server/api && go test ./... -short -count=1` | PASS (0 failures) |
| `npm run build` (tsc + vite) passes | `cd admin-web && npm run build` | PASS |
| Integration tests skip cleanly without Docker | `go test ./integration/ -short -v` | SKIP (clean, as designed) |
| `TestAdminGetStatsKPIs` (ADMIN-01 unit test) | Direct test run | PASS |
| `TestReadyzLivez` (ADMIN-07, 4 subtests) | Direct test run | PASS |
| `TestAdminUserControls` (ADMIN-02, 6 subtests) | Direct test run | PASS |
| `TestServerDrainHidesFromPublic` (ADMIN-04) | Direct test run | PASS |
| `TestMaintenanceMiddleware` (ADMIN-05, 6 subtests incl. WR-01 trailing-slash) | Direct test run | PASS |
| `TestSuspendedMiddleware` (ADMIN-02, 4 subtests) | Direct test run | PASS |
| `TestDepsHealth` (ADMIN-08, 2 subtests) | Direct test run | PASS |

### Requirements Coverage

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|-------------|-------------|--------|---------|
| ADMIN-01 | 07-02, 07-10 | KPI dashboard (paid_users, MRR, active_connections, signups, churn, failed payments) | SATISFIED | `GetDashboardKPIs` + `GetMRR` + `AdminGetStats` wired; test PASS; Dashboard.tsx wired |
| ADMIN-02 | 07-04, 07-05, 07-10 | Per-user controls (suspend/disconnect/force-cancel + history) | SATISFIED | All handlers exist; SuspendedRequired mounted on protected group; WithUserLock on cancel; tests PASS |
| ADMIN-03 | 07-05 | Advisory lock serializes force-cancel vs webhook tier-grant | SATISFIED (Docker proof pending) | `WithUserLock` wired on all 4 contract-mutating webhook paths + admin cancel; test compiles + skips; Docker CI needed |
| ADMIN-04 | 07-06, 07-10 | Server drain mode + force-disconnect-all | SATISFIED | `is_draining` filter in `ListActiveServers`; drain/undrain bust cache; tests PASS |
| ADMIN-05 | 07-07, 07-10 | Feature flags + maintenance mode + broadcasts | SATISFIED | Maintenance middleware mounted; flags cache 10s; admin CRUD routes; tests PASS |
| ADMIN-06 | 07-08, 07-10 | Webhook log + idempotent replay | SATISFIED (Docker proof pending) | `applyLavaEvent` extracted; replay endpoint flips REPLAYED; test compiles + skips; Docker CI needed |
| ADMIN-07 | 07-03 | GET /livez + /readyz (with tunnel freshness) | SATISFIED | Livez zero-I/O; Readyz 4-dep check; heartbeat endpoint; tunnel emitter; tests PASS |
| ADMIN-08 | 07-09, 07-10 | Admin deps-health page (detailed, admin-only) | SATISFIED | `AdminDepsHealth` with per-server freshness; reuses cached lava; tests PASS |

**All 8 requirements: SATISFIED** (ADMIN-03 and ADMIN-06 carry a Docker-only live concurrency/idempotency demonstration routed to human verification; implementation is complete)

### Code Review Findings (from 07-REVIEW.md)

| Finding | Status | Notes |
|---------|--------|-------|
| WR-01: Maintenance exempt-match without trailing-slash normalization | FIXED | `isMaintenanceExempt` trims trailing slash; regression subtest `on_exempts_probes_with_trailing_slash` PASS |
| WR-02: `handleLavaSubscriptionCancelled` outside WithUserLock | FIXED | Now resolves `contract.UserID` and wraps update in `WithUserLock` (line 507) |
| WR-03: `handleLavaRecurringFailed` outside WithUserLock | FIXED | Wrapped in `WithUserLock(parent.UserID, ...)` (line 462) |
| WR-04: `subscriptions.is_active` flip uses coarse predicate | DEFERRED (accepted risk) | `lava_contract_id = parentID` predicate not narrowed; documented — latent under a future multi-lava-subscription schema; revisit if/when that lands |
| IN-01..IN-06: Info-level findings | DEFERRED | Feature-flag allowlist, comment clarity, rune-vs-byte truncation, list-preview PII scope, pre-existing `/health` `go_version`. Non-launch-blocking per project CLAUDE.md |

### Anti-Patterns Found

No blocking anti-patterns found. The spot-check scan found no TODO/FIXME/placeholder comments in the phase-7 files, no empty handler bodies, no hardcoded empty data states that flow to rendering. The `type:unknown` for webhook payload in `admin-web/src/api/webhooks.ts` is intentional (opaque JSONB rendered via `JSON.stringify`), not a stub.

| Category | Notes |
|----------|-------|
| Blockers | None |
| Warnings | WR-04: coarse `subscriptions` predicate — deferred, latent only under future multi-subscription schema |
| Info | IN-01..IN-06 from code review — all deferred, non-launch-blocking |

### Pre-existing Out-of-scope Item (not a phase gap)

`server/tunnel/cmd/tunnel` link-step failure: `sagernet/sing` vs go1.26.1 toolchain incompatibility. Pre-dates Phase 7 (reproduces on clean base). The tunnel `internal` package (where `heartbeat.go` lives) compiles and vets clean. Tracked in `deferred-items.md`.

### Human Verification Required

#### 1. Full admin UI walkthrough (Plan 07-10 Task 4 — blocking checkpoint)

**Test:** Run `cd admin-web && npm run dev` against an API with migration 024 applied and at least one tunnel posting heartbeats. Walk the six-step UAT defined in Plan 07-10 Task 4:
1. Dashboard KPI bar refresh
2. Per-user: suspend/unsuspend → 403 enforcement; force-cancel; disconnect throttle
3. Servers: drain visibility, disconnect confirm
4. System: deps-health live, maintenance mode toggle (non-admin 503), signups_off, broadcasts
5. Payments: webhook log, payload modal, replay → REPLAYED status, no double grant

**Expected:** All six steps pass without errors.
**Why human:** Visual, real-time, cross-surface behaviors requiring a live backend. Cannot be verified by static analysis.

#### 2. TestForceCancelWebhookRace on Docker-backed CI (ADMIN-03 race proof)

**Test:** `cd server/api && go test ./integration/ -run TestForceCancelWebhookRace -count=1` on a host with Docker.
**Expected:** PASS across N=20 concurrent iterations; no hybrid state (tier≠contract active).
**Why human:** Requires testcontainers Postgres for `pg_advisory_xact_lock`. Implementation is complete; this verifies the live concurrency guarantee.

#### 3. TestWebhookReplayIdempotent on Docker-backed CI (ADMIN-06 idempotency proof)

**Test:** `cd server/api && go test ./integration/ -run TestWebhookReplayIdempotent -count=1` on a host with Docker.
**Expected:** PASS — single tier grant, identical expires_at, status=REPLAYED, retried_count incremented.
**Why human:** Requires testcontainers Postgres. Implementation is complete; this verifies the live idempotency guarantee.

---

## Summary

Phase 7 goal is **structurally achieved** across all 8 ADMIN requirements. The codebase delivers:
- A complete backend implementation (migration 024, all handlers, middleware, repositories, caches, tunnel heartbeat)
- A complete admin-web UI (Dashboard KPI bar, UserDetail controls, Servers drain/disconnect, System flags/broadcasts/deps-health, Payments webhook log + replay)
- Both build gates pass cleanly (`go build ./...` and `npm run build`)
- All non-Docker unit tests pass (17 packages, 0 failures under `-short`)
- All four code-review warnings resolved (WR-01/02/03 fixed; WR-04 accepted risk, documented)

Three items require live execution that cannot be verified statically:
1. The full admin UI UAT walkthrough (human, blocking — Plan 07-10 Task 4)
2. `TestForceCancelWebhookRace` — advisory-lock race proof (Docker CI)
3. `TestWebhookReplayIdempotent` — replay idempotency proof (Docker CI)

The implementation of all three is present and complete. Status is **human_needed** because the UAT checkpoint and Docker-backed integration tests require human execution against a live environment.

---
_Verified: 2026-06-01_
_Verifier: Claude (gsd-verifier)_
