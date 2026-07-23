import { decodeMessage, encodeMessage } from "./codec";
import {
  base64ToBytes,
  bytesEqual,
  bytesToBase64,
  errorMessage,
  hex,
  randomBytes,
  textDecoder,
  textEncoder,
} from "./bytes";
import { CrocSocket } from "./transport";
import { validateSenderInfo } from "./metadata";
import { verifySink } from "./storage";
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
import { wasm } from "../wasm/client";

const CONTROL_PORT = "9009";
const CHUNK_SIZE = 32 * 1024;
const HANDSHAKE = textEncoder.encode("handshake");
const IP_REQUEST = textEncoder.encode("ips?");
const WEAK_RELAY_KEY = new Uint8Array([1, 2, 3]);

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

function validateSecret(secret: string) {
  if (secret.length < 6) throw new Error("Code must be at least 6 characters");
  if (!/^[\x20-\x7e]+$/.test(secret)) {
    throw new Error("Custom codes must use printable ASCII characters");
  }
}

async function roomForSecret(secret: string) {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    textEncoder.encode(`${secret.slice(0, 4)}croc`),
  );
  return hex(new Uint8Array(digest));
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

function dataPorts(banner: string) {
  const ports = banner
    .split(",")
    .map((port) => port.trim())
    .filter((port) => /^\d{1,5}$/.test(port));
  if (ports.length === 0) throw new Error(`Relay returned an invalid port list: ${banner}`);
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

async function connectRelay(
  settings: TransferSettings,
  room: string,
  port: string,
  signal?: AbortSignal,
) {
  const engine = wasm();
  const socket = await CrocSocket.connect(settings.gatewayURL, port, signal);
  try {
    const pake = await engine.pakeInit(WEAK_RELAY_KEY, 0, "siec");
    await socket.send(pake.bytes);
    const peer = await socket.receive();
    const finished = await engine.pakeUpdate(pake.handle, peer);
    const salt = randomBytes(8);
    const key = await engine.deriveKey(finished.key, salt);
    await socket.send(salt);
    await socket.send(await engine.encrypt(textEncoder.encode(settings.relayPassword), key));
    const response = textDecoder.decode(
      await engine.decrypt(await socket.receive(), key),
    );
    const separator = response.indexOf("|||");
    if (separator < 0) throw new Error(`Relay rejected the connection: ${response}`);
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
    socket.close();
    throw error;
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
  secret: string,
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

    try {
      const probe = JSON.parse(textDecoder.decode(plain)) as {
        Bytes?: string;
        Kind?: string;
      };
      if (probe.Kind !== "pake1" || !probe.Bytes) throw new Error("not a probe");
      const pake = await engine.pakeInit(
        textEncoder.encode(secret.slice(5)),
        1,
        "p256",
      );
      const finished = await engine.pakeUpdate(
        pake.handle,
        base64ToBytes(probe.Bytes),
      );
      localKey = finished.key;
      await socket.send(
        textEncoder.encode(
          JSON.stringify({
            Bytes: bytesToBase64(finished.bytes),
            Kind: "pake2",
          }),
        ),
      );
    } catch {
      throw new Error("Peer sent an unexpected control handshake");
    }
  }
}

async function openDataConnections(
  settings: TransferSettings,
  room: string,
  ports: string[],
  signal?: AbortSignal,
) {
  const connected = await Promise.all(
    ports.map((port, index) =>
      connectRelay(settings, `${room}-${index}`, port, signal),
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
    await sendControl(control, {
      t: "error",
      m: errorMessage(error).slice(0, 500),
    }, key);
  } catch {
    // The connection may already be gone.
  }
}

export async function prepareFiles(
  selected: File[],
  callbacks: TransferCallbacks = {},
  signal?: AbortSignal,
) {
  if (selected.length === 0) throw new Error("Choose at least one file");
  const names = new Set<string>();
  for (const file of selected) {
    if (names.has(file.name)) throw new Error(`Duplicate filename: ${file.name}`);
    if (!Number.isSafeInteger(file.size)) throw new Error(`File is too large: ${file.name}`);
    names.add(file.name);
  }

  const prepared: PreparedFile[] = [];
  const engine = wasm();
  for (let index = 0; index < selected.length; index += 1) {
    checkAbort(signal);
    const file = selected[index];
    callbacks.onStatus?.(`Hashing ${index + 1}/${selected.length}: ${file.name}`);
    const hashHandle = await engine.hashInit();
    const reader = file.stream().getReader();
    try {
      for (;;) {
        checkAbort(signal);
        const { done, value } = await reader.read();
        if (done) break;
        await engine.hashUpdate(hashHandle, value);
      }
    } finally {
      reader.releaseLock();
    }
    prepared.push({
      file,
      name: file.name,
      size: file.size,
      hash: await engine.hashFinal(hashHandle),
      modified: new Date(file.lastModified).toISOString(),
    });
  }
  return prepared;
}

function senderInfo(files: PreparedFile[]): SenderInfoWire {
  const wireFiles: WireFileInfo[] = files.map((file) => ({
    n: file.name,
    fr: "./",
    h: bytesToBase64(file.hash),
    s: file.size,
    m: file.modified,
    md: 0o644,
  }));
  return {
    FilesToTransfer: wireFiles,
    EmptyFoldersToTransfer: null,
    TotalNumberFolders: 0,
    MachineID: machineID(),
    Ask: false,
    SendingText: false,
    NoCompress: false,
    HashAlgorithm: "xxhash",
    ReconnectVersion: 0,
    NextReconnectRoom: "",
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
  key: Uint8Array,
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
          await prepared.file.slice(position, position + CHUNK_SIZE).arrayBuffer(),
        );
        const plain = new Uint8Array(8 + data.byteLength);
        new DataView(plain.buffer).setBigUint64(0, BigInt(position), true);
        plain.set(data, 8);
        const compressed = await engine.compress(plain);
        await socket.send(await engine.encrypt(compressed, key));
        sent += data.byteLength;
        progress(sent);
      }
    }),
  );
}

export async function sendFiles(options: {
  files: PreparedFile[];
  secret: string;
  settings: TransferSettings;
  callbacks?: TransferCallbacks;
  signal?: AbortSignal;
}) {
  const { files, secret, settings, callbacks = {}, signal } = options;
  validateSecret(secret);
  if (files.length === 0) throw new Error("Choose at least one file");
  const totalSize = files.reduce((total, file) => total + file.size, 0);
  const room = await roomForSecret(secret);
  let control: CrocSocket | undefined;
  let data: CrocSocket[] = [];
  let key: Uint8Array | undefined;
  try {
    callbacks.onStatus?.("Connecting to relay…");
    const relay = await connectRelay(
      settings,
      room,
      controlPort(settings.relayAddress),
      signal,
    );
    control = relay.socket;
    callbacks.onStatus?.("Waiting for recipient…");
    await waitForHandshake(control, secret, signal);

    const peerPake = await receiveControl(control);
    if (peerPake.t !== "pake" || !peerPake.b || !peerPake.b2) {
      throw new Error("Recipient did not start a croc PAKE handshake");
    }
    const curve = textDecoder.decode(peerPake.b2);
    const pake = await wasm().pakeInit(
      textEncoder.encode(secret.slice(5)),
      1,
      curve,
    );
    const finished = await wasm().pakeUpdate(pake.handle, peerPake.b);
    const salt = randomBytes(8);
    await sendControl(control, {
      t: "pake",
      b: finished.bytes,
      b2: salt,
    });
    key = await wasm().deriveKey(finished.key, salt);

    callbacks.onStatus?.("Opening encrypted data channels…");
    data = await openDataConnections(
      settings,
      room,
      dataPorts(relay.banner),
      signal,
    );

    const peerIP = await receiveControl(control, key);
    if (peerIP.t !== "externalip") throw new Error("Recipient did not secure the channel");
    await sendControl(control, { t: "externalip", m: relay.externalIP }, key);
    await sendControl(control, {
      t: "fileinfo",
      b: textEncoder.encode(JSON.stringify(senderInfo(files))),
    }, key);

    let totalTransferred = 0;
    for (;;) {
      checkAbort(signal);
      const message = await receiveControl(control, key);
      if (message.t === "error") throw new Error(message.m || "Recipient refused transfer");
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
      callbacks.onStatus?.(`Sending ${prepared.name}`);
      const beforeFile = totalTransferred;
      await sendFileData(
        prepared,
        request.CurrentFileChunkRanges,
        data,
        key,
        (fileBytes) => {
          totalTransferred = beforeFile + fileBytes;
          callbacks.onProgress?.({
            fileIndex,
            fileCount: files.length,
            fileName: prepared.name,
            fileBytes,
            fileSize: prepared.size,
            totalBytes: totalTransferred,
            totalSize,
          });
        },
        signal,
      );
      const closed = await receiveControl(control, key);
      if (closed.t === "error") throw new Error(closed.m || "Recipient cancelled");
      if (closed.t !== "close-sender") {
        throw new Error(`Expected recipient to close the file, got ${closed.t}`);
      }
      await sendControl(control, { t: "close-recipient" }, key);
      callbacks.onFileComplete?.(prepared.name);
    }
  } catch (error) {
    await reportPeerError(control, key, error);
    throw error;
  } finally {
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

class DataReceiver {
  private active?: ActiveReceive;
  private stopped = false;

  constructor(
    private sockets: CrocSocket[],
    private key: Uint8Array,
    private noCompress: boolean,
  ) {
    for (const socket of sockets) void this.read(socket);
  }

  receive(
    file: OfferedFile,
    sink: ReceiveSink,
    progress: (fileBytes: number) => void,
  ) {
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
    this.active?.reject(new Error("Data receiver stopped"));
    this.active = undefined;
  }

  private async read(socket: CrocSocket) {
    const engine = wasm();
    while (!this.stopped) {
      try {
        let payload = await engine.decrypt(await socket.receive(), this.key);
        if (!this.noCompress) payload = await engine.decompress(payload);
        if (payload.byteLength < 9) throw new Error("Received an invalid file chunk");
        const positionBig = new DataView(
          payload.buffer,
          payload.byteOffset,
          payload.byteLength,
        ).getBigUint64(0, true);
        if (positionBig > BigInt(Number.MAX_SAFE_INTEGER)) {
          throw new Error("Received a file position that is too large");
        }
        const position = Number(positionBig);
        const bytes = payload.slice(8);
        await this.accept(position, bytes);
      } catch (error) {
        if (this.stopped) return;
        const normalized = error instanceof Error ? error : new Error(String(error));
        this.active?.reject(normalized);
        this.active = undefined;
        this.stopped = true;
      }
    }
  }

  private accept(position: number, bytes: Uint8Array) {
    const active = this.active;
    if (!active) throw new Error("Received file data before it was requested");
    active.queue = active.queue.then(async () => {
      if (active.received.has(position)) throw new Error("Received a duplicate file chunk");
      if (
        position < 0 ||
        position % CHUNK_SIZE !== 0 ||
        bytes.byteLength === 0 ||
        bytes.byteLength > CHUNK_SIZE ||
        position + bytes.byteLength > active.file.size
      ) {
        throw new Error("Received a file chunk outside the advertised file size");
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
  const room = await roomForSecret(secret);
  let control: CrocSocket | undefined;
  let data: CrocSocket[] = [];
  let key: Uint8Array | undefined;
  let receiver: DataReceiver | undefined;
  try {
    callbacks.onStatus?.("Connecting to relay…");
    const relay = await connectRelay(
      settings,
      room,
      controlPort(settings.relayAddress),
      signal,
    );
    control = relay.socket;
    await control.send(HANDSHAKE);

    callbacks.onStatus?.("Securing channel…");
    const pake = await wasm().pakeInit(
      textEncoder.encode(secret.slice(5)),
      0,
      "p256",
    );
    await sendControl(control, {
      t: "pake",
      b: pake.bytes,
      b2: textEncoder.encode("p256"),
    });
    const peerPake = await receiveControl(control);
    if (peerPake.t !== "pake" || !peerPake.b || !peerPake.b2) {
      throw new Error("Sender did not complete the croc PAKE handshake");
    }
    const finished = await wasm().pakeUpdate(pake.handle, peerPake.b);
    key = await wasm().deriveKey(finished.key, peerPake.b2);
    data = await openDataConnections(
      settings,
      room,
      dataPorts(relay.banner),
      signal,
    );
    await sendControl(control, {
      t: "externalip",
      m: relay.externalIP,
      b: peerPake.b,
    }, key);
    const peerIP = await receiveControl(control, key);
    if (peerIP.t !== "externalip") throw new Error("Sender did not secure the channel");

    const fileInfo = await receiveControl(control, key);
    if (fileInfo.t === "error") throw new Error(fileInfo.m || "Sender cancelled");
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
    receiver = new DataReceiver(data, key, offer.noCompress);
    for (let fileIndex = 0; fileIndex < offer.files.length; fileIndex += 1) {
      checkAbort(signal);
      const file = offer.files[fileIndex];
      if (file.size === 0) {
        await destination.createEmptyFile(file);
        callbacks.onFileComplete?.(file.path);
        continue;
      }

      callbacks.onStatus?.(`Receiving ${file.path}`);
      const sink = await destination.openFile(file);
      const beforeFile = totalTransferred;
      try {
        const receivePromise = receiver.receive(file, sink, (fileBytes) => {
          totalTransferred = beforeFile + fileBytes;
          callbacks.onProgress?.({
            fileIndex,
            fileCount: offer.files.length,
            fileName: file.path,
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
        };
        await sendControl(control, {
          t: "recipientready",
          b: textEncoder.encode(JSON.stringify(request)),
        }, key);
        await receivePromise;
        await sink.finalize();
        await sendControl(control, { t: "close-sender" }, key);
        const close = await receiveControl(control, key);
        if (close.t === "error") throw new Error(close.m || "Sender cancelled");
        if (close.t !== "close-recipient") {
          throw new Error(`Expected sender to close the file, got ${close.t}`);
        }
        callbacks.onStatus?.(`Verifying ${file.path}`);
        await verifySink(sink, file.hash);
        totalTransferred = beforeFile + file.size;
        callbacks.onFileComplete?.(file.path);
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
  } catch (error) {
    await reportPeerError(control, key, error);
    throw error;
  } finally {
    receiver?.stop();
    closeAll(control, data);
  }
}
