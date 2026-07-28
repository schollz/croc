const pending = new Map();
const waiters = new Map();

self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));

self.addEventListener("message", (event) => {
  if (event.data?.type !== "prepare" || !event.data.id || !event.ports[0]) return;
  const entry = {
    id: event.data.id,
    name: String(event.data.name || "download"),
    port: event.ports[0],
  };
  entry.timer = setTimeout(() => {
    if (!pending.delete(entry.id)) return;
    entry.port.postMessage({ error: "Browser did not start the streaming download" });
    entry.port.close();
  }, 15000);
  pending.set(entry.id, entry);
  const waiter = waiters.get(entry.id);
  if (waiter) {
    waiters.delete(entry.id);
    waiter(entry);
  }
  entry.port.postMessage({ ready: true });
});

function waitForEntry(id) {
  const existing = pending.get(id);
  if (existing) return Promise.resolve(existing);
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      waiters.delete(id);
      reject(new Error("Streaming download was not prepared"));
    }, 5000);
    waiters.set(id, (entry) => {
      clearTimeout(timer);
      resolve(entry);
    });
  });
}

function safeName(value) {
  return value.replace(/[\r\n"]/g, "_").slice(0, 255) || "download";
}

self.addEventListener("fetch", (event) => {
  const url = new URL(event.request.url);
  const match = url.pathname.match(/\/__croc_download__\/([^/]+)$/);
  if (!match) return;
  event.respondWith(
    (async () => {
      const entry = await waitForEntry(match[1]);
      pending.delete(entry.id);
      clearTimeout(entry.timer);
      const stream = new ReadableStream({
        start(controller) {
          entry.port.onmessage = (message) => {
            try {
              if (message.data?.type === "chunk") {
                controller.enqueue(new Uint8Array(message.data.bytes));
                entry.port.postMessage({ ok: true });
              } else if (message.data?.type === "end") {
                controller.close();
                entry.port.postMessage({ ok: true });
                entry.port.close();
              } else if (message.data?.type === "abort") {
                controller.error(new Error("Download cancelled"));
                entry.port.postMessage({ ok: true });
                entry.port.close();
              }
            } catch (error) {
              entry.port.postMessage({
                error: error instanceof Error ? error.message : String(error),
              });
              controller.error(error);
            }
          };
          entry.port.start();
        },
        cancel() {
          entry.port.postMessage({ error: "Browser download was cancelled" });
          entry.port.close();
        },
      });
      const filename = safeName(entry.name);
      return new Response(stream, {
        headers: {
          "Cache-Control": "no-store",
          "Content-Type": "application/octet-stream",
          "Content-Disposition": `attachment; filename="${filename}"; filename*=UTF-8''${encodeURIComponent(filename)}`,
          "X-Content-Type-Options": "nosniff",
        },
      });
    })(),
  );
});
