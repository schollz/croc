import { generateCode } from "./codephrase";
import {
  isRelayConnectionError,
  measureRelayLatency,
} from "./protocol/client";
import type { TransferSettings } from "./protocol/types";
import { wasm } from "./wasm/client";

export const relayProbeTimeoutMs = 1_000;
export const bestRelayCookieName = "croc-best-relay";
export const bestRelayCookieMaxAge = 30 * 24 * 60 * 60;

export type CookieStore = { cookie: string };

function cookieValue(cookieHeader: string, name: string) {
  const prefix = `${name}=`;
  return cookieHeader
    .split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(prefix))
    ?.slice(prefix.length);
}

export function rememberBestRelay(
  address: string,
  cookieStore: CookieStore = document,
  secure = window.location.protocol === "https:",
) {
  cookieStore.cookie = `${bestRelayCookieName}=${encodeURIComponent(address)}; Max-Age=${bestRelayCookieMaxAge}; Path=/; SameSite=Lax${secure ? "; Secure" : ""}`;
}

export function clearBestRelay(
  cookieStore: CookieStore = document,
  secure = window.location.protocol === "https:",
) {
  cookieStore.cookie = `${bestRelayCookieName}=; Max-Age=0; Path=/; SameSite=Lax${secure ? "; Secure" : ""}`;
}

export function loadBestRelay(
  relayAddresses: string[],
  cookieStore: CookieStore = document,
) {
  const encoded = cookieValue(cookieStore.cookie, bestRelayCookieName);
  if (encoded === undefined) return undefined;
  let address: string;
  try {
    address = decodeURIComponent(encoded);
  } catch {
    clearBestRelay(cookieStore);
    return undefined;
  }
  const relayIndex = relayAddresses.indexOf(address);
  if (relayIndex < 0) {
    clearBestRelay(cookieStore);
    return undefined;
  }
  console.debug("croc using cached public relay", { relayIndex, address });
  return relayIndex;
}

export function clearBestRelayOnConnectionError(
  error: unknown,
  cookieStore: CookieStore = document,
) {
  if (!isRelayConnectionError(error)) return false;
  clearBestRelay(cookieStore);
  return true;
}

export type RelayProbe = (
  settings: TransferSettings,
  relayIndex: number,
  timeoutMs: number,
  signal?: AbortSignal,
) => Promise<number>;

export type RelaySelectionOptions = {
  signal?: AbortSignal;
  probe?: RelayProbe;
  cookieStore?: CookieStore;
  onCacheState?: (cached: boolean) => void;
};

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

export async function selectRelayForSend(
  settings: TransferSettings,
  options: RelaySelectionOptions = {},
) {
  const cookieStore = options.cookieStore ?? document;
  const cachedRelay = loadBestRelay(settings.relayAddresses, cookieStore);
  if (cachedRelay !== undefined) {
    options.onCacheState?.(true);
    return cachedRelay;
  }
  options.onCacheState?.(false);
  const relayIndex = await selectBestRelay(
    settings,
    options.signal,
    options.probe ?? measureRelayLatency,
  );
  rememberBestRelay(settings.relayAddresses[relayIndex], cookieStore);
  return relayIndex;
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
