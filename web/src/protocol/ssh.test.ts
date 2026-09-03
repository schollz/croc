import { describe, expect, it } from "vitest";
import { encodeFrame, FrameDecoder } from "./framing";
import type { CrocMessage } from "./types";
import { validateSSHOffer } from "./ssh";

describe("browser SSH negotiation", () => {
  it("validates relay offers and preserves raw SSH bytes at handoff", () => {
    const valid: CrocMessage = {
      t: "ssh-offer",
      v: 2,
      m: "tailcat-address",
      b: new Uint8Array([1, 2, 3]),
      n: 22,
      f: ["read-write", "relay"],
    };
    expect(validateSSHOffer(valid)).toEqual({
      hostKey: valid.b,
      role: "read-write",
    });

    const malformed: Array<[string, CrocMessage]> = [
      ["version", { ...valid, v: 1 }],
      ["role", { ...valid, f: ["owner", "relay"] }],
      ["transport", { ...valid, f: ["read-write", "tailcat"] }],
      ["port", { ...valid, n: 23 }],
      ["host key", { ...valid, b: new Uint8Array() }],
    ];
    for (const [name, offer] of malformed) {
      expect(() => validateSSHOffer(offer), name).toThrow();
    }

    const offerFrame = encodeFrame(new Uint8Array([7, 8, 9]));
    const banner = new TextEncoder().encode("SSH-2.0-Go\r\n");
    const coalesced = new Uint8Array(offerFrame.length + banner.length);
    coalesced.set(offerFrame);
    coalesced.set(banner, offerFrame.length);
    const handoff = new FrameDecoder().pushOne(coalesced);
    expect(Array.from(handoff.message ?? [])).toEqual([7, 8, 9]);
    expect(Array.from(handoff.remainder ?? [])).toEqual(Array.from(banner));
  });
});
