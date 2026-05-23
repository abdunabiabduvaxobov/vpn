import "server-only";

import { cookies } from "next/headers";
import { NextResponse, type NextRequest } from "next/server";

import {
  COOKIE_MAX_AGE,
  COOKIE_NAMES,
  clearCookieAttrs,
  sessionCookieAttrs,
} from "./cookies";
import { env } from "./env";
import {
  decodePlanIdFromJwt,
  decodeSessionUser,
  encodeSessionUser,
} from "./session-cookie";

/**
 * Core same-origin proxy (Phase 4 D-07/D-09/D-17).
 *
 * Algorithm:
 *   1. Build upstream URL: `${BACKEND_API_URL}/api/v1/${segments}` + qs.
 *   2. Read rv_at / rv_rt / rv_user cookies.
 *   3. Forward Content-Type, Accept-Language, User-Agent. Inject
 *      Authorization: Bearer <rv_at>. NEVER forward Cookie:.
 *   4. Buffer request body (≤ BODY_BYTES_LIMIT). Replay on retry.
 *   5. fetch with AbortSignal.timeout(UPSTREAM_TIMEOUT_MS).
 *   6. On non-401: pipe response through.
 *   7. On 401 + rv_rt present (one-shot guard `triedRefresh`):
 *      a. POST /api/v1/auth/refresh { refresh_token }.
 *      b. If 200: decode plan_id from new access_token JWT; re-issue
 *         rv_user with {email: prior, planId: new || prior}; write
 *         rv_at/rv_rt/rv_user with locked attrs; retry original ONCE
 *         with new Bearer; pipe retry response (carrying new Set-Cookies).
 *      c. If non-200: 401 + clear all three session cookies.
 */

const BODY_BYTES_LIMIT = 64 * 1024; // PERF-09 alignment (T-04-03-04)
const UPSTREAM_TIMEOUT_MS = 15_000;

// Hop-by-hop / connection headers we never propagate to the upstream.
const STRIP_REQUEST_HEADERS = new Set([
  "host",
  "connection",
  "content-length",
  "cookie",
  "x-forwarded-host",
  "x-forwarded-proto",
  "x-forwarded-for",
  "x-forwarded-port",
]);

// Response headers we keep when piping the upstream response back.
const PASSTHROUGH_RESPONSE_HEADERS = [
  "content-type",
  "cache-control",
  "etag",
  "last-modified",
  "x-request-id",
];

function buildUpstreamUrl(segments: string[], origin: URL): string {
  const path = segments.map(encodeURIComponent).join("/");
  const upstream = new URL(`/api/v1/${path}`, env.BACKEND_API_URL);
  upstream.search = origin.search;
  return upstream.toString();
}

function buildForwardHeaders(
  req: NextRequest,
  accessToken: string | undefined,
): Headers {
  const headers = new Headers();
  req.headers.forEach((value, key) => {
    if (STRIP_REQUEST_HEADERS.has(key.toLowerCase())) return;
    if (key.toLowerCase() === "authorization") return; // we set our own
    headers.set(key, value);
  });
  if (accessToken) {
    headers.set("Authorization", `Bearer ${accessToken}`);
  }
  return headers;
}

function pipeResponseHeaders(upstream: Response, res: NextResponse): void {
  for (const name of PASSTHROUGH_RESPONSE_HEADERS) {
    const v = upstream.headers.get(name);
    if (v !== null) res.headers.set(name, v);
  }
}

async function bufferBody(
  req: NextRequest,
): Promise<{ body: ArrayBuffer | undefined; tooLarge: boolean }> {
  const method = req.method.toUpperCase();
  if (method === "GET" || method === "HEAD" || method === "DELETE") {
    return { body: undefined, tooLarge: false };
  }
  // Defensive Content-Length check before reading the stream.
  const declared = req.headers.get("content-length");
  if (declared && Number(declared) > BODY_BYTES_LIMIT) {
    return { body: undefined, tooLarge: true };
  }
  const buf = await req.arrayBuffer();
  if (buf.byteLength > BODY_BYTES_LIMIT) {
    return { body: undefined, tooLarge: true };
  }
  return { body: buf, tooLarge: false };
}

type RefreshResult =
  | { ok: true; accessToken: string; refreshToken: string }
  | { ok: false };

async function callRefresh(refreshToken: string): Promise<RefreshResult> {
  try {
    const resp = await fetch(`${env.BACKEND_API_URL}/api/v1/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: refreshToken }),
      signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
    });
    if (!resp.ok) return { ok: false };
    const json = (await resp.json()) as
      | { access_token?: unknown; refresh_token?: unknown }
      | null;
    if (
      !json ||
      typeof json.access_token !== "string" ||
      typeof json.refresh_token !== "string"
    ) {
      return { ok: false };
    }
    return {
      ok: true,
      accessToken: json.access_token,
      refreshToken: json.refresh_token,
    };
  } catch {
    return { ok: false };
  }
}

function clearSessionCookies(res: NextResponse): void {
  const attrs = clearCookieAttrs();
  res.cookies.set({ name: COOKIE_NAMES.ACCESS, value: "", ...attrs });
  res.cookies.set({ name: COOKIE_NAMES.REFRESH, value: "", ...attrs });
  res.cookies.set({ name: COOKIE_NAMES.USER, value: "", ...attrs });
}

function setSessionCookies(
  res: NextResponse,
  accessToken: string,
  refreshToken: string,
  userCookie: string,
): void {
  res.cookies.set({
    name: COOKIE_NAMES.ACCESS,
    value: accessToken,
    ...sessionCookieAttrs(COOKIE_MAX_AGE.ACCESS),
  });
  res.cookies.set({
    name: COOKIE_NAMES.REFRESH,
    value: refreshToken,
    ...sessionCookieAttrs(COOKIE_MAX_AGE.REFRESH),
  });
  res.cookies.set({
    name: COOKIE_NAMES.USER,
    value: userCookie,
    ...sessionCookieAttrs(COOKIE_MAX_AGE.USER),
  });
}

async function pipeUpstream(upstream: Response): Promise<NextResponse> {
  const body = await upstream.arrayBuffer();
  const res = new NextResponse(body.byteLength === 0 ? null : body, {
    status: upstream.status,
  });
  pipeResponseHeaders(upstream, res);
  return res;
}

export async function proxyToBackend(
  req: NextRequest,
  segments: string[],
): Promise<NextResponse> {
  // 1. URL + 2. Cookies + 3. Headers
  const upstreamUrl = buildUpstreamUrl(segments, new URL(req.url));
  const jar = await cookies();
  const at = jar.get(COOKIE_NAMES.ACCESS)?.value;
  const rt = jar.get(COOKIE_NAMES.REFRESH)?.value;
  const priorUserCookie = jar.get(COOKIE_NAMES.USER)?.value;

  // 4. Buffer body once (replayable across retry).
  const { body, tooLarge } = await bufferBody(req);
  if (tooLarge) {
    return NextResponse.json(
      { error: "payload_too_large" },
      { status: 413 },
    );
  }

  const forwardHeaders = buildForwardHeaders(req, at);

  let triedRefresh = false;

  // 5. Initial upstream call.
  let upstream: Response;
  try {
    upstream = await fetch(upstreamUrl, {
      method: req.method,
      headers: forwardHeaders,
      body,
      signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
      // Important: never forward credentials. We use Authorization header.
      credentials: "omit",
      redirect: "manual",
    });
  } catch {
    return NextResponse.json(
      { error: "upstream_unavailable" },
      { status: 502 },
    );
  }

  // 6. Non-401 — pipe through.
  if (upstream.status !== 401) {
    return pipeUpstream(upstream);
  }

  // 7. 401 path — refresh + retry once.
  if (!rt || triedRefresh) {
    // No refresh material — clear cookies + surface 401 to browser.
    const res = await pipeUpstream(upstream);
    if (rt) {
      // Defensive — shouldn't reach here with triedRefresh=true on first pass.
      clearSessionCookies(res);
    }
    return res;
  }

  triedRefresh = true;
  const refresh = await callRefresh(rt);
  if (!refresh.ok) {
    const res = NextResponse.json(
      { error: "session_expired" },
      { status: 401 },
    );
    clearSessionCookies(res);
    return res;
  }

  // B2/D-17 fix — re-issue rv_user with fresh plan_id from the new JWT.
  const newPlanId = decodePlanIdFromJwt(refresh.accessToken);
  const priorUser = decodeSessionUser(priorUserCookie);
  const priorEmail = priorUser?.email ?? "";
  const fallbackPlanId = priorUser?.planId ?? "";
  const effectivePlanId = newPlanId || fallbackPlanId;
  const newUserCookie = encodeSessionUser({
    email: priorEmail,
    planId: effectivePlanId,
  });

  // Retry original request with NEW Bearer header.
  const retryHeaders = buildForwardHeaders(req, refresh.accessToken);
  let retry: Response;
  try {
    retry = await fetch(upstreamUrl, {
      method: req.method,
      headers: retryHeaders,
      body,
      signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS),
      credentials: "omit",
      redirect: "manual",
    });
  } catch {
    const res = NextResponse.json(
      { error: "upstream_unavailable" },
      { status: 502 },
    );
    // Still propagate the fresh rotated tokens — refresh succeeded; the
    // retry failure is transient and the browser can replay later.
    setSessionCookies(
      res,
      refresh.accessToken,
      refresh.refreshToken,
      newUserCookie,
    );
    return res;
  }

  const res = await pipeUpstream(retry);
  setSessionCookies(
    res,
    refresh.accessToken,
    refresh.refreshToken,
    newUserCookie,
  );
  return res;
}
