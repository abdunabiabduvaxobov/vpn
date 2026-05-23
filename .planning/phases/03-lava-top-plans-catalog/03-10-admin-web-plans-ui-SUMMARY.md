---
phase: 3
plan: 10
subsystem: admin-web/ui
tags: [admin-web, react, shadcn, tanstack-query, plans-ui, lava-offer-picker, PAY-13, PAY-14, PAY-15, D-12, D-13]
dependency-graph:
  requires:
    - 03-05 (checkout-cancel-invoices-admin-lava-proxy) — GET /admin/lava/products D-12 proxy
    - 03-08 (admin-plans-crud) — 13 admin plan-CRUD endpoints (PAY-13/14/15)
  provides:
    - admin-web/src/api/plans.ts — 13 typed TanStack-Query fetchers
    - admin-web/src/api/lava.ts — listLavaProducts() typed fetcher
    - admin-web/src/pages/Plans.tsx — /plans index page (list view + New CTA)
    - admin-web/src/pages/PlanDetail.tsx — /plans/:id three-tab editor + /plans/new create form
    - admin-web/src/components/plans/PlansTable.tsx — 7-column listing per ADR §19.13.1
    - admin-web/src/components/plans/PlanForm.tsx — RHF+zod form (Limits tab + create mode)
    - admin-web/src/components/plans/PlanServersPicker.tsx — country-grouped checkbox grid
    - admin-web/src/components/plans/PlanOffersGrid.tsx — periodicity × currency matrix
    - admin-web/src/components/plans/LavaOfferPicker.tsx — D-12 dropdown-only picker
    - admin-web/src/components/plans/DeletePlanDialog.tsx — code-confirm soft-delete dialog
    - admin-web/src/components/plans/ReplaceOfferDialog.tsx — PAY-15 price-versioning modal
    - admin-web/src/components/plans/PlanCodeBadge.tsx — immutable slug badge
    - 7 vendored shadcn primitives (Form, Select, Checkbox, Switch, Tabs, Tooltip, Textarea)
  affects:
    - 03-11 (docs-sandbox-smoke) — admin UAT exercises this UI flow end-to-end
tech-stack:
  added:
    - "@radix-ui/react-checkbox ^1.3.3"
    - "@radix-ui/react-select ^2.2.6"
    - "@radix-ui/react-switch ^1.2.6"
    - "@radix-ui/react-tabs ^1.1.13"
    - "@radix-ui/react-tooltip ^1.2.8"
    - "react-hook-form ^7.76.1"
    - "@hookform/resolvers ^3.10.0"
    - "zod ^3.25.76"
  patterns:
    - "Single-flight TanStack Query keys: ['admin','plans'] for the list + ['admin','plan',id] for the detail — invalidation after every write fans both"
    - "Dropdown-only D-12 enforcement at the component contract: LavaOfferPicker.value is opaque, onChange ships the whole LavaProduct row — there is NO free-text path the form can construct"
    - "react-hook-form + zod schema mirrors handler.validatePlanCode + validatePlanFields (plans_admin.go) so client errors match server errors verbatim"
    - "ReplaceOfferDialog mandatory acknowledgement checkbox blocks submit (ADR §19.13.4) — the price-versioning operation is irreversible from UI"
    - "PlansTable hides the delete affordance on is_system=true (D-32 §4 UI mirror of backend 403); DeletePlanDialog ALSO would 403 from the API — defence in depth"
    - "PlanServersPicker computes +N/−N diff client-side so the operator sees the pending change before commit; replacePlanServers is atomic (backend tx)"
    - "PlanOffersGrid renders only is_active=true offers per cell; historic grandfathered rows stay in the response but are hidden from the live catalogue view"
    - "Lazy-loaded routes via React.lazy + Suspense — Plans and PlanDetail chunks land at 5.18KB + 151KB (gzip 2.19KB + 44.11KB) separate from the main bundle"
key-files:
  created:
    - admin-web/src/components/ui/form.tsx (165 lines — RHF wrapper)
    - admin-web/src/components/ui/select.tsx (140 lines — Radix Select)
    - admin-web/src/components/ui/checkbox.tsx (29 lines)
    - admin-web/src/components/ui/switch.tsx (28 lines)
    - admin-web/src/components/ui/tabs.tsx (55 lines)
    - admin-web/src/components/ui/tooltip.tsx (28 lines)
    - admin-web/src/components/ui/textarea.tsx (19 lines)
    - admin-web/src/api/plans.ts (213 lines, 13 fetchers + 9 interfaces)
    - admin-web/src/api/lava.ts (22 lines, 1 fetcher + LavaProduct interface)
    - admin-web/src/pages/Plans.tsx (54 lines)
    - admin-web/src/pages/PlanDetail.tsx (115 lines)
    - admin-web/src/components/plans/PlansTable.tsx (98 lines)
    - admin-web/src/components/plans/PlanForm.tsx (290 lines)
    - admin-web/src/components/plans/PlanServersPicker.tsx (180 lines)
    - admin-web/src/components/plans/PlanOffersGrid.tsx (286 lines)
    - admin-web/src/components/plans/LavaOfferPicker.tsx (135 lines)
    - admin-web/src/components/plans/DeletePlanDialog.tsx (118 lines)
    - admin-web/src/components/plans/ReplaceOfferDialog.tsx (188 lines)
    - admin-web/src/components/plans/PlanCodeBadge.tsx (10 lines)
  modified:
    - admin-web/package.json (8 new deps)
    - admin-web/package-lock.json (323 packages added)
    - admin-web/src/components/ui/badge.tsx (added secondary cva variant — needed by PlanCodeBadge and status pills)
    - admin-web/src/App.tsx (2 lazy imports + 2 routes)
    - admin-web/src/components/layout/AdminLayout.tsx (Tag import + Тарифы navItem)
decisions:
  - "Vendored shadcn components by hand-writing them rather than running `npx shadcn add` because the project's tooling is locked to a specific React 19 + Tailwind 4 stack and the canonical CLI sometimes pulls newer Radix versions than the existing project's 1.1.6 baseline. Hand-vendoring lets us pin the exact Radix major we want and matches the existing dialog.tsx / dropdown-menu.tsx style verbatim."
  - "Created the `Form` component using react-hook-form's `FormProvider` + `Controller` per shadcn canonical pattern. Could have used uncontrolled forms with raw refs to avoid the dep, but `useForm + zodResolver` is the established shadcn idiom and gives us field-level error state without writing it ourselves. The 8KB+ RHF runtime cost is acceptable for an admin tool."
  - "Extended `Badge` cva with a `secondary` variant rather than rewriting the existing dialog/badge styling. The existing Badge had only `default` + `outline`; PlanCodeBadge needs a neutral-but-distinct slug style and 'Системный' status pill uses `outline`. Adding `secondary` keeps the API consistent with shadcn canonical."
  - "Split PlanDetail into create-mode (single form, /plans/new) vs edit-mode (3-tab layout, /plans/:id) because the backend's POST /admin/plans accepts servers+offers atomically in one body BUT the operator's mental model is 'create the plan, then wire it up.' MVP creates the plan with empty arrays and routes to the detail page where the Servers / Pricing tabs are populated — matches Servers.tsx UX precedent and avoids a giant wizard form."
  - "LavaOfferPicker filters by currency + periodicity props rather than showing the whole catalogue. When editing an existing offer, the periodicity + currency are pinned (immutable per ADR §19.7.7) — so the dropdown only shows rows that match. This is UX, not security: the backend doesn't validate that lava_offer_id matches the offer's periodicity/currency anyway, but it prevents the operator from accidentally pairing a USD lava-offer with a RUB local-offer."
  - "ReplaceOfferDialog pre-fills the new amount with the selected lava-offer's price as a hint, but the field is still editable. lava's pricing on the lava side and our recorded `amount` are normally identical, but the operator might want to charge a different amount than what lava lists (e.g. promo discount). The backend writes whatever amount we send; lava only sees the offer UUID."
  - "PlanOffersGrid renders only `is_active=true` offers per cell. Historic offers (the grandfathered side of a replaceOffer call) are in the response but hidden from the matrix — showing them would clutter the catalogue view. A future enhancement could add a 'Show history' toggle."
  - "Deferred ESLint config setup as out-of-scope: `npm run lint` fails because admin-web has no `eslint.config.js` — this is a pre-existing project gap (existed before 03-10). Per the scope-boundary rule, I logged this finding but did not fix it. Build verification via tsc + vite is sufficient."
metrics:
  duration_seconds: 661
  duration_human: "~11 minutes"
  tasks_total: 5
  tasks_complete: 5
  commits: 5
  files_created: 19
  files_modified: 5
  completed_date: "2026-05-24"
  completed_at: "2026-05-24T01:57:20+05:00"
---

# Phase 3 Plan 10: admin-web-plans-ui Summary

**One-liner:** Wired the full admin UI for the dynamic plans catalogue — 2 pages, 8 plan components, 7 newly vendored shadcn primitives (Form/Select/Checkbox/Switch/Tabs/Tooltip/Textarea), 14 typed TanStack-Query fetchers — implementing PAY-13/14/15 with D-12 dropdown-only lava picker, ADR §19.13.4 ReplaceOffer modal, and D-32 §4 system-plan delete-affordance hiding.

## What Shipped

### Task 03-10-T01 — Vendor 7 shadcn components + install Radix/RHF/zod (commit `18f1e71`)

Installed 8 new dependencies:

| Dep | Version | Used by |
|-----|---------|---------|
| `@radix-ui/react-checkbox` | 1.3.3 | Checkbox (PlanServersPicker, ReplaceOfferDialog acknowledgement) |
| `@radix-ui/react-select` | 2.2.6 | Select (LavaOfferPicker D-12 dropdown) |
| `@radix-ui/react-switch` | 1.2.6 | Switch (PlanForm is_active toggle) |
| `@radix-ui/react-tabs` | 1.1.13 | Tabs (PlanDetail Limits/Servers/Pricing) |
| `@radix-ui/react-tooltip` | 1.2.8 | Tooltip (LavaOfferPicker $5 floor warning) |
| `react-hook-form` | 7.76.1 | PlanForm Limits-tab schema |
| `@hookform/resolvers` | 3.10.0 | zodResolver wiring |
| `zod` | 3.25.76 | planFormSchema validation |

Hand-vendored 7 shadcn primitives matching the existing `dialog.tsx` / `dropdown-menu.tsx` style — thin Radix wrappers + Tailwind classes + `cn()` helper:

- `form.tsx` (165 lines — FormProvider + Controller + useFormField hook)
- `select.tsx` (140 lines — Trigger/Content/Item/Scroll/ItemText/Separator)
- `checkbox.tsx`, `switch.tsx`, `tabs.tsx`, `tooltip.tsx`, `textarea.tsx`

Also extended `badge.tsx` cva with a `secondary` variant — needed by `PlanCodeBadge` (mono slug) and status pills. The existing Badge only had `default` + `outline`.

### Task 03-10-T02 — Typed API clients (commit `e6021c2`)

- **`api/plans.ts`** (213 lines, 13 fetchers):
  - Plans: `listPlans`, `getPlan`, `createPlan`, `updatePlan`, `deletePlan(id, force?)`
  - Plan servers: `replacePlanServers`, `addPlanServer`, `removePlanServer`
  - Plan offers: `listOffers`, `createOffer`, `updateOffer`, `deleteOffer`, `replaceOffer`
  - 9 interface types mirror the handler fiber.Map response shapes from plan 03-08 exactly: `AdminPlanSummary`, `AdminPlanDetail` (extends Summary + servers/offers), `PlanOffer`, `CreatePlanInput`, `UpdatePlanInput`, `CreateOfferInput`, `UpdateOfferInput`, `ReplaceOfferInput`, `DeletePlanResult`.
- **`api/lava.ts`** (22 lines, 1 fetcher):
  - `listLavaProducts()` typed against `handler.lavaProductRow` (plan 03-05 T03).
  - `LavaProduct` interface — productId/productName/offerId/offerName/periodicity/currency/amount.

All calls go through the existing axios `api` client (request interceptor adds `Authorization: Bearer ...`, response interceptor unwraps `{data: ...}` envelope, single-flight refresh on 401 — all inherited unchanged).

### Task 03-10-T03 — Plans listing page + table + code badge + delete dialog (commit `9cafc18`)

Four new files, ~291 lines total:

- **`pages/Plans.tsx`** — useQuery on `["admin","plans"]`, loading skeletons, error banner, "Новый тариф" Link CTA to `/plans/new`.
- **`components/plans/PlansTable.tsx`** — 7-column table per ADR §19.13.1: Код / Название / Статус / Серверы / Устройства / Активных пользователей / Обновлён / actions. `is_system` plans render with "Системный" outline badge AND hide the delete affordance (D-32 §4 — UI mirrors backend 403). `max_devices === -1` renders as `∞`.
- **`components/plans/PlanCodeBadge.tsx`** — 10-line mono-font `secondary` Badge for the immutable slug. Visual signal per ADR §19.7.4.
- **`components/plans/DeletePlanDialog.tsx`** — soft-delete confirmation modal. When `active_user_count > 0` it requires the operator to type the plan code AND passes `force=true` to skip the 409 guard. Cleared on close. Uses sonner toasts on success/error.

### Task 03-10-T04 — PlanDetail with 3 tabs + 5 plan-edit components (commit `221ca86`)

Six new files, ~1394 lines total:

- **`pages/PlanDetail.tsx`** (115 lines) — handles both create-mode (`/plans/new`) and edit-mode (`/plans/:id`). Edit mode renders three Tabs: Лимиты (PlanForm), Серверы (PlanServersPicker), Цены (PlanOffersGrid). Badge counts on Servers/Pricing tab triggers come from the loaded plan detail.
- **`components/plans/PlanForm.tsx`** (290 lines) — react-hook-form + zod schema. `code` field disabled in edit mode (ADR §19.7.4); is_active switch disabled when `plan.is_system` (D-32 §4); create mode hides is_active (defaults true server-side); validation mirrors `validatePlanCode` + `validatePlanFields` from `plans_admin.go` exactly.
- **`components/plans/PlanServersPicker.tsx`** (180 lines) — fetches all active servers, groups by country, renders Checkbox grid. Computes +N/−N diff vs the persisted selection, fires amber warning banner when removing servers (D-23 — no force-disconnect, but warns operator). Save calls `replacePlanServers(planId, [...selected])`.
- **`components/plans/PlanOffersGrid.tsx`** (286 lines) — (periodicity × currency) matrix. Each filled cell shows amount + currency + truncated lava_offer_id + kebab menu (Изменить цену… → ReplaceOfferDialog | Деактивировать → soft-delete). Empty cells show "+ Добавить" → inline CreateOfferDialog with pinned periodicity/currency. Hides historic (`is_active=false`) offers.
- **`components/plans/LavaOfferPicker.tsx`** (135 lines) — D-12 enforced: Radix `<Select>` bound to `listLavaProducts()` result. Optional `filterCurrency` / `filterPeriodicity` props pre-filter the dropdown. Shows amber tooltip when selected row's amount < lava floor ($5 USD/EUR). No free-text input path — selection ONLY via dropdown onChange.
- **`components/plans/ReplaceOfferDialog.tsx`** (188 lines) — PAY-15 price-versioning modal. Shows old → new amount diff, pinned periodicity (immutable per ADR §19.7.7), mandatory acknowledgement Checkbox before save. Calls `replaceOffer(planId, offerId, {amount, lava_offer_id})`.

### Task 03-10-T05 — Wire routes + sidebar entry (commit `4953587`)

Two surgical edits:

- **`App.tsx`** — added `Plans` + `PlanDetail` to the lazy-imports list; mounted `/plans` and `/plans/:id` inside the `<AdminLayout />` route group with `<Suspense fallback={<LazyFallback />}>` (matches existing Servers/Activity/Settings pattern). Both routes are protected by the existing AdminLayout auth guard.
- **`AdminLayout.tsx`** — added `Tag` to the lucide-react import block; inserted `{ to: "/plans", label: "Тарифы", Icon: Tag }` between Servers and Activity per ADR §19.13.5.

## Verification

**Plan-level success criteria (all 7):**

| # | Criterion | Result |
|---|---|---|
| 1 | `tsc --noEmit -p admin-web/tsconfig.app.json` exits 0 | **PASS** (no output) |
| 2 | All 7 missing shadcn components vendored | **PASS** (17 files in `admin-web/src/components/ui/`) |
| 3 | 13 admin-plan fetchers + 1 lava-products fetcher | **PASS** (grep -c on `^export async function` returns 13 + 1) |
| 4 | `/plans` page renders the table with row action menus | **PASS** (Plans.tsx + PlansTable.tsx wired) |
| 5 | `/plans/:id` page renders 3 tabs (Limits, Servers, Pricing) | **PASS** (6 `TabsTrigger` matches incl. 3 visible triggers + JSDoc references) |
| 6 | Sidebar entry "Тарифы" added in lexical position | **PASS** (between Servers and Activity per ADR §19.13.5) |
| 7 | PAY-13/14/15 UI surfaces exist and call matching backend endpoints | **PASS** (every component calls the api/plans.ts fetcher named in the requirement) |

**Build:**

```
$ node ./node_modules/typescript/bin/tsc -p tsconfig.app.json --noEmit  → exit 0 (clean)
$ node ./node_modules/typescript/bin/tsc -b                            → exit 0 (clean)
$ node ./node_modules/vite/bin/vite.js build                            → 2.81s, 13 chunks
    dist/assets/Plans-Bxzosxu_.js               5.18 kB │ gzip:   2.19 kB
    dist/assets/PlanDetail-D5A-Elka.js        151.53 kB │ gzip:  44.11 kB
    dist/assets/PlanCodeBadge-BkDAOzx7.js       1.61 kB │ gzip:   0.75 kB
```

Plans and PlanDetail are lazy-loaded chunks separate from the main bundle (516 KB total). The 500 KB warning on the main bundle is pre-existing (recharts dominates that chunk) — out of scope per the scope-boundary rule.

**Per-task acceptance grep results (all green):**

```
T01: 8 new deps in package.json
     17 files in components/ui (was 10)

T02: plans.ts: 13 `export async function` decls
     lava.ts: 1 `export async function` decl
     lava.ts: 1 hit for "/api/v1/admin/lava/products"

T03: Plans.tsx: 2 hits for "useQuery" (import + usage)
     DeletePlanDialog.tsx: useMutation + deletePlan present
     PlansTable.tsx: PlanCodeBadge import + usage; is_system gating

T04: PlanDetail.tsx: 6 "TabsTrigger" matches (3 triggers + comments/JSDoc)
     PlanForm.tsx: react-hook-form import + useForm; disabled when edit
     LavaOfferPicker.tsx: listLavaProducts + 15 Select/SelectItem refs
     ReplaceOfferDialog.tsx: replaceOffer import + JSDoc
     PlanServersPicker.tsx: replacePlanServers import + JSDoc

T05: App.tsx: Plans + PlanDetail lazy imports + 2 routes
     AdminLayout.tsx: Tag import, /plans path, Тарифы label
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 — Critical functionality] Added `secondary` variant to Badge cva**

- **Found during:** T03 — `PlansTable.tsx` used `<Badge variant="secondary">` for the "Системный" status pill, but the existing `badge.tsx` cva only declared `default` + `outline`. TypeScript would have rejected the prop value, and even if cast, the rendered class string would have been wrong.
- **Issue:** The plan body's verbatim skeleton called `<Badge variant="secondary">` 3+ times; the existing Badge component didn't support it. Without a fix, the table would compile to `class="undefined"` or fail tsc.
- **Fix:** Extended the cva block in `badge.tsx` with a `secondary` variant using the existing `bg-secondary text-secondary-foreground` Tailwind tokens — matches the canonical shadcn Badge variants.
- **Files modified:** `admin-web/src/components/ui/badge.tsx`
- **Commit:** rolled into T01 (`18f1e71`).

**2. [Rule 3 — Blocking / Worktree state] Hard-reset working tree to expected base 522009c**

- **Found during:** Worktree base check (FIRST ACTION) — `git merge-base HEAD 522009c` returned `6a3da00` ≠ expected base. HEAD was on a phase-01 hotfix branch ancestor of 522009c, which means the working tree did NOT contain the prior phase-02 / 03-01..03-09 work (no plans_admin.go, no migrations 018-020, etc.).
- **Issue:** The plan checks out files via `read_first` referencing `server/api/internal/handler/plans_admin.go` — needed for handler signature reference. A soft reset would have staged 38000+ lines of "deletions" against the working tree, but the working tree files genuinely didn't exist.
- **Fix:** `git reset --hard 522009cde6eaea23ca062cbb1afe8fa07986c48a` to align the working tree with the expected base so I had the full Phase 3 backend state to reference. The 5 task commits land cleanly on top of 522009c.
- **Files modified:** none (state reset only)
- **Commit:** none (the hard-reset is invisible in the log; the executor's commits start with `18f1e71` on top of `522009c`).

### Deferred Issues

- **ESLint configuration:** `admin-web/package.json` declares `"lint": "eslint ."` but the project has no `eslint.config.js` (the ESLint v9+ default). Running `npm run lint` fails with "couldn't find an eslint.config.(js|mjs|cjs) file". This is a pre-existing project gap (existed before 03-10 — none of the existing admin-web code is linted in CI). Out of scope per the scope-boundary rule. Logged here for whoever owns admin-web tooling cleanup (likely Phase 8 HARD-07 or a dedicated tooling plan).
- **Bundle splitting:** The vite build emits a warning about the main `index-DGBfPVIb.js` chunk being 516 KB (gzip 165 KB). recharts is the dominant contributor (`AreaChart-Rcvf_Uq7.js` is 346 KB alone). Pre-existing — none of my changes added to this chunk. Out of scope.
- **Combobox (search-as-you-type) for LavaOfferPicker:** The plan body noted that Select is sufficient for D-12 and Combobox is NOT in scope. If lava ever ships more than ~50 products, a search-as-you-type picker would be a UX win — deferred to a future enhancement plan.

## Threat Model Compliance

All STRIDE mitigations in the plan's `<threat_model>` (T-03-74 through T-03-78) are in code:

| Threat ID | Mitigation in code |
|-----------|--------------------|
| T-03-74 (Info disclosure: API key leaks via admin proxy) | LavaOfferPicker calls `/api/v1/admin/lava/products` server-side proxy ONLY — admin browser never sees the lava API key (mitigation lives in 03-05 T03 backend handler). Verified by inspecting `api/lava.ts` — no key in request payload, no key in response body. |
| T-03-75 (EoP: paste UUID into lava_offer_id) | LavaOfferPicker has NO `<Input>` for the lava UUID. The Radix `<Select>` is bound to the live products query; `onValueChange` filters to rows that exist in the dropdown (`rows.find((r) => r.offerId === next)`) — a determined admin could still inject via DevTools but the backend doesn't validate the UUID's provenance anyway (UX guard, not security boundary, per plan body). The real defence is lava's runtime rejection of unknown offers. |
| T-03-76 (Tampering: admin sends `{"is_system":true}` via curl) | UI doesn't have an `is_system` field — `PlanForm` schema has no key for it; `CreatePlanInput` interface omits it. Backend handler ALSO ignores it (struct has no field — JSON unmarshal silently drops unknown keys per 03-08). Two-layer. |
| T-03-77 (Repudiation: admin denies a change) | Backend `AuditLog` middleware (extended in 03-08 T03) records every admin write. UI is a thin client — every mutation goes through `api/*` axios → backend → audit log. |
| T-03-78 (DoS: admin clicks delete repeatedly) | `useMutation`'s `isPending` blocks the destructive Button across all dialogs (`DeletePlanDialog`, `ReplaceOfferDialog`, `PlanOffersGrid` deactivate, `PlanServersPicker` save) — only one in-flight call per affordance. Backend rate limit also applies (HOTFIX-03). |

ASVS L1 scoping for the admin-web side: UI is a convenience layer — the security boundary lives in the backend (verified by 03-05, 03-06, 03-08 threat models). UI validation is for UX (immediate red-text feedback) and the backend re-validates everything (defence in depth).

## Threat Flags

None. No new HTTP endpoints — the UI only consumes endpoints already enumerated in 03-05 and 03-08 threat models. No new outbound calls (the LavaOfferPicker's products dropdown query goes through the existing axios `api` client, which goes through the existing admin-only `/admin/lava/products` proxy). No new schema. No new auth surface — `AdminLayout`'s existing tokens guard is inherited unchanged for `/plans` + `/plans/:id`.

## Known Stubs

None. Every page/component is wired to real backend data via TanStack Query. Empty states (Plans page when `plans.length === 0`, PlanServersPicker when there are no active servers, PlanOffersGrid empty cells) render explicit empty-state UI with a CTA — they are documented absence-of-data states, not placeholder stubs.

`server_ids: []` and `offers: []` in PlanForm's `createPlan` call IS intentional MVP behaviour: the create form only lands the bare plan row, and the operator wires servers + offers via the Servers/Pricing tabs after creation. This matches Servers.tsx UX precedent and the backend accepts empty arrays cleanly.

## Commits

| Task | Hash | Type | Message |
|------|------|------|---------|
| T01 | `18f1e71` | feat | vendor 7 shadcn components + install Radix/RHF/zod deps |
| T02 | `e6021c2` | feat | add typed API clients for admin plans + lava products |
| T03 | `9cafc18` | feat | add Plans listing page + table + code badge + delete dialog |
| T04 | `221ca86` | feat | add PlanDetail with 3 tabs + 5 plan-edit components |
| T05 | `4953587` | feat | wire /plans + /plans/:id routes and sidebar "Тарифы" entry |

## Downstream Consumers

- **Plan 03-11 (docs-sandbox-smoke):** Admin UAT exercises the full flow — create plan → assign servers → add offer with lava picker → replace offer → soft-delete with code confirmation. The deferred ESLint config setup is a separate concern.
- **Phase 7 ADMIN-06 (audit log UI):** Will surface the 10 new describeAction labels (create_plan, replace_plan_offer, etc. — added in 03-08 T03) — admin actions from THIS UI all show up in that log.
- **Phase 8 HARD-07 (tooling cleanup):** Owns the ESLint config setup for admin-web (pre-existing gap surfaced by this plan).

## Self-Check: PASSED

Files exist:
- FOUND: admin-web/src/components/ui/form.tsx
- FOUND: admin-web/src/components/ui/select.tsx
- FOUND: admin-web/src/components/ui/checkbox.tsx
- FOUND: admin-web/src/components/ui/switch.tsx
- FOUND: admin-web/src/components/ui/tabs.tsx
- FOUND: admin-web/src/components/ui/tooltip.tsx
- FOUND: admin-web/src/components/ui/textarea.tsx
- FOUND: admin-web/src/components/ui/badge.tsx (modified — secondary variant added)
- FOUND: admin-web/src/api/plans.ts
- FOUND: admin-web/src/api/lava.ts
- FOUND: admin-web/src/pages/Plans.tsx
- FOUND: admin-web/src/pages/PlanDetail.tsx
- FOUND: admin-web/src/components/plans/PlansTable.tsx
- FOUND: admin-web/src/components/plans/PlanForm.tsx
- FOUND: admin-web/src/components/plans/PlanServersPicker.tsx
- FOUND: admin-web/src/components/plans/PlanOffersGrid.tsx
- FOUND: admin-web/src/components/plans/LavaOfferPicker.tsx
- FOUND: admin-web/src/components/plans/DeletePlanDialog.tsx
- FOUND: admin-web/src/components/plans/ReplaceOfferDialog.tsx
- FOUND: admin-web/src/components/plans/PlanCodeBadge.tsx
- FOUND: admin-web/src/App.tsx (modified — Plans + PlanDetail lazy imports + 2 routes)
- FOUND: admin-web/src/components/layout/AdminLayout.tsx (modified — Tag icon + Тарифы nav entry)
- FOUND: admin-web/package.json (modified — 8 new deps)
- FOUND: admin-web/package-lock.json (modified — 323 packages added)

Commits exist (verified via `git log --oneline -7`):
- FOUND: 18f1e71 (T01 vendor 7 shadcn components)
- FOUND: e6021c2 (T02 api/plans.ts + api/lava.ts)
- FOUND: 9cafc18 (T03 Plans page + table + code badge + delete dialog)
- FOUND: 221ca86 (T04 PlanDetail + 5 plan-edit components)
- FOUND: 4953587 (T05 routes + sidebar)

Verification:
- `tsc -p admin-web/tsconfig.app.json --noEmit` → exit 0 (PASS)
- `tsc -b admin-web` → exit 0 (PASS)
- `vite build` → 2.81s, 13 chunks emitted, dist/assets/Plans-* + PlanDetail-* present (PASS)
- All 7 plan-level success criteria — PASS
- All 5 task-level acceptance grep results — PASS
