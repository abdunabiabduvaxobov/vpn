---
phase: 02-auth-sso-backend
reviewed: 2026-05-22T00:00:00Z
depth: standard
files_reviewed: 23
files_reviewed_list:
  - docs/auth-sso-api.md
  - server/api/cmd/createadmin/main_test.go
  - server/api/cmd/main.go
  - server/api/go.mod
  - server/api/go.sum
  - server/api/internal/auth/apple/verifier.go
  - server/api/internal/auth/apple/verifier_test.go
  - server/api/internal/auth/google/verifier.go
  - server/api/internal/auth/google/verifier_test.go
  - server/api/internal/config/config.go
  - server/api/internal/config/config_test.go
  - server/api/internal/handler/auth.go
  - server/api/internal/handler/auth_test.go
  - server/api/internal/handler/payment_test.go
  - server/api/internal/middleware/admin_test.go
  - server/api/internal/model/user.go
  - server/api/internal/repository/device_repo.go
  - server/api/internal/repository/session_repo.go
  - server/api/internal/repository/subscription_repo_test.go
  - server/api/internal/repository/user_repo.go
  - server/api/internal/repository/user_repo_sso_test.go
  - server/api/internal/repository/user_repo_subscription_test.go
  - server/api/migrations/018_add_sso_columns.sql
findings:
  critical: 2
  warning: 4
  info: 3
  total: 9
status: issues_found
---

# Phase 02: Code Review Report

**Reviewed:** 2026-05-22T00:00:00Z
**Depth:** standard
**Files Reviewed:** 23
**Status:** issues_found

## Summary

Reviewed the Phase 2 SSO backend implementation end-to-end: Apple/Google JWT verifiers, the `resolveSSOUser` orchestration function, account-linking logic, the logout/blacklist flow, migration 018, repository functions, and the full test suite.

The overall architecture is sound. The security-critical paths (email body spoof, private-relay skip, audience whitelist, guest-JWT verification, ErrDuplicate race fallback, transactional reassign-and-orphan) are all implemented correctly and covered by tests. Two critical issues were found: an empty `sub` claim from a verified Apple/Google token silently creates a row with `apple_user_id = ""` which is functionally broken and bypasses the partial unique index; and the auto-link `db.Model(...).Updates(...)` call in `resolveSSOUser` Step B runs outside any transaction, creating a window where a concurrent signer can grab the same sub between the `FindUserByVerifiedEmailForLink` read and the `Updates` write.

---

## Critical Issues

### CR-01: Empty `sub` from verifier creates a broken user row

**File:** `server/api/internal/handler/auth.go:862-870` (AppleSignIn), `909-916` (GoogleSignIn)

**Issue:** If the Apple or Google JWT passes signature/aud/exp/iss checks but the `sub` claim is absent or an empty string, both `identity.Sub` and `p.sub` are `""`. `resolveSSOUser` then calls `repository.FindUserByAppleID(db, "")` (or Google equivalent), receives `ErrNotFound`, skips Steps B and C, and falls through to Step D where it creates a new `model.User` row with `AppleUserID = ptr("")`. The partial unique index `WHERE apple_user_id IS NOT NULL` fires (empty string IS NOT NULL), so a second concurrent call will get `ErrDuplicate` and then re-read the broken row — every sign-in with any Apple sub-less token will return the same phantom account.

In practice, a well-formed Apple token always has a `sub`, but the `keyfunc`/`jwt-go` library does not validate sub presence — `claims["sub"].(string)` type-asserts silently to `""` on a missing key and the handler has no guard.

**Fix:**
```go
// In AppleSignIn, after identity is returned from verifier:
if identity.Sub == "" {
    logger.Warn("apple signin: token missing sub claim")
    return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid identity token"})
}

// Same pattern in GoogleSignIn after google verifier.Verify():
if identity.Sub == "" {
    logger.Warn("google signin: token missing sub claim")
    return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid identity token"})
}
```

Additionally add a guard in `resolveSSOUser`:
```go
func resolveSSOUser(db *gorm.DB, logger *zap.Logger, p ssoResolveParams) (*model.User, error) {
    if p.sub == "" {
        return nil, errors.New("sso: empty provider sub")
    }
    // ...
}
```

---

### CR-02: Auto-link Step B is not transactional — TOCTOU between lookup and update

**File:** `server/api/internal/handler/auth.go:748-771`

**Issue:** Step B of `resolveSSOUser` performs two separate DB operations: `repository.FindUserByVerifiedEmailForLink(db, p.email)` followed by `db.Model(&model.User{}).Where("id = ?", linkCandidate.ID).Updates(updates)`. Between these two calls another concurrent SSO sign-in for the same email (different Apple sub) can also find the same `linkCandidate` and attempt to write a different `apple_user_id` to the same row. One write succeeds, the other receives a `ErrDuplicate` on the partial unique index and falls back to `findUserByProviderID` which now returns `ErrNotFound` (because the successful writer used a different sub), causing a `500` for the second caller.

The `ErrDuplicate` fallback on line 766 calls `findUserByProviderID(db, p.provider, p.sub)` — but the UNIQUE collision on `apple_user_id` means a *different* sub won the race, so the re-read still returns `ErrNotFound`, and the function bubbles up `nil, ErrNotFound` which the handler maps to `500`.

**Fix:** Wrap the lookup + update in a `db.Transaction`. This is the same pattern already used for Step A (reassign-and-orphan) and Step C (PromoteGuestToSSO):

```go
// Step B: auto-link inside a single transaction.
if p.email != "" && p.emailVerified && !p.isPrivateRelay {
    var linkedUser *model.User
    txErr := db.Transaction(func(tx *gorm.DB) error {
        linkCandidate, lerr := repository.FindUserByVerifiedEmailForLink(tx, p.email)
        if lerr != nil {
            if errors.Is(lerr, repository.ErrNotFound) {
                return nil // no candidate — proceed to Step C/D
            }
            return lerr
        }
        updates := map[string]interface{}{"auth_provider": p.provider}
        switch p.provider {
        case "apple":
            updates["apple_user_id"] = p.sub
        case "google":
            updates["google_user_id"] = p.sub
        }
        if err := tx.Model(&model.User{}).Where("id = ?", linkCandidate.ID).Updates(updates).Error; err != nil {
            if errors.Is(err, repository.ErrDuplicate) {
                // Race — another caller already linked this sub; proceed below.
                return nil
            }
            return err
        }
        var err error
        linkedUser, err = repository.FindUserByID(tx, linkCandidate.ID)
        return err
    })
    if txErr != nil {
        return nil, txErr
    }
    if linkedUser != nil {
        return linkedUser, nil
    }
    // linkedUser == nil: no candidate found or duplicate race — fall through.
}
```

---

## Warnings

### WR-01: `parseGuestJWT` does not validate that the token's `role` claim is `"user"` — an admin token is accepted as a guest promotion carrier

**File:** `server/api/internal/handler/auth.go:649-670`

**Issue:** `parseGuestJWT` only checks that the JWT is signed with `HS256` and has a non-empty `sub`. An admin access token (role=`admin`) presented in the `Authorization` header of `POST /auth/apple` passes this check and is treated as a guest-promotion intent. The admin row then gets `apple_user_id` attached and `auth_provider` overwritten to `apple`, which silently demotes the admin's `AuthProvider` field (even though `role` is not changed by `PromoteGuestToSSO`).

The more concerning case: a short-lived admin access token stolen from logs or via a network capture could be replayed to attach a new Apple sub to the admin account, changing the admin's authentication path.

**Fix:**
```go
func parseGuestJWT(authHeader, secret string) (string, error) {
    // ... existing parse ...
    sub, _ := claims["sub"].(string)
    if sub == "" {
        return "", errors.New("guest jwt: missing sub")
    }
    // Reject admin tokens — only guest/user tokens should promote.
    if role, _ := claims["role"].(string); role != "" && role != "user" {
        return "", errors.New("guest jwt: non-user role not allowed for promotion")
    }
    return sub, nil
}
```

---

### WR-02: `Logout` handler does not blacklist when `ttl == 0` — a token expiring within the current second is left un-blacklisted

**File:** `server/api/internal/handler/auth.go:1022-1028`

**Issue:** The blacklist write block is gated by `if ttl > 0`. When the access token's `exp` equals `time.Now()` (token expires within the current second, which happens in tests and under high-frequency rotation), `time.Until(time.Unix(int64(exp), 0))` can be `0` or slightly negative after the clamp `if ttl < 0 { ttl = 0 }`. The `if ttl > 0` guard skips the Redis write. The token is formally expired, so its blacklist entry would only matter for the sub-second window — but the middleware may allow it through for that window because `jwt.ParseWithClaims` uses a 30-second leeway by default (which is the same leeway Apple's verifier uses — the middleware may have leeway too).

More importantly, the skip of the `cache.BlacklistToken` call when `ttl == 0` means the audit trail is incomplete: an observer of the Redis keyspace cannot confirm the token was ever revoked.

**Fix:** Change the guard to `if ttl >= 0` so the key is written with a near-zero TTL (Redis will expire it immediately, which is correct behavior):
```go
if ttl >= 0 {
    tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(tokenString)))
    if err := cache.BlacklistToken(c.Context(), redisClient, tokenHash, ttl); err != nil {
        logger.Warn("logout: blacklist write failed (fail-open)",
            zap.String("user_id", userID), zap.Error(err))
    }
}
```

---

### WR-03: `resolveSSOUser` Step D does not create a `Subscription` row for new SSO users

**File:** `server/api/internal/handler/auth.go:793-821`

**Issue:** When Step D creates a brand-new SSO user row via `repository.CreateUser(db, newUser)`, it does not insert a corresponding `subscriptions` row. The `GuestLogin` path does create one (lines 458-477). As a result, `GET /api/v1/subscription` for a freshly-SSO'd user will return `ErrNotFound` (no active subscription row) and the handler likely returns a 404 rather than the expected `{plan: "free"}` response.

This is not a data-loss risk but it is a behavioral gap: SSO users who never go through the guest path will see a broken subscription screen on first login.

**Fix:** Add subscription creation inside Step D, mirroring `GuestLogin`:
```go
if err := repository.CreateUser(db, newUser); err != nil {
    if errors.Is(err, repository.ErrDuplicate) {
        return findUserByProviderID(db, p.provider, p.sub)
    }
    return nil, err
}
// Create the free subscription row for the new SSO user.
sub := model.Subscription{
    UserID:   newUser.ID,
    Plan:     "free",
    IsActive: true,
}
if err := repository.CreateSubscription(db, &sub); err != nil {
    // Non-fatal: log and continue. Subscription screen may 404 until
    // the next sign-in; a scheduled repair job can backfill.
    logger.Warn("sso: failed to create subscription for new user",
        zap.String("user_id", newUser.ID),
        zap.String("provider", p.provider),
        zap.Error(err))
}
return newUser, nil
```

---

### WR-04: `PromoteGuestToSSO` does not update `full_name` — promoted user retains `guest_XXXXXXXX` display name

**File:** `server/api/internal/repository/user_repo.go:376-399`

**Issue:** `PromoteGuestToSSO` sets `email`, `email_verified`, `email_is_private_relay`, `auth_provider`, and the provider ID column. It does not set `full_name`. A guest user whose `FullName` is `"guest_3fa4c12b"` retains that name after Apple/Google promotion even when the caller passes a non-empty `fullName` via `ssoResolveParams.fullName`.

The API contract (docs/auth-sso-api.md) states `full_name` is returned in the response and populated from `req.FullName` on first Apple sign-in. The response body built by `ssoResponseBody` uses `user.FullName`, which is read from the promoted row — so the name field in the JSON response will be `"guest_3fa4c12b"` instead of the real name Apple sent.

**Fix:** Pass `fullName` through `PromoteGuestToSSO` and include it in the `updates` map:
```go
func PromoteGuestToSSO(db *gorm.DB, guestUserID, sub, email, provider, fullName string, isPrivateRelay bool) error {
    // ...
    updates := map[string]interface{}{
        "email":                  email,
        "email_verified":         true,
        "email_is_private_relay": isPrivateRelay,
        "auth_provider":          provider,
    }
    if fullName != "" {
        updates["full_name"] = fullName
    }
    // ...
}
```

Update the caller in `resolveSSOUser` Step C:
```go
pErr := repository.PromoteGuestToSSO(db, p.guestUserID, p.sub, p.email, p.provider, p.fullName, p.isPrivateRelay)
```

---

## Info

### IN-01: `go 1.25.0` in `go.mod` refers to an unreleased Go version

**File:** `server/api/go.mod:3`

**Issue:** `go 1.25.0` is specified as the minimum Go version. As of the knowledge cutoff (August 2025), Go 1.25 is not yet released (Go 1.22 is the latest stable). This may cause `go mod tidy` or CI toolchain resolution to fail on machines without a toolchain override, or silently succeed if a `toolchain` directive is inferred. If the project runs on Go 1.22, the directive should reflect that.

**Fix:** Align `go.mod` with the actual toolchain used:
```
go 1.22.0
```

---

### IN-02: `seedAdminUser` in `auth_test.go` seeds `subscription_tier='ultimate'` — diverges from production invariant

**File:** `server/api/internal/handler/auth_test.go:165`

**Issue:** The `seedAdminUser` helper inserts a user with `subscription_tier='ultimate'`. The Phase 1 success criterion #8 (captured in `createadmin/main_test.go`) requires admin rows to default to `'free'`. This mismatch means `TestAdminLogin_HappyPath_Returns200WithTokens` produces a JWT with `tier=ultimate` in its claims, which diverges from the production createadmin behavior.

Not a security issue — the handler only checks `role='admin'`, not tier — but it is a test-data inconsistency that could mask a bug in tier-claim generation.

**Fix:** Change the seed in `seedAdminUser` to use `'free'`:
```go
`INSERT INTO users (email_hash, password_hash, full_name, role, subscription_tier)
 VALUES (?, ?, 'Admin', 'admin', 'free')`,
```

---

### IN-03: Migration 018 is missing a `ROLLBACK` on constraint failure — `BEGIN`/`COMMIT` block does not handle partial failure

**File:** `server/api/migrations/018_add_sso_columns.sql:14`

**Issue:** The migration file wraps all DDL in a `BEGIN; ... COMMIT;` block. PostgreSQL DDL is transactional, so if any statement fails (e.g., `ADD CONSTRAINT` fails because some existing rows already violate the CHECK constraint), the transaction will be rolled back by the database — which is correct. However, the migration file has no explicit `ROLLBACK` path, and some migration runners (e.g., golang-migrate with `--no-lock`) may attempt to execute subsequent statements after a partial failure without properly surfacing the error.

This is safe for a greenfield schema (no existing rows violate any constraint), but the absence of a guard is worth documenting.

**Fix:** Consider adding a comment and an explicit rollback pattern, or ensure the migration runner is configured to stop on first error. If using golang-migrate, `golang-migrate` by default wraps each migration in a transaction and issues ROLLBACK on error, so no code change is required — but a comment clarifying this is helpful:
```sql
-- Migration runner: this file requires transactional DDL (Postgres default).
-- If your runner does not auto-rollback on error, add error handling externally.
```

---

## Cross-cutting observations (no finding raised)

- **Device binding after SSO sign-in:** `AppleSignIn` and `GoogleSignIn` accept `deviceId`/`deviceSecret` in the request body but neither handler binds the device to the resolved user (Step A returns existing, or Step B/C/D creates/promotes). The `GuestLogin` handler does device binding. This means the first SSO sign-in from a new device does not create a `devices` row, so the device will not appear in `/devices` and device-based quotas will not be enforced. This was not flagged as a Warning because the spec in `auth-sso-api.md` is ambiguous on whether SSO routes bind devices, but it should be clarified.

- **Apple `aud` claim is array in some contexts:** Apple's identityToken spec allows `aud` to be either a JSON string or a JSON array of strings. The current verifier casts `claims["aud"].(string)` at line 120 of `verifier.go`. If Apple ever sends `aud` as `["com.flawlssr.risevpn"]` (array), the type assertion silently yields `""` and the whitelist check fails with `"apple: audience mismatch"` — a hard 401. `jwt-go` v5 may normalize `aud` to a string on `ParseWithClaims`; the comment in the file acknowledges `jwt.WithAudience` would also work. The current behavior fails safe (hard reject), so this is not flagged as a finding, but a production capture spike is recommended per the VALIDATION.md note.

- **`FindUserByVerifiedEmailForLink` is case-sensitive:** email comparison `email = ?` in Postgres is case-sensitive. If `alice@EXAMPLE.COM` exists in the DB and the Apple JWT presents `alice@example.com`, auto-link will miss. Email addresses are case-insensitive per RFC 5321. No finding raised as this matches the existing codebase convention, but worth noting.

---

_Reviewed: 2026-05-22T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
