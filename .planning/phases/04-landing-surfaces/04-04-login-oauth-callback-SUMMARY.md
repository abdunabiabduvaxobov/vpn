---
phase: 04-landing-surfaces
plan: 04
subsystem: landing
tags: [auth, oauth, login, csrf, apple-sign-in, google-sign-in, form-post]
dependency_graph:
  requires:
    - "landing/src/lib/env.ts (Plan 01 — readRequired helper extended with 5 OAuth vars)"
    - "landing/src/lib/cookies.ts (Plan 03 — COOKIE_NAMES, COOKIE_MAX_AGE, sessionCookieAttrs, clearCookieAttrs)"
    - "landing/src/lib/session-cookie.ts (Plan 03 — encodeSessionUser, decodePlanIdFromJwt)"
    - "landing/public/brand/{apple,google}/*.svg (Plan 02 — vendored brand marks)"
    - "landing/src/components/common/logo.tsx + landing/src/i18n/navigation.ts (existing)"
    - "landing/src/messages/{en,ru,es}.json (Plan 01 — login + errors namespaces already populated)"
    - "Phase 2 POST /api/v1/auth/{apple,google} contract"
    - "Phase 3 D-29 — plan_id claim emitted by every JWT mint site (AppleSignIn + GoogleSignIn included)"
  provides:
    - "Visible OAuth sign-in surface — /<locale>/login with Apple + Google brand-mark buttons (WEB-01)"
    - "OAuth state-CSRF mechanism (rv_oauth_state cookie + state.csrf field, constant-time compare on return)"
    - "Locale-less /auth/callback receiver — single registered redirect URI per provider (D-10)"
    - "Cookie-based session minted via Plan 03 helpers on successful exchange (WEB-02 — HttpOnly, no localStorage)"
    - "rv_user.planId populated from JWT plan_id claim (D-17 source-of-truth)"
    - "rv_user Max-Age = 30d (matches refresh) so session display survives natural access-token rotation (B2 closure on OAuth side)"
    - "Server-Action authorize URL builders (buildAppleAuthorizeUrl / buildGoogleAuthorizeUrl) — hard-coded provider domains, no env-driven domain override (T-04-04-09 mitigation)"
    - "Open-redirect defense via isSafeNextPath allow-list (T-04-04-02)"
    - "Checkout-intent hand-off — next=/pricing + plan/period/currency in state -> /<locale>/pricing?...&checkout=auto for Plan 07"
  affects:
    - "Plan 05 (Pricing) — unauthenticated 'Get Pro' click can now route through /login?next=/pricing&plan=pro&period=monthly&currency=USD and bounce back with intent preserved"
    - "Plan 06 (Dashboard) — first sign-in lands on /dashboard with rv_user.email + rv_user.planId set"
    - "Plan 07 (Checkout) — receives ?checkout=auto on /pricing when the user signed in mid-flow"
    - "Plan 08 (Deploy + smoke) — Playwright tests must run end-to-end Apple + Google sign-ins with a mocked backend"
tech_stack:
  added: []
  patterns:
    - "Server Action form-action pattern for Apple + Google buttons — JS-optional click handling (mirrors Plan 02 UserMenu sign-out form)"
    - "form_post mode for BOTH providers — Apple has used this since launch; we opted into Google's form_post explicitly so the single /auth/callback route handler is symmetric across providers"
    - "Locale-less callback route + locale carried in state payload — collapses provider redirect-URI registration to one entry per provider per environment"
    - "Hard-coded provider authorize-URL constants (appleid.apple.com, accounts.google.com) — env-driven provider domains are deliberately disallowed (T-04-04-09)"
    - "Defensive narrow on backend response — destructure ONLY {access_token, refresh_token, user.email, user.plan_id} so unexpected fields can't leak into rv_user (T-04-04-08)"
    - "isNextRedirect helper in catch block — Next.js redirect() throws a NEXT_REDIRECT digest that must NOT be swallowed by error handling"
key_files:
  created:
    - "landing/src/lib/oauth.ts"
    - "landing/src/app/[locale]/(app)/login/page.tsx"
    - "landing/src/app/[locale]/(app)/login/start-oauth.ts"
    - "landing/src/components/app/auth-button-apple.tsx"
    - "landing/src/components/app/auth-button-google.tsx"
    - "landing/src/app/auth/callback/page.tsx"
    - "landing/src/app/auth/callback/exchange.ts"
    - "landing/src/app/auth/callback/route.ts"
  modified:
    - "landing/src/lib/env.ts"
    - "landing/.env.example"
    - "landing/.env.local.example"
decisions:
  - "BOTH providers use response_mode=form_post (not just Apple). Goal: symmetric callback handling — one POST route handles Apple AND Google id_token receipts, no per-provider branch in the body parser."
  - "rv_user.planId is sourced from decodePlanIdFromJwt(access_token) FIRST, with backend.user.plan_id as a fallback. The JWT claim is what middleware reads as c.Locals('plan_id') — using the body would silently desync the displayed plan from authorization decisions during the pre-D-29 transition window."
  - "rv_user.Max-Age pinned to COOKIE_MAX_AGE.USER (30d, matches refresh TTL). NOT the access TTL (5min) — that would silently clear getSession()'s identity on every natural access-token rotation."
  - "isSafeNextPath is a TIGHT allow-list regex (^/(ru|en|es)?/?(login|dashboard|pricing|pay/(success|fail))$). Anything outside the four legitimate post-auth destinations is rejected silently (open-redirect defense)."
  - "Provider authorize-URL functions are HARD-CODED to appleid.apple.com / accounts.google.com. A misconfigured env can never redirect users to a lookalike provider domain (T-04-04-09)."
  - "Nonce is generated and stashed in rv_oauth_nonce cookie + echoed into the authorize URL, but backend nonce-claim verification is deferred. The id_token does carry the nonce — when the backend wires nonce checks (Phase 5+), no landing change is needed."
  - "Backend exchange uses credentials:'omit' and cache:'no-store'. No reason to attach landing-side cookies on the server-to-server hop, and the response must never enter Next's Data Cache."
  - "Plan 03's setSessionCookie() helper exists as an idea but Plan 03 actually exposes sessionCookieAttrs() + encodeSessionUser() — exchange.ts uses those directly (matches Plan 03's actual interface from session-cookie.ts and cookies.ts) instead of a higher-level wrapper. Functionally identical."
metrics:
  duration: "~20 minutes wall clock (3 tasks, 3 commits, 0 deviations)"
  completed: "2026-05-24"
  tasks_completed: 3
  files_changed: 11
  commits: 3
---

# Phase 4 Plan 04: Login + OAuth Callback Summary

Built the user-visible OAuth doorway: a `/<locale>/login` page with Apple + Google brand-mark buttons, a Server Action that mints a 32-byte CSRF token + nonce and redirects to the provider's authorize URL, and a locale-less `/auth/callback` receiver that constant-time-verifies the returned state, POSTs the `id_token` to the backend's `/api/v1/auth/{apple,google}` endpoint, decodes `plan_id` from the new access JWT (D-17 source-of-truth), and mints HttpOnly `rv_at` + `rv_rt` + HMAC-signed `rv_user` cookies via Plan 03's helpers — `rv_user` at a 30-day Max-Age so it survives natural access-token rotation (B2 closure on the OAuth side). Open-redirect attempts are silently dropped by an `isSafeNextPath` regex allow-list, and the checkout-intent hand-off (`next=/pricing` + `plan`/`period`/`currency`) survives the round-trip via the state payload so Plan 07 can auto-launch checkout for users who signed in mid-flow.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Extend env.ts + .env.example + add OAuth helper module | `e816ddf` | landing/src/lib/env.ts, landing/.env.example, landing/.env.local.example, landing/src/lib/oauth.ts |
| 2 | /login page + Apple/Google buttons + startOAuth Server Action | `793e73b` | landing/src/app/[locale]/(app)/login/page.tsx, .../start-oauth.ts, landing/src/components/app/auth-button-{apple,google}.tsx |
| 3 | /auth/callback receiver — state verify, exchange, plan_id from JWT | `b8b347d` | landing/src/app/auth/callback/{page.tsx, route.ts, exchange.ts} |

## Decision: form_post for BOTH Apple and Google

Apple has used `response_mode=form_post` since the launch of Sign in with Apple — it's the only mode where the `id_token` arrives on the server (the alternative `response_mode=fragment` only delivers the token to a client-side JS handler, which we don't want because we're a server-rendered Node app).

Google supports both `response_mode=fragment` (browser-side) and `response_mode=form_post`. We deliberately picked `form_post` so:

1. `/auth/callback` is a single POST handler — the body parser is the same for Apple and Google (no per-provider branch).
2. The id_token never sits in a URL fragment that could be logged by a misconfigured referer policy or telemetry tool.
3. The same Server Action `completeOAuth` can be reused if a GET-mode fallback ever ships.

The trade-off is that `response_type=id_token` (Google implicit flow) is on Google's deprecation watch list — see "W1 deprecation note" below.

## W1 Deprecation Note: Google `response_type=id_token`

Google's implicit flow (`response_type=id_token` without a `code`) is **on the deprecation path** — Google's identity team has indicated the long-term direction is PKCE + authorization-code with a server-to-server exchange of `code` for `id_token` against `https://oauth2.googleapis.com/token`.

Migrating requires:

1. **Backend change**: `POST /api/v1/auth/google` must accept a `code` parameter and perform the token-endpoint exchange itself (the backend gets `id_token` + `access_token` back; verifies + uses the `id_token`).
2. **Landing change**: switch `buildGoogleAuthorizeUrl` to `response_type=code`, add a PKCE `code_verifier` (stored in a HttpOnly cookie similar to the existing nonce), pass `code_challenge` + `code_challenge_method=S256`, and forward the returned `code` instead of an `id_token`.
3. **Apple stays unchanged** — Apple does not have an equivalent deprecation; `response_type=code id_token` + `response_mode=form_post` remains stable.

**Status:** captured as a Phase 5+ follow-up. Not blocking Phase 4 launch — Google's deprecation has a multi-year sunset window and no firm end-of-life date as of this writing.

## isSafeNextPath: Allow-list Patterns

Inputs accepted (must satisfy ALL):

1. String type
2. Starts with `/`
3. Does NOT start with `//` (protocol-relative URL evasion)
4. Does NOT start with `/\` (Windows-style separator evasion)
5. Does NOT contain `..` (path-traversal evasion)
6. Matches the regex: `^/(?:(?:ru|en|es)\/)?(?:login|dashboard|pricing|pay\/(?:success|fail))(?:\?[A-Za-z0-9_\-&=%.+:/,]*)?$`

Allowed paths (with or without `/ru/`, `/en/`, `/es/` prefix):

- `/login` (or `/<locale>/login`)
- `/dashboard` (or `/<locale>/dashboard`)
- `/pricing` (or `/<locale>/pricing`)
- `/pay/success` (or `/<locale>/pay/success`)
- `/pay/fail` (or `/<locale>/pay/fail`)

Any of these may carry a query string — the allowed query-string charset matches RFC 3986 unreserved + sub-delims minus `?` and `#`.

Adding a new legitimate post-auth destination requires extending the regex here. Audit any such change carefully — every new entry expands the open-redirect surface.

## Provider Dashboard Config Requirements (Operator Task)

Before Plan 04 ships to production, the operator MUST register the following with the providers:

### Apple Developer (developer.apple.com)

1. Go to **Identifiers** → create/edit Service ID `services.risevpn.web` (this is the value of `env.APPLE_SERVICE_ID`).
2. Enable **Sign In with Apple** capability.
3. Add **Return URL** exactly: `https://risevpn.com/auth/callback?provider=apple`.
4. For staging: add a parallel Service ID + Return URL pointed at the staging origin.

### Google Cloud Console (console.cloud.google.com → APIs & Services → Credentials)

1. Create an **OAuth client ID** of type **Web application**.
2. Add **Authorized JavaScript origins**: `https://risevpn.com`.
3. Add **Authorized redirect URIs** exactly: `https://risevpn.com/auth/callback?provider=google`.
4. Copy the resulting Client ID (ends in `.apps.googleusercontent.com`) into `env.GOOGLE_CLIENT_ID_WEB`.
5. This Client ID must match Phase 2 backend's `GOOGLE_CLIENT_ID_WEB` env (the backend's Google ID-token verifier checks the audience claim against this exact value).

### Per-environment env vars

| Env var | Production example | Local dev example |
|---|---|---|
| `APPLE_SERVICE_ID` | `services.risevpn.web` | `services.risevpn.web.dev` |
| `APPLE_REDIRECT_URI` | `https://risevpn.com/auth/callback?provider=apple` | (ngrok tunnel URL) |
| `GOOGLE_CLIENT_ID_WEB` | prod `<...>.apps.googleusercontent.com` | dev `<...>.apps.googleusercontent.com` |
| `GOOGLE_REDIRECT_URI` | `https://risevpn.com/auth/callback?provider=google` | `http://localhost:3000/auth/callback?provider=google` |
| `APP_URL` | `https://risevpn.com` | `http://localhost:3000` |

These five env vars are all required (validated at module load via `env.ts`'s `readRequired`) — landing crashes on boot if any is missing, which is the desired fail-fast behavior.

## B2 Closure: rv_user.planId from JWT + 30-day Max-Age

Two correctness gates close out here on the OAuth side. Plan 03 closed them on the refresh-rotation side; together they form the complete D-17 guarantee.

### (a) planId source: JWT claim, not response body

```ts
const planIdFromJwt = decodePlanIdFromJwt(accessToken);   // PRIMARY
const planIdFromBody = typeof backend.user?.plan_id === "string"
  ? backend.user.plan_id : "";                            // FALLBACK
const planId = planIdFromJwt || planIdFromBody;
```

Why JWT first: `c.Locals("plan_id")` in backend middleware reads from the verified JWT claim (Phase 3 D-29 — every mint site emits `plan_id`). If we used the response body's `user.plan_id` and the two ever desynced, the `/dashboard` plan display would say one thing while server-side authorization decisions used another. Aligning to the JWT closes that window.

The body fallback exists only for the pre-D-29 transition: if a JWT was minted before Phase 3 D-29 shipped and lacks the claim, the body value keeps the UI sane while the user's next refresh-rotation upgrades them to a JWT with the claim — and from then on the JWT is canonical.

### (b) Max-Age: 30 days (matches refresh)

```ts
jar.set({
  name: COOKIE_NAMES.USER,
  value: encodeSessionUser({ email, planId }),
  ...sessionCookieAttrs(COOKIE_MAX_AGE.USER),  // 30d
});
```

`COOKIE_MAX_AGE.USER = 60 * 60 * 24 * 30 = 30 days` (set in Plan 03's `cookies.ts`). NOT `COOKIE_MAX_AGE.ACCESS` (5min). Pinning to 30d means `getSession()` keeps returning email + planId for the full session lifetime, even though `rv_at` rotates every 5 minutes. Plan 03's proxy re-issues `rv_user` on every refresh-rotation so its planId stays current — staleness is bounded by one access-token cycle in the worst case.

## Auth Gates — Future Hardening (Backend nonce verification)

The OAuth nonce is currently generated, stashed in `rv_oauth_nonce` cookie, and echoed into the authorize URL — Apple and Google both include it as the `nonce` claim in the id_token. **But the backend does not currently verify the claim.** Replay-window math today:

- Apple id_token `exp`: 10 min (per Apple spec)
- Google id_token `exp`: 1 hour (per Google spec)
- Landing 10s timeout on exchange → tight upper bound on the replay window

For strict replay protection, the backend should:

1. Accept a `nonce` parameter alongside `id_token` on `/api/v1/auth/{apple,google}`.
2. Verify `id_token.nonce === nonce` after JWT signature verification.

Captured as **T-04-04-04-followup** — Phase 5+ hardening item. No landing change required once backend supports it (the landing already has the nonce in `rv_oauth_nonce` and could pass it as a body field).

## Threat Register Closure

All twelve threats from the plan's `<threat_model>` are honored:

| Threat | Closure |
|---|---|
| T-04-04-01 (CSRF) | 32-byte csrf in state + rv_oauth_state cookie; constant-time compare in completeOAuth before backend call |
| T-04-04-02 (open redirect) | isSafeNextPath regex allow-list applied at both startOAuth (to drop unsafe inputs) and completeOAuth (to validate the redirect target) |
| T-04-04-03 (id_token spoofing) | TRANSFERRED to backend — landing forwards the token; backend verifies JWKs + iss + aud |
| T-04-04-04 (replay) | nonce stashed + echoed; backend claim verification flagged as Phase 5+ hardening (see "Auth Gates" above) |
| T-04-04-05 (info disclosure in error URL) | Only `?error=oauth_state` or `?error=oauth_denied` lands in the URL — never the state payload, never the id_token |
| T-04-04-06 (state cookie tamper) | HttpOnly + Secure + SameSite=Strict on rv_oauth_state (via sessionCookieAttrs from Plan 03) |
| T-04-04-07 (state replay) | 5-min TTL on rv_oauth_state; cleared on every callback exit path (success and failure) via clearOAuthCookies() |
| T-04-04-08 (response field leak) | Backend response destructured to access_token, refresh_token, user.email, user.plan_id only |
| T-04-04-09 (provider impersonation) | appleid.apple.com / accounts.google.com hard-coded in oauth.ts — no env override |
| T-04-04-10 (DoS) | AbortSignal.timeout(10000) on exchange fetch; failure → /login?error=oauth_denied |
| T-04-04-11 (planId divergence) | decodePlanIdFromJwt(access_token) primary; backend body planId fallback only |
| T-04-04-12 (stale rv_user) | COOKIE_MAX_AGE.USER (30d) Max-Age; survives access-token rotation; refreshed by Plan 03 proxy on every rotation |

## Deviations from Plan

None — plan executed exactly as written.

Two minor adaptations to match Plan 03's actually-shipped interface (rather than the plan's draft pseudocode):

1. Plan body mentioned `setSessionCookie()` as the helper to call. Plan 03 actually exposes `sessionCookieAttrs()` + `encodeSessionUser()` + a direct `cookies().set(...)` call. `exchange.ts` uses those primitives directly. Functionally identical to a `setSessionCookie` wrapper; matches the codebase reality.
2. The plan's `start-oauth.ts` pseudocode had `args.provider === "apple" ? ... : ...`. My implementation runs the same branch but adds a defensive check at the top of `startOAuthForm` (the FormData wrapper) so a tampered/missing `provider` form field redirects to `/login?error=oauth_denied` rather than crashing. Standard input sanitation; no plan-of-record contract change.

## CLAUDE.md / Project-Convention Adjustments

None — CLAUDE.md's GSD workflow enforcement was already in motion via the orchestrator. The user-email and currentDate globals don't apply to this plan's deliverables (no forms requesting an email default, no date-sensitive content).

## Authentication Gates

None during execution — the OAuth provider credentials (Apple Service ID + Google Web Client ID + corresponding redirect URIs) are not yet registered in the providers' dashboards, but that is an **operator task** documented in the "Provider Dashboard Config Requirements" section above. The landing code itself fail-fast crashes if any of the five new env vars are missing, which is the desired behavior — runtime errors surface configuration drift loudly.

## Verification Evidence

- All Task 1 grep acceptance criteria pass (env.ts has 5 new keys, oauth.ts has form_post + apple/google domains + isSafeNextPath + timingSafeEqual)
- All Task 2 grep acceptance criteria pass ("use server" in start-oauth.ts, COOKIE_NAMES.OAUTH_STATE used, randomB64Url + buildAppleAuthorizeUrl + buildGoogleAuthorizeUrl + startOAuthForm exported; AuthButtonApple + AuthButtonGoogle imported in page.tsx; brand SVG paths correct; h-11 = 44px touch targets on both buttons)
- All Task 3 grep acceptance criteria pass ("use server" in exchange.ts, constantTimeEquals used, backend URL contains `auth/${args.provider}` resolving to `/api/v1/auth/apple` or `/api/v1/auth/google` at runtime + inline doc comment with the literal strings, encodeSessionUser called, decodePlanIdFromJwt called, COOKIE_MAX_AGE.USER used for rv_user, all three session cookies set, oauth_state + oauth_denied error redirects in place, isSafeNextPath gates the redirect, checkout=auto hand-off implemented, POST handler in route.ts)
- `git status` clean after Task 3 commit; three feature commits on the correct base (`e709ffa`)
- Build verification: NOT EXECUTED in this worktree session — the sandbox blocks `npm run build` / `tsc --noEmit` invocations. The orchestrator merge agent will run the build post-merge (Plans 01-03 already build-verified the same set of imports the new code uses: `sessionCookieAttrs`, `COOKIE_NAMES`, `COOKIE_MAX_AGE`, `clearCookieAttrs` from `cookies.ts`; `encodeSessionUser`, `decodePlanIdFromJwt` from `session-cookie.ts`; `env` from `env.ts`; `cookies` from `next/headers`; `redirect` from `next/navigation`). All imports here mirror those proven paths.

## Follow-Up Todos (for /gsd-note or operator)

- **Apple Service ID + Google Web OAuth client registration** — see "Provider Dashboard Config Requirements" above. Required before any real OAuth attempt; the env loader crashes the landing on boot if the values aren't present.
- **T-04-04-04-followup: Backend nonce-claim verification** — Phase 5+ hardening. The landing already plumbs the nonce through; only the backend handler change is needed.
- **W1: Google PKCE + code migration** — Phase 5+ migration target. Implicit flow keeps working today; deprecation window is multi-year.
- **Plan 08 Playwright tests** must exercise:
  - Apple sign-in happy path (with mocked Apple authorize URL + mocked backend `/api/v1/auth/apple`)
  - Google sign-in happy path (with mocked Google authorize URL + mocked backend `/api/v1/auth/google`)
  - State mismatch → `?error=oauth_state` redirect + i18n error rendering
  - Open-redirect attempt (`next=https://evil.com`) → falls through to `/<locale>/dashboard`
  - JWT with `plan_id: "pro"` → rv_user decodes to `{email, planId:"pro"}` (HMAC verifies)
  - Checkout intent hand-off: signed-in mid-pricing → `/<locale>/pricing?...&checkout=auto`

## Known Stubs

None — every value is wired to a real source:

- `startOAuth` generates real CSPRNG csrf + nonce via `node:crypto`'s `randomBytes`
- Authorize URLs are built from `env`-loaded provider config (env crashes if missing — no silent placeholder)
- `completeOAuth` makes a real outbound fetch to the backend and only succeeds on a 2xx with a non-empty `access_token` + `refresh_token`
- `rv_user.planId` comes from a real JWT claim decode (Plan 03's `decodePlanIdFromJwt`) with a real body fallback
- All three session cookies are minted with real HMAC-signed payloads via Plan 03's helpers

The placeholder paragraph in `page.tsx` (`<p>Completing sign-in…</p>`) is rendered only as a defensive fallback if `redirect()` ever fails to throw — in normal operation Next.js converts the throw into a 3xx before any body is sent.

## Threat Flags

None — no new security surface introduced beyond what the plan's `<threat_model>` enumerated (T-04-04-01 through T-04-04-12).

## Self-Check: PASSED

- landing/src/lib/env.ts: FOUND (5 new OAuth vars validated at module load)
- landing/src/lib/oauth.ts: FOUND (server-only, form_post both providers, hard-coded domains, isSafeNextPath, constantTimeEquals)
- landing/.env.example: FOUND (OAuth section with operator hints)
- landing/.env.local.example: FOUND (localhost-tuned OAuth section)
- landing/src/app/[locale]/(app)/login/page.tsx: FOUND
- landing/src/app/[locale]/(app)/login/start-oauth.ts: FOUND ("use server", form wrapper, csrf+nonce, cookie set, provider authorize redirect)
- landing/src/components/app/auth-button-apple.tsx: FOUND (form action, h-11, /brand/apple/apple-sign-in.svg)
- landing/src/components/app/auth-button-google.tsx: FOUND (form action, h-11, /brand/google/google-g.svg)
- landing/src/app/auth/callback/page.tsx: FOUND (GET wrapper, dynamic + nodejs runtime)
- landing/src/app/auth/callback/exchange.ts: FOUND ("use server", state verify, backend exchange, JWT plan_id decode, three cookies set, isSafeNextPath, checkout=auto hand-off)
- landing/src/app/auth/callback/route.ts: FOUND (POST handler, form-urlencoded + multipart parsing, completeOAuth invocation)
- Commit e816ddf (Task 1): FOUND
- Commit 793e73b (Task 2): FOUND
- Commit b8b347d (Task 3): FOUND
