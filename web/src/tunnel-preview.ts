import * as cssTree from "css-tree";
import shimSource from "es-module-shims?raw";
import runtimeSource from "./tunnel-runtime.js?raw";
import { TunnelSession, tunnelLimit } from "./protocol/tunnel";
import { errorMessage } from "./protocol/bytes";

export type PreviewNavigation = {
  url: string;
  method?: string;
  headers?: Record<string, string[]>;
  body?: Uint8Array;
};
type Options = {
  navigate(next: PreviewNavigation): void;
  error(message: string): void;
};
const escapeScript = (value: string) =>
  value.replace(/<\/script/gi, "<\\/script");
const escapeAttribute = (value: string) =>
  value
    .replaceAll("&", "&amp;")
    .replaceAll('"', "&quot;")
    .replaceAll("<", "&lt;");

// The outer document owns all network capabilities; the opaque preview gets
// only this per-frame message port. No application HTML executes in the parent.
export class TunnelPreview {
  private controller = new AbortController();
  private port?: MessagePort;
  private assets = new Map<string, Promise<string>>();
  private assetBytes = 0;
  private acknowledgements = new Map<number, () => void>();
  private sockets = new Map<
    number,
    Awaited<ReturnType<TunnelSession["websocket"]>>
  >();
  private requests = new Map<number, AbortController>();
  private origin: string;
  private currentURL: string;
  private stopped = false;
  constructor(
    private frame: HTMLIFrameElement,
    private session: TunnelSession,
    port: number,
    private options: Options,
  ) {
    this.origin = `http://localhost:${port}`;
    this.currentURL = this.origin + "/";
  }
  private url(value: string, base = this.currentURL) {
    const url = new URL(value, base);
    if (url.origin !== this.origin || url.username || url.password)
      throw new Error(
        "This preview supports resources on the shared port only",
      );
    return url.href;
  }
  private async fetch(
    value: string,
    init: Parameters<TunnelSession["fetch"]>[1] = {},
    hops = 0,
  ): Promise<Response> {
    const target = this.url(value);
    const response = await this.session.fetch(target, {
      ...init,
      signal: init?.signal ?? this.controller.signal,
    });
    if (
      [301, 302, 303, 307, 308].includes(response.status) &&
      response.headers.has("location")
    ) {
      void response.body?.cancel();
      if (hops >= 10) throw new Error("Too many preview redirects");
      const next = this.url(response.headers.get("location")!, target);
      const get =
        response.status === 303 ||
        ([301, 302].includes(response.status) && init?.method === "POST");
      return this.fetch(next, get ? { signal: init?.signal } : init, hops + 1);
    }
    Object.defineProperty(response, "url", { value: target });
    return response;
  }
  private async dataURL(bytes: Uint8Array, type: string) {
    this.assetBytes += bytes.length;
    if (this.assetBytes > tunnelLimit)
      throw new Error("Preview assets exceed the 64 MiB session limit");
    return new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result));
      reader.onerror = () =>
        reject(new Error("Could not prepare preview asset"));
      reader.readAsDataURL(new Blob([bytes.slice().buffer], { type }));
    });
  }
  private asset(
    value: string,
    base = this.currentURL,
    depth = 0,
  ): Promise<string> {
    if (value.startsWith("data:")) return Promise.resolve(value);
    const url = this.url(value, base);
    if (depth > 8)
      return Promise.reject(new Error("Stylesheet import nesting is too deep"));
    let pending = depth === 0 ? this.assets.get(url) : undefined;
    if (!pending) {
      pending = (async () => {
        const response = await this.fetch(url);
        if (!response.ok)
          throw new Error(`Asset returned HTTP ${response.status}`);
        const type =
          response.headers.get("content-type") ?? "application/octet-stream";
        let bytes = new Uint8Array(await response.arrayBuffer());
        if (type.startsWith("text/css"))
          bytes = new TextEncoder().encode(
            await this.css(new TextDecoder().decode(bytes), url, depth + 1),
          );
        return this.dataURL(bytes, type);
      })();
      this.assets.set(url, pending);
    }
    return pending;
  }
  private async css(source: string, base = this.currentURL, depth = 0) {
    const tree = cssTree.parse(source, {
      context:
        source.includes("{") || source.trimStart().startsWith("@")
          ? "stylesheet"
          : "declarationList",
    });
    const references: Array<{ value: string }> = [];
    cssTree.walk(tree, (node) => {
      if (node.type === "Url") references.push(node);
      if (
        node.type === "Atrule" &&
        node.name.toLowerCase() === "import" &&
        node.prelude?.type === "AtrulePrelude"
      ) {
        const first = node.prelude.children.first;
        if (first?.type === "String") references.push(first);
      }
    });
    // Resolve in order so cycles cannot wait on their own cached promises.
    for (const node of references) {
      if (node.value.startsWith("#") || node.value.startsWith("data:"))
        continue;
      const target = this.url(node.value, base);
      if (target === base) throw new Error("Cyclic stylesheet asset reference");
      node.value = await this.asset(target, base, depth);
    }
    return cssTree.generate(tree);
  }
  async load(navigation: PreviewNavigation) {
    this.currentURL = this.url(navigation.url);
    const response = await this.fetch(this.currentURL, navigation);
    if (!response.ok)
      throw new Error(`Local page returned HTTP ${response.status}`);
    if (!(response.headers.get("content-type") ?? "").includes("text/html"))
      throw new Error(
        "This port is not serving an HTML page. Use CLI forwarding for other services.",
      );
    this.currentURL = response.url;
    const template = document.createElement("template");
    template.innerHTML = await response.text();
    template.content
      .querySelectorAll(
        "base, meta[http-equiv], iframe, object, embed, link[rel='preconnect'], link[rel='dns-prefetch']",
      )
      .forEach((node) => node.remove());
    for (const script of template.content.querySelectorAll("script")) {
      if (script.type === "module") {
        script.type = "module-shim";
        if (script.hasAttribute("src"))
          script.src = this.url(script.getAttribute("src")!);
      } else if (script.type === "importmap") script.type = "importmap-shim";
      else if (script.hasAttribute("src")) {
        const loaded = await this.fetch(this.url(script.getAttribute("src")!));
        if (!loaded.ok)
          throw new Error(`Script returned HTTP ${loaded.status}`);
        script.removeAttribute("src");
        script.textContent = escapeScript(await loaded.text());
      }
      script.removeAttribute("integrity");
      script.removeAttribute("crossorigin");
    }
    for (const style of template.content.querySelectorAll("style"))
      style.textContent = await this.css(style.textContent ?? "");
    for (const element of template.content.querySelectorAll("*")) {
      if (element.hasAttribute("style"))
        element.setAttribute(
          "style",
          await this.css(element.getAttribute("style")!),
        );
      if (element.tagName === "SCRIPT") continue;
      for (const attribute of ["src", "poster", "srcset"]) {
        const value = element.getAttribute(attribute);
        if (!value) continue;
        if (attribute === "srcset") {
          element.setAttribute("data-croc-srcset", value);
          element.removeAttribute(attribute);
        } else element.setAttribute(attribute, await this.asset(value));
      }
      if (element.tagName === "LINK") {
        const rel = element.getAttribute("rel");
        if (rel === "modulepreload") {
          element.remove();
          continue;
        }
        const href = element.getAttribute("href");
        if (href) element.setAttribute("href", await this.asset(href));
        element.removeAttribute("integrity");
        element.removeAttribute("crossorigin");
      }
    }
    if (this.stopped) return;
    const channel = new MessageChannel();
    this.port = channel.port1;
    this.port.onmessage = (event) => {
      void this.message(event.data);
    };
    this.port.start();
    const boot = `window.__crocPreviewURL=${JSON.stringify(this.currentURL).replaceAll("<", "\\u003c")};\n${runtimeSource}`;
    const policy = `default-src 'none'; script-src 'unsafe-inline' 'wasm-unsafe-eval' blob:; connect-src 'none'; img-src data: blob:; media-src data: blob:; font-src data: blob:; style-src 'unsafe-inline' data: blob:; frame-src 'none'; worker-src 'none'; object-src 'none'; form-action 'none'; base-uri ${this.origin}`;
    this.frame.onload = () => {
      if (!this.stopped) {
        this.frame.contentWindow!.postMessage(
          { type: "croc-preview-init" },
          "*",
          [channel.port2],
        );
        this.frame.onload = null;
      }
    };
    this.frame.srcdoc = `<meta http-equiv="Content-Security-Policy" content="${escapeAttribute(policy)}"><base href="${escapeAttribute(this.currentURL)}"><script>${escapeScript(boot)}</script><script>${escapeScript(shimSource)}</script>${template.innerHTML}`;
  }
  private send(
    message: Record<string, unknown>,
    transfer: Transferable[] = [],
  ) {
    if (!this.stopped) this.port?.postMessage(message, transfer);
  }
  private async message(message: Record<string, unknown>) {
    const id = Number(message.id);
    if (!Number.isSafeInteger(id) || id < 1 || this.stopped) return;
    const type = message.type;
    if (type === "ack") {
      this.acknowledgements.get(id)?.();
      this.acknowledgements.delete(id);
      return;
    }
    if (type === "cancel") {
      this.requests.get(id)?.abort();
      this.acknowledgements.get(id)?.();
      return;
    }
    try {
      if (type === "fetch") {
        if (this.requests.size >= 64 || this.requests.has(id))
          throw new Error("Too many preview requests");
        const controller = new AbortController();
        this.requests.set(id, controller);
        const stop = () => controller.abort();
        this.controller.signal.addEventListener("abort", stop, { once: true });
        try {
          const body =
            message.body instanceof Uint8Array ? message.body : undefined;
          const response = await this.fetch(String(message.url), {
            method: String(message.method || "GET"),
            headers: message.headers as Record<string, string[]>,
            body,
            signal: controller.signal,
          });
          this.send({
            id,
            type: "headers",
            status: response.status,
            headers: [...response.headers],
            url: response.url,
          });
          const reader = response.body?.getReader();
          if (reader)
            for (;;) {
              const { done, value } = await reader.read();
              if (done || this.stopped) break;
              await new Promise<void>((resolve) => {
                this.acknowledgements.set(id, resolve);
                this.send({ id, type: "chunk", bytes: value }, [value.buffer]);
              });
              if (controller.signal.aborted) {
                await reader.cancel();
                break;
              }
            }
          this.send({ id, type: "end" });
        } finally {
          this.requests.delete(id);
          this.controller.signal.removeEventListener("abort", stop);
        }
      } else if (type === "asset")
        this.send({
          id,
          type: "result",
          value: await this.asset(String(message.url)),
        });
      else if (type === "css")
        this.send({
          id,
          type: "result",
          value: await this.css(
            String(message.source),
            this.url(String(message.base || this.currentURL)),
          ),
        });
      else if (type === "navigate") {
        this.options.navigate({
          url: this.url(String(message.url)),
          method: String(message.method || "GET"),
          headers: message.headers as Record<string, string[]>,
          body: message.body instanceof Uint8Array ? message.body : undefined,
        });
      } else if (type === "ws-open") {
        if (this.sockets.size >= 64 || this.sockets.has(id))
          throw new Error("Too many WebSockets");
        const socket = await this.session.websocket(
          String(message.url),
          message.protocols as string[],
          (bytes, binary) =>
            this.send({ id, type: "ws-message", bytes, binary }),
          (code, reason) => {
            this.sockets.delete(id);
            this.send({ id, type: "ws-close", code, reason });
          },
        );
        if (this.stopped) {
          await socket.close();
          return;
        }
        this.sockets.set(id, socket);
        this.send({ id, type: "ws-opened", protocol: socket.protocol });
      } else if (type === "ws-send") {
        const socket = this.sockets.get(Number(message.socket));
        if (!socket) throw new Error("WebSocket is closed");
        await socket.send(message.bytes as Uint8Array, Boolean(message.binary));
        this.send({ id, type: "result" });
      } else if (type === "ws-close") {
        const socket = this.sockets.get(id);
        this.sockets.delete(id);
        await socket?.close();
      } else if (type === "error") this.options.error(String(message.message));
    } catch (error) {
      this.send({ id, type: "error", message: errorMessage(error) });
    }
  }
  close() {
    this.stopped = true;
    this.controller.abort();
    for (const done of this.acknowledgements.values()) done();
    this.acknowledgements.clear();
    for (const socket of this.sockets.values()) void socket.close();
    this.sockets.clear();
    this.requests.clear();
    this.port?.close();
    this.assets.clear();
    this.frame.onload = null;
    this.frame.srcdoc = "";
  }
}
