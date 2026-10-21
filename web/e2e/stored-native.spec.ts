import { execFile } from "node:child_process";
import { promisify } from "node:util";
import { promises as fs } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { createCipheriv, createDecipheriv, hkdfSync } from "node:crypto";
import {
  expect,
  test,
  type BrowserContext,
  type Download,
  type Page,
  type TestInfo,
} from "@playwright/test";

const exec = promisify(execFile);
const webDirectory = fileURLToPath(new URL("..", import.meta.url));
const binary = join(webDirectory, ".e2e", process.env.CROC_E2E_BINARY_NAME!);
const wasmPath =
  /\/(?:croc(?:-ssh)?\.wasm|croc(?:-ssh)?-worker\.js|wasm_exec\.js)$/;

async function isolate(context: BrowserContext) {
  const wasm: string[] = [],
    tracking: string[] = [],
    claims: string[] = [];
  context.on("request", (request) => {
    const url = new URL(request.url());
    if (wasmPath.test(url.pathname)) wasm.push(url.pathname);
    if (/googlesyndication|analytics\.example/.test(url.hostname))
      tracking.push(request.url());
    if (/\/(?:claim|commit)$/.test(url.pathname)) claims.push(url.pathname);
  });
  await context.route("**/*", async (route) => {
    const url = new URL(route.request().url());
    if (wasmPath.test(url.pathname)) return route.abort();
    if (url.hostname !== "127.0.0.1" && url.hostname !== "localhost") {
      return route.fulfill({
        contentType: url.pathname.endsWith(".js")
          ? "text/javascript"
          : "application/json",
        body: "{}",
      });
    }
    return route.continue();
  });
  return { wasm, tracking, claims };
}

async function fixtures(info: TestInfo, large = true) {
  const directory = info.outputPath("files");
  await fs.mkdir(directory, { recursive: true });
  const contents = new Map([
    [
      "alpha.bin",
      Buffer.from(
        Uint8Array.from(
          { length: large ? 4 * 1024 * 1024 + 137 : 113 },
          (_, i) => i % 251,
        ),
      ),
    ],
    ["é-notes.txt", Buffer.from("native Store interoperability\n")],
    ["empty.dat", Buffer.alloc(0)],
  ]);
  for (const [name, bytes] of contents)
    await fs.writeFile(join(directory, name), bytes);
  return {
    contents,
    paths: [...contents.keys()].map((name) => join(directory, name)),
  };
}

async function cli(info: TestInfo, args: string[], token?: string) {
  const config = info.outputPath("config");
  await fs.mkdir(config, { recursive: true });
  return exec(
    binary,
    ["--yes", "--overwrite", "--ignore-stdin", "--disable-clipboard", ...args],
    {
      cwd: webDirectory,
      env: {
        ...process.env,
        CROC_CONFIG_DIR: config,
        ...(token ? { CROC_STORE_TOKEN: token } : {}),
      },
      timeout: 60_000,
    },
  );
}

async function uploadCLI(page: Page, info: TestInfo, paths: string[]) {
  const origin = new URL(page.url()).origin;
  const result = await cli(info, [
    "send",
    "--store",
    "--store-url",
    origin,
    ...paths,
  ]);
  const url = (result.stdout + result.stderr).match(
    /https?:\/\/\S+\/s\/[A-Za-z0-9_-]{22}#v1\.[A-Za-z0-9_-]+/,
  )?.[0];
  expect(url, result.stdout + result.stderr).toBeTruthy();
  return url!;
}

function shareParts(url: string) {
  const parsed = new URL(url),
    key = Buffer.from(parsed.hash.slice(4), "base64url");
  const derive = (label: string) =>
    Buffer.from(
      hkdfSync("sha256", key, Buffer.alloc(0), `croc-store-v1/${label}`, 32),
    );
  return { id: parsed.pathname.slice(3), origin: parsed.origin, derive };
}

async function remaining(page: Page, url: string) {
  const { id, origin, derive } = shareParts(url);
  const response = await page.request.get(
    `${origin}/api/v1/store/transfers/${id}/manifest`,
    {
      headers: {
        Authorization: `Bearer ${derive("redeem").toString("base64url")}`,
      },
    },
  );
  if (response.status() === 410) return 0;
  expect(response.ok()).toBe(true);
  return Number(response.headers()["x-croc-downloads-remaining"]);
}

async function receive(
  page: Page,
  contents: Map<string, Buffer>,
  streaming = false,
) {
  const downloads: Download[] = [];
  page.on("download", (download) => downloads.push(download));
  const panel = page.locator(".receive-panel");
  await expect(panel.getByText("Incoming transfer")).toBeVisible();
  if (streaming) {
    await page.evaluate(() =>
      Object.defineProperty(window, "showDirectoryPicker", {
        configurable: true,
        value: undefined,
      }),
    );
    // Re-render the offer after changing destination support.
    await page.getByRole("button", { name: /Switch to .* mode/ }).click();
  }
  const download = panel.getByRole("button", { name: "Download", exact: true });
  if (await download.isVisible()) await download.click();
  else
    await panel
      .getByRole("button", { name: "Accept files", exact: true })
      .click();
  await expect(panel).toContainText(/All files received and verified/, {
    timeout: 30_000,
  });
  await expect.poll(() => downloads.length).toBe(contents.size);
  for (const download of downloads) {
    const path = await download.path();
    expect(await fs.readFile(path!)).toEqual(
      contents.get(download.suggestedFilename().normalize("NFC")),
    );
  }
}

test("native browser Store → browser and CLI, with WASM blocked", async ({
  page,
  browser,
  context,
}, info) => {
  const requests = await isolate(context),
    files = await fixtures(info);
  await page.goto("/");
  const panel = page.locator(".send-panel");
  await panel.getByRole("button", { name: "Store for 1 day" }).click();
  await panel.getByLabel("Verified downloads").fill("2");
  await panel.locator('input[type="file"]').setInputFiles(files.paths);
  await panel.getByRole("button", { name: "Store 3 files" }).click();
  await expect(panel.getByText("Encrypted link ready")).toBeVisible();
  const token = await panel.getByLabel("CLI token").inputValue();
  const url = await panel.getByLabel("Browser link").inputValue();
  const receiver = await browser.newContext({
    viewport: page.viewportSize() ?? undefined,
    isMobile: info.project.use.isMobile,
    hasTouch: info.project.use.hasTouch,
  });
  try {
    const receivedRequests = await isolate(receiver),
      incoming = await receiver.newPage();
    await incoming.goto(url);
    await receive(incoming, files.contents, true);
    expect(receivedRequests.wasm).toEqual([]);
    expect(receivedRequests.tracking).toEqual([]);
    expect(await remaining(page, url)).toBe(1);
    const destination = info.outputPath("cli-received");
    await fs.mkdir(destination, { recursive: true });
    await cli(info, ["--out", destination], token);
    for (const [name, expected] of files.contents)
      expect(await fs.readFile(join(destination, name))).toEqual(expected);
    expect(await remaining(page, url)).toBe(0);
    expect(requests.wasm).toEqual([]);
  } finally {
    await receiver.close();
  }
});

test("native CLI Store → browser with WASM blocked", async ({
  page,
  context,
}, info) => {
  const requests = await isolate(context),
    files = await fixtures(info);
  await page.goto("/");
  const url = await uploadCLI(page, info, files.paths);
  await page.goto(url);
  await receive(page, files.contents);
  expect(await remaining(page, url)).toBe(0);
  expect(requests.wasm).toEqual([]);
});

test("cold preview is fast and refresh, refusal, cancellation spend nothing", async ({
  page,
  context,
}, info) => {
  const requests = await isolate(context),
    files = await fixtures(info, false);
  await page.goto("/");
  const url = await uploadCLI(page, info, files.paths);
  let release: (() => void) | undefined;
  let hold = false;
  await page.route("**/manifest", async (route) => {
    if (hold)
      await new Promise<void>((resolve) => {
        release = resolve;
      });
    else await new Promise((resolve) => setTimeout(resolve, 200));
    await route.continue().catch(() => {});
  });
  requests.tracking.length = 0;
  const started = Date.now();
  const response = await page.goto(url);
  await expect(page.getByText("Incoming transfer")).toBeVisible({
    timeout: 2000,
  });
  const previewMs = Date.now() - started;
  expect(previewMs).toBeLessThan(2000);
  await info.attach("preview-latency", {
    body: JSON.stringify({ browser: info.project.name, previewMs }),
    contentType: "application/json",
  });
  expect(response!.headers()["cache-control"]).toBe("no-store");
  expect(response!.headers()["referrer-policy"]).toBe("no-referrer");
  expect(await response!.text()).not.toMatch(
    /googlesyndication|analytics\.example/,
  );
  await page.getByRole("button", { name: "Refuse", exact: true }).click();
  hold = true;
  await page.reload({ waitUntil: "domcontentloaded" });
  await page
    .getByRole("button", { name: "Cancel receive", exact: true })
    .click();
  await expect(page.getByText("Transfer cancelled")).toBeVisible();
  release?.();
  await page.waitForTimeout(300);
  await expect(page.getByText("Incoming transfer")).toHaveCount(0);
  expect(await remaining(page, url)).toBe(1);
  expect(requests.claims).toEqual([]);
  expect(requests.wasm).toEqual([]);
  expect(requests.tracking).toEqual([]);
});

for (const corruption of ["chunk", "hash"])
  test(`native ${corruption} authentication failure never commits`, async ({
    page,
    context,
  }, info) => {
    const requests = await isolate(context),
      files = await fixtures(info, false);
    await page.goto("/");
    const url = await uploadCLI(page, info, files.paths);
    const { id, derive } = shareParts(url);
    // WebKit bypasses Playwright routing for requests from controlled pages.
    // Use the Blob fallback here so corruption is actually injected; the
    // successful streaming destination is exercised by the round-trip tests.
    await page.addInitScript(() => Object.defineProperty(navigator, "brave", {
      configurable: true, value: { isBrave: async () => true },
    }));
    let corrupted = false;
    await page.route(
      corruption === "chunk" ? "**/chunks/0" : "**/manifest",
      async (route) => {
        corrupted = true;
        const response = await route.fetch();
        let body = await response.body();
        if (corruption === "chunk") body[body.length - 1] ^= 1;
        else {
          const key = derive("manifest"),
            aad = Buffer.from(`croc-store-v1\0${id}\0manifest`);
          const decrypt = createDecipheriv(
            "aes-256-gcm",
            key,
            body.subarray(0, 12),
          );
          decrypt.setAAD(aad);
          decrypt.setAuthTag(body.subarray(-16));
          const manifest = JSON.parse(
            Buffer.concat([
              decrypt.update(body.subarray(12, -16)),
              decrypt.final(),
            ]).toString(),
          );
          manifest.f[0].h = Buffer.alloc(32).toString("base64url");
          const nonce = Buffer.alloc(12, 91),
            encrypt = createCipheriv("aes-256-gcm", key, nonce);
          encrypt.setAAD(aad);
          body = Buffer.concat([
            nonce,
            encrypt.update(JSON.stringify(manifest)),
            encrypt.final(),
            encrypt.getAuthTag(),
          ]);
        }
        await route.fulfill({
          response,
          body,
          headers: {
            ...response.headers(),
            "content-length": String(body.length),
          },
        });
      },
    );
    await page.goto(url);
    await expect(page.getByText("Incoming transfer")).toBeVisible();
    const download = page.getByRole("button", {
      name: "Download",
      exact: true,
    });
    if (await download.isVisible()) await download.click();
    else
      await page
        .getByRole("button", { name: "Accept files", exact: true })
        .click();
    await expect(page.getByRole("alert")).toContainText(
      /authentication failed|SHA-256 verification/,
    );
    expect(corrupted).toBe(true);
    expect(requests.claims.some((path) => path.endsWith("/commit"))).toBe(
      false,
    );
    expect(await remaining(page, url)).toBe(1);
    expect(requests.wasm).toEqual([]);
  });
