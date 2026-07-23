import { describe, expect, it, vi } from "vitest";
import { verifySink } from "./storage";
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
