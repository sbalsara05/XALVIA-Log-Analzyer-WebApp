const DEFAULT_BAUD_RATE = 115200;
const READ_IDLE_TIMEOUT_MS = 3000;
const RESPONSE_TIMEOUT_MS = 30000;

class XalivaSerialService {
  private port: SerialPort | null = null;
  private reader: ReadableStreamDefaultReader<Uint8Array> | null = null;
  private writer: WritableStreamDefaultWriter<Uint8Array> | null = null;
  private pendingRead: Promise<ReadableStreamReadResult<Uint8Array>> | null = null;
  private connecting = false;
  private listing = false;

  async connect(): Promise<void> {
    if (this.connecting || this.port) {
      throw new Error("Serial device is already connected or connecting.");
    }
    if (typeof navigator === "undefined" || !("serial" in navigator)) {
      throw new Error("Web Serial is not supported in this browser.");
    }

    this.connecting = true;
    let opened = false;
    try {
      const port = await navigator.serial.requestPort();
      await port.open({ baudRate: DEFAULT_BAUD_RATE });
      opened = true;
      this.port = port;
      if (!port.readable || !port.writable) {
        throw new Error("Serial port is not readable or writable.");
      }
      this.reader = port.readable.getReader();
      this.writer = port.writable.getWriter();
      await this.write("\r\n");
    } catch (error) {
      if (opened) {
        await this.disconnect().catch(() => undefined);
      }
      throw error;
    } finally {
      this.connecting = false;
    }
  }

  async write(command: string): Promise<void> {
    if (!this.writer) {
      throw new Error("Serial device is not connected.");
    }
    await this.writer.write(new TextEncoder().encode(command));
  }

  async listLogFiles(): Promise<string[]> {
    const reader = this.reader;
    if (!reader) {
      throw new Error("Serial device is not connected.");
    }
    if (this.listing) {
      throw new Error("A serial file listing is already in progress.");
    }

    this.listing = true;
    const decoder = new TextDecoder();
    let buffer = "";
    const deadline = Date.now() + RESPONSE_TIMEOUT_MS;
    try {
      await this.write("file_list\r\n");
      // Like the desktop client, finish after three seconds without data.
      // A wake-up prompt may still be buffered, so it cannot end this command.
      while (this.reader === reader) {
        const remaining = deadline - Date.now();
        if (remaining <= 0) {
          throw new Error("Timed out reading the serial file list.");
        }
        // Keep a timed-out read for the next command; racing a new read would
        // leave the old read consuming (and losing) the next response's bytes.
        const pending = this.pendingRead ?? reader.read();
        this.pendingRead = pending;
        let timer: ReturnType<typeof setTimeout> | undefined;
        let result: ReadableStreamReadResult<Uint8Array> | null;
        try {
          result = await Promise.race([
            pending,
            new Promise<null>((resolve) => {
              timer = setTimeout(() => resolve(null), Math.min(READ_IDLE_TIMEOUT_MS, remaining));
            }),
          ]);
        } finally {
          clearTimeout(timer);
        }
        if (!result) break;
        this.pendingRead = null;
        if (result.done || this.reader !== reader) {
          throw new Error("Serial device disconnected while reading the file list.");
        }
        buffer += decoder.decode(result.value, { stream: true });
      }
      if (this.reader !== reader) {
        throw new Error("Serial device disconnected while reading the file list.");
      }
      buffer += decoder.decode();
      // Parse only the complete response, so a split filename cannot produce
      // a truncated entry and '.txt.bak' is never mistaken for a log file.
      return Array.from(new Set(buffer.split(/\s+/).filter((token) => token.endsWith(".txt"))));
    } finally {
      this.listing = false;
    }
  }

  async disconnect(): Promise<void> {
    const reader = this.reader;
    const writer = this.writer;
    const port = this.port;
    this.reader = null;
    this.writer = null;
    this.port = null;
    this.pendingRead = null;

    // Always attempt every cleanup step, even if canceling the reader fails.
    try {
      if (reader) {
        try {
          await reader.cancel();
        } finally {
          reader.releaseLock();
        }
      }
    } finally {
      try {
        writer?.releaseLock();
      } finally {
        await port?.close();
      }
    }
  }

  isConnected(): boolean {
    return this.port !== null && this.reader !== null && this.writer !== null;
  }
}

export const xalivaSerial = new XalivaSerialService();
