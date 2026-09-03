import { decodeMessage, encodeMessage } from "./codec";
import {
  bytesEqual,
  errorMessage,
  randomBytes,
  textDecoder,
  textEncoder,
} from "./bytes";
import { CrocSocket } from "./transport";
import type { CrocMessage, TransferSettings } from "./types";
import { wasm } from "../wasm/client";

const CONTROL_PORT = "9009";
const RELAY_PING = textEncoder.encode("ping");
const RELAY_PONG = textEncoder.encode("pong");
const WEAK_RELAY_KEY = new Uint8Array([1, 2, 3]);

export type RelayConnection = {
  socket: CrocSocket;
  banner: string;
  externalIP: string;
};

export function controlPort(address: string) {
  try {
    const parsed = new URL(
      address.includes("://") ? address : `tcp://${address}`,
    );
    return parsed.port || CONTROL_PORT;
  } catch {
    return CONTROL_PORT;
  }
}

export function relayAddress(settings: TransferSettings, relayIndex: number) {
  const address = settings.relayAddresses[relayIndex];
  if (!address) throw new Error(`Relay index ${relayIndex} is not configured`);
  return address;
}

export class RelayConnectionError extends Error {
  constructor(message: string, cause: unknown) {
    super(message, { cause });
    this.name = "RelayConnectionError";
  }
}

export function isRelayConnectionError(error: unknown) {
  return error instanceof RelayConnectionError;
}

export async function connectRelay(
  settings: TransferSettings,
  room: string,
  port: string,
  relayIndex: number,
  signal?: AbortSignal,
) {
  const engine = wasm();
  let socket: CrocSocket | undefined;
  try {
    socket = await CrocSocket.connect(
      settings.gatewayURL,
      relayIndex,
      port,
      signal,
    );
    const pake = await engine.pakeInit(WEAK_RELAY_KEY, 0, "siec");
    await socket.send(pake.bytes);
    const peer = await socket.receive();
    const finished = await engine.pakeUpdate(pake.handle, peer);
    const salt = randomBytes(8);
    const key = await engine.deriveKey(finished.key, salt);
    await socket.send(salt);
    await socket.send(
      await engine.encrypt(textEncoder.encode(settings.relayPassword), key),
    );
    const response = textDecoder.decode(
      await engine.decrypt(await socket.receive(), key),
    );
    const separator = response.indexOf("|||");
    if (separator < 0) {
      throw new Error(`Relay rejected the connection: ${response}`);
    }
    const banner = response.slice(0, separator);
    const externalIP = response.slice(separator + 3);
    await socket.send(await engine.encrypt(textEncoder.encode(room), key));
    const confirmation = textDecoder.decode(
      await engine.decrypt(await socket.receive(), key),
    );
    if (confirmation !== "ok") {
      throw new Error(`Relay could not open the room: ${confirmation}`);
    }
    return { socket, banner, externalIP } satisfies RelayConnection;
  } catch (error) {
    socket?.close();
    if (signal?.aborted) throw error;
    if (error instanceof RelayConnectionError) throw error;
    throw new RelayConnectionError(
      `Could not establish the croc relay connection: ${errorMessage(error)}`,
      error,
    );
  }
}

export async function measureRelayLatency(
  settings: TransferSettings,
  relayIndex: number,
  timeoutMs = 1_000,
  signal?: AbortSignal,
) {
  const controller = new AbortController();
  const abort = () => controller.abort();
  signal?.addEventListener("abort", abort, { once: true });
  const timer = window.setTimeout(abort, timeoutMs);
  const started = performance.now();
  let socket: CrocSocket | undefined;
  try {
    const address = relayAddress(settings, relayIndex);
    socket = await CrocSocket.connect(
      settings.gatewayURL,
      relayIndex,
      controlPort(address),
      controller.signal,
    );
    await socket.send(RELAY_PING);
    const response = await socket.receive();
    if (!bytesEqual(response, RELAY_PONG)) {
      throw new Error("Relay did not return pong");
    }
    return performance.now() - started;
  } finally {
    window.clearTimeout(timer);
    signal?.removeEventListener("abort", abort);
    socket?.close();
  }
}

export async function sendControl(
  socket: CrocSocket,
  message: CrocMessage,
  key?: Uint8Array,
) {
  await socket.send(await encodeMessage(wasm(), message, key));
}

export async function receiveControl(
  socket: CrocSocket,
  key?: Uint8Array,
  maxSize?: number,
) {
  return decodeMessage(wasm(), await socket.receive(), key, maxSize);
}
