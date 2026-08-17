import { concatBytes } from "./bytes";

export const MAX_FRAME_SIZE = 64 * 1024 * 1024;
const MAGIC = new Uint8Array([0x63, 0x72, 0x6f, 0x63]);

export function encodeFrame(payload: Uint8Array) {
  if (payload.byteLength > MAX_FRAME_SIZE) {
    throw new Error(`Message is too large (${payload.byteLength} bytes)`);
  }
  const frame = new Uint8Array(8 + payload.byteLength);
  frame.set(MAGIC, 0);
  new DataView(frame.buffer).setUint32(4, payload.byteLength, true);
  frame.set(payload, 8);
  return frame;
}

export class FrameDecoder {
  private buffer: Uint8Array<ArrayBufferLike> = new Uint8Array();

  push(chunk: Uint8Array) {
    this.buffer =
      this.buffer.byteLength === 0 ? chunk : concatBytes(this.buffer, chunk);
    const messages: Uint8Array[] = [];
    let offset = 0;

    while (this.buffer.byteLength - offset >= 8) {
      for (let index = 0; index < MAGIC.byteLength; index += 1) {
        if (this.buffer[offset + index] !== MAGIC[index]) {
          this.buffer = new Uint8Array();
          throw new Error("Relay stream did not start with croc framing");
        }
      }
      const length = new DataView(
        this.buffer.buffer,
        this.buffer.byteOffset + offset,
        this.buffer.byteLength - offset,
      ).getUint32(4, true);
      if (length > MAX_FRAME_SIZE) {
        this.buffer = new Uint8Array();
        throw new Error(`Relay frame is too large (${length} bytes)`);
      }
      if (this.buffer.byteLength - offset < length + 8) break;
      messages.push(this.buffer.subarray(offset + 8, offset + length + 8));
      offset += length + 8;
    }

    this.buffer =
      offset === this.buffer.byteLength
        ? new Uint8Array()
        : this.buffer.subarray(offset);

    return messages;
  }
}
