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
--
-- Migration runner / rollback semantics (IN-03):
--
-- This file uses the implicit transactional-DDL property of Postgres: a
-- failure in any statement inside BEGIN; ... COMMIT; causes Postgres to
-- automatically ROLLBACK every prior statement in the transaction.
--
-- The project's migration runner (golang-migrate) wraps each migration file
-- in its own transaction by default and issues ROLLBACK on any non-zero exit
-- — see https://github.com/golang-migrate/migrate/blob/master/database/postgres/postgres.go.
-- A partial failure (e.g. the CHECK constraint addition below failing because
-- a row violates it) therefore leaves the schema fully rolled back to its
-- pre-migration state. No explicit ROLLBACK statement is required in this
-- file. If you switch to a runner that does NOT auto-rollback on error,
-- wrap this file in your runner's equivalent of `psql -1` (single transaction).
--
-- This migration is safe to re-run after a partial failure: every CREATE
-- INDEX uses IF NOT EXISTS, and the ALTER TABLE ADD COLUMN would fail
-- transactionally on the second run (no half-applied state to clean up).

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
