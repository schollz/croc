import { bytesEqual } from "./bytes";
import type {
  OfferedFile,
  ReceiveDestination,
  ReceiveSink,
  TransferOffer,
} from "./types";
import { wasm } from "../wasm/client";

declare global {
  interface Window {
    showDirectoryPicker?: (options?: {
      mode?: "read" | "readwrite";
    }) => Promise<FileSystemDirectoryHandle>;
  }
}

async function hashBlob(blob: Blob) {
  const engine = wasm();
  const handle = await engine.hashInit();
  const reader = blob.stream().getReader();
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      await engine.hashUpdate(handle, value);
    }
    return await engine.hashFinal(handle);
  } finally {
    reader.releaseLock();
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
    if (error instanceof DOMException && error.name === "NotFoundError") return false;
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

  async hash() {
    if (!this.closed) throw new Error("Destination must be closed before hashing");
    return hashBlob(await this.handle.getFile());
  }

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

  async createEmptyFile(file: OfferedFile) {
    const sink = await this.openFile(file);
    await sink.finalize();
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
  private chunks = new Map<number, Uint8Array>();
  private blob?: Blob;

  constructor(
    private name: string,
    private onDownload: (name: string, blob: Blob) => void,
  ) {}

  async writeAt(position: number, bytes: Uint8Array) {
    this.chunks.set(position, bytes.slice());
  }

  async finalize() {
    const ordered = [...this.chunks.entries()]
      .sort(([left], [right]) => left - right)
      .map(([, bytes]) => Uint8Array.from(bytes).buffer);
    this.blob = new Blob(ordered, { type: "application/octet-stream" });
    this.chunks.clear();
    this.onDownload(this.name, this.blob);
  }

  async hash() {
    if (!this.blob) throw new Error("Destination must be finalized before hashing");
    return hashBlob(this.blob);
  }

  async abort() {
    this.chunks.clear();
    this.blob = undefined;
  }
}

export class DownloadDestination implements ReceiveDestination {
  private usedNames = new Set<string>();

  async createEmptyFolder() {
    // Browser downloads cannot represent an empty folder.
  }

  async createEmptyFile(file: OfferedFile) {
    this.download(this.uniqueName(file.name), new Blob());
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

export async function verifySink(sink: ReceiveSink, expected: Uint8Array) {
  const actual = await sink.hash();
  if (!bytesEqual(actual, expected)) {
    throw new Error("The received file did not match its croc hash");
  }
}
