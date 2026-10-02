import { afterEach, describe, expect, it, vi } from "vitest";

const wasmMocks = vi.hoisted(() => ({
  storeGenerateKey: vi.fn(async () => new Uint8Array(32)),
  storeRedeemCapability: vi.fn(async () => new Uint8Array(32)),
  storeSealManifest: vi.fn(async () => new Uint8Array(29)),
  storeSealChunk: vi.fn(async () => new Uint8Array(32)),
  storeOpenChunk: vi.fn(async (
    _key: Uint8Array,
    _id: string,
    _objectIndex: number,
    _fileIndex: number,
    _fileChunk: number,
    plainSize: number,
    _ciphertext: Uint8Array,
  ) => new Uint8Array(plainSize)),
}));

vi.mock("../wasm/client", () => ({ wasm: () => wasmMocks }));
import {
  formatStoredBrowserURL,
  formatStoredCLIToken,
  parseStoredShare,
  prepareStoredFiles,
  receiveStoredTransfer,
  storedChunkSize,
  uploadStoredFiles,
  type StoredInspection,
} from "./stored";
import type { FileProgress } from "./types";

describe("stored-transfer shares", () => {
  const share = {
    origin: "https://files.example.test",
    id: "AwMDAwMDAwMDAwMDAwMDAw",
    key: new Uint8Array(32).fill(4),
  };

  it("round trips browser fragment URLs", () => {
    expect(parseStoredShare(formatStoredBrowserURL(share))).toEqual(share);
  });

  it("round trips CLI tokens", () => {
    expect(parseStoredShare(formatStoredCLIToken(share))).toEqual(share);
  });

  it("rejects links without a fragment key", () => {
    expect(() =>
      parseStoredShare("https://files.example.test/s/AwMDAwMDAwMDAwMDAwMDAw"),
    ).toThrow(/invalid stored-transfer URL/i);
  });

  it("rejects non-canonical links with query parameters", () => {
    expect(() =>
      parseStoredShare(
        "https://files.example.test/s/AwMDAwMDAwMDAwMDAwMDAw?tracking=1#v1.BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ",
      ),
    ).toThrow(/invalid stored-transfer URL/i);
  });
});

describe("stored file preparation", () => {
  it("reuses a SHA-256 hash that was started before Store", async () => {
    const file = new File(["croc"], "croc.txt", {
      lastModified: 1_723_420_800_000,
    });
    const digest = Uint8Array.of(4, 3, 2, 1);
    const hashProvider = vi.fn(async () => digest);

    const [prepared] = await prepareStoredFiles(
      [file],
      {
        storeAPI: "/api/v1/store",
        maxTransferBytes: 1024,
        maxFiles: 10,
        maxDownloads: 3,
        maxExpiresSeconds: 0,
      },
      {},
      undefined,
      hashProvider,
    );

    expect(hashProvider).toHaveBeenCalledWith(file);
    expect(prepared.sha256).toBe(digest);
  });
});

describe("stored-transfer download limits", () => {
  const settings = {
    storeAPI: "/api/v1/store",
    maxTransferBytes: 1024,
    maxFiles: 10,
    maxDownloads: 3,
    maxExpiresSeconds: 0,
  };

  it("rejects non-positive download counts before upload", async () => {
    await expect(
      uploadStoredFiles({ files: [], settings, downloads: 0 }),
    ).rejects.toThrow(/positive integer/i);
  });

  it("rejects counts above the server limit before upload", async () => {
    await expect(
      uploadStoredFiles({ files: [], settings, downloads: 4 }),
    ).rejects.toThrow(/at most 3 downloads/i);
  });
});

describe("stored-upload rate limits", () => {
  afterEach(() => vi.unstubAllGlobals());

  it.each([
    ["active-uploads", "60", /unfinished stored uploads.*about 1 minute/],
    ["create-rate", "91", /hourly stored-upload limit.*about 2 minutes/],
    ["create-rate", "invalid", /hourly stored-upload limit.*reached$/],
    ["", "", /Too many stored uploads; please try again later$/],
  ])("explains %s with Retry-After %s", async (reason, retryAfter, expected) => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("rate exceeded", {
      status: 429,
      headers: {
        "X-Croc-Rate-Limit-Reason": reason,
        "Retry-After": retryAfter,
      },
    })));
    await expect(uploadStoredFiles({
      files: [],
      settings: {
        storeAPI: "/api/v1/store", maxTransferBytes: 1024,
        maxFiles: 10, maxDownloads: 1, maxExpiresSeconds: 0,
      },
    })).rejects.toThrow(expected);
  });
});

describe("stored-transfer progress", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("reports the payload phase from zero through the declared total", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        if (init?.method === "POST" && init.body) {
          return new Response(
            JSON.stringify({
              id: "AwMDAwMDAwMDAwMDAwMDAw",
              uploadToken:
                "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ",
              uploadExpiresAt: "2026-08-11T13:00:00Z",
              chunkSize: 4 * 1024 * 1024,
            }),
            {
              status: 201,
              headers: {
                "Content-Type": "application/json",
                "X-Croc-Downloads": "1",
              },
            },
          );
        }
        if (init?.method === "PUT") {
          return new Response(null, { status: 204 });
        }
        expect(String(input)).toMatch(/\/complete$/);
        return new Response(
          JSON.stringify({ expiresAt: "2026-08-14T12:00:00Z" }),
          { status: 200, headers: { "Content-Type": "application/json" } },
        );
      },
    );
    vi.stubGlobal("fetch", fetchMock);
    const progress: Array<{ totalBytes: number; totalSize: number }> = [];
    const file = new File(["croc"], "croc.txt", { lastModified: 0 });

    await uploadStoredFiles({
      files: [
        {
          file,
          name: file.name,
          size: file.size,
          hash: new Uint8Array(),
          sha256: new Uint8Array(32),
          modified: new Date(file.lastModified).toISOString(),
          firstChunk: 0,
          chunkCount: 1,
        },
      ],
      settings: {
        storeAPI: "/api/v1/store",
        maxTransferBytes: 1024,
        maxFiles: 10,
        maxDownloads: 3,
        maxExpiresSeconds: 0,
      },
      callbacks: {
        onProgress: ({ totalBytes, totalSize }) =>
          progress.push({ totalBytes, totalSize }),
      },
    });

    expect(progress).toEqual([
      { totalBytes: 0, totalSize: 4 },
      { totalBytes: 4, totalSize: 4 },
    ]);
  });
});

describe("stored-download progress", () => {
  afterEach(() => {
    sessionStorage.clear();
    vi.unstubAllGlobals();
    vi.clearAllMocks();
  });

  function startDownload(sizes: number[]) {
    let chunkCount = 0;
    const hash = new Uint8Array(32);
    const files = sizes.map((size, index) => {
      const file = {
        n: `file-${index}.bin`,
        s: size,
        m: new Date(0).toISOString(),
        h: btoa(String.fromCharCode(...hash)),
        fc: chunkCount,
        cc: Math.ceil(size / storedChunkSize),
      };
      chunkCount += file.cc;
      return file;
    });
    const inspection: StoredInspection = {
      share: {
        origin: "https://files.example.test",
        id: "AwMDAwMDAwMDAwMDAwMDAw",
        key: new Uint8Array(32),
      },
      manifest: { v: 1, cs: storedChunkSize, f: files },
      offer: {
        kind: "files",
        files: files.map((file) => ({
          name: file.n, path: file.n, folder: "./", size: file.s, hash,
        })),
        emptyFolders: [],
        totalSize: sizes.reduce((total, size) => total + size, 0),
        senderMachineID: "encrypted temporary storage",
        noCompress: true,
        perFileCompression: false,
      },
    };
    const controllers: ReadableStreamDefaultController<Uint8Array>[] = [];
    const responses = Array.from({ length: chunkCount }, () => new Response(
      new ReadableStream<Uint8Array>({
        start(controller) { controllers.push(controller); },
      }),
    ));
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.endsWith("/claim")) {
        return new Response(JSON.stringify({
          claimToken: "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ",
        }));
      }
      if (url.endsWith("/commit")) return new Response(null, { status: 204 });
      const index = Number(url.match(/\/chunks\/(\d+)$/)![1]);
      return responses[index];
    });
    vi.stubGlobal("fetch", fetchMock);
    const sinks = sizes.map(() => ({
      writeAt: vi.fn(async (_position: number, _bytes: Uint8Array) => undefined),
      finalize: vi.fn(async () => undefined),
      hash: vi.fn(async () => hash),
      commit: vi.fn(async () => undefined),
      abort: vi.fn(async () => undefined),
    }));
    const progress: FileProgress[] = [];
    const onFileComplete = vi.fn();
    const done = receiveStoredTransfer({
      inspection,
      settings: {
        storeAPI: "/api/v1/store",
        maxTransferBytes: 1024 * 1024 * 1024,
        maxFiles: 10, maxDownloads: 3, maxExpiresSeconds: 0,
      },
      callbacks: {
        onOffer: async () => ({
          openFile: async (file) => sinks[files.findIndex((f) => f.n === file.name)],
          createEmptyFolder: async () => undefined,
        }),
        onProgress: (update) => progress.push(update),
        onFileComplete,
      },
    });
    return { controllers, responses, sinks, progress, onFileComplete, fetchMock, done };
  }

  it("updates during each response without double-counting chunks or files", async () => {
    const { controllers, sinks, progress, done } = startDownload([
      storedChunkSize + 40, 100,
    ]);
    const chunks = [
      { size: storedChunkSize, fileIndex: 0, position: 0, total: 0 },
      { size: 40, fileIndex: 0, position: storedChunkSize, total: storedChunkSize },
      { size: 100, fileIndex: 1, position: 0, total: storedChunkSize + 40 },
    ];
    for (const [index, chunk] of chunks.entries()) {
      const ciphertext = new Uint8Array(chunk.size + 28).fill(index + 1);
      const split = ciphertext.length / 2;
      controllers[index].enqueue(ciphertext.slice(0, split));
      await vi.waitFor(() => expect(progress.at(-1)).toMatchObject({
        fileIndex: chunk.fileIndex,
        fileBytes: chunk.position + chunk.size / 2,
        totalBytes: chunk.total + chunk.size / 2,
      }));
      // Receiving bytes must update the UI before the full chunk is decrypted.
      expect(wasmMocks.storeOpenChunk).toHaveBeenCalledTimes(index);
      controllers[index].enqueue(ciphertext.slice(split));
      controllers[index].close();
      await vi.waitFor(() => expect(sinks[chunk.fileIndex].writeAt.mock.calls.at(-1)?.[0])
        .toBe(chunk.position));
      expect(sinks[chunk.fileIndex].writeAt.mock.calls.at(-1)?.[1].byteLength)
        .toBe(chunk.size);
      const received = wasmMocks.storeOpenChunk.mock.calls[index][6];
      expect(received.byteLength).toBe(ciphertext.byteLength);
      expect(received.every((byte, offset) => byte === ciphertext[offset])).toBe(true);
    }
    await expect(done).resolves.toBe(0);
    expect(progress[0].totalBytes).toBe(0);
    expect(progress.at(-1)?.totalBytes).toBe(storedChunkSize + 140);
    expect(progress.map((update) => update.totalBytes)).toEqual(
      progress.map((update) => update.totalBytes).sort((a, b) => a - b),
    );
    for (const sink of sinks) {
      expect(sink.hash).toHaveBeenCalledWith("sha256");
      expect(sink.commit).toHaveBeenCalledOnce();
    }
  });

  it.each([
    new Error("Network interrupted"),
    new DOMException("Transfer cancelled", "AbortError"),
  ])("aborts an interrupted response without completing the file: %s", async (error) => {
    const { controllers, responses, sinks, progress, onFileComplete, fetchMock, done } =
      startDownload([100]);
    const failed = expect(done).rejects.toThrow(error);
    controllers[0].enqueue(new Uint8Array(64));
    await vi.waitFor(() => expect(progress.at(-1)?.totalBytes).toBe(50));
    controllers[0].error(error);
    await failed;
    expect(sinks[0].abort).toHaveBeenCalledOnce();
    expect(sinks[0].writeAt).not.toHaveBeenCalled();
    expect(sinks[0].commit).not.toHaveBeenCalled();
    expect(onFileComplete).not.toHaveBeenCalled();
    expect(fetchMock.mock.calls.some(([url]) => String(url).endsWith("/commit")))
      .toBe(false);
    expect(responses[0].body?.locked).toBe(false);
  });
});

describe("stored-transfer expiration", () => {
  const settings = {
    storeAPI: "/api/v1/store",
    maxTransferBytes: 1024,
    maxFiles: 10,
    maxDownloads: 3,
    maxExpiresSeconds: 3 * 24 * 60 * 60,
  };

  afterEach(() => vi.unstubAllGlobals());

  async function upload(expiresSeconds?: number) {
    const bodies: Array<Record<string, unknown>> = [];
    const expiresAt = "2026-08-14T12:00:00Z";
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === "POST" && init.body) {
        bodies.push(JSON.parse(String(init.body)) as Record<string, unknown>);
        return new Response(
          JSON.stringify({
            id: "AwMDAwMDAwMDAwMDAwMDAw",
            uploadToken:
              "BAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ",
            uploadExpiresAt: "2026-08-11T13:00:00Z",
            chunkSize: 4 * 1024 * 1024,
          }),
          {
            status: 201,
            headers: {
              "Content-Type": "application/json",
              "X-Croc-Downloads": "1",
            },
          },
        );
      }
      if (init?.method === "PUT") return new Response(null, { status: 204 });
      return new Response(JSON.stringify({ expiresAt }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);
    const result = await uploadStoredFiles({
      files: [],
      settings,
      expiresSeconds,
    });
    return { bodies, result };
  }

  it("omits the one-day default and uses the completed expiration", async () => {
    const { bodies, result } = await upload();
    expect(bodies[0]).not.toHaveProperty("expiresSeconds");
    expect(result.expiresAt).toBe("2026-08-14T12:00:00Z");
  });

  it("sends a custom expiration", async () => {
    const { bodies } = await upload(2 * 24 * 60 * 60);
    expect(bodies[0]).toHaveProperty("expiresSeconds", 172800);
  });

  it("enforces the runtime maximum before upload", async () => {
    await expect(
      uploadStoredFiles({
        files: [],
        settings,
        expiresSeconds: 4 * 24 * 60 * 60,
      }),
    ).rejects.toThrow(/at most/i);
  });
});

describe("stored commit recovery", () => {
  afterEach(() => {
    sessionStorage.clear();
    vi.unstubAllGlobals();
  });

  it("retries a previously verified commit without downloading again", async () => {
    const id = "AwMDAwMDAwMDAwMDAwMDAw";
    sessionStorage.setItem(`croc-store-claim:${id}`, "persisted-claim");
    sessionStorage.setItem(`croc-store-verified:${id}`, "true");
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(null, {
        status: 204,
        headers: { "X-Croc-Downloads-Remaining": "2" },
      }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const onOffer = vi.fn(async () => false as const);

    const remaining = await receiveStoredTransfer({
      inspection: {
        share: {
          origin: "https://files.example.test",
          id,
          key: new Uint8Array(32),
        },
        manifest: { v: 1, cs: 4 * 1024 * 1024, f: [] },
        offer: {
          kind: "files",
          files: [],
          emptyFolders: [],
          totalSize: 0,
          senderMachineID: "encrypted temporary storage",
          noCompress: true,
          perFileCompression: false,
        },
      },
      settings: {
        storeAPI: "/api/v1/store",
        maxTransferBytes: 1024,
        maxFiles: 10,
        maxDownloads: 3,
        maxExpiresSeconds: 0,
      },
      callbacks: { onOffer },
    });

    expect(remaining).toBe(2);
    expect(onOffer).not.toHaveBeenCalled();
    expect(fetchMock).toHaveBeenCalledOnce();
    expect(fetchMock.mock.calls[0][1]).toMatchObject({ method: "POST" });
    expect(sessionStorage.getItem(`croc-store-claim:${id}`)).toBeNull();
    expect(sessionStorage.getItem(`croc-store-verified:${id}`)).toBeNull();
  });
});
