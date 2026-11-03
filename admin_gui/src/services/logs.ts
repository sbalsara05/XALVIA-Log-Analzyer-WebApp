import { apiClient } from "@/lib/api";

export interface ParsedDeviceLogEntry {
  key: string;
  val: string;
}

export const logsService = {
  parseDeviceLog(
    macId: string,
    data: Uint8Array
  ): Promise<ParsedDeviceLogEntry[]> {
    const body = new Uint8Array(data);

    return apiClient.postRaw<ParsedDeviceLogEntry[]>(
      `/api/parse/device-log?mac_id=${encodeURIComponent(macId)}`,
      body.buffer
    );
  },
};