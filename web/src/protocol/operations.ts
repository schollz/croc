import { Pake } from "./pake";
import { XXH64 } from "./xxh64";
import { compress, decompress } from "./compression";
import {
  deriveKey,
  derivePeerKeys,
  confirmPeerKey,
  encryptAEAD,
  decryptAEAD,
  importCipher,
} from "./key";

// One instance belongs to one worker. Context handles never escape that owner.
export class ProtocolOperations {
  private next = 1;
  private pakes = new Map<number, Pake>();
  private ciphers = new Map<number, CryptoKey>();
  private hashes = new Map<number, XXH64>();
  pakeInit(password: Uint8Array, role: 0 | 1, curve: string) {
    return this.start(new Pake(password, role, curve));
  }
  pakeInitWithIdentities(
    password: Uint8Array,
    role: 0 | 1,
    curve: string,
    purpose: string,
    room: string,
  ) {
    return this.start(new Pake(password, role, curve, purpose, room));
  }
  private start(pake: Pake) {
    const handle = this.next++;
    this.pakes.set(handle, pake);
    return { handle, bytes: pake.bytes() };
  }
  async pakeUpdate(handle: number, bytes: Uint8Array) {
    const pake = this.get(this.pakes, handle);
    this.pakes.delete(handle);
    try {
      return await pake.update(bytes);
    } finally {
      pake.release();
    }
  }
  pakeRelease(handle: number) {
    this.pakes.get(handle)?.release();
    this.pakes.delete(handle);
  }
  deriveKey = deriveKey;
  derivePeerKeys = derivePeerKeys;
  confirmPeerKey = confirmPeerKey;
  async encrypt(bytes: Uint8Array, key: Uint8Array) {
    return encryptAEAD(bytes, await importCipher(key));
  }
  async decrypt(bytes: Uint8Array, key: Uint8Array) {
    return decryptAEAD(bytes, await importCipher(key));
  }
  compress = compress;
  decompress = decompress;
  async cipherInit(key: Uint8Array) {
    const cipher = await importCipher(key);
    const handle = this.next++;
    this.ciphers.set(handle, cipher);
    return handle;
  }
  cipherRelease(handle: number) {
    this.ciphers.delete(handle);
  }
  async encodeChunk(handle: number, bytes: Uint8Array, compressed: boolean) {
    const cipher = this.get(this.ciphers, handle);
    return encryptAEAD(compressed ? await compress(bytes) : bytes, cipher);
  }
  async decodeChunk(
    handle: number,
    bytes: Uint8Array,
    compressed: boolean,
    limit: number,
  ) {
    const plain = await decryptAEAD(bytes, this.get(this.ciphers, handle));
    if (compressed) return decompress(plain, limit);
    if (!Number.isSafeInteger(limit) || limit < 0 || plain.length > limit)
      throw new Error("Chunk exceeds output limit");
    return plain;
  }
  hashInit() {
    const handle = this.next++;
    this.hashes.set(handle, new XXH64());
    return handle;
  }
  hashUpdate(handle: number, bytes: Uint8Array) {
    this.get(this.hashes, handle).update(bytes);
  }
  hashFinal(handle: number) {
    const hash = this.get(this.hashes, handle);
    this.hashes.delete(handle);
    return hash.digest();
  }
  hashRelease(handle: number) {
    this.hashes.delete(handle);
  }
  private get<T>(map: Map<number, T>, handle: number) {
    const value = map.get(handle);
    if (!value) throw new Error("Unknown protocol context");
    return value;
  }
}
