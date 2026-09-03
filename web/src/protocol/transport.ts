import { FrameDecoder, encodeFrame } from "./framing";

type Reader = {
  resolve(value: Uint8Array): void;
  reject(reason: Error): void;
};

const MAX_RAW_BUFFER = 4 * 1024 * 1024;

type SocketMode = "framed" | "handoff" | "raw";

export function gatewayForPort(gateway: string, relayIndex: number, port: string) {
  const base = new URL(gateway || "/ws", window.location.href);
  if (base.protocol === "http:") base.protocol = "ws:";
  if (base.protocol === "https:") base.protocol = "wss:";
  if (base.protocol !== "ws:" && base.protocol !== "wss:") {
    throw new Error("Gateway URL must use ws:// or wss://");
  }
  if (!Number.isSafeInteger(relayIndex) || relayIndex < 0) {
    throw new Error("Relay index must be a non-negative integer");
  }
  base.searchParams.set("relay", String(relayIndex));
  base.searchParams.set("port", port);
  return base.toString();
}

export class CrocSocket {
  private socket: WebSocket;
  private decoder = new FrameDecoder();
  private messages: Uint8Array[] = [];
  private readers: Reader[] = [];
  private rawMessages: Uint8Array[] = [];
  private rawReaders: Reader[] = [];
  private rawBufferedBytes = 0;
  private failure?: Error;
  private mode: SocketMode = "framed";

  private constructor(socket: WebSocket, signal?: AbortSignal) {
    this.socket = socket;
    socket.binaryType = "arraybuffer";
    socket.addEventListener("message", (event) => {
      try {
        const chunk =
          event.data instanceof ArrayBuffer
            ? new Uint8Array(event.data)
            : new Uint8Array();
        if (this.mode === "raw") {
          this.deliverRaw(chunk);
          return;
        }
        if (this.mode === "handoff") {
          let pending: Uint8Array<ArrayBufferLike> = chunk;
          for (;;) {
            const { message, remainder } = this.decoder.pushOne(pending);
            if (!message) return;
            if (message.byteLength === 1 && message[0] === 1) {
              if (!remainder?.byteLength) return;
              pending = remainder;
              continue;
            }
            this.mode = "raw";
            this.deliver(message);
            if (remainder?.byteLength) this.deliverRaw(remainder);
            break;
          }
          return;
        }
        for (const message of this.decoder.push(chunk)) this.deliver(message);
      } catch (error) {
        this.fail(error instanceof Error ? error : new Error(String(error)));
        this.socket.close();
      }
    });
    socket.addEventListener("close", () => {
      this.fail(new Error("Relay connection closed"));
    });
    socket.addEventListener("error", () => {
      this.fail(new Error("Relay connection failed"));
    });
    signal?.addEventListener("abort", () => this.close(), { once: true });
  }

  static connect(
    gateway: string,
    relayIndex: number,
    port: string,
    signal?: AbortSignal,
  ) {
    return new Promise<CrocSocket>((resolve, reject) => {
      if (signal?.aborted) {
        reject(new DOMException("Transfer cancelled", "AbortError"));
        return;
      }
      const socket = new WebSocket(gatewayForPort(gateway, relayIndex, port));
      const crocSocket = new CrocSocket(socket, signal);
      const onOpen = () => {
        cleanup();
        resolve(crocSocket);
      };
      const onError = () => {
        cleanup();
        crocSocket.close();
        reject(new Error("Could not connect to the croc WebSocket gateway"));
      };
      const onAbort = () => {
        cleanup();
        crocSocket.close();
        reject(new DOMException("Transfer cancelled", "AbortError"));
      };
      const cleanup = () => {
        socket.removeEventListener("open", onOpen);
        socket.removeEventListener("error", onError);
        signal?.removeEventListener("abort", onAbort);
      };
      socket.addEventListener("open", onOpen, { once: true });
      socket.addEventListener("error", onError, { once: true });
      signal?.addEventListener("abort", onAbort, { once: true });
    });
  }

  async send(payload: Uint8Array) {
    if (this.mode === "raw") {
      throw new Error("Relay connection is in raw-stream mode");
    }
    await this.sendBytes(encodeFrame(payload));
  }

  async sendRaw(payload: Uint8Array) {
    if (this.mode !== "raw") {
      throw new Error("Relay connection is not in raw-stream mode");
    }
    await this.sendBytes(payload);
  }

  private async sendBytes(payload: Uint8Array) {
    if (this.socket.readyState !== WebSocket.OPEN) {
      throw this.failure ?? new Error("Relay connection is not open");
    }
    while (this.socket.bufferedAmount > 4 * 1024 * 1024) {
      await new Promise((resolve) => window.setTimeout(resolve, 10));
      if (this.failure) throw this.failure;
      if (this.socket.readyState !== WebSocket.OPEN) {
        throw new Error("Relay connection closed while sending");
      }
    }
    this.socket.send(new Uint8Array(payload).buffer);
  }

  async receive(skipPings = true): Promise<Uint8Array> {
    for (;;) {
      const message = await this.next();
      if (skipPings && message.byteLength === 1 && message[0] === 1) continue;
      return message;
    }
  }

  prepareRawHandoff() {
    if (this.mode !== "framed") {
      throw new Error("Relay raw-stream handoff was already started");
    }
    this.messages = this.messages.filter(
      (message) => !(message.byteLength === 1 && message[0] === 1),
    );
    if (this.messages.length > 0) {
      throw new Error("Relay has unread control messages before SSH handoff");
    }
    this.mode = "handoff";
  }

  receiveRaw(): Promise<Uint8Array> {
    const message = this.rawMessages.shift();
    if (message) {
      this.rawBufferedBytes -= message.byteLength;
      return Promise.resolve(message);
    }
    if (this.failure) return Promise.reject(this.failure);
    return new Promise<Uint8Array>((resolve, reject) => {
      this.rawReaders.push({ resolve, reject });
    });
  }

  close() {
    if (
      this.socket.readyState === WebSocket.OPEN ||
      this.socket.readyState === WebSocket.CONNECTING
    ) {
      this.socket.close();
    }
    this.fail(new Error("Relay connection closed"));
  }

  private next() {
    if (this.messages.length > 0) return Promise.resolve(this.messages.shift()!);
    if (this.failure) return Promise.reject(this.failure);
    return new Promise<Uint8Array>((resolve, reject) => {
      this.readers.push({ resolve, reject });
    });
  }

  private deliver(message: Uint8Array) {
    const reader = this.readers.shift();
    if (reader) reader.resolve(message);
    else this.messages.push(message);
  }

  private deliverRaw(message: Uint8Array) {
    const reader = this.rawReaders.shift();
    if (reader) {
      reader.resolve(message);
      return;
    }
    if (this.rawBufferedBytes + message.byteLength > MAX_RAW_BUFFER) {
      this.fail(new Error("SSH relay stream exceeded its browser buffer"));
      this.socket.close();
      return;
    }
    this.rawMessages.push(message);
    this.rawBufferedBytes += message.byteLength;
  }

  private fail(error: Error) {
    if (this.failure) return;
    this.failure = error;
    for (const reader of this.readers.splice(0)) reader.reject(error);
    for (const reader of this.rawReaders.splice(0)) reader.reject(error);
    this.rawMessages = [];
    this.rawBufferedBytes = 0;
  }
}
