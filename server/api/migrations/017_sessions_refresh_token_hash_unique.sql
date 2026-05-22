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
