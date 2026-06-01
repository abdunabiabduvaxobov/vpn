import { api } from "@/api/client";

// Mirror of model.VPNServer's JSON-visible fields only. Several columns
// (capacity, reality_public_key, reality_short_id, WS/AWG params) are
// marshalled as json:"-" in Go so they never reach the panel, even on
// admin reads. The PATCH endpoint still accepts those fields though —
// see UpdateServerInput below.
export interface AdminServer {
  id: string;
  hostname: string;
  ip_address: string;
  region: string;
  city: string;
  country: string;
  country_code: string;
  protocol: string;
  // load_percent is what the GORM struct serialises for "current_load".
  load_percent: number;
  is_active: boolean;
  // is_draining (migration 024 / ADMIN-04): existing tunnels survive but no
  // NEW connections are handed out and the server drops from the public
  // /servers list. last_seen_at is the last tunnel heartbeat (null = never).
  is_draining: boolean;
  last_seen_at: string | null;
  created_at: string;
}

export async function listServers(): Promise<AdminServer[]> {
  const resp = await api.get<AdminServer[]>("/api/v1/admin/servers");
  return resp.data;
}

// Fields the backend PATCH handler will actually accept. Omit any
// field you don't want to change; empty strings are ignored server-
// side for the string fields (see handler/admin.go:346-367).
export interface UpdateServerInput {
  ip_address?: string;
  protocol?: string;
  capacity?: number;
  is_active?: boolean;
  reality_public_key?: string;
  reality_short_id?: string;
  current_load?: number;
}

export async function updateServer(
  id: string,
  input: UpdateServerInput,
): Promise<{ id: string; updated: Record<string, unknown> }> {
  const resp = await api.patch<{ id: string; updated: Record<string, unknown> }>(
    `/api/v1/admin/servers/${id}`,
    input,
  );
  return resp.data;
}

// Soft delete — backend sets is_active=false. The row is not physically
// removed, so a subsequent toggle back to active via updateServer works.
export async function deleteServer(id: string): Promise<void> {
  await api.delete(`/api/v1/admin/servers/${id}`);
}

// --- ADMIN-04 server controls --------------------------------------------
//
// drain sets is_draining=true (existing tunnels survive, no new connections,
// drops from the public /servers list). force=true ALSO force-disconnects
// every live connection on the server, subject to the per-server throttle
// (429 on a second call inside the 60s window). undrain reverses it.
// disconnect is the standalone force-disconnect-all (also throttled).

export async function drainServer(
  id: string,
  force = false,
): Promise<{ id: string; is_draining: boolean; killed_count: number }> {
  const resp = await api.post<{
    id: string;
    is_draining: boolean;
    killed_count: number;
  }>(`/api/v1/admin/servers/${id}/drain`, { force });
  return resp.data;
}

export async function undrainServer(
  id: string,
): Promise<{ id: string; is_draining: boolean }> {
  const resp = await api.post<{ id: string; is_draining: boolean }>(
    `/api/v1/admin/servers/${id}/undrain`,
    {},
  );
  return resp.data;
}

export async function disconnectServer(
  id: string,
): Promise<{ killed_count: number }> {
  const resp = await api.post<{ killed_count: number }>(
    `/api/v1/admin/servers/${id}/disconnect`,
    {},
  );
  return resp.data;
}

// Operational snapshot for one server (live conn count, last heartbeat, load).
export interface ServerHealth {
  concurrent_conns: number;
  last_seen_at: string | null;
  current_load: number;
}

export async function getServerHealth(id: string): Promise<ServerHealth> {
  const resp = await api.get<ServerHealth>(
    `/api/v1/admin/servers/${id}/health`,
  );
  return resp.data;
}
