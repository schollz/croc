import { normalizeFilePath, collisionKey } from "./metadata";

export const storedProtocol = "croc-store-v1";
export const storedChunkSize = 4 * 1024 * 1024;

export type StoredManifestFile = {
  n: string;
  s: number;
  m: string;
  h: string;
  fc: number;
  cc: number;
};
export type StoredManifest = { v: number; cs: number; f: StoredManifestFile[] };

function subtle() {
  if (!globalThis.crypto?.subtle) {
    throw new Error(
      "Stored transfers require Web Crypto in a secure browser (HTTPS or localhost)",
    );
  }
  return crypto.subtle;
}

const encoder = new TextEncoder();
const bytes = (value: Uint8Array) => Uint8Array.from(value).buffer;

export function generateStoredKey() {
  subtle();
  return crypto.getRandomValues(new Uint8Array(32));
}

function object(value: unknown, fields: string[]) {
  if (
    !value ||
    typeof value !== "object" ||
    Array.isArray(value) ||
    Object.keys(value).some((key) => !fields.includes(key))
  ) {
    throw new Error("Invalid stored-transfer manifest fields");
  }
  return value as Record<string, unknown>;
}

function integer(value: unknown, minimum = 0) {
  return (
    typeof value === "number" && Number.isSafeInteger(value) && value >= minimum
  );
}

function timestamp(value: unknown) {
  if (typeof value !== "string") return false;
  const parts =
    /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(?:\.\d+)?(?:Z|[+-](\d\d):(\d\d))$/.exec(
      value,
    );
  if (!parts) return false;
  const [year, month, day, hour, minute, second] = parts
    .slice(1, 7)
    .map(Number);
  const zoneHour = Number(parts[7] ?? 0),
    zoneMinute = Number(parts[8] ?? 0);
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0);
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return (
    month >= 1 &&
    month <= 12 &&
    day >= 1 &&
    day <= days[month - 1] &&
    hour <= 23 &&
    minute <= 59 &&
    second <= 59 &&
    Number(zoneHour) <= 23 &&
    Number(zoneMinute) <= 59
  );
}

export function validateStoredManifest(
  value: unknown,
  maxBytes: number,
  maxFiles = 100,
): StoredManifest {
  const manifest = object(value, ["v", "cs", "f"]);
  if (manifest.v !== 1 || manifest.cs !== storedChunkSize) {
    throw new Error("Unsupported stored-transfer version or chunk size");
  }
  if (
    !integer(maxBytes, 1) ||
    !Array.isArray(manifest.f) ||
    manifest.f.length < 1 ||
    manifest.f.length > Math.min(maxFiles, 100)
  ) {
    throw new Error("Invalid stored-transfer file count or byte limit");
  }
  let total = 0;
  let nextChunk = 0;
  const names = new Set<string>();
  const files = manifest.f.map((value) => {
    const file = object(value, ["n", "s", "m", "h", "fc", "cc"]);
    if (typeof file.n !== "string")
      throw new Error("Invalid stored-transfer filename");
    const name = normalizeFilePath(".", file.n).name;
    const key = collisionKey(name);
    if (names.has(key))
      throw new Error("Stored-transfer manifest has duplicate names");
    names.add(key);
    if (!integer(file.s) || (file.s as number) > maxBytes - total) {
      throw new Error("Stored transfer exceeds the configured byte limit");
    }
    if (
      typeof file.h !== "string" ||
      !/^[A-Za-z0-9_-]{43}$/.test(file.h) ||
      atob(file.h.replaceAll("-", "+").replaceAll("_", "/") + "=").length !== 32
    ) {
      throw new Error("Stored-transfer file has an invalid SHA-256 hash");
    }
    if (!timestamp(file.m)) {
      throw new Error("Stored-transfer file has an invalid modification time");
    }
    const count = Math.ceil((file.s as number) / storedChunkSize);
    if (
      !integer(file.fc) ||
      !integer(file.cc) ||
      file.fc !== nextChunk ||
      file.cc !== count
    ) {
      throw new Error("Stored-transfer file has an invalid chunk map");
    }
    total += file.s as number;
    nextChunk += count;
    return { ...file, n: name } as StoredManifestFile;
  });
  return { v: 1, cs: storedChunkSize, f: files };
}

/** A transfer-scoped native implementation of the Go storecrypto wire format. */
export class StoredCrypto {
  private master?: Promise<CryptoKey>;
  private keys = new Map<string, Promise<CryptoKey>>();
  private capability?: Promise<Uint8Array>;
  private closed = false;

  constructor(key: Uint8Array) {
    if (key.byteLength !== 32)
      throw new Error("Stored-transfer key must be 32 bytes");
    this.master = subtle().importKey("raw", bytes(key), "HKDF", false, [
      "deriveKey",
      "deriveBits",
    ]);
  }

  private parameters(label: string): HkdfParams {
    return {
      name: "HKDF",
      hash: "SHA-256",
      salt: new Uint8Array(),
      info: encoder.encode(`${storedProtocol}/${label}`),
    };
  }

  private base() {
    if (this.closed || !this.master)
      throw new Error("Stored-transfer crypto context is closed");
    return this.master;
  }

  private key(label: string) {
    const master = this.base();
    let key = this.keys.get(label);
    if (!key) {
      key = master.then((base) =>
        subtle().deriveKey(
          this.parameters(label),
          base,
          { name: "AES-GCM", length: 256 },
          false,
          ["encrypt", "decrypt"],
        ),
      );
      this.keys.set(label, key);
    }
    return key;
  }

  redeem() {
    const master = this.base();
    return (this.capability ??= master.then(
      async (base) =>
        new Uint8Array(
          await subtle().deriveBits(this.parameters("redeem"), base, 256),
        ),
    ));
  }

  private async seal(label: string, plaintext: Uint8Array, aad: Uint8Array) {
    const key = await this.key(label);
    const nonce = crypto.getRandomValues(new Uint8Array(12));
    const encrypted = new Uint8Array(
      await subtle().encrypt(
        {
          name: "AES-GCM",
          iv: nonce,
          additionalData: bytes(aad),
          tagLength: 128,
        },
        key,
        bytes(plaintext),
      ),
    );
    const output = new Uint8Array(nonce.length + encrypted.length);
    output.set(nonce);
    output.set(encrypted, nonce.length);
    return output;
  }

  private async open(label: string, ciphertext: Uint8Array, aad: Uint8Array) {
    if (ciphertext.length < 28)
      throw new Error("Stored-transfer ciphertext is too short");
    const key = await this.key(label);
    try {
      return new Uint8Array(
        await subtle().decrypt(
          {
            name: "AES-GCM",
            iv: bytes(ciphertext.subarray(0, 12)),
            additionalData: bytes(aad),
            tagLength: 128,
          },
          key,
          bytes(ciphertext.subarray(12)),
        ),
      );
    } catch {
      throw new Error("Stored-transfer authentication failed");
    }
  }

  sealManifest(id: string, json: Uint8Array) {
    validateStoredManifest(
      JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(json)),
      2 ** 40,
    );
    return this.seal(
      "manifest",
      json,
      encoder.encode(`${storedProtocol}\0${id}\0manifest`),
    );
  }

  async openManifest(
    id: string,
    ciphertext: Uint8Array,
    maxBytes: number,
    maxFiles = 100,
  ) {
    const plain = await this.open(
      "manifest",
      ciphertext,
      encoder.encode(`${storedProtocol}\0${id}\0manifest`),
    );
    return validateStoredManifest(
      JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(plain)),
      maxBytes,
      maxFiles,
    );
  }

  private chunkAAD(
    id: string,
    objectIndex: number,
    fileIndex: number,
    fileChunk: number,
    plainSize: number,
  ) {
    const values = [objectIndex, fileIndex, fileChunk, plainSize];
    if (
      values.some((value) => !integer(value) || value > 0xffffffff) ||
      plainSize < 1 ||
      plainSize > storedChunkSize
    ) {
      throw new Error("Stored-transfer chunk has an invalid context");
    }
    const prefix = encoder.encode(`${storedProtocol}\0${id}\0chunk\0`);
    const aad = new Uint8Array(prefix.length + 16);
    aad.set(prefix);
    const view = new DataView(aad.buffer);
    values.forEach((value, index) =>
      view.setUint32(prefix.length + index * 4, value, false),
    );
    return aad;
  }

  sealChunk(
    id: string,
    objectIndex: number,
    fileIndex: number,
    fileChunk: number,
    plainSize: number,
    plaintext: Uint8Array,
  ) {
    if (plaintext.length !== plainSize)
      throw new Error("Stored-transfer chunk has an invalid plaintext length");
    return this.seal(
      "data",
      plaintext,
      this.chunkAAD(id, objectIndex, fileIndex, fileChunk, plainSize),
    );
  }

  openChunk(
    id: string,
    objectIndex: number,
    fileIndex: number,
    fileChunk: number,
    plainSize: number,
    ciphertext: Uint8Array,
  ) {
    if (ciphertext.length !== plainSize + 28)
      throw new Error("Stored-transfer chunk has an invalid size");
    return this.open(
      "data",
      ciphertext,
      this.chunkAAD(id, objectIndex, fileIndex, fileChunk, plainSize),
    );
  }

  close() {
    this.closed = true;
    this.master = undefined;
    this.capability = undefined;
    this.keys.clear();
  }
}
