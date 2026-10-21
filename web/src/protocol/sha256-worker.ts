import { sha256 } from "@noble/hashes/sha2.js";

const hash = sha256.create();
self.addEventListener(
  "message",
  (event: MessageEvent<{ id: number; method: string; bytes?: Uint8Array }>) => {
    const { id, method, bytes } = event.data;
    try {
      if (method === "update") hash.update(bytes!);
      else if (method !== "digest")
        throw new Error("Unknown SHA-256 operation");
      const result = method === "digest" ? hash.digest() : undefined;
      self.postMessage({ id, result }, result ? [result.buffer] : []);
    } catch (error) {
      self.postMessage({
        id,
        error: error instanceof Error ? error.message : String(error),
      });
    }
  },
);
