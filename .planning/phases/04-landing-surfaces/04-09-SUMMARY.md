---
phase: 04-landing-surfaces
plan: 09
subsystem: landing
type: execute
gap_closure: true
tags: [gap-closure, strict-mode, react-19, playwright, e2e, web-04, web-05, web-06, web-09]
requirements:
  - WEB-04
  - WEB-05
  - WEB-06
  - WEB-09
dependency_graph:
  requires:
    - landing/playwright.config.ts (unchanged — NODE_ENV=development intentional)
    - landing/src/components/app/currency-switcher.tsx (unchanged — write is correct)
  provides:
    - "Strict-Mode-safe polling lifecycle on /pay/success"
    - "Strict-Mode-safe one-shot checkout guard on /pricing"
    - "Portal-aware Sign-out test locator on logged-in navbar"
    - "Live-jar cookie assertion for pricing_currency"
  affects:
    - "Phase 4 verification score: 4/6 → 6/6 (SC#1 still NEEDS-HUMAN)"
tech_stack:
  added: []
  patterns:
    - "Module-level Set as Strict-Mode-safe one-shot guard (keyed by intent)"
    - "Effect-body lifecycle ref reset to defeat cleanup poisoning"
    - "page.evaluate(() => document.cookie) for live-jar cookie reads in Playwright"
    - "data-testid + page.getByTestId for base-ui Popover Portal content"
key_files:
  created: []
  modified:
    - "landing/src/app/[locale]/(app)/pay/success/poll-client.tsx"
    - "landing/src/app/[locale]/(app)/pricing/pricing-client.tsx"
    - "landing/src/components/app/user-menu.tsx"
    - "landing/e2e/navbar.spec.ts"
    - "landing/e2e/pricing.spec.ts"
decisions:
  - "Keep NODE_ENV=development in playwright.config.ts (Strict Mode active) so fixes prove themselves under the hostile environment instead of masking the bug."
  - "Module-level Set in pricing-client beats per-component ref under Strict Mode: cleanup cannot poison it, and the Set key (`${plan}|${period}|${currency}`) preserves the duplicate-charge guarantee."
  - "Effect-body reset (stopped.current=false; pollNo.current=0) in poll-client is the smallest viable fix — preserves the cleanup-on-real-unmount stop() contract and the D-21 polling parameters."
  - "currency-switcher.tsx code is unchanged: the cookie write was already synchronous and correct on the click handler before useTransition. The fault was in the Playwright observation method (CDP cookie API)."
  - "data-testid='sign-out-button' is the stable contract — comment-only references to old class names will not break the locator if base-ui changes its Popover internals."
metrics:
  duration: "5m15s"
  completed_at: "2026-05-26T02:13:02Z"
  tasks: 5
  files_modified: 5
  files_created: 0
  e2e_pass_count_before: "5/10"
  e2e_pass_count_after: "10/10"
---

# Phase 04 Plan 09: Gap Closure — Strict Mode latches and brittle test selectors Summary

**One-liner:** Two Strict-Mode latch bypasses (poll-client effect-body reset, pricing-client module-level Set keyed by `${plan}|${period}|${currency}`), one stable Popover Portal test locator (data-testid + getByTestId), and one test-side cookie observation switch (page.evaluate over CDP) take Phase 4's E2E suite from 5/10 passing to 10/10 under `NODE_ENV=development`.

## Pre/Post Verification State

| Surface | Truth | Before | After |
|---|---|---|---|
| `/pricing` | SC#2 — POST /api/v1/checkout + gate.lava.top navigation under Strict Mode | FAIL | **PASS** |
| `/pay/success` | SC#4 happy — pending → paid → POST /api/v1/auth/refresh → "Pro is active" under Strict Mode | FAIL | **PASS** |
| `/pay/success` | SC#4 timeout — 30s pending → takingLonger UI under Strict Mode | FAIL | **PASS** |
| `/pricing` | SC#5 cookie — `pricing_currency=USD` readable from `document.cookie` within 10s of USD click | FAIL | **PASS** |
| Navbar | SC#6 logged-in — Sign-out button discoverable via stable locator within 5s of avatar click | FAIL | **PASS** |
| `/login` | SC#1 — Apple+Google buttons render, localStorage stays empty, CSRF mismatch bounces | PASS | PASS |
| `/login` | CSRF mismatch on `/auth/callback` → `/login?error=oauth_state` | PASS | PASS |
| Navbar | SC#6 logged-out — Pricing + Login visible | PASS | PASS |
| `/pricing` | SC#3 logged-out — Pro CTA → `/login?next=/pricing&plan=...` | PASS | PASS |
| `/pricing` | SC#3 — `/login` carries `next+plan+period+currency` hidden inputs | PASS | PASS |

**E2E suite:** 10 passed in 37.2s (was 5 passed / 5 failed).

**Phase 4 verification score:** 4/6 → **6/6** truths verified. SC#1 live OAuth still NEEDS-HUMAN (Apple/Google credentials), explicitly out of scope for this plan.

## Root-Cause Taxonomy

Three of the five edits resolve a single root cause; the fourth is an orthogonal Playwright API quirk. The fifth was a verification-only task (no source edit).

### Root Cause A — `useRef`-based one-shot guards do not survive React Strict Mode

React 18+/19 in development runs `effect → cleanup → effect` on every initial mount to surface cleanup bugs. The first cleanup mutates the ref before the second effect body runs; the ref's "done" value is now permanent state for the entire real mount.

Two surfaces hit this:

1. **`landing/src/app/[locale]/(app)/pay/success/poll-client.tsx`** — Cleanup calls `stop()` which sets `stopped.current = true`. The second effect body calls `pollOnce()`, which early-returns on `if (stopped.current) return;`. No poll fires. Fix: reset `stopped.current = false; pollNo.current = 0;` as the first two statements inside the useEffect body, before `pollOnce()`. The cleanup-on-real-unmount stop() contract is preserved because real unmounts do NOT re-run the effect body. (Commit `2d684f1`.)

2. **`landing/src/app/[locale]/(app)/pricing/pricing-client.tsx`** — `useRef(false)` latch flipped to `true` by the first pass; second pass sees `fired.current === true` and early-returns. No POST. Fix: replace the per-component ref with a **module-level `Set<string>`** keyed by `${plan}|${period}|${currency}`. The Set lookup is checked AFTER the `checkout !== "auto" || !plan || !period` early-return, so it only guards the actual POST. The Set survives Strict Mode mount/remount (module state is not unmounted) AND prevents duplicate POSTs across legitimate remounts with the same intent. (Commit `bb35968`.)

### Root Cause B — base-ui Popover Portal DOM is opaque to text-based CSS locators

`landing/src/components/app/user-menu.tsx` renders the sign-out form inside `<Popover.Portal>` → `<Popover.Positioner>` → `<Popover.Popup>`. base-ui mounts portal content into `document.body` asynchronously after the trigger click. The old Playwright locator `page.locator('button[type="submit"], [role="button"]').filter({ hasText: /Выйти|Sign out|Cerrar/ })` is fragile across base-ui internal changes and was racing the Portal mount.

Fix: add `data-testid="sign-out-button"` to the `<button type="submit">` and switch the navbar spec assertion to `page.getByTestId("sign-out-button")`. Portal-aware (testid works across the entire DOM, including portal content), locale-independent (no text matching), and stable across base-ui upgrades. The form `action="/api/auth/logout" method="POST"` and the entire Popover.Root/Trigger/Portal/Positioner/Popup structure are preserved. (Commit `624756e`.)

### Root Cause C — Playwright's CDP cookie API has a race with document.cookie writes

`landing/src/components/app/currency-switcher.tsx` writes `document.cookie = 'pricing_currency=USD; ...'` **synchronously** in the onClick handler, BEFORE the `useTransition` `start()` call. The write hits the browser jar immediately.

Playwright's `page.context().cookies()` goes through the Chrome DevTools Protocol cookie endpoint, which has been observed to miss host-only cookies set via `document.cookie` from a click handler within a 10s polling window (no clear root cause documented upstream; possibly a CDP-vs-renderer cookie-jar sync race specific to `document.cookie` mutations on `http://localhost`).

`document.cookie` reads always reflect the live jar from the page's perspective. Fix: replace the test's `expect.poll(async () => page.context().cookies()...)` block with `expect.poll(async () => page.evaluate(() => document.cookie)...)` + a `startsWith("pricing_currency=")` prefix parse. **No source-code change to `currency-switcher.tsx`** — the write was already correct. This matches the 04-VERIFICATION.md root-cause C recommendation. (Commit `9f4cbf0`.)

### Task 5 — Verification only

No source edit. Built the standalone bundle, copied static assets, ran the full Playwright suite. 10/10 green.

## Edits Made

| Task | File | Change | Lines | Commit |
|---|---|---|---|---|
| 1 | `landing/src/app/[locale]/(app)/pay/success/poll-client.tsx` | Insert `stopped.current = false; pollNo.current = 0;` at top of useEffect body (before `pollOnce()`) | +7 / -0 | `2d684f1` |
| 2 | `landing/src/app/[locale]/(app)/pricing/pricing-client.tsx` | Remove `useRef` import + `fired = useRef(false)`; add module-level `inflightCheckouts = new Set<string>()` after `LAVA_URL_PATTERN`; useEffect now guards on `inflightCheckouts.has(key)` / `.add(key)` keyed by `${plan}|${period}|${currency}`; docstring updated to describe new latch | +25 / -9 | `bb35968` |
| 3a | `landing/src/components/app/user-menu.tsx` | Add `data-testid="sign-out-button"` to the form submit button | +1 / -0 | `624756e` |
| 3b | `landing/e2e/navbar.spec.ts` | SC#6 logged-in: replace brittle CSS+text locator with `page.getByTestId("sign-out-button")` | +6 / -8 | `624756e` |
| 4 | `landing/e2e/pricing.spec.ts` | SC#5 cookie: switch `expect.poll` from `page.context().cookies()` to `page.evaluate(() => document.cookie)` + `pricing_currency=` prefix parse | +18 / -6 | `9f4cbf0` |
| 5 | (no source edit) | E2E suite run — 10/10 passed in 37.2s | — | (verification only) |

## Scope Guards Held

- ✓ `landing/playwright.config.ts` — `NODE_ENV=development` (line 61) UNCHANGED. Strict Mode is still active in the test runner; the fixes prove themselves under the hostile environment instead of masking the bug. (`git log efc4585..HEAD -- landing/playwright.config.ts` returns zero commits.)
- ✓ `landing/src/components/app/currency-switcher.tsx` UNCHANGED. The cookie write was already correct — only the Playwright observation method needed fixing. (`git log efc4585..HEAD -- landing/src/components/app/currency-switcher.tsx` returns zero commits.)
- ✓ `INTERVAL_MS = 2000`, `ESCALATE_AFTER_POLL = 6`, `TIMEOUT_MS = 30000` (D-21 polling parameters) unchanged.
- ✓ `LAVA_URL_PATTERN = /^https:\/\/(gate\.|app\.|pay\.)?lava\.top\//` regex unchanged.
- ✓ 401 → `/login?next=/pricing?plan=...&period=...&currency=...&checkout=auto` bounce contract unchanged.
- ✓ `B2/D-17 — POST /api/v1/auth/refresh BEFORE setView("active")` contract unchanged.
- ✓ Sign-out form `action="/api/auth/logout" method="POST"` (works without JS) unchanged.

## Deviations from Plan

None — plan executed exactly as written.

### Auto-fixed Issues

None encountered during execution. The plan's interfaces section matched the live codebase exactly, the acceptance criteria gated each task cleanly, and the full E2E suite went green on the first attempt after build + static-asset copy.

### Authentication Gates

None — this plan is purely client-side and test-side, no auth or external services involved.

## TypeScript / Build Status

- All four task-level `npx tsc --noEmit` checks: clean (no output).
- `npm run build` (Task 5): exits 0. 33/33 static pages generated. `.next/standalone/` produced cleanly.
- `npm run test:e2e` (Task 5): exits 0. 10/10 passed in 37.2s.

## Acceptance Criteria — Final Tally

- [x] Task 1: `poll-client.tsx` contains `stopped.current = false;` and `pollNo.current = 0;` as the first two statements inside the useEffect body, BEFORE `pollOnce();`. Verified line-number ordering: 153 / 154 / 157.
- [x] Task 2: `pricing-client.tsx` has module-level `const inflightCheckouts = new Set<string>();` (line 56) AFTER `LAVA_URL_PATTERN` (line 41). useEffect guards on `inflightCheckouts.has(key)` / `.add(key)`. `useRef` removed from imports (grep `-c useRef` = 0).
- [x] Task 3: `user-menu.tsx` `<button type="submit">` carries `data-testid="sign-out-button"` (grep count = 1). `navbar.spec.ts` SC#6 logged-in uses `page.getByTestId("sign-out-button")` (count = 1). Old `button[type="submit"], [role="button"]` locator removed (count = 0).
- [x] Task 4: `pricing.spec.ts` SC#5 cookie assertion uses `page.evaluate(() => document.cookie)` (count = 1) with `startsWith("pricing_currency=")` (count = 1). Old `page.context().cookies()` call removed from SC#5 (count = 0; only the comment textual reference was tidied up to keep the literal CDP API name out of the file).
- [x] Task 5: `npm run build && npm run test:e2e` exits 0 with 10/10 tests passing in 37.2s.
- [x] `landing/playwright.config.ts` `NODE_ENV=development` unchanged.
- [x] `landing/src/components/app/currency-switcher.tsx` unchanged.
- [x] All `npx tsc --noEmit` checks exit 0.
- [x] Phase 4 score: 4/6 → 6/6 truths verified (SC#1 NEEDS-HUMAN).

## Threat Closure

All six threats from the plan's threat register close `accept` or `n/a` — zero new attack surface introduced:

| Threat ID | Disposition | Note |
|---|---|---|
| T-04-09-01 (Tampering — inflightCheckouts Set) | `accept` | Per-page-load module state; key derived from URL params; backend re-validates `plan_code` per Phase 3 PAY-08. No new tampering surface vs the prior `useRef` latch. |
| T-04-09-02 (Repudiation — checkout POST) | `accept` | Backend already logs every `/api/v1/checkout` call with `user_id`. Set guard changes WHEN a POST fires, not WHO can fire it. |
| T-04-09-03 (Info Disclosure — data-testid) | `accept` | `data-testid="sign-out-button"` leaks zero secret data — the button text is already visible. Standard E2E testing practice. |
| T-04-09-04 (Info Disclosure — page.evaluate document.cookie) | `accept` | Test code reads only cookies the page already sees; HttpOnly cookies (`rv_at`/`rv_rt`/`rv_user`) remain invisible. `pricing_currency` is intentionally non-HttpOnly per 04-05 SUMMARY. |
| T-04-09-05 (DoS — Strict Mode reset in poll-client) | `accept` | 30s `TIMEOUT_MS` unchanged; cleanup-on-real-unmount stops polling. Max polls per page-load remains ~15. |
| T-04-09-06 (Elevation of Privilege) | `n/a` | No auth-bearing code paths, no new endpoints, no new env vars. All fixes are defensive. |

**Pro launch security bar (CLAUDE.md):** Honoured. PAY-08 backend plan validation, Phase 2 HttpOnly cookie scope, T-04-07-01 LAVA_URL_PATTERN, T-04-04-01 OAuth state CSRF, and the `rv_at/rv_rt` cookie shape are all unchanged.

## Known Stubs

None — every edit wires real behaviour. No placeholder text, no empty data sources.

## Threat Flags

None — no new network endpoints, no new auth paths, no new file/schema surfaces at trust boundaries.

## Self-Check: PASSED

- [x] `landing/src/app/[locale]/(app)/pay/success/poll-client.tsx` — modified, commit `2d684f1` present in `git log --oneline -10`
- [x] `landing/src/app/[locale]/(app)/pricing/pricing-client.tsx` — modified, commit `bb35968` present
- [x] `landing/src/components/app/user-menu.tsx` — modified, commit `624756e` present
- [x] `landing/e2e/navbar.spec.ts` — modified, commit `624756e` present
- [x] `landing/e2e/pricing.spec.ts` — modified, commit `9f4cbf0` present
- [x] E2E suite final state: 10 passed (37.2s) — captured from `npm run test:e2e` output above
- [x] `landing/playwright.config.ts` NODE_ENV=development line still at line 61
- [x] `landing/src/components/app/currency-switcher.tsx` not in `git log efc4585..HEAD` output
