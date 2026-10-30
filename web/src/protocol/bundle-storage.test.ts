import { describe, expect, it, vi } from "vitest";
import { BundleSink } from "./bundle-storage";
import { XXH64 } from "./xxh64";
import type { OfferedFile, ReceiveSink } from "./types";

function hash(bytes: Uint8Array) {
  const h = new XXH64();
  h.update(bytes);
  return h.digest();
}
const payload = new TextEncoder().encode("abcdefghij");
const files: OfferedFile[] = ["a", "bc", "def", "ghij"].map((text, index) => ({
  name: `${index}.txt`,
  folder: ".",
  path: `${index}.txt`,
  size: text.length,
  hash: hash(new TextEncoder().encode(text)),
}));
const indices = [0, 1, 2, 3],
  offsets = [0, 1, 3, 6];
function destination(failIndex = -1) {
  const committed: string[] = [],
    opened: string[] = [],
    aborted: string[] = [];
  return {
    committed,
    opened,
    aborted,
    createEmptyFolder: vi.fn(),
    async openFile(file: OfferedFile): Promise<ReceiveSink> {
      opened.push(file.path);
      let bytes = new Uint8Array(file.size);
      return {
        async writeAt(position, data) {
          if (opened.length - 1 === failIndex) throw new Error("disk full");
          bytes.set(data, position);
        },
        async finalize() {},
        async hash() {
          return hash(bytes);
        },
        async commit() {
          committed.push(file.path);
        },
        async abort() {
          aborted.push(file.path);
          bytes = new Uint8Array();
        },
      };
    },
  };
}

describe("bounded browser bundle destinations", () => {
  it("verifies original members and commits once each", async () => {
    const sink = new BundleSink(payload.length),
      dest = destination(),
      complete = vi.fn();
    await sink.writeAt(0, payload);
    await sink.finalize();
    expect(await sink.hash()).toEqual(hash(payload));
    await sink.commitMembers(files, indices, offsets, dest, complete);
    expect(dest.committed).toEqual(files.map((file) => file.path));
    expect(complete.mock.calls).toEqual([[0], [1], [2], [3]]);
    await sink.abort();
    await expect(sink.hash()).rejects.toThrow(/ready|released/);
  });
  it("opens no destination if even the last member is corrupt", async () => {
    const sink = new BundleSink(payload.length),
      dest = destination();
    const corrupt = payload.slice();
    corrupt[9] ^= 1;
    await sink.writeAt(0, corrupt);
    await sink.finalize();
    await expect(
      sink.commitMembers(files, indices, offsets, dest, vi.fn()),
    ).rejects.toThrow(/verification/);
    expect(dest.opened).toEqual([]);
    await sink.abort();
  });
  it("retains the committed prefix and aborts the failed destination", async () => {
    const sink = new BundleSink(payload.length),
      dest = destination(2),
      complete = vi.fn();
    await sink.writeAt(0, payload);
    await sink.finalize();
    await expect(
      sink.commitMembers(files, indices, offsets, dest, complete),
    ).rejects.toThrow("disk full");
    expect(dest.committed).toEqual(["0.txt", "1.txt"]);
    expect(dest.aborted).toEqual(["2.txt"]);
    expect(complete.mock.calls).toEqual([[0], [1]]);
    await sink.abort();
  });
  it("checks cancellation before publication and enforces the 8 MiB ceiling", async () => {
    expect(() => new BundleSink((8 << 20) + 1)).toThrow();
    const sink = new BundleSink(payload.length),
      dest = destination(),
      controller = new AbortController();
    await sink.writeAt(0, payload);
    await sink.finalize();
    controller.abort();
    await expect(
      sink.commitMembers(
        files,
        indices,
        offsets,
        dest,
        vi.fn(),
        controller.signal,
      ),
    ).rejects.toThrow(/cancelled/);
    expect(dest.opened).toEqual([]);
    await sink.abort();
  });
  it("cancels during member verification before opening destinations", async () => {
    const bytes = new Uint8Array(512 << 10);
    const member = { ...files[0], size: bytes.length, hash: hash(bytes) };
    const sink = new BundleSink(bytes.length);
    const dest = destination();
    const controller = new AbortController();
    await sink.writeAt(0, bytes);
    await sink.finalize();
    const cancelled = new Promise<void>((resolve) => {
      setTimeout(() => {
        controller.abort();
        resolve();
      }, 0);
    });
    await expect(
      sink.commitMembers([member], [0], [0], dest, vi.fn(), controller.signal),
    ).rejects.toThrow(/cancelled/);
    await cancelled;
    expect(dest.opened).toEqual([]);
    await sink.abort();
  });
  it("retains committed members when cancellation interrupts publication", async () => {
    const sink = new BundleSink(payload.length);
    const dest = destination();
    const controller = new AbortController();
    const completed: number[] = [];
    await sink.writeAt(0, payload);
    await sink.finalize();
    await expect(
      sink.commitMembers(
        files,
        indices,
        offsets,
        dest,
        (index) => {
          completed.push(index);
          controller.abort();
        },
        controller.signal,
      ),
    ).rejects.toThrow(/cancelled/);
    expect(dest.committed).toEqual(["0.txt"]);
    expect(dest.opened).toEqual(["0.txt"]);
    expect(completed).toEqual([0]);
    await sink.abort();
  });
});
