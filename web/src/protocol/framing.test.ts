import { describe, expect, it } from "vitest";
import { FrameDecoder, MAX_FRAME_SIZE, encodeFrame } from "./framing";

describe("croc framing", () => {
  it("decodes a frame split across WebSocket messages", () => {
    const frame = encodeFrame(new Uint8Array([1, 2, 3, 4]));
    const decoder = new FrameDecoder();
    expect(decoder.push(frame.slice(0, 3))).toEqual([]);
    expect(decoder.push(frame.slice(3, 9))).toEqual([]);
    expect(decoder.push(frame.slice(9))).toEqual([
      new Uint8Array([1, 2, 3, 4]),
    ]);
  });

  it("decodes coalesced frames", () => {
    const first = encodeFrame(new Uint8Array([1]));
    const second = encodeFrame(new Uint8Array([2, 3]));
    const combined = new Uint8Array(first.length + second.length);
    combined.set(first);
    combined.set(second, first.length);
    expect(new FrameDecoder().push(combined)).toEqual([
      new Uint8Array([1]),
      new Uint8Array([2, 3]),
    ]);
  });

  it("rejects invalid magic bytes", () => {
    const frame = encodeFrame(new Uint8Array([1]));
    frame[0] = 0;
    expect(() => new FrameDecoder().push(frame)).toThrow(/croc framing/i);
  });

  it("rejects oversized writes and advertised reads", () => {
    expect(() => encodeFrame(new Uint8Array(MAX_FRAME_SIZE + 1))).toThrow(
      /too large/i,
    );
    const header = new Uint8Array(8);
    header.set(new TextEncoder().encode("croc"));
    new DataView(header.buffer).setUint32(4, MAX_FRAME_SIZE + 1, true);
    expect(() => new FrameDecoder().push(header)).toThrow(/too large/i);
  });
});
