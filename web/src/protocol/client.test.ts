import { describe, expect, it, vi } from "vitest";

// The receiver spins up a WASM worker to decrypt data; none of these tests
// reach a decrypt, so a stub keeps them off the (jsdom-less) Worker path.
vi.mock("../wasm/client", () => ({
  wasm: () => ({
    decrypt: () => Promise.reject(new Error("unused")),
    decompress: () => Promise.reject(new Error("unused")),
  }),
}));

import { DataReceiver } from "./client";
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

describe("DataReceiver failure handling", () => {
  it("rejects a receive requested after a socket already failed", async () => {
    const receiver = new DataReceiver([failingSocket(new Error("relay closed"))], key, true);
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
});
