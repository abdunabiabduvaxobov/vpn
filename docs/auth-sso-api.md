# RiseVPN Auth SSO API

**Phase:** 2 — Auth SSO backend
**Status:** Stable. Phases 4 (landing) and 5 (mobile) are coded against this contract.
**Source of truth:** This document. If implementation diverges from this doc, file an issue and fix the divergence.

Base URL: `https://api.risevpn.com/api/v1` (production), `http://localhost:8080/api/v1` (dev).

All requests/responses use `Content-Type: application/json`. Empty-body responses
(204) carry no body.

---

## Endpoints

### `POST /api/v1/auth/apple`

Sign in with Apple. Backend verifies the Apple `identityToken` against Apple's
JWKs and the configured Bundle ID / Service ID audience whitelist.

**Auth:** none required. **Optional** `Authorization: Bearer <guest_jwt>` header
signals a guest-promotion attempt.

**Request body:**

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `identityToken` | string | yes | Apple's identityToken JWT (RS256, signed by Apple) |
| `authorizationCode` | string | no | Accepted but NOT exchanged with Apple this phase (deferred per D-18) |
| `fullName` | string | no | First-sign-in only; Apple sends this once |
| `email` | string | no | First-sign-in only. **NEVER used as an auto-link key** — the auto-link lookup uses the verified JWT's `email` claim, not this body field. |
| `deviceId` | string | no | Mobile device id; binds the row to the device on success |
| `deviceSecret` | string | no | Per-device secret; reused from guest-login path |
| `platform` | string | no | `"ios"` \| `"web"` |
| `model` | string | no | Free-form device-model string for analytics |

**Success response — 200 OK:**

```json
{
  "data": {
    "access_token": "eyJhbGciOiJIUzI1NiI...",
    "refresh_token": "abcdef0123456789...",
    "expires_in": 300,
    "user": {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "auth_provider": "apple",
      "email": "user@example.com",
      "full_name": "Jane Doe",
      "subscription_tier": "free"
    }
  }
}
```

`access_token` and `refresh_token` shapes are **identical** to those returned by
`POST /api/v1/auth/guest` and `POST /api/v1/auth/admin-login` (AUTH-07 invariant).
HS256 signed; 5-minute access TTL, 30-day refresh TTL.

**Error responses:**

| Status | Body | When |
|--------|------|------|
| 400 | `{"error":"identityToken is required"}` | Missing or malformed `identityToken` |
| 401 | `{"error":"invalid identity token"}` | Apple verifier rejected the token (signature, audience, expiry, or issuer mismatch) |
| 403 | `{"error":"invalid guest token"}` | Optional `Authorization` header carried a malformed or expired guest JWT |
| 500 | `{"error":"internal server error"}` | DB write or JWT mint failed |

**Side effects:**

- If no row owns this Apple `sub` AND `Authorization: Bearer` carries a valid guest JWT → the guest user row is updated in place (D-06); `users.id` is preserved.
- If no row owns this Apple `sub` AND the verified JWT email matches an existing row with `email_verified=TRUE AND email_is_private_relay=FALSE` → the existing row gets `apple_user_id` set (D-03 auto-link).
- If the verified email is `@privaterelay.appleid.com` → **NEVER auto-links** (D-04); a new row is always created.
- If a row already owns this Apple `sub` AND the optional guest JWT points to a different row → the guest row is orphaned (D-06; let the stale-device scheduler clean it up).
- A new `sessions` row is inserted with the `refresh_token`'s SHA-256 hash.

---

### `POST /api/v1/auth/google`

Sign in with Google. Backend verifies the Google `idToken` via
`google.golang.org/api/idtoken` against the configured iOS / Android / Web OAuth
client IDs.

**Auth:** none required. **Optional** `Authorization: Bearer <guest_jwt>` for
guest-promotion (same semantics as `/auth/apple`).

**Request body:**

| Field | Type | Required | Notes |
|-------|------|----------|-------|
| `idToken` | string | yes | Google's idToken JWT (RS256, signed by Google) |
| `deviceId` | string | no | Same semantics as Apple |
| `deviceSecret` | string | no | Same |
| `platform` | string | no | `"ios"` \| `"android"` \| `"web"` |
| `model` | string | no | Same |

Google's idToken claims carry `email`, `email_verified`, and `hd` (hosted
domain) — no need to send these in the request body. The verifier rejects
any token with `email_verified=false` (D-17).

**Success response — 200 OK:** identical shape to `/auth/apple`, with
`user.auth_provider: "google"`.

**Error responses:** same matrix as `/auth/apple`. The Google-specific 401 case for unverified email is shown explicitly below for clarity:

| Status | Body | When |
|--------|------|------|
| 400 | `{"error":"idToken is required"}` | Missing or malformed `idToken` |
| 401 | `{"error":"invalid identity token"}` | Google verifier rejected the token (signature, audience, expiry, or `email_verified=false` per D-17 — rejected before reaching the auto-link path; T-2-EmailSpoof mitigation) |
| 403 | `{"error":"invalid guest token"}` | Optional `Authorization` header carried a malformed or expired guest JWT |
| 500 | `{"error":"internal server error"}` | DB write or JWT mint failed |

**Side effects:** same matrix as `/auth/apple` minus the private-relay exception
(Google has no private-relay concept; all Google `email_verified=true` claims
participate in auto-link).

---

### `POST /api/v1/auth/logout`

Log out. Deletes ALL refresh-session rows for the calling user (per the
"logout means logout everywhere" default — RESEARCH.md §Open Question #1
recommendation (a)) and blacklists the calling access token until its `exp`.

**Auth:** required (`Authorization: Bearer <access_jwt>`). Mounted under the
`protected` route group; the `AuthRequired` middleware (HOTFIX-02) validates
the JWT and sets `user_id` from the `sub` claim before the handler runs.

**Request body:** empty. (The CONTEXT.md D-23 alternative `{"refresh_token":"..."}` is **NOT** the current behavior — we delete ALL sessions for the user, not a single device's session.)

**Success response — 204 No Content** (no body).

**Error responses:**

| Status | Body | When |
|--------|------|------|
| 401 | `{"error":"token has been revoked"}` or middleware default | Token missing, malformed, expired, or already blacklisted (middleware rejects before reaching the handler) |
| 500 | `{"error":"internal server error"}` | Session-row deletion failed |

**Side effects:**

- Postgres: every row in `sessions` where `user_id = <caller_user_id>` is deleted. All of the user's devices are logged out — they MUST re-authenticate.
- Redis: a key `token:blacklist:<sha256_hex_of_access_token>` is set with TTL `min(exp - now, 5min)` (D-24 clamp). Subsequent protected requests with the same access token return 401 because `AuthRequired` middleware checks `cache.IsTokenBlacklisted` on every protected request.

**IMPORTANT — divergence from CONTEXT.md D-24:** CONTEXT.md D-24 specifies the
Redis key prefix as `jwt:blacklist:<...>`. The IN-TREE prefix
(`internal/cache/redis.go:35`) is `token:blacklist:`. We match the IN-TREE value
to avoid orphaning all existing blacklist entries and to keep the
`internal/middleware/auth.go:73-80` blacklist check working without surgery.
This divergence is documented in RESEARCH.md §TL;DR item 3 and is the correct
behavior for Phase 2.

**Refresh-token behavior after logout:** any subsequent `POST /api/v1/auth/refresh`
call with a refresh token that belonged to a now-deleted session returns 401 —
the session row no longer exists, so the refresh-token-hash lookup fails.

---

## Identity and account-linking rules

1. **Provider-id uniqueness:** at most one `users` row may have a given
   `apple_user_id`; same for `google_user_id`. Enforced by partial unique
   indexes from migration `018_add_sso_columns.sql`.
2. **Same sub → same row, every time:** signing in twice with the same Apple
   sub (from any client surface) returns the same `users.id`. AUTH-04 / ROADMAP
   SC#1.
3. **Auto-link by verified email:** when an Apple sign-in finds no existing
   row for the new Apple sub, but the verified JWT email matches an existing
   row with `email_verified=TRUE AND email_is_private_relay=FALSE`, that row
   gets the new provider attached instead of a new row being created. AUTH-06
   / ROADMAP SC#2.
4. **Private-relay exception:** emails ending in `@privaterelay.appleid.com`
   are stored with `email_is_private_relay=TRUE` and are NEVER used as
   auto-link keys. D-04.
5. **Guest promotion in place:** if the request carries a valid guest JWT in
   `Authorization: Bearer` AND no existing row owns the new provider sub, the
   guest user row is updated in place (one transaction): `apple_user_id` or
   `google_user_id` set, `email` set, `auth_provider` set, `users.id`
   preserved. Existing device rows remain bound. D-06 / AUTH-05.
6. **Guest with existing-owner conflict:** if the request carries a guest
   JWT AND a row already owns the new sub, the guest row's devices are
   reassigned to the existing owner (best-effort) and the guest row is left
   for the stale-device scheduler to clean up. D-06.

## Audience whitelist

Constructed once at server startup from environment variables
(D-30, D-34 — no per-request reconfiguration). Each env var below is required
at startup and contributes one allowed `aud` value to the verifier:

| Env var | Provider | Surface | Notes |
|---------|----------|---------|-------|
| `APPLE_BUNDLE_ID` | Apple | iOS app | e.g. `com.flawlssr.risevpn` |
| `APPLE_SERVICE_ID` | Apple | Web (risevpn.com) | Apple Service ID configured in Apple Developer |
| `GOOGLE_CLIENT_ID_IOS` | Google | iOS app | OAuth client ID for the iOS bundle |
| `GOOGLE_CLIENT_ID_ANDROID` | Google | Android app | OAuth client ID for the Android package |
| `GOOGLE_CLIENT_ID_WEB` | Google | Web (risevpn.com) | OAuth client ID for the web origin |

A token presenting any `aud` value outside this whitelist is rejected with 401.

## JWT shape (unchanged from existing endpoints — AUTH-07)

| Claim | Type | Notes |
|-------|------|-------|
| `sub` | string | UUID of `users.id` |
| `tier` | string | `"free"` \| `"pro"` (Phase 2 only emits `free`; Pro lands in Phase 3) |
| `role` | string | `"user"` \| `"admin"` |
| `name` | string | `user.full_name` snapshot at mint time |
| `iat` | number | Unix seconds at mint |
| `exp` | number | Unix seconds at expiry |

HS256, 5-minute access TTL. No `plan_id` claim this phase (Phase 3 adds it
once the dynamic-plans catalog exists).

---

*Phase: 02-auth-sso-backend*
*Document gathered: 2026-05-22*
*Source of truth: this document. Implementation: `server/api/internal/handler/auth.go::AppleSignIn,GoogleSignIn,Logout`.*
