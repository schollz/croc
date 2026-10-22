async function transform(
  input: Uint8Array,
  decompress: boolean,
  limit = Number.MAX_SAFE_INTEGER,
) {
  if (!Number.isSafeInteger(limit) || limit < 0)
    throw new Error(
      "Decompress byte limit must be a non-negative safe integer",
    );
  const stream = decompress
    ? new DecompressionStream("deflate-raw")
    : new CompressionStream("deflate-raw");
  const reader = new Blob([input.slice()])
    .stream()
    .pipeThrough(stream)
    .getReader();
  const chunks: Uint8Array[] = [];
  let total = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      total += value.length;
      if (total > limit) {
        await reader.cancel("Decompressed output exceeds limit");
        throw new Error("Decompressed output exceeds limit");
      }
      chunks.push(value);
    }
    const output = new Uint8Array(total);
    let at = 0;
    for (const chunk of chunks) {
      output.set(chunk, at);
      at += chunk.length;
    }
    return output;
  } finally {
    reader.releaseLock();
  }
}
export const compress = (input: Uint8Array) => transform(input, false);
export const decompress = (input: Uint8Array, limit: number) =>
  transform(input, true, limit);
export function checkProtocolCapabilities() {
  try {
    if (!globalThis.crypto?.subtle) throw new Error("Web Crypto unavailable");
    new CompressionStream("deflate-raw");
    new DecompressionStream("deflate-raw");
  } catch {
    throw new Error(
      "Direct transfers require Web Crypto and raw DEFLATE support. Use a current browser on HTTPS or localhost.",
    );
  }
}
