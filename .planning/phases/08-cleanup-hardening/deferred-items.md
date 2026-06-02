# Phase 08 — Deferred Items

Out-of-scope discoveries logged during execution (not fixed; pre-existing or
unrelated to the current task's changes).

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
