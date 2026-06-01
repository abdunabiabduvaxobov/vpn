---
phase: 07-admin-panel-overhaul
reviewed: 2026-06-01T00:00:00Z
depth: standard
files_reviewed: 47
files_reviewed_list:
  - server/api/cmd/main.go
  - server/api/migrations/024_admin_panel_overhaul.sql
  - server/api/internal/handler/admin.go
  - server/api/internal/handler/admin_server_controls.go
  - server/api/internal/handler/admin_system.go
  - server/api/internal/handler/admin_user_controls.go
  - server/api/internal/handler/admin_webhooks.go
  - server/api/internal/handler/broadcasts.go
  - server/api/internal/handler/health.go
  - server/api/internal/handler/webhook_lava.go
  - server/api/internal/middleware/audit.go
  - server/api/internal/middleware/internal_secret.go
  - server/api/internal/middleware/maintenance.go
  - server/api/internal/middleware/suspended.go
  - server/api/internal/model/broadcast.go
  - server/api/internal/model/feature_flag.go
  - server/api/internal/model/lava_webhook_event.go
  - server/api/internal/model/server.go
  - server/api/internal/model/user.go
  - server/api/internal/repository/admin_repo.go
  - server/api/internal/repository/audit_repo.go
  - server/api/internal/repository/broadcast_repo.go
  - server/api/internal/repository/connection_repo.go
  - server/api/internal/repository/feature_flag_repo.go
  - server/api/internal/repository/lock.go
  - server/api/internal/repository/server_repo.go
  - server/api/internal/repository/session_repo.go
  - server/api/internal/repository/webhook_event_repo.go
  - server/api/internal/cache/flags_cache.go
  - server/api/internal/cache/health_cache.go
  - server/api/internal/config/config.go
  - server/api/internal/testutil/pg.go
  - server/tunnel/cmd/tunnel/main.go
  - server/tunnel/internal/config.go
  - server/tunnel/internal/heartbeat.go
  - admin-web/src/api/servers.ts
  - admin-web/src/api/system.ts
  - admin-web/src/api/users.ts
  - admin-web/src/api/webhooks.ts
  - admin-web/src/pages/System.tsx
  - admin-web/src/pages/UserDetail.tsx
  - admin-web/src/pages/Payments.tsx
  - server/api/integration/admin_concurrency_test.go
  - server/api/integration/webhook_replay_test.go
  - server/api/internal/handler/admin_user_controls_test.go
  - server/api/internal/middleware/maintenance_test.go
  - server/api/internal/middleware/suspended_test.go
findings:
  critical: 0
  warning: 4
  info: 6
  total: 10
status: issues_found
---

# Phase 7: Code Review Report

**Reviewed:** 2026-06-01
**Depth:** standard
**Files Reviewed:** 47
**Status:** issues_found

## Summary

Phase 7 (admin-panel-overhaul) is well-engineered and the security-critical paths
are sound. The ADMIN-03 advisory lock (`repository.WithUserLock`) is correctly
implemented: it is transaction-scoped (`pg_advisory_xact_lock`, auto-released on
COMMIT/ROLLBACK/panic), keyed on the resolved `user_id` via a bound parameter
(injection-safe), and both contending paths (webhook tier-grant and admin
force-cancel) derive the same key from the same resolved user, with all writes
executed on the lock-holding `tx`. The webhook-replay idempotency is genuinely
set-not-increment (`SetUserPlanTx` SETs `expires_at = now()+periodicity`), the
idempotency `INSERT ... ON CONFLICT DO NOTHING` correctly commits outside the
lock transaction, and both live and replay paths funnel through `applyLavaEvent`
so the lock is inherited. Auth surfaces are correct: constant-time secret compares
(`internal_secret.go`, lava `X-Api-Key`), per-request DB read in `SuspendedRequired`,
fail-closed on empty internal secret, fail-open on flag-read errors in maintenance,
status-word-only `/readyz` vs. admin-gated `deps-health`, and reason persisted into
`audit_log.details` as bound parameters. All repository queries use parameterized
GORM clauses — no string-concatenated SQL was found, including the search `ILIKE`
and the MRR aggregate.

No Critical issues. Findings are correctness/robustness refinements and minor
quality items.

## Warnings

### WR-01: Maintenance and flag-guard middleware match on `c.Path()` without normalizing trailing slashes / case

**File:** `server/api/internal/middleware/maintenance.go:35,61-75`
**Issue:** `isMaintenanceExempt` and the route checks compare against `c.Path()`
with exact/prefix string matches (`/api/v1/admin/`, `== /api/v1/auth/admin-login`,
`== /api/v1/livez`). Fiber by default does not strip a trailing slash, and the
exempt-prefix uses a trailing-slash form `"/api/v1/admin/"`. The escape-hatch is
the *fail-safe* direction (admin can always reach `/admin/*`), so a path that
fails to match exempts *nothing* and would (correctly) be 503'd. The concern is
the inverse for the exact-match probes: a probe configured as `GET /api/v1/livez/`
(trailing slash) or a load balancer hitting `/api/v1/readyz/` would NOT match the
exact comparison and would be 503'd during maintenance, defeating the probe. This
is a robustness gap, not a security hole.
**Fix:** Normalize before matching, or rely on Fiber's `StrictRouting:false`
default consistently. Example:
```go
path := strings.TrimRight(c.Path(), "/")
switch {
case strings.HasPrefix(path, "/api/v1/admin"):
    return true
case method == fiber.MethodGet && path == "/api/v1/livez":
    return true
// ...
}
```
Confirm the deployed probes' exact paths in the LB config and add a test for the
trailing-slash variant.

### WR-02: `handleLavaSubscriptionCancelled` does not take `WithUserLock`

**File:** `server/api/internal/handler/webhook_lava.go:482-496`
**Issue:** `payment.success` and `recurring.success` both serialize against the
admin force-cancel via `WithUserLock(... parent.UserID / inv.UserID ...)`. But
`subscription.cancelled` writes `lava_contracts.is_active=false, cancelled_at=now`
directly with `db.WithContext(ctx).Model(...).Where("contract_id = ?", ...)` and
no lock. A lava `subscription.cancelled` racing an admin force-cancel (or a
`recurring.success`) on the same user is not serialized. The blast radius is
small because `subscription.cancelled` only touches the contract row (not the
user tier) and converges to the same `is_active=false`, but it can still interleave
with a `recurring.success` that just set `is_active=true` and leave the contract
`is_active=false` while the user tier was just refreshed to a future expiry — a
mild form of the exact hybrid state ADMIN-03 set out to prevent. It also does not
resolve `user_id` to take the same key.
**Fix:** Resolve the contract's `user_id` (it is already a column) and wrap the
update in `repository.WithUserLock(ctx, db, contract.UserID, func(tx ...) error {...})`,
mirroring the other two success handlers, so all five contract-mutating webhook
paths share the per-user lock.

### WR-03: `handleLavaRecurringFailed` flips `is_active` outside `WithUserLock`

**File:** `server/api/internal/handler/webhook_lava.go:457-469`
**Issue:** Same class as WR-02. The recurring-failed handler runs its own
`db.WithContext(ctx).Transaction(...)` flipping `subscriptions.is_active` and
`lava_contracts.is_active` to false, but it resolves `parent.UserID` and does
NOT take `WithUserLock(parent.UserID, ...)`. A `recurring.payment.failed`
arriving concurrently with an admin force-cancel or a `recurring.success` for the
same user is unserialized. The downgrade itself is deferred to the cron (tier is
untouched here), so the practical hybrid risk is lower than WR-02, but for
consistency and to honor the ADMIN-03 invariant ("the two writers serialize on
the same key") every contract/subscription writer keyed on a resolvable user
should take the lock.
**Fix:** Wrap the existing transaction body in `repository.WithUserLock(ctx, db,
parent.UserID, func(tx *gorm.DB) error { ... })` and run both updates on `tx`.

### WR-04: `subscriptions.is_active` flip uses a coarse `WHERE user_id = ? AND lava_contract_id IS NOT NULL`

**File:** `server/api/internal/handler/webhook_lava.go:458-460`
**Issue:** On `recurring.payment.failed` the handler sets `is_active=false` for
*every* subscription row of the user that has a non-null `lava_contract_id`, not
the one matching `parentID`. If a user ever holds more than one lava-backed
subscription row (e.g. an upgrade/downgrade left two rows, or a future
multi-product scenario), this deactivates unrelated active subscriptions. Today
the schema appears to be one-subscription-per-user so the impact is latent, but
the predicate does not match the precise contract the failed event refers to.
**Fix:** Scope by the contract: `Where("user_id = ? AND lava_contract_id = ?",
parent.UserID, parentID)` (or whatever column links the subscription to the
parent contract), so only the affected subscription is flipped.

## Info

### IN-01: `AdminSetFeatureFlag` accepts an arbitrary flag key (no allowlist)

**File:** `server/api/internal/handler/admin_system.go:53-99`, `repository/feature_flag_repo.go:41-57`
**Issue:** `PUT /admin/feature-flags/:key` upserts any `:key` the admin supplies
(the comment notes this is intentional so an unseeded flag can be set). Because
the value is a strict bool and the route is admin-authed + audited, this is not a
security issue, but a typo (`maintenance_modee`) silently creates a dead flag row
that the operator may believe is controlling behavior. The cache key
`cache:flag:<key>` likewise uses the raw key.
**Fix (optional):** Validate `:key` against the known set
`{signups_off, payments_off, maintenance_mode}` and 400 on anything else, or at
least surface a UI warning when setting a key not in the seeded list.

### IN-02: `AdminGetWebhookEvent` falls back silently if payload is not valid JSON

**File:** `server/api/internal/handler/admin_webhooks.go:126-142`
**Issue:** The comment says it "falls back to the raw bytes string if it is not
valid JSON," but the code unconditionally wraps `ev.Payload` in
`json.RawMessage` and returns it as `payload`. If the stored bytes are not valid
JSON, Fiber's JSON encoder will error on the whole response (or emit invalid
JSON), not "fall back to a raw string." The payload is always stored from a parsed
body so this is effectively unreachable, but the code does not match the
documented fallback.
**Fix:** Either drop the misleading comment or actually guard:
```go
var payload interface{}
if json.Valid(ev.Payload) {
    payload = json.RawMessage(ev.Payload)
} else {
    payload = string(ev.Payload)
}
```

### IN-03: `MarkWebhookReplayed` flips status to `REPLAYED` even when re-applying a FAILED event that may still be failing

**File:** `server/api/internal/handler/admin_webhooks.go:205-216`, `repository/webhook_event_repo.go:110-117`
**Issue:** The replay handler only calls `MarkWebhookReplayed` (status→REPLAYED,
count++) after `applyLavaEvent` returns nil, which is correct. But a FAILED event
that is replayed and *succeeds* now reads `REPLAYED`, losing the original FAILED
provenance in the status column (it survives only in `error`/`processed_at` and the
audit row). This is a minor forensic-clarity nit, not a bug.
**Fix (optional):** Consider a distinct terminal status or keep the prior status
in the audit details so the timeline is unambiguous.

### IN-04: `parseReason` truncates by byte length, can split a multibyte rune

**File:** `server/api/internal/handler/admin_user_controls.go:49-51`; mirrored in `admin_webhooks.go:180-182` and `admin_system.go:64-66`
**Issue:** `reason = reason[:maxSuspendReasonLen]` slices on bytes. A reason whose
500th byte lands in the middle of a multibyte UTF-8 character (Cyrillic/emoji —
likely given the RU admin UI) produces an invalid trailing byte sequence stored in
`audit_log.details`. It is bounded and not an injection vector (parameterized JSONB),
but the stored text can end in a broken glyph.
**Fix:** Truncate on rune boundaries, e.g.:
```go
if len([]rune(reason)) > maxSuspendReasonLen {
    reason = string([]rune(reason)[:maxSuspendReasonLen])
}
```
(or cap on `utf8.RuneCountInString` and slice via a rune-safe helper).

### IN-05: `redactEmails` regex will not mask emails embedded in already-escaped JSON edge cases

**File:** `server/api/internal/handler/admin_webhooks.go:22-42`
**Issue:** `redactEmails` runs over the raw JSONB string of the payload preview.
It correctly masks `local@domain` forms, but the list preview returns the entire
raw JSON string (not a field projection), so any PII other than email (e.g. a
`buyer.name`, phone, or a `firstName`/`lastName` lava may include) is NOT redacted
and leaks into the list view. The detail view is audited and intentionally full,
but the list view's contract (T-07-34) is "buyer PII masked," and only emails are
masked.
**Fix:** Confirm lava payloads carry no other PII in the preview, or project the
preview to a known-safe subset of fields rather than redacting a denylist of
patterns over the full blob.

### IN-06: `Health()` (`GET /health`) exposes `go_version` and uptime unauthenticated

**File:** `server/api/internal/handler/health.go:23-32`
**Issue:** The legacy `/health` endpoint returns `go_version` (e.g. `go1.25`) and
process uptime with no auth. This is pre-existing (not introduced this phase) and
low-severity, but it leaks the exact Go toolchain version to anonymous callers,
which aids targeted exploitation of any future Go-stdlib CVE. The new probes
(`/livez`, `/readyz`) correctly avoid this.
**Fix (optional):** Drop `go_version` from the public `/health` body, or fold
`/health` into the status-word-only `/readyz`/`/livez` family and remove the
version disclosure.

---

## Resolution (post-review fixes — commit `1453b11`)

| Finding | Disposition | Notes |
|---------|-------------|-------|
| WR-01 | **Fixed** | `isMaintenanceExempt` now trims a trailing slash before matching; probes `/livez/`, `/readyz/` and admin paths with trailing slash bypass maintenance. New regression subtest `on_exempts_probes_with_trailing_slash`. |
| WR-02 | **Fixed** | `handleLavaSubscriptionCancelled` now resolves the contract's `user_id` and wraps the update in `repository.WithUserLock` — all contract-mutating webhook paths now serialize on the same per-user key (ADMIN-03 invariant). |
| WR-03 | **Fixed** | `handleLavaRecurringFailed`'s two-row flip is now wrapped in `repository.WithUserLock(parent.UserID, …)` instead of a bare transaction. |
| WR-04 | **Deferred (accepted risk)** | Narrowing the `subscriptions` predicate to `lava_contract_id = parentID` was NOT applied: the existing code comment documents that `lava_contract_id` "may or may not equal parentID", so the narrower predicate risks *missing* the row and regressing today's correct one-sub-per-user behavior. Latent only under a future multi-lava-subscription schema; revisit if/when that lands. |
| IN-01..IN-06 | **Deferred** | Info-level; left for operator discretion (feature-flag allowlist, comment clarity, rune-vs-byte truncation, list-preview PII scope, pre-existing `/health` `go_version` disclosure). None are launch-blocking per project CLAUDE.md. |

_Reviewed: 2026-06-01_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
