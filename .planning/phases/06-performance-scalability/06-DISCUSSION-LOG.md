# Phase 6: Performance & scalability - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-29
**Phase:** 6-performance-scalability
**Areas discussed:** DB/Redis off-host topology, Heartbeat Redis architecture, Cache invalidation + TTLs, Verification depth + P2 polish scope

---

## Gray-area selection

| Option | Description | Selected |
|--------|-------------|----------|
| DB/Redis off-host topology | PERF-03 split target: real VM vs managed vs compose-restructure | ✓ |
| Heartbeat Redis architecture | PERF-02 durability fork: PG-source-of-truth + flush vs Redis-live-truth | ✓ |
| Cache invalidation + TTLs | PERF-01/04 keying, bust events, TTLs, c.Locals refactor | ✓ |
| Verification depth + P2 polish scope | definition-of-done + which P2 nice-to-haves ride along | ✓ |

**User's choice:** All four areas.

---

## DB/Redis off-host topology (PERF-03)

### Q1 — Split target for this phase
| Option | Description | Selected |
|--------|-------------|----------|
| Compose+config restructure now, move via runbook | Split docker-compose.data.yml + host-parameterized URLs; physical move = ops step, zero code change | ✓ |
| Real second VM this phase | Provision + run PG/Redis on a second VM now | |
| Managed PG + managed Redis | Provider-managed services | |

**User's choice:** Compose+config restructure now, physical move via ops runbook.

### Q2 — Cross-host transport security
| Option | Description | Selected |
|--------|-------------|----------|
| Private network + firewall, plaintext | sslmode=disable + Redis requirepass over trusted private link, TLS-ready strings | ✓ |
| TLS for PG + Redis | sslmode=require + Redis TLS even over private link | |
| You decide | — | |

**User's choice:** Private network + firewall, plaintext; connection strings TLS-ready, document upgrade path.

---

## Heartbeat Redis architecture (PERF-02)

### Q1 — Durability model
| Option | Description | Selected |
|--------|-------------|----------|
| Postgres source-of-truth + bulk flush | Redis-only write; scheduler bulk-UPDATEs PG every 10s; cleanup + PERF-05 index unchanged | ✓ |
| Redis live-truth + EXISTS check | Redis-only; cleanup checks EXISTS hb:<id>; no flush job; loses state on Redis restart | |

**User's choice:** Postgres stays source-of-truth + bulk flush.

### Q2 — Dirty-set tracking for the flush
| Option | Description | Selected |
|--------|-------------|----------|
| Redis Set of dirty conn IDs | SET hb:<id> + SADD hb:dirty pipeline; flush = SMEMBERS → UPDATE → SREM; O(changed) | ✓ |
| SCAN hb:* each flush | Scan all keys every 10s; re-writes unchanged rows | |
| You decide | — | |

**User's choice:** Redis Set of dirty conn IDs (`hb:dirty`).

---

## Cache invalidation + TTLs (PERF-01, PERF-04)

### Q1 — /servers cache keying (now plan-scoped post-Phase-3)
| Option | Description | Selected |
|--------|-------------|----------|
| Global active-server blob; filter per-plan in Go | One cache:servers:active key; plan filter live in Go; bust on 3 admin server handlers only | ✓ |
| Per-plan cache keys | cache:servers:plan:<id> + admin key; bust on server-writes AND plan↔server mapping changes | |

**User's choice:** Global active-server blob, filter per-plan in Go.

### Q2 — Which paths explicitly bust the user-tier cache (multiSelect)
| Option | Description | Selected |
|--------|-------------|----------|
| Admin user-update | Required by PERF-04 | ✓ |
| Payment webhook Pro-grant | Instant pay→unlock | ✓ |
| User delete / PerformRestore | Zombie token rejected next request | ✓ |
| Scheduler expiry-downgrade | Zero-lag downgrade (else rely on 5s TTL) | ✓ |

**User's choice:** All four — explicit bust on every mutation path (zero-lag everywhere).

### Q3 — c.Locals('user') refactor in scope?
| Option | Description | Selected |
|--------|-------------|----------|
| In scope | Pass AuthRequired-loaded user to handlers; delete redundant FindUserByID re-queries | ✓ |
| Defer to backlog | Caching layer only | |

**User's choice:** In scope.

---

## Verification depth + P2 polish scope

### Q1 — How 'done' is proven
| Option | Description | Selected |
|--------|-------------|----------|
| Assertion-level now, load test deferred to release | EXPLAIN/query-log/pg_stat_activity/gated-replica assertions; 10k load test → release HUMAN-UAT | ✓ |
| Real synthetic load test this phase | k6/vegeta drive ~10k in Phase 6 | |
| Hybrid: assertions + documented load runbook | Assertions now + written runbook for operator | |

**User's choice:** Assertion-level now; real 10k load test deferred to end-of-milestone release phase.

### Q2 — Which P2 nice-to-haves fold in (multiSelect)
| Option | Description | Selected |
|--------|-------------|----------|
| Redis maxmemory-policy | --maxmemory 256mb --maxmemory-policy allkeys-lru | ✓ |
| PG pool tuning | SetMaxIdleConns(25) + SetConnMaxIdleTime(5m) | ✓ |
| Per-job scheduler intervals | Per-job cadence registry | ✓ |
| GORM PrepareStmt | PrepareStmt: true in gorm.Config | ✓ |

**User's choice:** All four fold into Phase 6.

---

## Claude's Discretion

- PERF-07 ctx-rollout mechanism (thread `ctx` through repo signatures — recommended, research to confirm).
- Whether to additionally cache `plan_servers` membership for the D-05 in-Go filter.
- Migration `CONCURRENTLY`-vs-plain strategy given the migration runner's tx behavior.
- Exact `/servers` cache TTL within the ≤5min ceiling (start 60s).

## Deferred Ideas

- Real ~10k synthetic load test → end-of-milestone release phase (HUMAN-UAT).
- Mobile react-query retry jitter (audit §13.3) → Phase 8 cleanup (mobile surface).
- Managed PG/Redis → revisit only past a single second VM.
- Multi-replica API deploy (SCALE-01) → gate built here, deploy post-launch.
