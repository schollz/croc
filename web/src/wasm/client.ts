export interface PakeStart {
  handle: number;
  bytes: Uint8Array;
}

export interface PakeFinish {
  bytes: Uint8Array;
  key: Uint8Array;
}

export interface PeerKeys {
  key: Uint8Array;
  confirmationA: Uint8Array;
  confirmationB: Uint8Array;
}

export interface CodeComponents {
  room: string;
  passphrase: string;
  format: "legacy" | "three-word" | "four-word";
}

type Pending = {
  resolve(value: unknown): void;
  reject(reason: Error): void;
};

async function compileWasmModule() {
  const response = await fetch(`${import.meta.env.BASE_URL}croc.wasm`);
  if (!response.ok) {
    throw new Error(`Could not load croc.wasm (${response.status})`);
  }

  const contentType = response.headers.get("content-type")?.split(";", 1)[0];
  if (
    typeof WebAssembly.compileStreaming === "function" &&
    contentType === "application/wasm"
  ) {
    return WebAssembly.compileStreaming(response);
  }
  return WebAssembly.compile(await response.arrayBuffer());
}

export class CrocWasm {
  private worker: Worker;
  private nextID = 1;
  private pending = new Map<number, Pending>();
  private fatalError: Error | undefined;

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
        this.fail(new Error(error ?? "WASM worker failed"));
        return;
      }
      const pending = this.pending.get(id);
      if (!pending) return;
      this.pending.delete(id);
      if (error) pending.reject(new Error(error));
      else pending.resolve(result);
    });
    this.worker.addEventListener("error", (event) => {
      this.fail(new Error(event.message || "WASM worker crashed"));
    });
    void compileWasmModule()
      .then((module) => {
        if (!this.fatalError) {
          this.worker.postMessage({ type: "initialize", module });
        }
      })
      .catch((error) => this.fail(error));
  }

  close() {
    this.worker.terminate();
    this.fail(new Error("WASM worker closed"));
  }

  private fail(reason: unknown) {
    const error = reason instanceof Error ? reason : new Error(String(reason));
    this.fatalError = error;
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
      this.worker.postMessage({ id, method, args }, transfer);
    });
  }

  pakeInit(password: Uint8Array, role: 0 | 1, curve: string) {
    return this.call<PakeStart>("pakeInit", [password, role, curve]);
  }

  pakeInitWithIdentities(
    password: Uint8Array,
    role: 0 | 1,
    curve: string,
    purpose: string,
    room: string,
  ) {
    return this.call<PakeStart>("pakeInitWithIdentities", [
      password,
      role,
      curve,
      purpose,
      room,
    ]);
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
    return this.call<Uint8Array>(
      "encodeChunk",
      [handle, input, compressed],
      [input.buffer],
    );
  }

  decodeChunk(
    handle: number,
    input: Uint8Array,
    compressed: boolean,
    maxOutputSize: number,
  ) {
    // Framed WebSocket messages are views into a decoder buffer that may also
    // contain later frames. Transfer only an owned buffer so decoding one
    // chunk cannot detach the bytes backing the next chunk.
    const owned =
      input.byteOffset === 0 && input.byteLength === input.buffer.byteLength
        ? input
        : input.slice();
    return this.call<Uint8Array>(
      "decodeChunk",
      [handle, owned, compressed, maxOutputSize],
      [owned.buffer],
    );
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

  codeComponents(secret: string) {
    return this.call<CodeComponents>("codeComponents", [secret]);
  }

  relayIndex(secret: string, relayCount: number) {
    return this.call<number>("relayIndex", [secret, relayCount]);
  }

  sha256Init() {
    return this.call<number>("sha256Init");
  }

  sha256Update(handle: number, input: Uint8Array) {
    return this.call<void>("sha256Update", [handle, input]);
  }

  sha256Final(handle: number) {
    return this.call<Uint8Array>("sha256Final", [handle]);
  }

  storeGenerateKey() {
    return this.call<Uint8Array>("storeGenerateKey");
  }

  storeRedeemCapability(key: Uint8Array) {
    return this.call<Uint8Array>("storeRedeemCapability", [key]);
  }

  storeSealManifest(key: Uint8Array, id: string, json: Uint8Array) {
    return this.call<Uint8Array>("storeSealManifest", [key, id, json]);
  }

  storeOpenManifest(
    key: Uint8Array,
    id: string,
    ciphertext: Uint8Array,
    maxBytes: number,
  ) {
    return this.call<Uint8Array>("storeOpenManifest", [
      key,
      id,
      ciphertext,
      maxBytes,
    ]);
  }

  storeSealChunk(
    key: Uint8Array,
    id: string,
    objectIndex: number,
    fileIndex: number,
    fileChunk: number,
    plainSize: number,
    plaintext: Uint8Array,
  ) {
    return this.call<Uint8Array>("storeSealChunk", [
      key,
      id,
      objectIndex,
      fileIndex,
      fileChunk,
      plainSize,
      plaintext,
    ]);
  }

  storeOpenChunk(
    key: Uint8Array,
    id: string,
    objectIndex: number,
    fileIndex: number,
    fileChunk: number,
    plainSize: number,
    ciphertext: Uint8Array,
  ) {
    return this.call<Uint8Array>("storeOpenChunk", [
      key,
      id,
      objectIndex,
      fileIndex,
      fileChunk,
      plainSize,
      ciphertext,
    ]);
  }
}

let shared: CrocWasm | undefined;

export function preloadWasm() {
  wasm();
}

export function wasm() {
  shared ??= new CrocWasm();
  return shared;
}
