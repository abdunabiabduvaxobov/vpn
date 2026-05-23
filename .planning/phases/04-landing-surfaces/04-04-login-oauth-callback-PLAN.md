---
phase: 04-landing-surfaces
plan: 04
type: execute
wave: 3
depends_on: [04-02, 04-03]
files_modified:
  - landing/src/app/[locale]/(app)/login/page.tsx
  - landing/src/app/[locale]/(app)/login/start-oauth.ts
  - landing/src/app/auth/callback/page.tsx
  - landing/src/app/auth/callback/exchange.ts
  - landing/src/lib/oauth.ts
  - landing/src/components/app/auth-button-apple.tsx
  - landing/src/components/app/auth-button-google.tsx
  - landing/src/lib/env.ts
  - landing/.env.example
autonomous: true
requirements:
  - WEB-01
  - WEB-02
must_haves:
  truths:
    - "GET /<locale>/login renders Apple + Google sign-in buttons with the vendored brand-mark SVGs"
    - "Clicking Apple/Google issues a redirect to the provider's authorize URL with state=<csrf>, nonce, and redirect_uri=<landing>/auth/callback?provider=<p>"
    - "An `rv_oauth_state` HttpOnly cookie is set BEFORE the user is sent to the provider, with a 5-min TTL and the same value as the `state` query param"
    - "The /auth/callback page validates that the returned `state` matches `rv_oauth_state` (constant-time compare) before exchanging the ID token with the backend"
    - "On successful backend exchange, `rv_at`, `rv_rt`, `rv_user` cookies are set; rv_user.planId comes from decoding plan_id from the new rv_at JWT (NOT from the response body's user.plan_id) so JWT is the single source of truth"
    - "rv_user Max-Age matches the refresh-token TTL (30d) so it survives natural rv_at rotation"
    - "User is redirected to the parsed `next` URL (or `/<locale>/dashboard` by default)"
    - "On CSRF mismatch or provider denial, user is redirected to `/<locale>/login?error=oauth_state` or `?error=oauth_denied` with the appropriate i18n toast key"
  artifacts:
    - path: "landing/src/app/[locale]/(app)/login/page.tsx"
      provides: "Server-rendered login page with Apple + Google buttons"
    - path: "landing/src/app/[locale]/(app)/login/start-oauth.ts"
      provides: "Server Action: generate state, set rv_oauth_state cookie, build provider authorize URL, redirect"
      exports: ["startOAuth"]
    - path: "landing/src/app/auth/callback/page.tsx"
      provides: "OAuth callback receiver (locale-less route)"
    - path: "landing/src/app/auth/callback/exchange.ts"
      provides: "Server Action: verify state, POST to backend /auth/apple|/auth/google, set session cookies (rv_user.planId decoded from new JWT), redirect"
      exports: ["completeOAuth"]
    - path: "landing/src/lib/oauth.ts"
      provides: "OAuth helpers — buildAuthorizeUrl, parseStateNext, isSafeNextPath"
      exports: ["buildAppleAuthorizeUrl", "buildGoogleAuthorizeUrl", "isSafeNextPath", "encodeState", "decodeState"]
    - path: "landing/src/components/app/auth-button-apple.tsx"
      provides: "Apple sign-in button with vendored brand-mark — submits to startOAuth server action"
      exports: ["AuthButtonApple"]
    - path: "landing/src/components/app/auth-button-google.tsx"
      provides: "Google sign-in button with vendored brand-mark — submits to startOAuth server action"
      exports: ["AuthButtonGoogle"]
  key_links:
    - from: "landing/src/app/[locale]/(app)/login/page.tsx"
      to: "AuthButtonApple, AuthButtonGoogle"
      via: "import + render"
      pattern: "AuthButtonApple|AuthButtonGoogle"
    - from: "landing/src/app/[locale]/(app)/login/start-oauth.ts"
      to: "Apple / Google authorize endpoints"
      via: "redirect(buildAppleAuthorizeUrl(state) | buildGoogleAuthorizeUrl(state))"
      pattern: "appleid.apple.com|accounts.google.com"
    - from: "landing/src/app/auth/callback/exchange.ts"
      to: "BACKEND_API_URL/api/v1/auth/apple or /auth/google"
      via: "fetch POST with id_token"
      pattern: "auth/(apple|google)"
    - from: "landing/src/app/auth/callback/exchange.ts"
      to: "rv_at, rv_rt, rv_user cookies"
      via: "cookies().set(...); rv_user.planId via decodePlanIdFromJwt(access_token)"
      pattern: "rv_at|rv_rt|rv_user|decodePlanIdFromJwt"
tags: [auth, oauth, login, csrf]
---

<objective>
Build the user-visible sign-in path: a `/<locale>/login` page with Apple + Google buttons, the locale-agnostic `/auth/callback` receiver, and the supporting OAuth state-CSRF + exchange helpers. The flow:

1. User clicks Apple/Google → Server Action `startOAuth` runs server-side → generates a 32-byte random `state`, encodes `{next, plan, period, currency, csrf}` into it, sets `rv_oauth_state=<state>` cookie (HttpOnly, 5-min TTL), redirects to the provider's authorize URL with `state=<state>` and `redirect_uri=<APP_URL>/auth/callback?provider=<p>`.

2. Provider redirects back to `/auth/callback?provider=<p>&code=<...>&state=<...>` (Google) or POSTs form-encoded id_token+state (Apple form-post mode — we support both query and form_post via a single route).

3. Callback handler `completeOAuth`:
   - constant-time compares `state` against `rv_oauth_state` cookie
   - on mismatch → clear oauth_state cookie + redirect to `/<locale>/login?error=oauth_state`
   - on match → POST `id_token` (Apple) or `code` (Google) to backend `/api/v1/auth/apple` or `/auth/google` (D-10)
   - backend returns `{access_token, refresh_token, user: {id, email, plan_id, ...}}`
   - set `rv_at`, `rv_rt`, `rv_user` cookies via the Plan 03 helpers. **B2 fix:** rv_user.planId comes from `decodePlanIdFromJwt(access_token)` (Phase 3 D-29 claim), NOT from `backend.user.plan_id` — the JWT is the single source of truth and matches what `c.Locals("plan_id")` will see on subsequent authenticated calls. The response body's user.plan_id is used only as a fallback if JWT decode returns "". rv_user.email comes from the response body (the JWT does not carry email).
   - rv_user Max-Age uses `COOKIE_MAX_AGE.USER` (30-day, matches refresh TTL — B2/W5 fix from Plan 03) so it survives natural rv_at rotation
   - clear `rv_oauth_state` cookie
   - parse `next` from the state — if `isSafeNextPath(next)` → redirect there; else → `/<locale>/dashboard`

This satisfies WEB-01 (Apple + Google buttons on /login) and WEB-02 (HttpOnly cookies, no localStorage) for the OAuth side. Plan 03 owns the proxy-level WEB-02 enforcement; this plan owns the WEB-02 set-cookie-at-OAuth-completion path.

Purpose: this is the user-facing "doorway" — the visible UX equivalent of the entire backend SSO chain Phase 2 built. If this works, the whole product gates open: dashboard, pricing, pay/success all unlock for signed-in users. Aligning rv_user.planId to the JWT's plan_id claim means /dashboard's plan display is consistent with backend's authorization decisions from the very first request.

Output: A user lands on `/<locale>/login`, clicks Apple, completes Apple auth, lands on `/<locale>/dashboard` (or wherever `next` pointed), sees their correct plan, no JWT in localStorage. rv_user stays valid for 30 days across access-token rotations.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/04-landing-surfaces/04-CONTEXT.md
@.planning/phases/04-landing-surfaces/04-UI-SPEC.md
@.planning/phases/04-landing-surfaces/04-02-app-shell-navbar-primitives-PLAN.md
@.planning/phases/04-landing-surfaces/04-03-node-proxy-cookies-refresh-PLAN.md
@.planning/phases/02-auth-sso-backend/02-05-PLAN.md
@.planning/phases/03-lava-top-plans-catalog/03-07-public-plans-jwt-cache-SUMMARY.md
@landing/src/lib/cookies.ts
@landing/src/lib/session-cookie.ts
@landing/src/lib/env.ts
@landing/src/i18n/navigation.ts

<interfaces>
<!-- Locked CONTEXT.md decisions -->
- D-10: callback path = `/auth/callback?provider=<apple|google>`. Locale-LESS so it can be a single registered redirect URI per provider. After exchange, redirect to a locale-prefixed `next` URL.
- D-11: Apple Service ID = `services.risevpn.web`. Google Web Client ID = `GOOGLE_CLIENT_ID_WEB` env var (matches Phase 2 backend config).
- D-12: state CSRF cookie name = `rv_oauth_state`, HttpOnly, 5-min TTL. State payload should also include `next`, `plan`, `period`, `currency` so Plan 07 can resume the checkout flow.
- D-17: rv_user.planId is the source of truth for /dashboard plan display. The planId value MUST come from the JWT's `plan_id` claim (Phase 3 D-29) — decoded via Plan 03's `decodePlanIdFromJwt` helper — so it matches `c.Locals("plan_id")` on subsequent authenticated calls and stays in sync as the backend changes the user's tier.

New env vars (add to env.ts and .env.example in Task 1):
- APPLE_SERVICE_ID — Apple Service ID for "Sign in with Apple" on the web (e.g., `services.risevpn.web`)
- APPLE_REDIRECT_URI — full URL e.g., `https://risevpn.com/auth/callback?provider=apple` (must match Apple Developer config)
- GOOGLE_CLIENT_ID_WEB — Google OAuth Web client ID
- GOOGLE_REDIRECT_URI — full URL e.g., `https://risevpn.com/auth/callback?provider=google`
- APP_URL — landing's public origin (e.g., `https://risevpn.com`) — used to build absolute redirect_uri values for providers

Apple authorize URL (per Apple "Sign in with Apple" REST API):
- Endpoint: `https://appleid.apple.com/auth/authorize`
- Params: `client_id=<APPLE_SERVICE_ID>`, `redirect_uri=<APPLE_REDIRECT_URI>`, `response_type=code id_token`, `response_mode=form_post`, `scope=name email`, `state=<state>`, `nonce=<nonce>` (random 32 bytes b64url, stored in state)
- IMPORTANT: response_mode=form_post means Apple POSTs to the redirect URI with form-urlencoded body. The Next.js route must handle POST as well as GET.

Google authorize URL (per OpenID Connect spec, Google variant):
- Endpoint: `https://accounts.google.com/o/oauth2/v2/auth`
- Params: `client_id=<GOOGLE_CLIENT_ID_WEB>`, `redirect_uri=<GOOGLE_REDIRECT_URI>`, `response_type=id_token`, `response_mode=form_post`, `scope=openid email profile`, `state=<state>`, `nonce=<nonce>`, `prompt=select_account`
- W1 NOTE (deprecation watch): Google's `response_type=id_token` (implicit flow) is on the deprecation path. The long-term migration is PKCE + authorization code with a server-to-server exchange of `code` for `id_token` against Google's token endpoint. That migration requires backend changes to `/auth/google` to accept a `code` instead of an `id_token` and to perform the token-endpoint exchange. Captured as a Phase 5+ follow-up — not in scope for Phase 4 (see plan-level summary output).
- We use `response_mode=form_post` so the id_token arrives as a POST body (symmetric with Apple — single route handles both).

Backend exchange endpoints (Phase 2 contract):
- POST /api/v1/auth/apple — body `{id_token: string}` — returns `{access_token, refresh_token, expires_in, user: {id, email, plan_id, subscription_tier, ...}}`
- POST /api/v1/auth/google — body `{id_token: string}` — same response shape
- The returned access_token is a JWT containing the `plan_id` claim per Phase 3 D-29 / 03-07 SUMMARY ("Plan_id backfill at all JWT mint sites — AppleSignIn, GoogleSignIn call FindSystemPlanID + UPDATE users.plan_id BEFORE generateTokens so the very first JWT carries the claim").

State payload contract (encoded as base64url(JSON):
```ts
type StatePayload = {
  csrf: string;       // 32 random bytes b64url — must match rv_oauth_state cookie value
  next?: string;      // locale-less path like /pricing
  plan?: string;      // "pro" (Plan 07)
  period?: string;    // "monthly" | "yearly" (Plan 07)
  currency?: string;  // "USD" | "EUR" | "RUB" (Plan 07)
  locale: string;     // "ru" | "en" | "es" — preserved across the auth round-trip
};
```

isSafeNextPath validator:
- Reject if not starting with `/`
- Reject if starts with `//` or `\`
- Reject if contains `..`
- Reject if hostname-bearing (`http://`, `https://`)
- Allow only paths matching `^/(ru|en|es)/(login|dashboard|pricing|pay/(success|fail))(\?.*)?$` OR the locale-less variants `^/(login|dashboard|pricing|pay/(success|fail))(\?.*)?$`
</interfaces>
</context>

<tasks>

<task type="auto" tdd="false">
  <name>Task 1: Extend env.ts + .env.example with OAuth provider config + add OAuth helper module</name>
  <files>landing/src/lib/env.ts, landing/.env.example, landing/.env.local.example, landing/src/lib/oauth.ts</files>
  <read_first>
    - landing/src/lib/env.ts (created Plan 01)
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-10, D-11, D-12)
    - .planning/phases/02-auth-sso-backend/02-CONTEXT.md (Apple verifier audience, Google client IDs)
  </read_first>
  <action>
    Edit landing/src/lib/env.ts: add five new required keys:
    - APPLE_SERVICE_ID
    - APPLE_REDIRECT_URI
    - GOOGLE_CLIENT_ID_WEB
    - GOOGLE_REDIRECT_URI
    - APP_URL

    Pattern: each goes through the existing `readRequired` helper. Add to the frozen `env` object.

    Edit landing/.env.example + landing/.env.local.example: append a `# OAuth (Plan 04)` section with the five vars + descriptive comments. Production values mention Apple Developer + Google Cloud Console as the source.

    Create landing/src/lib/oauth.ts (server-only):
    ```ts
    import "server-only";
    import { randomBytes, timingSafeEqual } from "node:crypto";
    import { env } from "./env";

    export type StatePayload = {
      csrf: string;
      next?: string;
      plan?: string;
      period?: string;
      currency?: string;
      locale: string;
    };

    export function randomB64Url(bytes = 32): string {
      return randomBytes(bytes).toString("base64url");
    }

    export function encodeState(p: StatePayload): string {
      return Buffer.from(JSON.stringify(p), "utf8").toString("base64url");
    }

    export function decodeState(raw: string): StatePayload | null {
      try {
        const json = JSON.parse(Buffer.from(raw, "base64url").toString("utf8"));
        if (typeof json.csrf !== "string" || typeof json.locale !== "string") return null;
        return json as StatePayload;
      } catch { return null; }
    }

    const SAFE_NEXT_RE = /^\/(?:(?:ru|en|es)\/)?(?:login|dashboard|pricing|pay\/(?:success|fail))(?:\?[A-Za-z0-9_\-&=%.+:/,]*)?$/;

    export function isSafeNextPath(p: unknown): p is string {
      if (typeof p !== "string" || !p.startsWith("/") || p.startsWith("//") || p.startsWith("/\\")) return false;
      if (p.includes("..")) return false;
      return SAFE_NEXT_RE.test(p);
    }

    export function constantTimeEquals(a: string, b: string): boolean {
      const ab = Buffer.from(a);
      const bb = Buffer.from(b);
      if (ab.length !== bb.length) return false;
      return timingSafeEqual(ab, bb);
    }

    export function buildAppleAuthorizeUrl(state: string, nonce: string): string {
      const p = new URLSearchParams({
        client_id: env.APPLE_SERVICE_ID,
        redirect_uri: env.APPLE_REDIRECT_URI,
        response_type: "code id_token",
        response_mode: "form_post",
        scope: "name email",
        state,
        nonce,
      });
      return `https://appleid.apple.com/auth/authorize?${p.toString()}`;
    }

    export function buildGoogleAuthorizeUrl(state: string, nonce: string): string {
      const p = new URLSearchParams({
        client_id: env.GOOGLE_CLIENT_ID_WEB,
        redirect_uri: env.GOOGLE_REDIRECT_URI,
        response_type: "id_token",
        response_mode: "form_post",
        scope: "openid email profile",
        state,
        nonce,
        prompt: "select_account",
      });
      return `https://accounts.google.com/o/oauth2/v2/auth?${p.toString()}`;
    }
    ```
  </action>
  <acceptance_criteria>
    - `grep -n 'APPLE_SERVICE_ID\|APPLE_REDIRECT_URI\|GOOGLE_CLIENT_ID_WEB\|GOOGLE_REDIRECT_URI\|APP_URL' landing/src/lib/env.ts` returns at least 5 matches
    - `grep -n 'APPLE_SERVICE_ID' landing/.env.example` returns 1 match
    - `grep -n 'response_mode=form_post\|response_mode: "form_post"' landing/src/lib/oauth.ts` returns at least 2 matches (Apple + Google share form_post per D-10/contract)
    - `grep -n 'appleid.apple.com/auth/authorize' landing/src/lib/oauth.ts` returns 1 match
    - `grep -n 'accounts.google.com/o/oauth2/v2/auth' landing/src/lib/oauth.ts` returns 1 match
    - `grep -n 'isSafeNextPath' landing/src/lib/oauth.ts` returns at least 1 match
    - `grep -n 'timingSafeEqual\|constantTimeEquals' landing/src/lib/oauth.ts` returns at least 2 matches
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y APPLE_SERVICE_ID=x APPLE_REDIRECT_URI=https://x/cb GOOGLE_CLIENT_ID_WEB=x GOOGLE_REDIRECT_URI=https://x/cb APP_URL=https://x npx tsc --noEmit` exits 0
  </acceptance_criteria>
  <done>env.ts validates 5 new OAuth vars at boot, oauth.ts exports state encode/decode, safe-next validator, and authorize-URL builders for both providers.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 2: /login page with Apple + Google buttons + startOAuth Server Action</name>
  <files>landing/src/app/[locale]/(app)/login/page.tsx, landing/src/app/[locale]/(app)/login/start-oauth.ts, landing/src/components/app/auth-button-apple.tsx, landing/src/components/app/auth-button-google.tsx</files>
  <read_first>
    - landing/src/lib/oauth.ts (Task 1)
    - landing/src/lib/cookies.ts (Plan 03 — COOKIE_NAMES.OAUTH_STATE, sessionCookieAttrs)
    - landing/public/brand/apple/apple-sign-in.svg (Plan 02 — vendored asset)
    - landing/public/brand/google/google-g.svg (Plan 02)
    - .planning/phases/04-landing-surfaces/04-UI-SPEC.md (§Page-Specific Layout Contracts — /login)
    - landing/src/components/common/logo.tsx
    - landing/src/i18n/navigation.ts
  </read_first>
  <action>
    Create landing/src/app/[locale]/(app)/login/start-oauth.ts as a Server Action module:
    ```ts
    "use server";
    import { cookies } from "next/headers";
    import { redirect } from "next/navigation";
    import { encodeState, randomB64Url, buildAppleAuthorizeUrl, buildGoogleAuthorizeUrl, isSafeNextPath } from "@/lib/oauth";
    import { COOKIE_NAMES, COOKIE_MAX_AGE, sessionCookieAttrs } from "@/lib/cookies";

    type StartArgs = { provider: "apple" | "google"; locale: string; next?: string; plan?: string; period?: string; currency?: string };

    export async function startOAuth(args: StartArgs) {
      const csrf = randomB64Url(32);
      const nonce = randomB64Url(32);
      const next = args.next && isSafeNextPath(args.next) ? args.next : undefined;
      const payload = encodeState({ csrf, locale: args.locale, next, plan: args.plan, period: args.period, currency: args.currency });
      const jar = await cookies();
      jar.set({ name: COOKIE_NAMES.OAUTH_STATE, value: csrf, ...sessionCookieAttrs(COOKIE_MAX_AGE.OAUTH_STATE) });
      // Stash nonce in cookie too (Apple/Google id_token includes nonce; backend will verify) — separate cookie for clarity.
      jar.set({ name: "rv_oauth_nonce", value: nonce, ...sessionCookieAttrs(COOKIE_MAX_AGE.OAUTH_STATE) });
      const url = args.provider === "apple" ? buildAppleAuthorizeUrl(payload, nonce) : buildGoogleAuthorizeUrl(payload, nonce);
      redirect(url);
    }

    // FormData wrapper so AuthButton components can be Server Components.
    export async function startOAuthForm(formData: FormData) {
      const provider = formData.get("provider") as "apple" | "google";
      const locale = formData.get("locale") as string;
      const next = formData.get("next") as string | undefined;
      const plan = formData.get("plan") as string | undefined;
      const period = formData.get("period") as string | undefined;
      const currency = formData.get("currency") as string | undefined;
      return startOAuth({ provider, locale, next, plan, period, currency });
    }
    ```

    Create landing/src/components/app/auth-button-apple.tsx (Server Component — uses form action server action; no "use client" needed):
    ```tsx
    import Image from "next/image";
    import { getTranslations } from "next-intl/server";
    import { startOAuthForm } from "@/app/[locale]/(app)/login/start-oauth";

    type Props = { locale: string; next?: string; plan?: string; period?: string; currency?: string };
    export async function AuthButtonApple(p: Props) {
      const t = await getTranslations("login.signIn");
      return (
        <form action={startOAuthForm} className="w-full">
          <input type="hidden" name="provider" value="apple" />
          <input type="hidden" name="locale" value={p.locale} />
          {p.next && <input type="hidden" name="next" value={p.next} />}
          {p.plan && <input type="hidden" name="plan" value={p.plan} />}
          {p.period && <input type="hidden" name="period" value={p.period} />}
          {p.currency && <input type="hidden" name="currency" value={p.currency} />}
          <button type="submit" className="flex h-11 w-full items-center justify-center gap-2 rounded-[var(--radius-md)] bg-black px-4 text-white font-medium hover:opacity-90 focus-visible:ring-3 focus-visible:ring-ring/50">
            <Image src="/brand/apple/apple-sign-in.svg" alt="" width={20} height={20} aria-hidden />
            <span>{t("apple")}</span>
          </button>
        </form>
      );
    }
    ```

    Create landing/src/components/app/auth-button-google.tsx — same structure as Apple but with the Google G SVG and white background per Google branding (`bg-white text-[#3c4043] border border-[#dadce0]` per Google's guidelines). Set hidden input `provider=google`.

    Create landing/src/app/[locale]/(app)/login/page.tsx (Server Component):
    ```tsx
    import { getTranslations } from "next-intl/server";
    import { Link } from "@/i18n/navigation";
    import { Logo } from "@/components/common/logo";
    import { AuthButtonApple } from "@/components/app/auth-button-apple";
    import { AuthButtonGoogle } from "@/components/app/auth-button-google";

    export const dynamic = "force-dynamic";

    type Props = { params: Promise<{ locale: string }>; searchParams: Promise<{ next?: string; plan?: string; period?: string; currency?: string; error?: string }> };

    export default async function LoginPage({ params, searchParams }: Props) {
      const { locale } = await params;
      const sp = await searchParams;
      const t = await getTranslations("login");
      const tErr = await getTranslations("errors");
      return (
        <main className="mx-auto flex min-h-[80vh] max-w-md flex-col items-center justify-center px-6 py-12">
          <Logo className="mb-8" />
          <h1 className="text-3xl lg:text-4xl font-bold font-heading text-foreground">{t("heading")}</h1>
          <p className="mt-2 text-muted-foreground text-base">{t("subhead")}</p>
          {sp.error && (
            <div role="alert" className="mt-4 w-full rounded-[var(--radius-md)] border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive">
              {sp.error === "oauth_state" ? tErr("oauthState") : tErr("oauthDenied")}
            </div>
          )}
          <div className="mt-8 flex w-full flex-col gap-2">
            <AuthButtonApple locale={locale} next={sp.next} plan={sp.plan} period={sp.period} currency={sp.currency} />
            <AuthButtonGoogle locale={locale} next={sp.next} plan={sp.plan} period={sp.period} currency={sp.currency} />
          </div>
          <Link href="/" className="mt-4 text-sm text-subtle-foreground hover:text-muted-foreground">{t("backHome")}</Link>
        </main>
      );
    }
    ```
  </action>
  <acceptance_criteria>
    - `grep -n '"use server"' landing/src/app/\[locale\]/\(app\)/login/start-oauth.ts` returns 1 match
    - `grep -n 'COOKIE_NAMES.OAUTH_STATE\|rv_oauth_state' landing/src/app/\[locale\]/\(app\)/login/start-oauth.ts` returns at least 1 match
    - `grep -n 'randomB64Url' landing/src/app/\[locale\]/\(app\)/login/start-oauth.ts` returns at least 1 match
    - `grep -n 'buildAppleAuthorizeUrl\|buildGoogleAuthorizeUrl' landing/src/app/\[locale\]/\(app\)/login/start-oauth.ts` returns at least 2 matches
    - `grep -n 'startOAuthForm' landing/src/app/\[locale\]/\(app\)/login/start-oauth.ts` returns at least 1 match (FormData wrapper export)
    - `grep -n 'AuthButtonApple\|AuthButtonGoogle' landing/src/app/\[locale\]/\(app\)/login/page.tsx` returns at least 2 matches
    - `grep -n '/brand/apple/apple-sign-in.svg' landing/src/components/app/auth-button-apple.tsx` returns 1 match
    - `grep -n '/brand/google/google-g.svg' landing/src/components/app/auth-button-google.tsx` returns 1 match
    - `grep -n 'h-11\|h-\[44px\]' landing/src/components/app/auth-button-apple.tsx landing/src/components/app/auth-button-google.tsx` returns at least 2 matches (≥44px touch target per UI-SPEC)
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y APPLE_SERVICE_ID=x APPLE_REDIRECT_URI=https://x/cb GOOGLE_CLIENT_ID_WEB=x GOOGLE_REDIRECT_URI=https://x/cb APP_URL=https://x npm run build` exits 0
  </acceptance_criteria>
  <done>Visiting `/<locale>/login` renders Apple + Google brand-mark buttons; submitting either sets `rv_oauth_state` + `rv_oauth_nonce` cookies and redirects to the provider's authorize URL with state + nonce + form_post.</done>
</task>

<task type="auto" tdd="false">
  <name>Task 3: /auth/callback page + completeOAuth Server Action — verify state, exchange id_token, decode plan_id FROM JWT (not from response body), set session cookies, redirect</name>
  <files>landing/src/app/auth/callback/page.tsx, landing/src/app/auth/callback/exchange.ts, landing/src/app/auth/callback/route.ts</files>
  <read_first>
    - landing/src/lib/oauth.ts (Task 1)
    - landing/src/lib/cookies.ts (Plan 03 — COOKIE_NAMES, COOKIE_MAX_AGE.USER=30d)
    - landing/src/lib/session-cookie.ts (Plan 03 — encodeSessionUser, decodePlanIdFromJwt)
    - landing/src/lib/env.ts
    - .planning/phases/04-landing-surfaces/04-CONTEXT.md (D-10, D-12, D-17, D-19)
    - .planning/phases/02-auth-sso-backend/02-CONTEXT.md (backend auth response shape)
    - .planning/phases/03-lava-top-plans-catalog/03-07-public-plans-jwt-cache-SUMMARY.md (plan_id claim ships in every JWT)
  </read_first>
  <action>
    Create landing/src/app/auth/callback/exchange.ts:
    ```ts
    "use server";
    import { cookies } from "next/headers";
    import { redirect } from "next/navigation";
    import { env } from "@/lib/env";
    import { COOKIE_NAMES, COOKIE_MAX_AGE, sessionCookieAttrs, clearCookieAttrs } from "@/lib/cookies";
    import { encodeSessionUser, decodePlanIdFromJwt } from "@/lib/session-cookie";
    import { decodeState, constantTimeEquals, isSafeNextPath } from "@/lib/oauth";

    type ExchangeArgs = {
      provider: "apple" | "google";
      idToken: string;       // Apple form_post: id_token field. Google id_token implicit flow: id_token field.
      state: string;
    };

    export async function completeOAuth(args: ExchangeArgs) {
      const jar = await cookies();
      const cookieCsrf = jar.get(COOKIE_NAMES.OAUTH_STATE)?.value ?? "";
      const decoded = decodeState(args.state);

      // Always clear the oauth_state + nonce cookies before any redirect.
      const clearOAuth = () => {
        jar.set({ name: COOKIE_NAMES.OAUTH_STATE, value: "", ...clearCookieAttrs() });
        jar.set({ name: "rv_oauth_nonce", value: "", ...clearCookieAttrs() });
      };

      if (!decoded || !constantTimeEquals(cookieCsrf, decoded.csrf)) {
        clearOAuth();
        redirect(`/${decoded?.locale ?? "ru"}/login?error=oauth_state`);
      }

      // Exchange with backend.
      let backend;
      try {
        const r = await fetch(`${env.BACKEND_API_URL}/api/v1/auth/${args.provider}`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ id_token: args.idToken }),
          signal: AbortSignal.timeout(10000),
        });
        if (!r.ok) {
          clearOAuth();
          redirect(`/${decoded.locale}/login?error=oauth_denied`);
        }
        backend = await r.json();
      } catch {
        clearOAuth();
        redirect(`/${decoded.locale}/login?error=oauth_denied`);
      }

      const accessToken: string = backend.access_token;
      const refreshToken: string = backend.refresh_token;
      const email: string = backend.user?.email ?? "";

      // B2 FIX (D-17 source-of-truth):
      // rv_user.planId comes from decoding the JWT's plan_id claim, NOT from
      // backend.user.plan_id directly. The JWT is what middleware reads as
      // c.Locals("plan_id") on every authenticated request, so aligning rv_user
      // to that exact claim means /dashboard's plan display can never diverge
      // from backend authorization decisions. Phase 3 D-29 / 03-07 SUMMARY
      // confirms AppleSignIn + GoogleSignIn mint sites emit plan_id in the JWT.
      const planIdFromJwt = decodePlanIdFromJwt(accessToken);
      const planIdFromBody: string = typeof backend.user?.plan_id === "string" ? backend.user.plan_id : "";
      const planId = planIdFromJwt || planIdFromBody;  // JWT first, body fallback

      // Set session cookies. rv_user uses COOKIE_MAX_AGE.USER (30d — matches refresh)
      // per Plan 03 B2/W5 fix so it survives natural rv_at rotation.
      jar.set({ name: COOKIE_NAMES.ACCESS,  value: accessToken,  ...sessionCookieAttrs(COOKIE_MAX_AGE.ACCESS)  });
      jar.set({ name: COOKIE_NAMES.REFRESH, value: refreshToken, ...sessionCookieAttrs(COOKIE_MAX_AGE.REFRESH) });
      jar.set({ name: COOKIE_NAMES.USER,    value: encodeSessionUser({ email, planId }), ...sessionCookieAttrs(COOKIE_MAX_AGE.USER) });
      clearOAuth();

      // Decide redirect target.
      let target = `/${decoded.locale}/dashboard`;
      if (decoded.next && isSafeNextPath(decoded.next)) {
        // If logged-out → /pricing → /login flow, Plan 07 sets next to /pricing + extra query.
        const q = new URLSearchParams();
        if (decoded.plan) q.set("plan", decoded.plan);
        if (decoded.period) q.set("period", decoded.period);
        if (decoded.currency) q.set("currency", decoded.currency);
        if (decoded.next === "/pricing" && [...q.keys()].length > 0) {
          q.set("checkout", "auto");
        }
        const lp = decoded.next.startsWith(`/${decoded.locale}/`) ? decoded.next : `/${decoded.locale}${decoded.next}`;
        target = q.toString() ? `${lp}?${q.toString()}` : lp;
      }
      redirect(target);
    }
    ```

    Create landing/src/app/auth/callback/page.tsx — GET-side wrapper for query-mode callbacks (rare; primarily Google in non-form-post fallback). Most production traffic uses POST → route.ts.
    ```tsx
    import { completeOAuth } from "./exchange";

    export const dynamic = "force-dynamic";
    export const runtime = "nodejs";

    type Props = { searchParams: Promise<{ provider?: string; state?: string; id_token?: string; error?: string }> };

    export default async function AuthCallback({ searchParams }: Props) {
      const sp = await searchParams;
      const provider = sp.provider === "apple" || sp.provider === "google" ? sp.provider : null;

      if (!provider) return <main className="p-8"><p>Invalid callback.</p></main>;
      if (sp.error || !sp.id_token || !sp.state) {
        await completeOAuth({ provider, idToken: "", state: sp.state ?? "" });
      } else {
        await completeOAuth({ provider, idToken: sp.id_token, state: sp.state });
      }
      return <main className="p-8"><p>Completing sign-in…</p></main>;
    }
    ```

    Create landing/src/app/auth/callback/route.ts to handle the form_post POST body (primary path for both Apple and Google):
    ```ts
    import { NextRequest, NextResponse } from "next/server";
    import { completeOAuth } from "./exchange";

    export const dynamic = "force-dynamic";
    export const runtime = "nodejs";

    export async function POST(req: NextRequest) {
      const url = new URL(req.url);
      const provider = url.searchParams.get("provider");
      if (provider !== "apple" && provider !== "google") return new NextResponse("Bad provider", { status: 400 });

      const ct = req.headers.get("content-type") ?? "";
      let form: URLSearchParams;
      if (ct.includes("application/x-www-form-urlencoded")) {
        form = new URLSearchParams(await req.text());
      } else if (ct.includes("multipart/form-data")) {
        const fd = await req.formData();
        form = new URLSearchParams();
        for (const [k, v] of fd.entries()) form.set(k, typeof v === "string" ? v : "");
      } else {
        form = new URLSearchParams();
      }

      const idToken = form.get("id_token") ?? "";
      const state = form.get("state") ?? "";
      const error = form.get("error");

      // completeOAuth calls redirect() which throws NEXT_REDIRECT; Next.js App Router
      // route handlers (>=14.0) auto-convert it to a 3xx response.
      if (error || !idToken) {
        await completeOAuth({ provider, idToken: "", state });
      } else {
        await completeOAuth({ provider, idToken, state });
      }
      return new NextResponse(null, { status: 500 }); // unreachable — redirect throws above
    }
    ```
  </action>
  <acceptance_criteria>
    - `grep -n '"use server"' landing/src/app/auth/callback/exchange.ts` returns 1 match
    - `grep -n 'constantTimeEquals\|timingSafeEqual' landing/src/app/auth/callback/exchange.ts` returns at least 1 match
    - `grep -n 'auth/apple\|auth/google' landing/src/app/auth/callback/exchange.ts` returns at least 1 match (backend URL)
    - `grep -n 'encodeSessionUser' landing/src/app/auth/callback/exchange.ts` returns at least 1 match
    - `grep -n 'decodePlanIdFromJwt' landing/src/app/auth/callback/exchange.ts` returns at least 1 match (B2 fix — planId from JWT, not body)
    - `grep -n 'COOKIE_MAX_AGE.USER' landing/src/app/auth/callback/exchange.ts` returns at least 1 match (B2 fix — 30-day rv_user TTL)
    - `grep -n 'COOKIE_NAMES.ACCESS\|COOKIE_NAMES.REFRESH\|COOKIE_NAMES.USER' landing/src/app/auth/callback/exchange.ts` returns at least 3 matches
    - `grep -n 'oauth_state\|oauth_denied' landing/src/app/auth/callback/exchange.ts` returns at least 2 matches (the two error redirect targets)
    - `grep -n 'isSafeNextPath' landing/src/app/auth/callback/exchange.ts` returns 1 match
    - `grep -n 'checkout=auto' landing/src/app/auth/callback/exchange.ts` returns 1 match (Plan 07 hand-off)
    - `grep -n 'export async function POST' landing/src/app/auth/callback/route.ts` returns 1 match
    - `cd landing && BACKEND_API_URL=http://x REVALIDATE_SECRET=y APPLE_SERVICE_ID=x APPLE_REDIRECT_URI=https://x/cb GOOGLE_CLIENT_ID_WEB=x GOOGLE_REDIRECT_URI=https://x/cb APP_URL=https://x npm run build` exits 0
  </acceptance_criteria>
  <done>POST /auth/callback?provider=apple|google verifies state with constant-time compare, exchanges id_token with backend, decodes plan_id from the returned access_token JWT (NOT from response body), sets session cookies (rv_user 30-day TTL), redirects to next/dashboard, or returns to /login on error.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| user click → provider authorize URL | redirect_uri must be registered with provider; state cookie binds the round-trip |
| provider → /auth/callback | untrusted POST body (form_post); state must be CSRF-checked |
| /auth/callback → backend /auth/* | trusted server-to-server; id_token is the verifier input |
| backend → set-cookie | HttpOnly cookies set only after backend confirms identity |
| access_token JWT → rv_user.planId | JWT body is parsed without signature verification because we just received it on a trusted server-to-server hop in the same request — same trust model as reading the response JSON's other fields |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-04-04-01 | T (CSRF) | OAuth callback | mitigate | Task 1+3: 32-byte random `csrf` in state payload + cookie; constant-time compare in `completeOAuth`. Standard OAuth 2.0 §10.12 mitigation. |
| T-04-04-02 | T (Open redirect) | `next` param after sign-in | mitigate | Task 1: `isSafeNextPath` regex allow-list. No hostname-bearing strings, no `..`, no protocol-relative `//`. Task 3 calls validator before redirect. |
| T-04-04-03 | S (Spoofing) | Apple/Google id_token | transfer | Backend (Phase 2) verifies JWKs + iss + aud + nonce. Landing only forwards the token. AUTH-01 / AUTH-02 closure. |
| T-04-04-04 | T (Replay) | id_token replay | mitigate | nonce stored in `rv_oauth_nonce` cookie + included in authorize URL; id_tokens have short `exp` (≤10min per Apple/Google specs); replay window is the gap between provider issuance and backend exchange (we add 10s timeout in Task 3). For a strict nonce check, backend would need to accept + verify the nonce — flagged as future hardening item (T-04-04-04-followup in SUMMARY). |
| T-04-04-05 | I (Info disclosure) | error param leaks state values | accept | Error redirects to `/login?error=oauth_state|oauth_denied` carry only the error code, never the state payload |
| T-04-04-06 | T (Tampering) | rv_oauth_state cookie | mitigate | HttpOnly + SameSite=Strict; only the Server Action `startOAuth` writes it; only `completeOAuth` reads + clears it |
| T-04-04-07 | E (Elevation) | logged-out user re-uses old state | mitigate | 5-min TTL on rv_oauth_state cookie; Task 3 clears the cookie on every callback (success or failure) |
| T-04-04-08 | I (Info disclosure) | backend response shape leaks more than email/plan_id | mitigate | Task 3 destructures ONLY `access_token`, `refresh_token`, `user.email`, `user.plan_id` (fallback) — additional fields are discarded; rv_user only carries email+planId |
| T-04-04-09 | S (Provider impersonation) | typo'd authorize URL | mitigate | Task 1 hard-codes `appleid.apple.com` and `accounts.google.com` constants; no env-driven provider domain |
| T-04-04-10 | D (DoS) | backend hang during exchange | mitigate | `AbortSignal.timeout(10000)` on exchange fetch; failure → `/login?error=oauth_denied` |
| T-04-04-11 | E (Elevation via planId divergence) | response body says planId=pro but JWT says planId=free | mitigate | Task 3 prefers `decodePlanIdFromJwt(accessToken)` — the JWT claim is what middleware enforces server-side as `c.Locals("plan_id")`, so client display is always the same value the backend uses for authz. Body fallback only applies if JWT decode returns "" (backward-compat for pre-D-29 JWTs in the transition window). |
| T-04-04-12 | I (Info disclosure via stale rv_user) | rv_user expired but rv_at still valid | mitigate | Task 3 sets rv_user Max-Age to COOKIE_MAX_AGE.USER (30d, matches refresh) per Plan 03 B2/W5 fix; rv_user is also re-issued by the proxy on every refresh rotation. The 5-min rv_at expiry no longer causes empty email/planId in getSession(). |
</threat_model>

<verification>
- SC #1: After successful Apple sign-in, `document.cookie` (in browser DevTools) shows NO `rv_at`/`rv_rt`/`rv_user` (they're HttpOnly); `localStorage.length === 0` (no token storage)
- SC #1 (continued): `/dashboard` shows email + plan (verified end-to-end in Plan 06 + Plan 08); plan matches the JWT's plan_id claim
- CSRF: tampered state cookie → /login?error=oauth_state (Playwright test in Plan 08)
- Open redirect: state with `next=https://evil.com` → falls through to /<locale>/dashboard (isSafeNextPath rejects it; Plan 08 test)
- rv_user freshness: after a (mocked) JWT with `plan_id: "pro"` is returned, the rv_user cookie decodes to `{email, planId: "pro"}` (Plan 08 smoke)
- Build: `npm run build` exits 0 with all 5 OAuth env vars provided
</verification>

<success_criteria>
- /<locale>/login renders with Apple + Google brand-mark buttons (WEB-01)
- Server-side click handlers redirect to provider authorize URLs with state + nonce + form_post
- /auth/callback verifies state with constant-time compare before backend exchange
- Successful exchange sets rv_at + rv_rt + rv_user HttpOnly cookies (WEB-02)
- rv_user.planId comes from the JWT claim (D-17 source-of-truth), not the response body
- rv_user Max-Age = 30d (matches refresh) so it survives access-token rotation
- Redirect target validated via isSafeNextPath (defense-in-depth against open redirect)
- Failure modes (denied, state mismatch) redirect to /login with i18n error codes
</success_criteria>

<output>
After completion, create `.planning/phases/04-landing-surfaces/04-04-login-oauth-callback-SUMMARY.md` documenting:
- form_post mode chosen for both Apple AND Google so the callback is symmetric
- nonce verification flagged as Phase-future hardening (backend doesn't currently check nonces)
- W1 deprecation note: **Google `response_type=id_token` (implicit flow) is on the deprecation path. Migrating to PKCE + authorization code will require backend changes to `/auth/google` to accept a `code` + exchange server-to-server with Google's token endpoint. Captured as Phase 5+ follow-up.**
- isSafeNextPath regex + the allow-listed path patterns
- Apple/Google dashboard config requirements (operator task — redirect URIs to register)
- B2 closure: rv_user.planId comes from `decodePlanIdFromJwt(access_token)` (Plan 03 helper), Max-Age = 30d (matches refresh-token TTL) — see Plan 03 SUMMARY for the full rationale
</output>
</content>
</invoke>