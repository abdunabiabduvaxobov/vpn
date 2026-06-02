-- 026: HARD-02 per-user VLESS identities (audit S4-2 / S5-1, D-06/D-07).
--
-- Today GetServerConfig hands every user the SAME shared TUNNEL_VLESS_UUID, so a
-- single leaked/revoked UUID admits anyone and lets a free-tier user enumerate
-- the whole fleet. This table gives every user a DISTINCT random UUIDv4 that the
-- tunnel admits, rotated on plan change and revocable per-user.
--
-- Columns:
--   * id          — surrogate PK.
--   * user_id     — owner; ON DELETE CASCADE so identities die with the user.
--   * vless_uuid  — the actual VLESS client UUID the tunnel admits. UNIQUE so two
--                   users can never collide onto the same wire identity (the whole
--                   point of per-user isolation).
--   * is_active   — TRUE for the currently-admitted identity. Rotation flips the
--                   old row to FALSE and inserts a fresh active one (D-07: the old
--                   UUID keeps working until the next tunnel reload — the natural
--                   ~30-60s window IS the grace period).
--   * created_at  — issue time.
--   * revoked_at  — set when is_active flips to FALSE (rotation / revoke-all).
--
-- Indexes (both PARTIAL on is_active = TRUE — the only rows the hot paths read):
--   * idx_uvi_user_active — GetOrCreateActiveVlessUUID's "this user's active row"
--                           lookup. PARTIAL UNIQUE so the "at most one ACTIVE
--                           identity per user" invariant is enforced by the DB,
--                           not merely by repo discipline (WR-03): a check-then-
--                           insert race between two concurrent first-fetches can
--                           otherwise create two is_active=TRUE rows. With the
--                           unique index the loser's INSERT fails on conflict and
--                           the repo re-reads the winner.
--   * idx_uvi_active      — ListActiveVlessUUIDs' "all active UUIDs" scan that the
--                           tunnel pulls each heartbeat.
--
-- Idempotency: CREATE TABLE / INDEX IF NOT EXISTS make the DDL safe to re-run.
-- Applied by the generic lexicographic migration loop and the production runner;
-- no test wiring change needed. NOTE: this migration has not shipped to any
-- environment yet (pre-launch), so the partial unique index is added in place
-- rather than as a follow-up migration.

BEGIN;

CREATE TABLE IF NOT EXISTS user_vless_identities (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    vless_uuid   UUID NOT NULL UNIQUE,
    is_active    BOOLEAN NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at   TIMESTAMPTZ
);

-- WR-03: PARTIAL UNIQUE on (user_id) WHERE is_active enforces one active identity
-- per user. RotateVlessUUID retires the old active row and inserts the new one in
-- the SAME transaction, so the two never coexist as is_active=TRUE — the unique
-- index is satisfied at every commit boundary.
CREATE UNIQUE INDEX IF NOT EXISTS idx_uvi_user_active ON user_vless_identities(user_id) WHERE is_active = TRUE;
CREATE INDEX IF NOT EXISTS idx_uvi_active ON user_vless_identities(is_active) WHERE is_active = TRUE;

COMMIT;
