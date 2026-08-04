import { describe, expect, it, vi } from "vitest";
import { decodeMessage, encodeMessage } from "./codec";
import { MAX_FRAME_SIZE } from "./framing";
import type { CrocWasm } from "../wasm/client";

function identityWasm() {
  return {
    compress: vi.fn(async (bytes: Uint8Array) => bytes),
    decompress: vi.fn(async (bytes: Uint8Array) => bytes),
    encrypt: vi.fn(async (bytes: Uint8Array) => bytes),
    decrypt: vi.fn(async (bytes: Uint8Array) => bytes),
  } as unknown as CrocWasm;
}

describe("control message codec", () => {
  it("round trips croc's compact JSON and base64 byte fields", async () => {
    const engine = identityWasm();
    const encoded = await encodeMessage(
      engine,
      {
        t: "pake",
        v: 2,
        b: new Uint8Array([0, 1, 2, 255]),
        b2: new Uint8Array([9, 8]),
      },
      new Uint8Array(32),
    );
    const decoded = await decodeMessage(engine, encoded, new Uint8Array(32));
    expect(engine.decompress).toHaveBeenCalledWith(encoded, MAX_FRAME_SIZE);
    expect(decoded).toEqual({
      t: "pake",
      v: 2,
      m: undefined,
      b: new Uint8Array([0, 1, 2, 255]),
      b2: new Uint8Array([9, 8]),
      n: undefined,
    });
  });
});
