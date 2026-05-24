import type { Page } from "@playwright/test";

/**
 * Backend mocks for Phase 4 Playwright smoke suite.
 *
 * Every helper calls `page.route()` to intercept browser-issued HTTP. The
 * Node proxy still runs the cookies→Bearer transform — the upstream fetch
 * is what gets caught (matched against the absolute backend URL the proxy
 * forwards to). This means the proxy's session-cookie writes, refresh
 * rotation, body-size cap, and timeout behaviour all execute against the
 * mock, exercising the real Plan 03 code path.
 *
 * Pattern: each fn returns the route promise so the caller can `await`
 * setup before navigation. Sequence-based mocks (mockInvoicePolling)
 * track call count via a closed-over counter.
 */

export async function mockPlans(page: Page) {
  await page.route("**/api/v1/plans*", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        plans: [
          {
            code: "free",
            name: "Free",
            is_system: true,
            device_limit: 1,
            monthly_traffic_mb: 5120,
            server_countries: ["NL"],
            offers: [],
          },
          {
            code: "pro",
            name: "Pro",
            is_system: true,
            device_limit: 5,
            monthly_traffic_mb: 0,
            server_countries: ["NL", "DE", "US"],
            offers: [
              {
                period: "monthly",
                price: 4.99,
                currency: "USD",
                lava_offer_id: "uuid-monthly",
              },
            ],
          },
        ],
      }),
    });
  });
}

export async function mockOauthExchange(
  page: Page,
  opts: { email: string; planId: string },
) {
  const body = JSON.stringify({
    access_token: buildMockJwt({ sub: "u1", plan_id: opts.planId }),
    refresh_token: "test_rt",
    expires_in: 300,
    user: { id: "u1", email: opts.email, plan_id: opts.planId },
  });

  await page.route("**/api/v1/auth/apple", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body }),
  );
  await page.route("**/api/v1/auth/google", (route) =>
    route.fulfill({ status: 200, contentType: "application/json", body }),
  );
}

/**
 * Mock /api/v1/auth/refresh used by the Plan 03 proxy AND by Plan 07's
 * force-refresh trigger on `status=paid`. Returns a NEW access_token whose
 * embedded plan_id claim equals opts.planId so the proxy's
 * decodePlanIdFromJwt re-issues rv_user with the upgraded plan (B2 fix).
 */
export async function mockAuthRefresh(page: Page, opts: { planId: string }) {
  await page.route("**/api/v1/auth/refresh", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        access_token: buildMockJwt({ sub: "u1", plan_id: opts.planId }),
        refresh_token: "rotated_rt",
        expires_in: 300,
      }),
    }),
  );
}

export async function mockCheckout(
  page: Page,
  opts: { paymentUrl: string; invoiceId: string },
) {
  await page.route("**/api/v1/checkout", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        payment_url: opts.paymentUrl,
        invoice_id: opts.invoiceId,
      }),
    }),
  );
}

export async function mockInvoicePolling(
  page: Page,
  opts: { id: string; sequence: string[] },
) {
  let i = 0;
  await page.route(new RegExp(`/api/v1/invoices/${opts.id}.*$`), (route) => {
    const status = opts.sequence[Math.min(i, opts.sequence.length - 1)];
    i++;
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ id: opts.id, status, plan_code: "pro" }),
    });
  });
}

/**
 * Intercept the Apple / Google authorize redirect and immediately
 * auto-submit a form to /auth/callback with a placeholder id_token and
 * the original state. Lets the suite exercise the full OAuth round-trip
 * without leaving the browser. The state comes from the rv_oauth_state
 * cookie the start-oauth Server Action set just before the redirect, and
 * the form post lands at the locale-less /auth/callback route that
 * Plan 04-04 owns.
 */
export async function mockOAuthRedirect(page: Page) {
  await page.route(
    /https:\/\/(appleid\.apple\.com|accounts\.google\.com)\//,
    async (route) => {
      const url = new URL(route.request().url());
      const state = url.searchParams.get("state") ?? "";
      const provider = url.host.includes("apple") ? "apple" : "google";
      const html = `<!doctype html><html><body>
<form method="POST" action="http://localhost:3000/auth/callback?provider=${provider}">
  <input name="id_token" value="mock_id_token" />
  <input name="state" value="${state}" />
</form>
<script>document.forms[0].submit()</script>
</body></html>`;
      await route.fulfill({ status: 200, contentType: "text/html", body: html });
    },
  );
}

/**
 * Build an unsigned mock JWT (header.payload.signature) — the Plan 03
 * `decodePlanIdFromJwt` deliberately skips signature verification because
 * the JWT comes back from the same trusted backend hop. So this fake
 * signature is fine for surface tests of the rv_user freshness path.
 */
function buildMockJwt(payload: Record<string, unknown>) {
  const header = base64url(JSON.stringify({ alg: "HS256", typ: "JWT" }));
  const nowSec = Math.floor(Date.now() / 1000);
  const body = base64url(
    JSON.stringify({ iat: nowSec, exp: nowSec + 300, ...payload }),
  );
  return `${header}.${body}.mocksig`;
}

function base64url(input: string) {
  return Buffer.from(input, "utf8")
    .toString("base64")
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");
}
