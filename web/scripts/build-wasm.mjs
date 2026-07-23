import { chmod, copyFile, mkdir, stat } from "node:fs/promises";
import { execFile } from "node:child_process";
import { promisify } from "node:util";
import path from "node:path";
import process from "node:process";

const execFileAsync = promisify(execFile);
const webRoot = path.resolve(import.meta.dirname, "..");
const publicDir = path.join(webRoot, "public");
const wasmGoToolchain = "go1.26.0";
const goEnvironment = {
  ...process.env,
  GOTOOLCHAIN: wasmGoToolchain,
};

await mkdir(publicDir, { recursive: true });

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

await copyFile(wasmExecSource, path.join(publicDir, "wasm_exec.js"));
await execFileAsync(
  "go",
  [
    "build",
    "-buildvcs=false",
    "-trimpath",
    "-ldflags=-s -w",
    "-o",
    path.join(publicDir, "croc.wasm"),
    "./wasm",
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
await chmod(path.join(publicDir, "croc.wasm"), 0o644);
