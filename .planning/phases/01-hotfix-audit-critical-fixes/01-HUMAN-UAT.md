---
status: partial
phase: 01-hotfix-audit-critical-fixes
source: [01-VERIFICATION.md, 01-09-SMOKE-RESULTS.md]
started: 2026-05-22T00:23:55Z
updated: 2026-05-22T00:23:55Z
waived_by_operator: true
---

## Current Test

[awaiting operator-run staging smoke; tag v2.2.0-hotfix already pushed on a waiver]

## Tests

### 1. HOTFIX-08 — env validator fails fast on staging
expected: `JWT_SECRET= ./vpn-api 2>&1 | head -1 | jq -e '.missing'` exits 0; process exits 1.
result: WAIVED — not run on staging

### 2. HOTFIX-04 — 5xx body scrubbed on staging
expected: `POST /auth/refresh` with bad token returns `{"error":"internal server error","request_id":"…"}` with no `pq:`/`gorm:`/`bcrypt:` substrings.
result: WAIVED — not run on staging

### 3. HOTFIX-04 — X-Request-ID echoed on staging
expected: header `X-Request-ID: smoke-2` echoed in response header and body; generated UUIDv4 when absent.
result: WAIVED — not run on staging

### 4. HOTFIX-02 — admin demotion takes effect immediately on staging
expected: same JWT returns 200 → `UPDATE users SET role='user'` → same JWT returns 403 within seconds.
result: WAIVED — not run on staging (HIGH-RISK: cannot prove live with sqlite unit tests)

### 5. HOTFIX-03 — rate-limit TTL is positive on staging Redis
expected: after 35× `POST /auth/guest`, `redis-cli TTL rate:ip:<ip>` returns positive int.
result: WAIVED — not run on staging

### 6. HOTFIX-05 — refresh leaves exactly one session row on staging
expected: count=1 before, count=1 after, hash differs.
result: WAIVED — not run on staging (HIGH-RISK: rollback path needs live DB)

### 7. HOTFIX-01 — scheduler downgrades expired Pro user within ≤90s on staging
expected: `UPDATE users SET tier='pro', expires=NOW()-INTERVAL '1 hour'` → ≤90s later tier='free'.
result: WAIVED — not run on staging

### 8. HOTFIX-07 — EXPLAIN shows Index Scan on staging
expected: `bash smoke_test_session_index.sh "$DB_URL"` exits 0 with `Index Scan using idx_sessions_refresh_token_hash_unique`.
result: WAIVED — not run on staging (PREREQUISITE: operator must `psql -f migrations/017_*.sql` first because docker-entrypoint-initdb.d only fires on fresh data dir)

### 9. HOTFIX-06 — createadmin stdin + free tier on staging
expected: `./createadmin -password=anything` rejected; `./createadmin -email=…` prompts echo-off and seeds row as `admin|free`.
result: WAIVED — not run on staging

### 10. Regression — existing endpoints still 200/JSON on staging
expected: `/auth/guest`, `/auth/admin-login`, `/subscription`, `/servers` all 200/JSON.
result: WAIVED — not run on staging

## Summary

total: 10
passed: 0
issues: 0
pending: 0
skipped: 10
blocked: 0
waived: 10

## Gaps

(Operator explicitly waived all 10 smoke steps before tag push. To clear this UAT, run the
10-step smoke against staging post-deploy and update each row's `result:` to `passed` /
`failed`, or run `/gsd-verify-work 01` and walk through them interactively.)
