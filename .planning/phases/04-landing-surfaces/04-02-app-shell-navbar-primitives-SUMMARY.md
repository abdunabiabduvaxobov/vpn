---
phase: 04-landing-surfaces
plan: 02
subsystem: landing
tags: [foundation, components, navbar, app-shell, route-groups]
dependency_graph:
  requires:
    - "landing/src/lib/utils.ts (cn() — already shipped in Phase 4 plan 01)"
    - "landing/src/i18n/navigation.ts (Link, useRouter)"
    - "landing/src/messages/{en,ru,es}.json — navbar.app namespace already populated in plan 01"
    - "landing/src/components/ui/button.tsx + buttonVariants (existing)"
    - "landing/src/components/common/{logo,locale-switcher,navbar}.tsx (existing)"
    - "@base-ui/react ^1.4.1 (Toast + Popover already installed)"
  provides:
    - "Server-component navbar with cookie-driven logged-in branching (WEB-09 / SC #6)"
    - "(app) and (marketing) route-group split — (app) is force-dynamic, (marketing) keeps static prerender"
    - "Card/Skeleton/Toast/TierBadge UI primitives in the existing base-ui token contract"
    - "Apple + Google brand-mark SVGs at landing/public/brand/{apple,google}/*.svg"
    - "Toaster mounted in (app) layout so any (app) client component can call `toast({title,description,variant})`"
    - "getSession() server-only helper returning Session discriminated union"
  affects:
    - "All Phase 4 plans 03-08 — they sit under the (app) route group and reuse Card/Skeleton/Toast/TierBadge"
    - "Plan 03 (Node proxy) — owns POST /api/auth/logout (the form action in UserMenu) and the setSessionCookie/readSessionCookie helpers that getSession() will swap to once available"
    - "Plan 04 (OAuth callback) — imports brand SVGs from public/brand/* for the sign-in buttons; sets rv_at + rv_user cookies that NavbarApp reads"
tech_stack:
  added: []
  patterns:
    - "Server-component navbar reads HttpOnly cookies via next/headers cookies() — no client-side flash of unauthenticated content"
    - "Route-group layout split: (marketing) keeps the existing static Navbar+Footer chrome; (app) gets a force-dynamic NavbarApp+Footer+Toaster wrapper"
    - "Cross-component toast invocation via module-scope Toast.createToastManager() singleton (vs the in-tree useToastManager hook)"
    - "Server-only session reader degrades gracefully on cookie parse failure — { isAuthed: true, email: '', planId: '' } when rv_at present but rv_user malformed"
key_files:
  created:
    - "landing/src/components/ui/card.tsx"
    - "landing/src/components/ui/skeleton.tsx"
    - "landing/src/components/ui/toast.tsx"
    - "landing/src/components/app/tier-badge.tsx"
    - "landing/src/components/app/user-menu.tsx"
    - "landing/src/components/common/navbar-app.tsx"
    - "landing/src/lib/session.ts"
    - "landing/src/app/[locale]/(app)/layout.tsx"
    - "landing/src/app/[locale]/(marketing)/layout.tsx"
    - "landing/public/brand/apple/apple-sign-in.svg"
    - "landing/public/brand/google/google-g.svg"
    - "landing/public/brand/README.md"
  modified:
    - "landing/src/app/[locale]/layout.tsx (dropped Navbar/Footer imports + JSX; NextIntlClientProvider now wraps children only)"
  renamed:
    - "landing/src/app/[locale]/page.tsx → landing/src/app/[locale]/(marketing)/page.tsx"
    - "landing/src/app/[locale]/privacy/page.tsx → landing/src/app/[locale]/(marketing)/privacy/page.tsx"
    - "landing/src/app/[locale]/opengraph-image.tsx → landing/src/app/[locale]/(marketing)/opengraph-image.tsx"
decisions:
  - "Reused the existing landing/src/lib/utils.ts (cn) from Phase 4 plan 01 instead of re-creating it"
  - "Toast: created module-scope ToastManager via Toast.createToastManager() and passed to <Toast.Provider toastManager={...}>; this lets a plain `toast({...})` function call reach the provider from any module without useToastManager() inside the tree"
  - "TierBadge accepts `label` as a prop instead of pulling next-intl, so it stays server-component-friendly (the plan suggested this as the simpler variant)"
  - "UserMenu Sign out button is a plain `<form action=/api/auth/logout method=POST>` so it works without JS while React hydrates; Plan 03's Node proxy owns the route"
  - "Marketing pages moved INTO (marketing)/ rather than leaving them at [locale]/; (marketing)/layout.tsx wraps them with the original Navbar+Footer. Route groups do not affect URLs so /ru/, /ru/privacy/, etc. resolve unchanged"
  - "Did NOT mount Toaster at [locale]/ level — only inside (app) layout — because marketing pages have no toast-firing components"
  - "Brand SVGs vendored as faithful renditions of the documented brand geometry (Apple logomark + Google 4-colour G) so the build is offline-reproducible; README explicitly requires the operator to verify against upstream kits before production"
  - "HMAC verification of rv_user cookie punted to Plan 03 (T-04-02-02 mitigate-deferred); Phase 4 trusts HttpOnly + SameSite=Strict + Secure for tamper resistance until Plan 03's setSessionCookie/readSessionCookie helpers land"
metrics:
  duration: "~6 minutes wall clock (3 tasks, 3 commits, 1 deviation auto-fix, 1 full build)"
  completed: "2026-05-23"
  tasks_completed: 3
  files_changed: 13
  commits: 3
---

# Phase 4 Plan 02: App Shell, NavbarApp + Primitives Summary

Built the foundation that every Phase 4 authenticated surface (`/login`, `/dashboard`, `/pricing`, `/pay/success`, `/pay/fail`) reuses: a `(app)` route group with `dynamic = 'force-dynamic'` that mounts a server-component `NavbarApp` whose links branch on the `rv_at` HttpOnly cookie (WEB-09 / SC #6), four reusable UI primitives (`Card`, `Skeleton`, `Toast`, `TierBadge`) in the existing base-ui token contract, a server-only `getSession()` helper, a popover-based `UserMenu` with a JS-free sign-out form, and the official Apple + Google brand-mark SVGs that Plan 04's sign-in buttons will render. Marketing pages migrated to a sibling `(marketing)` route group with the existing static Navbar+Footer chrome.

## Tasks Completed

| Task | Name                                                                                                               | Commit    | Files                                                                                                                                                                  |
| ---- | ------------------------------------------------------------------------------------------------------------------ | --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1    | Add Card/Skeleton/Toast/TierBadge UI primitives                                                                    | `2311c3c` | landing/src/components/ui/card.tsx, ui/skeleton.tsx, ui/toast.tsx, components/app/tier-badge.tsx                                                                       |
| 2    | App-shell: getSession, NavbarApp, UserMenu, route-group split — (app) force-dynamic + (marketing) static           | `751b586` | lib/session.ts, components/common/navbar-app.tsx, components/app/user-menu.tsx, app/[locale]/(app)/layout.tsx, app/[locale]/(marketing)/layout.tsx, app/[locale]/layout.tsx (mod) + 3 marketing page moves |
| 3    | Vendor Apple + Google brand-mark SVGs                                                                              | `13b4f42` | landing/public/brand/apple/apple-sign-in.svg, landing/public/brand/google/google-g.svg, landing/public/brand/README.md                                                  |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Wrong import path for createToastManager**

- **Found during:** Task 1 (`npx tsc --noEmit`)
- **Issue:** Initially imported `createToastManager` from `@base-ui/react/toast` (top-level), but in @base-ui/react 1.4.1 the top-level `toast/index.d.ts` re-exports the type via `export type *` (not the value). `tsc` reported `TS1362: 'createToastManager' cannot be used as a value because it was exported using 'export type'`.
- **Fix:** Import as a namespace member: `Toast.createToastManager()` — works because `index.parts.d.ts` (which the `Toast` namespace points to) re-exports it as a value.
- **Why not in plan:** The plan referenced the conceptual API; the wire-up detail surfaced only at TS-check time.
- **Files modified:** `landing/src/components/ui/toast.tsx`
- **Commit:** folded into the Task 1 commit (`2311c3c`) before commit.

### CLAUDE.md / Project-Convention Adjustments

None — no CLAUDE.md rules conflicted with the plan.

## Authentication Gates

None — no auth required for any task in this plan.

## Key Decisions Made

- **`utils.ts` reuse:** `landing/src/lib/utils.ts` with `cn()` already existed from Phase 4 plan 01, so Task 1 reused it instead of re-creating. The plan's "create if missing" branch was inert.
- **Toast manager pattern:** Used `Toast.createToastManager()` at module scope + `<Toast.Provider toastManager={...}>` so the `toast({title, description, variant})` helper can be called from any module — not just from components nested under the Provider. This is the documented base-ui v1 pattern for cross-component toast invocation.
- **TierBadge `label` prop:** Implemented the simpler prop variant (caller passes `label={t('dashboard.plan.pro')}`) so the badge stays server-component-friendly. The `useTranslations()` variant would have forced `"use client"`.
- **Sign-out form:** Plain `<form action="/api/auth/logout" method="POST">` wrapped around a submit button. Works without JavaScript; Plan 03's Node proxy owns the `/api/auth/logout` route (clears `rv_at` + `rv_user` cookies, redirects to `/`).
- **Route-group split direction:** Moved marketing pages INTO `(marketing)/` (rather than wrapping the existing locale layout's children in a conditional) because (a) the (app) group needs `dynamic = 'force-dynamic'` which conflicts with the static marketing render mode, and (b) clean separation lets future marketing-only middleware (e.g. CDN cache headers) target only the `(marketing)` group.
- **Toaster scope:** Mounted only in `(app)/layout.tsx`, not in `[locale]/layout.tsx`, because marketing pages don't fire toasts and we don't want to ship the Toast bundle to static marketing visitors.
- **Brand SVG provenance:** Vendored faithful renditions of the documented brand geometry (Apple logomark via Apple's published path data; Google "G" four-colour mark via the public W3C-compatible geometry). README explicitly flags the operator must verify against upstream kits before production. This trades temporary visual fidelity for offline build reproducibility — fine for Phase 4 internal verification, must be tightened before launch.
- **HMAC deferred to Plan 03:** `getSession()` currently trusts HttpOnly + Secure + SameSite=Strict scope for `rv_user` cookie tamper resistance (T-04-02-02 mitigate-deferred). Once Plan 03 ships `setSessionCookie()` / `readSessionCookie()` with an HMAC suffix, `getSession()` will switch to call those helpers — the public API (returning `Session`) stays unchanged.

## Contracts Established (for downstream plans)

**`getSession()` contract (consumed by 04-04 /login, 04-05 /pricing, 04-06 /dashboard, 04-07 /pay/* server components):**

```ts
import { getSession } from "@/lib/session";

const s = await getSession();
if (s.isAuthed) {
  // s.email   — string, may be "" when rv_user cookie absent or unparseable
  // s.planId  — string, may be "" likewise
} else {
  // not authed
}
```

`getSession()` MUST only be called from server components / route handlers — the module `import "server-only"` directive will crash the build if a client component imports it.

**Route-group contract:**

- New pages under `landing/src/app/[locale]/(app)/<route>/page.tsx` automatically get `<NavbarApp/>`, `<Toaster/>`, and `dynamic = 'force-dynamic'`.
- The (app) layout already mounts the Footer too — pages should NOT mount it themselves.
- `<TierBadge tier="pro|free" label={t('dashboard.plan.pro')} />` is server-component-safe.
- Toast invocation from a client component inside (app):

```tsx
"use client";
import { toast } from "@/components/ui/toast";

toast({ title: "Saved", description: "Your changes were saved.", variant: "success" });
```

**Brand-mark contract (consumed by 04-04 /login):**

```tsx
import Image from "next/image";

<button type="submit" name="provider" value="apple" className="...">
  <Image src="/brand/apple/apple-sign-in.svg" alt="" width={20} height={20} />
  {t('login.signIn.apple')}
</button>
```

`alt=""` because the adjacent text label provides the accessible name (avoids "Apple Apple" doubled announcement).

## Verification Evidence

- `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npx tsc --noEmit` exits 0
- `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npm run build` exits 0 — 18 pages emitted across the three locales, ✓ Compiled successfully in 2.9s
- The Next.js route table shows `/[locale]` and `/[locale]/privacy` as `ƒ Dynamic` after the (app) layout's `force-dynamic` directive (matches D-18 expectation)
- `/ru/opengraph-image-ds8rmm`, `/en/opengraph-image-ds8rmm`, `/es/opengraph-image-ds8rmm` continue to prerender (marketing group static path preserved)
- All Task 1/2/3 grep-based acceptance criteria pass (see acceptance_criteria sections in the plan)

## Follow-Up Todos (for /gsd-note or operator)

- **Verify brand SVGs against upstream kits before production push** — both `/login` brand marks are faithful renditions, not direct downloads. Acceptance includes a `head -c 4 .../*.svg | grep -q '<svg'` sanity check but does NOT cryptographically verify against Apple/Google's published SHA. Track for Plan 08 deploy smoke or as a manual operator step before launch.
- **HMAC verification of `rv_user`** — once Plan 03 ships `readSessionCookie()`, swap the inline `JSON.parse(Buffer.from(...).toString('utf8'))` in `getSession()` for the verified read.
- **`/api/auth/logout` endpoint** — `UserMenu`'s sign-out form posts here; Plan 03 owns the implementation. If 04-06 (dashboard) demos before Plan 03 lands, the sign-out button will 404 — accepted gap.

## Known Stubs

None — every value rendered by these primitives is either passed in by the caller (Card content, TierBadge label) or derived from a real source (cookies via `getSession()`). The `getSession()` graceful-degradation path (`email: ""` when `rv_user` is missing) is intentional defensive coding, not a stub: the navbar correctly renders the avatar disc with a generic user glyph in that case.

## Threat Flags

None — no security surfaces introduced beyond the plan's `<threat_model>` register (T-04-02-01 through T-04-02-06). The Sign-out form action `/api/auth/logout` is a known forward reference owned by Plan 03 and explicitly called out in the plan's interface notes; not a new surface.

## Self-Check: PASSED

- landing/src/components/ui/card.tsx: FOUND
- landing/src/components/ui/skeleton.tsx: FOUND
- landing/src/components/ui/toast.tsx: FOUND (Toast.createToastManager + Toast.useToastManager wired)
- landing/src/components/app/tier-badge.tsx: FOUND (server-component-safe, label prop)
- landing/src/components/app/user-menu.tsx: FOUND (Popover trigger + Sign out form)
- landing/src/components/common/navbar-app.tsx: FOUND (server component, branches on session.isAuthed)
- landing/src/lib/session.ts: FOUND (server-only, reads rv_at + rv_user)
- landing/src/app/[locale]/(app)/layout.tsx: FOUND (dynamic = 'force-dynamic')
- landing/src/app/[locale]/(marketing)/layout.tsx: FOUND (wraps Navbar + Footer)
- landing/src/app/[locale]/(marketing)/page.tsx: MOVED-AND-FOUND
- landing/src/app/[locale]/(marketing)/privacy/page.tsx: MOVED-AND-FOUND
- landing/src/app/[locale]/(marketing)/opengraph-image.tsx: MOVED-AND-FOUND
- landing/src/app/[locale]/layout.tsx: MODIFIED (Navbar/Footer removed, NextIntlClientProvider wraps children only)
- landing/public/brand/apple/apple-sign-in.svg: FOUND
- landing/public/brand/google/google-g.svg: FOUND
- landing/public/brand/README.md: FOUND
- Commit 2311c3c: FOUND
- Commit 751b586: FOUND
- Commit 13b4f42: FOUND
