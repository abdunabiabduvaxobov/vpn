-- 021_lava_webhook_natural_key_no_null_hole.sql
--
-- Phase 3 CR-02 fix: tighten the lava_webhook_events idempotency UNIQUE index
-- so that NULL holes can no longer slip through.
--
-- Background:
--   Migration 020 created idx_lava_webhook_events_natural_key as
--       UNIQUE (event_type, contract_id,
--               COALESCE((payload->>'timestamp')::text,
--                        (payload->>'cancelledAt')::text))
--   When BOTH payload->>'timestamp' AND payload->>'cancelledAt' are absent
--   (which lava can do on certain retries or hand-crafted payloads), the
--   third column resolves to NULL. Postgres treats NULL <> NULL in unique
--   indexes by default, so two such rows are considered DISTINCT and BOTH
--   inserts succeed — idempotency is lost.
--
--   We add an explicit non-NULL sentinel ('no-timestamp') as the final
--   COALESCE branch so the third column is ALWAYS a non-NULL text value.
--   This means duplicate hostile payloads {"contractId":"X"} with no
--   timestamp now collide on the second insert — INSERT ... ON CONFLICT
--   DO NOTHING from InsertWebhookEventIfNew returns RowsAffected=0 and
--   the webhook handler short-circuits per PAY-04.
--
-- Migration is safe to run while data exists: dropping + re-creating
-- the UNIQUE index does not lose rows. Any pre-existing duplicates with
-- NULL coalesce values would now fail re-creation, but `lava_webhook_events`
-- is brand-new in 020 — no production data exists yet at the time this
-- ships, so the re-create cannot fail.

BEGIN;

DROP INDEX IF EXISTS idx_lava_webhook_events_natural_key;

CREATE UNIQUE INDEX idx_lava_webhook_events_natural_key
    ON lava_webhook_events (
        event_type,
        contract_id,
        COALESCE(
            (payload->>'timestamp')::text,
            (payload->>'cancelledAt')::text,
            'no-timestamp'
        )
    );

COMMIT;
