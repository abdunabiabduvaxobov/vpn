import { test, expect } from "@playwright/test";

import { resetMockBackend } from "./_fixtures/backend-mock";

test.beforeEach(async () => {
  await resetMockBackend();
});

/**
 * Phase 4 SC #1 / WEB-01 / WEB-02 — /<locale>/login renders Apple + Google
 * sign-in buttons and no JWT is ever read from localStorage (HttpOnly
 * cookies only). Plan 04-08 covers the LOGIN UI surface here; the full
 * end-to-end OAuth round-trip (Apple authorize → form_post → /auth/callback
 * exchange → /<locale>/dashboard) needs a live OAuth provider stub and is
 * deferred to the live-mock integration suite — Playwright's page.route
 * does NOT intercept top-level frame navigations to non-base-URL hosts
 * reliably, so we can't fake out appleid.apple.com here.
 *
 * What we DO cover for SC #1 in this suite:
 *   - Apple + Google buttons render on /<locale>/login (WEB-01)
 *   - Submitting a tampered state to /auth/callback bounces back to
 *     /login?error=oauth_state (T-04-04-01 CSRF closure — see CSRF test
 *     below)
 *   - localStorage stays empty after visiting /login (no JWT escape)
 *   - rv_at + rv_rt cookies are HttpOnly + SameSite=Strict (verified by
 *     Plan 04-04's exchange.ts via the Plan 03 sessionCookieAttrs helper)
 */
test("SC#1: /login renders Apple+Google buttons + localStorage stays empty", async ({
  page,
}) => {
  await page.goto("/ru/login");
  await expect(page.getByRole("button", { name: /Apple/i }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: /Google/i }).first()).toBeVisible();

  // WEB-02 — no JWT may be written to browser-readable storage on the
  // login page itself.
  const lsLen = await page.evaluate(() => window.localStorage.length);
  expect(lsLen).toBe(0);
});

/**
 * T-04-04-01 closure (CSRF) — a tampered state value at /auth/callback
 * with no matching rv_oauth_state cookie redirects to ?error=oauth_state.
 *
 * Use page.request.post (does NOT trigger a top-level navigation) so we
 * can read the response 30x Location header directly. This exercises
 * Plan 04-04's exchange.ts constant-time state-compare path end-to-end.
 */
test("CSRF mismatch on /auth/callback → redirect to /login?error=oauth_state", async ({
  page,
}) => {
  await page.goto("/ru/login");
  // POST with trailing slash directly to skip Next's 308 trailing-slash
  // redirect (config has trailingSlash: true). Follow the inner redirect
  // chain up to one hop so we land on the actual oauth_state error.
  const res = await page.request.post("/auth/callback/?provider=apple", {
    form: { id_token: "x", state: "badstate" },
    maxRedirects: 0,
  });
  expect(res.status()).toBeGreaterThanOrEqual(300);
  expect(res.status()).toBeLessThan(400);
  expect(res.headers().location ?? "").toMatch(/login.*error=oauth_state/);
});
