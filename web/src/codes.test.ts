import { describe, expect, it } from "vitest";
import { formatGeneratedCode } from "./codes";

describe("formatGeneratedCode", () => {
  it("uses two words for compact layouts", () => {
    expect(formatGeneratedCode("1234-alpha-bravo-charlie", true)).toBe(
      "1234-alpha-bravo",
    );
  });

  it("keeps three words for wider layouts", () => {
    expect(formatGeneratedCode("1234-alpha-bravo-charlie", false)).toBe(
      "1234-alpha-bravo-charlie",
    );
  });

  it("does not shorten an already compact code", () => {
    expect(formatGeneratedCode("1234-alpha-bravo", true)).toBe(
      "1234-alpha-bravo",
    );
  });
});
