import { wasm } from "../wasm/client";

export type FileHashAlgorithm = "xxhash" | "sha256";
export type FileHashProvider = (file: File) => Promise<Uint8Array>;

type HashJob = {
  controller: AbortController;
  promise: Promise<Uint8Array>;
};

type HashFile = (
  file: File,
  algorithm: FileHashAlgorithm,
  signal?: AbortSignal,
) => Promise<Uint8Array>;

function abortError() {
  return new DOMException("Transfer cancelled", "AbortError");
}

function checkAbort(signal?: AbortSignal) {
  if (signal?.aborted) throw abortError();
}

export async function hashFileContents(
  file: File,
  algorithm: FileHashAlgorithm,
  signal?: AbortSignal,
) {
  checkAbort(signal);
  const engine = wasm();
  const handle =
    algorithm === "sha256" ? await engine.sha256Init() : await engine.hashInit();
  const reader = file.stream().getReader();
  let finalized = false;
  try {
    for (;;) {
      checkAbort(signal);
      const { done, value } = await reader.read();
      checkAbort(signal);
      if (done) break;
      if (algorithm === "sha256") await engine.sha256Update(handle, value);
      else await engine.hashUpdate(handle, value);
    }
    finalized = true;
    return algorithm === "sha256"
      ? await engine.sha256Final(handle)
      : await engine.hashFinal(handle);
  } finally {
    reader.releaseLock();
    if (!finalized) {
      const cleanup =
        algorithm === "sha256"
          ? engine.sha256Final(handle)
          : engine.hashFinal(handle);
      await cleanup.catch(() => undefined);
    }
  }
}

export function waitForHash(
  hash: Promise<Uint8Array>,
  signal?: AbortSignal,
) {
  checkAbort(signal);
  if (!signal) return hash;
  return new Promise<Uint8Array>((resolve, reject) => {
    const onAbort = () => reject(abortError());
    signal.addEventListener("abort", onAbort, { once: true });
    void hash.then(resolve, reject).finally(() => {
      signal.removeEventListener("abort", onAbort);
    });
  });
}

export class FileHashCache {
  private jobs = new Map<File, Map<FileHashAlgorithm, HashJob>>();
  private tails = new Map<FileHashAlgorithm, Promise<void>>();

  constructor(private hashFile: HashFile = hashFileContents) {}

  hash(file: File, algorithm: FileHashAlgorithm) {
    let algorithms = this.jobs.get(file);
    if (!algorithms) {
      algorithms = new Map();
      this.jobs.set(file, algorithms);
    }
    const existing = algorithms.get(algorithm);
    if (existing) return existing.promise;

    const controller = new AbortController();
    const previous = this.tails.get(algorithm) ?? Promise.resolve();
    const promise = previous.then(() => {
      checkAbort(controller.signal);
      return this.hashFile(file, algorithm, controller.signal);
    });
    this.tails.set(algorithm, promise.then(() => undefined, () => undefined));
    const job = { controller, promise };
    algorithms.set(algorithm, job);
    void promise.catch(() => {
      const current = this.jobs.get(file);
      if (current?.get(algorithm) !== job) return;
      current.delete(algorithm);
      if (current.size === 0) this.jobs.delete(file);
    });
    return promise;
  }

  prime(files: File[], algorithm: FileHashAlgorithm) {
    for (const file of files) this.hash(file, algorithm);
  }

  retain(files: File[]) {
    const retained = new Set(files);
    for (const [file, algorithms] of this.jobs) {
      if (retained.has(file)) continue;
      for (const job of algorithms.values()) job.controller.abort();
      this.jobs.delete(file);
    }
  }

  clear() {
    this.retain([]);
  }
}
