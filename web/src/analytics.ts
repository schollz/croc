export const transferEvents = {
  sendDirect: "send-direct",
  sendWithStorage: "send-with-storage",
  receive: "receive",
} as const;

export type TransferEvent =
  (typeof transferEvents)[keyof typeof transferEvents];

export type AnalyticsConsent = "accepted" | "rejected";

type AnalyticsConsentRecord = {
  analytics: boolean;
  updatedAt: string;
  version: 1;
};

export const analyticsConsentKey = "croc-web-privacy-consent";
const analyticsScriptID = "croc-analytics-script";

export function readAnalyticsConsent(): AnalyticsConsent | null {
  try {
    const record = JSON.parse(
      window.localStorage.getItem(analyticsConsentKey) ?? "null",
    ) as Partial<AnalyticsConsentRecord> | null;
    if (record?.version !== 1 || typeof record.analytics !== "boolean") {
      return null;
    }
    return record.analytics ? "accepted" : "rejected";
  } catch {
    return null;
  }
}

export function saveAnalyticsConsent(choice: AnalyticsConsent) {
  try {
    const record: AnalyticsConsentRecord = {
      analytics: choice === "accepted",
      updatedAt: new Date().toISOString(),
      version: 1,
    };
    window.localStorage.setItem(analyticsConsentKey, JSON.stringify(record));
  } catch {
    // Keep the choice for this page even when storage is unavailable.
  }
}

export function loadAnalytics() {
  if (readAnalyticsConsent() !== "accepted") return;
  const config = window.__CROC_ANALYTICS__;
  if (!config?.scriptURL || !config.websiteID) return;
  if (document.getElementById(analyticsScriptID)) return;

  const script = document.createElement("script");
  script.id = analyticsScriptID;
  script.src = config.scriptURL;
  script.async = true;
  script.dataset.websiteId = config.websiteID;
  script.dataset.autoTrack = "false";
  script.dataset.excludeSearch = "true";
  script.dataset.excludeHash = "true";
  script.dataset.doNotTrack = "true";
  document.head.append(script);
}

export function unloadAnalytics() {
  document.getElementById(analyticsScriptID)?.remove();
  delete window.umami;
}

export function trackTransferEvent(event: TransferEvent) {
  if (readAnalyticsConsent() !== "accepted") return;
  try {
    window.umami?.track((properties) => ({
      ...properties,
      name: event,
      url: window.location.pathname,
    }));
  } catch {
    // Analytics must never interfere with a transfer.
  }
}
