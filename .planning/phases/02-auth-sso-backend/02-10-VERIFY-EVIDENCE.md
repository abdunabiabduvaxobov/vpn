# 02-10 Verify Evidence (IN-01 / IN-02 / IN-03)

Plan: `02-10` — REVIEW.md INFO-level gap closure
Base commit: `b886f67` ("docs(02-08): plan summary for SSO security gap-closure")
Worktree branch: `worktree-agent-a0b5f81c084b12189`

## Three findings closed

| Finding | Target file | Change | Commit |
|---|---|---|---|
| IN-01 | `server/api/go.mod` line 3 | `go 1.25.0` → `go 1.22.0` (CLAUDE.md locked stack) | `6c3bd30` |
| IN-02 | `server/api/internal/handler/auth_test.go` `seedAdminUser` | `'ultimate'` → `'free'` + invariant comment | `a5f870c` |
| IN-03 | `server/api/migrations/018_add_sso_columns.sql` header | Added 19-line transactional-DDL / golang-migrate comment block | `f076d45` |

## Acceptance-criteria snapshot

### IN-01 (go.mod)

```
$ head -5 server/api/go.mod
module vpnapp/server/api

go 1.22.0
```

- `head -5 go.mod | grep -c '^go 1.25'` → 0 ✓
- `head -5 go.mod | grep -cE '^go 1\.(22|23|24)'` → 1 ✓

### IN-02 (seedAdminUser)

```
$ grep -n "'Admin', 'admin'" server/api/internal/handler/auth_test.go
174:		 VALUES (?, ?, 'Admin', 'admin', 'free')`,
```

- `grep -c "'Admin', 'admin', 'free'" auth_test.go` → 1 ✓
- `grep -c "'Admin', 'admin', 'ultimate'" auth_test.go` → 0 ✓

### IN-03 (migration 018)

```
$ grep -c golang-migrate server/api/migrations/018_add_sso_columns.sql
2
$ grep -c "transactional-DDL\|auto-rollback\|ROLLBACK" server/api/migrations/018_add_sso_columns.sql
5
$ grep -c '^BEGIN;'  server/api/migrations/018_add_sso_columns.sql
1
$ grep -c '^COMMIT;' server/api/migrations/018_add_sso_columns.sql
1
$ grep -c apple_user_id server/api/migrations/018_add_sso_columns.sql
3
```

All five acceptance probes pass.

## Test / build sweep

`go mod tidy`, `go vet`, `go build`, and `go test` are NOT runnable inside this parallel-executor sandbox (the runtime denies all `go ...` subcommands except `go version`; see SUMMARY "Issues Encountered"). The orchestrator's post-merge build/test pipeline runs the full Phase 2 suite once all wave-2 worktrees are reintegrated. Last-known-green baseline at commit `b886f67`:

- Phase 2 test suite at `b886f67` (recorded in `02-08-SUMMARY.md`): 195 tests passing
  - handler 110
  - repository 43
  - auth/apple 8
  - auth/google 5
  - middleware 29
- `go build ./...`: clean at `b886f67`
- `go vet ./...`: clean at `b886f67`
- race detector: clean at `b886f67`

Plan 02-10's three changes are surface-level:

- `go.mod`: lowering the directive from 1.25.0 to 1.22.0 is permissive (Go is forward-compatible on module directives); no source-level Go 1.23+ feature is in this codebase per REVIEW.md IN-01 analysis.
- `auth_test.go`: a string-literal change inside an INSERT inside a test helper; the production handler never reads the tier on AdminLogin (REVIEW.md IN-02 confirms `role='admin'` is the only branch).
- `018_add_sso_columns.sql`: SQL comment lines only; zero DDL change.

None of the three changes can plausibly regress the build or test suite. The orchestrator's post-merge sweep is the authoritative gate.

## Phase 2 follow-up findings — final status

After plans 02-08, 02-09, and 02-10 all land, the nine REVIEW.md / VERIFICATION.md follow-ups are closed:

| Finding | Plan | Status |
|---|---|---|
| CR-01 (empty-sub guards) | 02-08 | closed (`db62f25`) |
| CR-02 (Step B transactional) | 02-08 | closed (`1045df8`) |
| WR-01 (parseGuestJWT role allow-list) | 02-08 | closed (`4e954f7`) |
| WR-02 (Logout TTL boundary) | 02-08 | closed (`6befbe4`) |
| WR-03 (free Subscription row) | 02-08 | closed (`b304fc1`) |
| WR-04 (user_repo.go race / null email) | 02-09 | (wave-2 sibling — separate worktree) |
| IN-01 (go.mod directive) | 02-10 | closed (`6c3bd30`) |
| IN-02 (seedAdminUser tier) | 02-10 | closed (`a5f870c`) |
| IN-03 (migration 018 comment) | 02-10 | closed (`f076d45`) |

---
*Generated: 2026-05-22 by plan 02-10 executor*
