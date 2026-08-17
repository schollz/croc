import { describe, expect, it, vi } from "vitest";
const wasmMocks = vi.hoisted(() => ({
  hashInit: vi.fn(async () => 1),
  hashUpdate: vi.fn(async () => undefined),
  hashFinal: vi.fn(async () => Uint8Array.of(1, 2, 3)),
}));

vi.mock("../wasm/client", () => ({
  wasm: () => wasmMocks,
}));

import { TextDestination, verifySink } from "./storage";
import type { ReceiveSink } from "./types";

function sinkWithHash(hash: Uint8Array): ReceiveSink {
  return {
    writeAt: vi.fn(),
    finalize: vi.fn(),
    hash: vi.fn(async () => hash),
    commit: vi.fn(),
    abort: vi.fn(),
  };
}

describe("received file verification", () => {
  it("accepts the advertised xxhash", async () => {
    const hash = Uint8Array.of(0xed, 0x70, 0x2c, 0xee, 0x86, 0x16, 0xa8, 0x5f);
    await expect(verifySink(sinkWithHash(hash), hash)).resolves.toBeUndefined();
  });

  it("reports both hashes when verification fails", async () => {
    await expect(
      verifySink(sinkWithHash(Uint8Array.of(0xaa, 0xbb)), Uint8Array.of(0x01, 0x02)),
    ).rejects.toThrow(
      "The sender advertised xxhash 0102, but the received file hashes to aabb",
    );
  });
});

describe("received text destination", () => {
  const offered = {
    name: "croc-stdin-123",
    folder: ".",
    path: "croc-stdin-123",
    size: 10,
    hash: Uint8Array.of(1, 2, 3),
  };

  it("assembles, hashes, and reveals verified UTF-8 text only on commit", async () => {
    const onText = vi.fn();
    const sink = await new TextDestination(onText).openFile(offered);
    await sink.writeAt(6, new TextEncoder().encode("🐊"));
    await sink.writeAt(0, new TextEncoder().encode("hello\n"));
    await sink.finalize();

    expect(onText).not.toHaveBeenCalled();
    await expect(sink.hash()).resolves.toEqual(Uint8Array.of(1, 2, 3));
    await sink.commit();
    expect(onText).toHaveBeenCalledWith("hello\n🐊");
  });

  it("rejects malformed UTF-8 and aborted incomplete text", async () => {
    const malformed = await new TextDestination(vi.fn()).openFile({
      ...offered,
      size: 1,
    });
    await malformed.writeAt(0, Uint8Array.of(0xff));
    await malformed.finalize();
    await expect(malformed.commit()).rejects.toThrow(/valid UTF-8/i);

    const aborted = await new TextDestination(vi.fn()).openFile(offered);
    await aborted.writeAt(0, new TextEncoder().encode("hello"));
    await aborted.abort();
    await expect(aborted.finalize()).rejects.toThrow(/advertised size/i);
  });
});
