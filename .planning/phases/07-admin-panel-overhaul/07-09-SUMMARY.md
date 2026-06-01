---
phase: 07-admin-panel-overhaul
plan: 09
subsystem: infra
tags: [deps-health, readyz-reuse, lava-cache, heartbeat-freshness, fiber, gorm, admin, tdd]

# Dependency graph
requires:
  - phase: 07-admin-panel-overhaul
    provides: "readyz probe helpers (checkPostgres/checkRedis/checkLava/statusWord) + cache.GetLavaReachable + vpn_servers.last_seen_at (07-03)"
  - phase: 07-admin-panel-overhaul
    provides: "internal/handler/admin_system.go scaffold + admin group route-wiring conventions (07-07)"
  - phase: 07-admin-panel-overhaul
    provides: "migration 024 (vpn_servers.last_seen_at column) (07-01)"
provides:
  - "GET /admin/system/deps-health — admin-only detailed dependency map: postgres/redis/lava status + per-tunnel-server list (id, hostname, is_active, current_load, last_seen_at, fresh)"
  - "repository.ListServerHealth — per-server heartbeat detail (health-only columns, ordered by hostname)"
  - "handler.AdminDepsHealth + checkLavaVerdict/lavaStatusWord (cache-only lava verdict with ok/down/unknown, no per-poll dial)"
affects: [07-10]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Admin-authed detailed health view reuses the SAME readyz probe building blocks (checkPostgres/checkRedis) — no duplicated DB/Redis check logic"
    - "lava verdict for the admin view is read cache-only (checkLavaVerdict) and never dials — distinguishes ok/down/unknown so the System page renders a pre-verdict miss honestly (T-07-38)"
    - "Admin endpoint always returns 200 (never 503): a degraded dep is data to RENDER, not a request failure — unlike the public readyz gate"
    - "Per-server detail (hostnames, last_seen_at) gated behind the AdminRequired group; the public /readyz still returns status-words only (T-07-37 info-disclosure boundary)"

key-files:
  created:
    - server/api/internal/handler/admin_deps_health_test.go
  modified:
    - server/api/internal/handler/admin_system.go
    - server/api/internal/repository/server_repo.go
    - server/api/cmd/main.go

key-decisions:
  - "deps-health reuses readyz's checkPostgres/checkRedis directly (same 500ms timeouts) — zero duplicated probe logic"
  - "lava status is read cache-only via a new checkLavaVerdict (never the dialing checkLava) and exposes ok/down/unknown — the admin view shows 'unknown' on a cold cache rather than falsely 'down'"
  - "AdminDepsHealth always returns 200, never 503 — the System page wants to display degraded deps, not treat them as a transport failure"
  - "ListServerHealth selects only health-relevant columns (id/hostname/is_active/current_load/last_seen_at) — never the REALITY keys/endpoints on the full VPNServer row"
  - "Route mounted on the audited admin group as a GET, so the AuditLog middleware skips it (GET/HEAD short-circuit) — no describeAction case needed"

patterns-established:
  - "Admin health detail vs public probe: same probe primitives, two response shapes — words-only for anonymous /readyz, full detail for admin deps-health"
  - "fresh = LastSeenAt != nil && LastSeenAt.After(now - tunnelFreshWindow) — reuses the 90s window constant shared with the readyz tunnel check"

requirements-completed: [ADMIN-08]

# Metrics
duration: 3min
completed: 2026-06-01
---

# Phase 7 Plan 09: Admin Deps-Health Endpoint (ADMIN-08) Summary

**Admin-only `GET /admin/system/deps-health` returning a detailed postgres/redis/lava status map plus a per-tunnel-server list with `last_seen_at` + a `fresh` flag, reusing the readyz probe primitives and the ≤60s Redis-cached lava verdict (no per-poll dial) — detail that is gated behind admin auth and never leaks from the public status-words-only `/readyz`.**

## Performance

- **Duration:** 3 min
- **Started:** 2026-06-01T12:47:30Z
- **Completed:** 2026-06-01T12:50:51Z
- **Tasks:** 1 (TDD: RED → GREEN)
- **Files created/modified:** 4 (1 created, 3 modified)

## Accomplishments

- **ADMIN-08 deps-health endpoint.** `handler.AdminDepsHealth(logger, db, redisClient, lavaClient)` returns `200 {data:{postgres, redis, lava, tunnel_servers:[...]}}`. `postgres`/`redis` reuse the readyz `checkPostgres`/`checkRedis` helpers verbatim (same 500ms per-dep timeouts); `lava` is read cache-only via a new `checkLavaVerdict` so a poll NEVER dials lava (T-07-38). Each tunnel server carries `id, hostname, is_active, current_load, last_seen_at, fresh`, where `fresh = last_seen_at within 90s` (the same `tunnelFreshWindow` constant the readyz tunnel check uses).
- **Info-disclosure boundary (T-07-37).** The per-server topology (hostnames, last_seen_at) is exposed ONLY here, on the `AdminRequired` group. The public `/readyz` is untouched and still returns status-words only — the admin detail cannot leak anonymously.
- **lava `ok`/`down`/`unknown` for the admin view.** Unlike readyz (binary ok/down + dial-on-miss), `checkLavaVerdict` returns `unknown` on a cache miss so the System page renders an honest "not yet probed" state instead of a false "down".
- **`repository.ListServerHealth`** — a pure ctx-threaded SELECT of only the five health-relevant columns (never the REALITY keys/endpoints), ordered by hostname for a stable display.
- **Never 503s.** The admin endpoint always returns 200 with the current per-dep status — a degraded dep is data the System page renders, not a request failure.
- **07-09 RED → GREEN.** `TestDepsHealth` (SQLite + miniredis, `handler_test` package reusing `systemApp`/`doJSON`) asserts the dep keys, the two-server list, fresh=true for a recent server / fresh=false for a 2-minute-stale one, and that the lava verdict comes straight from the seeded cache with a nil lava client (proving no dial).

## Task Commits

Each task was committed atomically:

1. **Task 1 (RED): failing deps-health test** - `5dff330` (test)
2. **Task 1 (GREEN): ListServerHealth + AdminDepsHealth + route** - `5dceb7f` (feat)

**Plan metadata:** committed separately (docs: complete plan)

_Task 1 is TDD (RED → GREEN); the GREEN implementation reused the existing readyz helpers cleanly, so no refactor commit was needed._

## Files Created/Modified

- `server/api/internal/handler/admin_deps_health_test.go` — GREEN `TestDepsHealth` (2 subtests: per-dep status + per-server freshness; lava cache-only verdict with nil client)
- `server/api/internal/handler/admin_system.go` — `AdminDepsHealth` handler + `checkLavaVerdict`/`lavaStatusWord` cache-only lava helpers; added `context` and `lava` imports
- `server/api/internal/repository/server_repo.go` — `ServerHealthRow` struct + `ListServerHealth` (ctx-threaded, health-only column SELECT, ordered by hostname)
- `server/api/cmd/main.go` — wired `admin.Get("/system/deps-health", handler.AdminDepsHealth(...))` into the audited admin group

## Decisions Made

- **Reuse readyz's `checkPostgres`/`checkRedis` rather than re-implement** — they live in the same `handler` package, already carry the right 500ms timeouts, and the must-have explicitly forbids a duplicated flaky per-call dial. This keeps a single source of truth for the DB/Redis probe behavior.
- **A cache-only `checkLavaVerdict` distinct from the dialing `checkLava`** — readyz needs a binary gate (and may lazily refresh on a miss), but the admin page needs the *display* truth without ever triggering a dial. Returning `unknown` on a cold cache is more honest for an operator than collapsing a miss to "down".
- **Always 200, never 503** — the public readyz gate is a load-balancer signal; the admin deps-health response is a UI data source. Returning 503 would make the System page treat a degraded-but-reachable dep as a failed fetch.

## Deviations from Plan

None - plan executed exactly as written. The plan anticipated that 07-03's readyz helpers were already factored (they are: `checkPostgres`/`checkRedis`/`checkLava`/`statusWord` are package-level funcs), so no `checkDeps` extraction was required. The only judgment call — adding a cache-only `checkLavaVerdict` with an `unknown` state rather than calling the dialing `checkLava` — is squarely within the plan's "reuse the cached value, never a fresh dial" intent and is documented above as a decision, not a deviation.

## Issues Encountered

- **Pre-existing `internal/repository/TestCtxCancelAbortsQuery` requires Docker (out of scope).** The full (non-`-short`) repository suite shows one FAIL: `TestCtxCancelAbortsQuery` (`ctx_cancel_test.go`, from phase 06) `t.Fatalf`s with "Cannot connect to the Docker daemon" because Docker is unavailable in this execution environment. It is untouched by 07-09 and unrelated to it — this plan's repository change is a pure additive `ListServerHealth` SELECT. The repository suite passes cleanly under `-short`, and the new `TestDepsHealth` (handler package, SQLite + miniredis) needs no Docker and passes. Logged to `deferred-items.md` (07-09 entry); the orchestrator's post-wave validation runs in the CI go/Docker environment where this test passes.

## User Setup Required

None - no external service configuration required. The endpoint reuses the existing DB, Redis, lava client, and `INTERNAL_HEARTBEAT_SECRET`-fed `last_seen_at` already wired by 07-03.

## Next Phase Readiness

- ADMIN-08 deps-health is live and admin-only. The 07-10 admin System page can now poll `GET /admin/system/deps-health` for live DB/Redis/lava status and per-tunnel-server heartbeat freshness.
- No blockers for the API path. The Docker-gated repository test is a pre-existing, out-of-scope environment limitation tracked in `deferred-items.md`.

## Self-Check: PASSED

All 4 files (`admin_deps_health_test.go` created; `admin_system.go`, `server_repo.go`, `main.go` modified) plus SUMMARY.md exist on disk; both task commits (`5dff330` RED, `5dceb7f` GREEN) are present in git history. `go test ./internal/handler/ -run TestDepsHealth -count=1` green, `go build ./...` exit 0.

---
*Phase: 07-admin-panel-overhaul*
*Completed: 2026-06-01*
