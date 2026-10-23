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
  listByOrg: async (
    orgId: number
  ): Promise<Device[]> => {
    return apiClient.get<Device[]>(`/api/orgs/${orgId}/devices`);
  },

  add: async (
    orgId: number,
    macId: string
  ): Promise<AddDeviceResponse> => {
    return apiClient.post<AddDeviceResponse>(`/api/orgs/${orgId}/devices`, {mac_id: macId, });
  },

  remove: async (
    orgId: number,
    macId: string
  ): Promise<void> => {
    await apiClient.delete(`/api/orgs/${orgId}/devices/${macId}`);
  },
};