import { apiClient } from "@/lib/api";

export interface OrgUser {
  user_id: string;
  email: string;
  role: "admin" | "user";
  can_read: boolean;
  can_upload: boolean;
  granted_at: string;
  added_by: string;
}

export interface UpsertUserPayload {
  email: string;
  role: "admin" | "user";
  can_read: boolean;
  can_upload: boolean;
}

export interface UpsertUserResponse {
  ok: boolean;
  user_id: string;
  changed: boolean;
  invited?: boolean;
  email_warning?: string;
}

export const usersService = {
  listByOrg: async (
    orgId: number
  ): Promise<OrgUser[]> => {
    return apiClient.get<OrgUser[]>(`/api/orgs/${orgId}/users`);
  },

  upsert: async (
    orgId: number,
    payload: UpsertUserPayload
  ): Promise<UpsertUserResponse> => {
    return apiClient.post<UpsertUserResponse>(`/api/orgs/${orgId}/users`, payload);
  },

  revoke: async (
    orgId: number,
    userId: string
  ): Promise<void> => {
    await apiClient.delete(`/api/orgs/${orgId}/users/${userId}`);
  },
};