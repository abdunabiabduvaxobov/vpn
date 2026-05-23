import "server-only";

/**
 * Typed server-only env loader for the landing Node runtime (Phase 4 D-07/D-08/D-14).
 *
 * Every Phase 4 server module (Node proxy, OAuth callback, /api/revalidate-pricing)
 * imports `env` from this file. Required vars are validated at module load — a
 * missing or empty value crashes the server on boot, which is preferable to
 * silently 500-ing on every request.
 *
 * `import "server-only"` is a Next.js marker that causes the bundler to throw at
 * build time if this module is imported into a client component (T-04-01-01).
 *
 * Never add a `NEXT_PUBLIC_` mirror of REVALIDATE_SECRET — it must stay
 * server-side only.
 */

type RequiredKey = "BACKEND_API_URL" | "REVALIDATE_SECRET";

function readRequired(key: RequiredKey): string {
  const v = process.env[key];
  if (!v || v.trim() === "") {
    throw new Error(
      `[landing/env] Required env var ${key} is missing or empty`,
    );
  }
  return v;
}

export const env = Object.freeze({
  // Backend API base URL (no trailing slash). Production: https://vpnapi.mydayai.uz.
  BACKEND_API_URL: readRequired("BACKEND_API_URL"),

  // Shared secret with the Go backend's admin write handlers for
  // /api/revalidate-pricing (D-14). Backend POSTs the landing route with
  // ?secret=<this>; the landing constant-time-compares before calling
  // revalidateTag('plans').
  REVALIDATE_SECRET: readRequired("REVALIDATE_SECRET"),

  // Cookie scope. Empty string = host-only cookie (default for local dev).
  // In production set to "risevpn.com" so cookies survive sub-domains.
  COOKIE_DOMAIN: process.env.COOKIE_DOMAIN ?? "",

  // Standard Next.js runtime env. Default to "development" if unset (tests).
  NODE_ENV: (process.env.NODE_ENV ?? "development") as
    | "development"
    | "production"
    | "test",

  // Convenience flag — Secure cookies only get set when this is true.
  IS_PROD: process.env.NODE_ENV === "production",
});
