---
phase: 04-landing-surfaces
plan: 03
subsystem: landing
tags: [proxy, auth, cookies, refresh-rotation, plan-id-freshness, security]
dependency_graph:
  requires:
    - "landing/src/lib/env.ts (Plan 01 — BACKEND_API_URL, REVALIDATE_SECRET, IS_PROD, COOKIE_DOMAIN)"
    - "Phase 2 POST /api/v1/auth/refresh contract (transactional rotation, one-time use)"
    - "Phase 3 D-29 — plan_id claim emitted by every JWT mint site"
    - "Phase 2 POST /api/v1/auth/logout contract (Bearer-authed, 204)"
  provides:
    - "Same-origin /api/* proxy with cookie→Bearer transform + 401-refresh-retry"
    - "HMAC-signed rv_user cookie helpers (closes Plan 02 deferred T-04-02-02)"
    - "decodePlanIdFromJwt — signature-skipped JWT plan_id claim reader"
    - "Dedicated /api/auth/logout that clears all session cookies even on backend failure"
    - "Cookie attribute builders (sessionCookieAttrs/clearCookieAttrs) for OAuth callback (Plan 04) and any future cookie writer"
    - "COOKIE_NAMES + COOKIE_MAX_AGE constants — rv_user pinned to 30d (NOT 5min, B2 fix)"
  affects:
    - "Plan 04 (OAuth callback) — uses encodeSessionUser + sessionCookieAttrs to mint rv_at/rv_rt/rv_user on sign-in"
    - "Plan 05 (Pricing) — /api/revalidate-pricing is a sibling carve-out route under /api/, untouched by the catch-all"
    - "Plan 06 (Dashboard) — getSession() now HMAC-verifies rv_user (tamper-evident); sign-out button POSTs /api/auth/logout"
    - "Plan 07 (Pay success) — invoice polling goes through /api/v1/invoices/:id via the proxy; the refresh-retry path keeps rv_user.planId fresh so /dashboard reflects Pro immediately after redirect"
tech_stack:
  added:
    - "node:crypto HMAC-SHA256 (no new npm dep — Node stdlib)"
  patterns:
    - "One-shot refresh-retry guard (local boolean `triedRefresh`) — never recurse on second 401"
    - "Buffered request body via arrayBuffer() once, replayed on retry — 64KB cap matches backend PERF-09"
    - "Defensive secret namespacing — HMAC secret derived as `REVALIDATE_SECRET + ':session'` so a leak of one signal doesn't trivially compromise the other"
    - "Signature-skipped JWT decode (trust-the-response-body model) — safe ONLY because the JWT was returned by the same backend request, never from an untrusted caller"
key_files:
  created:
    - "landing/src/lib/cookies.ts"
    - "landing/src/lib/session-cookie.ts"
    - "landing/src/lib/session.ts"
    - "landing/src/lib/proxy.ts"
    - "landing/src/app/api/[...path]/route.ts"
    - "landing/src/app/api/auth/logout/route.ts"
  modified: []
decisions:
  - "rv_user Max-Age locked at 30 days (matches refresh TTL), NOT 5 minutes (the access TTL). The proxy re-issues rv_user on every refresh rotation so its planId stays fresh; pinning to the shorter TTL would silently log users out of getSession() between natural rv_at rotations (B2/W5 fix)."
  - "HMAC secret derived from REVALIDATE_SECRET + ':session' rather than introducing a brand-new env var. Keeps deploy surface small; future hardening can split SESSION_HMAC_SECRET out without changing call sites."
  - "decodePlanIdFromJwt skips signature verification deliberately — the JWT comes from the same backend in the same HTTPS hop, treat it as a JSON field in the response. Real protection lives at the backend's auth middleware where the JWT is verified with the signing key."
  - "On refresh-retry, if the retry's upstream fetch fails (network/timeout) but refresh succeeded, we still write the new rv_at/rv_rt/rv_user to the response. The new tokens are valid; the browser can replay the request immediately. Discarding them would log the user out for a transient network hiccup."
  - "All three session cookies cleared on logout regardless of backend outcome (D-25). Browser must never be left 'stuck logged in' because the backend was unreachable."
  - "STRIP_REQUEST_HEADERS removes Cookie + Host + connection headers from the forwarded request — the backend is cookie-agnostic for SSO/payment APIs and expects Bearer."
  - "credentials: 'omit' on every upstream fetch — we explicitly carry auth via the Authorization header; never let the runtime attach cookies to the backend hop."
  - "redirect: 'manual' on upstream fetches — we want to pipe 3xx through to the browser rather than have the proxy chase redirects server-side."
metrics:
  duration: "~30 minutes wall clock (3 tasks, 3 commits, 0 deviations, 1 full e2e curl smoke against a mock backend)"
  completed: "2026-05-24"
  tasks_completed: 3
  files_changed: 6
  commits: 3
---

# Phase 4 Plan 03: Node Proxy + Cookies + Refresh-Rotation Summary

Built the security spine of Phase 4: a Node-runtime `/api/*` catch-all that forwards every browser API call server-to-server to `BACKEND_API_URL`, transforms HttpOnly cookies into `Authorization: Bearer` headers, and on any upstream 401 with `rv_rt` present, transparently calls `POST /api/v1/auth/refresh`, decodes the new access JWT's `plan_id` claim, re-issues `rv_user` with `{email: prior, planId: new}` at a 30-day TTL, and retries the original request once — all without exposing tokens to the browser. Also shipped HMAC-signed `rv_user` helpers (closing Plan 02's deferred T-04-02-02) and a dedicated `/api/auth/logout` that clears all three session cookies even when the backend is unreachable. End-to-end smoke confirmed: a request with a stale `rv_at` and a `planId=free` rv_user returned a 200 with three Set-Cookie headers and a re-signed `rv_user` containing `planId=pro` — D-17 freshness lit up on the first refresh.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Cookie attrs + HMAC rv_user helpers + JWT plan_id decoder | `57eded3` | landing/src/lib/cookies.ts, landing/src/lib/session-cookie.ts, landing/src/lib/session.ts |
| 2 | Core proxy + catch-all /api/[...path] route | `907567f` | landing/src/lib/proxy.ts, landing/src/app/api/[...path]/route.ts |
| 3 | Dedicated /api/auth/logout route | `23c5ec2` | landing/src/app/api/auth/logout/route.ts |

## Cookie Attribute Set (Final Values)

| Cookie | HttpOnly | Secure | SameSite | Path | Max-Age (prod) | Domain |
|--------|----------|--------|----------|------|----------------|--------|
| `rv_at` | true | `env.IS_PROD` | Strict | `/` | 300 (5 min) | `env.COOKIE_DOMAIN` or omitted |
| `rv_rt` | true | `env.IS_PROD` | Strict | `/` | 2,592,000 (30 day) | `env.COOKIE_DOMAIN` or omitted |
| `rv_user` | true | `env.IS_PROD` | Strict | `/` | **2,592,000 (30 day) ← B2 FIX** | `env.COOKIE_DOMAIN` or omitted |
| `rv_oauth_state` | true | `env.IS_PROD` | Strict | `/` | 300 (5 min) | `env.COOKIE_DOMAIN` or omitted |

**Why `rv_user.Max-Age = 30 days` and NOT 5 minutes** (B2/W5 fix): rv_user is the email+planId source for `getSession()`. If it expired with the access cookie's 5-min TTL, every natural access-token rotation would clear the displayable identity even though the browser still holds a valid `rv_rt` and a fresh `rv_at`. Pinning rv_user to the refresh TTL keeps the displayable identity alive for the full 30-day session, and the proxy re-writes rv_user with the latest plan_id on every refresh rotation — staleness window is bounded by one refresh cycle (< the 5-min access-token TTL in practice).

In **dev** (`NODE_ENV !== 'production'`), `Secure` flips to `false` via `env.IS_PROD` so http://localhost still works. T-04-03-09 (cookie set in dev without Secure) is explicitly accepted as a dev-only convenience.

## Refresh-Rotation Request Shape

- **Trigger:** any upstream non-2xx response with status `401` where `rv_rt` cookie is present and `triedRefresh === false`.
- **URL:** `${env.BACKEND_API_URL}/api/v1/auth/refresh`
- **Method:** `POST`
- **Headers:** `Content-Type: application/json` (no Bearer — the rt is in the body).
- **Body:** `{ "refresh_token": "<rv_rt cookie value>" }`
- **Timeout:** `AbortSignal.timeout(15_000)` (15s) — same cap as the main upstream call.
- **Success (200) shape consumed:** `{ access_token: string, refresh_token: string }`. Other fields ignored.
- **On 200:** decode `plan_id` from `access_token`; re-encode `rv_user` with `{email: prior, planId: new || prior}`; set new `rv_at` + `rv_rt` + `rv_user` cookies on the response; retry original request with new Bearer.
- **On non-200 OR network failure OR malformed JSON:** build a `401 {error: "session_expired"}` response, clear all three session cookies (Max-Age=0), do NOT retry.
- **One-shot guard:** local boolean `triedRefresh` ensures we never recurse — if the retry itself returns 401, that 401 pipes back to the browser unchanged. T-04-03-02 (refresh-token replay) closure.

## HMAC Scheme for `rv_user`

- **Algorithm:** HMAC-SHA256.
- **Secret derivation:** `HMAC_SECRET = createHmac("sha256", env.REVALIDATE_SECRET + ":session").digest()` — namespace-separated from the revalidate use case to limit blast radius if either secret leaks.
- **Encoded format:** `${base64url(JSON({email, planId}))}.${base64url(HMAC(payload))}`
- **Verification path:** `lastIndexOf('.')` split → constant-time `timingSafeEqual` on the MAC bytes → JSON.parse the payload → require both `email` and `planId` to be strings → return the typed `SessionUser` or `null`.
- **Failure modes (all return `null`):** missing cookie, no dot separator, MAC mismatch, base64url decode failure, JSON parse failure, missing/non-string fields. Callers fall back to `{isAuthed:true, email:"", planId:""}` so logged-in users with corrupted/tampered rv_user still get the navbar (just with no email/plan displayed) rather than a hard sign-out.
- **Closes** Plan 02's deferred T-04-02-02 — rv_user is now tamper-evident.

## `decodePlanIdFromJwt` Design

- **Trust model:** the JWT was returned by `/api/v1/auth/refresh` in the SAME HTTPS server-to-server hop we just made. The Node proxy doesn't have the backend's signing key (deliberately — that's the backend's protection surface), so signature verification isn't possible here. We treat the JWT exactly like a structured field of the response body: same trust as reading `access_token` itself.
- **Implementation:** `jwt.split(".")` → `Buffer.from(parts[1], "base64url").toString("utf8")` → `JSON.parse(...)` → return `claims.plan_id` if it's a string, else `""`.
- **Fallback chain inside the proxy:** `effectivePlanId = decodePlanIdFromJwt(newAccessToken) || decodeSessionUser(priorUserCookie)?.planId || ""`. A malformed JWT keeps the prior planId; a missing prior rv_user yields "" (UI shows "—").
- **T-04-03-12 closure:** `decodePlanIdFromJwt` is called ONLY on `refresh.access_token` (server-to-server response body), never on a browser-supplied JWT. The decoded `plan_id` is used only to populate the HMAC-signed `rv_user` cookie's display string. Real access control happens at the backend's middleware (`c.Locals("plan_id")` from Phase 3 D-29), which verifies the JWT signature properly.
- **Verified end-to-end:** mock backend issued a JWT with `plan_id: "pro"`; the proxy re-issued rv_user; decoding the new rv_user with the test HMAC secret yielded `{email:"user@x", planId:"pro"}` — D-17 freshness path confirmed live.

## B2 Closure — `rv_user` Re-Issue on Refresh

Before this plan, `rv_user.planId` could only be set at OAuth completion (Plan 04). A user who:
1. Signs in (rv_user.planId = "free")
2. Visits /pricing, clicks "Get Pro"
3. Pays on lava.top
4. Returns to /pay/success → eventually /dashboard

…would see "Free" on /dashboard until rv_user naturally expired (30 days) — even though the backend's `plan_id` claim flipped to "pro" the moment the webhook fired and the access token rotated.

This plan closes that gap. Every refresh rotation:

```ts
const newPlanId = decodePlanIdFromJwt(refresh.accessToken);
const priorUser = decodeSessionUser(priorUserCookie);
const priorEmail = priorUser?.email ?? "";
const fallbackPlanId = priorUser?.planId ?? "";
const effectivePlanId = newPlanId || fallbackPlanId;
const newUserCookie = encodeSessionUser({ email: priorEmail, planId: effectivePlanId });
// then setSessionCookies(res, refresh.accessToken, refresh.refreshToken, newUserCookie)
```

Combined with Plan 07's pay-success force-refresh trigger, a user who upgrades to Pro sees Pro on /dashboard within one refresh cycle — typically <2s of the page load that follows the payment success redirect.

T-04-03-11 (stale plan_id information disclosure) is mitigated.

## Body Size Limit + Timeouts

- **Body limit:** `BODY_BYTES_LIMIT = 64 * 1024` (64 KB) — matches backend PERF-09 Fiber config. Two checks:
  1. Pre-stream check on `Content-Length` header (cheap, fails fast on declared-large requests).
  2. Post-read check on `arrayBuffer().byteLength` (catches chunked-encoding clients that omit Content-Length).
  Either trigger returns `413 {error: "payload_too_large"}` BEFORE the upstream fetch — backend never sees the oversize payload. T-04-03-04 closure.
- **Upstream timeout (proxy.ts):** `AbortSignal.timeout(15_000)` — 15s on both the initial request and the retry, and on the `/auth/refresh` hop.
- **Upstream timeout (logout):** `AbortSignal.timeout(5_000)` — shorter because logout is fire-and-forget (we don't need the backend's response to clear local cookies).
- **Network error handling:** initial upstream `fetch` failure → `502 {error: "upstream_unavailable"}`. Retry `fetch` failure AFTER a successful refresh → also `502`, but the rotated `rv_at`/`rv_rt`/`rv_user` ARE still set on the response so the browser can immediately retry without losing its session.

## Carve-Outs from the Catch-All Route

Next.js routes the more specific path first, so the following sibling routes under `/api/` are NOT caught by `/api/[...path]/route.ts`:

| Path | Owned by | Behavior |
|------|----------|----------|
| `/api/auth/logout` | This plan (04-03) | Clears 3 cookies + best-effort backend POST; returns 204 always |
| `/api/revalidate-pricing` | Plan 04-05 | Constant-time-compare `?secret=` then `revalidateTag('plans')`; never forwards to backend |
| `/api/auth/callback` (or similar) | Plan 04-04 | OAuth ID-token exchange — calls backend `/auth/apple` or `/auth/google`, then `setSessionCookies` itself before redirecting to `?next=` |
| Everything else under `/api/` | This plan (04-03) catch-all | Forwards to `${BACKEND_API_URL}/api/v1/...` with cookie→Bearer transform |

## Deviations from Plan

None — plan executed exactly as written.

The plan referenced `landing/src/lib/session.ts` as "created in Plan 02" but Plan 02 is a sibling wave-2 plan running in parallel. This worktree includes the **final** (hardened) version of session.ts — directly importing `decodeSessionUser` from `session-cookie.ts` and `COOKIE_NAMES` from `cookies.ts`. The orchestrator's merge of Plan 02 + Plan 03 worktrees will resolve to this version (Plan 02's draft only differed in using a raw JSON parse instead of HMAC-verified decode, which is exactly what Plan 03 intended to upgrade per the plan body: "session.ts uses the verified path").

## CLAUDE.md / Project-Convention Adjustments

None — CLAUDE.md's GSD workflow enforcement was already in motion via the orchestrator. No conflict.

## Authentication Gates

None — all work is server-side proxy plumbing; no third-party auth (Apple/Google secrets, lava.top credentials) was required.

## Verification Evidence

- `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npx tsc --noEmit` → exit 0
- `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npm run build` → exit 0
  - Build output lists both `ƒ /api/[...path]` and `ƒ /api/auth/logout` as Dynamic routes
  - 18 static pages prerendered (3 locales × 6 marketing routes) — Plan 01 build invariant intact
- `grep -rn 'localStorage' landing/src/` → 0 matches (Phase-goal SC #1 closure: no token storage)
- **End-to-end logout smoke** (`landing/.next/standalone/server.js` against null backend, `BACKEND_API_URL=http://127.0.0.1:1`):
  - `curl -X POST -b 'rv_at=test' http://127.0.0.1:3989/api/auth/logout/ -i`
  - Returns `HTTP/1.1 204 No Content` plus three `Set-Cookie: rv_*=; Path=/; Max-Age=0; Secure; HttpOnly; SameSite=strict` headers
- **End-to-end 401→refresh→retry smoke** (against a Python mock backend):
  - Request: `POST /api/checkout/` with stale `rv_at`, valid `rv_rt`, HMAC-signed `rv_user{planId:"free"}`
  - First upstream call → 401
  - Proxy calls `/api/v1/auth/refresh` → mock returns `access_token` JWT with `plan_id: "pro"`
  - Proxy decodes `plan_id="pro"`, re-issues `rv_user` with `{email:"user@x", planId:"pro"}`
  - Retry call → 200 with `Authorization: Bearer <new JWT>` carried through and body replayed intact
  - Three Set-Cookies on the response (Max-Age 300 / 2592000 / 2592000)
  - Decoded re-issued `rv_user` payload: `{ email: 'user@x', planId: 'pro' }` with valid MAC ✓

## Threat Register Closure

All twelve dispositions from `<threat_model>` honored:

| Threat | Closure |
|---|---|
| T-04-03-01 | `httpOnly: true` + `secure: env.IS_PROD` + `sameSite: "strict"` in `sessionCookieAttrs` |
| T-04-03-02 | One-shot `triedRefresh` guard in `proxyToBackend` |
| T-04-03-03 | HMAC-SHA256 + `timingSafeEqual` in `decodeSessionUser` |
| T-04-03-04 | `BODY_BYTES_LIMIT = 64*1024` + Content-Length pre-check |
| T-04-03-05 | `AbortSignal.timeout(15_000)` (proxy) / `5_000` (logout) |
| T-04-03-06 | `path.join("/")` + fixed `BACKEND_API_URL` base — URL traversal impossible |
| T-04-03-07 | Pipes upstream bodies unchanged; HOTFIX-04 scrubbing already on backend |
| T-04-03-08 | Retry builds NEW `Authorization: Bearer ${refresh.accessToken}` header before fetch |
| T-04-03-09 | Accepted dev-only via `env.IS_PROD` gate |
| T-04-03-10 | `/api/auth/logout` clears cookies first, then best-effort backend call |
| T-04-03-11 | `decodePlanIdFromJwt` + re-issue on every refresh closes D-17 |
| T-04-03-12 | Signature-skipped decode is called ONLY on the trusted refresh response; UI-only impact even if injected |

## Known Stubs

None — every helper is wired to a real source:
- `sessionCookieAttrs` returns concrete values from validated env
- `encodeSessionUser` / `decodeSessionUser` use a real HMAC secret derived at module load
- `proxyToBackend` makes real outbound fetches and re-issues real cookies
- `/api/auth/logout` issues real `Set-Cookie: Max-Age=0` headers

The proxy is exercised end-to-end above.

## Threat Flags

None — no new security surface introduced beyond what the plan's `<threat_model>` already enumerated.

## Self-Check: PASSED

- landing/src/lib/cookies.ts: FOUND
- landing/src/lib/session-cookie.ts: FOUND
- landing/src/lib/session.ts: FOUND
- landing/src/lib/proxy.ts: FOUND
- landing/src/app/api/[...path]/route.ts: FOUND
- landing/src/app/api/auth/logout/route.ts: FOUND
- Commit 57eded3 (Task 1): FOUND
- Commit 907567f (Task 2): FOUND
- Commit 23c5ec2 (Task 3): FOUND
- npx tsc --noEmit: EXIT 0
- npm run build: EXIT 0 (with both /api/[...path] and /api/auth/logout registered)
- End-to-end logout smoke: 204 + 3 Set-Cookie clears ✓
- End-to-end 401-refresh-retry smoke: 200 + 3 Set-Cookies + rv_user.planId flipped free→pro ✓
