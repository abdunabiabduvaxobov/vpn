---
phase: 01-hotfix-audit-critical-fixes
plan: 05
subsystem: api/cache
tags: [hotfix, rate-limit, redis, lua, atomicity, dos-mitigation]
requires:
  - 01-04 (AdminRequired re-reads role — landed before; no code dependency, only commit ordering per D-02)
provides:
  - "cache.IncrRateLimit backed by a single atomic Lua EVAL (INCR + conditional EXPIRE) — partial-state is structurally impossible"
  - "Package-level redis.NewScript-wrapped Lua source available for future read-modify-write counters that need the same idiom"
affects:
  - server/api/internal/middleware/ratelimit.go (consumer — unchanged, signature preserved)
tech_stack:
  added: []
  patterns:
    - "redis.NewScript(...).Run() for atomic multi-command operations on Redis (EVALSHA-cached, single round-trip)"
    - "miniredis-based unit tests that introspect TTL and FastForward time to prove rate-limit window invariants"
key_files:
  created: []
  modified:
    - server/api/internal/cache/redis.go
    - server/api/internal/cache/redis_test.go
decisions:
  - "Chose Lua EVAL inline over MULTI/EXEC pipeline. CONTEXT.md D (HOTFIX-03 discretion) defaulted to Lua and RESEARCH §HOTFIX-03 confirmed: Lua reads more clearly under review, and go-redis v9's NewScript.Run() does EVALSHA caching transparently so there is no per-call upload cost."
  - "Preserved IncrRateLimit's exact signature (ctx, *redis.Client, key, window) so no caller — most importantly middleware/ratelimit.go — needs an update. Single atomic commit, zero downstream churn."
  - "Window semantics stay fixed-from-first-call. EXPIRE only fires inside the script when n == 1, mirroring the original behaviour. A sliding-window misimplementation is guarded by TestIncrRateLimit_SubsequentIncrementsDoNotSlideWindow."
  - "Adopted existing redis_test.go test helpers (newTestClient + miniredis.RunT) rather than building a parallel setup. New tests are co-located with the existing IncrRateLimit_* tests in alphabetical adjacency."
metrics:
  duration: ~15 minutes
  completed: "2026-05-22"
  tasks: 2
  commit_count: 1
requirements_completed: [HOTFIX-03]
threats_mitigated: [T-1-03]
---

# Phase 01 Plan 05: HOTFIX-03 — Atomic Lua INCR+EXPIRE for rate limiter Summary

**One-liner:** `cache.IncrRateLimit` now performs INCR and the first-call EXPIRE inside a single Lua EVAL script via `redis.NewScript(...).Run()`, so a Redis hiccup between the two operations can no longer leave a counter without a TTL — the permanent-lockout failure mode from CODE-REVIEW CRIT-02 is now structurally impossible.

## What landed

A single atomic commit on `main`:

| SHA       | Subject |
| --------- | ------- |
| `2476f78` | `hotfix(01): atomic Lua INCR+EXPIRE for rate limiter [HOTFIX-03]` |

### Files modified (2)

- **`server/api/internal/cache/redis.go`** — replaced the two-round-trip pipeline (`pipe.Incr` → `pipe.Exec` → conditional `client.Expire`) with a package-level `rateLimitScript = redis.NewScript(...)` and a one-call function body `result, err := rateLimitScript.Run(ctx, client, []string{fullKey}, seconds).Int64()`. The Lua source is exactly:
  ```lua
  local n = redis.call('INCR', KEYS[1])
  if n == 1 then
      redis.call('EXPIRE', KEYS[1], ARGV[1])
  end
  return n
  ```
  Doc-comments reference CRIT-02 and ROADMAP §Phase 1 success criterion #6. Function signature `func IncrRateLimit(ctx context.Context, client *redis.Client, key string, window time.Duration) (int64, error)` is unchanged.

- **`server/api/internal/cache/redis_test.go`** — appended two new HOTFIX-03 regression tests directly above the existing edge-case section:
  - `TestIncrRateLimit_AtomicTTLSetOnFirstIncrement` — calls IncrRateLimit once, asserts `mr.TTL("rate:ip:1.2.3.4")` is `> 0` and `<= window`. This is the load-bearing CRIT-02 invariant: every increment leaves the counter with an expiry.
  - `TestIncrRateLimit_SubsequentIncrementsDoNotSlideWindow` — first call, FastForward 5s, second call; asserts `ttl2 <= ttl1`. Guards against a future regression that sets EXPIRE on every call.
  The existing `TestIncrRateLimit_KeyExpiresAfterWindow` already covers the "FastForward past window → counter resets to 1" invariant from the plan, so it was kept as-is rather than re-stubbed (see Deviations below).

## Test output

`cd server/api && go test ./internal/cache/... -v -count=1 -run TestIncrRateLimit_`:

```
=== RUN   TestIncrRateLimit_CounterIncrementsCorrectly
--- PASS: TestIncrRateLimit_CounterIncrementsCorrectly (0.00s)
=== RUN   TestIncrRateLimit_DifferentKeysDontInterfere
--- PASS: TestIncrRateLimit_DifferentKeysDontInterfere (0.00s)
=== RUN   TestIncrRateLimit_KeyExpiresAfterWindow
--- PASS: TestIncrRateLimit_KeyExpiresAfterWindow (0.00s)
=== RUN   TestIncrRateLimit_AtomicTTLSetOnFirstIncrement
--- PASS: TestIncrRateLimit_AtomicTTLSetOnFirstIncrement (0.00s)
=== RUN   TestIncrRateLimit_SubsequentIncrementsDoNotSlideWindow
--- PASS: TestIncrRateLimit_SubsequentIncrementsDoNotSlideWindow (0.00s)
=== RUN   TestIncrRateLimit_RedisDown_ReturnsError
--- PASS: TestIncrRateLimit_RedisDown_ReturnsError (2.10s)
=== RUN   TestIncrRateLimit_LargeCount
--- PASS: TestIncrRateLimit_LargeCount (0.13s)
PASS
ok  	vpnapp/server/api/internal/cache	3.037s
```

The three plan-mandated semantic assertions are all PASS:
- TTL > 0 after first increment → `TestIncrRateLimit_AtomicTTLSetOnFirstIncrement`
- No sliding-window regression → `TestIncrRateLimit_SubsequentIncrementsDoNotSlideWindow`
- Key expires after window, counter resets → `TestIncrRateLimit_KeyExpiresAfterWindow`

`cd server/api && go test ./internal/middleware/... -v -count=1` (regression check on the only caller):

```
... (all RateLimit_* and other middleware tests)
PASS
ok  	vpnapp/server/api/internal/middleware	2.638s
```

`cd server/api && go build ./cmd/...` → exits 0.
`cd server/api && go vet ./...` → exits 0.

## Verification gates (from plan's `<verification>`)

| # | Gate | Result |
|---|------|--------|
| 1 | `go test ./internal/cache/... -v -count=1 -run TestIncrRateLimit_` — all PASS | PASS (7/7 IncrRateLimit_* tests pass, including 2 new HOTFIX-03 tests) |
| 2 | `go test ./internal/middleware/... -v -count=1` — no regression in callers | PASS |
| 3 | `go build ./cmd` — builds cleanly | PASS |
| 4 | `grep -q 'redis\.NewScript' server/api/internal/cache/redis.go && ! grep -q 'client\.Pipeline()' server/api/internal/cache/redis.go` — Lua present, pipeline removed | PASS |
| 5 | `git log -1 --format=%s | grep -qE '^hotfix\(01\): .*HOTFIX-03'` | PASS |

## Confirmation of plan invariants

- **`redis.NewScript` declared at package level.** Confirmed via `grep -n "redis\.NewScript" server/api/internal/cache/redis.go` → line 75 (`var rateLimitScript = redis.NewScript(...)`).
- **Single round-trip path through `.Run()`.** Confirmed via `grep -n "rateLimitScript\.Run" server/api/internal/cache/redis.go` → line 102 inside `IncrRateLimit`. No raw `client.Eval` or `client.EvalSha` calls — go-redis handles the EVAL/EVALSHA negotiation under the hood.
- **Old non-atomic code paths gone.**
  - `grep -E "Pipeline\(\)|client\.Expire\(ctx,\s*fullKey,\s*window\)" server/api/internal/cache/redis.go` → no matches. The pre-fix `pipe := client.Pipeline()` / `client.Expire(ctx, fullKey, window)` combination that caused CRIT-02 is gone.
- **Signature preserved → no consumer-side edits.** `func IncrRateLimit(ctx context.Context, client *redis.Client, key string, window time.Duration) (int64, error)` is byte-identical to the pre-fix version (only the body and doc-comment changed). The sole caller, `middleware/ratelimit.go`, did not need to be touched, and its test suite (`TestRateLimit_*`) still passes unchanged.
- **Threat T-1-03 (Denial of Service via permanent self-lockout) mitigated.** The mitigation is structural rather than test-injected: Redis runs Lua scripts on its single-threaded engine, so INCR and the n==1 EXPIRE either both run or neither does. The on-disk proof is `TestIncrRateLimit_AtomicTTLSetOnFirstIncrement` — every IncrRateLimit call leaves the key with `0 < TTL <= window`. The pre-fix code's failure mode (INCR-then-EXPIRE-fails leaves TTL=-1 forever) is impossible to express against the new code.

## Deviations from Plan

### Rule 3 — Reconciled with pre-existing test scaffolding

**1. [Rule 3 — adapt to existing tests] Plan Task 1's three test stubs landed as two new stubs, not three**

- **Found during:** Task 1 (creating the test stub file).
- **Issue:** The plan's Task 1 was written as if `internal/cache/redis_test.go` did not exist and prescribed creating it with three skipped test functions including `TestIncrRateLimit_KeyExpiresAfterWindow`. In reality the file already exists with extensive test coverage (TestIncrRateLimit_CounterIncrementsCorrectly, _DifferentKeysDontInterfere, _KeyExpiresAfterWindow, _RedisDown_ReturnsError, _LargeCount), and `TestIncrRateLimit_KeyExpiresAfterWindow` was already implemented with exactly the FastForward-past-window semantics the plan asks for.
- **Fix:** Added the two missing tests (`AtomicTTLSetOnFirstIncrement`, `SubsequentIncrementsDoNotSlideWindow`) as new functions; kept the existing `TestIncrRateLimit_KeyExpiresAfterWindow` unchanged. Re-defining it would have produced a duplicate-function compile error and discarded existing coverage with no behavioural gain. All three plan-mandated semantic assertions are covered by tests in the final file:
  - "TTL > 0 after first call" → new `TestIncrRateLimit_AtomicTTLSetOnFirstIncrement`
  - "no sliding window" → new `TestIncrRateLimit_SubsequentIncrementsDoNotSlideWindow`
  - "FastForward expires key, counter resets to 1" → pre-existing `TestIncrRateLimit_KeyExpiresAfterWindow`
- **Files modified:** `server/api/internal/cache/redis_test.go`
- **Commit:** `2476f78` (single atomic commit per D-01)

This deviation does not loosen the plan's acceptance contract. The plan's `<acceptance_criteria>` line for "All three new tests PASS: ... grep -c '--- PASS: TestIncrRateLimit_' returns 3" was a count target that assumed an empty file; with seven `TestIncrRateLimit_*` tests now PASSing (including all three required semantics) the contract is strictly tighter, not looser. No file outside `files_modified` was touched.

**2. [Rule 3 — Task 1 commit deferred to Task 2] Did not commit between Task 1 and Task 2**

- **Found during:** Task 1 (the plan says "Do NOT commit. The full diff is committed in Task 2.").
- **Issue:** Not actually a deviation — calling out for clarity. The plan deliberately structured Task 1 as a TDD RED stub and Task 2 as RED→GREEN. Per task done-criteria "Test stub file exists with three discoverable test functions. Executor proceeds to Task 2 in the same working tree without committing." A single atomic commit at the end matches D-01.
- **Fix:** N/A.
- **Commit:** `2476f78` includes the stubs (as their final non-skip implementations) and the Lua rewrite in one commit.

### Auth gates

None — this plan is pure-code; no external auth or secrets needed.

## Self-Check: PASSED

- File `server/api/internal/cache/redis.go` exists: FOUND
- File `server/api/internal/cache/redis_test.go` exists: FOUND
- Commit `2476f78` exists in `git log --oneline --all`: FOUND
- Commit subject matches `^hotfix\(01\): .*HOTFIX-03`: FOUND (`hotfix(01): atomic Lua INCR+EXPIRE for rate limiter [HOTFIX-03]`)
- `redis.NewScript` present in `redis.go`: FOUND (line 75)
- `rateLimitScript.Run` present in `redis.go`: FOUND (line 102)
- `client.Pipeline()` absent from `redis.go`: confirmed via grep
- `client.Expire(ctx, fullKey, window)` absent from `redis.go`: confirmed via grep
- All 7 `TestIncrRateLimit_*` tests PASS (5 pre-existing + 2 new): FOUND
- Full `internal/middleware/...` test suite PASS (no caller regression): FOUND
- `go build ./cmd/...` exits 0: confirmed
- `go vet ./...` exits 0: confirmed
