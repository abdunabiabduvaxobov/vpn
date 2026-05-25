import { test, expect } from "@playwright/test";

import { mockPlans, resetMockBackend } from "./_fixtures/backend-mock";

test.beforeEach(async () => {
  await resetMockBackend();
});

/**
 * SC #6 / WEB-09 — Logged-out navbar shows "Pricing" + "Login".
 */
test("SC#6 logged-out: navbar shows Pricing + Login", async ({ page }) => {
  await mockPlans(page);
  await page.goto("/ru/pricing/");

  await expect(
    page.getByRole("link", { name: /Тарифы|Pricing|Precios/ }).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: /Войти|Login|Iniciar/ }).first(),
  ).toBeVisible();
});

/**
 * SC #6 / WEB-09 — Logged-in navbar shows "Pricing" + "Dashboard" + a
 * Sign-out trigger (inside the UserMenu popover).
 */
test("SC#6 logged-in: navbar shows Pricing + Dashboard + Sign-out (via avatar menu)", async ({
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
  ]);
  await mockPlans(page);
  await page.goto("/ru/pricing/");

  await expect(
    page.getByRole("link", { name: /Тарифы|Pricing|Precios/ }).first(),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: /Кабинет|Dashboard|Panel/ }).first(),
  ).toBeVisible();

  // UserMenu trigger — base-ui Popover renders the popup inside a Portal.
  // Click the avatar trigger, then assert the Sign-out button appears
  // anywhere in the page (Portal content is still part of page.locator).
  const trigger = page.getByRole("button", { name: /Account menu/i });
  await expect(trigger).toBeVisible();
  await trigger.click();

  // The Sign-out button is rendered as <button type="submit"> inside a
  // <form action="/api/auth/logout">. Match by text content (locale aware)
  // — `name` matching covers both role=button (the <button>) and any
  // ARIA-button surfaced by base-ui.
  await expect(
    page
      .locator('button[type="submit"], [role="button"]')
      .filter({ hasText: /Выйти|Sign out|Cerrar/ })
      .first(),
  ).toBeVisible({ timeout: 5_000 });
});
