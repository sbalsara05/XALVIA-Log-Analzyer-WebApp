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
    const res = await apiClient.get("/api/orgs");
    return res.data;
  },

  create: async (payload: CreateOrgPayload): Promise<Org> => {
    const res = await apiClient.post("/api/orgs", payload);
    return res.data;
  },

  delete: async (orgId: number): Promise<void> => {
    await apiClient.delete(`/api/orgs/${orgId}`);
  },
};
