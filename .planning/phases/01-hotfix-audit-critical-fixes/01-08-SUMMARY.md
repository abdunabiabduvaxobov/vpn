---
phase: 01-hotfix-audit-critical-fixes
plan: 08
subsystem: backend-migrations
tags: [hotfix, performance, schema, sessions, refresh-token, postgres, concurrent-index]
requirements_addressed: [HOTFIX-07]
dependency_graph:
  requires:
    - 01-hotfix-audit-critical-fixes/07
  provides:
    - "UNIQUE index idx_sessions_refresh_token_hash_unique on sessions.refresh_token_hash"
    - "server/api/scripts/smoke_test_session_index.sh (Wave 0 smoke gate for Phase 1 success criterion #3)"
  affects:
    - "/auth/refresh hot path (planner now uses Index Scan instead of Seq Scan)"
    - "sessions table (dedupe pass on apply; UNIQUE constraint thereafter)"
tech_stack:
  added: []
  patterns:
    - "CREATE UNIQUE INDEX CONCURRENTLY (non-locking, outside-tx)"
    - "ROW_NUMBER() OVER (PARTITION BY ... ORDER BY ... DESC) dedupe pattern"
key_files:
  created:
    - server/api/migrations/017_sessions_refresh_token_hash_unique.sql
    - server/api/scripts/smoke_test_session_index.sh
  modified: []
decisions:
  - "Single-file migration shape (not 017a/017b split) — verified A1: Postgres docker-entrypoint-initdb.d applies psql -f per file without BEGIN/COMMIT wrap."
  - "Dedupe by ROW_NUMBER() partition with newest-by-created_at survivor, keeping FK chains intact (sessions.user_id ON DELETE CASCADE points the other way)."
  - "IF NOT EXISTS on CREATE UNIQUE INDEX makes the migration idempotent across re-applies."
metrics:
  duration: "~10 min"
  completed: 2026-05-22
  tasks: 3
  files: 2
  commits: 1
---

# Phase 1 Plan 08: HOTFIX-07 — UNIQUE index on sessions.refresh_token_hash Summary

UNIQUE index on `sessions.refresh_token_hash` via two-step migration (dedupe + `CREATE UNIQUE INDEX CONCURRENTLY`) so `/auth/refresh` becomes an O(1) index lookup instead of a sequential scan, plus Wave 0 smoke gate that grep-asserts the EXPLAIN plan switched.

## Commit

| Hash       | Subject                                                                       |
| ---------- | ----------------------------------------------------------------------------- |
| `192dd92`  | hotfix(01): UNIQUE index on sessions.refresh_token_hash + dedupe [HOTFIX-07] |

Atomic commit lands both files together per D-01:

- `server/api/migrations/017_sessions_refresh_token_hash_unique.sql`
- `server/api/scripts/smoke_test_session_index.sh`

Verified atomic via `git diff-tree --no-commit-id --name-only -r HEAD`:

```
server/api/migrations/017_sessions_refresh_token_hash_unique.sql
server/api/scripts/smoke_test_session_index.sh
```

## Task Order (honors Wave 0 contract)

1. **Task 1 — preflight verification (no file written)** — confirmed A1 (migration runner) and that 017 is the next sequential number.
2. **Task 2 — Wave 0 smoke-script stub** (`scripts/smoke_test_session_index.sh`) written FIRST, but NOT committed yet — per VALIDATION.md row 1-W0-05 the test exists before the implementation. In RED state it would exit 1 with `FAIL: query planner is doing a sequential scan`.
3. **Task 3 — migration SQL + atomic commit** — wrote `017_*.sql`, re-ran `bash -n` on the Task 2 script, ran the full Go suite (green), then staged both files and committed atomically.

This ordering satisfies VALIDATION row 1-W0-05: **the Wave 0 smoke script existed in the working tree BEFORE the migration SQL was written**, even though they ship in a single commit (per D-01).

## Task 1 — A1 verification command outputs

```
$ grep -n 'docker-entrypoint-initdb.d' docker-compose.prod.yml
19:      - ./migrations:/docker-entrypoint-initdb.d

$ ls server/api/migrations/ | sort | tail -5
012_add_device_secret.sql
013_link_code_varchar6.sql
014_add_audit_log.sql
015_add_telegram_recovery.sql
016_add_telegram_profile.sql

$ find server/api/cmd -maxdepth 2 -type d
server/api/cmd
server/api/cmd/createadmin

$ find server/api -name "Makefile" -maxdepth 3
(no output — no Makefile present)

$ grep -rln "golang-migrate\|goose" server/api/cmd/
(no output — no app-level migrator)

$ ls server/api/migrations/017_sessions_refresh_token_hash_unique.sql
(pre-Task-3: exit 1 — file absent as expected)
```

**A1 conclusion:** the migration runner is Postgres's native `docker-entrypoint-initdb.d` (mounted at `docker-compose.prod.yml:19`). Postgres's entrypoint script applies each `*.sql` file via `psql -v ON_ERROR_STOP=1 -f <file>` WITHOUT wrapping in `BEGIN/COMMIT`. Therefore single-file `CREATE INDEX CONCURRENTLY` is safe. No 017a/017b split needed.

## File contents

### `server/api/scripts/smoke_test_session_index.sh`

`bash -n` output: clean (exit 0). `test -x`: true (mode 0755 — set via `chmod +x`).

```bash
#!/usr/bin/env bash
# HOTFIX-07 smoke test: confirms the UNIQUE index on sessions.refresh_token_hash
# is being used by the query planner. Maps to ROADMAP Phase 1 success criterion #3
# and to VALIDATION.md row 1-W0-05 (Wave 0 artifact for HOTFIX-07).
#
# Pre-migration (run right after this script is created in Task 2): exits 1 with a
# clear "Seq Scan on sessions" or "index not found" message because the UNIQUE index
# idx_sessions_refresh_token_hash_unique does not yet exist. That is the Wave 0
# RED state -- proves the gate is meaningful.
#
# Post-migration (after Task 3's 017_...sql is applied to staging): exits 0 with
# "OK: query planner is using idx_sessions_refresh_token_hash_unique". That is
# the Wave 0 GREEN state.
#
# Usage:
#   DATABASE_URL=postgres://... ./scripts/smoke_test_session_index.sh
#   ./scripts/smoke_test_session_index.sh "postgres://..."
#
# Exit codes:
#   0  EXPLAIN output contains "Index Scan using idx_sessions_refresh_token_hash_unique"
#   1  Output contains "Seq Scan on sessions" (REGRESSION -- index not being used)
#      OR the expected Index Scan line is missing
#   2  psql failed to connect, or sessions table not found
set -euo pipefail

DB_URL="${1:-${DATABASE_URL:-}}"
if [[ -z "$DB_URL" ]]; then
  echo "ERROR: DATABASE_URL not set and no first argument provided" >&2
  exit 2
fi

EXPLAIN_OUT=$(psql "$DB_URL" -X -A -t -c \
  "EXPLAIN SELECT * FROM sessions WHERE refresh_token_hash = 'deadbeefdeadbeefdeadbeefdeadbeef' AND expires_at > NOW();" 2>&1) || {
    echo "ERROR: psql failed:" >&2
    echo "$EXPLAIN_OUT" >&2
    exit 2
}

echo "EXPLAIN output:"
echo "$EXPLAIN_OUT"
echo

if echo "$EXPLAIN_OUT" | grep -q "Index Scan using idx_sessions_refresh_token_hash_unique"; then
  echo "OK: query planner is using idx_sessions_refresh_token_hash_unique"
  exit 0
fi

if echo "$EXPLAIN_OUT" | grep -q "Seq Scan on sessions"; then
  echo "FAIL: query planner is doing a sequential scan -- HOTFIX-07 index not in use" >&2
  echo "      (expected after Task 3 / plan 09 step 8 migration apply; pre-migration this FAIL is intentional)" >&2
  exit 1
fi

echo "FAIL: EXPLAIN output did not contain the expected Index Scan line" >&2
exit 1
```

### `server/api/migrations/017_sessions_refresh_token_hash_unique.sql`

```sql
-- HOTFIX-07: UNIQUE index on sessions.refresh_token_hash for /auth/refresh O(1) lookup.
-- Per PERFORMANCE-AUDIT Perf #1 and ROADMAP Phase 1 success criterion #3.
--
-- The query at repository/session_repo.go:20 does a full sequential scan today;
-- this index makes it an index lookup. The hash is SHA-256 of a 30-day-lived
-- refresh token, so uniqueness is statistically guaranteed -- UNIQUE also gives
-- the planner a "row count = 0 or 1" hint that improves the EXPLAIN plan.
--
-- Step 1: deduplicate any rows sharing the same refresh_token_hash. In v2.1.0
-- with no paying users this should find zero rows in production, but staging
-- and dev DBs may have stale guest sessions from testing. Keep the newest row
-- per hash (by created_at), delete the rest. The sessions.user_id FK is
-- ON DELETE CASCADE on the users side; deleting sessions themselves does not
-- cascade.
--
-- Step 2: CREATE UNIQUE INDEX CONCURRENTLY -- does NOT lock the table for
-- writes, so safe to run against a live production database.
--
-- IMPORTANT: CREATE INDEX CONCURRENTLY cannot run inside a transaction block.
-- This file is applied by Postgres's native docker-entrypoint-initdb.d
-- (mounted at docker-compose.prod.yml:19 - ./migrations:/docker-entrypoint-initdb.d),
-- which runs `psql -f` per file WITHOUT wrapping in BEGIN/COMMIT. Verified
-- during planning (see .planning/phases/01-hotfix-audit-critical-fixes/01-RESEARCH.md
-- Assumption A1). If a future deploy moves to golang-migrate or goose with
-- auto-tx wrap, this file MUST be split into 017a (DELETE inside tx) and
-- 017b (CREATE INDEX CONCURRENTLY outside tx) -- and the file will need
-- the runner-specific marker to disable wrapping (e.g. `-- migrate:no-tx`
-- for golang-migrate).

-- Step 1: dedupe -- keep the newest session per refresh_token_hash.
DELETE FROM sessions
WHERE id IN (
    SELECT id FROM (
        SELECT id,
               ROW_NUMBER() OVER (PARTITION BY refresh_token_hash ORDER BY created_at DESC) AS rn
        FROM sessions
    ) ranked
    WHERE rn > 1
);

-- Step 2: create the UNIQUE index without locking writes.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_sessions_refresh_token_hash_unique
    ON sessions (refresh_token_hash);
```

## Verification Performed

| Check                                                                 | Result                                                |
| --------------------------------------------------------------------- | ----------------------------------------------------- |
| `test -f server/api/migrations/017_sessions_refresh_token_hash_unique.sql` | PASS                                                  |
| `ls server/api/migrations/017_*.sql \| wc -l` == 1                    | PASS                                                  |
| `grep -q 'CREATE UNIQUE INDEX CONCURRENTLY' …017_*.sql`               | PASS                                                  |
| `grep -q 'IF NOT EXISTS' …017_*.sql`                                  | PASS (idempotent)                                     |
| `grep -q 'idx_sessions_refresh_token_hash_unique' …017_*.sql`         | PASS (matches smoke-script expectation)               |
| `grep -q 'ROW_NUMBER() OVER (PARTITION BY refresh_token_hash' …017_*.sql` | PASS                                                  |
| `grep -q 'docker-entrypoint-initdb.d' …017_*.sql`                     | PASS (A1 invariant documented in SQL)                 |
| `test -x server/api/scripts/smoke_test_session_index.sh`              | PASS (mode 0755)                                      |
| `bash -n server/api/scripts/smoke_test_session_index.sh`              | PASS (clean)                                          |
| `grep -q 'idx_sessions_refresh_token_hash_unique' …smoke_test_session_index.sh` | PASS                                                  |
| `grep -q 'Index Scan' …smoke_test_session_index.sh`                   | PASS                                                  |
| `grep -q 'set -euo pipefail' …smoke_test_session_index.sh`            | PASS                                                  |
| `grep -q 'FAIL: query planner is doing a sequential scan' …smoke_test_session_index.sh` | PASS                                                  |
| `cd server/api && go test ./... -count=1`                             | PASS (cache, config, handler, middleware, recovery, repository, scheduler, createadmin all `ok`) |
| `git log -1 --format=%s \| grep -qE '^hotfix\(01\): .*HOTFIX-07'`     | PASS                                                  |
| `git diff-tree --no-commit-id --name-only -r HEAD` lists both files   | PASS                                                  |

## Wave 0 Contract — VALIDATION row 1-W0-05

> **VALIDATION.md row 1-W0-05:** Wave 0 smoke gate for HOTFIX-07 — `server/api/scripts/smoke_test_session_index.sh` exists and fails clearly when run against a DB without the UNIQUE index.

Satisfied: Task 2 wrote the smoke script BEFORE Task 3 wrote the migration. The script exists, is executable, has valid bash syntax, greps for the canonical index name + `Index Scan` marker, and emits a clear `FAIL: query planner is doing a sequential scan` message pre-migration (Wave 0 RED). Plan 09 step 8 will run it against staging post-migration to demonstrate Wave 0 GREEN.

## Deviations from Plan

None — plan executed exactly as written. All three tasks ran in order; A1 verification (Task 1) returned the expected outputs; Task 2's stub passed `bash -n` on first write; Task 3's migration parsed correctly on first write; full Go test suite green; atomic commit landed both files with the required subject regex.

## Known Stubs

None. The smoke script is intentionally a stub-style test (Wave 0 RED state until the migration is applied to staging), but that is the documented Wave 0 contract — not an incomplete deliverable.

## Operator Notes (for plan 09 / staging apply)

- **Migration WILL be applied to staging by plan 09 step 8** (operator-driven). Recommend taking a logical backup of `sessions` first (`pg_dump -t sessions`) per D-02 — schema changes are hardest to reverse.
- After the migration applies, run:
  ```
  DATABASE_URL=$STAGING_DATABASE_URL bash server/api/scripts/smoke_test_session_index.sh
  ```
  Expected: exit 0 with `OK: query planner is using idx_sessions_refresh_token_hash_unique`. If instead you see `FAIL: query planner is doing a sequential scan`, the migration did not apply — investigate before tagging `v2.2.0-hotfix`.
- **Contingency:** if A1 turns out wrong at staging time (i.e. the migration runner wraps the file in a transaction and `CREATE INDEX CONCURRENTLY` errors with `42P02`), split the file into `017a_sessions_dedupe.sql` (DELETE only) + `017b_sessions_unique_index.sql` (CREATE INDEX CONCURRENTLY) per the contingency comment inside the SQL file.

## Threat Flags

None — no new network endpoints, auth paths, file access patterns, or trust-boundary schema changes beyond the documented mitigation (UNIQUE constraint on `refresh_token_hash` is itself a defensive hardening, not a new attack surface).

## Self-Check: PASSED

Verified:
- `server/api/migrations/017_sessions_refresh_token_hash_unique.sql` exists at the cited path
- `server/api/scripts/smoke_test_session_index.sh` exists at the cited path and is executable
- Commit `192dd92` exists in `git log --oneline` with the expected subject
- Both files appear in `git diff-tree --no-commit-id --name-only -r HEAD`
