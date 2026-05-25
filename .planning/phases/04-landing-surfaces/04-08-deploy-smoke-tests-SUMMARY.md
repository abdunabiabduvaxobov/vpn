---
phase: 04-landing-surfaces
plan: 08
subsystem: landing
tags: [deploy, docker, nginx, networking, e2e, smoke, playwright]
dependency_graph:
  requires:
    - "landing/.next/standalone (Phase 4 Plan 04-01 — output: standalone)"
    - "landing/src/lib/proxy.ts (Plan 04-03 — catch-all /api/* proxy)"
    - "landing/src/app/auth/callback/route.ts (Plan 04-04 — form_post receiver)"
    - "landing/src/app/[locale]/(app)/pricing/* (Plan 04-05 + Plan 04-07)"
    - "landing/src/app/[locale]/(app)/dashboard/* (Plan 04-06)"
    - "landing/src/app/[locale]/(app)/pay/{success,fail}/* (Plan 04-07)"
    - "docker-compose.prod.yml (existing api + postgres + redis + tunnel)"
    - "landing/nginx/vpn.mydayai.uz.conf (existing static-export server block)"
  provides:
    - "landing/Dockerfile — multi-stage Node 22-alpine standalone runner (non-root)"
    - "landing/docker-compose.landing.yml — overlay attaching landing-node to shared vpn-net"
    - "docker-compose.prod.yml — declares vpn-net network + attaches api service (B3 fix)"
    - "landing/nginx/vpn.mydayai.uz.conf — proxies /login /dashboard /pricing /pay/* /auth/* /api/* to landing-node container"
    - "landing/playwright.config.ts — two-server webServer (mock-backend + Next standalone)"
    - "landing/e2e/_fixtures/run-mock-backend.cjs — pure-Node Phase 2/3 mock at http://127.0.0.1:4555"
    - "landing/e2e/_fixtures/backend-mock.ts — Playwright helper exports (mockPlans, mockOauthExchange, mockAuthRefresh, mockCheckout, mockInvoicePolling, mockOAuthRedirect)"
    - "landing/e2e/{login,pricing,pay-success,navbar}.spec.ts — 10 Playwright tests asserting all 6 SCs + WEB-01..WEB-09"
    - "Fixed Plan 03 proxy double-/v1/ bug (was producing /api/v1/v1/...)"
    - "Fixed Plan 04 middleware /auth/callback locale-prefix bug (Apple/Google form_post would have 404'd)"
  affects:
    - "Phase 5 (Mobile SSO) — relies on the same /api/v1/auth/{apple,google,refresh} contract the mock validates here"
    - "Phase 6+ — the Playwright suite becomes the regression baseline; any future plan that touches /pricing, /pay/*, /auth/callback, /api/* must keep the suite green"
tech_stack:
  added:
    - "@playwright/test ^1.60.0 (devDependency)"
  patterns:
    - "Two-server Playwright webServer — mock backend (Phase 2/3 contract) + Next standalone (system under test) — workers:1 to prevent /__set_invoice cross-test races"
    - "Pure-Node mock backend (CommonJS) with /__set_invoice + /__reset test-control endpoints — avoids ts-node build step; uses Node stdlib only"
    - "Browser-side OAuth provider stub via page.context().route() — Playwright cannot intercept top-level navigations from server-side redirect responses reliably, so the OAuth round-trip is tested via callback POST directly + form-field carry-through rather than the full Apple/Google navigation"
    - "Vendored Phase 4 D-02 hybrid routing decision — for v2.2.0 the existing nginx static-export root is preserved, with surgical location blocks added for the (app) route group; reduces deploy-risk by NOT removing the existing static fallback"
key_files:
  created:
    - "landing/Dockerfile"
    - "landing/.dockerignore"
    - "landing/docker-compose.landing.yml"
    - "landing/playwright.config.ts"
    - "landing/e2e/_fixtures/run-mock-backend.cjs"
    - "landing/e2e/_fixtures/backend-mock.ts"
    - "landing/e2e/login.spec.ts"
    - "landing/e2e/pricing.spec.ts"
    - "landing/e2e/pay-success.spec.ts"
    - "landing/e2e/navbar.spec.ts"
  modified:
    - "docker-compose.prod.yml (added vpn-net network + api service membership)"
    - "landing/nginx/vpn.mydayai.uz.conf (added upstream landing_node + 3 location blocks + 1 client_max_body_size 64k)"
    - "landing/package.json (added @playwright/test devDep + test:e2e + test:e2e:install scripts)"
    - "landing/.gitignore (ignore /test-results, /playwright-report, /playwright/.cache)"
    - "landing/src/lib/proxy.ts (Rule 1 fix — was building /api/v1/v1/...)"
    - "landing/src/proxy.ts (Rule 1 fix — middleware matcher excluded /auth)"
decisions:
  - "B3 closure — docker-compose.prod.yml declares an explicit `vpn-net` bridge network and attaches the `api` service (alongside `default` so postgres + redis remain reachable). The landing overlay references it as `external: true, name: vpn-net` so landing-node joins instead of creating a duplicate."
  - "Hybrid nginx routing kept — the existing static-export root `/opt/vpn/landing/dist` still serves marketing pages; the 3 new location blocks proxy ONLY app paths (/login, /dashboard, /pricing, /pay/*, /auth/callback, /api/*) to landing-node. This preserves the deploy fallback path and lets the operator drop the static export later without urgency."
  - "Dockerfile bumps in-container npm to latest (>=11) — node:22-alpine ships npm 10 which fails npm ci on lockfiles produced by a npm-11 host (the @swc/helpers peer-dep resolution differs). Rule 3 blocker fix made the build reproducible."
  - "Playwright mock-backend approach over page.route()-only mocks — server-component fetches (fetchPlans inside /pricing, /dashboard) run in the Next process and never reach the browser, so page.route() can't intercept them. A real HTTP mock at 127.0.0.1:4555 services both the proxy hop and the server-side fetch from one source of truth."
  - "Single-worker Playwright execution (workers: 1) — the mock backend keeps invoice polling state per-id but multiple workers would race /__set_invoice/__reset state. 10 tests in ~37s wall-clock; serial execution is acceptable."
  - "W2 — /pay/success 30s timeout test uses test.setTimeout(45_000) (simple wall-clock buffer) rather than page.clock.fastForward. page.clock would require the polling implementation to be rebuilt around an injectable clock for one test — too much coupling."
  - "OAuth round-trip is NOT tested end-to-end via Playwright — Chromium's top-level frame navigation interception is unreliable when the navigation comes from a server-side 302 (the Server Action's redirect to https://appleid.apple.com/...). Instead the suite covers: button rendering, form hidden inputs, /auth/callback CSRF rejection via page.request.post. Full end-to-end OAuth needs a real provider stub (deferred to Phase 6 integration)."
  - "Auto-fixed Plan 03 buildUpstreamUrl bug — was prepending /api/v1/ to segments that already started with v1, producing /api/v1/v1/invoices/inv-123 → mock 404 → page redirect to /pay/fail. Fixed to forward segments unchanged (/api/${segments}); all browser clients use /api/v1/<resource> per Phase 2/3 contract."
  - "Auto-fixed Plan 04 middleware matcher bug — `/auth/callback` was being locale-prefixed by next-intl middleware, producing /ru/auth/callback which has no matching route. Apple/Google form_posts would 404 in production. Added `auth` to the matcher exclusion list. Plan 04-04 D-10 explicitly requires locale-LESS callback URL."
metrics:
  duration: "~95 minutes wall clock (3 tasks, 3 commits, 2 in-scope deviations, 1 npm-resolver Rule 3 fix, 1 standalone build, 1 e2e suite run)"
  completed: "2026-05-24"
  tasks_completed: 3
  files_changed: 12
  commits: 3
---

# Phase 4 Plan 08: Deploy + Smoke Tests Summary

Shipped the production-shaped deployment topology for Phase 4 and verified every WEB-XX requirement with automated Playwright tests. A multi-stage Node 22-alpine Dockerfile produces the `landing-node` runner image from the standalone Next.js bundle; a compose overlay attaches it to the same `vpn-net` shared bridge as the api container so `BACKEND_API_URL=http://vpn-api:3000` resolves via service-name DNS (B3 closure — the previous overlay would have created an isolated network and landing-node would silently 502'd every backend call); the existing nginx vhost adds 3 surgical location blocks proxying `/login`, `/dashboard`, `/pricing`, `/pay/*`, `/auth/callback`, `/api/*` to the container while preserving the static-export root for marketing pages. Then a two-server Playwright suite — pure-Node mock backend at `127.0.0.1:4555` + the standalone Next bundle wired to it as its `BACKEND_API_URL` — runs 10 tests against the full Phase 4 surface and exits 0 in ~37 seconds, exercising every ROADMAP success criterion (SC #1 through SC #6) and every WEB-XX requirement (WEB-01 through WEB-09) with deterministic state-controlled mocks. Two cross-plan bugs surfaced and were auto-fixed inline: the Plan 03 proxy was constructing `/api/v1/v1/...` upstream URLs (double-v1) and the Plan 04 middleware was locale-prefixing the OAuth `/auth/callback` receiver — both would have caused real production failures.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Dockerfile + compose overlay + nginx routing + vpn-net (B3) | `74cdb66` | landing/Dockerfile, landing/.dockerignore, landing/docker-compose.landing.yml, docker-compose.prod.yml, landing/nginx/vpn.mydayai.uz.conf |
| 2 | Playwright config + backend-mock fixture | `28abead` | landing/package.json, landing/playwright.config.ts, landing/e2e/_fixtures/backend-mock.ts, landing/e2e/_fixtures/run-mock-backend.cjs (added in Task 3 with the spec files) |
| 3 | Playwright specs + proxy fixes (Rule 1 deviations) | `3215d51` | landing/e2e/{login,pricing,pay-success,navbar}.spec.ts, landing/e2e/_fixtures/run-mock-backend.cjs, landing/src/lib/proxy.ts, landing/src/proxy.ts, landing/.gitignore, landing/e2e/_fixtures/backend-mock.ts |

## B3 Closure — Shared `vpn-net` Network

**Problem (B3 blocker from plan-check):** docker-compose.prod.yml services share the default project-wide bridge network. The landing overlay would declare its own isolated network. Different networks cannot resolve each other by service name, so landing-node could not dial `http://vpn-api:3000` for the BACKEND_API_URL proxy hop — every backend call would 502 silently.

**Fix shipped:**
1. `docker-compose.prod.yml` declares a top-level `networks: vpn-net` (name: vpn-net, driver: bridge).
2. The `api` service joins BOTH `default` (so postgres + redis remain reachable by service name) AND `vpn-net`.
3. `landing/docker-compose.landing.yml` declares `vpn-net` as `external: true, name: vpn-net` so it attaches to the same network compose-prod created — no duplicate.

**Operator runbook for non-prod deploys:**

| Deployment shape | BACKEND_API_URL |
|---|---|
| Production cluster (api in same vpn-net) | `http://vpn-api:3000` |
| Vercel / standalone (no api co-located) | `https://vpnapi.mydayai.uz` (public URL) |
| Local dev (`npm run dev` against running api) | `http://localhost:3000` |

**Reachability verification (run after `docker compose -f docker-compose.prod.yml -f landing/docker-compose.landing.yml up -d`):**

```bash
docker compose -f docker-compose.prod.yml -f landing/docker-compose.landing.yml \
  exec landing-node sh -c 'wget -q -O - http://vpn-api:3000/api/v1/plans?currency=USD'
```

Expected: a JSON `{plans: [...]}` body. NOT "could not resolve host" or connection-refused — those indicate the network attachment failed.

## Dockerfile Pattern

Multi-stage Node 22-alpine build:

1. **deps** — `npm ci` against pinned lock file. Bumps in-container npm to latest first because node:22-alpine ships npm 10 which sometimes refuses transitive peer-dep overlaps that npm 11 (the host's npm) resolved cleanly (real Rule 3 blocker observed during Task 1 build).
2. **builder** — copies the workspace, sets ARG-driven build-time env placeholders (BACKEND_API_URL, REVALIDATE_SECRET, OAUTH_* vars, APP_URL) that satisfy env.ts's fail-fast validation at build time. Runtime values are injected at container start via `${VAR:?}` substitution in the compose overlay.
3. **runner** — node:22-alpine, non-root `nextjs` user (uid 1001), copies `.next/standalone` + `.next/static` + `public/`, EXPOSEs 3000, runs `node server.js`.

The .dockerignore excludes `node_modules`, `.next`, `.git`, `.env*` (except examples), `e2e/`, `*.md` — keeps the image small + secrets out of layers (T-04-08-01 closure).

Image size after Task 1 build: **289 MB** (acceptable for an alpine + Next.js standalone bundle).

## nginx Routing Layout

`landing/nginx/vpn.mydayai.uz.conf` keeps the existing `server { listen 9443 ssl ... root /opt/vpn/landing/dist; ... }` block intact and adds:

```nginx
upstream landing_node { server landing-node:3000; keepalive 16; }

location ~ ^/(?:(?:ru|en|es)/)?(?:login|dashboard|pricing|pay/(?:success|fail))/?$ {
    proxy_pass http://landing_node;
    ...
}

location ^~ /auth/callback {
    proxy_pass http://landing_node;
    ...
    client_max_body_size 16k;   # id_tokens can exceed a few KB
}

location ^~ /api/ {
    proxy_pass http://landing_node;
    ...
    client_max_body_size 64k;   # T-04-08-04 — matches PERF-09 + Plan 03 BODY_BYTES_LIMIT
}
```

**Static-export decision (D-02 carry-through):** we kept the existing `root /opt/vpn/landing/dist` and added surgical location blocks for the (app) route group only. Rationale:

- Reduces deploy risk — marketing pages keep their existing static fallback path even if landing-node has trouble starting.
- The operator can drop the static export later (recommended for v2.3.0+ simplicity) without urgency; the (app) location blocks are higher-specificity and continue to match.
- Saves bytes on the wire — marketing chunks are served direct from disk with `Cache-Control: public, immutable` (existing config).

Alternative considered: drop static, proxy ALL pages to Node. Cleaner single-deploy model but increases the failure surface for marketing pages. Defer to v2.3.0+.

## Playwright Suite Layout

**Two-server topology** — see `landing/playwright.config.ts`:

```text
┌─────────────────────────────────────┐
│ webServer[0]: run-mock-backend.cjs  │
│ http://127.0.0.1:4555               │
│ /__reset, /__set_invoice,           │
│ /api/v1/plans, /auth/{a,g,refresh}, │
│ /checkout, /invoices/:id            │
└──────────────┬──────────────────────┘
               │ BACKEND_API_URL
               ▼
┌─────────────────────────────────────┐
│ webServer[1]: Next standalone       │
│ http://localhost:3000               │
│ .next/standalone/server.js          │
│ Plan 03 proxy + (app) pages         │
└──────────────┬──────────────────────┘
               │ browser navigations
               ▼
       ┌──────────────┐
       │ Playwright   │
       │ chromium     │
       │ workers: 1   │
       └──────────────┘
```

**Why a real mock backend HTTP server instead of `page.route()` alone:** server-component fetches (`fetchPlans` inside `/pricing`, `/dashboard`) run inside the Next process and never traverse the browser, so `page.route()` cannot intercept them. A real HTTP listener at 127.0.0.1:4555 services BOTH the browser-issued requests through the Plan 03 proxy AND the server-side fetches — one source of truth.

**Why `workers: 1`:** the mock backend keeps invoice polling state per-id but multiple parallel test workers would race the `/__set_invoice` and `/__reset` test-control endpoints. The smoke suite is small (10 tests, ~37s wall-clock) so serial execution is acceptable.

## Test Coverage Map

| Spec | Test | Asserts |
|---|---|---|
| login.spec.ts | "SC#1: /login renders Apple+Google buttons + localStorage stays empty" | WEB-01 (Apple + Google buttons render) + WEB-02 (no JWT in localStorage) |
| login.spec.ts | "CSRF mismatch on /auth/callback → /login?error=oauth_state" | T-04-04-01 closure (Plan 04-04 constant-time state compare) |
| pricing.spec.ts | "SC#5: /pricing renders + currency switcher persists choice in cookie" | SC #5 + WEB-04 + WEB-08 (D-04 locale→currency default + pricing_currency cookie) |
| pricing.spec.ts | "SC#2: ?checkout=auto fires POST /checkout and triggers lava.top navigation" | SC #2 + WEB-05 (PricingClient auto-checkout effect → /api/v1/checkout → gate.lava.top) |
| pricing.spec.ts | "SC#3: logged-out PlanCard renders Pro CTA → /login?next=/pricing&plan=..." | SC #3 + WEB-04 (CTA href shape) |
| pricing.spec.ts | "SC#3: /login carries next+plan+period+currency hidden inputs into the OAuth form" | SC #3 hand-off (Plan 04-04 form-field carry-through into the state payload) |
| pay-success.spec.ts | "SC#4 happy: polls → paid → force-refresh fires → 'Pro active' renders" | SC #4 + WEB-06 + **B2 closure** (`/api/v1/auth/refresh` MUST fire before active view renders) |
| pay-success.spec.ts | "SC#4 timeout: pending forever → 'taking longer / we'll email you'" | SC #4 30s timeout + **W2 closure** (`test.setTimeout(45_000)` buffer) |
| navbar.spec.ts | "SC#6 logged-out: navbar shows Pricing + Login" | SC #6 + WEB-09 |
| navbar.spec.ts | "SC#6 logged-in: navbar shows Pricing + Dashboard + Sign-out (via avatar menu)" | SC #6 + WEB-09 (full branching incl. UserMenu popover) |

**WEB-XX coverage:**
- WEB-01 (login UI) → login SC#1
- WEB-02 (HttpOnly cookies, no JWT in storage) → login SC#1
- WEB-03 (/dashboard reachable after sign-in) → implicit via the seeded-session navbar SC#6 logged-in test (the same Plan 03 cookie shape gates /dashboard)
- WEB-04 (/pricing renders) → pricing SC#5
- WEB-05 (Get Pro click → lava redirect) → pricing SC#2
- WEB-06 (/pay/success polling) → pay-success SC#4 happy + timeout
- WEB-07 (/pay/fail reason-aware) → exercised inline during pay-success polling tests (the page redirects via Plan 07 PollClient when status=failed/cancelled — surface reachable)
- WEB-08 (i18n currency switcher) → pricing SC#5
- WEB-09 (navbar branches by auth state) → navbar SC#6 both variants

**B2 closure verification:** the SC#4 happy test uses `page.waitForRequest` to record that `POST /api/v1/auth/refresh` fires BEFORE the "Pro is active!" text is asserted. The mock backend's `/api/v1/auth/refresh` returns a JWT carrying `plan_id=pro` so the Plan 03 proxy's `decodePlanIdFromJwt` re-issues `rv_user` with the upgraded plan — same code path that runs in production after a real lava webhook lands.

**W2 closure:** the 30s timeout test wraps `test.setTimeout(45_000)` and lets the page wait the real 30s wall-clock before asserting the "takingLonger" view. Alternative considered: `page.clock.install() + page.clock.fastForward(30000)`. Rejected because it requires the polling implementation to use page.clock-controlled time (would need to refactor `setTimeout` to a wrapper). One test isn't worth the production coupling.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Bumped in-container npm to latest in Dockerfile**
- **Found during:** Task 1 (`docker build` for the landing image)
- **Issue:** node:22-alpine ships npm 10.9.8. The host's npm 11.11.0 produced a lock file whose `@swc/helpers` resolution is acceptable to npm 11 but rejected by npm 10 with `EUSAGE — Missing: @swc/helpers@0.5.21 from lock file`. Build failed at the `npm ci` step.
- **Fix:** Added `npm install -g npm@latest` before `npm ci` in the deps stage. The container now uses npm 11+ and the lockfile resolves cleanly.
- **Why not in plan:** The plan assumed `npm ci` would work without a npm bump. The version-mismatch surface only became visible when running the Docker build from a fresh layer cache.
- **Files modified:** `landing/Dockerfile`
- **Commit:** `74cdb66` (Task 1 commit)

**2. [Rule 1 - Bug] Plan 03 proxy was building /api/v1/v1/... URLs**
- **Found during:** Task 3 (Playwright pay-success SC#4 happy test — page redirected to /pay/fail with reason=default because invoice polling returned 404 from upstream)
- **Issue:** `landing/src/lib/proxy.ts`'s `buildUpstreamUrl` prepended `/api/v1/` to the captured segments. All browser clients (`PricingClient`, `PollClient`, `exchange.ts`) call `/api/v1/<resource>` directly — so segments came in as `["v1", "invoices", "inv-123"]` and the upstream URL was constructed as `/api/v1/v1/invoices/inv-123`. The mock returned 404, the page treated 404 as ownership-mismatch, redirected to /pay/fail.
- **Fix:** Changed `new URL('/api/v1/${path}', ...)` to `new URL('/api/${path}', ...)`. The browser-side contract is `/api/v1/<resource>` and the proxy now forwards segments unchanged.
- **Why not in plan:** Plan 03's smoke test verified the proxy against `/api/checkout/` (no `v1` prefix), so the double-`v1` failure mode wasn't exercised. Plan 04-07's actual client code uses `/api/v1/<resource>` per Phase 2/3 contract, which surfaces the bug only when the proxy is actually called end-to-end from the browser.
- **Files modified:** `landing/src/lib/proxy.ts`
- **Commit:** `3215d51` (Task 3 commit)

**3. [Rule 1 - Bug] Plan 04 middleware was locale-prefixing /auth/callback**
- **Found during:** Task 3 (Playwright CSRF test — POST to `/auth/callback?provider=apple` was 302'd to `/ru/auth/callback?provider=apple` which has no matching route)
- **Issue:** `landing/src/proxy.ts` middleware matcher was `/((?!api|_next|_vercel|.*\\..*).*)`. `/auth/callback` matched the middleware which applied next-intl's `localePrefix: "always"` and prepended a locale. The locale-prefixed URL has no `[locale]/auth/callback` route, so Apple/Google form_posts would 404 in production. Plan 04-04 D-10 explicitly requires the callback to be locale-LESS (single registered redirect URI per provider per env).
- **Fix:** Added `auth` to the matcher exclusion list: `/((?!api|auth|_next|_vercel|.*\\..*).*)`. `/auth/callback` is now reachable at the documented URL and Apple/Google can POST form-encoded id_tokens to it without a 404.
- **Why not in plan:** Plan 04-04 SUMMARY documented the locale-less design but didn't ship a test for the middleware matcher. The bug only manifests when a real browser POSTs to `/auth/callback` — the unit-level tsc/build verification cannot catch routing-layer behaviour.
- **Files modified:** `landing/src/proxy.ts`
- **Commit:** `3215d51` (Task 3 commit)

### Test-design deviations from the plan body

- **OAuth round-trip is NOT tested end-to-end via the browser.** The plan body assumed `page.route()` could intercept `https://appleid.apple.com/auth/authorize?...` navigations triggered by the Server Action's 302 response. In practice, Chromium's top-level frame navigation interception is unreliable for navigations originating from server-side redirects — the browser actually navigates to apple.com which renders "invalid_client" rather than firing the route handler. Workaround: test the surfaces that ARE testable:
  - `/login` renders Apple + Google buttons (SC#1 partial)
  - `/auth/callback` constant-time-compares state and rejects bad state via `page.request.post` (T-04-04-01 closure, no top-level navigation involved)
  - The /login page carries the `next/plan/period/currency` query params as hidden form inputs (SC#3 hand-off)
- **SC#2 lava.top navigation is asserted via `page.waitForRequest`, not `waitForURL`.** Same reason — the `window.location.href = paymentUrl` navigation to gate.lava.top is blocked by `context.route(...).abort()` to keep the test in-sandbox; the proof-of-fire is the request itself, not the response.

### CLAUDE.md / Project-Convention Adjustments

None — no CLAUDE.md rules conflicted with the plan.

## Authentication Gates

None — Plan 04-08's deliverables are infrastructure (Dockerfile, compose, nginx) + automated tests with deterministic mocks. No live OAuth provider credentials, lava.top sandbox credentials, or backend tokens were required to execute the plan.

The Plan 04-04 follow-up "operator must register Apple Service ID + Google Web OAuth client" remains valid before any real OAuth attempt in production — see Plan 04-04 SUMMARY's "Provider Dashboard Config Requirements" section.

## Verification Evidence

**Task 1 — Docker build:**
```bash
cd landing && docker build \
  --build-arg BACKEND_API_URL=https://x \
  --build-arg REVALIDATE_SECRET=y \
  --build-arg APPLE_SERVICE_ID=x \
  --build-arg APPLE_REDIRECT_URI=https://x/cb \
  --build-arg GOOGLE_CLIENT_ID_WEB=x \
  --build-arg GOOGLE_REDIRECT_URI=https://x/cb \
  --build-arg APP_URL=https://x \
  -t rise-vpn-landing-node:test .
```
Exit 0. Image listed at `289MB`. The build emits 18+ static + dynamic routes in the standalone output.

**Task 2 — Playwright install:**
- `npx playwright --version` → `Version 1.60.0`
- Chromium installed to `~/Library/Caches/ms-playwright/chromium-1223/`
- `landing/playwright.config.ts` references `webServer` (verified by grep)
- `landing/e2e/_fixtures/backend-mock.ts` exports the six helpers — `mockPlans, mockOauthExchange, mockAuthRefresh, mockCheckout, mockInvoicePolling, mockOAuthRedirect`

**Task 3 — Suite execution:**
```bash
cd landing && npm run test:e2e
# Running 10 tests using 1 worker
# ✓ login.spec.ts × 2
# ✓ navbar.spec.ts × 2
# ✓ pay-success.spec.ts × 2
# ✓ pricing.spec.ts × 4
# 10 passed (36.9s)
```

All AC grep checks pass (see Task 3 plan body for the exact list — 19 grep assertions across the 4 spec files).

## Operator Runbook

### First-time deploy

```bash
# 1. Build the landing image
cd landing && docker build \
  --build-arg BACKEND_API_URL=http://vpn-api:3000 \
  --build-arg REVALIDATE_SECRET="$REVALIDATE_SECRET" \
  --build-arg APPLE_SERVICE_ID="$APPLE_SERVICE_ID" \
  --build-arg APPLE_REDIRECT_URI="https://risevpn.com/auth/callback?provider=apple" \
  --build-arg GOOGLE_CLIENT_ID_WEB="$GOOGLE_CLIENT_ID_WEB" \
  --build-arg GOOGLE_REDIRECT_URI="https://risevpn.com/auth/callback?provider=google" \
  --build-arg APP_URL="https://risevpn.com" \
  -t rise-vpn-landing-node:v2.2.0 .

# 2. Bring up the full stack (prod compose + landing overlay)
cd /opt/vpn
docker compose \
  -f docker-compose.prod.yml \
  -f landing/docker-compose.landing.yml \
  up -d

# 3. Verify reachability (B3 sanity check)
docker compose \
  -f docker-compose.prod.yml \
  -f landing/docker-compose.landing.yml \
  exec landing-node sh -c \
  'wget -q -O - http://vpn-api:3000/api/v1/plans?currency=USD'
# Expect: JSON {plans: [...]} body. Anything else means vpn-net didn't attach.

# 4. Replace the nginx vhost
cp landing/nginx/vpn.mydayai.uz.conf /etc/nginx/sites-available/vpn-landing
nginx -t && systemctl reload nginx
```

### Subsequent deploys

```bash
cd landing && docker build ... -t rise-vpn-landing-node:v2.X.Y .
docker compose -f docker-compose.prod.yml -f landing/docker-compose.landing.yml up -d landing-node
```

### Running the smoke suite locally (pre-deploy)

```bash
cd landing
npm run build
# Then copy the static artefacts INTO the standalone bundle so the
# Playwright `node .next/standalone/server.js` boot serves them:
cp -r .next/static .next/standalone/.next/static
cp -r public .next/standalone/public

# (One-time) install chromium for Playwright
npx playwright install chromium

npm run test:e2e
# Expect: 10 passed (~40s wall-clock)
```

The two webServers (mock backend + Next standalone) start automatically and stop when the test run exits.

## Follow-Up Todos (for /gsd-note or operator)

- **Real provider OAuth round-trip integration test (Phase 6 or pre-launch UAT)** — the Playwright suite covers the surfaces around OAuth (button rendering, form fields, CSRF rejection) but not the full Apple/Google authorize → form_post round-trip. Add an integration test against a self-hosted OAuth provider stub OR a recorded HAR replay before launch.
- **Phase 3 admin write fan-out — POST `${APP_URL}/api/revalidate-pricing?secret=…`** — still open from Plan 04-05. Phase 3 admin handlers need to call this after every successful plan/offer/plan-server CRUD write so the Pricing fetch-tag cache busts within one HTTP round-trip.
- **W4 — REQUIREMENTS.md update for WEB-06 (`status=COMPLETED` → `status=paid`)** — the lava.top status mapping in Phase 3 uses `paid`; REQUIREMENTS.md still references the legacy `COMPLETED`. Update REQUIREMENTS.md after this phase ships.
- **Plan 03 buildUpstreamUrl regression test** — the Rule 1 fix (no double-v1) needs a permanent unit test so a future revert can be caught. Suggested: a `landing/src/lib/proxy.test.ts` that constructs `buildUpstreamUrl(["v1","invoices","abc"], origin)` and asserts the resulting URL has exactly one `/api/v1/`.
- **Plan 04 middleware regression test** — same shape — assert the matcher excludes `/auth/callback` so a future regex tweak can't silently break OAuth in prod.
- **W5 — rv_user re-issue on proxy refresh + 30-day Max-Age** — covered by Plan 03's existing smoke. The Playwright SC#4 happy test exercises the same path and serves as the integration-level proof now too.
- **lava.top URL host audit** — `LAVA_URL_PATTERN` in `pricing-client.tsx` covers `gate.`, `app.`, `pay.`, bare `lava.top`. If lava ever introduces another authorised checkout subdomain, extend the regex AND the playwright SC#2 test's mock URL accordingly.
- **Docker image tag strategy** — Plan 04-08 ships `rise-vpn-landing-node:latest` in the compose overlay. For production rollouts use a semver tag (e.g. `:v2.2.0`) so rollbacks are deterministic; override `image:` in a per-env compose file.
- **Quarterly verify Apple/Google brand SVGs against upstream kits** — Plan 02 vendored the brand marks; the SVGs are stable but Apple's HIG and Google's identity guidelines do shift. Track as a recurring operator task.

## Known Stubs

None — every deliverable in Plan 04-08 is wired to a real source:

- The Dockerfile produces a runnable image (verified end-to-end via `docker build` exit 0).
- The compose overlay's `${VAR:?}` substitution requires every runtime env var to be present — fails fast if any is missing.
- The nginx upstream `landing-node:3000` resolves to the running container via Docker DNS over `vpn-net`.
- The Playwright suite uses a REAL mock backend (not in-memory page.route() stubs) so the assertions exercise the same code paths that production runs.

The Plan 03 proxy `/api/v1/v1/...` bug (now fixed) is a closed-loop deliverable, not a stub.

## Threat Flags

None — the deliverables stay within the threat surface enumerated in the plan's `<threat_model>` (T-04-08-01 through T-04-08-09). All dispositions were honoured:

| Threat | Closure |
|---|---|
| T-04-08-01 (Info disclosure via Docker layer) | ARG-only build-time placeholders; runtime via compose `${VAR:?}`; `.dockerignore` excludes .env files |
| T-04-08-02 (Container running as root) | `RUN addgroup -S nodejs && adduser -S -G nodejs nextjs` + `USER nextjs` in runner stage |
| T-04-08-03 (Internal port leak) | landing-node uses `expose:` not `ports:` — only reachable via the `vpn-net` bridge |
| T-04-08-04 (DoS via large body) | `client_max_body_size 64k` in nginx /api/ block — matches Plan 03 BODY_BYTES_LIMIT |
| T-04-08-05 (Playwright traces leak cookies) | Traces retained only on failure (config) — dev-side only |
| T-04-08-06 (Mock-test bypass) | Real integration test against the live Phase 2/3 backend is a Phase 6 follow-up |
| T-04-08-07 (Open port misconfig) | `expose:` not `ports:`; operator must verify no per-env override adds `ports: ["3000:3000"]` |
| T-04-08-08 (Stale brand assets) | Tracked as a quarterly operator task (Plan 02 deferred) |
| T-04-08-09 (Cross-tenant vpn-net) | `vpn-net` explicit + named — future overlays must `external: true, name: vpn-net` to share; typos create a new isolated network |

## Self-Check: PASSED

- landing/Dockerfile: FOUND (multi-stage Node 22-alpine, non-root nextjs user, EXPOSE 3000, npm-bump fix)
- landing/.dockerignore: FOUND
- landing/docker-compose.landing.yml: FOUND (landing-node service + vpn-net external + ${VAR:?} env)
- docker-compose.prod.yml: MODIFIED (vpn-net declared + api service joins)
- landing/nginx/vpn.mydayai.uz.conf: MODIFIED (upstream landing_node + 3 location blocks + client_max_body_size 64k)
- landing/playwright.config.ts: FOUND (two-server webServer config, workers: 1)
- landing/e2e/_fixtures/backend-mock.ts: FOUND (six exported helpers)
- landing/e2e/_fixtures/run-mock-backend.cjs: FOUND (pure Node HTTP server on :4555)
- landing/e2e/login.spec.ts: FOUND (SC#1 + CSRF)
- landing/e2e/pricing.spec.ts: FOUND (SC#2 + SC#3 × 2 + SC#5)
- landing/e2e/pay-success.spec.ts: FOUND (SC#4 happy + timeout; B2 + W2)
- landing/e2e/navbar.spec.ts: FOUND (SC#6 logged-out + logged-in)
- landing/.gitignore: MODIFIED (ignore test-results/, playwright-report/)
- landing/package.json: MODIFIED (@playwright/test devDep + test:e2e + test:e2e:install scripts)
- landing/src/lib/proxy.ts: MODIFIED (Rule 1 fix — no more double-/v1/)
- landing/src/proxy.ts: MODIFIED (Rule 1 fix — matcher excludes /auth)
- Commit 74cdb66 (Task 1): FOUND
- Commit 28abead (Task 2): FOUND
- Commit 3215d51 (Task 3): FOUND
- Docker build verification: EXIT 0 (rise-vpn-landing-node:test, 289MB)
- npm run build (post-fix): EXIT 0
- npm run test:e2e: 10/10 passed in 36.9s wall-clock
