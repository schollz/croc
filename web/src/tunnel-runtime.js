/* Runs only inside the opaque preview iframe. Network access is mediated by
   the outer croc document; CSP blocks direct fetches, sockets, and resource loads. */
(() => {
  const limit = 64 * 1024 * 1024;
  let sequence = 0,
    port;
  const pending = new Map(),
    sockets = new Map();
  let ready;
  const initialized = new Promise((resolve) => {
    ready = resolve;
  });
  const base = window.__crocPreviewURL;
  delete window.__crocPreviewURL;
  const nativeSet = Element.prototype.setAttribute;
  const nativeText = Object.getOwnPropertyDescriptor(
    Node.prototype,
    "textContent",
  );
  const rawAttributes = new WeakMap();
  const knownStyles = new WeakMap();
  const report = (error) => {
    void send({
      type: "error",
      id: ++sequence,
      message: String(error?.message || error),
    });
  };
  const url = (value) => new URL(value, base).href;
  async function send(message, transfer = []) {
    await initialized;
    port.postMessage(message, transfer);
  }
  function call(type, fields) {
    const id = ++sequence;
    return new Promise((resolve, reject) => {
      pending.set(id, { resolve, reject });
      void send({ type, id, ...fields });
    });
  }
  window.addEventListener("message", (event) => {
    if (
      event.source !== parent ||
      event.data?.type !== "croc-preview-init" ||
      !event.ports[0] ||
      port
    )
      return;
    port = event.ports[0];
    port.onmessage = (event) => {
      const m = event.data;
      const socket = sockets.get(m.id);
      if (socket && (String(m.type).startsWith("ws-") || m.type === "error")) {
        socket.receive(m);
        return;
      }
      const request = pending.get(m.id);
      if (!request) return;
      if (m.type === "headers") {
        const stream = new ReadableStream(
          {
            start(controller) {
              request.controller = controller;
            },
            pull() {
              if (request.awaiting) {
                request.awaiting = false;
                void send({ type: "ack", id: m.id });
              }
            },
            cancel() {
              pending.delete(m.id);
              void send({ type: "cancel", id: m.id });
            },
          },
          { highWaterMark: 65536, size: (chunk) => chunk.byteLength },
        );
        const empty =
          request.method === "HEAD" || [204, 205, 304].includes(m.status);
        const response = new Response(empty ? null : stream, {
          status: m.status,
          headers: m.headers,
        });
        Object.defineProperty(response, "url", { value: m.url });
        request.resolve(response);
      } else if (m.type === "chunk") {
        request.controller.enqueue(m.bytes);
        if (request.controller.desiredSize > 0)
          void send({ type: "ack", id: m.id });
        else request.awaiting = true;
      } else if (m.type === "end") {
        request.controller?.close();
        pending.delete(m.id);
      } else if (m.type === "error") {
        const error = new Error(m.message);
        request.reject(error);
        request.controller?.error(error);
        pending.delete(m.id);
      } else {
        request.resolve(m.value);
        pending.delete(m.id);
      }
    };
    port.start();
    ready();
  });

  window.fetch = async (input, init) => {
    const target = input instanceof Request ? input.url : url(String(input));
    const request = new Request(
      target,
      input instanceof Request && !init ? input : init,
    );
    if (request.signal.aborted)
      throw new DOMException("Cancelled", "AbortError");
    const body = request.body
      ? new Uint8Array(await request.arrayBuffer())
      : undefined;
    if (body?.length > limit)
      throw new Error("Request exceeds 64 MiB preview limit");
    const id = ++sequence;
    const headers = {};
    request.headers.forEach((value, key) => {
      headers[key] = [value];
    });
    return new Promise((resolve, reject) => {
      pending.set(id, { resolve, reject, method: request.method });
      request.signal.addEventListener(
        "abort",
        () => {
          const entry = pending.get(id);
          if (!entry) return;
          const error = new DOMException("Cancelled", "AbortError");
          entry.reject(error);
          entry.controller?.error(error);
          pending.delete(id);
          void send({ type: "cancel", id });
        },
        { once: true },
      );
      void send(
        {
          type: "fetch",
          id,
          url: target,
          method: request.method,
          headers,
          body,
        },
        body ? [body.buffer] : [],
      );
    });
  };
  window.esmsInitOptions = {
    shimMode: true,
    fetch: window.fetch,
    onerror: report,
  };

  class TunnelSocket extends EventTarget {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;
    CONNECTING = 0;
    OPEN = 1;
    CLOSING = 2;
    CLOSED = 3;
    readyState = 0;
    protocol = "";
    extensions = "";
    binaryType = "blob";
    bufferedAmount = 0;
    onopen = null;
    onmessage = null;
    onerror = null;
    onclose = null;
    constructor(value, protocols = []) {
      super();
      this.url = url(String(value));
      this.id = ++sequence;
      this.queue = Promise.resolve();
      sockets.set(this.id, this);
      const list = typeof protocols === "string" ? [protocols] : protocols;
      void send({
        type: "ws-open",
        id: this.id,
        url: this.url,
        protocols: list,
      });
    }
    dispatch(type, event) {
      this.dispatchEvent(event);
      this["on" + type]?.(event);
    }
    receive(m) {
      if (m.type === "ws-opened") {
        if (this.readyState !== 0) return;
        this.protocol = m.protocol;
        this.readyState = 1;
        this.dispatch("open", new Event("open"));
      } else if (m.type === "ws-message") {
        const data = m.binary
          ? this.binaryType === "arraybuffer"
            ? m.bytes.buffer
            : new Blob([m.bytes])
          : new TextDecoder().decode(m.bytes);
        this.dispatch("message", new MessageEvent("message", { data }));
      } else {
        if (m.type === "error") this.dispatch("error", new Event("error"));
        this.readyState = 3;
        sockets.delete(this.id);
        this.dispatch(
          "close",
          new CloseEvent("close", {
            code: m.code || 1006,
            reason: m.reason || "",
            wasClean: m.code === 1000,
          }),
        );
      }
    }
    send(data) {
      if (this.readyState !== 1)
        throw new DOMException("WebSocket is not open", "InvalidStateError");
      const binary = typeof data !== "string";
      const size =
        typeof data === "string"
          ? new TextEncoder().encode(data).length
          : (data.size ?? data.byteLength);
      if (size > 1024 * 1024 || this.bufferedAmount + size > 4 * 1024 * 1024)
        throw new Error("WebSocket message or buffer exceeds preview limit");
      this.bufferedAmount += size;
      this.queue = this.queue
        .then(async () => {
          const bytes =
            typeof data === "string"
              ? new TextEncoder().encode(data)
              : data instanceof Blob
                ? new Uint8Array(await data.arrayBuffer())
                : new Uint8Array(
                    data.buffer ?? data,
                    data.byteOffset ?? 0,
                    data.byteLength,
                  );
          await call("ws-send", { socket: this.id, bytes, binary });
          this.bufferedAmount -= size;
        })
        .catch((error) => {
          this.dispatch("error", new Event("error"));
          this.close();
          report(error);
        });
    }
    close() {
      if (this.readyState >= 2) return;
      this.readyState = 2;
      void send({ type: "ws-close", id: this.id });
    }
  }
  window.WebSocket = TunnelSocket;

  class TunnelXHR extends EventTarget {
    readyState = 0;
    status = 0;
    statusText = "";
    response = null;
    responseText = "";
    responseURL = "";
    responseType = "";
    timeout = 0;
    withCredentials = false;
    onreadystatechange = null;
    onload = null;
    onerror = null;
    onabort = null;
    onloadend = null;
    onprogress = null;
    upload = new EventTarget();
    fire(name) {
      const event = new Event(name);
      this.dispatchEvent(event);
      this["on" + name]?.(event);
    }
    open(method, target, async = true) {
      if (!async)
        throw new Error(
          "Synchronous XMLHttpRequest is unavailable in previews",
        );
      this.method = method;
      this.target = url(target);
      this.headers = new Headers();
      this.readyState = 1;
      this.fire("readystatechange");
    }
    setRequestHeader(key, value) {
      this.headers.append(key, value);
    }
    getResponseHeader(key) {
      return this.resultHeaders?.get(key) ?? null;
    }
    getAllResponseHeaders() {
      return [...(this.resultHeaders ?? [])]
        .map(([k, v]) => k + ": " + v)
        .join("\r\n");
    }
    overrideMimeType() {}
    abort() {
      this.controller?.abort();
      this.fire("abort");
    }
    send(body = null) {
      this.controller = new AbortController();
      const timer =
        this.timeout > 0
          ? setTimeout(() => this.abort(), this.timeout)
          : undefined;
      void fetch(this.target, {
        method: this.method,
        headers: this.headers,
        body,
        signal: this.controller.signal,
      })
        .then(async (response) => {
          this.status = response.status;
          this.statusText = response.statusText;
          this.responseURL = response.url;
          this.resultHeaders = response.headers;
          this.readyState = 2;
          this.fire("readystatechange");
          if (this.responseType === "arraybuffer")
            this.response = await response.arrayBuffer();
          else if (this.responseType === "blob")
            this.response = await response.blob();
          else {
            this.responseText = await response.text();
            this.response =
              this.responseType === "json"
                ? JSON.parse(this.responseText)
                : this.responseText;
          }
          this.readyState = 4;
          this.fire("readystatechange");
          this.fire("load");
        })
        .catch(() => {
          this.readyState = 4;
          this.fire("error");
        })
        .finally(() => {
          clearTimeout(timer);
          this.fire("loadend");
        });
    }
  }
  window.XMLHttpRequest = TunnelXHR;

  const resourceAttribute = (element, name) =>
    ["src", "srcset", "poster"].includes(name) ||
    (element.tagName === "LINK" && name === "href");
  function resource(element, name, value) {
    if (!value || /^(data:|blob:)/.test(value)) {
      nativeSet.call(element, name, value);
      return;
    }
    let attributes = rawAttributes.get(element);
    if (!attributes) rawAttributes.set(element, (attributes = new Map()));
    attributes.set(name, value);
    if (element.tagName === "SCRIPT") {
      if (element.type === "module" || element.type === "module-shim") {
        element.type = "module-shim";
        nativeSet.call(element, name, url(value));
      } else
        void fetch(url(value))
          .then((response) => response.text())
          .then((source) => {
            element.removeAttribute("src");
            nativeText.set.call(element, source);
          })
          .catch(report);
      return;
    }
    if (name === "srcset") {
      const entries = value
        .split(",")
        .map((entry) => entry.trim().split(/\s+/));
      void Promise.all(
        entries.map(async ([source, ...descriptor]) =>
          [await call("asset", { url: url(source) }), ...descriptor].join(" "),
        ),
      )
        .then((result) => {
          if (attributes.get(name) === value)
            nativeSet.call(element, name, result.join(", "));
        })
        .catch(report);
    } else
      void call("asset", { url: url(value) })
        .then((result) => {
          if (attributes.get(name) === value)
            nativeSet.call(element, name, result);
        })
        .catch(report);
  }
  Element.prototype.setAttribute = function (name, value) {
    if (resourceAttribute(this, String(name).toLowerCase()))
      resource(this, String(name).toLowerCase(), String(value));
    else nativeSet.call(this, name, value);
  };
  for (const [Type, attributes] of [
    [HTMLImageElement, ["src", "srcset"]],
    [HTMLSourceElement, ["src", "srcset"]],
    [HTMLLinkElement, ["href"]],
    [HTMLScriptElement, ["src"]],
    [HTMLMediaElement, ["src"]],
    [HTMLVideoElement, ["poster"]],
  ]) {
    for (const name of attributes) {
      const descriptor = Object.getOwnPropertyDescriptor(Type.prototype, name);
      if (!descriptor?.set) continue;
      Object.defineProperty(Type.prototype, name, {
        configurable: true,
        enumerable: descriptor.enumerable,
        get() {
          return (
            rawAttributes.get(this)?.get(name) ?? descriptor.get.call(this)
          );
        },
        set(value) {
          resource(this, name, String(value));
        },
      });
    }
  }
  function styles(element) {
    const source =
      element.tagName === "STYLE"
        ? element.textContent
        : element.getAttribute("style");
    if (!source || source === knownStyles.get(element)) return;
    knownStyles.set(element, source);
    void call("css", { source, base })
      .then((result) => {
        if (knownStyles.get(element) !== source) return;
        knownStyles.set(element, result);
        if (element.tagName === "STYLE") nativeText.set.call(element, result);
        else nativeSet.call(element, "style", result);
      })
      .catch(report);
  }
  function scan(root) {
    if (!(root instanceof Element)) return;
    for (const element of [root, ...root.querySelectorAll("*")]) {
      if (element.tagName === "STYLE" || element.hasAttribute("style"))
        styles(element);
      for (const name of ["src", "poster", "srcset", "href"]) {
        if (!resourceAttribute(element, name) || element.tagName === "SCRIPT")
          continue;
        const original = element.getAttribute("data-croc-" + name);
        const value = original ?? element.getAttribute(name);
        if (original) element.removeAttribute("data-croc-" + name);
        if (value && !/^(data:|blob:)/.test(value))
          resource(element, name, value);
      }
    }
  }
  const observer = new MutationObserver((records) => {
    for (const record of records) {
      if (record.type === "childList") {
        record.addedNodes.forEach(scan);
        if (
          record.target instanceof Element &&
          record.target.tagName === "STYLE"
        )
          styles(record.target);
      } else if (record.target instanceof Element) scan(record.target);
    }
  });
  observer.observe(document.documentElement, {
    childList: true,
    subtree: true,
    attributes: true,
    attributeFilter: ["src", "srcset", "href", "poster", "style"],
  });
  document.addEventListener("DOMContentLoaded", () =>
    scan(document.documentElement),
  );
  document.addEventListener("click", (event) => {
    const anchor = event.target.closest?.("a[href]");
    if (!anchor) return;
    const href = anchor.getAttribute("href");
    if (href.startsWith("#")) return;
    event.preventDefault();
    void send({ type: "navigate", id: ++sequence, url: url(href) });
  });
  document.addEventListener("submit", (event) => {
    event.preventDefault();
    const form = event.target;
    const method = (form.method || "GET").toUpperCase();
    const target = new URL(form.getAttribute("action") || base, base);
    const data = new FormData(form, event.submitter);
    if (method === "GET") {
      target.search = new URLSearchParams(
        [...data].map(([k, v]) => [k, String(v)]),
      ).toString();
      void send({ type: "navigate", id: ++sequence, url: target.href });
    } else
      void (async () => {
        const request = new Request(target, { method, body: data });
        const body = new Uint8Array(await request.arrayBuffer());
        const headers = {};
        request.headers.forEach((v, k) => {
          headers[k] = [v];
        });
        await send(
          {
            type: "navigate",
            id: ++sequence,
            url: target.href,
            method,
            headers,
            body,
          },
          [body.buffer],
        );
      })();
  });
  window.addEventListener("error", (event) =>
    report(event.error || event.message),
  );
  window.addEventListener("unhandledrejection", (event) =>
    report(event.reason),
  );
})();
