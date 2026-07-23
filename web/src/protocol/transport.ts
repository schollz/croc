import { FrameDecoder, encodeFrame } from "./framing";

type Reader = {
  resolve(value: Uint8Array): void;
  reject(reason: Error): void;
};

function gatewayForPort(gateway: string, port: string) {
  const base = new URL(gateway || "/ws", window.location.href);
  if (base.protocol === "http:") base.protocol = "ws:";
  if (base.protocol === "https:") base.protocol = "wss:";
  if (base.protocol !== "ws:" && base.protocol !== "wss:") {
    throw new Error("Gateway URL must use ws:// or wss://");
  }
  base.searchParams.set("port", port);
  return base.toString();
}

export class CrocSocket {
  private socket: WebSocket;
  private decoder = new FrameDecoder();
  private messages: Uint8Array[] = [];
  private readers: Reader[] = [];
  private failure?: Error;

  private constructor(socket: WebSocket, signal?: AbortSignal) {
    this.socket = socket;
    socket.binaryType = "arraybuffer";
    socket.addEventListener("message", (event) => {
      try {
        const chunk =
          event.data instanceof ArrayBuffer
            ? new Uint8Array(event.data)
            : new Uint8Array();
        for (const message of this.decoder.push(chunk)) this.deliver(message);
      } catch (error) {
        this.fail(error instanceof Error ? error : new Error(String(error)));
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

  static connect(gateway: string, port: string, signal?: AbortSignal) {
    return new Promise<CrocSocket>((resolve, reject) => {
      if (signal?.aborted) {
        reject(new DOMException("Transfer cancelled", "AbortError"));
        return;
      }
      const socket = new WebSocket(gatewayForPort(gateway, port));
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
    this.socket.send(encodeFrame(payload));
  }

  async receive(skipPings = true): Promise<Uint8Array> {
    for (;;) {
      const message = await this.next();
      if (skipPings && message.byteLength === 1 && message[0] === 1) continue;
      return message;
    }
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

  private fail(error: Error) {
    if (this.failure) return;
    this.failure = error;
    for (const reader of this.readers.splice(0)) reader.reject(error);
  }
}
