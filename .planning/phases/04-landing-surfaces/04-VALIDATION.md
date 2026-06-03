---
phase: 4
slug: landing-surfaces
status: verified
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-03
validated: 2026-06-03
reconstructed: true
---

# Phase 4 — Validation Strategy

> Reconstructed from artifacts (State B) on 2026-06-03 during the v2.2.0 milestone Nyquist burn-down.
> Source: 04-0N-SUMMARY.md (esp. 04-08 deploy-smoke-tests), 04-HUMAN-UAT.md, and the committed Playwright suite.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Playwright (`@playwright/test ^1.60.0`) — E2E, two-server harness |
| **Harness** | `landing/playwright.config.ts` boots a pure-Node Phase 2/3 mock backend (`e2e/_fixtures/run-mock-backend.cjs` @ :4555) + the Next.js standalone build (system under test); `workers:1` to avoid `/__set_invoice` cross-test races |
| **Mock contract** | `e2e/_fixtures/backend-mock.ts` — mockPlans / mockOauthExchange / mockAuthRefresh / mockCheckout / mockInvoicePolling / mockOAuthRedirect |
| **i18n** | `landing/src/messages/{en,es,ru}.json` (next-intl — missing keys fail the build) |
| **Run command** | `cd landing && npm run test:e2e` (`playwright test`) |
| **Sandbox note** | node/playwright are BLOCKED in the GSD sandbox — the suite is executed in CI and at phase execution time. PROJECT.md + 04-08-SUMMARY record **E2E 10/10 green** under `NODE_ENV=development`. |

---

## Requirement Verification Map

The 10-test Playwright suite (login/pricing/pay-success/navbar specs) asserts SC#1..SC#6 and, per 04-08-SUMMARY, **WEB-01..WEB-09**. Mapping:

| Req | Secure Behavior | E2E coverage (spec :: title) | Status |
|-----|-----------------|------------------------------|--------|
| WEB-01 | /login renders Apple+Google; no JWT in localStorage | `login.spec` :: "SC#1: /login renders Apple+Google buttons + localStorage stays empty" | ✅ authored, 10/10 at exec · 🔶 live OAuth = UAT-1/2 |
| WEB-02 | OAuth callbacks exchange tokens; CSRF state checked | `login.spec` :: "CSRF mismatch on /auth/callback → /login?error=oauth_state" | ✅ authored (callback POST + state) · 🔶 live = UAT-1/2 |
| WEB-03 | /dashboard shows plan + sign-out (HttpOnly cookies) | `navbar.spec` :: "SC#6 logged-in: Dashboard + Sign-out via avatar" + cookie assertions | ✅ authored · 🔶 real-session = UAT-3 |
| WEB-04 | /pricing renders dynamically; no hardcoded prices | `pricing.spec` :: "SC#5: /pricing renders + currency switcher persists" | ✅ authored |
| WEB-05 | "Get Pro" → POST /checkout → lava redirect | `pricing.spec` :: "SC#2: ?checkout=auto fires POST /checkout + lava navigation"; "SC#3 Pro CTA → /login?next=…&plan=…" | ✅ authored · 🔶 live lava = UAT-4 |
| WEB-06 | /pay/success polls invoice ≤30s; force-refresh | `pay-success.spec` :: "SC#4 happy: polls → paid → force-refresh fires"; "SC#4 timeout: pending forever" | ✅ authored · 🔶 live lava = UAT-4 |
| WEB-07 | /pay/fail retry CTA → /pricing | `pay-success.spec` timeout/fail path → retry CTA | ✅ authored (timeout case) |
| WEB-08 | i18n keys for all 5 pages in en/es/ru | next-intl build + locale-rendering E2E (pages render under /ru, /en) | ✅ structural (missing key fails build) |
| WEB-09 | Navbar/footer: Pricing, Login/Dashboard+Sign-out | `navbar.spec` :: "SC#6 logged-out: Pricing+Login"; "SC#6 logged-in: …+Sign-out" | ✅ authored |

*Status: ✅ authored+green-at-exec · 🔶 live-cred manual UAT*

---

## Manual-Only Verifications (live credentials / external services — tracked in 04-HUMAN-UAT.md)

| # | Behavior | Requirement | Why Manual |
|---|----------|-------------|-----------|
| UAT-1 | Apple Sign-In end-to-end (live APPLE_SERVICE_ID + .p8) → /dashboard, HttpOnly cookies, empty localStorage | WEB-01/02 (SC#1) | Needs live Apple credentials + real Apple ID auth |
| UAT-2 | Google Sign-In end-to-end (live GOOGLE_CLIENT_ID_WEB) | WEB-01/02 (SC#1) | Needs live Google OAuth client |
| UAT-3 | UserMenu sign-out popover on a real session → POST /api/auth/logout 200, cookies cleared | WEB-09 (SC#6) | Needs a real authenticated session |
| UAT-4 | Pro activation full flow via lava.top **sandbox** → /pay/success → dashboard plan=Pro | WEB-05/06 | Needs lava.top sandbox + live payment round-trip |

---

## Validation Audit 2026-06-03

All WEB-01..09 have authored Playwright E2E coverage (10 tests asserting SC#1..SC#6, per 04-08-SUMMARY) that ran **10/10 green at phase execution** (PROJECT.md). The GSD sandbox cannot re-execute node/playwright, so this audit verifies coverage by artifact (committed specs + mock-backend fixture + execution record) rather than live re-run. The live-credential portions (Apple/Google OAuth, lava.top sandbox) are genuine manual UAT (UAT-1..4). No automatable gaps remain. `nyquist_compliant: true`.

---

## Validation Sign-Off

- [x] Every WEB requirement maps to an authored automated E2E test or a documented manual-only UAT
- [x] E2E suite recorded green at execution (10/10) and is the Phase 6+ regression baseline
- [x] Manual-only (live OAuth + lava sandbox) enumerated in 04-HUMAN-UAT.md
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** verified 2026-06-03 (E2E green at exec; live-cred UAT deferred to operator per 04-HUMAN-UAT.md)
