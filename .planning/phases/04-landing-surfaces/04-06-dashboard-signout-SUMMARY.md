---
phase: 04-landing-surfaces
plan: 06
subsystem: landing
tags: [dashboard, auth-gated, signout, base-ui-dialog, server-gating]
dependency_graph:
  requires:
    - "landing/src/lib/session.ts (Plan 02 + Plan 03 — getSession() with HMAC-verified rv_user)"
    - "landing/src/lib/cookies.ts (Plan 03 — COOKIE_NAMES)"
    - "landing/src/i18n/navigation.ts (next-intl 4.9.1 createNavigation — Link, useRouter, redirect)"
    - "landing/src/messages/{en,ru,es}.json — dashboard.* + auth.signOut.confirm.* + navbar.app.* (Plan 01 populated)"
    - "landing/src/components/ui/card.tsx + button.tsx (Plan 02)"
    - "landing/src/components/app/tier-badge.tsx (Plan 02)"
    - "landing/src/lib/constants.ts → SUPPORT.telegram (Phase 1 inherited)"
    - "@base-ui/react/dialog (1.4.1 — same primitive Sheet wraps)"
    - "Plan 04-05's fetchPlans + currencyForLocale (created inline this plan as a worktree-merge resolution; see Deviations)"
  provides:
    - "/<locale>/dashboard server-gated page (WEB-03)"
    - "DashboardCard server component (email + plan + context CTA)"
    - "SignOutButton client component (base-ui Dialog destructive confirm + POST /api/auth/logout)"
    - "Minimal landing/src/lib/plans.ts + locale-currency.ts (interim — Plan 04-05 sibling worktree owns these)"
  affects:
    - "WEB-09 / SC #6 — the dashboard surface is what makes 'signed in' tangible after Apple/Google sign-in"
    - "Plan 07 (Pay success) — after /pay/success forces a refresh, /dashboard is the first page that reads the new rv_user.planId"
    - "D-17 closure — /dashboard is the read-only consumer of the rv_user freshness pipeline (Plan 03 refresh re-issue + Plan 07 post-paid forced refresh)"
tech_stack:
  added: []
  patterns:
    - "Server-component page reads HttpOnly cookies via getSession() and gates render with next-intl's locale-aware redirect() — no client-side flash of unauthed content"
    - "next-intl redirect typed as `(...) => never`; we `return redirect(...)` explicitly so TS narrows the Session discriminated union without depending on inferred never-throw flow analysis"
    - "Destructive-confirm pattern via base-ui Dialog (same primitive Sheet uses) — awaits the logout POST BEFORE useTransition start() so Set-Cookie clears land before navigation triggers"
    - "Manage-Subscription fallback: until backend's /api/v1/subscription/manage-url ships (D-16), Pro users get an outbound link to SUPPORT.telegram with rel=noopener noreferrer (T-04-06-06 mitigation)"
key_files:
  created:
    - "landing/src/components/app/dashboard-card.tsx"
    - "landing/src/app/[locale]/(app)/dashboard/signout-button.tsx"
    - "landing/src/app/[locale]/(app)/dashboard/page.tsx"
    - "landing/src/lib/locale-currency.ts (interim — owned by Plan 04-05)"
    - "landing/src/lib/plans.ts (interim — owned by Plan 04-05)"
  modified: []
decisions:
  - "Returned redirect() (vs called) inside the !session.isAuthed branch so TS narrows session to the authed variant for downstream property reads — avoids the discriminated-union TS2339 on session.email / session.planId after the gate"
  - "Created minimal versions of fetchPlans + currencyForLocale in this worktree because the wave-3 sibling Plan 04-05 owns the canonical implementations; orchestrator merge resolves identical shapes (same precedent as Plan 03's session.ts handling)"
  - "Pro-tier 'Manage Subscription' CTA points to https://t.me/flawlssr (SUPPORT.telegram) instead of leaving a dead button — Phase 4 fallback for D-16 until backend `/api/v1/subscription/manage-url` lands"
  - "SignOutButton awaits the fetch() BEFORE entering the useTransition start() so the Set-Cookie clears actually apply before router.replace triggers a re-render; the catch block intentionally swallows fetch errors because Plan 03 clears cookies on the response even when backend POST fails"
  - "Did NOT add a billing-history / device-list / invoices section per CONTEXT.md D-15 (minimal dashboard — email + plan + ONE CTA + Sign-out). Captured as Phase 7+ deferred scope"
metrics:
  duration: "~10 minutes wall clock (3 tasks, 3 commits, 1 deviation, 1 full build)"
  completed: "2026-05-24"
  tasks_completed: 3
  files_changed: 5
  commits: 3
---

# Phase 4 Plan 06: Dashboard + Sign Out Summary

Shipped the protected `/<locale>/dashboard` surface (WEB-03) — a server-side gated page that reads the HMAC-verified `rv_user` cookie via Plan 03's `getSession()`, looks up the user's current plan display name from the backend's `/api/v1/plans` catalog (with Plan 04-05's `fetchPlans` cache contract), and renders a single context-aware card: email + plan badge + a `Get Pro` CTA for Free users (next-intl Link to `/pricing`) or a `Manage Subscription` outbound link to Telegram support for Pro users (D-16 fallback until backend's `/api/v1/subscription/manage-url` ships). A `SignOutButton` client component above the card opens a base-ui Dialog destructive-confirm prompt; confirming POSTs to `/api/auth/logout` (Plan 03) and navigates the user back to `/` with all three session cookies cleared. The page is the read-only consumer of the D-17 plan_id freshness pipeline that Plan 03's refresh-time re-issue + Plan 07's post-paid forced refresh maintain — Pro flashes on `/dashboard` within one refresh cycle of payment success.

## Tasks Completed

| Task | Name                                                                                                          | Commit    | Files                                                                       |
| ---- | ------------------------------------------------------------------------------------------------------------- | --------- | --------------------------------------------------------------------------- |
| 1    | DashboardCard server component — email + plan + single context-aware CTA                                      | `17f7a81` | landing/src/components/app/dashboard-card.tsx                               |
| 2    | SignOutButton client component with base-ui Dialog destructive confirm + POST /api/auth/logout                | `2618536` | landing/src/app/[locale]/(app)/dashboard/signout-button.tsx                 |
| 3    | /dashboard page — server-gated via getSession(), fetchPlans lookup, renders DashboardCard + SignOutButton     | `ac2e5d2` | landing/src/app/[locale]/(app)/dashboard/page.tsx, lib/plans.ts (interim), lib/locale-currency.ts (interim) |

## Server-Gating Pattern

```ts
const session = await getSession();
if (!session.isAuthed) {
  return redirect({
    href: { pathname: "/login", query: { next: "/dashboard" } },
    locale,
  });
}
```

- `getSession()` is the single gate (Plan 02 + Plan 03 contract). It reads the HttpOnly `rv_at` cookie via `next/headers cookies()` and the HMAC-verified `rv_user` via Plan 03's `decodeSessionUser`. Returns a discriminated union: `{ isAuthed: false }` or `{ isAuthed: true; email; planId }`.
- `next-intl`'s `redirect` is typed `(...) => never`, but we **return** it explicitly so TypeScript narrows the `Session` union to the authed variant for the downstream `session.email` / `session.planId` reads. Without the `return`, TS would still emit TS2339 on those reads.
- The `next` query param is hard-coded as the literal string `"/dashboard"` — never sourced from any untrusted input. Plan 04's login page (when it lands) validates `next` against an `isSafeNextPath` allow-list before honoring it on the post-login redirect; for this plan there's nothing to validate because the value is a compile-time constant.

## Manage Subscription Fallback (D-16 / T-04-06-06)

Backend's `GET /api/v1/subscription/manage-url` endpoint **does not exist** in Phase 3 (verified — `grep -r "subscription/manage-url" server/api/` returns 0 matches). The full design would call lava.top's billing-portal API, but that's deferred.

Phase 4 fallback:

```tsx
isPro ? (
  <a
    href={SUPPORT.telegram}
    target="_blank"
    rel="noopener noreferrer"
    className={buttonVariants({ size: "lg" }) + " w-full"}
  >
    {t("cta.manage")}
  </a>
) : (
  <Link href="/pricing" className={buttonVariants({ size: "lg" }) + " w-full"}>
    {t("cta.getPro")}
  </Link>
)
```

`SUPPORT.telegram` is `https://t.me/flawlssr` (from `landing/src/lib/constants.ts`). `rel="noopener noreferrer"` closes T-04-06-06 (Telegram-side referrer leak). The `i18n` key `dashboard.cta.manage` is already populated in all three locale files by Plan 01.

**Follow-up todo** (for `/gsd-note` or operator): when backend ships `/api/v1/subscription/manage-url`, swap the Telegram fallback for a Link to that URL. The component prop surface (just `planCode`) doesn't change; we just toggle the JSX branch.

## Confirmation Dialog Markup

base-ui Dialog primitives wired the same way Plan 04 (login) will use them indirectly via `Sheet`:

```tsx
<Dialog.Root open={open} onOpenChange={setOpen}>
  <Dialog.Trigger render={<Button variant="ghost" size="sm">…Sign out</Button>} />
  <Dialog.Portal>
    <Dialog.Backdrop className="…bg-black/50 backdrop-blur" />
    <Dialog.Popup className="…rounded-[var(--radius-xl)] bg-surface-elevated">
      <Dialog.Title>…Sign out?</Dialog.Title>
      <Dialog.Description>…You'll need to sign in again…</Dialog.Description>
      <Dialog.Close render={<Button variant="ghost">…Cancel</Button>} />
      <Button variant="destructive" onClick={performSignOut}>…Sign out</Button>
    </Dialog.Popup>
  </Dialog.Portal>
</Dialog.Root>
```

`performSignOut` awaits the `POST /api/auth/logout` fetch **before** entering the `useTransition start()` block — the order matters: the Set-Cookie clears need to land before `router.replace("/")` triggers the (app) layout re-render, otherwise `NavbarApp` could briefly render the logged-in branch during the navigation transition. The catch block intentionally swallows fetch errors because Plan 03's logout route returns 204 + Max-Age=0 cookies even when the backend `/auth/logout` POST fails (D-25: browser must never be left "stuck signed in").

## D-17 Freshness Pipeline Reference

`/dashboard` is a **pure read-only consumer** of the `rv_user.planId` freshness pipeline that other plans maintain:

| Upstream                                | What it guarantees                                                                                                                                                                                                            |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Plan 03 (Node proxy refresh rotation)   | Every time the proxy hits a 401 with `rv_rt` present, it calls `/api/v1/auth/refresh`, decodes the new access JWT's `plan_id` claim, and re-writes `rv_user` with `{ email: prior, planId: decoded }` at the 30-day TTL. |
| Plan 07 (Pay success forced refresh)    | After lava.top redirects the user back to `/pay/success` with `status=paid`, the page client-side triggers a refresh-rotation so the new Pro plan_id lands in `rv_user` BEFORE the user clicks "Continue to dashboard".      |
| Plan 04 (OAuth callback initial issue)  | Sets `rv_user` from the access JWT's `plan_id` claim on sign-in.                                                                                                                                                              |

The dashboard never WRITES to that pipeline. It only reads `session.planId` (via `getSession()` → HMAC-verified `rv_user` → `decodeSessionUser`), looks up the matching plan in the catalog, and renders. Staleness window is bounded by the refresh-token rotation cadence (≤ 5 min between natural rotations) plus the post-paid forced refresh (<2s of returning from `/pay/success`). T-04-06-08 closure.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Created landing/src/lib/plans.ts and landing/src/lib/locale-currency.ts inline**

- **Found during:** Task 3 (the page imports `fetchPlans` from `@/lib/plans` and `currencyForLocale` from `@/lib/locale-currency`, both of which are listed in Plan 04-05's `key_files.created`).
- **Issue:** Plan 04-05 is a sibling wave-3 plan running in parallel (same orchestrator wave); its worktree has not merged into this one. Without those files, the page's TypeScript phase fails with TS2307 (module not found) and the build cannot complete.
- **Fix:** Created minimal versions in this worktree matching Plan 04-05's spec exactly (`fetchPlans(currency)` returning `Plan[]` with `next: { tags: ["plans"], revalidate: 600 }`; `currencyForLocale(locale, override?)` returning `"USD" | "EUR" | "RUB"` with the D-04 default mapping; `formatPrice(amount, currency, locale)` using `Intl.NumberFormat`). Same precedent as Plan 03's `session.ts` handling — the orchestrator's merge of the wave-3 worktrees will resolve to identical shapes.
- **Why not in plan:** The plan declares `depends_on: [04-02, 04-03]` (not 04-05), so the parallel wave-3 scheduling assumes 04-05 also lands in the same merge. Rule 3 auto-fix applies because the missing import is a build blocker caused by the plan's own action (the planned page body references those helpers explicitly).
- **Files modified:** `landing/src/lib/plans.ts` (new), `landing/src/lib/locale-currency.ts` (new)
- **Commit:** folded into the Task 3 commit (`ac2e5d2`) because both helpers are required for the page to compile.

**2. [Rule 1 - Bug] TS2339 on session.email / session.planId after redirect()**

- **Found during:** Task 3 (`tsc --noEmit`)
- **Issue:** `getSession()` returns a discriminated union `{ isAuthed: false } | { isAuthed: true; email; planId }`. Inside `if (!session.isAuthed) { redirect(...); }`, TS sees `redirect` as a normal function call (it doesn't infer the union narrowing from a bare `redirect(...)` statement even though the type is `(...) => never`). After the `if` block, TS still considered `session` as the full union, so `session.planId` and `session.email` reads later raised TS2339.
- **Fix:** Changed `redirect(...)` to `return redirect(...)`. With the explicit `return`, TS narrows the union for the code path that follows the `if` block.
- **Files modified:** `landing/src/app/[locale]/(app)/dashboard/page.tsx`
- **Commit:** folded into the Task 3 commit (`ac2e5d2`).

**3. [Rule 3 - Blocking] Removed stale untracked `[locale]/page.tsx`, `[locale]/opengraph-image.tsx`, `[locale]/privacy/`, `messages/uz.json` left over in the worktree**

- **Found during:** Task 3 (`next build`)
- **Issue:** Next 16 raised `You cannot have two parallel pages that resolve to the same path. Please check /[locale]/(marketing)/privacy and /[locale]/privacy.` Plan 02 moved the marketing pages INTO the `(marketing)/` route group, but the worktree's working tree still had untracked copies of the pre-move files at the parent `[locale]/` level (along with the deleted `messages/uz.json`). These were not git-tracked but Next.js's filesystem router still picked them up.
- **Fix:** `git clean -fd` against the specific stale paths to remove them. They were already removed from git's index in the commit graph; only the working tree had leftover copies.
- **Files modified:** none (deletions of untracked artifacts only)
- **Commit:** none required (working-tree cleanup, not a code change)

### CLAUDE.md / Project-Convention Adjustments

None — no CLAUDE.md rules conflicted with the plan. CLAUDE.md's GSD workflow enforcement is in motion via the orchestrator that spawned this executor.

## Authentication Gates

None — no third-party auth required for any task in this plan. All work is server-component plumbing + a client-side dialog.

## Key Decisions Made

- **`return redirect(...)` over bare call:** TS narrowing on the discriminated `Session` union depends on the redirect call being recognized as a terminating statement. The explicit `return` makes that contract structural rather than relying on inferred never-throw flow analysis (which differs across TS versions).
- **Telegram fallback over a dead Manage button:** Pro users with a real subscription should always have a path to support. Until the backend's billing-portal endpoint ships, the Telegram support link is a graceful degradation; tracked as a follow-up to swap when the backend endpoint lands.
- **`fetch` BEFORE `useTransition start()`:** The natural shape would be `start(async () => { await fetch(...); router.replace(...) })`, but that risks the navigation triggering before the response's Set-Cookie clears land in document.cookie. Awaiting outside the transition guarantees ordering.
- **Plan 04-05 helper interim provisioning:** Created minimal versions matching the spec exactly (same currency union, same `fetchPlans` cache contract, same locale defaults). When Plan 04-05's worktree merges, the orchestrator sees identical content and the merge is a no-op.
- **No billing history / device list:** CONTEXT.md D-15 locks the dashboard scope to email + plan + ONE CTA + Sign-out. Anything else is deferred to Phase 7+.

## Contracts Established (for downstream plans)

**DashboardCard prop surface (consumed by /dashboard page and future test renderers):**

```tsx
type Props = {
  email: string;          // From session.email via getSession()
  planCode: string;       // "free" | "pro" | other system codes
  planDisplayName: string; // Resolved by parent from /api/v1/plans lookup
};

<DashboardCard email={…} planCode={…} planDisplayName={…} />
```

The card branches its CTA on `planCode === "pro"` (Pro → Telegram; non-Pro → /pricing). When the backend adds non-system plans (e.g. "starter"), they fall into the non-Pro branch by default — re-classify by extending the `isPro` check or threading `tier` directly from the backend.

**SignOutButton — zero props.** Reads its i18n strings from `auth.signOut.confirm.*` and `dashboard.signOut`. Drop it into any (app) page where sign-out is appropriate. The component is fully self-contained.

**/dashboard route surface:**

- `GET /<locale>/dashboard` with `rv_at` absent → `307 /<locale>/login?next=/dashboard`
- `GET /<locale>/dashboard` with `rv_at` present (any `rv_user` state) → `200` rendering the card
- Plan 08's smoke tests will exercise both paths.

## Verification Evidence

- `cd landing && node ./node_modules/typescript/bin/tsc --noEmit` exits 0
- `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y ./node_modules/next/dist/bin/next build` exits 0
  - Build output lists `ƒ /[locale]/dashboard` as a Dynamic route (force-dynamic inherited from the (app) layout)
  - 21 pages emitted total; marketing prerender (3 locales × OG image + privacy + home + manifest etc.) preserved
- All Task 1 grep acceptance checks pass:
  - `grep -n 'TierBadge' landing/src/components/app/dashboard-card.tsx` → 3 matches (≥ 1 required)
  - `grep -n 'isPro' landing/src/components/app/dashboard-card.tsx` → 4 matches (≥ 2 required)
  - `grep -n 'SUPPORT.telegram' landing/src/components/app/dashboard-card.tsx` → 1 match
  - `grep -n 'href="/pricing"' landing/src/components/app/dashboard-card.tsx` → 1 match
  - `grep -n 'target="_blank"' landing/src/components/app/dashboard-card.tsx` → 2 matches incl. docstring
  - `grep -n 'rel="noopener noreferrer"' landing/src/components/app/dashboard-card.tsx` → 2 matches incl. docstring
- All Task 2 grep acceptance checks pass:
  - `grep -n '"use client"' ...signout-button.tsx` → 1 match
  - `grep -n '/api/auth/logout' ...signout-button.tsx` → 2 matches incl. docstring
  - `grep -n 'method: "POST"' ...signout-button.tsx` → 1 match
  - `grep -n 'router.replace("/")' ...signout-button.tsx` → 2 matches incl. docstring
  - `grep -n 'Dialog.Root\|Dialog.Trigger\|Dialog.Popup' ...signout-button.tsx` → 4 matches
  - `grep -n 'destructive' ...signout-button.tsx` → 3 matches incl. docstring
- All Task 3 grep acceptance checks pass:
  - `grep -n 'getSession\|session.isAuthed' page.tsx` → 5 matches (≥ 2 required)
  - `grep -n 'redirect' page.tsx` → 6 matches (≥ 1 required)
  - `grep -n 'next: "/dashboard"' page.tsx` → 1 match
  - `grep -n 'fetchPlans' page.tsx` → 3 matches
  - `grep -n 'DashboardCard\|SignOutButton' page.tsx` → 4 matches (≥ 2 required)
  - `grep -n 'force-dynamic' page.tsx` → 2 matches incl. docstring (≥ 1 required)

## Threat Register Closure

All eight dispositions from `<threat_model>` honored:

| Threat       | Closure                                                                                                                                                                                       |
| ------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| T-04-06-01   | `getSession()` gate + `return redirect(...)` before any sensitive render in page.tsx                                                                                                          |
| T-04-06-02   | Accept — rv_user is HttpOnly + SameSite=Strict + HMAC-signed (Plan 03); email plaintext-in-cookie is user-known data, not a secret                                                            |
| T-04-06-03   | React auto-escapes `{planDisplayName}` and `{email}` in JSX; no `dangerouslySetInnerHTML` anywhere in the new files                                                                           |
| T-04-06-04   | `rv_at` is SameSite=Strict (Plan 03); cross-site CSRF cannot include the cookie on a logout POST. Even if it could, logout is idempotent and the only side effect is the attacker's session  |
| T-04-06-05   | `next: "/dashboard"` is a compile-time literal in page.tsx — no untrusted input feeds the redirect query. Downstream Plan 04 will validate the value at the OAuth callback                   |
| T-04-06-06   | `target="_blank" rel="noopener noreferrer"` on the Telegram support link                                                                                                                       |
| T-04-06-07   | `fetchPlans` uses `next: { tags: ["plans"], revalidate: 600 }` so the backend hit is deduped across all /dashboard renders for a given currency. Backend also has its own PAY-12 60s cache  |
| T-04-06-08   | Mitigated cross-plan via Plan 03 refresh re-issue + Plan 07 post-paid forced refresh; /dashboard is a read-only consumer of that pipeline                                                     |

## W3 Deferred Scope (Phase 7+)

Billing history (last N invoices), device list, and the lava.top billing-portal "Manage Subscription" link are **intentionally EXCLUDED** from Phase 4 per CONTEXT.md D-15 locked decision. The dashboard's Phase 4 contract is **email + plan + ONE CTA + Sign-out** — anything beyond that is deferred. Tracking items:

- **Billing history:** waiting on a backend `/api/v1/invoices?user=me` endpoint (does not exist). Phase 7+.
- **Device list:** waiting on a backend `/api/v1/sessions?user=me` endpoint (sessions table exists from Phase 1 hotfix, but no list endpoint). Phase 7+.
- **Real "Manage Subscription" URL:** waiting on a backend `/api/v1/subscription/manage-url` endpoint that proxies to lava.top's billing portal API. Phase 7+. Until then, the Phase 4 Telegram fallback ships.

## Follow-Up Todos (for /gsd-note or operator)

- **Plan 04-05 worktree merge:** when the orchestrator merges the wave-3 worktrees, verify that this plan's `landing/src/lib/plans.ts` + `locale-currency.ts` align with Plan 04-05's canonical versions. If they differ in shape, the merge resolution wins — but the contract this plan consumes (`fetchPlans(currency)` returning `Plan[]` with `code` + `name`; `currencyForLocale(locale)` returning `Currency`) must remain intact.
- **Backend `/api/v1/subscription/manage-url`:** when this endpoint ships (Phase 7+?), swap the Telegram fallback in `dashboard-card.tsx` for a Link to the returned URL. No prop changes required.
- **Stale-file cleanup in worktree base:** the worktree had untracked stale files left over from Plan 02's marketing route-group move (`[locale]/page.tsx`, `[locale]/privacy/`, `[locale]/opengraph-image.tsx`, `messages/uz.json`). They've been cleaned in this worktree but may resurface in other parallel worktrees — orchestrator merge should not re-introduce them since they are not git-tracked.
- **Plan 04-05 plan card "Current plan" state:** when Plan 04-05 ships, its PlanCard will need to identify the user's current plan via `getSession().planId`. This plan demonstrated the lookup pattern (`plans.find(p => p.code === planCode)`) which Plan 04-05 can reuse.

## Known Stubs

None — every value rendered by these components is wired to a real source:

- `email` comes from `session.email` (HMAC-verified `rv_user` cookie via Plan 03's `decodeSessionUser`)
- `planCode` comes from `session.planId` (same source; kept fresh by Plan 03's refresh re-issue + Plan 07's force refresh)
- `planDisplayName` is resolved via `fetchPlans()` lookup against the backend's plans catalog (real backend call; only fallback when not found is the raw planId string — which is itself a real backend-issued code)
- The "Manage Subscription" Telegram link is the operator's real Telegram support handle (`https://t.me/flawlssr` from `landing/src/lib/constants.ts`)
- Sign-out POSTs to Plan 03's real `/api/auth/logout` route handler (returns 204 + Set-Cookie clears even on backend failure)

The empty-string fallback (`email || "—"`) when `rv_user` decode fails is intentional graceful degradation per Plan 03's contract — not a stub.

## Threat Flags

None — no new security surface introduced beyond what the plan's `<threat_model>` already enumerated. T-04-06-01 through T-04-06-08 dispositions were honored.

## Self-Check: PASSED

- landing/src/components/app/dashboard-card.tsx: FOUND
- landing/src/app/[locale]/(app)/dashboard/signout-button.tsx: FOUND
- landing/src/app/[locale]/(app)/dashboard/page.tsx: FOUND
- landing/src/lib/locale-currency.ts: FOUND (interim — Plan 04-05 owns)
- landing/src/lib/plans.ts: FOUND (interim — Plan 04-05 owns)
- Commit 17f7a81 (Task 1 — DashboardCard): FOUND
- Commit 2618536 (Task 2 — SignOutButton): FOUND
- Commit ac2e5d2 (Task 3 — /dashboard page + interim helpers): FOUND
- node ./node_modules/typescript/bin/tsc --noEmit: EXIT 0
- node ./node_modules/next/dist/bin/next build (with .env.local supplying BACKEND_API_URL+REVALIDATE_SECRET): EXIT 0 (21 pages built, `/[locale]/dashboard` registered as Dynamic)
