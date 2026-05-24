---
phase: 04-landing-surfaces
plan: 07
subsystem: landing
tags: [checkout, payments, polling, lava, plan-id-freshness, oauth-resume, web-05, web-06, web-07]
dependency_graph:
  requires:
    - "landing/src/app/[locale]/(app)/pricing/pricing-client.tsx (Plan 05 stub — replaced inline)"
    - "landing/src/lib/session.ts (Plan 02 + Plan 03 — getSession with HMAC rv_user verify)"
    - "landing/src/lib/constants.ts (SUPPORT.telegram)"
    - "landing/src/components/ui/card.tsx + button.tsx (Plan 02)"
    - "landing/src/i18n/navigation.ts (next-intl Link, useRouter, redirect)"
    - "landing/src/messages/{en,ru,es}.json pay.success.* + pay.fail.* + errors.network (Plan 01)"
    - "Plan 03 catch-all proxy /api/[...path] (POST /api/v1/checkout, GET /api/v1/invoices/:id, POST /api/v1/auth/refresh)"
    - "Phase 3 POST /api/v1/checkout, GET /api/v1/invoices/:id[?escalate=true] contracts (03-05 SUMMARY)"
    - "Phase 3 D-29 — plan_id claim in JWT (re-decoded by Plan 03 proxy on refresh)"
  provides:
    - "Checkout flow client — auto-launches /api/v1/checkout when ?checkout=auto + plan + period present"
    - "/<locale>/pay/success — auth-gated server page hosting the poll-client (WEB-06)"
    - "Invoice polling state machine — 2s cadence, escalate at poll 6, 30s timeout (D-21)"
    - "B2/D-17 closure — force POST /api/v1/auth/refresh on status=paid so rv_user.planId rotates BEFORE /dashboard navigation"
    - "/<locale>/pay/fail — reason-aware (default/declined/cancelled) with retry CTA preserving plan/period/currency (WEB-07)"
    - "Payment-URL whitelist /^https:\\/\\/(gate\\.|app\\.|pay\\.)?lava\\.top\\// — defence-in-depth open-redirect guard"
    - "?reason= allow-list via safeReason() — anything other than declined/cancelled falls back to default"
  affects:
    - "Phase 4 milestone goal — closes the entire money loop: anon → /pricing → sign-in → auto-checkout → lava.top → /pay/success (Pro active) → /dashboard reflects Pro"
    - "Plan 04-08 (deploy + smoke) — must Playwright the full flow including the force-refresh trigger on paid status and the 30s timeout UI"
    - "/dashboard (Plan 06) — first surface after /pay/success → Continue; reads the rotated rv_user.planId set by the force-refresh"
tech_stack:
  added: []
  patterns:
    - "useRef one-shot latch in PricingClient — prevents duplicate POST /api/v1/checkout under React strict mode re-runs"
    - "Pre-poll lifecycle: kick first poll synchronously on mount so the user sees a transition at ~t=0 instead of waiting 2s for the first network call"
    - "Server-action-less server page → client poll component split — page does auth gate + URL prep; PollClient owns the state machine"
    - "Reason-aware body rendering driven by a typed Record<FailReason, key> lookup — replaces if/else branching with a single t() call"
    - "URLSearchParams.set() for round-trip query params on /pay/fail's Try-again — URL-encodes plan/period/currency safely on write"
    - "AbortSignal.timeout(5000) on the force-refresh fetch — bounds the worst-case time-to-active so a hung backend doesn't pin the UI on 'processing'"
key_files:
  created:
    - "landing/src/app/[locale]/(app)/pay/success/page.tsx"
    - "landing/src/app/[locale]/(app)/pay/success/poll-client.tsx"
    - "landing/src/app/[locale]/(app)/pay/fail/page.tsx"
    - "landing/src/components/app/payment-status-card.tsx"
    - "landing/src/components/app/payment-fail-card.tsx"
    - ".planning/phases/04-landing-surfaces/deferred-items.md (pre-existing Plan 04 /auth/callback conflict log)"
  modified:
    - "landing/src/app/[locale]/(app)/pricing/pricing-client.tsx (Plan 05 stub replaced with real checkout-flow client)"
decisions:
  - "payment_url whitelist: /^https:\\/\\/(gate\\.|app\\.|pay\\.)?lava\\.top\\// — covers documented lava.top subdomains (gate, app, pay) and bare lava.top; rejects everything else even from a trusted backend (T-04-07-01 defence-in-depth)"
  - "Status casing normalised to lowercase via `.toString().toLowerCase()` — backend's mapLavaStatusToLocal may return either casing (per 03-05 SUMMARY), and the client MUST not branch on raw response"
  - "force-refresh is fire-and-forget (try/catch swallows errors) — the natural rv_at ≤5min expiry is the worst-case fallback; never block the 'Pro is active!' UI on a transient refresh failure"
  - "First poll fires synchronously inside the useEffect rather than waiting INTERVAL_MS — gives instant feedback when lava webhook lands before the page loads"
  - "Refresh button after timeout is single-shot (NOT timer restart) — avoids accidental infinite polling loops; user can click again if they want another shot"
  - "/pay/fail is NOT auth-gated — a user whose session expired during the lava round-trip should still be able to read why their payment failed without being bounced to /login first"
  - "?reason= allow-list locked to {default, declined, cancelled} — extending requires adding both an enum value AND a translation key in all three locale files (D-23)"
  - "/pay/fail Try-again preserves plan/period/currency in the URL but does NOT carry checkout=auto — user must intentionally click 'Get Pro' again rather than re-triggering the failed checkout automatically"
  - "PaymentStatusCard split out from PollClient — keeps the three UI states isolated (easy to render in storybook/tests) and lets PollClient focus on the polling state machine"
metrics:
  duration: "~13 minutes wall clock (3 tasks, 3 commits, 0 in-scope deviations, 1 documented out-of-scope build blocker)"
  completed: "2026-05-24"
  tasks_completed: 3
  files_changed: 5
  commits: 3
---

# Phase 4 Plan 07: Checkout + /pay/success + /pay/fail Summary

Wired the entire money loop end-to-end. /pricing's PricingClient (previously a Plan 05 stub) now POSTs `/api/v1/checkout` when the URL carries `?checkout=auto&plan=<code>&period=<period>` and hard-navigates the browser to the lava.top `payment_url` (whitelisted against a regex even though the backend is trusted). 401 bounces back to `/login?next=/pricing?...&checkout=auto` so the auto-checkout intent survives sign-in. /pay/success renders a poll-client that hits `/api/v1/invoices/:id` every 2 seconds, escalates with `?escalate=true` from poll 6 onwards, hard-stops after 30 seconds with a "still processing" UI, and on `status === "paid"` fires `POST /api/v1/auth/refresh` through the Plan 03 proxy BEFORE flipping to the "Pro is active!" UI — the proxy decodes the new access JWT's `plan_id` claim and re-issues `rv_user` with the upgraded planId, so the user lands on `/dashboard` with Pro visible immediately rather than waiting up to 5 minutes for natural rv_at expiry (B2/D-17 closure on the post-paid side). /pay/fail renders reason-aware copy from a tight allow-list (default/declined/cancelled — anything else silently falls back to default) with a Try-again link that preserves plan/period/currency in the URL.

## Tasks Completed

| Task | Name                                                                                | Commit    | Files                                                                                                                                              |
| ---- | ----------------------------------------------------------------------------------- | --------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1    | PricingClient — replace stub with checkout-flow client (auto-checkout on ?checkout=auto) | `0e622a0` | landing/src/app/[locale]/(app)/pricing/pricing-client.tsx                                                                                          |
| 2    | /pay/success — page + poll-client + PaymentStatusCard (D-21 polling + B2 force-refresh) | `a9e30f2` | landing/src/app/[locale]/(app)/pay/success/page.tsx, .../poll-client.tsx, landing/src/components/app/payment-status-card.tsx                       |
| 3    | /pay/fail — page + PaymentFailCard (reason-aware copy + Try-again CTA)                 | `04a25d3` | landing/src/app/[locale]/(app)/pay/fail/page.tsx, landing/src/components/app/payment-fail-card.tsx                                                  |

## Payment URL Whitelist (T-04-07-01 closure)

```ts
const LAVA_URL_PATTERN = /^https:\/\/(gate\.|app\.|pay\.)?lava\.top\//;
```

Matches:
- `https://lava.top/...`
- `https://gate.lava.top/...`
- `https://app.lava.top/...`
- `https://pay.lava.top/...`

Rejects everything else including:
- Any other host (`https://evil.com/lava.top/`)
- HTTP scheme (`http://lava.top/`)
- Hosts without an explicit path (`https://lava.top` — no trailing slash; would never match)
- Sub-subdomains the backend hasn't documented (`https://x.gate.lava.top/`)

If the backend ever returns an unrecognised payment-URL host, the user sees an inline `errors.network` message and the click is NOT followed — same UX as a 5xx, no silent attacker-redirect. The regex MUST be revisited if lava.top introduces a new authorised checkout subdomain.

## Polling Numbers (CONTEXT D-21 verbatim — DO NOT change without revising D-21)

| Knob                   | Value      | Notes                                                                                                       |
| ---------------------- | ---------- | ----------------------------------------------------------------------------------------------------------- |
| `INTERVAL_MS`          | `2000`     | 2 seconds — fast enough to feel responsive, slow enough to keep escalate-mode lava calls cheap              |
| `ESCALATE_AFTER_POLL`  | `6`        | Polls 1-5 use backend cache only (`/api/v1/invoices/:id`); polls 6+ add `?escalate=true` to force lava read |
| `TIMEOUT_MS`           | `30000`    | 30 seconds total — after this, surface "Still processing your payment…" with a manual refresh button       |
| Effective max polls    | 15         | `TIMEOUT_MS / INTERVAL_MS = 15` — natural upper bound on backend hits per page-load                         |

Status casing normalisation (mirrors backend's `mapLavaStatusToLocal`):

```ts
const status = (json?.status ?? "").toString().toLowerCase();
```

Handles `"PAID"` / `"Paid"` / `"paid"` identically.

## /pay/fail Reason Allow-list (T-04-07-05 closure)

```ts
function safeReason(r: string | undefined): FailReason {
  return r === "declined" || r === "cancelled" ? r : "default";
}
```

The three legitimate values map to i18n keys:

| URL `?reason=` | Rendered key              | Copy summary                                                |
| -------------- | ------------------------- | ----------------------------------------------------------- |
| `declined`     | `pay.fail.body.declined`  | "Your bank declined the charge. Try a different card."      |
| `cancelled`    | `pay.fail.body.cancelled` | "You cancelled the payment. Pick a plan when you're ready." |
| `default`      | `pay.fail.body.default`   | "Your card was not charged. Please try again…"              |
| anything else  | `pay.fail.body.default`   | (silent fallback — never renders attacker-controlled text)  |

Extending the allow-list requires both adding a new value to the `FailReason` union AND adding the matching translation key to all three locale files (en/ru/es). The page intentionally fails-safe — an unknown reason renders the default copy rather than leaking the raw query value.

## B2 / D-17 Closure — Force-Refresh on `status === "paid"`

Before this plan, even with Plan 03's refresh-time rv_user re-issue in place, a user who paid would see "Free" on `/dashboard` until their next natural rv_at rotation (up to 5 minutes). Reason: the user has a valid rv_at JWT minted BEFORE the webhook landed; nothing triggers a refresh until that JWT expires.

This plan adds the trigger:

```ts
if (status === "paid") {
  stop();
  // B2/D-17 — force refresh BEFORE flipping to active so /dashboard
  // reads the fresh planId on the user's next navigation.
  await forceRefreshForNewPlanId();
  setView("active");
  return;
}
```

`forceRefreshForNewPlanId` POSTs `/api/v1/auth/refresh` (through the Plan 03 proxy) with a 5-second AbortSignal timeout. The proxy:
1. Calls the backend's `/api/v1/auth/refresh` with the current rv_rt.
2. Decodes `plan_id` from the new `access_token` JWT (the post-payment JWT carries `plan_id: "pro"`).
3. Atomically writes new `rv_at` + `rv_rt` + `rv_user` cookies (with `email` carried forward + `planId` from the new JWT).

By the time the UI flips to "Pro is active!" and the user clicks Continue, `getSession()` on the next page returns `{ isAuthed: true, email, planId: "pro" }` and the dashboard renders the Pro badge immediately.

### Non-fatal failure handling

The fetch is wrapped in `try { ... } catch { /* swallowed */ }`. Rationale:

- The "active" UI is the user-facing signal that their payment went through. Blocking it on a transient refresh failure (network blip, brief backend hiccup) would leave the user staring at the spinner indefinitely.
- The natural rv_at expiry (≤5 min) picks up the new plan_id on the user's next normal request. Worst-case staleness is bounded.
- The cookie side-effects are the only thing that matters — if they succeeded, great; if they didn't, the next request triggers a refresh anyway via the Plan 03 proxy's normal 401 path.

The same `forceRefreshForNewPlanId` is invoked from the manual `refresh()` button (single-shot retry after the 30s timeout) so even users who hit the "Still processing" UI get the same plan_id freshness once their payment finally lands.

## Deviations from Plan

### Auto-fixed Issues

None — Tasks 1-3 executed as written.

### Pre-existing out-of-scope blocker

The landing's `npm run build` currently fails with:

> Conflicting route and page at /auth/callback: route at /auth/callback/route and page at /auth/callback/page

This conflict was introduced by Plan 04-04 commit `b8b347d` which intentionally shipped BOTH a `route.ts` (POST handler for form_post OAuth) and a `page.tsx` (GET wrapper for operator hand-testing) at the same path. Next.js 16 rejects this configuration.

The conflict is NOT caused by Plan 04-07 changes (Plan 04-07 only modifies `pricing-client.tsx` + creates files under `/pay/success/`, `/pay/fail/`, and `components/app/payment-{status,fail}-card.tsx`). The conflict pre-exists this plan's commits and would surface regardless of any 04-07 work.

Documented in `.planning/phases/04-landing-surfaces/deferred-items.md` for the orchestrator and Plan 04-08 to action. Type safety of Plan 04-07's actual deliverables is verified via `npx tsc --noEmit` which exits 0 (see Verification Evidence).

### CLAUDE.md / Project-Convention Adjustments

None — CLAUDE.md's GSD workflow enforcement was already in motion via the orchestrator. The locked tech-stack (Next.js 16 + next-intl + Tailwind 4) and the Phase 4 D-* decisions were honored throughout.

## Authentication Gates

None during execution — Plan 04-07's deliverables are purely client + server-component plumbing. The lava.top, Apple, and Google credentials needed to exercise the full flow end-to-end are operator tasks (already documented in Plan 04-04's "Provider Dashboard Config Requirements" section) and Plan 04-08's smoke tests.

## Contracts Established (for downstream plans)

**PricingClient prop shape** (consumed by `/pricing/page.tsx`):

```ts
type Props = {
  locale: string;
  plan?: string;       // From URL ?plan=
  period?: string;     // From URL ?period= ("monthly" | "yearly")
  checkout?: string;   // From URL ?checkout= — fires auto-checkout when "auto"
  currency: string;    // Already-resolved currency (USD/EUR/RUB)
};
```

**PaymentStatusCard prop discriminator** (consumed by `PollClient`):

```ts
type Props =
  | { state: "processing"; invoiceId: string }
  | { state: "active"; invoiceId: string }
  | { state: "takingLonger"; invoiceId: string; onRefresh: () => void };
```

The card is a pure presentation component — render any state in isolation for storybook / tests.

**PaymentFailCard prop shape** (consumed by `/pay/fail/page.tsx`):

```ts
type Props = { reason: FailReason; tryAgainHref: string };
export type FailReason = "default" | "declined" | "cancelled";
```

Page computes `tryAgainHref` from URL params (preserves plan/period/currency); card renders it.

**Polling URL contract** (for backend HOTFIX-03 / 03-05 to honor):

- `GET /api/v1/invoices/:id` — cache-friendly, no lava call
- `GET /api/v1/invoices/:id?escalate=true` — force backend → lava lookup
- 401 → session expired (client redirects to /login)
- 404 → ownership mismatch (per 03-05) — client redirects to /pay/fail?reason=default
- 2xx body must include `{ status: "paid" | "pending" | "failed" | "cancelled" }` (any casing accepted)

**Force-refresh contract** (consumed by both pollOnce and the manual refresh button):

- POST `/api/v1/auth/refresh` via the Plan 03 catch-all proxy
- 5-second timeout, errors swallowed
- Side-effect: Plan 03 proxy rotates rv_at/rv_rt and re-issues rv_user with new plan_id

## Verification Evidence

**Type checking** (passes — exit 0):

```bash
cd landing && npx tsc --noEmit
```

Exit 0 with no output, confirming the new files in all 3 tasks are type-safe against the existing codebase (`@/lib/session`, `@/i18n/navigation`, `@/components/ui/{card,button}`, `@/lib/constants`, `@/lib/utils`, `next-intl/server`, `next-intl`, `react`, `lucide-react`).

**Build** (BLOCKED by pre-existing Plan 04 conflict — documented in deferred-items.md):

```bash
cd landing && BACKEND_API_URL=http://x ... npm run build
# → Conflicting route and page at /auth/callback (NOT caused by Plan 04-07)
```

**Task 1 grep AC** (all pass):

```bash
grep -cn '"use client"' .../pricing-client.tsx                           → 1
grep -cn '/api/v1/checkout' .../pricing-client.tsx                       → 2 (docstring + fetch)
grep -cn 'method: "POST"' .../pricing-client.tsx                         → 1
grep -cnE 'plan_code|periodicity' .../pricing-client.tsx                 → 3
grep -cn 'lava\.top' .../pricing-client.tsx                              → 1 (regex line)
grep -cnE 'status === 401|r\.status === 401' .../pricing-client.tsx     → 1
grep -cnE 'checkout !== "auto"|checkout === "auto"' .../pricing-client.tsx → 1
```

**Task 2 grep AC** (all pass):

```bash
grep -cn 'INTERVAL_MS = 2000' .../poll-client.tsx                        → 1
grep -cn 'ESCALATE_AFTER_POLL = 6' .../poll-client.tsx                   → 1
grep -cn 'TIMEOUT_MS = 30000' .../poll-client.tsx                        → 1
grep -cn 'escalate=true' .../poll-client.tsx                             → 3 (poll + refresh + docstring)
grep -cn '/api/v1/invoices/' .../poll-client.tsx                         → 4
grep -cn '/api/v1/auth/refresh' .../poll-client.tsx                      → 2 (docstring + fetch)
grep -cn 'forceRefreshForNewPlanId' .../poll-client.tsx                  → 3 (definition + pollOnce + refresh)
grep -cnE '"paid"|"failed"|"cancelled"|"pending"' .../poll-client.tsx    → 14 (all statuses + docstrings)
grep -cn 'toLowerCase' .../poll-client.tsx                               → 2 (poll + refresh)
grep -cn 'getSession' .../pay/success/page.tsx                           → 2
grep -cn 'PaymentStatusCard' .../components/app/payment-status-card.tsx  → 2
grep -cnE 'pay\.success\.takingLonger|...|pay\.success' .../payment-status-card.tsx → 9
```

**Task 3 grep AC** (all pass):

```bash
grep -cn 'pay\.fail' .../components/app/payment-fail-card.tsx            → 1
grep -cnE 'body\.default|body\.declined|body\.cancelled' .../payment-fail-card.tsx → 4
grep -cn 'SUPPORT\.telegram' .../payment-fail-card.tsx                   → 1
grep -cnE 'safeReason|"declined"|"cancelled"|"default"' .../pay/fail/page.tsx → 5
grep -cnE 'tryAgainHref|/pricing' .../pay/fail/page.tsx                  → 8
```

## Threat Register Closure

All thirteen dispositions from `<threat_model>` honored:

| Threat       | Closure                                                                                                                                                       |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| T-04-07-01   | `LAVA_URL_PATTERN` regex whitelist before `window.location.href = url` in PricingClient                                                                       |
| T-04-07-02   | Transferred — backend PAY-08 derives tier from lava `offerId`, not client metadata                                                                            |
| T-04-07-03   | Transferred — backend returns 404 (not 403) on invoice ownership mismatch; client treats 404 as redirect-to-fail                                              |
| T-04-07-04   | `TIMEOUT_MS = 30000` hard cap + `ESCALATE_AFTER_POLL = 6` amortises lava calls; max 15 polls per page-load                                                    |
| T-04-07-05   | `safeReason()` allow-list of {default, declined, cancelled}                                                                                                    |
| T-04-07-06   | Accepted — invoiceId is per-user ownership-checked; standard payment-flow UX                                                                                  |
| T-04-07-07   | React auto-escapes all `{t(...)}` calls; no `dangerouslySetInnerHTML` anywhere                                                                                |
| T-04-07-08   | Accepted — client-side timeout is purely UX; status comes from server                                                                                          |
| T-04-07-09   | SameSite=Strict cookies (Plan 03) + JSON body Content-Type — cross-site form-CSRF cannot include cookies + cannot produce JSON body                            |
| T-04-07-10   | `/pay/success/page.tsx` server-gates on `getSession()` and redirects to `/login?next=/pay/success?invoiceId=X`                                                |
| T-04-07-11   | Transferred — Phase 3 03-05 ownership check happens before escalate; backend won't call lava on a non-owned invoice                                            |
| T-04-07-12   | Transferred — backend refresh-rotation is transactional (HOTFIX-05); second call with same rv_rt fails. Plan 03 also has one-shot recursion guard             |
| T-04-07-13   | Transferred — Plan 03 proxy writes all three cookies atomically; even if rv_user re-issue's JWT decode returns "" defensive, prior rv_user.planId is fallback |

## Follow-Up Todos (for /gsd-note or operator)

- **Plan 04-04 /auth/callback conflict** — see `.planning/phases/04-landing-surfaces/deferred-items.md`. Recommended fix: delete `page.tsx`, keep `route.ts`. Blocking `npm run build` until resolved.
- **Plan 04-08 Playwright smokes** must exercise:
  - Logged-in click on /pricing "Get Pro" → POST /api/v1/checkout → window.location.href to lava.top URL (whitelist check)
  - Logged-out click → /login → sign-in → /pricing?...&checkout=auto → POST fires exactly once
  - /pay/success polling cadence (mock /invoices/:id with pending×5 then paid; assert poll 1-5 use cheap URL, poll 6+ add ?escalate=true)
  - B2 verification: mock paid response then assert POST /api/v1/auth/refresh fires BEFORE "Pro is active!" view renders
  - 30s timeout → "Still processing your payment…" UI; manual refresh single-shots
  - /pay/fail with each ?reason= value renders matching body copy
  - /pay/fail with malicious ?reason=<script> falls back to "default" silently
  - Try-again link from /pay/fail preserves ?plan=&period=&currency=
- **Lava.top URL host audit** — if lava ever introduces a new authorised checkout subdomain (e.g. `https://checkout.lava.top/`), extend `LAVA_URL_PATTERN` accordingly. Subdomain additions require this regex update or the user will see `errors.network` on a legitimate redirect.
- **Backend ?escalate=true rate-limit verification** — Plan 03-06 documented a per-IP rate limit on escalate calls. Plan 04-08 should exercise the rate-limit path to ensure malicious polling can't exhaust lava-side quotas.

## Known Stubs

None — every value in Plan 04-07's deliverables is wired to a real source:

- PricingClient POSTs real `/api/v1/checkout` (through Plan 03 proxy) and follows real lava `payment_url`
- PollClient hits real `/api/v1/invoices/:id` and real `/api/v1/auth/refresh`
- PaymentStatusCard renders real i18n strings from `pay.success.*` namespace (Plan 01)
- PaymentFailCard renders real i18n strings from `pay.fail.*` namespace (Plan 01)
- `SUPPORT.telegram` in both fail card and timeout card is the real operator handle (`https://t.me/flawlssr`)
- Cookie / session reads in /pay/success page.tsx use real `getSession()` (Plan 02 + Plan 03)

The only "no-op" path is `PricingClient`'s early `return null` when `?checkout=auto` is absent — that's the intended browse-pricing UX (the card grid handles the visible CTAs), not a stub.

## Threat Flags

None — Plan 04-07's deliverables stay within the threat surface already enumerated in `<threat_model>` (T-04-07-01 through T-04-07-13). No new network endpoints, auth paths, or trust boundaries beyond the documented checkout / invoice-polling / refresh trigger.

## Self-Check: PASSED

- landing/src/app/[locale]/(app)/pricing/pricing-client.tsx: FOUND (replaced stub with real checkout flow)
- landing/src/app/[locale]/(app)/pay/success/page.tsx: FOUND (auth-gated server page)
- landing/src/app/[locale]/(app)/pay/success/poll-client.tsx: FOUND (D-21 state machine + B2 force-refresh)
- landing/src/app/[locale]/(app)/pay/fail/page.tsx: FOUND (reason allow-list + Try-again query preservation)
- landing/src/components/app/payment-status-card.tsx: FOUND (3-state discriminated union)
- landing/src/components/app/payment-fail-card.tsx: FOUND (reason-aware body + Telegram fallback)
- .planning/phases/04-landing-surfaces/deferred-items.md: FOUND (Plan 04 /auth/callback conflict logged)
- Commit 0e622a0 (Task 1 — PricingClient): FOUND
- Commit a9e30f2 (Task 2 — /pay/success): FOUND
- Commit 04a25d3 (Task 3 — /pay/fail): FOUND
- npx tsc --noEmit: EXIT 0 (all 3 tasks type-safe)
- npm run build: BLOCKED by pre-existing Plan 04 /auth/callback conflict (deferred, NOT caused by 04-07)
