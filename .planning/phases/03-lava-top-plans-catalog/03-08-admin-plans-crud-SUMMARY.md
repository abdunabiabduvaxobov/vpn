---
phase: 3
plan: 08
subsystem: backend/handler+middleware+cmd
tags: [handlers, admin, plans, offers, plan-servers, cache-invalidation, audit-log, PAY-13, PAY-14, PAY-15, D-32]
dependency-graph:
  requires:
    - 03-01 (migrations-models-stripe-cleanup) — Plan / PlanOffer / PlanServer models + idx_plans_one_system + idx_plan_offers_unique_active
    - 03-03 (plan-repo) — full CRUD surface (CreatePlan / UpdatePlan / SoftDeletePlan / ReplaceOffer / AddPlanServer + ErrSystemPlan sentinel)
    - 03-07 (public-plans-jwt-cache) — cache.BustPlansCache helper
  provides:
    - handler.AdminListPlans / AdminCreatePlan / AdminGetPlan / AdminUpdatePlan / AdminDeletePlan
    - handler.AdminReplacePlanServers / AdminAddPlanServer / AdminRemovePlanServer
    - handler.AdminListPlanOffers / AdminCreatePlanOffer / AdminUpdatePlanOffer / AdminDeletePlanOffer / AdminReplacePlanOffer
    - handler.bustPlansCacheBest (best-effort cache-invalidation wrapper with WARN logging)
    - handler.validatePlanCode / validatePlanFields / validateOfferTuple (reusable validation helpers)
    - middleware.describeAction recognises 10 plan-CRUD action labels (create_plan, update_plan_offer, etc.)
    - 13 admin routes mounted on /api/v1/admin (inherits AuthRequired + AdminRequired + AuditLog)
  affects:
    - 03-09 (expiry-cron) — independent (touches scheduler + expiry_repo only); no overlap
    - 03-10 (admin-web-plans-ui) — consumes all 13 endpoints as the data source for the plans UI
    - 03-11 (docs-sandbox-smoke) — admin UAT exercises the create/update/replace flows end-to-end
tech-stack:
  added: []
  patterns:
    - "Two-layer defence in depth for is_system: adminPlanCreateReq struct has NO is_system field (json.Unmarshal ignores unknown keys), handler explicitly sets plan.IsSystem=false, repository.CreatePlan ALSO forces false"
    - "Two-layer defence in depth for system-plan delete: handler refuses with 403 BEFORE calling repository.SoftDeletePlan; repository ALSO returns ErrSystemPlan which handler maps to 403"
    - "Best-effort cache invalidation wrapper (bustPlansCacheBest) — WARN-logs failures but never propagates; the 60s TTL is the bounded fallback if Redis DEL is unavailable"
    - "Whole-plan atomic create: plan + plan_servers + plan_offers wrapped in db.Transaction so partial state is impossible"
    - "Pre-validate offers BEFORE opening tx so we fail fast with 400 instead of rolling back a partial create"
    - "Switch-case ordering for describeAction: more-specific URL patterns (/replace, /offers/, /servers/) BEFORE the top-level /admin/plans matches so the most-precise label wins"
    - "ReplaceOffer flow: handler reads old offer via repository.UpdatePlanOffer with empty updates map (short-circuits to a pure SELECT via findOfferByID), inherits periodicity + currency (immutable per ADR §19.7.7), then calls repository.ReplaceOffer for the atomic deactivate-old + insert-new"
    - "409 mapping on partial-unique violation in CreatePlanOffer — surfaced as 'use /replace endpoint to update price' so admin UI can prompt operator with the correct next action"
    - "Test infra carries 03-03 deviations: SetMaxOpenConns(1) for SQLite :memory: tx visibility; randomblob() UUID defaults; users DDL includes all User-model columns; explicit UPDATE-after-INSERT for is_system=true (GORM omits Go zero-value bool)"
key-files:
  created:
    - server/api/internal/handler/plans_admin.go (655 lines, 13 handlers + 3 validation helpers + bustPlansCacheBest)
    - server/api/internal/handler/plans_admin_test.go (669 lines, 5 test functions, 21 subtests)
  modified:
    - server/api/internal/middleware/audit.go (10 new switch-case branches in describeAction)
    - server/api/cmd/main.go (13 new admin route registrations after the /lava/products line)
decisions:
  - "Cache-bust helper extracted as bustPlansCacheBest(c, redisClient, logger, opName) wrapper instead of inline cache.BustPlansCache calls. Rationale: every write handler needs the same fail-open behaviour with WARN-log on failure; the wrapper keeps each call site to one line and ensures the op-name appears in the log so an operator can see WHICH admin action failed to invalidate."
  - "Pre-validate every offer in AdminCreatePlan BEFORE opening the db.Transaction (the plan's verbatim body had the validateOfferTuple call inside the tx callback). Rationale: a bad offer payload returning 400 is more user-friendly than a 500 from a rolled-back tx, and avoids touching the DB at all for malformed input. Functionally equivalent — both paths reject the request — but the new shape is a clear DX win."
  - "Switch-case ordering in describeAction matters: /replace checked first (most-specific), then /offers/ + /servers/ sub-resources, then top-level /admin/plans matches. The plan body called this out; the actual ordering I used puts /replace + /offers/ before /servers/ which avoids any chance of /admin/plans/:id/offers/:offer_id/replace being mistaken for an /admin/plans/:id/servers/... match (it can't be, but ordering for clarity matters when a future case is added)."
  - "Top-level /admin/plans POST uses `stripped == \"/admin/plans\"` (exact match) instead of HasPrefix so PATCH /admin/plans/:id/offers/:offer_id doesn't fall through to create_plan. The case ordering would also catch it, but the exact-match is documentation."
  - "Rule 3 deviation: test DDL `users` table needed the full set of columns from the User GORM model (email_hash, password_hash, telegram_user_id, telegram_linked_at, telegram_username, telegram_first_name, apple_user_id, google_user_id, email_is_private_relay, subscription_expires_at). Caught by Delete_409_OnActiveUsersWithoutForce — db.Create(&model.User{}) tried to INSERT all model fields. Pattern matches setupPaymentTestDB in payment_test.go."
  - "Rule 3 deviation: seedSystemPlan does an explicit UPDATE after Create to force is_system=true. GORM omits non-default bool field values when the struct field is a zero value — but is_system DEFAULT in DDL is 0, so we need the explicit UPDATE to override. (Same trap that bit 03-03 test infra; pattern matches plan_repo_test.go.)"
metrics:
  duration_seconds: 380
  duration_human: "~6 minutes"
  tasks_total: 4
  tasks_complete: 4
  commits: 4
  files_created: 2
  files_modified: 2
  completed_date: "2026-05-24"
  completed_at: "2026-05-24T01:36:08+05:00"
  tests_added: 5    # 5 functions (21 subtests total)
  tests_passing: 5
---

# Phase 3 Plan 08: admin-plans-crud Summary

**One-liner:** 13 admin plan-CRUD HTTP handlers (PAY-13/14/15) with two-layer defence in depth for the system-plan invariants (D-32 §4), every write handler busts the public /plans cache, and the audit middleware now labels each action with a stable operator-friendly name (create_plan, replace_plan_offer, etc.) — all 21 test subtests pass; full repository test suite green.

## What Shipped

### Task 03-08-T01 — `handler/plans_admin.go` (commit `5a8ca07`)

13 admin handlers covering the full plan / plan-server / plan-offer lifecycle per ADR §19.7:

| Handler | Method + Path | Notable behaviour |
|---------|---------------|-------------------|
| `AdminListPlans` | GET /admin/plans | Returns ALL plans (active + inactive) with computed `server_count`, `offer_count`, `active_user_count` per ADR §19.7.1 |
| `AdminCreatePlan` | POST /admin/plans | Whole-plan atomic create (plan + plan_servers + plan_offers in one db.Transaction); is_system FORCED to false (D-32 §4); pre-validates offers before opening tx |
| `AdminGetPlan` | GET /admin/plans/:id | Full detail with servers + offers + active_user_count |
| `AdminUpdatePlan` | PATCH /admin/plans/:id | code + is_system absent from struct (immutable per ADR §19.7.4); returns 403 if attempting to deactivate system plan; repository ALSO strips immutable keys (defence in depth) |
| `AdminDeletePlan` | DELETE /admin/plans/:id | Soft delete; 403 on system plan EVEN WITH ?force=true (D-32 §4 — two-layer: handler check + ErrSystemPlan); 409 on active users without ?force=true |
| `AdminReplacePlanServers` | PUT /admin/plans/:id/servers | Atomic replace; validates every server_id exists + is_active=true (ADR §19.7.6) — 422 on failure |
| `AdminAddPlanServer` | POST /admin/plans/:id/servers/:server_id | Idempotent (201 on re-add per ADR §19.7.6); 422 on inactive server |
| `AdminRemovePlanServer` | DELETE /admin/plans/:id/servers/:server_id | Does NOT force-disconnect active users (D-23); 204 on success, 404 on absent pairing |
| `AdminListPlanOffers` | GET /admin/plans/:id/offers | Returns ALL offers (active + inactive — admin needs to see grandfathered) |
| `AdminCreatePlanOffer` | POST /admin/plans/:id/offers | 409 on partial-unique violation (active offer for this (plan, periodicity, currency) already exists) — caller should use /replace endpoint to version the price |
| `AdminUpdatePlanOffer` | PATCH /admin/plans/:id/offers/:offer_id | periodicity + currency immutable (ADR §19.7.7 — repository ALSO strips); allows amount / lava_offer_id / is_active changes |
| `AdminDeletePlanOffer` | DELETE /admin/plans/:id/offers/:offer_id | Soft (is_active=false); 204 on success, 404 on miss |
| `AdminReplacePlanOffer` | POST /admin/plans/:id/offers/:offer_id/replace | PAY-15 price versioning — old offer deactivated + new offer inserted in one tx (repository.ReplaceOffer); periodicity + currency inherited from old (immutable); 400 if offer doesn't belong to the URL plan |

**Defence in depth (D-32 §4):**

- `adminPlanCreateReq` has NO `is_system` field — JSON unmarshal silently ignores unknown keys.
- `AdminCreatePlan` explicitly sets `plan.IsSystem = false`.
- `repository.CreatePlan` ALSO forces `plan.IsSystem = false`.
- `adminPlanUpdateReq` has NO `code` or `is_system` field.
- `repository.UpdatePlan` ALSO strips `code` / `is_system` / `id` from the updates map.
- `AdminDeletePlan` checks `plan.IsSystem` BEFORE calling repository.
- `repository.SoftDeletePlan` ALSO returns `ErrSystemPlan` on system plan.

**Cache invalidation:**

Every write handler calls `bustPlansCacheBest(c, redisClient, logger, opName)` after a successful 2xx response. The wrapper swallows errors but WARN-logs them with the op name so operators can correlate a stale-cache report with a failed bust. 60s TTL on the cache acts as a bounded fallback if Redis DEL fails.

### Task 03-08-T02 — `handler/plans_admin_test.go` (commit `d0b541b`)

5 test functions covering the plan-level success criteria from 03-VALIDATION.md plus extra defence-in-depth tests:

| Test | Subtests | Coverage |
|------|----------|----------|
| `TestAdminPlansCRUD` (PAY-13) | 10 | Create, Get, Patch, **Patch_System_Deactivate_403** (D-32 §4), **Delete_System_403_EvenWithForce** (D-32 §4), Delete_NonSystem_204_With_NoUsers, Delete_409_OnActiveUsersWithoutForce (with force-true follow-up), **IsSystem_Immutable_FromBody** (D-32 §4 defence in depth), Get_NotFound_404, List (verifies computed counts present) |
| `TestAdminPlanServers` (PAY-14) | 8 | Add_201, Add_Idempotent_StillOK (ADR §19.7.6), Add_InactiveServer_422, Add_NonExistentPlan_404, Remove_204, Remove_404OnAbsent, Replace_AtomicallyReplaces, Replace_RejectsInactiveServer_422 |
| `TestAdminReplaceOffer_Transactional` (PAY-15) | 1 | Verifies old offer is_active=false AND exactly 1 active offer remains with new amount; periodicity + currency inherited from old (immutable); lava_offer_id updated |
| `TestAdminCreatePlan_RejectsCodeRegexFailure` | 1 | Pro!Invalid (regex), 41-char, leading-hyphen all return 400 |
| `TestAdminCreatePlanOffer_RejectsInvalidPeriodicityAndCurrency` | 1 | WEEKLY periodicity, BTC currency, negative amount all return 400 |

Total: **21 subtests pass, 0 fail.**

### Task 03-08-T03 — `middleware/audit.go::describeAction` (commit `4c01474`)

Added 10 new switch-cases to the existing describeAction switch:

```
replace_plan_offer    (POST   ...:id/offers/:offer_id/replace)
update_plan_offer     (PATCH  ...:id/offers/:offer_id)
delete_plan_offer     (DELETE ...:id/offers/:offer_id)
create_plan_offer     (POST   ...:id/offers)
replace_plan_servers  (PUT    ...:id/servers)
add_plan_server       (POST   ...:id/servers/:server_id)
remove_plan_server    (DELETE ...:id/servers/:server_id)
create_plan           (POST   ==/admin/plans)
update_plan           (PATCH  /admin/plans/...)
delete_plan           (DELETE /admin/plans/...)
```

Case ordering: more-specific URL patterns (`/replace`, `/offers/`, `/servers/`) come BEFORE the top-level `/admin/plans` matches so the most-precise label wins. `create_plan` uses exact-match `stripped == "/admin/plans"` instead of HasPrefix as a defence-in-depth (case ordering would catch it anyway).

### Task 03-08-T04 — `cmd/main.go` route wiring (commit `c137471`)

13 new `admin.<METHOD>(...)` registrations added immediately after `admin.Get("/lava/products", ...)`. All routes inherit the admin group's middleware chain: `AuthRequired + AdminRequired + AuditLog(db, logger)`. The audit middleware records each action with the new describeAction labels from T03.

## Verification

**Plan-level success criteria (all 7):**

| # | Criterion | Result |
|---|---|---|
| 1 | `cd server/api && go build ./...` exits 0 | **PASS** |
| 2 | `cd server/api && go test ./... -count=1 -timeout=300s` exits 0 | **PASS** (all packages green) |
| 3 | PAY-13 evidence: `TestAdminPlansCRUD` passes with Create + IsSystem_Immutable_FromBody + Delete_System_403_EvenWithForce subtests | **PASS** (10/10 subtests) |
| 4 | PAY-14 evidence: `TestAdminPlanServers` passes with Add/Idempotent/InactiveServer-422/Remove subtests | **PASS** (8/8 subtests) |
| 5 | PAY-15 evidence: `TestAdminReplaceOffer_Transactional` passes — old offer is_active=false AND exactly 1 active offer remains | **PASS** |
| 6 | All 13 admin routes mounted in cmd/main.go on the admin group | **PASS** (grep returns 13) |
| 7 | describeAction recognises 10 new action names | **PASS** (grep returns 10) |

**Per-task acceptance grep results:**

```
T01 (plans_admin.go):
  grep -c "^func Admin" plans_admin.go                    → 13 (exact)
  grep "cache.BustPlansCache\|bustPlansCacheBest"          → 13 hits (every handler + helper decl)
  grep "fiber.StatusForbidden"                              → 3 hits (system plan delete + system plan deactivate + repo ErrSystemPlan mapping)
  grep "cannot delete system plan\|cannot deactivate system plan" → 3 hits
  grep "is_system" plans_admin.go                          → 5 hits, all in comments OR response-body output;
                                                             NONE in struct field definitions (defence in depth)
  go build ./internal/handler/...                          → exit 0

T02 (plans_admin_test.go):
  grep "TestAdminPlansCRUD"                                → 2 hits (decl + doc)
  grep "TestAdminPlanServers"                              → 2 hits
  grep "TestAdminReplaceOffer_Transactional"               → 2 hits
  grep "Delete_System_403_EvenWithForce\|IsSystem_Immutable_FromBody" → 2 hits (D-32 §4 evidence subtests)
  go test ./internal/handler/ -run "TestAdminPlans|TestAdminPlanServers|TestAdminReplaceOffer|TestAdminCreatePlan" → PASS (21 subtests)

T03 (audit.go):
  grep "create_plan\|update_plan\|delete_plan\|replace_plan_servers\|add_plan_server\|remove_plan_server\|create_plan_offer\|update_plan_offer\|delete_plan_offer\|replace_plan_offer" audit.go → 10 hits (exact match — one per new case)
  go build ./internal/middleware/... && go test ./internal/middleware/... → PASS (2.974s)

T04 (cmd/main.go):
  grep -c 'admin\.\(Get\|Post\|Patch\|Put\|Delete\)("/plans' main.go → 13 (exact)
  grep -c "AdminListPlans\|...\|AdminReplacePlanOffer" main.go        → 13 (exact)
  go build ./... && go test ./... -count=1                            → PASS (all packages)
```

**Final full-suite results:**

```
ok  	vpnapp/server/api/cmd/createadmin           5.354s
ok  	vpnapp/server/api/internal/auth/apple       1.533s
ok  	vpnapp/server/api/internal/auth/google      1.166s
ok  	vpnapp/server/api/internal/cache           10.495s
ok  	vpnapp/server/api/internal/config           2.102s
ok  	vpnapp/server/api/internal/handler          4.722s
ok  	vpnapp/server/api/internal/lava             2.498s
ok  	vpnapp/server/api/internal/middleware       4.094s
ok  	vpnapp/server/api/internal/recovery         1.472s
ok  	vpnapp/server/api/internal/repository       3.365s
ok  	vpnapp/server/api/internal/scheduler        3.579s
ok  	vpnapp/server/api/migrations                5.335s
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 — Test infrastructure / DDL completeness] users DDL needed full User-model column set**

- **Found during:** T02 — `TestAdminPlansCRUD/Delete_409_OnActiveUsersWithoutForce` failed with `table users has no column named email_hash` when seeding a user via `db.Create(&model.User{...})`.
- **Issue:** The plan's verbatim test DDL had a minimal `users` table (id, email, email_verified, full_name, subscription_tier, role, auth_provider, plan_id). GORM `db.Create(&model.User{...})` emits an INSERT for every field on the struct — including `email_hash`, `password_hash`, `telegram_user_id`, `telegram_linked_at`, `telegram_username`, `telegram_first_name`, `apple_user_id`, `google_user_id`, `email_is_private_relay`, `subscription_expires_at`. Without those columns in the test DDL, the INSERT fails.
- **Fix:** Extended the `users` CREATE TABLE in `setupAdminPlansDB` to mirror `setupPaymentTestDB` in `payment_test.go` (same package — the canonical reference for this test infra in handler/).
- **Files modified:** server/api/internal/handler/plans_admin_test.go
- **Commit:** rolled into T02 (`d0b541b`) — single DDL block; not a separate fix.

**2. [Rule 3 — Test infrastructure / GORM bool-zero-value trap] seedSystemPlan needed explicit UPDATE for is_system=true**

- **Found during:** T02 — initial probe of `TestAdminPlansCRUD/Delete_System_403_EvenWithForce` would have returned 200 instead of 403 because the seeded "free" row was actually `is_system=false`.
- **Issue:** GORM's `db.Create(&model.Plan{..., IsSystem: true, ...})` omits boolean fields with their zero value from INSERT; combined with the SQLite DDL DEFAULT `is_system INTEGER NOT NULL DEFAULT 0`, this means the row ends up with `is_system=0`. Same trap as 03-03 test infra (see `plan_repo_test.go::seedRows`).
- **Fix:** Added an explicit `db.Model(&model.Plan{}).Where("id = ?", id).Update("is_system", true)` after the Create in `seedSystemPlan`. Caught preemptively before writing the assertions.
- **Files modified:** server/api/internal/handler/plans_admin_test.go
- **Commit:** rolled into T02 (`d0b541b`).

**3. [Rule 1 — DX improvement] Pre-validate offers BEFORE opening db.Transaction in AdminCreatePlan**

- **Found during:** T01 — the plan's verbatim body had `validateOfferTuple(...)` inside the tx callback. A bad offer would still return 500 (because the validation returns an error from inside the tx, which rolls back the plan-level insert too).
- **Issue:** Functionally correct (the request is rejected and no row is persisted) but the response code is wrong — a 400 (Bad Request) is the appropriate signal for malformed input, not a 500 (Internal Server Error). The shape also wastes DB work (the plan and any preceding plan_server inserts happen before validation rejects on offer N).
- **Fix:** Hoisted the per-offer `validateOfferTuple` loop to BEFORE the `db.Transaction(...)` call. The tx body now only contains the actual DB writes, which means any 500 from it is a genuine DB error.
- **Files modified:** server/api/internal/handler/plans_admin.go
- **Commit:** rolled into T01 (`5a8ca07`).

**4. [Rule 2 — Critical functionality] AdminListPlanOffers + AdminCreatePlanOffer verify plan exists**

- **Found during:** T01 — plan body's verbatim AdminListPlanOffers just called `repository.ListOffersForPlan(...)` and returned the result. AdminCreatePlanOffer's plan-exists check was less prominent.
- **Issue:** Hitting `/admin/plans/<nonexistent-id>/offers` would return `200 OK` with an empty list — misleading because the URL implies the plan IS valid. Better: 404 explicitly so the admin UI's "create offer" form fails cleanly when the plan was deleted in another tab.
- **Fix:** Added `repository.FindPlanByID` check at the top of both handlers; 404 on not-found.
- **Files modified:** server/api/internal/handler/plans_admin.go
- **Commit:** rolled into T01 (`5a8ca07`).

**5. [Rule 1 — Operator UX] bustPlansCacheBest wrapper with WARN logging**

- **Found during:** T01 — plan body's verbatim cache-bust was `_ = cache.BustPlansCache(c.Context(), redisClient)` on each handler. Silently swallowing the error means a stale-cache outage is invisible.
- **Issue:** If Redis is partitioned, the public /pricing page shows stale data for up to 60s after every admin write. Without a log line, the operator can't correlate "users see old prices" with "the bust call failed at 14:23:01". Plan body said "_ =" the error; pragmatic improvement is "warn and continue".
- **Fix:** Extracted `bustPlansCacheBest(c, redisClient, logger, opName)` helper that calls `cache.BustPlansCache` and logs `WARN: plans cache bust failed op=create_plan` on error. Returns no error so handler call sites stay one line.
- **Files modified:** server/api/internal/handler/plans_admin.go
- **Commit:** rolled into T01 (`5a8ca07`).

### Deferred Issues

None — all in-scope work landed clean. Downstream owed work:

- **Plan 03-09 (expiry-cron)** is the wave-4 sibling running in parallel with this plan; it touches `scheduler/scheduler.go` + `repository/expiry_repo.go` only — no overlap with my files.
- **Plan 03-10 (admin-web-plans-ui)** consumes all 13 endpoints; the request/response shapes documented here are stable and grep-stable. The 409 partial-unique handling on CreatePlanOffer assumes the UI surfaces the "use /replace endpoint" error message to the operator.

## Threat Model Compliance

All STRIDE mitigations in the plan's `<threat_model>` (T-03-57 through T-03-69) are in code:

| Threat ID | Mitigation in code |
|-----------|--------------------|
| T-03-57 (EoP: POST `{"is_system":true}`) | `adminPlanCreateReq` struct has NO is_system field; handler forces `plan.IsSystem = false`; `repository.CreatePlan` ALSO forces false. PAY-13 evidence test `IsSystem_Immutable_FromBody` confirms. |
| T-03-58 (EoP: PATCH `{"is_system":true}`) | `adminPlanUpdateReq` has NO code OR is_system fields; `repository.UpdatePlan` ALSO strips both keys. |
| T-03-59 (Tampering: DELETE system plan with ?force=true) | Two-layer check: (1) handler checks `plan.IsSystem` BEFORE calling repository; (2) repository returns ErrSystemPlan → handler maps to 403. PAY-13 evidence test `Delete_System_403_EvenWithForce` confirms. |
| T-03-60 (Tampering: code regex bypass) | `validatePlanCode` enforces `^[a-z0-9][a-z0-9_-]*$` + 1-40 char length BEFORE the DB write. `TestAdminCreatePlan_RejectsCodeRegexFailure` covers regex / length / leading-hyphen failures. GORM parameterises INSERT — even if regex were bypassed, value is bound. |
| T-03-61 (Info disclosure: cross-tenant) | **Accepted** — single-tenant deployment (CLAUDE.md). |
| T-03-62 (Repudiation) | `AuditLog` middleware records every write with admin user_id + action label (extended via describeAction in T03) + path params + query string. |
| T-03-63 (DoS: rapid CREATE) | **Accepted per plan** — global per-IP rate limiter (HOTFIX-03) caps. Admin endpoints are NOT exempt. |
| T-03-64 (Tampering: negative max_devices) | `validatePlanFields` enforces max_devices -1 OR 1..1000; max_servers -1 OR 0..9999; speed_limit_mbps 0..100000. Returned 400 before DB write. |
| T-03-65 (Tampering: AddPlanServer for inactive server) | Both AddPlanServer + ReplacePlanServers handlers check `vpn_servers.is_active = TRUE` BEFORE calling repository. Returns 422 on failure. `TestAdminPlanServers/Add_InactiveServer_422` + `Replace_RejectsInactiveServer_422` confirm. |
| T-03-66 (EoP: direct DB write of is_system) | **Accepted per plan** — out of scope; PARTIAL UNIQUE INDEX `idx_plans_one_system` rejects a second system plan even via raw SQL. |
| T-03-67 (Tampering: ReplaceOffer race vs in-flight checkout) | ReplaceOffer wrapped in tx (repository.ReplaceOffer). User pays the price visible at /checkout time; new sign-ups after the commit see the new price (ADR §19.10 grandfathering). |
| T-03-68 (Tampering: duplicate lava_offer_id) | Migration 019's `idx_plan_offers_lava_offer_id` partial unique index rejects duplicate at DB layer → handler returns 409 via the existing CreatePlanOffer catch (logged as "rejected (likely duplicate active)"). |
| T-03-69 (DoS: cache bust floods Redis) | **Accepted per plan** — bounded by 3 currencies × write rate (~10/day); negligible cost. |

ASVS L2 controls applied: V2 (HOTFIX-02 AdminRequired re-reads role per request — admin group inherits), V4 (audit log + role re-check), V5 (regex + length + enum + range validation), V8 (is_system never accepted from body), V11 (system plan immutable + ReplaceOffer transactional grandfathering).

## Threat Flags

None. All 13 new endpoints are admin-only behind the `/api/v1/admin` group (AuthRequired + AdminRequired + AuditLog). No new outbound calls. No new schema. The only new surface (`/admin/plans/*` URL space) is fully enumerated in the plan's `<threat_model>` with mitigate dispositions for all 13 threats.

## Known Stubs

None. Every handler returns real data from the repository layer (no hardcoded empties flowing to UI rendering). The `bustPlansCacheBest` wrapper is a real Redis DEL call (or fail-open if client is nil — but `cmd/main.go` always provides a real `redisClient`). Validation helpers return real `error` values with descriptive messages.

`Empty []` returns in the success path of AdminReplacePlanServers (when `server_ids: []` is intentional — admin wants the plan to have zero servers) is documented behaviour per ADR §19.7.6, not a stub.

## Commits

| Task | Hash | Type | Message |
|------|------|------|---------|
| T01 | `5a8ca07` | feat | add 13 admin plan-CRUD handlers (PAY-13/14/15) |
| T02 | `d0b541b` | test | plans_admin_test.go (PAY-13/14/15 + D-32 §4 invariants) |
| T03 | `4c01474` | feat | extend describeAction with 10 plan-CRUD action labels |
| T04 | `c137471` | feat | wire 13 admin plan routes in cmd/main.go (PAY-13/14/15) |

## Downstream Consumers

- **Plan 03-10 (admin-web-plans-ui):** All 13 endpoints are the data source for the admin plans UI. Request/response shapes are stable. The 409 "active offer already exists; use /replace endpoint" message is the operator-facing prompt.
- **Plan 03-11 (docs-sandbox-smoke):** UAT exercises create + add server + create offer + replace offer + delete; verifies the public /plans cache reflects admin writes after the bust DEL.
- **Phase 7 ADMIN-06 (audit log UI):** The 10 new describeAction labels show up verbatim in the audit_log.action column — UI's Action filter dropdown should add: create_plan, update_plan, delete_plan, replace_plan_servers, add_plan_server, remove_plan_server, create_plan_offer, update_plan_offer, delete_plan_offer, replace_plan_offer.

## Self-Check: PASSED

Files exist:
- FOUND: server/api/internal/handler/plans_admin.go
- FOUND: server/api/internal/handler/plans_admin_test.go
- FOUND: server/api/internal/middleware/audit.go (modified)
- FOUND: server/api/cmd/main.go (modified)
- FOUND: .planning/phases/03-lava-top-plans-catalog/03-08-admin-plans-crud-SUMMARY.md (this file)

Commits exist (verified via `git log --oneline -6`):
- FOUND: 5a8ca07 (T01 plans_admin.go)
- FOUND: d0b541b (T02 plans_admin_test.go)
- FOUND: 4c01474 (T03 audit.go extension)
- FOUND: c137471 (T04 cmd/main.go wiring)

Verification:
- `cd server/api && go build ./...` → exit 0 — PASS
- `cd server/api && go vet ./...` → exit 0 — PASS
- `cd server/api && go test ./internal/handler/ -run "TestAdminPlans|TestAdminPlanServers|TestAdminReplaceOffer|TestAdminCreatePlan" -count=1` → 21 subtests PASS
- `cd server/api && go test ./... -count=1 -timeout=300s` → ALL packages PASS (no regressions in cache/middleware/repository/scheduler/migrations)
- All 7 plan-level success criteria — PASS
- All 4 task-level acceptance grep results — PASS
