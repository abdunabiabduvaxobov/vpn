import { timingSafeEqual } from "node:crypto";

import { revalidateTag } from "next/cache";
import { NextResponse, type NextRequest } from "next/server";

import { env } from "@/lib/env";

/**
 * POST /api/revalidate-pricing?secret=<REVALIDATE_SECRET>
 *
 * Phase 4 D-14 — admin-side fan-out callback. The Go backend's plan / offer
 * admin write handlers (Phase 3 plans 03-05/03-06) call this endpoint after
 * each successful write to invalidate the landing's fetch-tag cache for
 * "plans". Combined with `fetchPlans`'s `next: { tags: ["plans"] }` (Task 1),
 * the next /pricing request after an admin write fetches fresh data from
 * the backend rather than serving the (now-stale) cached body.
 *
 * Security model (closes T-04-05-01 and T-04-05-07):
 *
 *   1. `dynamic = "force-dynamic"` — the route MUST run per-request; never
 *      cache the auth response.
 *   2. `runtime = "nodejs"` — node:crypto.timingSafeEqual is a Node API.
 *      Edge runtime would silently fall back to a non-constant-time compare.
 *   3. Constant-time compare via `node:crypto.timingSafeEqual`. Length-mismatch
 *      bails BEFORE the compare (timingSafeEqual throws on length mismatch
 *      — handle as 401, do not leak the difference).
 *   4. Mismatched secret → 401 with no body content beyond `{error}`. No
 *      timing side channel beyond the constant-time compare itself.
 *   5. The secret is server-side only — `env.REVALIDATE_SECRET` lives in
 *      `lib/env.ts` which is `import "server-only"` (T-04-05-02 closure).
 *
 * Phase-future hardening flag (per plan): move the secret from `?secret=` to
 * a request header (e.g. `X-Revalidate-Token`) so it doesn't appear in nginx
 * access logs. Accepted for Phase 4 given low bust rate + HTTPS enforcement.
 */

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

function safeEq(a: string, b: string): boolean {
  const ab = Buffer.from(a, "utf8");
  const bb = Buffer.from(b, "utf8");
  if (ab.length !== bb.length) {
    // timingSafeEqual throws on length mismatch; short-circuit to avoid the
    // throw but still keep the no-information-leak invariant — both branches
    // (wrong length / wrong bytes) return the same 401 below.
    return false;
  }
  return timingSafeEqual(ab, bb);
}

export async function POST(req: NextRequest) {
  const url = new URL(req.url);
  const provided = url.searchParams.get("secret") ?? "";

  if (!safeEq(provided, env.REVALIDATE_SECRET)) {
    return NextResponse.json({ error: "unauthorized" }, { status: 401 });
  }

  // Bust the fetch-tag cache. All in-flight /pricing renders that touched
  // the "plans" tag will refetch on next render. Cheap — Next maintains an
  // O(1) tag → cache-entry index.
  //
  // Next.js 16 made the second argument REQUIRED (CacheLifeProfile or
  // CacheLifeConfig). Passing "max" preserves the single-arg legacy semantics
  // of "hard-invalidate immediately on next read"; updateTag() is not usable
  // here because it's restricted to Server Actions.
  // See: https://nextjs.org/docs/messages/revalidate-tag-single-arg
  revalidateTag("plans", "max");

  return NextResponse.json(
    { revalidated: true, tag: "plans" },
    { status: 200 },
  );
}
