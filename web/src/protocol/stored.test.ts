import { afterEach, describe, expect, it, vi } from "vitest";

const wasmMocks = vi.hoisted(() => ({
  storeGenerateKey: vi.fn(async () => new Uint8Array(32)),
  storeRedeemCapability: vi.fn(async () => new Uint8Array(32)),
  storeSealManifest: vi.fn(async () => new Uint8Array(29)),
}));

vi.mock("../wasm/client", () => ({ wasm: () => wasmMocks }));
import {
  formatStoredBrowserURL,
  formatStoredCLIToken,
  parseStoredShare,
  prepareStoredFiles,
  receiveStoredTransfer,
  uploadStoredFiles,
} from "./stored";

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
