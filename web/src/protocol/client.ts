import {
  BUNDLE_FEATURE,
  BUNDLE_LIMITS,
  BundleLifecycle,
  BundleControlGuard,
  effectiveBundleLimits,
  planBundles,
  decodeBundleJSON,
  validateBundleRequest,
  validateBundleOffer,
  validateBundleAck,
} from "./bundle";
import { createBundleProgress } from "./bundle-progress";
import { BundleSink } from "./bundle-storage";
import { checkProtocolCapabilities } from "./compression";
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
import {
  connectRelay,
  controlPort,
  isRelayConnectionError,
  measureRelayLatency,
  receiveControl,
  relayAddress,
  sendControl,
} from "./relay";
import { normalizeOutgoingFileName, validateSenderInfo } from "./metadata";
import { verifySink } from "./storage";
import { hashFileContents, waitForHash, type FileHashProvider } from "./hash";
import type {
  CrocMessage,
  FileProgress,
  OfferedFile,
  PreparedFile,
  ReceiveCallbacks,
  WritablePayload,
  RemoteFileRequestWire,
  SenderInfoWire,
  TransferCallbacks,
  TransferSettings,
  WireFileInfo,
} from "./types";
import { maxTextTransferBytes } from "./types";
import { protocolEngine } from "./engine";

const CHUNK_SIZE = 32 * 1024;
const MAX_DECOMPRESSED_CHUNK_SIZE = CHUNK_SIZE + 8;
const PER_FILE_COMPRESSION_FEATURE = "per-file-compression-v1";
const HANDSHAKE = textEncoder.encode("handshake");
const IP_REQUEST = textEncoder.encode("ips?");
const PAKE_PROTOCOL_VERSION = 2;
const PAKE_SALT_SIZE = 32;
const PAKE_PURPOSE_TRANSFER = "peer-transfer";
const PAKE_PURPOSE_LOCAL_PROBE = "local-ip-probe";

function abortError() {
  return new DOMException("Transfer cancelled", "AbortError");
}

function checkAbort(signal?: AbortSignal) {
  if (signal?.aborted) throw abortError();
}

export function requirePakeVersion(version: number | undefined) {
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

export {
  isRelayConnectionError,
  measureRelayLatency,
  RelayConnectionError,
} from "./relay";

async function waitForHandshake(
  socket: CrocSocket,
  room: string,
  passphrase: string,
  signal?: AbortSignal,
) {
  const engine = protocolEngine();
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
      signal,
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
  checkProtocolCapabilities();
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
      const compressedSample = await protocolEngine().compress(sample);
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
    p: true,
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
    Features: [PER_FILE_COMPRESSION_FEATURE, BUNDLE_FEATURE],
    BundleLimits: BUNDLE_LIMITS,
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
  prepared: { file: Blob; size: number },
  ranges: number[] | null,
  sockets: CrocSocket[],
  cipherHandle: number,
  compressed: boolean,
  progress: (bytes: number, position: number, length: number) => void,
  signal?: AbortSignal,
) {
  const engine = protocolEngine();
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
          await engine.encodeChunk(cipherHandle, plain, compressed),
        );
        sent += data.byteLength;
        progress(sent, position, data.byteLength);
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
  checkProtocolCapabilities();
  const engine = protocolEngine();
  validateSecret(secret);
  if (files.length === 0) throw new Error("Choose at least one file");
  if (sendingText && files.length !== 1) {
    throw new Error("A text transfer must contain exactly one text payload");
  }
  const totalSize = files.reduce((total, file) => total + file.size, 0);
  const { room, passphrase } = await engine.codeComponents(secret);
  const relayIndex = await engine.relayIndex(
    secret,
    settings.relayAddresses.length,
  );
  let control: CrocSocket | undefined;
  let data: CrocSocket[] = [];
  let key: Uint8Array | undefined;
  let cipherHandle: number | undefined;
  let pakeHandle: number | undefined;
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
    const pake = await engine.pakeInitWithIdentities(
      textEncoder.encode(passphrase),
      1,
      curve,
      PAKE_PURPOSE_TRANSFER,
      room,
      signal,
    );
    pakeHandle = pake.handle;
    const finished = await engine.pakeUpdate(pake.handle, peerPake.b);
    const salt = randomBytes(PAKE_SALT_SIZE);
    const peerKeys = await engine.derivePeerKeys(
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
      f: [BUNDLE_FEATURE],
    });
    const confirmationA = await receiveControl(control);
    if (confirmationA.t !== "pake-confirm" || !confirmationA.b) {
      throw new Error("Recipient did not confirm the croc PAKE handshake");
    }
    requirePakeVersion(confirmationA.v);
    if (
      !(await engine.confirmPeerKey(peerKeys.confirmationA, confirmationA.b))
    ) {
      throw new Error("Recipient PAKE confirmation failed");
    }
    await sendControl(control, {
      t: "pake-confirm",
      v: PAKE_PROTOCOL_VERSION,
      b: peerKeys.confirmationB,
    });
    key = peerKeys.key;
    cipherHandle = await engine.cipherInit(key);

    callbacks.onStatus?.("Opening encrypted data channels…");
    data = await openDataConnections(
      settings,
      room,
      dataPorts(relay.banner),
      relayIndex,
      signal,
    );

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
    let progressStarted = false;
    const bundleState = new BundleLifecycle();
    const bundleCapable =
      (peerPake.f ?? []).includes(BUNDLE_FEATURE) && !sendingText;
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
      if (message.t === "bundle-request") {
        if (!bundleCapable)
          throw new Error("Bundle request without negotiated support");
        const members = files.map((file) => ({
          size: file.size,
          eligible:
            file.size > 0 && file.size <= 1 << 20 && file.hash.byteLength === 8,
          requested: true,
        }));
        const { request, id, offsets, size } = validateBundleRequest(
          decodeBundleJSON(message.b),
          members,
          BUNDLE_LIMITS,
        );
        bundleState.request(id);
        const payload = new Blob(
          request.indices.map((index) => files[index].file),
        );
        if (payload.size !== size)
          throw new Error("Bundle source size changed");
        for (const index of request.indices) {
          if (
            !bytesEqual(
              await hashFileContents(files[index].file, "xxhash", signal),
              files[index].hash,
            )
          )
            throw new Error(`Bundle source changed: ${files[index].name}`);
        }
        const hash = await hashFileContents(payload, "xxhash", signal);
        const sample = new Uint8Array(
          await payload.slice(0, 256 << 10).arrayBuffer(),
        );
        const compressed =
          (await engine.compress(sample)).byteLength * 100 <
          sample.byteLength * 98;
        bundleState.advance(id, "offer");
        await sendControl(
          control,
          {
            t: "bundle-offer",
            b: textEncoder.encode(
              JSON.stringify({
                id,
                size,
                hash: bytesToBase64(hash),
                compressed,
              }),
            ),
          },
          key,
        );
        const ready = await receiveControl(control, key);
        if (ready.t !== "bundle-ready")
          throw new Error(`Expected bundle ready, got ${ready.t}`);
        validateBundleAck(decodeBundleJSON(ready.b), id);
        bundleState.advance(id, "ready");
        progressStarted = true;
        const update = createBundleProgress(
          request.indices,
          offsets,
          files.length,
          (index) => ({ size: files[index].size, name: files[index].name }),
          totalTransferred,
          totalSize,
          callbacks,
          (bytes) => {
            totalTransferred = bytes;
          },
        );
        update(0, 0);
        callbacks.onStatus?.(`Sending ${files[request.indices[0]].name}`);
        await sendFileData(
          { file: payload, size },
          null,
          data,
          cipherHandle,
          compressed,
          (_sent, position, length) => update(position, length),
          signal,
        );
        const done = await receiveControl(control, key);
        if (done.t !== "bundle-done")
          throw new Error(`Expected bundle done, got ${done.t}`);
        validateBundleAck(decodeBundleJSON(done.b), id);
        bundleState.advance(id, "done");
        // sendFileData awaited every worker before the close acknowledgement.
        await sendControl(
          control,
          { t: "bundle-closed", b: textEncoder.encode(JSON.stringify({ id })) },
          key,
        );
        bundleState.advance(id, "closed");
        for (const index of request.indices)
          callbacks.onFileComplete?.(files[index].name);
        continue;
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
      if (!progressStarted && prepared.size > 0) {
        progressStarted = true;
        callbacks.onProgress?.({
          fileIndex,
          fileCount: files.length,
          fileName: displayName,
          fileBytes: 0,
          fileSize: prepared.size,
          totalBytes: 0,
          totalSize,
        });
      }
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
    if (pakeHandle !== undefined)
      await engine.pakeRelease(pakeHandle).catch(() => undefined);
    if (cipherHandle !== undefined) {
      await engine.cipherRelease(cipherHandle).catch(() => {});
    }
    closeAll(control, data);
  }
}

type DataPayload = { size: number; compressed?: boolean };

type ActiveReceive = {
  payload: DataPayload;
  sink: WritablePayload;
  received: Set<number>;
  bytes: number;
  queue: Promise<void>;
  progress(fileBytes: number): void;
  strictLength: boolean;
  chunkProgress?: (position: number, length: number) => void;
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
    payload: DataPayload,
    sink: WritablePayload,
    progress: (fileBytes: number) => void,
    chunkProgress?: (position: number, length: number) => void,
    strictLength = false,
  ) {
    // A data socket can fail before the next file is requested, so surface the
    // recorded failure instead of waiting for data that will never arrive.
    if (this.failure) return Promise.reject(this.failure);
    if (this.active) throw new Error("A receive file is already active");
    return new Promise<void>((resolve, reject) => {
      this.active = {
        payload,
        sink,
        received: new Set(),
        bytes: 0,
        queue: Promise.resolve(),
        progress,
        strictLength,
        chunkProgress,
        resolve,
        reject,
      };
    });
  }

  assertHealthy() {
    if (this.failure) throw this.failure;
  }

  stop() {
    this.stopped = true;
    this.fail(new Error("Data receiver stopped"));
  }

  private async read(socket: CrocSocket) {
    const engine = protocolEngine();
    while (!this.stopped) {
      try {
        const encrypted = await socket.receive();
        const compressed =
          !this.noCompress && (this.active?.payload.compressed ?? true);
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
    engine: ReturnType<typeof protocolEngine>,
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
        position + bytes.byteLength > active.payload.size ||
        (active.strictLength &&
          bytes.byteLength !==
            Math.min(CHUNK_SIZE, active.payload.size - position))
      ) {
        throw new Error(
          "Received a file chunk outside the advertised file size",
        );
      }
      active.received.add(position);
      await active.sink.writeAt(position, bytes);
      active.bytes += bytes.byteLength;
      active.progress(active.bytes);
      active.chunkProgress?.(position, bytes.byteLength);
      if (active.bytes === active.payload.size) {
        this.active = undefined;
        active.resolve();
      } else if (active.bytes > active.payload.size) {
        throw new Error("Received more data than the advertised file size");
      }
    });
    active.queue.catch((error) => {
      const failure = error instanceof Error ? error : new Error(String(error));
      this.fail(failure);
      active.reject(failure);
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
  checkProtocolCapabilities();
  const engine = protocolEngine();
  validateSecret(secret);
  const { room, passphrase } = await engine.codeComponents(secret);
  const relayIndex = await engine.relayIndex(
    secret,
    settings.relayAddresses.length,
  );
  let control: CrocSocket | undefined;
  let data: CrocSocket[] = [];
  let key: Uint8Array | undefined;
  let cipherHandle: number | undefined;
  let pakeHandle: number | undefined;
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
    callbacks.onStatus?.("Waiting for sender…");
    await control.send(HANDSHAKE);

    callbacks.onStatus?.("Authenticating code…");
    const curve = "p256";
    const pake = await engine.pakeInitWithIdentities(
      textEncoder.encode(passphrase),
      0,
      curve,
      PAKE_PURPOSE_TRANSFER,
      room,
      signal,
    );
    pakeHandle = pake.handle;
    await sendControl(control, {
      t: "pake",
      v: PAKE_PROTOCOL_VERSION,
      b: pake.bytes,
      b2: textEncoder.encode(curve),
      f: [BUNDLE_FEATURE],
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
    const finished = await engine.pakeUpdate(pake.handle, peerPake.b);
    const peerKeys = await engine.derivePeerKeys(
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
      !(await engine.confirmPeerKey(peerKeys.confirmationB, confirmationB.b))
    ) {
      throw new Error("Sender PAKE confirmation failed");
    }
    key = peerKeys.key;
    cipherHandle = await engine.cipherInit(key);
    callbacks.onStatus?.("Opening transfer channels…");
    data = await openDataConnections(
      settings,
      room,
      dataPorts(relay.banner),
      relayIndex,
      signal,
    );
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

    callbacks.onStatus?.("Waiting for file list…");
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
    let progressStarted = false;
    receiver = new DataReceiver(data, key, offer.noCompress, cipherHandle);
    const bundleState = new BundleLifecycle();
    const canBundle =
      (peerPake.f ?? []).includes(BUNDLE_FEATURE) && offer.bundleLimits;
    const units = canBundle
      ? planBundles(
          offer.files.map((file) => ({
            size: file.size,
            eligible:
              Boolean(file.bundleEligible) && file.hash.byteLength === 8,
            requested: true,
          })),
          effectiveBundleLimits(BUNDLE_LIMITS, canBundle),
        )
      : offer.files.map((_file, index) => [index]);
    for (const indices of units) {
      const fileIndex = indices[0];
      if (indices.length >= 4) {
        checkAbort(signal);
        const id = bundleState.lastID + 1;
        bundleState.request(id);
        const offsets: number[] = [];
        let size = 0;
        for (const index of indices) {
          offsets.push(size);
          size += offer.files[index].size;
        }
        const request = {
          id,
          indices,
          limits: BUNDLE_LIMITS,
          MachineID: machineID(),
          ReconnectVersion: 0,
        };
        await sendControl(
          control,
          {
            t: "bundle-request",
            b: textEncoder.encode(JSON.stringify(request)),
          },
          key,
        );
        const response = await receiveControl(control, key);
        if (response.t !== "bundle-offer")
          throw new Error(`Expected bundle offer, got ${response.t}`);
        const bundleOffer = validateBundleOffer(
          decodeBundleJSON(response.b),
          id,
          size,
          offer.noCompress,
        );
        bundleState.advance(id, "offer");
        const sink = new BundleSink(size);
        const update = createBundleProgress(
          indices,
          offsets,
          offer.files.length,
          (index) => ({
            size: offer.files[index].size,
            name: offer.files[index].path,
          }),
          totalTransferred,
          offer.totalSize,
          callbacks,
          (bytes) => {
            totalTransferred = bytes;
          },
        );
        const guard = new BundleControlGuard(
          receiveControl(control, key),
          signal,
        );
        try {
          progressStarted = true;
          update(0, 0);
          const received = receiver.receive(
            {
              size,
              compressed: bundleOffer.compressed,
            },
            sink,
            () => {},
            update,
            true,
          );
          // Attach rejection handling before ready can fail or cancellation fires.
          void received.catch(() => undefined);
          bundleState.advance(id, "ready");
          await sendControl(
            control,
            {
              t: "bundle-ready",
              b: textEncoder.encode(JSON.stringify({ id })),
            },
            key,
          );
          await Promise.race([received, guard.failure]);
          await sink.finalize();
          await verifySink(sink, base64ToBytes(bundleOffer.hash), guard.signal);
          guard.check();
          receiver.assertHealthy();
          await sink.commitMembers(
            offer.files,
            indices,
            offsets,
            destination,
            (index) => {
              receiver!.assertHealthy();
              callbacks.onFileComplete?.(offer.files[index].path);
            },
            guard.signal,
          );
          guard.check();
          receiver.assertHealthy();
          bundleState.advance(id, "done");
          guard.allowClosed();
          await sendControl(
            control,
            { t: "bundle-done", b: textEncoder.encode(JSON.stringify({ id })) },
            key,
          );
          const closed = await guard.next;
          if (closed.t !== "bundle-closed")
            throw new Error(`Expected bundle closed, got ${closed.t}`);
          validateBundleAck(decodeBundleJSON(closed.b), id);
          bundleState.advance(id, "closed");
          receiver.assertHealthy();
        } catch (error) {
          guard.check();
          throw error;
        } finally {
          guard.dispose();
          await sink.abort();
        }
        continue;
      }
      checkAbort(signal);
      const file = offer.files[fileIndex];
      const displayName = offer.kind === "text" ? "Text message" : file.path;
      if (file.size === 0) {
        const sink = await destination.openFile(file);
        try {
          await sink.finalize();
          callbacks.onStatus?.(`Verifying ${displayName}`);
          await verifySink(sink, file.hash, signal);
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
        if (!progressStarted) {
          progressStarted = true;
          callbacks.onProgress?.({
            fileIndex,
            fileCount: offer.files.length,
            fileName: displayName,
            fileBytes: 0,
            fileSize: file.size,
            totalBytes: 0,
            totalSize: offer.totalSize,
          });
        }
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
        await verifySink(sink, file.hash, signal);
        await sink.commit();
        totalTransferred = beforeFile + file.size;
        callbacks.onFileComplete?.(displayName);
      } catch (error) {
        await sink.abort();
        throw error;
      }
    }

    receiver.assertHealthy();
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
    if (pakeHandle !== undefined)
      await engine.pakeRelease(pakeHandle).catch(() => undefined);
    if (cipherHandle !== undefined) {
      await engine.cipherRelease(cipherHandle).catch(() => {});
    }
    closeAll(control, data);
  }
}
