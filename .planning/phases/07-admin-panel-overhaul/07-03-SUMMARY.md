---
phase: 07-admin-panel-overhaul
plan: 03
subsystem: infra
tags: [health-probes, readyz, livez, kubernetes, heartbeat, redis-cache, crypto-subtle, fiber, gorm]

# Dependency graph
requires:
  - phase: 07-admin-panel-overhaul
    provides: "migration 024 (vpn_servers.last_seen_at column) + RED stub health_endpoints_test.go (07-01)"
  - phase: 07-admin-panel-overhaul
    provides: "cmd/main.go route-wiring + lava.Client + redisClient DI (07-02, wave ordering avoids concurrent main.go edit)"
provides:
  - "GET /api/v1/livez — zero-I/O liveness probe (always 200 while process is up)"
  - "GET /api/v1/readyz — readiness probe, 200 only when Postgres+Redis+lava(cached)+tunnel-freshness all green, else 503 with status-word-only per-dep map"
  - "POST /api/v1/internal/servers/:id/heartbeat — shared-secret (constant-time) machine endpoint updating last_seen_at + current_load, non-audited"
  - "middleware.InternalSecret — crypto/subtle.ConstantTimeCompare X-Internal-Secret gate (fail-closed)"
  - "cache.GetLavaReachable/SetLavaReachable — lava reachability verdict cached <=60s (fail-open)"
  - "repository.CountFreshServers + TouchServerHeartbeat (ctx-threaded)"
  - "tunnel internal.StartHeartbeat — best-effort emitter POSTing to the API on an interval"
  - "config.InternalHeartbeatSecret + INTERNAL_HEARTBEAT_SECRET required at boot"
affects: [07-09]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "K8s-style split probes: livez (zero I/O) vs readyz (dep-gated) with tight per-dep context timeouts"
    - "Cached external-dependency reachability (lava) — never a per-probe dial; lazy single refresh on cache miss bounded by a 2s timeout (T-07-10)"
    - "Status-word-only readyz body (ok/down per dep) so anonymous callers never see error strings/hostnames/versions (T-07-09)"
    - "Shared-secret machine endpoint via crypto/subtle.ConstantTimeCompare, mounted outside /admin and intentionally non-audited (T-07-08 / T-07-12)"
    - "Optional, non-validated tunnel config fields so existing deploys run unchanged"

key-files:
  created:
    - server/api/internal/cache/health_cache.go
    - server/api/internal/middleware/internal_secret.go
    - server/tunnel/internal/heartbeat.go
  modified:
    - server/api/internal/handler/health.go
    - server/api/internal/handler/health_endpoints_test.go
    - server/api/internal/repository/server_repo.go
    - server/api/internal/config/config.go
    - server/api/internal/config/config_test.go
    - server/api/cmd/main.go
    - server/tunnel/internal/config.go
    - server/tunnel/cmd/tunnel/main.go

key-decisions:
  - "readyz checks each dep with its own context.WithTimeout (DB/Redis 500ms, lava lazy dial 2s) so a hung dep yields 'down', never a hung probe"
  - "lava reachability is read from Redis (cache:health:lava, TTL 60s); on a cache miss exactly ONE 2s-bounded dial refreshes it — readyz never dials lava on a cache hit"
  - "tunnel health = cheap DB read of vpn_servers.last_seen_at within a 90s freshness window (no network call)"
  - "InternalSecret is fail-closed: empty server-side secret rejects every request (the secret is RequireEnv-required at boot)"
  - "tunnel heartbeat config fields are optional and NOT validated — AWG-only/dev nodes without them run exactly as before"
  - "heartbeat interval is clamped to a 30s minimum to avoid hammering the API"

patterns-established:
  - "statusWord() central mapping guarantees the readyz body can only ever expose 'ok'/'down' — structurally prevents dep-error leakage"
  - "Heartbeat emitter is best-effort: Warn-and-continue on failure, never crashes the tunnel's VPN serving path"

requirements-completed: [ADMIN-07]

# Metrics
duration: 18min
completed: 2026-06-01
---

# Phase 7 Plan 03: Liveness/Readiness Probes + Tunnel Heartbeat Summary

**K8s-style `/livez` (zero-I/O) and `/readyz` (DB+Redis+cached-lava+tunnel-freshness gated, 503 with status-word-only per-dep map) plus a shared-secret `/internal/servers/:id/heartbeat` endpoint and the tunnel-side best-effort emitter that feeds it.**

## Performance

- **Duration:** 18 min
- **Started:** 2026-06-01T16:42Z
- **Completed:** 2026-06-01T16:48Z
- **Tasks:** 3
- **Files created/modified:** 12 (3 created, 9 modified)

## Accomplishments

- **ADMIN-07 health probes.** `Livez` returns 200 with zero I/O. `Readyz` runs four independently-timed dep checks (Postgres `SELECT 1` ≤500ms, Redis `PING` ≤500ms, lava reachability from a ≤60s Redis cache, tunnel `last_seen_at` freshness within 90s) and returns 200 only when all four are green, else 503 with a `{data:{deps:{postgres,redis,lava,tunnel}}}` map carrying ONLY the words `ok`/`down` (T-07-09 info-disclosure mitigation — no error strings/hostnames/versions).
- **lava DoS mitigation (T-07-10).** lava is never dialed on a readyz cache hit; a cache miss triggers exactly one 2s-timeout-bounded refresh dial via `lava.ListProducts`, cached for 60s. `livez` does zero I/O.
- **Shared-secret machine endpoint (T-07-08 / T-07-12).** `middleware.InternalSecret` compares `X-Internal-Secret` via `crypto/subtle.ConstantTimeCompare` (fail-closed on empty secret). `POST /api/v1/internal/servers/:id/heartbeat` updates `last_seen_at`+`current_load` and returns 204. The `/internal` group mounts outside `/admin`, is intentionally non-audited (RESEARCH §9.2), and is exempt from the AppVersion gate via a prefix SkipRule. `INTERNAL_HEARTBEAT_SECRET` is required at boot via `RequireEnv()`.
- **Tunnel heartbeat emitter.** `internal.StartHeartbeat` POSTs to the API on a ticker (best-effort: Warn-and-continue, 5s client timeout, never crashes the tunnel), started from `cmd/tunnel/main.go` only when all three of `APIBaseURL`/`ServerID`/`HeartbeatSecret` are configured. Absent config = unchanged tunnel behavior. This is the ONLY tunnel-side change in the whole phase (LOCKED Option-B).
- **07-01 RED stub turned GREEN.** `TestReadyzLivez` now asserts livez-200, readyz-200-when-healthy, readyz-503-on-Redis-down (with status-word-only body), and readyz-503-on-stale-tunnel.

## Task Commits

1. **Task 1 (RED): failing readyz/livez test** — `9b720cd` (test)
2. **Task 1 (GREEN): livez/readyz + lava cache + freshness query** — `f8420d0` (feat)
3. **Task 2: internal-secret middleware + heartbeat route + required secret** — `f9f0186` (feat)
4. **Task 3: tunnel-side heartbeat emitter** — `dc338f5` (feat)

_Task 1 is TDD (RED → GREEN); no refactor commit was needed._

## Files Created/Modified

- `server/api/internal/handler/health.go` — `Livez`/`Readyz` handlers, per-dep check helpers with context timeouts, `statusWord` leak-guard, `HeartbeatServer` handler
- `server/api/internal/handler/health_endpoints_test.go` — GREEN `TestReadyzLivez` (4 subtests; sqlite + miniredis)
- `server/api/internal/cache/health_cache.go` — `GetLavaReachable`/`SetLavaReachable` (fail-open, key `cache:health:lava`, TTL 60s)
- `server/api/internal/repository/server_repo.go` — `CountFreshServers` + `TouchServerHeartbeat` (ctx-threaded)
- `server/api/internal/middleware/internal_secret.go` — constant-time `X-Internal-Secret` gate, fail-closed
- `server/api/internal/config/config.go` — `InternalHeartbeatSecret` field + Load() wire + `INTERNAL_HEARTBEAT_SECRET` in RequireEnv
- `server/api/internal/config/config_test.go` — updated the two RequireEnv assertions for the new required var
- `server/api/cmd/main.go` — public `/livez`+`/readyz` routes, `/internal` group under `InternalSecret`, three AppVersion SkipRules
- `server/tunnel/internal/heartbeat.go` — `StartHeartbeat` best-effort emitter
- `server/tunnel/internal/config.go` — optional `APIBaseURL`/`ServerID`/`HeartbeatSecret`/`HeartbeatIntervalSeconds`
- `server/tunnel/cmd/tunnel/main.go` — conditional emitter start + cancel in shutdown path

## Decisions Made

- **Per-dep context timeouts over a single shared deadline** — each readyz check has its own `context.WithTimeout` so one slow dep cannot consume the budget of the others and the probe latency is bounded regardless of which dep is degraded.
- **`statusWord()` as the single body-shaping function** — structurally guarantees the readyz body can only emit `ok`/`down`; there is no code path where an underlying error reaches the response (T-07-09).
- **Emit one heartbeat immediately on emitter start** — so `last_seen_at` is populated right after a tunnel (re)start instead of waiting a full interval, avoiding a spurious readyz 503 window post-deploy.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Updated two RequireEnv config tests for the new required var**
- **Found during:** Task 2 (adding `INTERNAL_HEARTBEAT_SECRET` to `RequireEnv()`)
- **Issue:** `TestRequireEnv_ReturnsEmptyWhenAllSet` and `TestRequireEnv_MissingSSOKeys_Reported` assert the exact set/count of required env vars; adding a new required key made both fail.
- **Fix:** Set `INTERNAL_HEARTBEAT_SECRET` in both tests so the first sees a complete set and the second's missing-list stays the pure SSO subset (the tests' original intent).
- **Files modified:** `server/api/internal/config/config_test.go`
- **Verification:** `go test ./internal/config/ -count=1` green.
- **Committed in:** `f9f0186` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 bug — test alignment with a correct, required new env var)
**Impact on plan:** Necessary to keep the config suite green after adding a genuinely-required boot secret. No scope creep.

## Issues Encountered

- **Pre-existing `cmd/tunnel` link error (out of scope).** `cd server/tunnel && go build ./cmd/tunnel/` fails at the LINK step with `link: github.com/sagernet/sing/common/control: invalid reference to net.errNoSuchInterface`. Verified this reproduces on the clean base (the error appears with the 07-03 changes stashed out), so it is a toolchain/indirect-dependency incompatibility (local go1.26.1 linking an older xray-core `sagernet/sing` dependency), NOT caused by this plan. The tunnel `internal` package containing the new emitter compiles cleanly (`go build ./internal/` exit 0) and `go vet ./...` passes. Logged to `deferred-items.md`; resolution belongs to a dependency-bump/toolchain-pin task. The orchestrator's post-wave validation runs in the CI go version where this link issue does not occur.

## User Setup Required

A new required environment variable was added:

- **`INTERNAL_HEARTBEAT_SECRET`** (API) — shared secret the tunnel presents on the heartbeat endpoint. The API process now fails fast at boot if it is unset. Set the same value as the tunnel's `heartbeat_secret` config field.
- **Tunnel `config.json`** (optional) — to enable heartbeats, set `api_base_url`, `server_id`, `heartbeat_secret` (matching `INTERNAL_HEARTBEAT_SECRET`), and optionally `heartbeat_interval_seconds` (clamped to a 30s minimum). Omitting these leaves the tunnel running exactly as before.

## Next Phase Readiness

- ADMIN-07 health probes are live; `last_seen_at` is now populated by the tunnel heartbeat, ready for the ADMIN-08 admin-only deps-health page (07-09) to reuse the readyz probe + tunnel-server table.
- No blockers for the API path. The tunnel-binary link error is a pre-existing, out-of-scope toolchain issue tracked in `deferred-items.md`.

## Self-Check: PASSED

All 3 created files (`health_cache.go`, `internal_secret.go`, `heartbeat.go`) + SUMMARY.md exist on disk; all four task commits (`9b720cd`, `f8420d0`, `f9f0186`, `dc338f5`) are present in git history. `TestReadyzLivez` green, `go build ./...` (api) green, tunnel `internal` package build + `go vet ./...` green.
