import { afterEach, describe, expect, it, vi } from "vitest";
import { preloadWasm, wasm } from "./client";

describe("WASM preloading", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("starts one shared protocol worker before the first method call", () => {
    const workers: FakeWorker[] = [];

    class FakeWorker {
      constructor(
        readonly url: string,
        readonly options: WorkerOptions,
      ) {
        workers.push(this);
      }

      addEventListener() {}
      postMessage() {}
      terminate() {}
    }

    vi.stubGlobal("Worker", FakeWorker);

    preloadWasm();

    expect(workers).toHaveLength(1);
    expect(workers[0]).toMatchObject({
      url: "/croc-worker.js",
      options: { name: "croc-protocol" },
    });
    expect(wasm()).toBe(wasm());
    expect(workers).toHaveLength(1);
  });
});
