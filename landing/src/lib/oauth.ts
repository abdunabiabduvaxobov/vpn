import "server-only";

import { createHash, randomBytes, timingSafeEqual } from "node:crypto";

import { env } from "./env";

/**
 * OAuth state-CSRF + authorize-URL builders for Apple + Google "Sign In"
 * on the web (Phase 4 Plan 04).
 *
 * The state payload carries a 32-byte random `csrf` that is ALSO written
 * into the `rv_oauth_state` HttpOnly cookie (5-min TTL). The callback
 * constant-time-compares `state.csrf` against the cookie value before
 * exchanging the id_token with the backend — this is the standard
 * OAuth 2.0 §10.12 CSRF mitigation.
 *
 * The state also tunnels the user's intent across the round-trip:
 *   - `next`     — where to send them after sign-in (validated via isSafeNextPath)
 *   - `plan`     — Plan 07 checkout resume hint ("pro")
 *   - `period`   — "monthly" | "yearly"
 *   - `currency` — "USD" | "EUR" | "RUB"
 *   - `locale`   — "ru" | "en" | "es" so the post-auth redirect lands in
 *                  the same language the user started in
 *
 * `buildAppleAuthorizeUrl` / `buildGoogleAuthorizeUrl` are intentionally
 * hard-coded to `appleid.apple.com` / `accounts.google.com` (no env-driven
 * provider domain) so a misconfigured env can never redirect users to a
 * lookalike provider (T-04-04-09 mitigation).
 *
 * Both providers use `response_mode=form_post` so the id_token arrives as
 * a POST body — symmetric callback handling for the single `/auth/callback`
 * route (D-10).
 *
 * W1 NOTE: Google `response_type=id_token` (implicit) is on the deprecation
 * path. Migrating to PKCE + authorization code requires backend changes to
 * `/auth/google` to accept `code` and exchange against Google's token
 * endpoint. Captured as Phase 5+ follow-up.
 */

export type StatePayload = {
  csrf: string;
  next?: string;
  plan?: string;
  period?: string;
  currency?: string;
  locale: string;
};

/** Crypto-strong random base64url-encoded string (default 32 bytes → 43 chars). */
export function randomB64Url(bytes = 32): string {
  return randomBytes(bytes).toString("base64url");
}

/** Encode a StatePayload as base64url(JSON) — safe to pass as a URL query param. */
export function encodeState(p: StatePayload): string {
  return Buffer.from(JSON.stringify(p), "utf8").toString("base64url");
}

/** Decode + shallow-validate a state string. Returns null on any failure. */
export function decodeState(raw: string): StatePayload | null {
  try {
    const json = JSON.parse(Buffer.from(raw, "base64url").toString("utf8"));
    if (typeof json.csrf !== "string" || typeof json.locale !== "string") {
      return null;
    }
    return json as StatePayload;
  } catch {
    return null;
  }
}

/**
 * Locale-prefixed OR locale-less variants of the four legitimate post-auth
 * destinations on the landing app: /login, /dashboard, /pricing,
 * /pay/{success,fail}. Anything else is rejected.
 *
 * This is the open-redirect defense (T-04-04-02). The allow-list is
 * deliberately tight — if a future route needs to be a redirect target,
 * extend the regex here and audit the change.
 */
const SAFE_NEXT_RE =
  /^\/(?:(?:ru|en|es)\/)?(?:login|dashboard|pricing|pay\/(?:success|fail))(?:\?[A-Za-z0-9_\-&=%.+:/,]*)?$/;

export function isSafeNextPath(p: unknown): p is string {
  if (
    typeof p !== "string" ||
    !p.startsWith("/") ||
    p.startsWith("//") ||
    p.startsWith("/\\")
  ) {
    return false;
  }
  if (p.includes("..")) return false;
  return SAFE_NEXT_RE.test(p);
}

/**
 * Constant-time equality check on two UTF-8 strings.
 *
 * Both inputs are first hashed with SHA-256 so the comparison runs on
 * fixed-length 32-byte digests regardless of the original string lengths.
 * This closes the WR-01 timing side-channel where the previous length
 * short-circuit allowed an attacker to distinguish "wrong length" from
 * "right length, wrong bytes" via response-time probing.
 *
 * Trade-off: SHA-256 collisions are computationally infeasible, so this is
 * equivalent to a direct compare for any realistic input pair, while the
 * runtime is now independent of input length AND independent of input
 * content (timingSafeEqual on equal-length buffers is constant-time per
 * Node's docs).
 */
export function constantTimeEquals(a: string, b: string): boolean {
  const ha = createHash("sha256").update(Buffer.from(a, "utf8")).digest();
  const hb = createHash("sha256").update(Buffer.from(b, "utf8")).digest();
  return timingSafeEqual(ha, hb);
}

/**
 * Build the Apple authorize URL.
 * - response_type=code id_token (Apple's spec for native id_token issuance)
 * - response_mode=form_post → Apple POSTs id_token to our redirect_uri
 * - scope=name email → request the user's name + email (returned ONCE on
 *   first consent; we only really need the email — backend dedupes)
 *
 * Endpoint per Apple "Sign in with Apple" REST API:
 * https://developer.apple.com/documentation/sign_in_with_apple/sign_in_with_apple_rest_api
 */
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

/**
 * Build the Google authorize URL.
 * - response_type=id_token → implicit flow (W1: deprecation watch)
 * - response_mode=form_post → Google POSTs id_token to our redirect_uri,
 *   symmetric with Apple so the single /auth/callback route handles both
 * - scope=openid email profile → OIDC standard for email + basic profile
 * - prompt=select_account → always show the account chooser so users
 *   switching between Google accounts aren't silently signed in as the wrong
 *   one
 *
 * Endpoint per OpenID Connect / Google OAuth 2.0:
 * https://developers.google.com/identity/openid-connect/openid-connect
 */
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
