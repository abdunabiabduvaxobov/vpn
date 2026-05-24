import { defineConfig, devices } from "@playwright/test";

/**
 * Phase 4 Plan 04-08 — Playwright config.
 *
 * Two-server topology:
 *   1. mock-backend (port 4555) — substitutes for the Go API. Both the
 *      Node proxy (browser → /api/v1/* → BACKEND_API_URL) and server
 *      components (fetchPlans inside /pricing, /dashboard) hit this.
 *   2. landing-node (port 3000) — the Next.js standalone bundle built by
 *      `npm run build`. `BACKEND_API_URL` points at the mock above.
 *
 * Why a Node mock server instead of `page.route()` alone: server-side
 * fetches (server components, the /api/[...path] proxy) run inside the
 * Next process and never touch the browser, so page.route() can't see
 * them. A real HTTP listener fed by /__set_invoice is the cleanest path
 * to deterministic full-stack tests.
 *
 * page.route() is still used for the BROWSER-side mocks where it does
 * apply — intercepting outbound navigations to appleid.apple.com /
 * accounts.google.com / gate.lava.top.
 */
const MOCK_BACKEND_URL = "http://127.0.0.1:4555";

export default defineConfig({
  testDir: "./e2e",
  testIgnore: ["**/_fixtures/**"],
  timeout: 45_000,
  retries: 0,
  forbidOnly: !!process.env.CI,
  // Single worker — the mock-backend is a shared HTTP process whose
  // /__set_invoice state must not be mutated concurrently by parallel
  // tests. Smoke suite is small (8 tests, ~50s wall clock) so this is fine.
  workers: 1,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: "http://localhost:3000",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: [
    {
      command: "node ./e2e/_fixtures/run-mock-backend.cjs",
      url: `${MOCK_BACKEND_URL}/__reset`,
      reuseExistingServer: !process.env.CI,
      timeout: 15_000,
    },
    {
      command:
        `BACKEND_API_URL=${MOCK_BACKEND_URL} ` +
        "REVALIDATE_SECRET=test-revalidate-secret " +
        "APPLE_SERVICE_ID=test.web " +
        "APPLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=apple " +
        "GOOGLE_CLIENT_ID_WEB=test.google " +
        "GOOGLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=google " +
        "APP_URL=http://localhost:3000 " +
        // Runtime NODE_ENV=development so env.IS_PROD=false → cookies are
        // written WITHOUT the Secure flag (http://localhost has no TLS).
        // The build itself was already done with production; only the
        // run-time switch matters here.
        "NODE_ENV=development " +
        "HOSTNAME=127.0.0.1 " +
        "PORT=3000 " +
        "node .next/standalone/server.js",
      url: "http://localhost:3000/ru/",
      timeout: 60_000,
      reuseExistingServer: !process.env.CI,
    },
  ],
});
