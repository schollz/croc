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
  private buffer = new Uint8Array();

  push(chunk: Uint8Array) {
    this.buffer =
      this.buffer.byteLength === 0 ? chunk.slice() : concatBytes(this.buffer, chunk);
    const messages: Uint8Array[] = [];

    while (this.buffer.byteLength >= 8) {
      for (let index = 0; index < MAGIC.byteLength; index += 1) {
        if (this.buffer[index] !== MAGIC[index]) {
          this.buffer = new Uint8Array();
          throw new Error("Relay stream did not start with croc framing");
        }
      }
      const length = new DataView(
        this.buffer.buffer,
        this.buffer.byteOffset,
        this.buffer.byteLength,
      ).getUint32(4, true);
      if (length > MAX_FRAME_SIZE) {
        this.buffer = new Uint8Array();
        throw new Error(`Relay frame is too large (${length} bytes)`);
      }
      if (this.buffer.byteLength < length + 8) break;
      messages.push(this.buffer.slice(8, length + 8));
      this.buffer = this.buffer.slice(length + 8);
    }

    return messages;
  }
}
