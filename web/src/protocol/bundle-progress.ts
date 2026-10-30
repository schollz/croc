import type { TransferCallbacks } from "./types";

type Member = { index: number; size: number; offset: number };

// Data workers report aligned, unique chunks. Retaining positions also makes
// accounting idempotent without rescanning previously credited ranges.
export class BundleProgress {
  readonly bytes: number[];
  totalBytes = 0;
  private positions = new Set<number>();

  constructor(private members: Member[]) {
    this.bytes = members.map(() => 0);
  }

  add(position: number, length: number): number[] {
    if (length === 0) return this.members.map((_, i) => i);
    if (this.positions.has(position)) return [];
    this.positions.add(position);
    const changed: number[] = [];
    for (let i = 0; i < this.members.length; i++) {
      const { offset, size } = this.members[i];
      const count = Math.max(
        0,
        Math.min(offset + size, position + length) - Math.max(offset, position),
      );
      if (count === 0) continue;
      this.bytes[i] += count;
      this.totalBytes += count;
      changed.push(i);
    }
    return changed;
  }
}

export function createBundleProgress(
  indices: number[],
  offsets: number[],
  fileCount: number,
  fileAt: (index: number) => { size: number; name: string },
  beforeBundle: number,
  totalSize: number,
  callbacks: TransferCallbacks,
  onTotal: (bytes: number) => void,
) {
  const members = indices.map((index, i) => ({
    index,
    offset: offsets[i],
    ...fileAt(index),
  }));
  const progress = new BundleProgress(members);
  return (position: number, length: number) => {
    const changed = progress.add(position, length);
    const totalBytes = beforeBundle + progress.totalBytes;
    onTotal(totalBytes);
    for (const i of changed) {
      const member = members[i];
      callbacks.onProgress?.({
        fileIndex: member.index,
        fileCount,
        fileName: member.name,
        fileBytes: progress.bytes[i],
        fileSize: member.size,
        totalBytes,
        totalSize,
      });
    }
  };
}
