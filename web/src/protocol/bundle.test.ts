import { describe, expect, it } from "vitest";
import fixture from "../../../testdata/bundles/v1.json";
import { XXH64 } from "./xxh64";
import {
  BundleControlGuard,
  BUNDLE_FEATURE,
  BUNDLE_LIMITS,
  BundleLifecycle,
  decodeBundleJSON,
  planBundles,
  validateBundleLimits,
  validateBundleRequest,
} from "./bundle";


describe("shared small-file bundle contract", () => {
  it("rejects premature control completion and aborts pending destinations", async () => {
    const guard = new BundleControlGuard(Promise.resolve({ t: "finished" }));
    await expect(guard.failure).rejects.toThrow(/Unexpected peer message/);
    expect(guard.signal.aborted).toBe(true);
    expect(() => guard.check()).toThrow();
    guard.dispose();
  });
  it("accepts a close only after the owner completes member commits", async () => {
    let resolve!: (message: import("./types").CrocMessage) => void;
    const guard = new BundleControlGuard(
      new Promise((done) => {
        resolve = done;
      }),
    );
    guard.allowClosed();
    resolve({ t: "bundle-closed" });
    expect((await guard.next).t).toBe("bundle-closed");
    guard.check();
    guard.dispose();
  });
  it("agrees on raw concatenation, offsets, and XXH64", () => {
    expect(fixture.feature).toBe(BUNDLE_FEATURE);
    let payload = "";
    const hash = (text: string) => {
      const h = new XXH64();
      h.update(new TextEncoder().encode(text));
      return Array.from(h.digest())
        .map((b) => b.toString(16).padStart(2, "0"))
        .join("");
    };
    for (const member of fixture.members) {
      expect(payload.length).toBe(member.offset);
      expect(hash(member.text)).toBe(member.xxh64);
      payload += member.text;
    }
    expect(payload).toBe(fixture.payload);
    expect(hash(payload)).toBe(fixture.xxh64);
  });
  for (const plan of fixture.plans)
    it(plan.name, () => {
      expect(
        planBundles(
          plan.sizes.map((size, index) => ({
            size,
            eligible: plan.eligible[index],
            requested: true,
          })),
          plan.limits,
        ),
      ).toEqual(plan.expected);
    });
  it("rejects all shared invalid requests", () => {
    for (const request of fixture.invalidRequests)
      expect(() =>
        validateBundleRequest(
          {
            ...request,
            limits: BUNDLE_LIMITS,
            MachineID: "web",
            ReconnectVersion: 0,
          },
          [1, 2, 3, 4].map((size) => ({
            size,
            eligible: true,
            requested: true,
          })),
          BUNDLE_LIMITS,
        ),
      ).toThrow();
  });
  it("bounds control JSON and limits", () => {
    expect(() => decodeBundleJSON(new Uint8Array(16385))).toThrow();
    for (const maxBytes of [0, -1, (32 << 20) + 1, Infinity, 1.5])
      expect(() => validateBundleLimits({ maxBytes, maxFiles: 128 })).toThrow();
  });
  it("binds every transition to one monotonically increasing ID", () => {
    const state = new BundleLifecycle();
    expect(() => state.advance(1, "offer")).toThrow();
    state.request(1);
    expect(() => state.request(2)).toThrow();
    expect(() => state.advance(2, "offer")).toThrow();
    expect(() => state.advance(1, "done")).toThrow();
    for (const op of ["offer", "ready", "done", "closed"] as const)
      state.advance(1, op);
    expect(() => state.request(1)).toThrow();
    state.request(2);
  });
});
