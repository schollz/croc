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
  __CROC_RUNTIME_CONFIG__?: {
    gatewayURL?: string;
    relayAddress?: string;
    relayPassword?: string;
  };
}
