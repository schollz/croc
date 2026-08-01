/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_CROC_GATEWAY_URL?: string;
  readonly VITE_CROC_RELAY_ADDRESS?: string;
  readonly VITE_CROC_RELAY_PASSWORD?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

interface Window {
  umami?: {
    track(
      payload:
        | string
        | ((properties: Record<string, unknown>) => Record<string, unknown>),
    ): void;
  };
  __CROC_ANALYTICS__?: {
    scriptURL?: string;
    websiteID?: string;
  };
  __CROC_RUNTIME_CONFIG__?: {
    gatewayURL?: string;
    relayAddress?: string;
    relayPassword?: string;
    store?: {
      enabled?: boolean;
      maxTransferBytes?: number;
      maxFiles?: number;
      expiresSeconds?: number;
    };
  };
}
