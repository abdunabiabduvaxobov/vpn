-- 024: Phase 7 admin-panel-overhaul — per-user/server operational columns + feature_flags + broadcast_messages + webhook status
--
-- Single transactional migration (NO CONCURRENTLY — plain DDL is fine and simpler).
-- Adds:
--   * users.suspended_at / suspended_reason          (ADMIN-02 per-user suspend control)
--   * vpn_servers.is_draining / last_seen_at         (ADMIN-04 server drain / force-disconnect)
--   * lava_webhook_events.status (+ backfill)        (ADMIN-06 webhook log + replay)
--   * lava_webhook_events.retried_count              (ADMIN-06 replay counter)
--   * feature_flags table (+ 3 seeded flags)         (ADMIN-05/08 system controls + maintenance)
--   * broadcast_messages table (+ active index)      (ADMIN-05 broadcast)
--
-- LOCKED constraints (07-01-PLAN <interfaces> + Phase 7 architecture decisions):
--   * Do NOT create an admin_audit_log table — the existing audit_log (migration 014) is reused.
--   * Do NOT create a generic webhook_events table — the existing lava_webhook_events (migration 020) is reused.
--
-- Idempotency: ADD COLUMN IF NOT EXISTS + CREATE TABLE IF NOT EXISTS + ON CONFLICT DO NOTHING
-- make the file safe to re-run. The status backfill (T-07-01) is deterministic:
--   error IS NOT NULL          -> 'FAILED'
--   processed_at IS NOT NULL   -> 'DELIVERED'
--   otherwise (default)        -> 'PENDING'
--
-- This file is applied by migrations_test.go's generic lexicographic loop (it only
-- skips 019/020/021), so no test wiring change is needed.

BEGIN;

-- per-user suspend controls (ADMIN-02)
ALTER TABLE users ADD COLUMN IF NOT EXISTS suspended_at timestamptz;
ALTER TABLE users ADD COLUMN IF NOT EXISTS suspended_reason text;

-- per-server drain / liveness (ADMIN-04)
ALTER TABLE vpn_servers ADD COLUMN IF NOT EXISTS is_draining boolean NOT NULL DEFAULT false;
ALTER TABLE vpn_servers ADD COLUMN IF NOT EXISTS last_seen_at timestamptz;

-- webhook log status + replay counter (ADMIN-06)
ALTER TABLE lava_webhook_events ADD COLUMN IF NOT EXISTS status varchar(16) NOT NULL DEFAULT 'PENDING';
-- backfill historical rows so the webhook log renders correct status
UPDATE lava_webhook_events SET status = 'FAILED'    WHERE error IS NOT NULL;
UPDATE lava_webhook_events SET status = 'DELIVERED' WHERE error IS NULL AND processed_at IS NOT NULL;
ALTER TABLE lava_webhook_events ADD COLUMN IF NOT EXISTS retried_count integer NOT NULL DEFAULT 0;

-- operational feature flags (ADMIN-05/08, maintenance mode)
CREATE TABLE IF NOT EXISTS feature_flags (
    key         varchar(64) PRIMARY KEY,
    value       boolean NOT NULL DEFAULT false,
    updated_at  timestamptz NOT NULL DEFAULT now(),
    updated_by  uuid
);
-- seed the three flags Phase 7 toggles so reads never miss
INSERT INTO feature_flags (key, value) VALUES
    ('signups_off', false), ('payments_off', false), ('maintenance_mode', false)
    ON CONFLICT (key) DO NOTHING;

-- broadcast messages shown to clients (ADMIN-05)
CREATE TABLE IF NOT EXISTS broadcast_messages (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    title       varchar(200) NOT NULL,
    body        text NOT NULL,
    severity    varchar(16) NOT NULL DEFAULT 'info',
    locale      varchar(8),
    target_tier varchar(32),
    starts_at   timestamptz,
    ends_at     timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_broadcasts_active ON broadcast_messages (starts_at, ends_at);

COMMIT;
