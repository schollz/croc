/* global Go, crocWasm */

let ready;

function initialize() {
  if (ready) return ready;
  ready = (async () => {
    importScripts(new URL("./wasm_exec.js", self.location.href).href);
    const go = new Go();
    const response = await fetch(new URL("./croc.wasm", self.location.href));
    if (!response.ok) {
      throw new Error(`Could not load croc.wasm (${response.status})`);
    }
    const bytes = await response.arrayBuffer();
    const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
    void go.run(instance);
    for (let attempts = 0; !self.crocWasm && attempts < 100; attempts += 1) {
      await new Promise((resolve) => setTimeout(resolve, 10));
    }
    if (!self.crocWasm) {
      throw new Error("croc WASM did not initialize");
    }
  })();
  return ready;
}

function transferables(value, output = []) {
  if (value instanceof Uint8Array) {
    output.push(value.buffer);
  } else if (value && typeof value === "object") {
    for (const child of Object.values(value)) transferables(child, output);
  }
  return output;
}

self.addEventListener("message", async (event) => {
  const { id, method, args = [] } = event.data;
  try {
    await initialize();
    const fn = self.crocWasm[method];
    if (typeof fn !== "function") throw new Error(`Unknown WASM method: ${method}`);
    const response = fn(...args);
    if (!response.ok) throw new Error(response.error || `${method} failed`);
    self.postMessage({ id, result: response.value }, transferables(response.value));
  } catch (error) {
    self.postMessage({
      id,
      error: error instanceof Error ? error.message : String(error),
    });
  }
});

void initialize().catch((error) => {
  self.postMessage({
    id: 0,
    error: error instanceof Error ? error.message : String(error),
  });
});
