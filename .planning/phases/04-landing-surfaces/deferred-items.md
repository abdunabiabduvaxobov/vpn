# Deferred Items - Phase 04 Landing Surfaces

## Plan 04-04 Build Conflict (pre-existing, out of scope for Plan 04-07)

**Issue:** `next build` fails with:
> Conflicting route and page at /auth/callback: route at /auth/callback/route and page at /auth/callback/page

**Source:** Commit `b8b347d` (Plan 04-04) intentionally shipped BOTH `route.ts` (POST handler) and `page.tsx` (GET wrapper) at `landing/src/app/auth/callback/`. Plan 04-04's SUMMARY documents this design (page.tsx is documented as "GET wrapper, dynamic + nodejs runtime"). However Next.js 16 rejects this configuration.

**Status:** Pre-existing — predates Plan 04-07 work. Not caused by Plan 04-07 changes (Plan 04-07 only modifies pricing-client.tsx + creates /pay/success + /pay/fail files).

**Detected during:** Plan 04-07 Task 1 `npm run build` verification.

**Mitigation in scope:** Plan 04-07 verified via `npx tsc --noEmit` which passes (exit 0) — type safety of the new files is confirmed even though full Next build is blocked by the pre-existing /auth/callback issue.

**Resolution owner:** Plan 04-08 (deploy + smoke tests) or a follow-up Plan 04-04 fix. Recommended fix: delete `page.tsx`, keep `route.ts` (the form_post POST handler is the documented primary path); query-mode operator hand-testing can still hit `route.ts` via a curl POST.
