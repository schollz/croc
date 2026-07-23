import { createServer } from "node:net";
import { defineConfig } from "@playwright/test";

async function freePort() {
  return new Promise<number>((resolve, reject) => {
    const server = createServer();
    server.unref();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      if (!address || typeof address === "string") {
        server.close();
        reject(new Error("Could not allocate an E2E port"));
        return;
      }
      const { port } = address;
      server.close((error) => {
        if (error) reject(error);
        else resolve(port);
      });
    });
  });
}

const allocatedPorts = await Promise.all(
  Array.from({ length: 6 }, () => freePort()),
);
const relayPorts =
  process.env.CROC_E2E_RELAY_PORTS ?? allocatedPorts.slice(0, 5).join(",");
const vitePort = process.env.CROC_E2E_VITE_PORT ?? String(allocatedPorts[5]);
const binaryName =
  process.env.CROC_E2E_BINARY_NAME ?? `croc-${vitePort}`;

process.env.CROC_E2E_RELAY_PORTS = relayPorts;
process.env.CROC_E2E_VITE_PORT = vitePort;
process.env.CROC_E2E_BINARY_NAME = binaryName;

export default defineConfig({
  testDir: "./e2e",
  outputDir: ".e2e/test-results",
  globalTeardown: "./e2e/global-teardown.ts",
  fullyParallel: false,
  workers: 1,
  timeout: 90_000,
  expect: {
    timeout: 20_000,
  },
  reporter: [["line"]],
  use: {
    baseURL: `http://127.0.0.1:${vitePort}`,
    acceptDownloads: true,
    trace: "retain-on-failure",
  },
  webServer: {
    command: "node e2e/stack.mjs",
    url: `http://127.0.0.1:${vitePort}`,
    reuseExistingServer: false,
    timeout: 120_000,
    env: {
      CROC_E2E_RELAY_PORTS: relayPorts,
      CROC_E2E_VITE_PORT: vitePort,
      CROC_E2E_BINARY_NAME: binaryName,
    },
  },
});
