import { useEffect, useMemo, useRef, useState } from "react";
import {
  AlertTriangle,
  ArrowLeftRight,
  Check,
  Copy,
  Download,
  File as FileIcon,
  Moon,
  RefreshCw,
  Settings2,
  ShieldCheck,
  Sun,
  Upload,
  X,
} from "lucide-react";
import { errorMessage, formatBytes } from "./protocol/bytes";
import {
  prepareFiles,
  receiveFiles,
  sendFiles,
} from "./protocol/client";
import {
  DownloadDestination,
  chooseReceiveDestination,
  supportsDirectoryDestination,
} from "./protocol/storage";
import type {
  FileProgress,
  ReceiveDestination,
  TransferOffer,
  TransferSettings,
} from "./protocol/types";
import {
  formatEta,
  TransferEstimator,
  type TransferEstimate,
} from "./progress";
import { wasm } from "./wasm/client";

type Activity = "idle" | "working" | "done" | "error";
type Theme = "dark" | "light";
type CopyState = "idle" | "copied" | "error";

const runtimeSettings = window.__CROC_RUNTIME_CONFIG__ ?? {};
const defaultSettings: TransferSettings = {
  gatewayURL:
    runtimeSettings.gatewayURL ||
    import.meta.env.VITE_CROC_GATEWAY_URL ||
    "/ws",
  relayAddress:
    runtimeSettings.relayAddress ||
    import.meta.env.VITE_CROC_RELAY_ADDRESS ||
    "croc.schollz.com:9009",
  relayPassword:
    runtimeSettings.relayPassword ||
    import.meta.env.VITE_CROC_RELAY_PASSWORD ||
    "pass123",
};

function storedValue(key: string, fallback: string) {
  try {
    return localStorage.getItem(key) || fallback;
  } catch {
    return fallback;
  }
}

function initialTheme(): Theme {
  try {
    const stored = localStorage.getItem("croc-web-theme");
    if (stored === "light" || stored === "dark") return stored;
  } catch {
    // Use the system preference.
  }
  return window.matchMedia?.("(prefers-color-scheme: light)").matches
    ? "light"
    : "dark";
}

function ProgressBlock({
  progress,
  status,
}: {
  progress?: FileProgress;
  status: string;
}) {
  const estimator = useRef<TransferEstimator | null>(null);
  const [estimate, setEstimate] = useState<TransferEstimate>();
  if (!estimator.current) estimator.current = new TransferEstimator();

  useEffect(() => {
    if (!progress) {
      estimator.current?.reset();
      setEstimate(undefined);
      return;
    }
    const next = estimator.current?.update(
      progress.totalBytes,
      progress.totalSize,
    );
    if (next) setEstimate(next);
  }, [progress?.totalBytes, progress?.totalSize]);

  const percent =
    progress && progress.totalSize > 0
      ? Math.min(100, (progress.totalBytes / progress.totalSize) * 100)
      : 0;
  return (
    <div className="progress-block" aria-live="polite">
      <div className="progress-copy">
        <span>{status}</span>
        {progress && (
          <span>
            {formatBytes(progress.totalBytes)} / {formatBytes(progress.totalSize)}
          </span>
        )}
      </div>
      <div
        className="progress-track"
        role="progressbar"
        aria-label="Transfer progress"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={progress ? Math.round(percent) : undefined}
      >
        <span style={{ width: `${percent}%` }} />
      </div>
      {progress && (
        <>
          <div className="progress-file">
            <span>
              {progress.fileIndex + 1}/{progress.fileCount} {progress.fileName}
            </span>
            <span>{Math.round(percent)}%</span>
          </div>
          <div
            className="progress-metrics"
            aria-label="Transfer speed and estimated time remaining"
          >
            <div>
              <span>Rate</span>
              <strong>
                {estimate
                  ? `${formatBytes(Math.round(estimate.bytesPerSecond))}/s`
                  : "measuring…"}
              </strong>
            </div>
            <div>
              <span>ETA</span>
              <strong>
                {estimate ? formatEta(estimate.etaMilliseconds) : "calculating…"}
              </strong>
            </div>
          </div>
        </>
      )}
    </div>
  );
}

function StatusMessage({
  activity,
  message,
}: {
  activity: Activity;
  message: string;
}) {
  if (!message) return null;
  return (
    <p className={`status-message ${activity}`} role={activity === "error" ? "alert" : "status"}>
      {activity === "done" ? <Check aria-hidden="true" /> : null}
      {activity === "error" ? <AlertTriangle aria-hidden="true" /> : null}
      <span>{message}</span>
    </p>
  );
}

export function App() {
  const [theme, setTheme] = useState<Theme>(initialTheme);
  const [settings, setSettings] = useState<TransferSettings>(() => ({
    gatewayURL: storedValue("croc-web-gateway", defaultSettings.gatewayURL),
    relayAddress: storedValue(
      "croc-web-relay-address",
      defaultSettings.relayAddress,
    ),
    relayPassword: storedValue(
      "croc-web-relay-password",
      defaultSettings.relayPassword,
    ),
  }));
  const [rememberPassword, setRememberPassword] = useState(() => {
    try {
      return localStorage.getItem("croc-web-remember-password") === "true";
    } catch {
      return false;
    }
  });

  const [selectedFiles, setSelectedFiles] = useState<File[]>([]);
  const [sendCode, setSendCode] = useState("");
  const [sendActivity, setSendActivity] = useState<Activity>("idle");
  const [sendStatus, setSendStatus] = useState("");
  const [sendProgress, setSendProgress] = useState<FileProgress>();
  const [completedSend, setCompletedSend] = useState<string[]>([]);
  const [copyState, setCopyState] = useState<CopyState>("idle");

  const [receiveCode, setReceiveCode] = useState("");
  const [receiveActivity, setReceiveActivity] = useState<Activity>("idle");
  const [receiveStatus, setReceiveStatus] = useState("");
  const [receiveProgress, setReceiveProgress] = useState<FileProgress>();
  const [completedReceive, setCompletedReceive] = useState<string[]>([]);
  const [offer, setOffer] = useState<TransferOffer>();
  const offerResolver = useRef<
    ((destination: ReceiveDestination | false) => void) | undefined
  >(undefined);

  const sendAbort = useRef<AbortController>(undefined);
  const receiveAbort = useRef<AbortController>(undefined);
  const fileInput = useRef<HTMLInputElement>(null);
  const copyReset = useRef<number>(undefined);

  const totalSelectedSize = useMemo(
    () => selectedFiles.reduce((total, file) => total + file.size, 0),
    [selectedFiles],
  );

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    document.documentElement.style.colorScheme = theme;
    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute("content", theme === "dark" ? "#000000" : "#ffffff");
    try {
      localStorage.setItem("croc-web-theme", theme);
    } catch {
      // Theme persistence is optional.
    }
  }, [theme]);

  useEffect(() => {
    try {
      localStorage.setItem("croc-web-gateway", settings.gatewayURL);
      localStorage.setItem("croc-web-relay-address", settings.relayAddress);
      localStorage.setItem(
        "croc-web-remember-password",
        String(rememberPassword),
      );
      if (rememberPassword) {
        localStorage.setItem("croc-web-relay-password", settings.relayPassword);
      } else {
        localStorage.removeItem("croc-web-relay-password");
      }
    } catch {
      // The app still works when storage is blocked.
    }
  }, [settings, rememberPassword]);

  useEffect(() => {
    let active = true;
    void wasm()
      .randomCode()
      .then((code) => {
        if (active) setSendCode((current) => current || code);
      })
      .catch((error) => {
        if (active) {
          setSendActivity("error");
          setSendStatus(`Could not initialize croc: ${errorMessage(error)}`);
        }
      });
    return () => {
      active = false;
      if (copyReset.current !== undefined) {
        window.clearTimeout(copyReset.current);
      }
      sendAbort.current?.abort();
      receiveAbort.current?.abort();
    };
  }, []);

  function addFiles(files: File[]) {
    if (sendActivity === "working") return;
    setSelectedFiles((current) => {
      const byName = new Map(current.map((file) => [file.name, file]));
      for (const file of files) byName.set(file.name, file);
      return [...byName.values()];
    });
    setSendActivity("idle");
    setSendStatus("");
  }

  async function regenerateCode() {
    if (sendActivity === "working") return;
    setCopyState("idle");
    setSendCode(await wasm().randomCode());
  }

  async function copyCode() {
    if (copyReset.current !== undefined) {
      window.clearTimeout(copyReset.current);
    }
    try {
      await navigator.clipboard.writeText(sendCode);
      setCopyState("copied");
    } catch {
      setCopyState("error");
    }
    copyReset.current = window.setTimeout(() => {
      setCopyState("idle");
      copyReset.current = undefined;
    }, 2_000);
  }

  async function startSend() {
    sendAbort.current?.abort();
    const controller = new AbortController();
    sendAbort.current = controller;
    setSendActivity("working");
    setSendStatus("Preparing files…");
    setSendProgress(undefined);
    setCompletedSend([]);
    try {
      const prepared = await prepareFiles(
        selectedFiles,
        {
          onStatus: setSendStatus,
        },
        controller.signal,
      );
      await sendFiles({
        files: prepared,
        secret: sendCode.trim(),
        settings,
        signal: controller.signal,
        callbacks: {
          onStatus: setSendStatus,
          onProgress: setSendProgress,
          onFileComplete: (name) =>
            setCompletedSend((current) => [...current, name]),
        },
      });
      setSendActivity("done");
      setSendStatus("All files arrived safely");
    } catch (error) {
      if (controller.signal.aborted) {
        setSendActivity("idle");
        setSendStatus("Transfer cancelled");
      } else {
        setSendActivity("error");
        setSendStatus(errorMessage(error));
      }
    }
  }

  async function startReceive() {
    receiveAbort.current?.abort();
    const controller = new AbortController();
    receiveAbort.current = controller;
    setReceiveActivity("working");
    setReceiveStatus("Connecting…");
    setReceiveProgress(undefined);
    setCompletedReceive([]);
    setOffer(undefined);
    try {
      await receiveFiles({
        secret: receiveCode.trim(),
        settings,
        signal: controller.signal,
        callbacks: {
          onStatus: setReceiveStatus,
          onProgress: setReceiveProgress,
          onFileComplete: (name) =>
            setCompletedReceive((current) => [...current, name]),
          onOffer: (incoming) =>
            new Promise((resolve) => {
              offerResolver.current = resolve;
              setOffer(incoming);
            }),
        },
      });
      setOffer(undefined);
      setReceiveActivity("done");
      setReceiveStatus("All files received and verified");
    } catch (error) {
      setOffer(undefined);
      offerResolver.current = undefined;
      if (controller.signal.aborted) {
        setReceiveActivity("idle");
        setReceiveStatus("Transfer cancelled");
      } else {
        setReceiveActivity("error");
        setReceiveStatus(errorMessage(error));
      }
    }
  }

  async function acceptOffer(downloadSeparately = false) {
    if (!offer || !offerResolver.current) return;
    try {
      const destination = downloadSeparately
        ? new DownloadDestination()
        : await chooseReceiveDestination(offer);
      const resolve = offerResolver.current;
      offerResolver.current = undefined;
      setOffer(undefined);
      resolve(destination);
    } catch (error) {
      if (error instanceof DOMException && error.name === "AbortError") return;
      setReceiveActivity("error");
      setReceiveStatus(errorMessage(error));
      offerResolver.current?.(false);
      offerResolver.current = undefined;
      setOffer(undefined);
    }
  }

  function refuseOffer() {
    offerResolver.current?.(false);
    offerResolver.current = undefined;
    setOffer(undefined);
  }

  const sendBusy = sendActivity === "working";
  const receiveBusy = receiveActivity === "working";

  return (
    <main className="site-shell">
      <header className="site-header">
        <div className="brand-mark" aria-hidden="true">
          <ArrowLeftRight />
        </div>
        <div>
          <p className="eyebrow">croc://web</p>
          <h1>Send files, secured end-to-end.</h1>
        </div>
        <button
          className="icon-button theme-toggle"
          type="button"
          aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} mode`}
          onClick={() => setTheme((current) => (current === "dark" ? "light" : "dark"))}
        >
          {theme === "dark" ? <Sun /> : <Moon />}
        </button>
      </header>

      <div className="security-strip">
        <ShieldCheck aria-hidden="true" />
        <span>End-to-end encrypted with croc PAKE</span>
        <span className="security-route">{settings.relayAddress}</span>
      </div>

      <section className="transfer-grid" aria-label="File transfer controls">
        <article className="panel send-panel">
          <div className="panel-heading">
            <span className="step">01</span>
            <div>
              <h2>Send</h2>
              <p>Choose several files. Share one croc code.</p>
            </div>
            <Upload aria-hidden="true" />
          </div>

          <button
            className="drop-zone"
            type="button"
            disabled={sendBusy}
            onClick={() => fileInput.current?.click()}
            onDragOver={(event) => {
              event.preventDefault();
              event.currentTarget.dataset.dragging = "true";
            }}
            onDragLeave={(event) => {
              delete event.currentTarget.dataset.dragging;
            }}
            onDrop={(event) => {
              event.preventDefault();
              delete event.currentTarget.dataset.dragging;
              addFiles([...event.dataTransfer.files]);
            }}
          >
            <span>Choose files</span>
            <small>or drop them here</small>
            <input
              ref={fileInput}
              type="file"
              multiple
              tabIndex={-1}
              onChange={(event) => {
                addFiles([...(event.target.files ?? [])]);
                event.target.value = "";
              }}
            />
          </button>

          <div className="selection-summary">
            <span>
              {selectedFiles.length} file{selectedFiles.length === 1 ? "" : "s"}
            </span>
            <span>{formatBytes(totalSelectedSize)}</span>
          </div>
          {selectedFiles.length > 0 && (
            <ul className="file-list selected-list">
              {selectedFiles.map((file) => (
                <li key={file.name}>
                  <FileIcon aria-hidden="true" />
                  <span className="file-name">{file.name}</span>
                  <span>{formatBytes(file.size)}</span>
                  <button
                    className="list-action"
                    type="button"
                    aria-label={`Remove ${file.name}`}
                    disabled={sendBusy}
                    onClick={() =>
                      setSelectedFiles((current) =>
                        current.filter((candidate) => candidate !== file),
                      )
                    }
                  >
                    <X />
                  </button>
                </li>
              ))}
            </ul>
          )}

          <label className="field-label" htmlFor="send-code">
            Croc code
          </label>
          <div className="field-with-actions">
            <input
              id="send-code"
              value={sendCode}
              disabled={sendBusy}
              spellCheck={false}
              autoComplete="off"
              onChange={(event) => {
                setCopyState("idle");
                setSendCode(event.target.value);
              }}
            />
            <button
              className="field-action"
              type="button"
              aria-label="Generate a new code"
              disabled={sendBusy}
              onClick={() => void regenerateCode()}
            >
              <RefreshCw />
            </button>
            <button
              className="field-action"
              type="button"
              aria-label={copyState === "copied" ? "Code copied" : "Copy code"}
              disabled={!sendCode}
              onClick={() => void copyCode()}
            >
              {copyState === "copied" ? <Check /> : <Copy />}
            </button>
            <span
              className={`copy-feedback ${copyState}`}
              role="status"
              aria-live="polite"
            >
              {copyState === "copied"
                ? "Copied"
                : copyState === "error"
                  ? "Copy failed"
                  : ""}
            </span>
          </div>

          {sendBusy || sendProgress ? (
            <ProgressBlock progress={sendProgress} status={sendStatus} />
          ) : (
            <StatusMessage activity={sendActivity} message={sendStatus} />
          )}
          {completedSend.length > 0 && (
            <p className="completed-count">
              <Check /> {completedSend.length} verified
            </p>
          )}

          {sendBusy ? (
            <button
              className="primary-button inverted"
              type="button"
              onClick={() => sendAbort.current?.abort()}
            >
              <X /> Cancel send
            </button>
          ) : (
            <button
              className="primary-button"
              type="button"
              disabled={selectedFiles.length === 0 || sendCode.trim().length < 6}
              onClick={() => void startSend()}
            >
              <Upload /> Send {selectedFiles.length || ""} file
              {selectedFiles.length === 1 ? "" : "s"}
            </button>
          )}
        </article>

        <article className="panel receive-panel">
          <div className="panel-heading">
            <span className="step">02</span>
            <div>
              <h2>Receive</h2>
              <p>Enter the sender’s code. Review before saving.</p>
            </div>
            <Download aria-hidden="true" />
          </div>

          <label className="field-label" htmlFor="receive-code">
            Croc code
          </label>
          <input
            id="receive-code"
            className="large-code-input"
            value={receiveCode}
            disabled={receiveBusy}
            placeholder="1234-word-word-word"
            spellCheck={false}
            autoComplete="off"
            onChange={(event) => setReceiveCode(event.target.value)}
          />
          <p className="field-help">
            Paste the same code shown by the sender’s browser or terminal.
          </p>

          {offer && (
            <div className="offer" aria-live="polite">
              <div className="offer-heading">
                <span>Incoming transfer</span>
                <span>{formatBytes(offer.totalSize)}</span>
              </div>
              <ul className="file-list offer-list">
                {offer.files.map((file) => (
                  <li key={file.path}>
                    <FileIcon aria-hidden="true" />
                    <span className="file-name">{file.path}</span>
                    <span>{formatBytes(file.size)}</span>
                  </li>
                ))}
                {offer.emptyFolders.map((folder) => (
                  <li key={folder}>
                    <span className="folder-glyph" aria-hidden="true">
                      /
                    </span>
                    <span className="file-name">{folder}</span>
                    <span>folder</span>
                  </li>
                ))}
              </ul>
              <p>
                {supportsDirectoryDestination()
                  ? "Choose a destination folder. Existing files require confirmation."
                  : "Your browser will download each file separately."}
              </p>
              <div
                className={`button-pair ${supportsDirectoryDestination() ? "three" : ""}`}
              >
                <button
                  className="secondary-button"
                  type="button"
                  onClick={refuseOffer}
                >
                  Refuse
                </button>
                {supportsDirectoryDestination() && (
                  <button
                    className="secondary-button"
                    type="button"
                    onClick={() => void acceptOffer(true)}
                  >
                    Download
                  </button>
                )}
                <button
                  className="primary-button"
                  type="button"
                  onClick={() => void acceptOffer()}
                >
                  <Download />{" "}
                  {supportsDirectoryDestination() ? "Choose folder" : "Accept files"}
                </button>
              </div>
            </div>
          )}

          {receiveBusy && !offer ? (
            <ProgressBlock progress={receiveProgress} status={receiveStatus} />
          ) : (
            !offer && (
              <StatusMessage
                activity={receiveActivity}
                message={receiveStatus}
              />
            )
          )}
          {completedReceive.length > 0 && (
            <ul className="completed-files">
              {completedReceive.map((name) => (
                <li key={name}>
                  <Check /> {name}
                </li>
              ))}
            </ul>
          )}

          {!offer &&
            (receiveBusy ? (
              <button
                className="primary-button inverted"
                type="button"
                onClick={() => receiveAbort.current?.abort()}
              >
                <X /> Cancel receive
              </button>
            ) : (
              <button
                className="primary-button"
                type="button"
                disabled={receiveCode.trim().length < 6}
                onClick={() => void startReceive()}
              >
                <Download /> Connect and review
              </button>
            ))}
        </article>
      </section>

      <details className="settings">
        <summary>
          <span>
            <Settings2 /> Relay settings
          </span>
          <span>advanced</span>
        </summary>
        <div className="settings-grid">
          <label>
            <span>WebSocket gateway</span>
            <input
              value={settings.gatewayURL}
              disabled={sendBusy || receiveBusy}
              spellCheck={false}
              onChange={(event) =>
                setSettings((current) => ({
                  ...current,
                  gatewayURL: event.target.value,
                }))
              }
            />
          </label>
          <label>
            <span>CLI relay address</span>
            <input
              value={settings.relayAddress}
              disabled={sendBusy || receiveBusy}
              spellCheck={false}
              onChange={(event) =>
                setSettings((current) => ({
                  ...current,
                  relayAddress: event.target.value,
                }))
              }
            />
          </label>
          <label>
            <span>Relay password</span>
            <input
              type="password"
              value={settings.relayPassword}
              disabled={sendBusy || receiveBusy}
              onChange={(event) =>
                setSettings((current) => ({
                  ...current,
                  relayPassword: event.target.value,
                }))
              }
            />
          </label>
          <label className="checkbox-label">
            <input
              type="checkbox"
              checked={rememberPassword}
              onChange={(event) => setRememberPassword(event.target.checked)}
            />
            <span>Remember relay password on this device</span>
          </label>
        </div>
      </details>

      <footer>
        <span>croc protocol · browser transport</span>
        <span>files encrypted before the gateway</span>
      </footer>
    </main>
  );
}
