import {
  base64ToBytes,
  bytesToBase64,
  textDecoder,
  textEncoder,
} from "./bytes";
import type { CrocMessage } from "./types";
import type { CrocWasm } from "../wasm/client";

type WireMessage = {
  t: CrocMessage["t"];
  m?: string;
  b?: string;
  b2?: string;
  n?: number;
};

export async function encodeMessage(
  wasm: CrocWasm,
  message: CrocMessage,
  key?: Uint8Array,
) {
  const wire: WireMessage = { t: message.t };
  if (message.m) wire.m = message.m;
  if (message.b?.byteLength) wire.b = bytesToBase64(message.b);
  if (message.b2?.byteLength) wire.b2 = bytesToBase64(message.b2);
  if (message.n) wire.n = message.n;
  let bytes = await wasm.compress(textEncoder.encode(JSON.stringify(wire)));
  if (key) bytes = await wasm.encrypt(bytes, key);
  return bytes;
}

export async function decodeMessage(
  wasm: CrocWasm,
  payload: Uint8Array,
  key?: Uint8Array,
) {
  let bytes = payload;
  if (key) bytes = await wasm.decrypt(bytes, key);
  bytes = await wasm.decompress(bytes);
  const wire = JSON.parse(textDecoder.decode(bytes)) as WireMessage;
  if (!wire.t) throw new Error("Peer message did not include a type");
  return {
    t: wire.t,
    m: wire.m,
    b: wire.b ? base64ToBytes(wire.b) : undefined,
    b2: wire.b2 ? base64ToBytes(wire.b2) : undefined,
    n: wire.n,
  } satisfies CrocMessage;
}
