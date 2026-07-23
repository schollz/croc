import { base64ToBytes } from "./bytes";
import type {
  OfferedFile,
  SenderInfoWire,
  TransferOffer,
  WireFileInfo,
} from "./types";

function cleanSegments(value: string) {
  const replaced = value.replaceAll("\\", "/");
  if (replaced.includes("\0")) throw new Error("A remote path contains a null byte");
  const segments: string[] = [];
  for (const segment of replaced.split("/")) {
    if (segment === "" || segment === ".") continue;
    if (segment === "..") throw new Error(`Remote path escapes the destination: ${value}`);
    if ([...segment].some((character) => !/\P{C}/u.test(character))) {
      throw new Error(`Remote path contains a non-printable character: ${value}`);
    }
    segments.push(segment);
  }
  return segments;
}

export function normalizeFolder(value = ".") {
  if (/^(?:[a-zA-Z]:|\/)/.test(value)) {
    throw new Error(`Remote path must be relative: ${value}`);
  }
  const segments = cleanSegments(value);
  const normalized = segments.join("/") || ".";
  if (normalized.includes(".ssh")) {
    throw new Error(`Remote path is not allowed: ${value}`);
  }
  return normalized;
}

export function normalizeFilePath(folderValue: string, nameValue: string) {
  const folder = normalizeFolder(folderValue);
  const nameSegments = cleanSegments(nameValue);
  if (nameSegments.length !== 1 || nameSegments[0] !== nameValue.replaceAll("\\", "/")) {
    throw new Error(`Remote filename must be a basename: ${nameValue}`);
  }
  const name = nameSegments[0];
  if (!name) throw new Error("Remote filename is empty");
  const path = folder === "." ? name : `${folder}/${name}`;
  return { folder, name, path };
}

export function normalizeOutgoingFileName(value: string) {
  // Go's unicode.IsPrint accepts ASCII space but rejects the other Unicode
  // separator characters commonly inserted into filenames by macOS.
  const compatible = value.replace(/\p{Z}+/gu, " ");
  return normalizeFilePath(".", compatible).name;
}

function finiteSize(file: WireFileInfo) {
  const size = file.s ?? 0;
  if (!Number.isSafeInteger(size) || size < 0) {
    throw new Error(`Invalid file size for ${file.n ?? "unnamed file"}`);
  }
  return size;
}

export function validateSenderInfo(info: SenderInfoWire): TransferOffer {
  if (info.SendingText) throw new Error("Text transfers are not supported yet");
  if (info.HashAlgorithm && info.HashAlgorithm !== "xxhash") {
    throw new Error(`Hash algorithm "${info.HashAlgorithm}" is not supported`);
  }

  const destinations = new Set<string>();
  const files: OfferedFile[] = [];
  let totalSize = 0;
  for (const wire of info.FilesToTransfer ?? []) {
    if (wire.sy) throw new Error("Symlink transfers are not supported in the browser");
    const normalized = normalizeFilePath(wire.fr ?? ".", wire.n ?? "");
    if (destinations.has(normalized.path)) {
      throw new Error(`Duplicate destination path: ${normalized.path}`);
    }
    destinations.add(normalized.path);
    const size = finiteSize(wire);
    totalSize += size;
    if (!Number.isSafeInteger(totalSize)) throw new Error("Transfer size is too large");
    files.push({
      ...normalized,
      size,
      hash: wire.h ? base64ToBytes(wire.h) : new Uint8Array(),
      modified: wire.m,
      mode: wire.md,
    });
  }

  const emptyFolders: string[] = [];
  for (const wire of info.EmptyFoldersToTransfer ?? []) {
    const folder = normalizeFolder(wire.fr ?? ".");
    if (destinations.has(folder)) {
      throw new Error(`Duplicate destination path: ${folder}`);
    }
    destinations.add(folder);
    emptyFolders.push(folder);
  }

  return {
    files,
    emptyFolders,
    totalSize,
    senderMachineID: info.MachineID || "unknown",
    noCompress: Boolean(info.NoCompress),
  };
}
