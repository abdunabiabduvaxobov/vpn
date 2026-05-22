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
