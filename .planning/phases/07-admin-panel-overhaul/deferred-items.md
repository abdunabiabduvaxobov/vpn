## 07-03: tunnel cmd/tunnel link error (pre-existing, out of scope)

`cd server/tunnel && go build ./cmd/tunnel/` fails at the LINK step with:
`link: github.com/sagernet/sing/common/control: invalid reference to net.errNoSuchInterface`

- Reproduces on the clean base (stashing the 07-03 changes does not fix it) — it is a toolchain/indirect-dependency incompatibility (local go1.26.1 linking an older xray-core `sagernet/sing` dependency), NOT caused by this plan.
- The tunnel `internal` package (where heartbeat.go lives) compiles cleanly: `go build ./internal/` exit 0, `go vet ./...` clean.
- Resolution belongs to a dependency-bump / toolchain-pin task, not Phase 7. The orchestrator's post-wave validation runs in the CI go version where this link issue does not occur.

## [07-07] internal/repository/TestCtxCancelAbortsQuery requires Docker
- Pre-existing testcontainers test that t.Fatalf-s when Docker is absent (not introduced by 07-07; unrelated to system controls).
- Passes under `-short` and with Docker. Orchestrator post-wave Docker validation covers it.

## [07-08] internal/repository/TestCtxCancelAbortsQuery still Docker-gated (re-confirmed)
- Same pre-existing test (from phase 06, `ctx_cancel_test.go`, untouched by 07-08). It `t.Fatalf`s without Docker rather than `t.Skip`ing.
- Out of scope for 07-08 (webhook replay): not in this plan's modified files; fails only in the non-`-short` repository run on a Docker-less host. The new `TestWebhookReplayIdempotent` itself SKIPs cleanly without Docker as designed.

## [07-09] internal/repository/TestCtxCancelAbortsQuery still Docker-gated (re-confirmed)
- Same pre-existing phase-06 test (`ctx_cancel_test.go`), untouched by 07-09. `t.Fatalf`s with "Cannot connect to the Docker daemon" on a Docker-less host instead of `t.Skip`ing.
- Out of scope for 07-09 (deps-health): this plan's repository change is a pure additive `ListServerHealth` SELECT; the full repo suite passes under `-short`. New `TestDepsHealth` (handler pkg, SQLite + miniredis) needs no Docker and passes.
