import { describe, expect, it } from "vitest";
import { BundleProgress, createBundleProgress } from "./bundle-progress";
import type { FileProgress } from "./types";

describe("bundle progress", () => {
  it("credits out-of-order chunks once across member boundaries", () => {
    const progress = new BundleProgress([
      { index: 2, size: 1, offset: 0 },
      { index: 4, size: 2, offset: 1 },
      { index: 5, size: 3, offset: 3 },
      { index: 8, size: 4, offset: 6 },
    ]);
    expect(progress.add(5, 5)).toEqual([2, 3]);
    expect(progress.add(0, 3)).toEqual([0, 1]);
    expect(progress.bytes).toEqual([1, 2, 1, 4]);
    expect(progress.totalBytes).toBe(8);
    expect(progress.add(5, 5)).toEqual([]);
    expect(progress.totalBytes).toBe(8);
    progress.add(3, 2);
    expect(progress.bytes).toEqual([1, 2, 3, 4]);
    expect(progress.totalBytes).toBe(10);
  });

  it("caches only active member metadata and emits original names and totals", () => {
    const updates: FileProgress[] = [];
    const totals: number[] = [];
    const lookedUp: number[] = [];
    const update = createBundleProgress(
      [3, 7],
      [0, 2],
      1000,
      (index) => {
        lookedUp.push(index);
        return { name: `folder/${index}.txt`, size: 2 };
      },
      10,
      50,
      { onProgress: (progress) => updates.push(progress) },
      (bytes) => totals.push(bytes),
    );
    update(0, 0);
    update(2, 2);
    update(0, 2);
    update(0, 2);
    expect(lookedUp).toEqual([3, 7]);
    expect(totals).toEqual([10, 12, 14, 14]);
    expect(updates).toHaveLength(4);
    expect(updates[2]).toMatchObject({
      fileIndex: 7,
      fileName: "folder/7.txt",
      fileBytes: 2,
      totalBytes: 12,
      fileCount: 1000,
    });
    expect(updates[3]).toMatchObject({
      fileIndex: 3,
      fileBytes: 2,
      totalBytes: 14,
    });
  });
});
