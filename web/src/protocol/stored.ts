import { errorMessage, textDecoder, textEncoder } from "./bytes";
import {
  hashFileContents,
  waitForHash,
  type FileHashProvider,
} from "./hash";
import { normalizeOutgoingFileName } from "./metadata";
import { verifySinkSHA256 } from "./storage";
import type {
  FileProgress,
  PreparedFile,
  ReceiveCallbacks,
  ReceiveDestination,
  TransferOffer,
  TransferSettings,
} from "./types";
import { wasm } from "../wasm/client";

export const storedProtocol = "croc-store-v1";
export const storedChunkSize = 4 * 1024 * 1024;
const storedKeyBytes = 32;
const maxManifestCiphertext = 256 * 1024;

type StoredManifestFile = {
  n: string;
  s: number;
  m: string;
  h: string;
  fc: number;
  cc: number;
};

type StoredManifest = {
  v: number;
  cs: number;
  f: StoredManifestFile[];
};

export type StoredShare = {
  origin: string;
  id: string;
  key: Uint8Array;
};

export type StoredPreparedFile = PreparedFile & {
  sha256: Uint8Array;
  firstChunk: number;
  chunkCount: number;
};

export type StoredUploadResult = {
  share: StoredShare;
  uploadToken: string;
  expiresAt: string;
  browserURL: string;
  cliToken: string;
  downloads: number;
};

export type StoredInspection = {
  share: StoredShare;
  manifest: StoredManifest;
  offer: TransferOffer;
  expiresAt?: string;
};

export type StoredSettings = Pick<TransferSettings, "storeAPI"> & {
  maxTransferBytes: number;
  maxFiles: number;
  maxDownloads: number;
  maxExpiresSeconds: number;
};

type StoredUploadCallbacks = {
  onStatus?(status: string): void;
  onProgress?(progress: FileProgress): void;
};

type StoredUploadPlan = {
  files: StoredPreparedFile[];
  manifestJSON: Uint8Array;
  chunkBytes: number[];
  totalSize: number;
};

type CreatedStoredUpload = {
  share: StoredShare;
  uploadToken: string;
};

class StoredHTTPError extends Error {
  constructor(
    message: string,
    readonly status: number,
  ) {
    super(message);
  }
}

function checkAbort(signal?: AbortSignal) {
  if (signal?.aborted) throw new DOMException("Transfer cancelled", "AbortError");
}

function base64URL(bytes: Uint8Array) {
  let binary = "";
  for (let offset = 0; offset < bytes.byteLength; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(offset, offset + 0x8000));
  }
  return btoa(binary).replaceAll("+", "-").replaceAll("/", "_").replace(/=+$/, "");
}

function fromBase64URL(value: string) {
  const normalized = value.replaceAll("-", "+").replaceAll("_", "/");
  const padded = normalized + "=".repeat((4 - (normalized.length % 4)) % 4);
  const binary = atob(padded);
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

function isCapability(value: string) {
  try {
    const bytes = fromBase64URL(value);
    return bytes.byteLength === 32 && base64URL(bytes) === value;
  } catch {
    return false;
  }
}

function normalizeOrigin(value: string) {
  const parsed = new URL(value);
  const loopback =
    parsed.hostname === "localhost" ||
    parsed.hostname === "127.0.0.1" ||
    parsed.hostname === "[::1]" ||
    parsed.hostname === "::1";
  if (
    (parsed.protocol !== "https:" && !(parsed.protocol === "http:" && loopback)) ||
    parsed.username ||
    parsed.password ||
    (parsed.pathname !== "/" && parsed.pathname !== "") ||
    parsed.search ||
    parsed.hash
  ) {
    throw new Error("Stored-transfer origin must contain only an HTTPS scheme and host");
  }
  return parsed.origin;
}

function validateShare(share: StoredShare) {
  if (!/^[A-Za-z0-9_-]{22}$/.test(share.id)) {
    throw new Error("Invalid stored-transfer id");
  }
  if (share.key.byteLength !== storedKeyBytes) {
    throw new Error("Invalid stored-transfer key");
  }
  share.origin = normalizeOrigin(share.origin);
  return share;
}

export function formatStoredBrowserURL(share: StoredShare) {
  validateShare(share);
  return `${share.origin}/s/${share.id}#v1.${base64URL(share.key)}`;
}

export function formatStoredCLIToken(share: StoredShare) {
  validateShare(share);
  return [
    storedProtocol,
    base64URL(textEncoder.encode(share.origin)),
    share.id,
    base64URL(share.key),
  ].join(".");
}

export function parseStoredShare(value: string): StoredShare {
  const trimmed = value.trim();
  if (trimmed.startsWith(`${storedProtocol}.`)) {
    const parts = trimmed.split(".");
    if (parts.length !== 4) throw new Error("Invalid stored-transfer token");
    return validateShare({
      origin: textDecoder.decode(fromBase64URL(parts[1])),
      id: parts[2],
      key: fromBase64URL(parts[3]),
    });
  }
  const parsed = new URL(trimmed);
  const match = parsed.pathname.match(/^\/s\/([A-Za-z0-9_-]{22})$/);
  if (
    !match ||
    parsed.username ||
    parsed.password ||
    parsed.search ||
    !parsed.hash.startsWith("#v1.")
  ) {
    throw new Error("Invalid stored-transfer URL");
  }
  return validateShare({
    origin: parsed.origin,
    id: match[1],
    key: fromBase64URL(parsed.hash.slice(4)),
  });
}

export function storedShareFromLocation(location: Location = window.location) {
  if (!/^\/s\/[A-Za-z0-9_-]{22}$/.test(location.pathname)) return undefined;
  if (!location.hash.startsWith("#v1.")) return undefined;
  return parseStoredShare(location.href);
}

export function isStoredShareValue(value: string) {
  const trimmed = value.trim();
  if (trimmed.startsWith(`${storedProtocol}.`)) return true;
  try {
    const parsed = new URL(trimmed);
    return /^\/s\/[A-Za-z0-9_-]{22}$/.test(parsed.pathname);
  } catch {
    return false;
  }
}

function api(settings: StoredSettings, suffix: string) {
  return `${settings.storeAPI.replace(/\/$/, "")}/transfers${suffix}`;
}

async function responseError(response: Response) {
  const message = (await response.text()).trim();
  if (response.status === 410) {
    return new StoredHTTPError(
      "This stored transfer has expired or has no downloads remaining",
      response.status,
    );
  }
  if (response.status === 423) {
    return new StoredHTTPError(
      "This stored transfer is currently being downloaded",
      response.status,
    );
  }
  if (response.status === 429) {
    return new StoredHTTPError(
      "Too many stored uploads; please try again later",
      response.status,
    );
  }
  if (response.status === 507) {
    return new StoredHTTPError(
      "The temporary storage service is full",
      response.status,
    );
  }
  return new StoredHTTPError(
    message || `Stored-transfer service returned ${response.status}`,
    response.status,
  );
}

async function authorizedFetch(
  input: string,
  token: string,
  init: RequestInit = {},
) {
  const response = await fetch(input, {
    ...init,
    redirect: "error",
    headers: {
      ...init.headers,
      Authorization: `Bearer ${token}`,
    },
  });
  if (!response.ok) throw await responseError(response);
  return response;
}

export async function prepareStoredFiles(
  selected: File[],
  settings: StoredSettings,
  callbacks: { onStatus?(status: string): void } = {},
  signal?: AbortSignal,
  hashProvider?: FileHashProvider,
) {
  if (selected.length === 0) throw new Error("Choose at least one file");
  if (selected.length > settings.maxFiles) {
    throw new Error(`Stored transfers can contain at most ${settings.maxFiles} files`);
  }
  const total = selected.reduce((sum, file) => sum + file.size, 0);
  if (!Number.isSafeInteger(total) || total > settings.maxTransferBytes) {
    throw new Error(`Stored transfer exceeds the ${settings.maxTransferBytes} byte limit`);
  }
  const names = new Set<string>();
  const prepared: StoredPreparedFile[] = [];
  let firstChunk = 0;
  for (let index = 0; index < selected.length; index += 1) {
    const file = selected[index];
    const name = normalizeOutgoingFileName(file.name);
    if (names.has(name)) throw new Error(`Duplicate filename: ${name}`);
    names.add(name);
    callbacks.onStatus?.(`Hashing ${index + 1}/${selected.length}: ${name}`);
    const chunkCount = Math.ceil(file.size / storedChunkSize);
    prepared.push({
      file,
      name,
      size: file.size,
      hash: new Uint8Array(),
      sha256: hashProvider
        ? await waitForHash(hashProvider(file), signal)
        : await hashFileContents(file, "sha256", signal),
      modified: new Date(file.lastModified).toISOString(),
      firstChunk,
      chunkCount,
    });
    firstChunk += chunkCount;
  }
  return prepared;
}

function manifestFor(files: StoredPreparedFile[]): StoredManifest {
  return {
    v: 1,
    cs: storedChunkSize,
    f: files.map((file) => ({
      n: file.name,
      s: file.size,
      m: file.modified,
      h: base64URL(file.sha256),
      fc: file.firstChunk,
      cc: file.chunkCount,
    })),
  };
}

async function digestHeader(payload: Uint8Array) {
  return base64URL(
    new Uint8Array(
      await crypto.subtle.digest("SHA-256", Uint8Array.from(payload).buffer),
    ),
  );
}

async function putCiphertext(
  target: string,
  token: string,
  payload: Uint8Array,
  signal?: AbortSignal,
) {
  let lastError: unknown;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    checkAbort(signal);
    try {
      const response = await fetch(target, {
        method: "PUT",
        redirect: "error",
        signal,
        headers: {
          Authorization: `Bearer ${token}`,
          "Content-Type": "application/octet-stream",
          "X-Croc-SHA256": await digestHeader(payload),
        },
        body: payload.slice(),
      });
      if (!response.ok) throw await responseError(response);
      return;
    } catch (error) {
      lastError = error;
      if (error instanceof DOMException && error.name === "AbortError") throw error;
      await new Promise((resolve) => window.setTimeout(resolve, (attempt + 1) * 250));
    }
  }
  throw lastError;
}

function planStoredUpload(files: StoredPreparedFile[]): StoredUploadPlan {
  const manifest = manifestFor(files);
  const chunkBytes: number[] = [];
  for (const file of files) {
    let remaining = file.size;
    for (let chunk = 0; chunk < file.chunkCount; chunk += 1) {
      const size = Math.min(remaining, storedChunkSize);
      chunkBytes.push(size + 28);
      remaining -= size;
    }
  }
  return {
    files,
    manifestJSON: textEncoder.encode(JSON.stringify(manifest)),
    chunkBytes,
    totalSize: files.reduce((sum, file) => sum + file.size, 0),
  };
}

async function createStoredUpload(
  key: Uint8Array,
  plan: StoredUploadPlan,
  downloads: number,
  expiresSeconds: number,
  settings: StoredSettings,
  callbacks: StoredUploadCallbacks,
  signal?: AbortSignal,
): Promise<CreatedStoredUpload> {
  const redeem = await wasm().storeRedeemCapability(key);
  const redeemVerifier = new Uint8Array(
    await crypto.subtle.digest("SHA-256", Uint8Array.from(redeem).buffer),
  );
  callbacks.onStatus?.("Reserving encrypted temporary storage…");
  const response = await fetch(api(settings, ""), {
    method: "POST",
    redirect: "error",
    signal,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      protocol: storedProtocol,
      manifestBytes: plan.manifestJSON.byteLength + 28,
      chunkBytes: plan.chunkBytes,
      redeemVerifier: base64URL(redeemVerifier),
      files: plan.files.length,
      plaintextBytes: plan.totalSize,
      ...(downloads === 1 ? {} : { downloads }),
      ...(expiresSeconds === 24 * 60 * 60 ? {} : { expiresSeconds }),
    }),
  });
  if (!response.ok) throw await responseError(response);
  const created = (await response.json()) as {
    id: string;
    uploadToken: string;
    uploadExpiresAt: string;
    chunkSize: number;
  };
  if (created.chunkSize !== storedChunkSize) {
    throw new Error(
      `Storage service returned unsupported chunk size ${created.chunkSize}`,
    );
  }
  if (!isCapability(created.uploadToken)) {
    throw new Error("Storage service returned an invalid upload capability");
  }
  const downloadsHeader = response.headers.get("X-Croc-Downloads");
  const acceptedDownloads =
    downloadsHeader === null ? 1 : Number(downloadsHeader);
  if (!Number.isSafeInteger(acceptedDownloads) || acceptedDownloads < 1) {
    throw new Error("Storage service returned an invalid download count");
  }
  if (acceptedDownloads !== downloads) {
    throw new Error(
      `Storage service created ${acceptedDownloads} downloads instead of ${downloads}`,
    );
  }
  return {
    share: validateShare({
      origin: window.location.origin,
      id: created.id,
      key,
    }),
    uploadToken: created.uploadToken,
  };
}

async function uploadStoredManifest(
  plan: StoredUploadPlan,
  created: CreatedStoredUpload,
  settings: StoredSettings,
  callbacks: StoredUploadCallbacks,
  signal?: AbortSignal,
) {
  callbacks.onStatus?.("Uploading encrypted manifest…");
  const ciphertext = await wasm().storeSealManifest(
    created.share.key,
    created.share.id,
    plan.manifestJSON,
  );
  await putCiphertext(
    api(settings, `/${created.share.id}/manifest`),
    created.uploadToken,
    ciphertext,
    signal,
  );
}

async function uploadStoredChunks(
  plan: StoredUploadPlan,
  created: CreatedStoredUpload,
  settings: StoredSettings,
  callbacks: StoredUploadCallbacks,
  signal?: AbortSignal,
) {
  let totalBytes = 0;
  for (let fileIndex = 0; fileIndex < plan.files.length; fileIndex += 1) {
    const file = plan.files[fileIndex];
    let fileBytes = 0;
    for (let fileChunk = 0; fileChunk < file.chunkCount; fileChunk += 1) {
      checkAbort(signal);
      const position = fileChunk * storedChunkSize;
      const plaintext = new Uint8Array(
        await file.file
          .slice(position, position + storedChunkSize)
          .arrayBuffer(),
      );
      const objectIndex = file.firstChunk + fileChunk;
      const ciphertext = await wasm().storeSealChunk(
        created.share.key,
        created.share.id,
        objectIndex,
        fileIndex,
        fileChunk,
        plaintext.byteLength,
        plaintext,
      );
      callbacks.onStatus?.(`Uploading ${file.name}`);
      await putCiphertext(
        api(settings, `/${created.share.id}/chunks/${objectIndex}`),
        created.uploadToken,
        ciphertext,
        signal,
      );
      fileBytes += plaintext.byteLength;
      totalBytes += plaintext.byteLength;
      callbacks.onProgress?.({
        fileIndex,
        fileCount: plan.files.length,
        fileName: file.name,
        fileBytes,
        fileSize: file.size,
        totalBytes,
        totalSize: plan.totalSize,
      });
    }
  }
}

async function completeStoredUpload(
  created: CreatedStoredUpload,
  settings: StoredSettings,
  callbacks: StoredUploadCallbacks,
  signal?: AbortSignal,
) {
  callbacks.onStatus?.("Finalizing encrypted transfer…");
  const response = await authorizedFetch(
    api(settings, `/${created.share.id}/complete`),
    created.uploadToken,
    { method: "POST", signal },
  );
  const result = (await response.json()) as { expiresAt: string };
  if (!Number.isFinite(Date.parse(result.expiresAt))) {
    throw new Error("Storage service returned an invalid expiration time");
  }
  return result.expiresAt;
}

export async function uploadStoredFiles(options: {
  files: StoredPreparedFile[];
  settings: StoredSettings;
  downloads?: number;
  expiresSeconds?: number;
  callbacks?: StoredUploadCallbacks;
  signal?: AbortSignal;
}) {
  const {
    files,
    settings,
    downloads = 1,
    expiresSeconds = 24 * 60 * 60,
    callbacks = {},
    signal,
  } = options;
  if (!Number.isSafeInteger(downloads) || downloads < 1) {
    throw new Error("Stored-transfer downloads must be a positive integer");
  }
  if (downloads > settings.maxDownloads) {
    throw new Error(
      `Stored transfers can allow at most ${settings.maxDownloads} downloads`,
    );
  }
  if (
    !Number.isSafeInteger(expiresSeconds) ||
    expiresSeconds < 60 ||
    expiresSeconds > 9_223_372_036
  ) {
    throw new Error(
      "Stored-transfer expiration must be a whole number of seconds of at least one minute",
    );
  }
  if (
    settings.maxExpiresSeconds > 0 &&
    expiresSeconds > settings.maxExpiresSeconds
  ) {
    throw new Error(
      `Stored transfers can expire after at most ${settings.maxExpiresSeconds} seconds`,
    );
  }
  const key = await wasm().storeGenerateKey();
  const plan = planStoredUpload(files);
  const created = await createStoredUpload(
    key,
    plan,
    downloads,
    expiresSeconds,
    settings,
    callbacks,
    signal,
  );
  let finalized = false;
  try {
    await uploadStoredManifest(plan, created, settings, callbacks, signal);
    await uploadStoredChunks(plan, created, settings, callbacks, signal);
    const expiresAt = await completeStoredUpload(
      created,
      settings,
      callbacks,
      signal,
    );
    finalized = true;
    return {
      share: created.share,
      uploadToken: created.uploadToken,
      expiresAt,
      browserURL: formatStoredBrowserURL(created.share),
      cliToken: formatStoredCLIToken(created.share),
      downloads,
    } satisfies StoredUploadResult;
  } finally {
    if (!finalized) {
      void revokeStoredTransfer(
        created.share,
        created.uploadToken,
        settings,
      ).catch(() => undefined);
    }
  }
}

function offerFromManifest(manifest: StoredManifest): TransferOffer {
  const files = manifest.f.map((file) => ({
    name: file.n,
    folder: "./",
    path: file.n,
    size: file.s,
    hash: fromBase64URL(file.h),
    modified: file.m,
    mode: 0o600,
  }));
  return {
    kind: "files",
    files,
    emptyFolders: [],
    totalSize: files.reduce((sum, file) => sum + file.size, 0),
    senderMachineID: "encrypted temporary storage",
    noCompress: true,
    perFileCompression: false,
  };
}

export async function inspectStoredTransfer(
  share: StoredShare,
  settings: StoredSettings,
  signal?: AbortSignal,
) {
  const redeem = await wasm().storeRedeemCapability(share.key);
  const response = await authorizedFetch(
    api(settings, `/${share.id}/manifest`),
    base64URL(redeem),
    { signal },
  );
  const ciphertext = new Uint8Array(await response.arrayBuffer());
  if (ciphertext.byteLength > maxManifestCiphertext) {
    throw new Error("Stored-transfer manifest is too large");
  }
  const plaintext = await wasm().storeOpenManifest(
    share.key,
    share.id,
    ciphertext,
    settings.maxTransferBytes,
  );
  const manifest = JSON.parse(textDecoder.decode(plaintext)) as StoredManifest;
  return {
    share,
    manifest,
    offer: offerFromManifest(manifest),
    expiresAt: response.headers.get("X-Croc-Expires-At") ?? undefined,
  } satisfies StoredInspection;
}

function claimSessionKey(id: string) {
  return `croc-store-claim:${id}`;
}

function verifiedSessionKey(id: string) {
  return `croc-store-verified:${id}`;
}

function forgetClaim(id: string) {
  try {
    sessionStorage.removeItem(claimSessionKey(id));
  } catch {
    // Session persistence is optional.
  }
}

function hasVerifiedDownload(id: string) {
  try {
    return sessionStorage.getItem(verifiedSessionKey(id)) === "true";
  } catch {
    return false;
  }
}

function rememberVerifiedDownload(id: string) {
  try {
    sessionStorage.setItem(verifiedSessionKey(id), "true");
  } catch {
    // Commit retries remain best-effort without session persistence.
  }
}

function forgetVerifiedDownload(id: string) {
  try {
    sessionStorage.removeItem(verifiedSessionKey(id));
  } catch {
    // Session persistence is optional.
  }
}

async function claimStored(
  inspection: StoredInspection,
  settings: StoredSettings,
  signal?: AbortSignal,
) {
  try {
    const existing = sessionStorage.getItem(claimSessionKey(inspection.share.id));
    if (existing) return existing;
  } catch {
    // Session persistence is optional.
  }
  const redeem = await wasm().storeRedeemCapability(inspection.share.key);
  const response = await authorizedFetch(
    api(settings, `/${inspection.share.id}/claim`),
    base64URL(redeem),
    { method: "POST", signal },
  );
  const claimed = (await response.json()) as { claimToken: string };
  if (!isCapability(claimed.claimToken)) {
    throw new Error("Storage service returned an invalid claim capability");
  }
  try {
    sessionStorage.setItem(claimSessionKey(inspection.share.id), claimed.claimToken);
  } catch {
    // The claim remains valid in memory.
  }
  return claimed.claimToken;
}

type StoredReceiveSession = {
  inspection: StoredInspection;
  settings: StoredSettings;
  callbacks: ReceiveCallbacks;
  signal?: AbortSignal;
  claimToken: string;
  totalBytes: number;
};

function isStaleClaim(error: unknown) {
  return (
    error instanceof StoredHTTPError &&
    (error.status === 404 || error.status === 410)
  );
}

async function withFreshClaim<T>(
  session: StoredReceiveSession,
  operation: (token: string) => Promise<T>,
) {
  try {
    return await operation(session.claimToken);
  } catch (error) {
    if (!isStaleClaim(error)) throw error;
    forgetClaim(session.inspection.share.id);
    session.claimToken = await claimStored(
      session.inspection,
      session.settings,
      session.signal,
    );
    return operation(session.claimToken);
  }
}

async function downloadStoredFile(
  session: StoredReceiveSession,
  destination: ReceiveDestination,
  fileIndex: number,
) {
  const { inspection, settings, callbacks, signal } = session;
  const storedFile = inspection.manifest.f[fileIndex];
  const offered = inspection.offer.files[fileIndex];
  const sink = await destination.openFile(offered);
  let fileBytes = 0;
  try {
    for (let fileChunk = 0; fileChunk < storedFile.cc; fileChunk += 1) {
      checkAbort(signal);
      const objectIndex = storedFile.fc + fileChunk;
      const position = fileChunk * storedChunkSize;
      const plainSize = Math.min(storedFile.s - position, storedChunkSize);
      callbacks.onStatus?.(`Downloading ${storedFile.n}`);
      const response = await withFreshClaim(session, (token) =>
        authorizedFetch(
          api(settings, `/${inspection.share.id}/chunks/${objectIndex}`),
          token,
          { signal },
        ),
      );
      const ciphertext = new Uint8Array(await response.arrayBuffer());
      const plaintext = await wasm().storeOpenChunk(
        inspection.share.key,
        inspection.share.id,
        objectIndex,
        fileIndex,
        fileChunk,
        plainSize,
        ciphertext,
      );
      await sink.writeAt(position, plaintext);
      fileBytes += plaintext.byteLength;
      session.totalBytes += plaintext.byteLength;
      callbacks.onProgress?.({
        fileIndex,
        fileCount: inspection.manifest.f.length,
        fileName: storedFile.n,
        fileBytes,
        fileSize: storedFile.s,
        totalBytes: session.totalBytes,
        totalSize: inspection.offer.totalSize,
      });
    }
    await sink.finalize();
    callbacks.onStatus?.(`Verifying ${storedFile.n}`);
    await verifySinkSHA256(sink, fromBase64URL(storedFile.h));
    await sink.commit();
    callbacks.onFileComplete?.(storedFile.n);
  } catch (error) {
    await sink.abort();
    throw error;
  }
}

async function commitStoredDownload(session: StoredReceiveSession) {
  const { inspection, settings, callbacks, signal } = session;
  callbacks.onStatus?.("Committing verified download…");
  let response: Response | undefined;
  let lastError: unknown;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      response = await withFreshClaim(session, (token) =>
        authorizedFetch(
          api(settings, `/${inspection.share.id}/commit`),
          token,
          { method: "POST", signal },
        ),
      );
      break;
    } catch (error) {
      lastError = error;
      if (
        (error instanceof DOMException && error.name === "AbortError") ||
        (error instanceof StoredHTTPError && error.status < 500)
      ) {
        throw error;
      }
      await new Promise((resolve) =>
        window.setTimeout(resolve, (attempt + 1) * 250),
      );
    }
  }
  if (!response) throw lastError;
  forgetClaim(inspection.share.id);
  forgetVerifiedDownload(inspection.share.id);
  const header = response.headers.get("X-Croc-Downloads-Remaining");
  if (header === null) return 0;
  const remaining = Number(header);
  if (!Number.isSafeInteger(remaining) || remaining < 0) {
    throw new Error(
      "Storage service returned an invalid remaining-download count",
    );
  }
  return remaining;
}

export async function receiveStoredTransfer(options: {
  inspection: StoredInspection;
  settings: StoredSettings;
  callbacks: ReceiveCallbacks;
  signal?: AbortSignal;
}) {
  const { inspection, settings, callbacks, signal } = options;

  if (hasVerifiedDownload(inspection.share.id)) {
    const session: StoredReceiveSession = {
      inspection,
      settings,
      callbacks,
      signal,
      claimToken: await claimStored(inspection, settings, signal),
      totalBytes: inspection.offer.totalSize,
    };
    return commitStoredDownload(session);
  }

  const destination = await callbacks.onOffer(inspection.offer);
  if (!destination) throw new Error("Transfer refused");
  const session: StoredReceiveSession = {
    inspection,
    settings,
    callbacks,
    signal,
    claimToken: await claimStored(inspection, settings, signal),
    totalBytes: 0,
  };
  for (
    let fileIndex = 0;
    fileIndex < inspection.manifest.f.length;
    fileIndex += 1
  ) {
    await downloadStoredFile(session, destination, fileIndex);
  }
  rememberVerifiedDownload(inspection.share.id);
  return commitStoredDownload(session);
}

export async function revokeStoredTransfer(
  share: StoredShare,
  uploadToken: string,
  settings: StoredSettings,
) {
  await authorizedFetch(api(settings, `/${share.id}`), uploadToken, {
    method: "DELETE",
  });
}

export function storedErrorMessage(error: unknown) {
  return errorMessage(error);
}
