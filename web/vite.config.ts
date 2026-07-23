import { configDefaults, defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

const gatewayProxy =
  process.env.CROC_GATEWAY_PROXY ?? "ws://127.0.0.1:9014";

export default defineConfig({
  plugins: [react()],
  server: {
    host: "127.0.0.1",
    proxy: {
      "/ws": {
        target: gatewayProxy,
        ws: true,
      },
      "/healthz": {
        target: gatewayProxy.replace(/^ws/, "http"),
      },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    exclude: [...configDefaults.exclude, "e2e/**"],
  },
});
