import "server-only";

import { cookies } from "next/headers";

/**
 * Server-only session reader for Phase 4 surfaces (/login /dashboard /pricing
 * /pay/*). Reads the two HttpOnly cookies set by:
 *   - Plan 03 proxy / Plan 04 OAuth callback: `rv_at` (access token, HttpOnly,
 *     Secure, SameSite=Strict — proof of authentication)
 *   - Plan 04 OAuth callback: `rv_user` (HttpOnly, Secure, SameSite=Strict —
 *     base64url-encoded `{email, planId}` snapshot for UI hydration so we
 *     don't have to round-trip /api/me for the navbar)
 *
 * THIS FILE INTENTIONALLY ONLY READS COOKIES. Setting them is owned by
 * Plan 03's `setSessionCookie()` helper, which will also stamp an HMAC suffix
 * on `rv_user` (see T-04-02-02 mitigation note in the plan threat model).
 * Once Plan 03 lands, this module will switch from `JSON.parse(atob(...))`
 * to calling `readSessionCookie()` for HMAC verification — until then the
 * trust comes from HttpOnly + SameSite=Strict scope (cookie cannot be set or
 * read by client-side JS).
 *
 * Defensive coding: any cookie parse failure degrades to `{isAuthed: true,
 * email: '', planId: ''}` when `rv_at` is present (so the user stays logged
 * in but the UI falls back to placeholder identity) rather than crashing.
 * When `rv_at` is absent we return `{isAuthed: false}`.
 */

export type Session =
  | { isAuthed: false }
  | { isAuthed: true; email: string; planId: string };

export async function getSession(): Promise<Session> {
  const jar = await cookies();
  const at = jar.get("rv_at")?.value;
  if (!at) return { isAuthed: false };

  const userRaw = jar.get("rv_user")?.value;
  if (!userRaw) {
    // Cookie de-sync (e.g. rv_at present but rv_user evicted). Treat as
    // authed but unknown identity; downstream pages can fall back to a
    // placeholder ("you" / "—") and optionally fetch /api/me via the proxy.
    return { isAuthed: true, email: "", planId: "" };
  }

  try {
    const json = JSON.parse(
      Buffer.from(userRaw, "base64url").toString("utf8"),
    ) as { email?: unknown; planId?: unknown };
    const email = typeof json.email === "string" ? json.email : "";
    const planId = typeof json.planId === "string" ? json.planId : "";
    return { isAuthed: true, email, planId };
  } catch {
    return { isAuthed: true, email: "", planId: "" };
  }
}
