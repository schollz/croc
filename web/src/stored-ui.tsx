import { useEffect, useRef, useState } from "react";
import { Check, Copy, X } from "lucide-react";
import type { StoredUploadResult } from "./protocol/stored";
import { ShareQRCode } from "./share-qr";

export type SendMode = "direct" | "stored";
export type StoredExpirationUnit = "minutes" | "hours" | "days" | "weeks";

export const maxStoredExpirationSeconds = 9_223_372_036;

const expirationUnits: Array<{
  unit: StoredExpirationUnit;
  seconds: number;
}> = [
  { unit: "minutes", seconds: 60 },
  { unit: "hours", seconds: 60 * 60 },
  { unit: "days", seconds: 24 * 60 * 60 },
  { unit: "weeks", seconds: 7 * 24 * 60 * 60 },
];

function secondsPerUnit(unit: StoredExpirationUnit) {
  return expirationUnits.find((candidate) => candidate.unit === unit)!.seconds;
}

export function storedExpirationSeconds(
  value: number,
  unit: StoredExpirationUnit,
) {
  return value * secondsPerUnit(unit);
}

export function storedExpirationValueLimit(
  unit: StoredExpirationUnit,
  maxExpiresSeconds: number,
) {
  const effectiveMaximum =
    maxExpiresSeconds > 0
      ? Math.min(maxExpiresSeconds, maxStoredExpirationSeconds)
      : maxStoredExpirationSeconds;
  return Math.floor(effectiveMaximum / secondsPerUnit(unit));
}

export function storedExpirationParts(expiresSeconds: number): {
  value: number;
  unit: StoredExpirationUnit;
} {
  const normalized = Math.max(
    60,
    Math.min(Math.floor(expiresSeconds), maxStoredExpirationSeconds),
  );
  for (const candidate of [...expirationUnits].reverse()) {
    if (normalized % candidate.seconds === 0) {
      return { value: normalized / candidate.seconds, unit: candidate.unit };
    }
  }
  return { value: Math.floor(normalized / 60), unit: "minutes" };
}

export function formatStoredExpiration(
  value: number,
  unit: StoredExpirationUnit,
) {
  const singular = unit.slice(0, -1);
  return `${value} ${value === 1 ? singular : unit}`;
}

type StoredModeSwitchProps = {
  mode: SendMode;
  disabled: boolean;
  durationLabel: string;
  onChange(mode: SendMode): void;
};

export function StoredModeSwitch({
  mode,
  disabled,
  durationLabel,
  onChange,
}: StoredModeSwitchProps) {
  return (
    <div className="send-mode-switch" role="group" aria-label="Send mode">
      <button
        type="button"
        className={mode === "direct" ? "active" : ""}
        aria-pressed={mode === "direct"}
        disabled={disabled}
        onClick={() => onChange("direct")}
      >
        Direct
      </button>
      <button
        type="button"
        className={mode === "stored" ? "active" : ""}
        aria-pressed={mode === "stored"}
        disabled={disabled}
        onClick={() => onChange("stored")}
      >
        Store for {durationLabel}
      </button>
    </div>
  );
}

type StoredExpirationControlProps = {
  value: number;
  unit: StoredExpirationUnit;
  maxExpiresSeconds: number;
  disabled: boolean;
  onChange(value: number, unit: StoredExpirationUnit): void;
};

export function StoredExpirationControl({
  value,
  unit,
  maxExpiresSeconds,
  disabled,
  onChange,
}: StoredExpirationControlProps) {
  const maximum = storedExpirationValueLimit(unit, maxExpiresSeconds);
  return (
    <>
      <label className="field-label" htmlFor="stored-expiration-value">
        Storage lifetime
      </label>
      <div className="stored-expiration-control">
        <input
          id="stored-expiration-value"
          aria-label="Storage lifetime"
          type="number"
          min={1}
          max={maximum}
          step={1}
          value={value}
          disabled={disabled}
          onChange={(event) => {
            const next = Number(event.target.value);
            if (Number.isSafeInteger(next) && next > 0) {
              onChange(Math.min(maximum, next), unit);
            }
          }}
        />
        <select
          aria-label="Storage lifetime unit"
          value={unit}
          disabled={disabled}
          onChange={(event) => {
            const nextUnit = event.target.value as StoredExpirationUnit;
            onChange(
              Math.min(
                value,
                storedExpirationValueLimit(nextUnit, maxExpiresSeconds),
              ),
              nextUnit,
            );
          }}
        >
          {expirationUnits.map((candidate) => (
            <option
              key={candidate.unit}
              value={candidate.unit}
              disabled={
                storedExpirationValueLimit(
                  candidate.unit,
                  maxExpiresSeconds,
                ) < 1
              }
            >
              {candidate.unit[0].toUpperCase() + candidate.unit.slice(1)}
            </option>
          ))}
        </select>
      </div>
    </>
  );
}

type StoredShareCardProps = {
  upload: StoredUploadResult;
  onCopy(value: string): Promise<boolean>;
  onRevoke(): void;
};

type CopyTarget = "browser" | "cli";
type CopyResult = "idle" | "copied" | "error";

export function StoredShareCard({
  upload,
  onCopy,
  onRevoke,
}: StoredShareCardProps) {
  const [copyResults, setCopyResults] = useState<Record<CopyTarget, CopyResult>>({
    browser: "idle",
    cli: "idle",
  });
  const copyReset = useRef<Partial<Record<CopyTarget, number>>>({});

  useEffect(() => {
    setCopyResults({ browser: "idle", cli: "idle" });
    return () => {
      for (const timeout of Object.values(copyReset.current)) {
        window.clearTimeout(timeout);
      }
      copyReset.current = {};
    };
  }, [upload.share.id]);

  async function copy(target: CopyTarget, value: string) {
    const copied = await onCopy(value);
    setCopyResults((current) => ({
      ...current,
      [target]: copied ? "copied" : "error",
    }));
    window.clearTimeout(copyReset.current[target]);
    copyReset.current[target] = window.setTimeout(() => {
      setCopyResults((current) => ({ ...current, [target]: "idle" }));
      delete copyReset.current[target];
    }, 2_000);
  }

  function copyButton(target: CopyTarget, value: string) {
    const result = copyResults[target];
    return (
      <button
        type="button"
        className="secondary-button"
        onClick={() => void copy(target, value)}
      >
        {result === "copied" ? <Check /> : <Copy />}
        <span role="status" aria-live="polite">
          {result === "copied"
            ? "Copied"
            : result === "error"
              ? "Copy failed"
              : "Copy"}
        </span>
      </button>
    );
  }

  return (
    <div className="stored-share" aria-live="polite">
      <div className="offer-heading">
        <span>Encrypted link ready</span>
        <span>
          {upload.downloads} verified{" "}
          {upload.downloads === 1 ? "download" : "downloads"}
          {" · "}expires {new Date(upload.expiresAt).toLocaleString()}
        </span>
      </div>
      <label>
        <span>Browser link</span>
        <div className="stored-share-row">
          <input readOnly value={upload.browserURL} />
          {copyButton("browser", upload.browserURL)}
        </div>
      </label>
      <ShareQRCode
        value={upload.browserURL}
        description="Scan with a phone to open this encrypted link and receive the files."
      />
      <label>
        <span>CLI token</span>
        <div className="stored-share-row">
          <input readOnly value={upload.cliToken} />
          {copyButton("cli", upload.cliToken)}
        </div>
      </label>
      <button
        type="button"
        className="secondary-button revoke-button"
        onClick={onRevoke}
      >
        <X /> Revoke now
      </button>
    </div>
  );
}
