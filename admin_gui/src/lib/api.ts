import { auth } from "@/lib/firebase";

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "";

export class ApiError extends Error {
  status: number;
  body: unknown;

  constructor(message: string, status: number, body?: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

async function request<T>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const user = auth.currentUser;

  if (!user) {
    throw new ApiError("No authenticated Firebase user.", 401);
  }

  async function send(token: string): Promise<Response> {
    const headers = new Headers(options.headers);

    headers.set("Authorization", `Bearer ${token}`);

    if (
      options.body &&
      !(options.body instanceof FormData) &&
      !headers.has("Content-Type")
    ) {
      headers.set("Content-Type", "application/json");
    }

    return fetch(`${API_BASE_URL}${path}`, {
      ...options,
      headers,
    });
  }

  let token = await user.getIdToken(false);
  let response = await send(token);

  // Retry once with a fresh Firebase ID token.
  if (response.status === 401) {
    token = await user.getIdToken(true);
    response = await send(token);
  }

  const contentType = response.headers.get("content-type");

  const body = contentType?.includes("application/json")
    ? await response.json()
    : await response.text();

  if (!response.ok) {
    throw new ApiError(
      `API request failed with status ${response.status}`,
      response.status,
      body
    );
  }

  return body as T;
}

export const apiClient = {
  get<T>(path: string) {
    return request<T>(path);
  },

  post<T>(path: string, body?: unknown) {
    return request<T>(path, {
      method: "POST",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  },

  postRaw<T>(
    path: string,
    body: BodyInit,
    contentType = "application/octet-stream"
  ) {
    return request<T>(path, {
      method: "POST",
      body,
      headers: {
        "Content-Type": contentType,
      },
    });
  },

  put<T>(path: string, body?: unknown) {
    return request<T>(path, {
      method: "PUT",
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  },

  delete<T>(path: string) {
    return request<T>(path, {
      method: "DELETE",
    });
  },
};