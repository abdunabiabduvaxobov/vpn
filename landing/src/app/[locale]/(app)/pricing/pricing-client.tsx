"use client";

/**
 * PricingClient — placeholder owned by Plan 04-07 (checkout flow).
 *
 * Mounted by /pricing's server page and receives the URL's checkout-related
 * search params (`plan`, `period`, `currency`, `checkout`). Plan 04-07 will
 * expand this stub into the auto-checkout effect:
 *
 *   - if `checkout === "auto"` and the user is authed (cookie present),
 *     POST /api/v1/checkout with { plan, period, currency } via the proxy
 *   - on 200 → window.location.href = response.payment_url
 *   - on error → toast({ variant: "destructive", ... })
 *
 * For Plan 04-05 the stub keeps the props in scope (so a future implementer
 * doesn't accidentally drop them) but renders nothing — the page must still
 * compile and ship with the rest of the ISR machinery before Plan 07 ships.
 */

type Props = {
  locale: string;
  plan?: string;
  period?: string;
  checkout?: string;
  currency: string;
};

// Acknowledge the prop shape without consuming it — Plan 07 will swap the
// implementation. Using a typed signature (instead of `_: Props`) preserves
// editor IntelliSense and the contract for the future implementer.
export function PricingClient(_props: Props): null {
  return null;
}
