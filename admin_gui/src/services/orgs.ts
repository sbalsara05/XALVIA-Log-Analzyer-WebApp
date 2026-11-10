import { apiClient } from "@/lib/api";

export interface Org {
  id: number;
  name: string;
  parent_org_id?: number | null;
}

export interface CreateOrgPayload {
  name: string;
  parent_org_id: number;
}

export const orgsService = {
  list: async (): Promise<Org[]> => {
    return apiClient.get<Org[]>("/api/orgs");
  },

  create: async (
    payload: CreateOrgPayload
  ): Promise<Org> => {
    return apiClient.post<Org>(
      "/api/orgs",
      payload
    );
  },

  delete: async (
    orgId: number
  ): Promise<void> => {
    await apiClient.delete(
      `/api/orgs/${orgId}`
    );
  },
};