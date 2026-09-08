import { wasm } from "../wasm/client";
import {
  TunnelWasmClient,
  type TunnelWorkerEvent,
} from "../wasm/tunnel-client";
import { connectRelay, controlPort, relayAddress } from "./relay";
import type { TransferSettings } from "./types";
import { errorMessage, textDecoder } from "./bytes";

export const tunnelLimit = 64 * 1024 * 1024;
const chunkSize = 64 * 1024;
const metadataRecord = 1,
  dataRecord = 2,
  endRecord = 3,
  errorRecord = 4;
const textRecord = 5,
  binaryRecord = 6,
  closeRecord = 7;
type Operation = {
  record(kind: number, bytes: Uint8Array): Promise<void> | void;
  fail(error: Error): void;
};
type Active = { worker: TunnelWasmClient; handle: number };
export type TunnelCallbacks = {
  state(connected: boolean, message: string, port?: number): void;
  reload(): void;
};

export class TunnelSession {
  readonly done: Promise<void>;
  private controller = new AbortController();
  private active?: Active;
  private operations = new Map<number, Operation>();
  private nextOperation = 0;
  private everConnected = false;
  private port = 0;
  private disconnectedAt = 0;

  constructor(
    private code: string,
    private settings: TransferSettings,
    private callbacks: TunnelCallbacks,
  ) {
    this.done = this.run();
  }
  disconnect() {
    this.controller.abort();
    this.failOperations(new Error("Tunnel disconnected"));
  }
  private failOperations(error: Error) {
    const operations = [...this.operations.values()];
    this.operations.clear();
    for (const operation of operations) operation.fail(error);
  }
  private async run() {
    const joinDeadline = Date.now() + 40_000;
    try {
      for (;;) {
        try {
          if (await this.attempt()) return;
          throw new Error("Tunnel connection lost");
        } catch (error) {
          if (this.controller.signal.aborted) return;
          if (!this.everConnected) {
            if (
              !/rendezvous busy|room full/i.test(errorMessage(error)) ||
              Date.now() >= joinDeadline
            )
              throw error;
          }
          this.disconnectedAt ||= Date.now();
          if (Date.now() - this.disconnectedAt >= 120_000)
            throw new Error("Tunnel reconnect window expired");
          this.callbacks.state(false, "Connection lost; reconnecting…");
          await new Promise<void>((resolve) => {
            const finish = () => {
              clearTimeout(timer);
              this.controller.signal.removeEventListener("abort", finish);
              resolve();
            };
            const timer = setTimeout(
              finish,
              this.everConnected ? 5_000 : 500 + Math.random() * 1000,
            );
            this.controller.signal.addEventListener("abort", finish, {
              once: true,
            });
          });
          if (this.controller.signal.aborted) return;
        }
      }
    } finally {
      this.active = undefined;
      this.code = "";
      this.failOperations(new Error("Tunnel ended"));
      this.callbacks.state(false, "Tunnel ended");
    }
  }
  private async attempt() {
    const signal = this.controller.signal;
    const attempt = new AbortController();
    const stop = () => attempt.abort();
    signal.addEventListener("abort", stop, { once: true });
    const timer = setTimeout(stop, 40_000);
    let socket: Awaited<ReturnType<typeof connectRelay>>["socket"] | undefined;
    let worker: TunnelWasmClient | undefined;
    let handle: number | undefined;
    let failure = new Error("Tunnel connection lost");
    let finish!: (clean: boolean) => void;
    const closed = new Promise<boolean>((resolve) => {
      finish = resolve;
    });
    const abortClosed = () => finish(false);
    attempt.signal.addEventListener("abort", abortClosed);
    try {
      this.callbacks.state(false, "Authenticating tunnel invitation…");
      const components = await wasm().tunnelCodeComponents(this.code);
      const index = await wasm().relayIndex(
        this.code,
        this.settings.relayAddresses.length,
      );
      ({ socket } = await connectRelay(
        this.settings,
        components.room,
        controlPort(relayAddress(this.settings, index)),
        index,
        attempt.signal,
      ));
      socket.useRawStream();
      const feed = (current: NonNullable<typeof socket>) => {
        void (async () => {
          try {
            for (;;) {
              const bytes = await current.receiveRaw();
              if (socket !== current) return;
              for (let offset = 0; offset < bytes.length; offset += chunkSize)
                await worker!.feed(
                  handle!,
                  bytes.subarray(offset, offset + chunkSize),
                );
            }
          } catch {
            if (socket === current)
              await worker!.endFeed(handle!).catch(() => finish(false));
          }
        })();
      };
      worker = new TunnelWasmClient(async (event: TunnelWorkerEvent) => {
        if (event.type === "network") await socket!.sendRaw(event.data);
        else if (event.type === "handoff") {
          const previous = socket;
          socket = undefined;
          previous?.close();
          ({ socket } = await connectRelay(
            this.settings,
            textDecoder.decode(event.data),
            controlPort(relayAddress(this.settings, index)),
            index,
            attempt.signal,
          ));
          socket.useRawStream();
          feed(socket);
        } else if (event.type === "output") {
          const id = new DataView(
            event.data.buffer,
            event.data.byteOffset,
            event.data.byteLength,
          ).getUint32(0);
          await this.operations
            .get(id)
            ?.record(event.data[4], event.data.slice(5));
        } else if (event.type === "connected") {
          clearTimeout(timer);
          this.everConnected = true;
          this.disconnectedAt = 0;
          this.port = event.port;
          this.callbacks.state(
            true,
            `Connected to localhost:${event.port}`,
            event.port,
          );
        } else if (event.type === "closed") {
          failure = new Error(event.message || "Tunnel connection lost");
          finish(event.clean);
        }
      });
      handle = await worker.start(this.code);
      this.active = { worker, handle };
      feed(socket);
      const clean = await closed;
      if (!clean) throw failure;
      return true;
    } finally {
      clearTimeout(timer);
      signal.removeEventListener("abort", stop);
      attempt.signal.removeEventListener("abort", abortClosed);
      attempt.abort();
      socket?.close();
      if (worker) {
        if (handle !== undefined) await worker.stop(handle);
        worker.dispose();
      }
      this.active = undefined;
      this.failOperations(new Error("Tunnel connection closed"));
    }
  }
  private allocate(operation: Operation) {
    if (!this.active) throw new Error("Tunnel is not connected");
    if (this.operations.size >= 64)
      throw new Error("Too many simultaneous preview requests");
    const id = ++this.nextOperation;
    this.operations.set(id, operation);
    return { id, ...this.active };
  }
  private path(value: string) {
    const origin = `http://localhost:${this.port}`;
    const url = new URL(value, origin);
    if (url.origin !== origin || url.username || url.password)
      throw new Error("Browser previews support only the shared service");
    return url.pathname + url.search;
  }

  fetch(
    path: string,
    init: {
      method?: string;
      headers?: Record<string, string[]>;
      body?: Uint8Array;
      signal?: AbortSignal;
    } = {},
  ): Promise<Response> {
    return new Promise((resolve, reject) => {
      let controller: ReadableStreamDefaultController<Uint8Array> | undefined;
      let pull: (() => void) | undefined;
      let settled = false;
      let total = 0;
      let active: ReturnType<TunnelSession["allocate"]>;
      const finish = () => {
        this.operations.delete(active.id);
        init.signal?.removeEventListener("abort", abort);
        pull?.();
      };
      const fail = (error: Error) => {
        if (!settled) reject(error);
        else {
          try {
            controller?.error(error);
          } catch {}
        }
        finish();
      };
      const abort = () => {
        fail(new DOMException("Request cancelled", "AbortError"));
        void active.worker
          .closeChannel(active.handle, active.id)
          .catch(() => {});
      };
      try {
        const requestPath = this.path(path);
        if ((init.body?.length ?? 0) > tunnelLimit)
          throw new Error("Request body exceeds 64 MiB preview limit");
        active = this.allocate({
          fail,
          record: async (kind, bytes) => {
            if (kind === metadataRecord) {
              const metadata = JSON.parse(textDecoder.decode(bytes)) as {
                status: number;
                headers: Record<string, string[]>;
              };
              const headers = new Headers();
              for (const [key, values] of Object.entries(
                metadata.headers ?? {},
              ))
                for (const value of values) headers.append(key, value);
              const stream = new ReadableStream<Uint8Array>(
                {
                  start(value) {
                    controller = value;
                  },
                  pull() {
                    pull?.();
                    pull = undefined;
                  },
                  cancel() {
                    abort();
                  },
                },
                { highWaterMark: chunkSize, size: (chunk) => chunk.byteLength },
              );
              const empty =
                init.method === "HEAD" ||
                [204, 205, 304].includes(metadata.status);
              settled = true;
              resolve(
                new Response(empty ? null : stream, {
                  status: metadata.status,
                  headers,
                }),
              );
            } else if (kind === dataRecord) {
              total += bytes.length;
              if (total > tunnelLimit) {
                abort();
                return;
              }
              controller?.enqueue(bytes);
              if (
                controller &&
                controller.desiredSize !== null &&
                controller.desiredSize <= 0
              )
                await new Promise<void>((resolve) => {
                  pull = resolve;
                });
            } else if (kind === endRecord) {
              controller?.close();
              finish();
            } else if (kind === errorRecord)
              fail(new Error(textDecoder.decode(bytes)));
          },
        });
        init.signal?.addEventListener("abort", abort, { once: true });
        if (init.signal?.aborted) {
          abort();
          return;
        }
        void (async () => {
          await active.worker.open(
            active.handle,
            active.id,
            "croc-http-v1",
            JSON.stringify({
              path: requestPath,
              method: init.method ?? "GET",
              headers: init.headers,
            }),
          );
          const body = init.body ?? new Uint8Array();
          for (let offset = 0; offset < body.length; offset += chunkSize)
            await active.worker.write(
              active.handle,
              active.id,
              dataRecord,
              body.subarray(offset, offset + chunkSize),
            );
          await active.worker.write(
            active.handle,
            active.id,
            endRecord,
            new Uint8Array(),
          );
        })().catch((error) => fail(new Error(errorMessage(error))));
      } catch (error) {
        reject(error);
      }
    });
  }

  async websocket(
    path: string,
    protocols: string[],
    onMessage: (data: Uint8Array, binary: boolean) => Promise<void> | void,
    onClose: (code: number, reason: string) => void,
  ) {
    let ready!: (protocol: string) => void, failed!: (error: Error) => void;
    const opened = new Promise<string>((resolve, reject) => {
      ready = resolve;
      failed = reject;
    });
    // An open rejection can precede the worker RPC settling.
    void opened.catch(() => {});
    let active: ReturnType<TunnelSession["allocate"]>;
    let finished = false;
    const finish = (code: number, reason: string) => {
      if (finished) return;
      finished = true;
      this.operations.delete(active.id);
      failed(new Error(reason || "WebSocket closed"));
      onClose(code, reason);
    };
    const requestPath = this.path(path.replace(/^ws:/, "http:"));
    active = this.allocate({
      fail: (error) => finish(1006, error.message),
      record: async (kind, bytes) => {
        if (kind === metadataRecord)
          ready(
            (JSON.parse(textDecoder.decode(bytes)) as { protocol?: string })
              .protocol ?? "",
          );
        else if (kind === textRecord || kind === binaryRecord) {
          if (protocols.includes("vite-hmr") && kind === textRecord) {
            try {
              const message = JSON.parse(textDecoder.decode(bytes));
              if (["update", "full-reload"].includes(message.type)) {
                this.callbacks.reload();
                return;
              }
            } catch {}
          }
          await onMessage(bytes, kind === binaryRecord);
        } else if (kind === closeRecord) {
          const close = JSON.parse(textDecoder.decode(bytes));
          finish(close.code, close.reason);
        } else if (kind === errorRecord)
          finish(1006, textDecoder.decode(bytes));
      },
    });
    try {
      await active.worker.open(
        active.handle,
        active.id,
        "croc-websocket-v1",
        JSON.stringify({ path: requestPath, protocols }),
      );
      const protocol = await opened;
      return {
        protocol,
        send: (bytes: Uint8Array, binary: boolean) =>
          active.worker.write(
            active.handle,
            active.id,
            binary ? binaryRecord : textRecord,
            bytes,
          ),
        close: async () => {
          finish(1000, "");
          await active.worker
            .closeChannel(active.handle, active.id)
            .catch(() => {});
        },
      };
    } catch (error) {
      finish(1006, errorMessage(error));
      throw error;
    }
  }
}
