import { describe, expect, it, vi } from "vitest";
import { FileHashCache, waitForHash } from "./hash";

describe("background file hashing", () => {
  it("starts hashes eagerly and reuses the same result", async () => {
    const digest = Uint8Array.of(1, 2, 3, 4);
    let finishHash: (value: Uint8Array) => void = () => {};
    const hashFile = vi.fn(
      () => new Promise<Uint8Array>((resolve) => (finishHash = resolve)),
    );
    const cache = new FileHashCache(hashFile);
    const file = new File(["croc"], "croc.txt");

    cache.prime([file], "xxhash");
    const first = cache.hash(file, "xxhash");
    const second = cache.hash(file, "xxhash");

    expect(first).toBe(second);
    await Promise.resolve();
    expect(hashFile).toHaveBeenCalledOnce();
    finishHash(digest);
    await expect(first).resolves.toBe(digest);
  });

  it("cancels work for files that are no longer selected", async () => {
    let hashSignal: AbortSignal | undefined;
    const hashFile = vi.fn(
      (_file: File, _algorithm: string, signal?: AbortSignal) => {
        hashSignal = signal;
        return new Promise<Uint8Array>((_resolve, reject) => {
          signal?.addEventListener(
            "abort",
            () => reject(new DOMException("cancelled", "AbortError")),
            { once: true },
          );
        });
      },
    );
    const cache = new FileHashCache(hashFile);
    const file = new File(["croc"], "croc.txt");

    cache.prime([file], "sha256");
    await Promise.resolve();
    cache.retain([]);

    expect(hashSignal?.aborted).toBe(true);
  });

  it("lets a cancelled send stop waiting without cancelling the cached hash", async () => {
    const hash = new Promise<Uint8Array>(() => {});
    const controller = new AbortController();
    const waiting = waitForHash(hash, controller.signal);

    controller.abort();

    await expect(waiting).rejects.toMatchObject({ name: "AbortError" });
  });
});
