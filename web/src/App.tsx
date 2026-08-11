import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import {
  AlertTriangle,
  ArrowRight,
  BookOpenText,
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
  Terminal,
  Upload,
  X,
} from "lucide-react";
import { FaGithub } from "react-icons/fa";
import { driver, type DriveStep, type Driver } from "driver.js";
import {
  loadAnalytics,
  trackTransferEvent,
  transferEvents,
  unloadAnalytics,
} from "./analytics";
import { errorMessage, formatBytes } from "./protocol/bytes";
import {
  prepareFiles,
  receiveFiles,
  sendFiles,
} from "./protocol/client";
import {
  formatStoredCLIToken,
  inspectStoredTransfer,
  isStoredShareValue,
  parseStoredShare,
  prepareStoredFiles,
  receiveStoredTransfer,
  revokeStoredTransfer,
  uploadStoredFiles,
  type StoredSettings,
  type StoredUploadResult,
} from "./protocol/stored";
import {
  DownloadDestination,
  chooseStoredReceiveDestination,
  chooseReceiveDestination,
  supportsDirectoryDestination,
} from "./protocol/storage";
import type {
  FileProgress,
  ReceiveCallbacks,
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
  StoredModeSwitch,
  StoredShareCard,
  type SendMode,
} from "./stored-ui";
import { makeDirectReceiveURL, ShareQRCode } from "./share-qr";
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
import { blogPosts } from "./blog-posts";

type Activity = "idle" | "working" | "done" | "error";
type Theme = "dark" | "light";
type CopyState = "idle" | "copied" | "error";
type MobileTransferPanel = "send" | "receive";

const runtimeSettings = window.__CROC_RUNTIME_CONFIG__ ?? {};
const requestedReceiveCode =
  new URLSearchParams(window.location.search).get("code")?.trim() ?? "";
const requestedStoredURL = /^\/s\/[A-Za-z0-9_-]{22}$/.test(
  window.location.pathname,
)
  ? window.location.href
  : "";
const requestedReceiveValue = requestedReceiveCode || requestedStoredURL;
const receiveOnly = requestedReceiveValue !== "";
const storeRuntime = runtimeSettings.store ?? {};
const storeEnabled = storeRuntime.enabled === true;
const storeMaxTransferBytes = storeRuntime.maxTransferBytes || 1024 ** 3;
const storeMaxFiles = storeRuntime.maxFiles || 100;
const crocWebsite = "https://infinitedigits.co/croc/";
const crocRepository = "https://github.com/schollz/croc";
const otherTools = [
  {
    description: "local weather, minus clutter",
    href: "https://wthrtxt.com",
    name: "wthrtxt",
  },
  {
    description: "write, read anywhere",
    href: "https://cowyo.com",
    name: "cowyo",
  },
  {
    description: "yes/no alerts when websites change",
    href: "https://yesnotice.com",
    name: "yesnotice",
  },
  {
    description: "find a piano wherever you are",
    href: "https://pianos.pub",
    name: "pianos.pub",
  },
  {
    description: "strange roadside detours",
    href: "https://makemydrivefun.com",
    name: "makemydrivefun",
  },
  {
    description: "claymation in browsers",
    href: "https://makestopmotion.com",
    name: "makestopmotion",
  },
];

type HomeReview = {
  author: { name: string };
  datePublished: string;
  reviewBody: string;
  reviewRating: { ratingValue: number };
};

type HomeReviewData = {
  ratingValue: number;
  reviewCount: number;
  reviews: HomeReview[];
};

function readHomeReviewData(): HomeReviewData | undefined {
  const element = document.querySelector<HTMLScriptElement>(
    'script[type="application/ld+json"][data-croc-home="true"]',
  );
  if (!element?.textContent) return undefined;

  try {
    const data = JSON.parse(element.textContent) as {
      aggregateRating?: { ratingValue?: unknown; reviewCount?: unknown };
      review?: Array<{
        author?: { name?: unknown };
        datePublished?: unknown;
        reviewBody?: unknown;
        reviewRating?: { ratingValue?: unknown };
      }>;
    };
    const ratingValue = Number(data.aggregateRating?.ratingValue);
    const reviewCount = Number(data.aggregateRating?.reviewCount);
    const reviews = (data.review ?? []).filter(
      (review): review is HomeReview =>
        typeof review.author?.name === "string" &&
        typeof review.datePublished === "string" &&
        /^\d{4}-\d{2}-\d{2}$/.test(review.datePublished) &&
        typeof review.reviewBody === "string" &&
        typeof review.reviewRating?.ratingValue === "number" &&
        Number.isFinite(review.reviewRating.ratingValue),
    );

    if (
      !Number.isFinite(ratingValue) ||
      !Number.isFinite(reviewCount) ||
      reviews.length !== reviewCount
    ) {
      return undefined;
    }

    return { ratingValue, reviewCount, reviews };
  } catch {
    return undefined;
  }
}

const homeReviewData = readHomeReviewData();
const homeReviewDateFormatter = new Intl.DateTimeFormat("en-US", {
  day: "numeric",
  month: "short",
  timeZone: "UTC",
  year: "numeric",
});

function formatHomeReviewDate(value: string) {
  return homeReviewDateFormatter.format(new Date(`${value}T00:00:00Z`));
}

function homeReviewStars(value: number) {
  const filled = Math.max(0, Math.min(5, Math.round(value)));
  return `${"★".repeat(filled)}${"☆".repeat(5 - filled)}`;
}

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
  storeAPI: "/api/v1/store",
};

function storedValue(key: string, fallback: string) {
  try {
    return localStorage.getItem(key) || fallback;
  } catch {
    return fallback;
  }
}

function restoreStoredUpload() {
  if (!storeEnabled) return undefined;
  try {
    let restored: StoredUploadResult | undefined;
    for (let index = 0; index < sessionStorage.length; index += 1) {
      const storageKey = sessionStorage.key(index);
      if (!storageKey?.startsWith("croc-store-upload:")) continue;
      try {
        const raw = sessionStorage.getItem(storageKey);
        if (!raw) continue;
        const receipt = JSON.parse(raw) as {
          browserURL?: string;
          uploadToken?: string;
          expiresAt?: string;
        };
        const expiresAt = new Date(receipt.expiresAt ?? "");
        if (
          !receipt.browserURL ||
          !receipt.uploadToken ||
          !Number.isFinite(expiresAt.getTime()) ||
          expiresAt.getTime() <= Date.now()
        ) {
          throw new Error("expired or invalid stored-upload receipt");
        }
        const share = parseStoredShare(receipt.browserURL);
        const candidate: StoredUploadResult = {
          share,
          uploadToken: receipt.uploadToken,
          expiresAt: expiresAt.toISOString(),
          browserURL: receipt.browserURL,
          cliToken: formatStoredCLIToken(share),
        };
        if (
          !restored ||
          new Date(candidate.expiresAt).getTime() >
            new Date(restored.expiresAt).getTime()
        ) {
          restored = candidate;
        }
      } catch {
        sessionStorage.removeItem(storageKey);
        index -= 1;
      }
    }
    return restored;
  } catch {
    return undefined;
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
      id="cli-download"
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

function TransferLinks() {
  return (
    <nav className="transfer-links" aria-label="More ways to transfer with croc">
      <a href="/#send-panel">
        <Upload aria-hidden="true" />
        <span><strong>Send in your browser</strong><small>No install needed</small></span>
      </a>
      <a href="/#receive">
        <Download aria-hidden="true" />
        <span><strong>Receive in your browser</strong><small>Paste a code or link</small></span>
      </a>
      <a href="/#cli-download">
        <Terminal aria-hidden="true" />
        <span><strong>Download the croc CLI</strong><small>Windows, macOS, and Linux</small></span>
      </a>
      <a href={crocWebsite} target="_blank" rel="noopener noreferrer">
        <BookOpenText aria-hidden="true" />
        <span><strong>Read the croc guide</strong><small>Install and usage docs</small></span>
      </a>
      <a href={crocRepository} target="_blank" rel="noopener noreferrer">
        <FaGithub aria-hidden="true" />
        <span><strong>Explore the codebase</strong><small>Source, issues, and releases</small></span>
      </a>
    </nav>
  );
}

function BlogTeaser() {
  return (
    <section
      className="home-blog-teaser"
      aria-labelledby="home-blog-title"
      data-tour="blog"
    >
      <div className="home-blog-heading">
        <div>
          <p className="eyebrow">Field notes</p>
          <h2 id="home-blog-title">What happens after you press Send?</h2>
          <p>
            Plainspoken notes about the relay, the four-word code, and the ways
            browsers and terminals meet in the same transfer.
          </p>
        </div>
        <a href="/blog">Read all seven notes <ArrowRight /></a>
      </div>
      <div className="home-blog-list">
        {blogPosts.slice(0, 3).map((post) => (
          <a href={`/blog/${post.slug}`} key={post.slug}>
            <span>{post.number}</span>
            <strong>{post.title}</strong>
            <small>{post.readingMinutes} min read</small>
            <ArrowRight aria-hidden="true" />
          </a>
        ))}
      </div>
    </section>
  );
}

function HomeReviews() {
  if (!homeReviewData) return null;

  const { ratingValue, reviewCount, reviews } = homeReviewData;

  return (
    <details className="home-reviews">
      <summary>
        <span className="home-reviews-score">
          <span aria-hidden="true">{homeReviewStars(ratingValue)}</span>
          <strong>{ratingValue}/5</strong>
          <span>
            from {reviewCount} reviewer{reviewCount === 1 ? "" : "s"}
          </span>
        </span>
        <span className="home-reviews-action">read reviews</span>
      </summary>
      <div className="home-reviews-body">
        <p>Comments from people using croc to move their files.</p>
        <ol className="home-review-list">
          {reviews.map((review, index) => (
            <li key={`${review.author.name}-${index}`}>
              <blockquote>
                <p>{review.reviewBody}</p>
                <footer>
                  <cite>{review.author.name}</cite>
                  <span className="home-review-meta">
                    <time dateTime={review.datePublished}>
                      {formatHomeReviewDate(review.datePublished)}
                    </time>
                    <span
                      className="home-review-rating"
                      aria-label={`Rated ${review.reviewRating.ratingValue} out of 5`}
                    >
                      <span aria-hidden="true">
                        {homeReviewStars(review.reviewRating.ratingValue)}
                      </span>
                    </span>
                  </span>
                </footer>
              </blockquote>
            </li>
          ))}
        </ol>
      </div>
    </details>
  );
}

export function App() {
  const restoredStoredUpload = useMemo(restoreStoredUpload, []);
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
    storeAPI: defaultSettings.storeAPI,
  }));
  const [rememberPassword, setRememberPassword] = useState(() => {
    try {
      return localStorage.getItem("croc-web-remember-password") === "true";
    } catch {
      return false;
    }
  });

  const [selectedFiles, setSelectedFiles] = useState<File[]>([]);
  const [sendMode, setSendMode] = useState<SendMode>(
    restoredStoredUpload ? "stored" : "direct",
  );
  const [mobileTransferPanel, setMobileTransferPanel] =
    useState<MobileTransferPanel>(receiveOnly ? "receive" : "send");
  const [sendCode, setSendCode] = useState("");
  const [sendActivity, setSendActivity] = useState<Activity>("idle");
  const [sendStatus, setSendStatus] = useState("");
  const [sendProgress, setSendProgress] = useState<FileProgress>();
  const [completedSend, setCompletedSend] = useState<string[]>([]);
  const [copyState, setCopyState] = useState<CopyState>("idle");
  const [storedUpload, setStoredUpload] =
    useState<StoredUploadResult | undefined>(restoredStoredUpload);

  const [receiveCode, setReceiveCode] = useState(requestedReceiveValue);
  const [receiveActivity, setReceiveActivity] = useState<Activity>("idle");
  const [receiveStatus, setReceiveStatus] = useState("");
  const [receiveProgress, setReceiveProgress] = useState<FileProgress>();
  const [completedReceive, setCompletedReceive] = useState<string[]>([]);
  const [offer, setOffer] = useState<TransferOffer>();
  const [storedReceiveActive, setStoredReceiveActive] = useState(false);
  const [storedReceiveExpiresAt, setStoredReceiveExpiresAt] = useState("");
  const offerResolver = useRef<
    ((destination: ReceiveDestination | false) => void) | undefined
  >(undefined);

  const sendAbort = useRef<AbortController>(undefined);
  const receiveAbort = useRef<AbortController>(undefined);
  const fileInput = useRef<HTMLInputElement>(null);
  const transferGrid = useRef<HTMLElement>(null);
  const receivePanel = useRef<HTMLFormElement>(null);
  const copyReset = useRef<number>(undefined);
  const tour = useRef<Driver>(undefined);

  useEffect(() => {
    loadAnalytics();
    return unloadAnalytics;
  }, []);

  const totalSelectedSize = useMemo(
    () => selectedFiles.reduce((total, file) => total + file.size, 0),
    [selectedFiles],
  );
  const directReceiveURL = useMemo(
    () => makeDirectReceiveURL(sendCode),
    [sendCode],
  );
  const storedSettings = useMemo<StoredSettings>(
    () => ({
      storeAPI: settings.storeAPI,
      maxTransferBytes: storeMaxTransferBytes,
      maxFiles: storeMaxFiles,
    }),
    [settings.storeAPI],
  );

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    document.documentElement.style.colorScheme = theme;
    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute("content", theme === "dark" ? "#121411" : "#f2f1ec");
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
        if (active) {
          setSendCode((current) => current || code);
        }
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
    if (!requestedReceiveValue) return;
    if (!isStoredShareValue(requestedReceiveValue) && requestedReceiveValue.length < 6) {
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

  function showMobileTransferPanel(panel: MobileTransferPanel) {
    setMobileTransferPanel(panel);
    window.requestAnimationFrame(() => {
      transferGrid.current?.scrollIntoView({ block: "start" });
    });
  }

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

  async function copyValue(value: string) {
    if (copyReset.current !== undefined) {
      window.clearTimeout(copyReset.current);
    }
    try {
      await navigator.clipboard.writeText(value);
      setCopyState("copied");
    } catch {
      setCopyState("error");
    }
    copyReset.current = window.setTimeout(() => {
      setCopyState("idle");
      copyReset.current = undefined;
    }, 2_000);
  }

  function rememberStoredUpload(result: StoredUploadResult) {
    setStoredUpload(result);
    try {
      sessionStorage.setItem(
        `croc-store-upload:${result.share.id}`,
        JSON.stringify({
          browserURL: result.browserURL,
          uploadToken: result.uploadToken,
          expiresAt: result.expiresAt,
        }),
      );
    } catch {
      // Revocation remains available while this component is mounted.
    }
  }

  async function sendStored(signal: AbortSignal) {
    const prepared = await prepareStoredFiles(
      selectedFiles,
      storedSettings,
      { onStatus: setSendStatus },
      signal,
    );
    const result = await uploadStoredFiles({
      files: prepared,
      settings: storedSettings,
      signal,
      callbacks: {
        onStatus: setSendStatus,
        onProgress: setSendProgress,
      },
    });
    rememberStoredUpload(result);
  }

  async function sendDirect(signal: AbortSignal) {
    const prepared = await prepareFiles(
      selectedFiles,
      { onStatus: setSendStatus },
      signal,
    );
    await sendFiles({
      files: prepared,
      secret: sendCode.trim(),
      settings,
      signal,
      callbacks: {
        onStatus: setSendStatus,
        onProgress: setSendProgress,
        onFileComplete: (name) =>
          setCompletedSend((current) => [...current, name]),
      },
    });
  }

  async function startSend() {
    sendAbort.current?.abort();
    const controller = new AbortController();
    const currentSendMode = sendMode;
    sendAbort.current = controller;
    setSendActivity("working");
    setSendStatus("Preparing files…");
    setSendProgress(undefined);
    setCompletedSend([]);
    setStoredUpload(undefined);
    try {
      await (currentSendMode === "stored"
        ? sendStored(controller.signal)
        : sendDirect(controller.signal));
      trackTransferEvent(
        currentSendMode === "stored"
          ? transferEvents.sendWithStorage
          : transferEvents.sendDirect,
      );
      setSendActivity("done");
      setSendStatus(
        currentSendMode === "stored"
          ? "Encrypted upload ready to share"
          : "All files arrived safely",
      );
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

  function receiveCallbacks(): ReceiveCallbacks {
    return {
      onStatus: setReceiveStatus,
      onProgress: setReceiveProgress,
      onFileComplete: (name) =>
        setCompletedReceive((current) => [...current, name]),
      onOffer: (incoming) =>
        new Promise((resolve) => {
          offerResolver.current = resolve;
          setOffer(incoming);
        }),
    };
  }

  async function receiveStored(signal: AbortSignal) {
    setStoredReceiveActive(true);
    const share = parseStoredShare(receiveCode);
    setReceiveStatus("Opening encrypted manifest…");
    const inspection = await inspectStoredTransfer(
      share,
      storedSettings,
      signal,
    );
    setStoredReceiveExpiresAt(inspection.expiresAt ?? "");
    await receiveStoredTransfer({
      inspection,
      settings: storedSettings,
      signal,
      callbacks: receiveCallbacks(),
    });
    window.history.replaceState({}, "", "/");
  }

  async function receiveDirect(signal: AbortSignal) {
    await receiveFiles({
      secret: receiveCode.trim(),
      settings,
      signal,
      callbacks: receiveCallbacks(),
    });
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
    setStoredReceiveActive(false);
    setStoredReceiveExpiresAt("");
    try {
      const stored = isStoredShareValue(receiveCode);
      await (stored
        ? receiveStored(controller.signal)
        : receiveDirect(controller.signal));
      trackTransferEvent(transferEvents.receive);
      setOffer(undefined);
      setReceiveActivity("done");
      setReceiveStatus(
        stored
          ? "All files received, verified, and removed from storage"
          : "All files received and verified",
      );
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

  async function revokeCurrentStoredUpload() {
    if (!storedUpload) return;
    setSendActivity("working");
    setSendStatus("Revoking encrypted upload…");
    try {
      await revokeStoredTransfer(
        storedUpload.share,
        storedUpload.uploadToken,
        storedSettings,
      );
      try {
        sessionStorage.removeItem(`croc-store-upload:${storedUpload.share.id}`);
      } catch {
        // The remote revocation still succeeded.
      }
      setStoredUpload(undefined);
      setSendActivity("done");
      setSendStatus("Stored transfer revoked");
    } catch (error) {
      setSendActivity("error");
      setSendStatus(errorMessage(error));
    }
  }

  async function acceptOffer(downloadSeparately = false) {
    if (!offer || !offerResolver.current) return;
    try {
      const destination = downloadSeparately
        ? new DownloadDestination()
        : storedReceiveActive
          ? await chooseStoredReceiveDestination(offer)
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
          description: receiveOnly
            ? requestedStoredURL
              ? "This encrypted stored link is opening its manifest. Review the incoming files, then choose where to save them before claiming its one download."
              : "This receive link already filled its croc code and started connecting. You only need to review the incoming files and choose where to save them."
            : "Send or receive files from this page with any compatible croc browser or command-line peer. This tour shows the complete flow.",
        },
      },
    ];

    if (!receiveOnly) {
      steps.push({
        element: '[data-tour="send"]',
        popover: {
          title: "Send one or several files",
          description:
            "Use Direct for a live croc-code transfer, or Store for an encrypted link that lasts up to 24 hours or one verified download. Choose files or drag them here to begin.",
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
            "Paste the sender’s croc code, encrypted browser link, or CLI token. Before anything is saved, inspect the names, paths, and sizes, then accept or refuse.",
          side: receiveOnly ? "top" : "left",
          align: "start",
        },
      },
      {
        element: ".transfer-grid",
        popover: {
          title: "The code or link provides the key",
          description:
            "Direct transfers use password-authenticated key exchange (PAKE) so both peers derive a strong shared key from the croc code. Stored links carry a separate random key after #, which is not sent to the server. File metadata and chunks are encrypted before leaving the browser.",
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
            "Direct browser transfers interoperate with normal croc command-line clients, and stored transfers include a pasteable CLI token. Download the detected build here, or choose another release from GitHub.",
          side: "top",
          align: "start",
        },
      },
      {
        element: '[data-tour="blog"]',
        popover: {
          title: "Read the field notes",
          description:
            "Seven plainspoken notes explain the relay, PAKE, encryption, browser and terminal interoperability, and the decisions behind croc.",
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
    <>
      <main className="site-shell">
      <aside className="donation-banner" aria-label="Support croc">
        <div className="donation-copy">
          <Heart aria-hidden="true" />
          <p>
            <strong>croc is free and supported by donations.</strong>{" "}
            <span className="donation-detail">
              If just 1% of users donate $1, it will be sustainable.
            </span>
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
        <a className="brand-link" href="/" aria-label="Go to croc home">
          <img
            className="brand-illustration"
            src="/croc.jpg"
            width="408"
            height="196"
            alt="Hand-drawn green crocodile floating in blue water"
          />
        </a>
        <div>
          <p className="eyebrow">
            <strong>croc</strong> is a free and open-source tool to
          </p>
          <h1>
            {receiveOnly
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
            className="icon-button blog-header-link"
            href="/blog"
            aria-label="Read croc field notes"
            title="Read croc field notes"
          >
            <BookOpenText aria-hidden="true" />
          </a>
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
        ref={transferGrid}
        className={`transfer-grid${receiveOnly ? " receive-only" : ""}`}
        aria-label="File transfer controls"
      >
        {!receiveOnly && (
          <div
            className="mobile-transfer-switch"
            role="tablist"
            aria-label="Choose transfer direction"
          >
            <button
              id="mobile-send-tab"
              type="button"
              role="tab"
              aria-controls="send-panel"
              aria-selected={mobileTransferPanel === "send"}
              onClick={() => showMobileTransferPanel("send")}
            >
              <Upload aria-hidden="true" />
              Send
            </button>
            <button
              id="mobile-receive-tab"
              type="button"
              role="tab"
              aria-controls="receive"
              aria-selected={mobileTransferPanel === "receive"}
              onClick={() => showMobileTransferPanel("receive")}
            >
              <Download aria-hidden="true" />
              Receive
            </button>
          </div>
        )}
        {!receiveOnly && (
          <article
            id="send-panel"
            className={`panel send-panel${mobileTransferPanel === "send" ? " mobile-active" : ""}`}
            data-tour="send"
          >
          <div className="panel-heading">
            <span className="step">
              <Upload aria-hidden="true" />
            </span>
            <div>
              <h2>Send</h2>
              <p>
                {sendMode === "stored"
                  ? "Upload encrypted files for 24 hours or one download."
                  : "Choose several files. Share one croc code."}
              </p>
            </div>
          </div>

          {storeEnabled && (
            <StoredModeSwitch
              mode={sendMode}
              disabled={sendBusy}
              onChange={(mode) => {
                setSendMode(mode);
                setStoredUpload(undefined);
                setSendStatus("");
                setSendActivity("idle");
              }}
            />
          )}

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
            <small>
              <span className="drop-desktop-copy">or drop them here</span>
              <span className="drop-mobile-copy">Select from this device</span>
            </small>
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

          {sendMode === "direct" && (
            <>
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
                  autoCapitalize="none"
                  autoCorrect="off"
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
                  onClick={() => void copyValue(sendCode)}
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
              <p className="field-help">
                Generated codes use the first two words to find the transfer
                and the last two words to secure it.
              </p>
              <ShareQRCode
                value={directReceiveURL}
                disabled={sendCode.trim().length < 6}
                description="Scan with a phone to open the receive page. Keep this browser open while the direct transfer runs."
              />
            </>
          )}

          {sendMode === "stored" && (
            <p className="field-help stored-privacy-note">
              Files and names are encrypted in this browser. The server never
              receives the decryption key.
            </p>
          )}

          {storedUpload && (
            <StoredShareCard
              upload={storedUpload}
              onCopy={(value) => void copyValue(value)}
              onRevoke={() => void revokeCurrentStoredUpload()}
            />
          )}

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
              disabled={
                selectedFiles.length === 0 ||
                (sendMode === "direct" && sendCode.trim().length < 6) ||
                (sendMode === "stored" &&
                  (selectedFiles.length > storeMaxFiles ||
                    totalSelectedSize > storeMaxTransferBytes))
              }
              onClick={() => void startSend()}
            >
              <Upload /> {sendMode === "stored" ? "Store" : "Send"}{" "}
              {selectedFiles.length || ""} file
              {selectedFiles.length === 1 ? "" : "s"}
            </button>
          )}
          </article>
        )}

        <form
          id="receive"
          ref={receivePanel}
          className={`panel receive-panel${mobileTransferPanel === "receive" ? " mobile-active" : ""}`}
          data-tour="receive"
          onSubmit={(event) => {
            event.preventDefault();
            if (receiveBusy || receiveCode.trim().length < 6) return;
            void startReceive();
          }}
        >
          <div className="panel-heading">
            <span className="step">
              <Download aria-hidden="true" />
            </span>
            <div>
              <h2>Receive</h2>
              <p>Enter a croc code or encrypted stored link. Review before saving.</p>
            </div>
          </div>

          <label className="field-label" htmlFor="receive-code">
            Croc code or stored link
          </label>
          <input
            id="receive-code"
            value={receiveCode}
            disabled={receiveBusy}
            placeholder="word-word-word-word or encrypted link"
            spellCheck={false}
            autoComplete="off"
            autoCapitalize="none"
            autoCorrect="off"
            enterKeyHint="go"
            onChange={(event) => setReceiveCode(event.target.value)}
          />
          <p className="field-help">
            Paste or type the code, stored link, or CLI token, then press Enter
            or select Receive.
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
              {storedReceiveActive && storedReceiveExpiresAt && (
                <p>
                  Encrypted storage expires{" "}
                  {new Date(storedReceiveExpiresAt).toLocaleString()} and is
                  removed after this verified download.
                </p>
              )}
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
                type="submit"
                disabled={receiveCode.trim().length < 6}
              >
                <Download /> Receive
              </button>
            ))}
        </form>
      </section>

      <TransferLinks />

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

      <BlogTeaser />

      {!receiveOnly && <HomeReviews />}

      <footer className="site-footer">
        <div className="site-footer-links">
          <span>
            made by{" "}
            <a
              href="https://github.com/sponsors/schollz"
              target="_blank"
              rel="noopener noreferrer"
            >
              schollz
            </a>
          </span>
          <span aria-hidden="true">·</span>
          <a
            href={crocRepository}
            target="_blank"
            rel="noopener noreferrer"
          >
            github
          </a>
          <span aria-hidden="true">·</span>
          <a href="/blog">blog</a>
          <span aria-hidden="true">·</span>
          <a
            href={crocWebsite}
            target="_blank"
            rel="noopener noreferrer"
          >
            croc website
          </a>
          <span aria-hidden="true">·</span>
          <span>
            hosted with{" "}
            <a
              href="https://disco.cloud"
              target="_blank"
              rel="noopener noreferrer"
            >
              disco
            </a>
          </span>
        </div>

        <details className="tools-menu">
          <summary>other tools</summary>
          <ul>
            {otherTools.map((tool) => (
              <li key={tool.href}>
                <a
                  href={tool.href}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <strong>{tool.name}</strong>
                  <span>{tool.description}</span>
                </a>
              </li>
            ))}
          </ul>
        </details>
      </footer>
      </main>
    </>
  );
}
