---
gsd_state_version: 1.0
milestone: v2.2.0
milestone_name: milestone
status: completed
last_updated: "2026-06-03T11:50:21.902Z"
progress:
  total_phases: 8
  completed_phases: 8
  total_plans: 71
  completed_plans: 72
  percent: 100
---

# STATE: RiseVPN

**Updated:** 2026-06-02 — Quick task 260602-214 (guest-login plan_id blocker fixed)

## Project Reference

- **Core Value:** A user signs in once with Apple or Google, pays on risevpn.com via lava.top, and Pro unlocks on every device immediately.
- **Current Milestone:** v2.2.0 — Lava.top + SSO refactor + audit fixes
- **Source of truth:** `docs/audit/MASTER-PLAN.md` + `docs/ADR-007-lava-sso-rework.md`
- **Granularity:** fine (8 phases)
- **Mode:** yolo
- **Model profile:** quality

## Current Position

Phase: 08 (cleanup-hardening) — EXECUTING
Plan: 1 of 9

- **Phase:** 08
- **Plan:** Not started
- **Status:** v2.2.0 milestone complete
- **Progress:** Phases 1–6 implemented; Phase 7 planned

```
[          ] 0% (Phase 0 of 8)
```

## Performance Metrics

| Metric | Value |
|---|---|
| Phases planned | 8 |
| Phases complete | 0 |
| v1 requirements mapped | 75 / 75 ✓ |
| Plans complete | 0 |

## Accumulated Context

### Decisions made (key items from PROJECT.md "Key Decisions")

- lava.top is the sole payment provider; Stripe is being fully removed.
- Apple + Google SSO are the primary identity; guest device-based login stays as a fallback.
- Plans are dynamic (DB-driven) — `PlanLimits` Go map is being deleted; `plans` / `plan_offers` / `plan_servers` replace it.
- Mobile app has NO IAP — Upgrade CTA points to risevpn.com (Spotify/Netflix precedent).
- Auto-link Apple+Google accounts by verified email; reject `@privaterelay.appleid.com` from auto-link.
- Webhook idempotency via UNIQUE on `lava_webhook_events`; 500-on-failure to trigger lava.top retries.
- Refresh-token rotation is transactional.
- `AdminRequired` re-reads role from DB on every admin request.

### Phase 2 blockers (must resolve before starting Phase 2)

From ADR-007 §15:

- [ ] Apple Developer Team ID + Bundle ID + Service ID + `.p8` key
- [ ] Google OAuth client IDs (iOS, Android, Web — three distinct IDs)
- [ ] lava.top offer IDs for monthly + yearly × USD/EUR/RUB (Phase 3 blocker but discoverable in Phase 2)
- [ ] Account-linking policy confirmation (default: auto-link by verified email, reject private-relay)
- [ ] Pro device limit (default recommendation: 5)

### Open todos

- (none yet — populated by `/gsd-plan-phase`)

### Blockers

- (none yet)

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260602-214 | Fix guest-login/createadmin plan_id NOT NULL blocker (assign system plan before CreateUser + real-Postgres onboarding test) | 2026-06-02 | 0e97975 | [260602-214-fix-guest-login-createadmin-plan-id-not-](./quick/260602-214-fix-guest-login-createadmin-plan-id-not-/) |

## Session Continuity

- Last session: 2026-05-22 — roadmap initialization
- Next command: `/gsd-plan-phase 1` to decompose Phase 1 (Hotfix) into executable plans
- Files of record:
  - `/Users/abdunabi/Desktop/vpn/.planning/PROJECT.md`
  - `/Users/abdunabi/Desktop/vpn/.planning/REQUIREMENTS.md`
  - `/Users/abdunabi/Desktop/vpn/.planning/ROADMAP.md`
  - `/Users/abdunabi/Desktop/vpn/docs/ADR-007-lava-sso-rework.md`
  - `/Users/abdunabi/Desktop/vpn/docs/audit/MASTER-PLAN.md`

---
*State initialized: 2026-05-22*
