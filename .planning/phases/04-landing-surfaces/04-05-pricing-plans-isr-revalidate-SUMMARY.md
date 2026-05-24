---
phase: 04-landing-surfaces
plan: 05
subsystem: landing
tags: [pricing, isr, plans, currency, i18n, revalidate, web-04, web-08]
dependency_graph:
  requires:
    - "landing/src/lib/env.ts (Plan 01 — BACKEND_API_URL, REVALIDATE_SECRET)"
    - "landing/src/lib/session.ts (Plan 02 + Plan 03 — getSession with HMAC rv_user verify)"
    - "landing/src/components/ui/card.tsx, ui/button.tsx (Plan 02)"
    - "landing/src/components/app/tier-badge.tsx (Plan 02)"
    - "landing/src/i18n/navigation.ts (Link, useRouter, usePathname)"
    - "landing/src/messages/{en,ru,es}.json pricing.* + dashboard.plan.* namespaces (Plan 01)"
    - "(app) route group + force-dynamic (Plan 02)"
    - "Phase 3 GET /api/v1/plans?currency=<C> contract (Phase 3 plan 03-07)"
  provides:
    - "GET /<locale>/pricing — server-rendered, per-request, fetch-tag-cached pricing page (WEB-04)"
    - "POST /api/revalidate-pricing?secret=<S> — admin tag-bust hook (D-14)"
    - "fetchPlans(currency) — server-only helper with next.tags=['plans'] revalidate=600 fetch cache"
    - "currencyForLocale(locale, override?) + formatPrice(amount, currency, locale) — locale/currency utilities"
    - "PlanCard / CurrencySwitcher / PricingClient components"
  affects:
    - "Plan 04-07 (checkout) — owns the PricingClient auto-checkout effect (placeholder shipped here)"
    - "Phase 3 admin write handlers (server/api/internal/handler/plans_admin.go) need to POST `${APP_URL}/api/revalidate-pricing?secret=${REVALIDATE_SECRET}` after every successful plan/offer/plan-server write — captured as Phase 3 follow-up (see Follow-Up Todos below)"
tech_stack:
  added: []
  patterns:
    - "Server-rendered per-request pricing with fetch-layer ISR tag cache (revalidate=600 + tags=['plans']) instead of page-level force-static — preserves request-time getSession() so per-user CTA branching cannot be poisoned by build-time cookie capture"
    - "Constant-time secret compare via node:crypto.timingSafeEqual + length-mismatch short-circuit to avoid the throw on length differences"
    - "Allow-listed enum sanitisation: ?currency= is normalised to USD/EUR/RUB or falls back to locale default"
    - "Server-component PlanCard receives all i18n labels as props so it stays renderable without a client tree"
    - "Client-component CurrencySwitcher writes pricing_currency cookie (non-HttpOnly, SameSite=Lax) + router.replaces ?currency= preserving other query params"
key_files:
  created:
    - "landing/src/lib/locale-currency.ts"
    - "landing/src/lib/plans.ts"
    - "landing/src/app/[locale]/(app)/pricing/page.tsx"
    - "landing/src/app/[locale]/(app)/pricing/pricing-client.tsx"
    - "landing/src/components/app/currency-switcher.tsx"
    - "landing/src/components/app/plan-card.tsx"
    - "landing/src/app/api/revalidate-pricing/route.ts"
  modified: []
decisions:
  - "Did NOT set `export const dynamic = 'force-static'` on /pricing. The (app) layout already sets force-dynamic; force-static would freeze the cookie jar to build-time empty values and break per-user CTA detection (a logged-in Pro user would see Get Pro and could re-pay). D-13's 'statically generated, tag-bust on admin write' intent is preserved at the fetch-cache layer instead via `next: { tags: ['plans'], revalidate: 600 }` + revalidateTag('plans', 'max')."
  - "pricing_currency cookie attributes locked: non-HttpOnly (client needs to read its own previous choice — value is just 'USD'/'EUR'/'RUB' with no privacy impact), Path=/, SameSite=Lax (survives top-level redirect /login → /pricing), Secure when location.protocol === 'https:', Max-Age=2592000 (30 days)."
  - "Currency allow-list (USD/EUR/RUB) is enforced in `currencyForLocale()` server-side. Any other ?currency= input falls back to the locale default. T-04-05-05 closure."
  - "fetchPlans returns [] on non-2xx OR network failure (with a console.warn) so the page renders the i18n empty state (pricing.empty.heading) rather than crashing — operator gets observability via the warn, users see a graceful message."
  - "PlanCard receives ALL i18n labels via props (proLabel, freeLabel, cta translations via local getTranslations) so it stays a pure server component without needing 'use client'. The TierBadge contract (label as prop) carries through."
  - "PricingClient is a typed-props no-op placeholder owned by Plan 04-07. Keeping the typed signature instead of `_: Props` preserves editor IntelliSense and the contract for the future implementer."
  - "Per the threat register, the secret-in-query-param model is accepted for Phase 4 with a note: future hardening should move it to an X-Revalidate-Token header so the secret doesn't appear in nginx access logs (T-04-05-07)."
metrics:
  duration: "~15 minutes wall clock (3 tasks, 3 commits, 1 deviation auto-fix, 1 full build + 1 e2e curl smoke)"
  completed: "2026-05-24"
  tasks_completed: 3
  files_changed: 7
  commits: 3
---

# Phase 4 Plan 05: Pricing — Plans Fetch + ISR + Revalidate Summary

Shipped the dynamic /pricing page that closes WEB-04 + WEB-08: a server-rendered page under `(app)/pricing/page.tsx` that fetches plans from the backend's public `/api/v1/plans?currency=<C>` with a 10-minute fetch-tag cache (tags: `['plans']`), reads `getSession()` per-request so a logged-in Pro user sees "Current plan" instead of "Get Pro" (no risk of duplicate checkout), and exposes a constant-time-secret-protected `POST /api/revalidate-pricing` that the Go backend's admin write handlers call to bust the tag cache after each plan/offer write. Currency selection is locale-default with a chip-group override that persists to a 30-day `pricing_currency` cookie. End-to-end smoke confirmed `?secret=wrong → 401 {error:"unauthorized"}` and `?secret=correct → 200 {revalidated:true, tag:"plans"}`. Build registers `/[locale]/pricing` and `/api/revalidate-pricing` as `ƒ Dynamic` routes — exactly the request-time render mode the per-user CTA branching requires.

## Tasks Completed

| Task | Name                                                                              | Commit    | Files                                                                                                                                                                                                                                                          |
| ---- | --------------------------------------------------------------------------------- | --------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1    | fetchPlans helper + locale → currency mapping + currency formatter                | `e2146b2` | landing/src/lib/locale-currency.ts, landing/src/lib/plans.ts                                                                                                                                                                                                  |
| 2    | /pricing page (server component) + PlanCard + CurrencySwitcher + PricingClient    | `47a6b1f` | landing/src/app/[locale]/(app)/pricing/page.tsx, landing/src/app/[locale]/(app)/pricing/pricing-client.tsx, landing/src/components/app/plan-card.tsx, landing/src/components/app/currency-switcher.tsx                                                          |
| 3    | /api/revalidate-pricing route — secret-protected revalidateTag('plans', 'max')    | `e66392a` | landing/src/app/api/revalidate-pricing/route.ts                                                                                                                                                                                                                |

## Why /pricing is NOT marked `force-static` (the core threat-register decision)

This is the single most important Phase 4 sub-decision in plan 04-05.

CONTEXT.md D-13 reads "statically generated, tag-bust on admin write". A literal reading would suggest `export const dynamic = "force-static"` on the page. **We deliberately did not do that** — and the threat register's T-04-05-09 closure depends on this.

`force-static` evaluates the page at build time. At build time, `cookies()` returns an empty cookie jar (because there is no request). `getSession()` reads `rv_at` and `rv_user` from that jar, so it returns `{isAuthed: false}` for the prerendered HTML — and the page's CTA branching renders "Get Pro" for every visitor, including logged-in Pro users.

A Pro user clicking that CTA is taken through the auto-checkout flow and could be charged a second time. Even if the backend correctly detects "user already has active subscription" and rejects the checkout, we've already burned a paid lava.top API request and given the user a confusing UX.

The fix is to keep the page in the (app) layout's request-time render mode (the layout already sets `dynamic = "force-dynamic"`). `getSession()` then reads the real cookies; the Pro user sees "Current plan" (disabled).

D-13's "tag-bust on admin write" intent is preserved at the **fetch-cache layer** instead of the page-render layer:

```ts
fetch(url, { next: { tags: ["plans"], revalidate: 600 } });
```

Even though the page is request-time-rendered, the fetch result is cached at the Next.js data cache with a 10-minute background refresh and the `"plans"` tag. `revalidateTag("plans", "max")` in `/api/revalidate-pricing` busts that fetch entry across all renders, so admin writes still propagate within one HTTP round-trip. Backend gets at most one call per 10 minutes per (locale, currency) variant on a steady-state hit rate.

The threat register tracks this as T-04-05-09 (Elevation by stale render) → mitigated.

## `pricing_currency` cookie attribute set

| Attribute   | Value                                       | Why                                                                                                                                                |
| ----------- | ------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| `HttpOnly`  | **NO** (intentional)                        | Cookie is read by the CurrencySwitcher client component itself to recover the user's previous choice; value is non-sensitive ("USD" / "EUR" / "RUB") |
| `Secure`    | `location.protocol === "https:"` (runtime)   | http://localhost works in dev (cookie still written); production forces Secure                                                                     |
| `SameSite`  | `Lax`                                       | Must survive the top-level redirect /login → /pricing so the user's chosen currency persists across the OAuth bounce                               |
| `Path`      | `/`                                         | Other (app) pages may want to read it later (e.g. dashboard's "manage subscription" CTA could carry currency hint)                                 |
| `Max-Age`   | `2592000` (30 days)                         | Long enough to be sticky across multiple sessions; shorter than the rv_rt 30-day refresh TTL so they expire together                              |

Documented in `landing/src/components/app/currency-switcher.tsx` lines 12-21.

T-04-05-08 disposition (accept — pricing_currency is plain non-sensitive data) is honored.

## CTA target matrix (PlanCard)

| Session            | Plan    | isCurrent | CTA          | Target                                                                              |
| ------------------ | ------- | --------- | ------------ | ----------------------------------------------------------------------------------- |
| Not authed         | Free    | n/a       | (none)       | —                                                                                   |
| Not authed         | Pro     | n/a       | "Get Pro"    | `/login?next=/pricing&plan=pro&period=monthly&currency=<C>`                          |
| Authed, planId=free | Free   | true      | "Current plan" (disabled) | —                                                                  |
| Authed, planId=free | Pro    | false     | "Get Pro"    | `/pricing?plan=pro&period=monthly&currency=<C>&checkout=auto`                        |
| Authed, planId=pro  | Free   | false     | (none)       | — (Pro user can't "downgrade" through pricing — Plan 06's manage page handles that) |
| Authed, planId=pro  | Pro    | true      | "Current plan" (disabled) | —                                                                  |

The logged-in CTA carries `checkout=auto` — Plan 07's PricingClient will detect that on mount and POST `/api/v1/checkout` through the Plan 03 proxy. The logged-out CTA carries `next=/pricing&plan=pro&period=monthly&currency=<C>` — Plan 04's OAuth callback honours `next` and forwards back to /pricing with the same params, so the user lands in the auto-checkout flow without re-clicking.

T-04-05-04 disposition (downstream backend re-validates the plan code) — Plan 07 / Phase 3 PAY-08 closes this; we just ship the right query string here.

## Revalidate route — security model

```
POST /api/revalidate-pricing?secret=<S>
```

| Threat                                  | Closure                                                                                                                                                            |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| T-04-05-01 (Elevation via wrong secret) | `safeEq` uses `node:crypto.timingSafeEqual` with a length-mismatch short-circuit; mismatched secret → `401 {error:"unauthorized"}` with no side effects             |
| T-04-05-02 (Secret leak to client)      | `env.REVALIDATE_SECRET` lives in `lib/env.ts` (`import "server-only"`); importing into a client component fails the build; no `NEXT_PUBLIC_` mirror                  |
| T-04-05-07 (Open admin tag-bust)        | Constant-time secret enforced; accepted that the secret is in the query param for Phase 4 (note flagged for header-based hardening in a future phase)               |

Smoke-verified end-to-end against a freshly built standalone server:

```text
POST /api/revalidate-pricing?secret=wrong  → 401 {"error":"unauthorized"}
POST /api/revalidate-pricing?secret=right  → 200 {"revalidated":true,"tag":"plans"}
```

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Next.js 16's `revalidateTag` second argument is required**

- **Found during:** Task 3 build (`Type error: Expected 2 arguments, but got 1` on `revalidateTag("plans")`)
- **Issue:** The plan body (and the Task 3 acceptance criterion `grep -n 'revalidateTag("plans")'`) was written for the legacy single-argument signature. Next.js 16's `revalidateTag` deprecated the single-arg form and made the second `profile: CacheLifeProfile | CacheLifeConfig` argument REQUIRED at the type level. Calling `revalidateTag("plans")` fails the build's TypeScript phase.
- **Fix:** Updated the call to `revalidateTag("plans", "max")`. The `"max"` profile is the documented replacement for the single-arg legacy semantics ("hard-invalidate immediately on next read") per `https://nextjs.org/docs/messages/revalidate-tag-single-arg`. `updateTag()` is not usable from a route handler — it's restricted to Server Actions (throws otherwise per the source in `next/dist/server/web/spec-extension/revalidate.js`).
- **Impact on AC:** The strict literal grep `grep -n 'revalidateTag("plans")'` no longer matches because the call is now `revalidateTag("plans", "max")`. The semantic intent (bust the `"plans"` tag) is preserved — verified end-to-end via the curl smoke above (200 response + admin tag-bust would invalidate the fetch cache on the next /pricing render).
- **Why this is in scope (Rule 1):** Code that doesn't compile is broken behavior caused by the current plan's own changes; fixing it inline is required to ship the task.
- **Files modified:** `landing/src/app/api/revalidate-pricing/route.ts`
- **Commit:** included in Task 3 commit (`e66392a`)

No other deviations — Tasks 1 and 2 executed exactly as written.

### CLAUDE.md / Project-Convention Adjustments

None — CLAUDE.md's GSD workflow enforcement was honored via the orchestrator. The project-level dark-only design and the (app) route-group force-dynamic constraint from Phase 4 Plan 02 were both respected.

## Authentication Gates

None — all work was server-side rendering, ISR plumbing, and a route handler. No third-party auth (Apple/Google SSO, lava.top, GitHub) was required to execute this plan.

## Acceptance Criteria — Final Status

**Task 1 (7/7 pass):**
- `currencyForLocale` exported from locale-currency.ts ✓
- USD/EUR/RUB allow-list literals present ✓
- `Intl.NumberFormat` invoked ✓
- `import "server-only"` line 1 of plans.ts ✓
- `tags: [...'plans']` present in plans.ts ✓
- `env.BACKEND_API_URL.../api/v1/plans` URL formed ✓
- `npx tsc --noEmit` exit 0 (verified via direct tsc invocation against worktree)

**Task 2 (10/11 pass + 1 build pass):**
- `fetchPlans` imported in page ✓
- `currencyForLocale` imported in page ✓
- `getSession` imported in page ✓
- `PlanCard` + `CurrencySwitcher` imported in page ✓
- `grep -n 'force-static'` returns 0 matches in page.tsx ✓ (string deliberately absent — see "Why /pricing is NOT marked `force-static`")
- `pricing_currency` cookie write in switcher ✓
- `SameSite=Lax` in switcher cookie write ✓
- No hardcoded prices (`grep -rn 'data-price=|"price":\\s*[0-9]|"\\$[0-9]|"€[0-9]|"₽[0-9]'` returns 0 matches in landing/src/) ✓
- `formatPrice` invoked in plan-card ✓
- `checkout=auto` query string in plan-card ✓
- `next=/pricing&plan=pro` query string in plan-card ✓
- `npm run build` exit 0 ✓ (with /[locale]/pricing registered as ƒ Dynamic — the right render mode)

**Task 3 (6/6 pass):**
- `revalidateTag("plans"` present (literal differs: `revalidateTag("plans", "max")` per Next 16 — see deviation) ✓
- `timingSafeEqual` invoked ✓
- `env.REVALIDATE_SECRET` referenced ✓
- Both `status: 401` and `status: 200` paths present ✓
- `runtime = "nodejs"` directive present ✓
- `npm run build` exit 0 ✓ (with /api/revalidate-pricing registered as ƒ Dynamic)

**Plan verification (6/6 pass):**
- SC #5 — no hardcoded prices anywhere in landing/src/ ✓
- Per-user CTA model implemented (request-time getSession, force-dynamic via (app) layout, no force-static on the page) ✓
- Revalidate route: wrong secret → 401; right secret → 200 — verified via curl end-to-end ✓
- Currency switcher updates ?currency= and persists pricing_currency cookie ✓
- D-04 mapping (ru→RUB / en→USD / es→EUR) enforced in `currencyForLocale` ✓
- Build exits 0 with all env vars provided ✓

## Follow-Up Todos (for /gsd-note or operator)

- **Phase 3 admin write handlers fan-out:** `server/api/internal/handler/plans_admin.go` (Phase 3 plans 03-05/03-06) need to POST `${APP_URL}/api/revalidate-pricing?secret=${REVALIDATE_SECRET}` after every successful plan / offer / plan-server CRUD write. Use `http.Client` with a short timeout (1s) and log-only on failure (tag bust failure should NOT fail the admin write — operator can re-trigger). Captured as a Phase 3 cleanup task.
- **Phase-future hardening — secret in header:** The plan threat register flagged that secrets in `?secret=` query parameters can land in nginx access logs. Future hardening should accept the secret via `X-Revalidate-Token` request header (with the same constant-time compare) and reject query-param submissions. Low priority — Phase 4 accepts the risk given low bust rate + HTTPS enforcement.
- **Plan 04-07 PricingClient implementation:** Replace the no-op placeholder with the auto-checkout effect. Contract: detect `checkout === "auto"` + authed session → POST `/api/v1/checkout` via the proxy with `{ plan, period, currency }` → window.location.href to the lava.top payment URL. On error, toast destructive variant.
- **Plan 04-08 Playwright smokes:** (a) Pro session cookie shows "Current plan" disabled state on Pro card; (b) anonymous session shows "Get Pro" linking to /login; (c) currency chip click flips URL + writes pricing_currency cookie; (d) revalidate-pricing 401 on wrong secret, 200 on right.

## Known Stubs

**PricingClient (`landing/src/app/[locale]/(app)/pricing/pricing-client.tsx`):** Returns `null`. This is documented as a Plan 04-07 (checkout) deliverable, NOT a stub-without-resolution. Plan 04-05's deliverables (page + cards + currency switcher + revalidate hook + ISR) are fully wired to real sources; the checkout invocation is the next plan's scope. The plan's `objective` explicitly says "The page is also the entry point for the checkout flow (Plan 07) — the CTA on each plan card carries `plan`, `period`, `currency` query params that Plan 07's checkout client component consumes." Intentional and tracked.

No other stubs — every other rendered value (plans, currency, CTA targets, session) is wired to a real data source.

## Threat Flags

None — no security surfaces introduced beyond what the plan's `<threat_model>` already enumerated (T-04-05-01 through T-04-05-09). All dispositions honored as documented in "Revalidate route — security model" and "Why /pricing is NOT marked `force-static`".

## Self-Check: PASSED

- landing/src/lib/locale-currency.ts: FOUND
- landing/src/lib/plans.ts: FOUND (server-only line 1)
- landing/src/app/[locale]/(app)/pricing/page.tsx: FOUND (no force-static directive)
- landing/src/app/[locale]/(app)/pricing/pricing-client.tsx: FOUND (Plan 07 placeholder)
- landing/src/components/app/currency-switcher.tsx: FOUND (pricing_currency cookie + SameSite=Lax)
- landing/src/components/app/plan-card.tsx: FOUND (formatPrice + checkout=auto + next=/pricing CTAs)
- landing/src/app/api/revalidate-pricing/route.ts: FOUND (timingSafeEqual + revalidateTag("plans", "max"))
- Commit e2146b2 (Task 1): FOUND
- Commit 47a6b1f (Task 2): FOUND
- Commit e66392a (Task 3): FOUND
- npm run build: EXIT 0 (with /[locale]/pricing and /api/revalidate-pricing both registered as ƒ Dynamic)
- E2E /api/revalidate-pricing smoke: 401 on wrong secret, 200 on right secret ✓
- No hardcoded prices in landing/src/ ✓
