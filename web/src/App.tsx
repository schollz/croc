import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  AlertTriangle,
  Check,
  CircleHelp,
  Copy,
  Download,
  File as FileIcon,
  Heart,
  Moon,
  RefreshCw,
  Settings2,
  Sun,
  Upload,
  X,
} from "lucide-react";
import { FaGithub } from "react-icons/fa";
import { driver, type DriveStep, type Driver } from "driver.js";
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
import {
  assetArchitectureLabel,
  assetsForPlatform,
  detectArchitecture,
  detectPlatform,
  fetchLatestRelease,
  latestReleasePage,
  preferredAsset,
  type DetectedArchitecture,
  type GitHubRelease,
} from "./releases";
import { wasm } from "./wasm/client";

type Activity = "idle" | "working" | "done" | "error";
type Theme = "dark" | "light";
type CopyState = "idle" | "copied" | "error";

const runtimeSettings = window.__CROC_RUNTIME_CONFIG__ ?? {};
const requestedReceiveCode =
  new URLSearchParams(window.location.search).get("code")?.trim() ?? "";
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

function CliDownload() {
  const [platform] = useState(detectPlatform);
  const [architecture, setArchitecture] =
    useState<DetectedArchitecture>("unknown");
  const [release, setRelease] = useState<GitHubRelease>();
  const [releaseFailed, setReleaseFailed] = useState(false);

  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    void detectArchitecture().then((detected) => {
      if (active) setArchitecture(detected);
    });
    void fetchLatestRelease(controller.signal)
      .then((latest) => {
        if (active) setRelease(latest);
      })
      .catch((error) => {
        if (active && !(error instanceof DOMException && error.name === "AbortError")) {
          setReleaseFailed(true);
        }
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, []);

  const matchingAssets = release
    ? assetsForPlatform(release.assets, platform)
    : [];
  const selectedAsset = preferredAsset(
    matchingAssets,
    platform,
    architecture,
  );
  const alternatives = matchingAssets.filter(
    (asset) => asset !== selectedAsset,
  );
  const platformLabel =
    platform === "other" ? "your operating system" : platform;

  return (
    <section
      className="cli-download"
      aria-labelledby="cli-download-title"
      data-tour="cli"
    >
      <div>
        <p className="eyebrow">croc CLI</p>
        <h2 id="cli-download-title">Download croc for {platformLabel}.</h2>
        {platform === "other" && (
          <p>Choose the latest build for your device on GitHub.</p>
        )}
      </div>
      <div className="cli-download-actions" aria-live="polite">
        {selectedAsset && release ? (
          <a
            className="cli-download-primary"
            href={selectedAsset.browser_download_url}
          >
            <Download aria-hidden="true" />
            <span>
              Download {release.tag_name}
              <small>
                {platform} · {assetArchitectureLabel(selectedAsset)}
              </small>
            </span>
          </a>
        ) : releaseFailed || platform === "other" ? (
          <a className="cli-download-primary" href={latestReleasePage}>
            <Download aria-hidden="true" />
            <span>
              Choose a download
              <small>GitHub releases</small>
            </span>
          </a>
        ) : (
          <span className="cli-download-primary loading" aria-label="Loading latest croc release">
            <Download aria-hidden="true" />
            <span>
              Finding latest release
              <small>GitHub releases</small>
            </span>
          </span>
        )}
        {alternatives.length > 0 && (
          <div className="cli-download-alternatives">
            <span>Other {platform} builds:</span>
            {alternatives.map((asset) => (
              <a key={asset.name} href={asset.browser_download_url}>
                {assetArchitectureLabel(asset)}
              </a>
            ))}
          </div>
        )}
        <a
          className="other-releases"
          href={latestReleasePage}
          target="_blank"
          rel="noopener noreferrer"
        >
          Other releases <span aria-hidden="true">↗</span>
        </a>
      </div>
    </section>
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

  const [receiveCode, setReceiveCode] = useState(requestedReceiveCode);
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
  const receivePanel = useRef<HTMLElement>(null);
  const copyReset = useRef<number>(undefined);
  const tour = useRef<Driver>(undefined);

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
      tour.current?.destroy();
      sendAbort.current?.abort();
      receiveAbort.current?.abort();
    };
  }, []);

  useEffect(() => {
    if (!requestedReceiveCode) return;
    if (requestedReceiveCode.length < 6) {
      setReceiveActivity("error");
      setReceiveStatus("The croc code in this link is too short");
      return;
    }
    void startReceive();
    return () => receiveAbort.current?.abort();
  }, []);

  useLayoutEffect(() => {
    if (requestedReceiveCode) {
      receivePanel.current?.scrollIntoView({ block: "start" });
    }
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

  function startTour() {
    tour.current?.destroy();

    const steps: DriveStep[] = [
      {
        popover: {
          title: "Welcome to croc web",
          description: requestedReceiveCode
            ? "This receive link already filled its croc code and started connecting. You only need to review the incoming files and choose where to save them."
            : "Send or receive files from this page with any compatible croc browser or command-line peer. This tour shows the complete flow.",
        },
      },
    ];

    if (!requestedReceiveCode) {
      steps.push({
        element: '[data-tour="send"]',
        popover: {
          title: "Send one or several files",
          description:
            "Choose files or drag them here, edit the generated croc code if you like, then press Send. Give that same code to the recipient while this page stays open.",
          side: "right",
          align: "start",
        },
      });
    }

    steps.push(
      {
        element: '[data-tour="receive"]',
        popover: {
          title: "Receive and review",
          description:
            "Paste the sender’s croc code and connect. Before anything is saved, you can inspect the names, paths, and sizes, then accept or refuse the transfer.",
          side: requestedReceiveCode ? "top" : "left",
          align: "start",
        },
      },
      {
        element: ".transfer-grid",
        popover: {
          title: "The code creates the encryption key",
          description:
            "croc uses password-authenticated key exchange (PAKE), so both peers derive a strong shared key from the croc code without sending that key through the relay. File metadata and chunks are encrypted before leaving the browser.",
          side: "bottom",
          align: "start",
        },
      },
      {
        element: '[data-tour="settings"]',
        popover: {
          title: "Use another relay when needed",
          description:
            "Most people can leave these advanced settings alone. Self-hosted setups can select their own WebSocket gateway, croc relay, and relay password.",
          side: "top",
          align: "start",
        },
      },
      {
        element: '[data-tour="cli"]',
        popover: {
          title: "Works with the croc CLI",
          description:
            "Browser transfers interoperate with normal croc command-line clients. Download the detected build here, or choose another release from GitHub.",
          side: "top",
          align: "start",
        },
      },
      {
        element: '[data-tour="about"]',
        popover: {
          title: "Simple by design",
          description:
            "No account, port forwarding, or local server is required. Follow the “Read how croc works” link for a deeper explanation of the protocol and its security model.",
          side: "top",
          align: "start",
        },
      },
    );

    tour.current = driver({
      steps,
      showProgress: true,
      progressText: "{{current}} / {{total}}",
      nextBtnText: "Next",
      prevBtnText: "Back",
      doneBtnText: "Done",
      popoverClass: "croc-tour",
      stagePadding: 8,
      stageRadius: 0,
      overlayOpacity: 0.76,
      animate: !window.matchMedia?.("(prefers-reduced-motion: reduce)").matches,
      smoothScroll: true,
      allowKeyboardControl: true,
      disableActiveInteraction: true,
      skipMissingElement: true,
      onDestroyed: () => {
        tour.current = undefined;
      },
    });
    tour.current.drive();
  }

  const sendBusy = sendActivity === "working";
  const receiveBusy = receiveActivity === "working";

  return (
    <main className="site-shell">
      <aside className="donation-banner" aria-label="Support croc">
        <div className="donation-copy">
          <Heart aria-hidden="true" />
          <p>
            <strong>croc is free, but depends on donations to keep going.</strong>{" "}
            If just 1% of users donate $1, it will be sustainable.
          </p>
        </div>
        <a
          href="https://github.com/sponsors/schollz"
          target="_blank"
          rel="noopener noreferrer"
        >
          Donate $1
        </a>
      </aside>

      <header className="site-header">
        <img
          className="brand-illustration"
          src="/croc.jpg"
          width="408"
          height="196"
          alt="Hand-drawn green crocodile floating in blue water"
        />
        <div>
          <p className="eyebrow">
            <strong>croc</strong> is a free and open-source tool to
          </p>
          <h1>
            {requestedReceiveCode
              ? "Receive files, secured end-to-end."
              : "Send files, secured end-to-end."}
          </h1>
        </div>
        <div className="header-actions">
          <button
            className="icon-button"
            type="button"
            aria-label="How to use croc web"
            title="How to use croc web"
            onClick={startTour}
          >
            <CircleHelp aria-hidden="true" />
          </button>
          <a
            className="icon-button"
            href="https://github.com/schollz/croc"
            target="_blank"
            rel="noopener noreferrer"
            aria-label="View croc on GitHub"
            title="View croc on GitHub"
          >
            <FaGithub aria-hidden="true" />
          </a>
          <button
            className="icon-button theme-toggle"
            type="button"
            aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} mode`}
            onClick={() =>
              setTheme((current) => (current === "dark" ? "light" : "dark"))
            }
          >
            {theme === "dark" ? <Sun /> : <Moon />}
          </button>
        </div>
      </header>

      <section
        className={`transfer-grid${requestedReceiveCode ? " receive-only" : ""}`}
        aria-label="File transfer controls"
      >
        {!requestedReceiveCode && (
          <article className="panel send-panel" data-tour="send">
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
        )}

        <article
          id="receive"
          ref={receivePanel}
          className="panel receive-panel"
          data-tour="receive"
        >
          <div className="panel-heading">
            <span className="step">{requestedReceiveCode ? "01" : "02"}</span>
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
                <Download /> Receive
              </button>
            ))}
        </article>
      </section>

      <details className="settings" data-tour="settings">
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

      <CliDownload />

      <section
        className="about-croc"
        aria-labelledby="about-croc-title"
        data-tour="about"
      >
        <div>
          <p className="eyebrow">Why croc?</p>
          <h2 id="about-croc-title">
            Fast, simple, and secure file transfer between any two computers.
          </h2>
          <p>
            croc relays files in real time, derives a strong end-to-end
            encryption key with PAKE, and works without running a server or
            configuring port forwarding.
          </p>
        </div>
        <a
          href="https://schollz.com/croc6/"
          target="_blank"
          rel="noopener noreferrer"
        >
          Read how croc works <span aria-hidden="true">↗</span>
        </a>
      </section>

      <footer>
        <span>end-to-end encrypted · over 2 petabytes transferred</span>
        <span>
          <a
            href="https://github.com/schollz/croc"
            target="_blank"
            rel="noopener noreferrer"
          >
            open-source
          </a>{" "}
          · made by{" "}
          <a
            href="https://github.com/sponsors/schollz"
            target="_blank"
            rel="noopener noreferrer"
          >
            schollz
          </a>
        </span>
      </footer>
    </main>
  );
}
