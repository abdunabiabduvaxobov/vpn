import { api } from "@/api/client";

import type { AdminServer } from "@/api/servers";

// Response shapes mirror handler/plans_admin.go fiber.Map payloads (plan 03-08).
// Field names are JSON-encoded snake_case; numeric counts come back as JS
// numbers (Go int64 → fits in JS Number for any realistic plan / offer /
// user count we'd ever ship in a single tenant).

export interface AdminPlanSummary {
  id: string;
  code: string;
  name: string;
  description: string;
  max_devices: number;
  max_servers: number;
  speed_limit_mbps: number;
  is_active: boolean;
  is_system: boolean;
  sort_order: number;
  server_count: number;
  offer_count: number;
  active_user_count: number;
  created_at: string;
  updated_at: string;
}

// PlanOffer mirrors model.PlanOffer JSON tags. lava_offer_id is nullable —
// a placeholder offer (D-09) has lava_offer_id=null until admin populates it
// via /admin/plans/:id/offers/:offer_id (PATCH) or /replace (POST).
export interface PlanOffer {
  id: string;
  plan_id: string;
  periodicity: string; // ONE_TIME | MONTHLY | PERIOD_90_DAYS | PERIOD_180_DAYS | PERIOD_YEAR
  currency: string; // USD | EUR | RUB
  amount: number;
  lava_offer_id: string | null;
  is_active: boolean;
  created_at: string;
  updated_at: string;
}

export interface AdminPlanDetail extends AdminPlanSummary {
  servers: AdminServer[];
  offers: PlanOffer[];
}

export interface CreatePlanInput {
  code: string;
  name: string;
  description?: string;
  max_devices: number;
  max_servers: number;
  speed_limit_mbps: number;
  sort_order?: number;
  server_ids: string[];
  offers: Array<{
    periodicity: string;
    currency: string;
    amount: number;
    lava_offer_id?: string | null;
  }>;
}

export interface UpdatePlanInput {
  name?: string;
  description?: string;
  max_devices?: number;
  max_servers?: number;
  speed_limit_mbps?: number;
  sort_order?: number;
  is_active?: boolean;
}

export interface CreateOfferInput {
  periodicity: string;
  currency: string;
  amount: number;
  lava_offer_id?: string | null;
}

export interface UpdateOfferInput {
  amount?: number;
  lava_offer_id?: string | null;
  is_active?: boolean;
}

export interface ReplaceOfferInput {
  amount: number;
  lava_offer_id?: string | null;
}

export interface DeletePlanResult {
  id: string;
  deleted: boolean;
  affected_users: number;
}

const ROOT = "/api/v1/admin/plans";

// --- Plans ---

export async function listPlans(): Promise<AdminPlanSummary[]> {
  const resp = await api.get<AdminPlanSummary[]>(ROOT);
  return resp.data;
}

export async function getPlan(id: string): Promise<AdminPlanDetail> {
  const resp = await api.get<AdminPlanDetail>(`${ROOT}/${id}`);
  return resp.data;
}

export async function createPlan(
  input: CreatePlanInput,
): Promise<AdminPlanDetail> {
  const resp = await api.post<AdminPlanDetail>(ROOT, input);
  return resp.data;
}

export async function updatePlan(
  id: string,
  input: UpdatePlanInput,
): Promise<AdminPlanDetail> {
  const resp = await api.patch<AdminPlanDetail>(`${ROOT}/${id}`, input);
  return resp.data;
}

// deletePlan soft-deletes. Backend refuses on system plans (403) even with
// force=true (D-32 §4 — two-layer defence). Caller should pass force=true
// when active_user_count > 0 to skip the 409 confirmation guard.
export async function deletePlan(
  id: string,
  force?: boolean,
): Promise<DeletePlanResult> {
  const query = force ? "?force=true" : "";
  const resp = await api.delete<DeletePlanResult>(`${ROOT}/${id}${query}`);
  return resp.data;
}

// --- Plan servers ---

export async function replacePlanServers(
  planId: string,
  serverIds: string[],
): Promise<void> {
  await api.put(`${ROOT}/${planId}/servers`, { server_ids: serverIds });
}

export async function addPlanServer(
  planId: string,
  serverId: string,
): Promise<void> {
  await api.post(`${ROOT}/${planId}/servers/${serverId}`);
}

export async function removePlanServer(
  planId: string,
  serverId: string,
): Promise<void> {
  await api.delete(`${ROOT}/${planId}/servers/${serverId}`);
}

// --- Plan offers ---

export async function listOffers(planId: string): Promise<PlanOffer[]> {
  const resp = await api.get<PlanOffer[]>(`${ROOT}/${planId}/offers`);
  return resp.data;
}

export async function createOffer(
  planId: string,
  input: CreateOfferInput,
): Promise<PlanOffer> {
  const resp = await api.post<PlanOffer>(`${ROOT}/${planId}/offers`, input);
  return resp.data;
}

export async function updateOffer(
  planId: string,
  offerId: string,
  input: UpdateOfferInput,
): Promise<PlanOffer> {
  const resp = await api.patch<PlanOffer>(
    `${ROOT}/${planId}/offers/${offerId}`,
    input,
  );
  return resp.data;
}

export async function deleteOffer(
  planId: string,
  offerId: string,
): Promise<void> {
  await api.delete(`${ROOT}/${planId}/offers/${offerId}`);
}

// replaceOffer (PAY-15) versions a price: backend deactivates the old offer
// and inserts a new active one in one tx, inheriting periodicity + currency
// from the old (immutable per ADR §19.7.7).
export async function replaceOffer(
  planId: string,
  offerId: string,
  input: ReplaceOfferInput,
): Promise<PlanOffer> {
  const resp = await api.post<PlanOffer>(
    `${ROOT}/${planId}/offers/${offerId}/replace`,
    input,
  );
  return resp.data;
}
