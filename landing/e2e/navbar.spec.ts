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

  // Wait for the Popover's Portal content to mount before asserting the
  // testid — base-ui Popover renders into a Portal that is appended to
  // the body asynchronously after the trigger click. The data-testid on
  // the form submit button is the stable selector that survives any
  // future base-ui DOM-structure changes.
  await expect(
    page.getByTestId("sign-out-button"),
  ).toBeVisible({ timeout: 5_000 });
});
