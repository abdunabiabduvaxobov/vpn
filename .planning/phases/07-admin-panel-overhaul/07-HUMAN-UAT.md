---
status: partial
phase: 07-admin-panel-overhaul
source: [07-VERIFICATION.md, 07-10-PLAN.md]
started: 2026-06-01T00:00:00Z
updated: 2026-06-01T00:00:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. Admin UI walkthrough (Plan 07-10 Task 4 — full Phase-7 UAT)
expected: Run `cd admin-web && npm run dev` against a live API with migration 024 applied and at least one tunnel posting heartbeats, then:
- **Dashboard:** KPI bar shows paid users / MRR (USD) / active connections / signups today-week-month / churn 30d / failed payments 30d; active connections refresh ~15s, the rest ~60s, no page reload.
- **Users → test user:** Suspend (with reason) → that user's next API request is 403; Unsuspend restores. Force-cancel Pro (refund + reason) → tier resets to free + audit row with reason in History → Аудит. Force-disconnect → confirm dialog echoes user_id; click twice fast → second shows 429 toast. History tabs (Аудит / Сессии / Подключения) populate.
- **Servers → a server:** toggle Drain → non-admin `GET /servers` no longer lists it while admin still sees it with a "Дренаж" badge. Disconnect-all → confirm dialog echoes hostname.
- **System → Зависимости:** DB/Redis/lava badges + tunnel server with "свежий" badge. **Фичефлаги:** maintenance_mode ON → non-admin request gets 503 while panel keeps working; toggle OFF. signups_off → `/auth/guest` returns 503. **Объявления:** create broadcast → `GET /api/v1/broadcasts` returns it.
- **Payments:** webhook log lists events with status + REDACTED emails; click DELIVERED event → payload modal; click Повторить (with reason) → status flips REPLAYED, retried_count increments, user tier unchanged (no double grant).
result: [pending]

### 2. ADMIN-03 advisory-lock race proof (Docker required)
expected: On a Docker host, `cd server/api && go test ./integration/ -run TestForceCancelWebhookRace -count=1` PASSES — proves `WithUserLock` serializes admin force-cancel against the lava webhook tier-grant across 20 concurrent interleavings with no hybrid state. (Implementation complete; skips cleanly without Docker.)
result: [pending]

### 3. ADMIN-06 webhook replay idempotency proof (Docker required)
expected: On a Docker host, `cd server/api && go test ./integration/ -run TestWebhookReplayIdempotent -count=1` PASSES — proves replay re-applies the stored event idempotently (single tier grant, expires_at stable, status → REPLAYED, retried_count == 1). (Implementation complete; skips cleanly without Docker.)
result: [pending]

## Summary

total: 3
passed: 0
issues: 0
pending: 3
skipped: 0
blocked: 0

## Gaps
