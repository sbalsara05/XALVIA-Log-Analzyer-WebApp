import { apiClient } from "@/lib/api";

export interface Device {
  id: string;
  org_id: number;
  added_at: string;
  added_by: string;
}

export interface AddDeviceResponse {
  ok: boolean;
  id: string;
  org_id: number;
}

export const devicesService = {
  listByOrg: async (orgId: number): Promise<Device[]> => {
    const res = await apiClient.get(`/api/orgs/${orgId}/devices`);
    return res.data;
  },

  add: async (orgId: number, macId: string): Promise<AddDeviceResponse> => {
    const res = await apiClient.post(`/api/orgs/${orgId}/devices`, {
      mac_id: macId,
    });
    return res.data;
  },

  remove: async (orgId: number, macId: string): Promise<void> => {
    await apiClient.delete(`/api/orgs/${orgId}/devices/${macId}`);
  },
};
