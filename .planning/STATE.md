---
gsd_state_version: 1.0
milestone: v2.2.0
milestone_name: milestone
status: executing
last_updated: "2026-05-30T15:58:20.371Z"
progress:
  total_phases: 8
  completed_phases: 6
  total_plans: 62
  completed_plans: 53
  percent: 85
---

# STATE: RiseVPN

**Updated:** 2026-05-22

## Project Reference

- **Core Value:** A user signs in once with Apple or Google, pays on risevpn.com via lava.top, and Pro unlocks on every device immediately.
- **Current Milestone:** v2.2.0 — Lava.top + SSO refactor + audit fixes
- **Source of truth:** `docs/audit/MASTER-PLAN.md` + `docs/ADR-007-lava-sso-rework.md`
- **Granularity:** fine (8 phases)
- **Mode:** yolo
- **Model profile:** quality

## Current Position

Phase: 07 (admin-panel-overhaul) — PLANNED (ready to execute)
Plan: 0 of 10 executed

- **Phase:** 7
- **Plan:** 10 plans across 10 waves — none executed yet
- **Status:** Ready to execute
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
