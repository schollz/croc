export interface PakeStart {
  handle: number;
  bytes: Uint8Array;
}

export interface PakeFinish {
  bytes: Uint8Array;
  key: Uint8Array;
}

type Pending = {
  resolve(value: unknown): void;
  reject(reason: Error): void;
};

export class CrocWasm {
  private worker: Worker;
  private nextID = 1;
  private pending = new Map<number, Pending>();

  constructor(worker?: Worker) {
    this.worker =
      worker ??
      new Worker(`${import.meta.env.BASE_URL}croc-worker.js`, {
        name: "croc-protocol",
      });
    this.worker.addEventListener("message", (event: MessageEvent) => {
      const { id, result, error } = event.data as {
        id: number;
        result?: unknown;
        error?: string;
      };
      if (id === 0) {
        for (const pending of this.pending.values()) {
          pending.reject(new Error(error ?? "WASM worker failed"));
        }
        this.pending.clear();
        return;
      }
      const pending = this.pending.get(id);
      if (!pending) return;
      this.pending.delete(id);
      if (error) pending.reject(new Error(error));
      else pending.resolve(result);
    });
    this.worker.addEventListener("error", (event) => {
      const error = new Error(event.message || "WASM worker crashed");
      for (const pending of this.pending.values()) pending.reject(error);
      this.pending.clear();
    });
  }

  close() {
    this.worker.terminate();
    const error = new Error("WASM worker closed");
    for (const pending of this.pending.values()) pending.reject(error);
    this.pending.clear();
  }

  private call<T>(method: string, args: unknown[] = [], transfer: Transferable[] = []) {
    const id = this.nextID++;
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, { resolve: resolve as (value: unknown) => void, reject });
      this.worker.postMessage({ id, method, args }, transfer);
    });
  }

  pakeInit(password: Uint8Array, role: 0 | 1, curve: string) {
    return this.call<PakeStart>("pakeInit", [password, role, curve]);
  }

  pakeUpdate(handle: number, peerBytes: Uint8Array) {
    return this.call<PakeFinish>("pakeUpdate", [handle, peerBytes]);
  }

  deriveKey(passphrase: Uint8Array, salt: Uint8Array) {
    return this.call<Uint8Array>("deriveKey", [passphrase, salt]);
  }

  encrypt(plaintext: Uint8Array, key: Uint8Array) {
    return this.call<Uint8Array>("encrypt", [plaintext, key]);
  }

  decrypt(ciphertext: Uint8Array, key: Uint8Array) {
    return this.call<Uint8Array>("decrypt", [ciphertext, key]);
  }

  compress(input: Uint8Array) {
    return this.call<Uint8Array>("compress", [input]);
  }

  decompress(input: Uint8Array) {
    return this.call<Uint8Array>("decompress", [input]);
  }

  hashInit() {
    return this.call<number>("hashInit");
  }

  hashUpdate(handle: number, input: Uint8Array) {
    return this.call<void>("hashUpdate", [handle, input]);
  }

  hashFinal(handle: number) {
    return this.call<Uint8Array>("hashFinal", [handle]);
  }

  randomCode() {
    return this.call<string>("randomCode");
  }
}

let shared: CrocWasm | undefined;

export function wasm() {
  shared ??= new CrocWasm();
  return shared;
}
