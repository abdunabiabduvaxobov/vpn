# Phase 1: Hotfix — audit critical fixes - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-22
**Phase:** 01-hotfix-audit-critical-fixes
**Areas discussed:** Delivery shape, HOTFIX-08 env scope, HOTFIX-04 error hardening, HOTFIX-01 + 07 migrations

---

## Delivery shape

### Q1: How should the 8 hotfixes be packaged for delivery?

| Option | Description | Selected |
|--------|-------------|----------|
| One commit per hotfix, single deploy | 8 atomic commits, same branch, deployed together as v2.2.0-hotfix. Easy to revert any one fix; one production rollout. | ✓ |
| Grouped by surface area | 3-4 commits grouped (DB-migrations, middleware/auth, runtime/config, CLI). Fewer commits but ties unrelated fixes together. | |
| One mega-commit | Single commit with all 8 fixes. Fastest to land but impossible to bisect/revert. | |

**User's choice:** One commit per hotfix, single deploy
**Notes:** Aligns with the project's fine granularity setting and zero-paying-user freedom to ship cleanly. Each hotfix touches a distinct file:line so atomicity is natural.

### Q2: How should commits be ordered within Phase 1?

| Option | Description | Selected |
|--------|-------------|----------|
| Risk-first: migrations last | CLI + middleware + error fixes first, refresh-rotation next, migrations (HOTFIX-01, 07) last — runtime fixes proven before harder-to-revert schema changes. | ✓ |
| Dependency order from MASTER-PLAN | Follow 0.1–0.8 ordering from `docs/audit/MASTER-PLAN.md`. Mirrors source-of-truth but mixes high and low risk. | |
| Claude decides | Planner picks an order. | |

**User's choice:** Risk-first: migrations last
**Notes:** Order encoded in CONTEXT.md D-02 as HOTFIX-06 → 08 → 04 → 02 → 03 → 05 → 01 → 07.

---

## HOTFIX-08 env scope

### Q1: Which env vars should fail startup if missing/empty in HOTFIX-08?

| Option | Description | Selected |
|--------|-------------|----------|
| Validation framework + required core only | Reusable validator, required set is existing core (DB_*, REDIS_*, JWT_SECRET). Stripe optional/warn. Phase 3 adds LAVA_* to required. | ✓ |
| Include Stripe envs as required now | Treat Stripe keys as required immediately. Closes audit finding literally but creates noise (Stripe leaving in Phase 8). | |
| Pre-add lava.top env keys as required | Make LAVA_API_KEY etc. required at startup now. Breaks local dev until keys exist. | |

**User's choice:** Validation framework + required core only
**Notes:** Keeps the validator reusable for Phase 3 without coupling Phase 1 to Stripe (about to be deleted) or lava (not yet integrated).

### Q2: What should the env validator's failure mode look like?

| Option | Description | Selected |
|--------|-------------|----------|
| Fail fast, list all missing | Scan all required envs, emit single error listing every missing one, then os.Exit(1). | ✓ |
| Fail on first missing | Exit on first missing env. Operator restarts repeatedly. | |
| Warn but continue | Log warnings, keep running. Defeats the purpose of HOTFIX-08. | |

**User's choice:** Fail fast, list all missing
**Notes:** Aggregate scan + single error message lets the operator fix all envs in one pass.

---

## HOTFIX-04 error hardening

### Q1: How should the global ErrorHandler emit errors?

| Option | Description | Selected |
|--------|-------------|----------|
| Generic body + zap structured log + request-ID | Body: `{"error":"internal server error","request_id":"<uuid>"}`; full err+stack to zap stdout; X-Request-ID response header. No external sink. | ✓ |
| Generic body + zap log only, no request-ID | Simpler; operator correlates by timestamp. Less debuggable. | |
| Generic body + Sentry-style external sink | Adds dependency; belongs in Phase 8 hardening. | |

**User's choice:** Generic body + zap structured log + request-ID
**Notes:** Sentry was discussed and deferred to Phase 8.

### Q2: What scope of errors should be scrubbed?

| Option | Description | Selected |
|--------|-------------|----------|
| Only 5xx — 4xx stays verbose | Scrub server errors only. 4xx validation keeps messages so clients can render UX. | ✓ |
| Scrub everything ≥ 400 | Generic message on ALL errors. Maximally defensive but breaks form-validation UX. | |
| Scrub 5xx; 4xx scrubbed only if wrapping GORM/bcrypt err | Walk error chain on 4xx. More work, marginal gain. | |

**User's choice:** Only 5xx — 4xx stays verbose
**Notes:** Matches the audit's actual finding (err.Error() from internal libs leaking on 500s) without breaking client UX.

---

## HOTFIX-01 + 07 migrations

### Q1: Where should the HOTFIX-01 subscription_expires_at fix actually live?

| Option | Description | Selected |
|--------|-------------|----------|
| DB column + scheduler now; webhook persistence in Phase 3 | Add column via migration, fix scheduler. Webhook code (which actually writes the column) lands in Phase 3 with lava.top. Avoid patching Stripe code that's being deleted. | ✓ |
| Patch existing Stripe handler too | Add column AND patch `handler/payment.go:271-294` even though file is deleted in Phase 8. Literal but wasteful. | |
| Defer entirely to Phase 3 | Skip HOTFIX-01 in Phase 1. Violates roadmap (HOTFIX-01 is in Phase 1's requirement list). | |

**User's choice:** DB column + scheduler now; webhook persistence in Phase 3
**Notes:** Captured in CONTEXT.md D-07. Net effect for Phase 1: column exists, scheduler honors it, no writes happen yet (because no payments happen yet). Stripe handler is left untouched and gets removed in Phase 8.

### Q2: How should the HOTFIX-07 UNIQUE index on sessions.refresh_token_hash be created?

| Option | Description | Selected |
|--------|-------------|----------|
| CREATE UNIQUE INDEX CONCURRENTLY + dedupe step | Migration dedupes existing duplicate hashes (keep newest) then creates index concurrently. Works against empty or populated tables. | ✓ |
| Plain CREATE UNIQUE INDEX | Blocking statement. OK with zero paying users but fails hard on duplicates. | |
| DROP TABLE + recreate with constraint | Nukes sessions table — logs every guest user out. Too noisy for a "hotfix". | |

**User's choice:** CREATE UNIQUE INDEX CONCURRENTLY + dedupe step
**Notes:** Defensive against dev/staging data even though production has no paying users.

---

## Claude's Discretion

User explicitly chose "I'm ready for context" when offered four follow-up areas. The following are noted in CONTEXT.md as Claude's Discretion for the planner:

- **Testing strategy** — per-fix unit/integration tests following existing `*_test.go` pattern; manual smoke on staging before the deploy tag.
- **HOTFIX-02 admin DB re-read** — default to pure DB-every-request (success criterion #1 says "next request, not five minutes later").
- **HOTFIX-03 atomic INCR+EXPIRE** — default to Lua `EVAL` script over MULTI/EXEC.
- **HOTFIX-06 createadmin stdin reader** — default to `golang.org/x/term.ReadPassword` (echo-off prompt).
- **Branching** — per `.planning/config.json` `branching_strategy: "none"`, commits land on the working branch.

## Deferred Ideas

- Sentry / external error sink — Phase 8 hardening
- Pre-adding LAVA_* env keys as required — Phase 3
- Patching Stripe webhook to persist current_period_end — no-op (file being deleted in Phase 8)
- Scrubbing 4xx validation errors — out of scope (4xx body is UX surface, not leak surface)
- Per-hotfix individual deploys — rejected in favor of single combined deploy
