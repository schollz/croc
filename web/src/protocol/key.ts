import { bytesEqual, concatBytes, randomBytes, textEncoder } from "./bytes";

export function frame(...fields: Uint8Array[]) {
  return concatBytes(
    ...fields.flatMap((field) => {
      const length = new Uint8Array(8);
      new DataView(length.buffer).setBigUint64(0, BigInt(field.length), true);
      return [length, field];
    }),
  );
}
const version = Uint8Array.of(2, 0, 0, 0, 0, 0, 0, 0);
export function identities(purpose: string, room: string) {
  if (!["peer-transfer", "local-ip-probe", "peer-ssh"].includes(purpose))
    throw new Error("Unsupported PAKE purpose");
  if (!room) throw new Error("PAKE room is required");
  const fields = [
    textEncoder.encode("croc/spake2/participant/v2"),
    version,
    textEncoder.encode(purpose),
    textEncoder.encode(room),
  ];
  return [
    frame(...fields, textEncoder.encode("receiver")),
    frame(...fields, textEncoder.encode("sender")),
  ];
}
export async function sha256(bytes: Uint8Array) {
  return new Uint8Array(await crypto.subtle.digest("SHA-256", bytes.slice()));
}
export async function deriveKey(password: Uint8Array, salt: Uint8Array) {
  if (!password.length) throw new Error("Passphrase is required");
  const key = await crypto.subtle.importKey(
    "raw",
    password.slice(),
    "PBKDF2",
    false,
    ["deriveBits"],
  );
  return new Uint8Array(
    await crypto.subtle.deriveBits(
      { name: "PBKDF2", hash: "SHA-256", iterations: 100, salt: salt.slice() },
      key,
      256,
    ),
  );
}
export async function derivePeerKeys(
  sharedKey: Uint8Array,
  salt: Uint8Array,
  purpose: string,
  room: string,
  curve: string,
  initiator: Uint8Array,
  responder: Uint8Array,
) {
  if (!sharedKey.length || !curve || !initiator.length || !responder.length)
    throw new Error("Incomplete PAKE key context");
  if (salt.length !== 32) throw new Error("PAKE salt must be 32 bytes");
  const [idA, idB] = identities(purpose, room);
  const transcript = frame(
    textEncoder.encode("croc/spake2/channel/v2"),
    version,
    textEncoder.encode(purpose),
    textEncoder.encode(room),
    textEncoder.encode(curve),
    idA,
    idB,
    initiator,
    responder,
    salt,
  );
  // Some Web Crypto providers cap HKDF info at 1024 bytes; croc's exact
  // transcript exceeds that. RFC 5869 extract/expand via Web Crypto HMAC
  // preserves Go's HKDF output without truncating or hashing the transcript.
  const hmac = async (keyBytes: Uint8Array, data: Uint8Array) => {
    const key = await crypto.subtle.importKey(
      "raw",
      keyBytes.slice(),
      { name: "HMAC", hash: "SHA-256" },
      false,
      ["sign"],
    );
    return new Uint8Array(await crypto.subtle.sign("HMAC", key, data.slice()));
  };
  const prk = await hmac(salt, sharedKey);
  const blocks: Uint8Array[] = [];
  let previous = new Uint8Array();
  for (let i = 1; i <= 3; i++) {
    previous = await hmac(
      prk,
      concatBytes(previous, transcript, Uint8Array.of(i)),
    );
    blocks.push(previous);
  }
  const material = concatBytes(...blocks);
  const confirm = async (bytes: Uint8Array) => {
    const key = await crypto.subtle.importKey(
      "raw",
      bytes.slice(),
      { name: "HMAC", hash: "SHA-256" },
      false,
      ["sign"],
    );
    return new Uint8Array(await crypto.subtle.sign("HMAC", key, transcript));
  };
  return {
    key: material.slice(0, 32),
    confirmationA: await confirm(material.subarray(32, 64)),
    confirmationB: await confirm(material.subarray(64)),
  };
}
export function confirmPeerKey(expected: Uint8Array, received: Uint8Array) {
  return expected.length === 32 && bytesEqual(expected, received);
}
export function importCipher(key: Uint8Array) {
  return crypto.subtle.importKey("raw", key.slice(), "AES-GCM", false, [
    "encrypt",
    "decrypt",
  ]);
}
export async function encryptAEAD(input: Uint8Array, key: CryptoKey) {
  const iv = randomBytes(12);
  return concatBytes(
    iv,
    new Uint8Array(
      await crypto.subtle.encrypt({ name: "AES-GCM", iv }, key, input.slice()),
    ),
  );
}
export async function decryptAEAD(input: Uint8Array, key: CryptoKey) {
  if (input.length < 28) throw new Error("Truncated AES-GCM payload");
  return new Uint8Array(
    await crypto.subtle.decrypt(
      { name: "AES-GCM", iv: input.slice(0, 12) },
      key,
      input.slice(12),
    ),
  );
}
