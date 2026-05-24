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

type RequiredKey =
  | "BACKEND_API_URL"
  | "REVALIDATE_SECRET"
  | "APPLE_SERVICE_ID"
  | "APPLE_REDIRECT_URI"
  | "GOOGLE_CLIENT_ID_WEB"
  | "GOOGLE_REDIRECT_URI"
  | "APP_URL";

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

  // OAuth (Plan 04) — Apple "Sign in with Apple" + Google OAuth Web client.
  // Sourced from Apple Developer + Google Cloud Console; see .env.example.

  // Apple Service ID — the "Services ID" registered under your Apple Developer
  // Team's "Sign in with Apple" configuration (e.g., "services.risevpn.web").
  // This is the `client_id` parameter Apple expects on the authorize URL.
  APPLE_SERVICE_ID: readRequired("APPLE_SERVICE_ID"),

  // Apple redirect URI — must EXACTLY match the "Return URL" registered for
  // the Service ID in Apple Developer (e.g.,
  // "https://risevpn.com/auth/callback?provider=apple"). Apple POSTs the
  // form_post id_token here.
  APPLE_REDIRECT_URI: readRequired("APPLE_REDIRECT_URI"),

  // Google OAuth Web client ID — the Web-type client created in Google Cloud
  // Console > APIs & Services > Credentials. Mirrors Phase 2 backend's
  // GOOGLE_CLIENT_ID_WEB env so the backend verifier accepts the audience.
  GOOGLE_CLIENT_ID_WEB: readRequired("GOOGLE_CLIENT_ID_WEB"),

  // Google redirect URI — must match the "Authorized redirect URI" registered
  // for the Web client (e.g.,
  // "https://risevpn.com/auth/callback?provider=google").
  GOOGLE_REDIRECT_URI: readRequired("GOOGLE_REDIRECT_URI"),

  // Landing's public origin (e.g., "https://risevpn.com"). Used to build
  // absolute redirect_uri values and for any future absolute-URL needs
  // (sitemap, OG image, etc.). No trailing slash.
  APP_URL: readRequired("APP_URL"),

  // Standard Next.js runtime env. Default to "development" if unset (tests).
  NODE_ENV: (process.env.NODE_ENV ?? "development") as
    | "development"
    | "production"
    | "test",

  // Convenience flag — Secure cookies only get set when this is true.
  IS_PROD: process.env.NODE_ENV === "production",
});
