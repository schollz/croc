import { bytesEqual, hex } from "./bytes";
import type {
  OfferedFile,
  ReceiveDestination,
  ReceiveSink,
  TransferOffer,
} from "./types";
import { maxTextTransferBytes } from "./types";
import { protocolEngine } from "./engine";
import { hashSHA256Blob, SHA256Hasher } from "./sha256";

declare global {
  interface Navigator {
    brave?: {
      isBrave?: () => Promise<boolean>;
    };
  }

  interface Window {
    showDirectoryPicker?: (options?: {
      mode?: "read" | "readwrite";
    }) => Promise<FileSystemDirectoryHandle>;
  }
}

async function hashBlob(
  blob: Blob,
  algorithm: "xxhash" | "sha256" = "xxhash",
  signal?: AbortSignal,
) {
  if (algorithm === "sha256") return hashSHA256Blob(blob, signal);
  const engine = protocolEngine();
  const handle = await engine.hashInit(signal);
  const reader = blob.stream().getReader();
  try {
    for (;;) {
      if (signal?.aborted)
        throw new DOMException("Transfer cancelled", "AbortError");
      const { done, value } = await reader.read();
      if (signal?.aborted)
        throw new DOMException("Transfer cancelled", "AbortError");
      if (done) break;
      await engine.hashUpdate(handle, value);
    }
    return await engine.hashFinal(handle);
  } finally {
    await reader.cancel().catch(() => undefined);
    reader.releaseLock();
    await engine.hashRelease(handle).catch(() => undefined);
  }
}

async function directoryAt(
  root: FileSystemDirectoryHandle,
  segments: string[],
  create: boolean,
) {
  let directory = root;
  for (const segment of segments) {
    directory = await directory.getDirectoryHandle(segment, { create });
  }
  return directory;
}

function splitPath(path: string) {
  return path.split("/").filter(Boolean);
}

async function fileExists(root: FileSystemDirectoryHandle, file: OfferedFile) {
  const segments = splitPath(file.path);
  const name = segments.pop()!;
  try {
    const directory = await directoryAt(root, segments, false);
    await directory.getFileHandle(name);
    return true;
  } catch (error) {
    if (error instanceof DOMException && error.name === "NotFoundError")
      return false;
    throw error;
  }
}

class DirectorySink implements ReceiveSink {
  private writable: FileSystemWritableFileStream;
  private closed = false;

  constructor(
    private handle: FileSystemFileHandle,
    writable: FileSystemWritableFileStream,
  ) {
    this.writable = writable;
  }

  async writeAt(position: number, bytes: Uint8Array) {
    if (this.closed) throw new Error("Destination file is already closed");
    await this.writable.write({
      type: "write",
      position,
      data: bytes.slice(),
    });
  }

  async finalize() {
    if (this.closed) return;
    this.closed = true;
    await this.writable.close();
  }

  async hash(algorithm: "xxhash" | "sha256" = "xxhash", signal?: AbortSignal) {
    if (!this.closed)
      throw new Error("Destination must be closed before hashing");
    return hashBlob(await this.handle.getFile(), algorithm, signal);
  }

  async commit() {}

  async abort() {
    if (this.closed) return;
    this.closed = true;
    await this.writable.abort().catch(() => undefined);
  }
}

export class DirectoryDestination implements ReceiveDestination {
  constructor(private root: FileSystemDirectoryHandle) {}

  async createEmptyFolder(path: string) {
    if (path === ".") return;
    await directoryAt(this.root, splitPath(path), true);
  }

  async openFile(file: OfferedFile) {
    const segments = splitPath(file.path);
    const name = segments.pop()!;
    const directory = await directoryAt(this.root, segments, true);
    const handle = await directory.getFileHandle(name, { create: true });
    const writable = await handle.createWritable({ keepExistingData: false });
    return new DirectorySink(handle, writable);
  }
}

class DownloadSink implements ReceiveSink {
  // Stored chunks own their backing ArrayBuffer (writeAt copies via slice()),
  // which lets finalize() hand them straight to Blob without a second copy.
  private chunks = new Map<number, Uint8Array<ArrayBuffer>>();
  private blob?: Blob;
  private committed = false;

  constructor(
    private name: string,
    private onDownload: (name: string, blob: Blob) => void,
  ) {}

  async writeAt(position: number, bytes: Uint8Array) {
    this.chunks.set(position, bytes.slice());
  }

  async finalize() {
    // Chunks are already private copies (see writeAt); hand them straight to
    // Blob, which copies its parts internally. Wrapping each one in
    // Uint8Array.from() first allocated a redundant second copy of the whole
    // file before the Blob was even built.
    const ordered = [...this.chunks.entries()]
      .sort(([left], [right]) => left - right)
      .map(([, bytes]) => bytes);
    this.blob = new Blob(ordered, { type: "application/octet-stream" });
    this.chunks.clear();
  }

  async hash(algorithm: "xxhash" | "sha256" = "xxhash", signal?: AbortSignal) {
    if (!this.blob)
      throw new Error("Destination must be finalized before hashing");
    return hashBlob(this.blob, algorithm, signal);
  }

  async commit() {
    if (!this.blob)
      throw new Error("Destination must be finalized before download");
    if (this.committed) return;
    this.committed = true;
    this.onDownload(this.name, this.blob);
  }

  async abort() {
    this.chunks.clear();
    this.blob = undefined;
  }
}

class TextSink implements ReceiveSink {
  private chunks = new Map<number, Uint8Array<ArrayBuffer>>();
  private bytes?: Uint8Array;
  private committed = false;

  constructor(
    private expectedSize: number,
    private onText: (text: string) => void,
  ) {}

  async writeAt(position: number, bytes: Uint8Array) {
    if (this.bytes) throw new Error("Text destination is already finalized");
    if (position < 0 || position + bytes.byteLength > this.expectedSize) {
      throw new Error("Text chunk is outside the advertised payload size");
    }
    this.chunks.set(position, bytes.slice());
  }

  async finalize() {
    if (this.bytes) return;
    const ordered = [...this.chunks.entries()].sort(
      ([left], [right]) => left - right,
    );
    let offset = 0;
    for (const [position, bytes] of ordered) {
      if (position !== offset)
        throw new Error("Text payload has a missing chunk");
      offset += bytes.byteLength;
    }
    if (offset !== this.expectedSize) {
      throw new Error("Text payload does not match its advertised size");
    }
    this.bytes = new Uint8Array(this.expectedSize);
    for (const [position, bytes] of ordered) this.bytes.set(bytes, position);
    this.chunks.clear();
  }

  async hash(algorithm: "xxhash" | "sha256" = "xxhash", signal?: AbortSignal) {
    if (!this.bytes)
      throw new Error("Text destination must be finalized before hashing");
    if (algorithm === "sha256") {
      const hasher = new SHA256Hasher(signal);
      try {
        await hasher.update(this.bytes);
        return await hasher.digest();
      } finally {
        hasher.dispose();
      }
    }
    const engine = protocolEngine();
    const handle = await engine.hashInit(signal);
    try {
      if (signal?.aborted)
        throw new DOMException("Transfer cancelled", "AbortError");
      await engine.hashUpdate(handle, this.bytes);
      return await engine.hashFinal(handle);
    } finally {
      await engine.hashRelease(handle).catch(() => undefined);
    }
  }

  async commit() {
    if (!this.bytes)
      throw new Error("Text destination must be finalized before display");
    if (this.committed) return;
    let text: string;
    try {
      text = new TextDecoder("utf-8", { fatal: true }).decode(this.bytes);
    } catch {
      throw new Error("The received text is not valid UTF-8");
    }
    this.committed = true;
    this.onText(text);
  }

  async abort() {
    this.chunks.clear();
    this.bytes = undefined;
  }
}

export class TextDestination implements ReceiveDestination {
  private opened = false;

  constructor(private onText: (text: string) => void) {}

  async createEmptyFolder() {
    throw new Error("Text transfers cannot contain folders");
  }

  async openFile(file: OfferedFile) {
    if (this.opened)
      throw new Error("Text transfers can contain only one payload");
    if (file.size <= 0 || file.size > maxTextTransferBytes) {
      throw new Error("Text payload must be between 1 byte and 1 MiB");
    }
    this.opened = true;
    return new TextSink(file.size, this.onText);
  }
}

let downloadWorker: Promise<ServiceWorker> | undefined;
const downloadWorkerControlTimeoutMs = 10_000;
const maxBlobDownloadBytes = 256 * 1024 * 1024;

function waitForDownloadController() {
  const container = navigator.serviceWorker;
  if (container.controller) return Promise.resolve(container.controller);

  return new Promise<ServiceWorker>((resolve, reject) => {
    let timer: number | undefined;
    const cleanup = () => {
      container.removeEventListener("controllerchange", onControllerChange);
      if (timer !== undefined) window.clearTimeout(timer);
    };
    const onControllerChange = () => {
      const controller = container.controller;
      if (!controller) return;
      cleanup();
      resolve(controller);
    };

    container.addEventListener("controllerchange", onControllerChange);
    // Recheck after subscribing so a controller change between the initial
    // check and listener registration cannot be missed.
    onControllerChange();
    if (container.controller) return;

    timer = window.setTimeout(() => {
      cleanup();
      reject(
        new Error(
          "The streaming download service did not take control of this page. Reload normally and try again.",
        ),
      );
    }, downloadWorkerControlTimeoutMs);
  });
}

async function streamingWorker() {
  downloadWorker ??= (async () => {
    if (
      !("serviceWorker" in navigator) ||
      typeof MessageChannel === "undefined"
    ) {
      throw new Error("Streaming browser downloads are unavailable");
    }
    await navigator.serviceWorker.register(
      `${import.meta.env.BASE_URL}croc-download-sw.js`,
      { scope: import.meta.env.BASE_URL },
    );
    await navigator.serviceWorker.ready;
    return waitForDownloadController();
  })();
  try {
    return await downloadWorker;
  } catch (error) {
    // A later transfer may succeed after the browser finishes activation.
    downloadWorker = undefined;
    throw error;
  }
}

class StreamingDownloadSink implements ReceiveSink {
  private offset = 0;
  private closed = false;
  private digest?: Uint8Array;
  private frame?: HTMLIFrameElement;
  private failure?: Error;
  private pending?: {
    resolve(): void;
    reject(error: Error): void;
    timer: ReturnType<typeof setTimeout>;
  };

  private constructor(
    private port: MessagePort,
    private hasher: SHA256Hasher,
  ) {
    this.port.addEventListener("message", (event) => {
      const pending = this.pending;
      if (!pending) return;
      this.pending = undefined;
      clearTimeout(pending.timer);
      if (event.data?.error) {
        this.failure = new Error(event.data.error);
        pending.reject(this.failure);
      } else pending.resolve();
    });
    this.port.start();
  }

  static async create(name: string, size: number, signal?: AbortSignal) {
    const worker = await streamingWorker();
    const id = crypto.randomUUID();
    const channel = new MessageChannel();
    const hasher = new SHA256Hasher(signal);
    const sink = new StreamingDownloadSink(channel.port1, hasher);
    try {
      await sink.send(
        { type: "prepare", id, name, size },
        [channel.port2],
        worker,
      );
    } catch (error) {
      hasher.dispose();
      channel.port1.close();
      throw error;
    }

    // A download-attributed anchor can bypass the controlling service worker.
    // Navigate a hidden frame instead; Content-Disposition supplies the name.
    sink.frame = document.createElement("iframe");
    sink.frame.hidden = true;
    sink.frame.src = `${import.meta.env.BASE_URL}__croc_download__/${id}`;
    document.body.append(sink.frame);
    return sink;
  }

  private send(
    message: Record<string, unknown>,
    transfer: Transferable[] = [],
    target?: ServiceWorker,
  ) {
    if (this.failure) throw this.failure;
    if (this.pending)
      throw new Error("Streaming download write is already pending");
    return new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending = undefined;
        this.failure = new Error(
          "The streaming browser download stopped responding",
        );
        reject(this.failure);
      }, 15_000);
      this.pending = { resolve, reject, timer };
      if (target) target.postMessage(message, transfer);
      else this.port.postMessage(message, transfer);
    });
  }

  async writeAt(position: number, bytes: Uint8Array) {
    if (this.closed) throw new Error("Destination file is already closed");
    if (position !== this.offset) {
      throw new Error("Streaming browser downloads require sequential chunks");
    }
    await this.hasher.update(bytes);
    const copy = Uint8Array.from(bytes);
    await this.send({ type: "chunk", bytes: copy.buffer }, [copy.buffer]);
    this.offset += bytes.byteLength;
  }

  async finalize() {
    if (this.closed) return;
    this.digest = await this.hasher.digest();
    await this.send({ type: "end" });
    this.closed = true;
    this.port.close();
    this.frame?.remove();
  }

  async hash(algorithm: "xxhash" | "sha256" = "sha256") {
    if (algorithm !== "sha256") {
      throw new Error("Streaming downloads support SHA-256 verification only");
    }
    if (!this.digest)
      throw new Error("Destination must be finalized before hashing");
    return this.digest;
  }

  async commit() {}

  async abort() {
    this.hasher.dispose();
    if (this.closed) return;
    this.closed = true;
    if (this.pending) {
      clearTimeout(this.pending.timer);
      this.pending.reject(new DOMException("Transfer cancelled", "AbortError"));
      this.pending = undefined;
    }
    // A failed/cancelled browser navigation may never acknowledge another
    // message. Cleanup must not prevent the transfer error from reaching UI.
    this.port.postMessage({ type: "abort" });
    this.port.close();
    this.frame?.remove();
  }
}

export class StreamingDownloadDestination implements ReceiveDestination {
  async createEmptyFolder() {}

  async openFile(file: OfferedFile, signal?: AbortSignal) {
    if (file.size === 0) return new DownloadDestination().openFile(file);
    return StreamingDownloadSink.create(file.name, file.size, signal);
  }
}

export class DownloadDestination implements ReceiveDestination {
  private usedNames = new Set<string>();

  async createEmptyFolder() {
    // Browser downloads cannot represent an empty folder.
  }

  async openFile(file: OfferedFile) {
    return new DownloadSink(this.uniqueName(file.name), (name, blob) =>
      this.download(name, blob),
    );
  }

  private uniqueName(name: string) {
    if (!this.usedNames.has(name)) {
      this.usedNames.add(name);
      return name;
    }
    const dot = name.lastIndexOf(".");
    const stem = dot > 0 ? name.slice(0, dot) : name;
    const extension = dot > 0 ? name.slice(dot) : "";
    let counter = 2;
    let candidate = `${stem}-${counter}${extension}`;
    while (this.usedNames.has(candidate)) {
      counter += 1;
      candidate = `${stem}-${counter}${extension}`;
    }
    this.usedNames.add(candidate);
    return candidate;
  }

  private download(name: string, blob: Blob) {
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = name;
    anchor.hidden = true;
    document.body.append(anchor);
    anchor.click();
    anchor.remove();
    window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
  }
}

export function supportsDirectoryDestination() {
  return typeof window.showDirectoryPicker === "function";
}

export function supportsStreamingDownloadDestination() {
  return (
    "serviceWorker" in navigator &&
    typeof MessageChannel !== "undefined" &&
    (window.isSecureContext ||
      window.location.hostname === "localhost" ||
      window.location.hostname === "127.0.0.1")
  );
}

async function isBraveBrowser() {
  const isBrave = navigator.brave?.isBrave;
  if (!isBrave) return navigator.userAgent.includes("Brave");
  try {
    return await isBrave.call(navigator.brave);
  } catch {
    return false;
  }
}

function blobDownloadDestination(offer: TransferOffer) {
  const largest = offer.files.reduce(
    (maximum, file) => Math.max(maximum, file.size),
    0,
  );
  if (largest > maxBlobDownloadBytes) {
    throw new Error(
      "This browser cannot stream a file this large. Receive it with the croc CLI instead.",
    );
  }
  return new DownloadDestination();
}

export async function chooseReceiveDestination(offer: TransferOffer) {
  if (!window.showDirectoryPicker) return new DownloadDestination();
  const directory = await window.showDirectoryPicker({ mode: "readwrite" });
  const collisions: string[] = [];
  for (const file of offer.files) {
    if (await fileExists(directory, file)) collisions.push(file.path);
  }
  if (
    collisions.length > 0 &&
    !window.confirm(
      `${collisions.length} file${collisions.length === 1 ? "" : "s"} already exist in that folder. Replace them?`,
    )
  ) {
    throw new DOMException("Destination selection cancelled", "AbortError");
  }
  return new DirectoryDestination(directory);
}

export async function chooseStoredReceiveDestination(offer: TransferOffer) {
  if (window.showDirectoryPicker) {
    return chooseReceiveDestination(offer);
  }
  // Brave currently rejects downloads backed by a service-worker-generated
  // streaming response even after the worker successfully intercepts them.
  // Use the bounded Blob path instead of producing a failed browser download.
  if (await isBraveBrowser()) return blobDownloadDestination(offer);
  if (supportsStreamingDownloadDestination()) {
    try {
      await streamingWorker();
      return new StreamingDownloadDestination();
    } catch {
      // Small transfers can still use the in-memory download path. The stored
      // claim is not created until after this destination has been selected.
    }
  }
  return blobDownloadDestination(offer);
}

export async function verifySink(
  sink: ReceiveSink,
  expected: Uint8Array,
  signal?: AbortSignal,
) {
  const actual = await sink.hash("xxhash", signal);
  if (signal?.aborted)
    throw new DOMException("Transfer cancelled", "AbortError");
  if (!bytesEqual(actual, expected)) {
    throw new Error(
      `The sender advertised xxhash ${hex(expected)}, but the received file hashes to ${hex(actual)}`,
    );
  }
}

export async function verifySinkSHA256(
  sink: ReceiveSink,
  expected: Uint8Array,
  signal?: AbortSignal,
) {
  const actual = await sink.hash("sha256", signal);
  if (!bytesEqual(actual, expected)) {
    throw new Error(
      `The stored file failed SHA-256 verification (${hex(actual)} != ${hex(expected)})`,
    );
  }
}
