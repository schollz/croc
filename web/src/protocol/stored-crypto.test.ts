import { webcrypto } from "node:crypto";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fixture from "../../../src/storecrypto/testdata/browser-v1.json";
import validation from "../../../src/storecrypto/testdata/manifest-validation.json";
import { StoredCrypto, validateStoredManifest } from "./stored-crypto";

const decode = (value: string) =>
  Uint8Array.from(Buffer.from(value, "base64url"));
const key = decode(fixture.key);
beforeEach(() => vi.stubGlobal("crypto", webcrypto));
afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Go/browser stored-transfer interoperability", () => {
  it("derives the Go capability and decrypts its manifest and chunk", async () => {
    const crypto = new StoredCrypto(key);
    try {
      expect(await crypto.redeem()).toEqual(decode(fixture.redeem));
      expect(
        await crypto.openManifest(
          fixture.id,
          decode(fixture.manifestCiphertext),
          1024,
        ),
      ).toEqual(JSON.parse(fixture.manifestJSON));
      const chunk = fixture.chunk;
      expect(
        await crypto.openChunk(
          fixture.id,
          chunk.objectIndex,
          chunk.fileIndex,
          chunk.fileChunk,
          chunk.plainSize,
          decode(chunk.ciphertext),
        ),
      ).toEqual(decode(chunk.plaintext));
    } finally {
      crypto.close();
    }
  });

  it("produces byte-identical ciphertext for the fixed Go nonce vectors", async () => {
    const crypto = new StoredCrypto(key);
    try {
      vi.spyOn(webcrypto, "getRandomValues").mockImplementation((array) => {
        (array as Uint8Array).fill(2);
        return array;
      });
      expect(
        await crypto.sealManifest(
          fixture.id,
          new TextEncoder().encode(fixture.manifestJSON),
        ),
      ).toEqual(decode(fixture.manifestCiphertext));
      vi.spyOn(webcrypto, "getRandomValues").mockImplementation((array) => {
        (array as Uint8Array).fill(3);
        return array;
      });
      const chunk = fixture.chunk;
      expect(
        await crypto.sealChunk(
          fixture.id,
          0,
          0,
          0,
          chunk.plainSize,
          decode(chunk.plaintext),
        ),
      ).toEqual(decode(chunk.ciphertext));
    } finally {
      crypto.close();
    }
  });

  it("rejects wrong keys, altered ciphertext, transfer IDs, and chunk context", async () => {
    const crypto = new StoredCrypto(key),
      wrong = new StoredCrypto(new Uint8Array(32));
    try {
      const ciphertext = decode(fixture.manifestCiphertext);
      await expect(
        wrong.openManifest(fixture.id, ciphertext, 1024),
      ).rejects.toThrow(/authentication/);
      await expect(
        crypto.openManifest("wrong", ciphertext, 1024),
      ).rejects.toThrow(/authentication/);
      ciphertext[ciphertext.length - 1] ^= 1;
      await expect(
        crypto.openManifest(fixture.id, ciphertext, 1024),
      ).rejects.toThrow(/authentication/);
      const chunk = fixture.chunk;
      for (const context of [
        [1, 0, 0],
        [0, 1, 0],
        [0, 0, 1],
      ]) {
        await expect(
          crypto.openChunk(
            fixture.id,
            context[0],
            context[1],
            context[2],
            chunk.plainSize,
            decode(chunk.ciphertext),
          ),
        ).rejects.toThrow(/authentication/);
      }
    } finally {
      crypto.close();
      wrong.close();
    }
  });

  it("drops contexts and reports unsupported native crypto without using WASM", () => {
    const crypto = new StoredCrypto(key);
    crypto.close();
    expect(() => crypto.redeem()).toThrow(/closed/);
    vi.stubGlobal("crypto", {});
    expect(() => new StoredCrypto(key)).toThrow(/Web Crypto/);
  });
});

describe("shared Go/browser manifest validation", () => {
  for (const test of validation)
    it(test.name, () => {
      const validate = () =>
        validateStoredManifest(JSON.parse(test.manifest), test.maxBytes);
      if (test.valid) expect(validate).not.toThrow();
      else expect(validate).toThrow();
    });
});
