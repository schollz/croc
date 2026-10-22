import { expect, test } from "@playwright/test";
import { disableWasm } from "./no-wasm";
import vectors from "../testdata/native-vectors.json" with { type: "json" };

test("native worker primitives enforce boundaries with WebAssembly unavailable", async ({
  page,
  context,
}) => {
  const verify = await disableWasm(context);
  await page.goto("/");
  await page.getByRole("button", { name: "Send live", exact: true }).click();
  await page.locator('.send-panel input[type="file"]').setInputFiles({
    name: "probe.bin",
    mimeType: "application/octet-stream",
    buffer: Buffer.from([1]),
  });
  await expect.poll(() => page.workers().length).toBeGreaterThan(0);
  const workerURL = page
    .workers()
    .find((w) => w.url().includes("engine-worker"))!
    .url();
  const hashes = vectors
    .filter((v) => v.request.Method === "hashPattern")
    .map((v) => ({ size: v.request.Count!, expected: v.expected as string }));
  await page.evaluate(
    async ({ workerURL, hashes }) => {
      const worker = new Worker(workerURL, { type: "module" });
      Object.assign(window, { __nativeProbeWorker: worker });
      let next = 0;
      const pending = new Map<
        number,
        { resolve(value: unknown): void; reject(error: Error): void }
      >();
      worker.onmessage = (event) => {
        const { id, result, error } = event.data;
        const request = pending.get(id)!;
        pending.delete(id);
        if (error !== undefined)
          request.reject(new Error(error || "Protocol operation failed"));
        else request.resolve(result);
      };
      const call = <T>(method: string, ...args: unknown[]) =>
        new Promise<T>((resolve, reject) => {
          const id = ++next;
          pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
          worker.postMessage({ id, method, args });
        });
      const equal = (a: Uint8Array, b: Uint8Array) => {
        if (a.length !== b.length || a.some((x, i) => x !== b[i]))
          throw new Error("Byte mismatch");
      };
      const rejects = async (operation: Promise<unknown>) => {
        let failed = false;
        try {
          await operation;
        } catch {
          failed = true;
        }
        if (!failed) throw new Error("Invalid input was accepted");
      };
      try {
        for (const curve of ["p256", "p384", "p521", "siec", "ed25519"]) {
          const a = await call<{ handle: number; bytes: Uint8Array }>(
            "pakeInitWithIdentities",
            new Uint8Array([1, 2, 3]),
            0,
            curve,
            "peer-transfer",
            "browser-room",
          );
          const b = await call<{ handle: number; bytes: Uint8Array }>(
            "pakeInitWithIdentities",
            new Uint8Array([1, 2, 3]),
            1,
            curve,
            "peer-transfer",
            "browser-room",
          );
          const bk = await call<{ bytes: Uint8Array; key: Uint8Array }>(
            "pakeUpdate",
            b.handle,
            a.bytes,
          );
          const ak = await call<{ key: Uint8Array }>(
            "pakeUpdate",
            a.handle,
            bk.bytes,
          );
          equal(ak.key, bk.key);
          await rejects(call("pakeUpdate", a.handle, bk.bytes));
        }
        for (const { size, expected } of hashes) {
          const data = Uint8Array.from(
              { length: size },
              (_, i) => (i * 37 + (i % 31)) % 256,
            ),
            handle = await call<number>("hashInit");
          for (let at = 0; at < size; at += 13)
            await call("hashUpdate", handle, data.subarray(at, at + 13));
          equal(
            await call<Uint8Array>("hashFinal", handle),
            Uint8Array.from(atob(expected), (c) => c.charCodeAt(0)),
          );
          await rejects(call("hashFinal", handle));
        }
        for (const size of [0, 1, 32776, 65536]) {
          const data = new Uint8Array(size).fill(97),
            encoded = await call<Uint8Array>("compress", data);
          equal(await call<Uint8Array>("decompress", encoded, size), data);
          if (size) await rejects(call("decompress", encoded, size - 1));
          await rejects(call("decompress", encoded.slice(0, -1), size));
        }
        await rejects(call("decompress", Uint8Array.of(255, 255, 255), 32776));
        for (const limit of [-1, 0.5, NaN, Infinity])
          await rejects(call("decompress", new Uint8Array(), limit));
        const handle = await call<number>("cipherInit", new Uint8Array(32));
        const plain = new Uint8Array(32776).fill(17),
          encoded = await call<Uint8Array>("encodeChunk", handle, plain, true);
        equal(
          await call<Uint8Array>("decodeChunk", handle, encoded, true, 32776),
          plain,
        );
        await rejects(call("decodeChunk", handle, encoded, true, 32775));
        const tampered = encoded.slice();
        tampered[15] ^= 1;
        await rejects(call("decodeChunk", handle, tampered, true, 32776));
        await rejects(
          call("decodeChunk", handle, encoded.slice(0, 27), false, 32776),
        );
        await call("cipherRelease", handle);
        await rejects(call("encodeChunk", handle, plain, false));
      } catch (error) {
        worker.terminate();
        throw error;
      }
    },
    { workerURL, hashes },
  );
  await verify();
  await page.evaluate(() =>
    (
      window as typeof window & { __nativeProbeWorker: Worker }
    ).__nativeProbeWorker.terminate(),
  );
});
