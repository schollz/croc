import { describe, expect, it } from "vitest";
import { effWords, generateCode, secureRandomIndex } from "./codephrase";

describe("browser codephrase generation", () => {
  it("uses the exact 1,296-word EFF list", () => {
    expect(effWords).toHaveLength(1296);
    expect(effWords[0]).toBe("acid");
    expect(effWords.at(-1)).toBe("zoom");
    expect(effWords).toContain("yo-yo");
    expect(new Set(effWords).size).toBe(1296);
  });

  it("generates three words without WASM", () => {
    const indexes = [0, 1, 2];
    expect(generateCode(() => indexes.shift() ?? 0)).toBe("acid-acorn-acre");
  });

  it("returns secure indexes inside the requested range", () => {
    for (let sample = 0; sample < 100; sample += 1) {
      expect(secureRandomIndex(1296)).toBeGreaterThanOrEqual(0);
      expect(secureRandomIndex(1296)).toBeLessThan(1296);
    }
  });
});
