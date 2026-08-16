import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

describe("WASM preloading", () => {
  beforeEach(() => vi.resetModules());
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it("compiles the document preload and sends the module to one shared worker", async () => {
    const workers: FakeWorker[] = [];
    const module = {} as WebAssembly.Module;

    class FakeWorker {
      readonly messages: unknown[] = [];

      constructor(
        readonly url: string,
        readonly options: WorkerOptions,
      ) {
        workers.push(this);
      }

      addEventListener() {}
      postMessage(message: unknown) {
        this.messages.push(message);
      }
      terminate() {}
    }

    vi.stubGlobal("Worker", FakeWorker);
    const fetchMock = vi.fn().mockResolvedValue(
      new Response("", {
        headers: { "content-type": "application/wasm" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const compileStreaming = vi
      .spyOn(WebAssembly, "compileStreaming")
      .mockResolvedValue(module);
    const { preloadWasm, wasm } = await import("./client");

    preloadWasm();

    expect(workers).toHaveLength(1);
    expect(workers[0]).toMatchObject({
      url: "/croc-worker.js",
      options: { name: "croc-protocol" },
    });
    expect(wasm()).toBe(wasm());
    expect(workers).toHaveLength(1);
    await vi.waitFor(() => {
      expect(workers[0].messages).toEqual([
        { type: "initialize", module },
      ]);
    });
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock).toHaveBeenCalledWith("/croc.wasm");
    expect(compileStreaming).toHaveBeenCalledOnce();
  });
});
