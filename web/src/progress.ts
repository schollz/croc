import { defaultClock, Estimation, type TimeFetcher } from "arrival-time";

export type TransferEstimate = {
  bytesPerSecond: number;
  etaMilliseconds: number;
  elapsedMilliseconds: number;
};

export class TransferEstimator {
  private estimation?: Estimation;
  private initialBytes = 0;
  private lastBytes = 0;
  private totalBytes = 0;
  private transferStartTime?: number;
  private sampledAtTime?: number;
  private latest?: TransferEstimate;

  private now = () => this.sampledAtTime ?? this.timeFetcher?.() ?? defaultClock();

  constructor(private timeFetcher?: TimeFetcher) {}

  reset() {
    this.estimation = undefined;
    this.initialBytes = 0;
    this.lastBytes = 0;
    this.totalBytes = 0;
    this.transferStartTime = undefined;
    this.sampledAtTime = undefined;
    this.latest = undefined;
  }

  update(
    transferredBytes: number,
    totalBytes: number,
    transferStartTime?: number,
    sampledAtTime?: number,
  ) {
    if (
      !Number.isFinite(transferredBytes) ||
      !Number.isFinite(totalBytes) ||
      (transferStartTime !== undefined &&
        !Number.isFinite(transferStartTime)) ||
      (sampledAtTime !== undefined && !Number.isFinite(sampledAtTime)) ||
      transferredBytes < 0 ||
      totalBytes <= 0 ||
      transferredBytes > totalBytes
    ) {
      this.reset();
      return undefined;
    }
    this.sampledAtTime = sampledAtTime;

    if (
      !this.estimation ||
      totalBytes !== this.totalBytes ||
      transferredBytes < this.lastBytes ||
      (transferStartTime !== undefined &&
        transferStartTime !== this.transferStartTime)
    ) {
      this.initialBytes = transferStartTime === undefined ? transferredBytes : 0;
      this.lastBytes = this.initialBytes;
      this.totalBytes = totalBytes;
      this.transferStartTime = transferStartTime;
      this.latest = undefined;
      this.estimation = new Estimation({
        progress: 0,
        total: totalBytes - this.initialBytes,
        startTime: transferStartTime,
        timeFetcher: this.now,
      });
      if (transferredBytes === this.initialBytes) return undefined;
    }

    if (transferredBytes === this.lastBytes) return this.latest;

    this.lastBytes = transferredBytes;
    const measurement = this.estimation.update(
      transferredBytes - this.initialBytes,
      totalBytes - this.initialBytes,
    );
    if (
      !Number.isFinite(measurement.speed) ||
      measurement.speed < 0 ||
      !Number.isFinite(measurement.estimate) ||
      measurement.estimate < 0 ||
      !Number.isFinite(measurement.timeDelta) ||
      measurement.timeDelta < 0
    ) {
      return this.latest;
    }

    this.latest = {
      bytesPerSecond: measurement.speed,
      etaMilliseconds:
        transferredBytes === totalBytes ? 0 : measurement.estimate,
      elapsedMilliseconds: measurement.timeDelta,
    };
    return this.latest;
  }
}

export function formatEta(milliseconds: number) {
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return "—";
  if (milliseconds === 0) return "0s";
  const seconds = Math.max(1, Math.ceil(milliseconds / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  if (minutes < 60) return `${minutes}m ${remainingSeconds}s`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}
