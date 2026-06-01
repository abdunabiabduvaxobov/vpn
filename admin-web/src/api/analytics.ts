import { api } from "@/api/client";

// All field names match the Go JSON tags in repository/admin_repo.go.
// Keep this file in lockstep when backend shapes change.

export interface BytesBucket {
  date: string; // YYYY-MM-DD UTC
  bytes_up: number;
  bytes_down: number;
}

export interface PlatformCount {
  platform: string; // "android", "ios", "unknown"
  count: number;
}

export interface TierCount {
  tier: "free" | "pro";
  count: number;
}

export interface TopServer {
  server_id: string;
  hostname: string;
  city: string;
  country: string;
  country_code: string;
  connection_count: number;
}

export interface AnalyticsPayload {
  traffic: BytesBucket[] | null;
  platforms: PlatformCount[] | null;
  tiers: TierCount[] | null;
  top_servers: TopServer[] | null;
  // `errors` is present only when one or more sub-queries failed.
  // The UI renders a targeted error card per widget rather than
  // failing the whole dashboard.
  errors?: Partial<
    Record<"traffic" | "platforms" | "tiers" | "top_servers", string>
  >;
}

export async function getAnalytics(days = 30): Promise<AnalyticsPayload> {
  const resp = await api.get<AnalyticsPayload>("/api/v1/admin/analytics", {
    params: { days, top_servers_limit: 5 },
  });
  return resp.data;
}
