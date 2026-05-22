# Phase 2: Auth SSO backend - Context

**Gathered:** 2026-05-22
**Status:** Ready for planning
**Source:** PRD Express Path (`docs/ADR-007-lava-sso-rework.md`)

<domain>
## Phase Boundary

Ship the **backend-only** layer of Apple + Google SSO so identity calls work end-to-end against real provider tokens. Land migrations, verifier packages, handlers, repository functions, JWT mint changes, and the new `/auth/logout` endpoint — but **no landing or mobile UI work** and **no lava.top / payments / dynamic-plans work**. After this phase, a curl request from a known-good Apple `identityToken` or Google `idToken` returns a backend JWT pair tied to a deterministic `users.id`; logout deletes the refresh session and blacklists the calling access token until its `exp`.

**In-scope (this phase only):**
- DB migration that adds `apple_user_id`, `google_user_id`, `email`, `email_verified`, `email_is_private_relay`, `auth_provider` columns + partial-unique indexes on each provider id.
- New verifier packages: `internal/auth/apple` (Apple JWKs RS256) and `internal/auth/google` (`google.golang.org/api/idtoken`). Pure libs — no DB access, no Fiber types.
- New handlers: `POST /api/v1/auth/apple`, `POST /api/v1/auth/google`, `POST /api/v1/auth/logout`. All under existing `/api/v1` group.
- Repository functions: `FindUserByAppleID`, `FindUserByGoogleID`, `FindUserByEmailForLink`, plus the guest-promotion path.
- Account-linking-by-verified-email logic with the private-relay exception (`@privaterelay.appleid.com`).
- Guest → identified promote-in-place (preserves `users.id`, keeps existing device rows bound).
- JWT mint shape unchanged (HS256, same claim set as today's `auth.go::generateTokens`). Adds a `prov` claim only if the planner deems it needed for downstream; otherwise `auth_provider` lives in DB and the JWT keeps the existing `sub`/`tier`/`role`/`name`/`iat`/`exp` shape.
- Access-token blacklist for logout (Redis-backed, TTL = remaining token life, ~5min cap).
- Unit + integration tests for: happy path Apple, happy path Google, audience mismatch (Apple bundle id vs service id), expired token, signature mismatch, private-relay email auto-link skipped, guest-promote-in-place, dup `users.id` guarantees on second sign-in.
- Existing `/auth/guest`, `/auth/refresh`, `/auth/link`, `/auth/admin-login`, `/auth/telegram/*` regression tests still pass.

**Out-of-scope (explicitly deferred to later phases):**
- Landing `/login` page, Apple/Google JS SDK loaders, HttpOnly cookie handling, `/dashboard` — ADR §11, Phase 4 (or wherever GSD assigns landing-side SSO).
- Mobile `LoginScreen.tsx`, RN native deps (`@invertase/react-native-apple-authentication`, `@react-native-google-signin/google-signin`), entitlements — ADR §12, Phase 5.
- lava.top integration, `/checkout`, `/webhook/lava`, dynamic plans catalog, `users.plan_id` FK, `plan_offers` table — ADR §9, §19, GSD Phase 3.
- `/api/v1/admin/plans/*`, public `GET /api/v1/plans`, expiry cron — ADR §19.7, §19.9, §19.10, GSD Phase 3.
- Apple `external link entitlement` paperwork — operational, not a code task.
- Stripe deletion — happens in Phase 8 cleanup after lava.top fully replaces it.

</domain>

<decisions>
## Implementation Decisions

> Every entry is a **locked decision** sourced from ADR-007 unless marked Claude's Discretion. Where the ADR proposes alternatives and recommends one, the recommendation is taken as the locked choice for this phase.

### Identity model (ADR §5)

- **D-01:** Two separate columns `apple_user_id VARCHAR(255)` and `google_user_id VARCHAR(255)` (not a polymorphic `identities` table). Each carries a `WHERE col IS NOT NULL` partial unique index. Rationale: 2-provider scope, simpler indexing, account-linking via OR-of-columns. Revisit if a 3rd provider lands (separate ADR).
- **D-02:** Both columns can be populated on the **same** `users` row (account-linked user). Re-binding never happens silently — the only paths that set them are the SSO handlers themselves.
- **D-03:** Account-linking rule (ADR §5.3): auto-link by **verified email**. When `email_verified=TRUE` AND the matching row's `email_is_private_relay=FALSE` AND the new sign-in email is not `@privaterelay.appleid.com`, attach the new provider's `*_user_id` to the existing row instead of creating a new row.
- **D-04:** Private-relay exception (ADR §5.3, §11): emails ending in `@privaterelay.appleid.com` are stored as-is with `email_is_private_relay=TRUE` and are **never** used for auto-link lookup. They are stable Apple identifiers for that user but not a global email address, so the global-email-collision logic is skipped.
- **D-05:** Email zero-knowledge tradeoff (ADR §5.4): for Apple/Google users we store **cleartext** email in `users.email`. Admin login path continues to use `email_hash` (separate column, untouched). Guest users have neither. This is a deliberate break of the prior zero-knowledge policy and must be reflected in a privacy-policy update tracked outside this phase.
- **D-06:** Guest → identified promotion (ADR §5.5): if the request carries a guest JWT in `Authorization: Bearer` AND no other row owns this Apple/Google `sub`, **promote the guest row in place** — set `apple_user_id` or `google_user_id`, set `email`, set `auth_provider`, keep `users.id` stable. If a row already owns this provider sub, reassign the guest's device rows to that user and orphan the guest user row (let the stale-device scheduler clean it up).
- **D-07:** `auth_provider` column is a soft enum: `'guest' | 'apple' | 'google' | 'admin'`. The last-used provider wins on update (Apple sign-in then Google sign-in on the same row sets it to `'google'`). This is informational — handlers do not branch on it.

### Schema (ADR §8.1)

- **D-08:** Migration filename is `018_add_sso_columns.sql`. ADR §8.1 says `017_…` but `017_sessions_refresh_token_hash_unique.sql` already shipped in Phase 1. Phase 2 starts at **018** and is the only migration this phase produces.
- **D-09:** Column set (ADR §8.1 verbatim):
  ```sql
  ALTER TABLE users
      ADD COLUMN apple_user_id          VARCHAR(255),
      ADD COLUMN google_user_id         VARCHAR(255),
      ADD COLUMN email                  VARCHAR(320),
      ADD COLUMN email_verified         BOOLEAN NOT NULL DEFAULT FALSE,
      ADD COLUMN email_is_private_relay BOOLEAN NOT NULL DEFAULT FALSE,
      ADD COLUMN auth_provider          VARCHAR(20) NOT NULL DEFAULT 'guest';

  CREATE UNIQUE INDEX IF NOT EXISTS idx_users_apple_user_id
      ON users(apple_user_id)  WHERE apple_user_id  IS NOT NULL;
  CREATE UNIQUE INDEX IF NOT EXISTS idx_users_google_user_id
      ON users(google_user_id) WHERE google_user_id IS NOT NULL;
  CREATE INDEX IF NOT EXISTS idx_users_email_verified
      ON users(email) WHERE email_verified = TRUE AND email_is_private_relay = FALSE;
  ```
- **D-10:** No `users.plan_id`, no `plans`/`plan_servers`/`plan_offers` tables in this migration — those are Phase 3 work even though ADR §19.3 mentions them.
- **D-11:** GORM model additions to `internal/model/user.go` (ADR §8.4 verbatim, minus `PlanID`):
  ```go
  AppleUserID         *string `json:"-" gorm:"column:apple_user_id;uniqueIndex"`
  GoogleUserID        *string `json:"-" gorm:"column:google_user_id;uniqueIndex"`
  Email               *string `json:"email" gorm:"column:email;size:320"`
  EmailVerified       bool    `json:"email_verified" gorm:"column:email_verified;default:false"`
  EmailIsPrivateRelay bool    `json:"-" gorm:"column:email_is_private_relay;default:false"`
  AuthProvider        string  `json:"auth_provider" gorm:"column:auth_provider;default:guest"`
  ```

### Verifier packages (ADR §4, §7)

- **D-12:** Apple verifier lives at `server/api/internal/auth/apple/{verifier.go,verifier_test.go}`. Public API: `Verify(ctx, identityToken, opts) (AppleIdentity, error)`. `AppleIdentity` struct: `Sub, Email string; EmailVerified, IsPrivateRelay bool`. Pure package — no DB, no Fiber, no globals; opts carry the audience whitelist + JWKs source.
- **D-13:** Google verifier lives at `server/api/internal/auth/google/{verifier.go,verifier_test.go}`. Public API: `Verify(ctx, idToken, opts) (GoogleIdentity, error)`. `GoogleIdentity` struct: `Sub, Email string; EmailVerified bool; HostedDomain string`. Same purity constraint.
- **D-14:** Apple JWKs library: `github.com/MicahParks/keyfunc/v3` paired with the existing `github.com/golang-jwt/jwt/v5`. Library cache TTL: 24h with stale-while-revalidate (return cached keys if refresh fails — risk row 10).
- **D-15:** Google ID-token library: `google.golang.org/api/idtoken`. Call `idtoken.Validate(ctx, idToken, audience)` once per allowed audience (iOS, Android, Web client IDs), accept the first success. Reject if all fail.
- **D-16:** Apple `iss` must equal `https://appleid.apple.com`. Apple `aud` must be in `{APPLE_BUNDLE_ID, APPLE_SERVICE_ID}` — both are accepted because mobile uses bundle id and web uses service id (ADR §6.3). Google `aud` must be in `{GOOGLE_CLIENT_ID_IOS, GOOGLE_CLIENT_ID_ANDROID, GOOGLE_CLIENT_ID_WEB}`. All three audience lists are loaded from config at startup; rejection on mismatch returns 401.
- **D-17:** Reject Google sign-in with `email_verified=false` (ADR §7). Apple's `email_verified` may be `true` even for relay addresses — accept it but flag `email_is_private_relay=true` (ADR §5.4).
- **D-18:** Authorization-code exchange with Apple is **deferred**. Verifier accepts the optional `authorizationCode` field in the request body but does not exchange it server-side this phase — verification of the `identityToken` JWT is sufficient identity assertion (ADR §6.1 note). Leave a TODO comment pointing at the future hardening.

### Handlers (ADR §10)

- **D-19:** Handler files: extend `internal/handler/auth.go` with `AppleSignIn`, `GoogleSignIn`, `Logout`. Tests added/extended in `internal/handler/auth_test.go`. Composition pattern: handler does request parsing → calls verifier (`internal/auth/...`) → looks up/creates user (`internal/repository/user.go`) → mints JWT (`auth.go::generateTokens`).
- **D-20:** Request shape for `POST /api/v1/auth/apple` (ADR §10.1):
  ```json
  {
    "identityToken": "string (required)",
    "authorizationCode": "string (optional, not exchanged this phase)",
    "fullName": "string (optional, first sign-in only)",
    "email": "string (optional, first sign-in only)",
    "deviceId": "string (optional)",
    "deviceSecret": "string (optional)",
    "platform": "string (optional: 'ios' | 'web')",
    "model": "string (optional)"
  }
  ```
  An optional `Authorization: Bearer <guest_jwt>` header signals guest-promotion attempt.
- **D-21:** Response shape (ADR §10.1, identical for Apple and Google):
  ```json
  { "data": {
      "access_token":  "...",
      "refresh_token": "...",
      "expires_in":    300,
      "user": { "id":"uuid", "auth_provider":"apple|google", "email":"...", "full_name":"...", "subscription_tier":"free" }
  } }
  ```
- **D-22:** Request shape for `POST /api/v1/auth/google` (ADR §10.2): `{ idToken, deviceId?, deviceSecret?, platform?, model? }`. No `fullName`/`email` request fields — Google's idToken claims carry them.
- **D-23:** Logout endpoint shape (ADR §10 implicit, AUTH-08 explicit):
  - `POST /api/v1/auth/logout`. **Auth: required (Bearer JWT).**
  - Body: empty (or optionally `{ "refresh_token": "..." }` for revoking a specific session; planner decides — see Discretion).
  - Behaviour: deletes the matching `sessions` row, adds the calling access-token JTI (or token-hash if no JTI) to a Redis blacklist key with TTL equal to remaining token life.
  - Response: 204 No Content.
- **D-24:** Access-token blacklist mechanism: Redis SET with key `jwt:blacklist:<jti-or-hash>` and TTL = `exp - now()` clamped to access-token-lifetime (5 min). JWT-validation middleware checks the blacklist on every protected request; cache miss = allow, hit = 401. Acceptable for 5-min access tokens — refresh tokens are revoked by row deletion, not blacklist.
- **D-25:** JWT mint stays HS256 with the existing claim set (`sub`, `tier`, `role`, `name`, `iat`, `exp`) (ADR §7). **No `plan_id` claim** is added in this phase — that's Phase 3 work when the plans catalog exists. JWT MAY add a `jti` (UUID4) claim if the planner deems it needed for the blacklist; the access-token-hash fallback is acceptable for v1.
- **D-26:** Route registration in `cmd/main.go`:
  ```
  + api.Post("/auth/apple",  handler.AppleSignIn(...))
  + api.Post("/auth/google", handler.GoogleSignIn(...))
  + protected.Post("/auth/logout", handler.Logout(...))   // behind JWT middleware
  ```
  Apple/Google sign-in routes are public (no JWT required) but optionally read `Authorization` header for guest-promotion.
- **D-27:** Errors (ADR §10.1):
  - 400 — malformed `identityToken`/`idToken`, missing required field.
  - 401 — signature invalid, audience mismatch, token expired, `iss` mismatch, Google `email_verified=false`.
  - 403 — request claims to promote a guest but the guest JWT is invalid.
  - 500 — JWKs fetch failed AND no cached keys (only after stale-while-revalidate also fails).

### Repository functions (ADR §14 Phase 1)

- **D-28:** New repository functions live in `internal/repository/user.go` (extend existing file, no new file):
  - `FindUserByAppleID(db, sub string) (*User, error)`
  - `FindUserByGoogleID(db, sub string) (*User, error)`
  - `FindUserByVerifiedEmailForLink(db, email string) (*User, error)` — returns the auto-link candidate only when `email_verified=TRUE AND email_is_private_relay=FALSE`.
  - `PromoteGuestToSSO(db, guestUserID, sub, email string, provider AuthProvider) error` — in-place UPDATE setting the provider id, email, auth_provider, optionally email_verified.
  - `BindDeviceToUser(db, deviceID, deviceSecret, userID string) error` — reused if it already exists from guest path; otherwise added as a thin wrapper around the device repo's update.
- **D-29:** Each function runs in a single GORM call; no transaction wrapping unless writing to multiple tables. Guest-promotion is the one multi-write path and **MUST** be transactional (one TX wrapping update-users + update-device-rows + delete-orphan-guest-if-applicable).

### Config additions (ADR §14 Phase 1)

- **D-30:** New env vars added to `internal/config/config.go` and the existing `HOTFIX-08` required-env validator (Phase 1 D-03). All required at startup; if any are missing/empty, the validator emits a single aggregate error and exits non-zero:
  - `APPLE_TEAM_ID` — opaque short string from Apple Developer.
  - `APPLE_BUNDLE_ID` — iOS app bundle (e.g. `com.flawlssr.risevpn`).
  - `APPLE_SERVICE_ID` — separate web Service ID.
  - `APPLE_KEY_ID`, `APPLE_PRIVATE_KEY_P8` — `.p8` content + key id. **Loaded but not used this phase** (kept for future `authorizationCode` exchange). Planner decides whether to mark them required-yet or optional-with-warn-log. Default: optional-with-warn-log until the exchange ships.
  - `GOOGLE_CLIENT_ID_IOS`, `GOOGLE_CLIENT_ID_ANDROID`, `GOOGLE_CLIENT_ID_WEB` — three distinct values.
- **D-31:** No `LAVA_*` env vars are added in Phase 2 — those land with Phase 3.

### Operational constraints

- **D-32:** Backwards-compat: no existing `User` row currently has `apple_user_id`, `google_user_id`, or `email` set, so the migration is destruction-free (matches ADR §2 — "No paying users today"). Default `auth_provider='guest'` on existing rows correctly classifies them.
- **D-33:** No mobile or web client work in this phase — but the API contracts MUST be stable, because Phases 4/5 (landing + mobile clients) are coded against them. Plan must include a documented OpenAPI/Swagger snippet or hand-written contract doc per endpoint, committed to `.planning/phases/02-auth-sso-backend/` (or `docs/auth-sso-api.md` — planner's call).
- **D-34:** Audience whitelist (Apple bundle id, Apple service id, three Google client ids) is configured **once at startup** in the verifier struct's constructor. Verifier is wired into the handler at server-init time via dependency injection — no global, no init() reading env vars.
- **D-35:** Test coverage gate: minimum one happy-path + one audience-mismatch + one expired-token test per verifier package. Handler tests must cover: guest-promote-in-place, account-link-by-email, private-relay-skip-link, cross-surface-same-sub-same-id, logout deletes session and blacklists access token, refresh after logout returns 401, second login on a different device for the same Apple sub returns the same `users.id`.
- **D-36:** Per project CLAUDE.md "GSD Workflow Enforcement", every code change lands through a GSD execution. No direct edits outside the plan's task list.
- **D-37:** Branching: per `.planning/config.json` branching strategy from Phase 1, plans land on the working branch directly with atomic commits per logical unit (one commit per migration, one per verifier package, one per handler set, one per repo function set, etc.). Planner decides the exact commit boundaries.

### Claude's Discretion

- **JWT `jti` claim vs token-hash blacklist key.** D-24 leaves this open. Default: token-hash (simpler, no claim-shape change for downstream tooling); adopt `jti` only if the planner spots a concrete reason (e.g. existing middleware already extracts a claim).
- **Logout request body.** D-23 allows empty body OR `{ refresh_token }`. Default: empty body — server reads access token from Authorization header, then resolves and deletes the matching `sessions` row by user id + access-token jti (or by deleting all sessions for the user — planner picks). Document the choice.
- **`auth_provider` enum enforcement.** D-07 says soft enum, no DB CHECK. Planner can add a `CHECK (auth_provider IN ('guest','apple','google','admin'))` if it fits cleanly into migration 018 — not required.
- **Tests organization.** Planner picks between extending existing `auth_test.go` (one large file) vs creating `auth_apple_test.go`, `auth_google_test.go`, `auth_logout_test.go`. Default: extend existing file; split only if it exceeds ~1500 lines.
- **Apple `authorizationCode` storage.** Planner decides whether to log it for forensic capture or discard it. Default: discard — storage adds GDPR surface area and we're not using it.
- **Threat-model coverage.** Phase 2 is auth-critical; planner MUST include a `<threat_model>` block in each PLAN.md per the project's security_enforcement gate. ASVS level inherits from `.planning/config.json` (default L1). At minimum, the threat model must cover: token replay, audience confusion (wrong-`aud` accepted), email-spoofing for auto-link, race condition in guest-promotion, blacklist bypass via clock skew, JWKs MITM (HTTPS required for fetch, no `InsecureSkipVerify`).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Architecture — the source of truth for this phase
- `docs/ADR-007-lava-sso-rework.md` §4 — component boundaries, package tree (`internal/auth/apple`, `internal/auth/google`)
- `docs/ADR-007-lava-sso-rework.md` §5 — identity model, schema columns, account-linking rule, zero-knowledge tradeoff, guest promotion
- `docs/ADR-007-lava-sso-rework.md` §6 — auth-flow diagrams (mobile + web Apple, mobile + web Google, cross-surface continuity)
- `docs/ADR-007-lava-sso-rework.md` §7 — JWT strategy, verifier libraries, audience rules
- `docs/ADR-007-lava-sso-rework.md` §8.1 — migration column set, indexes (note: rename to `018_…` since `017_…` is taken — D-08)
- `docs/ADR-007-lava-sso-rework.md` §8.4 — GORM `User` model additions (skip the `PlanID` line — Phase 3)
- `docs/ADR-007-lava-sso-rework.md` §10.1, §10.2 — API contracts for `/auth/apple`, `/auth/google`
- `docs/ADR-007-lava-sso-rework.md` §13 rows 3, 4, 7, 9, 10 — risks relevant to Phase 2 (private-relay, secret leaks, two accounts same person, share-code conflict, JWKs outage)
- `docs/ADR-007-lava-sso-rework.md` §14 Phase 1 — original "Backend SSO" phase definition, files-touched list, exit criteria
- `docs/ADR-007-lava-sso-rework.md` §15 open questions 1, 2, 4, 7, 8 — Apple/Google asset prep, account-linking confirmation, RN module choice (informational only — Phase 5), email privacy

### Roadmap / requirements
- `.planning/ROADMAP.md` §"Phase 2: Auth SSO backend" — phase goal, depends-on (Phase 1 hotfixes), 5 numbered success criteria, blockers list
- `.planning/REQUIREMENTS.md` §"Auth — Apple + Google SSO" — AUTH-01 through AUTH-08 acceptance criteria
- `.planning/PROJECT.md` — payment-provider/identity-provider locked decisions

### Code anchors (existing surface this phase extends)
- `server/api/cmd/main.go` — Fiber app construction, route registration. New `/auth/apple`, `/auth/google`, `/auth/logout` routes mount here.
- `server/api/internal/handler/auth.go` — existing `generateTokens`, `GuestAuth`, `AdminLogin`, `Refresh` handlers. Phase 2 extends this file with `AppleSignIn`, `GoogleSignIn`, `Logout`.
- `server/api/internal/handler/auth_test.go` — existing test patterns. Phase 2 extends with SSO-specific tests.
- `server/api/internal/model/user.go` — existing `User` struct. Phase 2 adds the six new columns (D-11).
- `server/api/internal/repository/user.go` — existing find/create. Phase 2 adds `FindUserByAppleID`, `FindUserByGoogleID`, `FindUserByVerifiedEmailForLink`, `PromoteGuestToSSO`.
- `server/api/internal/config/config.go` — existing env loader. Phase 2 adds the Apple + Google env vars (D-30) and registers them with the HOTFIX-08 validator from Phase 1.
- `server/api/migrations/017_sessions_refresh_token_hash_unique.sql` — the existing migration that pushes us to start at `018_…` (D-08).
- `server/api/go.mod` — Phase 2 adds `github.com/MicahParks/keyfunc/v3` and `google.golang.org/api`.

### Project-wide rules
- `CLAUDE.md` (project root) — GSD workflow enforcement, lava-only / Apple+Google-only / no-IAP constraints, webhook-reliability (Phase 3 not this one), security-gate "Critical/High before any paying user" (Phase 2 is on the gating path because it precedes Phase 3).
- `~/.claude/CLAUDE.md` (user globals) — architect → quality pipeline pattern; treat as planning input even though execution is the next phase.

</canonical_refs>

<specifics>
## Specific Ideas

- **Apple JWKs library: `github.com/MicahParks/keyfunc/v3`** (D-14). Pairs with the already-used `github.com/golang-jwt/jwt/v5`. 24h TTL cache with stale-while-revalidate.
- **Google idtoken library: `google.golang.org/api/idtoken`** (D-15). Official, validates audience inline.
- **Exact migration name: `018_add_sso_columns.sql`** (D-08). Not `017_…` as ADR-007 §8.1 says — that number is taken.
- **Logout blacklist: Redis SET, key pattern `jwt:blacklist:<token-hash-or-jti>`, TTL clamped to 5 min** (D-24). Refresh tokens revoked by `sessions` row delete, not by blacklist.
- **Audience whitelist constructed once at startup, not per-request** (D-34). Inject into handler via DI.
- **Apple `iss` exact match: `https://appleid.apple.com`** (D-16). Apple `aud` ∈ `{APPLE_BUNDLE_ID, APPLE_SERVICE_ID}`. Google `aud` ∈ `{GOOGLE_CLIENT_ID_IOS, GOOGLE_CLIENT_ID_ANDROID, GOOGLE_CLIENT_ID_WEB}`.
- **Reject Google `email_verified=false`** (D-17). Apple may report `email_verified=true` for relay addresses — accept identity, flag `email_is_private_relay=true`.
- **Reverse provider lookup is O(log n)** because of partial unique indexes from D-09.
- **HTTPS-only JWKs fetch — no `InsecureSkipVerify`** (D-CD threat-model). MitM on Apple keys = full account takeover.

</specifics>

<deferred>
## Deferred Ideas

- **Apple `authorizationCode` exchange** with `appleid.apple.com/auth/token`. Verifier accepts the field, doesn't exchange it this phase (D-18). Future hardening — add when the operational story for `APPLE_PRIVATE_KEY_P8` is settled.
- **Landing SSO UI, `/login`, `/dashboard`, HttpOnly cookie wiring** — ADR §11, separate GSD phase.
- **Mobile SSO UI, native deps, deep-linking, `LoginScreen.tsx`, `PaymentScreen.tsx` rewrite** — ADR §12, separate GSD phase.
- **lava.top integration, `/checkout`, `/webhook/lava`, `lava_contracts` / `invoices` / `lava_webhook_events` tables** — ADR §9, GSD Phase 3.
- **Dynamic plans catalog (`plans`, `plan_servers`, `plan_offers`), `users.plan_id` FK, admin plans CRUD, `GET /api/v1/plans`, expiry cron** — ADR §19, GSD Phase 3.
- **JWT `plan_id` claim** — ADR §19.9.2. Add when the plans catalog exists; not before.
- **Privacy-policy update reflecting cleartext-email storage** — ADR §5.4, §13 row 8. Operational task, not a code task.
- **App Store external-link entitlement application** — ADR §12.4, §13 row 1. Operational paperwork, not a code task; needed for Phase 5 ship, not Phase 2.
- **Merge-accounts flow** (Apple-on-web + Google-on-mobile with different emails) — ADR §13 row 7. Phase 6 work.
- **Stripe code deletion** — ADR §14 Phase 6. After Phase 3 lava.top fully takes over.
- **`subscription_tier`-removal in favour of pure `plan_id` join** — ADR §19.4 alternative; explicitly rejected for now (denormalised copy stays).

</deferred>

---

*Phase: 02-auth-sso-backend*
*Context gathered: 2026-05-22 via PRD Express Path (`docs/ADR-007-lava-sso-rework.md`)*
