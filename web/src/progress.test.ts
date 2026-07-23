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
    });

    now = 2_000;
    expect(estimator.update(98_304, 131_072)).toEqual({
      bytesPerSecond: 32_768,
      etaMilliseconds: 1_000,
    });

    now = 3_000;
    expect(estimator.update(131_072, 131_072)).toEqual({
      bytesPerSecond: 32_768,
      etaMilliseconds: 0,
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

  it("formats short and long arrival times", () => {
    expect(formatEta(0)).toBe("0s");
    expect(formatEta(999)).toBe("1s");
    expect(formatEta(61_000)).toBe("1m 1s");
    expect(formatEta(3_661_000)).toBe("1h 1m");
    expect(formatEta(Number.NaN)).toBe("—");
  });
});
