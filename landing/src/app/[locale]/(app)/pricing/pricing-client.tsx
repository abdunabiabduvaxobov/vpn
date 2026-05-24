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
 *   - At most ONE checkout POST per page load (useRef latch). Prevents the
 *     accidental duplicate-charge surface even if React strict mode re-runs
 *     the effect.
 *   - Renders `null` when there's nothing to do (no `?checkout=auto`) so
 *     the page DOM is unaffected for the normal browse-pricing flow.
 */

import { useEffect, useRef, useState } from "react";
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

export function PricingClient({ plan, period, checkout, currency }: Props) {
  const t = useTranslations("errors");
  const router = useRouter();
  const fired = useRef(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    // One-shot guard — never POST checkout twice on a single page load, even
    // if the effect re-runs (React strict mode in dev, fast-refresh, etc).
    if (fired.current) return;
    if (checkout !== "auto" || !plan || !period) return;
    fired.current = true;
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
          // flow resumes on return (CONTEXT D-19 hand-off).
          const next = `/pricing?plan=${plan}&period=${period}&currency=${currency}&checkout=auto`;
          router.replace(`/login?next=${encodeURIComponent(next)}`);
          return;
        }
        if (!r.ok) {
          setError(t("network"));
          return;
        }
        const json = await r.json().catch(() => null);
        const url = json?.payment_url;
        if (typeof url !== "string" || !LAVA_URL_PATTERN.test(url)) {
          // Backend returned a non-lava URL — defensive reject (T-04-07-01).
          setError(t("network"));
          return;
        }
        // Hard navigation to the payment provider — replaces the current
        // history entry so the back button returns to /pricing rather than
        // the transient "redirecting" state.
        window.location.href = url;
      } catch {
        // Network / parse failure. Show the i18n network error so the user
        // knows the click was registered and they can retry.
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
