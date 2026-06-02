-- 025: HARD-04 session device/IP binding + HARD-03 clean-break cutover.
--
-- Adds two columns to the sessions table, bound at refresh-token issue time:
--   * device_id  — the OS device identifier (ANDROID_ID / IDFV). HARD-checked
--                  on every /auth/refresh: a refresh presented with a different
--                  device_id than the one bound at issue is rejected 401 (D-10).
--                  Blocks a stolen refresh token from being replayed on another
--                  device (audit S1-7 / T-08-04).
--   * issue_ip   — the client IP captured at issue. SOFT-checked: an IP change
--                  at refresh is logged as a security event but the refresh is
--                  STILL allowed, because mobile clients legitimately roam
--                  networks (D-10 / T-08-04b). VARCHAR(45) is IPv6-safe.
--
-- idx_sessions_device_id backs the device-binding lookup path.
--
-- D-09 clean-break cutover (HARD-03 / T-08-03b): HARD-03 changes refresh tokens
-- from signed JWTs to opaque 32-byte random strings. Every pre-existing session
-- row holds a JWT-format refresh token that the new mint will never reproduce,
-- and none of those rows carry a device_id. DELETE FROM sessions invalidates
-- them all at deploy, forcing exactly one re-login across every device. This is
-- coordinated with the mobile Keychain move (plan 08-09). No paying users exist
-- (PROJECT.md), so a single forced re-login is acceptable.
--
-- Idempotency: ADD COLUMN IF NOT EXISTS + CREATE INDEX IF NOT EXISTS make the
-- DDL safe to re-run. The DELETE is a clean-break cutover and is intentionally
-- unconditional — re-running it on an already-migrated DB only clears whatever
-- sessions have accumulated since, which is harmless (users re-login once).
--
-- Applied by testutil/pg.go's generic lexicographic loop and the production
-- migration runner; no test wiring change is needed.

BEGIN;

ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS device_id VARCHAR(255),  -- bound at issue; hard-checked on refresh (D-10)
    ADD COLUMN IF NOT EXISTS issue_ip  VARCHAR(45);   -- bound at issue; soft-checked, log-only (D-10, IPv6-safe)

CREATE INDEX IF NOT EXISTS idx_sessions_device_id ON sessions(device_id);

-- D-09 clean-break cutover: all existing JWT-format refresh sessions become
-- invalid at deploy, forcing exactly one re-login (coordinated with the mobile
-- Keychain move, plan 08-09).
DELETE FROM sessions;

COMMIT;
