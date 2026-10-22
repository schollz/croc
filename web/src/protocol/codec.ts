import {
  base64ToBytes,
  bytesToBase64,
  textDecoder,
  textEncoder,
} from "./bytes";
import { MAX_FRAME_SIZE } from "./framing";
import type { CrocMessage } from "./types";
import type { CrocProtocolEngine } from "./engine";

type WireMessage = {
  t: CrocMessage["t"];
  v?: number;
  m?: string;
  b?: string;
  b2?: string;
  n?: number;
  f?: string[];
};

export async function encodeMessage(
  protocolEngine: CrocProtocolEngine,
  message: CrocMessage,
  key?: Uint8Array,
) {
  const wire: WireMessage = { t: message.t };
  if (message.v) wire.v = message.v;
  if (message.m) wire.m = message.m;
  if (message.b?.byteLength) wire.b = bytesToBase64(message.b);
  if (message.b2?.byteLength) wire.b2 = bytesToBase64(message.b2);
  if (message.n) wire.n = message.n;
  if (message.f?.length) wire.f = message.f;
  let bytes = await protocolEngine.compress(textEncoder.encode(JSON.stringify(wire)));
  if (key) bytes = await protocolEngine.encrypt(bytes, key);
  return bytes;
}

export async function decodeMessage(
  protocolEngine: CrocProtocolEngine,
  payload: Uint8Array,
  key?: Uint8Array,
  maxOutputSize = MAX_FRAME_SIZE,
) {
  let bytes = payload;
  if (key) bytes = await protocolEngine.decrypt(bytes, key);
  bytes = await protocolEngine.decompress(bytes, maxOutputSize);
  const wire = JSON.parse(textDecoder.decode(bytes)) as WireMessage;
  if (!wire.t) throw new Error("Peer message did not include a type");
  return {
    t: wire.t,
    v: wire.v,
    m: wire.m,
    b: wire.b ? base64ToBytes(wire.b) : undefined,
    b2: wire.b2 ? base64ToBytes(wire.b2) : undefined,
    n: wire.n,
    f: wire.f,
  } satisfies CrocMessage;
}
