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
  MessageSquareText,
  Moon,
  Settings2,
  Sun,
  Upload,
  X,
} from "lucide-react";
import { FaGithub } from "react-icons/fa";
import { driver, type DriveStep, type Driver } from "driver.js";
import {
  trackTransferEvent,
  transferEvents,
} from "./analytics";
import { errorMessage, formatBytes } from "./protocol/bytes";
import {
  FileHashCache,
  type FileHashAlgorithm,
} from "./protocol/hash";
import {
  prepareFiles,
  prepareText,
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
  TextDestination,
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
import { maxTextTransferBytes } from "./protocol/types";
import {
  formatEta,
  TransferEstimator,
  type TransferEstimate,
} from "./progress";
import {
  formatStoredExpiration,
  StoredExpirationControl,
  StoredModeSwitch,
  StoredShareCard,
  storedExpirationParts,
  storedExpirationSeconds,
  maxStoredExpirationSeconds,
  type SendMode,
} from "./stored-ui";
import { makeDirectReceiveURL, ShareQRCode } from "./share-qr";
import {
  clearBestRelayOnConnectionError,
  generateCodeForRelay,
  selectRelayForSend,
} from "./public-relay";
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
import { blogPosts } from "./blog-posts";
import { TransferLinks } from "./TransferLinks";

type Activity = "idle" | "working" | "done" | "error";
type Theme = "dark" | "light";
type CopyState = "idle" | "copied" | "error";
type MobileTransferPanel = "send" | "receive";
type SendContent = "files" | "text";

function sendHashAlgorithm(mode: SendMode): FileHashAlgorithm {
  return mode === "stored" ? "sha256" : "xxhash";
}

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
const storeMaxDownloads = storeRuntime.maxDownloads || 1;
const storeMaxExpiresSeconds =
  Number.isSafeInteger(storeRuntime.maxExpiresSeconds) &&
  (storeRuntime.maxExpiresSeconds ?? 0) >= 60
    ? Math.min(storeRuntime.maxExpiresSeconds!, maxStoredExpirationSeconds)
    : 0;
const configuredStoreExpiresSeconds =
  Number.isSafeInteger(storeRuntime.expiresSeconds) &&
  (storeRuntime.expiresSeconds ?? 0) >= 60
    ? Math.min(storeRuntime.expiresSeconds!, maxStoredExpirationSeconds)
    : storeMaxExpiresSeconds > 0
      ? Math.min(24 * 60 * 60, storeMaxExpiresSeconds)
      : 24 * 60 * 60;
const storeExpiresSeconds =
  storeMaxExpiresSeconds > 0
    ? Math.min(configuredStoreExpiresSeconds, storeMaxExpiresSeconds)
    : configuredStoreExpiresSeconds;
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
        /^\d{4}-\d{2}(?:-\d{2})?$/.test(review.datePublished) &&
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
const homeReviewMonthFormatter = new Intl.DateTimeFormat("en-US", {
  month: "short",
  timeZone: "UTC",
  year: "numeric",
});

function formatHomeReviewDate(value: string) {
  const formatter = value.length === 7
    ? homeReviewMonthFormatter
    : homeReviewDateFormatter;
  const normalizedValue = value.length === 7 ? `${value}-01` : value;
  return formatter.format(new Date(`${normalizedValue}T00:00:00Z`));
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
  relayAddresses:
    runtimeSettings.relayAddresses?.filter(Boolean) ||
    import.meta.env.VITE_CROC_RELAY_ADDRESSES?.split(",")
      .map((address) => address.trim())
      .filter(Boolean) || [
      "1.getcroc.com:9009",
      "2.getcroc.com:9009",
      "3.getcroc.com:9009",
    ],
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
          downloads?: number;
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
          downloads:
            Number.isSafeInteger(receipt.downloads) &&
            (receipt.downloads ?? 0) > 0
              ? receipt.downloads!
              : 1,
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

function BlogTeaser() {
  return (
    <section
      className="home-blog-teaser"
      aria-labelledby="home-blog-title"
      data-tour="blog"
    >
      <div className="home-blog-heading">
        <div>
          <p className="eyebrow">Notes &amp; updates</p>
          <h2 id="home-blog-title">What happens after you press Send?</h2>
          <p>
            Plainspoken notes about the relay, the three-word code, and the ways
            browsers and terminals meet, plus updates when croc changes.
          </p>
        </div>
        <a href="/blog">Read all {blogPosts.length} posts <ArrowRight /></a>
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
  const [fileHashes] = useState(() => new FileHashCache());
  const restoredStoredUpload = useMemo(restoreStoredUpload, []);
  const [theme, setTheme] = useState<Theme>(initialTheme);
  const [settings, setSettings] = useState<TransferSettings>(() => ({
    gatewayURL: storedValue("croc-web-gateway", defaultSettings.gatewayURL),
    relayAddresses: defaultSettings.relayAddresses,
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
  const [sendContent, setSendContent] = useState<SendContent>("files");
  const [sendText, setSendText] = useState("");
  const [sendMode, setSendMode] = useState<SendMode>(
    restoredStoredUpload ? "stored" : "direct",
  );
  const [mobileTransferPanel, setMobileTransferPanel] =
    useState<MobileTransferPanel>(receiveOnly ? "receive" : "send");
  const [sendCode, setSendCode] = useState("");
  const [sendDetailsVisible, setSendDetailsVisible] = useState(false);
  const [sendActivity, setSendActivity] = useState<Activity>("idle");
  const [sendStatus, setSendStatus] = useState("");
  const [sendProgress, setSendProgress] = useState<FileProgress>();
  const [completedSend, setCompletedSend] = useState<string[]>([]);
  const [codeCopyState, setCodeCopyState] = useState<CopyState>("idle");
  const [storedUpload, setStoredUpload] =
    useState<StoredUploadResult | undefined>(restoredStoredUpload);
  const [storedDownloads, setStoredDownloads] = useState(1);
  const [storedExpiration, setStoredExpiration] = useState(() =>
    storedExpirationParts(storeExpiresSeconds),
  );

  const [receiveCode, setReceiveCode] = useState(requestedReceiveValue);
  const [receiveActivity, setReceiveActivity] = useState<Activity>("idle");
  const [receiveStatus, setReceiveStatus] = useState("");
  const [receiveProgress, setReceiveProgress] = useState<FileProgress>();
  const [completedReceive, setCompletedReceive] = useState<string[]>([]);
  const [offer, setOffer] = useState<TransferOffer>();
  const [storedReceiveActive, setStoredReceiveActive] = useState(false);
  const [storedReceiveExpiresAt, setStoredReceiveExpiresAt] = useState("");
  const [receivingText, setReceivingText] = useState(false);
  const [receivedText, setReceivedText] = useState<string>();
  const [receivedTextCopyState, setReceivedTextCopyState] =
    useState<CopyState>("idle");
  const offerResolver = useRef<
    ((destination: ReceiveDestination | false) => void) | undefined
  >(undefined);

  const sendAbort = useRef<AbortController>(undefined);
  const receiveAbort = useRef<AbortController>(undefined);
  const fileInput = useRef<HTMLInputElement>(null);
  const transferGrid = useRef<HTMLElement>(null);
  const receivePanel = useRef<HTMLFormElement>(null);
  const codeCopyReset = useRef<number>(undefined);
  const receivedTextCopyReset = useRef<number>(undefined);
  const receiveOfferKind = useRef<TransferOffer["kind"]>("files");
  const tour = useRef<Driver>(undefined);

  const totalSelectedSize = useMemo(
    () => selectedFiles.reduce((total, file) => total + file.size, 0),
    [selectedFiles],
  );
  const sendTextBytes = useMemo(() => new Blob([sendText]).size, [sendText]);
  const directReceiveURL = useMemo(
    () => makeDirectReceiveURL(sendCode),
    [sendCode],
  );
  const storedSettings = useMemo<StoredSettings>(
    () => ({
      storeAPI: settings.storeAPI,
      maxTransferBytes: storeMaxTransferBytes,
      maxFiles: storeMaxFiles,
      maxDownloads: storeMaxDownloads,
      maxExpiresSeconds: storeMaxExpiresSeconds,
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
    fileHashes.retain(selectedFiles);
    fileHashes.prime(selectedFiles, sendHashAlgorithm(sendMode));
  }, [fileHashes, selectedFiles, sendMode]);

  useEffect(() => {
    return () => {
      if (codeCopyReset.current !== undefined)
        window.clearTimeout(codeCopyReset.current);
      if (receivedTextCopyReset.current !== undefined)
        window.clearTimeout(receivedTextCopyReset.current);
      tour.current?.destroy();
      sendAbort.current?.abort();
      receiveAbort.current?.abort();
      fileHashes.clear();
    };
  }, [fileHashes]);

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

  function resetSendPresentation() {
    setSendActivity("idle");
    setSendStatus("");
    setSendProgress(undefined);
    setCompletedSend([]);
    setSendDetailsVisible(false);
  }

  function addFiles(files: File[]) {
    if (sendActivity === "working") return;
    fileHashes.prime(files, sendHashAlgorithm(sendMode));
    setSelectedFiles((current) => {
      const byName = new Map(current.map((file) => [file.name, file]));
      for (const file of files) byName.set(file.name, file);
      return [...byName.values()];
    });
    resetSendPresentation();
  }

  async function copyValue(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      return true;
    } catch {
      return false;
    }
  }

  async function copyWithFeedback(
    value: string,
    setState: (state: CopyState) => void,
    reset: { current: number | undefined },
  ) {
    if (reset.current !== undefined) window.clearTimeout(reset.current);
    setState((await copyValue(value)) ? "copied" : "error");
    reset.current = window.setTimeout(() => {
      setState("idle");
      reset.current = undefined;
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
          downloads: result.downloads,
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
      (file) => fileHashes.hash(file, "sha256"),
    );
    const result = await uploadStoredFiles({
      files: prepared,
      settings: storedSettings,
      downloads: storedDownloads,
      expiresSeconds: storedExpirationSeconds(
        storedExpiration.value,
        storedExpiration.unit,
      ),
      signal,
      callbacks: {
        onStatus: setSendStatus,
        onProgress: setSendProgress,
      },
    });
    rememberStoredUpload(result);
  }

  async function sendDirect(signal: AbortSignal) {
    const relayIndex = await selectRelayForSend(settings, {
      signal,
      onCacheState: (cached) =>
        setSendStatus(cached ? "Using saved relay…" : "Finding fastest relay…"),
    });
    const code = await generateCodeForRelay(
      relayIndex,
      settings.relayAddresses.length,
    );
    setSendCode(code);
    const sendingText = sendContent === "text";
    const prepared = sendingText
      ? await prepareText(sendText, { onStatus: setSendStatus }, signal)
      : await prepareFiles(
          selectedFiles,
          { onStatus: setSendStatus },
          signal,
          (file) => fileHashes.hash(file, "xxhash"),
        );
    try {
      await sendFiles({
        files: prepared,
        sendingText,
        secret: code,
        settings,
        signal,
        callbacks: {
          onStatus: setSendStatus,
          onProgress: setSendProgress,
          onFileComplete: (name) =>
            setCompletedSend((current) => [...current, name]),
        },
      });
    } catch (error) {
      clearBestRelayOnConnectionError(error);
      throw error;
    }
  }

  async function startSend() {
    sendAbort.current?.abort();
    const controller = new AbortController();
    const currentSendMode = sendMode;
    if (currentSendMode === "direct") {
      setSendCode("");
      setSendDetailsVisible(true);
    }
    sendAbort.current = controller;
    setSendActivity("working");
    setSendStatus(
      currentSendMode === "direct" && sendContent === "text"
        ? "Preparing text…"
        : "Preparing files…",
    );
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
          : sendContent === "text"
            ? "Text arrived safely"
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
      onFileComplete: (name) => {
        if (receiveOfferKind.current !== "text") {
          setCompletedReceive((current) => [...current, name]);
        }
      },
      onOffer: (incoming) =>
        new Promise((resolve) => {
          receiveOfferKind.current = incoming.kind;
          setReceivingText(incoming.kind === "text");
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
    const remaining = await receiveStoredTransfer({
      inspection,
      settings: storedSettings,
      signal,
      callbacks: receiveCallbacks(),
    });
    window.history.replaceState({}, "", "/");
    return remaining;
  }

  async function receiveDirect(signal: AbortSignal) {
    return receiveFiles({
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
    setReceivingText(false);
    setReceivedText(undefined);
    setReceivedTextCopyState("idle");
    receiveOfferKind.current = "files";
    try {
      const stored = isStoredShareValue(receiveCode);
      let remaining: number | undefined;
      let directOffer: TransferOffer | undefined;
      if (stored) {
        remaining = await receiveStored(controller.signal);
      } else {
        directOffer = await receiveDirect(controller.signal);
      }
      trackTransferEvent(transferEvents.receive);
      setOffer(undefined);
      setReceiveActivity("done");
      setReceiveStatus(
        stored
          ? remaining === 0
            ? "All files received and verified; stored ciphertext removed"
            : `All files received and verified; ${remaining} ${
                remaining === 1 ? "download remains" : "downloads remain"
              }`
          : directOffer?.kind === "text"
            ? "Text received and verified"
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
      const destination =
        offer.kind === "text"
          ? new TextDestination(setReceivedText)
          : downloadSeparately
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
              ? "This encrypted stored link is opening its manifest. Review the incoming files, then choose where to save them before claiming an allowed download."
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
          description: `Use Direct for a live croc-code transfer, or Store for an encrypted link that lasts ${formatStoredExpiration(
            storedExpiration.value,
            storedExpiration.unit,
          )} or until its configured verified-download limit. Choose files or drag them here to begin; Direct also has a secondary option for short text.`,
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
            "Paste the sender’s croc code, encrypted browser link, or CLI token. Review incoming files or text before choosing whether to save, display, or refuse it.",
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
            "Plainspoken notes explain the relay, PAKE, encryption, browser and terminal interoperability, stored sharing, and the decisions behind croc.",
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
  const storedShareReady =
    sendMode === "stored" && sendActivity === "done" && storedUpload !== undefined;

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
                  ? `Upload encrypted files for ${formatStoredExpiration(
                      storedExpiration.value,
                      storedExpiration.unit,
                    )} or ${storedDownloads} ${
                      storedDownloads === 1 ? "download" : "downloads"
                    }.`
                  : "Choose several files. Share one croc code."}
              </p>
            </div>
          </div>

          {storeEnabled && (
            <StoredModeSwitch
              mode={sendMode}
              disabled={sendBusy || storedUpload !== undefined}
              durationLabel={formatStoredExpiration(
                storedExpiration.value,
                storedExpiration.unit,
              )}
              onChange={(mode) => {
                fileHashes.prime(selectedFiles, sendHashAlgorithm(mode));
                setSendMode(mode);
                if (mode === "stored") setSendContent("files");
                setStoredUpload(undefined);
                resetSendPresentation();
              }}
            />
          )}

          {sendContent === "files" || sendMode === "stored" ? (
            <>
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
                <button
                  className="send-content-link"
                  type="button"
                  disabled={sendBusy}
                  onClick={() => {
                    setSendContent("text");
                    resetSendPresentation();
                  }}
                >
                  <MessageSquareText aria-hidden="true" /> Send text instead
                </button>
              )}
            </>
          ) : (
            <div className="text-composer">
              <label className="field-label" htmlFor="send-text">
                Text to send
              </label>
              <textarea
                id="send-text"
                value={sendText}
                disabled={sendBusy}
                placeholder="Paste a URL or short message"
                spellCheck={true}
                aria-invalid={sendTextBytes > maxTextTransferBytes}
                aria-describedby="send-text-size send-text-help"
                onChange={(event) => {
                  setSendText(event.target.value);
                  resetSendPresentation();
                }}
              />
              <div className="text-composer-meta">
                <span
                  id="send-text-size"
                  className={sendTextBytes > maxTextTransferBytes ? "error" : ""}
                  role={sendTextBytes > maxTextTransferBytes ? "alert" : undefined}
                >
                  {formatBytes(sendTextBytes)} / 1 MiB
                  {sendTextBytes > maxTextTransferBytes ? " — too large" : ""}
                </span>
                <button
                  className="send-content-link"
                  type="button"
                  disabled={sendBusy}
                  onClick={() => {
                    setSendContent("files");
                    resetSendPresentation();
                  }}
                >
                  <Upload aria-hidden="true" /> Send files instead
                </button>
              </div>
              <p id="send-text-help" className="field-help">
                Sent directly and encrypted end-to-end. Text is not stored.
              </p>
            </div>
          )}

          {sendMode === "direct" && sendDetailsVisible && (
            <>
              <div className="send-code">
                <span className="send-code-label">Use this code:</span>
                <code
                  className={`send-code-value ${codeCopyState}`}
                  aria-label="Croc code"
                >
                  {sendCode}
                </code>
                <button
                  className={`send-code-copy ${codeCopyState}`}
                  type="button"
                  aria-label={codeCopyState === "copied" ? "Code copied" : "Copy code"}
                  disabled={!sendCode}
                  onClick={() =>
                    void copyWithFeedback(
                      sendCode,
                      setCodeCopyState,
                      codeCopyReset,
                    )
                  }
                >
                  {codeCopyState === "copied" ? <Check /> : <Copy />}
                </button>
                <span
                  className="visually-hidden"
                  role="status"
                  aria-live="polite"
                >
                  {codeCopyState === "copied"
                    ? "Copied"
                    : codeCopyState === "error"
                      ? "Copy failed"
                      : ""}
                </span>
              </div>
              <ShareQRCode
                value={directReceiveURL}
                disabled={sendCode.trim().length < 6}
                description="Scan with a phone to open the receive page. Keep this browser open while the direct transfer runs."
              />
            </>
          )}

          {sendMode === "stored" && !sendBusy && !storedUpload && (
            <>
              <StoredExpirationControl
                value={storedExpiration.value}
                unit={storedExpiration.unit}
                maxExpiresSeconds={storeMaxExpiresSeconds}
                disabled={sendBusy}
                onChange={(value, unit) =>
                  setStoredExpiration({ value, unit })
                }
              />
              <label className="field-label" htmlFor="stored-downloads">
                Verified downloads
              </label>
              <input
                id="stored-downloads"
                type="number"
                min={1}
                max={storeMaxDownloads}
                step={1}
                value={storedDownloads}
                disabled={sendBusy}
                onChange={(event) => {
                  const next = Number(event.target.value);
                  if (Number.isSafeInteger(next)) {
                    setStoredDownloads(
                      Math.max(1, Math.min(storeMaxDownloads, next)),
                    );
                  }
                }}
              />
              <p className="field-help stored-privacy-note">
                Files and names are encrypted in this browser. The server never
                receives the decryption key.
              </p>
            </>
          )}

          {storedShareReady && storedUpload && (
            <StoredShareCard
              upload={storedUpload}
              onCopy={copyValue}
              onRevoke={() => void revokeCurrentStoredUpload()}
            />
          )}

          {!storedShareReady && (
            <>
              {sendBusy || sendProgress ? (
                <ProgressBlock progress={sendProgress} status={sendStatus} />
              ) : (
                <StatusMessage activity={sendActivity} message={sendStatus} />
              )}
              {completedSend.length > 0 && sendContent !== "text" && (
                <p className="completed-count">
                  <Check /> {completedSend.length} verified
                </p>
              )}
            </>
          )}

          {!storedShareReady && (
            sendBusy ? (
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
                  sendMode === "direct" && sendContent === "text"
                    ? sendTextBytes === 0 || sendTextBytes > maxTextTransferBytes
                    : selectedFiles.length === 0 ||
                      (sendMode === "stored" &&
                        (selectedFiles.length > storeMaxFiles ||
                          totalSelectedSize > storeMaxTransferBytes))
                }
                onClick={() => void startSend()}
              >
                {sendMode === "direct" && sendContent === "text" ? (
                  <>
                    <MessageSquareText /> Send text
                  </>
                ) : (
                  <>
                    <Upload /> {sendMode === "stored" ? "Store" : "Send"}
                    {selectedFiles.length > 1
                      ? ` ${selectedFiles.length} files`
                      : " file"}
                  </>
                )}
              </button>
            )
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
              <p>
                Enter a croc code or encrypted stored link. Review before saving
                or displaying.
              </p>
            </div>
          </div>

          <label className="field-label" htmlFor="receive-code">
            Croc code or stored link
          </label>
          <input
            id="receive-code"
            value={receiveCode}
            disabled={receiveBusy}
            placeholder="word-word-word or encrypted link"
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
                <span>{offer.kind === "text" ? "Incoming text" : "Incoming transfer"}</span>
                <span>{formatBytes(offer.totalSize)}</span>
              </div>
              {offer.kind === "files" ? (
                <>
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
                      {new Date(storedReceiveExpiresAt).toLocaleString()}; this
                      verified download consumes one allowed download.
                    </p>
                  )}
                  <p>
                    {supportsDirectoryDestination()
                      ? "Choose a destination folder. Existing files require confirmation."
                      : "Your browser will download each file separately."}
                  </p>
                </>
              ) : (
                <p>
                  Display this encrypted text after it has been received and
                  verified. Nothing will be downloaded.
                </p>
              )}
              <div
                className={`button-pair ${offer.kind === "files" && supportsDirectoryDestination() ? "three" : ""}`}
              >
                <button
                  className="secondary-button"
                  type="button"
                  onClick={refuseOffer}
                >
                  Refuse
                </button>
                {offer.kind === "files" && supportsDirectoryDestination() && (
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
                  {offer.kind === "text" ? (
                    <>
                      <MessageSquareText /> Display text
                    </>
                  ) : (
                    <>
                      <Download />{" "}
                      {supportsDirectoryDestination() ? "Choose folder" : "Accept files"}
                    </>
                  )}
                </button>
              </div>
            </div>
          )}

          {(receiveBusy || receiveProgress) && !offer ? (
            <ProgressBlock progress={receiveProgress} status={receiveStatus} />
          ) : (
            !offer && (
              <StatusMessage
                activity={receiveActivity}
                message={receiveStatus}
              />
            )
          )}
          {receivedText !== undefined && !offer && (
            <section className="received-text" aria-labelledby="received-text-title">
              <div className="received-text-heading">
                <span id="received-text-title">Text received</span>
                <button
                  className={`secondary-button ${receivedTextCopyState}`}
                  type="button"
                  aria-label={
                    receivedTextCopyState === "copied"
                      ? "Text copied"
                      : "Copy text"
                  }
                  onClick={() =>
                    void copyWithFeedback(
                      receivedText,
                      setReceivedTextCopyState,
                      receivedTextCopyReset,
                    )
                  }
                >
                  {receivedTextCopyState === "copied" ? <Check /> : <Copy />}
                  {receivedTextCopyState === "copied" ? "Copied" : "Copy text"}
                </button>
              </div>
              <pre aria-label="Received text" tabIndex={0}>{receivedText}</pre>
              <span className="visually-hidden" role="status" aria-live="polite">
                {receivedTextCopyState === "copied"
                  ? "Text copied"
                  : receivedTextCopyState === "error"
                    ? "Copy failed"
                    : ""}
              </span>
            </section>
          )}
          {completedReceive.length > 0 && !receivingText && (
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

      <details className="settings" data-tour="settings">
        <summary>
          <span>
            <Settings2 /> Relay settings
          </span>
          <span>advanced</span>
        </summary>
        <form
          className="settings-grid"
          autoComplete="off"
          onSubmit={(event) => event.preventDefault()}
        >
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
            <span>CLI relay addresses</span>
            <input
              value={settings.relayAddresses.join(", ")}
              readOnly
              spellCheck={false}
            />
          </label>
          <label>
            <span>Relay password</span>
            <input
              type="password"
              name="relay-password"
              autoComplete="off"
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
        </form>
      </details>

      <CliDownload />

      <BlogTeaser />

      {!receiveOnly && <HomeReviews />}

      <TransferLinks />

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
