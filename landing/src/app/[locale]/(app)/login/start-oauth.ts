"use server";

import { cookies } from "next/headers";
import { redirect } from "next/navigation";

import {
  COOKIE_MAX_AGE,
  COOKIE_NAMES,
  sessionCookieAttrs,
} from "@/lib/cookies";
import {
  buildAppleAuthorizeUrl,
  buildGoogleAuthorizeUrl,
  encodeState,
  isSafeNextPath,
  randomB64Url,
} from "@/lib/oauth";

/**
 * Server Action: kick off the OAuth round-trip for Apple or Google.
 *
 * Pre-redirect work (must all happen BEFORE we navigate to the provider):
 *   1. Generate a 32-byte CSRF token (`csrf`) and a 32-byte nonce (`nonce`).
 *   2. Validate the optional `next` path via isSafeNextPath — silently drop
 *      it if unsafe. We DO NOT echo unsafe input back to the caller.
 *   3. Encode the StatePayload (carrying csrf, next, plan/period/currency
 *      checkout hints, and locale).
 *   4. Set the HttpOnly `rv_oauth_state` cookie to the raw `csrf` value
 *      (5-min TTL) — the callback constant-time-compares `state.csrf`
 *      against this cookie value.
 *   5. Set the HttpOnly `rv_oauth_nonce` cookie (5-min TTL) — currently
 *      stored for forward-compat; backend nonce verification is a Phase
 *      5+ hardening item (see SUMMARY).
 *   6. Redirect to the provider's authorize URL.
 *
 * Threat closures:
 * - T-04-04-01 (CSRF):  csrf in state + cookie + constant-time compare on return
 * - T-04-04-06 (state cookie tamper): HttpOnly + SameSite=Strict on the cookie
 * - T-04-04-07 (replay): 5-min TTL on the cookie via COOKIE_MAX_AGE.OAUTH_STATE
 */

type Provider = "apple" | "google";

type StartArgs = {
  provider: Provider;
  locale: string;
  next?: string;
  plan?: string;
  period?: string;
  currency?: string;
};

export async function startOAuth(args: StartArgs): Promise<never> {
  const csrf = randomB64Url(32);
  const nonce = randomB64Url(32);

  // Drop unsafe `next` values silently — they'd just be ignored at callback
  // time anyway, but stripping here keeps the encoded state minimal.
  const safeNext =
    args.next && isSafeNextPath(args.next) ? args.next : undefined;

  const payload = encodeState({
    csrf,
    locale: args.locale,
    next: safeNext,
    plan: args.plan || undefined,
    period: args.period || undefined,
    currency: args.currency || undefined,
  });

  const jar = await cookies();
  jar.set({
    name: COOKIE_NAMES.OAUTH_STATE,
    value: csrf,
    ...sessionCookieAttrs(COOKIE_MAX_AGE.OAUTH_STATE),
  });
  // Stash nonce in a sibling cookie. Apple/Google echo the nonce into the
  // id_token; the backend will verify the claim when we wire up nonce checks
  // (Phase 5+ hardening — flagged in SUMMARY as T-04-04-04 follow-up).
  jar.set({
    name: "rv_oauth_nonce",
    value: nonce,
    ...sessionCookieAttrs(COOKIE_MAX_AGE.OAUTH_STATE),
  });

  const url =
    args.provider === "apple"
      ? buildAppleAuthorizeUrl(payload, nonce)
      : buildGoogleAuthorizeUrl(payload, nonce);

  // redirect() throws NEXT_REDIRECT; declared `never` to make TS happy at
  // call sites.
  redirect(url);
}

/**
 * FormData wrapper so the `<AuthButtonApple>` / `<AuthButtonGoogle>`
 * components can stay Server Components — a Server Action used as a
 * <form action=...> handler receives the FormData as its single argument.
 *
 * Hidden inputs the form must carry:
 *   - provider: "apple" | "google" (required)
 *   - locale:   "ru" | "en" | "es" (required)
 *   - next:     optional, locale-less safe path (e.g., "/pricing")
 *   - plan:     optional, "pro"
 *   - period:   optional, "monthly" | "yearly"
 *   - currency: optional, "USD" | "EUR" | "RUB"
 */
export async function startOAuthForm(formData: FormData): Promise<never> {
  const provider = formData.get("provider");
  const locale = formData.get("locale");
  if (provider !== "apple" && provider !== "google") {
    redirect(`/${typeof locale === "string" ? locale : "ru"}/login?error=oauth_denied`);
  }
  if (typeof locale !== "string" || locale === "") {
    redirect(`/ru/login?error=oauth_denied`);
  }
  const next = formData.get("next");
  const plan = formData.get("plan");
  const period = formData.get("period");
  const currency = formData.get("currency");
  return startOAuth({
    provider,
    locale,
    next: typeof next === "string" && next ? next : undefined,
    plan: typeof plan === "string" && plan ? plan : undefined,
    period: typeof period === "string" && period ? period : undefined,
    currency: typeof currency === "string" && currency ? currency : undefined,
  });
}
