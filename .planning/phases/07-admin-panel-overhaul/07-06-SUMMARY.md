---
phase: 07-admin-panel-overhaul
plan: 06
subsystem: api
tags: [fiber, gorm, redis, rate-limit, cache-bust, drain, audit-log, admin, tdd]

# Dependency graph
requires:
  - phase: 07-admin-panel-overhaul
    provides: "migration 024 vpn_servers.is_draining/last_seen_at columns + RED TestServerDrainHidesFromPublic stub (plan 07-01)"
  - phase: 07-admin-panel-overhaul
    provides: "Option-B force-disconnect-by-user pattern (DisconnectConnectionsByUser, IncrRateLimit throttle, describeAction labels) to mirror by server_id (plan 07-04)"
provides:
  - "model.VPNServer.IsDraining/LastSeenAt struct fields (migration 024 columns)"
  - "repository.ListActiveServers gains AND is_draining=false so a drained server drops from non-admin GET /servers"
  - "repository.DisconnectConnectionsByServer (Option-B mark-disconnected by server_id) + CountActiveConnectionsByServer"
  - "handler.AdminDrainServer/AdminUndrainServer: set is_draining + synchronous BustServersCache; drain {force:true} also force-disconnects"
  - "handler.AdminDisconnectServer: force-disconnect-all-on-a-server (Option-B), throttled <=1/server/60s"
  - "handler.AdminServerHealth: per-server concurrent_conns/last_seen_at/current_load read"
  - "describeAction labels: drain_server / undrain_server / disconnect_server"
affects: [07-10]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "DB filter is the source of truth, cache-bust is the fast path: ListActiveServers filters is_draining at the DB so a cache miss is correct even before BustServersCache runs (T-07-24)"
    - "Shared forceDisconnectServer helper applies the per-server throttle + Option-B disconnect identically on the dedicated /disconnect path and the drain {force:true} branch"
    - "Per-server force-disconnect throttle window is 60s (heavier/rarer than the per-user 30s) via atomic IncrRateLimit -> 429 (T-07-23)"

key-files:
  created:
    - server/api/internal/handler/admin_server_controls.go
    - server/api/internal/handler/admin_server_controls_test.go
  modified:
    - server/api/internal/model/server.go
    - server/api/internal/repository/server_repo.go
    - server/api/internal/repository/connection_repo.go
    - server/api/internal/middleware/audit.go
    - server/api/cmd/main.go
    - server/api/internal/handler/admin_kpis_test.go

key-decisions:
  - "Drain is is_draining=true + synchronous BustServersCache; ListActiveServers also filters is_draining at the DB so a cache miss after a Redis hiccup is still correct (T-07-24, Option-B LOCKED)"
  - "force-disconnect marks connections by server_id (Option-B); no Redis tunnel:kill channel — live tunnels die on the existing ~3-min stale sweep (T-07-26 accepted)"
  - "Per-server disconnect throttle is 60s (vs 30s per-user) because draining/disconnecting a whole server is a far heavier, rarer blast-radius operation (T-07-23)"
  - "drain body {force} is optional — a bare POST drains without force; a malformed body defaults to force=false so the common no-body drain always works"
  - "Server-action audit labels (drain/undrain/disconnect_server) ordered BEFORE create_server in describeAction so the readable label wins over the post_admin_servers_<uuid>_<action> fallback"

patterns-established:
  - "forceDisconnectServer(c, logger, db, redis, serverID) -> (throttled, killedCount, err): one throttle+disconnect primitive shared by AdminDisconnectServer and AdminDrainServer force branch"
  - "AdminServerHealth composes CountActiveConnectionsByServer + the server row into a {concurrent_conns,last_seen_at,current_load} snapshot"

requirements-completed: [ADMIN-04]

# Metrics
duration: 8min
completed: 2026-06-01
---

# Phase 7 Plan 06: Server Controls (drain / force-disconnect / health) Summary

**ADMIN-04 server-control surface: drain mode (is_draining=true + ListActiveServers `AND is_draining=false` filter + synchronous cache-bust so a drained server vanishes from non-admin /servers within one request while existing tunnels survive), per-server force-disconnect-all (Option-B mark-disconnected by server_id, throttled <=1/server/60s -> 429), and a per-server health read (concurrent_conns/last_seen_at/current_load).**

## Performance

- **Duration:** 8 min
- **Started:** 2026-06-01T17:15:00Z
- **Completed:** 2026-06-01T17:23:00Z
- **Tasks:** 2 (both TDD)
- **Files created:** 2
- **Files modified:** 14 (6 production/test for the feature + 8 sibling test DDLs — see Deviations)

## Accomplishments

- **Drain mode (ADMIN-04 SC-4):** `model.VPNServer.IsDraining` + `repository.ListActiveServers` gains `AND is_draining = false`, the load-bearing change that drops a drained server from non-admin GET /servers (which reads ListActiveServers via the cache). `AdminDrainServer` sets `is_draining=true` via `UpdateServer` and synchronously `BustServersCache` so the drop is visible within one request; `ListAllServers` (admin path) is unchanged so operators still see drained servers. The 07-01 RED `TestServerDrainHidesFromPublic` is now GREEN, asserting exactly this asymmetry.
- **Force-disconnect-all-on-a-server (Option-B):** `repository.DisconnectConnectionsByServer` marks every live connection on a server disconnected by `server_id` (mirrors 07-04's by-user variant). `AdminDisconnectServer` is throttled to <=1/server/60s via atomic `IncrRateLimit` (429 on the second call within the window, T-07-23). Live VLESS/REALITY tunnels are NOT killed in real time — they die on the existing ~3-min stale sweep (T-07-26 accepted, LOCKED). No Redis tunnel:kill channel.
- **drain {force:true}** also force-disconnects in the same call via a shared `forceDisconnectServer` helper, so the throttle and blast-radius behaviour are identical on both the dedicated /disconnect path and the force-drain branch.
- **Per-server health read:** `AdminServerHealth` composes `CountActiveConnectionsByServer` + the server row into `{concurrent_conns, last_seen_at, current_load}`; 404 for an unknown server.
- **Readable audit labels:** `describeAction` now resolves `drain_server`/`undrain_server`/`disconnect_server`, ordered before the generic `create_server` case so the `/admin/servers/:id/<action>` POSTs do not fall through to the `post_admin_servers_<uuid>_<action>` fallback.

## Task Commits

Each task was committed atomically (TDD — sibling DDL fixes and impl landed within the task they unblocked):

1. **Task 1: is_draining filter + disconnect/count-by-server repo (+ sibling DDL backfill)** - `de88ef6` (feat)
2. **Task 2: drain/undrain/disconnect/health handlers + routes + audit (RED→GREEN)** - `2f2802e` (feat)

_Plan metadata commit is made by the orchestrator after the wave._

## Files Created/Modified

- `server/api/internal/handler/admin_server_controls.go` - 4 handlers (drain/undrain/disconnect/health) + shared forceDisconnectServer helper
- `server/api/internal/handler/admin_server_controls_test.go` - GREEN tests: cache-bust on drain/undrain, disconnect-by-server + throttle 429, health snapshot + 404, force-drain
- `server/api/internal/model/server.go` - IsDraining + LastSeenAt struct fields (migration 024 columns)
- `server/api/internal/repository/server_repo.go` - ListActiveServers `AND is_draining = false` filter
- `server/api/internal/repository/connection_repo.go` - DisconnectConnectionsByServer + CountActiveConnectionsByServer
- `server/api/internal/middleware/audit.go` - describeAction drain/undrain/disconnect_server cases (before create_server)
- `server/api/cmd/main.go` - wire 4 admin routes (drain/undrain/disconnect/health)
- `server/api/internal/handler/admin_kpis_test.go` - 07-01 RED TestServerDrainHidesFromPublic → GREEN; vpn_servers DDL backfill

## Decisions Made

- **DB filter is the backstop, cache-bust is the fast path** — `ListActiveServers` filters `is_draining` at the DB so the result is correct even on a cache miss (Redis hiccup mid-write). `BustServersCache` is best-effort and never fails the drain; the 60s TTL is the secondary safety net (T-07-24).
- **Option-B force-disconnect by server_id** (LOCKED) — mirrors the per-user variant from 07-04: mark `disconnected_at` for all live rows on the server, no tunnel:kill. The accepted weaker guarantee (T-07-26) is that live tunnels persist until the ~3-min stale sweep; flipping the timestamp removes them from "active" accounting immediately and drain stops new connects.
- **60s per-server throttle** (vs 30s per-user) — a whole-server force-disconnect is a heavier, rarer operation; the longer window is the right blast-radius guard (T-07-23).
- **Shared forceDisconnectServer helper** — the dedicated `/disconnect` endpoint and the `drain {force:true}` branch route through one function so throttle + Option-B semantics can never drift between the two paths.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Backfilled is_draining/last_seen_at into sibling SQLite test DDLs**
- **Found during:** Task 1 (after adding model.VPNServer.IsDraining/LastSeenAt)
- **Issue:** Adding the two struct fields to `model.VPNServer` made GORM reference `is_draining`/`last_seen_at` on every vpn_servers SELECT/INSERT. Eight test files build their own explicit SQLite `CREATE TABLE vpn_servers` DDLs (no AutoMigrate, because of PG-specific defaults), and those hardcoded schemas predated migration 024 — so `model.VPNServer` operations failed with "table vpn_servers has no column named is_draining" across the handler and repository suites.
- **Fix:** Added `is_draining` (+ `last_seen_at` where missing) to every vpn_servers test DDL: `admin_kpis_test.go`, `health_endpoints_test.go` (already had last_seen_at from 07-03), `plans_public_test.go`, `plans_admin_test.go`, `servers_test.go`, `admin_test.go`, `connection_test.go`, `repository/plan_repo_test.go`, `repository/connection_repo_test.go`.
- **Files modified:** 9 sibling test files (8 with new columns; health_endpoints_test.go needed only is_draining)
- **Verification:** `go test ./internal/handler/ ./internal/repository/ -count=1` green; full `go test ./... -short -count=1` green across all 14 packages.
- **Committed in:** `de88ef6` (Task 1) and `2f2802e` (Task 2, admin_kpis_test.go which also carries the GREEN test)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** Mechanical prerequisite identical to 07-04's user-column case — the model field change is correct and intended; the sibling test schemas simply had to track it. No production behavior changed beyond the plan; no scope creep. The two new repository columns and the four handlers are exactly as specified in the plan's `<interfaces>`.

## Issues Encountered

- **Docker unavailable in the execution environment.** `internal/repository`'s `TestCtxCancelAbortsQuery` (a testcontainers-backed Postgres test) fails without Docker when run with the default tag set — a pre-existing condition documented in 07-01 and 07-04, unrelated to this plan. It is correctly skipped under `go test ./... -short` (the full short suite is green). The Docker-backed run is the orchestrator's post-wave validation step.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- ADMIN-04 server-control surface is complete and audited. 07-10 (admin UI) can wire the drain/undrain/disconnect actions (with a confirm dialog echoing the target per T-07-23) and the per-server health card.
- No blockers.

## Self-Check: PASSED

Both created files exist on disk (`internal/handler/admin_server_controls.go`, `internal/handler/admin_server_controls_test.go`); both task commits (`de88ef6`, `2f2802e`) are present in git history. `go build ./...`, the targeted `TestServerDrain*/TestServerDisconnect/TestServerHealth/TestServers` suite, and the full `go test ./... -short` run are all green.

---
*Phase: 07-admin-panel-overhaul*
*Completed: 2026-06-01*
