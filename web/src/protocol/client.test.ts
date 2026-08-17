import { beforeEach, describe, expect, it, vi } from "vitest";

const wasmMocks = vi.hoisted(() => ({
  decrypt: vi.fn(),
  decompress: vi.fn(),
  compress: vi.fn(async (input: Uint8Array) => input),
}));

// Keep receiver tests off the (jsdom-less) Worker path.
vi.mock("../wasm/client", () => ({
  wasm: () => wasmMocks,
}));

import { DataReceiver, prepareFiles, prepareText } from "./client";
import type { CrocSocket } from "./transport";
import type { OfferedFile, ReceiveSink } from "./types";

function failingSocket(error: Error): CrocSocket {
  return {
    receive: () => Promise.reject(error),
  } as unknown as CrocSocket;
}

function neverSocket(): CrocSocket {
  return {
    receive: () => new Promise<Uint8Array>(() => {}),
  } as unknown as CrocSocket;
}

const key = new Uint8Array(32);

const file: OfferedFile = {
  name: "a.txt",
  folder: ".",
  path: "a.txt",
  size: 4,
  hash: new Uint8Array(),
};

const sink = {
  writeAt: () => Promise.resolve(),
  finalize: () => Promise.resolve(),
  hash: () => Promise.resolve(new Uint8Array()),
  commit: () => Promise.resolve(),
  abort: () => Promise.resolve(),
} satisfies ReceiveSink;

beforeEach(() => {
  wasmMocks.decrypt.mockReset().mockRejectedValue(new Error("unused"));
  wasmMocks.decompress.mockReset().mockRejectedValue(new Error("unused"));
});

describe("DataReceiver failure handling", () => {
  it("rejects a receive requested after a socket already failed", async () => {
    const receiver = new DataReceiver(
      [failingSocket(new Error("relay closed"))],
      key,
      true,
    );
    // Let the read loop observe the socket failure before we request a file.
    await Promise.resolve();
    await expect(receiver.receive(file, sink, () => {})).rejects.toThrow(
      "relay closed",
    );
  });

  it("rejects an in-flight receive when a socket fails", async () => {
    let reject: (error: Error) => void = () => {};
    const socket = {
      receive: () => new Promise<Uint8Array>((_, r) => (reject = r)),
    } as unknown as CrocSocket;
    const receiver = new DataReceiver([socket], key, true);
    const pending = receiver.receive(file, sink, () => {});
    reject(new Error("relay closed mid-transfer"));
    await expect(pending).rejects.toThrow("relay closed mid-transfer");
  });

  it("rejects new receives after stop()", async () => {
    const receiver = new DataReceiver([neverSocket()], key, true);
    receiver.stop();
    await expect(receiver.receive(file, sink, () => {})).rejects.toThrow(
      "Data receiver stopped",
    );
  });

  it("bounds decompressed chunks to the payload and position header", async () => {
    const encrypted = new Uint8Array([1]);
    const compressed = new Uint8Array([2]);
    const chunk = new Uint8Array(12);
    chunk.set([1, 2, 3, 4], 8);
    wasmMocks.decrypt.mockResolvedValueOnce(compressed);
    wasmMocks.decompress.mockResolvedValueOnce(chunk);

    const socket = {
      receive: vi
        .fn()
        .mockResolvedValueOnce(encrypted)
        .mockImplementation(() => new Promise<Uint8Array>(() => {})),
    } as unknown as CrocSocket;
    const receiver = new DataReceiver([socket], key, false);

    await receiver.receive(file, sink, () => {});

    expect(wasmMocks.decompress).toHaveBeenCalledWith(
      compressed,
      32 * 1024 + 8,
    );
    receiver.stop();
  });
});

describe("outgoing file preparation", () => {
  it("reuses a hash that was started before Send", async () => {
    const file = new File(["croc"], "croc.txt", {
      lastModified: 1_723_420_800_000,
    });
    const digest = Uint8Array.of(1, 2, 3, 4);
    const hashProvider = vi.fn(async () => digest);

    const [prepared] = await prepareFiles([file], {}, undefined, hashProvider);

    expect(hashProvider).toHaveBeenCalledWith(file);
    expect(prepared).toMatchObject({
      file,
      name: "croc.txt",
      size: 4,
      hash: digest,
    });
  });

  it("prepares exact UTF-8 text with the CLI-compatible temporary name", async () => {
    const digest = Uint8Array.of(9, 8, 7, 6);
    const hashProvider = vi.fn(async (payload: File) => {
      expect(await payload.text()).toBe("hello\n🐊");
      return digest;
    });

    const [prepared] = await prepareText(
      "hello\n🐊",
      {},
      undefined,
      hashProvider,
    );

    expect(prepared).toMatchObject({
      name: "croc-stdin-web",
      size: new Blob(["hello\n🐊"]).size,
      hash: digest,
    });
  });

  it("rejects empty and oversized text", async () => {
    await expect(prepareText("")).rejects.toThrow(/enter some text/i);
    await expect(prepareText("x".repeat(1024 * 1024 + 1))).rejects.toThrow(
      /1 MiB/i,
    );
  });
});
