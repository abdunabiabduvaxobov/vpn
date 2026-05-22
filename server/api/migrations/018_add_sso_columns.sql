-- 018_add_sso_columns.sql
--
-- AUTH-03: add Apple + Google SSO identity columns to users.
--
-- Per ADR-007 §8.1 + .planning/phases/02-auth-sso-backend/02-CONTEXT.md D-09.
-- Phase 1 took migration number 017 (sessions.refresh_token_hash UNIQUE),
-- so this phase starts at 018 (D-08).
--
-- Destruction-free per D-32: no existing row has these fields set today;
-- the `auth_provider='guest'` default correctly classifies legacy rows.
-- Three partial unique indexes guarantee one row per provider sub and
-- exclude private-relay rows from the auto-link search space (D-09, D-03, D-04).

BEGIN;

ALTER TABLE users
    ADD COLUMN apple_user_id          VARCHAR(255),
    ADD COLUMN google_user_id         VARCHAR(255),
    ADD COLUMN email                  VARCHAR(320),
    ADD COLUMN email_verified         BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN email_is_private_relay BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN auth_provider          VARCHAR(20) NOT NULL DEFAULT 'guest';

-- Soft enum guardrail (CONTEXT.md Discretion: "auth_provider enum enforcement").
-- One extra line; prevents typos at the DB layer.
ALTER TABLE users
    ADD CONSTRAINT users_auth_provider_check
        CHECK (auth_provider IN ('guest', 'apple', 'google', 'admin'));

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_apple_user_id
    ON users(apple_user_id)  WHERE apple_user_id  IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_google_user_id
    ON users(google_user_id) WHERE google_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_users_email_verified
    ON users(email) WHERE email_verified = TRUE AND email_is_private_relay = FALSE;

COMMIT;
