---
phase: 3
plan: 03
subsystem: backend/repository
tags: [repository, plans, offers, invoices, lava, plan-repo, invoice-repo, PAY-08, PAY-09, PAY-11]
dependency-graph:
  requires:
    - 03-01 (migrations-models-stripe-cleanup) — Plan / PlanServer / PlanOffer / Invoice / Subscription models
    - 03-02 (lava-client-config) — none directly consumed; sibling-wave dependency only for downstream handler integration
  provides:
    - repository.FindPlanByID / FindPlanByCode / FindSystemPlanID / ListActivePlans / ListAllPlans
    - repository.ListServersForPlan / IsServerAllowedForPlan (PAY-11, D-21)
    - repository.FindActiveOffer / FindOfferByLavaOfferID (PAY-08 reverse-lookup, ADR §19.10 grandfathering)
    - repository.ListOffersForPlan / ListActiveOffersForPublic
    - repository.ListPlanServerCountries / ListPlanServersJoined
    - repository.SetUserPlan (PAY-09 — transactional users + subscription upsert)
    - repository.CreatePlan / UpdatePlan / SoftDeletePlan / CountActiveUsersOnPlan
    - repository.ReplacePlanServers / AddPlanServer / RemovePlanServer
    - repository.CreatePlanOffer / UpdatePlanOffer / DeletePlanOffer / ReplaceOffer (ADR §19.10 versioning)
    - repository.ErrSystemPlan sentinel + repository.NormalizePlanCode helper
    - repository.CreateInvoice / FindInvoiceByID / FindInvoiceByLavaID
    - repository.FindActivePendingInvoice (ADR §9.2 60s checkout idempotency)
    - repository.UpdateInvoiceStatus
  affects:
    - 03-04 (server-access-enforcement) — consumes ListServersForPlan + IsServerAllowedForPlan; can now remove legacyPlanLimits shim
    - 03-05 (checkout) — consumes FindActiveOffer + FindActivePendingInvoice + CreateInvoice
    - 03-06 (webhook-handler) — consumes FindOfferByLavaOfferID + FindInvoiceByLavaID + SetUserPlan + UpdateInvoiceStatus
    - 03-07 (public-plans) — consumes ListActivePlans + ListActiveOffersForPublic + ListPlanServerCountries
    - 03-08 (admin-plans-crud) — consumes the full plan / offer / plan-server CRUD surface
    - 03-09 (expiry-cron) — consumes FindSystemPlanID + SetUserPlan(planID=systemID, contractID=nil, expiresAt=nil)
tech-stack:
  added: []
  patterns:
    - "SELECT-then-INSERT-or-UPDATE upsert wrapped in db.Transaction (SetUserPlan), mirroring subscription_repo.CreateOrUpdateSubscription"
    - "Defence-in-depth at the repository layer: UpdatePlan strips immutable keys (code, is_system, id); UpdatePlanOffer strips periodicity/currency/id/plan_id; CreatePlan forces plan.IsSystem=false"
    - "Idempotent upsert via find-first pattern (AddPlanServer) — POST returns 200 on already-present per ADR §19.7.6"
    - "Atomic replacement pattern (ReplacePlanServers, ReplaceOffer) — DELETE + bulk INSERT (or deactivate + insert) in a single transaction"
    - "Grandfathered renewal lookup (FindOfferByLavaOfferID NOT filtered on is_active) — webhook PAY-08 must resolve inactive offers per ADR §19.10"
    - "Time-windowed reuse helper (FindActivePendingInvoice) — caller-supplied window matches ADR §9.2 60s idempotency"
    - "SQLite test schema with `id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16))))` so GORM's default:gen_random_uuid() tag doesn't leave id=NULL inside transactions"
    - "SetMaxOpenConns(1) on test sqlite handles to keep db.Transaction(...) on the same per-connection in-memory database"
key-files:
  created:
    - server/api/internal/repository/plan_repo.go (27 functions, 452 lines)
    - server/api/internal/repository/plan_repo_test.go (15 tests, 481 lines)
    - server/api/internal/repository/invoice_repo.go (5 functions, 84 lines)
    - server/api/internal/repository/invoice_repo_test.go (5 tests, 137 lines)
  modified: []
decisions:
  - "Rule 1 deviation: subscriptions / invoices test DDL needed `id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16))))` because GORM's `default:gen_random_uuid()` tag makes the driver omit the column on INSERT; with no DB-side default in Postgres-shaped DDL the row stored id=NULL which broke `tx.Model(&existing).Updates(...)` with 'WHERE conditions required' on the SetUserPlan renewal path. Pattern matches subscription_repo_test.go::openTestDB which uses the same DEFAULT."
  - "Rule 1 deviation: seeding rows with `IsActive: false` is silently coerced to true because GORM omits Go zero-value bool fields and the SQLite DDL defaults `is_active INTEGER NOT NULL DEFAULT 1`. Fixed by inserting with IsActive=true then issuing an UPDATE — same pattern subscription_repo_test.go::TestFindSubscriptionByUserID_SkipsInactiveSub uses."
  - "vpn_servers test DDL extended with region/city/capacity NOT NULL DEFAULT '' columns (omitted by the plan's verbatim body) so `db.Create(&model.VPNServer{...})` succeeds — the GORM model declares these as `not null` and would otherwise hit `table vpn_servers has no column named region`."
  - "SetMaxOpenConns(1) + SetMaxIdleConns(1) on every test sqlite handle (matches handler/auth_test.go) so db.Transaction(...) sees prior db.Create(...) writes."
metrics:
  duration_seconds: 1058
  duration_human: "~17 minutes"
  tasks_total: 3
  tasks_complete: 3
  commits: 3
  files_created: 4
  files_modified: 0
  completed_date: "2026-05-23"
---

# Phase 3 Plan 03: plan-repo Summary

**One-liner:** Pure-GORM repository layer for the dynamic plans catalog (27 functions in `plan_repo.go`) and lava-issued invoices (5 functions in `invoice_repo.go`), covering the PAY-08 grandfathered offer reverse-lookup, the PAY-09 transactional SetUserPlan upsert, the PAY-11 server-access enforcement filters, and the ADR §9.2 60-second checkout idempotency helper — all SQLite-test-compatible for downstream handler tests.

## What Shipped

- **`plan_repo.go` (T01)** — 27 functions covering five concerns:
  1. **Read helpers** — FindPlanByID, FindPlanByCode, FindSystemPlanID, ListActivePlans, ListAllPlans.
  2. **Server-access enforcement (PAY-11, D-21)** — ListServersForPlan (active-only, ORDER BY current_load ASC) + IsServerAllowedForPlan (404 vs 403 per D-22).
  3. **Offers** — FindActiveOffer (strict is_active=true for /checkout) + FindOfferByLavaOfferID (NOT filtered on is_active — webhook grandfathering per ADR §19.10) + ListOffersForPlan + ListActiveOffersForPublic + ListPlanServerCountries + ListPlanServersJoined.
  4. **SetUserPlan (PAY-09)** — single-transaction update of `users.plan_id` + `users.subscription_tier` + `users.subscription_expires_at` + subscriptions-row upsert; mirrors subscription_repo.CreateOrUpdateSubscription's find-first-or-insert pattern; accepts nil `lavaContractID` and nil `expiresAt` for the expiry-cron downgrade path (03-09 / D-26).
  5. **Admin CRUD (03-08)** — CreatePlan (forces plan.IsSystem=false), UpdatePlan (strips immutable keys: code, is_system, id), SoftDeletePlan (refuses system plans with ErrSystemPlan, deactivates all child offers in one tx), CountActiveUsersOnPlan; ReplacePlanServers / AddPlanServer (idempotent per ADR §19.7.6) / RemovePlanServer; CreatePlanOffer / UpdatePlanOffer (strips periodicity, currency per ADR §19.7.7) / DeletePlanOffer (soft) / ReplaceOffer (ADR §19.10 versioning — deactivate old + insert new in one tx). Exports `ErrSystemPlan` sentinel and `NormalizePlanCode(code)` helper.

- **`plan_repo_test.go` (T02)** — 15 SQLite-backed unit tests covering the named test functions from 03-VALIDATION.md:
  - TestFindPlanByID_FoundAndNotFound, TestFindPlanByCode_FoundAndNotFound, TestFindSystemPlanID_HappyPath, TestListActivePlans_FiltersInactive
  - TestListServersForPlan_FiltersByPlanAndActive (PAY-11 named test)
  - TestIsServerAllowedForPlan_TrueFalse
  - TestFindActiveOffer_ReturnsActiveOnly
  - TestFindOfferByLavaOfferID_GrandfatheredInactive (PAY-08 grandfathering proof)
  - TestSetUserPlan_UpdatesUserAndUpsertsSubscription (PAY-09 — inserts on first call, updates in place on renewal)
  - TestSoftDeletePlan_RefusesSystemPlan (D-32 §4)
  - TestUpdatePlan_StripsImmutableFields (code + is_system + id stripped)
  - TestReplaceOffer_DeactivatesOldInsertsNewInOneTx (PAY-15)
  - TestReplacePlanServers_AtomicReplacement (PAY-14)
  - TestAddPlanServer_IdempotentOnReinsert (ADR §19.7.6)
  - TestRemovePlanServer_ReturnsErrNotFoundWhenMissing

- **`invoice_repo.go` (T03)** — 5 functions:
  - CreateInvoice (defaults Status="pending"), FindInvoiceByID, FindInvoiceByLavaID (webhook reverse-lookup PAY-08), FindActivePendingInvoice (ADR §9.2 — caller-supplied 60s window; returns ErrNotFound on outside-window or absent), UpdateInvoiceStatus (returns ErrNotFound on missing id).

- **`invoice_repo_test.go` (T03)** — 5 SQLite-backed tests:
  - TestCreateInvoice_DefaultsStatusToPending
  - TestFindInvoiceByID_FoundAndNotFound
  - TestFindInvoiceByLavaID_HappyPath
  - TestFindActivePendingInvoice_WithinAndOutsideWindow (proves the 60s cut-off — backdates a row by 5 minutes via raw UPDATE then asserts ErrNotFound)
  - TestUpdateInvoiceStatus_HappyAndMissing

## Verification

**Plan-level success criteria (all 6):**

| # | Criterion | Result |
|---|---|---|
| 1 | `cd server/api && go build ./...` exits 0 | **PASS** |
| 2 | `cd server/api && go test ./internal/repository/ -count=1 -timeout=60s` exits 0 | **PASS** (1.079s) |
| 3 | `TestListServersForPlan_FiltersByPlanAndActive` exists (PAY-11) | **PASS** |
| 4 | `TestFindOfferByLavaOfferID_GrandfatheredInactive` exists (PAY-08) | **PASS** |
| 5 | `TestSetUserPlan_UpdatesUserAndUpsertsSubscription` exists (PAY-09 expires_at + upsert) | **PASS** |
| 6 | `grep ErrSystemPlan server/api/internal/repository/plan_repo.go` finds var decl + use in SoftDeletePlan | **PASS** (4 hits: 2 in decl/comment, 2 in usage) |

**Verification command results:**

```
$ go build -C server/api ./...                                           → exit 0
$ go vet  -C server/api ./internal/repository/...                        → exit 0
$ go test -C server/api ./internal/repository/ -count=1 -timeout=60s     → ok 1.079s
$ go test -C server/api ./... -short -count=1 -timeout=120s              → all PASS (no regressions)
$ grep -c "^func " server/api/internal/repository/plan_repo.go           → 27 (>= 17)
$ grep -c "^func " server/api/internal/repository/invoice_repo.go        → 5  (== 5)
$ grep -c "db.Transaction" server/api/internal/repository/plan_repo.go   → 4 (SetUserPlan, SoftDeletePlan, ReplacePlanServers, ReplaceOffer)
$ grep "delete(updates, \"code\")"      server/api/internal/repository/plan_repo.go → 1 hit (UpdatePlan)
$ grep "delete(updates, \"is_system\")" server/api/internal/repository/plan_repo.go → 1 hit (UpdatePlan)
$ grep "delete(updates, \"periodicity\")" server/api/internal/repository/plan_repo.go → 1 hit (UpdatePlanOffer)
$ grep "FindOfferByLavaOfferID" server/api/internal/repository/plan_repo.go → 2 hits (function decl + cross-reference comment)
$ grep "60\*time.Second" server/api/internal/repository/invoice_repo_test.go → 2 hits (60s window inside + outside)
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Test infrastructure bug] subscriptions.id stored as NULL inside db.Transaction**

- **Found during:** T02 — `TestSetUserPlan_UpdatesUserAndUpsertsSubscription` failed with `WHERE conditions required` on the renewal call's `tx.Model(&existing).Updates(...)`.
- **Issue:** The Subscription GORM model has `ID string gorm:"primaryKey;type:uuid;default:gen_random_uuid()"`. The `default:gen_random_uuid()` tag tells GORM to OMIT the column from INSERT when the field is the zero value, expecting the DB to fill it. Postgres has `gen_random_uuid()`; SQLite does not. The plan's verbatim test DDL declared `id TEXT PRIMARY KEY` with no DEFAULT, so the row stored `id = NULL`. On the renewal call, `tx.Where(...).First(&existing)` returned an empty `existing.ID = ""`, and `tx.Model(&existing).Updates(...)` then tried to UPDATE without a WHERE clause (GORM's safety guard fires).
- **Fix:** Added `DEFAULT (lower(hex(randomblob(16))))` to the SQLite `subscriptions.id` column in `setupPlanRepoDB`. SQLite now auto-fills a hex-encoded random value, the row stores a non-empty id, and GORM extracts it correctly into `existing.ID`. The same pattern is used in the existing `subscription_repo_test.go::openTestDB` (line 33 + 54) and was clearly the intended SQLite affordance — the plan body simply forgot the DEFAULT. Pre-emptively applied the same fix to the `invoices.id` column in T03 to avoid the same trap when a future plan adds tx-wrapped invoice helpers.
- **Files modified:** server/api/internal/repository/plan_repo_test.go, server/api/internal/repository/invoice_repo_test.go
- **Commits:** 3c667c7 (T02), 03ad9ad (T03)

**2. [Rule 1 — Test infrastructure bug] GORM omits `IsActive: false` from INSERT, DDL default flips it to active**

- **Found during:** T02 — `TestListServersForPlan_FiltersByPlanAndActive` returned 3 rows when 2 were expected; `TestFindActiveOffer_ReturnsActiveOnly` returned the inactive offer (which was inserted as active because of GORM's zero-value omission).
- **Issue:** GORM's struct-based Create omits Go zero-value fields by default; `false` is the zero value for `bool`. Combined with `is_active INTEGER NOT NULL DEFAULT 1` in the SQLite DDL, this means `db.Create(&model.VPNServer{IsActive: false, ...})` actually inserts `is_active = 1`. The existing `subscription_repo_test.go::TestFindSubscriptionByUserID_SkipsInactiveSub` (lines 124-145) calls out this exact trap and uses an insert-then-UPDATE pattern.
- **Fix:** Reworked `TestListServersForPlan_FiltersByPlanAndActive` (s3), `TestFindActiveOffer_ReturnsActiveOnly` (inactive offer), and `TestFindOfferByLavaOfferID_GrandfatheredInactive` (off) to insert with `IsActive: true` then issue an explicit `db.Model(...).Where(...).Update("is_active", false)` after creation.
- **Files modified:** server/api/internal/repository/plan_repo_test.go
- **Commit:** 3c667c7

**3. [Rule 3 — Blocking issue] vpn_servers test DDL missing NOT-NULL columns required by the GORM model**

- **Found during:** Initial T02 build/test attempts would have crashed `db.Create(&model.VPNServer{...})` because the model declares `region`, `city`, `country`, and `capacity` columns; the plan's verbatim DDL omitted them. (Caught at planning-review time before the failing run.)
- **Issue:** GORM includes ALL struct fields in INSERTs (unless the value is the zero one AND there's a `default:` tag). With the plan-verbatim DDL, `db.Create(&model.VPNServer{...})` would emit `INSERT INTO vpn_servers (..., region, ...)` against a table that has no `region` column → SQL error "table vpn_servers has no column named region".
- **Fix:** Extended the `CREATE TABLE vpn_servers` statement in `setupPlanRepoDB` with `region TEXT NOT NULL DEFAULT ''`, `city TEXT NOT NULL DEFAULT ''`, `capacity INTEGER NOT NULL DEFAULT 500`, and `awg_params TEXT` (for completeness; the model declares it as a `jsonb` column).
- **Files modified:** server/api/internal/repository/plan_repo_test.go
- **Commit:** 3c667c7

**4. [Rule 3 — Defence in depth] SetMaxOpenConns(1) + SetMaxIdleConns(1) on every test sqlite handle**

- **Found during:** Initial T02 investigation — before pinpointing the actual root cause (deviation #1), I suspected SQLite's per-connection `:memory:` isolation might be the culprit when `db.Transaction(...)` grabs a fresh connection. Set both pool ceilings to 1 as a precaution.
- **Issue:** SQLite `:memory:` databases are per-connection by default. If GORM's connection pool keeps multiple connections open and `db.Transaction(...)` happens to use a different one than the prior `db.Create(...)`, the transaction's SELECT will see an empty database. Although deviation #1 turned out to be the real fix, the `SetMaxOpenConns(1)` insurance is consistent with the existing `handler/auth_test.go::TestAppleRaceConditionPartialIndex` pattern (line 885) and prevents any future test from tripping over the same SQLite quirk.
- **Files modified:** server/api/internal/repository/plan_repo_test.go (line 37-38), server/api/internal/repository/invoice_repo_test.go (line 25-26)
- **Commits:** 3c667c7, 03ad9ad

### Deferred Issues

None — all in-scope work landed clean. The 03-01 SUMMARY's "Deferred Issues" listed:
- Removing `internal/handler/legacy_plan_limits.go` once 03-04 wires real `plan_repo.FindPlanByID` calls — **now unblocked** (this plan provides FindPlanByID).
- Removing `repository.FindSubscriptionByStripeID` once 03-05 rewrites the webhook surface — still owned by 03-05; nothing this plan can do.

## Threat Model Compliance

All STRIDE mitigations in the plan's `<threat_model>` are now in code:

| Threat ID | Mitigation in code |
|-----------|--------------------|
| T-03-16 (EoP: UpdatePlan mass-assignment) | `UpdatePlan` deletes `code`, `is_system`, `id` from the updates map before applying (plan_repo.go line 264-266). Verified by `TestUpdatePlan_StripsImmutableFields`. |
| T-03-17 (EoP: CreatePlan force is_system=true) | `CreatePlan` zeroes `plan.IsSystem = false` regardless of the caller's input (plan_repo.go line 258). |
| T-03-18 (Tampering: SetUserPlan with client-controlled planID) | Repo function is safe with any valid plan UUID; threat is the caller's responsibility — webhook handler in 03-06 MUST derive planID from `FindOfferByLavaOfferID(...).PlanID`, never from the webhook body. Threat-model accept-vs-mitigate boundary documented. |
| T-03-19 (Tampering: SoftDeletePlan force-deletes system plan) | `SoftDeletePlan` checks `plan.IsSystem` BEFORE deactivation and returns `ErrSystemPlan` (plan_repo.go line 287). Verified by `TestSoftDeletePlan_RefusesSystemPlan`. Defence in depth: `idx_plans_one_system` partial unique landed in migration 019. |
| T-03-20 (Tampering: concurrent SetUserPlan races vs admin force-cancel) | **Accepted** per plan threat model; Phase 7 ADMIN-03 adds the per-user advisory lock. SetUserPlan IS transactional (atomicity guaranteed); the documented gap is cross-request serialisation. |
| T-03-21 (DoS: ListServersForPlan join cost) | **Accepted** — vpn_servers table is bounded (~50 rows production); join is O(servers × log(plan_servers)). Phase 6 PERF-01 caches /servers separately. |
| T-03-22 (Tampering: negative amount in plan_offers) | **Accepted** — migration 019 has `CHECK (amount >= 0)`; handler-layer validation in 03-08 adds the explicit error message. |

ASVS L2 controls applied: V5 (input validation via field stripping in UpdatePlan/UpdatePlanOffer), V11 (system-plan immutability + grandfathered-offer resolution), V13 (access control branch in handlers 03-04+).

## Threat Flags

None. No new HTTP endpoints, no new outbound calls, no new auth paths. Each new function operates on tables already trust-boundaried by migrations 019 + 020.

## Known Stubs

None. Every function returns real data or a sentinel error; no UI-bound placeholder strings; no hardcoded empty arrays that flow to a response.

## Commits

| Hash | Type | Message |
|------|------|---------|
| `360a062` | feat | plan_repo.go (read helpers + SetUserPlan tx + plan/offer/server CRUD) |
| `3c667c7` | test | plan_repo_test.go (15 sqlite-backed tests, PAY-08/09/11 named) |
| `03ad9ad` | feat | invoice_repo.go + tests (CreateInvoice, FindInvoiceByLavaID, 60s idempotency) |

## Self-Check: PASSED

Files exist:
- FOUND: server/api/internal/repository/plan_repo.go
- FOUND: server/api/internal/repository/plan_repo_test.go
- FOUND: server/api/internal/repository/invoice_repo.go
- FOUND: server/api/internal/repository/invoice_repo_test.go
- FOUND: .planning/phases/03-lava-top-plans-catalog/03-03-plan-repo-SUMMARY.md (this file)

Commits exist (verified via `git log --oneline -5`):
- FOUND: 360a062 (T01 plan_repo.go)
- FOUND: 3c667c7 (T02 plan_repo_test.go)
- FOUND: 03ad9ad (T03 invoice_repo.go + tests)

Verification:
- `go build -C server/api ./...` → exit 0 — PASS
- `go vet -C server/api ./internal/repository/...` → exit 0 — PASS
- `go test -C server/api ./internal/repository/ -count=1` → ok 1.079s — PASS
- `go test -C server/api ./... -short` → all packages PASS — PASS (no regressions)
- `grep -c "^func " plan_repo.go` → 27 (>= 17 floor) — PASS
- `grep -c "^func " invoice_repo.go` → 5 (== required) — PASS
- `grep -c "^func Test" plan_repo_test.go` → 15 (>= 13 floor) — PASS
- All 6 plan-level success criteria — PASS
