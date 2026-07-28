import { describe, expect, it } from "vitest";
import {
  formatStoredBrowserURL,
  formatStoredCLIToken,
  parseStoredShare,
} from "./stored";

describe("stored-transfer shares", () => {
  const share = {
    origin: "https://files.example.test",
    id: "AwMDAwMDAwMDAwMDAwMDAw",
    key: new Uint8Array(32).fill(4),
  };

  it("round trips browser fragment URLs", () => {
    expect(parseStoredShare(formatStoredBrowserURL(share))).toEqual(share);
  });

  it("round trips CLI tokens", () => {
    expect(parseStoredShare(formatStoredCLIToken(share))).toEqual(share);
  });

  it("rejects links without a fragment key", () => {
    expect(() =>
      parseStoredShare("https://files.example.test/s/AwMDAwMDAwMDAwMDAwMDAw"),
    ).toThrow(/invalid stored-transfer URL/i);
  });

  it("rejects non-canonical links with query parameters", () => {
    expect(() =>
      parseStoredShare(
        "https://files.example.test/s/AwMDAwMDAwMDAwMDAwMDAw?tracking=1#v1.BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ",
      ),
    ).toThrow(/invalid stored-transfer URL/i);
  });
});
