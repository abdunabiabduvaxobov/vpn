---
phase: 06-performance-scalability
plan: 01
subsystem: deploy-topology-and-server-hardening
tags: [perf-03, perf-09, docker-compose, data-tier-split, host-parameterization, postgresql-tuning, fiber-timeouts, gorm-preparestmt, pool-tuning, redis-maxmemory, wave-1]
requires:
  - "06-00 Wave-0 test scaffolding (TestServerConfig stub target) — co-wave; this plan ships the real TestServerConfig"
provides:
  - "docker-compose.data.yml — data tier (postgres + redis), Redis --maxmemory 256mb --maxmemory-policy allkeys-lru (D-10a), postgres mounts tuned postgresql.conf via config_file flag"
  - "docker-compose.prod.yml — app tier only (api + tunnel); DATABASE_URL/REDIS_URL resolve via ${DB_HOST:-postgres}/${REDIS_HOST:-redis} (PERF-03 / D-01)"
  - "server/api/postgresql.conf — pgtune values for a 4GB/SSD data host (shared_buffers 1GB, effective_cache_size 2GB, max_connections 200, work_mem 16MB, maintenance_work_mem 256MB, random_page_cost 1.1, listen_addresses '*')"
  - "buildFiberConfig() in cmd/main.go — BodyLimit 64KB, ReadTimeout 15s, WriteTimeout 30s, IdleTimeout 120s, Prefork false (PERF-09 / D-09c)"
  - "gormConfig() (PrepareStmt true, D-10d) + applyPoolSettings() (MaxIdleConns 25 + ConnMaxIdleTime 5m, D-10b) in internal/repository/db.go"
  - "TestServerConfig (cmd) + TestGormConfig_PrepareStmtEnabled / TestApplyPoolSettings_NoPanic (repository)"
affects:
  - "Wave-later perf-index migrations (022/023) — depend on the data tier's ./migrations initdb mount surviving the split"
  - "Operator deploy runbook — single-host bring-up now needs BOTH compose files; deploy.yml updated to do this automatically"
  - "Second-host move (OUT of Phase 6 scope) — now a zero-code .env change (set DB_HOST/REDIS_HOST)"
tech-stack:
  added: []
  patterns:
    - "Two-tier compose split (data.yml + prod.yml) sharing an external vpn-net network"
    - "Host-parameterized connection strings with service-name defaults (DB_HOST/REDIS_HOST) — second-host move becomes a .env change, zero image rebuild"
    - "Mounted postgresql.conf selected via `postgres -c config_file=...` (cleaner than N inline -c flags)"
    - "Extract-for-testability: buildFiberConfig()/gormConfig()/applyPoolSettings() pulled out of imperative boot code so config values are unit-asserted"
key-files:
  created:
    - docker-compose.data.yml
    - server/api/postgresql.conf
    - server/api/cmd/server_config_test.go
    - server/api/internal/repository/db_test.go
  modified:
    - docker-compose.prod.yml
    - server/api/cmd/main.go
    - server/api/internal/repository/db.go
    - .github/workflows/deploy.yml
decisions:
  - "Data-tier migrations mount stays ./migrations (NOT ./server/api/migrations as the plan text suggested) — the deploy workflow relocates server/api/migrations → /opt/vpn/migrations, so ./migrations is the correct in-/opt/vpn source; using the plan's literal path would break fresh-volume initdb seeding"
  - "deploy.yml updated (Rule 3) to copy docker-compose.data.yml + postgresql.conf and bring up BOTH compose files; dropped the `rm -rf server/` step because data.yml now mounts server/api/postgresql.conf directly"
  - "vpn-net declared in both compose files with the same name so the merged single-host bring-up and the standalone data-host bring-up both attach to the same bridge"
  - "Fiber config extracted into buildFiberConfig(errorHandler) so TestServerConfig can assert the four timeout/limit values without booting the app (ErrorHandler injected because it closes over the request logger)"
  - "db.go split into gormConfig() + applyPoolSettings() so PrepareStmt is unit-assertable against sqlite; pool literals kept inline in applyPoolSettings to satisfy the acceptance-criteria grep targets"
metrics:
  tasks: 2
  files: 8
  commits: 2
  duration: "~12m"
  completed: 2026-05-29
---

# Phase 6 Plan 01: Deploy Topology + Server Hardening Summary

Split the single `docker-compose.prod.yml` into an app tier (`api` + `tunnel`) and a new `docker-compose.data.yml` (`postgres` + `redis`), host-parameterized the app's `DATABASE_URL`/`REDIS_URL` through `DB_HOST`/`REDIS_HOST` env vars (defaulting to the service names so co-located dev is a zero-change bring-up), added Redis `--maxmemory 256mb --maxmemory-policy allkeys-lru`, mounted a pgtune-tuned `postgresql.conf` into Postgres, hardened the Fiber server with the four PERF-09 timeout/limit fields, tuned the GORM/PG pool (PrepareStmt + 25 idle conns + 5m idle-time), and added unit tests locking the Fiber and GORM config contracts. The physical second-host move stays a documented zero-code ops runbook step (OUT of Phase 6 scope per D-01). Closes PERF-03, PERF-09, and the riding D-10a/D-10b/D-10d polish items.

## What Was Built

### Task 1 — Data-tier compose split + host-parameterization + Redis maxmemory + mounted postgresql.conf (commit `bac065e`)
- **`docker-compose.data.yml` (NEW)** — holds the `postgres` + `redis` services moved out of prod.yml. Postgres keeps `postgres:16-alpine`, `vpn-postgres`, the `POSTGRES_*` env, the `pgdata` + `./migrations` initdb mounts and healthcheck, plus a NEW read-only mount of `./server/api/postgresql.conf` and `command: ["postgres", "-c", "config_file=/etc/postgresql/postgresql.conf"]`. Redis command becomes `redis-server --requirepass ${REDIS_PASSWORD:-changeme} --maxmemory 256mb --maxmemory-policy allkeys-lru` (D-10a). Neither service publishes a public host port (D-02 / T-06-DATALINK). Both join `default` + `vpn-net`. In-file runbook documents the second-host move + the TLS-ready upgrade path (`sslmode=require` / `rediss://`, no code change).
- **`docker-compose.prod.yml` (EDITED)** — removed both `postgres:` and `redis:` service blocks. `api`'s `DATABASE_URL`/`REDIS_URL` now interpolate `${DB_HOST:-postgres}` / `${REDIS_HOST:-redis}`. Removed `depends_on` health gates (data tier is a separate file / possibly host). Kept `api` on `default` + `vpn-net` (B3 fix comment preserved). Header documents the co-located dual-file bring-up vs second-host bring-up.
- **`server/api/postgresql.conf` (NEW)** — pgtune values for a ~4GB/SSD data host: `shared_buffers 1GB`, `effective_cache_size 2GB`, `max_connections 200`, `work_mem 16MB`, `maintenance_work_mem 256MB`, `random_page_cost 1.1`, `listen_addresses '*'`. Header documents the RAM-scaling recompute path.
- **`.github/workflows/deploy.yml` (EDITED, Rule 3)** — the split would have broken the existing single-file deploy, so the workflow now copies `docker-compose.data.yml` + `server/api/postgresql.conf` alongside the existing files, drops the `rm -rf server/` step (data.yml mounts `server/api/postgresql.conf` directly), and runs `pull` + `up -d` against BOTH compose files.

### Task 2 — Fiber timeouts + BodyLimit; PG pool + PrepareStmt; TestServerConfig (commit `80385bd`)
- **`cmd/main.go` (EDITED)** — added `time` to imports; extracted `buildFiberConfig(errorHandler fiber.ErrorHandler) fiber.Config` carrying the existing fields plus `BodyLimit: 64*1024`, `ReadTimeout: 15*time.Second`, `WriteTimeout: 30*time.Second`, `IdleTimeout: 120*time.Second` (PERF-09 / D-09c; Prefork left false). The boot path is now `app := fiber.New(buildFiberConfig(handler.ErrorHandler(logger)))`.
- **`internal/repository/db.go` (EDITED)** — extracted `gormConfig()` (sets `PrepareStmt: true`, D-10d) and `applyPoolSettings(*sql.DB)` (sets `SetMaxIdleConns(25)` + `SetConnMaxIdleTime(5 * time.Minute)` alongside the existing `SetMaxOpenConns(100)` + `SetConnMaxLifetime(1 * time.Hour)`, D-10b). `NewDB` now calls both helpers.
- **`cmd/server_config_test.go` (NEW)** — `TestServerConfig` asserts the four Fiber timeout/limit values + `Prefork == false` + guards `AppName`/`EnableTrustedProxyCheck`/empty-`TrustedProxies`.
- **`internal/repository/db_test.go` (NEW)** — `TestGormConfig_PrepareStmtEnabled` asserts `gormConfig().PrepareStmt == true`; `TestApplyPoolSettings_NoPanic` exercises `applyPoolSettings` against an in-memory sqlite `*sql.DB` (same pattern as the existing repo tests) and pings to confirm the conn stays usable.

## Verification Results

| Acceptance criterion | Method | Result |
|---|---|---|
| `docker compose -f docker-compose.data.yml -f docker-compose.prod.yml config` exits 0 | `docker compose config` (CLI available) | PASS (`COMPOSE-VALID`) |
| data.yml has both `postgres:` + `redis:` service keys | grep | PASS (2) |
| data.yml contains `--maxmemory 256mb` | grep | PASS |
| data.yml contains `config_file=/etc/postgresql/postgresql.conf` | grep | PASS |
| prod.yml `^  postgres:` count == 0 | grep -c | PASS (0); `^  redis:` also 0 |
| prod.yml contains `${DB_HOST:-postgres}` + `${REDIS_HOST:-redis}` | grep -F | PASS (1 + 1) |
| postgresql.conf contains `shared_buffers` + `random_page_cost = 1.1` | grep | PASS |
| `ReadTimeout: *15 \* time.Second` in main.go | grep -E | PASS (line 51) |
| `BodyLimit: *64 \* 1024` in main.go | grep -E | PASS (line 50) |
| `WriteTimeout: *30 \* time.Second` in main.go | grep -E | PASS (line 52) |
| `IdleTimeout: *120 \* time.Second` in main.go | grep -E | PASS (line 53) |
| `PrepareStmt: *true` in db.go | grep -E | PASS (line 31) |
| `SetMaxIdleConns(25)` in db.go | grep -F | PASS (line 42) |
| `SetConnMaxIdleTime(5 \* time.Minute)` in db.go | grep -E | PASS (line 45) |
| `go build ./...` exits 0 | `go build` | NOT RUN — toolchain blocked (see Deferred Verification) |
| `go test ./cmd/... -run TestServerConfig` passes | `go test` | NOT RUN — toolchain blocked (see Deferred Verification) |

## Deferred Verification (Environment Limitation — for the orchestrator)

`go build` / `go test` / `gofmt` are **denied by the execution environment** in this worktree (only read-only invocations like `go version` are permitted). The two `go`-gated acceptance criteria (`go build ./...` exit 0; `go test ./cmd/... -run TestServerConfig`) therefore could NOT be executed here. They are deferred to the orchestrator's post-merge validation pass.

**Manual correctness review performed in lieu of the toolchain** (high confidence the build + tests are green):
- `cmd/main.go`: `time` added to the import block and used by the four duration fields; `buildFiberConfig` returns `fiber.Config`; the only `fiber.Config{}` literal is inside the helper; call site `fiber.New(buildFiberConfig(handler.ErrorHandler(logger)))` — `handler.ErrorHandler` returns `fiber.ErrorHandler`, which is exactly the helper's parameter type. No unused imports introduced.
- `internal/repository/db.go`: `database/sql` added (used by `applyPoolSettings(*sql.DB)`), `time` still used, `gormConfig()` + `applyPoolSettings()` both consumed by `NewDB`. No dangling symbols.
- `cmd/server_config_test.go` (`package main`): imports only `testing` + `time`, both used; calls `buildFiberConfig(nil)` (the helper tolerates a nil ErrorHandler — it is merely assigned).
- `internal/repository/db_test.go` (`package repository`): mirrors the exact `gorm.Open(sqlite.Open(":memory:"), ...)` pattern proven by `connection_repo_test.go`/`plan_repo_test.go`; `sqlite` + `gorm` are already module deps (`gorm.io/driver/sqlite v1.6.0`, `github.com/mattn/go-sqlite3 v1.14.22`).

**Recommended orchestrator commands:**
```
cd server/api && go build ./...
cd server/api && go test ./cmd/... ./internal/repository/... -run "TestServerConfig|TestGormConfig|TestApplyPoolSettings" -count=1
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Migrations mount path + deploy workflow would break the production deploy**
- **Found during:** Task 1 (reading `.github/workflows/deploy.yml` per the task's `read_first`).
- **Issue:** The plan text instructed the data tier to mount `./server/api/migrations:/docker-entrypoint-initdb.d`. But the live deploy workflow copies `server/api/migrations` to the host, **relocates it to `/opt/vpn/migrations`**, then `rm -rf server/`. The pre-split prod.yml correctly used `./migrations` (relative to `/opt/vpn`). Mounting `./server/api/migrations` would point at a path the deploy deletes — fresh-volume initdb seeding would silently break. RESEARCH §Fork-2 (line 237) confirms `docker-compose.prod.yml:19` mounts `./migrations`.
- **Fix:** Used `./migrations:/docker-entrypoint-initdb.d` in `docker-compose.data.yml` (matching the deploy relocation). Then updated `deploy.yml` to (a) ALSO copy `docker-compose.data.yml` + `server/api/postgresql.conf`, (b) drop the `rm -rf server/` step (data.yml now mounts `server/api/postgresql.conf` directly, so `server/api/` must survive), and (c) run `pull` + `up -d` against BOTH compose files. Without this, the split would have brought up an app tier with no data tier.
- **Files modified:** `docker-compose.data.yml`, `.github/workflows/deploy.yml`.
- **Commit:** `bac065e`.

**2. [Rule 2 - Critical functionality] vpn-net network declared in BOTH compose files**
- **Found during:** Task 1 (compose `config` validation design).
- **Issue:** The data tier's postgres/redis must be reachable by the app tier's `api` over the same bridge. If only prod.yml declares `vpn-net`, the standalone data-host bring-up (`docker compose -f docker-compose.data.yml up -d`) has no network for the data services.
- **Fix:** Declared the same-named `vpn-net` bridge in both files and attached postgres + redis to `default` + `vpn-net`. The merged single-host bring-up reconciles the shared name; the standalone data-host bring-up creates it. `docker compose config` validates the merged topology (exit 0).
- **Files modified:** `docker-compose.data.yml`, `docker-compose.prod.yml`.
- **Commit:** `bac065e`.

### Testability Refactors (not behavior changes)

- `cmd/main.go`: the inline `fiber.New(fiber.Config{...})` was extracted into `buildFiberConfig(errorHandler)` so `TestServerConfig` can assert the values without booting the app. Runtime behavior is identical (same config object). The plan explicitly sanctioned this ("extract the Fiber config into a small `buildFiberConfig() fiber.Config` helper").
- `internal/repository/db.go`: `NewDB`'s inline config + pool calls were extracted into `gormConfig()` + `applyPoolSettings()` so `PrepareStmt` is unit-assertable against sqlite (NewDB itself needs a live Postgres for its `Ping`). The pool literals were kept inline inside `applyPoolSettings` (not hoisted to named constants) specifically so the acceptance-criteria grep targets `SetMaxIdleConns(25)` and `SetConnMaxIdleTime(5 * time.Minute)` still match. Runtime behavior identical.

## Authentication Gates

None.

## Known Stubs

None. All config values are concrete and production-ready; the second-host physical move is intentionally OUT of Phase 6 scope (D-01) and documented as an operator runbook step in-file, not stubbed in code.

## Threat Flags

No new security-relevant surface beyond the plan's `<threat_model>`. The implementation addresses exactly the four registered threats:
- **T-06-DATALINK** — postgres/redis publish no public host port in compose; Redis `--requirepass` enforced; connection strings written TLS-ready with the documented `sslmode=require`/`rediss://` upgrade path.
- **T-06-SLOWLORIS** — `ReadTimeout 15s` + `IdleTimeout 120s` (asserted by TestServerConfig).
- **T-06-BODYDOS** — `BodyLimit 64*1024` (asserted by TestServerConfig).
- **T-06-REDISOOM** — `--maxmemory 256mb --maxmemory-policy allkeys-lru`.

## Self-Check: PASSED

- All 4 created files (`docker-compose.data.yml`, `server/api/postgresql.conf`, `server/api/cmd/server_config_test.go`, `server/api/internal/repository/db_test.go`) verified present on disk via `ls`.
- All 4 modified files (`docker-compose.prod.yml`, `server/api/cmd/main.go`, `server/api/internal/repository/db.go`, `.github/workflows/deploy.yml`) committed.
- Both task commits (`bac065e`, `80385bd`) verified in `git log`.
- STATE.md / ROADMAP.md NOT modified (orchestrator owns those writes after the wave completes, per objective).
- One caveat carried forward: the two `go`-gated acceptance criteria are deferred to the orchestrator's post-merge validation (toolchain blocked in this worktree). All non-toolchain criteria PASS.
