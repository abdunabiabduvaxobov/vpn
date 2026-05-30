---
phase: 7
slug: admin-panel-overhaul
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-30
---

# Phase 7 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from `07-RESEARCH.md` § Validation Architecture. Per-task map is finalized once plans assign task IDs.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework (backend)** | Go stdlib `testing` + `testify` (table tests throughout `server/api`) |
| **Framework (admin-web)** | none configured — `npm run build` (tsc typecheck) is the gate; vitest only if Wave 0 adds it |
| **Config file** | none — backend uses `go test`; admin-web uses `tsconfig.json` |
| **Quick run command** | `cd server/api && go test ./internal/handler/... ./internal/middleware/...` |
| **Full suite command** | `cd server/api && go test ./... && cd ../../admin-web && npm run build` |
| **Estimated runtime** | ~60–120 seconds (backend incl. integration tests needing Postgres) |

Backend HTTP handlers are tested in-process via Fiber `app.Test(req)` — no live server needed — EXCEPT the advisory-lock race (SC-2) and webhook-replay idempotency (SC-3) integration tests under `server/api/integration/`, which need a real Postgres connection to exercise `pg_advisory_xact_lock` (SQLite cannot). Those use the `testutil.StartPostgres` testcontainers pattern and skip when Docker is unavailable.

---

## Sampling Rate

- **After every task commit:** Run `{quick run command}` (handler + middleware package tests)
- **After every plan wave:** Run `{full suite command}`
- **Before `/gsd-verify-work`:** Full suite must be green + `admin-web` builds clean
- **Max feedback latency:** ~120 seconds

---

## Per-Criterion Validation Map (seed — task IDs filled at plan time)

| SC | Requirement | Observation point | Test type | Automated Command |
|----|-------------|-------------------|-----------|-------------------|
| SC-1 live KPIs | ADMIN-01 | `GET /admin/stats` JSON body fields (total/paid/MRR/active/signups/churn/failed) | unit + manual | `go test ./internal/handler -run TestAdminGetStatsKPIs` |
| SC-2 advisory lock | ADMIN-03 | two concurrent goroutines (force-cancel + webhook) through `WithUserLock`; final `subscription_tier` deterministic, never hybrid | integration (real PG) | `go test ./integration -run TestForceCancelWebhookRace` |
| SC-3 webhook replay | ADMIN-06 | call `applyLavaEvent` twice w/ same stored payload → single tier grant + `status=REPLAYED` | integration (real PG) | `go test ./integration -run TestWebhookReplayIdempotent` |
| SC-4 server drain | ADMIN-04 | set `is_draining=true` → non-admin `GET /servers` omits it, admin sees it; force-disconnect marks rows | unit | `go test ./internal/handler -run TestServerDrainHidesFromPublic` |
| SC-5 readyz/livez | ADMIN-07 | `/readyz` with each dep toggled down → 503; `/livez` always 200 | unit (mocked probes) | `go test ./internal/handler -run TestReadyzLivez` |
| SC-6 maintenance mode | ADMIN-05 | toggle flag → non-admin 503, admin 200 | unit (middleware) | `go test ./internal/middleware -run TestMaintenanceMiddleware` |

ADMIN-02 (per-user controls UI) and ADMIN-05 (system controls UI) validate via `admin-web` `npm run build` + manual UAT (no SPA test runner).

---

## Wave 0 Requirements

- [ ] Backend integration-test helper (`testutil.StartPostgres`) to connect a real Postgres for the advisory-lock race (SC-2) + webhook-replay idempotency (SC-3) tests under `server/api/integration/` — `app.Test()` + SQLite alone cannot exercise `pg_advisory_xact_lock`.
- [ ] RED-first stub tests for ADMIN-01,03,04,06,07,08 handler/middleware behaviors.
- [ ] (Optional, operator decision per RESEARCH Open Q3) introduce `vitest` in `admin-web` for the new pages; otherwise `tsc` build + manual UAT.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Dashboard refreshes without page reload | ADMIN-01 | UI polling behavior, no SPA test runner | Open dashboard, observe TanStack `refetchInterval` updating numbers without navigation |
| Per-user action buttons (suspend / force-Pro / force-disconnect) | ADMIN-02 | UI interaction | Click each on a seeded test user; confirm DB state + audit-log row |
| Webhook log "Replay" button | ADMIN-06 | UI interaction | Click Replay on a DELIVERED event; confirm status → REPLAYED, no duplicate grant |
| Maintenance-mode toggle UX | ADMIN-05/08 | UI interaction | Toggle on; confirm non-admin client gets friendly 503, admin panel still works |
| Force-disconnect-all kicks live tunnels in one request | ADMIN-04 | Requires live tunnel + clients (tunnel kill plumbing — RESEARCH Open Q1) | Connect a client, drain+force-disconnect server, confirm client dropped within sweep window |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references (PG integration helper)
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
