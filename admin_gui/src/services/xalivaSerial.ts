const DEFAULT_BAUD_RATE = 115200;
const READ_IDLE_TIMEOUT_MS = 3000;
const RESPONSE_TIMEOUT_MS = 30000;
const PROMPT_TIMEOUT_MS = 5000;

class XalivaSerialService {
  private port: SerialPort | null = null;
  private reader: ReadableStreamDefaultReader<Uint8Array> | null = null;
  private writer: WritableStreamDefaultWriter<Uint8Array> | null = null;

  private decoder = new TextDecoder();
  private encoder = new TextEncoder();

  private pendingRead: Promise<ReadableStreamReadResult<Uint8Array>> | null =
    null;

  private connecting = false;
  private listing = false;
  private readingFile = false;

  async connect(): Promise<void> {
    if (this.connecting || this.port) {
      throw new Error("Serial device is already connected or connecting.");
    }

    if (
      typeof navigator === "undefined" ||
      !("serial" in navigator) ||
      !navigator.serial
    ) {
      throw new Error("Web Serial is not supported in this browser.");
    }

    this.connecting = true;
    let opened = false;

    try {
      const port = await navigator.serial.requestPort();

      await port.open({
        baudRate: DEFAULT_BAUD_RATE,
      });

      opened = true;
      this.port = port;

      if (!port.readable || !port.writable) {
        throw new Error("Serial port is not readable or writable.");
      }

      this.reader = port.readable.getReader();
      this.writer = port.writable.getWriter();

      // Same idea as the legacy desktop client's _pre_command():
      // wake the XALIVA CLI.
      await this.write("\r\n");

      // Consume the wake-up response so the next command starts
      // from a clean serial state.
      await this.drainUntilPrompt();
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

    const bytes = this.encoder.encode(command);
    await this.writer.write(bytes);
  }

  async listLogFiles(): Promise<string[]> {
    const reader = this.reader;

    if (!reader) {
      throw new Error("Serial device is not connected.");
    }

    if (this.listing) {
      throw new Error("A serial file listing is already in progress.");
    }

    if (this.readingFile) {
      throw new Error("A serial file read is already in progress.");
    }

    this.listing = true;

    const decoder = new TextDecoder();
    let buffer = "";
    const deadline = Date.now() + RESPONSE_TIMEOUT_MS;

    try {
      await this.write("file_list\r\n");

      // Match the legacy desktop behavior:
      // finish after a period of no incoming data.
      while (this.reader === reader) {
        const remaining = deadline - Date.now();

        if (remaining <= 0) {
          throw new Error("Timed out reading the serial file list.");
        }

        const pending = this.pendingRead ?? reader.read();
        this.pendingRead = pending;

        let timer: ReturnType<typeof setTimeout> | undefined;
        let result: ReadableStreamReadResult<Uint8Array> | null;

        try {
          result = await Promise.race([
            pending,
            new Promise<null>((resolve) => {
              timer = setTimeout(
                () => resolve(null),
                Math.min(READ_IDLE_TIMEOUT_MS, remaining)
              );
            }),
          ]);
        } finally {
          if (timer) {
            clearTimeout(timer);
          }
        }

        // Idle timeout: keep the unresolved read so the next command
        // can reuse it safely.
        if (!result) {
          break;
        }

        if (this.pendingRead === pending) {
          this.pendingRead = null;
        }

        if (result.done || this.reader !== reader) {
          throw new Error(
            "Serial device disconnected while reading the file list."
          );
        }

        if (result.value) {
          buffer += decoder.decode(result.value, { stream: true });
        }
      }

      if (this.reader !== reader) {
        throw new Error(
          "Serial device disconnected while reading the file list."
        );
      }

      buffer += decoder.decode();

      const files = buffer
        .split(/\s+/)
        .filter((token) => token.endsWith(".txt"));

      return Array.from(new Set(files));
    } finally {
      this.listing = false;
    }
  }

  async readLogFile(filename: string): Promise<Uint8Array> {
    const reader = this.reader;

    if (!reader) {
      throw new Error("Serial device is not connected.");
    }

    if (this.readingFile) {
      throw new Error("A serial file read is already in progress.");
    }

    if (this.listing) {
      throw new Error("A serial file listing is still in progress.");
    }

    this.readingFile = true;

    const decoder = new TextDecoder();
    const baseName = filename.endsWith(".txt")
      ? filename.slice(0, -4)
      : filename;

    const startMarker = `data = ${baseName}`;
    const endMarker = "XALIVA>";
    const deadline = Date.now() + RESPONSE_TIMEOUT_MS;

    let responseBuffer = "";
    let logText = "";
    let reading = false;

    try {
      await this.write(`file_read ${baseName} \n`);

      while (this.reader === reader) {
        const remaining = deadline - Date.now();

        if (remaining <= 0) {
          throw new Error(`Timed out reading ${filename}.`);
        }

        const result = await this.readNextChunk(reader, remaining);

        if (result.done || this.reader !== reader) {
          throw new Error(
            "Serial device disconnected while reading the log file."
          );
        }

        if (!result.value) {
          continue;
        }

        responseBuffer += decoder.decode(result.value, { stream: true });

        if (!reading) {
          const startIndex = responseBuffer.indexOf(startMarker);

          if (startIndex === -1) {
            // Keep enough trailing characters to detect a marker
            // that is split across two serial chunks.
            const keepLength = Math.max(startMarker.length - 1, 0);

            if (responseBuffer.length > keepLength) {
              responseBuffer = responseBuffer.slice(-keepLength);
            }

            continue;
          }

          reading = true;

          const contentStart = startIndex + startMarker.length;
          responseBuffer = responseBuffer.slice(contentStart);
        }

        const endIndex = responseBuffer.indexOf(endMarker);

        if (endIndex !== -1) {
          logText += responseBuffer.slice(0, endIndex);
          break;
        }

        // Keep a short tail so "XALIVA>" can still be detected
        // if it is split across two serial chunks.
        const keepLength = endMarker.length - 1;

        if (responseBuffer.length > keepLength) {
          const safeLength = responseBuffer.length - keepLength;

          logText += responseBuffer.slice(0, safeLength);
          responseBuffer = responseBuffer.slice(safeLength);
        }
      }

      if (!reading) {
        throw new Error(
          `The device did not return the expected start marker for ${filename}.`
        );
      }

      return this.encoder.encode(logText);
    } finally {
      this.readingFile = false;
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

    this.listing = false;
    this.readingFile = false;

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
    return (
      this.port !== null &&
      this.reader !== null &&
      this.writer !== null
    );
  }

  private async readNextChunk(
    reader: ReadableStreamDefaultReader<Uint8Array>,
    timeoutMs: number
  ): Promise<ReadableStreamReadResult<Uint8Array>> {
    const pending = this.pendingRead ?? reader.read();
    this.pendingRead = pending;

    let timer: ReturnType<typeof setTimeout> | undefined;

    try {
      const result = await Promise.race([
        pending,
        new Promise<null>((resolve) => {
          timer = setTimeout(() => resolve(null), timeoutMs);
        }),
      ]);

      if (!result) {
        throw new Error("Timed out waiting for serial data.");
      }

      if (this.pendingRead === pending) {
        this.pendingRead = null;
      }

      return result;
    } finally {
      if (timer) {
        clearTimeout(timer);
      }
    }
  }

  private async drainUntilPrompt(): Promise<void> {
    const reader = this.reader;

    if (!reader) {
      throw new Error("Serial device is not connected.");
    }

    const decoder = new TextDecoder();
    let buffer = "";
    const deadline = Date.now() + PROMPT_TIMEOUT_MS;

    while (this.reader === reader) {
      const remaining = deadline - Date.now();

      if (remaining <= 0) {
        throw new Error("Timed out waiting for the XALIVA CLI prompt.");
      }

      const result = await this.readNextChunk(reader, remaining);

      if (result.done || this.reader !== reader) {
        throw new Error(
          "Serial device disconnected while waiting for the CLI prompt."
        );
      }

      if (!result.value) {
        continue;
      }

      buffer += decoder.decode(result.value, { stream: true });

      if (buffer.includes("XALIVA>")) {
        return;
      }

      // No need to let the wake-up response grow forever.
      if (buffer.length > 2048) {
        buffer = buffer.slice(-2048);
      }
    }

    throw new Error(
      "Serial connection ended before the XALIVA CLI prompt was received."
    );
  }
}

export const xalivaSerial = new XalivaSerialService();