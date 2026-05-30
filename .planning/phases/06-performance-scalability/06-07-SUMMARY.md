---
phase: 06-performance-scalability
plan: 07
subsystem: infra
tags: [runbook, deploy, postgres, indexes, docker-compose, uat, operations]

# Dependency graph
requires:
  - phase: 06-performance-scalability
    provides: migrations 022/023 (06-02), docker-compose.data.yml split (06-01), heartbeat flush + scheduler gate (06-05), ctx propagation (06-06)
provides:
  - Production deploy runbook for the manual live-DB index backfill, co-located bring-up, and off-host data-tier move
  - Deferred-UAT tracker (06-HUMAN-UAT.md) for the operator-gated backfill and the deferred ~10k load test + second-host move
  - Human-verified operational sign-off closing the "CI green / prod missing" gap
affects: [release-phase, ops]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Operational caveats captured as explicit, human-gated runbook + HUMAN-UAT deliverables — never silently false-positived by a fresh-volume CI run"

key-files:
  created:
    - docs/runbooks/06-perf-deploy-runbook.md
    - .planning/phases/06-performance-scalability/06-HUMAN-UAT.md
  modified: []

key-decisions:
  - "The live-DB index backfill is a tracked operator gate (PENDING-OPERATOR), not an in-phase task — docker-entrypoint-initdb.d only fires on an empty pgdata volume"
  - "The ~10k synthetic load test and the physical second-host move are DEFERRED to the release phase, consistent with how Phases 4/5 deferred hardware-dependent UAT"
  - "The five D-09 automated assertions are the in-phase definition-of-done; the live load test is release-time confirmation only"

patterns-established:
  - "Document the empty-volume migration caveat + the warning sign (CI EXPLAIN green but prod pg_stat_user_indexes idx_scan=0) so the gap is operator-detectable"

requirements-completed: [PERF-03, PERF-05, PERF-08]

# Metrics
duration: ~10min (inline orchestrator execution + human checkpoint)
completed: 2026-05-30
---

# Phase 06 Plan 07: Performance Deploy Runbook + Deferred-UAT Tracker

**Production runbook for the manual live-DB index backfill (the empty-volume initdb caveat made operator-actionable), the co-located two-file compose bring-up, and the off-host data-tier move — plus a HUMAN-UAT tracker for the deferred load test, human-verified.**

## Performance

- **Duration:** ~10 min (executed inline by the orchestrator — docs-only plan with a blocking human checkpoint)
- **Completed:** 2026-05-30
- **Tasks:** 3/3 (2 auto + 1 human-verify checkpoint, approved)
- **Files created:** 2

## Accomplishments
- **Task 1 — runbook** (`docs/runbooks/06-perf-deploy-runbook.md`): why a manual backfill is required (initdb empty-volume semantics + the CI-green/prod-`idx_scan=0` warning sign); verbatim `psql -f CREATE INDEX CONCURRENTLY` backfill commands for 022/023 with pg_indexes + pg_stat_user_indexes verification; co-located two-file bring-up with Redis maxmemory + tuned postgresql.conf confirmation; off-host data-tier move (DB_HOST/REDIS_HOST + private interface + firewall + TLS-ready upgrade, D-02); rollback/safety.
- **Task 2 — HUMAN-UAT** (`06-HUMAN-UAT.md`): three operator/deferred gates — live backfill (PENDING-OPERATOR), ~10k load test (DEFERRED to release), second-host provisioning (DEFERRED) — cross-referencing the runbook and noting the five D-09 assertions as the in-phase definition-of-done.
- **Task 3 — human-verify checkpoint:** operator reviewed the runbook + tracker and replied **approved**.

## Task Commits

1. **Task 1 + Task 2: runbook + HUMAN-UAT tracker** - `19db5fc` (docs)
2. **Task 3: human-verify checkpoint** - approved by operator (no file change; checkpoint task)

## Files Created/Modified
- `docs/runbooks/06-perf-deploy-runbook.md` - prod deploy runbook (backfill, bring-up, off-host move, rollback)
- `.planning/phases/06-performance-scalability/06-HUMAN-UAT.md` - deferred/operator-gated UAT tracker

## Decisions Made
- Live-DB index backfill tracked as a PENDING-OPERATOR gate (manual online step), not an in-phase task.
- ~10k load test + physical second-host move DEFERRED to the release phase (D-09).

## Deviations from Plan
None — executed inline by the orchestrator (docs-only checkpoint plan); content follows the plan's `<action>` specs verbatim. Inline execution (vs. a worktree subagent) was chosen because the plan modifies no code and pauses for a human gate, so worktree isolation would only add merge complexity.

## Issues Encountered
None.

## User Setup Required
**Operational follow-up (tracked in 06-HUMAN-UAT.md):**
- Run the live-DB index backfill (`docs/runbooks/06-perf-deploy-runbook.md` §2) on the next prod deploy — PENDING-OPERATOR.
- ~10k load test + physical second-host move — DEFERRED to the release phase.

## Next Phase Readiness
- Phase 6 code complete (06-01…06-06) with automated assertions GREEN; operational gaps documented and human-verified.
- The live index backfill is the one mandatory operator action on the next prod deploy.

---
*Phase: 06-performance-scalability*
*Completed: 2026-05-30*
