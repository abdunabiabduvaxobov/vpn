---
status: complete
phase: 07-admin-panel-overhaul
source: [07-VERIFICATION.md, 07-10-PLAN.md]
started: 2026-06-01T00:00:00Z
updated: 2026-06-02T01:30:00Z
---

## Current Test

[testing complete]

## Tests

### 1. Admin UI walkthrough (Plan 07-10 Task 4 — full Phase-7 UAT)
expected: Full admin panel walkthrough against a live API + tunnel heartbeats.
result: pass
verified_by: |
  Stood up a LOCAL stack (Postgres16+Redis7 via infra compose, migration 001..024
  applied fresh, API via `go run`, admin-web `npm run dev` proxied to localhost:3000,
  heartbeat keepalive loop, admin user + 2 test users + 2 webhook events seeded).
  Production was NOT touched (the dev proxy normally points at prod; repointed locally
  and reverted after).

  CHROME (visual, screenshots captured):
  - Login page renders; admin-login succeeds → /dashboard.
  - Dashboard KPI bar shows ALL expected tiles live from the API: Активные подключения,
    Всего пользователей (3), Активные подписки (1), Активные серверы (6), Всего серверов,
    Платящие, MRR (USD), Регистрации сегодня/неделя/месяц, Отток 30d, Сбои оплат 30d +
    "Последние 30 дней" chart.
  - Users list renders live (Test Pro User correctly shows Бесплатный after the API
    force-cancel below).
  - User detail renders: device limit, subscription actions, access controls.
  - Force-disconnect confirm dialog echoes `user_id: 11111111-…` in a highlighted box,
    has a reason field ("записывается в аудит"), and warns a repeat within 30s → 429.

  API-LEVEL (deterministic proof of every assertion; user token minted via known JWT_SECRET):
  - Suspend → user GET /account 200→403; Unsuspend(+reason) → 200. Audited
    (suspend_user / unsuspend_user). Unsuspend requires a reason by design (400 without).
  - Force-cancel Pro (reason+refund) → tier=free, lava_contract is_active=false, audit
    row cancel_subscription carries the reason. (Needs an active contract; 409 otherwise — correct.)
  - Force-disconnect (with reason) → #1 200, #2 within window → 429 (Lua INCR guard).
  - Drain → non-admin GET /servers no longer lists the server; admin GET /admin/servers
    still returns it. Undrain restores.
  - deps-health → postgres ok, redis ok, lava unknown (sandbox; expected), tunnel server
    reports fresh:true while the heartbeat loop runs.
  - maintenance_mode ON → non-admin /servers 503 while /admin/stats still 200; OFF → 200.
  - signups_off ON → POST /auth/guest 503; OFF restores.
  - Broadcast create → public GET /api/v1/broadcasts returns it.
  - Webhook-events list does not leak the buyer email. Live replay of an ad-hoc event
    500s only because it has no backing invoice ("no invoice for contractId") — the
    happy-path idempotent replay is proven by Test 3 below.
note: |
  Visual screenshots cover login/dashboard/users/user-detail/confirm-dialog. The
  Servers/System/Платежи PAGES were not screenshotted because the Chrome extension
  disconnected mid-walkthrough; their behaviors are fully proven at the API level above.

### 2. ADMIN-03 advisory-lock race proof (Docker required)
expected: `go test ./integration/ -run TestForceCancelWebhookRace -count=1` PASSES.
result: pass
note: |
  PASSES now — but only after fixing a STALE TEST FIXTURE. As committed, the test
  never ran: its seed INSERTed `plans` rows missing the NOT NULL columns max_devices /
  max_servers (added in migration 019), then collided with 019's own seeded free/pro
  plans (plans.code UNIQUE). Fixed the seed to look up the migration-seeded plan IDs.
  After that, the WithUserLock serialization proof passes across all 20 interleavings.

### 3. ADMIN-06 webhook replay idempotency proof (Docker required)
expected: `go test ./integration/ -run TestWebhookReplayIdempotent -count=1` PASSES.
result: pass
note: |
  PASSES now — after the same plans-fixture fix PLUS two more fixture bugs: (a) the
  test re-inserted a MONTHLY/USD pro plan_offer that collides with 019's seeded offer
  (idx_plan_offers_unique_active) — changed to UPDATE the seeded offer's lava_offer_id
  (what an admin does in the UI); (b) the acting admin_id was a random UUID never
  inserted into users, so the audit insert silently failed its FK (audit_log.admin_id
  → users.id) and the audit assertion saw 0 rows — now seed the admin user first.
  After that the replay proof passes: set-not-increment grant (no double-extend),
  status DELIVERED→REPLAYED, retried_count==1, audited.

## Summary

total: 3
passed: 3
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

# These are NOT Phase-7 deliverable failures — Phase 7's 3 UAT items all pass.
# They are pre-existing defects SURFACED while standing up a fresh stack for UAT.

- truth: "A new guest can onboard (POST /auth/guest succeeds)"
  status: failed
  reason: >
    CRITICAL / launch-blocker. On any DB where migration 019 has run (users.plan_id
    UUID NOT NULL, no default), POST /auth/guest returns 500. GuestLogin (auth.go ~91)
    calls CreateUser BEFORE the D-29 post-insert UPDATE that sets the system plan_id;
    the model field `PlanID string gorm:"...;not null"` has NO `default` tag, so GORM
    sends plan_id='' in the INSERT and Postgres rejects '' for a uuid column
    ("invalid input syntax for type uuid"). The post-insert UPDATE is dead code. This
    is the primary free-tier onboarding path. It almost certainly passes unit/CI tests
    because handler tests use SQLite (loosely typed, accepts '').
  severity: blocker
  test: 1
  root_cause: "users.plan_id is NOT NULL with no default; GuestLogin/CreateUser insert plan_id='' before the system-plan UPDATE runs."
  artifacts:
    - path: "server/api/internal/handler/auth.go"
      issue: "GuestLogin creates the user before assigning plan_id; INSERT carries plan_id=''"
    - path: "server/api/internal/model/user.go"
      issue: "PlanID field lacks a default tag / pointer, so GORM serializes '' into the uuid column"
    - path: "server/api/cmd/createadmin/main.go"
      issue: "Same bug — createadmin INSERT fails with plan_id='' (admin bootstrap broken)"
  missing:
    - "Set user.PlanID = system plan id BEFORE CreateUser (or make plan_id nullable / give it a real default), in both GuestLogin and createadmin"
    - "Add a real-Postgres integration test for guest onboarding so SQLite can't mask uuid-typing failures"

- truth: "The committed ADMIN-03/ADMIN-06 integration tests run green on a Docker host"
  status: fixed
  reason: >
    SC-2/SC-3 proofs had never executed — 4 stale-fixture bugs (missing max_devices/
    max_servers, duplicate free/pro plan inserts, duplicate plan_offer, missing admin
    user FK). Fixed in this session; both tests now pass. The product logic was correct
    all along; only the fixtures were stale relative to migrations 019/014/024.
  severity: major
  test: 2
  artifacts:
    - path: "server/api/integration/admin_concurrency_test.go"
      issue: "stale plans seed — fixed to reuse migration-seeded plan IDs"
    - path: "server/api/integration/webhook_replay_test.go"
      issue: "stale plans/plan_offers seed + missing admin user — fixed"
