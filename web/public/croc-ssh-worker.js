/* global Go, crocSSHWasm */

let ready;
let supplyModule;
const moduleReady = new Promise((resolve) => {
  supplyModule = resolve;
});

function initialize() {
  if (ready) return ready;
  ready = (async () => {
    importScripts(new URL("./wasm_exec.js", self.location.href).href);
    const go = new Go();
    const module = await moduleReady;
    const instance = await WebAssembly.instantiate(module, go.importObject);
    void go.run(instance);
    for (let attempts = 0; !self.crocSSHWasm && attempts < 100; attempts += 1) {
      await new Promise((resolve) => setTimeout(resolve, 10));
    }
    if (!self.crocSSHWasm) throw new Error("croc SSH WASM did not initialize");
  })();
  return ready;
}

self.addEventListener("message", async (event) => {
  const { type, id, method, args = [], module } = event.data;
  if (type === "initialize") {
    supplyModule(module);
    return;
  }
  try {
    await initialize();
    const fn = self.crocSSHWasm[method];
    if (typeof fn !== "function") throw new Error(`Unknown SSH WASM method: ${method}`);
    const response = fn(...args);
    if (!response.ok) throw new Error(response.error || `${method} failed`);
    self.postMessage({ type: "response", id, result: response.value });
  } catch (error) {
    self.postMessage({
      type: "response",
      id,
      error: error instanceof Error ? error.message : String(error),
    });
  }
});

void initialize().catch((error) => {
  self.postMessage({
    type: "fatal",
    error: error instanceof Error ? error.message : String(error),
  });
});
