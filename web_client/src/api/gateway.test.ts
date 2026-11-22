import { describe, it, expect, vi, afterEach } from "vitest";
import { listFiles } from "./gateway";

afterEach(() => vi.restoreAllMocks());

describe("gateway", () => {
  it("listFiles sends the device's mac_id", async () => {
    const fetchMock = vi
      .spyOn(globalThis, "fetch")
      .mockResolvedValue(new Response(JSON.stringify(["a.log"]), { status: 200 }));

    const files = await listFiles("AA:BB:CC");

    expect(String(fetchMock.mock.calls[0][0])).toContain("/api/files?mac_id=AA%3ABB%3ACC");
    expect(files).toEqual(["a.log"]);
  });
});
