import { test, expect } from "@playwright/test";

import {
  mockAuthRefresh,
  mockInvoicePolling,
  mockPlans,
  resetMockBackend,
} from "./_fixtures/backend-mock";

test.beforeEach(async () => {
  await resetMockBackend();
});

/**
 * SC #4 (happy) / WEB-06 — /pay/success polls /api/v1/invoices/:id and
 * flips to "Pro is active!" within ~2 seconds of the webhook landing
 * (mock returns "paid" on poll #2). The B2 fix REQUIRES the page to POST
 * /api/v1/auth/refresh on `status === "paid"` BEFORE flipping to the
 * active view — otherwise rv_user.planId still claims "free" on the next
 * navigation to /dashboard.
 */
test("SC#4 happy: polls → paid → force-refresh fires → 'Pro active' renders", async ({
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
  await mockInvoicePolling(page, {
    id: "inv-123",
    sequence: ["pending", "paid"],
  });
  await mockAuthRefresh(page, { planId: "pro" });

  // B2 assertion — record that POST /api/v1/auth/refresh fires.
  const refreshFired = page.waitForRequest(
    (req) =>
      req.url().endsWith("/api/v1/auth/refresh") && req.method() === "POST",
    { timeout: 15_000 },
  );

  await page.goto("/ru/pay/success/?invoiceId=inv-123");
  await refreshFired;

  // pay.success.active key — "Pro активирован!" / "Pro is active!" /
  // "Pro está activo!" — accept any.
  await expect(
    page.getByText(/активир|Pro is active|Pro est? activ/i),
  ).toBeVisible({ timeout: 10_000 });
});

/**
 * SC #4 (timeout) — 30s of "pending" → "takingLonger" UI fires.
 *
 * W2 fix: setTimeout(45_000) keeps the assertion reachable since the page
 * waits a wall-clock 30s before flipping the view. Trade-off considered:
 * page.clock.fastForward avoids the wait but requires the polling impl to
 * use page.clock-controlled time. Not worth the coupling for one test.
 */
test("SC#4 timeout: pending forever → 'taking longer / we'll email you'", async ({
  page,
  context,
}) => {
  test.setTimeout(45_000);

  await context.addCookies([
    {
      name: "rv_at",
      value: "test_at",
      domain: "localhost",
      path: "/",
      httpOnly: true,
      sameSite: "Strict",
    },
  ]);
  await mockPlans(page);
  await mockInvoicePolling(page, {
    id: "inv-456",
    sequence: Array(30).fill("pending"),
  });

  await page.goto("/ru/pay/success/?invoiceId=inv-456");

  // pay.success.takingLonger.heading "Платёж всё ещё обрабатывается…"
  await expect(
    page.getByText(/обрабатыва|takingLonger|processing|longer|email/i),
  ).toBeVisible({ timeout: 35_000 });
});
