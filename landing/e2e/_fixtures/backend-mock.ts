import type { Page } from "@playwright/test";

/**
 * Fixture helpers for Phase 4 Playwright smoke suite.
 *
 * Architecture: the deterministic Phase 2/3 API surface is served by a
 * tiny Node HTTP server at http://127.0.0.1:4555 (e2e/_fixtures/run-mock-
 * backend.cjs). Playwright's `webServer` boots it before the Next.js
 * standalone bundle whose BACKEND_API_URL points at it. So the helpers
 * below are split into two camps:
 *
 *   • Browser-side interception (page.route) — for outbound navigations
 *     from the browser to provider domains (appleid.apple.com,
 *     accounts.google.com, gate.lava.top). The Next process never sees
 *     these so route() catches them cleanly.
 *
 *   • Test-control HTTP — call the mock backend's /__set_invoice or
 *     /__reset endpoints to drive deterministic state across the
 *     polling tests (mockInvoicePolling, resetMockBackend).
 *
 * Server-component fetches (fetchPlans inside /pricing, /dashboard) and
 * the Plan 03 `/api/[...path]` proxy reach the mock without any further
 * setup — that's the point of running it as a real HTTP server.
 */

export const MOCK_BACKEND_URL = "http://127.0.0.1:4555";

/**
 * Wipe the mock backend's in-memory state between tests.
 */
export async function resetMockBackend() {
  await fetch(`${MOCK_BACKEND_URL}/__reset`, { method: "POST" });
}

/**
 * Register a per-invoice polling sequence on the mock backend. Each GET
 * /api/v1/invoices/:id will return the next item in `sequence` until the
 * sequence is exhausted (then it sticks at the final value forever).
 *
 * This drives SC #4: ["pending","paid"] → flip after one poll;
 * Array(30).fill("pending") → always pending, exercises 30s timeout UI.
 */
export async function mockInvoicePolling(
  _page: Page,
  opts: { id: string; sequence: string[] },
) {
  await fetch(`${MOCK_BACKEND_URL}/__set_invoice`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(opts),
  });
}

/**
 * Stub the upstream POST to the mock backend's checkout endpoint. The
 * mock returns a fixed payment_url + invoice_id by default; this helper
 * lets a test override per-call to assert a specific URL.
 *
 * NOTE: the mock currently returns a single hardcoded shape — if a future
 * test needs different values per-test, extend run-mock-backend.cjs with
 * a /__set_checkout endpoint mirroring /__set_invoice. For now we only
 * assert that the value the mock returns lands the browser at lava.top.
 */
export async function mockCheckout(
  _page: Page,
  _opts: { paymentUrl: string; invoiceId: string },
) {
  // No-op: the mock backend always returns
  //   { payment_url: "https://gate.lava.top/pay/abc", invoice_id: "inv-123" }
  // The browser-side redirect interception (see test files) catches the
  // outbound navigation to lava.top — that's the surface under test.
}

/**
 * Mock /api/v1/auth/refresh response. The shared mock backend already
 * returns a JWT carrying plan_id=pro from /api/v1/auth/refresh, so this
 * helper is a no-op kept for spec-readability. (Plan 04-08 wanted six
 * named helpers — keeping the import alive avoids reworking spec bodies
 * if a future change needs per-test override.)
 */
export async function mockAuthRefresh(
  _page: Page,
  _opts: { planId: string },
) {
  // No-op: see /api/v1/auth/refresh in run-mock-backend.cjs — JWT already
  // carries plan_id=pro for the B2 force-refresh assertion path.
}

/**
 * Same pattern: the mock backend already returns a free-tier user from
 * /api/v1/auth/{apple,google}. Helper preserved for spec-readability.
 */
export async function mockOauthExchange(
  _page: Page,
  _opts: { email: string; planId: string },
) {
  // No-op: see /api/v1/auth/apple + /api/v1/auth/google in
  // run-mock-backend.cjs — returns alice@example.com with plan_id=free.
}

/**
 * Same pattern. The mock backend returns the plans catalog; this helper
 * preserved for spec-readability.
 */
export async function mockPlans(_page: Page) {
  // No-op: see /api/v1/plans in run-mock-backend.cjs.
}

/**
 * Intercept the Apple / Google authorize redirect and immediately
 * auto-submit a form to /auth/callback with a placeholder id_token and
 * the original state. This is the one helper that must stay on the
 * browser side — the navigation to appleid.apple.com / accounts.google.com
 * happens in the browser context, never traverses the Next process.
 *
 * The form POSTs back to /auth/callback?provider=... which lives in the
 * Next process; Plan 04-04's route.ts owns the receiver, does the
 * constant-time state compare against rv_oauth_state, calls the backend
 * exchange (against the mock), and sets rv_at/rv_rt/rv_user on response
 * before redirecting to /<locale>/dashboard.
 */
export async function mockOAuthRedirect(page: Page) {
  const handler = async (route: import("@playwright/test").Route) => {
    const url = new URL(route.request().url());
    const state = url.searchParams.get("state") ?? "";
    const provider = url.host.includes("apple") ? "apple" : "google";
    const html = `<!doctype html><html><body>
<form method="POST" action="http://localhost:3000/auth/callback?provider=${provider}" id="autoform">
  <input name="id_token" value="mock_id_token" />
  <input name="state" value="${state}" />
</form>
<script>document.getElementById("autoform").submit()</script>
</body></html>`;
    await route.fulfill({ status: 200, contentType: "text/html", body: html });
  };
  // Use context.route — required for intercepting top-level frame doc
  // navigations (page.route does not always catch these in Chromium when
  // the navigation is initiated by a server-side redirect response).
  await page.context().route("https://appleid.apple.com/**", handler);
  await page.context().route("https://accounts.google.com/**", handler);
}
