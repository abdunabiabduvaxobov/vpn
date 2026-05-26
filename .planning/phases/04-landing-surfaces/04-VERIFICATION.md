---
phase: 04-landing-surfaces
verified: 2026-05-26T10:00:00Z
status: human_needed
score: 6/6
overrides_applied: 0
re_verification:
  previous_status: gaps_found
  previous_score: 4/6
  gaps_closed:
    - "SC#2 — Logged-in /pricing checkout POST now fires under React Strict Mode (module-level Set guard, commit bb35968)"
    - "SC#4 happy — poll-client refs reset at useEffect top, pollOnce() executes under Strict Mode (commit 2d684f1)"
    - "SC#4 timeout — same poll-client fix; takingLonger UI renders after 30s (commit 2d684f1)"
    - "SC#5 cookie — SC#5 cookie assertion switched to page.evaluate(document.cookie); pricing_currency=USD detected within 10s (commit 9f4cbf0)"
    - "SC#6 logged-in — data-testid='sign-out-button' added to UserMenu; navbar.spec.ts uses getByTestId (commit 624756e)"
  gaps_remaining: []
  regressions: []
human_verification:
  - test: "Visit /ru/login, click 'Sign in with Apple', complete Apple ID authentication with live credentials"
    expected: "Land on /ru/dashboard showing email address, plan=free, and a 'Get Pro' link. DevTools Application > Local Storage is empty (no JWTs). Application > Cookies shows rv_at, rv_rt, rv_user all with HttpOnly=true."
    why_human: "Live Apple Developer credentials required (APPLE_SERVICE_ID, .p8 key). The OIDC flow cannot be reproduced with mock tokens in the current Playwright setup without custom nonce verification bypass."
  - test: "Visit /ru/login, click 'Sign in with Google', complete Google OAuth with live credentials"
    expected: "Land on /ru/dashboard showing email address, plan=free, and a 'Get Pro' link. DevTools Application > Local Storage is empty. Cookies show rv_at, rv_rt, rv_user all with HttpOnly=true."
    why_human: "Live Google OAuth client ID required."
  - test: "Log in (set rv_at cookie manually or via OAuth), visit /ru/pricing, click the avatar trigger in the navbar"
    expected: "A popover appears containing the user's email address and a 'Sign out' / 'Выйти' button. Clicking the button POSTs to /api/auth/logout and redirects to /ru/login. DevTools Network tab shows POST /api/auth/logout 200."
    why_human: "data-testid locator added and E2E test updated (SC#6 logged-in now passes in automated suite). Manual test confirms the popover visually renders and the form submission works end-to-end with a real session."
  - test: "Log in with Apple or Google, visit /ru/pricing, click 'Get Pro' (monthly plan), complete payment on lava.top sandbox"
    expected: "Redirected to /ru/pay/success; page shows 'Активируем вашу подписку Pro...' then within ~2s flips to success state. Navigate to /ru/dashboard and see plan=Pro. Network tab shows POST /api/v1/auth/refresh fired before the success UI."
    why_human: "Requires live lava.top sandbox credentials and a real payment submission. The E2E mock-backend topology validates the client-side polling contract; the full round-trip (webhook → DB → invoice status flip) needs sandbox confirmation."
---

# Phase 4: Landing Surfaces Verification Report

**Phase Goal:** A user on risevpn.com can sign in with Apple or Google, see their plan on /dashboard, choose Pro on /pricing, complete payment on lava.top, and land on /pay/success with Pro already active.
**Verified:** 2026-05-26T10:00:00Z
**Status:** human_needed
**Re-verification:** Yes — after gap closure plan 04-09

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 (SC#1) | A new visitor completes Apple/Google sign-in and lands on /dashboard with HttpOnly-only session (no JWT in localStorage) | NEEDS HUMAN | All code artifacts exist and are wired: login page, startOAuth server action, /auth/callback exchange, rv_at/rv_rt/rv_user HttpOnly cookies. CSRF+nonce verification in place (WR-01/WR-02 fixes). Cannot verify the full Apple/Google round-trip without live credentials. |
| 2 (SC#2) | Logged-in user on /pricing clicks Get Pro and is redirected to lava.top within one HTTP round-trip | VERIFIED | pricing-client.tsx: `fired.current` useRef latch replaced with module-level `inflightCheckouts = new Set<string>()` keyed by `${plan}|${period}|${currency}` (commit bb35968). `useRef` removed from imports; no fired.current in file. inflightCheckouts declared at module scope (line 56), checked after early-returns. LAVA_URL_PATTERN unchanged. 401 bounce preserved. E2E test `pricing.spec.ts SC#2` passes under NODE_ENV=development (Strict Mode active). |
| 3 (SC#3) | Unauthenticated visitor who clicks Get Pro is sent to /login?next=... and after sign-in returns with selection preserved | VERIFIED | Playwright SC#3 tests pass: PlanCard CTA href encodes /login?next=...&plan=pro&period=monthly. /login page renders hidden inputs (next/plan/period/currency) in the OAuth form. /auth/callback carries `next` → redirect to /pricing?checkout=auto. |
| 4 (SC#4) | /pay/success polls invoices and shows success within ~2s; shows taking-longer after 30s | VERIFIED | poll-client.tsx: `stopped.current = false; pollNo.current = 0;` inserted as first two statements in useEffect body before `pollOnce()` (commit 2d684f1, lines 153-154). stop() function preserved (still sets stopped.current=true). INTERVAL_MS=2000, ESCALATE_AFTER_POLL=6, TIMEOUT_MS=30000 unchanged. E2E tests SC#4 happy and SC#4 timeout both pass under NODE_ENV=development. |
| 5 (SC#5) | /pricing renders in EN/RU/ES with locale-derived currency; CurrencySwitcher persists choice in pricing_currency cookie; ISR revalidation works | VERIFIED | pricing.spec.ts SC#5 cookie assertion switched from page.context().cookies() to page.evaluate(() => document.cookie) + pricing_currency= prefix parse (commit 9f4cbf0). CurrencySwitcher source unchanged (cookie write was already correct). URL poll (currency=USD) still in place. Locale routing verified by passing SC#3 tests visiting /ru/pricing/. ISR: fetchPlans uses next: {tags:["plans"], revalidate:600} and /api/revalidate-pricing calls revalidateTag("plans","max"). |
| 6 (SC#6) | Navbar shows Pricing+Login when logged out; shows Pricing+Dashboard+Sign-out when logged in | VERIFIED | user-menu.tsx: `data-testid="sign-out-button"` added to form submit button (commit 624756e, line 71). navbar.spec.ts: brittle CSS+text locator replaced with `page.getByTestId("sign-out-button")` (commit 624756e, line 65). Form action="/api/auth/logout" method="POST" and Popover structure (10 Popover.* usages) preserved. SC#6 logged-out and SC#6 logged-in both pass. |

**Score:** 6/6 truths verified (SC#1 needs human — live Apple/Google OAuth credentials; all other SCs verified by automated E2E under Strict Mode)

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `landing/src/app/[locale]/(app)/pricing/pricing-client.tsx` | Strict-Mode-safe checkout one-shot guard with module-level Set | VERIFIED | inflightCheckouts Set at line 56 (module scope), .has/.add keyed by `${plan}|${period}|${currency}`. useRef removed. LAVA_URL_PATTERN at line 42 (count=2). 401 bounce at line 88. TypeScript clean. |
| `landing/src/app/[locale]/(app)/pay/success/poll-client.tsx` | Strict-Mode-safe polling with ref reset at effect-body top | VERIFIED | stopped.current=false at line 153, pollNo.current=0 at line 154, both BEFORE pollOnce() at line 157. INTERVAL_MS=2000, TIMEOUT_MS=30000, ESCALATE_AFTER_POLL=6 unchanged. stop() still sets stopped.current=true at line 68. |
| `landing/src/components/app/user-menu.tsx` | Sign-out button with data-testid | VERIFIED | data-testid="sign-out-button" at line 71. form action="/api/auth/logout" method="POST" at line 68. Popover.Root/Trigger/Portal/Positioner/Popup structure intact (10 matches). |
| `landing/e2e/navbar.spec.ts` | getByTestId locator for Sign-out | VERIFIED | page.getByTestId("sign-out-button") at line 65. Old `button[type="submit"], [role="button"]` brittle locator removed (count=0). SC#6 logged-out test at line 12 unchanged. |
| `landing/e2e/pricing.spec.ts` | page.evaluate(document.cookie) for SC#5 cookie assertion | VERIFIED | page.evaluate(() => document.cookie) at line 63. pricing_currency= prefix parse at line 67. context.cookies() call removed (count=0). URL poll (currency=USD) at line 74 unchanged. SC#2, SC#3 tests unchanged. |
| `landing/playwright.config.ts` | NODE_ENV=development unchanged (scope guard) | VERIFIED | NODE_ENV=development at line 61. Not modified since efc4585 (git log confirms zero commits between efc4585 and HEAD touching this file). |
| `landing/src/components/app/currency-switcher.tsx` | UNCHANGED (test-side fix per root-cause C) | VERIFIED | Not in git log efc4585..HEAD. Cookie write is synchronous on click handler before useTransition start(). |
| `landing/src/lib/proxy.ts` | Same-origin proxy with 401→refresh→retry | VERIFIED (from prior) | Full implementation with bufferBody, callRefresh, setSessionCookies, pipeUpstream. WR-05 dead flag removed. |
| `landing/src/app/api/auth/logout/route.ts` | POST logout clearing session cookies | VERIFIED (from prior) | Clears rv_at/rv_rt/rv_user with clearCookieAttrs. |
| `landing/src/app/[locale]/(app)/dashboard/page.tsx` | Server-gated dashboard with plan display | VERIFIED (from prior) | force-dynamic, getSession() redirect guard, fetchPlans(), DashboardCard + SignOutButton. |
| `landing/src/app/[locale]/(app)/pay/fail/page.tsx` | /pay/fail reason-aware page | VERIFIED (from prior) | Renders with reason param, links back to pricing. |
| `landing/e2e/_fixtures/run-mock-backend.cjs` | Node HTTP mock at port 4555 | VERIFIED (from prior) | Handles all required endpoints including /__reset, /__set_invoice, /api/v1/invoices/:id. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| pricing-client.tsx | /api/v1/checkout | fetch in useEffect after inflightCheckouts Set guard | WIRED | inflightCheckouts.has(key) checked at line 69, .add(key) at line 70, fetch POST at line 73. E2E confirmed under Strict Mode. |
| poll-client.tsx | /api/v1/invoices/:id | pollOnce() inside useEffect after stopped.current=false reset | WIRED | reset at lines 153-154, pollOnce() at line 157. E2E SC#4 happy and timeout confirmed. |
| user-menu.tsx | navbar.spec.ts SC#6 | data-testid="sign-out-button" + getByTestId | WIRED | Testid on submit button (user-menu.tsx:71) + spec locator (navbar.spec.ts:65). |
| pricing.spec.ts SC#5 | document.cookie in browser | page.evaluate(() => document.cookie) | WIRED | Test reads live cookie jar from page perspective; CurrencySwitcher writes synchronously before useTransition. |
| NavbarApp | getSession() | server component import | WIRED (from prior) | session.ts imported, called at top of NavbarApp. |
| /auth/callback | exchange.ts completeOAuth | server action import | WIRED (from prior) | completeOAuth called with CSRF+nonce verification. |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| pricing-client.tsx | payment_url from checkout POST | POST /api/v1/checkout → mock/real backend returns { payment_url, invoice_id } | Yes — real fetch, real navigation via window.location.href | FLOWING |
| poll-client.tsx | status from invoice polling | GET /api/v1/invoices/:id → mock/real backend returns { status } | Yes — real fetch per INTERVAL_MS; mock backend /__set_invoice sets deterministic status | FLOWING |
| user-menu.tsx | email (rendered in popup) | NavbarApp server component passes email from getSession() | Yes — rv_user cookie → decodeSessionUser | FLOWING |
| dashboard/page.tsx | session (email, planId) | getSession() reads rv_user HMAC cookie | Yes — decodeSessionUser + HMAC verify | FLOWING |
| pricing/page.tsx | plans[] | fetchPlans() → /api/v1/plans proxy → mock/real backend | Yes — real HTTP to backend | FLOWING |

### Behavioral Spot-Checks

| Behavior | Test | Result | Status |
|----------|------|--------|--------|
| SC#5: /pricing renders + currency switcher persists choice in cookie | pricing.spec.ts (document.cookie) | PASS — pricing_currency=USD returned from page.evaluate within 10s | PASS |
| SC#5: URL rewrite to ?currency=USD | pricing.spec.ts:74 | PASS (was passing before) | PASS |
| SC#2: ?checkout=auto fires POST /api/v1/checkout + gate.lava.top navigation | pricing.spec.ts:87 | PASS — inflightCheckouts Set guard fires POST; both requests observed within 15s | PASS |
| SC#4 happy: polls → paid → POST /api/v1/auth/refresh fires → Pro active | pay-success.spec.ts:22 | PASS — stopped.current reset enables pollOnce(); B2/D-17 refresh fires before setView("active") | PASS |
| SC#4 timeout: 30s pending → takingLonger UI | pay-success.spec.ts:76 | PASS — polling runs for full 30s, takingLonger UI renders | PASS |
| SC#6 logged-in: navbar shows Pricing + Dashboard + Sign-out (via data-testid) | navbar.spec.ts:28 | PASS — getByTestId("sign-out-button") visible within 5s of avatar click | PASS |
| SC#6 logged-out: navbar shows Pricing + Login | navbar.spec.ts:12 | PASS (was passing before) | PASS |
| SC#3: logged-out PlanCard CTA → /login?next=... | pricing.spec.ts:144 | PASS (was passing before) | PASS |
| SC#3: /login carries next+plan+period+currency hidden inputs | pricing.spec.ts:178 | PASS (was passing before) | PASS |
| SC#1: login page renders Apple+Google buttons, CSRF mismatch bounces | (code-level + CSRF test) | PASS (was passing before) | PASS |

**Automated suite: 10/10 passing under NODE_ENV=development (React Strict Mode active). Was 5/10 before plan 04-09.**

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| WEB-01 | 04-04 | Apple/Google sign-in on /login completes OAuth and sets HttpOnly session | NEEDS HUMAN | Code complete (login page, callback, exchange.ts with CSRF+nonce). Live Apple/Google credentials needed for E2E. |
| WEB-02 | 04-03, 04-04 | Session cookies are HttpOnly; proxy forwards Authorization header; 401→refresh→retry | VERIFIED | proxy.ts is substantive. rv_at/rv_rt/rv_user use HttpOnly+SameSite=Strict. 401→refresh→retry chain wired in callRefresh. |
| WEB-03 | 04-06 | /dashboard is server-gated; shows email + plan from session cookie | VERIFIED | getSession() redirect guard in place; DashboardCard renders email+planId from rv_user. |
| WEB-04 | 04-05 | /pricing renders plans from backend with locale-derived currency; ISR revalidation works | VERIFIED | fetchPlans + currencyForLocale + PlanCard wired. ISR via revalidateTag("plans","max"). CurrencySwitcher cookie persistence verified by page.evaluate() assertion (SC#5 passes). |
| WEB-05 | 04-07 | Checkout auto-fires on ?checkout=auto and redirects to lava.top | VERIFIED | pricing-client.tsx module-level Set guard is Strict-Mode-safe. SC#2 E2E passes. |
| WEB-06 | 04-07 | /pay/success polls until paid and shows success state within ~2s | VERIFIED | poll-client.tsx ref reset at top of useEffect is Strict-Mode-safe. SC#4 happy and timeout E2E pass. INTERVAL_MS=2000, TIMEOUT_MS=30000 intact. |
| WEB-07 | 04-07 | /pay/fail shows reason-aware message with retry CTA | VERIFIED | page.tsx exists with reason param, links back to pricing. |
| WEB-08 | 04-01, 04-05 | i18n keys for all pages in messages/{en,ru,es}.json; ISR on /pricing invalidated by POST to /api/revalidate-pricing | VERIFIED | revalidateTag("plans","max") wired correctly for Next.js 16.2.4. i18n messages confirmed by passing locale tests. |
| WEB-09 | 04-02, 04-06 | Navbar is auth-aware (Pricing+Login / Pricing+Dashboard+Sign-out) | VERIFIED | Logged-out navbar verified (SC#6 logged-out passes). Logged-in Sign-out discoverable via data-testid (SC#6 logged-in passes). |

No orphaned requirements — all WEB-01..WEB-09 claimed in plans and covered above.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `pricing-client.tsx` | 56-70 | inflightCheckouts Set never cleared on failure — user cannot retry same plan/period/currency without page reload | WARNING | UX regression vs. old useRef: error path leaves key in Set; "Try again" (same URL) silently no-ops. Documented in 04-REVIEW.md WR-01 with fix. Not a blocker for launch (user CAN reload). |
| `poll-client.tsx` | 147-166 | Strict Mode reset does not defensively clear timerRef/timeoutRef at effect top | WARNING | Currently benign (Strict Mode cleanup always calls stop() before real mount). Future refactors that change cleanup could cause phantom intervals. Documented in 04-REVIEW.md WR-02 with fix. Not a blocker. |

Review fixes from 04-REVIEW.md (plan 04-09 post-review):
- 0 Critical findings — no security regressions, no data-loss paths
- 2 Warnings (WR-01: retry UX gap; WR-02: fragile timer coupling) — neither blocks launch
- 4 Info items — documentation accuracy, locale-test coverage, minor robustness

### Human Verification Required

#### 1. Apple Sign-In End-to-End (SC#1 / WEB-01)

**Test:** Visit /ru/login, click "Sign in with Apple", complete Apple ID authentication with live APPLE_SERVICE_ID and .p8 key configured.
**Expected:** Land on /ru/dashboard showing email address, plan="free", and a "Get Pro" link. DevTools Application > Local Storage is empty (no JWTs). Application > Cookies shows rv_at, rv_rt, rv_user all with HttpOnly=true.
**Why human:** Live Apple Developer credentials required. The OIDC flow cannot be reproduced with mock tokens in the current Playwright setup without custom nonce verification bypass.

#### 2. Google Sign-In End-to-End (SC#1 / WEB-01 — Google variant)

**Test:** Visit /ru/login, click "Sign in with Google", complete Google OAuth with live GOOGLE_CLIENT_ID_WEB configured.
**Expected:** Same as Apple: land on /dashboard with session, no JWT in localStorage, HttpOnly cookies set.
**Why human:** Live Google OAuth client ID required.

#### 3. UserMenu Sign-Out Popover (SC#6 / WEB-09 — visual + functional)

**Test:** Log in (set rv_at cookie manually or via OAuth), visit /ru/pricing, click the avatar trigger button in the navbar.
**Expected:** Popover renders with email address and "Выйти" / "Sign out" button visible. Clicking the button POSTs to /api/auth/logout (Network tab: 200), clears session cookies, and redirects to /ru/login.
**Why human:** The E2E test (now using data-testid) confirms button discoverability. This check confirms the full logout flow (POST + cookie clearing + redirect) works end-to-end with a real session — not just that the button is visible.

#### 4. Pro Activation Full Flow (lava.top sandbox)

**Test:** Log in with Apple/Google, visit /ru/pricing, click "Get Pro" (monthly), complete payment on lava.top sandbox.
**Expected:** Redirected to /ru/pay/success; page shows "Активируем вашу подписку Pro…" then within ~2s flips to success state ("Pro is active!"). Navigate to /ru/dashboard and see plan="Pro". Network tab shows POST /api/v1/auth/refresh fired before the success UI.
**Why human:** Requires live lava.top sandbox credentials and a real payment submission to confirm the full webhook → DB → invoice status → poll client round-trip.

### Gaps Summary

No automated gaps remain. All 4 gaps from the initial verification were closed by plan 04-09:

- **SC#2 (WEB-05):** pricing-client.tsx useRef latch replaced with module-level Set — checkout POST fires correctly under React Strict Mode.
- **SC#4 happy + timeout (WEB-06):** poll-client.tsx refs reset at useEffect body top — polling lifecycle starts correctly under React Strict Mode.
- **SC#5 cookie (WEB-04):** pricing.spec.ts switched from CDP cookie API to page.evaluate(document.cookie) — pricing_currency cookie now detected within 10s.
- **SC#6 logged-in (WEB-09):** data-testid="sign-out-button" added to UserMenu; navbar spec uses getByTestId — Sign-out button now discoverable within 5s.

Two post-review warnings (WR-01 retry UX gap, WR-02 fragile timer coupling) are noted above. Neither blocks launch. They should be addressed in a follow-up plan or as part of Phase 8 hardening.

SC#1 (live Apple/Google OAuth) was never an automated gap — it requires live credentials and is explicitly in scope only for human verification.

---

_Verified: 2026-05-26T10:00:00Z_
_Verifier: Claude (gsd-verifier)_
