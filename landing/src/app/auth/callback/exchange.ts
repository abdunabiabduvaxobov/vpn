"use server";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import {
  COOKIE_MAX_AGE,
  COOKIE_NAMES,
  clearCookieAttrs,
  sessionCookieAttrs,
} from "@/lib/cookies";
import { env } from "@/lib/env";
import {
  constantTimeEquals,
  decodeNonceFromIdToken,
  decodeState,
  isSafeNextPath,
} from "@/lib/oauth";
import {
  decodePlanIdFromJwt,
  encodeSessionUser,
} from "@/lib/session-cookie";

/**
 * Server Action: complete the OAuth round-trip.
 *
 * Pipeline:
 *   1. Decode + shape-validate the `state` parameter.
 *   2. Constant-time-compare `state.csrf` against the `rv_oauth_state`
 *      cookie set by startOAuth. Mismatch → clear oauth cookies, redirect
 *      to /<locale>/login?error=oauth_state (T-04-04-01 closure).
 *   3. POST the id_token to the backend's /api/v1/auth/<provider>
 *      endpoint (Phase 2 contract). 10s timeout (T-04-04-10).
 *   4. On non-2xx or network failure → clear oauth cookies, redirect to
 *      /<locale>/login?error=oauth_denied.
 *   5. On success:
 *        - decode plan_id from the new access JWT (D-17 / B2 fix —
 *          backend.user.plan_id is only a fallback)
 *        - encode + set rv_user (HMAC-signed, 30-day TTL — matches refresh)
 *        - set rv_at (5-min) + rv_rt (30-day)
 *        - clear rv_oauth_state + rv_oauth_nonce cookies
 *   6. Validate the destination path with isSafeNextPath. If the original
 *      `next` was `/pricing` AND carried plan/period/currency hints, add
 *      `checkout=auto` so Plan 07's pricing page auto-launches checkout.
 *   7. Redirect.
 *
 * Backend response shape (Phase 2 contract):
 *   {
 *     access_token: string,
 *     refresh_token: string,
 *     expires_in: number,
 *     user: { id, email, plan_id, subscription_tier, ... }
 *   }
 *
 * Threat closures (see plan <threat_model>):
 * - T-04-04-01 (CSRF): constant-time state compare before any backend call
 * - T-04-04-02 (open redirect): isSafeNextPath gates the redirect target
 * - T-04-04-05 (info leak): only the error code lands in the URL
 * - T-04-04-08 (info leak): only access_token, refresh_token, user.email,
 *   user.plan_id are read from the backend response
 * - T-04-04-10 (DoS): AbortSignal.timeout(10000) on the exchange fetch
 * - T-04-04-11 (planId divergence): JWT claim first, body fallback only
 * - T-04-04-12 (stale rv_user): 30-day Max-Age via COOKIE_MAX_AGE.USER
 */

type Provider = "apple" | "google";

type ExchangeArgs = {
  provider: Provider;
  /** Apple/Google form_post id_token; "" when the caller already detected
   *  an error and just wants the redirect-to-login flow. */
  idToken: string;
  state: string;
};

type BackendAuthResponse = {
  access_token?: unknown;
  refresh_token?: unknown;
  expires_in?: unknown;
  user?: {
    id?: unknown;
    email?: unknown;
    plan_id?: unknown;
    subscription_tier?: unknown;
  };
};

const DEFAULT_LOCALE = "ru";

export async function completeOAuth(args: ExchangeArgs): Promise<never> {
  const jar = await cookies();
  const cookieCsrf = jar.get(COOKIE_NAMES.OAUTH_STATE)?.value ?? "";
  const cookieNonce = jar.get("rv_oauth_nonce")?.value ?? "";
  const decoded = decodeState(args.state);
  const locale = decoded?.locale ?? DEFAULT_LOCALE;

  // Defensive: ALWAYS clear the oauth_state + nonce cookies on any exit
  // path (success or failure). Once consumed, they have no business
  // surviving (T-04-04-07).
  const clearOAuthCookies = () => {
    jar.set({
      name: COOKIE_NAMES.OAUTH_STATE,
      value: "",
      ...clearCookieAttrs(),
    });
    jar.set({
      name: "rv_oauth_nonce",
      value: "",
      ...clearCookieAttrs(),
    });
  };

  // State validation gate.
  if (
    !decoded ||
    !cookieCsrf ||
    !constantTimeEquals(cookieCsrf, decoded.csrf)
  ) {
    clearOAuthCookies();
    redirect(`/${locale}/login?error=oauth_state`);
  }

  // If the caller already knows there is no id_token (provider sent
  // error=access_denied, or the user cancelled at Apple), bail early.
  if (!args.idToken) {
    clearOAuthCookies();
    redirect(`/${locale}/login?error=oauth_denied`);
  }

  // CR-01 closure — Nonce binding gate.
  //
  // Apple/Google embed our `nonce` (sent on the authorize URL, also stashed
  // in the HttpOnly rv_oauth_nonce cookie) into the id_token's `nonce`
  // claim. A replay attack with an id_token minted in a DIFFERENT context
  // (e.g. another application's auth flow, a previous session) would pass
  // the CSRF state check (state is freshly minted per session) but the
  // id_token's nonce would not match our cookie. Decoding the JWT here
  // without signature verification is safe — same trust model as
  // decodePlanIdFromJwt: we received the token on a trusted server-to-
  // server hop, the backend does the cryptographic verification, this is
  // a fast-fail edge check.
  //
  // The nonce is ALSO forwarded to the backend so the backend's OIDC
  // verifier can check the JWT nonce claim against the same value the
  // landing pinned (defense in depth).
  const idTokenNonce = decodeNonceFromIdToken(args.idToken);
  if (
    !cookieNonce ||
    !idTokenNonce ||
    !constantTimeEquals(cookieNonce, idTokenNonce)
  ) {
    clearOAuthCookies();
    redirect(`/${locale}/login?error=oauth_state`);
  }

  // Exchange with backend. Single attempt — refresh-retry isn't relevant
  // because we don't have a refresh token yet (this IS sign-in).
  // Resolves to POST /api/v1/auth/apple or POST /api/v1/auth/google
  // (Phase 2 contract — both endpoints accept { id_token, nonce } and
  // return { access_token, refresh_token, expires_in, user }). CR-01:
  // nonce is forwarded so backend can verify the id_token's nonce claim
  // matches what the landing minted.
  let backend: BackendAuthResponse | null = null;
  try {
    const r = await fetch(
      `${env.BACKEND_API_URL}/api/v1/auth/${args.provider}`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ id_token: args.idToken, nonce: cookieNonce }),
        signal: AbortSignal.timeout(10000),
        credentials: "omit",
        cache: "no-store",
      },
    );
    if (!r.ok) {
      clearOAuthCookies();
      redirect(`/${locale}/login?error=oauth_denied`);
    }
    backend = (await r.json()) as BackendAuthResponse;
  } catch (err) {
    // Don't swallow Next.js redirect signals.
    if (isNextRedirect(err)) throw err;
    clearOAuthCookies();
    redirect(`/${locale}/login?error=oauth_denied`);
  }

  // Shape-check the response — anything malformed dumps to oauth_denied
  // rather than letting an empty session through.
  if (!backend) {
    clearOAuthCookies();
    redirect(`/${locale}/login?error=oauth_denied`);
  }
  const accessToken =
    typeof backend.access_token === "string" ? backend.access_token : "";
  const refreshToken =
    typeof backend.refresh_token === "string" ? backend.refresh_token : "";
  const email =
    typeof backend.user?.email === "string" ? backend.user.email : "";
  if (!accessToken || !refreshToken) {
    clearOAuthCookies();
    redirect(`/${locale}/login?error=oauth_denied`);
  }

  // B2 fix (D-17 source-of-truth): planId comes from the JWT's plan_id
  // claim — what backend middleware enforces as c.Locals("plan_id") on
  // every authenticated call. Body fallback only applies if the JWT
  // doesn't carry the claim (pre-D-29 transition window).
  const planIdFromJwt = decodePlanIdFromJwt(accessToken);
  const planIdFromBody =
    typeof backend.user?.plan_id === "string" ? backend.user.plan_id : "";
  const planId = planIdFromJwt || planIdFromBody;

  // Set the three session cookies. rv_user gets COOKIE_MAX_AGE.USER (30d,
  // matches refresh — B2/W5 fix) so it survives natural rv_at rotation.
  jar.set({
    name: COOKIE_NAMES.ACCESS,
    value: accessToken,
    ...sessionCookieAttrs(COOKIE_MAX_AGE.ACCESS),
  });
  jar.set({
    name: COOKIE_NAMES.REFRESH,
    value: refreshToken,
    ...sessionCookieAttrs(COOKIE_MAX_AGE.REFRESH),
  });
  jar.set({
    name: COOKIE_NAMES.USER,
    value: encodeSessionUser({ email, planId }),
    ...sessionCookieAttrs(COOKIE_MAX_AGE.USER),
  });

  clearOAuthCookies();

  // Decide destination. Default = /<locale>/dashboard.
  let target = `/${locale}/dashboard`;
  if (decoded.next && isSafeNextPath(decoded.next)) {
    // Re-attach checkout hints if the original intent was /pricing.
    const q = new URLSearchParams();
    if (decoded.plan) q.set("plan", decoded.plan);
    if (decoded.period) q.set("period", decoded.period);
    if (decoded.currency) q.set("currency", decoded.currency);
    if (decoded.next === "/pricing" && [...q.keys()].length > 0) {
      // Plan 07 hand-off: pricing page sees ?checkout=auto and immediately
      // POSTs to /api/checkout with the carried plan/period/currency.
      q.set("checkout", "auto");
    }
    const localePrefixed = decoded.next.startsWith(`/${locale}/`)
      ? decoded.next
      : `/${locale}${decoded.next}`;
    target = q.toString() ? `${localePrefixed}?${q.toString()}` : localePrefixed;
  }

  redirect(target);
}

/**
 * Next.js's `redirect()` throws a sentinel error with `digest` starting with
 * "NEXT_REDIRECT". Catching `fetch` errors must NOT swallow these — they're
 * the mechanism by which the action navigates.
 */
function isNextRedirect(err: unknown): boolean {
  return (
    typeof err === "object" &&
    err !== null &&
    "digest" in err &&
    typeof (err as { digest?: unknown }).digest === "string" &&
    (err as { digest: string }).digest.startsWith("NEXT_REDIRECT")
  );
}
