import { defineConfig, devices } from "@playwright/test";

/**
 * Phase 4 Plan 04-08 — Playwright config.
 *
 * Runs against the Next.js standalone server (`.next/standalone/server.js`).
 * Build the server first (`npm run build`); the webServer block boots it
 * with placeholder env values that satisfy env.ts's fail-fast validation
 * while keeping the backend hop unreachable — Playwright's page.route()
 * fixtures (see e2e/_fixtures/backend-mock.ts) intercept every /api/v1/*
 * call so the suite never needs the real Go backend.
 *
 * BACKEND_API_URL is pinned to http://127.0.0.1:1 (deliberately blackholed
 * port 1 — guarantees any un-mocked call fails fast and visibly rather
 * than hitting a real service).
 */
export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  retries: 0,
  forbidOnly: !!process.env.CI,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: "http://localhost:3000",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command:
      "BACKEND_API_URL=http://127.0.0.1:1 " +
      "REVALIDATE_SECRET=test-revalidate-secret " +
      "APPLE_SERVICE_ID=test.web " +
      "APPLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=apple " +
      "GOOGLE_CLIENT_ID_WEB=test.google " +
      "GOOGLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=google " +
      "APP_URL=http://localhost:3000 " +
      "NODE_ENV=production " +
      "HOSTNAME=127.0.0.1 " +
      "PORT=3000 " +
      "node .next/standalone/server.js",
    url: "http://localhost:3000/ru/",
    timeout: 60_000,
    reuseExistingServer: !process.env.CI,
  },
});
