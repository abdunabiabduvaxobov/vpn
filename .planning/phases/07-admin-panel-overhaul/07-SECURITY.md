---
phase: 07
slug: admin-panel-overhaul
status: verified
threats_open: 0
asvs_level: 2
created: 2026-06-01
---

# Phase 07 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Verified by direct code inspection (file:line evidence). The gsd-security-auditor
> agent failed twice in this runtime (tool_uses: 0 — emitted hallucinated tool-call
> text without reading any file), so the orchestrator performed the verification
> directly against HEAD `40ac67d`.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| anonymous internet → /livez, /readyz | unauthenticated health probes (LB, uptime monitor) | dep status WORDS only (ok/down), no detail |
| tunnel server → /internal/servers/:id/heartbeat | machine-to-machine, shared-secret authed | server id, load int |
| API → lava.top | outbound reachability probe (cached ≤60s, never per-request) | none outbound |
| admin SPA → /admin/* | authenticated admin (AuthRequired + AdminRequired re-reads role per request) | full admin data, bearer token |
| admin force-cancel ⟷ lava webhook | two writers racing on one user's subscription state | user_id, tier, contract state |
| replay ⟷ live webhook | a replay must not race a live webhook into a double grant | user_id, tier |
| anonymous/mobile → GET /broadcasts | unauthenticated public read | {title, body, severity} only |
| every request → Maintenance middleware | gate that must not lock the operator out | flag state |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation (evidence) | Status |
|-----------|----------|-----------|-------------|------------------------|--------|
| T-07-01 | Tampering | migration 024 backfill | mitigate | Idempotent: `ADD COLUMN IF NOT EXISTS` + deterministic status backfill + `ON CONFLICT DO NOTHING` — `migrations/024_admin_panel_overhaul.sql` | closed |
| T-07-02 | DoS | migration ALTER lock window | accept | PG16 metadata-only add on small columns; single-VM low write volume | closed |
| T-07-03 | Info Disclosure | feature_flags seed | accept | Operational booleans only, no secrets | closed |
| T-07-04 | Elevation | /admin/stats access | mitigate | `admin` group `AdminRequired(db)` (per-request DB role re-read) — `cmd/main.go:399-401` | closed |
| T-07-05 | DoS | MRR multi-join per poll | mitigate | 5-min Redis cache `cache:admin:mrr:<currency>`, fail-open — `handler/admin.go:545,590` | closed |
| T-07-06 | Info Disclosure | KPI numbers | accept | Aggregate counts only, no PII, admin-only | closed |
| T-07-07 | Tampering | currency param → cache key/query | mitigate | Cache-key suffix + parameterized WHERE, no concat — `repository/admin_repo.go` | closed |
| T-07-08 | Spoofing | tunnel-heartbeat endpoint | mitigate | `subtle.ConstantTimeCompare` + `INTERNAL_HEARTBEAT_SECRET` required at boot — `middleware/internal_secret.go:24`, `config.go:157,272` | closed |
| T-07-09 | Info Disclosure | readyz dep status | mitigate | `statusWord()` per dep — words only, no error strings/hostnames/versions — `handler/health.go:72-75` | closed |
| T-07-10 | DoS | readyz dialing lava per probe | mitigate | lava reachability cached ≤60s; 2s dial timeout; livez zero I/O (`status:"alive"`) — `handler/health.go:50`, `cache/health_cache.go` | closed |
| T-07-11 | Tampering | heartbeat load_percent | accept | Display int, parameterized UPDATE, no security impact | closed |
| T-07-12 | Elevation | /internal bypassing admin auth | mitigate | `internalGroup` mounts `InternalSecret`, separate from `/admin` — `cmd/main.go:302` | closed |
| T-07-13 | Elevation | suspended user via cached existence | mitigate | `DeleteUserSessions` + `BustUserCache` + `SuspendedRequired` per-request DB read of `suspended_at` → 403 — `handler/admin_user_controls.go:93,101`, `middleware/suspended.go:52` | closed |
| T-07-14 | DoS | force-disconnect blast / double-click | mitigate | `IncrRateLimit` ≤1/user/30s → 429 — `handler/admin_user_controls.go:182,189` | closed |
| T-07-15 | Repudiation | suspend without justification | mitigate | `CreateAuditEntry` with `Details["reason"]` (Pitfall 4) — `handler/admin_user_controls.go` | closed |
| T-07-16 | Tampering | reason into audit JSONB | mitigate | Trimmed + length-capped, parameterized JSONB via GORM Valuer — `handler/admin_user_controls.go` | closed |
| T-07-17 | DoS | admin self-lockout via SuspendedRequired | mitigate | `admin` group carries only `AdminRequired`; `SuspendedRequired` mounted on `protected` group only — `cmd/main.go:349,399` | closed |
| T-07-18 | Tampering | concurrent force-cancel + payment.success → hybrid | mitigate | Both take `pg_advisory_xact_lock(hashtextextended(?,0))` same key — `repository/lock.go:54`, `webhook_lava.go:263`, `admin_user_controls.go:330` | closed |
| T-07-19 | Tampering | webhook replay/dup → double grant | mitigate | Natural-key UNIQUE index + `ON CONFLICT DO NOTHING` + set-not-increment grant — `migrations/020_lava_payments.sql:76`, `repository/webhook_event_repo.go:15` | closed |
| T-07-20 | DoS | advisory-lock exhaustion | mitigate | xact-scoped, auto-release on commit/rollback — `repository/lock.go:45-59` | closed |
| T-07-21 | Spoofing | force-cancel keyed on wrong identity | mitigate | Both lock on resolved user_id (webhook resolves `inv.UserID`/`parent.UserID`/`contract.UserID`) — `webhook_lava.go:263,358,462,507`, `admin_user_controls.go:330` | closed |
| T-07-22 | Business Logic | lava refund without capability | accept | Records refund INTENT only; no lava refund method called/exists — `handler/admin_user_controls.go:278-280` | closed |
| T-07-23 | DoS | force-disconnect blast on busy server | mitigate | `IncrRateLimit` ≤1/server/60s → 429 — `handler/admin_server_controls.go:74` | closed |
| T-07-24 | Tampering | drained server still served from cache | mitigate | drain/undrain bust `cache:servers:active` synchronously + `ListActiveServers` filters `AND is_draining = false` at DB — `handler/admin_server_controls.go:50,62`, `repository/server_repo.go:24` | closed |
| T-07-25 | Elevation | server controls without admin | mitigate | All routes on `admin` (AdminRequired + AuditLog) group — `cmd/main.go:399` | closed |
| T-07-26 | Availability | can't kill live tunnels real-time | accept | Option-B (LOCKED): mark-disconnected + drain; live tunnels die on ~3-min stale sweep — documented weaker guarantee | closed |
| T-07-27 | DoS/Elevation | maintenance locks operator out | mitigate | Exempts /admin, /auth/admin-login, /livez, /readyz, /internal (WR-01: trailing-slash normalized); fail-open on read error — `middleware/maintenance.go`, `cmd/main.go:262` | closed |
| T-07-28 | DoS | per-request DB read for flag | mitigate | 10s Redis cache fronts DB, fail-open — `cache/flags_cache.go:25` | closed |
| T-07-29 | Info Disclosure | broadcast public leak | mitigate | Public handler projects to DTO `{title, body, severity}` only — `handler/broadcasts.go:12-17` | closed |
| T-07-30 | Tampering | severity / flag value injection | mitigate | `validBroadcastSeverities` enum allowlist, flag strict bool, reason capped, parameterized GORM — `handler/admin_system.go:20-22` | closed |
| T-07-31 | Elevation | flag/broadcast writes without admin | mitigate | All mutating routes on `admin` (AdminRequired + AuditLog) group — `cmd/main.go:399,438` | closed |
| T-07-32 | Tampering | replay → double tier grant | mitigate | Replay calls the SAME `applyLavaEvent` (set-not-increment via SetUserPlanTx) — `handler/admin_webhooks.go:205` | closed |
| T-07-33 | Tampering | replay racing live webhook reopens race | mitigate | `applyLavaEvent` success path takes same `WithUserLock(user_id)` — `handler/admin_webhooks.go:202-205`, `webhook_lava.go:162` | closed |
| T-07-34 | Info Disclosure | buyer email in webhook log list | mitigate | `redactEmails` on LIST payload preview; full DETAIL admin-only + audited — `handler/admin_webhooks.go:29,92,109` | closed |
| T-07-35 | Elevation | replay without admin | mitigate | Replay route on `admin` group; writes replay audit row with reason — `cmd/main.go:455`, `handler/admin_webhooks.go` | closed |
| T-07-36 | Tampering | replaying non-final event | mitigate | Only DELIVERED/FAILED replayable; else 400 — `handler/admin_webhooks.go:196-198` | closed |
| T-07-37 | Info Disclosure | infra topology exposed | mitigate | deps-health admin-only; public /readyz status-words only — `handler/admin_system.go`, `handler/health.go:72` | closed |
| T-07-38 | DoS | deps-health dialing lava per poll | mitigate | Reuses ≤60s cached lava reachability; DB/Redis 500ms timeouts — `handler/admin_system.go`, `cache/health_cache.go` | closed |
| T-07-39 | Elevation | deps-health without admin | mitigate | Route on `admin` (AdminRequired) group — `cmd/main.go:399` | closed |
| T-07-40 | Tampering (CSRF) | state-changing admin POSTs | accept | Bearer-token axios (Authorization header), NOT cookies → not CSRF-exploitable; documented for Phase 8 if cookie auth ever added | closed |
| T-07-41 | DoS (self) | accidental force-disconnect/drain | mitigate | Confirm dialog echoes target id/hostname + backend throttle backstop — `admin-web/src/pages/UserDetail.tsx:409`, `Servers.tsx:184` | closed |
| T-07-42 | Elevation | maintenance toggle lockout | mitigate | Backend Maintenance exempts admin routes; toggle warning copy — `middleware/maintenance.go`, `admin-web/src/pages/System.tsx` | closed |
| T-07-43 | Info Disclosure | buyer emails in webhook log | mitigate | Server-side redaction (T-07-34); SPA renders only redacted list — `admin-web/src/pages/Payments.tsx` | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-07-01 | T-07-02 | ALTER on small columns is PG16 metadata-only; single-VM low write volume → acceptable lock window | abdunabi (planning) | 2026-06-01 |
| AR-07-02 | T-07-03 | feature_flags seed is operational booleans, no secrets | abdunabi (planning) | 2026-06-01 |
| AR-07-03 | T-07-06 | KPI endpoint returns aggregate counts only, no PII, admin-only | abdunabi (planning) | 2026-06-01 |
| AR-07-04 | T-07-11 | heartbeat load_percent is a display int, parameterized UPDATE | abdunabi (planning) | 2026-06-01 |
| AR-07-05 | T-07-26 | Option-B (LOCKED): no real-time tunnel kill this phase; live tunnels die on ~3-min stale sweep | abdunabi (planning) | 2026-06-01 |
| AR-07-06 | T-07-40 | Admin SPA uses bearer-token axios not cookies → CSRF not applicable; revisit if cookie auth added (Phase 8) | abdunabi (planning) | 2026-06-01 |

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-01 | 43 | 43 | 0 | orchestrator (direct code inspection; gsd-security-auditor agent unusable — tool_uses: 0 twice) |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-01

> Note: T-07-18/20/21 (advisory-lock race) and T-07-32/33 (replay idempotency) mitigations
> are verified present in code. Their load-bearing integration proofs
> (`TestForceCancelWebhookRace`, `TestWebhookReplayIdempotent`) require a Docker-backed
> Postgres and are tracked as pending CI items in `07-HUMAN-UAT.md` — not security gaps.
