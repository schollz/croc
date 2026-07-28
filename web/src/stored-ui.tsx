import { Copy, X } from "lucide-react";
import type { StoredUploadResult } from "./protocol/stored";

export type SendMode = "direct" | "stored";

type StoredModeSwitchProps = {
  mode: SendMode;
  disabled: boolean;
  onChange(mode: SendMode): void;
};

export function StoredModeSwitch({
  mode,
  disabled,
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
        Store for 24 hours
      </button>
    </div>
  );
}

type StoredShareCardProps = {
  upload: StoredUploadResult;
  onCopy(value: string): void;
  onRevoke(): void;
};

export function StoredShareCard({
  upload,
  onCopy,
  onRevoke,
}: StoredShareCardProps) {
  return (
    <div className="stored-share" aria-live="polite">
      <div className="offer-heading">
        <span>Encrypted link ready</span>
        <span>expires {new Date(upload.expiresAt).toLocaleString()}</span>
      </div>
      <label>
        <span>Browser link</span>
        <div className="stored-share-row">
          <input readOnly value={upload.browserURL} />
          <button
            type="button"
            className="secondary-button"
            onClick={() => onCopy(upload.browserURL)}
          >
            <Copy /> Copy
          </button>
        </div>
      </label>
      <label>
        <span>CLI token</span>
        <div className="stored-share-row">
          <input readOnly value={upload.cliToken} />
          <button
            type="button"
            className="secondary-button"
            onClick={() => onCopy(upload.cliToken)}
          >
            <Copy /> Copy
          </button>
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
