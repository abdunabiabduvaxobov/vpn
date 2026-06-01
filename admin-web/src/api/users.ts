import { api } from "@/api/client";

// Mirror of model.User from server/api/internal/model/user.go. Keep the
// field names in exact lockstep with the Go JSON tags — any drift here
// means the UI silently shows blanks.
export interface AdminUser {
  id: string;
  full_name: string;
  subscription_tier: "free" | "premium" | "ultimate";
  subscription_expires_at: string | null;
  role: "user" | "admin";
  // ADR-006 Telegram recovery binding. All four nullable — users
  // who haven't linked Telegram just have everything as null.
  // Profile fields are cached from Telegram at link time; pre-016
  // linked rows have telegram_user_id set but profile fields null.
  telegram_user_id: number | null;
  telegram_linked_at: string | null;
  telegram_username: string | null;
  telegram_first_name: string | null;
  created_at: string;
}

export interface Pagination {
  page: number;
  limit: number;
  total: number;
  total_pages: number;
}

// GET /admin/users response payload after the global axios interceptor
// unwraps one level of `data`.
export interface ListUsersResponse {
  users: AdminUser[];
  pagination: Pagination;
}

export interface ListUsersParams {
  page: number;
  limit: number;
  search?: string;
}

export async function listUsers(
  params: ListUsersParams,
): Promise<ListUsersResponse> {
  const resp = await api.get<ListUsersResponse>("/api/v1/admin/users", {
    params: {
      page: params.page,
      limit: params.limit,
      // Skip the search param entirely when empty so the backend query
      // planner hits the fast path (no LIKEs).
      ...(params.search ? { search: params.search } : {}),
    },
  });
  return resp.data;
}

export async function getUser(id: string): Promise<AdminUser> {
  const resp = await api.get<AdminUser>(`/api/v1/admin/users/${id}`);
  return resp.data;
}

// adminUpdateUserRequest shape from admin.go:106. Only send fields that
// the caller actually wants to change; zero-valued fields are left
// out rather than sent as empty strings/zeros.
export interface UpdateUserInput {
  subscription_tier?: "free" | "premium" | "ultimate";
  role?: "user" | "admin";
  // RFC3339 timestamp, empty string clears the expiration, undefined
  // leaves the field alone. Matches the backend's *string pointer.
  subscription_expires_at?: string;
  // Positive integer; adds this many days to the current expiration
  // (or to now if none is set). Caller is responsible for sanity caps.
  extend_days?: number;
}

export interface UpdateUserResponse {
  id: string;
  updated: Record<string, unknown>;
}

export async function updateUser(
  id: string,
  input: UpdateUserInput,
): Promise<UpdateUserResponse> {
  const resp = await api.patch<UpdateUserResponse>(
    `/api/v1/admin/users/${id}`,
    input,
  );
  return resp.data;
}

// --- ADMIN-02 per-user controls ------------------------------------------
//
// All three reason-carrying mutations (suspend/unsuspend/disconnect) require a
// non-empty reason (the backend 400s otherwise) which is written into the
// audit log. cancel-subscription takes {refund, reason}. The backend
// surfaces 409 (already cancelled) and 429 (force-disconnect throttle) which
// the UI renders as toasts.

export async function suspendUser(
  id: string,
  reason: string,
): Promise<{ id: string; suspended_at: string }> {
  const resp = await api.post<{ id: string; suspended_at: string }>(
    `/api/v1/admin/users/${id}/suspend`,
    { reason },
  );
  return resp.data;
}

export async function unsuspendUser(
  id: string,
  reason: string,
): Promise<{ id: string }> {
  const resp = await api.post<{ id: string }>(
    `/api/v1/admin/users/${id}/unsuspend`,
    { reason },
  );
  return resp.data;
}

export async function disconnectUser(
  id: string,
  reason: string,
): Promise<{ killed_count: number }> {
  const resp = await api.post<{ killed_count: number }>(
    `/api/v1/admin/users/${id}/disconnect`,
    { reason },
  );
  return resp.data;
}

export interface CancelSubscriptionInput {
  refund: boolean;
  reason: string;
}

export async function cancelSubscription(
  id: string,
  input: CancelSubscriptionInput,
): Promise<{
  subscription_id: string;
  cancelled_at: string;
  refund_status: string;
}> {
  const resp = await api.post<{
    subscription_id: string;
    cancelled_at: string;
    refund_status: string;
  }>(`/api/v1/admin/users/${id}/cancel-subscription`, input);
  return resp.data;
}

// Audit-log row (mirror of model.AuditLogEntry). details is an opaque map
// carrying the action's reason + action-specific fields.
export interface AuditLogEntry {
  id: string;
  admin_id: string;
  action: string;
  target_id: string | null;
  details: Record<string, unknown> | null;
  ip: string;
  created_at: string;
}

export interface UserAuditLogResponse {
  entries: AuditLogEntry[];
  pagination: Pagination;
}

// GET /admin/users/:id/audit-log?page=&limit= → { entries, pagination }.
export async function getUserAuditLog(
  id: string,
  page = 1,
): Promise<UserAuditLogResponse> {
  const resp = await api.get<UserAuditLogResponse>(
    `/api/v1/admin/users/${id}/audit-log`,
    { params: { page, limit: 50 } },
  );
  return resp.data;
}

// Read-only session view — the refresh-token hash is never serialized.
export interface UserSession {
  id: string;
  device_info: string;
  created_at: string;
  expires_at: string;
}

// GET /admin/users/:id/sessions → array of sessions.
export async function getUserSessions(id: string): Promise<UserSession[]> {
  const resp = await api.get<UserSession[]>(
    `/api/v1/admin/users/${id}/sessions`,
  );
  return resp.data;
}
