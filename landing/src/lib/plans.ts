import "server-only";

import { env } from "./env";
import type { Currency } from "./locale-currency";

/**
 * fetchPlans — server-only reader for the backend's public plans catalog.
 *
 * Owned by Plan 04-05; included in this worktree so that Plan 04-06's
 * /dashboard page can resolve a planId → plan display name without
 * duplicating the fetch logic. Plan 04-05 also uses this same module on
 * the /pricing page. The orchestrator's merge of the wave-3 worktrees
 * will resolve to this version (Plan 04-05's draft is identical — see
 * 04-05 plan §Task 1).
 *
 * Caching: `next: { tags: ["plans"], revalidate: 600 }` means the same
 * backend response services up to 10 minutes of /dashboard + /pricing
 * traffic per currency. Plan 04-05's POST /api/revalidate-pricing busts
 * the same tag after admin writes, so /dashboard never serves a stale
 * plan name for more than the time between an admin write and the next
 * /dashboard request.
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

export async function fetchPlans(currency: Currency): Promise<Plan[]> {
  const url = `${env.BACKEND_API_URL}/api/v1/plans?currency=${currency}`;
  try {
    const r = await fetch(url, {
      next: { tags: ["plans"], revalidate: 600 },
      signal: AbortSignal.timeout(8000),
    });
    if (!r.ok) {
      // Surface empty array → caller renders empty state / falls back to
      // the raw planId string for display.
      console.warn("[plans] fetch non-ok", { status: r.status, currency });
      return [];
    }
    const json = (await r.json().catch(() => ({ plans: [] }))) as {
      plans?: Plan[];
    };
    const plans = Array.isArray(json?.plans) ? json.plans : [];
    return plans;
  } catch (err) {
    console.warn("[plans] fetch threw", err);
    return [];
  }
}
