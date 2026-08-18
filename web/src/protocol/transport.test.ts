import { describe, expect, it } from "vitest";
import { gatewayForPort } from "./transport";

describe("WebSocket gateway routing", () => {
  it("includes the selected relay index and port", () => {
    expect(gatewayForPort("https://example.test/ws", 2, "9009")).toBe(
      "wss://example.test/ws?relay=2&port=9009",
    );
  });

  it("rejects invalid relay indexes", () => {
    expect(() => gatewayForPort("/ws", -1, "9009")).toThrow(
      "Relay index must be a non-negative integer",
    );
  });
});
