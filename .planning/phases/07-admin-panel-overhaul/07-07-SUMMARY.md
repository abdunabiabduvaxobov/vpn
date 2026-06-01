---
phase: 07-admin-panel-overhaul
plan: 07
subsystem: backend-api
tags: [go, fiber, gorm, redis, feature-flags, maintenance-mode, broadcasts, middleware, admin]

# Dependency graph
requires:
  - phase: 07-admin-panel-overhaul
    plan: 01
    provides: "migration 024 (feature_flags + broadcast_messages tables, 3 seeded flags); RED TestMaintenanceMiddleware stub"
  - phase: 07-admin-panel-overhaul
    plan: 06
    provides: "admin server-control conventions (drain/undrain/disconnect) the system-control handlers mirror"
provides:
  - "middleware.Maintenance(db, redis, logger): 503s non-admins when maintenance_mode is ON, exempts admin/auth-login/livez/readyz/internal (Pitfall 5), fail-open on read error"
  - "middleware.RequireFlagOff(db, redis, logger, key, message): route-scoped flag guard used for signups_off (/auth/guest) and payments_off (/checkout)"
  - "cache.GetFlag / cache.BustFlag: two-layer ~10s Redis cache fronting the authoritative feature_flags DB row, fail-open from Redis to DB"
  - "repository feature_flag_repo (Get/Set/List) + broadcast_repo (active/all/Create/Update/Delete/Find)"
  - "Admin system routes: GET/PUT /admin/feature-flags(/:key), GET/POST/PATCH/DELETE /admin/broadcasts"
  - "Public GET /api/v1/broadcasts returning minimal {title, body, severity} active banners"
affects: [07-08, 07-09, 07-10]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Two-layer fail-open flag cache (cache.GetFlag) mirroring the user-existence cache: Redis hit → cached bool; miss → DB + populate; Redis outage → DB direct; never errors out the whole site"
    - "Route-scoped flag-guard middleware (RequireFlagOff) keeps handler signatures unchanged while gating /auth/guest and /checkout"
    - "Operator escape-hatch exemption list in maintenance gate mirrors the AppVersion SkipRule precedent (prefix + exact (method,path) matches)"

key-files:
  created:
    - server/api/internal/model/feature_flag.go
    - server/api/internal/model/broadcast.go
    - server/api/internal/repository/feature_flag_repo.go
    - server/api/internal/repository/broadcast_repo.go
    - server/api/internal/cache/flags_cache.go
    - server/api/internal/middleware/maintenance.go
    - server/api/internal/handler/admin_system.go
    - server/api/internal/handler/broadcasts.go
    - server/api/internal/handler/admin_system_test.go
  modified:
    - server/api/internal/middleware/maintenance_test.go
    - server/api/internal/middleware/audit.go
    - server/api/cmd/main.go

key-decisions:
  - "set_feature_flag audit row leaves target_id NULL (audit_log.target_id is a UUID column, migration 014); the flag key + value live in Details instead"
  - "GetFeatureFlag treats a missing flag row as the safe default (false) rather than an error, so an unseeded flag never 503s signups/payments/site"
  - "Maintenance + RequireFlagOff both fail open on a flag read error — a flag we cannot read must never lock the operator (or paying users) out"
  - "Public /broadcasts uses an explicit publicBroadcast projection struct ({title,body,severity}) so internal columns (id/target_tier/locale/created_at) cannot leak (T-07-29)"

requirements-completed: [ADMIN-05]

# Metrics
duration: 18 min
completed: 2026-06-01
---

# Phase 7 Plan 07: ADMIN-05 System Controls Summary

**Feature flags (signups_off / payments_off), maintenance mode (503 to non-admins with an operator escape hatch), and broadcast banners — DB feature_flags is the source of truth fronted by a ~10s fail-open Redis cache; the 07-01 RED maintenance test is now GREEN.**

## Performance

- **Duration:** ~18 min
- **Tasks:** 2 (both TDD)
- **Files created:** 9 (8 code + 1 test)
- **Files modified:** 3

## Accomplishments

- **Maintenance gate** (`middleware.Maintenance`, mounted on the `/api/v1` group right after `RateLimit`): when `maintenance_mode` is ON every non-exempt request gets `503 {"error":"service temporarily unavailable for maintenance"}` + a `Retry-After` header. The **operator escape hatch** (`/api/v1/admin/*`, `POST /auth/admin-login`, `GET /livez`, `GET /readyz`, `/api/v1/internal/*`) always passes through so the operator can always turn maintenance back off (Pitfall 5 / T-07-27). The gate **fails open** on a flag read error — a Redis+DB read failure allows the request rather than 503ing the whole site.
- **Two-layer flag cache** (`cache.GetFlag` / `cache.BustFlag`): the DB `feature_flags` row is the source of truth; a ~10s Redis cache (`cache:flag:<key>` = `"1"`/`"0"`) fronts it so the per-request gate does not hammer the DB (T-07-28). Redis hit → cached bool; miss → DB read + populate; Redis outage → DB read directly; admin write busts the key so the toggle propagates immediately (the TTL is only the backstop). Mirrors the existing fail-open `user_cache` shape exactly.
- **Feature-flag enforcement**: `middleware.RequireFlagOff` route-scoped guards gate `POST /auth/guest` with `signups_off` and `POST /checkout` with `payments_off` (503 with a flag-specific message), leaving `GuestLogin` / `CreateCheckoutSession` unchanged.
- **Admin system CRUD** (audited admin group): `GET/PUT /admin/feature-flags(/:key)` (set busts the cache and writes a reason-carrying audit row) and `GET/POST/PATCH/DELETE /admin/broadcasts` (severity validated against `{info, warning, critical}`, 404 on missing id).
- **Public broadcasts**: `GET /api/v1/broadcasts` returns only the **active** banners projected to `{title, body, severity}` (T-07-29 — no `id`/`target_tier`/`locale`/`created_at` leak); added an `AppVersion` SkipRule for it (landing/mobile may not send the header, mirroring `/plans`).
- **07-01 RED → GREEN**: `TestMaintenanceMiddleware` now asserts (a) non-admin path 503 + `Retry-After` under maintenance, (b) `/admin/*` passes, (c) `/auth/admin-login` passes, (c2) `/livez` + `/readyz` pass, (d) all paths pass when maintenance is off — backed by an in-memory SQLite `feature_flags` table with a nil Redis client (cache falls through to DB).

## Task Commits

1. **Task 1: models + repos + flags_cache + Maintenance middleware (GREEN the maintenance test)** — `f6e06f0` (feat)
2. **Task 2: mount Maintenance + flag guards + admin flag/broadcast CRUD + public broadcasts** — `af6e8f8` (feat)

## Decisions Made

- **`set_feature_flag` audit row leaves `target_id` NULL.** `audit_log.target_id` is a UUID column (migration 014) and a flag key (`"maintenance_mode"`) is not a UUID — inserting it would break the row on Postgres. The flag key + new value are carried in `Details` instead (the AuditLog middleware also records the outer method+path row).
- **Missing flag row = safe default (false), not an error.** `GetFeatureFlag` returns `(false, nil)` on `ErrRecordNotFound` so an unseeded flag never accidentally 503s signups/payments/the whole site.
- **Both gates fail open on read error.** A flag we cannot read must never lock the operator out (maintenance) or block paying users (checkout). `GetFlag` already fails open Redis→DB; the middleware adds the last-resort allow-on-DB-error guard.

## Deviations from Plan

None - plan executed exactly as written. (The plan's `<interfaces>` block anticipated the `target_id`/audit shape via `writeUserControlAudit`; the NULL-`target_id` choice for flag actions is the documented design decision above, not a scope change.)

**Total deviations:** 0
**Impact on plan:** None.

## Issues Encountered

- **Pre-existing Docker-dependent test out of scope.** `internal/repository/TestCtxCancelAbortsQuery` `t.Fatalf`s when Docker is absent (testcontainers Postgres) — it is unrelated to this plan and passes under `-short` / with Docker. Same condition the 07-01 SUMMARY documented for the testcontainers-backed tests; it is the orchestrator's post-wave Docker validation concern. Logged to `deferred-items.md`. All packages this plan touches (`middleware`, `cache`, `repository -short`, `handler`, `model`) and the full `-short` suite are green, and `go build ./...` + `go vet` are clean.

## Next Phase Readiness

- ADMIN-05 system controls are complete and wired. `maintenance_mode` / `signups_off` / `payments_off` are toggle-able via the audited admin API and enforced on every request / on `/auth/guest` / on `/checkout`; the public `/broadcasts` feed is live for landing + mobile.
- No blockers. Ready for the next wave plan (07-08 webhook replay / remaining admin plans).

## Self-Check: PASSED

All 9 created files + 3 modified files exist on disk; both task commits (`f6e06f0`, `af6e8f8`) are present in git history; `TestMaintenanceMiddleware` is GREEN; `go build ./...` and `go vet` are clean.

---
*Phase: 07-admin-panel-overhaul*
*Completed: 2026-06-01*
