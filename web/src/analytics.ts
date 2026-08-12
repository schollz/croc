export const transferEvents = {
  sendDirect: "send-direct",
  sendWithStorage: "send-with-storage",
  receive: "receive",
} as const;

export type TransferEvent =
  (typeof transferEvents)[keyof typeof transferEvents];

const analyticsScriptID = "croc-analytics-script";

export function loadAnalytics() {
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

export function trackTransferEvent(event: TransferEvent) {
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
