import "server-only";

import type { ResponseCookie } from "next/dist/compiled/@edge-runtime/cookies";

import { env } from "./env";

/**
 * Phase 4 D-08 cookie attribute builder.
 *
 * Returns the locked HttpOnly + SameSite=Strict attribute set every session
 * cookie issued by the landing Node runtime must carry. `Secure` is gated by
 * `env.IS_PROD` so http://localhost still works in dev (T-04-03-09 accept).
 *
 * Domain is set from `env.COOKIE_DOMAIN` when non-empty; otherwise omitted so
 * the cookie is host-only — matches local-dev expectations.
 */

type CookieMaxAge = number;

export function sessionCookieAttrs(
  maxAge: CookieMaxAge,
): Omit<ResponseCookie, "name" | "value"> {
  return {
    httpOnly: true,
    secure: env.IS_PROD, // dev allows http://localhost
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
  OAUTH_STATE: "rv_oauth_state", // used by Plan 04
});

/**
 * NOTE on USER Max-Age (B2 fix, W5 mitigation):
 *
 * rv_user must survive natural rv_at rotation (every 5 min) — it is the
 * email+planId source for getSession(). Pinning rv_user to the REFRESH
 * token's TTL means getSession() keeps returning the user's email + plan
 * for the full session, and is RE-WRITTEN by the proxy on every
 * refresh-rotation so its planId stays current with the JWT's plan_id
 * claim (Phase 3 D-29).
 */
export const COOKIE_MAX_AGE = Object.freeze({
  ACCESS: 60 * 5, // 5 min — matches backend access TTL
  REFRESH: 60 * 60 * 24 * 30, // 30 day — matches backend refresh TTL
  USER: 60 * 60 * 24 * 30, // 30 day — matches REFRESH (B2 fix)
  OAUTH_STATE: 60 * 5, // 5 min — CSRF cookie (Plan 04)
});
