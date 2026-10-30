import { bytesEqual } from "./bytes";
import { BUNDLE_LIMITS } from "./bundle";
import { XXH64 } from "./xxh64";
import type { OfferedFile, ReceiveDestination, ReceivePayload } from "./types";

function checkAbort(signal?: AbortSignal) {
  if (signal?.aborted)
    throw new DOMException("Transfer cancelled", "AbortError");
}

// One bounded, attempt-local payload. It is discarded after its members have
// reached their ordinary destinations; completed bundles are never accumulated.
export class BundleSink implements ReceivePayload {
  private bytes?: Uint8Array<ArrayBuffer>;
  private finalized = false;
  constructor(size: number) {
    if (!Number.isInteger(size) || size <= 0 || size > BUNDLE_LIMITS.maxBytes)
      throw new Error("Invalid browser bundle size");
    this.bytes = new Uint8Array(size);
  }
  async writeAt(position: number, bytes: Uint8Array) {
    if (
      !this.bytes ||
      this.finalized ||
      position < 0 ||
      position + bytes.byteLength > this.bytes.byteLength
    )
      throw new Error("Invalid bundle sink write");
    this.bytes.set(bytes, position);
  }
  async finalize() {
    this.finalized = true;
  }
  async hash(algorithm: "xxhash" | "sha256" = "xxhash", signal?: AbortSignal) {
    checkAbort(signal);
    if (algorithm !== "xxhash" || !this.finalized || !this.bytes)
      throw new Error("Bundle sink is not ready for verification");
    return this.hashRange(0, this.bytes.byteLength, signal);
  }
  private async hashRange(offset: number, size: number, signal?: AbortSignal) {
    if (!this.bytes) throw new Error("Bundle sink has been released");
    const hash = new XXH64();
    for (
      let position = offset;
      position < offset + size;
      position += 32 << 10
    ) {
      checkAbort(signal);
      hash.update(
        this.bytes.subarray(
          position,
          Math.min(offset + size, position + (32 << 10)),
        ),
      );
      if ((position - offset) % (256 << 10) === 0)
        await new Promise<void>((resolve) => setTimeout(resolve, 0));
    }
    return hash.digest();
  }
  async abort() {
    this.bytes = undefined;
  }

  // Verify all members before opening even the first destination. Folder and
  // individual-download behavior is supplied by the existing destination.
  async commitMembers(
    files: OfferedFile[],
    indices: number[],
    offsets: number[],
    destination: ReceiveDestination,
    completed: (index: number) => void,
    signal?: AbortSignal,
  ) {
    if (!this.finalized || !this.bytes)
      throw new Error("Bundle sink is not finalized");
    for (let i = 0; i < indices.length; i++) {
      const file = files[indices[i]];
      if (
        !bytesEqual(
          await this.hashRange(offsets[i], file.size, signal),
          file.hash,
        )
      )
        throw new Error(`Bundle member verification failed: ${file.path}`);
    }
    for (let i = 0; i < indices.length; i++) {
      checkAbort(signal);
      const index = indices[i],
        file = files[index];
      const sink = await destination.openFile(file, signal);
      try {
        for (let position = 0; position < file.size; position += 32 << 10) {
          checkAbort(signal);
          await sink.writeAt(
            position,
            this.bytes.subarray(
              offsets[i] + position,
              offsets[i] + Math.min(file.size, position + (32 << 10)),
            ),
          );
        }
        checkAbort(signal);
        await sink.finalize();
        checkAbort(signal);
        if (!bytesEqual(await sink.hash("xxhash", signal), file.hash))
          throw new Error(`Destination verification failed: ${file.path}`);
        checkAbort(signal);
        await sink.commit();
        completed(index);
      } catch (error) {
        await sink.abort();
        throw error;
      }
    }
  }
}
