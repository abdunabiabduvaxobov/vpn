import "server-only";

import { cookies } from "next/headers";

import { COOKIE_NAMES } from "./cookies";
import { decodeSessionUser } from "./session-cookie";

/**
 * Server-only session reader used by app-page layouts and server components
 * to determine logged-in/out state without ever exposing tokens to the browser.
 *
 * Reads rv_at to detect "has session?" and rv_user (HMAC-verified via
 * decodeSessionUser — Plan 03 T-04-02-02 closure) for the identity payload.
 * Falls back to {isAuthed:true, email:"", planId:""} when rv_user is missing
 * or fails HMAC verification — callers can then surface "—" rather than
 * crashing.
 */

export type Session =
  | { isAuthed: false }
  | { isAuthed: true; email: string; planId: string };

export async function getSession(): Promise<Session> {
  const jar = await cookies();
  const at = jar.get(COOKIE_NAMES.ACCESS)?.value;
  if (!at) return { isAuthed: false };
  const userRaw = jar.get(COOKIE_NAMES.USER)?.value;
  const user = decodeSessionUser(userRaw);
  if (!user) {
    // Cookie de-sync OR HMAC verification failed — treat as authed but
    // unknown identity. Downstream pages can fetch /api/me via the proxy.
    return { isAuthed: true, email: "", planId: "" };
  }
  return { isAuthed: true, email: user.email, planId: user.planId };
}
