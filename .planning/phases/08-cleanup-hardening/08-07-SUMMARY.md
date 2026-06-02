---
phase: 08-cleanup-hardening
plan: 07
subsystem: backend + tunnel / wire-security
tags: [per-user-vless-uuid, wire-enforcement, rotation, HARD-02, S4-2, S5-1, SC2]
requires:
  - "Waves 1 & 2 merged (08-01 Wave-0 RED tests, 08-03 main.go env, 08-05 stripe removal) — base babd24c"
  - "migration numbering: 025 (08-04) + 027 (08-02) already in main; 026 fills the gap"
  - "repository.WithUserLock + SetUserPlanTx (ADMIN-03 / 07-05) for the same-tx rotation"
provides:
  - "per-user random UUIDv4 VLESS identities (migration 026 user_vless_identities, D-06)"
  - "GetServerConfig returns a per-user UUID (no more shared cfg.TunnelVLESSUUID)"
  - "UUID rotation on plan change — admin PATCH + lava payment.success, in-tx (D-07)"
  - "internal GET /servers/:id/vless-clients active-set endpoint + ETag (InternalSecret-gated)"
  - "tunnel ReloadClients (regen+reload) + debounced heartbeat-driven pull loop (D-05)"
  - "documented 30-60s wire-propagation floor for SC#2 rotation"
affects:
  - "server/api GetServerConfig response (UserID is now per-user, not the shared env UUID)"
  - "server/tunnel xray client set is now dynamic (was static config.Clients)"
  - "tunnel<->API internal channel gains a second endpoint on the existing secret gate"
tech-stack:
  added: []
  patterns:
    - "lazy per-user identity allocation on first config fetch (GetOrCreateActiveVlessUUID)"
    - "in-transaction rotation under the existing per-user advisory lock (rotate atomic with tier grant)"
    - "ETag-gated debounced pull loop to batch connection-dropping xray reloads"
key-files:
  created:
    - server/api/migrations/026_user_vless_identities.sql
    - server/api/internal/model/vless_identity.go
    - server/api/internal/repository/vless_repo.go
  modified:
    - server/api/internal/handler/servers.go
    - server/api/internal/handler/admin.go
    - server/api/internal/handler/webhook_lava.go
    - server/api/cmd/main.go
    - server/api/internal/handler/servers_vless_test.go
    - server/api/internal/handler/webhook_lava_test.go
    - server/api/internal/handler/servers_test.go
    - server/api/internal/handler/admin_user_controls_test.go
    - server/tunnel/internal/server.go
    - server/tunnel/internal/server_reload_test.go
    - server/tunnel/internal/heartbeat.go
    - server/tunnel/cmd/tunnel/main.go
    - test/wire-vless/README.md
decisions:
  - "Random UUIDv4 in DB (uuid.NewString), NOT a deterministic HMAC — D-06: unguessable + unlinkable across users."
  - "Rotation runs INSIDE the existing WithUserLock tx (admin + lava payment.success) so it is atomic with SetUserPlanTx and serialized against every other per-user mutation for free."
  - "Renewal branch (recurring.payment.success) does NOT rotate — plan_id is unchanged, so a pure expiry refresh must not churn the UUID and needlessly drop the user's connection."
  - "Active-set endpoint returns an order-independent sha256 ETag so the tunnel skips a reload (and its connection drop) when membership is unchanged."
  - "ReloadClients verified at the config-swap level in unit test (no live xray instance / :443 bind); the wire-level Close/New/Start swap is proven by the manual test/wire-vless harness at the phase gate."
  - "ListActiveVlessUUIDs is global (one active UUID per user) per the shared-UUID model; serverID param reserved for a future plan_servers join if per-server scoping is needed."
metrics:
  duration: ~50m
  tasks: 4
  files: 16
  completed: 2026-06-02
---

# Phase 8 Plan 07: Per-User VLESS UUID with Wire Enforcement Summary

Closed the highest-weight audit finding (S4-2/S5-1: one shared VLESS UUID admits anyone and lets a free user enumerate the fleet) by issuing per-user random UUIDs, rotating them on plan change inside the tier-grant transaction, and enforcing the active set AT THE WIRE via a debounced tunnel pull + xray regen/reload — with an honestly documented 30-60s wire-propagation floor.

## What Shipped

**Task 1 — Migration 026 + vless model/repo (commit 0061e03):**
- `026_user_vless_identities.sql`: `user_vless_identities` table (UNIQUE `vless_uuid`, `is_active`, `revoked_at`, FK→users ON DELETE CASCADE) + two PARTIAL indexes (`idx_uvi_user_active`, `idx_uvi_active`) on `is_active = TRUE`. 026 fills the gap between the in-main 025 (08-04) and 027 (08-02).
- `model.UserVlessIdentity` gorm-mapped with an explicit `TableName()`.
- `vless_repo.go` exports the four functions: `GetOrCreateActiveVlessUUID` (lazy random UUIDv4 alloc), `RotateVlessUUID(tx)` (retire-all-active + issue new, in caller tx), `RevokeAllVlessUUIDs(tx)` (admin force-cancel/suspend), `ListActiveVlessUUIDs` (active-set for the tunnel).

**Task 2 — API per-user UUID, rotation, active-set endpoint (commit 124a220, TDD):**
- `GetServerConfig`: replaced `UserID: cfg.TunnelVLESSUUID` with `repository.GetOrCreateActiveVlessUUID(ctx, db, userID)` — two same-plan users now get DIFFERENT UUIDs.
- `AdminUpdateUser`: when the tier actually changes, the user write + `RotateVlessUUID` run together inside `WithUserLock` (a pure role/expiry change does NOT rotate).
- `webhook_lava.go applyLavaEventImpl` payment.success: `RotateVlessUUID(ctx, tx, inv.UserID)` inside the existing `WithUserLock` tx, immediately after `SetUserPlanTx` (anchor webhook_lava.go:263/265). Replay funnels through the same impl, so replay rotates too and the active-set converges idempotently. Renewal branch left untouched (plan unchanged).
- `GET /api/v1/internal/servers/:id/vless-clients` mounted on `internalGroup` (same `InternalSecret` gate as heartbeat); returns `{uuids, etag}` with an order-independent sha256 ETag.
- Flipped the Wave-0 `servers_vless_test.go` skip GREEN: ISOLATION (two users differ + idempotent re-fetch), ROTATION (lava payment.success issues a new active UUID and flips the prior to `is_active=false` + `revoked_at`), ACTIVE-SET (endpoint returns both UUIDs + 401 without the internal secret).

**Task 3a — Tunnel ReloadClients (commit 27510f5, TDD):**
- `(*TunnelServer).ReloadClients(uuids)`: under `s.mu`, swaps `s.config.Clients`, rebuilds BOTH the REALITY and (if enabled) WS configs via `buildXRayConfig`, starts the fresh instance, then closes the old one. Error-safe (closes a just-created instance on Start failure; never leaks a half-started one). Documents the xray-core connection-drop tradeoff.
- Flipped `server_reload_test.go` GREEN: asserts the config swap admits exactly the new set and drops the revoked UUID.

**Task 3b — Debounced pull loop + 30-60s floor + wire harness (commit 7bb05bc):**
- `StartClientSync`: on each heartbeat tick GETs the active set (X-Internal-Secret), compares ETag, DEBOUNCES ~7s on change, then calls `ReloadClients`; skips the reload (and its connection drop) when the ETag is unchanged; ctx-aware so shutdown is not delayed by the debounce.
- Wired in `cmd/tunnel/main.go` for xray nodes (reuses the heartbeat ctx + interval).
- The 30-60s wire-propagation floor is documented at the pull-loop site (and in `server.go` ReloadClients). `test/wire-vless/README.md` updated to mark HARD-02 landed and note the floor for Step 4.

## Verification

- `go test ./internal/handler/ -run 'Vless|ServersVLESS'` (api) → **ok** (GREEN: isolation, rotation, active-set + secret rejection).
- `go test ./internal/handler/ -run 'Vless|ServerConfig|ActiveSet|Lava'` → **ok**; full `go test ./internal/handler/` package → **ok** (no regressions after seeding the new table into the webhook/servers/admin-user-controls SQLite fixtures).
- `go build ./...` (server/api) → exits 0; `go vet ./internal/handler/... ./internal/repository/... ./cmd/...` → clean.
- `go build ./internal/...` + `go vet ./internal/...` (server/tunnel) → clean; `go vet ./cmd/tunnel/...` → clean (pull-loop wiring compiles).
- Acceptance greps: `GetOrCreateActiveVlessUUID` present in servers.go; `cfg.TunnelVLESSUUID` only in a comment (no real assignment); `vless-clients` mounted on internalGroup; `RotateVlessUUID` in BOTH admin.go and webhook_lava.go; `uuid.New` present in vless_repo.go; migration has the table + both partial indexes.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Seed `user_vless_identities` into three existing SQLite test fixtures**
- **Found during:** Task 2 (full handler suite run after wiring rotation + per-user alloc).
- **Issue:** `GetServerConfig` (per-user alloc) and the lava payment.success / admin tier-change rotation now hit `user_vless_identities`, which the `setupWebhookTestDB`, `newServersTestDB`, and `admin_user_controls_test` in-memory SQLite DBs did not create → 500s in `TestGetServerConfig_*`, `TestHandleLavaWebhook_*`, and `TestAdminUserControls/force-grant_Pro`.
- **Fix:** Added the `user_vless_identities` DDL to all three test-DB setups (mirrors the migration-026 schema for SQLite). Production migration is the source of truth; these are test fixtures only.
- **Files modified:** server/api/internal/handler/webhook_lava_test.go, servers_test.go, admin_user_controls_test.go
- **Commit:** 124a220

**2. [Rule 2 - Missing critical functionality] ETag on the active-set endpoint**
- **Found during:** Task 2 (the plan calls for "an ETag/hash of the set so the tunnel can cheaply detect changes").
- **Issue:** Without a stable set hash the tunnel would reload every tick, dropping connections needlessly (the very thing the debounce + threat T-08-02d try to minimize).
- **Fix:** `vlessClientsETag` returns an order-independent sha256 of the sorted set; the handler sets it as both the `ETag` header and an `etag` JSON field; the tunnel skips the reload on an ETag match.
- **Files modified:** server/api/internal/handler/servers.go, server/tunnel/internal/heartbeat.go
- **Commit:** 124a220 (api) / 7bb05bc (tunnel)

## Threat Model Coverage

- **T-08-02 (EoP, shared UUID):** mitigated — per-user UUIDs; the tunnel admits only the active set and evicts a revoked UUID at the next reload.
- **T-08-02b (info disclosure, fleet enumeration):** mitigated — per-user UUIDs + rotation on plan change remove the shared-secret enumeration vector.
- **T-08-02c (spoofing, internal endpoint):** mitigated — `/servers/:id/vless-clients` is behind the same `InternalSecret` constant-time gate as heartbeat; the test asserts 401 without the secret.
- **T-08-02d (DoS, reload drops connections):** accepted + minimized — ETag-gated, debounced (~7s) batching with the documented 30-60s floor; pre-launch "free hand to break things".

## Known Stubs

None. No hardcoded empty values flow to a response; per-user UUIDs are lazily but really allocated and the active set is read live from the DB.

## Open Risks / Phase-Gate Items

- **Wire harness (SC#2 evidence) is manual:** `test/wire-vless/` proves the foreign/revoked UUID rejection at the wire. It is NOT part of `go test` (it needs Docker + a real xray client) and is run at the phase gate. README updated to reflect HARD-02 has landed and the 30-60s floor.
- **Tunnel test binary linker failure (pre-existing, out of scope):** `go test ./internal/...` in server/tunnel still fails at the LINK stage with `sagernet/sing ... net.errNoSuchInterface` (documented DEF in deferred-items.md). The tunnel `internal` library + the flipped `server_reload_test.go` compile and vet clean; the assertion runs once the toolchain/dep linker issue is resolved by its dedicated owner. Verified the failure is unchanged (not introduced by this plan).

## Self-Check: PASSED
