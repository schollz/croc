import { describe, expect, it, vi } from "vitest";
import { createHash } from "node:crypto";
import codeWordList from "../../src/codephrase/wordlists/google-english-no-swears.txt?raw";
import { codeWords, generateCode, generateLetterCode, secureRandomIndex } from "./codephrase";

describe("browser codephrase generation", () => {
  it("uses the exact filtered 7,223-word Google list shared with Go", () => {
    expect(codeWords).toHaveLength(7223);
    expect(codeWords[0]).toBe("about");
    expect(codeWords.at(-1)).toBe("laos");
    expect(codeWords.every((word) => /^[a-z]{3,8}$/.test(word))).toBe(true);
    expect(new Set(codeWords).size).toBe(7223);
    expect(createHash("sha256").update(codeWordList.replace(/\r\n/g, "\n")).digest("hex"))
      .toBe("b407eaf72beeecb8d51a234492c132688fdd562128d9bf2405199e1c50aef2c3");
  });

  it("generates three words without WASM", () => {
    const indexes = [0, 1, 2];
    expect(generateCode(() => indexes.shift() ?? 0)).toBe("about-search-other");
    expect(generateCode((bound) => {
      expect(bound).toBe(7223);
      return bound - 1;
    })).toBe("laos-laos-laos");
  });

  it("generates exactly three eight-letter groups from the full alphabet", () => {
    const bounds: number[] = [];
    expect(generateLetterCode((bound) => {
      bounds.push(bound);
      return 0;
    })).toBe("aaaaaaaa-aaaaaaaa-aaaaaaaa");
    expect(bounds).toEqual(Array(24).fill(26));
    expect(generateLetterCode(() => 25)).toBe("zzzzzzzz-zzzzzzzz-zzzzzzzz");
    expect(generateLetterCode()).toMatch(/^[a-z]{8}-[a-z]{8}-[a-z]{8}$/);
  });

  it("rejects biased random samples outside the alphabet", () => {
    const samples = [0xffff_ffff, 0xffff_fff9, 25];
    const random = vi.spyOn(crypto, "getRandomValues").mockImplementation((array) => {
      (array as Uint32Array)[0] = samples.shift()!;
      return array;
    });
    try {
      expect(secureRandomIndex(26)).toBe(25);
      expect(random).toHaveBeenCalledTimes(3);
    } finally {
      random.mockRestore();
    }
  });

  it("returns secure indexes inside the requested range", () => {
    for (let sample = 0; sample < 100; sample += 1) {
      expect(secureRandomIndex(codeWords.length)).toBeGreaterThanOrEqual(0);
      expect(secureRandomIndex(codeWords.length)).toBeLessThan(codeWords.length);
    }
  });
});
