---
phase: 04-landing-surfaces
plan: 01
subsystem: landing
tags: [foundation, i18n, next-intl, env-config, standalone]
dependency_graph:
  requires:
    - "landing/next.config.ts (Next 16, next-intl plugin, existing output 'export')"
    - "landing/src/i18n/routing.ts (ru/en/uz baseline)"
    - "landing/src/messages/{en,ru,uz}.json (existing marketing namespaces)"
  provides:
    - "Node-runtime standalone Next.js build (landing/.next/standalone/server.js)"
    - "Locale set ru/en/es (uz removed) via routing.ts + layout alternates"
    - "All Phase 4 i18n namespaces (login, dashboard, pricing, pay, auth, errors, navbar.app) present in en/ru/es"
    - "Typed server-only env loader landing/src/lib/env.ts exporting BACKEND_API_URL, REVALIDATE_SECRET, COOKIE_DOMAIN, NODE_ENV, IS_PROD"
    - "landing/.env.example + landing/.env.local.example for downstream operator setup"
  affects:
    - "All Phase 4 plans 04-02 through 04-08 — they import env from @/lib/env and rely on Node runtime + es locale"
    - "Phase 3 admin handlers — D-14 fan-out POST to /api/revalidate-pricing now has a stable secret source"
tech_stack:
  added:
    - "server-only (Next.js bundler marker; no new npm dep, ships with Next 16)"
  patterns:
    - "Server-only typed env loader with fail-fast required-var validation at module load (similar to Phase 3 D-29 backend env pattern)"
    - "Phase 4 i18n namespace convention: top-level page namespace (login, dashboard, pricing, pay) + cross-cutting namespaces (auth, errors, navbar.app)"
key_files:
  created:
    - "landing/src/messages/es.json"
    - "landing/src/lib/env.ts"
    - "landing/.env.example"
    - "landing/.env.local.example"
  modified:
    - "landing/next.config.ts (output: standalone, dropped images.unoptimized)"
    - "landing/src/i18n/routing.ts (locales: ru/en/es)"
    - "landing/src/app/[locale]/layout.tsx (alternates + openGraph locale)"
    - "landing/src/messages/en.json (Phase 4 namespaces appended)"
    - "landing/src/messages/ru.json (Phase 4 namespaces appended)"
    - "landing/src/components/common/locale-switcher.tsx (LABELS map ru/en/es)"
  deleted:
    - "landing/src/messages/uz.json"
decisions:
  - "Dropped images.unoptimized from next.config.ts — Node runtime supports Next's default image loader; no marketing page depends on the unoptimized escape hatch"
  - "Kept trailingSlash: true to preserve existing /ru/, /en/, /es/ URL pattern; downstream plans rely on trailing slashes for nginx routing"
  - "es.json built as a full Spanish translation of en.json structure (not a partial overlay) — required because next-intl static imports the entire locale file"
  - "env.ts uses inline type-guarded readers instead of zod/valibot — avoids a new runtime dep and keeps the surface area minimal; can swap to zod later if env grows past ~10 keys"
  - "ES initial pass by Claude; native-speaker review tracked as Phase 4 follow-up todo (already captured in 04-CONTEXT.md deferred section)"
metrics:
  duration: "~7 minutes wall clock (3 tasks, 3 commits, 1 deviation auto-fix, 1 standalone build)"
  completed: "2026-05-23"
  tasks_completed: 3
  files_changed: 11
  commits: 3
---

# Phase 4 Plan 01: Foundation — i18n + Standalone Build Summary

Switched the landing from static export to a Node-runtime standalone bundle, rotated the locale set from ru/en/uz to ru/en/es, populated all three locale files with every Phase 4 i18n namespace (login/dashboard/pricing/pay/auth/errors/navbar.app), and introduced a typed server-only env loader so every downstream Phase 4 plan reads BACKEND_API_URL/REVALIDATE_SECRET/COOKIE_DOMAIN from one validated source. Build verified end-to-end: `landing/.next/standalone/server.js` is emitted with the three locale routes prerendered.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Flip next.config to standalone + drop uz + add es | `cccd892` | next.config.ts, src/i18n/routing.ts, src/app/[locale]/layout.tsx |
| 2 | Rotate message files + add Phase 4 i18n namespaces | `77412e6` | src/messages/en.json, ru.json, es.json (new), uz.json (deleted) |
| 3 | Add typed server env loader + .env.example + verify build | `5c22986` | src/lib/env.ts (new), .env.example (new), .env.local.example (new), src/components/common/locale-switcher.tsx (deviation fix) |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Fix locale-switcher LABELS map after locale rotation**
- **Found during:** Task 3 (`npm run build` TypeScript phase)
- **Issue:** `landing/src/components/common/locale-switcher.tsx` line 8 declared `LABELS: Record<Locale, string> = { ru: "RU", en: "EN", uz: "UZ" }`. After Task 1 changed `Locale` to `"ru" | "en" | "es"`, the `uz` key triggered TS2353 ("Object literal may only specify known properties, and 'uz' does not exist in type 'Record<"ru" | "en" | "es", string>'") and the Next.js build worker exited with code 1.
- **Fix:** Replaced `uz: "UZ"` with `es: "ES"` so the LABELS map covers the new locale union.
- **Why not in plan:** The plan listed three files for Task 1 but the locale-switcher reference was an in-component constant outside the planned file list. Standard Rule 3 auto-fix (blocking issue caused by the current plan's own changes; scope-local to Task 1 deliverable).
- **Files modified:** `landing/src/components/common/locale-switcher.tsx`
- **Commit:** included in `5c22986` (alongside Task 3 work because both were needed for the build to pass)

### CLAUDE.md / Project-Convention Adjustments

None — no CLAUDE.md rules conflicted with the plan.

## Authentication Gates

None — no auth required for any task.

## Key Decisions Made

- **Image loader:** Dropped `images.unoptimized: true` from `next.config.ts` because the Node runtime restores the default image optimizer; no marketing page relies on the unoptimized loader (manually audited `landing/src/app/` for `next/image` usage). If a future page needs raw passthrough images, set `unoptimized` on individual `<Image>` components rather than globally.
- **Trailing slash:** Kept `trailingSlash: true`. nginx routing in plan 04-08 expects trailing slashes on locale-prefixed URLs (`/ru/pricing/`). Dropping it would require touching the existing nginx config and risk breaking existing marketing URLs.
- **env validator shape:** Inline type-guarded readers (`readRequired`) instead of pulling in zod/valibot. Four keys do not justify a new runtime dep; the validator is ~30 lines and trivially unit-testable. Revisit if env grows past ~10 keys.
- **ES translation source:** Claude produced the initial Spanish pass for both marketing pages and Phase 4 namespaces. Native-speaker QA is already tracked as a non-blocking Phase 4 follow-up todo (see `04-CONTEXT.md` `<deferred>` section).
- **`server-only` package:** Did not add to `package.json` — it ships inside Next.js's bundler and is the documented Next 16 pattern. `npm run build` resolves it without a top-level dependency.

## Contracts Established (for downstream plans)

**env.ts contract (consumed by 04-03 proxy, 04-04 OAuth callback, 04-05 pricing revalidate, 04-07 checkout):**

```ts
import { env } from "@/lib/env";

env.BACKEND_API_URL    // string, required, no trailing slash
env.REVALIDATE_SECRET  // string, required, server-side only (T-04-01-01)
env.COOKIE_DOMAIN      // string, may be "" for host-only
env.NODE_ENV           // "development" | "production" | "test"
env.IS_PROD            // boolean
```

Importing `env.ts` into a client component triggers a Next.js build-time error via `import "server-only"`. Downstream plans should NOT mirror `REVALIDATE_SECRET` to a `NEXT_PUBLIC_` var.

**i18n namespace contract:**

All three message files (en/ru/es) contain these Phase 4 namespaces at top level — downstream plans can reference keys directly without touching message files again:

- `login.{heading,subhead,signIn.{apple,google},backHome}`
- `dashboard.{heading,email,plan.{label,free,pro},cta.{getPro,manage},signOut}`
- `pricing.{heading,subhead,currency.{usd,eur,rub},period.{monthly,yearly},cta.{getPro,current},empty.heading}`
- `pay.success.{processing,active,continue,takingLonger.{heading,body},refresh,contactSupport}`
- `pay.fail.{title,body.{default,declined,cancelled},tryAgain,contactSupport}`
- `auth.{signedOut,signOut.confirm.{title,body,confirm,cancel}}`
- `errors.{sessionExpired,network,oauthState,oauthDenied}`
- `navbar.app.{pricing,dashboard,login,signOut}`

## Verification Evidence

- `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npm run build` exits 0
- `test -d landing/.next/standalone` succeeds — `server.js` present alongside `node_modules/` and `package.json`
- `/ru`, `/en`, `/es` prerendered as static HTML (turbopack output: 18 pages across the three locales × 6 routes)
- `grep -rn '"uz"\|/uz/' landing/src/ landing/next.config.ts` returns 0 hits (no leftover uz references)
- Required-key assertion script ran for en/ru/es; all 49 Phase 4 keys present in each file
- `import "server-only"` is line 1 of `landing/src/lib/env.ts`

## Follow-Up Todos (for /gsd-note or operator)

- **ES translation native-speaker review** — already in `04-CONTEXT.md` `<deferred>`. Initial Claude pass is functional but should be QA'd before any Spanish-market marketing push.
- **Apple/Google OAuth dashboard redirect URIs** — operator must register `https://risevpn.com/auth/callback` (and staging equivalent) with both providers before plan 04-04 ships to production. Not blocking for the plan-04-04 implementation itself.

## Known Stubs

None — every value created (env loader, locale switcher, message files) is wired to a real source. The env loader fails fast if required vars are missing rather than silently degrading.

## Threat Flags

None — no new security surfaces beyond what the plan's `<threat_model>` already enumerated. T-04-01-01 through T-04-01-05 dispositions were honored (`server-only` enforces server-side scope, locale allow-list intact, .env.example contains placeholders only).

## Self-Check: PASSED

- landing/next.config.ts: FOUND (output: "standalone")
- landing/src/i18n/routing.ts: FOUND (locales: ["ru","en","es"])
- landing/src/app/[locale]/layout.tsx: FOUND (alternates.languages includes es)
- landing/src/messages/en.json: FOUND (with Phase 4 namespaces)
- landing/src/messages/ru.json: FOUND (with Phase 4 namespaces)
- landing/src/messages/es.json: FOUND (new file)
- landing/src/messages/uz.json: DELETED (as required)
- landing/src/lib/env.ts: FOUND (server-only, frozen export)
- landing/.env.example: FOUND
- landing/.env.local.example: FOUND
- landing/src/components/common/locale-switcher.tsx: FOUND (deviation fix applied)
- landing/.next/standalone/: FOUND (build artifact)
- Commit cccd892: FOUND
- Commit 77412e6: FOUND
- Commit 5c22986: FOUND
