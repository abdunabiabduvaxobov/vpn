# Milestones

## v2.2.0 Lava + SSO (Shipped: 2026-06-03)

**Phases completed:** 8 phases, 71 plans, 153 tasks
**Timeline:** 2026-05-22 → 2026-06-03 (12 days)

**Delivered:** A user signs in once with Apple or Google, pays on risevpn.com via lava.top, and Pro unlocks on every device — backed by a hardened single-tenant Go API, dynamic plans catalog, and per-user VLESS wire enforcement.

**Key accomplishments:**

- **Apple + Google SSO backend** (Phase 2) — pure-library JWKs/idtoken verification, stable cross-surface `users.id`, in-place guest→SSO promotion, auto-link by verified email (relay rejected), logout with token blacklist.
- **lava.top payment integration** (Phase 3) — dynamic `plans`/`plan_offers`/`plan_servers` catalog replacing hardcoded tiers, `/checkout`, and a strictly-idempotent webhook (UNIQUE event key, constant-time API-key check, IP allowlist, 500-on-error retries) that grants Pro under a per-user advisory lock.
- **Landing money flow** (Phase 4) — `/login` (Apple+Google), dynamic `/pricing`, `/pay/success` invoice polling, and a Node-runtime API proxy with HttpOnly cookies + 401→refresh→retry; 10 Playwright E2E covering SC#1–6.
- **Mobile SSO + Pro CTA** (Phase 5/8) — Apple/Google sign-in, informational "Upgrade at risevpn.com" CTA (no IAP), and auth tokens moved to iOS Keychain / Android EncryptedSharedPreferences with `device_id` on refresh.
- **Security hardening** (Phase 8) — per-user VLESS UUIDs with real tunnel-side wire enforcement (rotate on plan change), opaque 32-byte device/IP-bound refresh tokens, admin security headers, zap token redaction, fail-closed rate limiting, bcrypt 12, full Stripe removal with a durable regression fence, and a blocking govulncheck CI gate.
- **Performance & scale** (Phase 6) — Redis caching for `/servers` + user-tier lookups, heartbeat bulk-flush, perf indexes, `ctx` propagation, `RUN_SCHEDULER` gate.
- **Admin panel overhaul** (Phase 7) — live KPI dashboard (MRR/paid/churn), per-user controls (suspend/force-Pro/force-disconnect), server drain, webhook log + idempotent replay, feature flags / maintenance mode / broadcasts, readyz/livez.
- **8 critical hotfixes** (Phase 1) — subscription-expiry persistence, instant admin-demotion, atomic rate-limit INCR+EXPIRE, scrubbed 5xx bodies, transactional refresh rotation, stdin createadmin, sessions UNIQUE index, fail-fast env validation.

**Quality:** All 8 phases verified; all 8 `nyquist_compliant`; all 17 HARD review warnings fixed; milestone audit found + fixed a critical mobile-SSO contract bug (camelCase↔snake_case) and a VLESS-revoke gap before completion.

**Known deferred (operator HUMAN-UAT before GA):** live Apple/Google OAuth, lava.top sandbox payment, on-device Keychain inspection, ~10k load test, and the GitHub branch-protection toggle for the govulncheck gate — tracked across 6 `*-HUMAN-UAT.md` files.

---
