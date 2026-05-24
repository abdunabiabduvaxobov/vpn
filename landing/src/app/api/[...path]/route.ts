import type { NextRequest } from "next/server";

import { proxyToBackend } from "@/lib/proxy";

/**
 * Catch-all /api/* proxy (Phase 4 D-07).
 *
 * Forwards every browser /api/* request to `${BACKEND_API_URL}/api/v1/...`
 * with cookie→Bearer transformation and 401→refresh→retry handled by
 * proxyToBackend. Dedicated routes (`/api/auth/logout`,
 * `/api/revalidate-pricing` in Plan 05, `/api/auth/callback/*` in Plan 04)
 * have higher specificity and are matched FIRST by Next.js routing.
 */

export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type Ctx = { params: Promise<{ path: string[] }> };

async function handle(req: NextRequest, { params }: Ctx) {
  const { path } = await params;
  return proxyToBackend(req, path);
}

export const GET = handle;
export const POST = handle;
export const PATCH = handle;
export const PUT = handle;
export const DELETE = handle;
