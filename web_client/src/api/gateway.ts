import { request } from "./client";

const qs = (params: Record<string, string>) => new URLSearchParams(params).toString();

// ── No device needed ─────────────────────────────────────────────
export const getHealth = () => request<unknown>("/health");
export const getProfile = () => request<unknown>("/api/profile");
export const getMyPermissions = () => request<unknown>("/api/me/permissions");

// ── Files (Cloud tab) ────────────────────────────────────────────
export const listFiles = (macId: string) =>
  request<string[]>(`/api/files?${qs({ mac_id: macId })}`);

export async function getReadUrl(macId: string, filename: string): Promise<string> {
  const body = await request<{ url: string }>(
    `/api/signed-url/read?${qs({ mac_id: macId, filename })}`
  );
  return body.url;
}

export async function readFile(macId: string, filename: string): Promise<Blob> {
  const url = await getReadUrl(macId, filename);
  const res = await fetch(url); // straight to GCS, no auth header
  if (!res.ok) throw new Error(`Download failed (${res.status})`);
  return res.blob();
}

// ── 3-step upload (PC / USB tabs) ────────────────────────────────
export interface UploadSession {
  session_id: string;
  urls: Record<string, string>;
  required_headers?: Record<string, string>;
}

export const getUploadUrls = (macId: string, filenames: string[]) =>
  request<UploadSession>("/api/signed-url/upload", {
    method: "POST",
    body: JSON.stringify({ mac_id: macId, files: filenames }),
  });

export async function putToSignedUrl(
  signedUrl: string,
  data: Blob,
  requiredHeaders: Record<string, string> = {}
): Promise<void> {
  const res = await fetch(signedUrl, {
    method: "PUT",
    body: data,
    headers: { "Content-Type": "application/octet-stream", ...requiredHeaders },
  });
  if (!res.ok) throw new Error(`Upload failed (${res.status})`);
}

export async function commitSession(
  macId: string,
  sessionId: string,
  files: string[]
): Promise<string> {
  const body = await request<{ committed: string }>(
    `/api/commit?${qs({ mac_id: macId, session_id: sessionId })}`,
    { method: "POST", body: JSON.stringify(files) }
  );
  return body.committed;
}

// Runs all 3 steps: get URLs → upload each file → commit
export async function uploadFiles(macId: string, files: File[]): Promise<string> {
  const names = files.map((f) => f.name);
  const { session_id, urls, required_headers } = await getUploadUrls(macId, names);
  for (const file of files) {
    await putToSignedUrl(urls[file.name], file, required_headers);
  }
  return commitSession(macId, session_id, names);
}

// ── Parsing ──────────────────────────────────────────────────────
export interface DeviceLogEntry {
  timestamp: string; // confirm the real type with backend
  description: string;
}

export const parseDeviceLog = (macId: string, data: Blob) =>
  request<DeviceLogEntry[]>(`/api/parse/device-log?${qs({ mac_id: macId })}`, {
    method: "POST",
    body: data,
    headers: { "Content-Type": "application/octet-stream" },
  });

export const parseTestLog = (macId: string, data: Blob) =>
  request<{ success?: boolean; [key: string]: unknown }>(
    `/api/parse/test-log?${qs({ mac_id: macId })}`,
    {
      method: "POST",
      body: data,
      headers: { "Content-Type": "application/octet-stream" },
    }
  );
