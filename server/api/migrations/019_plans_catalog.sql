-- 019_plans_catalog.sql
--
-- Phase 3 (PAY-01): dynamic plans replace the hard-coded PlanLimits Go map.
-- Three new tables (plans, plan_servers, plan_offers), a new FK (users.plan_id),
-- legacy tier coercion (premium/ultimate -> pro — destruction-free per CONTEXT D-08,
-- zero paying users today), and seed rows so the existing 'free'/'pro' strings
-- still resolve via plans.code lookup.
--
-- Sources of truth:
--   - docs/ADR-007-lava-sso-rework.md §19.3 (DDL)
--   - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-07 (filename 019),
--     D-08 (Pro max_devices=3 not 5), D-09 (6 placeholder Pro offers).
--
-- Idempotency: wrapped in BEGIN/COMMIT; golang-migrate auto-rollbacks on failure.
-- All CREATE INDEX statements use IF NOT EXISTS so a partial re-run is safe.

BEGIN;

-- 1. plans table
CREATE TABLE plans (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code               VARCHAR(40)  NOT NULL UNIQUE,
    name               VARCHAR(100) NOT NULL,
    description        TEXT         NOT NULL DEFAULT '',
    max_devices        INT          NOT NULL,
    max_servers        INT          NOT NULL,
    speed_limit_mbps   INT          NOT NULL DEFAULT 0,
    is_active          BOOLEAN      NOT NULL DEFAULT TRUE,
    is_system          BOOLEAN      NOT NULL DEFAULT FALSE,
    sort_order         INT          NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Soft enum guardrail on code (regex-like CHECK).
ALTER TABLE plans
    ADD CONSTRAINT plans_code_format_check
        CHECK (code ~ '^[a-z0-9][a-z0-9_-]*$');

-- Enforce exactly-one-system-plan invariant.
CREATE UNIQUE INDEX IF NOT EXISTS idx_plans_one_system
    ON plans(is_system) WHERE is_system = TRUE;
CREATE INDEX IF NOT EXISTS idx_plans_active_sort
    ON plans(is_active, sort_order) WHERE is_active = TRUE;

-- 2. plan_servers M:N table
CREATE TABLE plan_servers (
    plan_id    UUID NOT NULL REFERENCES plans(id)        ON DELETE CASCADE,
    server_id  UUID NOT NULL REFERENCES vpn_servers(id)  ON DELETE CASCADE,
    PRIMARY KEY (plan_id, server_id)
);
CREATE INDEX IF NOT EXISTS idx_plan_servers_server ON plan_servers(server_id);

-- 3. plan_offers table
CREATE TABLE plan_offers (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    plan_id        UUID         NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    periodicity    VARCHAR(20)  NOT NULL,
    currency       VARCHAR(3)   NOT NULL,
    amount         NUMERIC(10,2) NOT NULL CHECK (amount >= 0),
    lava_offer_id  VARCHAR(64),
    is_active      BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Whitelist periodicity + currency at the DB level (defence in depth — handlers
-- ALSO validate, per RESEARCH §"Security Domain").
ALTER TABLE plan_offers
    ADD CONSTRAINT plan_offers_periodicity_check
        CHECK (periodicity IN ('ONE_TIME','MONTHLY','PERIOD_90_DAYS','PERIOD_180_DAYS','PERIOD_YEAR'));
ALTER TABLE plan_offers
    ADD CONSTRAINT plan_offers_currency_check
        CHECK (currency IN ('USD','EUR','RUB'));

-- One ACTIVE offer per (plan, periodicity, currency); grandfathered (is_active=false)
-- copies stay around for renewal webhooks (ADR §19.10).
CREATE UNIQUE INDEX IF NOT EXISTS idx_plan_offers_unique_active
    ON plan_offers(plan_id, periodicity, currency) WHERE is_active = TRUE;
CREATE UNIQUE INDEX IF NOT EXISTS idx_plan_offers_lava_offer_id
    ON plan_offers(lava_offer_id) WHERE lava_offer_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_plan_offers_plan_active
    ON plan_offers(plan_id) WHERE is_active = TRUE;

-- 4. Seed plans. D-08: Pro max_devices=3 (NOT 5 — overrides ADR §19.3 default).
INSERT INTO plans (code, name, description, max_devices, max_servers, speed_limit_mbps, is_system, sort_order)
VALUES
    ('free', 'Free', '', 1, 3, 50, TRUE, 0),
    ('pro',  'Pro',  '', 3, -1, 0, FALSE, 10);

-- 5. Coerce legacy premium/ultimate -> pro (D-08, destruction-free, zero paying users today).
UPDATE users         SET subscription_tier = 'pro' WHERE subscription_tier IN ('premium', 'ultimate');
UPDATE subscriptions SET plan              = 'pro' WHERE plan              IN ('premium', 'ultimate');

-- 6. Add users.plan_id FK, backfill, then NOT NULL.
ALTER TABLE users ADD COLUMN plan_id UUID REFERENCES plans(id) ON DELETE SET NULL;
UPDATE users u
   SET plan_id = p.id
  FROM plans p
 WHERE p.code = u.subscription_tier;
-- Defensive: any orphan row gets the system plan.
UPDATE users
   SET plan_id = (SELECT id FROM plans WHERE is_system = TRUE)
 WHERE plan_id IS NULL;
ALTER TABLE users ALTER COLUMN plan_id SET NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_plan_id ON users(plan_id);

-- 7. Seed plan_servers — Free gets 3 lowest-load active servers (matches the old
--    PlanLimits["free"].MaxServers=3 slicing); Pro gets every active server.
INSERT INTO plan_servers (plan_id, server_id)
SELECT (SELECT id FROM plans WHERE code = 'free'), v.id
  FROM vpn_servers v
 WHERE v.is_active = TRUE
 ORDER BY v.current_load ASC
 LIMIT 3;

INSERT INTO plan_servers (plan_id, server_id)
SELECT (SELECT id FROM plans WHERE code = 'pro'), v.id
  FROM vpn_servers v
 WHERE v.is_active = TRUE;

-- 8. D-09: seed 6 PLACEHOLDER plan_offers rows for Pro
--    ({MONTHLY, PERIOD_YEAR} × {USD, EUR, RUB}) with lava_offer_id=NULL.
--    Admin opens each in the UI dropdown picker post-deploy and selects matching
--    lava offer. /checkout returns 409 offer_not_configured until lava_offer_id is set.
INSERT INTO plan_offers (plan_id, periodicity, currency, amount, lava_offer_id)
SELECT id, 'MONTHLY',     'USD', 5.00,  NULL FROM plans WHERE code='pro' UNION ALL
SELECT id, 'PERIOD_YEAR', 'USD', 49.99, NULL FROM plans WHERE code='pro' UNION ALL
SELECT id, 'MONTHLY',     'EUR', 5.00,  NULL FROM plans WHERE code='pro' UNION ALL
SELECT id, 'PERIOD_YEAR', 'EUR', 49.99, NULL FROM plans WHERE code='pro' UNION ALL
SELECT id, 'MONTHLY',     'RUB', 499.00,NULL FROM plans WHERE code='pro' UNION ALL
SELECT id, 'PERIOD_YEAR', 'RUB', 4990.0,NULL FROM plans WHERE code='pro';

COMMIT;
