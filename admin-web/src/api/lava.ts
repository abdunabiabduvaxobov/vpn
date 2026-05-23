import { api } from "@/api/client";

// LavaProduct mirrors handler.lavaProductRow (plan 03-05 T03). One row per
// (product, offer, price) tuple — the admin selects ONE row from this list
// in the LavaOfferPicker dropdown (D-12 Option B: dropdown only, no paste).
//
// The backend proxies lava.top's /api/v2/products server-side; the admin
// API key NEVER reaches the browser. T-03-74 (info-disclosure) is mitigated
// at the proxy layer — see 03-05 SUMMARY § "Threat Model Compliance".
export interface LavaProduct {
  productId: string;
  productName: string;
  offerId: string;
  offerName: string;
  periodicity: string; // ONE_TIME | MONTHLY | PERIOD_90_DAYS | PERIOD_180_DAYS | PERIOD_YEAR
  currency: string; // USD | EUR | RUB
  amount: number;
}

export async function listLavaProducts(): Promise<LavaProduct[]> {
  const resp = await api.get<LavaProduct[]>("/api/v1/admin/lava/products");
  return resp.data;
}
