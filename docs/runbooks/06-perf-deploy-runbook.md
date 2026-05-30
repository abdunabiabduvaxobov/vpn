# Phase 6 — Performance & Scalability Deploy Runbook

**Scope:** Operator steps required to ship Phase 6 (performance & scalability) to production.
All *code* changes (compose split, perf indexes, `/servers` cache, user cache, heartbeat→Redis
flush, scheduler `RUN_SCHEDULER` gate, `ctx` propagation) landed in plans 06-01…06-06 with
automated assertions. This runbook covers the **operational gaps** that automated CI on a
fresh-volume database cannot prove:

1. The **live-DB index backfill** (PERF-05 / PERF-08) — the new indexes are **not** auto-applied to an existing prod database.
2. The **co-located single-host bring-up** (the new two-file compose).
3. The **off-host data-tier move** (PERF-03) — an operator decision, out of Phase 6 code scope.
4. **Rollback / safety** notes.

> Companion tracker: `.planning/phases/06-performance-scalability/06-HUMAN-UAT.md`
> (deferred load test + the operator-gated backfill).

---

## 1. Why a manual index backfill is required

Migrations 022/023 (`server/api/migrations/022_add_perf_indexes.sql`,
`023_connections_connected_at_index.sql`) are mounted into the Postgres container at
`/docker-entrypoint-initdb.d`. **The Postgres entrypoint only runs the files in that directory
when the data directory is empty — i.e. on the FIRST initialization of a fresh `pgdata` volume.**

Consequences for an existing production database:

- A normal `docker compose ... up -d` against a populated `pgdata` volume **does NOT re-run**
  `022`/`023`. The deploy workflow (`.github/workflows/deploy.yml`) copies the migration files to
  `/opt/vpn/migrations` on every deploy, but it still relies on initdb semantics — copying the
  file does not apply it to a live DB.
- Therefore, after deploying Phase 6 to an existing prod DB, the new indexes **will be missing**
  until you backfill them manually (Section 2).

**Warning sign (how you detect the gap):** the `EXPLAIN` assertions in
`perf_indexes_test.go` are **green in CI** because CI spins a *fresh* testcontainer Postgres that
runs all migrations from empty. But in prod, after traffic, the index shows **`idx_scan = 0`** in
`pg_stat_user_indexes` (it is never used because it does not exist, or it exists but the planner
never picks it). CI-green + prod-`idx_scan=0` = the backfill was skipped.

---

## 2. Live-DB index backfill (PERF-05 / PERF-08)

Run these **once** against the live database after deploying Phase 6. `CREATE INDEX CONCURRENTLY`
takes **no write lock** (safe online — reads and writes continue), and `IF NOT EXISTS` makes the
commands **idempotent / re-runnable** (safe to run again; a no-op if the index already exists).

```bash
# Apply migration 022 — idx_connections_heartbeat_active (partial, stale-sweep)
docker exec vpn-postgres psql -U vpnapp -d vpnapp -f /docker-entrypoint-initdb.d/022_add_perf_indexes.sql

# Apply migration 023 — idx_connections_connected_at (analytics date-bucket)
docker exec vpn-postgres psql -U vpnapp -d vpnapp -f /docker-entrypoint-initdb.d/023_connections_connected_at_index.sql
```

> `CONCURRENTLY` cannot run inside a transaction block — these migration files intentionally
> contain no `BEGIN`/`COMMIT`. If a `CONCURRENTLY` build is interrupted it can leave an `INVALID`
> index; drop it (`DROP INDEX CONCURRENTLY <name>;`) and re-run the file.

### Verify the indexes exist

```bash
docker exec vpn-postgres psql -U vpnapp -d vpnapp -c \
  "SELECT indexname FROM pg_indexes WHERE tablename='connections';"
```

Expect both `idx_connections_heartbeat_active` and `idx_connections_connected_at` in the list.

### Verify the indexes are actually used (after some traffic)

```bash
docker exec vpn-postgres psql -U vpnapp -d vpnapp -c \
  "SELECT indexrelname, idx_scan FROM pg_stat_user_indexes WHERE indexrelname IN ('idx_connections_heartbeat_active','idx_connections_connected_at');"
```

After the stale-connection sweep (runs every minute) and at least one admin analytics query,
`idx_scan` should be **> 0** and climbing. If it stays at `0`, the planner is not using the index
— investigate before declaring PERF-05/PERF-08 shipped.

---

## 3. Co-located single-host bring-up (DEFAULT)

Phase 6 split the data tier (`postgres` + `redis`) into `docker-compose.data.yml`, leaving the app
tier (`api` + `tunnel`) in `docker-compose.prod.yml`. On a single host, bring both up together —
**zero `.env` change** (services resolve by name on the shared `vpn-net` bridge; the app tier's
`DB_HOST`/`REDIS_HOST` default to `postgres`/`redis`):

```bash
docker compose -f docker-compose.data.yml -f docker-compose.prod.yml up -d
```

Confirm the data-tier hardening landed:

- **Redis** runs with `--requirepass <REDIS_PASSWORD> --maxmemory 256mb --maxmemory-policy allkeys-lru`
  (bounded memory under unique-key spam — T-06-REDISOOM / D-10a). Check:
  ```bash
  docker exec vpn-redis redis-cli -a "$REDIS_PASSWORD" CONFIG GET maxmemory maxmemory-policy
  ```
  Expect `maxmemory 268435456` (256mb) and `maxmemory-policy allkeys-lru`.
- **Postgres** mounts the tuned `server/api/postgresql.conf` via `-c config_file=...` (PERF-09 /
  D-09c). Check:
  ```bash
  docker exec vpn-postgres psql -U vpnapp -d vpnapp -c "SHOW shared_buffers; SHOW config_file;"
  ```
- Neither `postgres` nor `redis` publishes a public host port (D-02 / T-06-DATALINK) — they are
  reachable only on the compose network.

---

## 4. Off-host data-tier move (PERF-03 — operator step, OUT of code scope)

Phase 6 ships only the **parameterized compose split**; the physical move of PG/Redis to a second
host is the operator's call and requires **no image or code change**.

1. **Run the data tier on the data host:**
   ```bash
   docker compose -f docker-compose.data.yml up -d
   ```
2. **Lock down the data host:** bind PG/Redis to the **private interface** (VPC / WireGuard /
   locked subnet) and **firewall** it so neither is publicly reachable. The data compose file
   publishes no host ports by design — do not add public port mappings.
3. **Point the app host at the data host:** set `DB_HOST` and `REDIS_HOST` in the app host's
   `/opt/vpn/.env` to the data host's **private** address, then bring up the app tier there:
   ```bash
   docker compose -f docker-compose.prod.yml up -d
   ```
4. **Data migration is a SEPARATE operator decision.** A fresh empty `pgdata` volume re-runs
   `001..023` via initdb, but the existing rows do **not** migrate themselves — use `pg_dump`/
   `pg_restore` or streaming replication. (Out of Phase 6 scope.)

**TLS-ready upgrade path (D-02 — no code change):** when you expose the cross-host link, flip
`DATABASE_URL` `sslmode=disable` → `sslmode=require` (or `verify-full`) and `REDIS_URL`
`redis://` → `rediss://` (enable Redis `--tls-port` + firewall). The app needs no rebuild — only
env + the data-host TLS config.

---

## 5. Rollback / safety

- **Indexes are additive** — to revert: `DROP INDEX CONCURRENTLY idx_connections_heartbeat_active;`
  and `DROP INDEX CONCURRENTLY idx_connections_connected_at;` (online, no write lock). Nothing else
  depends on them existing; the stale-sweep query is correct with or without them (just slower).
- **The compose split is reversible** — running both files on one host (Section 3) is behaviorally
  identical to the pre-split single `docker-compose.prod.yml`.
- **The COALESCE-drop on `CleanupStaleConnections` (plan 06-02) is behavior-equivalent** — active
  rows always have a non-null `last_heartbeat_at`, so `disconnected_at IS NULL AND last_heartbeat_at < cutoff`
  selects the same rows the old `COALESCE(...)` predicate did, while letting the planner range-scan
  the partial index. Reverting the predicate is safe but reintroduces the Seq Scan.
- **`RUN_SCHEDULER` gate (PERF-06):** if periodic jobs misbehave, set `RUN_SCHEDULER=false` on a
  replica to disable its scheduler (only one replica should run it). Unset / any value other than
  `{false,0,no}` = enabled (default-on).

---

*Phase: 06-performance-scalability · Requirements: PERF-03, PERF-05, PERF-08 (+ PERF-02/06/09 operational notes)*
