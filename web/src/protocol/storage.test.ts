import { afterEach, describe, expect, it, vi } from "vitest";
const wasmMocks = vi.hoisted(() => ({
  hashInit: vi.fn(async () => 1),
  hashUpdate: vi.fn(async () => undefined),
  hashFinal: vi.fn(async () => Uint8Array.of(1, 2, 3)),
}));

vi.mock("../wasm/client", () => ({
  wasm: () => wasmMocks,
}));

import { TextDestination, verifySink } from "./storage";
import type { ReceiveSink, TransferOffer } from "./types";

function sinkWithHash(hash: Uint8Array): ReceiveSink {
  return {
    writeAt: vi.fn(),
    finalize: vi.fn(),
    hash: vi.fn(async () => hash),
    commit: vi.fn(),
    abort: vi.fn(),
  };
}

describe("received file verification", () => {
  it("accepts the advertised xxhash", async () => {
    const hash = Uint8Array.of(0xed, 0x70, 0x2c, 0xee, 0x86, 0x16, 0xa8, 0x5f);
    await expect(verifySink(sinkWithHash(hash), hash)).resolves.toBeUndefined();
  });

  it("reports both hashes when verification fails", async () => {
    await expect(
      verifySink(sinkWithHash(Uint8Array.of(0xaa, 0xbb)), Uint8Array.of(0x01, 0x02)),
    ).rejects.toThrow(
      "The sender advertised xxhash 0102, but the received file hashes to aabb",
    );
  });
});

describe("received text destination", () => {
  const offered = {
    name: "croc-stdin-123",
    folder: ".",
    path: "croc-stdin-123",
    size: 10,
    hash: Uint8Array.of(1, 2, 3),
  };

  it("assembles, hashes, and reveals verified UTF-8 text only on commit", async () => {
    const onText = vi.fn();
    const sink = await new TextDestination(onText).openFile(offered);
    await sink.writeAt(6, new TextEncoder().encode("🐊"));
    await sink.writeAt(0, new TextEncoder().encode("hello\n"));
    await sink.finalize();

    expect(onText).not.toHaveBeenCalled();
    await expect(sink.hash()).resolves.toEqual(Uint8Array.of(1, 2, 3));
    await sink.commit();
    expect(onText).toHaveBeenCalledWith("hello\n🐊");
  });

  it("rejects malformed UTF-8 and aborted incomplete text", async () => {
    const malformed = await new TextDestination(vi.fn()).openFile({
      ...offered,
      size: 1,
    });
    await malformed.writeAt(0, Uint8Array.of(0xff));
    await malformed.finalize();
    await expect(malformed.commit()).rejects.toThrow(/valid UTF-8/i);

    const aborted = await new TextDestination(vi.fn()).openFile(offered);
    await aborted.writeAt(0, new TextEncoder().encode("hello"));
    await aborted.abort();
    await expect(aborted.finalize()).rejects.toThrow(/advertised size/i);
  });
});

function storedOffer(size = 1024): TransferOffer {
  return {
    kind: "files",
    files: [
      {
        name: "example.bin",
        folder: ".",
        path: "example.bin",
        size,
        hash: Uint8Array.of(1, 2, 3),
      },
    ],
    emptyFolders: [],
    totalSize: size,
    senderMachineID: "stored transfer",
    noCompress: true,
    perFileCompression: false,
  };
}

class FakeServiceWorkerContainer extends EventTarget {
  controller: ServiceWorker | null;
  ready = Promise.resolve({} as ServiceWorkerRegistration);
  register = vi.fn(async () => ({} as ServiceWorkerRegistration));

  constructor(controller: ServiceWorker | null = null) {
    super();
    this.controller = controller;
  }
}

describe("stored browser download destination", () => {
  const originalServiceWorker = Object.getOwnPropertyDescriptor(
    navigator,
    "serviceWorker",
  );
  const originalSecureContext = Object.getOwnPropertyDescriptor(
    window,
    "isSecureContext",
  );
  const originalBrave = Object.getOwnPropertyDescriptor(navigator, "brave");

  function installContainer(container: FakeServiceWorkerContainer) {
    Object.defineProperty(navigator, "serviceWorker", {
      configurable: true,
      value: container,
    });
    Object.defineProperty(window, "isSecureContext", {
      configurable: true,
      value: true,
    });
    vi.stubGlobal("MessageChannel", class {});
  }

  async function freshStorage() {
    vi.resetModules();
    return import("./storage");
  }

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    if (originalServiceWorker) {
      Object.defineProperty(navigator, "serviceWorker", originalServiceWorker);
    } else {
      Reflect.deleteProperty(navigator, "serviceWorker");
    }
    if (originalSecureContext) {
      Object.defineProperty(window, "isSecureContext", originalSecureContext);
    } else {
      Reflect.deleteProperty(window, "isSecureContext");
    }
    if (originalBrave) {
      Object.defineProperty(navigator, "brave", originalBrave);
    } else {
      Reflect.deleteProperty(navigator, "brave");
    }
  });

  it("uses an existing controller instead of merely an active registration", async () => {
    const controller = { postMessage: vi.fn() } as unknown as ServiceWorker;
    const container = new FakeServiceWorkerContainer(controller);
    installContainer(container);
    const storage = await freshStorage();

    const destination = await storage.chooseStoredReceiveDestination(storedOffer());

    expect(destination).toBeInstanceOf(storage.StreamingDownloadDestination);
    expect(container.register).toHaveBeenCalledWith("/croc-download-sw.js", {
      scope: "/",
    });
  });

  it("uses the bounded Blob path in Brave without starting a streaming download", async () => {
    const controller = { postMessage: vi.fn() } as unknown as ServiceWorker;
    const container = new FakeServiceWorkerContainer(controller);
    installContainer(container);
    Object.defineProperty(navigator, "brave", {
      configurable: true,
      value: { isBrave: vi.fn(async () => true) },
    });
    const storage = await freshStorage();

    const destination = await storage.chooseStoredReceiveDestination(storedOffer());

    expect(destination).toBeInstanceOf(storage.DownloadDestination);
    expect(container.register).not.toHaveBeenCalled();
  });

  it("waits for controllerchange before enabling a streaming download", async () => {
    const controller = { postMessage: vi.fn() } as unknown as ServiceWorker;
    const container = new FakeServiceWorkerContainer();
    const addListener = vi.spyOn(container, "addEventListener");
    const removeListener = vi.spyOn(container, "removeEventListener");
    installContainer(container);
    const storage = await freshStorage();
    let settled = false;

    const selecting = storage
      .chooseStoredReceiveDestination(storedOffer())
      .then((destination) => {
        settled = true;
        return destination;
      });
    await vi.waitFor(() =>
      expect(addListener).toHaveBeenCalledWith(
        "controllerchange",
        expect.any(Function),
      ),
    );
    expect(settled).toBe(false);

    container.controller = controller;
    container.dispatchEvent(new Event("controllerchange"));

    await expect(selecting).resolves.toBeInstanceOf(
      storage.StreamingDownloadDestination,
    );
    expect(removeListener).toHaveBeenCalledWith(
      "controllerchange",
      expect.any(Function),
    );
  });

  it("rechecks control after subscribing so the activation race is not missed", async () => {
    const controller = { postMessage: vi.fn() } as unknown as ServiceWorker;
    const container = new FakeServiceWorkerContainer();
    const addEventListener = container.addEventListener.bind(container);
    vi.spyOn(container, "addEventListener").mockImplementation(
      (type, listener, options) => {
        addEventListener(type, listener, options);
        container.controller = controller;
      },
    );
    installContainer(container);
    const storage = await freshStorage();

    await expect(
      storage.chooseStoredReceiveDestination(storedOffer()),
    ).resolves.toBeInstanceOf(storage.StreamingDownloadDestination);
  });

  it("falls back to a Blob download when control times out for a small file", async () => {
    vi.useFakeTimers();
    installContainer(new FakeServiceWorkerContainer());
    const storage = await freshStorage();

    const selecting = storage.chooseStoredReceiveDestination(storedOffer());
    await vi.advanceTimersByTimeAsync(10_000);

    await expect(selecting).resolves.toBeInstanceOf(storage.DownloadDestination);
  });

  it("rejects an oversized download when control times out", async () => {
    vi.useFakeTimers();
    installContainer(new FakeServiceWorkerContainer());
    const storage = await freshStorage();

    const selecting = storage.chooseStoredReceiveDestination(
      storedOffer(256 * 1024 * 1024 + 1),
    );
    const rejected = expect(selecting).rejects.toThrow(/croc CLI/i);
    await vi.advanceTimersByTimeAsync(10_000);

    await rejected;
  });
});
