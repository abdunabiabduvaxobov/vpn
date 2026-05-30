# Phase 7: Admin Panel Overhaul - Research

**Researched:** 2026-05-30
**Domain:** Go/Fiber admin API + Vite/React 19/TanStack Query admin SPA; Postgres advisory locks; webhook replay; operational controls (drain, maintenance, readyz/livez)
**Confidence:** HIGH (every recommendation grounded in named existing files read this session; no external library research needed — stack is locked and already in `go.mod`/`package.json`)

## Summary

Phase 7 is **almost entirely additive against an already-mature codebase**. The backend (`server/api`) is at migration `023`, has a working lava webhook handler + idempotency table (`lava_webhook_events`), an audit-logging middleware that auto-records every admin mutation, a Redis cache layer with established bust patterns (`cache:servers:active`, `user:<id>`), a scheduler with a `RUN_SCHEDULER` gate, and per-server tunnel heartbeat plumbing already exists on the tunnel side. The admin SPA (`admin-web`, Vite + React 19 + TanStack Query + shadcn/ui) has 7 pages, an axios client with auth-refresh interceptor, and a `refetchInterval: 60_000` polling precedent already on the Dashboard. **Nothing here requires a new framework, a new transport, or a rewrite.**

The eight ADMIN requirements break into three risk tiers: (A) **read-only KPIs + health endpoints** (ADMIN-01, ADMIN-07, ADMIN-08) — pure new GET handlers + SQL, zero mutation risk; (B) **stateful controls** (ADMIN-02, ADMIN-04, ADMIN-05) — new columns (`users.suspended_at`, `vpn_servers.is_draining`), new middleware (suspended-check, maintenance-mode), feature-flag + broadcast tables; (C) **concurrency-safe payment ops + webhook replay** (ADMIN-03, ADMIN-06) — the genuinely hard part, requiring a Postgres transaction-level advisory lock shared between the existing webhook handler and the new admin force-cancel handler, plus refactoring the webhook event-dispatch into a reusable function the replay endpoint can call.

**Primary recommendation:** Sequence the work A → B → C exactly as `ADMIN-IMPROVEMENTS.md §7` lays out. Use **`pg_advisory_xact_lock(hashtextextended(user_id,0))`** via a new `repository.WithUserLock` helper, wired into BOTH `webhook_lava.go`'s tier-grant path AND the new force-cancel handler. Use **TanStack Query `refetchInterval`** (already the codebase's pattern) for live KPIs — NOT SSE/websocket. Add migration **`024`** (next number) for all Phase-7 columns/tables; they have no inter-dependencies and can be one migration or several.

<user_constraints>
## User Constraints

No `CONTEXT.md` exists for this phase — the operator chose to plan from research + the existing design docs (`ADMIN-IMPROVEMENTS.md` is effectively the PRD). The binding constraints therefore come from `CLAUDE.md`, `REQUIREMENTS.md` (ADMIN-01..08), and the GSD phase-boundary note. These are LOCKED:

### Locked Decisions (from CLAUDE.md + ADR-007 + accumulated STATE.md)
- **Backend stack:** Go 1.25 + Fiber v2 + GORM + Postgres 16 + Redis 7. No language switch. `[VERIFIED: CLAUDE.md, server/api/go.mod directive]`
- **Admin-web stack:** Vite + React 19 + TanStack Query + shadcn/ui + Tailwind 4. `[VERIFIED: admin-web/package.json — react 19.0.0, @tanstack/react-query 5.66, axios 1.7.9, recharts 3.8, react-router-dom 7.1, zustand 5]`
- **Deployment:** single VM via Docker Compose for v2.2.0. Multi-replica API is a future goal gated behind `RUN_SCHEDULER` (already implemented) — design for single-VM but do not BREAK the multi-replica future. `[VERIFIED: CLAUDE.md; config.RunScheduler]`
- **Webhook idempotency:** lava.top retries ≤20×. Handler MUST be idempotent (UNIQUE on event identifier) and MUST return 500 on processing error so retries fire. The existing `lava_webhook_events` natural-key UNIQUE already enforces this. `[VERIFIED: CLAUDE.md; migration 020/021; webhook_lava.go]`
- **Security:** Pro launch = real money. Critical/High audit findings must land before any user pays. Every new mutating admin endpoint MUST stay on the audited admin group. `[VERIFIED: CLAUDE.md]`
- **Single `admin` role for v1.** No scoped-permission system; `users.role IN ('user','admin')`. Do NOT build a permission table (`ADMIN-IMPROVEMENTS.md §9.3` — explicitly rejected for v1). `[CITED: ADMIN-IMPROVEMENTS.md §9.3]`
- **GSD workflow enforcement:** all repo edits go through a GSD command. `[VERIFIED: CLAUDE.md]`

### Claude's Discretion (within the design doc's guardrails)
- Live-KPI refresh mechanism (SSE vs polling vs websocket) — design doc leaves it open; this research RESOLVES it to short-polling (§ Architecture Patterns, Decision 1).
- Exact migration split (one `024` migration vs several) — both acceptable; columns/tables are independent.
- Whether MRR/churn KPIs are computed live or Redis-cached — research recommends a 5-min Redis cache for MRR only (§ ADMIN-01).
- Maintenance-mode storage (Redis flag vs DB vs in-memory) — research RESOLVES to a DB-backed `feature_flags` row fronted by a short-TTL Redis cache (§ ADMIN-05).

### Deferred Ideas (OUT OF SCOPE for Phase 7)
- **Phase 8 hardening (HARD-*) — DO NOT PULL IN.** Specifically: admin security headers HSTS/CSP/X-Content-Type-Options (HARD-08), admin search `len>=3` + prefix-only hardening (HARD-06), admin role-change before→after diff (HARD-07), `/health` runtime.Version() removal (HARD-17), `govulncheck` CI (HARD-09), log redaction (HARD-10). Phase 7 may NOTE these boundaries but must not implement them. `[CITED: additional_context — "admin route security headers + admin search hardening are Phase 8"]`
- `super_admin` scoped roles (`ADMIN-IMPROVEMENTS.md §9.3` — "only if the team grows past 3 operators").
- Impersonate user (read-only) — `ADMIN-IMPROVEMENTS.md §3.1` flags as out-of-scope v1.
- Funnel "visitor count" stage — needs landing analytics not yet built (`§2.5`); render as "—".
- `deploy_markers` CI hook, incident timeline, anomaly-banner Redis 5xx buckets, server probe, backup-run-now — `ADMIN-IMPROVEMENTS.md` tags these `[later]` / Phase E. Treat as out-of-scope unless explicitly added; the 8 ADMIN reqs do NOT require them.
- Refund execution against lava.top — `ADMIN-IMPROVEMENTS.md §3.1` notes lava refund API support is uncertain; force-cancel marks local state + records the operator's refund intent, it does NOT call a lava refund endpoint (no such method exists in `internal/lava/`). See Assumptions A3.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ADMIN-01 | Dashboard live KPIs: total users, paid users this period, MRR estimate, active connections, signups today/wk/mo, churn count, failed payments count | § ADMIN-01 — extend `repository.GetGlobalStats` + new `GetRevenueSummary`; all data already in `users`/`connections`/`plans`/`plan_offers`/`lava_webhook_events`; poll via TanStack Query `refetchInterval` |
| ADMIN-02 | Per-user controls: suspend, force-grant Pro, force-cancel Pro, reset to free, force-disconnect all devices, view audit/payment/connection history | § ADMIN-02 — new `users.suspended_at`/`suspended_reason` columns + suspended middleware; force-grant/reset already exist via `AdminUpdateUser`; new suspend/disconnect/cancel handlers on the audited admin group |
| ADMIN-03 | Per-user advisory lock so admin force-cancel + webhook payment.success never leave inconsistent state | § ADMIN-03 — `repository.WithUserLock` using `pg_advisory_xact_lock(hashtextextended($uid,0))`; wire into BOTH `webhook_lava.go` tier-grant AND force-cancel handler |
| ADMIN-04 | Server controls: force-disconnect all clients, mark in/out of rotation, drain mode | § ADMIN-04 — new `vpn_servers.is_draining` column; extend `adminUpdateServerRequest`; filter draining from `ListActiveServers`; disconnect-all sets `disconnected_at=now()` by server_id + Redis pub-sub kill to tunnel |
| ADMIN-05 | System controls: feature flags (signups/payments off), maintenance mode (503 to non-admins), broadcast banner | § ADMIN-05 — new `feature_flags` + `broadcast_messages` tables; new maintenance middleware mounted before admin group; public `GET /broadcasts` |
| ADMIN-06 | Webhook event log: every lava webhook with status (DELIVERED/FAILED/REPLAYED), payload, replay button | § ADMIN-06 — `lava_webhook_events` table already exists; add `status` column; refactor `webhook_lava.go` dispatch into reusable `applyLavaEvent`; new `GET /admin/webhook-events` + `POST .../replay` |
| ADMIN-07 | `GET /readyz` 200 only when DB+Redis+lava+tunnel healthy; `GET /livez` 200 if process alive | § ADMIN-07 — new public handlers; `livez` returns immediately; `readyz` pings DB+Redis, checks `lava` reachability + tunnel `last_seen_at` freshness, with per-dep timeout + short Redis cache to avoid flapping |
| ADMIN-08 | Dependencies-health page: live status of DB, Redis, lava reachability, tunnel heartbeat | § ADMIN-08 — admin-auth `GET /admin/system/deps-health` reuses the readyz probe + tunnel-server table; new System page tab polling it |
</phase_requirements>

## Standard Stack

No new libraries are required. Everything is already a direct dependency. **Do NOT add SSE libraries, websocket libraries, a job queue, or a permissions library.**

### Core (already present — use these)
| Library | Version (verified) | Purpose | Why Standard Here |
|---------|-------------------|---------|-------------------|
| Fiber v2 | in `server/api/go.mod` | HTTP router/middleware | Entire API is Fiber; admin group already wired `[VERIFIED: cmd/main.go]` |
| GORM | in `server/api/go.mod` | ORM + raw SQL escape hatch (`db.Exec`, `db.Raw`) | Advisory lock + KPI aggregates use `db.WithContext(ctx).Exec/Raw` `[VERIFIED: connection_repo.go uses Exec for INSERT…SELECT]` |
| go-redis v9 | `github.com/redis/go-redis/v9` | cache, pub-sub, flags | Already used for `cache:servers:active`, `user:<id>`, rate limit, blacklist `[VERIFIED: cache/redis.go]` |
| zap | `go.uber.org/zap` | structured logging | Every handler takes `*zap.Logger` `[VERIFIED: admin.go signatures]` |
| @tanstack/react-query | ^5.66.0 | data fetching + polling | `refetchInterval` already used on Dashboard `[VERIFIED: pages/Dashboard.tsx:53]` |
| axios | ^1.7.9 | HTTP client w/ auth-refresh interceptor | `admin-web/src/api/client.ts` `[VERIFIED]` |
| shadcn/ui + Radix | per package.json | UI primitives (Card, Table, Dialog, Switch, Tabs, Badge, Select, Tooltip) | All needed primitives already vendored under `components/ui/` `[VERIFIED]` |
| recharts | ^3.8.1 | charts/sparklines | `StatsChart.tsx`, `AnalyticsSection.tsx` already use it `[VERIFIED]` |
| sonner | ^1.7.4 | toast notifications | for undo/confirm toasts `[VERIFIED: package.json]` |
| zod + react-hook-form | ^3.25 / ^7.76 | form validation (broadcast/suspend reason forms) | `components/ui/form.tsx` + PlanForm precedent `[VERIFIED]` |

### Supporting (mechanism, not library)
| Mechanism | Where it lives | When to use |
|-----------|---------------|-------------|
| `pg_advisory_xact_lock` | Postgres built-in, called via `db.Exec` | per-user serialization (ADMIN-03) |
| Redis pub-sub | go-redis `Publish`/`Subscribe` | force-disconnect kill signal to tunnel (ADMIN-02/04) — **see Open Question 1: a live pub-sub kill channel does NOT currently exist** |
| `feature_flags` Redis-cached map | new table + new `cache/flags.go` | feature flags + maintenance mode (ADMIN-05) |

### Alternatives Considered (and rejected)
| Instead of | Could Use | Why rejected for this phase |
|------------|-----------|----------------------------|
| TanStack `refetchInterval` polling | SSE (`text/event-stream`) | SSE holds a long-lived connection per open dashboard tab; on single-VM Fiber it adds connection-management complexity for ~1 operator. Polling is already the codebase pattern. Multi-replica future would need sticky routing for SSE. `[CITED: ADMIN-IMPROVEMENTS.md §2 implies live-refresh, no transport mandated]` |
| TanStack `refetchInterval` polling | websocket | Same downside as SSE plus a new dependency. No bidirectional need. |
| advisory locks | `SELECT … FOR UPDATE` row locks | Cancellation touches `users` + `subscriptions` + `lava_contracts` + `audit_log`; row locks don't compose across tables, and the webhook may INSERT a brand-new subscription (no row to lock). `[CITED: ADMIN-IMPROVEMENTS.md §8.2]` |
| advisory locks | optimistic `users.version` CAS | Single-table only; doesn't cover cross-table coordination. `[CITED: ADMIN-IMPROVEMENTS.md §8.5]` |
| Redis-only maintenance flag | DB `feature_flags` + Redis cache | Pure-Redis loses the flag on Redis flush/restart; DB is source of truth, Redis is the fast read path with short TTL (matches existing `user:<id>` two-layer pattern). |

## Architecture Patterns

### Existing project structure (Phase 7 extends, does not restructure)
```
server/api/
├── cmd/main.go                    # ALL route wiring + Fiber setup (NOT cmd/api/ — note the path)
├── migrations/                    # NNN_name.sql, plain SQL, currently at 023 → next is 024
├── internal/
│   ├── handler/                   # admin.go, webhook_lava.go, health.go, servers.go, payment.go
│   │   └── (NEW) admin_system.go, admin_users_controls.go, admin_servers_controls.go,
│   │            admin_webhooks.go, admin_revenue.go, health (extend)
│   ├── middleware/                # auth.go, admin.go (AdminRequired), audit.go (AuditLog)
│   │   └── (NEW) maintenance.go, suspended.go
│   ├── repository/                # user_repo, server_repo, connection_repo, webhook_event_repo,
│   │   │                          #   admin_repo (stats), subscription_repo, expiry_repo, db.go
│   │   └── (NEW) lock.go (WithUserLock), feature_flag_repo.go, broadcast_repo.go,
│   │            extend admin_repo.go (KPIs), webhook_event_repo.go (list+replay)
│   ├── cache/                     # redis.go, servers_cache.go, user_cache.go, heartbeat_cache.go
│   │   └── (NEW) flags_cache.go (maintenance/feature flag map)
│   ├── model/                     # user.go, server.go, subscription.go, lava_webhook_event.go, audit.go
│   │   └── (NEW) feature_flag.go, broadcast.go; extend server.go (+IsDraining,+LastSeenAt), user.go (+SuspendedAt)
│   ├── lava/                      # client.go (HTTP client, 5s timeout) — readyz reachability uses this
│   └── scheduler/scheduler.go     # RUN_SCHEDULER-gated; can host the concurrent-conn sampler if added
admin-web/src/
├── App.tsx                        # react-router routes (ADD: /payments, /system, extend /servers, /users/:id)
├── api/                           # one file per resource (client.ts is the axios instance)
│   └── (NEW) system.ts, webhooks.ts, revenue.ts; extend users.ts, servers.ts, stats.ts
├── pages/                         # Dashboard, Users, UserDetail, Servers, Activity, Settings, Plans
│   └── (NEW) System.tsx, Payments.tsx; revamp Dashboard, Servers, UserDetail
└── components/layout/AdminLayout.tsx   # sidebar (ADD nav items)
```

### Pattern 1: Audited mutating admin endpoint (FOLLOW EXACTLY)
**What:** Every new state-changing admin endpoint mounts on the existing admin group so it is auto-audited.
**When:** ADMIN-02, ADMIN-04, ADMIN-05, ADMIN-06 mutation endpoints.
```go
// Source: cmd/main.go route wiring + middleware/audit.go (VERIFIED this session)
// The admin group is: api.Group("/admin", AuthRequired(...), AdminRequired(db), AuditLog(db, logger))
// AuditLog runs POST-handler and only writes when the handler returned 2xx — so a
// new endpoint gets a compliance row for free. The action name is derived by
// describeAction(method, path); add a new case there for a readable label
// (e.g. "suspend_user", "drain_server", "replay_webhook") or it falls back to
// post_admin_users_<uuid>_suspend (still correct, just less readable).
admin.Post("/users/:id/suspend", handler.AdminSuspendUser(logger, db, redisClient))
```
**Critical:** the audit middleware records query+params, NOT the JSON body (`audit.go` comment: "Fiber does not expose a parsed map we can snapshot"). The mandatory `reason` field from `ADMIN-IMPROVEMENTS.md §9.1` therefore will NOT land in `audit_log.details` unless the handler explicitly writes it. **Plan a follow-up:** either (a) the handler calls `repository.CreateAuditEntry` itself with the reason in details, or (b) accept that v1 reason is validated-but-not-audited. Recommend (a) for force-cancel/suspend. See Pitfall 4.

### Pattern 2: Synchronous cache-bust after a write (FOLLOW EXACTLY)
**What:** After mutating user/server state, synchronously bust the relevant Redis cache, best-effort (log on failure, never fail the write).
**When:** ADMIN-02 (user writes → `BustUserCache`), ADMIN-04 (server drain/disconnect → `BustServersCache`).
```go
// Source: handler/admin.go AdminUpdateUser:229 and AdminUpdateServer:481 (VERIFIED)
if err := cache.BustUserCache(c.Context(), redisClient, userID); err != nil {
    logger.Warn("admin: BustUserCache failed (5s TTL is the backstop)", zap.String("user_id", userID), zap.Error(err))
}
// For server drain toggle, bust cache:servers:active so the next non-admin GET /servers
// stops returning the drained server within one request:
if err := cache.BustServersCache(c.Context(), redisClient); err != nil { ... }
```
**Why load-bearing for ADMIN-04 success criterion 4:** the drain toggle MUST bust `cache:servers:active` or `GET /servers` keeps serving the drained server from cache for up to the TTL.

### Pattern 3: Per-user advisory lock (NEW — the core of ADMIN-03)
**What:** Serialize all subscription-mutating operations for a single user under one symbolic Postgres lock.
**When:** force-cancel handler AND the webhook payment.success tier-grant path.
```go
// NEW repository/lock.go — pattern from ADMIN-IMPROVEMENTS.md §8.1, adapted to the
// codebase's ctx-propagation convention (db.WithContext(ctx) everywhere — PERF-07).
func WithUserLock(ctx context.Context, db *gorm.DB, userID string, fn func(tx *gorm.DB) error) error {
    return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        // hashtextextended maps the UUID string → bigint (the type advisory locks need).
        // _xact_lock auto-releases on COMMIT/ROLLBACK — no unlock bookkeeping, crash-safe.
        if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", userID).Error; err != nil {
            return err
        }
        return fn(tx)   // all subscription/user/audit writes happen on tx, inside the lock
    })
}
```
**Wiring (both sides MUST take the lock or the race is not closed):**
- Force-cancel handler: `WithUserLock(ctx, db, userID, func(tx){ re-read state; if already cancelled → 409; mark lava_contracts cancelled; write audit row })`.
- `webhook_lava.go` tier-grant path (`payment.success` / `recurring.payment.success`): wrap the existing tier-grant + contract-upsert in `WithUserLock` keyed on the resolved `user_id`. The webhook currently resolves user via the invoice/contract → user mapping; lock on that resolved `user_id`. **The webhook's own UNIQUE-constraint idempotency stays — the advisory lock is ADDITIONAL, serializing against the admin path, not replacing event de-dup.**

### Pattern 4: Webhook replay reusing the dispatch path (NEW — core of ADMIN-06)
**What:** Refactor the existing `webhook_lava.go` event dispatch so the replay endpoint re-applies a stored payload through the SAME code path, idempotently.
```go
// Today webhook_lava.go does: auth → dedup-insert → dispatch-by-event-type → mark processed.
// REFACTOR the "dispatch-by-event-type" block into a standalone, transport-free function:
func applyLavaEvent(ctx context.Context, db *gorm.DB, redis *redis.Client, logger *zap.Logger, ev model.LavaWebhookEvent) error { ... }
// The live handler calls it; the new replay endpoint calls it with the STORED payload row.
// Replay semantics (per success criterion 3 + ADMIN-06):
//   1. Load the lava_webhook_events row by id (admin endpoint).
//   2. Re-run applyLavaEvent using the stored payload — it is idempotent because the
//      underlying tier-grant is "set tier=pro, set expires_at=period_end" (set-not-increment),
//      so re-applying yields the same state, never a double grant.
//   3. Set the row's status → 'REPLAYED' (new column) and bump retried_count.
// CRITICAL: replay MUST take the same WithUserLock(user_id) as the live path, or a replay
// racing a live webhook re-introduces the ADMIN-03 race.
```

### Pattern 5: Frontend live-data page (FOLLOW Dashboard.tsx)
```tsx
// Source: pages/Dashboard.tsx:50-54 (VERIFIED) — the established live-refresh idiom
const { data, isLoading, isError } = useQuery({
  queryKey: ["admin", "stats"],
  queryFn: getAdminStats,
  refetchInterval: 60_000,      // KPI cards
});
// For the "active connections" sparkline that needs to feel live, use a shorter
// interval (e.g. 15_000) on its own query key — do NOT globally drop the 60s default.
```

### Anti-Patterns to Avoid
- **Mounting a mutating endpoint OUTSIDE the admin group** → no audit row, breaks the compliance guarantee. The ONLY intentional non-audited endpoint is the tunnel heartbeat (`§9.2`).
- **Reading `X-Forwarded-For`/`X-Real-IP` by hand** → forbidden by PAY-06; the webhook IP allowlist already uses Fiber `TrustedProxies` (`middleware/lava_ip_allowlist.go`). The internal tunnel-heartbeat endpoint should use a shared-secret header, not IP.
- **Doing the advisory lock on only one of the two code paths** → the race is NOT closed. Both webhook and admin must lock.
- **Computing MRR on every 60s dashboard poll without caching** → repeated multi-join aggregate against `plans`/`plan_offers` every minute per open tab. Cache MRR 5 min in Redis (`cache:admin:mrr:<currency>`).
- **Making `/readyz` synchronously dial lava.top on every call** → turns a health probe into a flaky, slow dependency. Cache the lava-reachability result (and tunnel heartbeat freshness is a cheap DB read of `last_seen_at`). See Pitfall 3.
- **Pulling Phase 8 hardening in** (CSP headers, search `len>=3`, role-diff audit) — explicitly out of scope.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Per-user serialization | A Redis mutex / in-process `sync.Map` of locks | `pg_advisory_xact_lock` via `WithUserLock` | In-process locks break across replicas; Redis locks need lease/renewal/fencing. Postgres xact lock is crash-safe (auto-release on COMMIT/ROLLBACK) and works the moment the row is in the same DB. `[CITED: ADMIN-IMPROVEMENTS.md §8.1]` |
| Webhook idempotency | A new de-dup scheme for replay | The existing `lava_webhook_events` UNIQUE natural key + set-not-increment tier grant | Already built & tested (`webhook_idempotency_test.go` was referenced; `migration 020/021` natural-key index). Replay reuses it. |
| Audit logging of admin actions | A new audit write in every handler | Mount on the audited admin group (`AuditLog` middleware) | Auto-records every 2xx mutation. Only add a manual `CreateAuditEntry` when you need the `reason` body field in details (Pitfall 4). `[VERIFIED: middleware/audit.go]` |
| Cache invalidation | A new pub-sub cache-coherence layer | Existing synchronous `BustUserCache` / `BustServersCache` | Established two-layer (bust + short TTL) pattern; matches PERF-04/PERF-01. `[VERIFIED: cache/user_cache.go, servers_cache.go]` |
| Live dashboard transport | SSE/websocket server | TanStack `refetchInterval` | Already the pattern; single operator; no bidirectional need. |
| Scheduler for periodic sampling | A new goroutine/cron | The existing `scheduler` package (RUN_SCHEDULER-gated, modulo-tick registry) | If a concurrent-connection sparkline sampler is added it belongs here. `[VERIFIED: scheduler/scheduler.go]` |

**Key insight:** the hardest-looking requirements (ADMIN-03 advisory lock, ADMIN-06 replay) are made tractable by ONE refactor each — extract `WithUserLock` and extract `applyLavaEvent` — after which the new admin endpoints are thin callers. Resist building parallel machinery.

## Runtime State Inventory

> Phase 7 is largely additive (new endpoints/columns/pages), but it DOES introduce new runtime state and touches the live tunnel server. Inventory below.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | New columns: `users.suspended_at`, `users.suspended_reason`, `vpn_servers.is_draining`, `vpn_servers.last_seen_at` (+ `last_state_changed_at` if uptime shown), `lava_webhook_events.status` (DELIVERED/FAILED/REPLAYED). New tables: `feature_flags`, `broadcast_messages`. All in **migration 024+** (next free number — 023 is highest). Code edit only for new rows; **data migration:** backfill `lava_webhook_events.status` from existing `processed_at`/`error` (processed→DELIVERED, error→FAILED) so old rows render correctly in the log. | migration + backfill |
| Live service config | **Tunnel server** receives a new responsibility for ADMIN-04 force-disconnect (kill live tunnels) and ADMIN-07/08 heartbeat. The tunnel already runs (`server/tunnel`); its heartbeat-emit + kill-subscribe behaviour is NOT confirmed present — see Open Question 1. lava.top webhook + offer config unchanged. | verify tunnel capability / API patch |
| OS-registered state | None. No Task Scheduler / launchd / pm2 state embeds Phase-7 identifiers. Single-VM Docker Compose; the scheduler is in-process behind `RUN_SCHEDULER`. **None — verified by repo structure (docker-compose.*.yml, no host cron).** | none |
| Secrets/env vars | New env likely needed: tunnel-heartbeat shared secret (`INTERNAL_HEARTBEAT_SECRET` or similar) for `POST /internal/servers/:id/heartbeat`; optionally `METRICS_USER/PASS` if `/metrics` is added (out of scope unless requested). `RequireEnv()` in `config.go` is a single-pass aggregate validator — add any new REQUIRED key there. Existing `LAVA_*` keys are reused for readyz reachability. | add env + RequireEnv entry |
| Build artifacts | None. Go binary + Vite bundle rebuild from source; no stale egg-info/global-install equivalents. **None — verified.** | none |

**Canonical question — after every file is updated, what runtime state still holds old values?** Only the `lava_webhook_events` rows that predate the new `status` column (handled by backfill) and any live tunnel connections at drain/disconnect time (handled by the kill mechanism — Open Question 1). Nothing else persists Phase-7 state outside the DB.

## Common Pitfalls

### Pitfall 1: The advisory lock keyed on the wrong identity
**What goes wrong:** Webhook locks on `contract_id` or invoice while the admin handler locks on `user_id` → different lock keys → no mutual exclusion → the ADMIN-03 race stays open.
**Why it happens:** the webhook's natural identifier is the contract; the admin's is the user.
**How to avoid:** BOTH paths must `WithUserLock(resolved_user_id)`. The webhook must resolve `user_id` (via invoice→user or contract→user mapping it already does for the tier grant) BEFORE taking the lock.
**Warning sign:** a concurrency test that interleaves force-cancel + payment.success and still produces a hybrid state.

### Pitfall 2: Drain toggle not busting `cache:servers:active`
**What goes wrong:** Admin marks a server draining, but `GET /servers` keeps returning it to mobile clients until the cache TTL expires (criterion 4 fails: "stops returning it to non-admins").
**Why it happens:** PERF-01 cached `/servers` in Redis; a column write doesn't auto-invalidate.
**How to avoid:** the drain/undrain handler MUST call `cache.BustServersCache` synchronously (same pattern as `AdminUpdateServer`). AND `ListActiveServers` must add `AND is_draining = false` to its WHERE (currently `WHERE is_active = ?`).
**Warning sign:** drained server still appears in a non-admin `/servers` response within the TTL window.

### Pitfall 3: `/readyz` flapping or hanging on lava.top
**What goes wrong:** `readyz` dials lava on every probe; lava latency/blips make readyz flap 200↔503, or a hung dial blocks the probe.
**Why it happens:** treating an external 3rd-party API as a hard, synchronous, per-call readiness dependency.
**How to avoid:** (a) give each dep check a tight timeout (DB/Redis ping ~500ms per `ADMIN-IMPROVEMENTS.md §4.1`); (b) cache the lava-reachability result in Redis for ~30–60s (a background or last-known check, not a fresh dial per probe); (c) tunnel health = cheap DB read of `vpn_servers.last_seen_at > now()-interval` (no network call). `livez` does ZERO I/O and returns immediately.
**Warning sign:** readyz p95 latency tracks lava latency; container restarts triggered by transient lava blips.

### Pitfall 4: `reason` validated but not audited
**What goes wrong:** Force-cancel/suspend require a typed `reason` (`§9.1`), the handler validates it, but `AuditLog` middleware records only query+params (not the JSON body), so the operator's justification never lands in `audit_log.details`.
**Why it happens:** the audit middleware explicitly can't snapshot the parsed body (`audit.go` comment).
**How to avoid:** for reason-carrying endpoints, the handler itself calls `repository.CreateAuditEntry` with `details["reason"]=reason` (and let the middleware's row be the redundant outer record, or skip the group's audit for these specific routes). Decide one approach consistently.
**Warning sign:** audit log shows "suspend_user" rows with no reason text.

### Pitfall 5: Maintenance middleware blocking admin's own escape hatch
**What goes wrong:** maintenance mode 503s ALL non-admin requests — but if mounted too broadly it also 503s the admin login or the flag-toggle route, so the operator can't turn it back off.
**Why it happens:** middleware ordering / route-group scoping.
**How to avoid:** mount the maintenance middleware so admin routes (and `/auth/admin-login`, `/livez`, `/readyz`) are exempt. `ADMIN-IMPROVEMENTS.md §3.6`: "Admin routes remain available so the operator can turn it back off." Check the flag from a short-TTL Redis cache (10s) so toggling propagates fast without hammering the DB per request.
**Warning sign:** operator locked out after enabling maintenance mode.

### Pitfall 6: Migration numbering collision
**What goes wrong:** a Phase-7 migration is numbered `022` or `023` (already taken by Phase 6 perf indexes).
**Why it happens:** stale assumption about the highest number.
**How to avoid:** highest existing is `023_connections_connected_at_index.sql`. **Next free number is `024`.** `[VERIFIED: ls migrations/]` Note: this repo uses single-file `.sql` migrations (not separate up/down) — match that convention; there's a `migrations_test.go` that likely asserts sequential numbering.

### Pitfall 7: Force-disconnect blast radius
**What goes wrong:** force-disconnect-all on a busy server is high-impact; double-clicks or scripts amplify it.
**How to avoid:** `§9.5` recommends a server-side throttle (≤1 disconnect/server/60s → 429; ≤1/user/30s). Use the existing `IncrRateLimit` Redis helper (already atomic via Lua). Confirm dialogs echoing the target identifier (`§9.1`) for destructive UI actions, reusing the Plans-delete confirm pattern.

## Code Examples

### KPI extension (ADMIN-01) — extend the existing stats repo
```go
// Source pattern: admin.go AdminGetStats → repository.GetGlobalStats (VERIFIED exists).
// Extend GetGlobalStats (or add GetDashboardKPIs) with the columns ADMIN-01 needs.
// All data is local; SQL straight from ADMIN-IMPROVEMENTS.md §2.1:
//   paid_users:  count(*) FROM users WHERE subscription_tier != '<system_plan_code>'
//                AND (subscription_expires_at IS NULL OR subscription_expires_at > now())
//   active_concurrent: count(*) FROM connections
//                WHERE disconnected_at IS NULL AND last_heartbeat_at > now() - interval '2 minutes'
//   signups_today/7d/30d: existing GetTimeseries already returns per-day signups — sum windows
//   churn_30d: count(*) FROM subscriptions WHERE is_active=false AND <cancelled-window>
//              (needs a cancelled/updated timestamp — see Assumptions A2)
//   failed_payments_30d: count(*) FROM lava_webhook_events
//                WHERE event_type LIKE '%failed%' AND received_at > now() - interval '30 days'
// Use db.WithContext(ctx).Raw(...).Scan(...) — match the ctx-propagation rule (PERF-07).
```

### MRR with Redis cache (ADMIN-01)
```go
// Cache key cache:admin:mrr:<currency>, TTL 5m (ADMIN-IMPROVEMENTS.md §2.1).
// MRR = sum of MONTHLY offer amounts for active paid users; yearly offers contribute amount/12.
// Source tables: users JOIN plans JOIN plan_offers (all exist post-Phase-3).
// On cache miss: run the aggregate, SET with 5m TTL. This is a read; no bust needed
// (5m staleness on an estimate is acceptable per the design doc).
```

### readyz/livez (ADMIN-07)
```go
// Source: health.go currently has only Health() (shallow). Add two siblings, public, no auth.
// livez: return 200 {"status":"alive"} immediately, ZERO I/O.
// readyz: parallel-ish dep checks each with a tight timeout:
//   postgres: db.WithContext(ctxTimeout).Exec("SELECT 1")
//   redis:    redisClient.Ping(ctxTimeout)
//   lava:     last-known reachability from Redis cache (refreshed ≤60s), NOT a fresh dial
//   tunnel:   SELECT count(*) FROM vpn_servers WHERE is_active AND last_seen_at > now()-interval
//   200 only if all green; else 503 with {deps:{postgres:"ok",redis:"ok",lava:"...",tunnel:"..."}}
```

### Suspended-user middleware (ADMIN-02)
```go
// NEW middleware/suspended.go — runs AFTER AuthRequired on the protected user group.
// Reads users.suspended_at (or carry a 'suspended' bit; cheap since AuthRequired already
// loads/cache-checks the user). If suspended_at IS NOT NULL → 401/403, fire client refresh.
// Suspend handler should also DELETE sessions for the user (§3.1) + bust user:<id> cache.
```

### Frontend: webhook replay action (ADMIN-06)
```tsx
// Pattern: admin-web/src/api/* one-file-per-resource + TanStack useMutation + sonner toast.
// NEW api/webhooks.ts: listWebhookEvents(filters), replayWebhookEvent(id, reason).
// Payments.tsx: TanStack table; row → modal with full payload (Dialog) + "Повторить" button
// → useMutation(replayWebhookEvent) → on success invalidate ["admin","webhook-events"] + toast.
```

## State of the Art

| Old Approach (current code) | Phase-7 Approach | Impact |
|--------------|------------------|--------|
| 4 hard-coded KPI cards on Dashboard (`total_users`, `active_subscriptions`, `active_server_count`, `server_count`) | KPI bar with MRR/paid/churn/failed-payments/active-connections | extend `GetGlobalStats` + new revenue queries; Dashboard.tsx `kpis[]` grows |
| `GET /servers` filters only `is_active` | also filter `is_draining` | drain is the finer-grained primitive between active and deleted |
| Single shallow `GET /health` | `/livez` + `/readyz` + `/admin/system/deps-health` | K8s-style probes; deps-health page |
| Webhook events stored but never surfaced | webhook log UI + replay + status column | biggest support-ticket killer per `§Summary` |
| No suspend / maintenance / flags | `users.suspended_at`, `feature_flags`, maintenance middleware, broadcasts | new operational controls |

**Deprecated/outdated within this codebase:**
- `model.PlanLimits` Go map — already deleted (Phase 3); limits live in `plans` table. Don't reference it.
- Stripe paths — being removed in Phase 8 (HARD-01); webhook log should be lava-only in practice but the table is provider-agnostic in design (`ADMIN-IMPROVEMENTS.md §3.4` mentioned a generic `webhook_events`; the ACTUAL table is lava-specific `lava_webhook_events`). **Use the existing `lava_webhook_events` table — do NOT create the generic `webhook_events` table from the design doc.** See Assumptions A1.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The webhook log (ADMIN-06) is backed by the EXISTING `lava_webhook_events` table (not the generic `webhook_events` table described in `ADMIN-IMPROVEMENTS.md §3.4`, which predates the Phase-3 implementation). Add a `status` column to it. | ADMIN-06 / State of the Art | If a generic multi-provider table is actually wanted, the table + migration shape changes. Low risk: Stripe is being removed, lava is the only provider. |
| A2 | Churn (ADMIN-01) needs a cancellation timestamp. `subscriptions` has `started_at`/`expires_at`/`is_active` but `ADMIN-IMPROVEMENTS.md §2.1` says a `cancelled_at` column must be added; `lava_contracts` already HAS `cancelled_at`. Churn can be computed from `lava_contracts.cancelled_at` (Phase-3 schema) without a new column. | ADMIN-01 | If churn must count free-tier or non-lava cancellations, `lava_contracts` is insufficient and `subscriptions.cancelled_at` must be added. Confirm churn definition with operator. |
| A3 | Force-cancel "with refund" (ADMIN-02 / `§3.1`) does NOT execute a lava refund — `internal/lava/` has no refund method, and the design doc says lava refund support is uncertain. The handler validates a `refund` flag, marks local state cancelled, and records the refund intent for the operator. | ADMIN-02 | If real refunds are required, a new lava client method + lava API capability is needed (likely a separate phase). |
| A4 | A live "kill the tunnel now" channel between API and tunnel server does NOT yet exist (force-disconnect currently can only set `connections.disconnected_at`). ADMIN-04's "kicks every active client within one request" needs either an existing Redis pub-sub kill channel the tunnel subscribes to, OR the tunnel polling a kill signal. | ADMIN-04 / Open Q1 | If no kill channel exists, "force-disconnect" v1 may only mark rows + drop new connects, with live tunnels dying on next heartbeat fail — a weaker guarantee than criterion 4 literally states. MUST verify tunnel capability. |
| A5 | Tunnel heartbeat to populate `vpn_servers.last_seen_at` (needed by readyz/deps-health) is NOT yet emitted by the tunnel; `ADMIN-IMPROVEMENTS.md §2.3` describes adding `POST /internal/servers/:id/heartbeat`. | ADMIN-07/08 | If the tunnel can't be changed this phase, readyz "tunnel healthy" must use a proxy signal (e.g. recent connections per server) instead of a true heartbeat. |
| A6 | Live-KPI refresh = TanStack short-polling (resolved by this research, not by a user decision). | ADMIN-01 | Low — matches existing codebase pattern; if operator wants true-realtime, revisit, but unnecessary for ~1 operator. |

**If A4/A5 are wrong in the operator's favor (a kill channel + heartbeat already exist), great — the plans get simpler. If wrong against us, ADMIN-04/07/08 acquire a tunnel-server sub-task. The planner MUST resolve A4/A5 against the tunnel code before finalizing those plans.**

## Open Questions

1. **Does a live API→tunnel "kill connection" channel exist, and does the tunnel emit heartbeats?** (Blocks ADMIN-04 "kicks every active client within one request" and ADMIN-07/08 tunnel health.)
   - What we know: `server/tunnel` exists with `internal/server.go`, `health.go`, `config.go`; the API has go-redis pub-sub available; `vpn_servers` has no `last_seen_at` yet.
   - What's unclear: whether the tunnel subscribes to any Redis channel for live kills, and whether it already POSTs a heartbeat anywhere.
   - Recommendation: planner reads `server/tunnel/internal/server.go` + `cmd/tunnel/main.go` in full at plan time. If no kill/heartbeat path exists, scope ADMIN-04 v1 to "mark disconnected + drain stops new connects" (live tunnels die on next heartbeat-stale sweep, ~3 min per `STALE_CONNECTION_AFTER`) and add `POST /internal/servers/:id/heartbeat` (shared-secret) + a tunnel-side emitter as an explicit sub-task; readyz tunnel-health uses `last_seen_at` once that lands.

2. **Churn definition** — lava contract cancellations only, or all downgrades? (Drives whether `subscriptions.cancelled_at` is needed — Assumptions A2.)
   - Recommendation: default to `lava_contracts.cancelled_at` count in last 30d (no schema change); confirm with operator during plan-check.

3. **Does `repository.GetGlobalStats` / `GetTimeseries` already exist with the exact signatures the new KPIs extend?** (They're called from `admin.go` — verified the call sites exist; the function bodies in `admin_repo.go` were not fully read due to tool-classifier outages.)
   - Recommendation: planner reads `admin_repo.go` in full to confirm the extension surface before writing the ADMIN-01 plan. Low risk — the call sites prove the functions exist.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Postgres 16 | advisory locks, KPI SQL, all new tables | ✓ (locked stack, compose) | 16 | — (hard requirement; advisory locks are Postgres-native) |
| Redis 7 | flag cache, MRR cache, disconnect throttle, readyz, (pub-sub kill) | ✓ | 7 | flags/readyz fall through to DB; MRR computes live; pub-sub kill has no fallback (Open Q1) |
| Go 1.25 toolchain | backend build/test | ✓ | 1.25 | — |
| Node + Vite | admin-web build | ✓ | per package.json | — |
| lava.top API reachability | readyz "lava" dep + (no refund) | ✓ (config present) | — | readyz caches last-known; never hard-dials per probe |
| Tunnel server modifiability | ADMIN-04 kill + ADMIN-07/08 heartbeat | ✗ UNCONFIRMED | — | weaker disconnect (mark+stale-sweep) if not modifiable this phase |

**Missing dependencies with no fallback:** a live tunnel kill channel + tunnel heartbeat emitter (Open Q1 / A4 / A5) — the planner must resolve by reading tunnel source; this is the single biggest unknown gating ADMIN-04/07/08.
**Missing dependencies with fallback:** none blocking for ADMIN-01/02/03/05/06.

## Validation Architecture

> nyquist_validation is enabled. This section maps each ADMIN success criterion to a test/observation point so a VALIDATION.md can be derived.

### Test Framework
| Property | Value |
|----------|-------|
| Framework (backend) | Go stdlib `testing` + table tests; integration tests under `server/api/internal/**/*_test.go` and `server/api/integration/` (e.g. `webhook_idempotency_test.go`, `lava_sandbox_test.go`, `migrations_test.go`) `[VERIFIED: file listing]` |
| Config file | none — Go `testing` needs none; migrations validated by `migrations/migrations_test.go` |
| Quick run command | `cd server/api && go test ./internal/handler/... ./internal/repository/... -run <Name> -count=1` |
| Full suite command | `cd server/api && go test ./... -count=1` (backend) ; `cd admin-web && npm run build` (type-check + bundle; no unit-test runner is configured — `package.json` has no `test` script) `[VERIFIED: package.json scripts = dev/build/lint/preview only]` |
| Frontend tests | **No test runner configured in admin-web.** Validation for UI is `npm run build` (tsc -b type-check) + manual/observation. Adding vitest is a Wave-0 option but NOT currently present. |

### Phase Requirements → Test Map
| Req | Behavior | Test Type | Automated Command | File Exists? |
|-----|----------|-----------|-------------------|-------------|
| ADMIN-01 | KPI SQL returns correct counts (paid/active-conn/churn/failed) | unit (repo) on seeded DB | `go test ./internal/repository/ -run TestDashboardKPIs -count=1` | ❌ Wave 0 |
| ADMIN-01 | `/admin/stats` response carries the new fields | handler test | `go test ./internal/handler/ -run TestAdminGetStats -count=1` | ❌ Wave 0 (extend admin_test.go) |
| ADMIN-02 | suspend → sessions revoked → suspended user gets 401 | handler+middleware test | `go test ./internal/middleware/ -run TestSuspended -count=1` | ❌ Wave 0 |
| ADMIN-02 | force-grant/reset still works; reason validated | handler test | `go test ./internal/handler/ -run TestAdminUserControls -count=1` | ❌ Wave 0 |
| **ADMIN-03** | **force-cancel + payment.success interleaved → consistent final state (never hybrid); 2nd op sees latest, returns 409 / idempotent** | **integration (concurrent goroutines on real Postgres)** | `go test ./integration/ -run TestForceCancelWebhookRace -count=1` | ❌ Wave 0 — **highest-value test; build first** |
| ADMIN-04 | drain toggle → `cache:servers:active` busted → `GET /servers` (non-admin) omits server | handler+cache test | `go test ./internal/handler/ -run TestServerDrain -count=1` | ❌ Wave 0 |
| ADMIN-04 | force-disconnect-all sets disconnected_at for server_id; kill emitted | repo+(tunnel?) test | `go test ./internal/repository/ -run TestDisconnectByServer -count=1` | ❌ Wave 0 (kill-channel observation manual if Open Q1 unresolved) |
| ADMIN-05 | maintenance ON → non-admin route 503 + Retry-After; admin route 200 | middleware test | `go test ./internal/middleware/ -run TestMaintenance -count=1` | ❌ Wave 0 |
| ADMIN-05 | feature flag signups_off → /auth/guest 503; broadcasts public endpoint returns active msgs | handler test | `go test ./internal/handler/ -run TestFeatureFlags -count=1` | ❌ Wave 0 |
| **ADMIN-06** | replay DELIVERED event re-applies idempotently (no double grant; status→REPLAYED) | integration | `go test ./integration/ -run TestWebhookReplayIdempotent -count=1` | ❌ Wave 0 (extend webhook_idempotency_test.go) |
| ADMIN-07 | `/livez` always 200; `/readyz` 200 all-green, 503 when one dep red | handler test w/ stubbed deps | `go test ./internal/handler/ -run TestReadyzLivez -count=1` | ❌ Wave 0 |
| ADMIN-08 | deps-health admin endpoint returns per-dep status incl. tunnel last_seen | handler test | `go test ./internal/handler/ -run TestDepsHealth -count=1` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** the quick run for the touched package, e.g. `go test ./internal/handler/ -run <Name> -count=1`.
- **Per wave merge:** `cd server/api && go test ./... -count=1` + `cd admin-web && npm run build`.
- **Phase gate:** full backend suite green + admin-web build green before `/gsd-verify-work`; the two integration race/replay tests (ADMIN-03, ADMIN-06) MUST pass — they are the load-bearing correctness proofs.

### Wave 0 Gaps
- [ ] `integration/admin_concurrency_test.go` — ADMIN-03 force-cancel vs webhook race (real Postgres, two goroutines, advisory-lock proof). **Build first — it defines "done" for the hardest requirement.**
- [ ] extend `integration/` webhook test — ADMIN-06 replay idempotency.
- [ ] `internal/middleware/maintenance_test.go`, `suspended_test.go` — new middleware.
- [ ] extend `internal/handler/admin_test.go` — KPI fields, user/server controls, deps-health.
- [ ] `internal/handler/health_test.go` (or extend) — readyz/livez dep matrix.
- [ ] `internal/repository/` tests for new KPI aggregates + `WithUserLock` + disconnect-by-server.
- [ ] (Optional) introduce vitest in `admin-web` for the new pages, OR rely on `npm run build` type-check + manual observation. No runner exists today.
- [ ] Migration test: `migrations_test.go` likely asserts sequential numbering — ensure `024` slots in cleanly.

## Security Domain

> security_enforcement is ON (ASVS L1). Every new admin capability widens the attack surface; threat model per ADMIN-IMPROVEMENTS.md §9. Phase-8 hardening (CSP/headers/search) is OUT OF SCOPE but noted at the boundary.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control (this codebase) |
|---------------|---------|----------------------------------|
| V2 Authentication | yes | Reuse existing `AuthRequired` JWT (HS256) + admin password login; no new auth surface except tunnel-heartbeat shared secret (`crypto/subtle.ConstantTimeCompare`, mirror PAY-07) |
| V3 Session Management | yes | Suspend MUST revoke sessions (`DELETE FROM sessions WHERE user_id=?`) + bust `user:<id>`; existing token blacklist + 5s cache TTL backstop |
| V4 Access Control | yes | All mutating endpoints on the `AdminRequired` group (re-reads role from DB per HOTFIX-02 — no stale-JWT privilege); maintenance middleware MUST exempt admin routes (Pitfall 5); deps-health is admin-only (don't leak infra topology publicly) |
| V5 Input Validation | yes | Validate `reason` (non-empty/trimmed), `refund` bool, broadcast severity enum, currency enum; reuse `allowedCurrencies`/`allowedPeriodicities` map pattern; zod on the frontend forms |
| V6 Cryptography | yes | Tunnel-heartbeat secret compare via `crypto/subtle.ConstantTimeCompare` (existing lava webhook precedent); never hand-roll |
| V7 Error Handling/Logging | yes | Existing `ErrorHandler` scrubs 5xx; new endpoints inherit it; redact buyer emails in webhook-log row preview (`§9.4`: `a***@example.com`, full only on detail) |
| V11 Business Logic | yes | The advisory lock IS a business-logic integrity control (ADMIN-03); replay idempotency prevents double-grant abuse |

### Known Threat Patterns for {Go/Fiber admin + payment webhooks}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Privilege escalation via stale admin JWT | Elevation | `AdminRequired` re-reads role from DB every request (HOTFIX-02, already done) — keep new endpoints on that group |
| Maintenance-mode bypass / admin lockout | DoS / Elevation | middleware exempts admin + auth + liveness routes; flag from DB source-of-truth (Pitfall 5) |
| Webhook replay abuse → double tier grant | Tampering | set-not-increment tier grant + idempotent `applyLavaEvent` + `WithUserLock`; status→REPLAYED audited |
| Advisory-lock exhaustion / DoS | DoS | xact-scoped locks auto-release on commit/rollback; force-disconnect throttle (≤1/server/60s) via existing `IncrRateLimit`; tight transaction bodies |
| CSRF on state-changing admin POSTs | Tampering | admin SPA uses bearer-token axios (not cookies) → not CSRF-exploitable; if any cookie auth added later, enforce SameSite/Origin (Phase-8 territory) |
| Buyer PII exposure in webhook log | Info Disclosure | redact emails in list view; full-email view is itself audited (`§9.4`) |
| Force-disconnect blast radius | DoS (self-inflicted) | per-server/per-user throttle + confirm dialog echoing target (`§9.5`) |
| Tunnel-heartbeat spoofing | Spoofing | shared-secret header constant-time compare; endpoint NOT on public/admin group; documented as the one intentionally non-audited endpoint (`§9.2`) |
| reason-field injection into audit JSONB | Tampering | trim/length-cap reason; it's stored as JSONB value (parameterized via GORM), not concatenated SQL |

**Phase-8 boundary (note, do NOT implement):** HSTS/CSP/X-Content-Type-Options on the admin group (HARD-08), admin search `len>=3` + prefix-only (HARD-06), admin role-change before→after diff (HARD-07). The planner should leave hooks/comments but not build these.

## Sources

### Primary (HIGH confidence — read this session)
- `server/api/internal/handler/admin.go` — existing admin endpoints, DTOs, cache-bust pattern, `adminUpdateServerRequest`
- `server/api/internal/handler/webhook_lava.go`, `payment.go`, `health.go` — webhook dispatch, cancel flow, current shallow health
- `server/api/internal/middleware/{auth.go,admin.go,audit.go}` — AuthRequired (user cache), AdminRequired (DB role re-read), AuditLog (records query+params not body)
- `server/api/internal/repository/{user_repo.go,server_repo.go,connection_repo.go,webhook_event_repo.go,db.go}` — repo conventions, `ListActiveServers` filter, ctx propagation
- `server/api/internal/cache/{redis.go,servers_cache.go,user_cache.go}` — bust helpers, atomic rate-limit Lua, two-layer cache pattern
- `server/api/internal/model/{user.go,server.go,subscription.go,lava_webhook_event.go,audit.go}` — schema; `lava_webhook_events` has no `status` yet; `lava_contracts.cancelled_at` exists
- `server/api/internal/config/config.go` — `RequireEnv` aggregate validator, `RunScheduler`, lava env
- `server/api/internal/scheduler/scheduler.go` — RUN_SCHEDULER gate, modulo-tick registry, 10s heartbeat flush
- `server/api/migrations/` (full listing + 013/020/021/022/023 read) — highest = 023; next = 024; single-file `.sql` convention; `migrations_test.go` present
- `admin-web/package.json`, `src/App.tsx`, `src/pages/Dashboard.tsx`, `src/api/client.ts`, `src/components/layout/AdminLayout.tsx` — stack versions, routing, axios+TanStack patterns, `refetchInterval` precedent
- `docs/audit/ADMIN-IMPROVEMENTS.md` (full) — the PRD: §2 KPIs, §3 controls, §4 reliability, §6 endpoint contracts, §7 phasing, §8 advisory locks, §9 risks
- `docs/ADR-007-lava-sso-rework.md`, `.planning/REQUIREMENTS.md`, `.planning/STATE.md`, `CLAUDE.md`, `.planning/config.json`

### Secondary (MEDIUM — read but not exhaustively)
- `server/tunnel/internal/server.go` (read) — tunnel structure; kill-channel/heartbeat capability NOT confirmed (Open Q1)
- `admin_repo.go`, `subscription_repo.go`, `expiry_repo.go`, `lava/client.go` — partial visibility due to tool-classifier outages; call sites confirm the functions exist

### Tertiary (LOW — inferred, flagged in Assumptions)
- Exact churn definition (A2), refund capability (A3), tunnel kill/heartbeat presence (A4/A5) — all flagged for planner resolution.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — verified against `go.mod`/`package.json`; no new deps needed.
- Architecture (advisory lock, replay refactor, polling, cache-bust): HIGH — every pattern grounded in a named existing file.
- ADMIN-01/02/03/05/06: HIGH — additive against confirmed surfaces.
- ADMIN-04/07/08 (tunnel kill + heartbeat): MEDIUM — depends on unconfirmed tunnel capability (Open Q1 / A4 / A5); planner must read tunnel source.
- Pitfalls: HIGH — derived from the actual cache/audit/migration mechanics observed.

**Research date:** 2026-05-30
**Valid until:** 2026-06-29 (stable internal codebase; ~30 days). Re-verify only if Phase 8 lands before Phase 7 or the tunnel server is modified.
