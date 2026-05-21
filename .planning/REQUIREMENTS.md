# Requirements: RiseVPN

**Defined:** 2026-05-22
**Core Value:** A user signs in once with Apple or Google, pays on risevpn.com via lava.top, and Pro unlocks on every device immediately.

## v1 Requirements

Requirements for v2.2.0 milestone "Lava.top + SSO refactor + audit fixes". Each maps to exactly one phase in ROADMAP.md.

### Hotfix (audit findings — Tranche 0)

- [ ] **HOTFIX-01**: Subscription expiry persists from payment provider's `current_period_end` so the scheduler can auto-downgrade expired Pro users to free
- [ ] **HOTFIX-02**: `AdminRequired` middleware re-reads role from the database on every admin request (closes 5-minute privilege-revocation lag from stale JWT)
- [ ] **HOTFIX-03**: Rate-limit `INCR` and `EXPIRE` execute atomically (Lua script or MULTI/EXEC) so a Redis hiccup can never leave a counter without TTL
- [ ] **HOTFIX-04**: Global `ErrorHandler` returns a generic 500 message; raw `err.Error()` is never sent to the client
- [ ] **HOTFIX-05**: Refresh-token rotation runs inside a single transaction so a failed insert never leaves the user with no session row
- [ ] **HOTFIX-06**: `createadmin` CLI reads password from stdin (not argv); seed admin defaults to `subscription_tier='free'`
- [ ] **HOTFIX-07**: `sessions.refresh_token_hash` has a UNIQUE index so `/auth/refresh` is an index lookup, not a sequential scan
- [ ] **HOTFIX-08**: API server fails to start when any required payment-provider env var is missing or empty (no silent defaults to `""` or placeholders)

### Auth — SSO backend

- [ ] **AUTH-01**: User can sign in with Apple from any client; backend verifies the Apple ID token's signature via Apple's JWKs and the audience matches the registered Bundle ID or Service ID
- [ ] **AUTH-02**: User can sign in with Google from any client; backend verifies the Google ID token via `google.golang.org/api/idtoken` and the audience matches the registered iOS / Android / Web OAuth client IDs
- [ ] **AUTH-03**: New `users.apple_user_id`, `users.google_user_id`, `users.email`, `users.email_verified`, `users.email_is_private_relay`, `users.auth_provider` columns exist with partial-unique indexes on each provider id
- [ ] **AUTH-04**: Same Apple `sub` returns the same `users.id` across mobile, website, and admin; same Google `sub` does the same
- [ ] **AUTH-05**: A guest user (device-based) who later signs in with Apple or Google is promoted in-place to a SSO-bound user when no other row owns that provider id
- [ ] **AUTH-06**: Apple+Google accounts with the same verified email auto-link to one user row, EXCEPT when the email ends in `@privaterelay.appleid.com` (relay rejected from auto-link)
- [ ] **AUTH-07**: Backend mints HS256 access (5 min) + refresh (30 day) tokens identical in shape to today's after Apple/Google signin
- [ ] **AUTH-08**: `POST /api/v1/auth/logout` endpoint exists, deletes the refresh-session row and blacklists the calling access token until its `exp`

### Pay — Lava.top + plans catalog

- [ ] **PAY-01**: New `plans`, `plan_servers`, `plan_offers` tables exist with the schema from ADR-007 §19.3; existing hardcoded tiers are seeded as rows; `users.plan_id` FK replaces the hardcoded tier string as source of truth (denormalized `subscription_tier` kept for JWT convenience)
- [ ] **PAY-02**: `POST /api/v1/checkout` accepts `{plan_code, periodicity, currency}` and returns `{payment_url, invoice_id}` from lava.top
- [ ] **PAY-03**: `POST /api/v1/webhook/lava` handles `payment.success`, `payment.failed`, `subscription.recurring.payment.success`, `subscription.recurring.payment.failed`, `subscription.cancelled` events
- [ ] **PAY-04**: Webhook handler is strictly idempotent — duplicate events for the same `(eventType, contractId, timestamp)` insert into `lava_webhook_events` is rejected by UNIQUE and the handler returns 200 without re-applying the side effect
- [ ] **PAY-05**: Webhook handler returns HTTP 500 on processing errors so lava.top retries (per their 20-attempt policy); only successful application returns 200
- [ ] **PAY-06**: Webhook IP allowlist is enforced via Fiber's `EnableTrustedProxyCheck` + `TrustedProxies` config; reading `X-Forwarded-For` / `X-Real-IP` directly is forbidden
- [ ] **PAY-07**: `X-Api-Key` comparison in the webhook handler uses `crypto/subtle.ConstantTimeCompare`
- [ ] **PAY-08**: Plan tier is derived from the lava.top `offerId` in the webhook payload via `plan_offers` lookup, NEVER from client-supplied metadata
- [ ] **PAY-09**: Subscription expiry (`subscription_expires_at`) is set from the webhook's `period_end` on every successful renewal
- [ ] **PAY-10**: `POST /api/v1/subscription/cancel` calls `DELETE /api/v1/subscriptions` on lava.top; user keeps Pro until current period ends (no immediate downgrade)
- [ ] **PAY-11**: Server access is enforced at the repository layer — `ListServersForPlan(planID)` and `IsServerAllowedForPlan(planID, serverID)` filter by `plan_servers`. Admins bypass the filter.
- [ ] **PAY-12**: `GET /api/v1/plans` (public, no auth) returns active plans with their offers in the caller's preferred currency for the landing /pricing page
- [ ] **PAY-13**: Admin can CRUD plans via `GET/POST/PATCH/DELETE /api/v1/admin/plans`; soft-delete refuses `is_system=true` plans
- [ ] **PAY-14**: Admin can manage which servers belong to a plan via `PUT /admin/plans/:id/servers` (replace), `POST /admin/plans/:id/servers/:server_id` (add), `DELETE /admin/plans/:id/servers/:server_id` (remove)
- [ ] **PAY-15**: Admin can manage offers (multi-currency × multi-period) via `GET/POST/PATCH/DELETE /admin/plans/:id/offers/...` with `POST .../offers/replace` for price versioning (existing subscribers grandfathered on old price)
- [ ] **PAY-16**: Lava.top HTTP client lives in `internal/lava/`, hard-codes the base URL `https://gate.lava.top`, uses a 5-second context timeout, no SSRF surface

### Web — Landing surfaces

- [ ] **WEB-01**: `/login` page on landing.risevpn renders Sign in with Apple + Sign in with Google buttons (using Apple JS SDK + Google Identity Services)
- [ ] **WEB-02**: `/auth/apple/callback` and `/auth/google/callback` Next.js route handlers exchange tokens with the backend and set HttpOnly Secure cookies (no localStorage for session tokens)
- [ ] **WEB-03**: `/dashboard` shows current plan, billing history, "Manage subscription" link, and "Sign out" — protected by middleware that redirects unauthenticated users to `/login`
- [ ] **WEB-04**: `/pricing` renders dynamically from `GET /api/v1/plans` (no hardcoded prices in `landing/`), respects the active locale and currency, and uses ISR with on-demand revalidate on admin plan-save
- [ ] **WEB-05**: "Get Pro" on `/pricing` calls `POST /checkout` and redirects to the lava.top `paymentUrl`; if not authenticated, redirects to `/login?next=/pricing&plan=pro&period=monthly`
- [ ] **WEB-06**: `/pay/success?invoiceId=X` polls `GET /api/v1/invoices/{id}` for up to 30 seconds, shows success once `status=COMPLETED`, and surfaces a clear "we'll email you" message if the webhook hasn't landed yet
- [ ] **WEB-07**: `/pay/fail` shows a friendly retry CTA pointing back to `/pricing`
- [ ] **WEB-08**: i18n keys for `/login`, `/dashboard`, `/pricing`, `/pay/success`, `/pay/fail` exist in `messages/en.json`, `messages/ru.json`, `messages/es.json`
- [ ] **WEB-09**: Navbar and footer expose Pricing, Login (when logged out), Dashboard + Sign out (when logged in)

### App — Mobile SSO + Pro CTA

- [ ] **APP-01**: Sign in with Apple flow works on iOS via `@invertase/react-native-apple-authentication`; backend `POST /auth/apple` returns the same JWT shape as guest login
- [ ] **APP-02**: Sign in with Google flow works on iOS and Android via `@react-native-google-signin/google-signin`; backend `POST /auth/google` returns same JWT shape
- [ ] **APP-03**: `LoginScreen` is the auth entry for users without a session; offers "Continue with Apple", "Continue with Google", "Continue as Guest" (guest path preserved)
- [ ] **APP-04**: A guest user who taps "Continue with Apple/Google" upgrades their existing guest row to an identified user (single user_id preserved across the transition)
- [ ] **APP-05**: `PaymentScreen` is informational only — shows current plan limits and a single "Upgrade to Pro at risevpn.com" button that opens `https://risevpn.com/<locale>/pricing` in the system browser. No IAP, no buy button.
- [ ] **APP-06**: Universal Link / App Link `vpnapp://payment/success?invoiceId=X` returns the user to the app; app polls `GET /invoices/{id}` and refreshes plan state. iOS Info.plist and Android intent filter registered.
- [ ] **APP-07**: app.json bumped to `2.2.0`; build ships to TestFlight (iOS) and Play Internal Track (Android)

### Perf — Performance & scalability

- [ ] **PERF-01**: `GET /servers` results are cached in Redis with admin-side invalidation (cache key bust on any admin server-write); cache TTL ≤ 5 minutes
- [ ] **PERF-02**: Connection heartbeat writes go to Redis first; a scheduler-flushed bulk update to Postgres runs every 10 seconds (eliminates ~167 write-q/s at 10k users)
- [ ] **PERF-03**: Postgres and Redis run on separate hosts (or as separate scaled services) in production; not co-located with the API container on the same VM
- [ ] **PERF-04**: User existence + tier are cached in Redis for `AuthRequired` with TTL ≤ 5 seconds; cache busted on admin user-update
- [ ] **PERF-05**: New `idx_connections_heartbeat_active` partial index supports the stale-connection sweep query in O(connected) not O(history)
- [ ] **PERF-06**: `RUN_SCHEDULER` env gate exists; the scheduler only starts when set to `true`, allowing N-1 of N API replicas to run without the scheduler
- [ ] **PERF-07**: Every GORM call uses `db.WithContext(ctx)`; no unbounded query can outlive its request
- [ ] **PERF-08**: New `idx_connections_connected_at` index + 90-day pruning of historical connection rows runs in the scheduler
- [ ] **PERF-09**: Fiber config sets `BodyLimit: 64*1024`, `ReadTimeout: 15s`, `WriteTimeout: 30s`; Postgres `postgresql.conf` is tuned for the actual host RAM via `pgtune` defaults

### Admin — Admin panel overhaul

- [ ] **ADMIN-01**: Dashboard shows live KPIs — total users, paid users this period, MRR estimate, active connections, signups today / this week / this month, churn count, failed payments count
- [ ] **ADMIN-02**: Per-user controls — suspend, force-grant Pro (comp), force-cancel Pro (refund), reset to free, force-disconnect all devices, view audit history, view payment history, view connection history
- [ ] **ADMIN-03**: Per-user actions hold a per-user advisory lock against concurrent payment webhook processing so admin "force-cancel" + webhook `payment.success` can never leave the user in inconsistent state
- [ ] **ADMIN-04**: Server controls — force-disconnect all clients on a server, mark in/out of rotation, "drain" mode (no new connections; existing ones survive)
- [ ] **ADMIN-05**: System controls — feature flags (turn off signups, turn off new payments), maintenance mode (returns 503 to non-admins), broadcast banner (pushes a message visible in all mobile clients on next foreground)
- [ ] **ADMIN-06**: Webhook event log view shows every received lava.top webhook with status (DELIVERED/FAILED/REPLAYED), payload, and a "replay this event" button
- [ ] **ADMIN-07**: `GET /readyz` returns 200 when DB + Redis + lava.top + tunnel are all healthy; `GET /livez` returns 200 if the process is alive
- [ ] **ADMIN-08**: Dependencies-health page shows live status of DB, Redis, lava.top API reachability, tunnel-server heartbeat

### Hard — Cleanup & hardening

- [ ] **HARD-01**: All Stripe code is removed from `handler/payment.go`, `cmd/main.go`, `config/config.go`; `stripe-go` is removed from `go.mod`; migration drops `subscriptions.stripe_id`
- [ ] **HARD-02**: Tunnel-side change: per-user VLESS UUIDs are assigned on first connection and rotated on plan changes; `GET /servers/:id/config` returns the per-user UUID, not a shared one
- [ ] **HARD-03**: Refresh tokens are 32-byte opaque random strings (URL-safe base64), not JWTs; `recovery/start_token.go` pattern is reused
- [ ] **HARD-04**: Refresh sessions are bound to `device_id` + IP at issue; refresh rejects if device_id changes
- [ ] **HARD-05**: Telegram bot refuses non-private chats (`msg.Chat.Type != "private"`)
- [ ] **HARD-06**: Admin user-search requires `len(search) >= 3` and uses prefix-match on indexed columns only (no `ILIKE %x%` scans)
- [ ] **HARD-07**: Audit log for admin role changes records before→after diff
- [ ] **HARD-08**: Security headers middleware (HSTS, X-Content-Type-Options, CSP) applied to admin route group
- [ ] **HARD-09**: `govulncheck` runs in CI on every PR
- [ ] **HARD-10**: zap encoder has a regex redactor for JWT-shaped and base64url{32} strings so accidental `zap.String("token", x)` never leaks to log aggregation
- [ ] **HARD-11**: bcrypt cost increased from 10 to 12; `createadmin` and admin password-change use the new cost
- [ ] **HARD-12**: `LinkAttemptLimit` fails CLOSED on Redis outage (returns 503) instead of fail-open
- [ ] **HARD-13**: `/api/v1/debug/error` has a dedicated 5/min/IP rate-limit bucket
- [ ] **HARD-14**: `ListServers` returns servers in a deterministic order rotated per-user via HMAC(user_id) (defeats fleet enumeration via repeated free signups)
- [ ] **HARD-15**: Mobile RN — `useVpnConnection` (590 lines) is split into smaller hooks; `vpnStore.connect` busy-wait is replaced with an event-driven wait
- [ ] **HARD-16**: Mobile RN — auth tokens move from AsyncStorage to platform secure storage (Keychain on iOS, EncryptedSharedPreferences on Android)
- [ ] **HARD-17**: `/health` no longer returns `runtime.Version()` to unauthenticated callers

## v2 Requirements

Deferred to a future milestone — captured here so they're not forgotten.

### Multi-region / horizontal scale
- **SCALE-01**: Multi-replica API tier with sticky-session-free auth (depends on PERF-06 + plan_id cache)
- **SCALE-02**: Read-replica Postgres for analytics dashboard queries
- **SCALE-03**: Multi-region tunnel servers with geo-DNS routing

### Identity extensions
- **IDX-01**: Email magic-link as a third SSO option (for users who want neither Apple nor Google)
- **IDX-02**: Account-linking UX in dashboard ("link my Google account to this Apple-signed-in account")
- **IDX-03**: 2FA via TOTP for admin users

### Mobile UX polish
- **MUX-01**: Sentry crash reporting integration
- **MUX-02**: In-app subscription management screen (cancel link via web, view billing history)
- **MUX-03**: Apple/Google deep-link from "Upgrade" CTA carries SSO state so user doesn't re-login on web

## Out of Scope

| Feature | Reason |
|---|---|
| In-app purchase (Apple IAP / Play Billing) | Operator explicitly chose web-only payment to keep margin; Spotify/Netflix precedent |
| Stripe as a parallel payment provider | Operator confirmed full removal; no paying Stripe users exist today |
| Backwards compatibility with the v2.1.0 plan model | Operator confirmed no paying users; free hand to break things |
| Per-server pricing | Plans bundle servers; server-level pricing is a different product model |
| Mid-cycle plan upgrade with proration | lava.top does not support proration |
| Apple Hide My Email private relay as a linking key | Relay is user-scoped, not a global identity |
| Telegram-only recovery for paid users | Telegram recovery (ADR-006) stays as a backwards-compat path but Apple/Google SSO is primary identity |
| Multi-region deployment in v2.2.0 | Single-host launch is fine for current scale; multi-region is v2 |

## Traceability

Updated by `gsd-roadmapper` during roadmap creation.

| Requirement | Phase | Status |
|---|---|---|
| HOTFIX-01 | Phase 1 | Pending |
| HOTFIX-02 | Phase 1 | Pending |
| HOTFIX-03 | Phase 1 | Pending |
| HOTFIX-04 | Phase 1 | Pending |
| HOTFIX-05 | Phase 1 | Pending |
| HOTFIX-06 | Phase 1 | Pending |
| HOTFIX-07 | Phase 1 | Pending |
| HOTFIX-08 | Phase 1 | Pending |
| AUTH-01 | Phase 2 | Pending |
| AUTH-02 | Phase 2 | Pending |
| AUTH-03 | Phase 2 | Pending |
| AUTH-04 | Phase 2 | Pending |
| AUTH-05 | Phase 2 | Pending |
| AUTH-06 | Phase 2 | Pending |
| AUTH-07 | Phase 2 | Pending |
| AUTH-08 | Phase 2 | Pending |
| PAY-01 | Phase 3 | Pending |
| PAY-02 | Phase 3 | Pending |
| PAY-03 | Phase 3 | Pending |
| PAY-04 | Phase 3 | Pending |
| PAY-05 | Phase 3 | Pending |
| PAY-06 | Phase 3 | Pending |
| PAY-07 | Phase 3 | Pending |
| PAY-08 | Phase 3 | Pending |
| PAY-09 | Phase 3 | Pending |
| PAY-10 | Phase 3 | Pending |
| PAY-11 | Phase 3 | Pending |
| PAY-12 | Phase 3 | Pending |
| PAY-13 | Phase 3 | Pending |
| PAY-14 | Phase 3 | Pending |
| PAY-15 | Phase 3 | Pending |
| PAY-16 | Phase 3 | Pending |
| WEB-01 | Phase 4 | Pending |
| WEB-02 | Phase 4 | Pending |
| WEB-03 | Phase 4 | Pending |
| WEB-04 | Phase 4 | Pending |
| WEB-05 | Phase 4 | Pending |
| WEB-06 | Phase 4 | Pending |
| WEB-07 | Phase 4 | Pending |
| WEB-08 | Phase 4 | Pending |
| WEB-09 | Phase 4 | Pending |
| APP-01 | Phase 5 | Pending |
| APP-02 | Phase 5 | Pending |
| APP-03 | Phase 5 | Pending |
| APP-04 | Phase 5 | Pending |
| APP-05 | Phase 5 | Pending |
| APP-06 | Phase 5 | Pending |
| APP-07 | Phase 5 | Pending |
| PERF-01 | Phase 6 | Pending |
| PERF-02 | Phase 6 | Pending |
| PERF-03 | Phase 6 | Pending |
| PERF-04 | Phase 6 | Pending |
| PERF-05 | Phase 6 | Pending |
| PERF-06 | Phase 6 | Pending |
| PERF-07 | Phase 6 | Pending |
| PERF-08 | Phase 6 | Pending |
| PERF-09 | Phase 6 | Pending |
| ADMIN-01 | Phase 7 | Pending |
| ADMIN-02 | Phase 7 | Pending |
| ADMIN-03 | Phase 7 | Pending |
| ADMIN-04 | Phase 7 | Pending |
| ADMIN-05 | Phase 7 | Pending |
| ADMIN-06 | Phase 7 | Pending |
| ADMIN-07 | Phase 7 | Pending |
| ADMIN-08 | Phase 7 | Pending |
| HARD-01 | Phase 8 | Pending |
| HARD-02 | Phase 8 | Pending |
| HARD-03 | Phase 8 | Pending |
| HARD-04 | Phase 8 | Pending |
| HARD-05 | Phase 8 | Pending |
| HARD-06 | Phase 8 | Pending |
| HARD-07 | Phase 8 | Pending |
| HARD-08 | Phase 8 | Pending |
| HARD-09 | Phase 8 | Pending |
| HARD-10 | Phase 8 | Pending |
| HARD-11 | Phase 8 | Pending |
| HARD-12 | Phase 8 | Pending |
| HARD-13 | Phase 8 | Pending |
| HARD-14 | Phase 8 | Pending |
| HARD-15 | Phase 8 | Pending |
| HARD-16 | Phase 8 | Pending |
| HARD-17 | Phase 8 | Pending |

**Coverage:**
- v1 requirements: 75 total
- Mapped to phases: 75
- Unmapped: 0 ✓

---
*Requirements defined: 2026-05-22*
*Last updated: 2026-05-22 after initial definition (derived from `docs/audit/MASTER-PLAN.md` and `docs/ADR-007-lava-sso-rework.md`)*
