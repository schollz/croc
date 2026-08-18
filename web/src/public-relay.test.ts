import { describe, expect, it, vi } from "vitest";
import type { TransferSettings } from "./protocol/types";
import { generateCodeForRelay, selectBestRelay } from "./public-relay";

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
