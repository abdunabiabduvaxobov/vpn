-- PERF-05: partial b-tree index on connections so the stale-connection sweep is
-- O(connected) not O(history). The sweep predicate is
-- `disconnected_at IS NULL AND last_heartbeat_at < cutoff`
-- (see repository/connection_repo.go CleanupStaleConnections, COALESCE dropped in
-- this same plan). A partial index ON (last_heartbeat_at) WHERE disconnected_at IS NULL
-- lets the planner range-scan only the live rows instead of sequential-scanning the
-- full connections table, which grows to millions of rows over time (audit §2.2).
--
-- IMPORTANT: CREATE INDEX CONCURRENTLY cannot run inside a transaction block, so
-- this file contains NO BEGIN/COMMIT. It is applied by Postgres's native
-- docker-entrypoint-initdb.d runner (docker-compose.prod.yml mounts
-- ./migrations:/docker-entrypoint-initdb.d), which runs `psql -f` per file WITHOUT
-- wrapping in BEGIN/COMMIT. This is the same mechanism migration 017 relies on
-- (see 017_sessions_refresh_token_hash_unique.sql header, lines 16-28). The test
-- harness (migrations/migrations_test.go:38-61) special-cases any file containing
-- CONCURRENTLY and runs each statement on its own connection so no implicit tx wraps it.
--
-- CONCURRENTLY avoids the ACCESS EXCLUSIVE lock a plain CREATE INDEX would take on the
-- high-churn connections table, so the heartbeat write path stays available during the
-- build (threat T-06-IDX-01). IF NOT EXISTS makes the file idempotent.
--
-- CAVEAT (empty-volume manual backfill — carry to the plan 06 runbook):
-- docker-entrypoint-initdb.d scripts run ONLY on first container init, when the
-- pgdata volume is empty. On the existing live database (volume already populated)
-- a normal `docker compose up` does NOT re-run this file. The production backfill
-- MUST be a manual runbook step, run online (no write lock) thanks to CONCURRENTLY:
--   docker exec vpn-postgres psql -U vpnapp -d vpnapp -f /docker-entrypoint-initdb.d/022_add_perf_indexes.sql
-- IF NOT EXISTS keeps the manual run idempotent whether or not initdb already created it.

CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_connections_heartbeat_active
    ON connections (last_heartbeat_at)
    WHERE disconnected_at IS NULL;
