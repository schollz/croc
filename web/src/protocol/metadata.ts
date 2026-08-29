import { base64ToBytes } from "./bytes";
import type {
  OfferedFile,
  SenderInfoWire,
  TransferOffer,
  WireFileInfo,
} from "./types";
import { maxTextTransferBytes } from "./types";

const forbiddenComponents = new Set([".ssh", ".git", ".gnupg"]);
const windowsDeviceNames = new Set([
  "CON",
  "PRN",
  "AUX",
  "NUL",
  "CLOCK$",
  "CONIN$",
  "CONOUT$",
]);

function collisionKey(value: string) {
  // Go uses cases.Fold. NFC + lowercase covers the ASCII filesystem hazards
  // and normalization collisions enforced by the browser client.
  return value.normalize("NFC").toLowerCase();
}

function isWindowsDeviceName(component: string) {
  const dot = component.indexOf(".");
  const stem = (dot === -1 ? component : component.slice(0, dot)).toUpperCase();
  return (
    windowsDeviceNames.has(stem) ||
    /^(?:COM|LPT)(?:[1-9¹²³])$/.test(stem)
  );
}

function validateSegment(raw: string, value: string) {
  const segment = raw.normalize("NFC");
  if ([...segment].some((character) => !/\P{C}/u.test(character))) {
    throw new Error(`Remote path contains a non-printable character: ${value}`);
  }
  if (segment.includes(":")) {
    throw new Error(
      `Remote path contains alternate-data-stream syntax: ${value}`,
    );
  }
  if (segment.endsWith(".") || segment.endsWith(" ")) {
    throw new Error(`Remote path has a Windows-trimmed ending: ${value}`);
  }
  if (isWindowsDeviceName(segment)) {
    throw new Error(`Remote path uses a Windows device name: ${value}`);
  }
  if (forbiddenComponents.has(collisionKey(segment))) {
    throw new Error(`Remote path is not allowed: ${value}`);
  }
  return segment;
}

function cleanSegments(value: string) {
  const replaced = value.replaceAll("\\", "/");
  if (replaced.includes("\0"))
    throw new Error("A remote path contains a null byte");
  if (/^(?:[a-zA-Z]:|\/)/.test(replaced)) {
    throw new Error(`Remote path must be relative: ${value}`);
  }
  const segments: string[] = [];
  for (const raw of replaced.split("/")) {
    if (raw === "" || raw === ".") continue;
    if (raw === "..")
      throw new Error(`Remote path escapes the destination: ${value}`);
    segments.push(validateSegment(raw, value));
  }
  return segments;
}

export function normalizeFolder(value = ".") {
  const segments = cleanSegments(value);
  return segments.join("/") || ".";
}

export function normalizeFilePath(folderValue: string, nameValue: string) {
  const folder = normalizeFolder(folderValue);
  const nameSegments = cleanSegments(nameValue);
  if (
    nameSegments.length !== 1 ||
    nameValue.replaceAll("\\", "/").includes("/")
  ) {
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

type DestinationKind = "file" | "directory";

interface Destination {
  path: string;
  kind: DestinationKind;
}

function addDestination(
  destinations: Map<string, Destination>,
  path: string,
  kind: DestinationKind,
) {
  const key = collisionKey(path);
  const previous = destinations.get(key);
  if (previous) {
    throw new Error(
      `Duplicate destination path: ${path} conflicts with ${previous.path}`,
    );
  }
  destinations.set(key, { path, kind });
}

function validateDestinationAncestors(
  destinations: Map<string, Destination>,
) {
  for (const destination of destinations.values()) {
    const segments = destination.path === "." ? [] : destination.path.split("/");
    for (let length = 1; length < segments.length; length += 1) {
      const ancestorPath = segments.slice(0, length).join("/");
      const ancestor = destinations.get(collisionKey(ancestorPath));
      if (ancestor && ancestor.kind !== "directory") {
        throw new Error(
          `Destination path ${destination.path} is beneath non-directory ${ancestor.path}`,
        );
      }
    }
  }
}

export function validateSenderInfo(info: SenderInfoWire): TransferOffer {
  if (info.HashAlgorithm && info.HashAlgorithm !== "xxhash") {
    throw new Error(`Hash algorithm "${info.HashAlgorithm}" is not supported`);
  }

  const perFileCompression = (info.Features ?? []).includes(
    "per-file-compression-v1",
  );
  const destinations = new Map<string, Destination>();
  const files: OfferedFile[] = [];
  let totalSize = 0;
  for (const wire of info.FilesToTransfer ?? []) {
    if (wire.sy)
      throw new Error("Symlink transfers are not supported in the browser");
    const normalized = normalizeFilePath(wire.fr ?? ".", wire.n ?? "");
    addDestination(destinations, normalized.path, "file");
    const size = finiteSize(wire);
    totalSize += size;
    if (!Number.isSafeInteger(totalSize))
      throw new Error("Transfer size is too large");
    files.push({
      ...normalized,
      size,
      hash: wire.h ? base64ToBytes(wire.h) : new Uint8Array(),
      modified: wire.m,
      mode: wire.md,
      compressed: perFileCompression ? Boolean(wire.c) : !info.NoCompress,
    });
  }

  const emptyFolders: string[] = [];
  for (const wire of info.EmptyFoldersToTransfer ?? []) {
    const folder = normalizeFolder(wire.fr ?? ".");
    addDestination(destinations, folder, "directory");
    emptyFolders.push(folder);
  }
  validateDestinationAncestors(destinations);

  if (info.SendingText) {
    if (
      files.length !== 1 ||
      emptyFolders.length !== 0 ||
      info.TotalNumberFolders !== 0
    ) {
      throw new Error("A text transfer must contain exactly one text payload");
    }
    if (files[0].size === 0) {
      throw new Error("A text transfer cannot be empty");
    }
    if (files[0].size > maxTextTransferBytes) {
      throw new Error("The text transfer is larger than 1 MiB");
    }
    if ((info.FilesToTransfer ?? [])[0]?.tf) {
      throw new Error("A text transfer cannot be an extractable archive");
    }
    if (files[0].folder !== "." || !files[0].name.startsWith("croc-stdin-")) {
      throw new Error(
        "A text transfer must use a croc-stdin- filename in the receive root",
      );
    }
  }

  return {
    kind: info.SendingText ? "text" : "files",
    files,
    emptyFolders,
    totalSize,
    senderMachineID: info.MachineID || "unknown",
    noCompress: Boolean(info.NoCompress),
    perFileCompression,
  };
}
