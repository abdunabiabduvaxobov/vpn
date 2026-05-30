---
phase: 06-performance-scalability
reviewed: 2026-05-30T04:14:52Z
depth: standard
files_reviewed: 18
files_reviewed_list:
  - server/api/internal/cache/servers_cache.go
  - server/api/internal/cache/user_cache.go
  - server/api/internal/cache/heartbeat_cache.go
  - server/api/internal/middleware/auth.go
  - server/api/internal/handler/connection.go
  - server/api/internal/handler/servers.go
  - server/api/internal/handler/admin.go
  - server/api/internal/handler/webhook_lava.go
  - server/api/internal/scheduler/scheduler.go
  - server/api/internal/repository/connection_repo.go
  - server/api/internal/repository/expiry_repo.go
  - server/api/internal/repository/user_repo.go
  - server/api/internal/config/config.go
  - server/api/internal/repository/db.go
  - server/api/internal/bot/recovery.go
  - server/api/cmd/main.go
  - server/api/migrations/022_add_perf_indexes.sql
  - server/api/migrations/023_connections_connected_at_index.sql
findings:
  critical: 0
  warning: 4
  info: 5
  total: 9
status: issues_found
---

# Phase 6: Code Review Report

**Reviewed:** 2026-05-30T04:14:52Z
**Depth:** standard
**Files Reviewed:** 18
**Status:** issues_found

## Summary

Reviewed the PERF phase: the `user:<id>` entitlement cache, the `cache:servers:active` blob cache, the Redis heartbeat write-coalescing buffer + 10s flush goroutine, the scheduler gate / per-job registry, the bulk-downgrade busts, and migrations 022/023.

Overall the design is sound and the security-sensitive invariants hold for the **money path**: a downgrade/expiry/delete of a **paying** user always busts `user:<id>` synchronously. The webhook Pro-grant, admin update, both bulk-downgrade crons (`DowngradeExpiredSubscriptions`, `DowngradeExpiredPlans`), and the Telegram restore all bust correctly, and every cache read fails open to the DB. **No Critical findings** — there is no path that serves stale Pro after a downgrade/expiry/delete.

However, the `user:<id>` cache contract is documented as "EVERY mutation path synchronously busts `user:<id>`" and three real mutation paths do **not** call `BustUserCache`, contradicting the contract. None of them can leak Pro today (they only touch free-tier guests), but the gap is load-bearing on a free-tier-only invariant that is easy to break in a future edit, so it is the top Warning. Separately, the heartbeat flush has a small lost-beat race (SREM of a key that was re-beaten after the SMEMBERS snapshot), bounded by the 3-min stale grace.

Migrations 022/023 are correct: `CONCURRENTLY` with no transaction block, `IF NOT EXISTS` idempotent, and the partial-index predicate `WHERE disconnected_at IS NULL` exactly matches the COALESCE-dropped `CleanupStaleConnections` predicate.

## Warnings

### WR-01: `user:<id>` cache not busted on guest delete / SSO promote — contradicts the "EVERY mutation path" contract

**File:** `server/api/internal/cache/user_cache.go:75-79`, `server/api/internal/handler/devices.go:408`, `server/api/internal/handler/auth.go:766`, `server/api/internal/handler/auth.go:853`

**Issue:** `BustUserCache`'s doc comment asserts it is "Called by EVERY mutation path that changes a user's existence/tier/expiry (admin update, webhook Pro-grant, user delete / DeleteOrphanGuestUser / PerformRestore, and both bulk expiry downgrades)". A grep of all `BustUserCache` call sites shows three user-existence mutations that do **not** bust:

1. `devices.go:408` — `DeleteOrphanGuestUser` in `LinkDevice` (deletes the previous device owner).
2. `auth.go:766` — `DeleteOrphanGuestUser` inside the SSO reassign-and-orphan transaction.
3. `auth.go:853` — `PromoteGuestToSSO` (Step C: a guest row's existence/identity changes in place).

Today this does **not** leak Pro: `DeleteOrphanGuestUser` refuses any row whose `subscription_tier != 'free'` or that has an active subscription (`user_repo.go:101-122`), and `PromoteGuestToSSO` promotes a free guest without escalating tier. So the cached value for these ids is always `"free"`, and the worst case is a deleted guest passing the existence gate for up to the 5s TTL — i.e. the exact "zombie" 401-vs-404 problem `AuthRequired` was built to avoid (`middleware/auth.go:90-114`), bounded to 5s and free-tier only.

The risk is that the contract is *documented as universal* but is *actually conditional on a free-tier-only invariant* that lives in a different file. A future change that lets `DeleteOrphanGuestUser` remove a paid row, or that has `PromoteGuestToSSO` carry a tier, would silently start serving stale entitlement with no compiler or test signal.

**Fix:** Either bust on these paths or tighten the doc to match reality. Preferred — add the bust so the contract is literally true:
```go
// devices.go, after the DeleteOrphanGuestUser call (~line 413)
if orphanedUserID != "" && orphanedUserID != owner.ID {
    if err := repository.DeleteOrphanGuestUser(c.Context(), db, orphanedUserID); err != nil && !errors.Is(err, repository.ErrNotFound) {
        logger.Warn("link: orphan cleanup failed", zap.String("orphan_user_id", orphanedUserID), zap.Error(err))
    } else {
        // Existence changed — bust so the deleted guest can't pass the 5s existence gate.
        _ = cache.BustUserCache(c.Context(), redisClient, orphanedUserID) // best-effort; 5s TTL backstop
    }
}
```
Add the equivalent bust for `p.guestUserID` after the reassign-and-orphan tx commits (`auth.go:778`) and after `PromoteGuestToSSO` succeeds (`auth.go:854`). If instead you keep the paths bust-free, change the `BustUserCache` doc comment to state explicitly that guest-delete / SSO-promote rely on the free-tier invariant + 5s TTL rather than an explicit bust, so the next editor knows the invariant is load-bearing. (Note: `devices.go` / `auth.go` are out of the declared file scope but are the call sites that break the in-scope contract; flagging here per the cross-reference.)

### WR-02: Heartbeat flush can drop the most recent beat (SREM removes a key re-beaten after the SMEMBERS snapshot)

**File:** `server/api/internal/cache/heartbeat_cache.go:84-116`

**Issue:** `FlushHeartbeats` reads the dirty set with `SMEMBERS` (line 89), bulk-UPDATEs those ids to `time.Now()` (lines 99-103), then `SREM`s the exact slice it read (line 111). If a connection beats again **between** the SMEMBERS snapshot and the SREM, `TouchHeartbeat` re-adds the id with `SADD` (line 53), but the subsequent `SREM hb:dirty ids` deletes that id anyway. The new beat's freshness is now only in the `hb:<id>` string key — the dirty marker is gone, so the next flush will not pick the id up. The Postgres `last_heartbeat_at` therefore lags the true latest beat by up to one flush window per affected connection. Under sustained per-10s beats this can persist indefinitely for a hot connection (every flush window removes the marker that the in-window beat just set).

This is bounded and not Critical — the 3-min `StaleConnectionAfter` grace (`scheduler.go:188`, `config.go:117`) absorbs ~18 missed 10s windows, and the value written is monotonically close (it is "10s stale" not "stale forever" as long as beats keep arriving and any single flush snapshots it). But the doc comment at lines 76-80 claims the only loss is "one 10s window of heartbeat freshness" on a *crash*; the steady-state SREM-after-rebeat race is an additional, undocumented source of staleness that the comment's "idempotent re-flush" reasoning does not actually cover (the re-flush can't happen because the marker was removed).

**Fix:** Only remove the ids you actually consumed, and do it atomically against concurrent SADDs. Two standard options:
- **Snapshot-and-pop atomically**: replace `SMEMBERS` + later `SREM` with a Lua script (or `SPOP count`) that reads-and-removes the current members in one round-trip, so a beat arriving after the pop re-adds a marker that the next flush will see. Then UPDATE the popped set.
- Or, if you keep SMEMBERS, SREM each id only after confirming its `hb:<id>` timestamp has not advanced past the snapshot — heavier, prefer the pop approach.

```go
// sketch: atomic drain
ids, err := client.SPopN(ctx, heartbeatDirtySet, 10000).Result() // pops up to N; loop if needed
// ...UPDATE for ids...
// on UPDATE error: re-SADD ids so they aren't lost (at-least-once), since SPOP already removed them.
```
Note the error path inverts with SPOP: because the pop removes eagerly, an UPDATE failure must `SADD` the ids back to preserve the at-least-once guarantee the current SMEMBERS-then-SREM ordering gives for free.

### WR-03: `bustExpiredUsers` busts after the UPDATE commits but `DowngradeExpiredPlans` selects-then-updates non-atomically — a concurrent renewal in the gap can be clobbered to free

**File:** `server/api/internal/repository/expiry_repo.go:69-90`, `server/api/internal/repository/user_repo.go:298-313`

**Issue:** Both bulk downgrades do a `Pluck` of eligible ids followed by a separate `UPDATE ... WHERE id IN (plucked ids)`. The WHERE on the UPDATE only re-checks `id IN (...)` — it does **not** re-assert the eligibility predicate (`expires_at < now()` / `subscription_tier != 'free'`). If a `payment.success` / `recurring.success` webhook commits a renewal for one of the plucked users in the window between the Pluck and the UPDATE, the UPDATE will still flip that just-renewed user to `subscription_tier = 'free'` / system plan, because the id is in the list and the predicate is not re-evaluated.

The webhook then busts `user:<id>` (so the cache reflects the renewed Pro briefly), but the cron's UPDATE lands *after* and writes free to the authoritative `users` row, and `bustExpiredUsers` busts again — so the next read repopulates from the now-free DB row. Net effect: a user who paid in the ~ms-to-seconds gap gets wrongly downgraded until the *next* webhook or admin action. On a money path this is a real (if low-probability) correctness bug, not just staleness.

**Fix:** Re-assert the eligibility predicate on the UPDATE so a row that became ineligible between the two statements is skipped:
```go
// expiry_repo.go DowngradeExpiredPlans — make the UPDATE self-guarding:
result := db.Model(&model.User{}).
    Where("id IN ? AND plan_id != ?", userIDs, systemPlanID).
    Where("EXISTS (SELECT 1 FROM subscriptions s WHERE s.user_id = users.id AND s.expires_at IS NOT NULL AND s.expires_at < ?)", time.Now()).
    Updates(map[string]interface{}{"plan_id": systemPlanID, "subscription_tier": "free"})
```
Apply the analogous `AND subscription_tier <> 'free' AND subscription_expires_at < NOW()` guard to `DowngradeExpiredSubscriptions`'s UPDATE (`user_repo.go:307-309`). The returned id list should then be reconciled with `result.RowsAffected` if you want the bust list to match exactly what was flipped (a user skipped by the re-assert should ideally not be busted, though an extra bust is harmless).

### WR-04: `ListServersCached` admin path serves mobile-hidden server fields from the shared cache blob

**File:** `server/api/internal/handler/servers.go:142-145`, `server/api/internal/handler/servers.go:197-222`

**Issue:** The cached `cache:servers:active` blob is `json.Marshal([]model.VPNServer)` from `ListActiveServers` (servers.go:209, 215). On the admin branch the handler returns `fullServers` as-is (servers.go:143-145). `AdminListServers` deliberately uses a separate `adminServerResponse` DTO (admin.go:249-283) to expose capacity/load/REALITY-key fields that are `json:"-"` on `model.VPNServer` and must never reach the mobile client — but `GET /servers` (this handler, the mobile-facing one) for an admin user marshals the raw `model.VPNServer`, so whatever the model exposes by default is what the admin gets here. This is correct *only* as long as `model.VPNServer`'s default JSON tags hide the sensitive fields. The two code paths now have two different serialization contracts for the same data, and the safety of the `/servers` admin branch silently depends on the model's struct tags rather than an explicit DTO. A future field added to `model.VPNServer` without `json:"-"` would leak through `/servers` for admins without leaking through `AdminListServers`, defeating the DTO's purpose.

This is not an active leak (the model currently tags sensitive fields `json:"-"`), so it is a Warning, not Critical. But it is a maintainability/security-coupling hazard worth pinning.

**Fix:** Either document at the `model.VPNServer` definition that its default JSON shape is the mobile-safe contract and `/servers` (incl. the admin branch) depends on it, or have the admin branch of `ListServersCached` project through the same mobile-safe shape used for non-admins rather than returning the raw model. If admins genuinely need the richer fields on `/servers`, they should use `/admin/servers`; keep `/servers` mobile-shaped for all roles.

## Info

### IN-01: `GetServerConfig` comment claims Go 1.22 but stack is now Go 1.25

**File:** `server/api/internal/handler/servers.go:301`

**Issue:** The comment `// Note: math/rand is auto-seeded in Go >= 1.20 (we use 1.22).` is stale — CLAUDE.md records the stack was bumped to Go 1.25 on 2026-05-23. The auto-seed statement is still true (>= 1.20), only the parenthetical version is wrong.

**Fix:** Update to `(we use 1.25)` or drop the version parenthetical. Also consider `math/rand/v2` for new code, though `math/rand.Intn` here is non-security (SNI selection) so it is fine.

### IN-02: Stripe config fields and `OptionalEnvWarnings` are dead weight now that lava is the sole provider

**File:** `server/api/internal/config/config.go:17-19`, `server/api/internal/config/config.go:110-113`, `server/api/internal/config/config.go:296-314`

**Issue:** `StripeKey`, `StripeWebhookSecret`, `StripePricePremium`, `StripePriceUltimate` and the Stripe branch of `OptionalEnvWarnings` remain, but CLAUDE.md states "Payment provider: lava.top exclusively" and main.go no longer mounts a Stripe webhook route (cmd/main.go:277 "old Stripe webhook route has been removed"). These fields are now unused configuration surface that emits a misleading WARN ("stripe checkout will fail at runtime") on every boot.

**Fix:** Remove the Stripe fields and the Stripe entries from `OptionalEnvWarnings` when Phase 8 lands (the code comments already flag "Stripe leaves in Phase 8"). Tracking here so it is not forgotten; no action required this phase.

### IN-03: `_ = tier` retained only as a log-context variable in `RegisterConnection`

**File:** `server/api/internal/handler/connection.go:148`

**Issue:** `tier` is computed (including the real-time expiry-as-free downgrade at lines 70-73) but then explicitly discarded with `_ = tier` at line 148, used only in two `logger.Warn`/`Info` calls later. The expiry-downgrade logic that produces `tier` no longer affects any control flow (device limits now come from the plan row via `plan_id`, not tier), so the in-handler expiry check is effectively dead for enforcement and survives only for log text.

**Fix:** Confirm this is intentional. If the per-request expiry-as-free behaviour is meant to gate anything (e.g. it once chose limits), that intent is now lost — limits come from `plan.MaxDevices` regardless of the computed `tier`. If it is purely for logging, a short comment saying so would prevent a future reader from "fixing" the apparent unused variable and accidentally removing the log context.

### IN-04: `FlushHeartbeats` ignores the SREM error but the early-return both return `len(ids), nil` identically

**File:** `server/api/internal/cache/heartbeat_cache.go:111-115`

**Issue:** The `if rerr := ...SRem(...).Err(); rerr != nil { return len(ids), nil }` block and the fall-through `return len(ids), nil` return the identical value, so the `if` does nothing observable except swallow `rerr` without logging it. The intent (per the comment) is "SREM failure is non-fatal," which is fine, but the branch is structurally a no-op and the SREM error is silently dropped — the scheduler can never log a persistent SREM failure that would cause repeated re-flushes.

**Fix:** Collapse to a single `_ = client.SRem(ctx, heartbeatDirtySet, ids).Err()` (or actually surface `rerr` to the caller so `scheduler.go:118-123` can log a chronic SREM failure). Functionally harmless today; flagged as dead structure.

### IN-05: `isDuplicateError` matches on substring of the error message rather than a typed driver error

**File:** `server/api/internal/repository/db.go:74-80`

**Issue:** `isDuplicateError` does `strings.Contains(msg, "23505")` / `"UNIQUE constraint failed"`. String-matching driver error text is brittle — a wrapped/translated error, a localized SQLite build, or a future driver could change the text and silently turn `ErrDuplicate` into a generic 500. The webhook idempotency and SSO race-recovery paths depend on `ErrDuplicate` being correctly detected.

**Fix:** Prefer typed detection where available: for pgx/lib-pq, `errors.As` onto `*pgconn.PgError` and compare `.Code == "23505"`. Keep the SQLite string fallback for the unit-test driver. Not urgent (the `23505` SQLSTATE substring is stable for pgx today), so Info.

---

_Reviewed: 2026-05-30T04:14:52Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
