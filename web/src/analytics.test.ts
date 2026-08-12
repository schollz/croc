import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { trackTransferEvent, transferEvents } from "./analytics";

describe("transfer analytics", () => {
  beforeEach(() => {
    window.history.replaceState({}, "", "/");
  });

  afterEach(() => {
    delete window.umami;
  });

  it.each([
    transferEvents.sendDirect,
    transferEvents.sendWithStorage,
    transferEvents.receive,
  ])("tracks the %s event when Umami is enabled", (event) => {
    const track = vi.fn();
    window.umami = { track };

    trackTransferEvent(event);

    expect(track).toHaveBeenCalledOnce();
    const transform = track.mock.calls[0][0] as (
      properties: Record<string, unknown>,
    ) => Record<string, unknown>;
    expect(typeof transform).toBe("function");
    expect(transform({ url: "/?code=secret#key", website: "site-id" })).toEqual({
      name: event,
      url: "/",
      website: "site-id",
    });
  });

  it("does nothing when Umami is disabled", () => {
    expect(() => trackTransferEvent(transferEvents.receive)).not.toThrow();
  });

  it("does not let tracker failures interrupt a transfer", () => {
    window.umami = {
      track: vi.fn(() => {
        throw new Error("analytics unavailable");
      }),
    };

    expect(() => trackTransferEvent(transferEvents.sendDirect)).not.toThrow();
  });
});
