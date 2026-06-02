# Phase 08 — Deferred Items

Out-of-scope discoveries logged during plan execution (not fixed — they are
pre-existing or unrelated to the task that surfaced them).

## DEF-08-02-A — Pre-existing `TestMigrations019_020` ordering bug

- **Found during:** Plan 08-02, Task 2 (running the full migration test suite after adding migration 027).
- **File:** `server/api/migrations/migrations_test.go` (NOT touched by 08-02).
- **Symptom:** `TestMigrations019_020` fails with `apply 024_admin_panel_overhaul.sql: relation "lava_webhook_events" does not exist (SQLSTATE 42P01)`.
- **Root cause:** The test's apply loop iterates all `*.sql` files in lexicographic order but explicitly **skips** `019/020/021` (it applies them after the loop with per-stage assertions). Migration `024_admin_panel_overhaul.sql` references `lava_webhook_events`, a table created by `020_lava_payments.sql`. Because 020 is skipped in the loop, 024 is applied **before** 020 and fails. This is a test-harness ordering defect that predates 08-02.
- **Independent of 08-02:** The new `027_admin_search_index.sql` sorts after 024 and is never reached; the test does not reference 027. Removing 027 would not change this failure.
- **Why deferred:** SCOPE BOUNDARY — the failing assertion is in a file 08-02 did not modify. Fixing the loop (apply 019/020/021 inline in order, or apply 024+ only after the staged migrations) is a separate change.
- **Suggested owner:** A Phase 08 migration-hardening plan or a quick task; fix is to apply the staged 019/020/021 in numeric order within the loop rather than deferring them past 024.

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
