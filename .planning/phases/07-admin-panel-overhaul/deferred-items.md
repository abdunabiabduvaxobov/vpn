## 07-03: tunnel cmd/tunnel link error (pre-existing, out of scope)

`cd server/tunnel && go build ./cmd/tunnel/` fails at the LINK step with:
`link: github.com/sagernet/sing/common/control: invalid reference to net.errNoSuchInterface`

- Reproduces on the clean base (stashing the 07-03 changes does not fix it) — it is a toolchain/indirect-dependency incompatibility (local go1.26.1 linking an older xray-core `sagernet/sing` dependency), NOT caused by this plan.
- The tunnel `internal` package (where heartbeat.go lives) compiles cleanly: `go build ./internal/` exit 0, `go vet ./...` clean.
- Resolution belongs to a dependency-bump / toolchain-pin task, not Phase 7. The orchestrator's post-wave validation runs in the CI go version where this link issue does not occur.

## [07-07] internal/repository/TestCtxCancelAbortsQuery requires Docker
- Pre-existing testcontainers test that t.Fatalf-s when Docker is absent (not introduced by 07-07; unrelated to system controls).
- Passes under `-short` and with Docker. Orchestrator post-wave Docker validation covers it.
