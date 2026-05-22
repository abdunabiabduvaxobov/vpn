# RiseVPN

## What This Is

RiseVPN is a consumer VPN service operating on Android (Google Play) and iOS, backed by a single-tenant Go API, Postgres, Redis and a VLESS/REALITY tunnel server. The product is currently free-tier only with anonymous device-based accounts; a Pro tier (paid subscription) is being introduced on the web with lava.top as the sole payment provider, and Apple/Google Sign-In is being added as the primary cross-surface identity so a user pays on the website and gets Pro reflected in the mobile app on next foreground.

## Core Value

**A user signs in once with Apple or Google, pays on risevpn.com via lava.top, and Pro unlocks on every device immediately.** Everything else (admin tooling, performance work, hardening) serves that path.

## Requirements

### Validated

<!-- Inferred from the existing codebase. These already ship in production v2.1.0 on Google Play. -->

- ✓ Anonymous guest login via device_id + device_secret — existing (`server/api/internal/handler/auth.go` `GuestLogin`)
- ✓ VLESS/REALITY tunnel with multi-protocol fallback (CARD/AWG) — existing (`server/tunnel/`)
- ✓ Admin web panel with users + servers CRUD, audit log, analytics — existing (`admin-web/`, `handler/admin.go`)
- ✓ Telegram recovery flow for cross-device account restore (ADR-006) — existing (`bot/recovery.go`)
- ✓ Free-tier device limit + speed cap enforcement — existing (`handler/connection.go`, hardcoded `PlanLimits` map)
- ✓ JWT access (5 min) + refresh (30 day) auth with Redis blacklist infrastructure — existing
- ✓ Per-IP + per-user rate limiting via Redis — existing (`middleware/ratelimit.go`)
- ✓ Audit-logged admin actions — existing (`middleware/audit.go`)
- ✓ React Native mobile app published on Play Store as v2.1.0 — existing (`app/`)
- ✓ Next.js 16 landing site with EN/RU/ES i18n — existing (`landing/`)

### Active

<!-- v2.2.0 milestone: "Lava.top + SSO refactor + audit fixes". Driven by docs/audit/MASTER-PLAN.md. -->

- [x] Stop-the-bleeding fixes: 8 critical bugs (subscription_expires_at persistence, AdminRequired DB re-read, atomic INCR+EXPIRE, ErrorHandler leak, transactional refresh rotation, createadmin stdin password, sessions index, payment env validation) — **Phase 1 complete (v2.2.0-hotfix tagged 2026-05-22). Staging smoke WAIVED by operator; live verification deferred. See `.planning/phases/01-hotfix-audit-critical-fixes/PHASE-SUMMARY.md`.**
- [ ] Sign in with Apple + Sign in with Google on backend (`/auth/apple`, `/auth/google`)
- [ ] Account-linking by verified email; reject `@privaterelay.appleid.com` from linking
- [ ] Lava.top HTTP client (`internal/lava/`) + checkout endpoint (`POST /checkout`)
- [ ] Lava.top webhook handler with strict idempotency, IP allowlist via Fiber `TrustedProxies`, constant-time `X-Api-Key` check
- [ ] Dynamic plans catalog (`plans`, `plan_servers`, `plan_offers` tables) replacing hardcoded `PlanLimits` map
- [ ] Server access enforcement at repository layer via `plan_servers` join (admin bypass)
- [ ] Admin CRUD for plans / offers / plan-servers (`/admin/plans/*`)
- [ ] Public `GET /api/v1/plans` for landing /pricing dynamic rendering
- [ ] Landing site: `/login`, `/dashboard`, `/pricing`, `/pay/success` (polls invoice as defense in depth), `/pay/fail`
- [ ] Mobile: LoginScreen (Apple + Google + Guest), PaymentScreen informational only (no IAP), deep-link return handler
- [ ] Performance: cache `/servers`, move heartbeat to Redis with bulk flush, cache user lookup, missing DB indexes
- [ ] Admin panel overhaul: KPI dashboard (MRR / paid users / churn), user controls (suspend, force-Pro, force-disconnect), webhook log + replay, system controls (feature flags, maintenance mode, broadcast), readyz/livez
- [ ] Cleanup: delete Stripe code, per-user VLESS UUID rotation, refresh tokens → 32-byte opaque, security headers middleware, govulncheck in CI

### Out of Scope

- **In-app purchase via Apple IAP / Google Play Billing** — App Store and Play Store rules require their billing for digital subs, BUT the operator's explicit choice is to direct users to the website for Pro. Mobile app shows informational "Upgrade at risevpn.com" CTA only. Spotify/Netflix precedent.
- **Stripe** — being fully removed (`STRIPE_KEY` and friends drop out of config). Decided after operator confirmed no paying Stripe users exist.
- **Backwards compatibility with v2.1.0 user data** — operator confirmed no paying users; only free guest accounts in the wild. Migration is "best effort" not "must preserve".
- **Per-server pricing** — plans bundle servers, not the other way round (ADR-007 §19 open Q resolved).
- **Mid-cycle subscription upgrade with proration** — lava.top does not support proration; users upgrade by cancelling and re-subscribing.
- **Telegram-only recovery for paid users** — Telegram recovery remains for backward compatibility but Apple/Google SSO becomes the primary identity. Telegram link is supplementary.
- **Apple Hide My Email private relay** as an account-linking key — relays are user-scoped, not global identity. Reject `@privaterelay.appleid.com` from automatic email-linking.

## Context

- **Operator situation:** Solo founder. App is published on Play Store as v2.1.0 but Stripe was never actually wired up there — there are zero paying users today. Goal is to launch Pro for real with a payment processor that works for RU/CIS/EU customers (Stripe doesn't reliably serve those markets), under a tier model the operator can manage from the admin panel without code changes.
- **Production deployment:** Single VM at `194.87.31.44` running `docker-compose.prod.yml`: Postgres + Redis + API + tunnel containers. Tunnel binds :443, API binds :3000 behind localhost. Admin panel runs at `vpnadmin.mydayai.uz:9443` per the locked CORS allowlist.
- **Codebase architecture:** Monorepo with 4 surfaces — `server/api` (Go Fiber backend), `server/tunnel` (Go VLESS/REALITY proxy), `app` (React Native), `landing` (Next.js 16), `admin-web` (Vite + React 19 + shadcn/ui).
- **Audit findings:** 4 parallel audit agents (code review, security, performance, admin panel) produced reports under `docs/audit/`. 4 critical / 8 high / 12 medium code issues. 3 critical / 8 high security issues. 4 P0 perf wins (sessions index, `/servers` cache, Redis heartbeat, off-host PG/Redis). 32 new admin endpoints + 5 new pages designed. All consolidated into `docs/audit/MASTER-PLAN.md`.
- **Architecture decision record:** `docs/ADR-007-lava-sso-rework.md` is the source of truth for SSO + payment + dynamic-plans design. Section 19 covers the dynamic-plans extension. Section 15 lists open questions / blockers that must be resolved before Phase 2 (SSO) can begin: Apple Developer Team ID/Bundle ID/Service ID/.p8 key, Google OAuth client IDs (iOS/Android/Web), lava.top offer IDs, account-linking policy, Pro device limit.
- **Telegram recovery (ADR-006):** Existing cross-device identity mechanism. Stays in place but becomes secondary to Apple/Google SSO.
- **No paying users:** operator confirmed only free guest accounts exist. This is freedom to break things — but not freedom to ship broken security.

## Constraints

- **Tech stack — Backend:** Go 1.22 + Fiber v2 + GORM + Postgres 16 + Redis 7. Locked. No language switch.
- **Tech stack — Mobile:** React Native 0.84, TypeScript, Zustand stores, axios, react-navigation. Locked.
- **Tech stack — Landing:** Next.js 16 App Router + next-intl (EN/RU/ES) + shadcn/ui + Tailwind 4. Locked.
- **Tech stack — Admin web:** Vite + React 19 + TanStack Query + shadcn/ui. Locked.
- **Payment provider:** lava.top exclusively. Single-provider strategy.
- **Identity provider:** Apple + Google SSO (web + mobile). Guest device-based login preserved for "try before sign up".
- **App-store compliance:** No IAP buttons in mobile app. CTA points to risevpn.com.
- **Deployment:** Single VM via Docker Compose for v2.2.0. Horizontal scaling (multi-replica API) is a Tranche 3 goal (`RUN_SCHEDULER` env gate) but not required for launch.
- **Security:** No paying users yet but launching Pro means real money flow — security audit findings classified Critical/High MUST land before any user pays. See `docs/audit/SECURITY-AUDIT.md` "Top 3 must-fix-before-lava-launch".
- **Webhook reliability:** lava.top retries up to 20 times. Webhook handler MUST be idempotent (UNIQUE constraint on event identifier) and MUST return 500 on processing error so retries trigger.
- **lava.top constraints:** 8% commission, minimum $5/€5 per offer, payment URL TTL ~24h, contracts identified by UUID.

## Key Decisions

| Decision | Rationale | Outcome |
|---|---|---|
| Use lava.top as sole payment provider, drop Stripe | Stripe restricted in RU/CIS; lava.top accepts cards from 95+ countries, supports SBP/PayPal/Stripe under the hood, has Russian tax/payout pipelines | — Pending (validates after first real payment in Phase 3) |
| No IAP in mobile app — direct users to web | Apple/Google take 15-30% on IAP. Spotify/Netflix precedent allows external-payment apps if no in-app purchase UI is present. Risk: external-link entitlement compliance. | — Pending |
| Apple + Google SSO as primary identity | Operator's explicit ask. Most users already have one of these. Telegram recovery (ADR-006) stays as supplementary. | — Pending |
| Dynamic plans catalog in DB instead of hardcoded `PlanLimits` map | Operator needs admin-panel control over plan name, devices, servers, price without code deploys. Per request after ADR-007 v1. | — Pending |
| Two-tier seed (free + Pro), but configurable | "Two tiers" is just the seed of the dynamic catalog. Operator can add more plans from admin UI later. | — Pending |
| Auto-link Apple + Google accounts by verified email | Reduces account fragmentation. Rejects `@privaterelay.appleid.com` from auto-link because relay is user-scoped. | — Pending |
| Webhook idempotency via UNIQUE on `lava_webhook_events`, 500-on-failure | lava.top retries 20x; "log and continue" pattern (current Stripe handler) is the exact antipattern that lost provisioning events in production-style systems | — Pending |
| Refresh-token rotation transactional (not just delete-then-insert) | Audit S1-1: current pattern silently logs users out on insert failure. Fix lands in Tranche 0. | — Pending |
| AdminRequired re-reads role from DB | Audit S2-2: 5-minute privilege-revocation lag + self-re-promotion window from stale JWT. Fix lands in Tranche 0. | — Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-05-22 after initialization*
