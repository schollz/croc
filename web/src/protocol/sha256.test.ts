import { Blob as NodeBlob } from "node:buffer";
import { createHash } from "node:crypto";
import { sha256 } from "@noble/hashes/sha2.js";
import { afterEach, describe, expect, it, vi } from "vitest";
import { SHA256Hasher, hashSHA256Blob } from "./sha256";
import {
  DirectoryDestination,
  DownloadDestination,
  verifySinkSHA256,
} from "./storage";

vi.mock("./engine", () => ({
  protocolEngine: () => {
    throw new Error("SHA-256 destinations must not start the direct protocol engine");
  },
}));

class HashWorker extends EventTarget {
  hash = sha256.create();
  terminated = false;
  messages: { id: number; method: string; bytes?: Uint8Array }[] = [];
  postMessage(message: { id: number; method: string; bytes?: Uint8Array }) {
    this.messages.push(message);
    queueMicrotask(() => {
      if (this.terminated) return;
      if (message.method === "update") this.hash.update(message.bytes!);
      const result =
        message.method === "digest" ? this.hash.digest() : undefined;
      this.dispatchEvent(
        new MessageEvent("message", { data: { id: message.id, result } }),
      );
    });
  }
  terminate() {
    this.terminated = true;
  }
}

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("streaming SHA-256", () => {
  it.each([0, 3, 55, 56, 63, 64, 65, 1024 * 1024 + 1])(
    "matches the native digest for %i bytes across updates",
    async (size) => {
      const worker = new HashWorker();
      const hasher = new SHA256Hasher(undefined, worker as unknown as Worker);
      const input = Uint8Array.from(
        { length: size },
        (_, index) => index % 251,
      );
      for (let offset = 0; offset < size; offset += 63)
        await hasher.update(input.subarray(offset, offset + 63));
      expect(await hasher.digest()).toEqual(
        Uint8Array.from(createHash("sha256").update(input).digest()),
      );
      expect(worker.terminated).toBe(true);
    },
  );

  it("copies caller bytes before transferring them", async () => {
    const worker = new HashWorker(),
      hasher = new SHA256Hasher(undefined, worker as unknown as Worker);
    const bytes = Uint8Array.of(1, 2, 3);
    const update = hasher.update(bytes);
    bytes.fill(0);
    await update;
    expect(worker.messages[0].bytes).toEqual(Uint8Array.of(1, 2, 3));
    hasher.dispose();
  });

  it("cancels pending work and closes the worker", async () => {
    const worker = new HashWorker(),
      controller = new AbortController();
    const hasher = new SHA256Hasher(
      controller.signal,
      worker as unknown as Worker,
    );
    const pending = expect(
      hasher.update(Uint8Array.of(1)),
    ).rejects.toMatchObject({ name: "AbortError" });
    controller.abort();
    await pending;
    expect(worker.terminated).toBe(true);
    await expect(hasher.digest()).rejects.toMatchObject({ name: "AbortError" });
  });

  it("rejects pending work when a worker crashes or never responds", async () => {
    const worker = new HashWorker();
    vi.spyOn(worker, "postMessage").mockImplementation(() => {});
    const hasher = new SHA256Hasher(undefined, worker as unknown as Worker);
    const failure = expect(hasher.update(new Uint8Array())).rejects.toThrow(
      "crashed",
    );
    worker.dispatchEvent(new ErrorEvent("error", { message: "crashed" }));
    await failure;
    expect(worker.terminated).toBe(true);
    vi.useFakeTimers();
    const hung = new HashWorker();
    vi.spyOn(hung, "postMessage").mockImplementation(() => {});
    const pending = expect(
      new SHA256Hasher(undefined, hung as unknown as Worker).digest(),
    ).rejects.toThrow(/timed out/);
    await vi.advanceTimersByTimeAsync(30_000);
    await pending;
    expect(hung.terminated).toBe(true);
  });

  it("streams a Blob without reading its full arrayBuffer", async () => {
    vi.stubGlobal("Worker", HashWorker);
    const blob = new NodeBlob([new Uint8Array(128 * 1024).fill(3)]);
    const fullRead = vi.spyOn(blob, "arrayBuffer");
    const result = await hashSHA256Blob(blob);
    expect(result).toEqual(
      Uint8Array.from(
        createHash("sha256")
          .update(new Uint8Array(128 * 1024).fill(3))
          .digest(),
      ),
    );
    expect(fullRead).not.toHaveBeenCalled();
  });
});

describe("native destination verification", () => {
  it("verifies directory and Blob saves with WASM unavailable", async () => {
    vi.stubGlobal("Worker", HashWorker);
    vi.stubGlobal("Blob", NodeBlob);
    const chunks: Uint8Array<ArrayBuffer>[] = [];
    const root = {
      getFileHandle: async () => ({
        createWritable: async () => ({
          write: async (data: { data: Uint8Array }) => {
            chunks.push(data.data.slice());
          },
          close: async () => {},
          abort: async () => {},
        }),
        getFile: async () => new NodeBlob(chunks),
      }),
    } as unknown as FileSystemDirectoryHandle;
    const input = Uint8Array.of(1, 2, 3),
      expected = Uint8Array.from(createHash("sha256").update(input).digest());
    const file = {
      name: "native.bin",
      path: "native.bin",
      folder: ".",
      size: 3,
      hash: expected,
    };
    for (const destination of [
      new DirectoryDestination(root),
      new DownloadDestination(),
    ]) {
      const sink = await destination.openFile(file);
      await sink.writeAt(0, input);
      await sink.finalize();
      await expect(verifySinkSHA256(sink, expected)).resolves.toBeUndefined();
      await expect(verifySinkSHA256(sink, new Uint8Array(32))).rejects.toThrow(
        /SHA-256/,
      );
    }
  });
});
