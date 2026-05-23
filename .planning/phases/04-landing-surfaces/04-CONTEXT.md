# Phase 4: Landing Surfaces - Context

**Gathered:** 2026-05-24
**Status:** Ready for planning
**Source:** /gsd-discuss-phase interactive session

<domain>
## Phase Boundary

Add five authenticated/transactional pages to the existing Next.js 16 landing at risevpn.com so a signed-in user can complete the **discover → sign-in → pay → return-with-Pro-active** loop entirely on the web:

- `/login` — Apple + Google sign-in (no email/password, no guest)
- `/auth/callback` — OAuth ID-token exchange + cookie handoff
- `/dashboard` — minimal post-auth landing (email, plan, single CTA)
- `/pricing` — public plan catalog (no auth), checkout entry point
- `/pay/success` — invoice polling, Pro-active confirmation
- `/pay/fail` — payment failure with retry/support paths

**Out of scope (deferred to other phases):**
- Device list / device revoke (Phase 7 — Admin panel overhaul)
- Subscription cancel flow (backend exists from 03-05; UI in Phase 7)
- Invoice/billing history list (Phase 7)
- Mobile-app sign-in flow (Phase 5)
- Production cutover from `vpn.mydayai.uz` → `risevpn.com` DNS (operator task, not a phase deliverable)

</domain>

<decisions>
## Implementation Decisions

> Every entry is a **locked decision** unless marked Claude's Discretion. These came from the interactive discuss-phase session and supersede any conflicting defaults in PROJECT.md or RESEARCH.md guesses.

### Deployment Model

- **D-01:** **Hybrid execution within a single Next.js app.** Change `landing/next.config.ts` from `output: 'export'` to `output: 'standalone'` so the landing acquires a Node runtime. Marketing pages (`/`, locale-prefixed home, faq, etc.) remain statically-rendered at build time (Next's default SSG); nginx can still cache them. New app pages (`/login`, `/auth/callback`, `/dashboard`, `/pricing`, `/pay/success`, `/pay/fail`, `/api/*` proxy) use the Node runtime. Single build, single deploy, single repo.
- **D-02:** Production deployment surface gains one Node container (`landing-node`) alongside the existing nginx static-serving container. Compose-level routing: nginx terminates TLS + serves `/` static assets + proxies `/login`, `/dashboard`, `/pricing`, `/pay/*`, `/auth/*`, `/api/*` to the Node container on a private port. Planner adds the Compose change to plan 04-01 or sibling.

### Locales & Currency

- **D-03:** **Locales for Phase 4 and forward: `ru`, `en`, `es`.** Drop existing `uz`. Default = `ru` (unchanged). Migrations: delete `landing/src/messages/uz.json`, add `landing/src/messages/es.json`, update `landing/src/i18n/routing.ts` `locales` array, audit existing pages for UZ-specific copy and translate to ES. PROJECT.md update: confirm RU/EN/ES (already matches docs — UZ was a code-side drift). Initial ES translation by Claude; human translator review is a follow-up todo.
- **D-04:** **Currency per locale:** RU → RUB, EN → USD, ES → EUR. Backend `/api/v1/plans` accepts `?currency=USD|EUR|RUB` (Phase 3 D-27). Landing's `/pricing` derives currency from active locale + sends explicit `?currency=` query. URL parameter `?currency=` overrides locale default and persists in a `pricing_currency` cookie for the session. This **extends** Phase 3 D-27 (which mapped only "RU→RUB else USD") — the backend already supports all three currencies, so no backend change needed.

### UI / Component Library

- **D-05:** **Continue with `@base-ui/react` in landing.** Add Form, Input, Card, Skeleton, Toast primitives from base-ui (or build minimal wrappers using base-ui Slot if not in the catalog). Do NOT introduce shadcn/ui into landing. Update PROJECT.md: landing uses base-ui (intentional), admin-web uses shadcn (intentional) — different libraries by surface, both on Tailwind 4.
- **D-06:** Reuse existing landing primitives where applicable: `Navbar`, `LocaleSwitcher`, `Logo`, `Button`, `Sheet` (for mobile menu), `Accordion`. New `UserMenu` component for logged-in navbar state.

### Auth + Cookie Strategy

- **D-07:** **Same-origin proxy.** Landing's Node runtime exposes `/api/*` routes that proxy server-side to `vpnapi.mydayai.uz` (production) or the dev API URL. Browser never calls the backend directly. Cookies set on `risevpn.com` (or current `vpn.mydayai.uz`) with `HttpOnly; Secure; SameSite=Strict; Path=/`. No cross-domain cookie complexity. Trade-off: +5–20ms server-side hop per call (negligible vs the security + UX win).
- **D-08:** **Access token + refresh token both in HttpOnly cookies** set by the landing Node proxy after `/auth/apple` or `/auth/google` succeeds. Names: `rv_at` (access, ~5min TTL matching backend), `rv_rt` (refresh, ~30 days TTL matching backend). No tokens in `localStorage`, `sessionStorage`, JS-readable cookies, or in-memory app state. Satisfies SC #1 verbatim.
- **D-09:** **401 → refresh → retry pattern** in the Node proxy. When the proxied call returns 401 and `rv_rt` is present, proxy auto-calls `POST /auth/refresh`, updates `rv_at` cookie, retries the original call once. Refresh rotation rules from Phase 2 still apply (transactional rotation, one-time use). On refresh failure, proxy returns 401 to the browser and clears both cookies; client redirects to `/login`.

### OAuth Flow

- **D-10:** **Landing handles the OAuth callback** at `/auth/callback?provider=apple|google`. Sign-in button on `/login` constructs the Apple/Google authorize URL with `redirect_uri=https://risevpn.com/auth/callback&state=<csrf-token-from-cookie>`. Provider redirects back with `id_token` (Apple form-post) or `code` (Google). Callback page extracts the token, POSTs to backend `/auth/apple` or `/auth/google` via the same-origin proxy, backend returns user info + tokens, proxy sets cookies, page redirects to `?next=` URL (parsed from `state` payload) or `/dashboard`.
- **D-11:** **OAuth env config:** Apple Service ID = `services.risevpn.web` (matches Phase 2 D-30 expectation). Google Web Client ID = the existing `GOOGLE_CLIENT_ID_WEB` from Phase 2 config. Redirect URIs must be registered with Apple + Google for both production (`https://risevpn.com/auth/callback`) and the staging domain (whatever the dev/staging URL is). Operator task — planner notes this as a deployment prerequisite, not a code task.
- **D-12:** **CSRF protection for OAuth state:** generate cryptographically random `state` value, store in HttpOnly cookie `rv_oauth_state` with 5-min TTL before redirecting to provider; callback verifies `state` matches the cookie before accepting the ID token. Standard OAuth CSRF mitigation.

### /pricing Data Flow

- **D-13:** **On-demand ISR with `revalidateTag('plans')`.** /pricing is statically generated at build time using `fetch('/api/v1/plans?currency=...', { next: { tags: ['plans'] } })`. Admin-write handlers from Phase 3 plan 03-08 (CreatePlan, UpdatePlan, DeletePlan, AddPlanServer, RemovePlanServer, ReplacePlanServers, CreatePlanOffer, UpdatePlanOffer, DeletePlanOffer, ReplacePlanOffer) fan out a `POST https://risevpn.com/api/revalidate-pricing?secret=<shared>` call after every successful write. The landing route `/api/revalidate-pricing` validates the shared secret then calls `revalidateTag('plans')`. Three variants pre-rendered: `/ru/pricing` (RUB), `/en/pricing` (USD), `/es/pricing` (EUR). Cold-cache p99 ~50ms, warm p99 <10ms. Satisfies SC #5 verbatim.
- **D-14:** **Revalidation auth:** `REVALIDATE_SECRET` env var shared between admin backend + landing Node. Constant-time compare in the landing route. Phase 3 plans need a follow-up small task (or operator note) to add the fan-out call to all 10 admin write handlers — captured in the deferred ideas section below.

### /dashboard Scope

- **D-15:** **Minimal dashboard:** show email, current plan name + tier badge, single CTA (Free user → "Get Pro" → `/pricing`; Pro user → "Manage Subscription" → opens lava.top customer portal URL in new tab), and Sign-out button. No device list, no billing history, no cancel button. Satisfies SC #1 verbatim.
- **D-16:** **"Manage Subscription" URL** for Pro users: lava.top provides a per-contract management URL. Backend already stores `lava_contract_id` per user (Phase 3 03-01). Add a tiny backend helper `GET /api/v1/subscription/manage-url` that returns the lava-hosted management URL for the user's active contract (typed as `{ url: string }`). If user has no active contract, returns 404 — UI hides the link.
- **D-17:** **Plan + email source:** initial render uses the new JWT `plan_id` claim (Phase 3 D-29) decoded server-side in the Node proxy, joined with a cached `GET /api/v1/plans` lookup for the plan name. Backend `GET /api/v1/auth/me` (or equivalent — check Phase 2) returns the email. Single server-side round-trip per page load.

### Navbar States (SC #6)

- **D-18:** **Logged-in detection** via cookie presence on the Node server. Layout component reads `rv_at` cookie at request time:
  - No cookie → render "Pricing" + "Login" links
  - Cookie present → render "Pricing" + "Dashboard" + "Sign out"
  - Detection happens server-side so there's no flash-of-unauthenticated content. Layout `dynamic = 'force-dynamic'` on app pages, default static on marketing pages.

### /pricing CTA (Logged-Out Auto-Checkout)

- **D-19:** **Logged-out "Get Pro" → redirect → auto-checkout on return.** Anonymous user clicks plan card "Get Pro" → router pushes `/login?next=%2Fpricing&plan=pro&period=monthly&currency=USD`. After sign-in success, `/login` reads `next` from URL, redirects to `/pricing?plan=pro&period=monthly&checkout=auto`. /pricing detects `checkout=auto`, immediately POSTs `/api/v1/checkout` with the prefilled plan+period+currency, redirects to the lava payment URL. Zero extra clicks after sign-in. Satisfies SC #3 verbatim.
- **D-20:** **Logged-in "Get Pro" → straight to checkout.** Single POST `/api/v1/checkout`, redirect to lava. Matches SC #2 ("one HTTP round-trip").

### /pay/success Polling UX

- **D-21:** **Phase 3 D-25 contract implementation:** Page reads `?invoiceId=X` from URL, polls `GET /api/v1/invoices/:id` every 2s.
  - **Polls 1–5 (0–10s):** Pure-DB poll, no `?escalate`.
  - **Poll 6+ (10s onward):** Adds `?escalate=true` to force backend → lava fallback.
  - **Status flips to `paid`:** Show "Pro is active!" + button → `/dashboard`. Stop polling.
  - **Still `pending` at 30s:** Show "We're processing your payment — we'll email you when it's active. Status: pending." + "Refresh" button + Telegram support link (`https://t.me/flawlssr` per `landing/src/lib/constants.ts`). Keep showing same content; do NOT auto-retry from this point.
  - **Status flips to `failed`:** Redirect to `/pay/fail?invoiceId=X`.
- **D-22:** Spinner copy by locale (i18n message keys):
  - `pay.success.processing`: "Activating your Pro subscription…"
  - `pay.success.active`: "Pro is active!"
  - `pay.success.takingLonger`: "We're processing your payment — we'll email you when it's active."
  - `pay.success.contactSupport`: "Need help? Contact support on Telegram."

### /pay/fail Content

- **D-23:** **Claude's Discretion** within these constraints:
  - Headline: "Payment didn't go through" (i18n key `pay.fail.title`)
  - One-line explanation if `?reason=` is in URL (decline, timeout, cancelled — backend already maps lava error codes)
  - Primary CTA: "Try again" → `/pricing` (with same plan/period in URL)
  - Secondary CTA: "Contact support" → `https://t.me/flawlssr`
  - No retry button that re-fires the failed invoice (lava billing rule — must create a new checkout)

### Theme

- **D-24:** **Respect OS via `next-themes` `defaultTheme='system'`** across all pages (marketing + app). Existing landing already has next-themes installed and used. No commerce-pages light-lock.

### Sign-Out

- **D-25:** **`POST /api/v1/auth/logout` then clear cookies + redirect `/`.** Sign-out button calls the Node proxy `POST /auth/logout` which forwards to backend `/auth/logout` with the refresh cookie, backend revokes the refresh-session row (Phase 2 contract), proxy clears `rv_at` + `rv_rt` cookies via `Set-Cookie: <name>=; Max-Age=0; HttpOnly; Secure`, returns 204. Client navigates to `/`. If backend call fails, still clear cookies and redirect (don't leave the user stuck logged in client-side).

### Telemetry / Observability

- **Claude's Discretion:** No analytics SDK in Phase 4 (deferred). Server-side log lines (Pino or Next's built-in logger) for: OAuth callback successes/failures, checkout initiation, polling result, refresh-token rotation. Researcher/planner can wire to whatever logging the landing already uses.

### Claude's Discretion

- Loading skeleton designs for /pricing, /pay/success, /dashboard
- Mobile menu sheet content for app pages (logged-in vs logged-out variants)
- Apple/Google sign-in button styling (must match Apple HIG + Google brand guide for their respective buttons — fully spec'd, low ambiguity)
- ES translation initial pass (human review is a Phase 4 follow-up todo, not a phase blocker)
- Error toasts vs inline error messages on /login (use whatever pattern existing landing uses)
- 404 / 500 handling for app pages (reuse existing `not-found.tsx` patterns)

### Folded Todos

None — no pending todos surfaced from cross-reference.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Architecture & Decisions
- `docs/ADR-007-lava-sso-rework.md` — Authoritative SSO + payments architecture decisions for the whole milestone
- `docs/audit/MASTER-PLAN.md` — Cross-surface audit findings consolidation
- `.planning/PROJECT.md` — Locked tech stack constraints (Next 16, next-intl, base-ui in landing per D-05)

### Prior Phase Context (Carry-Forward Decisions)
- `.planning/phases/02-auth-sso-backend/02-CONTEXT.md` — Apple/Google verifier expectations, JWT shape, refresh-token rotation rules
- `.planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md` — D-25 (`/pay/success` polling contract), D-27 (currency derivation), D-29 (JWT plan_id claim), D-13 (admin UI for plans/offers ships in Phase 3, no admin work in Phase 4)
- `.planning/phases/03-lava-top-plans-catalog/03-05-checkout-cancel-invoices-admin-lava-proxy-SUMMARY.md` — `POST /api/v1/checkout` contract, `GET /api/v1/invoices/:id?escalate=true` contract
- `.planning/phases/03-lava-top-plans-catalog/03-07-public-plans-jwt-cache-SUMMARY.md` — `GET /api/v1/plans` shape + cache semantics + JWT plan_id resolution
- `.planning/phases/03-lava-top-plans-catalog/03-08-admin-plans-crud-SUMMARY.md` — Admin write endpoints that need the new `revalidateTag` fan-out (D-13/D-14)

### Existing Landing Codebase
- `landing/next.config.ts` — Currently `output: 'export'`; D-01 changes to `'standalone'`
- `landing/src/i18n/routing.ts` — Currently RU/EN/UZ; D-03 changes to RU/EN/ES
- `landing/src/i18n/request.ts` — next-intl request config (locale resolver)
- `landing/src/messages/{ru,en}.json` — Existing translations; ES added per D-03, UZ removed
- `landing/src/app/layout.tsx` + `landing/src/app/[locale]/layout.tsx` — Layout patterns to mirror
- `landing/src/lib/constants.ts` — Site name, Telegram support link (`https://t.me/flawlssr`), App download URLs
- `landing/src/components/common/{navbar,locale-switcher,logo}.tsx` — Reusable components
- `landing/src/components/ui/{button,sheet,accordion}.tsx` — base-ui primitives to extend per D-06
- `landing/components.json` — base-ui config; consult before adding primitives

### Backend Contracts (Phase 2 + 3 Shipped APIs)
- `server/api/internal/handler/auth.go` — `/auth/apple`, `/auth/google`, `/auth/refresh`, `/auth/logout`, `/auth/me` endpoints
- `server/api/internal/handler/payment.go` — `POST /checkout`, `POST /subscription/cancel`, `GET /invoices/:id` (+ `?escalate=true`)
- `server/api/internal/handler/plans_public.go` — `GET /api/v1/plans?currency=...`
- `server/api/internal/handler/webhook_lava.go` — Webhook side-effects context (informs why polling exists)
- `docs/lava-payments-api.md` — Operator-facing API reference, env vars, error catalogue

### Requirements
- `.planning/REQUIREMENTS.md` §WEB-01..WEB-09 — Each requirement must be addressed by at least one plan

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **next-intl wired**: locale routing, request config, message files — only the locale list and one message file need to change for D-03
- **Theme toggle**: `next-themes` installed; just consume the existing provider for D-24
- **Marketing layout**: `landing/src/app/[locale]/layout.tsx` defines the locale-scoped layout — app pages can share or override
- **`@base-ui/react`**: already gives us Slot, Popover, Dialog, Select primitives — use these for sign-in modal, plan dropdowns, sign-out menu
- **`motion`**: installed for animations; use for page transitions on app pages if needed
- **`lucide-react`**: icon set already in use — reuse for navbar, user menu, plan cards, status indicators
- **`landing/src/lib/constants.ts`**: SITE name, SUPPORT.telegram URL, SOCIAL_LINKS — reuse in /pay/* support links

### Established Patterns
- **Server components default, client components opt-in with `'use client'`** — match existing pattern
- **Component organization**: `components/sections/` for marketing, `components/common/` for cross-cutting (navbar, locale-switcher), `components/ui/` for primitives. Phase 4 adds `components/app/` for app-page components (UserMenu, PlanCard, PaymentStatusCard, AuthButton) — proposed, planner can revise.
- **i18n message namespacing**: top-level keys per page (e.g., `hero.title`, `faq.questions`). Phase 4 adds `login.*`, `dashboard.*`, `pricing.*`, `pay.success.*`, `pay.fail.*`.

### Integration Points
- **`landing/next.config.ts`** is the foundational change (D-01) — every other plan depends on it
- **nginx config** (`landing/nginx/` directory exists per scout) needs updating for D-02 hybrid routing
- **Apple/Google OAuth dashboards** need the new `/auth/callback` redirect URI added (operator task)
- **Phase 3 admin handlers** (10 write endpoints in `server/api/internal/handler/plans_admin.go`) need a small fan-out call added for D-14 — this is either an in-scope Phase 4 plan or a Phase 3 follow-up; planner decides which.

### Architectural Constraints
- **No client-side token storage** (D-08) — every API call must go through the Node proxy
- **Cookies must be `HttpOnly; Secure; SameSite=Strict`** — set only on HTTPS responses; dev workflow needs HTTPS too (or `Secure=false` only in `NODE_ENV=development`)
- **Locale prefix is always present in URL** (`localePrefix: 'always'` per current routing config) — keep this; affects all internal links
- **Static export is being dropped** (D-01) — verify no other code depends on `output: 'export'` semantics (e.g., any `next/image` usage that relied on `unoptimized: true`)

</code_context>

<specifics>
## Specific Ideas

- **Polling cadence on /pay/success:** 2s interval, escalate at poll 6, 30s total timeout (D-21) — these are concrete numbers the planner should not change without reason
- **JWT cookie names:** `rv_at` (access), `rv_rt` (refresh), `rv_oauth_state` (CSRF) (D-08, D-12)
- **Telegram support link:** `https://t.me/flawlssr` (from `landing/src/lib/constants.ts`)
- **Apple Service ID:** `services.risevpn.web` (D-11; matches Phase 2 D-30)
- **Currency cookie name:** `pricing_currency` (D-04)
- **Revalidate fan-out endpoint:** `POST /api/revalidate-pricing?secret=<REVALIDATE_SECRET>` on the landing Node, called by every Phase 3 admin write handler (D-14)
- **/auth/callback redirect URIs to register:** `https://risevpn.com/auth/callback`, plus dev/staging equivalents
- **Plan card pre-selection on logged-out → logged-in return:** query params `plan=pro&period=monthly&currency=USD&checkout=auto` (D-19)

</specifics>

<deferred>
## Deferred Ideas

### Phase 5 (Mobile SSO + Pro CTA)
- Mobile-app `LoginScreen` with Apple/Google/Guest, informational PaymentScreen, deep-link return from `risevpn.com/pay/success`

### Phase 7 (Admin panel overhaul)
- Device list + revoke flow on /dashboard
- Billing history list (last N invoices) on /dashboard
- In-app cancel subscription flow (backend exists from 03-05; UI deferred)

### Phase 4 follow-up todos (not blocking, capture in /gsd-note)
- **ES translation human review** — Claude does an initial pass per D-03, but a native Spanish-speaking translator should review before any marketing push targeting Spanish-speaking markets
- **PROJECT.md update** — clarify landing uses base-ui (intentional split from admin-web's shadcn) per D-05; remove "shadcn/ui" from the landing tech-stack line
- **PROJECT.md update** — confirm RU/EN/ES locale list (UZ removed) per D-03 once the migration lands
- **Phase 3 follow-up: add `revalidateTag` fan-out** — per D-14, the 10 admin write handlers in `server/api/internal/handler/plans_admin.go` need a small POST after success. Either an early Phase 4 plan or a one-off 3.x fix.
- **Apple/Google OAuth dashboard config** — add `https://risevpn.com/auth/callback` to authorized redirect URIs in both dashboards. Operator task, not a code change.
- **Production cutover plan** — vpn.mydayai.uz → risevpn.com DNS + SSL. Not a Phase 4 deliverable; operator-managed.

### Reviewed Todos (not folded)
None — no todos surfaced during cross-reference.

</deferred>

---

*Phase: 04-landing-surfaces*
*Context gathered: 2026-05-24 via /gsd-discuss-phase*
