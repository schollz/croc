import { generateCode } from "./codephrase";
import { measureRelayLatency } from "./protocol/client";
import type { TransferSettings } from "./protocol/types";
import { wasm } from "./wasm/client";

export const relayProbeTimeoutMs = 1_000;

export type RelayProbe = (
  settings: TransferSettings,
  relayIndex: number,
  timeoutMs: number,
  signal?: AbortSignal,
) => Promise<number>;

export async function selectBestRelay(
  settings: TransferSettings,
  signal?: AbortSignal,
  probe: RelayProbe = measureRelayLatency,
) {
  if (settings.relayAddresses.length === 0) {
    throw new Error("Public relay pool is empty");
  }
  const controller = new AbortController();
  const abort = () => controller.abort(signal?.reason);
  if (signal?.aborted) abort();
  else signal?.addEventListener("abort", abort, { once: true });
  try {
    return await Promise.any(
      settings.relayAddresses.map(async (address, relayIndex) => {
        const duration = await probe(
          settings,
          relayIndex,
          relayProbeTimeoutMs,
          controller.signal,
        );
        console.debug("croc public relay probe won", {
          relayIndex,
          address,
          duration,
        });
        return relayIndex;
      }),
    );
  } catch (error) {
    if (signal?.aborted) throw signal.reason ?? error;
    throw new Error("No public relay available", { cause: error });
  } finally {
    controller.abort();
    signal?.removeEventListener("abort", abort);
  }
}

export async function generateCodeForRelay(
  relayIndex: number,
  relayCount: number,
  generator: () => string = generateCode,
  indexer: (code: string, count: number) => Promise<number> = (code, count) =>
    wasm().relayIndex(code, count),
) {
  if (!Number.isSafeInteger(relayCount) || relayCount < 1) {
    throw new Error("Relay count must be positive");
  }
  if (!Number.isSafeInteger(relayIndex) || relayIndex < 0 || relayIndex >= relayCount) {
    throw new Error("Relay index is outside the relay pool");
  }
  for (;;) {
    const code = generator();
    if ((await indexer(code, relayCount)) === relayIndex) return code;
  }
}
