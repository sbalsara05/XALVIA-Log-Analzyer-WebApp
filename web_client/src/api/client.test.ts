import { describe, it, expect, vi, afterEach } from "vitest";
import { request, ApiError, setTokenGetter } from "./client";

afterEach(() => {
  vi.restoreAllMocks();
  setTokenGetter(async () => null);
});

describe("request", () => {
  it("adds the auth header when a token exists", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(JSON.stringify({ ok: true }), { status: 200 }));
    setTokenGetter(async () => "abc");

    await request("/ping");

    const headers = fetchMock.mock.calls[0][1]!.headers as Headers;
    expect(headers.get("Authorization")).toBe("Bearer abc");
  });

  it("throws ApiError when the gateway returns an error", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response("nope", { status: 500 }));
    await expect(request("/ping")).rejects.toBeInstanceOf(ApiError);
  });
});
