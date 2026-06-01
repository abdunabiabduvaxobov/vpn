import { api } from "@/api/client";

// One file per resource (mirrors api/stats.ts). The webhook log routes are
// under the admin bearer-token group; the shared axios response interceptor
// unwraps one level of `{ data: ... }`. Field names match the
// webhookEventListItem / detail fiber.Map in admin_webhooks.go exactly.

// A row in the webhook-log LIST view. payload_preview is a redacted string
// (buyer emails masked server-side, T-07-34) — NOT a JSON object. The full
// (unredacted) payload is only available via getWebhookEvent.
export interface WebhookEventListItem {
  id: string;
  event_type: string;
  contract_id: string | null;
  status: string;
  retried_count: number;
  received_at: string;
  processed_at: string | null;
  error: string | null;
  payload_preview: string;
}

export interface WebhookPagination {
  page: number;
  limit: number;
  total: number;
  total_pages: number;
}

// GET /admin/webhook-events → { events, pagination }.
export interface ListWebhookEventsResponse {
  events: WebhookEventListItem[];
  pagination: WebhookPagination;
}

export interface WebhookEventFilters {
  status?: string;
  event_type?: string;
  page?: number;
  limit?: number;
}

export async function listWebhookEvents(
  filters: WebhookEventFilters = {},
): Promise<ListWebhookEventsResponse> {
  const resp = await api.get<ListWebhookEventsResponse>(
    "/api/v1/admin/webhook-events",
    {
      params: {
        ...(filters.page ? { page: filters.page } : {}),
        ...(filters.limit ? { limit: filters.limit } : {}),
        // Skip empty filters so the backend hits its unfiltered fast path.
        ...(filters.status ? { status: filters.status } : {}),
        ...(filters.event_type ? { event_type: filters.event_type } : {}),
      },
    },
  );
  return resp.data;
}

// The DETAIL view — payload is the FULL parsed JSON object (unredacted; the
// GET itself is audited). invoice_id is present on the detail shape only.
export interface WebhookEventDetail {
  id: string;
  event_type: string;
  contract_id: string | null;
  invoice_id: string | null;
  status: string;
  retried_count: number;
  received_at: string;
  processed_at: string | null;
  error: string | null;
  // Raw JSONB payload — shape varies by event_type, so it's an opaque object.
  payload: unknown;
}

// GET /admin/webhook-events/:id.
export async function getWebhookEvent(
  id: string,
): Promise<WebhookEventDetail> {
  const resp = await api.get<WebhookEventDetail>(
    `/api/v1/admin/webhook-events/${id}`,
  );
  return resp.data;
}

// POST /admin/webhook-events/:id/replay with { reason }. Only DELIVERED or
// FAILED events are replayable; the side effect re-applies idempotently
// (no double grant). Returns { outcome: "REPLAYED", retried_count }.
export async function replayWebhookEvent(
  id: string,
  reason: string,
): Promise<{ outcome: string; retried_count: number }> {
  const resp = await api.post<{ outcome: string; retried_count: number }>(
    `/api/v1/admin/webhook-events/${id}/replay`,
    { reason },
  );
  return resp.data;
}
