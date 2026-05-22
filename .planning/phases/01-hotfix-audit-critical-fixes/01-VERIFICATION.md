---
phase: 01-hotfix-audit-critical-fixes
verified: 2026-05-22T12:00:00Z
status: human_needed
score: 7/8 must-haves verified on disk; staging verification waived by operator
overrides_applied: 0
gaps:
  - truth: "All 10 smoke steps from VALIDATION.md pass end-to-end on staging"
    status: partial
    reason: "Operator explicitly waived staging smoke on 2026-05-22. All 10 rows in SMOKE-RESULTS.md are marked WAIVED, not executed. RESEARCH §9 called staging mandatory for HOTFIX-02 (admin demotion) and HOTFIX-05 (refresh transaction), which exercise live DB+Redis behavior that sqlite/miniredis unit tests cannot fully prove. Tag was pushed on unit-test-only verification."
    artifacts:
      - path: ".planning/phases/01-hotfix-audit-critical-fixes/01-09-SMOKE-RESULTS.md"
        issue: "All 10 smoke rows say 'WAIVED / not run on staging'. No live system verification was performed."
    missing:
      - "Execute SMOKE-RESULTS.md steps 1-10 against a live staging or production-equivalent environment before any paying user touches v2.2.0-hotfix"
      - "Particular priority: step 4 (HOTFIX-02 admin demotion takes effect immediately) and step 6 (HOTFIX-05 refresh leaves exactly one session row)"
human_verification:
  - test: "SMOKE 1/10 — HOTFIX-08 env validator fails fast: run 'JWT_SECRET= ./vpn-api 2>&1 | head -1 | jq -e .missing' on staging; process must exit 1 with a single JSON log line listing JWT_SECRET"
    expected: "Process exits 1; head -1 output is valid JSON with a non-empty 'missing' array containing 'JWT_SECRET'"
    why_human: "Requires a running vpn-api binary against a real environment; cannot be verified without staging infrastructure"
  - test: "SMOKE 2/10 — HOTFIX-04 5xx scrub: POST a malformed refresh token to /api/v1/auth/refresh; verify response body is exactly {error: 'internal server error', request_id: '<uuid>'} and contains no 'pq:', 'gorm:', or 'bcrypt:' substrings"
    expected: "HTTP 500 body has exactly two keys (error + request_id); no internal error text leaks to the client; X-Request-ID header is present"
    why_human: "Requires a running staging API endpoint accessible via curl"
  - test: "SMOKE 3/10 — HOTFIX-04 X-Request-ID echo: send request with 'X-Request-ID: smoke-2' header; verify the same value appears in both the response header and the response body's request_id field"
    expected: "Response header X-Request-ID: smoke-2 and body {request_id: 'smoke-2'}"
    why_human: "Requires live staging endpoint"
  - test: "SMOKE 4/10 — HOTFIX-02 admin demotion: mint admin JWT, confirm /admin/users returns 200, run 'UPDATE users SET role=user WHERE email=<admin>', issue the same request with the SAME JWT within the token's 5-minute lifetime, expect 403 not 200"
    expected: "HTTP 403 on the second call, proving AdminRequired re-reads from DB rather than trusting the JWT claim"
    why_human: "Requires a live Postgres database on staging and a real HTTP round-trip within the same JWT window"
  - test: "SMOKE 5/10 — HOTFIX-03 rate-limit TTL: hit a rate-limited endpoint 35 times, then run 'redis-cli TTL rate:ip:<ip>' and verify it returns a positive integer (never -1 or -2)"
    expected: "TTL is between 1 and the configured window seconds; never -1 (no expiry) which would indicate the EXPIRE did not run"
    why_human: "Requires access to the staging Redis instance via redis-cli"
  - test: "SMOKE 6/10 — HOTFIX-05 refresh transaction: sign in as a user, note the session count (should be 1), call /auth/refresh, verify the count remains exactly 1 after rotation (old row deleted, new row inserted atomically)"
    expected: "SELECT count(*) FROM sessions WHERE user_id=X returns 1 both before and after a successful refresh"
    why_human: "Requires a live Postgres database and a valid refresh token from a staging session"
  - test: "SMOKE 7/10 — HOTFIX-01 scheduler downgrade: seed a Pro user with subscription_expires_at = NOW() - 1h, wait up to 90 seconds (scheduler runs every 60s), verify subscription_tier flips to 'free' and subscription_expires_at is preserved"
    expected: "User's subscription_tier becomes 'free' within 90s; subscription_expires_at column retains the original past timestamp as audit trail"
    why_human: "Requires a live staging database and a running scheduler process"
  - test: "SMOKE 8/10 — HOTFIX-07 index scan: run 'bash server/api/scripts/smoke_test_session_index.sh $STAGING_DATABASE_URL' after manually applying migration 017 to the existing staging DB (docker-entrypoint-initdb.d does NOT auto-apply for existing data dirs)"
    expected: "Script exits 0 and outputs 'OK: query planner is using idx_sessions_refresh_token_hash_unique'"
    why_human: "Requires live Postgres with migration applied; migration 017 must be manually run (psql -f) against pre-existing staging DB before this step is meaningful"
  - test: "SMOKE 9/10 — HOTFIX-06 createadmin: SSH to staging container and run './createadmin -password=anything'; verify error 'flag provided but not defined: -password'; then run the real flow and verify the seeded row has role=admin, subscription_tier=free"
    expected: "Flag rejection error on -password=...; new admin row has subscription_tier='free' not 'ultimate'"
    why_human: "Requires SSH access to the staging container running the createadmin binary"
  - test: "SMOKE 10/10 — Regression: hit /auth/guest, /auth/admin-login, /subscription, /servers; confirm all return expected 200/JSON shapes and none return 500"
    expected: "All four endpoints return their documented response shapes; any 5xx responses use the scrubbed body format with request_id"
    why_human: "Requires live staging API with valid credentials for authenticated endpoints"
---

# Phase 1: Hotfix — Audit Critical Fixes Verification Report

**Phase Goal:** Land the 8 audit Critical/High hotfixes (HOTFIX-01..08) as atomic commits on main and push the v2.2.0-hotfix tag.
**Verified:** 2026-05-22T12:00:00Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Preamble: Smoke Waiver

Per the prompt instructions and confirmed by `01-09-SMOKE-RESULTS.md`: the operator explicitly waived staging smoke on 2026-05-22 and authorized pushing the `v2.2.0-hotfix` tag on unit-test-only verification. This report faithfully records that:

- All on-disk threat mitigations (code + commits + tests) are verified PASS below.
- All 10 staging smoke steps remain unexecuted against live infrastructure.
- The gap is surfaced as `human_needed` (not a blocker) because the operator authorized the waiver; the live-system verification items are recorded as human verification tasks.

---

## Goal Achievement

### Observable Truths (Roadmap Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|---------|
| 1 | Admin demotion takes effect on very next request (not 5 min later) | VERIFIED (code only) | `AdminRequired(db *gorm.DB)` calls `repository.FindUserByID` on every admin request; JWT role claim lookup removed (`! grep -qE 'c\.Locals\("role"\)' admin.go` passes); no Redis cache introduced. Unit test `TestAdminRequired_DemotionTakesEffect` asserts 200→403 flip after DB-side role update with same JWT. Live staging verification: WAIVED. |
| 2 | Expired Pro tier downgraded automatically when scheduler runs | VERIFIED (code only) | `subscription_expires_at TIMESTAMPTZ` column confirmed in `migrations/001_initial.sql:12`; `repository.DowngradeExpiredSubscriptions` confirmed at `user_repo.go:277-288`; scheduler wire confirmed at `scheduler.go:124`. Three regression tests (`TestDowngradeExpiredSubscriptions_*`) all PASS. Webhook WRITE (populates `subscription_expires_at`) intentionally deferred to Phase 3 per D-07. Live staging verification: WAIVED. |
| 3 | `/auth/refresh` uses index scan on sessions (not seq scan) | VERIFIED (migration only) | `017_sessions_refresh_token_hash_unique.sql` exists, contains `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_sessions_refresh_token_hash_unique ON sessions(refresh_token_hash)`. Migration not yet applied to existing staging DB (WR-01 from code review: `docker-entrypoint-initdb.d` only fires on fresh data dir). Smoke script exists and is valid bash. Live EXPLAIN verification: WAIVED. |
| 4 | API refuses to start with missing required env vars | VERIFIED (code only) | `config.RequireEnv()` exists and scans `JWT_SECRET, DATABASE_URL, REDIS_URL, TUNNEL_VLESS_UUID`. Wired in `cmd/main.go` before `config.Load()`. `logger.Fatal` on non-empty slice (aggregate, single log line). LAVA_* keys correctly excluded (D-03). Unit tests `TestRequireEnv_*` PASS. Live process-exit verification: WAIVED. |
| 5 | 5xx responses return generic message; err.Error() never reaches client | VERIFIED (code only) | `ErrorHandler` in `health.go` returns `{"error":"internal server error","request_id":"<uuid>"}` on code >= 500; `err.Error()` only returned on code < 500 (4xx passthrough per D-06). Four `TestErrorHandler_*` integration tests PASS via `fiber.app.Test()`. `requestid.New` wired before `recover.New` at lines 98 and 102 respectively. Live 500-trigger verification: WAIVED. |
| 6 | Rate-limit INCR cannot leave counter without TTL after Redis hiccup | VERIFIED (code only) | `rateLimitScript = redis.NewScript(...)` at package level; `IncrRateLimit` body uses single `rateLimitScript.Run()` call; old `client.Pipeline()` and standalone `client.Expire(window)` removed. Three `TestIncrRateLimit_*` tests using miniredis all PASS. Live Redis TTL verification: WAIVED. |
| 7 | Failed refresh-token insert rolls back DeleteSession in same transaction | VERIFIED (code only) | `db.Transaction(func(tx *gorm.DB) error)` wraps `DeleteSession(tx)` + `FindUserByID(tx)` + `generateTokens` + `storeRefreshSession(tx)`. All four ops use `tx` not outer `db`. `errors.Is(err, repository.ErrNotFound)` branch present. Tokens returned only after closure returns nil. Three `TestRefreshToken_*` tests PASS. Live DB transaction verification: WAIVED. |
| 8 | `createadmin` does not accept -password on argv; seeds tier=free | VERIFIED (code + unit tests) | `flag.String("password", ...)` removed from `main.go`; `term.ReadPassword` + `term.IsTerminal` present; `bufio.NewReader` fallback present; `SubscriptionTier: "free"` confirmed; `"ultimate"` not present. `golang.org/x/term v0.25.0` is a direct require. Three `TestCreateAdmin_*` tests PASS. Live interactive prompt test: WAIVED. |

**Score:** 8/8 truths verified on disk. 0/8 truths verified on live staging (smoke waived).

---

### Deferred Items

No must-haves are addressed in later milestone phases. The HOTFIX-01 webhook write (populating `subscription_expires_at`) is the one intentionally deferred item, but the truth it supports (SC #2: scheduler downgrades) is verified via the existing column + scheduler code — the regression tests confirm that path. Phase 3 will add the write half; this is documented in ROADMAP and PHASE-SUMMARY.

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `server/api/cmd/createadmin/main.go` | stdin password + free tier seed | VERIFIED | term.ReadPassword present; SubscriptionTier="free"; -password flag gone |
| `server/api/cmd/createadmin/main_test.go` | 3 regression tests | VERIFIED | 3 TestCreateAdmin_* functions; no t.Skip remaining |
| `server/api/go.mod` | golang.org/x/term as direct require | VERIFIED | `golang.org/x/term v0.25.0` in direct require block |
| `server/api/internal/config/config.go` | RequireEnv() + OptionalEnvWarnings() | VERIFIED | Both functions present; LAVA_* excluded |
| `server/api/internal/config/config_test.go` | 3 unit tests for validator | VERIFIED | 3 functions; no t.Skip |
| `server/api/cmd/main.go` | RequireEnv + OptionalEnvWarnings wired; requestid before recover | VERIFIED | Both calls present; requestid at line 98 before recover at line 102 |
| `server/api/internal/handler/health.go` | 5xx scrub + request_id; 4xx passthrough | VERIFIED | "internal server error" literal; err.Error() for 4xx; request_id field |
| `server/api/internal/handler/errorhandler_test.go` | 4 integration tests | VERIFIED | 4 TestErrorHandler_* functions; no t.Skip |
| `server/api/internal/middleware/admin.go` | AdminRequired(db) with DB re-read | VERIFIED | Signature takes *gorm.DB; FindUserByID called; no c.Locals("role"); no Redis |
| `server/api/internal/middleware/admin_test.go` | 3 new HOTFIX-02 regression tests | VERIFIED | DemotionTakesEffect, DeletedUserDuringSession, EmptyLocals present; no t.Skip |
| `server/api/internal/cache/redis.go` | Lua script via redis.NewScript | VERIFIED | rateLimitScript = redis.NewScript(...) present; Pipeline() removed |
| `server/api/internal/cache/redis_test.go` | 3 TTL invariant tests | VERIFIED | 7 TestIncrRateLimit_* functions total (3 new + 4 prior); no t.Skip |
| `server/api/internal/handler/auth.go` | db.Transaction wrapping rotation | VERIFIED | db.Transaction present; all 4 ops use tx; ErrNotFound branch present |
| `server/api/internal/handler/auth_test.go` | 3 rotation tests | VERIFIED | RollbackOnInsertFailure, HappyPath, UserDeletedDuringRotation; no t.Skip |
| `server/api/internal/repository/user_repo_subscription_test.go` | 3 downgrade regression tests | VERIFIED | 3 TestDowngradeExpiredSubscriptions_* functions; no t.Skip |
| `server/api/migrations/017_sessions_refresh_token_hash_unique.sql` | Dedupe + CONCURRENT UNIQUE index | VERIFIED | Both steps present; IF NOT EXISTS; correct index name; A1 note in comments |
| `server/api/scripts/smoke_test_session_index.sh` | Executable smoke script | VERIFIED | Exists; executable; bash -n passes; greps for idx_sessions_refresh_token_hash_unique |
| `.planning/phases/01-hotfix-audit-critical-fixes/01-09-SMOKE-RESULTS.md` | Smoke checklist (all rows green) | PARTIAL | File exists; CI gate section green; all 10 smoke rows WAIVED (not executed); approval marker present |

---

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/createadmin/main.go` | `term.ReadPassword` | import + call replacing flag.String("password") | WIRED | grep confirms both term.ReadPassword and term.IsTerminal present; flag.String("password") absent |
| `cmd/createadmin/main.go` | `SubscriptionTier: "free"` | literal in User struct seed | WIRED | grep confirms "free"; no "ultimate" |
| `cmd/main.go` | `config.RequireEnv()` | called before config.Load(); logger.Fatal on non-empty | WIRED | grep confirms both calls; LAVA_* excluded |
| `cmd/main.go` | `requestid.New` | app.Use before recover.New at line 98 vs 102 | WIRED | awk ordering check passes (r=98 < c=102) |
| `handler/health.go::ErrorHandler` | `c.Locals("requestid")` | reads request_id for 5xx body and log line | WIRED | "request_id" literal present in handler |
| `middleware/admin.go::AdminRequired` | `repository.FindUserByID(db, userID)` | DB PK lookup on every admin request | WIRED | FindUserByID present; no Redis; c.Locals("role") absent |
| `cmd/main.go admin route group` | `middleware.AdminRequired(db)` | passes db as argument (signature changed from no-arg) | WIRED | grep confirms `middleware.AdminRequired(db` in main.go |
| `internal/cache/redis.go::IncrRateLimit` | `rateLimitScript.Run(ctx, client, []string{fullKey}, seconds)` | single EVAL/EVALSHA round-trip; INCR+EXPIRE in Lua | WIRED | redis.NewScript present; Pipeline() absent; INCR+EXPIRE in Lua body |
| `handler/auth.go::RefreshToken` | `db.Transaction(func(tx *gorm.DB) error)` | all 4 ops (DeleteSession, FindUserByID, generateTokens, storeRefreshSession) inside closure | WIRED | All 4 tx-variants confirmed by grep |
| `migrations/017_*.sql` | `sessions.refresh_token_hash` | CREATE UNIQUE INDEX CONCURRENTLY | WIRED (schema-only) | Migration file present and correct; index NOT yet applied to existing staging DB (see human_needed section) |
| `scheduler.go:124` | `repository.DowngradeExpiredSubscriptions(db)` | scheduler already calls every minute | WIRED | grep confirms call site at scheduler.go |

---

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| `AdminRequired` | `user.Role` | `repository.FindUserByID(db, userID)` → Postgres `users` table | Yes (live DB query on every request) | FLOWING |
| `config.RequireEnv()` | required env keys | `os.Getenv()` on JWT_SECRET, DATABASE_URL, REDIS_URL, TUNNEL_VLESS_UUID | Yes (real env reads) | FLOWING |
| `ErrorHandler` | `requestID` | `c.Locals("requestid").(string)` populated by requestid middleware | Yes (Fiber middleware chain sets it on every request) | FLOWING |
| `IncrRateLimit` | `result (int64)` | `rateLimitScript.Run()` → Redis INCR via Lua EVAL | Yes (real Redis round-trip) | FLOWING |
| Refresh handler `tokens` | `tokens *authResponse` | assigned inside `db.Transaction` closure only after `storeRefreshSession` succeeds | Yes (only flows to response when transaction commits) | FLOWING |

---

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Binaries build cleanly | `cd server/api && go build ./cmd ./cmd/createadmin` | Exit 0 | PASS |
| -password flag rejected | Static code check: `! grep -q 'flag.String("password"' main.go` | No flag.String found | PASS |
| term.ReadPassword present | `grep -q 'term.ReadPassword' main.go` | Found | PASS |
| SubscriptionTier free | `grep -q 'SubscriptionTier:.*"free"' main.go` | Found | PASS |
| RequireEnv wired in main.go | `grep -q 'config.RequireEnv()' cmd/main.go` | Found | PASS |
| requestid before recover | `awk` ordering check on cmd/main.go | line 98 < 102 | PASS |
| 5xx scrub body present | `grep -q '"internal server error"' health.go` | Found | PASS |
| AdminRequired DB lookup | `grep -q 'FindUserByID' admin.go` | Found | PASS |
| No Redis cache in admin | `! grep -q 'redis' admin.go` | Not found | PASS |
| Lua script in redis.go | `grep -q 'redis.NewScript' redis.go` | Found | PASS |
| Old Pipeline removed | `! grep -q 'client.Pipeline()' redis.go` | Not found | PASS |
| db.Transaction in auth | `grep -q 'db.Transaction(' auth.go` | Found | PASS |
| tx used inside closure | `grep -q 'storeRefreshSession(tx' auth.go` | Found | PASS |
| Migration 017 exists | `test -f migrations/017_*.sql` | Exists | PASS |
| CONCURRENTLY in migration | `grep -q 'CREATE UNIQUE INDEX CONCURRENTLY'` | Found | PASS |
| Smoke script valid bash | `bash -n smoke_test_session_index.sh` | Exit 0 | PASS |
| v2.2.0-hotfix tag annotated | `git cat-file -t v2.2.0-hotfix` | Returns "tag" | PASS |
| Tag pushed to origin | `git ls-remote --tags origin v2.2.0-hotfix` | SHA present | PASS |
| 8 hotfix commits present | `git log --format=%s \| grep -cE '^hotfix\(01\):'` | 8 | PASS |
| D-07: payment.go untouched | `git diff $(merge-base)..HEAD -- payment.go \| wc -l` | 0 | PASS |
| No t.Skip remaining | grep across all 7 hotfix test files | None found | PASS |

---

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|---------|
| HOTFIX-01 | 01-07-PLAN.md | Subscription expiry persists from payment provider; scheduler auto-downgrades | SATISFIED (code + tests; webhook write deferred to Phase 3) | Column in 001_initial.sql; DowngradeExpiredSubscriptions at user_repo.go:277-288; scheduler.go:124 wired; 3 regression tests PASS |
| HOTFIX-02 | 01-04-PLAN.md | AdminRequired re-reads role from DB per request | SATISFIED (code + tests; live behavior unverified) | admin.go rewrites to use FindUserByID; 3 new HOTFIX-02 tests PASS |
| HOTFIX-03 | 01-05-PLAN.md | Rate-limit INCR+EXPIRE atomic | SATISFIED (code + tests; live Redis unverified) | Lua NewScript present; Pipeline removed; 3 TTL tests via miniredis PASS |
| HOTFIX-04 | 01-03-PLAN.md | ErrorHandler scrubs 5xx; err.Error never to client | SATISFIED (code + tests; live 500 trigger unverified) | health.go rewritten; 4 errorhandler_test.go tests PASS via Fiber app.Test() |
| HOTFIX-05 | 01-06-PLAN.md | Refresh rotation transactional | SATISFIED (code + tests; live DB behavior unverified) | db.Transaction wraps all 4 ops; 3 auth_test.go rollback tests PASS |
| HOTFIX-06 | 01-01-PLAN.md | createadmin reads password from stdin; seeds tier=free | SATISFIED (code + tests; live interactive prompt unverified) | flag.String("password") removed; term.ReadPassword present; 3 createadmin tests PASS |
| HOTFIX-07 | 01-08-PLAN.md | sessions.refresh_token_hash UNIQUE index | SATISFIED (migration on disk; NOT YET APPLIED to existing staging DB) | 017_*.sql present and correct; smoke script valid; live EXPLAIN unverified |
| HOTFIX-08 | 01-02-PLAN.md | API fails to start on missing env vars | SATISFIED (code + tests; live process exit unverified) | RequireEnv() wired before config.Load(); 3 unit tests PASS; LAVA_* excluded |

---

### Anti-Patterns Found

| File | Pattern | Severity | Impact |
|------|---------|----------|--------|
| `server/api/internal/handler/auth.go:400, 501` | `_ = storeRefreshSession(db, ...)` — error discarded silently in GuestLogin and AdminLogin fast paths (WR-02 from code review) | Warning | GuestLogin slow path creates a user account then silently discards storeRefreshSession failure — user gets a non-refreshable token; not blocked by HOTFIX-05 fix which only covers RefreshToken handler |
| `server/api/cmd/createadmin/main.go:113-122` | `bufio.ReadString` error swallowed for any non-empty read (not just io.EOF) — truncated password silently accepted (WR-04) | Warning | Defense-in-depth gap; practical risk low for one-shot CLI |
| `server/api/internal/config/config.go:93-97, 107-111` | `getEnvDuration`/`getEnvInt64` silently swallow parse errors (WR-05) | Warning | Operator tuning values (STALE_CONNECTION_AFTER, LINK_CODE_TTL) silently ignored if malformed |
| `server/api/internal/config/config.go:68-74` | Duplicate required-key check — both RequireEnv() and Load() check JWT_SECRET/TUNNEL_VLESS_UUID (IN-02) | Info | Two sources of truth that can drift; not a security issue today |
| `server/api/migrations/017_*.sql` | Migration 017 will NOT auto-apply on existing production DB (WR-01) | Warning | ROADMAP success criterion #3 ("index scan") is blocked until operator manually runs `psql -f` on staging/production |
| `server/api/Dockerfile:1` | `FROM golang:1.24-alpine` vs `go 1.22.0` in go.mod (IN-03) | Info | Implicit toolchain version divergence |

None of the anti-patterns above are blockers for the phase goal (8 atomic commits + tag). WR-01 (migration not auto-applied) is the most operationally significant — it means success criterion #3 is schema-on-disk but not yet live-verified. This is already captured in the human_verification section.

---

### Human Verification Required

The 10 staging smoke steps listed in the frontmatter `human_verification` section above represent the known gap. In priority order:

**1. Migration 017 manual apply (prerequisite for SMOKE 8/10)**

Before running smoke step 8, the operator must manually apply the migration to the existing staging database:

```
psql "$STAGING_DATABASE_URL" -f server/api/migrations/017_sessions_refresh_token_hash_unique.sql
psql "$STAGING_DATABASE_URL" -c "\d sessions" | grep idx_sessions_refresh_token_hash_unique
```

`docker-entrypoint-initdb.d` will NOT pick up this file on an existing `pgdata` volume. This is documented as WR-01 in the code review report.

**2. HOTFIX-02 live demotion test (SMOKE 4/10) — highest risk gap**

HOTFIX-02 is the most consequential unverified hotfix: it closes a 5-minute privilege-revocation window. The unit test uses sqlite in-memory which proves the DB-lookup path fires, but it cannot prove that the production Postgres query + HTTP round-trip latency stays within acceptable bounds on the live stack.

**3. HOTFIX-05 live rollback count test (SMOKE 6/10)**

The unit test for HOTFIX-05 injects a GORM callback to force CreateSession failure. The live test verifies the actual DB session row count before and after a real refresh call, confirming the rollback path operates correctly end-to-end.

**4. All remaining smoke steps 1, 2, 3, 5, 7, 9, 10** — verify unit-test-proven behaviors hold on the real binary against real infrastructure.

---

### Gaps Summary

The phase delivered all 8 atomic hotfix commits in the correct order (D-02 verified), the `v2.2.0-hotfix` annotated tag was pushed to origin, and every threat mitigation is provable on disk via code inspection and passing unit tests. The Go test suite passed with `-race` across all packages.

The single gap is that staging smoke was explicitly waived by the operator. This is recorded honestly:

- **On-disk verification:** 8/8 must-haves verified via code grep, unit test pass counts, commit log, and binary build.
- **Live-system verification:** 0/8 success criteria verified against real infrastructure.
- **Highest-risk items:** HOTFIX-02 (admin demotion timing) and HOTFIX-05 (refresh transaction rollback) cannot be fully proven by unit tests alone. A follow-up smoke against staging or a production-like environment is strongly recommended before any paying user hits v2.2.0-hotfix.
- **Migration 017:** must be applied manually to pre-existing Postgres instances before ROADMAP criterion #3 (index scan) is live. No deployment pipeline mechanism currently guarantees this.

The `status: human_needed` reflects the waived staging smoke as a documented gap requiring operator action, not a blocking failure — the operator authorized the waiver with full awareness of the risk.

---

_Verified: 2026-05-22T12:00:00Z_
_Verifier: Claude (gsd-verifier)_
