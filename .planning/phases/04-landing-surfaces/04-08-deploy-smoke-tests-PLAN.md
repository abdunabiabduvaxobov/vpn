---
phase: 04-landing-surfaces
plan: 08
type: execute
wave: 5
depends_on: [04-04, 04-05, 04-06, 04-07]
files_modified:
  - landing/Dockerfile
  - landing/docker-compose.landing.yml
  - docker-compose.prod.yml
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
    - "landing/docker-compose.landing.yml adds the landing-node container alongside the existing nginx service AND attaches it to the same shared network as the api service so landing-node can reach http://vpn-api:3000"
    - "docker-compose.prod.yml is updated to declare a shared external network (vpn-net) and attach the api service to it; landing-node joins the same network as external: true"
    - "landing/nginx/vpn.mydayai.uz.conf routes /login /dashboard /pricing /pay/* /auth/* /api/* to the Node container on a private port; / and other marketing pages stay served from the static export OR pass through to Node (we choose all-Node for v2.2.0 simplicity)"
    - "Playwright E2E specs assert each ROADMAP success criterion against a backend mock"
    - "`npm run test:e2e` exits 0"
  artifacts:
    - path: "landing/Dockerfile"
      provides: "Production image — multi-stage build, Node 22 alpine, non-root user, exposes 3000"
    - path: "landing/docker-compose.landing.yml"
      provides: "Compose override for the landing-node service; attaches to vpn-net (shared with api)"
    - path: "docker-compose.prod.yml"
      provides: "Updated to declare vpn-net network + attach api service to it"
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
      pattern: "proxy_pass\\s+http://landing[-_]node"
    - from: "landing-node container"
      to: "vpn-api container"
      via: "BACKEND_API_URL=http://vpn-api:3000 over shared vpn-net network"
      pattern: "vpn-api:3000"
    - from: "landing/e2e/*.spec.ts"
      to: "landing standalone server"
      via: "Playwright webServer config"
      pattern: "webServer"
tags: [deploy, docker, nginx, networking, e2e, smoke]
---

<objective>
Ship Phase 4 to production-shaped infrastructure and verify every WEB-XX requirement with automated Playwright tests. Concretely:

1. Build a Dockerfile that produces the `landing-node` container from the standalone output (Plan 01).
2. **B3 fix:** Add a shared external network `vpn-net` to `docker-compose.prod.yml` and attach the `api` service to it. Add a `docker-compose.landing.yml` overlay that runs `landing-node` next to the existing static nginx (D-02) AND joins the same `vpn-net` network so `landing-node` can reach `http://vpn-api:3000` for the backend proxy. (Previously the overlay created an isolated `landing-net` with no path to the api service — landing-node would have failed every backend call. This fix makes the cluster actually reachable.)
3. Update the existing nginx vhost to proxy app paths (`/login`, `/dashboard`, `/pricing`, `/pay/*`, `/auth/*`, `/api/*`) to the Node container — keeping the existing static paths working.
4. Stand up Playwright with a backend-mock fixture so the E2E suite asserts the 6 ROADMAP success criteria + WEB-01..WEB-09 without needing the Go backend.

Purpose: deploys the Phase 4 work end-to-end, validates every requirement, and produces the smoke test bundle that becomes the regression baseline for Phase 5+ (mobile work depends on this same backend surface). Network plumbing right means a real deploy works, not just `docker compose up` returning success while landing-node silently 502s every API call.

Output: A `docker compose -f docker-compose.prod.yml -f landing/docker-compose.landing.yml up -d` brings the entire stack up; nginx serves marketing pages + proxies app pages to Node; landing-node resolves `vpn-api` via the shared network; Playwright smoke `npm run test:e2e` exits 0 with 8+ green tests covering every WEB-XX.
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
@docker-compose.prod.yml
@landing/nginx/vpn.mydayai.uz.conf
@landing/package.json
@landing/next.config.ts

<interfaces>
<!-- Locked CONTEXT.md decisions -->
- D-01: output 'standalone' → landing/.next/standalone is the runnable bundle
- D-02: hybrid routing — nginx terminates TLS + proxies app paths to a private port (we pick 3000 on container, exposed to nginx via Docker network). Marketing pages currently live in /opt/vpn/landing/dist (static export). For v2.2.0, the simpler model is: drop the static export and serve ALL pages from Node behind nginx as a reverse proxy. This avoids dual-path bookkeeping. Document this simplification in the SUMMARY.

Existing nginx config: listens on 9443 (port 443 is the xray VPN). The HTTP 80 redirect goes to 9443. We keep that exactly — only the location blocks change.

**B3 — networking topology (verified against docker-compose.prod.yml):**
- `docker-compose.prod.yml` declares services `postgres`, `redis`, `api`, `tunnel`. The `api` container is named `vpn-api` (via `container_name: vpn-api`), listens on `PORT=3000` internally, and currently binds to `127.0.0.1:3000` on the host (not exposed to the docker network beyond the default bridge).
- There is no explicit `networks:` declaration in docker-compose.prod.yml today, so all services share the default project-wide bridge network. landing-node, brought up via the overlay, would NOT be on that default bridge if the overlay declares its own network — services on different networks cannot resolve each other by service name.
- **Fix:** declare an EXPLICIT `vpn-net` network in `docker-compose.prod.yml`, attach the `api` service to it, and reference it as `external: false, name: vpn-net` (so compose creates it on first `up`). The landing overlay declares the SAME network as `external: true` so it joins instead of creating a duplicate. Now landing-node can dial `http://vpn-api:3000` for its BACKEND_API_URL.
- Compose-managed networks: `vpn-net` is created when prod stack starts; landing overlay joins it. If operator runs landing standalone (without prod stack), they would need to either: (a) start prod stack first, (b) set BACKEND_API_URL to a public URL (e.g., https://vpnapi.mydayai.uz), or (c) drop `external: true`. Document tradeoff in SUMMARY.

Phase 4 environment matrix (required to start the container):
- BACKEND_API_URL — production cluster: `http://vpn-api:3000` (internal DNS via shared vpn-net network). Standalone/Vercel: `https://vpnapi.mydayai.uz` (public URL). Operator picks per deployment.
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

**W2 — Playwright 30s timeout test:** The "SC#4 timeout" assertion in pay-success.spec.ts needs to wait for the 30s timeout view to render. Two implementation choices:
- (a) `test.setTimeout(45_000)` — simple wall-clock wait (RECOMMENDED — chosen for Phase 4)
- (b) `page.clock.install()` + `page.clock.fastForward(30000)` (Playwright ≥1.45) — requires the polling implementation to use the system clock in a way `page.clock` can intercept, which adds coupling. Not worth it for one test.
The Task 3 spec uses option (a).
</interfaces>
</context>

<tasks>

<task type="auto" tdd="false">
  <name>Task 1: Dockerfile + compose overlay + shared network + nginx routing update for the landing-node container</name>
  <files>landing/Dockerfile, landing/docker-compose.landing.yml, docker-compose.prod.yml, landing/nginx/vpn.mydayai.uz.conf, landing/.dockerignore</files>
  <read_first>
    - docker-compose.prod.yml (existing — services: postgres, redis, api, tunnel; api container_name=vpn-api, PORT=3000)
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

    **B3 — Edit docker-compose.prod.yml** to add a top-level `networks:` block and attach the `api` service to it. Surgical change — do not touch postgres/redis/tunnel beyond joining the network (so api can still reach them; the new network is ADDITIONAL, postgres/redis remain accessible via the default bridge):
    ```yaml
    services:
      # ... (postgres, redis unchanged — they stay on default bridge; api still reaches them by service name)
      api:
        # ... (existing config unchanged)
        networks:
          - default       # keeps connectivity to postgres + redis
          - vpn-net       # NEW: allows landing-node to dial http://vpn-api:3000
      # ... (tunnel unchanged)

    networks:
      vpn-net:
        name: vpn-net     # explicit name so the landing overlay can reference it as external
        driver: bridge
    ```
    The `default` network entry under api: ensures backward compatibility — without it, joining `networks:` would REMOVE api from the default network and break api → postgres / api → redis service-name DNS. Always list both. (Compose semantics: when ANY service has a `networks:` key, it loses the default-bridge attachment unless `default` is listed.) Per Docker Compose spec.

    **B3 — Create landing/docker-compose.landing.yml** (overlay — applied with `-f` alongside the prod compose). This overlay joins the already-created `vpn-net` as external:
    ```yaml
    services:
      landing-node:
        build:
          context: ./landing
          dockerfile: Dockerfile
        image: rise-vpn-landing-node:latest
        container_name: landing-node
        restart: unless-stopped
        environment:
          NODE_ENV: production
          PORT: "3000"
          HOSTNAME: "0.0.0.0"
          # In production stack (api on the same vpn-net), use the internal hostname:
          #   BACKEND_API_URL=http://vpn-api:3000
          # For Vercel / standalone deploys (no api in same cluster), use the public URL:
          #   BACKEND_API_URL=https://vpnapi.mydayai.uz
          BACKEND_API_URL: ${BACKEND_API_URL:?}
          REVALIDATE_SECRET: ${REVALIDATE_SECRET:?}
          COOKIE_DOMAIN: ${COOKIE_DOMAIN:-}
          APPLE_SERVICE_ID: ${APPLE_SERVICE_ID:?}
          APPLE_REDIRECT_URI: ${APPLE_REDIRECT_URI:?}
          GOOGLE_CLIENT_ID_WEB: ${GOOGLE_CLIENT_ID_WEB:?}
          GOOGLE_REDIRECT_URI: ${GOOGLE_REDIRECT_URI:?}
          APP_URL: ${APP_URL:?}
        networks:
          - vpn-net
        expose:
          - "3000"

    networks:
      vpn-net:
        external: true
        name: vpn-net
    ```
    NOTE: `external: true` means the network must already exist before this overlay starts — which is the case when `docker-compose.prod.yml` was brought up first. The `name: vpn-net` ensures we attach to the SAME network compose-prod created (some compose versions prefix project name otherwise).

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
    - `grep -n 'vpn-net' landing/docker-compose.landing.yml` returns at least 2 matches (B3 fix — network referenced + declared external)
    - `grep -n 'external: true' landing/docker-compose.landing.yml` returns 1 match (B3 fix — overlay joins existing network)
    - `grep -n 'vpn-net' docker-compose.prod.yml` returns at least 2 matches (B3 fix — network declared + api joins)
    - `grep -n 'networks:' docker-compose.prod.yml` returns at least 2 matches (top-level + api service)
    - `grep -n 'proxy_pass http://landing_node' landing/nginx/vpn.mydayai.uz.conf` returns at least 3 matches
    - `grep -n 'login\|dashboard\|pricing\|pay/(success\|fail)' landing/nginx/vpn.mydayai.uz.conf` returns at least 1 match (app paths)
    - `grep -n '/auth/callback' landing/nginx/vpn.mydayai.uz.conf` returns at least 1 match
    - `grep -n 'client_max_body_size 64k' landing/nginx/vpn.mydayai.uz.conf` returns 1 match
    - `cd landing && docker build --build-arg BACKEND_API_URL=https://x --build-arg REVALIDATE_SECRET=y --build-arg APPLE_SERVICE_ID=x --build-arg APPLE_REDIRECT_URI=https://x/cb --build-arg GOOGLE_CLIENT_ID_WEB=x --build-arg GOOGLE_REDIRECT_URI=https://x/cb --build-arg APP_URL=https://x -t rise-vpn-landing-node:test .` exits 0
    - **B3 reachability check:** After `docker compose -f docker-compose.prod.yml -f landing/docker-compose.landing.yml up -d`, run `docker compose -f docker-compose.prod.yml -f landing/docker-compose.landing.yml exec landing-node sh -c 'wget -q -O - http://vpn-api:3000/health || wget -q -O - http://vpn-api:3000/api/v1/plans?currency=USD'` and assert the response is a 200 or a recognised API JSON body (NOT a "Could not resolve host" / connection-refused error). Document this as part of the operator runbook in the SUMMARY. (`wget` is preferred over `curl` because node:22-alpine includes wget by default; `curl` may need `apk add`.)
  </acceptance_criteria>
  <done>Dockerfile builds the standalone bundle into a 22-alpine runner image; compose overlay enforces required env vars; nginx config proxies /login/dashboard/pricing/pay/*/auth/*/api/* to the Node container; landing-node and vpn-api share the `vpn-net` network so service-name DNS works (`http://vpn-api:3000` resolves from inside landing-node).</done>
</task>

<task type="auto" tdd="false">
  <name>Task 2: Playwright setup — config, deps, backend-mock fixture, test scripts</name>
  <files>landing/playwright.config.ts, landing/package.json, landing/e2e/_fixtures/backend-mock.ts</files>
  <read_first>
    - landing/package.json (existing scripts + deps)
    - .planning/phases/04-landing-surfaces/04-04-login-oauth-callback-PLAN.md (Apple/Google authorize URL format)
    - .planning/phases/04-landing-surfaces/04-05-pricing-plans-isr-revalidate-PLAN.md (plans response shape)
    - .planning/phases/04-landing-surfaces/04-07-checkout-pay-success-fail-PLAN.md (checkout + invoice response shapes + B2 force-refresh trigger)
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

    /**
     * Mock the backend's /api/v1/auth/refresh used by the Plan 03 proxy AND by Plan 07's
     * force-refresh trigger on `status=paid`. Returns a NEW access_token whose embedded
     * plan_id claim equals opts.planId so we can assert the post-paid rv_user re-issue.
     * Build a tiny unsigned JWT: header.payload.signature where signature is meaningless
     * (Plan 03's decodePlanIdFromJwt skips signature verification).
     */
    export async function mockAuthRefresh(page: Page, opts: { planId: string }) {
      const header = Buffer.from(JSON.stringify({ alg: "HS256", typ: "JWT" })).toString("base64url");
      const payload = Buffer.from(JSON.stringify({ sub: "u1", plan_id: opts.planId, iat: Math.floor(Date.now() / 1000), exp: Math.floor(Date.now() / 1000) + 300 })).toString("base64url");
      const jwt = `${header}.${payload}.mocksig`;
      await page.route("**/api/v1/auth/refresh", async (route) => route.fulfill({
        status: 200, contentType: "application/json",
        body: JSON.stringify({ access_token: jwt, refresh_token: "rotated_rt", expires_in: 300 }),
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
    - `grep -n 'mockPlans\|mockOauthExchange\|mockCheckout\|mockInvoicePolling\|mockOAuthRedirect\|mockAuthRefresh' landing/e2e/_fixtures/backend-mock.ts` returns at least 6 matches (B2 fix — mockAuthRefresh added)
    - `cd landing && npx playwright --version` exits 0 (Playwright installed)
  </acceptance_criteria>
  <done>Playwright is installed, config points at the standalone server with mock env, backend-mock fixture exports the six helpers above (including mockAuthRefresh for the B2 force-refresh path).</done>
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
    import { mockInvoicePolling, mockAuthRefresh } from "./_fixtures/backend-mock";

    test("SC#4 (happy): /pay/success polls and flips to active within ~2s of webhook landing; forces refresh on paid", async ({ page, context }) => {
      await context.addCookies([
        { name: "rv_at", value: "test_at", domain: "localhost", path: "/", httpOnly: true, sameSite: "Strict" },
        { name: "rv_rt", value: "test_rt", domain: "localhost", path: "/", httpOnly: true, sameSite: "Strict" },
      ]);
      await mockInvoicePolling(page, { id: "inv-123", sequence: ["pending", "paid"] });
      // B2 fix verification — mockAuthRefresh returns a JWT with plan_id=pro; the page
      // must POST /api/v1/auth/refresh on `paid` BEFORE displaying "Pro is active!".
      await mockAuthRefresh(page, { planId: "pro" });

      // Track that POST /api/v1/auth/refresh was issued. waitForRequest resolves on first match.
      const refreshRequestPromise = page.waitForRequest((req) =>
        req.url().endsWith("/api/v1/auth/refresh") && req.method() === "POST",
        { timeout: 10_000 }
      );

      await page.goto("/ru/pay/success?invoiceId=inv-123");
      await refreshRequestPromise;  // B2 — assert force-refresh fired after paid
      await expect(page.getByText(/Pro is active|активна|activ/i)).toBeVisible({ timeout: 6_000 });
      await expect(page.getByRole("link", { name: /dashboard|кабинет/i })).toBeVisible();
    });

    test("SC#4 (timeout): /pay/success shows 'we'll email you' after 30s of pending", async ({ page, context }) => {
      // W2 fix — bump test timeout above the page's 30s timeout so the assertion is reachable.
      test.setTimeout(45_000);

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
    - `grep -n 'mockAuthRefresh\|/api/v1/auth/refresh' landing/e2e/pay-success.spec.ts` returns at least 2 matches (B2 fix — force-refresh assertion)
    - `grep -n 'test.setTimeout(45_000)\|test.setTimeout(45000)' landing/e2e/pay-success.spec.ts` returns 1 match (W2 fix — wall-clock timeout buffer)
    - `test -e landing/e2e/navbar.spec.ts && grep -n 'SC#6\|Pricing\|Login\|Dashboard\|Sign out' landing/e2e/navbar.spec.ts` returns at least 4 matches
    - `cd landing && BACKEND_API_URL=http://127.0.0.1:1 REVALIDATE_SECRET=test-revalidate-secret APPLE_SERVICE_ID=test.web APPLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=apple GOOGLE_CLIENT_ID_WEB=test.google GOOGLE_REDIRECT_URI=http://localhost:3000/auth/callback?provider=google APP_URL=http://localhost:3000 npm run build && npm run test:e2e` exits 0
  </acceptance_criteria>
  <done>4 Playwright spec files; suite asserts SC #1-6 + WEB-01..WEB-09; B2 force-refresh assertion present; W2 wall-clock timeout used for 30s scenario; `npm run test:e2e` exits 0.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| nginx → landing-node | Internal docker network; no TLS termination needed inside |
| landing-node → vpn-api | Shared `vpn-net` bridge network; service-name DNS (`vpn-api:3000`) over plain HTTP; internal-only, never traverses untrusted network |
| browser → nginx :9443 | TLS 1.2+ from Let's Encrypt cert |
| Playwright → standalone server | Loopback localhost:3000; not a security boundary |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-04-08-01 | I (Info disclosure) | docker layer secrets | mitigate | Task 1 Dockerfile uses ARG only for placeholder build-time vars; runtime secrets come from compose's `${VAR:?}` which fails to start if unset. No COPY of .env files (excluded in .dockerignore) |
| T-04-08-02 | E (Elevation) | container running as root | mitigate | Task 1 adds `addgroup --system nodejs && adduser --system --uid 1001 nextjs` and `USER nextjs` directive |
| T-04-08-03 | T (Tampering) | nginx exposes internal proxy_pass to public | mitigate | landing-node port 3000 is `expose`-only (not `ports:`), accessible only over the internal `vpn-net` docker network; nginx is the only ingress |
| T-04-08-04 | D (DoS) | nginx → Node body buffering | mitigate | Task 1 sets `client_max_body_size 64k` matching PERF-09 + Plan 03's BODY_BYTES_LIMIT |
| T-04-08-05 | I (Info disclosure) | Playwright traces contain cookies | accept | Traces retained only on failure (Task 2 config); developer-only; not pushed beyond CI artifact bucket |
| T-04-08-06 | T (Mock test bypasses real backend) | mocks paper over a real backend bug | accept | Plan 08 mocks the Phase 2/3 contracts ONLY; a future integration test (Phase 6 or roll-into Phase 5 ship) runs against the real backend. Document follow-up |
| T-04-08-07 | I (Open port surface) | port 3000 leak through misconfig | mitigate | Compose overlay uses `expose:` not `ports:`. Operator must verify no `ports: ["3000:3000"]` accidentally added in env-specific overrides |
| T-04-08-08 | T (Stale brand assets) | Apple/Google launch policy changes after deploy | accept | Plan 02 documented the source URLs in `landing/public/brand/README.md`; operator runbook should add a quarterly check |
| T-04-08-09 | I (Cross-tenant network exposure) | vpn-net joined by extra services in future | mitigate | `vpn-net` declared explicit + named; future overlays that need it MUST list `external: true, name: vpn-net`. Inadvertent typos create a NEW separate network instead of accidentally exposing data. Document network-naming convention in SUMMARY. |
</threat_model>

<verification>
- Phase-goal verification mapping:
  - SC #1 → login.spec.ts "SC#1: Apple sign-in completes ..."
  - SC #2 → pricing.spec.ts "SC#2: logged-in click ..."
  - SC #3 → pricing.spec.ts "SC#3: logged-out 'Get Pro' ..."
  - SC #4 → pay-success.spec.ts "SC#4 (happy)" (includes B2 force-refresh assertion) + "SC#4 (timeout)" (W2 timeout-buffered)
  - SC #5 → pricing.spec.ts "SC#5: /pricing renders in RU with RUB ..."
  - SC #6 → navbar.spec.ts "SC#6 logged-out" + "SC#6 logged-in"
- WEB-XX coverage: all 9 requirements asserted at least once in the suite (login covers WEB-01/02; navbar covers WEB-09; pricing covers WEB-04/05/08; pay-success covers WEB-06; pay-fail manual + via redirect in pay-success; dashboard reachable via SC #1 flow → WEB-03)
- Docker build: `docker build ...` exits 0 (Task 1 acceptance)
- B3 reachability: `docker compose ... exec landing-node sh -c 'wget -q -O - http://vpn-api:3000/api/v1/plans?currency=USD'` returns a JSON body (Task 1 operator runbook step)
- E2E: `npm run test:e2e` exits 0 (Task 3 acceptance)
</verification>

<success_criteria>
- Dockerfile produces a runnable container on Node 22 alpine as non-root
- Compose overlay enforces required env vars (`${VAR:?}` syntax)
- **B3 closure**: docker-compose.prod.yml declares `vpn-net` + attaches api; landing overlay joins it as external. landing-node can dial `http://vpn-api:3000` for BACKEND_API_URL.
- nginx routes app paths to landing-node, keeps everything else on the existing static serving path
- Playwright suite covers all 6 ROADMAP SCs + all 9 WEB-XX requirements
- **B2 closure**: pay-success.spec.ts asserts /api/v1/auth/refresh fires before "Pro is active!" renders
- **W2 closure**: 30s timeout test wrapped with `test.setTimeout(45_000)` so it's not flaky
- `npm run test:e2e` exits 0
</success_criteria>

<output>
After completion, create `.planning/phases/04-landing-surfaces/04-08-deploy-smoke-tests-SUMMARY.md` documenting:
- Multi-stage Dockerfile pattern (placeholder build args; runtime secrets via compose)
- nginx routing layout (which paths go where)
- **B3 closure: `vpn-net` shared external network. docker-compose.prod.yml declares the network and attaches `api`; landing overlay joins via `external: true, name: vpn-net`. Result: `BACKEND_API_URL=http://vpn-api:3000` resolves from inside landing-node. Operator runbook for non-prod deploys (Vercel, standalone): use `BACKEND_API_URL=https://vpnapi.mydayai.uz` public URL instead.**
- Static-export-removal vs keep-static decision (recommend: remove for v2.2.0 simplicity)
- Playwright env block matched to Phase 4 env contract
- **B2 closure: pay-success.spec.ts asserts the force-refresh fires on `status === "paid"` before "Pro is active!" renders. mockAuthRefresh fixture returns a JWT with `plan_id` claim so the rv_user re-issue path is exercised end-to-end.**
- **W2 closure: 30s timeout test uses `test.setTimeout(45_000)` (simple wall-clock buffer) rather than `page.clock.fastForward`. Rationale: `page.clock` requires polling implementation to use the system clock in a way Playwright can intercept — too much coupling for one test.**
- Follow-up: integration test against real Phase 2/3 backend (deferred to /gsd-note)
- Follow-up: Phase 3 admin handlers need to POST /api/revalidate-pricing fan-out (still open from Plan 05)
- **W4 follow-up captured as /gsd-note: "After Phase 4 ships, update REQUIREMENTS.md WEB-06 to replace `status=COMPLETED` with `status=paid` to match the lava.top mapping used in Phase 3 + Phase 4."**
- W5 reference: rv_user re-issue on proxy refresh + 30-day Max-Age implemented in Plan 03; this plan's smoke tests verify it via the SC#4 happy-path assertion.
</output>
</content>
</invoke>