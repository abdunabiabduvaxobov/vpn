---
phase: 1
slug: hotfix-audit-critical-fixes
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-22
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution. Source: `01-RESEARCH.md` §Validation Architecture.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (stdlib `testing`) — Go 1.22 |
| **Config file** | None (Go convention) — `*_test.go` siblings per package |
| **Quick run command** | `cd server/api && go test ./internal/middleware/... ./internal/handler/... ./internal/cache/... ./internal/config/... ./internal/repository/... -count=1` |
| **Full suite command** | `cd server/api && go test ./... -race -count=1` |
| **Estimated runtime** | ~30s quick / ~60s full (race) |

---

## Sampling Rate

- **After every task commit:** Run `go test` for the touched package only (e.g., `go test ./internal/cache/... -count=1`) — sub-5s iteration loop.
- **After every plan wave (all 8 hotfix commits landed):** Run `cd server/api && go test ./... -race -count=1` — full suite green.
- **Before `/gsd-verify-work` / `v2.2.0-hotfix` tag:** Full suite green PLUS the manual staging smoke checklist (see below).
- **Max feedback latency:** 60 seconds (full suite with race detector).

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 1-W0-01 | 00 | 0 | HOTFIX-01 | T-1-01 | Wave 0 test stub | unit | `test -f server/api/internal/repository/user_repo_subscription_test.go` | ❌ W0 | ⬜ pending |
| 1-W0-02 | 00 | 0 | HOTFIX-03 | T-1-03 | Wave 0 test stub | unit | `test -f server/api/internal/cache/redis_test.go` | ❌ W0 | ⬜ pending |
| 1-W0-03 | 00 | 0 | HOTFIX-04 | T-1-04 | Wave 0 test stub | integration | `test -f server/api/internal/handler/errorhandler_test.go` | ❌ W0 | ⬜ pending |
| 1-W0-04 | 00 | 0 | HOTFIX-06 | T-1-06 | Wave 0 test stub | unit | `test -f server/api/cmd/createadmin/main_test.go` | ❌ W0 | ⬜ pending |
| 1-W0-05 | 00 | 0 | HOTFIX-07 | T-1-07 | Wave 0 smoke | shell | `test -f server/api/scripts/smoke_test_session_index.sh` | ❌ W0 | ⬜ pending |
| 1-06-01 | 06 | 1 | HOTFIX-06 | T-1-06 / V6.2.3 | `-password` argv rejected; stdin path works; seed tier='free' | shell + unit | `cd server/api && go test ./cmd/createadmin/... -v -run TestCreateAdmin` | ❌ W0 | ⬜ pending |
| 1-08-01 | 08 | 2 | HOTFIX-08 | T-1-08 / V5 | Exit 1 + one aggregated log line on missing required env | shell + unit | `cd server/api && go test ./internal/config/... -v -run TestRequireEnv` | 🟡 extend | ⬜ pending |
| 1-04-01 | 04 | 3 | HOTFIX-04 | T-1-04 / V7.4.1 | 5xx body scrubbed to `{"error":"internal server error","request_id":"<uuid>"}`; `X-Request-ID` echoed | integration | `cd server/api && go test ./internal/handler/... -v -run TestErrorHandler` | ❌ W0 | ⬜ pending |
| 1-02-01 | 02 | 4 | HOTFIX-02 | T-1-02 / V4 | Demoted admin gets 403 on very next request with same JWT | integration | `cd server/api && go test ./internal/middleware/... -v -run TestAdminRequired_DemotionTakesEffect` | 🟡 extend | ⬜ pending |
| 1-03-01 | 03 | 5 | HOTFIX-03 | T-1-03 / V5 | Every `IncrRateLimit` leaves `TTL > 0`; never -1 | unit (miniredis) | `cd server/api && go test ./internal/cache/... -v -run TestIncrRateLimit` | ❌ W0 | ⬜ pending |
| 1-05-01 | 05 | 6 | HOTFIX-05 | T-1-05 / V3 | Failed insert during rotation rolls back delete; old session preserved | integration (sqlite) | `cd server/api && go test ./internal/handler/... -v -run TestRefreshToken_Rollback` | 🟡 extend | ⬜ pending |
| 1-01-01 | 01 | 7 | HOTFIX-01 | T-1-01 | Scheduler downgrades expired pro user; `payment.go` diff empty (D-07 invariant — PHASE-WIDE) | unit + diff | `cd server/api && go test ./internal/repository/... -v -run TestDowngradeExpiredSubscriptions && BASE=$(git merge-base HEAD main) && [ $(git diff $BASE..HEAD -- server/api/internal/handler/payment.go \| wc -l) -eq 0 ]` | ❌ W0 | ⬜ pending |
| 1-07-01 | 07 | 8 | HOTFIX-07 | T-1-07 / V3 | `EXPLAIN` shows `Index Scan using idx_sessions_refresh_token_hash_unique` (not `Seq Scan`) | DB smoke | `bash server/api/scripts/smoke_test_session_index.sh` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

*Wave numbers in this table follow the CONTEXT.md D-02 risk-first ordering (06 → 08 → 04 → 02 → 03 → 05 → 01 → 07). Planner is free to assign different wave numbers in PLAN.md frontmatter as long as ordering is preserved.*

---

## Wave 0 Requirements

- [ ] `server/api/internal/repository/user_repo_subscription_test.go` — HOTFIX-01 regression tests for `DowngradeExpiredSubscriptions`
- [ ] `server/api/internal/cache/redis_test.go` — HOTFIX-03 atomic INCR+EXPIRE via miniredis (`github.com/alicebob/miniredis/v2`)
- [ ] `server/api/internal/handler/errorhandler_test.go` — HOTFIX-04 scrub + `X-Request-ID` behavior via Fiber `app.Test()`
- [ ] `server/api/cmd/createadmin/main_test.go` — HOTFIX-06 stdin path + seed-tier assertion (sqlite test DB)
- [ ] `server/api/scripts/smoke_test_session_index.sh` — HOTFIX-07 EXPLAIN check against real Postgres (sqlite doesn't model `Index Scan` output)
- [ ] **(Optional)** Promote shared sqlite-in-memory DB fixture to `server/api/internal/repository/testdb.go` if duplication forms across HOTFIX-01/02/05 tests (existing pattern in `subscription_repo_test.go`).

Existing test files (extend, do not recreate):
- `server/api/internal/middleware/admin_test.go` — extended for HOTFIX-02
- `server/api/internal/handler/auth_test.go` — extended for HOTFIX-05
- `server/api/internal/config/config_test.go` — extended for HOTFIX-08 (if file exists; else create)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `v2.2.0-hotfix` staging smoke (all 8 hotfixes end-to-end) | HOTFIX-01..08 | Requires a running staging stack + Postgres + Redis + production-like env vars; can't be expressed as `go test` | Execute the 10-step Cross-Cutting Staging Smoke Checklist (below). Every step MUST pass green before the tag is pushed. |
| HOTFIX-07 EXPLAIN on real Postgres | HOTFIX-07 | sqlite's `EXPLAIN` doesn't emit `Index Scan using <name>` strings — only real PG validates the audit's success criterion #3 | `psql $STAGING_DATABASE_URL -c "EXPLAIN SELECT * FROM sessions WHERE refresh_token_hash='deadbeef'"` — assert output contains `Index Scan using idx_sessions_refresh_token_hash_unique` |
| HOTFIX-08 process-level exit code | HOTFIX-08 | `os.Exit(1)` cannot be unit-tested in-process; needs subprocess + exit-code assertion (covered by shell test, but also smoked manually on staging) | `JWT_SECRET= ./vpn-api 2>&1; echo "exit=$?"` — assert `exit=1` and single JSON log line with `missing` field |

---

## Cross-Cutting Staging Smoke Checklist

**MUST pass green before pushing the `v2.2.0-hotfix` tag.** (Source: `01-RESEARCH.md` §Validation Architecture.)

1. `JWT_SECRET= ./vpn-api 2>&1 | head -1 | jq -e '.missing'` — exit 1, JSON log with `missing` field. **(HOTFIX-08)**
2. `curl -i -XPOST -H 'Content-Type: application/json' -d '{"refresh_token":"not-a-real-token"}' http://staging/api/v1/auth/refresh | grep -q "request_id"` — response body has `request_id`; no `gorm:|bcrypt:|pq:` strings. Endpoint chosen because it hits the rotation transaction (HOTFIX-05) and returns a scrubbed 500 once HOTFIX-04 lands. If the endpoint returns 401 instead of 500 (validation rejects the malformed token early), see plan 09 Task 4 for the alternative trigger. **(HOTFIX-04)**
3. `curl -i -XPOST -H 'Content-Type: application/json' -H 'X-Request-ID: smoke-2' -d '{"refresh_token":"not-a-real-token"}' http://staging/api/v1/auth/refresh | grep -q "smoke-2"` — pre-set header echoed in both response header AND response body's `request_id` field. Uses the same auth/refresh endpoint as step 2 to keep the smoke surface minimal (no debug-only routes). **(HOTFIX-04)**
4. Log in as admin on staging admin panel; in psql: `UPDATE users SET role='user' WHERE id=...`; refresh admin page → expect 403 within the current access-token lifetime. **(HOTFIX-02)**
5. Hit `/api/v1/auth/guest` 35× in <1min → expect 429 around request 31. `redis-cli TTL rate:ip:<your_ip>` returns a positive integer. **(HOTFIX-03)**
6. `curl -X POST http://staging/api/v1/auth/refresh -d '{"refresh_token":"<valid>"}'` succeeds; `SELECT count(*) FROM sessions WHERE user_id=?` returns exactly 1. **(HOTFIX-05)**
7. Seed a user with `subscription_tier='pro', subscription_expires_at=NOW() - 1 hour`. Wait ≤90s for scheduler. `SELECT subscription_tier FROM users WHERE id=?` returns `free`. **(HOTFIX-01)**
8. `psql -c "EXPLAIN SELECT * FROM sessions WHERE refresh_token_hash='x'"` contains `Index Scan using idx_sessions_refresh_token_hash_unique`. **(HOTFIX-07)**
9. `./createadmin -email=smoke-admin@example.com` (no `-password` flag) prompts for password, accepts input, inserts row with `role='admin'` and `subscription_tier='free'`. `./createadmin -password=anything` errors with `not defined`. **(HOTFIX-06)**
10. Regression: `/api/v1/auth/guest`, `/api/v1/auth/admin-login`, `/api/v1/subscription`, `/api/v1/servers` all return their expected 200 / JSON shape. No Phase-1 fix introduced a regression.

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all ❌ MISSING references in the Per-Task Verification Map
- [ ] No watch-mode flags in any test command
- [ ] Feedback latency < 60s for full suite
- [ ] `nyquist_compliant: true` set in frontmatter once planner has assigned every task to a row above

**Approval:** pending
