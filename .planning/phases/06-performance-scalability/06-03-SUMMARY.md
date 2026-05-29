---
phase: 06-performance-scalability
plan: 03
subsystem: api
tags: [redis, cache-aside, fail-open, fiber, gorm, perf-01, servers]

# Dependency graph
requires:
  - phase: 06-performance-scalability (Wave 0, plan 00)
    provides: RED test scaffolds — servers_cache_test.go (cache unit tests) and servers_cache_test.go handler test (TestServersCacheNoSelect, D-09 (b))
  - phase: 06-performance-scalability (Wave 1, plan 01)
    provides: tuned Fiber/PG config in main.go that this plan threads redisClient through
  - phase: 02-auth-sso-backend
    provides: plans_cache.go (cache-aside + fail-open template), ListPlansPublic cache-read idiom, admin plans-handler redisClient wiring pattern
provides:
  - "cache:servers:active — single global Redis blob of ListActiveServers JSON (TTL 60s, cache-aside + fail-open)"
  - "ListServersCached handler: zero-SELECT /servers on a cache hit; plan filter stays live in Go"
  - "Synchronous BustServersCache on the 3 admin server-write handlers (create/update/delete)"
affects: [06-performance-scalability plan 07 (ctx threading through repos), 06-VERIFY, any phase touching /servers or admin server CRUD]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Cache-aside + fail-open mirror of plans_cache.go for a second cache namespace"
    - "Cache the FULL shared blob; apply the entitlement filter live in Go post-cache (intersect-in-Go) so the cache win benefits both admin and non-admin paths"
    - "Synchronous DEL-on-write before the 2xx return (bust-within-one-request), with TTL as the safety net"

key-files:
  created:
    - server/api/internal/cache/servers_cache.go
  modified:
    - server/api/internal/handler/servers.go
    - server/api/internal/handler/admin.go
    - server/api/cmd/main.go
    - server/api/internal/handler/servers_test.go
    - server/api/internal/handler/admin_test.go

key-decisions:
  - "Named the cache-aware handler ListServersCached (not a modified ListServers) because the Wave 0 RED test pins that exact symbol + (logger, db, redisClient) signature; the old non-cached ListServers was removed to avoid a dead duplicate"
  - "Chose the intersect-in-Go filter form (build allowed-ID set from ListServersForPlan, select from the cached full blob) so the cache benefits non-admins too, not only admins"
  - "Cache the raw ListActiveServers slice JSON (the full server list), not the /servers response envelope — the non-admin path needs the full list to intersect against plan IDs"

patterns-established:
  - "Second cache namespace via plans_cache.go copy: Get returns '' on miss/outage (fail-open), Set best-effort, Bust synchronous DEL"
  - "Corrupt/unparseable cache entry is treated as a miss (fail-open) — falls through to DB and the next Set overwrites it"

requirements-completed: [PERF-01]

# Metrics
duration: 6min
completed: 2026-05-29
---

# Phase 6 Plan 03: Cache /servers in Redis (cache:servers:active) Summary

**PERF-01 / D-05: the heavy `/servers` read now serves from a single global `cache:servers:active` Redis blob (60s TTL, cache-aside + fail-open) with synchronous bust on the three admin server-write handlers, while the per-plan entitlement filter stays live in Go — removing the ~833 q/s read amplifier without per-plan cache keys.**

## Performance

- **Duration:** ~6 min
- **Started:** 2026-05-29T17:28:18Z
- **Completed:** 2026-05-29T17:33:34Z
- **Tasks:** 3
- **Files modified:** 6 (1 created, 5 modified)

## Accomplishments
- New `servers_cache.go` mirrors `plans_cache.go` exactly: `GetServersCache`/`SetServersCache`/`BustServersCache`, key `cache:servers:active`, TTL `60 * time.Second` (Fork 4), fail-open contract (a Redis outage never breaks `/servers`).
- `ListServersCached` reads the cached full-list blob first; on a cache hit it emits ZERO `ListActiveServers` SELECT (D-09 (b)) for both admin and non-admin; on a miss/outage it loads the DB and best-effort re-populates the cache.
- The plan-scoped filter stays live in Go: admins get the full cached blob; non-admins intersect it (in Go) with their plan's allowed server IDs from the live `ListServersForPlan` JOIN (Fork 3 — `plan_servers` membership is NOT additionally cached).
- The three admin server-write handlers (`AdminCreateServer`, `AdminUpdateServer`, `AdminDeleteServer`) synchronously call `BustServersCache` after a successful DB write and before the 2xx return, so the next `/servers` reflects the change within one request; namespace isolation preserved (server writes never touch `cache:plans:public:*`).

## Task Commits

Each task was committed atomically:

1. **Task 1: Create servers_cache.go (mirror plans_cache.go)** - `ee9ba13` (feat)
2. **Task 2: Cache-read in ListServers (filter stays in Go) + main.go wiring** - `0bfa8ca` (feat)
3. **Task 3: Synchronous BustServersCache on the 3 admin server-write handlers + main.go wiring** - `ca76216` (feat)

_Plan metadata commit (SUMMARY) is created separately by this executor._

## Files Created/Modified
- `server/api/internal/cache/servers_cache.go` (CREATED) - cache:servers:active get/set/bust, mirror of plans_cache.go (cache-aside + fail-open, 60s TTL).
- `server/api/internal/handler/servers.go` (MODIFIED) - `ListServers` → `ListServersCached(logger, db, redisClient)`; new `loadActiveServersCached` helper (cache read → DB fallback → best-effort populate); in-Go plan-intersect filter for non-admins.
- `server/api/internal/handler/admin.go` (MODIFIED) - added `redisClient` to the 3 server-write handlers; synchronous `BustServersCache` after each successful write.
- `server/api/cmd/main.go` (MODIFIED) - wired `redisClient` into the `/servers` route and the 3 admin server-write routes.
- `server/api/internal/handler/servers_test.go` (MODIFIED) - updated the test helper call site to `ListServersCached(log, db, nil)` (nil client = permanent miss, exercises the DB path the existing tests assert).
- `server/api/internal/handler/admin_test.go` (MODIFIED) - added nil-safe `stubRedis()` helper + `redis` import; updated all 7 server-write handler call sites to the 3-arg signature.

## Decisions Made
- **Handler renamed to `ListServersCached`, not a modified `ListServers`.** The Wave 0 RED test (`servers_cache_test.go:96`) explicitly calls `handler.ListServersCached(zap.NewNop(), db, rc)`. The test is the binding D-09 (b) contract, so the production symbol had to be named `ListServersCached` with the `(logger, db, redisClient)` signature. The old non-cached `ListServers(logger, db)` was removed (not kept alongside) to avoid a dead duplicate handler. The plan's Task-2 acceptance text says "ListServers signature includes redisClient" — functionally equivalent; the symbol name is dictated by the test. Documented as a Rule 3 deviation below.
- **Intersect-in-Go filter form.** The plan offered two acceptable forms; I chose the intersect form (build the allowed-ID set from `ListServersForPlan`, then select matching rows from the cached full blob) so the cache win applies to non-admins too, not only admins. The cached blob ordering (`current_load ASC`) is preserved by iterating the blob.
- **Cache the raw server slice, not the response envelope.** Unlike `ListPlansPublic` (which caches the whole serialized response), `cache:servers:active` holds the `[]model.VPNServer` JSON so the non-admin path can unmarshal and intersect it; the `id` json tag matches the Wave 0 test's primed blob `[{"id":"s1","hostname":"h1"}]`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Handler named `ListServersCached` (test-contract symbol) instead of modifying `ListServers` in place**
- **Found during:** Task 2 (cache-read in ListServers)
- **Issue:** The plan's Task-2 instruction says "Change the `ListServers` signature to accept redisClient", but the Wave 0 RED test that must turn GREEN (`internal/handler/servers_cache_test.go`, `TestServersCacheNoSelect`) calls `handler.ListServersCached(...)` — a different symbol name. Keeping `ListServers` would leave the RED test unable to compile.
- **Fix:** Created the cache-aware handler as `ListServersCached(logger, db, redisClient)` (matching the test exactly) and removed the old `ListServers(logger, db)`. Updated all call sites: `cmd/main.go` and the existing `servers_test.go` helper (passing `nil` redisClient → permanent miss → DB path unchanged for those tests).
- **Files modified:** server/api/internal/handler/servers.go, server/api/cmd/main.go, server/api/internal/handler/servers_test.go
- **Verification:** `grep` confirms zero remaining references to the old `handler.ListServers(` symbol; all call sites use `ListServersCached`. (Compile/test deferred to the orchestrator — see "Action needed" below.)
- **Committed in:** `0bfa8ca` (Task 2 commit)

**2. [Rule 3 - Blocking] Updated existing admin_test.go call sites to the new 3-arg server-write signatures**
- **Found during:** Task 3 (BustServersCache on admin server-write handlers)
- **Issue:** Adding `redisClient` to `AdminCreateServer`/`AdminUpdateServer`/`AdminDeleteServer` broke 7 existing call sites in `internal/handler/admin_test.go` that used the old 2-arg form, which would fail to compile.
- **Fix:** Added a nil-safe `stubRedis()` helper (returns `nil` `*redis.Client`; `BustServersCache(ctx, nil)` is a no-op via the nil guard) and the `redis` import, then updated all 7 call sites. The tests' DB-write and status-code assertions are unaffected because the bust is a no-op with a nil client.
- **Files modified:** server/api/internal/handler/admin_test.go
- **Verification:** `grep` confirms no remaining 2-arg server-write call sites; `stubRedis` defined once and used at all sites.
- **Committed in:** `ca76216` (Task 3 commit)

---

**Total deviations:** 2 auto-fixed (both Rule 3 - blocking, driven by the Wave 0 test contract and signature propagation).
**Impact on plan:** Both deviations are mechanical consequences of the RED-test-pinned symbol name and the signature change the plan itself mandates. No scope creep; the must_haves and acceptance criteria are all satisfied (the symbol-name detail aside, which the test dictates).

## Issues Encountered
- **`go build` / `go test` / `gofmt` are denied in this worktree sandbox** (only `go version` is permitted). Per the plan's `<validation_note>`, the implementation was validated by careful manual review against the Wave 0 tests and the `plans_cache.go` template; the orchestrator must run the toolchain post-merge (commands below).
- **Worktree base was behind the required base.** The branch HEAD was an ancestor of the required base `808e3c0` (Wave 1 merge), so a `git merge --ff-only 808e3c0` was performed first to pull in the Wave 1 work (RED scaffolds, tuned main.go, migrations) before implementing. Base is now correct.

## Action needed from the orchestrator (toolchain validation)

The `go` build/test toolchain is blocked in this sandbox. Run these post-merge to confirm GREEN:

```bash
cd server/api
go build ./...
go test ./internal/cache/...   -run "TestServersCache|TestBustServersCache" -short -count=1
go test ./internal/handler/...  -run "TestServersCacheNoSelect" -count=1
go test ./internal/handler/...  -run "TestAdmin(Create|Update|Delete)Server" -count=1   # confirm signature change didn't regress
go vet ./internal/cache/... ./internal/handler/...
gofmt -l internal/cache/servers_cache.go internal/handler/servers.go internal/handler/admin.go internal/handler/admin_test.go internal/handler/servers_test.go cmd/main.go
```

Expected: build exits 0; all listed tests pass (the Wave 0 servers-cache RED tests and `TestServersCacheNoSelect` turn GREEN); `gofmt -l` prints nothing.

## Known Stubs
None — the cache serves real data or falls through to the live DB; no placeholder/empty values flow to any response.

## Next Phase Readiness
- PERF-01 implementation is complete and committed; ready for 06-VERIFY once the orchestrator runs the toolchain validation above.
- Plan 07 (ctx threading through repos) will later add a `ctx` parameter to `ListActiveServers`/`ListServersForPlan`; `loadActiveServersCached` already calls them through `c.Context()`-aware wrappers, so the threading change is localized to the repo signatures + these two call sites.

## Self-Check: PASSED

- Files verified present: `server/api/internal/cache/servers_cache.go`, `server/api/internal/handler/servers.go`, `server/api/internal/handler/admin.go`, `server/api/cmd/main.go`, `.planning/phases/06-performance-scalability/06-03-SUMMARY.md`.
- Commits verified in `git log`: `ee9ba13` (Task 1), `0bfa8ca` (Task 2), `ca76216` (Task 3).
- Note: `go build`/`go test`/`gofmt` could not be run (sandbox-denied); functional GREEN confirmation is deferred to the orchestrator per "Action needed from the orchestrator" above.

---
*Phase: 06-performance-scalability*
*Completed: 2026-05-29*
