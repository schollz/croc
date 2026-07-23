import { Estimation, type TimeFetcher } from "arrival-time";

export type TransferEstimate = {
  bytesPerSecond: number;
  etaMilliseconds: number;
};

export class TransferEstimator {
  private estimation?: Estimation;
  private initialBytes = 0;
  private lastBytes = 0;
  private totalBytes = 0;
  private latest?: TransferEstimate;

  constructor(private timeFetcher?: TimeFetcher) {}

  reset() {
    this.estimation = undefined;
    this.initialBytes = 0;
    this.lastBytes = 0;
    this.totalBytes = 0;
    this.latest = undefined;
  }

  update(transferredBytes: number, totalBytes: number) {
    if (
      !Number.isFinite(transferredBytes) ||
      !Number.isFinite(totalBytes) ||
      transferredBytes < 0 ||
      totalBytes <= 0 ||
      transferredBytes > totalBytes
    ) {
      this.reset();
      return undefined;
    }

    if (
      !this.estimation ||
      totalBytes !== this.totalBytes ||
      transferredBytes < this.lastBytes
    ) {
      this.initialBytes = transferredBytes;
      this.lastBytes = transferredBytes;
      this.totalBytes = totalBytes;
      this.latest = undefined;
      this.estimation = new Estimation({
        progress: 0,
        total: totalBytes - transferredBytes,
        timeFetcher: this.timeFetcher,
      });
      return undefined;
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
      measurement.estimate < 0
    ) {
      return this.latest;
    }

    this.latest = {
      bytesPerSecond: measurement.speed,
      etaMilliseconds:
        transferredBytes === totalBytes ? 0 : measurement.estimate,
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
