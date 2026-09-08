import { errorMessage } from "../protocol/bytes";

type Pending = {
  resolve(value: unknown): void;
  reject(reason: Error): void;
};

export type TunnelWorkerEvent =
  | {
      type: "network" | "output" | "handoff";
      handle: number;
      sequence: number;
      data: Uint8Array;
    }
  | { type: "connected"; handle: number; port: number }
  | {
      type: "closed";
      handle: number;
      message: string;
      clean: boolean;
    };

let compiledModule: Promise<WebAssembly.Module> | undefined;

function compileTunnelModule() {
  compiledModule ??= fetch(`${import.meta.env.BASE_URL}croc-tunnel.wasm`).then(
    async (response) => {
      if (!response.ok) {
        throw new Error(`Could not load croc-tunnel.wasm (${response.status})`);
      }
      const contentType = response.headers
        .get("content-type")
        ?.split(";", 1)[0];
      if (
        typeof WebAssembly.compileStreaming === "function" &&
        contentType === "application/wasm"
      ) {
        return WebAssembly.compileStreaming(response);
      }
      return WebAssembly.compile(await response.arrayBuffer());
    },
  );
  return compiledModule;
}

export class TunnelWasmClient {
  private worker: Worker;
  private nextID = 1;
  private pending = new Map<number, Pending>();
  private fatalError: Error | undefined;
  private handles = new Set<number>();

  constructor(
    private readonly onEvent: (
      event: TunnelWorkerEvent,
    ) => void | Promise<void>,
    worker?: Worker,
  ) {
    this.worker =
      worker ??
      new Worker(`${import.meta.env.BASE_URL}croc-tunnel-worker.js`, {
        name: "croc-tunnel",
      });
    this.worker.addEventListener("message", (event: MessageEvent) => {
      const message = event.data as {
        type: "response" | "fatal" | "tunnel-event";
        id?: number;
        result?: unknown;
        error?: string;
        event?: TunnelWorkerEvent["type"];
        handle?: number;
        sequence?: number;
        data?: Uint8Array;
        message?: string;
        clean?: boolean;
      };
      if (message.type === "fatal") {
        this.fail(new Error(message.error ?? "Tunnel WASM worker failed"));
        return;
      }
      if (message.type === "response") {
        const pending = this.pending.get(message.id ?? 0);
        if (!pending) return;
        this.pending.delete(message.id ?? 0);
        if (message.error) pending.reject(new Error(message.error));
        else pending.resolve(message.result);
        return;
      }
      if (message.type === "tunnel-event" && message.event && message.handle) {
        const workerEvent =
          message.event === "network" ||
          message.event === "output" ||
          message.event === "handoff"
            ? {
                type: message.event,
                handle: message.handle,
                sequence: message.sequence ?? 0,
                data: message.data ?? new Uint8Array(),
              }
            : message.event === "connected"
              ? {
                  type: "connected" as const,
                  handle: message.handle,
                  port: Number(message.message),
                }
              : {
                  type: "closed" as const,
                  handle: message.handle,
                  message: message.message ?? "",
                  clean: message.clean === true,
                };
        if (workerEvent.type === "closed")
          this.handles.delete(workerEvent.handle);
        void (async () => {
          let failure = "";
          try {
            await this.onEvent(workerEvent);
          } catch (error) {
            failure = errorMessage(error);
          }
          if (
            workerEvent.type === "network" ||
            workerEvent.type === "output" ||
            workerEvent.type === "handoff"
          ) {
            await this.call("ack", [
              workerEvent.handle,
              workerEvent.sequence,
              failure,
            ]).catch(() => {});
          }
          if (failure) this.fail(new Error(failure));
        })();
      }
    });
    this.worker.addEventListener("error", (event) => {
      this.fail(new Error(event.message || "Tunnel WASM worker crashed"));
    });
    void compileTunnelModule()
      .then((module) => {
        if (!this.fatalError) {
          this.worker.postMessage({ type: "initialize", module });
        }
      })
      .catch((error) => this.fail(error));
  }

  async start(code: string) {
    const handle = await this.call<number>("start", [code]);
    this.handles.add(handle);
    return handle;
  }
  feed(handle: number, bytes: Uint8Array) {
    const owned = bytes.slice();
    return this.call<void>("feed", [handle, owned], [owned.buffer]);
  }
  endFeed(handle: number) {
    return this.call<void>("endFeed", [handle]);
  }
  open(handle: number, operation: number, kind: string, metadata: string) {
    return this.call<void>("open", [handle, operation, kind, metadata]);
  }
  write(handle: number, operation: number, kind: number, bytes: Uint8Array) {
    const owned = bytes.slice();
    return this.call<void>(
      "write",
      [handle, operation, kind, owned],
      [owned.buffer],
    );
  }
  closeChannel(handle: number, operation: number) {
    return this.call<void>("closeChannel", [handle, operation]);
  }

  async stop(handle: number) {
    if (!this.fatalError) {
      await this.call<void>("close", [handle]).catch(() => {});
    }
  }

  dispose() {
    this.worker.terminate();
    this.fail(new Error("Tunnel WASM worker closed"));
  }

  async close(handle?: number) {
    if (handle !== undefined && !this.fatalError) {
      await this.call<void>("close", [handle]).catch(() => {});
    }
    this.dispose();
  }

  private call<T>(
    method: string,
    args: unknown[] = [],
    transfer: Transferable[] = [],
  ) {
    if (this.fatalError) return Promise.reject(this.fatalError);
    const id = this.nextID++;
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, {
        resolve: resolve as (value: unknown) => void,
        reject,
      });
      this.worker.postMessage({ id, method, args }, transfer);
    });
  }

  private fail(reason: unknown) {
    const error = reason instanceof Error ? reason : new Error(String(reason));
    if (this.fatalError) return;
    this.fatalError = error;
    for (const pending of this.pending.values()) pending.reject(error);
    this.pending.clear();
    for (const handle of this.handles) {
      void this.onEvent({
        type: "closed",
        handle,
        message: error.message,
        clean: false,
      });
    }
    this.handles.clear();
  }
}
