import { cookies } from "next/headers";
import { NextResponse, type NextRequest } from "next/server";

import { COOKIE_NAMES, clearCookieAttrs } from "@/lib/cookies";
import { env } from "@/lib/env";

/**
 * Dedicated /api/auth/logout endpoint (Phase 4 D-25).
 *
 * Higher specificity than the catch-all /api/[...path] so Next.js routes here
 * first. The endpoint:
 *   1. Reads the current rv_at (best-effort).
 *   2. Builds a 204 response that ALWAYS clears rv_at / rv_rt / rv_user
 *      via Max-Age=0 — even if the backend call fails the browser must
 *      not be left "stuck logged in" (D-25).
 *   3. Forwards POST /api/v1/auth/logout with Bearer when present;
 *      swallows backend errors (local cookie clear is the source of
 *      truth for the browser).
 *
 * Threat T-04-03-10 (logout without access token): cookies still cleared.
 * Threat T-04-03-05 (backend hang): 5s upstream cap via AbortSignal.timeout.
 */

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

const LOGOUT_TIMEOUT_MS = 5_000;

export async function POST(_req: NextRequest) {
  const jar = await cookies();
  const at = jar.get(COOKIE_NAMES.ACCESS)?.value;

  // Always clear cookies regardless of backend outcome (D-25 — never leave
  // the user stuck logged in).
  const res = new NextResponse(null, { status: 204 });
  for (const name of [
    COOKIE_NAMES.ACCESS,
    COOKIE_NAMES.REFRESH,
    COOKIE_NAMES.USER,
  ]) {
    res.cookies.set({ name, value: "", ...clearCookieAttrs() });
  }

  if (!at) return res;

  try {
    await fetch(`${env.BACKEND_API_URL}/api/v1/auth/logout`, {
      method: "POST",
      headers: { Authorization: `Bearer ${at}` },
      signal: AbortSignal.timeout(LOGOUT_TIMEOUT_MS),
    });
  } catch {
    // Swallow — local cookie clear is the source of truth for the browser.
  }

  return res;
}
