---
phase: 04-landing-surfaces
plan: 08
type: execute
wave: 5
depends_on: [04-04, 04-05, 04-06, 04-07]
files_modified:
  - landing/Dockerfile
  - landing/docker-compose.landing.yml
  - landing/nginx/vpn.mydayai.uz.conf
  - landing/playwright.config.ts
  - landing/package.json
  - landing/e2e/login.spec.ts
  - landing/e2e/pricing.spec.ts
  - landing/e2e/pay-success.spec.ts
  - landing/e2e/navbar.spec.ts
  - landing/e2e/_fixtures/backend-mock.ts
  - landing/.dockerignore
autonomous: true
requirements:
  - WEB-01
  - WEB-02
  - WEB-03
  - WEB-04
  - WEB-05
  - WEB-06
  - WEB-07
  - WEB-09
must_haves:
  truths:
    - "landing/Dockerfile builds a Node 22 image running the standalone Next.js server"
    - "landing/docker-compose.landing.yml adds the landing-node container alongside the existing nginx service"
    - "landing/nginx/vpn.mydayai.uz.conf routes /login /dashboard /pricing /pay/* /auth/* /api/* to the Node container on a private port; / and other marketing pages stay served from the static export OR pass through to Node (we choose all-Node for v2.2.0 simplicity)"
    - "Playwright E2E specs assert each ROADMAP success criterion against a backend mock"
    - "`npm run test:e2e` exits 0"
  artifacts:
    - path: "landing/Dockerfile"
      provides: "Production image — multi-stage build, Node 22 alpine, non-root user, exposes 3000"
    - path: "landing/docker-compose.landing.yml"
      provides: "Compose override for the landing-node service"
    - path: "landing/nginx/vpn.mydayai.uz.conf"
      provides: "Updated nginx routing (D-02)"
    - path: "landing/e2e/*.spec.ts"
      provides: "Playwright assertions for each WEB-XX requirement"
    - path: "landing/e2e/_fixtures/backend-mock.ts"
      provides: "Playwright route.fulfill helper that mocks /api/v1/* responses"
  key_links:
    - from: "landing/nginx/vpn.mydayai.uz.conf"
      to: "landing-node container"
      via: "proxy_pass to upstream"
      pattern: "proxy_pass\\s+http://landing-node"
    - from: "landing/e2e/*.spec.ts"
      to: "landing standalone server"
      via: "Playwright webServer config"
      pattern: "webServer"
tags: [deploy, docker, nginx, e2e, smoke]
---

<objective>
Ship Phase 4 to production-shaped infrastructure and verify every WEB-XX requirement with automated Playwright tests. Concretely:

1. Build a Dockerfile that produces the `landing-node` container from the standalone output (Plan 01).
2. Add a `docker-compose.landing.yml` overlay that runs `landing-node` next to the existing static nginx (D-02).
3. Update the existing nginx vhost to proxy app paths (`/login`, `/dashboard`, `/pricing`, `/pay/*`, `/auth/*`, `/api/*`) to the Node container — keeping the existing static paths working.
4. Stand up Playwright with a backend-mock fixture so the E2E suite asserts the 6 ROADMAP success criteria + WEB-01..WEB-09 without needing the Go backend.

Purpose: deploys the Phase 4 work end-to-end, validates every requirement, and produces the smoke test bundle that becomes the regression baseline for Phase 5+ (mobile work depends on this same backend surface).

Output: A `docker compose -f docker-compose.yml -f landing/docker-compose.landing.yml up -d` brings the entire landing stack up; nginx serves marketing pages + proxies app pages to Node; Playwright smoke `npm run test:e2e` exits 0 with 8+ green tests covering every WEB-XX.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/04-landing-surfaces/04-CONTEXT.md
@.planning/phases/04-landing-surfaces/04-UI-SPEC.md
@.planning/phases/04-landing-surfaces/04-01-foundation-i18n-standalone-PLAN.md
@.planning/phases/04-landing-surfaces/04-03-node-proxy-cookies-refresh-PLAN.md
@.planning/phases/04-landing-surfaces/04-04-login-oauth-callback-PLAN.md
@.planning/phases/04-landing-surfaces/04-05-pricing-plans-isr-revalidate-PLAN.md
@.planning/phases/04-landing-surfaces/04-06-dashboard-signout-PLAN.md
@.planning/phases/04-landing-surfaces/04-07-checkout-pay-success-fail-PLAN.md
@landing/nginx/vpn.mydayai.uz.conf
@landing/package.json
@landing/next.config.ts

<interfaces>
<!-- Locked CONTEXT.md decisions -->
- D-01: output 'standalone' → landing/.next/standalone is the runnable bundle
- D-02: hybrid routing — nginx terminates TLS + proxies app paths to a private port (we pick 3000 on container, exposed to nginx via Docker network). Marketing pages currently live in /opt/vpn/landing/dist (static export). For v2.2.0, the simpler model is: drop the static export and serve ALL pages from Node behind nginx as a reverse proxy. This avoids dual-path bookkeeping. Document this simplification in the SUMMARY.

Existing nginx config: listens on 9443 (port 443 is the xray VPN). The HTTP 80 redirect goes to 9443. We keep that exactly — only the location blocks change.

Phase 4 environment matrix (required to start the container):
- BACKEND_API_URL (Plan 01)
- REVALIDATE_SECRET (Plan 01)
- COOKIE_DOMAIN (Plan 01 — optional)
- APPLE_SERVICE_ID, APPLE_REDIRECT_URI (Plan 04)
- GOOGLE_CLIENT_ID_WEB, GOOGLE_REDIRECT_URI (Plan 04)
- APP_URL (Plan 04)
- NODE_ENV=production
- HOSTNAME=0.0.0.0  (default for Next standalone)
- PORT=3000

Playwright backend-mock pattern:
- Use `page.route("**/api/v1/**", route => { ... })` to intercept proxied calls
- Mocks live in landing/e2e/_fixtures/backend-mock.ts as reusable fns:
  - mockPlans(page) → returns 2 plans (free + pro)
  - mockOauthExchange(page, { email, planId }) → returns 200 with tokens + user object
  - mockInvoice(page, { id, statusSequence: ["pending","pending","paid"] }) → returns each call's nth status
  - mockCheckout(page, { paymentUrl }) → returns 200 with paymentUrl
- Backend mock intercepts go on the BROWSER side (Playwright), so the Node proxy still runs the cookies → Bearer transformation against the mock. For OAuth provider URLs (appleid.apple.com, accounts.google.com), use `page.route` to intercept and immediately POST to /auth/callback with a known id_token (since real OAuth provider calls won't work in CI).
</interfaces>
</context>

<tasks>

<task type="auto" tdd="false">
  <name>Task 1: Dockerfile + compose overlay + nginx routing update for the landing-node container</name>
  <files>landing/Dockerfile, landing/docker-compose.landing.yml, landing/nginx/vpn.mydayai.uz.conf, landing/.dockerignore</files>
  <read_first>
    - landing/nginx/vpn.mydayai.uz.conf (existing — port 9443 listener; static root /opt/vpn/landing/dist)
    - landing/next.config.ts (standalone output)
    - landing/package.json
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-01, D-02)
  </read_first>
  <action>
    Create landing/Dockerfile (multi-stage):
    ```dockerfile
    # syntax=docker/dockerfile:1.7
    FROM node:22-alpine AS deps
    WORKDIR /app
    COPY package.json package-lock.json* ./
    RUN npm ci

    FROM node:22-alpine AS builder
    WORKDIR /app
    COPY --from=deps /app/node_modules ./node_modules
    COPY . .
    # Build-time env vars (these become baked into static chunks); secrets must NOT be passed here.
    ENV NEXT_TELEMETRY_DISABLED=1
    # The build needs the OAuth + revalidate vars satisfied even if they're per-env values at runtime.
    # Pass placeholder values; runtime overrides them.
    ARG BACKEND_API_URL=https://placeholder
    ARG REVALIDATE_SECRET=placeholder
    ARG APPLE_SERVICE_ID=placeholder
    ARG APPLE_REDIRECT_URI=https://placeholder/auth/callback?provider=apple
    ARG GOOGLE_CLIENT_ID_WEB=placeholder
    ARG GOOGLE_REDIRECT_URI=https://placeholder/auth/callback?provider=google
    ARG APP_URL=https://placeholder
    ENV BACKEND_API_URL=$BACKEND_API_URL REVALIDATE_SECRET=$REVALIDATE_SECRET APPLE_SERVICE_ID=$APPLE_SERVICE_ID APPLE_REDIRECT_URI=$APPLE_REDIRECT_URI GOOGLE_CLIENT_ID_WEB=$GOOGLE_CLIENT_ID_WEB GOOGLE_REDIRECT_URI=$GOOGLE_REDIRECT_URI APP_URL=$APP_URL
    RUN npm run build

    FROM node:22-alpine AS runner
    WORKDIR /app
    ENV NODE_ENV=production
    ENV NEXT_TELEMETRY_DISABLED=1
    RUN addgroup --system --gid 1001 nodejs && adduser --system --uid 1001 nextjs
    COPY --from=builder /app/public ./public
    COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
    COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static
    USER nextjs
    EXPOSE 3000
    ENV HOSTNAME=0.0.0.0 PORT=3000
    CMD ["node", "server.js"]
    ```

    Create landing/.dockerignore:
    ```
    node_modules
    .next
    .git
    .env*
    !.env.example
    e2e
    *.md
    ```

    Create landing/docker-compose.landing.yml (overlay — applied with `-f` alongside the existing top-level compose):
    ```yaml
    services:
      landing-node:
        build:
          context: ./landing
          dockerfile: Dockerfile
        image: rise-vpn-landing-node:latest
        restart: unless-stopped
        environment:
          NODE_ENV: production
          PORT: "3000"
          HOSTNAME: "0.0.0.0"
          BACKEND_API_URL: ${BACKEND_API_URL:?}
          REVALIDATE_SECRET: ${REVALIDATE_SECRET:?}
          COOKIE_DOMAIN: ${COOKIE_DOMAIN:-}
          APPLE_SERVICE_ID: ${APPLE_SERVICE_ID:?}
          APPLE_REDIRECT_URI: ${APPLE_REDIRECT_URI:?}
          GOOGLE_CLIENT_ID_WEB: ${GOOGLE_CLIENT_ID_WEB:?}
          GOOGLE_REDIRECT_URI: ${GOOGLE_REDIRECT_URI:?}
          APP_URL: ${APP_URL:?}
        networks:
          - landing-net
        expose:
          - "3000"
    networks:
      landing-net:
        driver: bridge
    ```

    Update landing/nginx/vpn.mydayai.uz.conf — add the following AFTER the existing `location = /` block (which currently 302's to /ru/) and BEFORE any existing `location /_next/static/` block:
    ```nginx
    # Phase 4 — app pages + API proxy to landing-node container.
    upstream landing_node {
        server landing-node:3000;
        keepalive 16;
    }

    # All app pages — proxied to Node.
    location ~ ^/(?:[a-z]{2}/)?(?:login|dashboard|pricing|pay/(?:success|fail))/?$ {
        proxy_pass http://landing_node;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_read_timeout 60s;
    }

    # OAuth callback (locale-less).
    location ^~ /auth/callback {
        proxy_pass http://landing_node;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 30s;
    }

    # Node proxy + revalidate webhook + logout (all /api/*).
    location ^~ /api/ {
        proxy_pass http://landing_node;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_request_buffering off;
        client_max_body_size 64k;     # matches PERF-09 + Plan 03's BODY_BYTES_LIMIT
        proxy_read_timeout 30s;
    }
    ```

    NOTE: For v2.2.0, decide whether to drop the static `root /opt/vpn/landing/dist` and proxy ALL pages to Node, or keep the static export for marketing pages. Recommendation in SUMMARY: drop static, proxy all → simpler, single deploy. If the operator prefers to keep static for marketing, the above location blocks are surgical enough that the existing static `root` keeps serving everything else. Document the choice.
  </action>
  <acceptance_criteria>
    - `grep -n 'FROM node:22' landing/Dockerfile` returns at least 3 matches (deps/builder/runner stages)
    - `grep -n 'standalone' landing/Dockerfile` returns 1 match
    - `grep -n 'USER nextjs' landing/Dockerfile` returns 1 match
    - `grep -n 'EXPOSE 3000' landing/Dockerfile` returns 1 match
    - `grep -n 'landing-node' landing/docker-compose.landing.yml` returns at least 1 match
    - `grep -n 'BACKEND_API_URL.*\${BACKEND_API_URL:?}' landing/docker-compose.landing.yml` returns 1 match (required env enforced)
    - `grep -n 'proxy_pass http://landing_node' landing/nginx/vpn.mydayai.uz.conf` returns at least 3 matches
    - `grep -n 'login\|dashboard\|pricing\|pay/(success\|fail)' landing/nginx/vpn.mydayai.uz.conf` returns at least 1 match (app paths)
    - `grep -n '/auth/callback' landing/nginx/vpn.mydayai.uz.conf` returns at least 1 match
    - `grep -n 'client_max_body_size 64k' landing/nginx/vpn.mydayai.uz.conf` returns 1 match
    - `cd landing && docker build --build-arg BACKEND_API_URL=https://x --build-arg REVALIDATE_SECRET=y --build-arg APPLE_SERVICE_ID=x --build-arg APPLE_REDIRECT_URI=https://x/cb --build-arg GOOGLE_CLIENT_ID_WEB=x --build-arg GOOGLE_REDIRECT_URI=https://x/cb --build-arg APP_URL=https://x -t rise-vpn-landing-node:test .` exits 0
  </acceptance_criteria>
  <done>Dockerfile builds the standalone bundle into a 22-alpine runner image, compose overlay enforces required env vars, nginx config proxies /login/dashboard/pricing/pay/*/auth/*/api/* to the Node container.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 2: Playwright setup — config, deps, backend-mock fixture, test scripts</name>
  <files>landing/playwright.config.ts, landing/package.json, landing/e2e/_fixtures/backend-mock.ts</files>
  <read_first>
    - landing/package.json (existing scripts + deps)
    - .planning/phases/04-landing-surfaces/04-04-login-oauth-callback-PLAN.md (Apple/Google authorize URL format)
    - .planning/phases/04-landing-surfaces/04-05-pricing-plans-isr-revalidate-PLAN.md (plans response shape)
    - .planning/phases/04-landing-surfaces/04-07-checkout-pay-success-fail-PLAN.md (checkout + invoice response shapes)
  </read_first>
  <action>
    Add devDependencies to landing/package.json: `@playwright/test`. Add scripts:
    - `"test:e2e": "playwright test"`
    - `"test:e2e:install": "playwright install --with-deps chromium"`

    Install: `cd landing && npm install --save-dev @playwright/test && npm run test:e2e:install`

    Create landing/playwright.config.ts:
    ```ts
    import { defineConfig, devices } from "@playwright/test";

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
        // Use `npm run start` against the built standalone server. Env vars supplied here are
        // the placeholders the build accepted; mocks intercept the upstream calls.
        command: "BACKEND_API_URL=http://127.0.0.1:1 REVALIDATE_SECRET=test-revalidate-secret APPLE_SERVICE_ID=test.web APPLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=apple GOOGLE_CLIENT_ID_WEB=test.google GOOGLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=google APP_URL=http://localhost:3000 NODE_ENV=production node .next/standalone/server.js",
        url: "http://localhost:3000/ru/",
        timeout: 60_000,
        reuseExistingServer: !process.env.CI,
      },
    });
    ```

    Create landing/e2e/_fixtures/backend-mock.ts:
    ```ts
    import type { Page, Route } from "@playwright/test";

    export async function mockPlans(page: Page) {
      await page.route("**/api/v1/plans*", async (route) => {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            plans: [
              { code: "free", name: "Free", is_system: true, device_limit: 1, monthly_traffic_mb: 5120, server_countries: ["NL"], offers: [] },
              { code: "pro",  name: "Pro",  is_system: true, device_limit: 5, monthly_traffic_mb: 0,    server_countries: ["NL","DE","US"], offers: [
                { period: "monthly", price: 4.99, currency: "USD", lava_offer_id: "uuid-monthly" }
              ] }
            ]
          }),
        });
      });
    }

    export async function mockOauthExchange(page: Page, opts: { email: string; planId: string }) {
      await page.route("**/api/v1/auth/apple", async (route) => route.fulfill({
        status: 200, contentType: "application/json",
        body: JSON.stringify({ access_token: "test_at", refresh_token: "test_rt", expires_in: 300, user: { id: "u1", email: opts.email, plan_id: opts.planId } }),
      }));
      await page.route("**/api/v1/auth/google", async (route) => route.fulfill({
        status: 200, contentType: "application/json",
        body: JSON.stringify({ access_token: "test_at", refresh_token: "test_rt", expires_in: 300, user: { id: "u1", email: opts.email, plan_id: opts.planId } }),
      }));
    }

    export async function mockCheckout(page: Page, opts: { paymentUrl: string; invoiceId: string }) {
      await page.route("**/api/v1/checkout", async (route) => route.fulfill({
        status: 200, contentType: "application/json",
        body: JSON.stringify({ payment_url: opts.paymentUrl, invoice_id: opts.invoiceId }),
      }));
    }

    export async function mockInvoicePolling(page: Page, opts: { id: string; sequence: string[] }) {
      let i = 0;
      await page.route(new RegExp(`/api/v1/invoices/${opts.id}.*$`), async (route) => {
        const status = opts.sequence[Math.min(i, opts.sequence.length - 1)];
        i++;
        await route.fulfill({
          status: 200, contentType: "application/json",
          body: JSON.stringify({ id: opts.id, status, plan_code: "pro" }),
        });
      });
    }

    export async function mockOAuthRedirect(page: Page) {
      // Intercept appleid.apple.com or accounts.google.com authorize URL and immediately POST back to /auth/callback.
      await page.route(/https:\/\/(appleid\.apple\.com|accounts\.google\.com)\//, async (route) => {
        const url = new URL(route.request().url());
        const state = url.searchParams.get("state") ?? "";
        const provider = url.host.includes("apple") ? "apple" : "google";
        // POST id_token + state back to /auth/callback.
        const body = `id_token=mock_id_token&state=${encodeURIComponent(state)}`;
        // Use Playwright's fulfill to issue a 302 to a synthetic page that POSTs the form. Easier: do the POST via APIRequest from the test.
        // Implementation: respond with a tiny HTML that auto-submits a form to /auth/callback.
        const html = `<!doctype html><html><body><form method="POST" action="http://localhost:3000/auth/callback?provider=${provider}"><input name="id_token" value="mock_id_token"/><input name="state" value="${state}"/></form><script>document.forms[0].submit()</script></body></html>`;
        await route.fulfill({ status: 200, contentType: "text/html", body: html });
      });
    }
    ```
  </action>
  <acceptance_criteria>
    - `grep -n '"@playwright/test"' landing/package.json` returns 1 match
    - `grep -n '"test:e2e"' landing/package.json` returns 1 match
    - `test -e landing/playwright.config.ts && grep -n 'webServer' landing/playwright.config.ts` returns at least 1 match
    - `test -e landing/e2e/_fixtures/backend-mock.ts`
    - `grep -n 'mockPlans\|mockOauthExchange\|mockCheckout\|mockInvoicePolling\|mockOAuthRedirect' landing/e2e/_fixtures/backend-mock.ts` returns at least 5 matches
    - `cd landing && npx playwright --version` exits 0 (Playwright installed)
  </acceptance_criteria>
  <done>Playwright is installed, config points at the standalone server with mock env, backend-mock fixture exports the five helpers above.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 3: Playwright specs — 4 files covering all 6 SCs + WEB-XX requirements</name>
  <files>landing/e2e/login.spec.ts, landing/e2e/pricing.spec.ts, landing/e2e/pay-success.spec.ts, landing/e2e/navbar.spec.ts</files>
  <read_first>
    - landing/e2e/_fixtures/backend-mock.ts (Task 2)
    - landing/playwright.config.ts (Task 2)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (all decisions)
    - .planning/ROADMAP.md (Phase 4 success criteria — the 6 truths the suite must assert)
  </read_first>
  <action>
    Create landing/e2e/login.spec.ts — covers SC #1 + WEB-01/WEB-02:
    ```ts
    import { test, expect } from "@playwright/test";
    import { mockOauthExchange, mockOAuthRedirect } from "./_fixtures/backend-mock";

    test("SC#1: Apple sign-in completes → /dashboard with HttpOnly-only session and no JWT in localStorage", async ({ page, context }) => {
      await mockOAuthRedirect(page);
      await mockOauthExchange(page, { email: "alice@example.com", planId: "free" });
      await page.goto("/ru/login");
      await expect(page.getByRole("button", { name: /Apple/i })).toBeVisible();
      await expect(page.getByRole("button", { name: /Google/i })).toBeVisible();
      await page.getByRole("button", { name: /Apple/i }).click();
      // Auto-submitting form from mockOAuthRedirect lands at /auth/callback which POSTs → set cookies → redirect to /ru/dashboard.
      await page.waitForURL("**/ru/dashboard", { timeout: 15_000 });
      await expect(page.getByText("alice@example.com")).toBeVisible();
      // Assertion #1: no JWT in localStorage.
      const lsLen = await page.evaluate(() => window.localStorage.length);
      expect(lsLen).toBe(0);
      // Assertion #2: session cookies exist + are HttpOnly.
      const cookies = await context.cookies();
      const at = cookies.find((c) => c.name === "rv_at");
      const rt = cookies.find((c) => c.name === "rv_rt");
      expect(at?.httpOnly).toBe(true);
      expect(rt?.httpOnly).toBe(true);
      expect(at?.sameSite).toBe("Strict");
    });

    test("CSRF mismatch on /auth/callback → /login?error=oauth_state", async ({ page }) => {
      await page.goto("/ru/login");
      // Manually POST with bad state.
      const res = await page.request.post("/auth/callback?provider=apple", {
        form: { id_token: "x", state: "badstate" },
        maxRedirects: 0,
      });
      expect(res.status()).toBeGreaterThanOrEqual(300);
      expect(res.headers().location ?? "").toMatch(/login\?error=oauth_state/);
    });
    ```

    Create landing/e2e/pricing.spec.ts — covers SC #2, SC #3, SC #5 + WEB-04/WEB-05/WEB-08:
    ```ts
    import { test, expect } from "@playwright/test";
    import { mockPlans, mockCheckout, mockOauthExchange, mockOAuthRedirect } from "./_fixtures/backend-mock";

    test("SC#5: /pricing renders in RU with RUB by default, switches to USD via switcher", async ({ page }) => {
      await mockPlans(page);
      await page.goto("/ru/pricing");
      await expect(page.getByRole("heading", { name: /Pro/i })).toBeVisible();
      await page.getByRole("button", { name: "USD" }).click();
      await page.waitForURL("**/ru/pricing?currency=USD**");
      const cookies = await page.context().cookies();
      expect(cookies.find((c) => c.name === "pricing_currency")?.value).toBe("USD");
    });

    test("SC#2: logged-in click 'Get Pro' → POST /checkout → redirect to lava paymentUrl in one HTTP round-trip", async ({ page, context }) => {
      // Pre-seed session cookies so visitor is logged in.
      await context.addCookies([
        { name: "rv_at", value: "test_at", domain: "localhost", path: "/", httpOnly: true, sameSite: "Strict" },
        { name: "rv_rt", value: "test_rt", domain: "localhost", path: "/", httpOnly: true, sameSite: "Strict" },
        // rv_user value is HMAC-signed; in test mode we accept that decodeSessionUser returns null and dashboard falls back gracefully.
      ]);
      await mockPlans(page);
      await mockCheckout(page, { paymentUrl: "https://gate.lava.top/pay/abc", invoiceId: "inv-123" });
      // Intercept the lava redirect so the test doesn't navigate offsite.
      await page.route("https://gate.lava.top/**", (r) => r.fulfill({ status: 200, body: "lava-mock" }));
      await page.goto("/ru/pricing?checkout=auto&plan=pro&period=monthly&currency=USD");
      await page.waitForURL("**/gate.lava.top/**", { timeout: 10_000 });
      expect(page.url()).toContain("gate.lava.top/pay/abc");
    });

    test("SC#3: logged-out 'Get Pro' → /login?next=/pricing&plan=...; after sign-in returns to /pricing with checkout=auto", async ({ page }) => {
      await mockPlans(page);
      await mockOAuthRedirect(page);
      await mockOauthExchange(page, { email: "bob@example.com", planId: "free" });
      await mockCheckout(page, { paymentUrl: "https://gate.lava.top/pay/xyz", invoiceId: "inv-xyz" });
      await page.route("https://gate.lava.top/**", (r) => r.fulfill({ status: 200, body: "lava-mock" }));

      await page.goto("/ru/pricing");
      await page.getByRole("link", { name: /Pro/i }).first().click();
      await page.waitForURL("**/ru/login**");
      expect(page.url()).toMatch(/next=.*pricing.*plan=pro.*period=monthly/);
      await page.getByRole("button", { name: /Apple/i }).click();
      // After OAuth completes, callback redirects to /ru/pricing?...&checkout=auto, which fires checkout, which redirects to lava.
      await page.waitForURL("**/gate.lava.top/**", { timeout: 20_000 });
    });
    ```

    Create landing/e2e/pay-success.spec.ts — covers SC #4 + WEB-06:
    ```ts
    import { test, expect } from "@playwright/test";
    import { mockInvoicePolling } from "./_fixtures/backend-mock";

    test("SC#4 (happy): /pay/success polls and flips to active within ~2s of webhook landing", async ({ page, context }) => {
      await context.addCookies([
        { name: "rv_at", value: "test_at", domain: "localhost", path: "/", httpOnly: true, sameSite: "Strict" },
      ]);
      await mockInvoicePolling(page, { id: "inv-123", sequence: ["pending", "paid"] });
      await page.goto("/ru/pay/success?invoiceId=inv-123");
      await expect(page.getByText(/Pro is active|активна|activ/i)).toBeVisible({ timeout: 6_000 });
      await expect(page.getByRole("link", { name: /dashboard|кабинет/i })).toBeVisible();
    });

    test("SC#4 (timeout): /pay/success shows 'we'll email you' after 30s of pending", async ({ page, context }) => {
      await context.addCookies([
        { name: "rv_at", value: "test_at", domain: "localhost", path: "/", httpOnly: true, sameSite: "Strict" },
      ]);
      // Always pending — never flips.
      await mockInvoicePolling(page, { id: "inv-456", sequence: Array(20).fill("pending") });
      await page.goto("/ru/pay/success?invoiceId=inv-456");
      await expect(page.getByText(/processing your payment|email when it|обрабатываем|email/i)).toBeVisible({ timeout: 35_000 });
    });
    ```

    Create landing/e2e/navbar.spec.ts — covers SC #6 + WEB-09:
    ```ts
    import { test, expect } from "@playwright/test";

    test("SC#6 logged-out: navbar shows Pricing + Login", async ({ page }) => {
      await page.goto("/ru/pricing");
      await expect(page.getByRole("link", { name: /Pricing|Тарифы|Precios/i })).toBeVisible();
      await expect(page.getByRole("link", { name: /Login|Войти|Iniciar/i })).toBeVisible();
    });

    test("SC#6 logged-in: navbar shows Pricing + Dashboard + Sign out", async ({ page, context }) => {
      await context.addCookies([
        { name: "rv_at", value: "test_at", domain: "localhost", path: "/", httpOnly: true, sameSite: "Strict" },
      ]);
      await page.goto("/ru/pricing");
      await expect(page.getByRole("link", { name: /Pricing|Тарифы|Precios/i })).toBeVisible();
      await expect(page.getByRole("link", { name: /Dashboard|Кабинет|Panel/i })).toBeVisible();
      // Sign-out is inside UserMenu — click to expand if needed.
      const userMenu = page.getByRole("button", { name: /[A-Z@\.]+/ }).first();
      if (await userMenu.isVisible()) await userMenu.click();
      await expect(page.getByRole("button", { name: /Sign out|Выйти|Cerrar sesión/i })).toBeVisible();
    });
    ```

    Finally run the suite: `cd landing && BACKEND_API_URL=http://127.0.0.1:1 REVALIDATE_SECRET=test-revalidate-secret APPLE_SERVICE_ID=test.web APPLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=apple GOOGLE_CLIENT_ID_WEB=test.google GOOGLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=google APP_URL=http://localhost:3000 npm run build && npm run test:e2e`.
  </action>
  <acceptance_criteria>
    - `test -e landing/e2e/login.spec.ts && grep -n 'SC#1\|alice@example.com\|HttpOnly\|httpOnly' landing/e2e/login.spec.ts` returns at least 2 matches
    - `test -e landing/e2e/pricing.spec.ts && grep -n 'SC#2\|SC#3\|SC#5\|gate.lava.top\|pricing_currency' landing/e2e/pricing.spec.ts` returns at least 4 matches
    - `test -e landing/e2e/pay-success.spec.ts && grep -n 'SC#4\|pending\|paid\|email' landing/e2e/pay-success.spec.ts` returns at least 3 matches
    - `test -e landing/e2e/navbar.spec.ts && grep -n 'SC#6\|Pricing\|Login\|Dashboard\|Sign out' landing/e2e/navbar.spec.ts` returns at least 4 matches
    - `cd landing && BACKEND_API_URL=http://127.0.0.1:1 REVALIDATE_SECRET=test-revalidate-secret APPLE_SERVICE_ID=test.web APPLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=apple GOOGLE_CLIENT_ID_WEB=test.google GOOGLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=google APP_URL=http://localhost:3000 npm run build && npm run test:e2e` exits 0
  </acceptance_criteria>
  <done>4 Playwright spec files; suite asserts SC #1-6 + WEB-01..WEB-09; `npm run test:e2e` exits 0.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| nginx → landing-node | Internal docker network; no TLS termination needed inside |
| browser → nginx :9443 | TLS 1.2+ from Let's Encrypt cert |
| Playwright → standalone server | Loopback localhost:3000; not a security boundary |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-04-08-01 | I (Info disclosure) | docker layer secrets | mitigate | Task 1 Dockerfile uses ARG only for placeholder build-time vars; runtime secrets come from compose's `${VAR:?}` which fails to start if unset. No COPY of .env files (excluded in .dockerignore) |
| T-04-08-02 | E (Elevation) | container running as root | mitigate | Task 1 adds `addgroup --system nodejs && adduser --system --uid 1001 nextjs` and `USER nextjs` directive |
| T-04-08-03 | T (Tampering) | nginx exposes internal proxy_pass to public | mitigate | landing-node port 3000 is `expose`-only (not `ports:`), accessible only over the internal `landing-net` docker network; nginx is the only ingress |
| T-04-08-04 | D (DoS) | nginx → Node body buffering | mitigate | Task 1 sets `client_max_body_size 64k` matching PERF-09 + Plan 03's BODY_BYTES_LIMIT |
| T-04-08-05 | I (Info disclosure) | Playwright traces contain cookies | accept | Traces retained only on failure (Task 2 config); developer-only; not pushed beyond CI artifact bucket |
| T-04-08-06 | T (Mock test bypasses real backend) | mocks paper over a real backend bug | accept | Plan 08 mocks the Phase 2/3 contracts ONLY; a future integration test (Phase 6 or roll-into Phase 5 ship) runs against the real backend. Document follow-up |
| T-04-08-07 | I (Open port surface) | port 3000 leak through misconfig | mitigate | Compose overlay uses `expose:` not `ports:`. Operator must verify no `ports: ["3000:3000"]` accidentally added in env-specific overrides |
| T-04-08-08 | T (Stale brand assets) | Apple/Google launch policy changes after deploy | accept | Plan 02 documented the source URLs in `landing/public/brand/README.md`; operator runbook should add a quarterly check |
</threat_model>

<verification>
- Phase-goal verification mapping:
  - SC #1 → login.spec.ts "SC#1: Apple sign-in completes ..."
  - SC #2 → pricing.spec.ts "SC#2: logged-in click ..."
  - SC #3 → pricing.spec.ts "SC#3: logged-out 'Get Pro' ..."
  - SC #4 → pay-success.spec.ts "SC#4 (happy)" + "SC#4 (timeout)"
  - SC #5 → pricing.spec.ts "SC#5: /pricing renders in RU with RUB ..."
  - SC #6 → navbar.spec.ts "SC#6 logged-out" + "SC#6 logged-in"
- WEB-XX coverage: all 9 requirements asserted at least once in the suite (login covers WEB-01/02; navbar covers WEB-09; pricing covers WEB-04/05/08; pay-success covers WEB-06; pay-fail manual + via redirect in pay-success; dashboard reachable via SC #1 flow → WEB-03)
- Docker build: `docker build ...` exits 0 (Task 1 acceptance)
- E2E: `npm run test:e2e` exits 0 (Task 3 acceptance)
</verification>

<success_criteria>
- Dockerfile produces a runnable container on Node 22 alpine as non-root
- Compose overlay enforces required env vars (`${VAR:?}` syntax)
- nginx routes app paths to landing-node, keeps everything else on the existing static serving path
- Playwright suite covers all 6 ROADMAP SCs + all 9 WEB-XX requirements
- `npm run test:e2e` exits 0
</success_criteria>

<output>
After completion, create `.planning/phases/04-landing-surfaces/04-08-deploy-smoke-tests-SUMMARY.md` documenting:
- Multi-stage Dockerfile pattern (placeholder build args; runtime secrets via compose)
- nginx routing layout (which paths go where)
- Static-export-removal vs keep-static decision (recommend: remove for v2.2.0 simplicity)
- Playwright env block matched to Phase 4 env contract
- Follow-up: integration test against real Phase 2/3 backend (deferred to /gsd-note)
- Follow-up: Phase 3 admin handlers need to POST /api/revalidate-pricing fan-out (still open from Plan 05)
</output>
