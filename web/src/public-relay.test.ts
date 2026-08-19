import { describe, expect, it, vi } from "vitest";
import { RelayConnectionError } from "./protocol/client";
import type { TransferSettings } from "./protocol/types";
import {
  bestRelayCookieMaxAge,
  clearBestRelayOnConnectionError,
  generateCodeForRelay,
  loadBestRelay,
  rememberBestRelay,
  selectBestRelay,
  selectRelayForSend,
} from "./public-relay";

const settings: TransferSettings = {
  gatewayURL: "/ws",
  relayAddresses: ["one:9009", "two:9009", "three:9009"],
  relayPassword: "pass123",
  storeAPI: "/api/v1/store",
};

describe("public relay selection", () => {
  it("uses the first successful probe and cancels the others", async () => {
    const canceled: number[] = [];
    const probe = vi.fn(async (
      _settings: TransferSettings,
      relayIndex: number,
      _timeout: number,
      signal?: AbortSignal,
    ) => {
      if (relayIndex === 1) return 6;
      return await new Promise<number>((_resolve, reject) => {
        signal?.addEventListener("abort", () => {
          canceled.push(relayIndex);
          reject(signal.reason);
        }, { once: true });
      });
    });
    await expect(selectBestRelay(settings, undefined, probe)).resolves.toBe(1);
    expect(probe).toHaveBeenCalledTimes(3);
    expect(canceled.sort()).toEqual([0, 2]);
  });

  it("ignores failures that arrive before a successful probe", async () => {
    const probe = vi.fn(async (_settings: TransferSettings, relayIndex: number) => {
      if (relayIndex !== 2) throw new Error("unavailable");
      return 10;
    });
    await expect(selectBestRelay(settings, undefined, probe)).resolves.toBe(2);
  });

  it("fails when no relay answers successfully", async () => {
    const probe = vi.fn(async () => {
      throw new Error("unavailable");
    });
    await expect(selectBestRelay(settings, undefined, probe)).rejects.toThrow(
      "No public relay available",
    );
  });
});

describe("best relay cookie", () => {
  it("stores the exact address for thirty days with production attributes", () => {
    const cookieStore = { cookie: "" };
    rememberBestRelay("two:9009", cookieStore, true);
    expect(bestRelayCookieMaxAge).toBe(2_592_000);
    expect(cookieStore.cookie).toBe(
      "croc-best-relay=two%3A9009; Max-Age=2592000; Path=/; SameSite=Lax; Secure",
    );
  });

  it("resolves a cached address using the current pool order", () => {
    const cookieStore = { cookie: "other=value; croc-best-relay=two%3A9009" };
    expect(loadBestRelay(settings.relayAddresses, cookieStore)).toBe(1);
    expect(loadBestRelay(["three:9009", "one:9009", "two:9009"], cookieStore)).toBe(2);
    expect(cookieStore.cookie).toBe("other=value; croc-best-relay=two%3A9009");
  });

  it("uses a valid cookie without probing or extending its expiry", async () => {
    const original = "other=value; croc-best-relay=two%3A9009";
    const cookieStore = { cookie: original };
    const probe = vi.fn(async () => 1);
    const cacheStates: boolean[] = [];
    await expect(
      selectRelayForSend(settings, {
        cookieStore,
        probe,
        onCacheState: (cached) => cacheStates.push(cached),
      }),
    ).resolves.toBe(1);
    expect(probe).not.toHaveBeenCalled();
    expect(cacheStates).toEqual([true]);
    expect(cookieStore.cookie).toBe(original);
  });

  it("races and remembers a relay when the cookie is missing", async () => {
    const cookieStore = { cookie: "" };
    const probe = vi.fn(async (_settings: TransferSettings, relayIndex: number) => {
      if (relayIndex === 2) return 4;
      throw new Error("unavailable");
    });
    const cacheStates: boolean[] = [];
    await expect(
      selectRelayForSend(settings, {
        cookieStore,
        probe,
        onCacheState: (cached) => cacheStates.push(cached),
      }),
    ).resolves.toBe(2);
    expect(probe).toHaveBeenCalledTimes(3);
    expect(cacheStates).toEqual([false]);
    expect(cookieStore.cookie).toContain("croc-best-relay=three%3A9009");
    expect(cookieStore.cookie).toContain("Max-Age=2592000");
  });

  it.each(["missing%3A9009", "%E0%A4%A"])(
    "clears invalid cached value %s",
    (value) => {
      const cookieStore = { cookie: `croc-best-relay=${value}` };
      expect(loadBestRelay(settings.relayAddresses, cookieStore)).toBeUndefined();
      expect(cookieStore.cookie).toContain("Max-Age=0");
    },
  );

  it("clears only typed relay connection failures", () => {
    const relayFailureStore = { cookie: "croc-best-relay=two%3A9009" };
    expect(
      clearBestRelayOnConnectionError(
        new RelayConnectionError("unavailable", new Error("dial failed")),
        relayFailureStore,
      ),
    ).toBe(true);
    expect(relayFailureStore.cookie).toContain("Max-Age=0");

    const peerFailureStore = { cookie: "croc-best-relay=two%3A9009" };
    expect(
      clearBestRelayOnConnectionError(
        new Error("recipient disconnected"),
        peerFailureStore,
      ),
    ).toBe(false);
    expect(peerFailureStore.cookie).toBe("croc-best-relay=two%3A9009");
  });
});

describe("relay-matched code generation", () => {
  it("retries normal codes until the requested index matches", async () => {
    const candidates = ["acid-acorn-acre", "poker-hedge-floss"];
    const indexes = new Map([
      ["acid-acorn-acre", 1],
      ["poker-hedge-floss", 2],
    ]);
    await expect(
      generateCodeForRelay(
        2,
        3,
        () => candidates.shift()!,
        async (code) => indexes.get(code)!,
      ),
    ).resolves.toBe("poker-hedge-floss");
  });

  it("validates relay counts and indexes", async () => {
    await expect(generateCodeForRelay(0, 0)).rejects.toThrow(
      "Relay count must be positive",
    );
    await expect(generateCodeForRelay(3, 3)).rejects.toThrow(
      "Relay index is outside the relay pool",
    );
  });
});
