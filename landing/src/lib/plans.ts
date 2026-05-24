import "server-only";

import { env } from "./env";
import type { Currency } from "./locale-currency";

/**
 * Phase 4 D-13 — server-only plans fetch with fetch-level ISR tag cache.
 *
 * Calls the backend's public `GET /api/v1/plans?currency=<C>` (Phase 3
 * plan 03-07) and caches the body at the Next.js fetch layer with:
 *   - revalidate: 600   → background refresh every 10 minutes
 *   - tags: ["plans"]   → tag-bust via `revalidateTag("plans")` in the
 *                         /api/revalidate-pricing route (Task 3) lets the
 *                         backend admin write handlers invalidate the cache
 *                         on every successful plan/offer write
 *
 * We deliberately do NOT use Next's `cache: "force-cache"` directive — the
 * `revalidate` + `tags` options already give us the same data-cache semantics,
 * AND keep the per-request page render dynamic so getSession() works for
 * per-user CTA branching (T-04-05-09 closure).
 *
 * On any non-2xx OR network failure we return [] so the page renders the
 * i18n-keyed empty state rather than crashing (UI-SPEC §pricing).
 */

export type PlanOffer = {
  period: "monthly" | "yearly";
  price: number;
  currency: Currency;
  lava_offer_id: string;
};

export type Plan = {
  code: string;
  name: string;
  is_system: boolean;
  device_limit: number;
  monthly_traffic_mb: number;
  server_countries: string[];
  offers: PlanOffer[];
};

type PlansResponse = {
  plans?: unknown;
};

export async function fetchPlans(currency: Currency): Promise<Plan[]> {
  const url = `${env.BACKEND_API_URL}/api/v1/plans?currency=${currency}`;
  try {
    const r = await fetch(url, {
      next: { tags: ["plans"], revalidate: 600 },
      signal: AbortSignal.timeout(8000),
    });
    if (!r.ok) {
      // Surface empty array → page renders the i18n empty state.
      console.warn("[plans] fetch non-ok", { status: r.status, currency });
      return [];
    }
    const json = (await r.json().catch(() => ({}))) as PlansResponse;
    const plans = Array.isArray(json?.plans) ? (json.plans as Plan[]) : [];
    return plans;
  } catch (err) {
    // Network failure / timeout / abort. Empty state.
    console.warn("[plans] fetch failed", { currency, error: String(err) });
    return [];
  }
}
