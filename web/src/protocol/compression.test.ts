// @vitest-environment node
import { afterEach, expect, it, vi } from "vitest";
import { checkProtocolCapabilities } from "./compression";
afterEach(() => vi.unstubAllGlobals());
it("checks secure crypto and raw DEFLATE support before connecting", () => {
  expect(() => checkProtocolCapabilities()).not.toThrow();
  vi.stubGlobal(
    "CompressionStream",
    class {
      constructor() {
        throw new TypeError("Unsupported format");
      }
    },
  );
  expect(() => checkProtocolCapabilities()).toThrow(
    /Web Crypto and raw DEFLATE/,
  );
  vi.unstubAllGlobals();
  vi.stubGlobal("crypto", undefined);
  expect(() => checkProtocolCapabilities()).toThrow(/HTTPS or localhost/);
});
