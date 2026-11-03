import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { xalivaSerial } from "@/services/xalivaSerial";

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function bytes(text: string): Uint8Array {
  return encoder.encode(text);
}

class FakeSerialReader {
  private chunks: Uint8Array[];
  private index = 0;

  read = vi.fn((): Promise<ReadableStreamReadResult<Uint8Array>> => {
    if (this.index < this.chunks.length) {
      const value = this.chunks[this.index++];

      return Promise.resolve({
        done: false,
        value,
      });
    }

    // Simulate a serial read waiting for more device data.
    return new Promise(() => {});
  });

  cancel = vi.fn(async () => undefined);
  releaseLock = vi.fn();

  constructor(chunks: string[]) {
    this.chunks = chunks.map(bytes);
  }
}

class FakeSerialWriter {
  writes: string[] = [];

  write = vi.fn(async (data: Uint8Array) => {
    this.writes.push(decoder.decode(data));
  });

  releaseLock = vi.fn();
}

function createFakePort(chunks: string[]) {
  const reader = new FakeSerialReader(chunks);
  const writer = new FakeSerialWriter();

  const port = {
    readable: {
      getReader: () => reader,
    },

    writable: {
      getWriter: () => writer,
    },

    open: vi.fn(async () => undefined),
    close: vi.fn(async () => undefined),
  };

  Object.defineProperty(navigator, "serial", {
    configurable: true,
    value: {
      requestPort: vi.fn(async () => port),
    },
  });

  return {
    port,
    reader,
    writer,
  };
}

describe("XalivaSerialService", () => {
  beforeEach(() => {
    vi.useRealTimers();
  });

  afterEach(async () => {
    if (xalivaSerial.isConnected()) {
      await xalivaSerial.disconnect();
    }

    vi.restoreAllMocks();
  });

  it("connects at 115200 baud and wakes the XALIVA CLI", async () => {
    const { port, writer } = createFakePort(["\r\nXALIVA>"]);

    await xalivaSerial.connect();

    expect(port.open).toHaveBeenCalledWith({
      baudRate: 115200,
    });

    expect(writer.writes).toEqual(["\r\n"]);
    expect(xalivaSerial.isConnected()).toBe(true);
  });

  it("lists .txt log files and removes duplicates", async () => {
    vi.useFakeTimers();

    const { writer } = createFakePort([
      "XALIVA>",
      [
        "file_list\r\n",
        "LIST of DIR [/fatfs/log/]\r\n",
        "f 100 10302076_devicelog.txt\r\n",
        "f 200 payload.csv\r\n",
        "f 300 10302076_logtrack.txt\r\n",
        "f 100 10302076_devicelog.txt\r\n",
      ].join(""),
    ]);

    await xalivaSerial.connect();

    const filesPromise = xalivaSerial.listLogFiles();

    await vi.advanceTimersByTimeAsync(3000);

    const files = await filesPromise;

    expect(writer.writes).toContain("file_list\r\n");

    expect(files).toEqual([
      "10302076_devicelog.txt",
      "10302076_logtrack.txt",
    ]);
  });

  it("handles a filename split across serial chunks", async () => {
    vi.useFakeTimers();

    createFakePort([
      "XALIVA>",
      "file_list\r\nf 100 10302076_devi",
      "celog.txt\r\nf 200 another_log.txt\r\n",
    ]);

    await xalivaSerial.connect();

    const filesPromise = xalivaSerial.listLogFiles();

    await vi.advanceTimersByTimeAsync(3000);

    const files = await filesPromise;

    expect(files).toEqual([
      "10302076_devicelog.txt",
      "another_log.txt",
    ]);
  });

  it("reads a log when the start and end markers are split across chunks", async () => {
    const { writer } = createFakePort([
      "XALIVA>",
      "file_read 10302076_devicelog\r\nda",
      [
        "ta = 10302076_devicelog",
        "\r\n",
        "5a015c0a00000a00",
        "\r\n",
        "550300000000c00a",
        "\r\n",
        "XALI",
      ].join(""),
      "VA>",
    ]);

    await xalivaSerial.connect();

    const data = await xalivaSerial.readLogFile(
      "10302076_devicelog.txt"
    );

    const text = decoder.decode(data);

    expect(writer.writes).toContain(
      "file_read 10302076_devicelog \n"
    );

    expect(text).toContain("5a015c0a00000a00");
    expect(text).toContain("550300000000c00a");

    expect(text).not.toContain(
      "data = 10302076_devicelog"
    );

    expect(text).not.toContain("XALIVA>");
  });

  it("disconnects and releases the serial resources", async () => {
    const { port, reader, writer } = createFakePort(["XALIVA>"]);

    await xalivaSerial.connect();
    await xalivaSerial.disconnect();

    expect(reader.cancel).toHaveBeenCalled();
    expect(reader.releaseLock).toHaveBeenCalled();
    expect(writer.releaseLock).toHaveBeenCalled();
    expect(port.close).toHaveBeenCalled();

    expect(xalivaSerial.isConnected()).toBe(false);
  });

  it("throws when Web Serial is unavailable", async () => {
    Object.defineProperty(navigator, "serial", {
      configurable: true,
      value: undefined,
    });

    await expect(
      xalivaSerial.connect()
    ).rejects.toThrow(
      "Web Serial is not supported in this browser."
    );
  });
});