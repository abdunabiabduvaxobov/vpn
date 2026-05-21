# Phase 1: Hotfix — audit critical fixes - Context

**Gathered:** 2026-05-22
**Status:** Ready for planning

<domain>
## Phase Boundary

Land the 8 stop-the-bleeding fixes (HOTFIX-01 through HOTFIX-08) from `docs/audit/MASTER-PLAN.md` Tranche 0 so the live Go API is safe to extend with SSO (Phase 2) and real money flow (Phase 3). Each fix is already mapped to a specific file:line in the audit; this phase ships those fixes, no additional capability.

In-scope: behavior changes only at the cited file:line locations plus required migrations.
Out-of-scope: any HIGH/MEDIUM audit finding not listed in the 8 hotfixes, any lava.top or SSO work, any Stripe refactor beyond what's needed to ship Tranche 0.

</domain>

<decisions>
## Implementation Decisions

### Delivery shape
- **D-01:** One atomic commit per hotfix (8 commits total) on the working branch. Single combined deploy at the end (no per-fix rolling deploys). Tag `v2.2.0-hotfix` after smoke test on staging, per MASTER-PLAN.md Tranche 0 exit criteria.
- **D-02:** Risk-first ordering. Land lowest-blast-radius fixes first, migrations last:
  1. HOTFIX-06 (`createadmin` CLI — local-only, no runtime impact)
  2. HOTFIX-08 (env validation framework — fails fast at startup)
  3. HOTFIX-04 (ErrorHandler scrub + request-ID — runtime, but only error paths)
  4. HOTFIX-02 (`AdminRequired` DB re-read — middleware, hot path but bounded)
  5. HOTFIX-03 (atomic INCR+EXPIRE — middleware, Redis behavior change)
  6. HOTFIX-05 (transactional refresh rotation — handler, auth hot path)
  7. HOTFIX-01 (`subscription_expires_at` migration + scheduler — DB schema change)
  8. HOTFIX-07 (UNIQUE index on `sessions.refresh_token_hash` — DB schema, may need dedup)

### HOTFIX-08 — env validation scope
- **D-03:** Build a reusable required-env validator in `internal/config/`. Initial required set is the existing core only: `DB_*`, `REDIS_*`, `JWT_SECRET` (and anything else the running v2.1.0 actually depends on at startup). Stripe env vars become **optional with warn-log** since Stripe is leaving in Phase 8. `LAVA_*` keys are NOT pre-added here — Phase 3 adds them to the required set when lava.top integration lands.
- **D-04:** Fail-fast aggregate mode. The validator scans every required env in one pass, then if any are missing/empty emits a SINGLE log line listing all of them, then `os.Exit(1)`. No partial startups, no per-env restarts.

### HOTFIX-04 — error hardening
- **D-05:** Global Fiber `ErrorHandler` returns generic body `{"error":"internal server error","request_id":"<uuid>"}` for 5xx responses. Full error chain + stack go to the existing zap logger (stdout, picked up by `docker logs`) as a structured event with the same `request_id`. The `request_id` is also echoed back to the client in an `X-Request-ID` response header. No external sink (Sentry, etc.) — that belongs in Phase 8 hardening.
- **D-06:** Scrub scope is 5xx only. 4xx responses (including validation errors) keep their existing verbose messages so clients can still render user-facing errors like "email required". The audit finding is specifically about `err.Error()` from GORM/bcrypt leaking on 500s, which is what this fix targets.

### HOTFIX-01 — placement
- **D-07:** Add `subscription_expires_at TIMESTAMPTZ NULL` column via migration in Phase 1. Update the scheduler (the periodic job that downgrades expired Pro users) to read it. Do NOT patch the existing Stripe handler at `handler/payment.go:271-294` — that file is being deleted entirely in Phase 8 and there are zero paying Stripe users in production. The webhook code that actually populates `subscription_expires_at` lands in Phase 3 with the lava.top webhook handler. Net effect for Phase 1: column exists, scheduler honors it, no writes happen yet because no payments happen yet.

### HOTFIX-07 — migration shape
- **D-08:** Two-step migration. Step 1: deduplicate any existing rows sharing the same `refresh_token_hash` (keep the row with the newest `created_at`, delete the rest). Step 2: `CREATE UNIQUE INDEX CONCURRENTLY idx_sessions_refresh_token_hash_unique ON sessions(refresh_token_hash)`. Works against both empty and populated tables, no production lock. The dedupe step is defensive even though no paying users exist — dev and staging DBs may have stale guest sessions.

### Claude's Discretion
- **Testing strategy:** Planner picks per-fix granularity. Default expectation: every fix gets at least one targeted unit/integration test that fails before the fix and passes after; existing `*_test.go` files in `handler/` and `middleware/` are the established pattern. Manual smoke on staging is required regardless before the v2.2.0-hotfix tag.
- **HOTFIX-02 (admin DB re-read):** Planner chooses between pure DB-every-admin-request vs a very short Redis-backed cache. Constraint: success criterion #1 says "very next request, not five minutes later" — anything longer than ~1s TTL violates the spirit. Default to pure DB read unless the planner sees a clear hot-path concern.
- **HOTFIX-03 (atomic INCR+EXPIRE):** Planner chooses Lua `EVAL` script vs `MULTI/EXEC` pipeline. Default to Lua — it's the canonical atomic pattern and easier to reason about under partial failures. The script can live inline in `cache/redis.go`.
- **HOTFIX-06 (createadmin stdin reader):** Planner chooses `golang.org/x/term.ReadPassword` (echo-off prompt, sudo-style) vs raw `bufio.NewReader`. Default to `term.ReadPassword` for the password-leak resistance benefit.
- **Branching:** Per `.planning/config.json` `branching_strategy: "none"`, all 8 commits land on the working branch directly. No per-phase branch.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Audit reports — authoritative source for the 8 hotfixes
- `docs/audit/MASTER-PLAN.md` §Tranche 0 — file:line citation for every hotfix; this is the source-of-truth list
- `docs/audit/CODE-REVIEW.md` — search for CRIT-01 (HOTFIX-01), CRIT-02 (HOTFIX-03), CRIT-03 (HOTFIX-02), CRIT-04 (HOTFIX-04), HIGH-08 (HOTFIX-08)
- `docs/audit/SECURITY-AUDIT.md` — search for S1-1 (HOTFIX-05), S2-1 (HOTFIX-06), S2-2 (HOTFIX-02), S3-4/S3-5 (HOTFIX-08), S9-1 (HOTFIX-04)
- `docs/audit/PERFORMANCE-AUDIT.md` — search for Perf #1 (HOTFIX-07 sessions UNIQUE index detail and EXPLAIN expectations)

### Roadmap / project decisions
- `.planning/ROADMAP.md` §Phase 1 — phase goal, depends-on, requirement list, 8 numbered success criteria
- `.planning/REQUIREMENTS.md` §"Hotfix (audit findings — Tranche 0)" — HOTFIX-01 through HOTFIX-08 acceptance criteria
- `.planning/PROJECT.md` §"Key Decisions" — pre-locked decisions that govern multiple hotfixes (transactional refresh rotation, AdminRequired DB re-read)

### Architecture context (for forward-compatibility decisions)
- `docs/ADR-007-lava-sso-rework.md` — explains what Phase 2 (SSO) and Phase 3 (lava.top) will need; informs HOTFIX-01 placement and HOTFIX-08 env scope
- `docs/audit/MASTER-PLAN.md` §Tranche 1, §Tranche 2 — for understanding what NOT to pre-build in Phase 1

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `server/api/cmd/main.go:61` — Fiber app construction; ErrorHandler is wired at line 64 via `handler.ErrorHandler(logger)`. HOTFIX-04 edits the function this points to.
- `server/api/internal/handler/health.go:155-172` — current `ErrorHandler` body; the leak site.
- `server/api/internal/middleware/admin.go:8-17` — `AdminRequired` middleware; reads role from JWT claim today, HOTFIX-02 changes it to a DB lookup.
- `server/api/internal/middleware/ratelimit.go` — rate-limit middleware; calls into cache layer.
- `server/api/internal/cache/redis.go:67-96` — `IncrRateLimit` with the non-atomic INCR+EXPIRE; HOTFIX-03 rewrites this.
- `server/api/internal/handler/auth.go:241-263` — refresh-token rotation; HOTFIX-05 wraps in a transaction.
- `server/api/internal/handler/payment.go:271-294` — existing Stripe webhook handler. **DO NOT patch for HOTFIX-01** — file is deleted in Phase 8.
- `server/api/internal/config/config.go:48-77` — env loading; HOTFIX-08 adds a validator that runs before Fiber starts.
- `server/api/cmd/createadmin/main.go:29-79` — CLI tool; HOTFIX-06 changes the password input path + the hardcoded seed tier.
- `server/api/migrations/` — SQL migration directory (existing `001` is the initial schema). HOTFIX-01 and HOTFIX-07 each add a new migration file here.

### Established Patterns
- **Testing:** every package under `internal/handler/` and `internal/middleware/` already has `*_test.go` siblings (auth_test.go, admin_test.go, payment_test.go, ratelimit_test.go, etc.). Planner should follow this pattern — one test file per touched component.
- **Logging:** zap logger is injected into handlers/middleware (passed to `ErrorHandler(logger)` in main.go). Reuse the existing instance for HOTFIX-04 structured logs.
- **Config loading:** `internal/config/config.go` returns a `*Config` struct; HOTFIX-08 validator is called between config-load and Fiber-init in `cmd/main.go`.
- **Migrations:** numbered SQL files in `server/api/migrations/`. HOTFIX-01 and HOTFIX-07 each get the next sequential number(s).

### Integration Points
- HOTFIX-04 (ErrorHandler) wraps every handler, so the request-ID middleware must run BEFORE any handler that could 500 — add it early in the Fiber middleware chain in `cmd/main.go`.
- HOTFIX-08 (env validator) runs in `cmd/main.go` between `config.Load()` and `fiber.New()` — fails before any port is bound.
- HOTFIX-01 (subscription_expires_at) is read by the scheduler in `internal/scheduler/` — planner should check whether the scheduler is already started in main.go and just ensure the new column is in the model.
- HOTFIX-07 (sessions UNIQUE index) is queried by `handler/auth.go` refresh logic — the index name should match anything the audit's EXPLAIN expectations cite.

</code_context>

<specifics>
## Specific Ideas

- Deploy tag is `v2.2.0-hotfix` (or just `v2.2.0` per MASTER-PLAN.md Tranche 0 exit criteria — confirm with planner). Planner should produce a single staged deploy at the end of all 8 commits, with smoke-test steps before the tag is pushed.
- "No paying users in v2.1.0" — operator's repeated confirmation. This means: behavior changes that log out guest users on dedup (HOTFIX-07 step 1) are acceptable; behavior changes that break security guarantees are not.
- Request-ID format: UUIDv4 string. Header name: `X-Request-ID`. If a request arrives with `X-Request-ID` already set (e.g., from a proxy), accept it; otherwise generate.
- Seed admin tier fix in HOTFIX-06: the spec says `subscription_tier='free'` (not `"ultimate"` which is the current bug). Per ROADMAP.md success criterion #8.

</specifics>

<deferred>
## Deferred Ideas

- **Sentry / external error sink** — raised during HOTFIX-04 discussion. Belongs in Phase 8 hardening, not Phase 1.
- **Pre-adding LAVA_* env keys to the required validator** — raised during HOTFIX-08 discussion. Phase 3 owns this when lava.top integration lands.
- **Patching the Stripe webhook to persist `current_period_end`** — raised during HOTFIX-01 discussion. No-op; the Stripe handler file is being deleted in Phase 8 and has zero paying users.
- **Scrubbing 4xx validation errors** — raised during HOTFIX-04 discussion. Out of scope: 4xx body content is client-UX surface, not a leak surface.
- **Per-hotfix individual deploys** — raised during delivery-shape discussion. Single combined deploy is cleaner; per-fix rolling deploys add ops overhead with no benefit.

</deferred>

---

*Phase: 01-hotfix-audit-critical-fixes*
*Context gathered: 2026-05-22*
