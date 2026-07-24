import { afterEach, describe, expect, it, vi } from "vitest";
import { DownloadDestination } from "./storage";
import type { OfferedFile } from "./types";

function offered(name: string, size: number): OfferedFile {
  return { name, folder: ".", path: name, size, hash: new Uint8Array() };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("DownloadSink assembly", () => {
  it("reassembles out-of-order chunks into the original bytes", async () => {
    const created: Blob[] = [];
    vi.stubGlobal("URL", {
      createObjectURL: (blob: Blob) => {
        created.push(blob);
        return "blob:test";
      },
      revokeObjectURL: () => {},
    });

    const destination = new DownloadDestination();
    const sink = await destination.openFile(offered("a.bin", 8));
    // Deliberately write the second chunk first.
    await sink.writeAt(4, Uint8Array.of(5, 6, 7, 8));
    await sink.writeAt(0, Uint8Array.of(1, 2, 3, 4));
    await sink.finalize();
    await sink.commit();

    expect(created).toHaveLength(1);
    const bytes = new Uint8Array(await created[0].arrayBuffer());
    expect([...bytes]).toEqual([1, 2, 3, 4, 5, 6, 7, 8]);
  });

  it("keeps stored chunks independent of the caller's buffer", async () => {
    const created: Blob[] = [];
    vi.stubGlobal("URL", {
      createObjectURL: (blob: Blob) => {
        created.push(blob);
        return "blob:test";
      },
      revokeObjectURL: () => {},
    });

    const destination = new DownloadDestination();
    const sink = await destination.openFile(offered("b.bin", 4));
    const shared = Uint8Array.of(1, 2, 3, 4);
    await sink.writeAt(0, shared);
    // Mutating the caller's buffer after writeAt must not corrupt the download.
    shared.fill(0);
    await sink.finalize();
    await sink.commit();

    const bytes = new Uint8Array(await created[0].arrayBuffer());
    expect([...bytes]).toEqual([1, 2, 3, 4]);
  });
});
