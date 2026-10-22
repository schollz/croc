import { describe, expect, it, vi } from "vitest";
import { CrocProtocolEngine } from "./engine";
class FakeWorker extends EventTarget {
  calls: Array<{ id: number; method: string; args: unknown[] }> = [];
  terminated = false;
  postMessage(
    message: { id: number; method: string; args: unknown[] },
    transfer: Transferable[],
  ) {
    // Real structured cloning detaches transfer buffers. Detect accidental caller ownership.
    this.calls.push(structuredClone(message, { transfer }));
  }
  terminate() {
    this.terminated = true;
  }
  result(result?: unknown) {
    this.dispatchEvent(
      new MessageEvent("message", {
        data: { id: this.calls.at(-1)!.id, result },
      }),
    );
  }
}
describe("native engine worker lifecycle", () => {
  it("keeps code parsing and routing independent of worker startup", async () => {
    const create = vi.fn();
    vi.stubGlobal("Worker", create);
    try {
      const engine = new CrocProtocolEngine();
      expect((await engine.codeComponents("apple-banana-zebra")).format).toBe(
        "three-word",
      );
      expect(await engine.relayIndex("apple-banana-zebra", 4)).toBeLessThan(4);
      expect(create).not.toHaveBeenCalled();
      engine.close();
    } finally {
      vi.unstubAllGlobals();
    }
  });
  it("copies retained buffers for encode, decode and hashing, including shared frame views", async () => {
    const worker = new FakeWorker(),
      engine = new CrocProtocolEngine(worker as unknown as Worker);
    const backing = Uint8Array.of(9, 1, 2, 3, 8);
    const calls = [
      () => engine.decodeChunk(7, backing.subarray(1, 4), true, 100),
      () => engine.encodeChunk(7, backing, false),
      () => engine.hashUpdate(1, backing),
    ];
    for (const call of calls) {
      const result = call();
      expect([...backing]).toEqual([9, 1, 2, 3, 8]);
      expect(worker.calls.at(-1)!.args[1]).not.toBe(backing);
      worker.result(new Uint8Array());
      await result;
    }
    engine.close();
  });
  it("rejects cancellation during worker allocation and releases a late PAKE context", async () => {
    const worker = new FakeWorker(),
      engine = new CrocProtocolEngine(worker as unknown as Worker);
    const controller = new AbortController();
    const result = engine.pakeInit(
      Uint8Array.of(1),
      0,
      "p256",
      controller.signal,
    );
    controller.abort();
    await expect(result).rejects.toMatchObject({ name: "AbortError" });
    worker.result({ handle: 71, bytes: new Uint8Array() });
    await vi.waitFor(() =>
      expect(worker.calls.at(-1)).toMatchObject({
        method: "pakeRelease",
        args: [71],
      }),
    );
    worker.result();
    engine.close();
  });
  it("rejects errors whose browser DOMException message is empty", async () => {
    const worker = new FakeWorker(),
      engine = new CrocProtocolEngine(worker as unknown as Worker);
    const result = engine.decrypt(new Uint8Array(28), new Uint8Array(32));
    worker.dispatchEvent(
      new MessageEvent("message", {
        data: { id: worker.calls.at(-1)!.id, error: "" },
      }),
    );
    await expect(result).rejects.toThrow("Protocol operation failed");
    engine.close();
  });
  it("rejects all pending operations and destroys state on close, crash or message failure", async () => {
    for (const event of [
      undefined,
      new ErrorEvent("error", { message: "crashed" }),
      new MessageEvent("messageerror"),
    ]) {
      const worker = new FakeWorker(),
        engine = new CrocProtocolEngine(worker as unknown as Worker);
      const pending = Promise.allSettled([
        engine.hashInit(),
        engine.pakeInit(Uint8Array.of(1), 0, "p256"),
      ]);
      if (event) worker.dispatchEvent(event);
      else engine.close();
      expect((await pending).map((x) => x.status)).toEqual([
        "rejected",
        "rejected",
      ]);
      expect(worker.terminated).toBe(true);
      await expect(engine.hashInit()).rejects.toThrow();
    }
  });
});
