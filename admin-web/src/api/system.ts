import { api } from "@/api/client";

// One file per resource (mirrors api/stats.ts). All routes are under the
// admin bearer-token group /api/v1/admin/...; the shared axios response
// interceptor unwraps one level of `{ data: ... }` so callers read
// response.data directly. Field names are in lockstep with the Go JSON
// tags on FeatureFlag / BroadcastMessage and the AdminDepsHealth /
// AdminListBroadcasts handler maps.

// --- Deps-health (ADMIN-08) ----------------------------------------------

// One row of the per-tunnel-server health table from AdminDepsHealth.
// Mirrors the fiber.Map built in admin_system.go.
export interface TunnelServerHealth {
  id: string;
  hostname: string;
  is_active: boolean;
  current_load: number;
  // last_seen_at is the last tunnel heartbeat (RFC3339) or null for a
  // server that has never posted one.
  last_seen_at: string | null;
  // fresh = last_seen_at within the backend's tunnelFreshWindow.
  fresh: boolean;
}

// GET /admin/system/deps-health response. postgres/redis are "ok"/"down";
// lava is "ok"/"down"/"unknown" (cache miss before the first verdict).
export interface DepsHealth {
  postgres: string;
  redis: string;
  lava: string;
  tunnel_servers: TunnelServerHealth[];
}

export async function getDepsHealth(): Promise<DepsHealth> {
  const resp = await api.get<DepsHealth>("/api/v1/admin/system/deps-health");
  return resp.data;
}

// --- Feature flags (ADMIN-05) --------------------------------------------

// Mirror of model.FeatureFlag.
export interface FeatureFlag {
  key: string;
  value: boolean;
  updated_at: string;
  updated_by: string | null;
}

// GET /admin/feature-flags → { flags: [...] }.
export async function listFeatureFlags(): Promise<FeatureFlag[]> {
  const resp = await api.get<{ flags: FeatureFlag[] }>(
    "/api/v1/admin/feature-flags",
  );
  return resp.data.flags;
}

// PUT /admin/feature-flags/:key with { value, reason }. The reason is
// recorded in the audit row; it is optional server-side but the UI always
// sends one for non-repudiation.
export async function setFeatureFlag(
  key: string,
  value: boolean,
  reason: string,
): Promise<{ key: string; value: boolean }> {
  const resp = await api.put<{ key: string; value: boolean }>(
    `/api/v1/admin/feature-flags/${key}`,
    { value, reason },
  );
  return resp.data;
}

// --- Broadcasts (ADMIN-05) -----------------------------------------------

export type BroadcastSeverity = "info" | "warning" | "critical";

// Mirror of model.BroadcastMessage.
export interface Broadcast {
  id: string;
  title: string;
  body: string;
  severity: BroadcastSeverity;
  locale: string | null;
  target_tier: string | null;
  starts_at: string | null;
  ends_at: string | null;
  created_at: string;
}

// Create/update body. starts_at/ends_at are RFC3339 strings (the backend
// parses them into *time.Time). Omit a field to leave it unchanged on
// PATCH; on create, title/body/severity are required.
export interface BroadcastInput {
  title?: string;
  body?: string;
  severity?: BroadcastSeverity;
  locale?: string | null;
  target_tier?: string | null;
  starts_at?: string | null;
  ends_at?: string | null;
}

// GET /admin/broadcasts → { broadcasts: [...] }.
export async function listBroadcasts(): Promise<Broadcast[]> {
  const resp = await api.get<{ broadcasts: Broadcast[] }>(
    "/api/v1/admin/broadcasts",
  );
  return resp.data.broadcasts;
}

// POST /admin/broadcasts → the created row (201).
export async function createBroadcast(
  input: BroadcastInput,
): Promise<Broadcast> {
  const resp = await api.post<Broadcast>("/api/v1/admin/broadcasts", input);
  return resp.data;
}

// PATCH /admin/broadcasts/:id → { id }.
export async function updateBroadcast(
  id: string,
  input: BroadcastInput,
): Promise<{ id: string }> {
  const resp = await api.patch<{ id: string }>(
    `/api/v1/admin/broadcasts/${id}`,
    input,
  );
  return resp.data;
}

// DELETE /admin/broadcasts/:id → { id }.
export async function deleteBroadcast(id: string): Promise<{ id: string }> {
  const resp = await api.delete<{ id: string }>(
    `/api/v1/admin/broadcasts/${id}`,
  );
  return resp.data;
}
