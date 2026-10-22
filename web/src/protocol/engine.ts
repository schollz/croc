import type { PakeStart, PakeFinish, PeerKeys } from "./engine-types";
import { codeComponents, sshCodeComponents, relayIndex } from "../codephrase";
export type {
  PakeStart,
  PakeFinish,
  PeerKeys,
  CodeComponents,
  SSHCodeComponents,
} from "./engine-types";

type Pending = {
  resolve(value: unknown): void;
  reject(reason: Error): void;
};

export class CrocProtocolEngine {
  private worker?: Worker;
  private nextID = 1;
  private pending = new Map<number, Pending>();
  private fatalError: Error | undefined;

  constructor(worker?: Worker) {
    if (worker) this.attach(worker);
  }

  private attach(worker: Worker) {
    this.worker = worker;
    this.worker.addEventListener("message", (event: MessageEvent) => {
      const { id, result, error } = event.data as {
        id: number;
        result?: unknown;
        error?: string;
      };
      if (id === 0) {
        this.fail(new Error(error ?? "Protocol worker failed"));
        return;
      }
      const pending = this.pending.get(id);
      if (!pending) return;
      this.pending.delete(id);
      if (error !== undefined)
        pending.reject(new Error(error || "Protocol operation failed"));
      else pending.resolve(result);
    });
    this.worker.addEventListener("messageerror", () =>
      this.fail(new Error("Protocol worker message failed")),
    );
    this.worker.addEventListener("error", (event) => {
      this.fail(new Error(event.message || "Protocol worker crashed"));
    });
  }

  private startWorker() {
    if (!this.worker)
      this.attach(
        new Worker(new URL("./engine-worker.ts", import.meta.url), {
          type: "module",
          name: "croc-protocol",
        }),
      );
    return this.worker!;
  }

  get closed() {
    return this.fatalError !== undefined;
  }

  close() {
    this.fail(new Error("Protocol worker closed"));
  }

  private fail(reason: unknown) {
    const error = reason instanceof Error ? reason : new Error(String(reason));
    this.fatalError = error;
    this.worker?.terminate();
    for (const pending of this.pending.values()) pending.reject(error);
    this.pending.clear();
  }

  private call<T>(
    method: string,
    args: unknown[] = [],
    transfer: Transferable[] = [],
  ) {
    if (this.fatalError) return Promise.reject(this.fatalError);
    const id = this.nextID++;
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, {
        resolve: resolve as (value: unknown) => void,
        reject,
      });
      try {
        this.startWorker().postMessage({ id, method, args }, transfer);
      } catch (error) {
        this.pending.delete(id);
        reject(error);
      }
    });
  }

  private ownContext<T>(
    operation: Promise<T>,
    release: (value: T) => Promise<void>,
    signal?: AbortSignal,
  ): Promise<T> {
    if (!signal) return operation;
    return new Promise((resolve, reject) => {
      const abort = () =>
        reject(new DOMException("Transfer cancelled", "AbortError"));
      signal.addEventListener("abort", abort, { once: true });
      if (signal.aborted) abort();
      void operation
        .then((value) => {
          if (signal.aborted) void release(value).catch(() => undefined);
          else resolve(value);
        }, reject)
        .finally(() => signal.removeEventListener("abort", abort));
    });
  }

  pakeInit(
    password: Uint8Array,
    role: 0 | 1,
    curve: string,
    signal?: AbortSignal,
  ) {
    return this.ownContext(
      this.call<PakeStart>("pakeInit", [password, role, curve]),
      (value) => this.pakeRelease(value.handle),
      signal,
    );
  }

  pakeInitWithIdentities(
    password: Uint8Array,
    role: 0 | 1,
    curve: string,
    purpose: string,
    room: string,
    signal?: AbortSignal,
  ) {
    return this.ownContext(
      this.call<PakeStart>("pakeInitWithIdentities", [
        password,
        role,
        curve,
        purpose,
        room,
      ]),
      (value) => this.pakeRelease(value.handle),
      signal,
    );
  }

  pakeUpdate(handle: number, peerBytes: Uint8Array) {
    return this.call<PakeFinish>("pakeUpdate", [handle, peerBytes]);
  }

  deriveKey(passphrase: Uint8Array, salt: Uint8Array) {
    return this.call<Uint8Array>("deriveKey", [passphrase, salt]);
  }

  derivePeerKeys(
    sharedKey: Uint8Array,
    salt: Uint8Array,
    purpose: string,
    room: string,
    curve: string,
    initiator: Uint8Array,
    responder: Uint8Array,
  ) {
    return this.call<PeerKeys>("derivePeerKeys", [
      sharedKey,
      salt,
      purpose,
      room,
      curve,
      initiator,
      responder,
    ]);
  }

  confirmPeerKey(expected: Uint8Array, received: Uint8Array) {
    return this.call<boolean>("confirmPeerKey", [expected, received]);
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

  decompress(input: Uint8Array, maxOutputSize: number) {
    return this.call<Uint8Array>("decompress", [input, maxOutputSize]);
  }

  cipherInit(key: Uint8Array) {
    return this.call<number>("cipherInit", [key]);
  }

  cipherRelease(handle: number) {
    return this.call<void>("cipherRelease", [handle]);
  }

  encodeChunk(handle: number, input: Uint8Array, compressed: boolean) {
    const owned = input.slice();
    return this.call<Uint8Array>(
      "encodeChunk",
      [handle, owned, compressed],
      [owned.buffer],
    );
  }

  decodeChunk(
    handle: number,
    input: Uint8Array,
    compressed: boolean,
    maxOutputSize: number,
  ) {
    // Callers may retain even full-buffer views. Transfer only our own copy.
    const owned = input.slice();
    return this.call<Uint8Array>(
      "decodeChunk",
      [handle, owned, compressed, maxOutputSize],
      [owned.buffer],
    );
  }

  hashInit(signal?: AbortSignal) {
    return this.ownContext(
      this.call<number>("hashInit"),
      (handle) => this.hashRelease(handle),
      signal,
    );
  }

  hashUpdate(handle: number, input: Uint8Array) {
    const owned = input.slice();
    return this.call<void>("hashUpdate", [handle, owned], [owned.buffer]);
  }

  hashFinal(handle: number) {
    return this.call<Uint8Array>("hashFinal", [handle]);
  }

  codeComponents = codeComponents;

  sshCodeComponents = sshCodeComponents;

  relayIndex = relayIndex;

  pakeRelease(handle: number) {
    return this.call<void>("pakeRelease", [handle]);
  }
  hashRelease(handle: number) {
    return this.call<void>("hashRelease", [handle]);
  }
}

let shared: CrocProtocolEngine | undefined;

export function protocolEngine() {
  if (!shared || shared.closed) shared = new CrocProtocolEngine();
  return shared;
}
