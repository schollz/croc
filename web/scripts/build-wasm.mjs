import { chmod, copyFile, mkdir, rm, stat } from "node:fs/promises";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import path from "node:path";
import process from "node:process";

const execFileAsync = promisify(execFile);
const webRoot = path.resolve(import.meta.dirname, "..");
const publicDir = path.join(webRoot, "public");
const wasmGoToolchain = "go1.27.1";
const goEnvironment = {
  ...process.env,
  GOTOOLCHAIN: wasmGoToolchain,
};

await mkdir(publicDir, { recursive: true });
// Remove old direct-transfer output before Vite copies public assets.
await Promise.all(
  ["croc.wasm", "croc-worker.js"].map((name) =>
    rm(path.join(publicDir, name), { force: true }),
  ),
);

const { stdout } = await execFileAsync("go", ["env", "GOROOT"], {
  cwd: webRoot,
  env: goEnvironment,
});
const goRoot = stdout.trim();
const candidates = [
  path.join(goRoot, "lib", "wasm", "wasm_exec.js"),
  path.join(goRoot, "misc", "wasm", "wasm_exec.js"),
];

let wasmExecSource;
for (const candidate of candidates) {
  try {
    await stat(candidate);
    wasmExecSource = candidate;
    break;
  } catch {
    // Try the location used by another supported Go release.
  }
}
if (!wasmExecSource) {
  throw new Error(`Could not find wasm_exec.js below ${goRoot}`);
}

const wasmExecDestination = path.join(publicDir, "wasm_exec.js");
try {
  await chmod(wasmExecDestination, 0o644);
} catch (error) {
  if (error.code !== "ENOENT") {
    throw error;
  }
}
await copyFile(wasmExecSource, wasmExecDestination);
await chmod(wasmExecDestination, 0o644);
await execFileAsync(
  "go",
  [
    "build",
    "-buildvcs=false",
    "-trimpath",
    "-ldflags=-s -w",
    "-o",
    path.join(publicDir, "croc-ssh.wasm"),
    "./sshwasm",
  ],
  {
    cwd: webRoot,
    env: {
      ...goEnvironment,
      GOOS: "js",
      GOARCH: "wasm",
    },
  },
);
await chmod(path.join(publicDir, "croc-ssh.wasm"), 0o644);
