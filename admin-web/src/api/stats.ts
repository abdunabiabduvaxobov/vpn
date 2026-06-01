import { api } from "@/api/client";

// Shape returned by GET /api/v1/admin/stats. Mirrors
// repository.GetGlobalStats in admin_repo.go — keep this in lockstep with
// the backend map keys. Fields are int64 on the backend which JSON-encodes
// as plain numbers (safe for values well below 2^53).
export interface AdminStats {
  total_users: number;
  active_subscriptions: number;
  server_count: number;
  active_server_count: number;
  // ADMIN-01 KPI bar fields. Added by GetDashboardKPIs in admin_repo.go
  // (the four legacy keys above are merged in from GetGlobalStats). All
  // are int64 on the backend EXCEPT mrr, which is a float64 currency
  // figure (e.g. 10.0) resolved per-currency (default USD) from the
  // 5-min Redis cache. Keep field names in lockstep with the backend
  // map keys — any drift silently shows blanks.
  paid_users: number;
  mrr: number;
  active_connections: number;
  signups_today: number;
  signups_week: number;
  signups_month: number;
  churn_30d: number;
  failed_payments_30d: number;
}

export async function getAdminStats(): Promise<AdminStats> {
  const resp = await api.get<AdminStats>("/api/v1/admin/stats");
  return resp.data;
}
