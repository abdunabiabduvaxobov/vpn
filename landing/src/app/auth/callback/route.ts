import { NextRequest, NextResponse } from "next/server";

import { completeOAuth } from "./exchange";

/**
 * POST /auth/callback?provider=<apple|google>
 *
 * The primary OAuth receiver. Both Apple and Google authorize with
 * response_mode=form_post, so the id_token + state arrive as a
 * `application/x-www-form-urlencoded` body. (Some Apple flows use
 * `multipart/form-data` — handled defensively below.)
 *
 * The handler:
 *   1. Validates the provider query param.
 *   2. Parses the form body (urlencoded or multipart).
 *   3. Extracts id_token + state (+ optional error).
 *   4. Calls completeOAuth — which always redirects via Next.js's
 *      `redirect()` throw mechanism. Next 16 App Router converts the
 *      NEXT_REDIRECT digest into a 3xx response automatically inside
 *      route handlers.
 *
 * The `return new NextResponse(null, { status: 500 })` at the bottom is
 * unreachable in normal operation — kept as a defensive marker so any
 * future regression that swallows the redirect throw still surfaces a
 * visible error instead of silently 200-ing.
 *
 * Body size: form_post bodies from Apple/Google carry a JWT (~1-2 KB)
 * plus a state b64url string (~200 bytes). Far below the proxy's 64 KB
 * cap, so no separate size guard here.
 */
export const dynamic = "force-dynamic";
export const runtime = "nodejs";

type Provider = "apple" | "google";

export async function POST(req: NextRequest) {
  const url = new URL(req.url);
  const providerParam = url.searchParams.get("provider");
  if (providerParam !== "apple" && providerParam !== "google") {
    return new NextResponse("Bad provider", { status: 400 });
  }
  const provider: Provider = providerParam;

  const contentType = req.headers.get("content-type") ?? "";
  let form: URLSearchParams;
  if (contentType.includes("application/x-www-form-urlencoded")) {
    form = new URLSearchParams(await req.text());
  } else if (contentType.includes("multipart/form-data")) {
    const fd = await req.formData();
    form = new URLSearchParams();
    for (const [k, v] of fd.entries()) {
      form.set(k, typeof v === "string" ? v : "");
    }
  } else {
    // Unknown content type — pass empty form so completeOAuth's
    // missing-id_token branch fires and redirects to /login?error=oauth_denied.
    form = new URLSearchParams();
  }

  const idToken = form.get("id_token") ?? "";
  const state = form.get("state") ?? "";
  const error = form.get("error");

  // completeOAuth always throws NEXT_REDIRECT — Next.js's App Router
  // converts that into a 3xx response with the Location header set.
  if (error || !idToken) {
    await completeOAuth({ provider, idToken: "", state });
  } else {
    await completeOAuth({ provider, idToken, state });
  }

  // Unreachable in normal operation (see header comment).
  return new NextResponse(null, { status: 500 });
}
