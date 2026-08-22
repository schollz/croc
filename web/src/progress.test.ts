import { describe, expect, it } from "vitest";
import { formatEta, TransferEstimator } from "./progress";

describe("transfer estimation", () => {
  it("uses transferred bytes to calculate bytes per second and ETA", () => {
    let now = 0;
    const estimator = new TransferEstimator(() => now);

    expect(estimator.update(32_768, 131_072)).toBeUndefined();

    now = 1_000;
    expect(estimator.update(65_536, 131_072)).toEqual({
      bytesPerSecond: 32_768,
      etaMilliseconds: 2_000,
      elapsedMilliseconds: 1_000,
    });

    now = 2_000;
    expect(estimator.update(98_304, 131_072)).toEqual({
      bytesPerSecond: 32_768,
      etaMilliseconds: 1_000,
      elapsedMilliseconds: 2_000,
    });

    now = 3_000;
    expect(estimator.update(131_072, 131_072)).toEqual({
      bytesPerSecond: 32_768,
      etaMilliseconds: 0,
      elapsedMilliseconds: 3_000,
    });
  });

  it("includes the first chunk in the completed average", () => {
    let now = 5_000;
    const estimator = new TransferEstimator(() => now);

    expect(estimator.update(0, 100_000)).toBeUndefined();
    now = 7_500;

    expect(estimator.update(100_000, 100_000)).toEqual({
      bytesPerSecond: 40_000,
      etaMilliseconds: 0,
      elapsedMilliseconds: 2_500,
    });
  });

  it("uses the explicit start when the zero-byte render is coalesced", () => {
    let now = 20_000;
    const estimator = new TransferEstimator(() => now);

    expect(estimator.update(100_000, 100_000, 5_000, 7_500)).toEqual({
      bytesPerSecond: 40_000,
      etaMilliseconds: 0,
      elapsedMilliseconds: 2_500,
    });

    now = 30_000;
    expect(estimator.update(100_000, 100_000, 5_000, 7_500)).toEqual({
      bytesPerSecond: 40_000,
      etaMilliseconds: 0,
      elapsedMilliseconds: 2_500,
    });
  });

  it("resets when a new transfer starts", () => {
    let now = 0;
    const estimator = new TransferEstimator(() => now);
    estimator.update(100, 1_000);
    now = 1_000;
    expect(estimator.update(200, 1_000)).toBeDefined();
    expect(estimator.update(10, 500)).toBeUndefined();
  });

  it("ignores invalid progress and timing", () => {
    let now = 1_000;
    const estimator = new TransferEstimator(() => now);

    expect(estimator.update(0, 1_000)).toBeUndefined();
    now = 500;
    expect(estimator.update(500, 1_000)).toBeUndefined();
    expect(estimator.update(Number.NaN, 1_000)).toBeUndefined();
    expect(estimator.update(1_001, 1_000)).toBeUndefined();
  });

  it("formats short and long arrival times", () => {
    expect(formatEta(0)).toBe("0s");
    expect(formatEta(999)).toBe("1s");
    expect(formatEta(61_000)).toBe("1m 1s");
    expect(formatEta(3_661_000)).toBe("1h 1m");
    expect(formatEta(Number.NaN)).toBe("—");
  });
});
