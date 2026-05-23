-- 020_lava_payments.sql
--
-- Phase 3 (PAY-03..05, PAY-09, PAY-11): lava.top payment provider schema.
--
-- Three tables (invoices, lava_contracts, lava_webhook_events), plus a
-- schema patch on the existing subscriptions table (drop stripe_id, add
-- lava_contract_id). The invoices table carries BOTH the lava-side
-- offer UUID (offer_id — forensics) AND the internal FKs (plan_id,
-- plan_offer_id — joins/reporting) per ADR §19.6.
--
-- Sources of truth:
--   - docs/ADR-007-lava-sso-rework.md §8.3 (base DDL) + §19.6 (plan_id/plan_offer_id additions)
--   - .planning/phases/03-lava-top-plans-catalog/03-CONTEXT.md D-10 (COALESCE timestamp/cancelledAt), D-11 (drop stripe_id)
--   - .planning/phases/03-lava-top-plans-catalog/03-RESEARCH.md §1.5 (subscription.cancelled has no timestamp), §4.2 (CREATE UNIQUE INDEX, not inline expression UNIQUE)

BEGIN;

-- 1. invoices — one row per /checkout call, status lifecycle pending->paid|failed|cancelled
CREATE TABLE invoices (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lava_invoice_id VARCHAR(64)  NOT NULL UNIQUE,
    offer_id        VARCHAR(64)  NOT NULL,            -- lava-side offer UUID (forensics)
    plan_id         UUID         REFERENCES plans(id),        -- ADR §19.6 amendment
    plan_offer_id   UUID         REFERENCES plan_offers(id),  -- ADR §19.6 amendment
    plan            VARCHAR(20)  NOT NULL,
    periodicity     VARCHAR(20)  NOT NULL,
    currency        VARCHAR(3)   NOT NULL,
    amount          NUMERIC(10,2) NOT NULL,
    status          VARCHAR(20)  NOT NULL,            -- pending|paid|failed|cancelled
    payment_url     TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_invoices_user_id ON invoices(user_id);
CREATE INDEX IF NOT EXISTS idx_invoices_status  ON invoices(status);
CREATE INDEX IF NOT EXISTS idx_invoices_plan_id ON invoices(plan_id);

-- 2. lava_contracts — one row per recurring contract (lava-side identity)
CREATE TABLE lava_contracts (
    id                  UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    contract_id         VARCHAR(64)  NOT NULL UNIQUE,   -- lava {contractId}
    parent_contract_id  VARCHAR(64),                    -- populated on subscription.recurring.* events
    offer_id            VARCHAR(64)  NOT NULL,
    plan                VARCHAR(20)  NOT NULL,
    periodicity         VARCHAR(20)  NOT NULL,
    currency            VARCHAR(3)   NOT NULL,
    is_active           BOOLEAN      NOT NULL DEFAULT TRUE,
    started_at          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    expires_at          TIMESTAMPTZ,
    cancelled_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lava_contracts_user_id ON lava_contracts(user_id);
CREATE INDEX IF NOT EXISTS idx_lava_contracts_parent ON lava_contracts(parent_contract_id);

-- 3. lava_webhook_events — idempotency log
CREATE TABLE lava_webhook_events (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type   VARCHAR(64)  NOT NULL,
    contract_id  VARCHAR(64),
    invoice_id   VARCHAR(64),
    payload      JSONB        NOT NULL,
    received_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,
    error        TEXT
);

-- Idempotency UNIQUE: subscription.cancelled events have no `timestamp` field
-- in lava's webhook payload (only `cancelledAt`) — so we COALESCE both per
-- RESEARCH §1.5 + §4.2. Both `->>` operators produce text; no cast needed
-- but CONTEXT.md D-10 keeps `::text` as documentation — harmless no-op.
CREATE UNIQUE INDEX IF NOT EXISTS idx_lava_webhook_events_natural_key
    ON lava_webhook_events (
        event_type,
        contract_id,
        COALESCE((payload->>'timestamp')::text, (payload->>'cancelledAt')::text)
    );

-- 4. Patch subscriptions: drop legacy Stripe column (D-11), add lava_contract_id.
--    The Go code referencing stripe_id is rewritten in companion tasks (T04, T06).
ALTER TABLE subscriptions DROP COLUMN IF EXISTS stripe_id;
ALTER TABLE subscriptions ADD COLUMN IF NOT EXISTS lava_contract_id VARCHAR(64);
CREATE INDEX IF NOT EXISTS idx_subscriptions_lava_contract_id
    ON subscriptions(lava_contract_id);

COMMIT;
