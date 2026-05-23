---
phase: 04-landing-surfaces
plan: 03
type: execute
wave: 2
depends_on: [04-01]
files_modified:
  - landing/src/lib/cookies.ts
  - landing/src/lib/proxy.ts
  - landing/src/lib/session-cookie.ts
  - landing/src/app/api/[...path]/route.ts
  - landing/src/app/api/auth/logout/route.ts
autonomous: true
requirements:
  - WEB-02
must_haves:
  truths:
    - "Browser never calls the backend directly — every `/api/*` request on the landing is server-proxied to BACKEND_API_URL"
    - "Sign-in success sets `rv_at`, `rv_rt`, `rv_user` cookies with HttpOnly + Secure + SameSite=Strict + Path=/"
    - "rv_user cookie Max-Age matches the refresh-token lifetime (30d) — NOT the 5-min access-token lifetime — so getSession() keeps returning email/planId across natural access-token rotations"
    - "When a proxied request returns 401 and `rv_rt` is present, the proxy auto-refreshes via POST /api/v1/auth/refresh and retries once; on success the proxy decodes plan_id from the NEW rv_at JWT and RE-ISSUES rv_user with {email: prior email, planId: new plan_id from JWT}"
    - "Sign-out clears all three session cookies (`Max-Age=0`) and returns 204"
    - "No JWT or refresh token is ever written to localStorage, sessionStorage, or a JS-readable cookie (HttpOnly only)"
  artifacts:
    - path: "landing/src/lib/cookies.ts"
      provides: "cookie attribute builder — HttpOnly Secure SameSite=Strict Path=/ Domain"
      exports: ["sessionCookieAttrs", "clearCookieAttrs"]
    - path: "landing/src/lib/session-cookie.ts"
      provides: "HMAC-signed rv_user cookie serialiser/parser + JWT plan_id decoder"
      exports: ["encodeSessionUser", "decodeSessionUser", "decodePlanIdFromJwt"]
    - path: "landing/src/lib/proxy.ts"
      provides: "core proxy function — forwards body, headers, handles 401→refresh→retry, re-mints rv_at AND rv_user cookies"
      exports: ["proxyToBackend"]
    - path: "landing/src/app/api/[...path]/route.ts"
      provides: "catch-all /api/* proxy — supports GET/POST/PATCH/PUT/DELETE"
      exports: ["GET", "POST", "PATCH", "PUT", "DELETE"]
    - path: "landing/src/app/api/auth/logout/route.ts"
      provides: "logout endpoint — calls backend, clears cookies, returns 204"
      exports: ["POST"]
  key_links:
    - from: "landing/src/app/api/[...path]/route.ts"
      to: "landing/src/lib/proxy.ts"
      via: "proxyToBackend(req, params.path)"
      pattern: "proxyToBackend\\("
    - from: "landing/src/lib/proxy.ts"
      to: "BACKEND_API_URL"
      via: "import { env } from '@/lib/env'"
      pattern: "env\\.BACKEND_API_URL"
    - from: "landing/src/lib/proxy.ts"
      to: "rv_at / rv_rt / rv_user cookies"
      via: "cookies().get() + Authorization: Bearer + decodePlanIdFromJwt(newAccessToken) → re-encode rv_user"
      pattern: "rv_at|rv_rt|rv_user|decodePlanIdFromJwt"
tags: [proxy, auth, cookies, refresh-rotation, plan-id-freshness]
---

<objective>
Build the Node-runtime proxy that all browser API calls go through (D-07). It (a) forwards `/api/*` requests server-to-server to BACKEND_API_URL, (b) reads HttpOnly cookies `rv_at` / `rv_rt` and converts them to `Authorization: Bearer <access>` headers, (c) on any 401 with a `rv_rt` cookie present, calls `POST /api/v1/auth/refresh` once, updates the `rv_at` cookie, decodes `plan_id` from the new access-token JWT (Phase 3 D-29 claim), re-issues the `rv_user` cookie so `getSession()` reflects the user's CURRENT plan (closes D-17), and retries the original request (D-09), and (d) on `/api/auth/logout`, forwards the call to the backend's logout endpoint and clears all three cookies (D-25). Also provide HMAC-signed `rv_user` cookie helpers + a JWT `plan_id` decoder so OAuth callback (Plan 04) and pay/success (Plan 07) can persist + refresh `{email, planId}` securely (closing T-04-02-02 from Plan 02).

Purpose: this is the security spine of Phase 4 AND the freshness gate for D-17. Without it, the OAuth callback can't set cookies, the dashboard can't read its user, the pricing page can't checkout, pay/success can't poll, AND a user who upgrades to Pro on lava.top would continue seeing "Free" on /dashboard for up to 30 days (until rv_user naturally expires) because nothing re-issues rv_user with the new plan_id. WEB-02 ("HttpOnly cookies, no localStorage") is fully owned here; the rv_user freshness path is what makes "land on /pay/success with Pro already active" actually true.

Output: every Phase 4 client component can `fetch("/api/v1/...")` and get an authenticated, refresh-rotating call without touching tokens directly. The landing page bundle contains zero token storage. Every refresh rotation propagates the latest plan_id into rv_user so server-side reads via getSession() are accurate.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/04-landing-surfaces/04-CONTEXT.md
@.planning/phases/04-landing-surfaces/04-01-foundation-i18n-standalone-PLAN.md
@.planning/phases/04-landing-surfaces/04-02-app-shell-navbar-primitives-PLAN.md
@.planning/phases/02-auth-sso-backend/02-CONTEXT.md
@.planning/phases/03-lava-top-plans-catalog/03-05-checkout-cancel-invoices-admin-lava-proxy-SUMMARY.md
@.planning/phases/03-lava-top-plans-catalog/03-07-public-plans-jwt-cache-SUMMARY.md
@landing/src/lib/env.ts

<interfaces>
<!-- Locked CONTEXT.md decisions -->
- D-07: same-origin proxy. Browser hits `/api/...` on landing; landing forwards to `${BACKEND_API_URL}/api/v1/...`. NEVER expose backend domain to client bundles.
- D-08: cookie names exact. Cookie attributes:
  - rv_at: HttpOnly; Secure; SameSite=Strict; Path=/; Max-Age=300  (5 min, matches backend access TTL)
  - rv_rt: HttpOnly; Secure; SameSite=Strict; Path=/; Max-Age=2592000  (30 day, matches backend refresh TTL)
  - rv_user: HttpOnly; Secure; SameSite=Strict; Path=/; Max-Age=2592000  (30 day — matches REFRESH token, NOT access. See B2/W5 fix below.)
  - Domain attribute: set from env.COOKIE_DOMAIN when non-empty; omit otherwise (host-only)
  - Secure: ALWAYS true in production. In dev (NODE_ENV !== 'production'), Secure may be omitted to allow http://localhost — wrap via env.IS_PROD.
- D-09: 401 → POST /api/v1/auth/refresh with rv_rt as cookie + Authorization. On 200: update rv_at, RE-ISSUE rv_user with new plan_id from JWT, retry original once. On non-200: clear all three cookies, return 401 to browser. Never retry more than once per request.
- D-17: rv_user.planId is the source of truth for /dashboard's plan display. It is ALWAYS refreshed at the same time as rv_at (at OAuth completion in Plan 04, and at every refresh-rotation here). The plan_id value comes from decoding the JWT's `plan_id` claim (Phase 3 D-29, confirmed shipped per `.planning/phases/03-lava-top-plans-catalog/03-07-public-plans-jwt-cache-SUMMARY.md`: "Plan_id backfill at all JWT mint sites" + middleware `c.Locals("plan_id")`).

**rv_user Max-Age rationale (B2/W5 fix):**
Previously rv_user used the 5-min access-token Max-Age, which meant any natural cookie expiry between user navigations would silently log the user out of getSession() (browser still has rv_rt and rv_at, but getSession() reads rv_user). Set rv_user Max-Age to match `rv_rt` (30d). The rv_user content is refreshed at every refresh rotation (this plan) and at OAuth completion (Plan 04), so it stays in sync with the actual session lifetime.

Backend contracts (confirmed via Phase 2/3 SUMMARYs):
- POST /api/v1/auth/refresh — accepts `{refresh_token: string}` JSON body. Returns `{access_token, refresh_token, expires_in, ...}`. Refresh-token rotation is transactional (HOTFIX-05) and one-time-use. Returned access_token is a signed JWT whose payload includes the `plan_id` claim (Phase 3 D-29 / 03-07 SUMMARY, "generateTokens signature extended with planID — emits 'plan_id' claim").
- POST /api/v1/auth/logout — requires Bearer access token; deletes the refresh-session row and blacklists the access token. Returns 204.

The path-prefix mapping:
- Browser → `/api/v1/checkout` → Node proxy at `/api/v1/checkout` → forwards to `${BACKEND_API_URL}/api/v1/checkout`
- Browser → `/api/auth/logout` → Node proxy at `/api/auth/logout` (a dedicated route, not the catch-all) so it can clear cookies before responding
- Browser → `/api/revalidate-pricing` → Node proxy at `/api/revalidate-pricing` (Plan 05; NOT a backend forward; locally handled)

HMAC for rv_user cookie:
- Algo: HMAC-SHA256
- Secret: derived from REVALIDATE_SECRET via `crypto.createHmac("sha256", env.REVALIDATE_SECRET + ":session").digest()` — reuses existing secret, separately namespaced. Future hardening can split SESSION_HMAC_SECRET into its own var.
- Format: `${base64url(json)}.${base64url(hmac)}`
- Verify: constant-time compare with `crypto.timingSafeEqual`

JWT plan_id decode (B2 fix):
- Server-side ONLY (Node proxy). Decode WITHOUT verifying signature — we trust the access_token because the backend just minted it for us in the same HTTPS request. Signature verification is for protecting endpoints that accept JWTs from untrusted callers; here the JWT IS the response body.
- Implementation: split on '.', base64url-decode the middle segment, JSON.parse, read `plan_id` claim. Defensive — if claim missing OR parse fails, fall back to the prior rv_user.planId rather than crash.
</interfaces>
</context>

<tasks>

<task type="auto" tdd="false">
  <name>Task 1: Cookie attribute builder + HMAC-signed rv_user cookie helpers + JWT plan_id decoder</name>
  <files>landing/src/lib/cookies.ts, landing/src/lib/session-cookie.ts</files>
  <read_first>
    - landing/src/lib/env.ts (env.IS_PROD, env.COOKIE_DOMAIN, env.REVALIDATE_SECRET)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-08, D-12, D-17)
    - landing/src/lib/session.ts (created in Plan 02 — getSession reads rv_user)
    - .planning/phases/03-lava-top-plans-catalog/03-07-public-plans-jwt-cache-SUMMARY.md (confirms plan_id claim is in every JWT)
  </read_first>
  <action>
    Create landing/src/lib/cookies.ts (server-only):
    ```ts
    import "server-only";
    import type { ResponseCookie } from "next/dist/compiled/@edge-runtime/cookies";
    import { env } from "./env";

    type CookieMaxAge = number;

    export function sessionCookieAttrs(maxAge: CookieMaxAge): Omit<ResponseCookie, "name" | "value"> {
      return {
        httpOnly: true,
        secure: env.IS_PROD,        // dev allows http://localhost
        sameSite: "strict",
        path: "/",
        maxAge,
        ...(env.COOKIE_DOMAIN ? { domain: env.COOKIE_DOMAIN } : {}),
      };
    }

    export function clearCookieAttrs(): Omit<ResponseCookie, "name" | "value"> {
      return { ...sessionCookieAttrs(0), maxAge: 0 };
    }

    export const COOKIE_NAMES = Object.freeze({
      ACCESS: "rv_at",
      REFRESH: "rv_rt",
      USER: "rv_user",
      OAUTH_STATE: "rv_oauth_state",   // used by Plan 04
    });

    // NOTE on USER Max-Age (B2 fix, W5 mitigation):
    // rv_user must survive natural rv_at rotation (every 5 min) — it is the email+planId
    // source for getSession(). Pinning rv_user to the REFRESH token's TTL means
    // getSession() keeps returning the user's email + plan for the full session,
    // and is RE-WRITTEN by the proxy on every refresh-rotation so its planId stays
    // current with the JWT's plan_id claim.
    export const COOKIE_MAX_AGE = Object.freeze({
      ACCESS: 60 * 5,                // 5 min
      REFRESH: 60 * 60 * 24 * 30,    // 30 day
      USER: 60 * 60 * 24 * 30,       // 30 day — matches REFRESH (B2 fix)
      OAUTH_STATE: 60 * 5,           // 5 min (Plan 04)
    });
    ```

    Create landing/src/lib/session-cookie.ts (server-only):
    ```ts
    import "server-only";
    import { createHmac, timingSafeEqual } from "node:crypto";
    import { env } from "./env";

    export type SessionUser = { email: string; planId: string };

    const HMAC_SECRET = createHmac("sha256", env.REVALIDATE_SECRET + ":session").digest();

    function b64url(buf: Buffer | string): string {
      const b = typeof buf === "string" ? Buffer.from(buf, "utf8") : buf;
      return b.toString("base64url");
    }

    export function encodeSessionUser(u: SessionUser): string {
      const payload = b64url(JSON.stringify({ email: u.email, planId: u.planId }));
      const mac = b64url(createHmac("sha256", HMAC_SECRET).update(payload).digest());
      return `${payload}.${mac}`;
    }

    export function decodeSessionUser(raw: string | undefined): SessionUser | null {
      if (!raw) return null;
      const dot = raw.lastIndexOf(".");
      if (dot < 1) return null;
      const payload = raw.slice(0, dot);
      const mac = raw.slice(dot + 1);
      const expected = b64url(createHmac("sha256", HMAC_SECRET).update(payload).digest());
      const a = Buffer.from(mac, "base64url");
      const b = Buffer.from(expected, "base64url");
      if (a.length !== b.length || !timingSafeEqual(a, b)) return null;
      try {
        const json = JSON.parse(Buffer.from(payload, "base64url").toString("utf8"));
        if (typeof json.email !== "string" || typeof json.planId !== "string") return null;
        return { email: json.email, planId: json.planId };
      } catch {
        return null;
      }
    }

    /**
     * B2 FIX — decode the `plan_id` claim from a backend-issued JWT (rv_at) without
     * verifying the signature. We do NOT verify because:
     *   (a) the JWT came from the same backend request we just made (HTTPS, trusted hop)
     *   (b) signature verification needs the backend's secret key, which the landing
     *       Node deliberately does not have — that's the backend's protection surface
     *   (c) treat this exactly as "parse a structured response field" — same trust model
     *       as reading the JSON body's other fields
     * Returns "" on any parse failure so callers fall back to prior rv_user.planId.
     * Phase 3 D-29 / 03-07 SUMMARY confirms every JWT mint site (AppleSignIn,
     * GoogleSignIn, AdminLogin, refresh, GuestLogin, LinkDevice) emits this claim.
     */
    export function decodePlanIdFromJwt(jwt: string | undefined): string {
      if (!jwt) return "";
      const parts = jwt.split(".");
      if (parts.length < 2) return "";
      try {
        const payloadJson = Buffer.from(parts[1], "base64url").toString("utf8");
        const claims = JSON.parse(payloadJson);
        if (typeof claims?.plan_id === "string") return claims.plan_id;
        return "";
      } catch {
        return "";
      }
    }
    ```

    Then update landing/src/lib/session.ts (created in Plan 02) to use `decodeSessionUser` from this new module instead of the trust-the-cookie JSON parse. The `getSession()` signature is unchanged — only the body is hardened. This closes T-04-02-02 from Plan 02.
  </action>
  <acceptance_criteria>
    - `grep -n 'import "server-only"' landing/src/lib/cookies.ts` returns 1 match
    - `grep -n "httpOnly: true" landing/src/lib/cookies.ts` returns 1 match
    - `grep -n 'sameSite: "strict"' landing/src/lib/cookies.ts` returns 1 match
    - `grep -n 'rv_at\|rv_rt\|rv_user\|rv_oauth_state' landing/src/lib/cookies.ts` returns at least 4 matches
    - `grep -n 'USER: 60 \* 60 \* 24 \* 30' landing/src/lib/cookies.ts` returns 1 match (B2 fix — rv_user gets 30-day TTL, NOT 5-min)
    - `grep -n 'timingSafeEqual' landing/src/lib/session-cookie.ts` returns 1 match
    - `grep -n 'createHmac' landing/src/lib/session-cookie.ts` returns at least 2 matches
    - `grep -n 'decodePlanIdFromJwt\|plan_id' landing/src/lib/session-cookie.ts` returns at least 2 matches
    - `grep -n 'decodeSessionUser' landing/src/lib/session.ts` returns 1 match (session.ts updated)
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npx tsc --noEmit` exits 0
  </acceptance_criteria>
  <done>Cookie attribute builders return the locked attribute set, rv_user HMAC sign/verify works with constant-time compare, rv_user Max-Age = 30 days (matches refresh), and decodePlanIdFromJwt parses the plan_id claim from any backend-issued access JWT. session.ts uses the verified path.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 2: Core proxy with 401→refresh→retry + rv_user RE-ISSUE on refresh + catch-all /api/[...path] route</name>
  <files>landing/src/lib/proxy.ts, landing/src/app/api/[...path]/route.ts</files>
  <read_first>
    - landing/src/lib/cookies.ts (created Task 1)
    - landing/src/lib/session-cookie.ts (created Task 1 — decodePlanIdFromJwt, encodeSessionUser, decodeSessionUser)
    - landing/src/lib/env.ts
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-07, D-09, D-17)
    - .planning/phases/02-auth-sso-backend/02-CONTEXT.md (refresh-token rotation contract)
    - .planning/phases/03-lava-top-plans-catalog/03-05-checkout-cancel-invoices-admin-lava-proxy-SUMMARY.md (backend response shapes)
    - .planning/phases/03-lava-top-plans-catalog/03-07-public-plans-jwt-cache-SUMMARY.md (plan_id claim in every JWT)
  </read_first>
  <action>
    Create landing/src/lib/proxy.ts (server-only) — exports `proxyToBackend(req: NextRequest, segments: string[]): Promise<NextResponse>`:

    Algorithm:
    1. Build upstream URL: `${env.BACKEND_API_URL}/api/v1/${segments.join("/")}` + original query string.
    2. Read cookies: `const jar = await cookies(); const at = jar.get("rv_at")?.value; const rt = jar.get("rv_rt")?.value; const priorUserCookie = jar.get("rv_user")?.value;`
    3. Build forward headers: copy `Content-Type`, `Accept-Language`, `User-Agent` from request. ADD `Authorization: Bearer ${at}` when `at` is present. DO NOT forward `Cookie:` to the backend (the backend is cookie-agnostic for SSO/payment APIs — it expects Bearer).
    4. Build forward body: buffer `req.body` via `arrayBuffer()` once at step 4 (max BODY_BYTES_LIMIT = 64*1024; reject larger with 413). For GET/HEAD/DELETE pass `undefined`. Buffering required so the body can be replayed on the retry path.
    5. Execute fetch with `signal: AbortSignal.timeout(15000)` (15s upstream cap).
    6. On non-401 status: pipe response back unchanged. Copy `Content-Type`, `Cache-Control` headers. Return NextResponse with status, body, and copied headers.
    7. On 401 AND `rt` is present AND we haven't already retried (recursion guard local var `triedRefresh = false`):
       a. Set `triedRefresh = true`. Call `${env.BACKEND_API_URL}/api/v1/auth/refresh` with JSON body `{refresh_token: rt}`.
       b. If refresh succeeds (200):
          - Read `{access_token, refresh_token}` from response.
          - **B2/D-17 fix — re-issue rv_user with fresh plan_id:**
            - `const newPlanId = decodePlanIdFromJwt(access_token);`
            - `const priorUser = decodeSessionUser(priorUserCookie);  // verified prior {email, planId} or null`
            - `const priorEmail = priorUser?.email ?? "";  // keep existing email — refresh doesn't change identity`
            - `const fallbackPlanId = priorUser?.planId ?? "";  // safety net if JWT decode failed`
            - `const effectivePlanId = newPlanId || fallbackPlanId;`
            - `const newUserCookie = encodeSessionUser({ email: priorEmail, planId: effectivePlanId });`
          - Build a NextResponse for the retried call.
          - Set new cookies on the response:
            - `rv_at` ← `access_token` with `sessionCookieAttrs(COOKIE_MAX_AGE.ACCESS)`
            - `rv_rt` ← `refresh_token` with `sessionCookieAttrs(COOKIE_MAX_AGE.REFRESH)`
            - `rv_user` ← `newUserCookie` with `sessionCookieAttrs(COOKIE_MAX_AGE.USER)`  ← B2 KEY FIX: re-issued with the JUST-DECODED plan_id; 30-day TTL keeps it alive across access-token rotations
          - Retry the original upstream call with `Authorization: Bearer ${access_token}` (NEW access). Pipe response. Attach updated `Set-Cookie` headers (Next's cookies() API supports `.set()` on the response).
       c. If refresh fails: build a 401 response and DELETE cookies (`rv_at`, `rv_rt`, `rv_user`) via `Set-Cookie: <name>=; Max-Age=0; ...`. Return 401 with body `{error: "session_expired"}`.
    8. Recursion guard: refresh-retry MUST be one-shot per request — guard with the local `triedRefresh` variable.

    Note: streaming the request body twice (once for original, once for retry) requires buffering. For Phase 4 the largest body is a JSON checkout payload (≤1KB); buffer to `arrayBuffer()` once at step 4. Document size limit `BODY_BYTES_LIMIT = 64 * 1024` (matches PERF-09 Fiber config) and reject larger payloads with 413.

    Create landing/src/app/api/[...path]/route.ts:
    ```ts
    import { NextRequest } from "next/server";
    import { proxyToBackend } from "@/lib/proxy";

    export const dynamic = "force-dynamic";
    export const runtime = "nodejs";

    type Ctx = { params: Promise<{ path: string[] }> };

    async function handle(req: NextRequest, { params }: Ctx) {
      const { path } = await params;
      // Carve-outs: /api/auth/logout and /api/revalidate-pricing have dedicated routes — Next.js routes them first.
      return proxyToBackend(req, path);
    }

    export const GET = handle;
    export const POST = handle;
    export const PATCH = handle;
    export const PUT = handle;
    export const DELETE = handle;
    ```
  </action>
  <acceptance_criteria>
    - `grep -n 'env.BACKEND_API_URL' landing/src/lib/proxy.ts` returns at least 1 match
    - `grep -n 'auth/refresh' landing/src/lib/proxy.ts` returns at least 1 match
    - `grep -n 'Authorization.*Bearer' landing/src/lib/proxy.ts` returns at least 2 matches (original + retry)
    - `grep -n 'rv_at\|rv_rt\|rv_user' landing/src/lib/proxy.ts` returns at least 6 matches (rv_user must be present for B2 re-issue)
    - `grep -n 'sessionCookieAttrs\|clearCookieAttrs' landing/src/lib/proxy.ts` returns at least 2 matches
    - `grep -n 'decodePlanIdFromJwt' landing/src/lib/proxy.ts` returns at least 1 match (B2 fix — decode plan_id from refresh response's new access_token)
    - `grep -n 'encodeSessionUser' landing/src/lib/proxy.ts` returns at least 1 match (B2 fix — re-encode rv_user with new plan_id)
    - `grep -n 'decodeSessionUser' landing/src/lib/proxy.ts` returns at least 1 match (B2 fix — read prior email)
    - `grep -n 'COOKIE_MAX_AGE.USER' landing/src/lib/proxy.ts` returns at least 1 match (B2 fix — 30-day TTL on re-issued rv_user)
    - `grep -n 'runtime = "nodejs"' landing/src/app/api/\[...path\]/route.ts` returns 1 match
    - `grep -n 'force-dynamic' landing/src/app/api/\[...path\]/route.ts` returns 1 match
    - `grep -n 'export const GET\|export const POST\|export const PATCH\|export const PUT\|export const DELETE' landing/src/app/api/\[...path\]/route.ts` returns 5 matches
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npm run build` exits 0
  </acceptance_criteria>
  <done>The proxy handles all 5 HTTP methods, transforms cookies → Bearer, auto-refreshes on 401 once, decodes plan_id from the new JWT, re-issues rv_user with the new plan_id + prior email at the 30-day TTL, and writes new cookies on the response. Build succeeds.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 3: Dedicated /api/auth/logout route — calls backend, clears all session cookies, returns 204</name>
  <files>landing/src/app/api/auth/logout/route.ts</files>
  <read_first>
    - landing/src/lib/cookies.ts (Task 1 — COOKIE_NAMES, clearCookieAttrs)
    - landing/src/lib/env.ts
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-25 sign-out flow)
    - .planning/phases/02-auth-sso-backend/02-07-PLAN.md (logout contract — needs Authorization: Bearer)
  </read_first>
  <action>
    Create landing/src/app/api/auth/logout/route.ts:
    ```ts
    import { NextRequest, NextResponse } from "next/server";
    import { cookies } from "next/headers";
    import { env } from "@/lib/env";
    import { COOKIE_NAMES, clearCookieAttrs } from "@/lib/cookies";

    export const dynamic = "force-dynamic";
    export const runtime = "nodejs";

    export async function POST(req: NextRequest) {
      const jar = await cookies();
      const at = jar.get(COOKIE_NAMES.ACCESS)?.value;

      // Always clear cookies regardless of backend outcome (D-25 — never leave the user stuck logged in).
      const res = new NextResponse(null, { status: 204 });
      for (const name of [COOKIE_NAMES.ACCESS, COOKIE_NAMES.REFRESH, COOKIE_NAMES.USER]) {
        res.cookies.set({ name, value: "", ...clearCookieAttrs() });
      }

      if (!at) return res;

      try {
        await fetch(`${env.BACKEND_API_URL}/api/v1/auth/logout`, {
          method: "POST",
          headers: { Authorization: `Bearer ${at}` },
          signal: AbortSignal.timeout(5000),
        });
      } catch {
        // Swallow — the local cookie clear is the source of truth for the browser.
      }

      return res;
    }
    ```
  </action>
  <acceptance_criteria>
    - `grep -n 'COOKIE_NAMES.ACCESS\|COOKIE_NAMES.REFRESH\|COOKIE_NAMES.USER' landing/src/app/api/auth/logout/route.ts` returns at least 3 matches
    - `grep -n 'BACKEND_API_URL.*/api/v1/auth/logout' landing/src/app/api/auth/logout/route.ts` returns 1 match
    - `grep -n 'Authorization.*Bearer' landing/src/app/api/auth/logout/route.ts` returns 1 match
    - `grep -n 'clearCookieAttrs' landing/src/app/api/auth/logout/route.ts` returns at least 1 match
    - `grep -n 'new NextResponse(null, { status: 204' landing/src/app/api/auth/logout/route.ts` returns 1 match
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npm run build` exits 0
  </acceptance_criteria>
  <done>POST /api/auth/logout clears all three session cookies even on backend failure, forwards to backend logout when an access token is present, and returns 204.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| browser → Node proxy | Untrusted; proxy must validate every body against backend's schema (we delegate to backend's existing validators) |
| Node proxy → backend | Trusted (same data centre); cookies → Bearer transformation happens here |
| browser ⇄ cookies | HttpOnly so client JS can never read tokens; SameSite=Strict prevents cross-site abuse |
| rv_user.planId → /dashboard, /pricing CTA, navbar | rv_user is the server-side source of truth for "what plan does this user have right now" — staleness here means user sees wrong tier; freshness is enforced at refresh-rotation by decoding plan_id from the new JWT |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-04-03-01 | I (Info disclosure) | rv_at / rv_rt cookies | mitigate | Task 1 forces `HttpOnly: true`, `Secure: env.IS_PROD`, `SameSite: 'strict'`. No `NEXT_PUBLIC_*` reference to BACKEND_API_URL. WEB-02 closure. |
| T-04-03-02 | E (Elevation) | refresh-token replay | mitigate | Backend's HOTFIX-05 transactional rotation already invalidates rt on use; proxy retries refresh ONCE per request (Task 2 recursion guard) |
| T-04-03-03 | T (Tampering) | rv_user cookie content | mitigate | Task 1 HMAC-signs the payload; `decodeSessionUser` does constant-time compare; tampered cookies become anonymous |
| T-04-03-04 | D (DoS) | proxy body buffering | mitigate | Task 2 enforces `BODY_BYTES_LIMIT = 64*1024` (matches PERF-09) → reject larger with 413 |
| T-04-03-05 | D (DoS) | backend hang on refresh | mitigate | `AbortSignal.timeout(15000)` on proxied calls; `AbortSignal.timeout(5000)` on logout backend call |
| T-04-03-06 | S (Spoofing) | open path traversal in /api/[...path] | mitigate | `proxyToBackend` joins `path.join("/")` then concatenates with a fixed `${env.BACKEND_API_URL}/api/v1/` prefix — `..` segments inside path become literal in the URL (backend rejects them) and SSRF is impossible because the base URL is hard-coded from env at boot |
| T-04-03-07 | I (Info disclosure) | leak of upstream error bodies | accept | HOTFIX-04 already scrubs 5xx bodies on the backend (generic message + X-Request-ID). Phase 4 proxy passes them through unchanged. |
| T-04-03-08 | T (Tampering) | refresh-retry uses STALE Authorization header | mitigate | Task 2 builds NEW Authorization header from the refresh response's `access_token` before retry; original `at` is discarded |
| T-04-03-09 | I (Info disclosure) | cookie set in dev without Secure | accept | Dev convenience only; production sets `Secure: true` via `env.IS_PROD` gate |
| T-04-03-10 | S (Spoofing) | logout without access token | mitigate | Task 3 still clears cookies even if backend call fails (D-25 — never leave the user stuck) |
| T-04-03-11 | I (Stale plan_id information disclosure) | rv_user.planId stays stale across plan changes | mitigate | **B2/D-17 fix:** Task 2 decodes `plan_id` from the new access_token JWT at every refresh rotation and re-issues rv_user with `{email: prior, planId: new}`. Combined with the Plan 07 pay-success force-refresh trigger, a user who upgrades to Pro on lava.top sees Pro on /dashboard within one refresh cycle — typically <2s of the page load that follows payment success. rv_user TTL aligned to refresh-token TTL (30d) prevents premature expiry. |
| T-04-03-12 | E (Elevation via signature-skipped JWT decode) | malicious JWT injected as rv_at | mitigate | `decodePlanIdFromJwt` is called ONLY on the access_token field of the trusted `/auth/refresh` response body (server-to-server HTTPS hop, same request) — never on a JWT supplied by the browser. The JWT contents are used only to populate rv_user's planId (display string), which is HMAC-signed before write. Even a malicious plan_id value would only mis-render the UI (HMAC-signed rv_user is opaque to the browser); access control happens at the backend (Phase 2/3 c.Locals("plan_id") + tier check). |
</threat_model>

<verification>
- Phase-goal SC #1: `grep -rn 'localStorage' landing/src/` returns 0 matches for token storage (only the brand asset README may use the word in non-storage context)
- Phase-goal SC #1: every Set-Cookie issued by the proxy contains `HttpOnly`, `Secure` (when prod), `SameSite=Strict`, `Path=/`
- 401 refresh flow: simulated via Plan 08 smoke test (Playwright intercept)
- D-17 plan_id freshness: simulated in Plan 08 smoke — pre-seed rv_user with planId=free, force a proxy refresh that returns a JWT with `plan_id: "pro"`, assert the new rv_user (decoded via decodeSessionUser) contains planId=pro
- Logout: `curl -X POST -b 'rv_at=test' http://localhost:3000/api/auth/logout -i` shows three `Set-Cookie: rv_*=; Max-Age=0` headers + 204
- TypeScript: `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npx tsc --noEmit` exits 0
- Build: `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y npm run build` exits 0
</verification>

<success_criteria>
- Catch-all `/api/[...path]` route forwards every method to `${BACKEND_API_URL}/api/v1/...` with cookie→Bearer transformation
- 401 + rv_rt → POST /auth/refresh → decode plan_id from new access_token → re-issue rv_user with {email: prior, planId: new} at 30-day TTL → retry once → return upstream response
- Logout endpoint clears all three session cookies regardless of backend outcome
- HMAC-signed rv_user cookie prevents identity forgery (closes Plan 02's T-04-02-02)
- rv_user.planId reflects the user's CURRENT plan after every refresh rotation (D-17 closure)
- No JWT or refresh token ever lands in localStorage / sessionStorage / JS-readable cookie
</success_criteria>

<output>
After completion, create `.planning/phases/04-landing-surfaces/04-03-node-proxy-cookies-refresh-SUMMARY.md` documenting:
- Cookie attribute set (final values per env) — explicitly call out rv_user.Max-Age=30d (NOT 5min) and why
- Refresh-rotation request shape (body, headers, error semantics)
- HMAC scheme for rv_user (algo, secret derivation, format)
- decodePlanIdFromJwt design (signature-skipped decode, trust model, fallback chain)
- B2 closure: rv_user re-issued on every refresh with `{email: prior, planId: decodePlanIdFromJwt(new access_token)}`
- Body size limit and timeout values
- Carve-outs from the catch-all route (logout in this plan, revalidate-pricing in Plan 05, auth/callback in Plan 04)
</output>
</content>
</invoke>