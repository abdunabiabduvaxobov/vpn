# Roadmap: RiseVPN v2.2.0 — Lava.top + SSO refactor + audit fixes

**Defined:** 2026-05-22
**Granularity:** fine
**Mode:** yolo
**Source of truth:** `docs/audit/MASTER-PLAN.md` (pre-derived 8-tranche plan) + `docs/ADR-007-lava-sso-rework.md`

## Core Value

A user signs in once with Apple or Google, pays on risevpn.com via lava.top, and Pro unlocks on every device immediately. Everything else (admin tooling, performance, hardening) serves that path.

## Coverage

- v1 requirements: 75
- Mapped to phases: 75 ✓
- Unmapped: 0
- v2 / deferred: see REQUIREMENTS.md

## Phases

- [x] **Phase 1: Hotfix — audit critical fixes** - 8 stop-the-bleeding fixes that must land before any paying user touches the system
- [~] **Phase 2: Auth SSO backend** - Apple + Google sign-in endpoints, guest-promotion, account-linking, JWT logout (gap closure 02-08..10 pending — see VERIFICATION/REVIEW)
- [ ] **Phase 3: Lava.top + plans catalog** - dynamic plans schema, lava HTTP client, checkout, webhook, public plans API, admin CRUD
- [ ] **Phase 4: Landing surfaces** - /login, /dashboard, /pricing, /pay/success, /pay/fail on risevpn.com
- [ ] **Phase 5: Mobile SSO + Pro CTA** - LoginScreen with Apple/Google/Guest, informational PaymentScreen, deep-link return, 2.2.0 ship
- [ ] **Phase 6: Performance & scalability** - /servers cache, Redis heartbeat, off-host PG/Redis, user-tier cache, missing indexes, scheduler gate, GORM context propagation
- [ ] **Phase 7: Admin panel overhaul** - KPI dashboard, per-user controls with advisory locks, server controls, system controls, webhook log + replay, readyz/livez
- [ ] **Phase 8: Cleanup & hardening** - delete Stripe, per-user VLESS UUID rotation, opaque refresh tokens, device-binding, security headers, govulncheck, secure mobile storage

## Phase Details

### Phase 1: Hotfix — audit critical fixes
**Goal**: Eight known-unsafe behaviors in the live codebase are fixed so the system is safe to extend with SSO and real money flow.
**Depends on**: Nothing (lands first)
**Requirements**: HOTFIX-01, HOTFIX-02, HOTFIX-03, HOTFIX-04, HOTFIX-05, HOTFIX-06, HOTFIX-07, HOTFIX-08
**Success Criteria** (what must be TRUE):
  1. Admin demotes a user via the admin panel and that user loses admin access on their very next request — not five minutes later.
  2. A user whose Pro subscription period ends sees their tier downgrade to `free` automatically the next time the scheduler runs (because `subscription_expires_at` is now persisted from the provider's `current_period_end`).
  3. `/auth/refresh` completes in single-digit milliseconds even on a `sessions` table with millions of rows (UNIQUE index on `refresh_token_hash` is in use, EXPLAIN shows index scan not seq scan).
  4. The API server refuses to start with a clear error message when any required payment-provider env var is missing or empty (no silent default to `""`).
  5. A 500 response from any handler returns a generic message; `err.Error()` from GORM/bcrypt/internal libraries never reaches the client body.
  6. A Redis outage in the middle of a rate-limit `INCR` cannot leave a counter without a TTL (atomic Lua / MULTI-EXEC verified by induced failure).
  7. A failed insert during refresh-token rotation rolls back the delete in the same transaction; the user never ends up logged out by a transient DB error.
  8. `createadmin` does not accept the password on argv; the seeded admin starts as `subscription_tier='free'`.
**Plans**: 9 plans
  - [x] 01-01-PLAN.md — HOTFIX-06: createadmin reads password from stdin + seeds tier=free
  - [x] 01-02-PLAN.md — HOTFIX-08: fail-fast aggregate env validator (DB_*, REDIS_*, JWT_SECRET, TUNNEL_VLESS_UUID)
  - [x] 01-03-PLAN.md — HOTFIX-04: scrub 5xx error bodies + X-Request-ID middleware
  - [x] 01-04-PLAN.md — HOTFIX-02: AdminRequired re-reads role from DB on every admin request
  - [x] 01-05-PLAN.md — HOTFIX-03: atomic Lua INCR+EXPIRE for rate limiter
  - [x] 01-06-PLAN.md — HOTFIX-05: transactional refresh-token rotation
  - [x] 01-07-PLAN.md — HOTFIX-01: regression tests for subscription downgrade (test-only, column+scheduler already correct)
  - [x] 01-08-PLAN.md — HOTFIX-07: UNIQUE index on sessions.refresh_token_hash + dedupe migration
  - [x] 01-09-PLAN.md — staging smoke (10 steps) + v2.2.0-hotfix tag

### Phase 2: Auth SSO backend
**Goal**: Apple and Google identities map deterministically to backend `users.id` rows, on any surface (mobile, web, admin), with the existing guest-login path preserved as a fallback.
**Depends on**: Phase 1 (HOTFIX-02 fixes the same admin/auth middleware Phase 2 extends; HOTFIX-05 fixes the refresh-rotation transaction Phase 2 re-uses; HOTFIX-07 adds the index Phase 2 leans on).
**Blockers** (from ADR-007 §15 — must be resolved before this phase starts):
  - Apple Developer Team ID, Bundle ID, Service ID, and the `.p8` key
  - Google OAuth client IDs (iOS, Android, Web — three distinct IDs)
  - Lava.top offer IDs (these are technically a Phase 3 blocker but are discoverable in Phase 2 and worth resolving in parallel)
  - Account-linking policy confirmation (default recommendation: auto-link by verified email, reject `@privaterelay.appleid.com`)
  - Pro device limit (recommendation: 5)
**Requirements**: AUTH-01, AUTH-02, AUTH-03, AUTH-04, AUTH-05, AUTH-06, AUTH-07, AUTH-08
**Success Criteria** (what must be TRUE):
  1. A user signs in with Apple on the website, then opens the mobile app and signs in with Apple — the backend returns the same `user_id` both times (verifiable in admin user table: one row, two surfaces).
  2. A user signs in with Google using a Gmail address, later signs in with Apple using the same Gmail address — both providers attach to the same `users` row (account-linking) — UNLESS the email is `@privaterelay.appleid.com`, in which case a new row is created.
  3. A guest user who taps "Continue with Apple" keeps the same `users.id` (in-place promotion); their existing device row remains bound to that id.
  4. `POST /api/v1/auth/logout` returns 204, deletes the refresh-session row, and the calling access token returns 401 on any subsequent request until its `exp`.
  5. Apple/Google tokens with the wrong `aud` (e.g. a token issued for the iOS bundle id presented against the web service id) are rejected with 401 and never produce a backend JWT.
**Plans**: 10 plans total (7 original + 3 gap-closure plans 02-08..02-10 closing CR-01, CR-02, WR-01..WR-04, IN-01..IN-03 per 02-VERIFICATION.md + 02-REVIEW.md)
  - [x] 02-01-PLAN.md (wave 1) — schema migration + GORM model + Apple/Google env vars [AUTH-03]
  - [x] 02-02-PLAN.md (wave 1) — Apple verifier package (JWKs + iss + aud + exp) [AUTH-01]
  - [x] 02-03-PLAN.md (wave 1) — Google verifier package (idtoken.Validate + email_verified gate) [AUTH-02]
  - [x] 02-04-PLAN.md (wave 2) — user_repo SSO functions + DeleteUserSessions + ReassignDevicesByUserID (W-1) [AUTH-04,05,06,08]
  - [x] 02-05-PLAN.md (wave 3) — Apple+Google signin handlers + main.go wiring [AUTH-01,02,04,05,06,07]
  - [x] 02-06-PLAN.md (wave 4) — Logout handler + protected-group mount [AUTH-07,08]
  - [x] 02-07-PLAN.md (wave 5) — docs/auth-sso-api.md API contract [AUTH-01,02,08]
  - [x] 02-08-PLAN.md (wave 1, gap-closure) — handler hardening: empty-sub guards [CR-01], auto-link Step B transaction [CR-02], parseGuestJWT role check [WR-01], logout ttl boundary [WR-02], free-subscription row for new SSO users [WR-03]
  - [x] 02-09-PLAN.md (wave 2, gap-closure, depends 02-08) — repository layer: PromoteGuestToSSO updates full_name [WR-04]
  - [x] 02-10-PLAN.md (wave 2, gap-closure, depends 02-08) — polish: go.mod 1.22 [IN-01], seedAdminUser tier=free [IN-02], migration 018 doc comment [IN-03]

### Phase 3: Lava.top + plans catalog
**Goal**: A real card payment via lava.top sandbox grants Pro to a specific signed-in user within seconds of the webhook arriving, with strict idempotency, all plan limits and prices managed in the `plans` / `plan_offers` / `plan_servers` tables (no hardcoded `PlanLimits` map).
**Depends on**: Phase 2 (a payment must identify a user, and the only identified users are SSO-signed; lava.top requires the user's email which only exists on SSO-bound rows).
**Requirements**: PAY-01, PAY-02, PAY-03, PAY-04, PAY-05, PAY-06, PAY-07, PAY-08, PAY-09, PAY-10, PAY-11, PAY-12, PAY-13, PAY-14, PAY-15, PAY-16
**Success Criteria** (what must be TRUE):
  1. A test card payment through lava.top sandbox grants Pro to a specific `user_id` within 5 seconds of the webhook arriving — verified by `GET /api/v1/subscription` flipping from `free` to `pro` and `subscription_expires_at` being populated from the webhook's `period_end`.
  2. The same lava.top webhook delivered 20 times (their retry policy) results in exactly one tier-grant; the 19 duplicates are rejected by the UNIQUE constraint on `lava_webhook_events` and return 200 without re-applying the side effect.
  3. A webhook handler that errors mid-processing returns HTTP 500 so lava.top retries — verified by inducing a DB failure and observing lava.top send the event again.
  4. A webhook request from an IP outside the lava.top allowlist is rejected at the Fiber `TrustedProxies` layer, regardless of `X-Forwarded-For` content.
  5. `GET /api/v1/plans` (no auth) returns the seeded `free` and `pro` plans with their offers, and the landing /pricing page renders without any hardcoded price strings in `landing/`.
  6. Admin removes server S from plan P via `DELETE /admin/plans/:id/servers/:server_id`; a non-admin user on plan P calling `GET /servers` no longer sees S; an admin user on the same plan still sees it (admin bypass works).
  7. The tier granted by a webhook is derived from the lava.top `offerId` in the payload via `plan_offers` lookup, never from any client-supplied metadata.
**Plans**: 11 plans
  - [ ] 03-01-PLAN.md (wave 1) — migrations 019 + 020 + GORM models + Stripe-leakage cleanup [PAY-01]
  - [ ] 03-02-PLAN.md (wave 1) — internal/lava/ HTTP client + LAVA_* config + LAVA_ENV selector [PAY-07, PAY-16, PAY-02 (DTO), PAY-10 (DTO)]
  - [ ] 03-03-PLAN.md (wave 2, depends 01) — plan_repo.go (17 functions) + invoice_repo.go + tests [PAY-01, PAY-08, PAY-09, PAY-11]
  - [ ] 03-04-PLAN.md (wave 2, depends 01+03) — server-access enforcement (delete PlanLimits map; rewire servers.go, connection.go, devices.go, admin.go, health.go) [PAY-11]
  - [ ] 03-05-PLAN.md (wave 3, depends 01+02+03) — POST /checkout + GET /invoices/:id + POST /subscription/cancel + GET /admin/lava/products + payment.go rewrite (Stripe deleted) [PAY-02, PAY-09 (escalate), PAY-10, PAY-13 (admin proxy)]
  - [ ] 03-06-PLAN.md (wave 3, depends 01+02+03+05) — POST /webhook/lava + LavaWebhookIPAllowlist middleware + 5 event-type dispatch + idempotency UPSERT [PAY-03, PAY-04, PAY-05, PAY-06, PAY-07, PAY-08, PAY-09]
  - [ ] 03-07-PLAN.md (wave 3, depends 01+03) — GET /api/v1/plans (public) + Redis cache wrapper + JWT plan_id claim + middleware fallback [PAY-12]
  - [ ] 03-08-PLAN.md (wave 4, depends 01+03+07) — admin /plans CRUD (13 endpoints) + audit-log integration + cache busting [PAY-13, PAY-14, PAY-15]
  - [ ] 03-09-PLAN.md (wave 4, depends 01+03) — expiry-downgrade cron (every 10 min) in scheduler.go [PAY-09]
  - [ ] 03-10-PLAN.md (wave 5, depends 05+08) — admin-web Plans UI (7 shadcn install + 2 pages + 8 components + D-12 dropdown picker) [PAY-13, PAY-14, PAY-15]
  - [ ] 03-11-PLAN.md (wave 5, depends 02+05+06+07+08+09) — docs/lava-payments-api.md + sandbox integration test + final grep smoke (PlanLimits, BaseURL, c.IP()) [PAY-01..PAY-16 closure]

### Phase 4: Landing surfaces
**Goal**: A user on risevpn.com can sign in with Apple or Google, see their plan on `/dashboard`, choose Pro on `/pricing`, complete payment on lava.top, and land on `/pay/success` with Pro already active.
**Depends on**: Phase 3 (every page on this list calls a backend endpoint introduced in Phase 2 or 3 — login → AUTH, pricing → PAY-12, pay/success → PAY invoices).
**Requirements**: WEB-01, WEB-02, WEB-03, WEB-04, WEB-05, WEB-06, WEB-07, WEB-08, WEB-09
**Success Criteria** (what must be TRUE):
  1. A new visitor clicks "Sign in with Apple" on `/login`, completes Apple ID auth, and lands on `/dashboard` showing their email, current plan (`free`), and a "Get Pro" link — with no JWT visible in `localStorage` or any other browser-readable storage (HttpOnly cookies only).
  2. A logged-in user on `/pricing` clicks "Get Pro" and is redirected to a `lava.top` payment URL within one HTTP round-trip (no client-side delay).
  3. An unauthenticated visitor on `/pricing` who clicks "Get Pro" is redirected to `/login?next=/pricing&plan=pro&period=monthly` and after sign-in returns to `/pricing` with the same selection preserved.
  4. `/pay/success?invoiceId=X` polls `/api/v1/invoices/{id}` and shows a success state within ~2 seconds of the webhook landing; if the webhook is delayed, it shows a clear "we'll email you" message after 30s of polling instead of hanging silently.
  5. The `/pricing` page renders in EN, RU, and ES from `messages/{en,ru,es}.json` with currency derived from the active locale, and on-demand ISR revalidates after an admin updates a plan (no manual rebuild needed).
  6. Navbar exposes "Pricing" and "Login" when logged out; exposes "Pricing", "Dashboard", and "Sign out" when logged in.
**Plans**: 8 plans
  - [x] 04-01-foundation-i18n-standalone-PLAN.md (wave 1) — next.config standalone + locale ru/en/es + env loader [WEB-08]
  - [x] 04-02-app-shell-navbar-primitives-PLAN.md (wave 2, depends 04-01) — Card/Skeleton/Toast/TierBadge primitives + NavbarApp with server-side cookie branching + brand-mark SVGs [WEB-09]
  - [x] 04-03-node-proxy-cookies-refresh-PLAN.md (wave 2, depends 04-01) — /api/[...path] catch-all proxy + 401→refresh→retry + HttpOnly cookie helpers + /api/auth/logout [WEB-02]
  - [x] 04-04-login-oauth-callback-PLAN.md (wave 3, depends 04-02+04-03) — /<locale>/login with Apple+Google buttons + /auth/callback CSRF + id_token exchange + session cookie set [WEB-01, WEB-02]
  - [x] 04-05-pricing-plans-isr-revalidate-PLAN.md (wave 3, depends 04-02+04-03) — /<locale>/pricing dynamic + CurrencySwitcher + /api/revalidate-pricing tag-bust endpoint [WEB-04, WEB-08]
  - [x] 04-06-dashboard-signout-PLAN.md (wave 3, depends 04-02+04-03) — /<locale>/dashboard server-gated + DashboardCard + SignOutButton with destructive confirm [WEB-03, WEB-09]
  - [x] 04-07-checkout-pay-success-fail-PLAN.md (wave 4, depends 04-04+04-05) — checkout flow (auto-resume after sign-in) + /pay/success polling (D-21 contract) + /pay/fail reason-aware [WEB-05, WEB-06, WEB-07]
  - [x] 04-08-deploy-smoke-tests-PLAN.md (wave 5, depends 04-04+04-05+04-06+04-07) — Dockerfile + compose overlay + nginx routing + Playwright E2E covering all 6 SCs and WEB-01..WEB-09 [WEB-01..WEB-09]
**UI hint**: yes

### Phase 5: Mobile SSO + Pro CTA
**Goal**: The mobile app at version 2.2.0 lets a user sign in with Apple, Google, or Guest; the upgrade flow opens the website in a browser (no IAP); and a deep link from the website's `/pay/success` returns the user to the app with Pro already reflected.
**Depends on**: Phase 3 (mobile depends on the same `/auth/apple`, `/auth/google`, and `/invoices/{id}` endpoints used by the web). Can run in parallel with Phase 4.
**Requirements**: APP-01, APP-02, APP-03, APP-04, APP-05, APP-06, APP-07
**Success Criteria** (what must be TRUE):
  1. A tester on iOS taps "Continue with Apple" on `LoginScreen`, completes Apple auth, and lands on Home — the backend issued a JWT with the same shape as today's guest JWT, the auth store has the same fields.
  2. A guest user who taps "Continue with Apple" or "Continue with Google" is upgraded in-place: their existing `users.id` is preserved (verifiable via admin panel: one row, `auth_provider` flipped from `guest` to `apple`/`google`).
  3. `PaymentScreen` shows the current plan limits and exactly one button — "Upgrade to Pro at risevpn.com" — which opens `https://risevpn.com/<locale>/pricing` in the system browser. There is no buy button, no price displayed on a CTA-styled element, no IAP code path.
  4. After paying on the web, the user taps "Open in app" on `/pay/success`; the universal link `vpnapp://payment/success?invoiceId=X` opens the app, the app polls `GET /invoices/{id}`, and the Home screen shows Pro within 5 seconds — without the user manually logging in again.
  5. `app.json` reads `2.2.0`, a TestFlight build is uploaded for iOS, and a Play Internal Track build is uploaded for Android.
**Plans**: 5 plans
  - [x] 05-00-PLAN.md (wave 0) — Test scaffolding: Jest mocks for SSO libs + 10 stub test files + 05-HUMAN-UAT.md prereqs gate [APP-01..APP-07]
  - [x] 05-01-PLAN.md (wave 1, depends 05-00) — Native config: BLOCKING operator prereqs + install pinned SSO packages + pod install + iOS Bundle ID fix + Info.plist/entitlements/AppDelegate + AndroidManifest intent-filter + strings.xml [APP-01, APP-02, APP-06]
  - [x] 05-02-PLAN.md (wave 2, depends 05-01) — Services layer: appleSignIn.ts, googleSignIn.ts, deepLink.ts, payment.ts rewrite, api.ts _skipAuthRefresh patch (T-7), authStore extension, User type extension [APP-01, APP-02, APP-04, APP-05, APP-06]
  - [x] 05-03-PLAN.md (wave 3, depends 05-02) — UI layer: LoginScreen, PaymentScreen rewrite (D-14), AccountScreen sync card, LeavingAppSheet, ActivatingProModal (polling 2s × 5 → ?escalate=true → 30s timeout), App.tsx wiring, RootNavigator Login screen, i18n EN+RU + stale-key cleanup [APP-03, APP-04, APP-05, APP-06]
  - [x] 05-04-PLAN.md (wave 4, depends 05-03) — Release prep: bump 4 version sources to 2.2.0 (D-17), signed .aab via gradlew bundleRelease, operator manual UAT on physical Android device (D-19) — TestFlight + Play Internal Track uploads DEFERRED per D-18 to end-of-milestone release phase [APP-07]
**UI hint**: yes

### Phase 6: Performance & scalability
**Goal**: The system handles roughly 5–8k concurrent active connections on a moderately-sized VM without the API becoming the bottleneck — by caching hot reads, batching hot writes, splitting DB/Redis off the API host, and adding missing indexes.
**Depends on**: Phase 3 (some perf items — e.g. user-tier cache invalidation on admin user-update, /servers cache invalidation on admin server-write — require the admin endpoints introduced in Phase 3). Independent of Phases 4, 5, 7, 8.
**Requirements**: PERF-01, PERF-02, PERF-03, PERF-04, PERF-05, PERF-06, PERF-07, PERF-08, PERF-09
**Success Criteria** (what must be TRUE):
  1. `GET /servers` for a logged-in user returns from Redis cache (verified by the absence of a `servers` SELECT in the query log) and invalidates within one request when an admin saves a server.
  2. Under a synthetic 10k-connection load, the `connections` table absorbs roughly one bulk write every 10 seconds rather than ~167 writes per second (heartbeat goes through Redis with scheduler-flushed bulk update).
  3. Postgres and Redis run on hosts (or separate scaled services) distinct from the API container in the production compose file — `docker-compose.prod.yml` reflects the split.
  4. A second API replica started with `RUN_SCHEDULER=false` runs cleanly without firing the scheduler twice; the primary replica with `RUN_SCHEDULER=true` is the only one running periodic jobs.
  5. The stale-connection sweep query uses `idx_connections_heartbeat_active` (EXPLAIN shows index-only scan) and completes in O(connected), not O(history).
  6. Every GORM query inherits the request `ctx`; killing a long-running client request via `ctx` cancellation aborts the underlying DB query (verifiable via `pg_stat_activity`).
**Plans**: TBD

### Phase 7: Admin panel overhaul
**Goal**: The operator can run RiseVPN day-to-day from `admin-web` — see live KPIs, take per-user actions (suspend, force-Pro, force-disconnect) with concurrency safety against payment webhooks, manage system-wide controls, and replay any lava.top webhook from the audit log.
**Depends on**: Phase 3 (the admin webhook-log view depends on the `lava_webhook_events` table introduced in Phase 3; KPI MRR/paid-users metrics depend on Phase 3 data; per-user advisory locks must coordinate against the Phase 3 webhook handler). Independent of Phases 4, 5, 6, 8.
**Requirements**: ADMIN-01, ADMIN-02, ADMIN-03, ADMIN-04, ADMIN-05, ADMIN-06, ADMIN-07, ADMIN-08
**Success Criteria** (what must be TRUE):
  1. The dashboard page shows live numbers for total users, paid users this period, MRR estimate, active connections, signups today/week/month, churn count, and failed payments — all sourced from the existing DB and refreshed without a page reload.
  2. An admin clicks "Force-cancel Pro" on a user while a `payment.success` webhook arrives for the same user — the per-user advisory lock serializes the two operations and the final user state is consistent (either canceled-with-refund or paid-and-active, never a hybrid).
  3. From the webhook event log page, the admin clicks "Replay" on any DELIVERED event and the handler re-applies the side effect idempotently (no duplicate tier grant; status flips to REPLAYED).
  4. The admin marks a server as "drain" — existing connections to it survive, but `GET /servers` stops returning it to non-admins; force-disconnect-all on that server kicks every active client within one request.
  5. `GET /readyz` returns 200 only when DB + Redis + lava.top + tunnel-server heartbeat are all healthy; flipping any one to red flips the response to 503. `GET /livez` returns 200 whenever the process is alive.
  6. The operator toggles "Maintenance mode" from system controls; all non-admin requests immediately return 503 with a friendly message; admin routes continue to work.
**Plans**: TBD
**UI hint**: yes

### Phase 8: Cleanup & hardening
**Goal**: Stripe is gone, mobile tokens live in the platform keychain, refresh tokens are opaque and device-bound, security headers are applied to admin, `govulncheck` runs on every PR, and every observation made by the four audit reports that wasn't fixed earlier is closed.
**Depends on**: Phase 3 (HARD-01 deletes the Stripe code that Phase 3 superseded — it can only land once lava.top has been the sole payment path for a stable period). Independent of Phases 4, 5, 6, 7.
**Requirements**: HARD-01, HARD-02, HARD-03, HARD-04, HARD-05, HARD-06, HARD-07, HARD-08, HARD-09, HARD-10, HARD-11, HARD-12, HARD-13, HARD-14, HARD-15, HARD-16, HARD-17
**Success Criteria** (what must be TRUE):
  1. `grep -rn stripe server/` returns zero hits in `.go` files; `stripe-go` is absent from `go.mod`; `subscriptions.stripe_id` is dropped via migration.
  2. A user's `/servers/:id/config` returns a VLESS UUID specific to that user, and changing the user's plan rotates that UUID — two users on the same plan get different UUIDs for the same server.
  3. `govulncheck` runs in CI on every PR; a PR introducing a vulnerable dependency fails the check and is unmergeable.
  4. A refresh token issued to device A is rejected if presented from device B — the refresh session is bound to `device_id` at issue time.
  5. On iOS, auth tokens stored by the app are present in the Keychain (verifiable via Xcode) and absent from `AsyncStorage` plist; on Android, they live in `EncryptedSharedPreferences`.
  6. The zap log encoder redacts any string matching a JWT or base64url-32 pattern — even a stray `zap.String("token", x)` results in `"token": "[REDACTED]"` in the log aggregator.
  7. Admin routes return `Strict-Transport-Security`, `X-Content-Type-Options`, and a CSP header; admin search rejects searches shorter than 3 characters and never uses `ILIKE %x%` on non-indexed columns.
**Plans**: TBD

## Dependency Graph

```
Phase 1 (Hotfix)
   │
   ▼
Phase 2 (Auth SSO backend)
   │
   ▼
Phase 3 (Lava.top + plans catalog)
   │
   ├──────┬──────┬──────┬──────┐
   ▼      ▼      ▼      ▼      ▼
Phase 4  Phase 5  Phase 6  Phase 7  Phase 8
(Web)    (Mobile) (Perf)   (Admin)  (Hardening)
   ↑      ↑
   └──┬───┘
      independent of each other; can run in parallel
```

- Phase 1 → Phase 2 → Phase 3 is the **critical path** (must be sequential).
- Phases 4 and 5 both depend on Phase 3; they are independent of each other and can land in parallel.
- Phases 6, 7, and 8 depend only on Phase 3 and are independent of each other and of Phases 4/5.

## Progress

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Hotfix — audit critical fixes | 0/? | Not started | - |
| 2. Auth SSO backend | 0/? | Not started | - |
| 3. Lava.top + plans catalog | 0/? | Not started | - |
| 4. Landing surfaces | 0/8 | Not started | - |
| 5. Mobile SSO + Pro CTA | 0/? | Not started | - |
| 6. Performance & scalability | 0/? | Not started | - |
| 7. Admin panel overhaul | 0/? | Not started | - |
| 8. Cleanup & hardening | 0/? | Not started | - |

---
*Roadmap defined: 2026-05-22*
*Last updated: 2026-05-22 — Phase 2 plan list revised (W-3): plan 03 moved to Wave 1 (parallel with plans 01 & 02); plan 04 picks up AUTH-08 dependency + ReassignDevicesByUserID per W-1.*
