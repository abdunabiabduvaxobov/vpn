import "server-only";

import { createHmac, timingSafeEqual } from "node:crypto";

import { env } from "./env";

/**
 * HMAC-signed rv_user cookie serialiser/parser + JWT plan_id decoder.
 *
 * Closes Plan 02's deferred T-04-02-02: rv_user is now tamper-evident via
 * HMAC-SHA256 over base64url(JSON({email, planId})). The signing secret is
 * derived from REVALIDATE_SECRET with a `:session` namespace so a leak of
 * one doesn't trivially compromise the other.
 *
 * B2 fix: decodePlanIdFromJwt parses the `plan_id` claim from a
 * backend-issued access JWT (signature-skipped). Used by the proxy at every
 * refresh rotation to re-issue rv_user with the user's CURRENT plan_id
 * (D-17 closure).
 */

export type SessionUser = { email: string; planId: string };

const HMAC_SECRET = createHmac("sha256", env.REVALIDATE_SECRET + ":session")
  .digest();

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
  const expected = b64url(
    createHmac("sha256", HMAC_SECRET).update(payload).digest(),
  );
  const a = Buffer.from(mac, "base64url");
  const b = Buffer.from(expected, "base64url");
  if (a.length !== b.length || !timingSafeEqual(a, b)) return null;
  try {
    const json = JSON.parse(
      Buffer.from(payload, "base64url").toString("utf8"),
    );
    if (typeof json.email !== "string" || typeof json.planId !== "string") {
      return null;
    }
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
 *
 * Returns "" on any parse failure so callers fall back to prior rv_user.planId.
 *
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
