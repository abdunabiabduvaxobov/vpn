"use client";

/**
 * PricingClient — checkout flow client (Phase 4 Plan 04-07 / WEB-05).
 *
 * Behavior:
 *   - Mounted by /pricing's server page on every render.
 *   - When the URL carries `?checkout=auto&plan=<code>&period=<period>` (and an
 *     active session cookie is present), POSTs once to /api/v1/checkout via
 *     the Plan 03 proxy with `{ plan_code, periodicity, currency }`.
 *   - On 200: validates `payment_url` against an https://*.lava.top
 *     whitelist (defence-in-depth — T-04-07-01) and navigates the browser
 *     there with `window.location.href`.
 *   - On 401: bounces to /<locale>/login with `next=` re-encoded so the
 *     auto-checkout resumes after sign-in (matches CONTEXT D-19).
 *   - On any other failure: renders an inline error using `errors.network`.
 *
 * Guarantees:
 *   - At most ONE checkout POST per page load (module-level Set keyed by
 *     `${plan}|${period}|${currency}`). Prevents the accidental duplicate-
 *     charge surface even if React Strict Mode re-runs the effect after a
 *     simulated unmount → remount cycle (NODE_ENV=development).
 *   - Renders `null` when there's nothing to do (no `?checkout=auto`) so
 *     the page DOM is unaffected for the normal browse-pricing flow.
 */

import { useEffect, useState } from "react";
import { useTranslations } from "next-intl";
import { useRouter } from "@/i18n/navigation";

type Props = {
  locale: string;
  plan?: string;
  period?: string;
  checkout?: string;
  currency: string;
};

// Defence-in-depth whitelist for the backend-supplied payment_url. The backend
// is trusted, but a single regex eliminates the open-redirect-via-payment-
// provider surface entirely — see threat T-04-07-01.
const LAVA_URL_PATTERN = /^https:\/\/(gate\.|app\.|pay\.)?lava\.top\//;

// Module-level one-shot guard. Keyed by (plan, period, currency) so a user
// who genuinely re-attempts a different plan/period/currency combination
// gets a fresh checkout, but the same combination from a Strict Mode
// simulated remount (NODE_ENV=development) does NOT fire a duplicate POST.
//
// Why module-level instead of a per-component ref: React Strict Mode
// unmounts then remounts every component in dev to surface cleanup bugs.
// A per-component boolean ref either (a) loses its value on the real
// mount if reset in the effect body, defeating the latch, or (b) persists
// across mount/remount but the cleanup poisons it, blocking the real
// mount's POST. A module-level Set survives both passes and is checked
// AFTER the early-return conditions so it only protects the actual POST.
const inflightCheckouts = new Set<string>();

export function PricingClient({ plan, period, checkout, currency }: Props) {
  const t = useTranslations("errors");
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (checkout !== "auto" || !plan || !period) return;
    // Strict-Mode-safe one-shot — module-level Set survives the simulated
    // unmount → remount cycle (NODE_ENV=development) AND prevents duplicate
    // POST across legitimate remounts of the same (plan, period, currency).
    const key = `${plan}|${period}|${currency}`;
    if (inflightCheckouts.has(key)) return;
    inflightCheckouts.add(key);
    (async () => {
      try {
        const r = await fetch("/api/v1/checkout", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          credentials: "same-origin",
          body: JSON.stringify({
            plan_code: plan,
            periodicity: period,
            currency,
          }),
        });
        if (r.status === 401) {
          // Session expired between page render and POST. Round-trip back
          // through /login preserving the same auto-checkout intent so the
          // flow resumes on return (CONTEXT D-19 hand-off). Clear the latch:
          // the next page-load re-fires the intent and we want it to POST.
          inflightCheckouts.delete(key);
          const next = `/pricing?plan=${plan}&period=${period}&currency=${currency}&checkout=auto`;
          router.replace(`/login?next=${encodeURIComponent(next)}`);
          return;
        }
        if (!r.ok) {
          // Failure path — clear the latch so the user can retry without
          // changing the URL (browser back, locale swap, or a future "Try
          // again" affordance all re-mount this client on the same query).
          inflightCheckouts.delete(key);
          setError(t("network"));
          return;
        }
        const json = await r.json().catch(() => null);
        const url = json?.payment_url;
        if (typeof url !== "string" || !LAVA_URL_PATTERN.test(url)) {
          // Backend returned a non-lava URL — defensive reject (T-04-07-01).
          inflightCheckouts.delete(key);
          setError(t("network"));
          return;
        }
        // SUCCESS — leave the key in the Set. The hard navigation makes any
        // re-mount irrelevant, and the surviving key prevents a double-POST
        // race if the navigation is slow (a true Strict Mode remount in the
        // gap between fetch resolution and href assignment).
        window.location.href = url;
      } catch {
        // Network / parse failure. Show the i18n network error so the user
        // knows the click was registered and they can retry. Clear the latch
        // so the retry path is not silently no-op'd (WR-01).
        inflightCheckouts.delete(key);
        setError(t("network"));
      }
    })();
  }, [checkout, plan, period, currency, router, t]);

  if (error) {
    return (
      <div
        role="alert"
        className="mx-auto mt-8 max-w-md rounded-[var(--radius-md)] border border-destructive/40 bg-destructive/10 p-3 text-sm text-destructive"
      >
        {error}
      </div>
    );
  }
  return null;
}
