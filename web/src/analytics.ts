export const transferEvents = {
  sendDirect: "send-direct",
  sendWithStorage: "send-with-storage",
  receive: "receive",
} as const;

export const sshEvents = {
  browserSession: "ssh-browser-session",
} as const;

export type TransferEvent =
  (typeof transferEvents)[keyof typeof transferEvents];

export type SSHEvent = (typeof sshEvents)[keyof typeof sshEvents];

function trackEvent(event: TransferEvent | SSHEvent) {
  try {
    window.umami?.track((properties) => ({
      ...properties,
      name: event,
      url: window.location.pathname,
    }));
  } catch {
    // Analytics must never interfere with user activity.
  }
}

export function trackTransferEvent(event: TransferEvent) {
  trackEvent(event);
}

export function trackSSHEvent(event: SSHEvent) {
  trackEvent(event);
}
