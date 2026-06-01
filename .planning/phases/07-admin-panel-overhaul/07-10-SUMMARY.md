---
phase: 07-admin-panel-overhaul
plan: 10
subsystem: ui
tags: [admin-web, react, tanstack-query, shadcn, vite, kpi, webhooks, feature-flags, broadcasts]

requires:
  - phase: 07-admin-panel-overhaul (02-09)
    provides: "admin KPI/stats, per-user controls, server drain/disconnect, system controls (deps-health/flags/broadcasts), webhook log + replay endpoints"
provides:
  - "Dashboard KPI bar wired to GET /admin/stats (8 new KPIs) with a live 15s active-connections card"
  - "UserDetail per-user controls: suspend/unsuspend, force-cancel (refund+reason), force-disconnect, + audit/sessions/connections history tabs"
  - "Servers drain/undrain toggle, is_draining badge, force-disconnect-all confirm echoing hostname"
  - "System page: deps-health (15s poll) + feature-flag Switch toggles (maintenance_mode 503 warning) + broadcast CRUD"
  - "Payments page: webhook-event log (redacted preview) + payload detail modal + idempotent replay"
  - "admin-web /payments + /system routes and nav items"
affects: [admin-web, phase-08]

tech-stack:
  added: []
  patterns:
    - "One api/*.ts file per resource (system.ts, webhooks.ts); types in lockstep with Go JSON tags"
    - "TanStack useQuery/useMutation + sonner toasts; query invalidation on mutation success"
    - "Destructive actions gated behind a confirm Dialog echoing the target identifier (T-07-41)"
    - "Separate fast query key for the most-volatile KPI (active_connections @ 15s) without lowering the global 60s cadence"

key-files:
  created:
    - admin-web/src/api/system.ts
    - admin-web/src/api/webhooks.ts
    - admin-web/src/pages/System.tsx
    - admin-web/src/pages/Payments.tsx
  modified:
    - admin-web/src/api/stats.ts
    - admin-web/src/api/users.ts
    - admin-web/src/api/servers.ts
    - admin-web/src/pages/Dashboard.tsx
    - admin-web/src/pages/Servers.tsx
    - admin-web/src/pages/UserDetail.tsx
    - admin-web/src/App.tsx
    - admin-web/src/components/layout/AdminLayout.tsx

key-decisions:
  - "Reused the existing api/connections.ts listUserConnections instead of adding a duplicate getUserConnections to users.ts"
  - "disconnectServer / undrainServer send empty bodies and take no reason — matched the actual Go handlers (no reason param), not the plan's loose signature"
  - "active_connections rendered from its own 15s query key; global stats stay at 60s"
  - "MRR formatted as a USD currency figure (backend defaults MRR currency to USD)"

patterns-established:
  - "Reason-carrying control dialogs share a ReasonField; submit disabled until a non-whitespace reason is entered (backend 400s on empty)"
  - "409/429 backend statuses mapped to friendly localized toasts in a shared toastError helper per page"

requirements-completed: [ADMIN-01, ADMIN-02, ADMIN-04, ADMIN-05, ADMIN-06, ADMIN-08]

duration: 8min
completed: 2026-06-01
---

# Phase 7 Plan 10: Admin-web UI for every Phase-7 backend capability Summary

**Wired the admin-web SPA to every Phase-7 backend endpoint: a live KPI dashboard, per-user suspend/force-cancel/force-disconnect controls with history tabs, server drain/disconnect, a System page (deps-health + feature flags + maintenance + broadcasts), and a webhook log with idempotent replay — `npm run build` clean.**

## Performance

- **Duration:** 8 min
- **Started:** 2026-06-01T12:55:09Z
- **Completed:** 2026-06-01T13:03:36Z
- **Tasks:** 3 implementation tasks (Task 4 is a human-UAT checkpoint, deferred to the operator)
- **Files modified:** 13 (2 new api + 3 extended api, 2 new pages + 3 revamped pages, routes + nav)

## Accomplishments
- **Dashboard KPI bar (ADMIN-01):** 8 new KPI cards (paid users, MRR as USD, signups today/week/month, churn 30d, failed payments 30d) on the existing 60s query; a dedicated live active-connections card on its own 15s query key.
- **Per-user controls (ADMIN-02):** suspend/unsuspend, force-cancel Pro (refund checkbox + reason), force-disconnect — each a reason-carrying dialog echoing the user_id; 409 (already cancelled) and 429 (throttle) surfaced as toasts. Added a History card with Tabs for audit log, active sessions, and connections.
- **Server controls (ADMIN-04):** per-row drain/undrain toggle, is_draining badge, force-disconnect-all behind a confirm dialog echoing the hostname; 429 throttle toast.
- **System controls (ADMIN-05/08):** deps-health tab polling every 15s (postgres/redis/lava status badges + tunnel-server freshness/load table); feature-flag Switch toggles with a maintenance_mode "это вернёт 503 всем не-админам" confirm; full broadcast CRUD (create/edit form with severity Select + optional locale/tier/time window, delete confirm).
- **Webhook log + replay (ADMIN-06):** Payments page lists events (status filter, redacted email preview), row click opens a full unredacted payload modal, Replay on DELIVERED/FAILED rows with a required reason → invalidates the webhook-events query so status flips to REPLAYED and retried_count increments.
- Routes `/payments` + `/system` (lazy) and nav items Платежи (CreditCard) + Система (HeartPulse) added.

## Task Commits

1. **Task 1: API client files (system, webhooks) + extend users/servers/stats** - `26b5421` (feat)
2. **Task 2: Dashboard KPI bar + Servers drain/disconnect + UserDetail controls** - `a7b6282` (feat)
3. **Task 3: System page + Payments page + routes + nav** - `da6081d` (feat)

## Files Created/Modified
- `admin-web/src/api/system.ts` (new) - deps-health, feature-flags, broadcasts CRUD typed clients
- `admin-web/src/api/webhooks.ts` (new) - list/get/replay webhook-events; redacted preview + detail types
- `admin-web/src/api/stats.ts` - AdminStats extended with 8 KPI fields
- `admin-web/src/api/users.ts` - suspend/unsuspend/disconnect/cancelSubscription + audit-log/sessions
- `admin-web/src/api/servers.ts` - drain/undrain/disconnect/getServerHealth + is_draining/last_seen_at fields
- `admin-web/src/pages/Dashboard.tsx` - KPI bar + live active-connections card
- `admin-web/src/pages/Servers.tsx` - drain/disconnect controls + is_draining badge
- `admin-web/src/pages/UserDetail.tsx` - control dialogs + history tabs
- `admin-web/src/pages/System.tsx` (new) - deps-health + flags + broadcasts tabs
- `admin-web/src/pages/Payments.tsx` (new) - webhook log + payload modal + replay
- `admin-web/src/App.tsx` - /payments + /system lazy routes
- `admin-web/src/components/layout/AdminLayout.tsx` - nav items

## Decisions Made
- Reused the existing `api/connections.ts` `listUserConnections` rather than adding a duplicate `getUserConnections` to users.ts (the plan listed it, but the resource file already exists and is wired in ConnectionsSection).
- `disconnectServer` and `undrainServer` send empty bodies and take NO reason param — the actual Go handlers (`AdminDisconnectServer`, `AdminUndrainServer`) accept no body, contrary to the plan's loose `disconnectServer(id, reason)` signature. Matched the handlers (the plan explicitly required cross-checking shapes against the Go code).
- MRR rendered as a USD currency figure since the backend defaults the MRR currency to USD.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Installed admin-web node_modules**
- **Found during:** Task 1 (tsc gate)
- **Issue:** `admin-web/node_modules` was absent in the worktree, so neither `npx tsc` nor `npm run build` could run.
- **Fix:** Ran `npm install` in admin-web (323 packages, already lockfile-pinned). node_modules is gitignored — no commit.
- **Files modified:** none committed (node_modules gitignored)
- **Verification:** `./node_modules/.bin/tsc -b` then `npm run build` ran.
- **Committed in:** n/a

**2. [Rule 1 - Bug] Removed unused CardHeader/CardTitle imports in Payments.tsx**
- **Found during:** Task 3 (npm run build)
- **Issue:** tsc TS6133 — `CardHeader`/`CardTitle` imported but unused after the page used a header outside the Card; build failed.
- **Fix:** Narrowed the import to `{ Card, CardContent }`.
- **Files modified:** admin-web/src/pages/Payments.tsx
- **Verification:** `npm run build` clean afterward.
- **Committed in:** da6081d (Task 3 commit)

---

**Total deviations:** 2 auto-fixed (1 blocking, 1 bug)
**Impact on plan:** Both necessary to make the build gate pass. No scope creep — the UI matches the plan and the real backend shapes.

## Threat Flags
None — the SPA only consumes endpoints shipped in Waves 1-8; no new network surface, auth path, or trust boundary introduced. Destructive actions are confirm-gated and echo the target (T-07-41); webhook list emails stay server-side-redacted (T-07-43); the maintenance toggle carries the lock-out warning (T-07-42).

## Issues Encountered
None beyond the two auto-fixed deviations above.

## Known Stubs
None — every page is wired to a real Phase-7 endpoint via TanStack queries. The webhook detail `payload` is typed `unknown` (opaque JSONB by design, rendered via `JSON.stringify`), not a stub.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- All six UI-bearing ADMIN requirements (01, 02, 04, 05, 06, 08) have working pages wired to the backend; `npm run build` passes clean.
- **Blocking checkpoint outstanding:** Task 4 is a human-UAT walkthrough (autonomous:false). It requires a live API with migration 024 applied and at least one tunnel posting heartbeats. The orchestrator should present the six-step walkthrough (see PLAN Task 4 / the checkpoint return) to the operator before marking the plan/phase complete.

## Self-Check: PASSED

All key-files exist on disk (system.ts, webhooks.ts, System.tsx, Payments.tsx, SUMMARY.md) and all three task commits (26b5421, a7b6282, da6081d) are present in git history. `npm run build` (tsc -b + Vite bundle) passes clean.

---
*Phase: 07-admin-panel-overhaul*
*Completed: 2026-06-01*
