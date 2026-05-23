# Phase 3: Lava.top + plans catalog - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-23
**Phase:** 03-lava-top-plans-catalog
**Areas discussed:** Stripe disposition + chunking, Lava offer ID + seed strategy, Webhook security + failure semantics, User-facing policy edges, Threat model coverage

---

## Stripe disposition + chunking

### Stripe code handling

| Option | Description | Selected |
|--------|-------------|----------|
| Rewrite payment.go in-place | Delete Stripe handlers, replace with lava in same file; keep stripe-go in go.mod until Phase 8 | ✓ |
| Add payment_lava.go alongside | Leave Stripe dormant; create new file | |
| Hybrid: rewrite payment.go + payment_test.go skeleton | Rewrite both; drop stripe-go from payment.go | |

**User's choice:** Rewrite payment.go in-place (recommended).
**Notes:** Matches ADR §19.12. Clean diff. Test file becomes orphaned until Phase 8 cleanup.

### Phase 3 chunking

| Option | Description | Selected |
|--------|-------------|----------|
| ~7-9 plans across 4-5 waves | Standard wave-based parallelism per Phase 2 pattern | ✓ |
| 3-4 bigger plans | Faster planning, bigger blast radius per commit | |
| 10-14 small plans | Tiny diffs, heavier overhead | |
| Let planner decide | Capture as guidance, planner picks | |

**User's choice:** ~7-9 plans / 4-5 waves.

### Branching

| Option | Description | Selected |
|--------|-------------|----------|
| Working branch, atomic per-plan commits | Match Phase 1/2 pattern | ✓ |
| Per-phase branch `phase-3-lava` | Separate branch, single PR at end | |
| Per-wave PRs | Smaller review units, risk of broken interim state | |

**User's choice:** Working branch, atomic per-plan commits.

### Sandbox vs production

| Option | Description | Selected |
|--------|-------------|----------|
| Sandbox first, prod manual smoke before launch | Standard hosted-checkout sandbox | ✓ |
| Production from start, $1 test offer | Real charges, full prod path | |
| Both — sandbox in dev/CI, prod in staging | Dual config | |

**User's choice:** Sandbox first.

---

## Lava offer ID + seed strategy

### Offer ID sourcing

| Option | Description | Selected |
|--------|-------------|----------|
| Option A: manual paste only | Admin pastes UUID in form field | |
| Option A + B together | Both paste and synced dropdown | |
| Option B only: synced dropdown | Admin picks from `GET /admin/lava/products` proxy | ✓ |

**User's choice:** Option B only.
**Notes:** Triggers admin-web UI work in Phase 3. ADR §19.13 was originally Phase 3.5.

### Seed plan_offers

| Option | Description | Selected |
|--------|-------------|----------|
| Plans only, no offers | Admin configures post-deploy | |
| Plans + 2 example offers w/ NULL lava_offer_id | Schema exercised | |
| Plans + 2 offers per currency (6 offers, NULL lava_offer_id) | Full grid placeholders | ✓ |

**User's choice:** 6 placeholder offers (MONTHLY+YEARLY × USD+EUR+RUB).

### Migration numbering

| Option | Description | Selected |
|--------|-------------|----------|
| Bump to 019 + 020 | Sequential after Phase 2's 018 | ✓ |
| Keep ADR numbers, split 018 into a/b | Awkward, no benefit | |
| Timestamp-prefix scheme | Out of scope | |

**User's choice:** 019 + 020.

### Pro max_devices seed (follow-up after picking "Other number")

| Option | Description | Selected |
|--------|-------------|----------|
| 3 devices | Tighter, old "premium" sizing | ✓ |
| 10 devices | Looser, family-friendly | |
| -1 (unlimited) | No cap | |

**User's choice:** 3 devices.

### Option-B reconciliation with seeded offers (follow-up)

| Option | Description | Selected |
|--------|-------------|----------|
| Seeded rows = placeholders; admin opens each → picks lava offer → saves | Migration creates 6 NULL rows; dropdown picker fills each | ✓ |
| Drop the seed offers — admin creates via dropdown POST | Migration only seeds plans | |
| Hybrid: seed 4-grid (USD+RUB), admin adds EUR later | Tighter seed | |

**User's choice:** Placeholder rows + dropdown fills each.

### Option-B scope confirmation (follow-up)

| Option | Description | Selected |
|--------|-------------|----------|
| Ship Option B in Phase 3 (1-2 day cost) | Proxy + admin UI dropdown in Phase 3 | ✓ |
| Defer Option B to Phase 3.5, allow manual paste interim | Backend Option A; Phase 3.5 layers B | |
| Build only proxy endpoint, defer UI to Phase 3.5 | Smallest scope creep | |

**User's choice:** Ship full Option B in Phase 3.

---

## Webhook security + failure semantics

### IP allowlist mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| Env-driven CIDR list LAVA_WEBHOOK_ALLOWED_CIDRS | Default `158.160.60.174/32` | ✓ |
| Hardcoded constant + override env | Mixes secret and code | |
| Reverse-DNS lookup | DNS dep in security path | |

**User's choice:** Env-driven CIDR list.

### Idempotency timestamp casting

| Option | Description | Selected |
|--------|-------------|----------|
| Cast to text via `->>` | Handles string and numeric JSON | ✓ |
| Cast to bigint | Fails on non-numeric values | |
| Hash whole payload (sha256) | Defensive but overkill | |

**User's choice:** Cast to text.

### Failed-renewal semantics

| Option | Description | Selected |
|--------|-------------|----------|
| Immediate is_active=false; downgrade at expires_at via cron | User keeps paid-for time | ✓ |
| 3-day grace, then downgrade | Friendlier but needs reminder pipeline | |
| Immediate downgrade | Harshest, ADR rejects | |

**User's choice:** Immediate is_active=false; cron handles downgrade.

### API-key secret rotation

| Option | Description | Selected |
|--------|-------------|----------|
| Single env var, manual rotation | Simple single source | |
| Two env vars (LAVA_WEBHOOK_SECRET + _PREVIOUS) | Zero-downtime rotation | ✓ |
| Secrets manager (Vault/SOPS) | Out of scope | |

**User's choice:** Two env vars for zero-downtime rotation.

---

## User-facing policy edges

### Server-denied response

| Option | Description | Selected |
|--------|-------------|----------|
| 404 Not Found | Don't leak server existence | ✓ |
| 403 Forbidden | Clear DX | |
| 402 Payment Required | Cute but risky | |

**User's choice:** 404.

### Force-disconnect on plan-server removal

| Option | Description | Selected |
|--------|-------------|----------|
| Let existing connections survive; deny next reconnect | No mid-call drops | ✓ |
| Force-disconnect everyone immediately | Cleaner state, disruptive | |
| Hybrid: warn admin + opt-in checkbox | Most flexible | |

**User's choice:** Survive, deny on reconnect.

### Invoice polling endpoint scope

| Option | Description | Selected |
|--------|-------------|----------|
| DB-only this phase | Cheap, webhook is truth | |
| DB read + lava.top GET fallback after N=5 polls | Defense in depth, `?escalate=true` triggers proxy | ✓ |
| DB read + immediate lava.top GET if pending | Amplifies lava traffic | |

**User's choice:** DB + escalation fallback after 5 polls.

### Expiry cron frequency

| Option | Description | Selected |
|--------|-------------|----------|
| Every 10 minutes | Snappy downgrade | ✓ |
| Every 1 hour | Cheaper, lagging UX | |
| Every 1 minute | Overkill | |
| Event-driven (lazy check on each request) | Moves work to hot path | |

**User's choice:** Every 10 minutes.

---

## Threat model coverage

### ASVS level

| Option | Description | Selected |
|--------|-------------|----------|
| ASVS L1 — same as Phase 2 | Project default | |
| ASVS L2 for payment paths only | L2 on /checkout, /webhook/lava, /admin/plans/*, lava client | ✓ |
| ASVS L2 across whole phase | Overkill for read-only catalog | |
| ASVS L3 for webhook only | May exceed self-verifiable scope | |

**User's choice:** L2 for payment paths only.

### Required threat categories (multi-select)

| Option | Description | Selected |
|--------|-------------|----------|
| Webhook security & idempotency | Replay, burst handling, race, IP spoof, timing attack | ✓ |
| Payment data integrity | Tier from offerId not client metadata, checkout idempotency, invoice ownership | ✓ |
| Outbound SSRF + secret hygiene | Hardcoded base URL, no redirects, key never logged | ✓ |
| Admin abuse + privilege boundary | is_system protection, force-flag handling, audit-log diff, immutable code | ✓ |

**User's choice:** All four categories required.

### Webhook rate-limiting

| Option | Description | Selected |
|--------|-------------|----------|
| No additional rate-limit | Global per-IP middleware suffices; IP allowlist is primary defense | ✓ |
| Dedicated higher rate-limit bucket | Absorb 20-retry bursts | |
| Disable rate-limit on webhook route | Trust IP allowlist alone | |

**User's choice:** No additional rate-limit.

### Threat model authoring format

| Option | Description | Selected |
|--------|-------------|----------|
| Per-plan inline `<threat_model>` block | Matches Phase 2 D-CD | ✓ |
| Phase-level THREAT-MODEL.md | Single catalog | |
| Hybrid: phase-level + per-plan | Both | |

**User's choice:** Per-plan inline blocks.

---

## Claude's Discretion

Areas where user explicitly deferred to Claude/planner:

- Wave 5 plan split (UI vs docs vs both in one plan)
- `LAVA_WEBHOOK_ALLOWED_CIDRS` default vs strict-required (HOTFIX-08 framework dependent)
- `LAVA_API_KEY` vs `LAVA_API_KEY_SANDBOX` selection logic
- Invoice polling escalate threshold (5 polls suggested, planner may tune)
- Public `/plans` cache key shape
- GORM `OnConflict` clause shape for webhook UPSERT
- Confirmation that admin SSO is NOT pulled into scope

## Deferred Ideas

- Lava.top product auto-creation API (Option C) — defer until lava documents endpoint
- Email reminders on failed recurring payment — no email pipeline
- Force-disconnect on plan-server removal as opt-in admin checkbox — backlog candidate
- Per-user advisory lock (ADMIN-03) — ships Phase 7; Phase 3 documents the gap
- Stripe code/dep/column removal — Phase 8 (HARD-01)
- Admin SSO — Apple/Google is consumer-only
- Full admin panel overhaul — Phase 7
- PERF-06 RUN_SCHEDULER env gate — Phase 6
- Webhook event log UI + replay button — Phase 7 (ADMIN-06)
- KPI dashboard — Phase 7 (ADMIN-01)
- Email magic-link SSO (IDX-01) — v2
- Multi-region scale — v2
