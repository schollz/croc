import { webcrypto } from "node:crypto";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import fixture from "../../../src/storecrypto/testdata/browser-v1.json";
import { inspectStoredTransfer, manifestTimeoutMs } from "./stored";

vi.mock("./engine", () => ({
  protocolEngine: () => {
    throw new Error("Store must not start the direct protocol engine");
  },
}));
const ciphertext = Uint8Array.from(
  Buffer.from(fixture.manifestCiphertext, "base64url"),
);
const share = {
  origin: "https://files.example.test",
  id: fixture.id,
  key: Uint8Array.from(Buffer.from(fixture.key, "base64url")),
};
const settings = {
  storeAPI: "/api/v1/store",
  maxTransferBytes: 1024,
  maxFiles: 100,
  maxDownloads: 1,
  maxExpiresSeconds: 0,
};

beforeEach(() => vi.stubGlobal("crypto", webcrypto));
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("native stored-link inspection", () => {
  it("cancels even when the transport does not settle its fetch promise", async () => {
    const controller = new AbortController();
    const fetch = vi.fn(() => new Promise<Response>(() => {}));
    vi.stubGlobal("fetch", fetch);
    const result = expect(
      inspectStoredTransfer(share, settings, controller.signal),
    ).rejects.toMatchObject({ name: "AbortError" });
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledOnce());
    controller.abort();
    await result;
  });
  it("only fetches the manifest and reports the actual stages", async () => {
    const fetch = vi.fn(
      async (_input: RequestInfo | URL) => new Response(ciphertext),
    );
    vi.stubGlobal("fetch", fetch);
    const status = vi.fn();
    const result = await inspectStoredTransfer(
      share,
      settings,
      undefined,
      status,
    );
    expect(result.offer.files.map((file) => file.name)).toEqual([
      "hello.txt",
      "empty.bin",
    ]);
    expect(fetch).toHaveBeenCalledOnce();
    expect(fetch.mock.calls[0][0]).toBe(
      `/api/v1/store/transfers/${fixture.id}/manifest`,
    );
    expect(status.mock.calls.flat()).toEqual([
      "Fetching encrypted manifest…",
      "Decrypting and validating manifest…",
    ]);
  });

  it("cancels an oversized streaming response without reading it all", async () => {
    const cancel = vi.fn();
    vi.stubGlobal(
      "fetch",
      vi.fn(
        async () =>
          new Response(
            new ReadableStream({
              start(controller) {
                controller.enqueue(new Uint8Array(256 * 1024 + 1));
              },
              cancel,
            }),
          ),
      ),
    );
    await expect(inspectStoredTransfer(share, settings)).rejects.toThrow(
      /too large/,
    );
    expect(cancel).toHaveBeenCalledOnce();
  });

  it("times out a stalled body and permits a fresh retry", async () => {
    vi.useFakeTimers();
    const cancel = vi.fn();
    let fetched = false;
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        fetched = true;
        return new Response(new ReadableStream({ cancel }));
      }),
    );
    const pending = expect(
      inspectStoredTransfer(share, settings),
    ).rejects.toThrow(/timed out/);
    await vi.waitFor(() => expect(fetched).toBe(true));
    await vi.advanceTimersByTimeAsync(manifestTimeoutMs);
    await pending;
    expect(cancel).toHaveBeenCalledOnce();
    vi.useRealTimers();
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(ciphertext)),
    );
    await expect(
      inspectStoredTransfer(share, settings),
    ).resolves.toHaveProperty("offer");
  });

  it("does not publish a late crypto result after cancellation", async () => {
    const controller = new AbortController(),
      status = vi.fn();
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        controller.abort();
        return new Response(ciphertext);
      }),
    );
    await expect(
      inspectStoredTransfer(share, settings, controller.signal, status),
    ).rejects.toMatchObject({ name: "AbortError" });
    expect(status).toHaveBeenCalledTimes(1);
    const alreadyCancelled = new AbortController();
    alreadyCancelled.abort();
    await expect(
      inspectStoredTransfer(share, settings, alreadyCancelled.signal),
    ).rejects.toMatchObject({ name: "AbortError" });
  });
});
