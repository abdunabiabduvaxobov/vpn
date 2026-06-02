# Phase 08 — Deferred Items

Out-of-scope discoveries logged during plan execution (not fixed — they are
pre-existing or unrelated to the task that surfaced them).

## DEF-08-W2-A — Pre-existing `TestFlushHeartbeatsCollapsesNto1` cache failure

- **Found during:** Wave 2 post-merge test run (orchestrator).
- **File:** `server/api/internal/cache/heartbeat_cache_test.go` (NOT touched by any Phase 08 plan).
- **Symptom:** `TestFlushHeartbeatsCollapsesNto1` fails at `heartbeat_cache_test.go:130` with `SMembers after flush: ERR no such key`.
- **Scope:** PRE-EXISTING + ENVIRONMENTAL — confirmed failing at base `2122b84`; `git diff 2122b84..HEAD -- internal/cache/` is empty (no wave touched the package). The `ERR no such key` on `SMembers` is a miniredis/test-double quirk (real Redis returns an empty set for a missing key), not a code defect.
- **Why deferred:** unrelated to any Phase 08 plan scope. Fix is to seed the key before the SMembers assertion or upgrade/replace the Redis test double.
- **Suggested owner:** a cache/test-hardening task.

## DEF-08-W1-A — Pre-existing `TestPerfIndexes` plan_id seed failure

- **Found during:** Wave 1 post-merge test run (orchestrator).
- **File:** `server/api/migrations/perf_indexes_test.go` (NOT touched by any Phase 08 plan).
- **Symptom:** `TestPerfIndexes` fails with `null value in column "plan_id" of relation "users" violates not-null constraint (SQLSTATE 23502)` while seeding a test user.
- **Scope:** PRE-EXISTING — confirmed failing at base `2122b84` before any Wave 1 commit. Related to the post-019 `plan_id` NOT-NULL constraint (cf. memory `guest-login-planid-blocker`); the test's seed helper inserts a user without setting `plan_id`.
- **Why deferred:** unrelated to Wave 1 scope; the seed helper predates Phase 08. Fix is to set a system `plan_id` (or a valid plan FK) in the test's user seed before insert.
- **Suggested owner:** a migration/test-hardening task alongside DEF-08-02-A (same migrations test package).

## DEF-08-02-A — Pre-existing `TestMigrations019_020` ordering bug

- **Found during:** Plan 08-02, Task 2 (running the full migration test suite after adding migration 027).
- **File:** `server/api/migrations/migrations_test.go` (NOT touched by 08-02).
- **Symptom:** `TestMigrations019_020` fails with `apply 024_admin_panel_overhaul.sql: relation "lava_webhook_events" does not exist (SQLSTATE 42P01)`.
- **Root cause:** The test's apply loop iterates all `*.sql` files in lexicographic order but explicitly **skips** `019/020/021` (it applies them after the loop with per-stage assertions). Migration `024_admin_panel_overhaul.sql` references `lava_webhook_events`, a table created by `020_lava_payments.sql`. Because 020 is skipped in the loop, 024 is applied **before** 020 and fails. This is a test-harness ordering defect that predates 08-02.
- **Independent of 08-02:** The new `027_admin_search_index.sql` sorts after 024 and is never reached; the test does not reference 027. Removing 027 would not change this failure.
- **Why deferred:** SCOPE BOUNDARY — the failing assertion is in a file 08-02 did not modify. Fixing the loop (apply 019/020/021 inline in order, or apply 024+ only after the staged migrations) is a separate change.
- **Suggested owner:** A Phase 08 migration-hardening plan or a quick task; fix is to apply the staged 019/020/021 in numeric order within the loop rather than deferring them past 024.

## From plan 08-05 (stripe removal + durable fence)

### `TestGenerateTokens_RefreshIsOpaque` RED (sibling Wave-2 plan, not 08-05)

- **Found during:** plan 08-05 full changed-package test run.
- **File:** `server/api/internal/handler/auth_opaque_refresh_test.go` (NOT touched by 08-05).
- **Symptom:** refresh token is still a JWT (`eyJ...`), so the HARD-03 opaque-token
  assertion (`^[A-Za-z0-9_-]{43}$`) is RED.
- **Scope:** PRE-EXISTING RED test owned by the HARD-03 plan (refresh tokens →
  32-byte opaque). 08-05 (stripe removal) touches no token-generation path. Goes
  GREEN when HARD-03 lands. Out of scope for 08-05.

### Redis-dependent handler tests fail with `connection refused`

- **Found during:** plan 08-05 full changed-package test run.
- **Symptom:** `dial tcp 127.0.0.1:...: connect: connection refused` and
  `database connection is nil` in handler tests that need a live Redis/Postgres.
- **Scope:** ENVIRONMENTAL — no local Redis/Postgres in this worktree's `go test`.
  Not caused by 08-05 (stripe removal touches no Redis/DB path). CI provides both.
  The stripe-relevant handler tests (Admin/Payment/Webhook) and `TestNoStripeReferences`
  all pass `ok`. Out of scope for 08-05.

## 08-01 (Wave 0 test infra)

### Pre-existing tunnel test-binary linker failure (xray-core / sagernet/sing)

- **Discovered:** plan 08-01, Task 2 (server_reload_test.go).
- **Symptom:** `go test ./internal/...` and `go build ./...` (cmd/tunnel) in
  `server/tunnel` fail at the LINK stage with:

  ```
  link: github.com/sagernet/sing/common/control: invalid reference to net.errNoSuchInterface
  ```

- **Scope:** PRE-EXISTING. Reproduces on an untouched test
  (`TestAWGServerConfigValidateHappyPath`) and is unrelated to the new
  `server_reload_test.go`. The tunnel `internal` library and the new test
  source compile cleanly (`go build ./internal/...` and `go vet ./internal/...`
  both pass); only the final link of the test/cmd binary fails.
- **Likely cause:** the local Go toolchain's internal linker rejects a private
  symbol reference (`net.errNoSuchInterface`) in the `sagernet/sing` transitive
  dep pulled by `xtls/xray-core`. This is a toolchain/dep-version incompatibility,
  not a code defect.
- **Impact on 08-01:** the tunnel-side RED tests (`TestTunnelServer_ReloadClients`
  skip-gate + `TestBuildXRayConfig_ClientListMatchesInputUUIDs`) cannot be
  EXECUTED in this environment until the linker issue is resolved, but they
  compile and are committed. They will run once the tunnel test binary links.
- **Recommended fix (separate task):** bump/pin the Go toolchain or the
  `sagernet/sing` / `xtls/xray-core` versions until the tunnel test binary links;
  verify with `cd server/tunnel && go test ./internal/...`.
- **Not fixed here:** out of scope for a Wave 0 test-seeding plan; touching the
  toolchain/dep graph is an architectural change (Rule 4) owned by a dedicated
  plan.
