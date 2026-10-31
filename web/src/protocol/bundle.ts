import { textDecoder } from "./bytes";
import type { CrocMessage } from "./types";

// The control owner watches for premature completion/errors while workers and
// destinations are busy. It never leaves an invalid control message queued
// until after destination publication.
export class BundleControlGuard {
  private controller = new AbortController();
  private failed?: Error;
  private acceptingClosed = false;
  private rejectFailure!: (error: Error) => void;
  readonly failure: Promise<never>;
  readonly next: Promise<CrocMessage>;
  readonly signal: AbortSignal;
  private abort = () =>
    this.fail(new DOMException("Transfer cancelled", "AbortError"));
  constructor(
    next: Promise<CrocMessage>,
    private parent?: AbortSignal,
  ) {
    this.signal = this.controller.signal;
    this.failure = new Promise<never>((_, reject) => {
      this.rejectFailure = reject;
    });
    void this.failure.catch(() => undefined);
    this.next = next.then(
      (message) => {
        if (!this.acceptingClosed)
          this.fail(
            new Error(
              message.t === "error"
                ? message.m || "Sender cancelled"
                : `Unexpected peer message during bundle payload: ${message.t}`,
            ),
          );
        return message;
      },
      (error) => {
        const failure =
          error instanceof Error ? error : new Error(String(error));
        this.fail(failure);
        throw failure;
      },
    );
    void this.next.catch(() => undefined);
    parent?.addEventListener("abort", this.abort, { once: true });
    if (parent?.aborted) this.abort();
  }
  private fail(error: Error) {
    if (this.failed) return;
    this.failed = error;
    this.controller.abort(error);
    this.rejectFailure(error);
  }
  allowClosed() {
    this.check();
    this.acceptingClosed = true;
  }
  check() {
    if (this.failed) throw this.failed;
  }
  dispose() {
    this.parent?.removeEventListener("abort", this.abort);
  }
}

export const BUNDLE_FEATURE = "small-file-bundles-v1";
export const BUNDLE_MAX_FILE_BYTES = 1 << 20;
export const BUNDLE_MAX_CONTROL_BYTES = 16 << 10;
export const BUNDLE_LIMITS = { maxBytes: 8 << 20, maxFiles: 128 };
export type BundleLimits = typeof BUNDLE_LIMITS;
export type BundleMember = {
  size: number;
  eligible: boolean;
  requested: boolean;
};
export type BundleRequest = {
  id: number;
  indices: number[];
  limits: BundleLimits;
  MachineID: string;
  ExternalIP?: string;
  ReconnectVersion: number;
};
export type BundleOffer = {
  id: number;
  size: number;
  hash: string;
  compressed: boolean;
};

export function validateBundleLimits(value: unknown): BundleLimits {
  const limits = value as BundleLimits | undefined;
  if (
    !limits ||
    !Number.isInteger(limits.maxBytes) ||
    limits.maxBytes <= 0 ||
    limits.maxBytes > 32 << 20 ||
    !Number.isInteger(limits.maxFiles) ||
    limits.maxFiles < 4 ||
    limits.maxFiles > 128
  )
    throw new Error("Invalid bundle limits");
  return limits;
}

export function effectiveBundleLimits(
  a: BundleLimits,
  b: BundleLimits,
): BundleLimits {
  validateBundleLimits(a);
  validateBundleLimits(b);
  return {
    maxBytes: Math.min(a.maxBytes, b.maxBytes),
    maxFiles: Math.min(a.maxFiles, b.maxFiles),
  };
}

export function planBundles(
  members: BundleMember[],
  limits: BundleLimits,
): number[][] {
  validateBundleLimits(limits);
  const units: number[][] = [];
  let group: number[] = [],
    size = 0;
  const flush = () => {
    if (group.length >= 4) units.push(group);
    else for (const index of group) units.push([index]);
    group = [];
    size = 0;
  };
  members.forEach((member, index) => {
    const eligible =
      member.eligible &&
      member.size > 0 &&
      member.size <= BUNDLE_MAX_FILE_BYTES &&
      member.size <= limits.maxBytes;
    if (!eligible) flush();
    if (!member.requested) return;
    if (!eligible) {
      units.push([index]);
      return;
    }
    if (
      group.length === limits.maxFiles ||
      size + member.size > limits.maxBytes
    )
      flush();
    group.push(index);
    size += member.size;
  });
  flush();
  return units;
}

export function decodeBundleJSON(
  bytes: Uint8Array | undefined,
): Record<string, unknown> {
  if (!bytes || bytes.byteLength > BUNDLE_MAX_CONTROL_BYTES)
    throw new Error("Invalid bundle control size");
  const value: unknown = JSON.parse(textDecoder.decode(bytes));
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Invalid bundle control");
  return value as Record<string, unknown>;
}

export function bundleID(id: unknown): number {
  if (
    !Number.isInteger(id) ||
    (id as number) <= 0 ||
    (id as number) > 0xffffffff
  )
    throw new Error("Invalid bundle ID");
  return id as number;
}

export function validateBundleRequest(
  value: Record<string, unknown>,
  members: BundleMember[],
  sender: BundleLimits,
) {
  const allowed = [
    "id",
    "indices",
    "limits",
    "MachineID",
    "ExternalIP",
    "ReconnectVersion",
  ];
  if (Object.keys(value).some((key) => !allowed.includes(key)))
    throw new Error("Unknown bundle request field");
  const id = bundleID(value.id);
  const limits = effectiveBundleLimits(
    sender,
    validateBundleLimits(value.limits),
  );
  const indices = value.indices as number[];
  if (
    !Array.isArray(indices) ||
    indices.length < 4 ||
    indices.length > limits.maxFiles
  )
    throw new Error("Invalid bundle member count");
  let size = 0,
    last = -1;
  const offsets: number[] = [];
  for (const index of indices) {
    if (
      !Number.isInteger(index) ||
      index <= last ||
      index < 0 ||
      index >= members.length
    )
      throw new Error("Invalid, duplicate, or unordered bundle index");
    const member = members[index];
    if (
      !member.eligible ||
      member.size <= 0 ||
      member.size > BUNDLE_MAX_FILE_BYTES ||
      size + member.size > limits.maxBytes
    )
      throw new Error("Ineligible or oversized bundle member");
    for (let j = last + 1; last >= 0 && j < index; j++)
      if (!members[j].eligible)
        throw new Error("Bundle crosses ineligible member");
    offsets.push(size);
    size += member.size;
    last = index;
  }
  if (
    typeof value.MachineID !== "string" ||
    !Number.isInteger(value.ReconnectVersion) ||
    (value.ExternalIP !== undefined && typeof value.ExternalIP !== "string")
  )
    throw new Error("Invalid bundle receiver identity");
  return { request: value as BundleRequest, id, offsets, size };
}

export function validateBundleOffer(
  value: Record<string, unknown>,
  id: number,
  size: number,
  noCompress: boolean,
): BundleOffer {
  if (
    Object.keys(value).some(
      (key) => !["id", "size", "hash", "compressed"].includes(key),
    ) ||
    value.id !== bundleID(id) ||
    value.size !== size ||
    typeof value.compressed !== "boolean" ||
    (noCompress && value.compressed) ||
    typeof value.hash !== "string" ||
    !/^[A-Za-z0-9+/]{11}=$/.test(value.hash)
  )
    throw new Error("Bundle offer does not match request");
  return value as BundleOffer;
}

export function validateBundleAck(value: Record<string, unknown>, id: number) {
  if (Object.keys(value).length !== 1 || value.id !== bundleID(id))
    throw new Error("Bundle acknowledgement does not match request");
}

type BundlePhase = "" | "request" | "offer" | "ready" | "done" | "closed";

export class BundleLifecycle {
  lastID = 0;
  activeID = 0;
  phase: BundlePhase = "";
  request(id: number) {
    bundleID(id);
    if (this.activeID || id <= this.lastID)
      throw new Error("Invalid or reused bundle request ID");
    this.lastID = this.activeID = id;
    this.phase = "request";
  }
  advance(id: number, operation: Exclude<BundlePhase, "" | "request">) {
    const next: Record<string, string> = {
      request: "offer",
      offer: "ready",
      ready: "done",
      done: "closed",
    };
    if (!id || id !== this.activeID || next[this.phase] !== operation)
      throw new Error("Invalid bundle lifecycle transition");
    this.phase = operation;
    if (operation === "closed") {
      this.activeID = 0;
      this.phase = "";
    }
  }
}

