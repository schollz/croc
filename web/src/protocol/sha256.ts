type Pending = {
  resolve(value: Uint8Array): void;
  reject(error: Error): void;
  timer: ReturnType<typeof setTimeout>;
};

/** One bounded-memory hash per worker; never transfers ownership of caller bytes. */
export class SHA256Hasher {
  private worker: Worker;
  private pending = new Map<number, Pending>();
  private nextID = 0;
  private failure?: Error;
  private abort = () =>
    this.dispose(new DOMException("Transfer cancelled", "AbortError"));

  constructor(
    private signal?: AbortSignal,
    worker?: Worker,
  ) {
    this.worker =
      worker ??
      new Worker(new URL("./sha256-worker.ts", import.meta.url), {
        type: "module",
        name: "croc-sha256",
      });
    this.worker.addEventListener("message", (event: MessageEvent) => {
      const { id, result, error } = event.data;
      const pending = this.pending.get(id);
      if (!pending) return;
      this.pending.delete(id);
      clearTimeout(pending.timer);
      if (error) pending.reject(new Error(error));
      else pending.resolve(result);
    });
    this.worker.addEventListener("error", (event) =>
      this.dispose(new Error(event.message || "SHA-256 worker failed")),
    );
    this.worker.addEventListener("messageerror", () =>
      this.dispose(new Error("SHA-256 worker response could not be read")),
    );
    signal?.addEventListener("abort", this.abort, { once: true });
    if (signal?.aborted) this.abort();
  }

  private call(method: string, bytes?: Uint8Array) {
    if (this.failure) return Promise.reject(this.failure);
    const id = ++this.nextID;
    return new Promise<Uint8Array>((resolve, reject) => {
      const timer = setTimeout(
        () => this.dispose(new Error("SHA-256 worker timed out")),
        30_000,
      );
      this.pending.set(id, { resolve, reject, timer });
      const owned = bytes?.slice();
      try {
        this.worker.postMessage(
          { id, method, bytes: owned },
          owned ? [owned.buffer] : [],
        );
      } catch (error) {
        this.dispose(error instanceof Error ? error : new Error(String(error)));
      }
    });
  }

  async update(bytes: Uint8Array) {
    await this.call("update", bytes);
  }

  async digest() {
    try {
      return await this.call("digest");
    } finally {
      this.dispose();
    }
  }

  dispose(error = new Error("SHA-256 context closed")) {
    if (this.failure) return;
    this.failure = error;
    this.signal?.removeEventListener("abort", this.abort);
    this.worker.terminate();
    for (const pending of this.pending.values()) {
      clearTimeout(pending.timer);
      pending.reject(error);
    }
    this.pending.clear();
  }
}

export async function hashSHA256Blob(blob: Blob, signal?: AbortSignal) {
  const hasher = new SHA256Hasher(signal);
  const reader = blob.stream().getReader();
  const abort = () => {
    void reader.cancel().catch(() => {});
  };
  signal?.addEventListener("abort", abort, { once: true });
  try {
    for (;;) {
      if (signal?.aborted)
        throw new DOMException("Transfer cancelled", "AbortError");
      const { done, value } = await reader.read();
      if (signal?.aborted)
        throw new DOMException("Transfer cancelled", "AbortError");
      if (done) break;
      await hasher.update(value);
    }
    return await hasher.digest();
  } finally {
    signal?.removeEventListener("abort", abort);
    await reader.cancel().catch(() => {});
    reader.releaseLock();
    hasher.dispose();
  }
}
