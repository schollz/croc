import { decodeMessage, encodeMessage } from "./codec";
import {
  base64ToBytes,
  bytesEqual,
  bytesToBase64,
  errorMessage,
  randomBytes,
  textDecoder,
  textEncoder,
} from "./bytes";
import { CrocSocket } from "./transport";
import { normalizeOutgoingFileName, validateSenderInfo } from "./metadata";
import { verifySink } from "./storage";
import { hashFileContents, waitForHash, type FileHashProvider } from "./hash";
import type {
  CrocMessage,
  FileProgress,
  OfferedFile,
  PreparedFile,
  ReceiveCallbacks,
  ReceiveSink,
  RemoteFileRequestWire,
  SenderInfoWire,
  TransferCallbacks,
  TransferSettings,
  WireFileInfo,
} from "./types";
import { maxTextTransferBytes } from "./types";
import { wasm } from "../wasm/client";

const CONTROL_PORT = "9009";
const CHUNK_SIZE = 32 * 1024;
const MAX_DECOMPRESSED_CHUNK_SIZE = CHUNK_SIZE + 8;
const PER_FILE_COMPRESSION_FEATURE = "per-file-compression-v1";
const HANDSHAKE = textEncoder.encode("handshake");
const RELAY_PING = textEncoder.encode("ping");
const RELAY_PONG = textEncoder.encode("pong");
const IP_REQUEST = textEncoder.encode("ips?");
const WEAK_RELAY_KEY = new Uint8Array([1, 2, 3]);
const PAKE_PROTOCOL_VERSION = 2;
const PAKE_SALT_SIZE = 32;
const PAKE_PURPOSE_TRANSFER = "peer-transfer";
const PAKE_PURPOSE_LOCAL_PROBE = "local-ip-probe";

type RelayConnection = {
  socket: CrocSocket;
  banner: string;
  externalIP: string;
};

function abortError() {
  return new DOMException("Transfer cancelled", "AbortError");
}

function checkAbort(signal?: AbortSignal) {
  if (signal?.aborted) throw abortError();
}

function requirePakeVersion(version: number | undefined) {
  if (version !== PAKE_PROTOCOL_VERSION) {
    throw new Error(
      `Peer uses unsupported PAKE protocol version ${version ?? 0}; upgrade both croc clients`,
    );
  }
}

function validateSecret(secret: string) {
  if (secret.length < 6) throw new Error("Code must be at least 6 characters");
  if (!/^[\x20-\x7e]+$/.test(secret)) {
    throw new Error("Custom codes must use printable ASCII characters");
  }
}

function controlPort(relayAddress: string) {
  try {
    const parsed = new URL(
      relayAddress.includes("://") ? relayAddress : `tcp://${relayAddress}`,
    );
    return parsed.port || CONTROL_PORT;
  } catch {
    return CONTROL_PORT;
  }
}

function relayAddress(settings: TransferSettings, relayIndex: number) {
  const address = settings.relayAddresses[relayIndex];
  if (!address) throw new Error(`Relay index ${relayIndex} is not configured`);
  return address;
}

function dataPorts(banner: string) {
  const ports = banner
    .split(",")
    .map((port) => port.trim())
    .filter((port) => /^\d{1,5}$/.test(port));
  if (ports.length === 0)
    throw new Error(`Relay returned an invalid port list: ${banner}`);
  return ports;
}

function machineID() {
  const key = "croc-web-machine-id";
  try {
    const existing = localStorage.getItem(key);
    if (existing) return existing;
    const created = `web-${crypto.randomUUID()}`;
    localStorage.setItem(key, created);
    return created;
  } catch {
    return `web-${crypto.randomUUID()}`;
  }
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

async function connectRelay(
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
    if (separator < 0)
      throw new Error(`Relay rejected the connection: ${response}`);
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
    const detail = error instanceof Error ? error.message : String(error);
    throw new RelayConnectionError(
      `Could not establish the croc relay connection: ${detail}`,
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

async function sendControl(
  socket: CrocSocket,
  message: CrocMessage,
  key?: Uint8Array,
) {
  await socket.send(await encodeMessage(wasm(), message, key));
}

async function receiveControl(socket: CrocSocket, key?: Uint8Array) {
  return decodeMessage(wasm(), await socket.receive(), key);
}

async function waitForHandshake(
  socket: CrocSocket,
  room: string,
  passphrase: string,
  signal?: AbortSignal,
) {
  const engine = wasm();
  let localKey: Uint8Array | undefined;
  for (;;) {
    checkAbort(signal);
    const payload = await socket.receive();
    if (bytesEqual(payload, HANDSHAKE)) return;

    let plain = payload;
    if (localKey) {
      try {
        plain = await engine.decrypt(payload, localKey);
      } catch {
        // The final handshake is deliberately not part of the local probe.
      }
    }
    if (bytesEqual(plain, IP_REQUEST) && localKey) {
      await socket.send(
        await engine.encrypt(textEncoder.encode(JSON.stringify([])), localKey),
      );
      continue;
    }

    let probe: {
      Bytes?: string;
      Bytes2?: string;
      Kind?: string;
      Version?: number;
      Curve?: string;
    };
    try {
      probe = JSON.parse(textDecoder.decode(plain)) as typeof probe;
    } catch {
      throw new Error("Peer sent an unexpected control handshake");
    }
    if (probe.Kind !== "pake1" || !probe.Bytes || !probe.Curve) {
      throw new Error("Peer sent an unexpected control handshake");
    }
    requirePakeVersion(probe.Version);
    const initiator = base64ToBytes(probe.Bytes);
    const pake = await engine.pakeInitWithIdentities(
      textEncoder.encode(passphrase),
      1,
      probe.Curve,
      PAKE_PURPOSE_LOCAL_PROBE,
      room,
    );
    const finished = await engine.pakeUpdate(pake.handle, initiator);
    const salt = randomBytes(PAKE_SALT_SIZE);
    const keys = await engine.derivePeerKeys(
      finished.key,
      salt,
      PAKE_PURPOSE_LOCAL_PROBE,
      room,
      probe.Curve,
      initiator,
      finished.bytes,
    );
    localKey = keys.key;
    await socket.send(
      textEncoder.encode(
        JSON.stringify({
          Bytes: bytesToBase64(finished.bytes),
          Bytes2: bytesToBase64(salt),
          Kind: "pake2",
          Version: PAKE_PROTOCOL_VERSION,
          Curve: probe.Curve,
        }),
      ),
    );
  }
}

async function openDataConnections(
  settings: TransferSettings,
  room: string,
  ports: string[],
  relayIndex: number,
  signal?: AbortSignal,
) {
  const connected = await Promise.all(
    ports.map((port, index) =>
      connectRelay(settings, `${room}-${index}`, port, relayIndex, signal),
    ),
  );
  return connected.map(({ socket }) => socket);
}

function closeAll(control?: CrocSocket, data: CrocSocket[] = []) {
  control?.close();
  for (const socket of data) socket.close();
}

async function reportPeerError(
  control: CrocSocket | undefined,
  key: Uint8Array | undefined,
  error: unknown,
) {
  if (!control || !key) return;
  try {
    await sendControl(
      control,
      {
        t: "error",
        m: errorMessage(error).slice(0, 500),
      },
      key,
    );
  } catch {
    // The connection may already be gone.
  }
}

export async function prepareFiles(
  selected: File[],
  callbacks: TransferCallbacks = {},
  signal?: AbortSignal,
  hashProvider?: FileHashProvider,
) {
  if (selected.length === 0) throw new Error("Choose at least one file");
  const names = new Set<string>();
  const outgoingNames = selected.map((file) =>
    normalizeOutgoingFileName(file.name),
  );
  for (let index = 0; index < selected.length; index += 1) {
    const file = selected[index];
    const outgoingName = outgoingNames[index];
    if (names.has(outgoingName))
      throw new Error(`Duplicate filename: ${outgoingName}`);
    if (!Number.isSafeInteger(file.size))
      throw new Error(`File is too large: ${file.name}`);
    names.add(outgoingName);
  }

  const prepared: PreparedFile[] = [];
  for (let index = 0; index < selected.length; index += 1) {
    checkAbort(signal);
    const file = selected[index];
    callbacks.onStatus?.(
      `Hashing ${index + 1}/${selected.length}: ${file.name}`,
    );
    const hash = hashProvider
      ? await waitForHash(hashProvider(file), signal)
      : await hashFileContents(file, "xxhash", signal);
    let compressed = false;
    if (file.size > 0) {
      const sample = new Uint8Array(
        await file.slice(0, Math.min(file.size, 256 << 10)).arrayBuffer(),
      );
      const compressedSample = await wasm().compress(sample);
      compressed = compressedSample.byteLength * 100 < sample.byteLength * 98;
    }
    prepared.push({
      file,
      name: outgoingNames[index],
      size: file.size,
      hash,
      modified: new Date(file.lastModified).toISOString(),
      compressed,
    });
  }
  return prepared;
}

export async function prepareText(
  text: string,
  callbacks: TransferCallbacks = {},
  signal?: AbortSignal,
  hashProvider?: FileHashProvider,
) {
  const payload = new File([text], "croc-stdin-web", {
    type: "text/plain;charset=utf-8",
    lastModified: Date.now(),
  });
  if (payload.size === 0) throw new Error("Enter some text to send");
  if (payload.size > maxTextTransferBytes) {
    throw new Error("Text must be 1 MiB or smaller");
  }
  return prepareFiles([payload], callbacks, signal, hashProvider);
}

function senderInfo(
  files: PreparedFile[],
  sendingText: boolean,
): SenderInfoWire {
  const wireFiles: WireFileInfo[] = files.map((file) => ({
    n: file.name,
    fr: "./",
    h: bytesToBase64(file.hash),
    s: file.size,
    m: file.modified,
    md: 0o644,
    c: file.compressed ?? true,
  }));
  return {
    FilesToTransfer: wireFiles,
    EmptyFoldersToTransfer: null,
    TotalNumberFolders: 0,
    MachineID: machineID(),
    Ask: false,
    SendingText: sendingText,
    NoCompress: false,
    HashAlgorithm: "xxhash",
    ReconnectVersion: 0,
    NextReconnectRoom: "",
    Features: [PER_FILE_COMPRESSION_FEATURE],
  };
}

function requestedOffset(offset: number, ranges: number[] | null | undefined) {
  if (!ranges || ranges.length === 0) return true;
  const rangeChunkSize = ranges[0];
  for (let index = 1; index + 1 < ranges.length; index += 2) {
    const start = ranges[index];
    const count = ranges[index + 1];
    if (offset >= start && offset < start + count * rangeChunkSize) return true;
  }
  return false;
}

async function sendFileData(
  prepared: PreparedFile,
  ranges: number[] | null,
  sockets: CrocSocket[],
  cipherHandle: number,
  compressed: boolean,
  progress: (bytes: number) => void,
  signal?: AbortSignal,
) {
  const engine = wasm();
  const chunkCount = Math.ceil(prepared.size / CHUNK_SIZE);
  let sent = 0;

  await Promise.all(
    sockets.map(async (socket, socketIndex) => {
      for (
        let chunkIndex = socketIndex;
        chunkIndex < chunkCount;
        chunkIndex += sockets.length
      ) {
        checkAbort(signal);
        const position = chunkIndex * CHUNK_SIZE;
        if (!requestedOffset(position, ranges)) continue;
        const data = new Uint8Array(
          await prepared.file
            .slice(position, position + CHUNK_SIZE)
            .arrayBuffer(),
        );
        const plain = new Uint8Array(8 + data.byteLength);
        new DataView(plain.buffer).setBigUint64(0, BigInt(position), true);
        plain.set(data, 8);
        await socket.send(
          await engine.encodeChunk(
            cipherHandle,
            plain,
            compressed,
          ),
        );
        sent += data.byteLength;
        progress(sent);
      }
    }),
  );
}

export async function sendFiles(options: {
  files: PreparedFile[];
  sendingText?: boolean;
  secret: string;
  settings: TransferSettings;
  callbacks?: TransferCallbacks;
  signal?: AbortSignal;
}) {
  const {
    files,
    sendingText = false,
    secret,
    settings,
    callbacks = {},
    signal,
  } = options;
  validateSecret(secret);
  if (files.length === 0) throw new Error("Choose at least one file");
  if (sendingText && files.length !== 1) {
    throw new Error("A text transfer must contain exactly one text payload");
  }
  const totalSize = files.reduce((total, file) => total + file.size, 0);
  const { room, passphrase } = await wasm().codeComponents(secret);
  const relayIndex = await wasm().relayIndex(secret, settings.relayAddresses.length);
  let control: CrocSocket | undefined;
  let data: CrocSocket[] = [];
  let key: Uint8Array | undefined;
  let cipherHandle: number | undefined;
  try {
    callbacks.onStatus?.("Connecting to relay…");
    const relay = await connectRelay(
      settings,
      room,
      controlPort(relayAddress(settings, relayIndex)),
      relayIndex,
      signal,
    );
    control = relay.socket;
    callbacks.onStatus?.("Waiting for recipient…");
    await waitForHandshake(control, room, passphrase, signal);

    const peerPake = await receiveControl(control);
    if (peerPake.t !== "pake" || !peerPake.b || !peerPake.b2) {
      throw new Error("Recipient did not start a croc PAKE handshake");
    }
    requirePakeVersion(peerPake.v);
    const curve = textDecoder.decode(peerPake.b2);
    const pake = await wasm().pakeInitWithIdentities(
      textEncoder.encode(passphrase),
      1,
      curve,
      PAKE_PURPOSE_TRANSFER,
      room,
    );
    const finished = await wasm().pakeUpdate(pake.handle, peerPake.b);
    const salt = randomBytes(PAKE_SALT_SIZE);
    const peerKeys = await wasm().derivePeerKeys(
      finished.key,
      salt,
      PAKE_PURPOSE_TRANSFER,
      room,
      curve,
      peerPake.b,
      finished.bytes,
    );
    await sendControl(control, {
      t: "pake",
      v: PAKE_PROTOCOL_VERSION,
      b: finished.bytes,
      b2: salt,
    });
    const confirmationA = await receiveControl(control);
    if (confirmationA.t !== "pake-confirm" || !confirmationA.b) {
      throw new Error("Recipient did not confirm the croc PAKE handshake");
    }
    requirePakeVersion(confirmationA.v);
    if (
      !(await wasm().confirmPeerKey(peerKeys.confirmationA, confirmationA.b))
    ) {
      throw new Error("Recipient PAKE confirmation failed");
    }
    await sendControl(control, {
      t: "pake-confirm",
      v: PAKE_PROTOCOL_VERSION,
      b: peerKeys.confirmationB,
    });
    key = peerKeys.key;
    cipherHandle = await wasm().cipherInit(key);

    callbacks.onStatus?.("Opening encrypted data channels…");
    data = await openDataConnections(settings, room, dataPorts(relay.banner), relayIndex, signal);

    const peerIP = await receiveControl(control, key);
    if (peerIP.t !== "externalip")
      throw new Error("Recipient did not secure the channel");
    await sendControl(control, { t: "externalip", m: relay.externalIP }, key);
    await sendControl(
      control,
      {
        t: "fileinfo",
        b: textEncoder.encode(JSON.stringify(senderInfo(files, sendingText))),
      },
      key,
    );

    let totalTransferred = 0;
    for (;;) {
      checkAbort(signal);
      const message = await receiveControl(control, key);
      if (message.t === "error")
        throw new Error(message.m || "Recipient refused transfer");
      if (message.t === "finished") {
        await sendControl(control, { t: "finished" }, key);
        callbacks.onStatus?.("Transfer complete");
        return;
      }
      if (message.t !== "recipientready" || !message.b) {
        throw new Error(`Unexpected peer message: ${message.t}`);
      }

      const request = JSON.parse(
        textDecoder.decode(message.b),
      ) as RemoteFileRequestWire;
      const fileIndex = request.FilesToTransferCurrentNum;
      const prepared = files[fileIndex];
      if (!prepared) throw new Error("Recipient requested an unknown file");
      const displayName = sendingText ? "Text message" : prepared.name;
      callbacks.onStatus?.(
        sendingText ? "Sending text message" : `Sending ${prepared.name}`,
      );
      const beforeFile = totalTransferred;
      await sendFileData(
        prepared,
        request.CurrentFileChunkRanges,
        data,
        cipherHandle,
        (request.Features ?? []).includes(PER_FILE_COMPRESSION_FEATURE)
          ? (prepared.compressed ?? true)
          : true,
        (fileBytes) => {
          totalTransferred = beforeFile + fileBytes;
          callbacks.onProgress?.({
            fileIndex,
            fileCount: files.length,
            fileName: displayName,
            fileBytes,
            fileSize: prepared.size,
            totalBytes: totalTransferred,
            totalSize,
          });
        },
        signal,
      );
      const closed = await receiveControl(control, key);
      if (closed.t === "error")
        throw new Error(closed.m || "Recipient cancelled");
      if (closed.t !== "close-sender") {
        throw new Error(
          `Expected recipient to close the file, got ${closed.t}`,
        );
      }
      await sendControl(control, { t: "close-recipient" }, key);
      callbacks.onFileComplete?.(displayName);
    }
  } catch (error) {
    await reportPeerError(control, key, error);
    throw error;
  } finally {
    if (cipherHandle !== undefined) {
      await wasm()
        .cipherRelease(cipherHandle)
        .catch(() => {});
    }
    closeAll(control, data);
  }
}

type ActiveReceive = {
  file: OfferedFile;
  sink: ReceiveSink;
  received: Set<number>;
  bytes: number;
  queue: Promise<void>;
  progress(fileBytes: number): void;
  resolve(): void;
  reject(error: Error): void;
};

export class DataReceiver {
  private active?: ActiveReceive;
  private stopped = false;
  private failure?: Error;

  constructor(
    private sockets: CrocSocket[],
    private key: Uint8Array,
    private noCompress: boolean,
    private cipherHandle?: number,
  ) {
    for (const socket of sockets) void this.read(socket);
  }

  receive(
    file: OfferedFile,
    sink: ReceiveSink,
    progress: (fileBytes: number) => void,
  ) {
    // A data socket can fail before the next file is requested, so surface the
    // recorded failure instead of waiting for data that will never arrive.
    if (this.failure) return Promise.reject(this.failure);
    if (this.active) throw new Error("A receive file is already active");
    return new Promise<void>((resolve, reject) => {
      this.active = {
        file,
        sink,
        received: new Set(),
        bytes: 0,
        queue: Promise.resolve(),
        progress,
        resolve,
        reject,
      };
    });
  }

  stop() {
    this.stopped = true;
    this.fail(new Error("Data receiver stopped"));
  }

  private async read(socket: CrocSocket) {
    const engine = wasm();
    while (!this.stopped) {
      try {
        const encrypted = await socket.receive();
        const compressed =
          !this.noCompress && (this.active?.file.compressed ?? true);
        const payload =
          this.cipherHandle === undefined
            ? await this.decodeLegacy(engine, encrypted, compressed)
            : await engine.decodeChunk(
                this.cipherHandle,
                encrypted,
                compressed,
                MAX_DECOMPRESSED_CHUNK_SIZE,
              );
        if (payload.byteLength < 9)
          throw new Error("Received an invalid file chunk");
        const positionBig = new DataView(
          payload.buffer,
          payload.byteOffset,
          payload.byteLength,
        ).getBigUint64(0, true);
        if (positionBig > BigInt(Number.MAX_SAFE_INTEGER)) {
          throw new Error("Received a file position that is too large");
        }
        const position = Number(positionBig);
        const bytes = payload.subarray(8);
        await this.accept(position, bytes);
      } catch (error) {
        if (this.stopped) return;
        this.stopped = true;
        this.fail(error instanceof Error ? error : new Error(String(error)));
      }
    }
  }

  private async decodeLegacy(
    engine: ReturnType<typeof wasm>,
    encrypted: Uint8Array,
    compressed: boolean,
  ) {
    let payload = await engine.decrypt(encrypted, this.key);
    if (compressed) {
      payload = await engine.decompress(payload, MAX_DECOMPRESSED_CHUNK_SIZE);
    }
    return payload;
  }

  private fail(error: Error) {
    this.failure ??= error;
    this.active?.reject(error);
    this.active = undefined;
  }

  private accept(position: number, bytes: Uint8Array) {
    const active = this.active;
    if (!active) throw new Error("Received file data before it was requested");
    active.queue = active.queue.then(async () => {
      if (active.received.has(position))
        throw new Error("Received a duplicate file chunk");
      if (
        position < 0 ||
        position % CHUNK_SIZE !== 0 ||
        bytes.byteLength === 0 ||
        bytes.byteLength > CHUNK_SIZE ||
        position + bytes.byteLength > active.file.size
      ) {
        throw new Error(
          "Received a file chunk outside the advertised file size",
        );
      }
      active.received.add(position);
      await active.sink.writeAt(position, bytes);
      active.bytes += bytes.byteLength;
      active.progress(active.bytes);
      if (active.bytes === active.file.size) {
        this.active = undefined;
        active.resolve();
      } else if (active.bytes > active.file.size) {
        throw new Error("Received more data than the advertised file size");
      }
    });
    active.queue.catch((error) => {
      if (this.active === active) this.active = undefined;
      active.reject(error instanceof Error ? error : new Error(String(error)));
    });
    return active.queue;
  }
}

export async function receiveFiles(options: {
  secret: string;
  settings: TransferSettings;
  callbacks: ReceiveCallbacks;
  signal?: AbortSignal;
}) {
  const { secret, settings, callbacks, signal } = options;
  validateSecret(secret);
  const { room, passphrase } = await wasm().codeComponents(secret);
  const relayIndex = await wasm().relayIndex(secret, settings.relayAddresses.length);
  let control: CrocSocket | undefined;
  let data: CrocSocket[] = [];
  let key: Uint8Array | undefined;
  let cipherHandle: number | undefined;
  let receiver: DataReceiver | undefined;
  try {
    callbacks.onStatus?.("Connecting to relay…");
    const relay = await connectRelay(
      settings,
      room,
      controlPort(relayAddress(settings, relayIndex)),
      relayIndex,
      signal,
    );
    control = relay.socket;
    await control.send(HANDSHAKE);

    callbacks.onStatus?.("Securing channel…");
    const curve = "p256";
    const pake = await wasm().pakeInitWithIdentities(
      textEncoder.encode(passphrase),
      0,
      curve,
      PAKE_PURPOSE_TRANSFER,
      room,
    );
    await sendControl(control, {
      t: "pake",
      v: PAKE_PROTOCOL_VERSION,
      b: pake.bytes,
      b2: textEncoder.encode(curve),
    });
    const peerPake = await receiveControl(control);
    if (peerPake.t !== "pake" || !peerPake.b || !peerPake.b2) {
      throw new Error("Sender did not complete the croc PAKE handshake");
    }
    requirePakeVersion(peerPake.v);
    if (peerPake.b2.byteLength !== PAKE_SALT_SIZE) {
      throw new Error(
        `Sender provided an invalid ${peerPake.b2.byteLength}-byte PAKE salt`,
      );
    }
    const finished = await wasm().pakeUpdate(pake.handle, peerPake.b);
    const peerKeys = await wasm().derivePeerKeys(
      finished.key,
      peerPake.b2,
      PAKE_PURPOSE_TRANSFER,
      room,
      curve,
      pake.bytes,
      peerPake.b,
    );
    await sendControl(control, {
      t: "pake-confirm",
      v: PAKE_PROTOCOL_VERSION,
      b: peerKeys.confirmationA,
    });
    const confirmationB = await receiveControl(control);
    if (confirmationB.t !== "pake-confirm" || !confirmationB.b) {
      throw new Error("Sender did not confirm the croc PAKE handshake");
    }
    requirePakeVersion(confirmationB.v);
    if (
      !(await wasm().confirmPeerKey(peerKeys.confirmationB, confirmationB.b))
    ) {
      throw new Error("Sender PAKE confirmation failed");
    }
    key = peerKeys.key;
    cipherHandle = await wasm().cipherInit(key);
    data = await openDataConnections(settings, room, dataPorts(relay.banner), relayIndex, signal);
    await sendControl(
      control,
      {
        t: "externalip",
        m: relay.externalIP,
        b: peerPake.b,
      },
      key,
    );
    const peerIP = await receiveControl(control, key);
    if (peerIP.t !== "externalip")
      throw new Error("Sender did not secure the channel");

    const fileInfo = await receiveControl(control, key);
    if (fileInfo.t === "error")
      throw new Error(fileInfo.m || "Sender cancelled");
    if (fileInfo.t !== "fileinfo" || !fileInfo.b) {
      throw new Error("Sender did not provide file metadata");
    }
    const sender = JSON.parse(textDecoder.decode(fileInfo.b)) as SenderInfoWire;
    const offer = validateSenderInfo(sender);
    callbacks.onStatus?.("Review the incoming files");
    const destination = await callbacks.onOffer(offer);
    if (!destination) {
      await sendControl(control, { t: "error", m: "refusing files" }, key);
      throw new Error("Transfer refused");
    }

    for (const folder of offer.emptyFolders) {
      await destination.createEmptyFolder(folder);
    }
    let totalTransferred = 0;
    receiver = new DataReceiver(data, key, offer.noCompress, cipherHandle);
    for (let fileIndex = 0; fileIndex < offer.files.length; fileIndex += 1) {
      checkAbort(signal);
      const file = offer.files[fileIndex];
      const displayName = offer.kind === "text" ? "Text message" : file.path;
      if (file.size === 0) {
        const sink = await destination.openFile(file);
        try {
          await sink.finalize();
          callbacks.onStatus?.(`Verifying ${displayName}`);
          await verifySink(sink, file.hash);
          await sink.commit();
          callbacks.onFileComplete?.(displayName);
        } catch (error) {
          await sink.abort();
          throw error;
        }
        continue;
      }

      callbacks.onStatus?.(`Receiving ${displayName}`);
      const sink = await destination.openFile(file);
      const beforeFile = totalTransferred;
      try {
        const receivePromise = receiver.receive(file, sink, (fileBytes) => {
          totalTransferred = beforeFile + fileBytes;
          callbacks.onProgress?.({
            fileIndex,
            fileCount: offer.files.length,
            fileName: displayName,
            fileBytes,
            fileSize: file.size,
            totalBytes: totalTransferred,
            totalSize: offer.totalSize,
          });
        });
        const request: RemoteFileRequestWire = {
          CurrentFileChunkRanges: [],
          FilesToTransferCurrentNum: fileIndex,
          MachineID: machineID(),
          ReconnectVersion: 0,
          Features: offer.perFileCompression
            ? [PER_FILE_COMPRESSION_FEATURE]
            : undefined,
        };
        await sendControl(
          control,
          {
            t: "recipientready",
            b: textEncoder.encode(JSON.stringify(request)),
          },
          key,
        );
        await receivePromise;
        await sink.finalize();
        await sendControl(control, { t: "close-sender" }, key);
        const close = await receiveControl(control, key);
        if (close.t === "error") throw new Error(close.m || "Sender cancelled");
        if (close.t !== "close-recipient") {
          throw new Error(`Expected sender to close the file, got ${close.t}`);
        }
        callbacks.onStatus?.(`Verifying ${displayName}`);
        await verifySink(sink, file.hash);
        await sink.commit();
        totalTransferred = beforeFile + file.size;
        callbacks.onFileComplete?.(displayName);
      } catch (error) {
        await sink.abort();
        throw error;
      }
    }

    await sendControl(control, { t: "finished" }, key);
    const finishedMessage = await receiveControl(control, key);
    if (finishedMessage.t !== "finished") {
      throw new Error(`Expected transfer completion, got ${finishedMessage.t}`);
    }
    callbacks.onStatus?.("Transfer complete");
    return offer;
  } catch (error) {
    await reportPeerError(control, key, error);
    throw error;
  } finally {
    receiver?.stop();
    if (cipherHandle !== undefined) {
      await wasm()
        .cipherRelease(cipherHandle)
        .catch(() => {});
    }
    closeAll(control, data);
  }
}
