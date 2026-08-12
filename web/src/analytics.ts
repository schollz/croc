export const transferEvents = {
  sendDirect: "send-direct",
  sendWithStorage: "send-with-storage",
  receive: "receive",
} as const;

export type TransferEvent =
  (typeof transferEvents)[keyof typeof transferEvents];

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
