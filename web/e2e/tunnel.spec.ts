import { spawn } from "node:child_process";
import { promises as fs } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { createServer, type ViteDevServer } from "vite";
import react from "@vitejs/plugin-react";
import { WebSocketServer } from "ws";
import { test, expect, type Page, type TestInfo } from "@playwright/test";

const web = dirname(dirname(fileURLToPath(import.meta.url)));
async function fixture(page: Page, info: TestInfo, isolation = false) {
  if (process.env.CROC_E2E_DEBUG) {
    page.on("console", (message) =>
      console.log(message.type(), message.text()),
    );
    page.on("pageerror", (error) => console.log("pageerror", error.message));
  }
  const root = await fs.mkdtemp(join(web, ".e2e", "tunnel-app-"));
  const source = (
    title: string,
  ) => `import React from 'react'; import {createRoot} from 'react-dom/client'; import './style.css';
    const {suffix} = await import('./dynamic.js');
    function App(){const [api,setAPI]=React.useState('');const [echo,setEcho]=React.useState('');
      React.useEffect(()=>{const u=new URL('/echo',document.baseURI);u.protocol='ws:';const s=new WebSocket(u);s.onopen=()=>s.send('hello');s.onmessage=e=>setEcho(e.data);return()=>s.close();},[]);
      return <><h1>${title} {suffix}</h1><img alt="asset" src="/image.svg"/><button onClick={async()=>{const r=await fetch('/api',{method:'POST',body:'from preview'});setAPI(await r.text())}}>Call API</button><p>{api}</p><p>{echo}</p><a href="/next.html">Next page</a></>;
    } createRoot(document.getElementById('root')).render(<App/>);`;
  await Promise.all([
    fs.writeFile(
      join(root, "index.html"),
      '<div id="root"></div><script type="module" src="/main.tsx"></script>',
    ),
    fs.writeFile(join(root, "main.tsx"), source("Tunnel demo")),
    fs.writeFile(join(root, "dynamic.js"), 'export const suffix="loaded";'),
    fs.writeFile(join(root, "style.css"), "h1 { color: rgb(10, 100, 30); }"),
    fs.writeFile(
      join(root, "image.svg"),
      '<svg xmlns="http://www.w3.org/2000/svg" width="12" height="12"><rect width="12" height="12" fill="green"/></svg>',
    ),
    fs.writeFile(
      join(root, "next.html"),
      '<h1>Second page</h1><form method="post" action="/form"><input name="message" value="test"/><button>Submit form</button></form>',
    ),
    fs.writeFile(
      join(root, "isolation.html"),
      `<h1>Isolated preview</h1><pre id="result"></pre><script>
      const results={};for(const [name,fn] of [['parentDOM',()=>parent.document.body],['parentStorage',()=>parent.localStorage.getItem('tunnel-sentinel')],['cookie',()=>document.cookie],['storage',()=>localStorage.length]]){try{fn();results[name]='allowed'}catch{results[name]='blocked'}}
      document.getElementById('result').textContent=JSON.stringify(results);
      try{navigator.sendBeacon('/healthz?probe=tunnel-leak','private')}catch{}
    </script>`,
    ),
  ]);
  let server: ViteDevServer | undefined;
  const sockets = new WebSocketServer({ noServer: true });
  server = await createServer({
    configFile: false,
    root,
    plugins: [
      react(),
      {
        name: "tunnel-fixture-api",
        configureServer(server) {
          server.middlewares.use((request, response, next) => {
            if (request.url !== "/api" && request.url !== "/form")
              return next();
            if (process.env.CROC_E2E_DEBUG)
              console.log("UPSTREAM", request.method, request.url);
            let body = "";
            request.on("data", (chunk) => {
              body += chunk;
            });
            request.on("end", () => {
              if (process.env.CROC_E2E_DEBUG) console.log("UPSTREAM END", body);
              response.setHeader(
                "Content-Type",
                request.url === "/form" ? "text/html" : "text/plain",
              );
              response.end(
                request.url === "/form"
                  ? "<h1>Form submitted</h1>"
                  : "API: " + body,
              );
            });
          });
        },
      },
    ],
    server: { host: "127.0.0.1", port: 0, fs: { allow: [root, web] } },
  });
  server.httpServer!.on("upgrade", (request, socket, head) => {
    if (process.env.CROC_E2E_DEBUG) console.log("UPGRADE", request.url);
    if (request.url === "/echo")
      sockets.handleUpgrade(request, socket, head, (connection) => {
        connection.on("message", (bytes) => {
          if (process.env.CROC_E2E_DEBUG)
            console.log("WS MESSAGE", String(bytes));
          connection.send("echo: " + bytes);
        });
      });
  });
  await server.listen();
  const address = server.httpServer!.address();
  if (!address || typeof address === "string")
    throw new Error("Missing fixture port");
  const config = info.outputPath("config");
  await fs.mkdir(config, { recursive: true });
  let output = "";
  const child = spawn(
    join(web, ".e2e", process.env.CROC_E2E_BINARY_NAME!),
    [
      "--relay",
      `127.0.0.1:${process.env.CROC_E2E_RELAY_PORTS!.split(",")[0]}`,
      "--pass",
      "pass123",
      "tunnel",
      "--duration",
      "3m",
      String(address.port),
    ],
    {
      cwd: web,
      env: { ...process.env, CROC_CONFIG_DIR: config, CROC_SECRET: "" },
      stdio: ["ignore", "pipe", "pipe"],
    },
  );
  child.stdout.on("data", (data) => {
    output += data;
  });
  child.stderr.on("data", (data) => {
    output += data;
  });
  const done = new Promise<void>((resolve) =>
    child.once("exit", () => resolve()),
  );
  const close = async () => {
    child.kill("SIGTERM");
    await done;
    for (const socket of sockets.clients) socket.terminate();
    sockets.close();
    await server?.close();
    await fs.rm(root, { recursive: true, force: true });
  };
  try {
    await expect.poll(() => output, { timeout: 30_000 }).toContain("Browser:");
    const invitation = new URL(output.match(/Browser:\s+(\S+)/)![1]).hash;
    const leaked: string[] = [];
    page.on("request", (request) => {
      if (request.url().includes("probe=tunnel-leak"))
        leaked.push(request.url());
    });
    await page.goto("/");
    await page.evaluate(() =>
      localStorage.setItem("tunnel-sentinel", "private"),
    );
    await page.goto("/" + invitation);
    await expect(page.locator(".tunnel-workspace")).toContainText(
      "Connected to localhost:",
      { timeout: 30_000 },
    );
    const app = page.frameLocator('iframe[title="Shared web app"]');
    if (isolation) {
      // Navigate using the trusted preview control path, as an ordinary app link would.
      await expect(
        app.getByRole("heading", { name: "Tunnel demo loaded" }),
      ).toBeVisible();
      await app.locator("body").evaluate((body) => {
        const link = document.createElement("a");
        link.href = "/isolation.html";
        link.textContent = "Isolation";
        body.append(link);
        link.click();
      });
      await expect(
        app.getByRole("heading", { name: "Isolated preview" }),
      ).toBeVisible();
    }
    return { app, close, root, source, leaked, output: () => output };
  } catch (error) {
    if (process.env.CROC_E2E_DEBUG) console.log(output);
    await close();
    throw error;
  }
}

test("CLI tunnel → Vite browser preview, API, assets, WebSocket, navigation and reload", async ({
  page,
}, info) => {
  const f = await fixture(page, info);
  try {
    await expect(
      f.app.getByRole("heading", { name: "Tunnel demo loaded" }),
    ).toHaveCSS("color", "rgb(10, 100, 30)");
    await expect
      .poll(() =>
        f.app
          .getByAltText("asset")
          .evaluate((img: HTMLImageElement) => img.naturalWidth),
      )
      .toBe(12);
    await f.app.getByRole("button", { name: "Call API" }).click();
    await expect(f.app.getByText("API: from preview")).toBeVisible();
    await expect(f.app.getByText("echo: hello")).toBeVisible();
    await fs.writeFile(join(f.root, "main.tsx"), f.source("Updated demo"));
    await expect(
      f.app.getByRole("heading", { name: "Updated demo loaded" }),
    ).toBeVisible({ timeout: 30_000 });
    await f.app.getByRole("link", { name: "Next page" }).click();
    await expect(
      f.app.getByRole("heading", { name: "Second page" }),
    ).toBeVisible();
    await f.app.getByRole("button", { name: "Submit form" }).click();
    await expect(
      f.app.getByRole("heading", { name: "Form submitted" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Previous preview page" }).click();
    await expect(
      f.app.getByRole("heading", { name: "Second page" }),
    ).toBeVisible();
    await page.getByRole("button", { name: "Disconnect", exact: true }).click();
    await expect(page.locator(".tunnel-frame")).toHaveCount(0);
  } finally {
    await f.close();
  }
});

test("Tunnel browser preview isolates the app and clears its connection", async ({
  page,
}, info) => {
  const f = await fixture(page, info, true);
  try {
    await expect(f.app.locator("#result")).toHaveText(
      JSON.stringify({
        parentDOM: "blocked",
        parentStorage: "blocked",
        cookie: "blocked",
        storage: "blocked",
      }),
    );
    expect(f.leaked).toEqual([]);
    expect(new URL(page.url()).hash).toBe("#tunnel");
    await page.getByRole("button", { name: "Disconnect", exact: true }).click();
    await expect(page.locator(".tunnel-frame")).toHaveCount(0);
    await expect(page.getByLabel("Tunnel invitation")).toHaveValue("");
    expect(
      await page.evaluate(() => localStorage.getItem("tunnel-sentinel")),
    ).toBe("private");
  } finally {
    await f.close();
  }
});
