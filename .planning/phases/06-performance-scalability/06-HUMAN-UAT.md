---
status: partial
phase: 06-performance-scalability
source: [06-VALIDATION.md, 06-07-PLAN.md]
started: 2026-05-30T00:00:00Z
updated: 2026-05-30T00:00:00Z
---

> **Consistent with Phases 4 and 5**, which deferred hardware-dependent UAT to a real
> environment. Phase 6's in-phase **definition-of-done is the five D-09 automated assertions**
> (full `cd server/api && go test ./...` green, incl. the testcontainers EXPLAIN / ctx-cancel /
> zero-SELECT / heartbeat-flush / scheduler-gate proofs). The items below are operational /
> hardware-dependent gates that CI on a fresh-volume DB cannot prove — tracked here so none is
> silently skipped. See `docs/runbooks/06-perf-deploy-runbook.md`.

## Current Test

number: 1
name: Live-DB index backfill (PERF-05 / PERF-08)
expected: |
  On the next prod deploy, operator runs the psql -f backfill from the runbook against the live
  DB, then confirms idx_scan increments in pg_stat_user_indexes after traffic.
awaiting: operator action on next production deploy

## Tests

### 1. Live-DB index backfill (PERF-05 / PERF-08) — PENDING-OPERATOR
why_manual: The migration runner (docker-entrypoint-initdb.d) only fires on an EMPTY pgdata volume, so a normal `docker compose up` against the existing prod DB does NOT apply migrations 022/023. The live backfill is a manual online step.
steps: |
  1. Deploy Phase 6 to prod as usual.
  2. Run the two backfill commands from docs/runbooks/06-perf-deploy-runbook.md §2:
       docker exec vpn-postgres psql -U vpnapp -d vpnapp -f /docker-entrypoint-initdb.d/022_add_perf_indexes.sql
       docker exec vpn-postgres psql -U vpnapp -d vpnapp -f /docker-entrypoint-initdb.d/023_connections_connected_at_index.sql
  3. Verify existence: SELECT indexname FROM pg_indexes WHERE tablename='connections';
  4. After traffic, verify usage: SELECT idx_scan FROM pg_stat_user_indexes WHERE indexrelname='idx_connections_heartbeat_active'; (expect > 0)
  CONCURRENTLY = online-safe (no write lock); IF NOT EXISTS = idempotent / re-runnable.
warning_sign: CI EXPLAIN assertion green (fresh testcontainer) BUT prod pg_stat_user_indexes idx_scan=0 ⇒ backfill was skipped.
ref: docs/runbooks/06-perf-deploy-runbook.md §1–2
result: [PENDING-OPERATOR — must run on next prod deploy]

### 2. Real ~10k synthetic connection load test (PERF-02 / phase goal) — DEFERRED
why_manual: Requires staging hardware + a k6/vegeta load driver; not reproducible in CI.
steps: |
  Drive ~10k synthetic connections against staging; measure:
    - Postgres heartbeat write q/s drop (expect ~167/s → ~1 bulk UPDATE / 10s)
    - /auth/refresh p99 latency
    - stale-connection cleanup latency
note: The five D-09 automated assertions are the in-phase definition-of-done; this live load test is the release-time confirmation, NOT a Phase 6 gate.
result: [DEFERRED to end-of-milestone release phase]

### 3. Physical second-host PG/Redis provisioning + private-link/firewall (PERF-03) — DEFERRED
why_manual: Operator/ops-runbook action on real infra; no code change. Phase 6 ships only the parameterized compose split + host parameterization.
steps: |
  Per docs/runbooks/06-perf-deploy-runbook.md §4:
    1. Run docker-compose.data.yml on the data host.
    2. Bind PG/Redis to the private interface (VPC/WireGuard) + firewall (no public ports).
    3. Set DB_HOST / REDIS_HOST in the app host /opt/vpn/.env; run docker-compose.prod.yml there.
    4. (TLS-ready) flip sslmode=disable→require and redis://→rediss:// when exposing the link — no code change (D-02).
result: [DEFERRED — operator's call]

## Summary

total: 3
passed: 0
issues: 0
pending: 1
skipped: 0
blocked: 0
deferred: 2

## Gaps
