import { execFileSync, spawn } from "node:child_process";
import { mkdirSync, rmSync } from "node:fs";
import { createConnection } from "node:net";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const webDirectory = dirname(dirname(fileURLToPath(import.meta.url)));
const artifactDirectory = join(webDirectory, ".e2e");
const binaryName = process.env.CROC_E2E_BINARY_NAME;
const binaryPath = join(artifactDirectory, binaryName ?? "croc");
const relayPorts = process.env.CROC_E2E_RELAY_PORTS;
const vitePort = process.env.CROC_E2E_VITE_PORT;

if (!relayPorts || !vitePort || !binaryName) {
  throw new Error("The Playwright configuration did not provide E2E ports");
}

mkdirSync(artifactDirectory, { recursive: true });
execFileSync("go", ["build", "-o", binaryPath, ".."], {
  cwd: webDirectory,
  stdio: "inherit",
});

const children = [];
let stopping = false;

function shutdown(exitCode) {
  if (stopping) return;
  stopping = true;
  for (const child of children) {
    if (child.exitCode === null && child.signalCode === null) {
      child.kill("SIGTERM");
    }
  }
  const forceTimer = setTimeout(() => {
    for (const child of children) {
      if (child.exitCode === null && child.signalCode === null) {
        child.kill("SIGKILL");
      }
    }
    process.exit(exitCode);
  }, 2_000);
  forceTimer.unref();
  setTimeout(() => process.exit(exitCode), 2_100);
}

function start(name, command, args, options = {}) {
  const child = spawn(command, args, {
    cwd: webDirectory,
    env: process.env,
    stdio: "inherit",
    ...options,
  });
  children.push(child);
  child.once("error", (error) => {
    console.error(`[e2e:${name}] ${error.message}`);
    shutdown(1);
  });
  child.once("exit", (code, signal) => {
    if (!stopping) {
      console.error(
        `[e2e:${name}] exited unexpectedly (${signal ?? code ?? "unknown"})`,
      );
      shutdown(code || 1);
    }
  });
  return child;
}

function waitForTCP(port, timeoutMs = 20_000) {
  const deadline = Date.now() + timeoutMs;
  return new Promise((resolve, reject) => {
    const attempt = () => {
      const socket = createConnection({ host: "127.0.0.1", port: Number(port) });
      socket.once("connect", () => {
        socket.destroy();
        resolve();
      });
      socket.once("error", () => {
        socket.destroy();
        if (Date.now() >= deadline) {
          reject(new Error(`Timed out waiting for 127.0.0.1:${port}`));
        } else {
          setTimeout(attempt, 50);
        }
      });
    };
    attempt();
  });
}

async function waitForHTTP(url, timeoutMs = 20_000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch {
      // The embedded server is still starting.
    }
    await new Promise((resolve) => setTimeout(resolve, 50));
  }
  throw new Error(`Timed out waiting for ${url}`);
}

process.once("SIGINT", () => shutdown(130));
process.once("SIGTERM", () => shutdown(0));
process.once("SIGHUP", () => shutdown(0));
process.once("exit", () => rmSync(binaryPath, { force: true }));

const [controlPort] = relayPorts.split(",");
start("relay", binaryPath, [
  "--pass",
  "pass123",
  "relay",
  "--host",
  "127.0.0.1",
  "--ports",
  relayPorts,
]);
await waitForTCP(controlPort);

start("server", binaryPath, [
  "--pass",
  "pass123",
  "serve",
  "--relay",
  "127.0.0.1",
  "--ports",
  relayPorts,
  `127.0.0.1:${vitePort}`,
]);
await waitForHTTP(`http://127.0.0.1:${vitePort}/healthz`);

await new Promise(() => {});
