import { test, expect } from "@playwright/test";

import {
  mockCheckout,
  mockOAuthRedirect,
  mockOauthExchange,
  mockPlans,
  resetMockBackend,
} from "./_fixtures/backend-mock";

test.beforeEach(async () => {
  await resetMockBackend();
});

/**
 * SC #5 / WEB-04 / WEB-08 — /pricing renders in RU with the Pro card +
 * CurrencySwitcher persists the user's choice in the pricing_currency
 * cookie. The default RU currency is RUB (D-04); switching to USD
 * rewrites the URL with ?currency=USD and stores it.
 */
test("SC#5: /pricing renders + currency switcher persists choice in cookie", async ({
  page,
}) => {
  await mockPlans(page);
  await page.goto("/ru/pricing/", { waitUntil: "networkidle" });

  // Heading text varies per locale; just assert the H1 exists with non-empty content.
  await expect(page.locator("h1").first()).toBeVisible();

  // Pro card MUST render (mock backend supplies a "Pro" plan).
  await expect(page.getByText(/Pro/).first()).toBeVisible();

  // Wait for hydration — the CurrencySwitcher is a client component, the
  // onClick handler only fires once React hydrates the chip.
  await page.waitForFunction(
    () => {
      const btn = Array.from(
        document.querySelectorAll('button'),
      ).find((b) => b.textContent?.trim() === 'USD');
      // The button's onClick is the proof of hydration; the easiest
      // signal is whether the button has React's hydration property.
      return !!btn && !btn.hasAttribute('disabled');
    },
    { timeout: 5_000 },
  );

  // Switcher: chip text is the literal "USD" / "EUR" / "RUB" from
  // pricing.currency.*. The CurrencySwitcher uses router.replace (no
  // hard navigation event), so we assert the cookie + URL state by
  // expect.poll rather than waitForURL.
  await page.getByRole("button", { name: "USD", exact: true }).click();

  // Cookie write is synchronous in the click handler (CurrencySwitcher's
  // document.cookie assignment runs BEFORE the start() transition). Read
  // the cookie from the page's own document.cookie rather than via the
  // Playwright CDP context cookie API — CDP has been observed to miss
  // host-only cookies written via document.cookie from a click handler
  // within the 10s polling window, while document.cookie reads always
  // reflect the live jar from the page's perspective.
  await expect
    .poll(
      async () => {
        const raw = await page.evaluate(() => document.cookie);
        const match = raw
          .split(";")
          .map((p) => p.trim())
          .find((p) => p.startsWith("pricing_currency="));
        return match ? match.split("=")[1] : undefined;
      },
      { timeout: 10_000 },
    )
    .toBe("USD");

  await expect.poll(() => page.url(), { timeout: 10_000 }).toMatch(/currency=USD/);
});

/**
 * SC #2 / WEB-05 — Logged-in user with `?checkout=auto&plan=...` hits
 * PricingClient's auto-checkout effect → POST /api/v1/checkout (Plan 03
 * proxy) within ~one HTTP round-trip. The follow-on `window.location.href`
 * to the lava.top payment_url is verified by intercepting the navigation
 * request, not the response — Playwright cannot reliably fulfil top-level
 * navigations to non-baseURL hosts in Chromium when they originate from
 * `window.location.href` assignment, but page.waitForRequest WILL catch
 * the navigation attempt itself.
 */
test("SC#2: ?checkout=auto fires POST /checkout and triggers lava.top navigation", async ({
  page,
  context,
}) => {
  await context.addCookies([
    {
      name: "rv_at",
      value: "test_at",
      domain: "localhost",
      path: "/",
      httpOnly: true,
      sameSite: "Strict",
    },
    {
      name: "rv_rt",
      value: "test_rt",
      domain: "localhost",
      path: "/",
      httpOnly: true,
      sameSite: "Strict",
    },
  ]);

  await mockPlans(page);
  await mockCheckout(page, {
    paymentUrl: "https://gate.lava.top/pay/abc",
    invoiceId: "inv-123",
  });

  // Block gate.lava.top entirely so the browser never leaves the test
  // sandbox (request fails fast; navigation is recorded by the
  // waitForRequest below regardless of fulfilment outcome).
  await context.route("https://gate.lava.top/**", (r) => r.abort());

  // Both the inbound POST to the proxy AND the subsequent navigation
  // request to gate.lava.top must fire — capture both.
  const checkoutPosted = page.waitForRequest(
    (req) =>
      req.url().endsWith("/api/v1/checkout") && req.method() === "POST",
    { timeout: 15_000 },
  );
  const lavaRequested = page.waitForRequest(
    (req) => req.url().startsWith("https://gate.lava.top/"),
    { timeout: 15_000 },
  );

  await page.goto(
    "/ru/pricing/?checkout=auto&plan=pro&period=monthly&currency=USD",
  );
  await checkoutPosted;
  await lavaRequested;
});

/**
 * SC #3 / WEB-04 — Logged-out PlanCard renders the Pro CTA as a link
 * to /login?next=/pricing&plan=pro&period=monthly&currency=<C>.
 */
test("SC#3: logged-out PlanCard renders Pro CTA → /login?next=/pricing&plan=...", async ({
  page,
}) => {
  await mockPlans(page);
  await page.goto("/ru/pricing/");

  // The Pro CTA href encodes the checkout-resume intent. After the user
  // signs in, /auth/callback honours `next` and appends `checkout=auto`
  // so PricingClient picks up where the click left off.
  const proLink = page
    .getByRole("link", { name: /Pro/ })
    .filter({ has: page.locator("text=/Pro/i") })
    .first();
  await expect(proLink).toBeVisible({ timeout: 10_000 });
  const href = await proLink.getAttribute("href");
  expect(href, "Pro CTA has href").toBeTruthy();
  expect(href!).toMatch(/\/login/);
  expect(href!).toMatch(/next=.*pricing/);
  expect(href!).toMatch(/plan=pro/);
  expect(href!).toMatch(/period=monthly/);
});

/**
 * SC #3 follow-on — The /login page accepts ?next=/pricing&plan=...
 * search params and the Apple button form carries them as hidden inputs
 * into the start-oauth Server Action. Verified by inspecting the rendered
 * <input type=hidden> values in the form.
 *
 * The full OAuth round-trip (Apple authorize → form_post → callback →
 * /pricing?checkout=auto → /api/v1/checkout → lava.top) cannot be
 * faithfully reproduced under Playwright's top-level-navigation
 * interception limits; the integration test against the live mock backend
 * (Phase 4-08 follow-up) will cover that path.
 */
test("SC#3: /login carries next+plan+period+currency hidden inputs into the OAuth form", async ({
  page,
}) => {
  await page.goto(
    "/ru/login?next=/pricing&plan=pro&period=monthly&currency=USD",
  );

  // The AuthButtonApple component renders a <form> containing hidden
  // inputs for next/plan/period/currency — start-oauth.ts then echoes
  // these into the state payload of the provider authorize URL.
  const nextInput = page.locator('input[name="next"]').first();
  const planInput = page.locator('input[name="plan"]').first();
  const periodInput = page.locator('input[name="period"]').first();
  const currencyInput = page.locator('input[name="currency"]').first();

  await expect(nextInput).toHaveValue("/pricing");
  await expect(planInput).toHaveValue("pro");
  await expect(periodInput).toHaveValue("monthly");
  await expect(currencyInput).toHaveValue("USD");
});
