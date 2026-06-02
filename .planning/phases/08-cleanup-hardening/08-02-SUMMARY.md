---
phase: 08-cleanup-hardening
plan: 02
subsystem: backend-security
tags: [hardening, audit-fixes, telegram-bot, admin-search, audit-log, health-endpoint]
requires:
  - 08-01 (Wave 0 RED tests — recovery_private_test.go, health_test.go, admin_search_test.go, admin_audit_diff_test.go)
provides:
  - private-chat-gate (bot dispatch refuses non-private chats)
  - prefix-match-admin-search (indexed, len>=3 gated)
  - role-change-audit-diff (before->after in audit_log.details)
  - health-without-go-version
affects:
  - server/api/internal/bot/recovery.go
  - server/api/internal/handler/health.go
  - server/api/internal/handler/admin.go
  - server/api/internal/repository/admin_repo.go
  - server/api/internal/middleware/audit.go
  - server/api/migrations/027_admin_search_index.sql
tech-stack:
  added: []
  patterns:
    - "handler computes before->after diff and stashes via c.Locals; AuditLog middleware merges into details JSONB (no re-query)"
    - "anchored-prefix ILIKE 'x%' on indexed column instead of unbounded ILIKE '%x%'"
key-files:
  created:
    - server/api/migrations/027_admin_search_index.sql
  modified:
    - server/api/internal/bot/recovery.go
    - server/api/internal/handler/health.go
    - server/api/internal/handler/admin.go
    - server/api/internal/repository/admin_repo.go
    - server/api/internal/middleware/audit.go
decisions:
  - "Search index numbered 027 (025 reserved for HARD-04 sessions in 08-04, 026 for HARD-02 vless in 08-07)"
  - "Index on lower(full_name) text_pattern_ops so case-insensitive ILIKE 'x%' prefix is index-eligible"
  - "before-state load moved AFTER request-shape validation so invalid/no-field requests still 400 without a DB read (regression fix)"
requirements: [HARD-05, HARD-06, HARD-07, HARD-17]
metrics:
  tasks: 2
  files-created: 1
  files-modified: 5
  commits: 4
  duration: ~30m
  completed: 2026-06-02
---

# Phase 8 Plan 02: Audit-Finding Hardening (bot gate, admin search, audit diff, health leak) Summary

Closed four independent audit findings: a Telegram private-chat gate (HARD-05/S1-8), prefix-only indexed admin user-search with a len>=3 reject (HARD-06/S2-3), a before->after role/tier audit diff merged into the audit_log details JSONB (HARD-07/S2-4,S9-4), and removal of the Go runtime version from the unauthenticated /health response (HARD-17/S9-2).

## What Was Built

### Task 1 — Telegram private-chat gate (HARD-05) + /health version removal (HARD-17) — commit 58db83f

- `bot/recovery.go handleUpdate`: after the existing `msg == nil || msg.From == nil` nil-guard and before the command dispatch, added `if msg.Chat == nil || msg.Chat.Type != "private" { return }`. The bot now silently ignores group/supergroup/channel updates, so `/start`, `/help`, and `/status` — all of which echo to `msg.Chat.ID` — can no longer leak account state into a shared chat (S1-8).
- `handler/health.go Health()`: removed `"go_version": runtime.Version()` from the `/health` JSON body and dropped the now-unused `runtime` import (verified `runtime` was referenced nowhere else in the file). Status/uptime/timestamp are preserved (S9-2).

### Task 2 — Admin search hardening (HARD-06) + role-change audit diff (HARD-07) — commits 8e8b005, b2a7d6d

- `repository/admin_repo.go ListUsers`: replaced the unbounded `CAST(id AS TEXT) ILIKE '%x%' OR email_hash ILIKE '%x%' OR full_name ILIKE '%x%'` full-table scan with an **anchored prefix** `full_name ILIKE 'search%'` (no leading `%`) on the indexed column, plus an exact `email_hash = sha256hex(search)` equality when the input parses as a full email (new `looksLikeEmail` helper mirroring the auth-login validation). The cast-id-to-text branch was dropped entirely (a text cast over id can never use an index). T-08-06 DoS mitigation.
- `handler/admin.go AdminListUsers`: trims the search and rejects a non-empty search shorter than 3 chars with HTTP 400 `{"error":"search must be at least 3 characters"}`. An empty search (no filter) still returns the full page.
- `handler/admin.go AdminUpdateUser`: loads the current row (`before`) **after** request-shape validation, then after a successful update computes a changed-field diff for `role` and `subscription_tier` (only fields that actually changed) as `{ "role": {"before": old, "after": new} }` and stashes it via `c.Locals("audit_details", diff)`. The pre-existing `extend_days` branch was rewired to reuse this single `before` load instead of a second query.
- `middleware/audit.go AuditLog`: merges any `c.Locals("audit_details")` map into the `details` JSONB it already writes (accepts both `map[string]any` and `map[string]map[string]any`), with **no re-query** inside the middleware. The audit row for a role change now carries e.g. `{"role":{"before":"user","after":"admin"}}` (S2-4/S9-4).
- `migrations/027_admin_search_index.sql`: `CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_users_full_name ON users (lower(full_name) text_pattern_ops)` — `lower(...) text_pattern_ops` so the case-insensitive anchored prefix is index-eligible. Numbered 027 because 025 (sessions, 08-04) and 026 (vless, 08-07) are reserved.

## Verification

- `go build ./...` — clean.
- `go vet ./internal/handler/... ./internal/repository/... ./internal/middleware/... ./internal/bot/...` — clean.
- `go test ./internal/handler/... ./internal/repository/... ./internal/middleware/...` — all PASS (after regression fix below).
- Acceptance greps: `go_version` in health.go → 0; `runtime.Version` in health.go → 0; `Chat.Type` gate present before command dispatch; `CAST(id AS TEXT)` in admin_repo.go → 0; no leading-`%` ILIKE (only `prefix := search + "%"`); `search must be at least 3 characters` present; `c.Locals("audit_details", diff)` present and merged in AuditLog.

**Wave 0 RED tests not present in this worktree:** `recovery_private_test.go`, `health_test.go`, `admin_search_test.go`, `admin_audit_diff_test.go` are produced by a Wave 0 plan whose output was not merged into this parallel worktree (the worktree base 2122b84 predates them). The implementation was written directly against the contracts those tests specify in the plan; it will turn them GREEN when the wave is merged. Existing `admin_test.go` / `health_endpoints_test.go` suites pass.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] AdminUpdateUser before-state load broke two existing 400 validation tests**
- **Found during:** Task 2 (running the existing handler suite after wiring the audit diff).
- **Issue:** My first cut loaded the `before` user row immediately after body-parse, before role/no-field validation. `TestAdminUpdateUser_InvalidRole_Returns400` and `TestAdminUpdateUser_NoFields_Returns400` pass a `nil` DB and expect 400 (they validate request shape before any DB touch) — the early load returned 500 on the nil DB instead.
- **Fix:** Moved the `before` load to AFTER request-shape validation (after the role-validity check and the no-updatable-fields guard). `extend_days` (the only field needing the current row) was deferred via an `applyExtendDays` flag and resolved after the load. Bad/empty requests now 400 without a DB read.
- **Files modified:** server/api/internal/handler/admin.go
- **Commit:** 8e8b005

## Deferred Issues

**DEF-08-02-A — Pre-existing `TestMigrations019_020` ordering bug** (logged in `deferred-items.md`, commit 11340e0)
- `migrations/migrations_test.go TestMigrations019_020` fails applying `024_admin_panel_overhaul.sql` with `relation "lava_webhook_events" does not exist`. The test's apply loop skips 019/020/021 (applying them after the loop) but 024 depends on `lava_webhook_events` created by 020, so 024 runs before 020. This is a test-harness ordering defect in a file 08-02 did not touch; the new migration 027 sorts after 024 and is never reached. Out of scope per SCOPE BOUNDARY — deferred to a migration-hardening plan.

## Known Stubs

None. All four findings are fully implemented and wired; no placeholder/empty-value stubs introduced.

## Threat Flags

None. The changes shrink existing trust-boundary surface (bot dispatch, admin query, health response) rather than introducing new endpoints, auth paths, or schema at a trust boundary. Migration 027 adds an index only.

## Self-Check: PASSED

- FOUND: server/api/migrations/027_admin_search_index.sql
- FOUND: server/api/internal/bot/recovery.go (Chat.Type gate)
- FOUND: server/api/internal/handler/health.go (no go_version)
- FOUND: server/api/internal/handler/admin.go (len>=3 gate + audit_details diff)
- FOUND: server/api/internal/repository/admin_repo.go (prefix ILIKE, looksLikeEmail)
- FOUND: server/api/internal/middleware/audit.go (audit_details merge)
- FOUND commit: 58db83f (Task 1)
- FOUND commit: 8e8b005 (Task 2)
- FOUND commit: 11340e0 (deferred-items)
- FOUND commit: b2a7d6d (comment reword)
