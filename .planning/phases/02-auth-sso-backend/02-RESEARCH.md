# Phase 2: Auth SSO Backend — Research

**Status:** ## RESEARCH COMPLETE
**Date:** 2026-05-22
**Domain:** Go / Fiber backend — Apple + Google SSO verification, JWT lifecycle, account-linking
**Confidence:** HIGH on stack & integration points (verified by reading repo + Context7 pulls); MEDIUM on JWKs stale-cache semantics (library does not document explicitly).

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (D-01 through D-37 — verbatim guidance for the planner)

**Identity model (ADR §5)**
- **D-01:** Two separate columns `apple_user_id VARCHAR(255)` and `google_user_id VARCHAR(255)` (not a polymorphic `identities` table). Each carries a `WHERE col IS NOT NULL` partial unique index.
- **D-02:** Both columns can be populated on the **same** `users` row (account-linked user). Re-binding never happens silently — the only paths that set them are the SSO handlers themselves.
- **D-03:** Account-linking rule: auto-link by **verified email**. When `email_verified=TRUE` AND the matching row's `email_is_private_relay=FALSE` AND the new sign-in email is not `@privaterelay.appleid.com`, attach the new provider's `*_user_id` to the existing row.
- **D-04:** Private-relay exception: emails ending in `@privaterelay.appleid.com` are stored as-is with `email_is_private_relay=TRUE` and **never** used for auto-link lookup.
- **D-05:** Email zero-knowledge tradeoff: for Apple/Google users we store **cleartext** email in `users.email`. Admin login path continues to use `email_hash`. Guest users have neither.
- **D-06:** Guest → identified promotion: if a guest JWT is in `Authorization: Bearer` AND no other row owns this Apple/Google `sub`, **promote the guest row in place** — keep `users.id` stable. If a row already owns this sub, reassign the guest's device rows and orphan the guest user.
- **D-07:** `auth_provider` column is a soft enum: `'guest' | 'apple' | 'google' | 'admin'`. Last-used provider wins. Informational only — handlers do not branch on it.

**Schema (ADR §8.1)**
- **D-08:** Migration filename is **`018_add_sso_columns.sql`** (017 is taken by HOTFIX-07). Only migration this phase produces.
- **D-09:** Column set (verbatim ALTER TABLE — see CONTEXT.md).
- **D-10:** No `users.plan_id`, no `plans`/`plan_servers`/`plan_offers` tables — Phase 3.
- **D-11:** GORM model additions (six fields — see CONTEXT.md).

**Verifier packages (ADR §4, §7)**
- **D-12:** Apple verifier at `server/api/internal/auth/apple/{verifier.go,verifier_test.go}`. API: `Verify(ctx, identityToken, opts) (AppleIdentity, error)`. Struct: `{Sub, Email, EmailVerified, IsPrivateRelay}`. Pure package — no DB, no Fiber, no globals.
- **D-13:** Google verifier at `server/api/internal/auth/google/{verifier.go,verifier_test.go}`. API: `Verify(ctx, idToken, opts) (GoogleIdentity, error)`. Struct: `{Sub, Email, EmailVerified, HostedDomain}`. Same purity constraint.
- **D-14:** Apple JWKs library: `github.com/MicahParks/keyfunc/v3` + existing `github.com/golang-jwt/jwt/v5`. Library cache TTL: 24h with stale-while-revalidate.
- **D-15:** Google ID-token library: `google.golang.org/api/idtoken`. Call `idtoken.Validate(ctx, idToken, audience)` once per allowed audience; accept first success.
- **D-16:** Apple `iss` must equal `https://appleid.apple.com`. Apple `aud ∈ {APPLE_BUNDLE_ID, APPLE_SERVICE_ID}`. Google `aud ∈ {GOOGLE_CLIENT_ID_IOS, GOOGLE_CLIENT_ID_ANDROID, GOOGLE_CLIENT_ID_WEB}`. Mismatch → 401.
- **D-17:** Reject Google `email_verified=false`. Apple may report `email_verified=true` even for relay — accept identity, flag `email_is_private_relay=true`.
- **D-18:** Authorization-code exchange with Apple is **deferred** this phase. Accept the optional field, do not exchange.

**Handlers (ADR §10)**
- **D-19:** Extend `internal/handler/auth.go` with `AppleSignIn`, `GoogleSignIn`, `Logout`. Composition: parse → verifier → repo lookup/upsert → `generateTokens`.
- **D-20 / D-22:** Request shapes (see CONTEXT.md). Optional `Authorization: Bearer <guest_jwt>` for promotion.
- **D-21:** Response shape — identical to existing `data.{access_token, refresh_token, expires_in, user{…}}`.
- **D-23:** `POST /api/v1/auth/logout` — auth required, empty body (planner picks shape), deletes the matching `sessions` row, blacklists the access token. Response 204.
- **D-24:** Blacklist: Redis SET, key `jwt:blacklist:<jti-or-hash>`, TTL = `exp - now()` clamped to access-token-lifetime (5 min).
- **D-25:** JWT mint stays HS256 with existing claim set (`sub`, `tier`, `role`, `name`, `iat`, `exp`). No `plan_id` claim. `jti` MAY be added if blacklist needs it; token-hash fallback acceptable.
- **D-26:** Route registration: `api.Post("/auth/apple", …)`, `api.Post("/auth/google", …)`, `protected.Post("/auth/logout", …)`.
- **D-27:** Errors — 400 malformed, 401 sig/aud/exp/iss/email_verified=false, 403 invalid guest JWT for promotion, 500 JWKs+cache both fail.

**Repository functions (ADR §14 Phase 1)**
- **D-28:** New functions in `internal/repository/user_repo.go` (extend existing file):
  - `FindUserByAppleID(db, sub string) (*User, error)`
  - `FindUserByGoogleID(db, sub string) (*User, error)`
  - `FindUserByVerifiedEmailForLink(db, email string) (*User, error)` — returns auto-link candidate only when `email_verified=TRUE AND email_is_private_relay=FALSE`.
  - `PromoteGuestToSSO(db, guestUserID, sub, email string, provider AuthProvider) error` — in-place UPDATE.
  - `BindDeviceToUser(db, deviceID, deviceSecret, userID string) error` — reused or thin wrapper.
- **D-29:** Single GORM call per function unless writing multiple tables. Guest-promotion is the one multi-write path and **MUST** be transactional.

**Config additions (ADR §14 Phase 1)**
- **D-30:** New env vars registered with HOTFIX-08 validator:
  - `APPLE_TEAM_ID`, `APPLE_BUNDLE_ID`, `APPLE_SERVICE_ID` — required at startup.
  - `APPLE_KEY_ID`, `APPLE_PRIVATE_KEY_P8` — optional this phase (default: warn-not-fail).
  - `GOOGLE_CLIENT_ID_IOS`, `GOOGLE_CLIENT_ID_ANDROID`, `GOOGLE_CLIENT_ID_WEB` — required at startup.
- **D-31:** No `LAVA_*` env vars in Phase 2.

**Operational constraints**
- **D-32:** Migration is destruction-free (no paying users; `auth_provider='guest'` default correctly classifies existing rows).
- **D-33:** API contracts MUST be stable — committed contract doc in this phase folder OR `docs/auth-sso-api.md`.
- **D-34:** Audience whitelist constructed **once at startup** in verifier constructor. Injected via DI — no globals, no `init()`.
- **D-35:** Test gate: minimum one happy-path + one audience-mismatch + one expired-token test per verifier. Handler tests cover guest-promote-in-place, account-link-by-email, private-relay-skip-link, cross-surface-same-sub-same-id, logout deletes session + blacklists token, refresh after logout 401, second login on different device for same sub returns same `users.id`.
- **D-36:** Every code change through GSD execution.
- **D-37:** Atomic commits per logical unit (one commit per migration, per verifier package, per handler set, per repo function set).

### Claude's Discretion (planner picks; defaults below)
- **JWT `jti` vs token-hash blacklist key.** Default: **token-hash** (existing `cache.IsTokenBlacklisted(tokenHash)` is already wired in the middleware — see §Integration with Phase 1 Surface; switching to `jti` would require a claim-shape change + middleware rewrite for zero gain this phase).
- **Logout request body.** Default: **empty body**. Server reads access token from `Authorization` header, decodes claims (no signature re-verify — the middleware already did that), resolves the user's sessions and deletes only the session matching the user. Document the choice.
- **`auth_provider` CHECK constraint.** Optional — recommended **add** it: `CHECK (auth_provider IN ('guest','apple','google','admin'))` is one extra line in migration 018, prevents typos at the DB layer.
- **Tests organization.** Default: **extend existing `auth_test.go`** (currently 449 lines — well under the 1500-line split threshold).
- **Apple `authorizationCode` storage.** Default: **discard** — GDPR-clean, no forensic value while we don't exchange it.
- **Threat-model coverage in PLAN.md** is REQUIRED per `security_enforcement` (config absent = enabled). At minimum: token replay, audience confusion, email-spoofing for auto-link, race condition in guest-promotion, blacklist bypass via clock skew, JWKs MITM.

### Deferred Ideas (OUT OF SCOPE — do not research)
- Apple `authorizationCode` exchange with `appleid.apple.com/auth/token`.
- Landing SSO UI (`/login`, `/dashboard`, HttpOnly cookies).
- Mobile SSO UI (RN native deps, `LoginScreen.tsx`).
- lava.top integration (`/checkout`, `/webhook/lava`).
- Dynamic plans catalog (`plans`, `plan_offers`, `users.plan_id`).
- JWT `plan_id` claim.
- Privacy-policy update (operational).
- App Store external-link entitlement (operational).
- Merge-accounts flow (Phase 6).
- Stripe deletion (Phase 8).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| AUTH-01 | Sign in with Apple from any client; signature verified via Apple JWKs; `aud` matches bundle id OR service id | §Apple Verifier, §Race & Failure Modes (audience confusion) |
| AUTH-02 | Sign in with Google from any client; verified via `google.golang.org/api/idtoken`; `aud` matches iOS / Android / Web client IDs | §Google Verifier |
| AUTH-03 | New `users.apple_user_id`, `users.google_user_id`, `users.email`, `users.email_verified`, `users.email_is_private_relay`, `users.auth_provider` columns + partial-unique indexes | §Migration 018, §GORM model additions |
| AUTH-04 | Same Apple/Google `sub` returns same `users.id` across mobile, website, admin | §Account-link race, §Repository functions |
| AUTH-05 | Guest user (device-based) who later signs in with Apple/Google is promoted **in-place** when no other row owns that sub | §Guest-promote transactional, §PromoteGuestToSSO |
| AUTH-06 | Apple+Google with same verified email auto-link to one row, EXCEPT `@privaterelay.appleid.com` (relay rejected) | §FindUserByVerifiedEmailForLink, §Account-link race |
| AUTH-07 | Backend mints HS256 access (5 min) + refresh (30 day) identical to today's after SSO | §generateTokens reuse — no JWT shape change |
| AUTH-08 | `POST /api/v1/auth/logout` returns 204, deletes refresh-session row, blacklists access token until `exp` | §Logout integration with existing blacklist |
</phase_requirements>

---

## TL;DR for the planner

1. **Use `MicahParks/keyfunc/v3.8.0`** (verified current Feb 2026) — `NewDefaultCtx(ctx, []string{"https://appleid.apple.com/auth/keys"})` returns a `Keyfunc` whose `.Keyfunc` method satisfies `jwt.ParseWithClaims`'s callback signature. Background goroutine refreshes JWKs; refresh interval lives on `Override`.
2. **Use `google.golang.org/api/idtoken`** with `idtoken.Validate(ctx, token, audience)` — loop the three audiences (iOS, Android, Web) and accept the first non-error return. Library auto-fetches Google's JWKs and validates `aud` inline. `Payload.Subject` is sub; `email` + `email_verified` + `hd` live in `Payload.Claims` (typed `map[string]interface{}` — must type-assert).
3. **Blacklist infrastructure already exists.** `internal/cache/redis.go` has `BlacklistToken(ctx, client, tokenHash, ttl)` and `IsTokenBlacklisted(ctx, client, tokenHash)`. The auth middleware at `internal/middleware/auth.go:73-80` already checks the blacklist on every protected request. Logout is **one Redis SET** away from working. **Discretion decision: use the existing token-hash key** (not jti) — switching adds zero value and would require middleware surgery.
4. **The Phase 1 env validator is a literal slice append.** `config.RequireEnv()` (config.go:151) is a `[]string` of literal keys. Adding the six new required keys (`APPLE_TEAM_ID`, `APPLE_BUNDLE_ID`, `APPLE_SERVICE_ID`, `GOOGLE_CLIENT_ID_IOS/_ANDROID/_WEB`) is appending six strings. The two optional Apple `.p8` keys go into `OptionalEnvWarnings()` with the same shape as the existing Stripe warnings.
5. **`auth_test.go` uses SQLite in-memory** via `gorm.io/driver/sqlite` — `:memory:`. Phase 2 tests extend the existing `newAuthTestDB` helper to add the six new columns; the SQLite `CREATE TABLE` shape is in `auth_test.go:44-99`. No real Apple/Google calls needed — verifier interface is mocked via dependency injection (see §Testing Strategy).
6. **Use an `idtoken.Validator` interface, not the concrete library, in the handler signature.** Tests inject a fake; production wires the real one. Same pattern for Apple — inject a `keyfunc.Keyfunc` interface (or a project-defined `JWKSource`) so verifier tests sign their own tokens with an ed25519 / RSA test key.
7. **Account-link race fix: `INSERT … ON CONFLICT DO NOTHING RETURNING id` via GORM** — `db.Clauses(clause.OnConflict{DoNothing: true})` on `apple_user_id` or `google_user_id`. If two parallel sign-ins arrive for the same Apple `sub`, the partial unique index makes the second one a no-op; both end up reading the same row.
8. **JWKs cold-start failure mode: boot anyway, 503 SSO until cache fills.** `keyfunc.NewDefaultCtx` does NOT block on the first fetch — it launches a refresh goroutine. First sign-in request after boot will trigger an on-demand fetch (the library refreshes when an unknown `kid` is encountered). If Apple/Google JWKs are unreachable at that moment, return 500 with a request_id (the existing `ErrorHandler` already does that — HOTFIX-04). Do NOT block server startup on JWKs reachability — that risks total outage during a hiccup mid-deploy.
9. **Reuse `generateTokens` and `storeRefreshSession` from existing `auth.go` verbatim** — D-25 locks the JWT shape. The SSO handlers compose: `verify → repo lookup/upsert → generateTokens → storeRefreshSession → return`. Mirror the `GuestLogin` and `AdminLogin` structure exactly, including the "fail the login if session insert fails" branch.
10. **Migration `018_add_sso_columns.sql` follows the same idempotent pattern as `017`** — `IF NOT EXISTS` on indexes, `ADD COLUMN IF NOT EXISTS` is **not** supported pre-PG9.6 syntax-wise; the project uses PG16 and the existing migrations use plain `ADD COLUMN`. Match the existing style (see migration 015, 016 for column additions). NO `CREATE INDEX CONCURRENTLY` needed — partial indexes on a freshly-added nullable column have zero rows to scan.

---

## Technical Approach

### Apple Verifier (`internal/auth/apple/verifier.go`)

**Library:** `github.com/MicahParks/keyfunc/v3` (latest stable v3.8.0, Feb 2026) [VERIFIED: pkg.go.dev]

**Constructor (called once at startup):**
```go
package apple

import (
    "context"
    "errors"
    "github.com/MicahParks/keyfunc/v3"
    "github.com/golang-jwt/jwt/v5"
)

type AppleIdentity struct {
    Sub            string
    Email          string
    EmailVerified  bool
    IsPrivateRelay bool
}

// Options is the audience whitelist plus the JWKs source. Wired at startup.
type Options struct {
    AllowedAudiences []string  // {BUNDLE_ID, SERVICE_ID}
    AllowedIssuer    string    // "https://appleid.apple.com" — caller injects
}

type Verifier struct {
    kf   keyfunc.Keyfunc        // injected; in tests, a stub
    opts Options
}

// New constructs a Verifier that lazily fetches Apple's JWKs.
// Per the Context7 doc, NewDefaultCtx does NOT block on first fetch —
// the refresh goroutine runs in the background; the JWKs cache is
// populated on first ParseWithClaims call (and on every "unknown kid"
// thereafter).
func New(ctx context.Context, opts Options) (*Verifier, error) {
    kf, err := keyfunc.NewDefaultCtx(ctx, []string{"https://appleid.apple.com/auth/keys"})
    if err != nil { return nil, err }
    return &Verifier{kf: kf, opts: opts}, nil
}

func (v *Verifier) Verify(ctx context.Context, identityToken string) (AppleIdentity, error) {
    claims := jwt.MapClaims{}
    _, err := jwt.ParseWithClaims(identityToken, claims, v.kf.Keyfunc,
        jwt.WithIssuer(v.opts.AllowedIssuer),
        // jwt/v5 supports multiple audiences but the WithAudience matcher
        // tests for any-of-the-set; we still must check the token's aud
        // is in our whitelist manually because Apple's spec allows aud
        // as a single string.
    )
    if err != nil { return AppleIdentity{}, err }

    // Manual aud whitelist check — Apple's aud is a single string (per spec).
    aud, _ := claims["aud"].(string)
    if !contains(v.opts.AllowedAudiences, aud) {
        return AppleIdentity{}, errors.New("apple: audience mismatch")
    }
    sub, _ := claims["sub"].(string)
    email, _ := claims["email"].(string)
    emailVerifiedRaw, _ := claims["email_verified"].(string) // Apple sends "true"/"false" strings!
    isPrivateRelay, _ := claims["is_private_email"].(string)
    return AppleIdentity{
        Sub: sub, Email: email,
        EmailVerified:  emailVerifiedRaw == "true",
        IsPrivateRelay: isPrivateRelay == "true",
    }, nil
}
```

**[CITED: https://developer.apple.com/documentation/sign_in_with_apple/sign_in_with_apple_rest_api/authenticating_users_with_sign_in_with_apple]** Apple's `email_verified` and `is_private_email` claims are typed as **string** ("true"/"false"), not boolean. This is a well-known footgun — handle it explicitly.

**Pitfall:** `jwt.MapClaims` returns `interface{}`; if you read `claims["email_verified"]` and treat it as `bool`, the type assertion returns the zero value (`false`) silently. The above code reads as string and compares.

### Google Verifier (`internal/auth/google/verifier.go`)

**Library:** `google.golang.org/api/idtoken` (v0.280.0+, 29 transitive deps) [VERIFIED: pkg.go.dev]

**Pattern — loop over allowed audiences:**
```go
package google

import (
    "context"
    "errors"
    "google.golang.org/api/idtoken"
)

type GoogleIdentity struct {
    Sub           string
    Email         string
    EmailVerified bool
    HostedDomain  string
}

type Verifier struct {
    AllowedAudiences []string  // {iOS, Android, Web client IDs}
}

func (v *Verifier) Verify(ctx context.Context, idToken string) (GoogleIdentity, error) {
    var lastErr error
    for _, aud := range v.AllowedAudiences {
        payload, err := idtoken.Validate(ctx, idToken, aud)
        if err != nil { lastErr = err; continue }
        // Successful validation — extract claims.
        sub := payload.Subject
        email, _    := payload.Claims["email"].(string)
        verified, _ := payload.Claims["email_verified"].(bool)   // Google IS bool, unlike Apple
        hd, _       := payload.Claims["hd"].(string)
        if !verified {
            return GoogleIdentity{}, errors.New("google: email not verified")  // D-17
        }
        return GoogleIdentity{Sub: sub, Email: email, EmailVerified: true, HostedDomain: hd}, nil
    }
    return GoogleIdentity{}, lastErr
}
```

**[VERIFIED: pkg.go.dev/google.golang.org/api/idtoken]** `idtoken.Validate` performs JWKs fetch + RS256 signature verify + `aud` match + `exp` check in one call. The library caches Google's keys; no manual caching needed.

**Pitfall:** Apple `email_verified` is a string; Google `email_verified` is a bool. Don't share a helper. The two verifiers are intentionally distinct packages (D-12, D-13).

### JWT Blacklist for Logout — use the existing infrastructure

**Discovery:** `internal/cache/redis.go` already has the complete blacklist API and the auth middleware (`internal/middleware/auth.go:73-80`) already checks it. **Phase 2 does not write new blacklist plumbing** — it writes a Logout handler that calls one existing function.

Existing API (verbatim from `cache/redis.go`):
```go
const blacklistKeyPrefix = "token:blacklist:"      // <-- note: "token:blacklist:" not "jwt:blacklist:"
func BlacklistToken(ctx context.Context, client *redis.Client, tokenHash string, ttl time.Duration) error
func IsTokenBlacklisted(ctx context.Context, client *redis.Client, tokenHash string) bool  // fail-open on Redis error
```

Middleware integration (verbatim from `middleware/auth.go:73-80`):
```go
if redisClient != nil {
    tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tokenString)))
    if cache.IsTokenBlacklisted(c.Context(), redisClient, tokenHash) {
        return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
            "error": "token has been revoked",
        })
    }
}
```

**Note:** CONTEXT.md D-24 says key pattern `jwt:blacklist:<…>`; the existing prefix is `token:blacklist:`. **Match the existing prefix** — the middleware reads it and changing it would orphan all existing blacklist entries. Document this divergence from D-24 in the plan; this is a "match reality" call, not an alternative to a locked decision.

**Logout handler shape:**
```go
func Logout(logger *zap.Logger, redisClient *redis.Client, db *gorm.DB) fiber.Handler {
    return func(c *fiber.Ctx) error {
        // The middleware already validated the token; we re-parse to get exp.
        authHeader := c.Get("Authorization")
        tokenString := strings.TrimPrefix(authHeader, "Bearer ")
        userID, _ := c.Locals("user_id").(string)

        // Decode claims to compute remaining TTL. Signature already verified by middleware.
        parser := jwt.NewParser(jwt.WithoutClaimsValidation())  // we only want exp
        claims := jwt.MapClaims{}
        _, _, _ = parser.ParseUnverified(tokenString, claims)
        var ttl time.Duration
        if exp, ok := claims["exp"].(float64); ok {
            ttl = time.Until(time.Unix(int64(exp), 0))
            if ttl > 5*time.Minute { ttl = 5*time.Minute }  // clamp per D-24
            if ttl < 0 { ttl = 0 }                          // already expired — skip blacklist
        }

        // Delete the user's sessions. CONTEXT.md "logout request body" Discretion
        // default is "empty body, delete by user". For a single-device logout
        // pattern, the planner would need the refresh_token to scope the delete.
        // Default: delete ALL sessions for this user. Simpler, matches "logout
        // means logout".
        if err := repository.DeleteUserSessions(db, userID); err != nil {
            logger.Error("logout: failed to delete sessions", zap.String("user_id", userID), zap.Error(err))
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "internal server error"})
        }
        if ttl > 0 {
            tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tokenString)))
            _ = cache.BlacklistToken(c.Context(), redisClient, tokenHash, ttl)
        }
        return c.SendStatus(fiber.StatusNoContent)
    }
}
```

**New repository function needed:** `repository.DeleteUserSessions(db, userID)` — single DELETE on the sessions table. The existing `DeleteSession` deletes one row by session id; the planner adds the multi-row sibling. Or, alternatively, if the planner picks the "scoped to refresh_token in body" option, they call `FindSessionByTokenHash` + `DeleteSession` and never add a new repo function.

### Guest-Promote-in-Place — Transactional

GORM transaction pattern is already canonical in this repo. From `auth.go:262-289`:
```go
err = db.Transaction(func(tx *gorm.DB) error {
    if err := repository.DeleteSession(tx, session.ID); err != nil { … }
    // ... etc using tx, never the outer db
    return nil
})
```

**Phase 2 mirrors this:**
```go
// repository/user_repo.go (extension)
func PromoteGuestToSSO(db *gorm.DB, guestUserID, sub, email string, provider string, isPrivateRelay bool) error {
    return db.Transaction(func(tx *gorm.DB) error {
        updates := map[string]interface{}{
            "email":                   email,
            "email_verified":          true,   // SSO providers verify
            "email_is_private_relay":  isPrivateRelay,
            "auth_provider":           provider,
        }
        switch provider {
        case "apple":  updates["apple_user_id"] = sub
        case "google": updates["google_user_id"] = sub
        }
        result := tx.Model(&model.User{}).Where("id = ?", guestUserID).Updates(updates)
        if result.Error != nil {
            if isDuplicateError(result.Error) { return ErrDuplicate }
            return result.Error
        }
        if result.RowsAffected == 0 { return ErrNotFound }
        return nil
    })
}
```

**The harder case** (D-06): a row already owns this `sub`, so the guest must be orphaned and the guest's device rows reassigned. Pseudocode:
```go
// Inside the handler — done in one TX after FindUserByAppleID returns existing
err := db.Transaction(func(tx *gorm.DB) error {
    if err := repository.ReassignDevicesToUser(tx, guestUserID, existingUser.ID); err != nil {
        return err
    }
    // Let the stale-device scheduler garbage-collect the guest row.
    // OR delete it explicitly if it meets DeleteOrphanGuestUser's safety criteria.
    if err := repository.DeleteOrphanGuestUser(tx, guestUserID); err != nil &&
       !errors.Is(err, repository.ErrNotFound) {
        return err
    }
    return nil
})
```
Both `ReassignDevicesToUser` and `DeleteOrphanGuestUser` accept any `*gorm.DB` and so naturally compose into a tx. `DeleteOrphanGuestUser` already returns `ErrNotFound` (not an error) when the row doesn't meet safety criteria — the handler must treat that as the "skip cleanup" path, not a failure.

### Account-Linking Race Condition

**The race:** two parallel requests arrive with the same Apple `sub` for a user that doesn't exist yet. Both call `FindUserByAppleID` → both get `ErrNotFound` → both call `CreateUser` → second one collides on the partial unique index `idx_users_apple_user_id`.

**The fix:** use ON CONFLICT semantics. GORM's `clause.OnConflict` exposes Postgres's `ON CONFLICT … DO NOTHING RETURNING *` semantics:

```go
import "gorm.io/gorm/clause"

func UpsertSSOUser(db *gorm.DB, user *model.User) error {
    // ON CONFLICT on the partial-unique-index column, DoNothing means the second
    // writer's INSERT silently no-ops. After the call, re-read to get the winner's row.
    err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(user).Error
    if err != nil { return err }
    if user.ID == "" {
        // The DoNothing path doesn't populate user.ID. Re-read by the conflicting key.
        return ErrConflict  // caller re-reads via FindUserByAppleID / FindUserByGoogleID
    }
    return nil
}
```

**Simpler alternative — let the second insert fail with ErrDuplicate, then re-read:**
```go
// Inside the handler:
user, err := repository.FindUserByAppleID(db, identity.Sub)
if errors.Is(err, repository.ErrNotFound) {
    newUser := &model.User{AppleUserID: &identity.Sub, ...}
    if err := repository.CreateUser(db, newUser); err != nil {
        if errors.Is(err, repository.ErrDuplicate) {
            // Race lost — the other request created the row first. Re-read.
            user, err = repository.FindUserByAppleID(db, identity.Sub)
            if err != nil { return err }
        } else { return err }
    } else {
        user = newUser
    }
}
```

`isDuplicateError` already detects both Postgres `23505` and SQLite `UNIQUE constraint failed` (db.go:50), so this pattern works in tests too. **Recommendation: simpler alternative.** Avoids importing `clause` and keeps the handler readable.

### Verifier Constructor & Dependency Injection

Per D-34, audience whitelists are read **once** at startup and the verifier is injected into the handler. Pattern in `cmd/main.go`:

```go
// After config.Load() succeeds and before route registration:
appleVerifier, err := apple.New(context.Background(), apple.Options{
    AllowedAudiences: []string{cfg.AppleBundleID, cfg.AppleServiceID},
    AllowedIssuer:    "https://appleid.apple.com",
})
if err != nil { logger.Fatal("apple verifier init", zap.Error(err)) }

googleVerifier := &google.Verifier{
    AllowedAudiences: []string{cfg.GoogleClientIDIOS, cfg.GoogleClientIDAndroid, cfg.GoogleClientIDWeb},
}

// Routes
api.Post("/auth/apple",  handler.AppleSignIn(logger, cfg, db, appleVerifier))
api.Post("/auth/google", handler.GoogleSignIn(logger, cfg, db, googleVerifier))
protected.Post("/auth/logout", handler.Logout(logger, redisClient, db))
```

Handler signature accepts an **interface** (not the concrete verifier) so tests can inject a fake:
```go
type AppleVerifier interface {
    Verify(ctx context.Context, identityToken string) (apple.AppleIdentity, error)
}

func AppleSignIn(logger *zap.Logger, cfg *config.Config, db *gorm.DB, v AppleVerifier) fiber.Handler { … }
```

The verifier package exports both the concrete `*Verifier` (which satisfies the interface) and the `AppleIdentity` value type. The handler package defines the interface it consumes — Go's structural typing means no explicit "implements" declaration is needed.

### Testing Strategy — Mock the Verifier Interface

Two viable options for testing SSO handlers without real Apple/Google tokens:

| Option | Approach | Tradeoff |
|--------|----------|----------|
| **A. Mock verifier interface** | Handler tests inject a `fakeAppleVerifier` whose `Verify` returns a canned `AppleIdentity`. **No real JWT involved.** Tests run in microseconds. | Handler tests do NOT exercise signature/audience/exp verification — those live in verifier tests. |
| **B. Sign test tokens** | Generate a local RSA keypair; sign a JWT with the test private key; stub the JWKs source to return the test public key. Tests exercise the full verify path. | More setup; slower; brings `crypto/rsa` into the test imports; risk of test brittleness when libraries upgrade. |

**Recommendation: A for handler tests, B for verifier tests.**

- `internal/auth/apple/verifier_test.go` uses **B** — signs JWTs with a test RSA key, stubs the `jwkset.Storage` interface (or just constructs a `keyfunc.Keyfunc` from a static `jwt.VerificationKeySet`). Covers happy path, audience mismatch, expired token, signature mismatch, `iss` mismatch, `email_verified="false"` string handling.
- `internal/handler/auth_test.go` uses **A** — handler tests inject `&fakeAppleVerifier{returnIdentity: AppleIdentity{Sub:"appleSub123", Email:"x@example.com", EmailVerified:true}}`. Covers handler-level behaviour: guest-promote-in-place, account-link-by-email, private-relay-skip, cross-surface-same-sub-same-id.

**For Google verifier:** `idtoken.Validate` is a top-level function (not a method), so the verifier-level test cannot mock the JWKs source from inside the library. Two options: (a) accept that Google verifier tests need network or a recorded fixture (brittle), or (b) wrap the call in a tiny adapter interface so the test injects a fake `Validator`:
```go
type idtokenValidator interface {
    Validate(ctx context.Context, idToken, audience string) (*idtoken.Payload, error)
}
type defaultValidator struct{}
func (defaultValidator) Validate(ctx context.Context, t, a string) (*idtoken.Payload, error) {
    return idtoken.Validate(ctx, t, a)
}
```
**Recommendation: (b)** — same pattern as Apple, consistent and testable.

### Existing `auth_test.go` SQLite Pattern

The test DB is constructed in `auth_test.go:35-99` via `gorm.io/driver/sqlite` `:memory:`. Phase 2 extends `newAuthTestDB` to add the six new columns to the `users` CREATE TABLE statement. The schema fragment:

```sql
CREATE TABLE IF NOT EXISTS users (
    -- ... existing columns ...
    apple_user_id           TEXT,
    google_user_id          TEXT,
    email                   TEXT,
    email_verified          INTEGER NOT NULL DEFAULT 0,
    email_is_private_relay  INTEGER NOT NULL DEFAULT 0,
    auth_provider           TEXT NOT NULL DEFAULT 'guest'
);
-- partial unique indexes — SQLite supports them
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_apple_user_id
    ON users(apple_user_id) WHERE apple_user_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_google_user_id
    ON users(google_user_id) WHERE google_user_id IS NOT NULL;
```

SQLite's `BOOLEAN` is just `INTEGER 0/1` — GORM maps `bool` to that correctly. `email_verified=TRUE` becomes `email_verified=1` in the WHERE clause. The existing `subscriptions` and `sessions` table fragments stay unchanged.

---

## Library / Dependency Footprint

| Library | Verified Version | Import Path | Purpose | Footprint |
|---------|------------------|-------------|---------|-----------|
| MicahParks/keyfunc/v3 | v3.8.0 (Feb 2026) | `github.com/MicahParks/keyfunc/v3` | Apple JWKs cache + jwt.Keyfunc bridge | Thin — depends on `MicahParks/jwkset`; both are small Go-stdlib-only packages |
| google.golang.org/api/idtoken | v0.280.0+ | `google.golang.org/api/idtoken` | Google ID-token validation incl. JWKs cache | **29 transitive deps** [VERIFIED: pkg.go.dev]. Marginal binary size (~3MB per ADR §7) acceptable for a server. |

**Verification commands the planner should run before writing the import lines:**
```bash
# from server/api/
go list -m -versions github.com/MicahParks/keyfunc/v3 | tr ' ' '\n' | tail -5
go list -m -versions google.golang.org/api | tr ' ' '\n' | tail -5
```

**go.mod additions:**
```go
require (
    github.com/MicahParks/keyfunc/v3 v3.8.0
    google.golang.org/api v0.280.0  // pin a minor; bump when team agrees
)
```

`go.sum` will gain ~30 entries from the google.golang.org/api transitive closure. Acceptable per ADR §7 "the marginal cost is irrelevant for a server."

**No hand-roll alternative recommended.** A custom Google JWKs fetcher + RS256 verifier is ~150 LOC and the project's `golang-jwt/jwt/v5` is already present, but the official library handles edge cases (Google's key rotation, `aud` validation, leeway for clock skew) that we'd otherwise re-discover via incidents. CONTEXT.md D-15 already locks this choice.

---

## Integration with Phase 1 Surface

| Phase 1 deliverable | Phase 2 integration |
|---------------------|---------------------|
| `config.RequireEnv()` (HOTFIX-08) | Append six required keys: `APPLE_TEAM_ID`, `APPLE_BUNDLE_ID`, `APPLE_SERVICE_ID`, `GOOGLE_CLIENT_ID_IOS`, `GOOGLE_CLIENT_ID_ANDROID`, `GOOGLE_CLIENT_ID_WEB`. One-line append to the `required` slice in config.go:151. |
| `config.OptionalEnvWarnings()` | Append `APPLE_KEY_ID` and `APPLE_PRIVATE_KEY_P8` with empty placeholders (D-30 says optional-with-warn until authorizationCode exchange ships). |
| `handler.ErrorHandler` (HOTFIX-04 — generic 500 + X-Request-ID) | New handlers MUST return errors via `c.Status(...).JSON(fiber.Map{"error": "..."})` for client-facing errors and bare `return err` (no JSON) for 500 — the global ErrorHandler will scrub. Verifier errors are NEVER surfaced raw to the client; map them to canonical strings ("invalid identity token", "audience mismatch", "token expired"). |
| `middleware.AuthRequired` (HOTFIX-02 — re-reads role from DB) | The Logout endpoint mounts under `protected.Post(...)` which already runs `AuthRequired`. The middleware ALREADY checks `cache.IsTokenBlacklisted` (auth.go:73-80). So a single Redis SET in the Logout handler causes the next request from that token to 401 — no middleware changes needed. |
| Transactional refresh (HOTFIX-05) | `db.Transaction(func(tx *gorm.DB) error { … })` is the locked pattern. Guest-promotion follows the same shape (auth.go:262-289). |
| `sessions.refresh_token_hash` UNIQUE (HOTFIX-07) | Existing index — no Phase 2 change. The new `DeleteUserSessions(db, userID)` (if added) is `DELETE FROM sessions WHERE user_id = $1`, hits the existing `idx_sessions_user_id` index. |
| `cache.BlacklistToken` / `cache.IsTokenBlacklisted` | Already exists in `internal/cache/redis.go:50-58` with the `token:blacklist:` key prefix. Logout handler calls `BlacklistToken`; middleware already calls `IsTokenBlacklisted`. **No new blacklist code.** |
| Migration runner (file-based `docker-entrypoint-initdb.d`) | Migration 018 is a plain `.sql` file in `server/api/migrations/`. No CONCURRENTLY needed (new columns, fresh indexes, zero rows to scan). Same idempotent style as migrations 015, 016. |

**Critical wiring note for `cmd/main.go`:**
- `apple.New(ctx, opts)` returns an error. Wrap in `logger.Fatal` like the existing `bot.New` line (main.go:256). The verifier's background JWKs-refresh goroutine binds to a context — pass `context.Background()` (or wire a shutdown context if the planner wants graceful shutdown of the refresh goroutine).
- Google verifier has no init error path.
- The verifier instances are injected as function arguments to the handler factories. Mirrors the existing `handler.GuestLogin(logger, db, cfg)` shape.

---

## Race & Failure Modes the Planner Must Address

| # | Mode | Severity | Mitigation |
|---|------|----------|------------|
| 1 | **Account-link race** — two parallel sign-ins for same Apple `sub` for a new user | High (causes 500 to one client; with the partial unique index, the second insert errors) | Catch `ErrDuplicate` from `CreateUser`, re-read via `FindUserByAppleID`. See §Account-Linking Race Condition. Tested via `go test -race` and a deliberate two-goroutine handler test. |
| 2 | **Cold-start JWKs unavailable** — server boots, first Apple sign-in arrives before JWKs fetched | Medium | `keyfunc.NewDefaultCtx` does NOT block on first fetch. First sign-in triggers an on-demand fetch; if Apple is down at that moment, return 500 (existing ErrorHandler scrubs to generic message + request_id). Do NOT block server startup on JWKs reachability. Server logs the fetch failure via keyfunc's default error handler. |
| 3 | **Mid-runtime JWKs unreachable** | Low | keyfunc's underlying jwkset.Storage caches keys; refresh goroutine retries on the configured interval. Cached keys continue to verify tokens during the outage. New `kid` from Apple key rotation during an outage is the only break — extremely rare. **[ASSUMED: stale-while-revalidate behavior]** — neither the keyfunc README nor jwkset README I reviewed explicitly documents whether cached keys are returned when refresh fails; the architecture strongly implies yes (otherwise the cache is pointless) but the planner should verify by reading `jwkset/http.go` directly before committing the assumption to plan text. Risk if wrong: ADR §13 row 10 promise is unmet. |
| 4 | **Blacklist Redis outage** | Low | `cache.IsTokenBlacklisted` is **fail-open** by design (redis.go:43 — comment "Fail open — Redis unavailability must not block all authenticated traffic"). A logout during a Redis outage silently fails to blacklist; the user's tokens remain valid until their natural 5-minute exp. Refresh-token deletion still happens (Postgres), so the user can't get NEW tokens. Acceptable per ADR §13. Documented; no code change. |
| 5 | **Clock skew between API server and Apple/Google** | Low | `jwt/v5` defaults to zero leeway. Apple/Google's `exp` is set by their servers; if our server's clock is more than a few seconds behind theirs at `iat`, we'd reject legitimate tokens. Mitigation: add `jwt.WithLeeway(30 * time.Second)` to `ParseWithClaims` in the Apple verifier; Google's `idtoken.Validate` already has an implicit leeway. Tests cover this with a clock-skew fixture. |
| 6 | **Partial guest-promotion failure** | High | The 3-step path (update users + reassign devices + delete orphan guest) MUST be one transaction (D-29). If any step fails, rollback — the user retries and either succeeds atomically or sees a 500. Without the TX, a user could end up with `apple_user_id` set on the guest row but devices still bound to a stale row. |
| 7 | **Same email, two providers, both sign in concurrently** | Low | Both calls hit `FindUserByVerifiedEmailForLink`, both get the same row, both try to set the other provider's column. Postgres serializes the UPDATEs by row lock; the second writer overwrites the first's `auth_provider` (D-07 allows this). No data corruption. |
| 8 | **Stale device row owned by a guest, attacker arrives with same `device_id`** | Existing risk from `GuestLogin` | Already mitigated by `device_secret_hash` check in `GuestLogin` (auth.go:373-390). The SSO path inherits this — when the handler binds `deviceId` to the SSO user, it should reuse the same secret-check semantics OR refuse to bind if the device row is owned by another user. **Planner decision: reuse GuestLogin's BindDeviceToUser semantics; if device_id exists with a different user_id, log + leave untouched.** Don't create cross-account device rows. |
| 9 | **`@privaterelay.appleid.com` email used as auto-link key** | High (privacy/security) — would auto-link a stranger's Apple sub to a victim's Google account | `FindUserByVerifiedEmailForLink` MUST filter `email_is_private_relay = FALSE` in the WHERE clause (D-03, D-04). The partial unique index `idx_users_email_verified` from D-09 already excludes private-relay rows from the auto-link search space. Tested: feed in `x@privaterelay.appleid.com` matching an existing row → no link, new row created. |
| 10 | **Verifier accepts a token signed by an attacker's key** | Critical | The whole verifier purpose. Caught by keyfunc's signature verification + Apple's HTTPS-only JWKs fetch. **NEVER set `InsecureSkipVerify`** on the HTTP client used by keyfunc. Default `http.Client` already verifies certificates against the system CA bundle. |
| 11 | **Cross-surface mismatch — Apple iOS bundle id token presented to web endpoint** | Medium (audience-confusion attack) | Both audiences are in the same whitelist (D-16). The verifier accepts both. The risk is the inverse — an attacker who got a web-aud token and tries to use it on a service that ONLY accepts the iOS-aud. Since our backend accepts both, this is moot. Documented and tested. |
| 12 | **Logout without refresh-token revocation** | High — if the planner picks "delete only the access token from blacklist", refresh tokens still mint new access tokens | D-23 mandates "deletes the matching `sessions` row". Plan MUST ensure session deletion happens — verify with a regression test: logout, then call `/auth/refresh` with the old refresh token → expect 401. |

---

## Validation Architecture

> Per `.planning/config.json` `workflow.nyquist_validation: true` — this section is mandatory.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` (stdlib) + Fiber's `app.Test(httptest.NewRequest(...))` + GORM SQLite `:memory:` |
| Config file | none — `go.mod` is sole config |
| Quick run command | `cd server/api && go test ./internal/auth/... ./internal/handler/... -run "Apple\|Google\|Logout\|SSO" -count=1` |
| Full suite command | `cd server/api && go test ./... -count=1 -race` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|--------------|
| AUTH-01 | Apple sig verified via JWKs; aud whitelist accepts BUNDLE or SERVICE | unit (verifier) | `go test ./internal/auth/apple/... -run TestVerify_HappyPath` | Wave 0 |
| AUTH-01 | Apple wrong aud → 401 | unit (verifier) + integration (handler) | `go test ./internal/auth/apple/... -run TestVerify_AudienceMismatch` + handler test | Wave 0 |
| AUTH-02 | Google verified via idtoken.Validate; loop over 3 audiences | unit (verifier) | `go test ./internal/auth/google/... -run TestVerify_HappyPath` | Wave 0 |
| AUTH-02 | Google `email_verified=false` rejected | unit (verifier) | `go test ./internal/auth/google/... -run TestVerify_EmailNotVerified` | Wave 0 |
| AUTH-03 | Migration 018 adds six columns + partial unique indexes | manual + smoke | `psql -f migrations/018_add_sso_columns.sql && \d users` | Wave 0 |
| AUTH-04 | Same Apple sub returns same `users.id` on second sign-in | integration (handler) | `go test ./internal/handler -run TestAppleSignIn_CrossSurfaceSameSubSameID` | Wave 0 |
| AUTH-05 | Guest with valid guest JWT signs in with Apple, `users.id` unchanged | integration (handler) | `go test ./internal/handler -run TestAppleSignIn_PromoteGuestInPlace` | Wave 0 |
| AUTH-06 | Apple + Google with same verified email auto-link | integration (handler) | `go test ./internal/handler -run TestAppleSignIn_AutoLinkByEmail` | Wave 0 |
| AUTH-06 | `@privaterelay.appleid.com` does NOT auto-link | integration (handler) | `go test ./internal/handler -run TestAppleSignIn_PrivateRelaySkipsLink` | Wave 0 |
| AUTH-07 | JWT shape identical to today's GuestLogin / AdminLogin | regression | `go test ./internal/handler -run TestAuth_JWTShapeUnchanged` | Wave 0 (extends existing) |
| AUTH-08 | `POST /auth/logout` returns 204, deletes session, blacklists token | integration | `go test ./internal/handler -run TestLogout_204_DeletesSession_BlacklistsToken` | Wave 0 |
| AUTH-08 | After logout, calling access token → 401 | integration (full Fiber app + miniredis) | `go test ./internal/handler -run TestLogout_AccessTokenInvalidAfterLogout` | Wave 0 |
| AUTH-08 | After logout, refresh token → 401 | integration | `go test ./internal/handler -run TestLogout_RefreshTokenInvalidAfterLogout` | Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/auth/... ./internal/handler/... -run <relevant>` (single-package, <10s)
- **Per wave merge:** `go test ./internal/auth/... ./internal/handler/... ./internal/repository/... ./internal/config/... -count=1 -race`
- **Phase gate:** `go test ./... -count=1 -race` (full suite green before `/gsd-verify-work`)

### Wave 0 Gaps
- [ ] `internal/auth/apple/verifier_test.go` — covers AUTH-01; uses test RSA keypair + stub JWKs source
- [ ] `internal/auth/google/verifier_test.go` — covers AUTH-02; uses injected `idtokenValidator` interface
- [ ] `internal/handler/auth_test.go` — extends existing file with SSO + Logout tests (AUTH-04..AUTH-08)
- [ ] `internal/handler/auth_test.go` — extend `newAuthTestDB` to add six new columns + indexes
- [ ] `internal/repository/user_repo_test.go` (new or extend) — covers `FindUserByAppleID`, `FindUserByGoogleID`, `FindUserByVerifiedEmailForLink` (incl. private-relay exclusion), `PromoteGuestToSSO`
- [ ] `internal/config/config_test.go` — extend to assert new env keys are in `RequireEnv()` output when unset (matches existing HOTFIX-08 test pattern)
- [ ] Go module fetches: `go get github.com/MicahParks/keyfunc/v3@v3.8.0 && go get google.golang.org/api/idtoken` (Wave 0 — must land before verifier package writes will compile)

### Eight-Dimension Coverage

1. **End-to-end criterion coverage.** Each of the 5 ROADMAP success criteria has a named test above. SC1 (same sub same id) = `TestAppleSignIn_CrossSurfaceSameSubSameID`. SC2 (auto-link except relay) = two tests. SC3 (guest promote) = `TestAppleSignIn_PromoteGuestInPlace`. SC4 (logout 204 + 401) = three tests. SC5 (wrong aud → 401) = verifier + handler tests.

2. **Unit-level coverage.** Verifier packages have their own `*_test.go` exercising sig verify, aud match, exp check, iss match, `email_verified` typing (Apple string vs Google bool), private-relay flag extraction. Repository functions have unit tests on the SQLite in-memory DB. `generateTokens` is unchanged from Phase 1 — regression tests already pass.

3. **Integration-test boundaries.** Postgres replaced by SQLite `:memory:` for handler tests (existing pattern in `auth_test.go`). Redis replaced by `miniredis/v2` (already in go.mod, used in middleware/auth_test.go:170). Apple/Google verifiers replaced by fake-interface stubs in handler tests. **Zero real Apple/Google network calls in CI.**

4. **Negative-path coverage.** Audience mismatch (Apple BUNDLE_ID token presented when only SERVICE_ID allowed in a hypothetical sub-config), expired token (`exp` in the past), signature mismatch (token signed with attacker's key, verifier rejects), missing required field (no `identityToken` in request body → 400), blacklist hit (token blacklisted via Redis, next request → 401), Google `email_verified=false` → 401, private-relay email → no auto-link.

5. **Concurrency coverage.** A test that launches two goroutines both calling `POST /auth/apple` with the same identityToken (mocked verifier returns same `Sub`) and asserts: both return 200, both `users.id` are the same, only one row exists in DB. Run with `-race`. Logout + refresh concurrency: blacklist token, parallel calls to `/auth/refresh` and a protected endpoint — both 401.

6. **Cross-surface coverage.** Test seeds a `users` row with `apple_user_id="ABC123"` and `auth_provider="apple"`, then calls `POST /auth/apple` with a mocked verifier returning `Sub="ABC123"` and `Aud="com.flawlssr.risevpn"` (BUNDLE_ID). Asserts the returned `user.id` matches the seeded row. Repeat with `Aud="services.risevpn.web"` (SERVICE_ID). Both produce the same `users.id`.

7. **Backwards-compat coverage.** Run the existing `auth_test.go` test suite unchanged against the new schema (the SQLite table-create in `newAuthTestDB` gains six columns but all existing INSERTs use named columns or default to NULL — should pass). Spec: `TestGuestLogin_*`, `TestAdminLogin_*`, `TestRefreshToken_*` all green. Plus add `TestLinkDevice_PostSSOMigration` to verify `/auth/link` still works.

8. **Operational coverage.** `TestRequireEnv_MissingSSOKeys_Reported` asserts that with no `APPLE_BUNDLE_ID` etc. set, `config.RequireEnv()` returns those keys (matches Phase 1's HOTFIX-08 test style). `TestAppleVerifier_JWKsColdStart_LogsButDoesNotPanic` exercises the cold-start path with an unreachable JWKs URL. No live external dependency; the test points keyfunc at a localhost URL that's not listening.

---

## Security Domain

> `security_enforcement` config key is absent in `.planning/config.json` — per agent contract, this means security is enabled. Default ASVS Level L1.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|------------------|
| V2 Authentication | **yes** | OAuth2/OIDC delegation to Apple/Google (V2.6.1); HS256 JWT for our session (V2.4.4); short-lived 5min access tokens (V2.2.5); refresh-token rotation already in place from Phase 1 |
| V3 Session Management | **yes** | Session row in Postgres bound by refresh-token hash; blacklist on logout (V3.3.1); refresh-token rotation (V3.3.3, Phase 1 HOTFIX-05) |
| V4 Access Control | partial | RBAC via `role` claim (existing); Phase 2 adds nothing new |
| V5 Input Validation | **yes** | `BodyParser` validates JSON structure; verifier rejects malformed JWTs (V5.1.3); audience whitelist enforced (V5.1.4) |
| V6 Cryptography | **yes** | RS256 (Apple/Google) and HS256 (our own) via `golang-jwt/jwt/v5` — **never hand-roll** signature verification. JWKs fetched over HTTPS with default `http.Client` cert validation (V6.2.5). |

### Known Threat Patterns for Go + Fiber + GORM Auth

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Token replay (stolen Apple/Google token re-used) | Spoofing | Tokens have short `exp` (Apple ~5 min, Google ~1h). Our backend JWT bound to refresh session in DB; logout deletes session. Cannot fully prevent provider-token replay in the verify window — accepted risk per ADR §13. |
| Audience confusion (token for service A presented to service B) | Spoofing / Tampering | Both Apple audiences (BUNDLE + SERVICE) and three Google audiences (iOS + Android + Web) explicitly whitelisted; mismatch → 401 (D-16). |
| Email spoofing to trigger auto-link onto a victim's account | Spoofing / Elevation | Auto-link gated on `email_verified = TRUE` AND `email_is_private_relay = FALSE`. SSO providers' `email_verified` claim is trusted; manual emails in `/auth/apple` request body (first-sign-in only) are NOT trusted — they MUST be IGNORED when the JWT's `email_verified=false`. |
| Race condition in guest-promotion | Tampering | TX-wrapped (D-29); duplicate-key constraint on apple/google_user_id ensures at most one row owns a sub. |
| Blacklist bypass via clock skew | Spoofing | TTL clamped to 5min — even if Redis cache is bypassed via timing race, the token expires shortly after. Acceptable per D-24. |
| JWKs MITM | Tampering | HTTPS-only fetch; default Go `http.Transport` verifies certs against system CA. Never `InsecureSkipVerify`. (D-CD threat-model.) |
| GORM SQL injection via untrusted strings in WHERE clauses | Tampering | All Phase 2 queries use parameterized GORM clauses (`.Where("apple_user_id = ?", sub)`). No string concatenation. Verified by grepping the new repo functions before commit. |
| Goroutine leak via JWKs refresh tied to wrong context | DoS | `keyfunc.NewDefaultCtx` accepts a context that controls the refresh goroutine. Pass `context.Background()` for now (server lifetime), or wire to a shutdown context if the planner wants the goroutine to stop on SIGTERM. Currently main.go doesn't expose a server-lifetime context; planner can either add one or accept the daemon-style goroutine. |

---

## Open Technical Questions

These are unanswered by CONTEXT.md and ADR-007 and need a planner decision (or a small spike before the plan writes the relevant section).

1. **Logout sessions-deletion scope.** "Delete this user's session corresponding to the calling access token" vs "Delete ALL sessions for this user". The current JWT has only `sub` (user_id) — there is no per-session identifier in the access token. So we cannot select the matching session from the access token alone. Options:
   - (a) Delete ALL sessions for the user (simplest, matches "logout means logout everywhere"). User must re-authenticate on every device.
   - (b) Require `{ "refresh_token": "..." }` in the logout body; delete only that session. Allows "logout this device only".
   - (c) Add `jti` to the access token AND mirror it onto `sessions` (new column) so we can match them. Schema change required — adds Phase 2 scope.

   **Default recommendation: (a)** — matches the "logout means logout" mental model and avoids new schema. Document the choice in the API contract. Phase 6/8 can revisit if the UX demands per-device logout.

2. **Goroutine lifecycle for the keyfunc JWKs refresher.** `cmd/main.go` does not currently expose a server-lifetime `context.Context`. Pass `context.Background()` and accept that the refresher goroutine lives until process death (clean — server is the only owner). Alternative: refactor main.go to create `serverCtx, serverCancel := context.WithCancel(context.Background())` and cancel on shutdown. **Recommendation: `context.Background()` now**, refactor when a future phase has a real reason.

3. **Apple `email_verified` typing — string or bool?** Apple's REST API doc historically returned `"true"`/`"false"` strings. **[ASSUMED]** — based on training data. Confirm against a real Apple identityToken or recent Apple Developer docs before locking this in the verifier. If Apple has migrated to native booleans, the type assertion in the example code above breaks silently (assertion fails → zero value `false` → user looks unverified). Risk: low impact (all real Apple tokens have `email_verified=true` anyway) but worth a one-line spike: log `fmt.Sprintf("%T", claims["email_verified"])` for the first few real tokens in dev.

4. **GORM `*string` pointer vs `sql.NullString` for nullable columns.** D-11 specifies `*string` in the GORM tags. The existing `User` struct uses `*string` for `EmailHash`, `PasswordHash`, `TelegramUsername`. Matches. Just confirming the planner uses the same pattern for `apple_user_id`, `google_user_id`, `email`.

5. **jwkset cached-keys-during-fetch-failure semantics.** [ASSUMED above] The keyfunc + jwkset library architecture implies cached keys are returned when refresh fails — the cache exists precisely for this — but neither README I reviewed explicitly states it. **The planner should read `jwkset/http.go` directly** to verify, since CONTEXT.md D-14 promises this behaviour. If the library does NOT do stale-while-revalidate, the plan must add a thin custom cache layer.

6. **Bot username collision check.** Out of scope — Phase 2 doesn't touch the Telegram recovery bot. Flagging only because the `cmd/main.go` already calls `handler.SetTelegramBotUsername`; the new verifier setup should not interfere with the existing init order.

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Apple's `email_verified` and `is_private_email` claims are stringly-typed ("true"/"false") | §Apple Verifier code sample | If Apple has switched to native bool, the verifier silently reads `false` for everyone. Low-impact (real Apple tokens always have `email_verified=true`) but verify with a one-line type-log in dev. |
| A2 | `keyfunc/jwkset` returns cached keys when remote JWKs refresh fails | §Race & Failure Modes #3, §Cold-start | If the library returns an error instead, every sign-in during a JWKs outage 500s and ADR §13 row 10 is unmet. Planner should read `jwkset/http.go` to confirm. |
| A3 | `idtoken.Validate` builds in clock-skew leeway by default | §Race & Failure Modes #5 | If not, legitimate Google sign-ins on time-skewed servers would 401. Low probability given Google's library maturity. |
| A4 | The Phase 1 `RequireEnv()` validator pattern is "append literal strings to a slice" — no registration API beyond that | §Integration with Phase 1 Surface | Verified by reading config.go:151-165. No risk — pattern is concrete code. |
| A5 | SQLite `:memory:` supports partial unique indexes (`CREATE UNIQUE INDEX ... WHERE col IS NOT NULL`) | §Existing auth_test.go SQLite Pattern | SQLite 3.8+ supports partial indexes. Local dev should be on a recent SQLite. Verify with `go test ./internal/handler -run TestSQLitePartialIndex` smoke. |

---

## Files to Touch (informational — planner writes the authoritative list)

**New files:**
- `server/api/migrations/018_add_sso_columns.sql`
- `server/api/internal/auth/apple/verifier.go`
- `server/api/internal/auth/apple/verifier_test.go`
- `server/api/internal/auth/google/verifier.go`
- `server/api/internal/auth/google/verifier_test.go`
- `.planning/phases/02-auth-sso-backend/auth-sso-api.md` (or `docs/auth-sso-api.md` — D-33)

**Modified files:**
- `server/api/internal/model/user.go` (D-11 — six new fields)
- `server/api/internal/repository/user_repo.go` (D-28 — four/five new functions)
- `server/api/internal/repository/session_repo.go` (potentially — `DeleteUserSessions` if discretion-default for logout chosen)
- `server/api/internal/handler/auth.go` (D-19 — three new handlers)
- `server/api/internal/handler/auth_test.go` (D-35 — SSO + logout tests; extend `newAuthTestDB` for new columns)
- `server/api/internal/config/config.go` (D-30 — new env vars in Config struct, in Load, in RequireEnv, in OptionalEnvWarnings)
- `server/api/internal/config/config_test.go` (assert new keys are validated)
- `server/api/cmd/main.go` (D-26 — verifier construction + 3 route registrations)
- `server/api/go.mod`, `server/api/go.sum` (D-14, D-15 — two new direct deps)

**Out-of-scope (do not touch):**
- `server/api/internal/handler/payment.go` — Phase 3
- `landing/` — Phase 4
- `app/` (React Native) — Phase 5
- Any `subscriptions.*` schema — Phase 3
- Any `plans*` schema — Phase 3

---

## Sources

### Primary (HIGH confidence)
- `pkg.go.dev/github.com/MicahParks/keyfunc/v3` — Verifier library API, Keyfunc interface, Override struct
- `pkg.go.dev/google.golang.org/api/idtoken` — Validate signature, Payload struct, Claims map typing
- `github.com/MicahParks/keyfunc` (GitHub release notes) — v3.8.0 confirmed Feb 2026 stable
- `server/api/internal/cache/redis.go` (in-tree) — Existing `BlacklistToken` / `IsTokenBlacklisted` API
- `server/api/internal/middleware/auth.go` (in-tree) — Existing blacklist check integration
- `server/api/internal/handler/auth.go` (in-tree) — Existing handler composition pattern
- `server/api/internal/config/config.go` (in-tree) — `RequireEnv()` and `OptionalEnvWarnings()` shape

### Secondary (MEDIUM confidence)
- `docs/ADR-007-lava-sso-rework.md` §4–§14 — All locked design decisions; cross-verified with REQUIREMENTS.md AUTH-01..AUTH-08
- `.planning/phases/01-hotfix-audit-critical-fixes/01-02-PLAN.md` — Phase 1 env-validator integration pattern (RequireEnv slice append)

### Tertiary (LOW confidence — needs validation)
- Apple `email_verified` type semantics ("true"/"false" string vs bool) — based on training-era memory of Apple's spec; verify with a real token before committing the verifier impl.
- `jwkset/http.go` stale-during-refresh-failure behaviour — strongly implied by architecture, not documented in README.

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — Context7-verified, in-tree pattern confirms
- Architecture: HIGH — Phase 1 integration points all verified by reading source
- Pitfalls: HIGH — Race conditions and TX patterns concretely mappable to existing code
- Stale-while-revalidate semantics: MEDIUM — library architecture implies it, README silent

**Research date:** 2026-05-22
**Valid until:** 2026-06-22 (Go libs are stable; revisit if Apple deprecates `https://appleid.apple.com/auth/keys` URL or Google migrates `idtoken` package)
