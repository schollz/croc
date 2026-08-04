import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  loadAnalytics,
  saveAnalyticsConsent,
  trackTransferEvent,
  transferEvents,
} from "./analytics";

describe("transfer analytics", () => {
  beforeEach(() => {
    const values = new Map<string, string>();
    Object.defineProperty(window, "localStorage", {
      configurable: true,
      value: {
        clear: () => values.clear(),
        getItem: (key: string) => values.get(key) ?? null,
        key: (index: number) => [...values.keys()][index] ?? null,
        get length() {
          return values.size;
        },
        removeItem: (key: string) => values.delete(key),
        setItem: (key: string, value: string) => values.set(key, value),
      } satisfies Storage,
    });
    window.history.replaceState({}, "", "/");
  });

  afterEach(() => {
    delete window.umami;
    delete window.__CROC_ANALYTICS__;
    document.getElementById("croc-analytics-script")?.remove();
  });

  it.each([
    transferEvents.sendDirect,
    transferEvents.sendWithStorage,
    transferEvents.receive,
  ])("tracks the %s event when Umami is enabled", (event) => {
    const track = vi.fn();
    window.umami = { track };
    saveAnalyticsConsent("accepted");

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
    saveAnalyticsConsent("accepted");
    expect(() => trackTransferEvent(transferEvents.receive)).not.toThrow();
  });

  it("does not track before an explicit opt-in", () => {
    const track = vi.fn();
    window.umami = { track };

    trackTransferEvent(transferEvents.receive);

    expect(track).not.toHaveBeenCalled();
  });

  it("loads the configured tracker only after opt-in", () => {
    window.__CROC_ANALYTICS__ = {
      scriptURL: "https://analytics.example.test/script.js",
      websiteID: "site-id",
    };

    loadAnalytics();
    expect(document.getElementById("croc-analytics-script")).toBeNull();

    saveAnalyticsConsent("accepted");
    loadAnalytics();

    const script = document.getElementById(
      "croc-analytics-script",
    ) as HTMLScriptElement;
    expect(script.src).toBe("https://analytics.example.test/script.js");
    expect(script.dataset.websiteId).toBe("site-id");
    expect(script.dataset.autoTrack).toBe("false");
    expect(script.dataset.excludeSearch).toBe("true");
    expect(script.dataset.excludeHash).toBe("true");
  });

  it("does not let tracker failures interrupt a transfer", () => {
    saveAnalyticsConsent("accepted");
    window.umami = {
      track: vi.fn(() => {
        throw new Error("analytics unavailable");
      }),
    };

    expect(() => trackTransferEvent(transferEvents.sendDirect)).not.toThrow();
  });
});
