// --baseline-ref reconstructs test-only legacy sources in an ignored directory.
// The application always uses the native protocol engine.
import { createServer } from "vite";
import { chromium } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import path from "node:path";
const option = (name) =>
  process.argv.find((a) => a.startsWith(`--${name}=`))?.slice(name.length + 3);
const ref = option("baseline-ref"),
  assets = new Map();
let temporary,
  modulePath = "/src/protocol/engine.ts";
if (ref) {
  await mkdir(".e2e", { recursive: true });
  temporary = await mkdtemp(path.resolve(".e2e/benchmark-"));
  const source = (file) => execFileSync("git", ["show", `${ref}:${file}`]);
  await writeFile(
    path.join(temporary, "client.ts"),
    source("web/src/wasm/client.ts"),
  );
  await writeFile(path.join(temporary, "main.go"), source("web/wasm/main.go"));
  const env = {
    ...process.env,
    GOTOOLCHAIN: "go1.27.1",
    GOOS: "js",
    GOARCH: "wasm",
  };
  execFileSync(
    "go",
    [
      "build",
      "-buildvcs=false",
      "-trimpath",
      "-ldflags=-s -w",
      "-o",
      path.join(temporary, "croc.wasm"),
      path.join(temporary, "main.go"),
    ],
    { env },
  );
  assets.set("/croc.wasm", await readFile(path.join(temporary, "croc.wasm")));
  assets.set(
    "/croc-worker.js",
    source("web/public/croc-worker.js")
      .toString()
      .replace(
        "const go = new Go();",
        "const go = new Go(); self.__benchmarkGo = go;",
      ),
  );
  const goRoot = execFileSync("go", ["env", "GOROOT"], {
    env,
    encoding: "utf8",
  }).trim();
  assets.set(
    "/wasm_exec.js",
    await readFile(path.join(goRoot, "lib/wasm/wasm_exec.js")),
  );
  modulePath = `/@fs/${temporary}/client.ts`;
}
const server = await createServer({
  server: { port: 0 },
  plugins: [
    {
      name: "benchmark-fixture",
      configureServer(server) {
        server.middlewares.use((req, res, next) => {
          const asset = assets.get(req.url?.split("?")[0]);
          if (!asset) return next();
          res.setHeader(
            "Content-Type",
            req.url.endsWith(".wasm") ? "application/wasm" : "text/javascript",
          );
          res.end(asset);
        });
      },
    },
  ],
});
await server.listen();
const browser = await chromium.launch();
try {
  const runs = [];
  for (let run = 0; run < 3; run++) {
    const context = await browser.newContext(),
      page = await context.newPage();
    await page.goto(server.resolvedUrls.local[0]);
    const result = await page.evaluate(
      async ({ modulePath, legacy }) => {
        const started = performance.now(),
          module = await import(modulePath),
          engine = legacy ? module.wasm() : module.protocolEngine();
        const handle = await engine.hashInit(),
          startupMs = performance.now() - started;
        const bytes = new Uint8Array(1024 * 1024).fill(97),
          begin = performance.now();
        for (let i = 0; i < 64; i++) await engine.hashUpdate(handle, bytes);
        await engine.hashFinal(handle);
        const hashMiBps = 64000 / (performance.now() - begin),
          cipher = await engine.cipherInit(new Uint8Array(32)),
          chunks = performance.now();
        for (let i = 0; i < 512; i++) {
          const encoded = await engine.encodeChunk(
            cipher,
            bytes.slice(0, 32768),
            false,
          );
          await engine.decodeChunk(cipher, encoded, false, 32768);
        }
        const chunkRoundtripMiBps = 16000 / (performance.now() - chunks);
        await engine.cipherRelease(cipher);
        window.__benchmarkEngine = engine;
        return { startupMs, hashMiBps, chunkRoundtripMiBps };
      },
      { modulePath, legacy: !!ref },
    );
    const worker = page
      .workers()
      .find((w) => /(?:croc-worker|engine-worker)/.test(w.url()));
    result.wasmLinearMemoryBytes = await worker.evaluate(
      () => globalThis.__benchmarkGo?.mem?.buffer.byteLength ?? 0,
    );
    const cdp = await browser.newBrowserCDPSession(),
      { targetInfos } = await cdp.send("Target.getTargets");
    const target = targetInfos.find(
      (t) => t.type === "worker" && /(?:croc-worker|engine-worker)/.test(t.url),
    );
    const { sessionId } = await cdp.send("Target.attachToTarget", {
      targetId: target.targetId,
      flatten: false,
    });
    let id = 0;
    const send = (method) =>
      new Promise(async (resolve, reject) => {
        const requestId = ++id,
          timeout = setTimeout(
            () => reject(new Error("Worker measurement timed out")),
            5000,
          );
        const listener = (event) => {
          const message = JSON.parse(event.message);
          if (event.sessionId === sessionId && message.id === requestId) {
            clearTimeout(timeout);
            cdp.off("Target.receivedMessageFromTarget", listener);
            resolve(message.result);
          }
        };
        cdp.on("Target.receivedMessageFromTarget", listener);
        await cdp.send("Target.sendMessageToTarget", {
          sessionId,
          message: JSON.stringify({ id: requestId, method }),
        });
      });
    result.workerMemory = await send("Runtime.getHeapUsage");
    await send("HeapProfiler.collectGarbage");
    result.workerMemoryAfterGC = await send("Runtime.getHeapUsage");
    await cdp.detach();
    await page.evaluate(() => window.__benchmarkEngine.close());
    runs.push(result);
    await context.close();
  }
  const output = {
    engine: ref ? "go-wasm" : "native",
    baselineRef: ref,
    browser: browser.version(),
    runs,
  };
  console.log(JSON.stringify(output, null, 2));
  if (option("output")) {
    await mkdir(".e2e", { recursive: true });
    await writeFile(option("output"), JSON.stringify(output, null, 2) + "\n");
  }
} finally {
  await browser.close();
  await server.close();
  if (temporary) await rm(temporary, { recursive: true, force: true });
}
