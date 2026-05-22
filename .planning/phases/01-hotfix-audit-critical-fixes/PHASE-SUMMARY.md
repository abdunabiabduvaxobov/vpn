---
phase: 01-hotfix-audit-critical-fixes
status: complete-with-waiver
started: 2026-05-22
finished: 2026-05-22
tag: v2.2.0-hotfix
tag_commit: eea6e25273c6907d55c4e40bc808ed98621a152d
requirements: [HOTFIX-01, HOTFIX-02, HOTFIX-03, HOTFIX-04, HOTFIX-05, HOTFIX-06, HOTFIX-07, HOTFIX-08]
threats_mitigated_on_disk: [T-1-01, T-1-02, T-1-03, T-1-04, T-1-05, T-1-06, T-1-07, T-1-08]
threats_verified_on_staging: []
---

# Phase 1 — Hotfix Audit Critical Fixes

Tag `v2.2.0-hotfix` pushed to origin at commit `eea6e25`. All 8 audit Critical/High findings
landed atomically per CONTEXT D-01 and committed in D-02 order. Full Go test suite passes
with `-race`. **Staging smoke was waived by operator** — see `01-09-SUMMARY.md` and
`01-09-SMOKE-RESULTS.md`.

## What shipped (D-02 commit order)

| # | Commit | HOTFIX | Threat | One-liner |
|---|--------|--------|--------|-----------|
| 1 | `63fde77` | HOTFIX-06 | T-1-06 (InfoDisclosure) | `createadmin` reads password from stdin via `golang.org/x/term`; seeds `tier=free` (was hardcoded `"ultimate"`) |
| 2 | `af92b63` | HOTFIX-08 | T-1-08 (Tampering)      | Fail-fast env validator scans `JWT_SECRET`, `DATABASE_URL`, `REDIS_URL`, `TUNNEL_VLESS_UUID` in one pass; `logger.Fatal` on missing |
| 3 | `b54b727` | HOTFIX-04 | T-1-04 (InfoDisclosure) | 5xx response body scrubbed to `{"error":"internal server error","request_id":"<uuid>"}`; `requestid` middleware wired before `recover` |
| 4 | `204b80f` | HOTFIX-02 | T-1-02 (EoP)            | `AdminRequired(db *gorm.DB)` re-reads user role from Postgres on every admin request (no Redis cache, no TTL) |
| 5 | `2476f78` | HOTFIX-03 | T-1-03 (DoS)            | Rate-limit `INCR` + `EXPIRE` collapsed into single atomic `redis.NewScript().Run()` Lua call |
| 6 | `2f6d86b` | HOTFIX-05 | T-1-05 (Tampering)      | Refresh-token rotation wrapped in `db.Transaction(func(tx *gorm.DB) error{...})`; rollback on any step |
| 7 | `15073e4` | HOTFIX-01 | T-1-01 (Repudiation)    | Regression tests for subscription downgrade scheduler (production code already correct; webhook write deferred to Phase 3) |
| 8 | `192dd92` | HOTFIX-07 | T-1-07 (DoS)            | `CREATE UNIQUE INDEX CONCURRENTLY idx_sessions_refresh_token_hash_unique` + dedupe step |

## Plans landed

- `01-01-SUMMARY.md` — HOTFIX-06 (createadmin stdin + tier=free)
- `01-02-SUMMARY.md` — HOTFIX-08 (env validator)
- `01-03-SUMMARY.md` — HOTFIX-04 (scrub 5xx + X-Request-ID)
- `01-04-SUMMARY.md` — HOTFIX-02 (AdminRequired re-reads role)
- `01-05-SUMMARY.md` — HOTFIX-03 (atomic Lua INCR+EXPIRE)
- `01-06-SUMMARY.md` — HOTFIX-05 (transactional refresh rotation)
- `01-07-SUMMARY.md` — HOTFIX-01 (regression test, production code intentionally untouched)
- `01-08-SUMMARY.md` — HOTFIX-07 (UNIQUE index migration 017)
- `01-09-SUMMARY.md` — final integration (CI gate PASS; smoke WAIVED; tag pushed)

## Verification matrix

| Check | Method | Result |
|-------|--------|--------|
| 8 hotfix commits present, D-02 order | `git log --format=%s --reverse \| grep '^hotfix(01)'` | PASS |
| Full Go suite | `go test ./... -race -count=1` | PASS (cache 10.4s, handler 7.3s, middleware 5.0s, scheduler 4.0s) |
| Build | `go build ./cmd ./cmd/createadmin` | PASS |
| Annotated tag exists locally | `git cat-file -t v2.2.0-hotfix` | `tag` |
| Tag pushed to origin | `git ls-remote --tags origin v2.2.0-hotfix` | `47f5a5c22b9c... refs/tags/v2.2.0-hotfix` |
| Migration 017 syntax-valid | `psql --dry-run` not run; sql parses on inspection | PASS (on-disk) |
| **Staging smoke** | 10 checks against live infra | **WAIVED** — not run |

## Threats — on-disk vs live verification

All eight STRIDE threats classified High are **mitigated on disk** (test or code-evidence
in the SUMMARY for each plan). **None has been verified on live infrastructure** because
staging smoke was waived. The follow-up tracker should record this as a deferred
verification debt before any paying user touches `v2.2.0-hotfix`.

## Open carry-forward for next phase

1. **HOTFIX-01 webhook write** is intentionally deferred to Phase 3 (lava.top webhook).
   The Phase 1 commit added regression tests only; the production write that populates
   `users.subscription_expires_at` does not yet exist. Confirmed safe today because
   the existing Stripe handler is being deleted in Phase 8 and there are zero paying
   Stripe users.
2. **Migration 017** must still be applied to any pre-existing Postgres instance manually
   (`psql -f server/api/migrations/017_*.sql`) — `docker-entrypoint-initdb.d` only fires
   on a fresh data dir.
3. **Live-env verification debt** for all 8 hotfixes (smoke WAIVED).

## Cleanup deltas observed during execution

- `server/api/Dockerfile` was modified during HOTFIX-06 (not listed in plan's
  `files_modified`); reasonable extension since the command's invocation surface changed.
- `.claude/settings.local.json` drift was stashed by several executor agents during
  worktree reset; not material to phase outcome.
- Three executor worktrees lingered locked after merge-back (handled in cleanup).
- GitHub repo redirected at push time to `abdunabiabduvaxobov/vpn`; remote URL not
  updated locally (push succeeded via redirect).

## What Phase 2 inherits

- `middleware.AdminRequired` signature now takes `(db *gorm.DB)` — Phase 2's new admin
  routes must pass `db` explicitly.
- Refresh rotation is transactional — Phase 2's SSO token-issuance helper should use
  the same `db.Transaction` pattern so SSO refresh paths are equally atomic.
- `sessions.refresh_token_hash` has a UNIQUE btree index — Phase 2 SSO sessions
  inheriting the `sessions` table benefit automatically.
- All Stripe code remains (deletion is Phase 8); env validator marks Stripe vars as
  optional-with-warn so Phase 2 staging deploys won't be blocked by missing Stripe keys.
