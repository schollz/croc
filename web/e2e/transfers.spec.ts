import { spawn } from "node:child_process";
import { promises as fs } from "node:fs";
import { basename, dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import {
  expect,
  test,
  type Download,
  type Locator,
  type Page,
  type TestInfo,
} from "@playwright/test";

const webDirectory = dirname(dirname(fileURLToPath(import.meta.url)));
const crocBinary = join(
  webDirectory,
  ".e2e",
  process.env.CROC_E2E_BINARY_NAME ?? "croc",
);
const relayPorts = process.env.CROC_E2E_RELAY_PORTS?.split(",") ?? [];
const relayAddress =
  process.env.CROC_E2E_RELAY_ADDRESS ?? `127.0.0.1:${relayPorts[0]}`;
const webAddress = process.env.CROC_E2E_WEB_ADDRESS ?? "/";
const relayPassword = "pass123";
const transferTimeout = 60_000;

type FixtureSet = {
  paths: string[];
  contents: Map<string, Buffer>;
};

type RunningCroc = {
  child: ReturnType<typeof spawn>;
  done: Promise<void>;
  stop(): void;
};

function patternedBytes(size: number, seed: number) {
  const bytes = Buffer.alloc(size);
  for (let index = 0; index < bytes.length; index += 1) {
    bytes[index] = (index * 31 + seed * 47) % 251;
  }
  return bytes;
}

async function createFixtures(testInfo: TestInfo): Promise<FixtureSet> {
  const directory = testInfo.outputPath("fixtures");
  await fs.mkdir(directory, { recursive: true });
  const contents = new Map<string, Buffer>([
    // This file spans every advertised data connection at least once.
    ["alpha.bin", patternedBytes(32 * 1024 * 6 + 137, 1)],
    ["beta.bin", patternedBytes(32 * 1024 * 2 + 19, 2)],
    ["empty.dat", Buffer.alloc(0)],
  ]);
  for (const [name, bytes] of contents) {
    await fs.writeFile(join(directory, name), bytes);
  }
  return {
    paths: [...contents.keys()].map((name) => join(directory, name)),
    contents,
  };
}

async function createCrocBinaryFixture(): Promise<FixtureSet> {
  return {
    paths: [crocBinary],
    contents: new Map([[basename(crocBinary), await fs.readFile(crocBinary)]]),
  };
}

function runCroc(
  args: string[],
  secret: string,
  configDirectory: string,
  executable = crocBinary,
): RunningCroc {
  let output = "";
  const child = spawn(executable, args, {
    cwd: webDirectory,
    env: {
      ...process.env,
      CROC_CONFIG_DIR: configDirectory,
      CROC_SECRET: secret,
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stdout?.on("data", (chunk) => {
    output += chunk.toString();
  });
  child.stderr?.on("data", (chunk) => {
    output += chunk.toString();
  });
  const done = new Promise<void>((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", (code, signal) => {
      if (code === 0) resolve();
      else {
        reject(
          new Error(
            `croc exited with ${signal ?? code ?? "unknown"}\n${output.trim()}`,
          ),
        );
      }
    });
  });
  return {
    child,
    done,
    stop() {
      if (child.exitCode === null && child.signalCode === null) {
        child.kill("SIGTERM");
      }
    },
  };
}

function commonCLIArgs() {
  return [
    "--relay",
    relayAddress,
    "--relay6",
    "",
    "--pass",
    relayPassword,
    "--yes",
    "--overwrite",
    "--ignore-stdin",
    "--disable-clipboard",
  ];
}

async function configurePage(page: Page) {
  await page.goto(webAddress);
  await page.locator("details.settings > summary").click();
  await page.getByLabel("CLI relay address").fill(relayAddress);
  await page.getByLabel("WebSocket gateway").fill("/ws");
  await page
    .getByRole("textbox", { name: "Relay password", exact: true })
    .fill(relayPassword);
  await expect(page.getByLabel("CLI relay address")).toHaveValue(relayAddress);
}

async function prepareWebSender(
  page: Page,
  secret: string,
  fixtures: FixtureSet,
) {
  const panel = page.locator(".send-panel");
  await panel.locator('input[type="file"]').setInputFiles(fixtures.paths);
  await panel.getByLabel("Croc code").fill(secret);
  await expect(panel.getByText("3 files", { exact: true })).toBeVisible();
  return panel;
}

async function connectWebReceiver(page: Page, secret: string) {
  const panel = page.locator(".receive-panel");
  await panel.getByLabel("Croc code").fill(secret);
  await panel.getByRole("button", { name: "Connect and review" }).click();
  return panel;
}

async function acceptAsDownloads(page: Page, panel: Locator) {
  const downloads: Download[] = [];
  page.on("download", (download) => downloads.push(download));
  await expect(panel.getByText("Incoming transfer")).toBeVisible();
  const fallback = panel.getByRole("button", { name: "Download", exact: true });
  if (await fallback.isVisible()) {
    await fallback.click();
  } else {
    await panel.getByRole("button", { name: "Accept files" }).click();
  }
  return downloads;
}

async function expectDownloads(
  downloads: Download[],
  fixtures: FixtureSet,
) {
  await expect
    .poll(() => downloads.length, { timeout: transferTimeout })
    .toBe(fixtures.contents.size);
  const actual = new Map<string, Buffer>();
  for (const download of downloads) {
    const path = await download.path();
    expect(path, `download path for ${download.suggestedFilename()}`).not.toBeNull();
    actual.set(download.suggestedFilename(), await fs.readFile(path!));
  }
  expect([...actual.keys()].sort()).toEqual([...fixtures.contents.keys()].sort());
  for (const [name, expected] of fixtures.contents) {
    expect(actual.get(name)).toEqual(expected);
  }
}

async function expectDirectory(
  directory: string,
  fixtures: FixtureSet,
) {
  expect((await fs.readdir(directory)).sort()).toEqual(
    [...fixtures.contents.keys()].sort(),
  );
  for (const [name, expected] of fixtures.contents) {
    expect(await fs.readFile(join(directory, name))).toEqual(expected);
  }
}

async function expectTransferMetrics(panel: Locator) {
  const metrics = panel.locator(".progress-metrics");
  await expect(metrics).toContainText(/Rate.*\/s.*ETA/);
}

test.describe.configure({ mode: "serial" });

test("publishes rich metadata and project links", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "platform", {
      configurable: true,
      value: "MacIntel",
    });
  });
  await page.route(
    "https://api.github.com/repos/schollz/croc/releases/latest",
    async (route) => {
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          tag_name: "v99.0.0",
          html_url: "https://github.com/schollz/croc/releases/tag/v99.0.0",
          assets: [
            {
              name: "croc_v99.0.0_macOS-64bit.tar.gz",
              browser_download_url: "https://example.test/croc-macos-intel",
            },
            {
              name: "croc_v99.0.0_macOS-ARM64.tar.gz",
              browser_download_url: "https://example.test/croc-macos-arm",
            },
          ],
        }),
      });
    },
  );
  await page.goto("/");
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
    "href",
    "https://getcroc.com/",
  );
  await expect(page.locator('meta[property="og:title"]')).toHaveAttribute(
    "content",
    /croc web/i,
  );
  const structuredData = JSON.parse(
    (await page.locator('script[type="application/ld+json"]').textContent()) ?? "{}",
  ) as { "@type"?: string };
  expect(structuredData["@type"]).toBe("WebApplication");
  await expect(
    page.getByRole("link", { name: "View croc on GitHub" }),
  ).toHaveAttribute("href", "https://github.com/schollz/croc");
  await expect(
    page.getByRole("link", { name: /Read how croc works/i }),
  ).toHaveAttribute("href", "https://schollz.com/croc6/");
  await expect(
    page.getByRole("heading", { name: "Download croc for macOS." }),
  ).toBeVisible();
  await expect(
    page.getByText("Detected macOS. Release assets come directly from GitHub."),
  ).toHaveCount(0);
  await expect(
    page.locator('a[href="https://example.test/croc-macos-intel"]'),
  ).toBeVisible();
  await expect(
    page.locator('a[href="https://example.test/croc-macos-arm"]'),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "Other releases" }),
  ).toHaveAttribute(
    "href",
    "https://github.com/schollz/croc/releases/latest",
  );
});

test("serves the installer to curl and the app to browsers", async ({
  page,
  request,
}) => {
  const installer = await request.get("/", {
    headers: { "User-Agent": "curl/8.10.1" },
  });
  expect(installer.ok()).toBe(true);
  expect(installer.headers()["content-type"]).toBe("text/plain; charset=utf-8");
  expect(installer.headers()["cache-control"]).toBe("no-store");
  expect(installer.headers()["vary"]).toBe("User-Agent");
  expect(await installer.text()).toMatch(
    /^#!\/bin\/bash[\s\S]*croc_version="/,
  );

  await page.goto("/");
  await expect(
    page.getByRole("heading", { name: "Send files, secured end-to-end." }),
  ).toBeVisible();
  expect((await page.locator("html").textContent()) ?? "").not.toContain(
    "croc Installer Script",
  );
});

test("help tour explains browser transfers and end-to-end encryption", async ({
  page,
}) => {
  await page.emulateMedia({ colorScheme: "light" });
  await page.route(
    "https://api.github.com/repos/schollz/croc/releases/latest",
    async (route) => {
      await route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          tag_name: "v99.0.0",
          html_url: "https://github.com/schollz/croc/releases/tag/v99.0.0",
          assets: [],
        }),
      });
    },
  );
  await page.goto("/");
  const helpButton = page.getByRole("button", { name: "How to use croc web" });
  const githubButton = page.getByRole("link", { name: "View croc on GitHub" });
  const themeButton = page.getByRole("button", { name: /Switch to .* mode/ });
  const [helpBox, githubBox, themeBox] = await Promise.all([
    helpButton.boundingBox(),
    githubButton.boundingBox(),
    themeButton.boundingBox(),
  ]);
  expect([helpBox?.width, githubBox?.width, themeBox?.width]).toEqual([
    42, 42, 42,
  ]);
  expect([helpBox?.height, githubBox?.height, themeBox?.height]).toEqual([
    42, 42, 42,
  ]);
  await expect(
    helpButton.locator("svg.lucide-circle-question-mark"),
  ).toHaveCount(1);
  await helpButton.click();

  const tour = page.locator(".driver-popover.croc-tour");
  const title = tour.locator(".driver-popover-title");
  const description = tour.locator(".driver-popover-description");
  await expect(description).toHaveCSS("font-size", "12px");
  await expect(description).toHaveCSS("font-weight", "500");
  await expect(description).toHaveCSS("color", "rgb(0, 0, 0)");
  const next = tour.getByRole("button", { name: "Next" });
  const steps = [
    "Welcome to croc web",
    "Send one or several files",
    "Receive and review",
    "The code creates the encryption key",
    "The gateway cannot read your files",
    "Use another relay when needed",
    "Works with the croc CLI",
    "Simple by design",
  ];

  for (const [index, expectedTitle] of steps.entries()) {
    await expect(title).toHaveText(expectedTitle);
    if (expectedTitle === "The code creates the encryption key") {
      await expect(tour).toContainText(
        "password-authenticated key exchange (PAKE)",
      );
      await expect(tour).toContainText(
        "encrypted before leaving the browser",
      );
    }
    if (expectedTitle === "The gateway cannot read your files") {
      await expect(tour).toContainText(
        "cannot decrypt filenames or file contents",
      );
    }
    if (index < steps.length - 1) {
      await next.click();
    }
  }

  await tour.getByRole("button", { name: "Done" }).click();
  await expect(tour).toHaveCount(0);
});

test("copying a croc code shows confirmation", async ({ page }) => {
  await configurePage(page);
  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: async () => undefined },
    });
  });
  const panel = page.locator(".send-panel");
  await panel.getByLabel("Croc code").fill("1234-copy-test-code");
  await panel.getByRole("button", { name: "Copy code" }).click();
  await expect(panel.getByRole("status")).toHaveText("Copied");
  await expect(panel.getByRole("button", { name: "Code copied" })).toBeVisible();
});

test("CLI → Web transfers and verifies multiple files", async ({
  page,
}, testInfo) => {
  const secret = "1111-cli-to-web-e2e";
  const fixtures = await createFixtures(testInfo);
  const configDirectory = testInfo.outputPath("croc-config");
  await fs.mkdir(configDirectory, { recursive: true });
  await configurePage(page);
  await page.goto(`/?code=${encodeURIComponent(secret)}`);
  await expect(page.locator(".send-panel")).toHaveCount(0);
  const receivePanel = page.locator(".receive-panel");
  await expect(receivePanel.getByLabel("Croc code")).toHaveValue(secret);
  await expect(
    receivePanel.getByRole("button", { name: "Cancel receive" }),
  ).toBeVisible();
  const cli = runCroc(
    [...commonCLIArgs(), "send", "--no-local", ...fixtures.paths],
    secret,
    configDirectory,
    process.env.CROC_E2E_SENDER_BINARY,
  );
  try {
    const downloads = await acceptAsDownloads(page, receivePanel);
    const metricsVisible = expectTransferMetrics(receivePanel);
    await expect(receivePanel).toContainText("All files received and verified", {
      timeout: transferTimeout,
    });
    await cli.done;
    await metricsVisible;
    await expectDownloads(downloads, fixtures);
  } finally {
    cli.stop();
    await cli.done.catch(() => undefined);
  }
});

test("CLI → Web verifies a large croc executable", async ({
  page,
}, testInfo) => {
  const secret = "1112-large-cli-to-web-e2e";
  const fixtures = await createCrocBinaryFixture();
  const configDirectory = testInfo.outputPath("croc-config");
  await fs.mkdir(configDirectory, { recursive: true });
  await configurePage(page);
  const receivePanel = await connectWebReceiver(page, secret);
  const cli = runCroc(
    [...commonCLIArgs(), "send", "--no-local", ...fixtures.paths],
    secret,
    configDirectory,
    process.env.CROC_E2E_SENDER_BINARY,
  );
  try {
    const downloads = await acceptAsDownloads(page, receivePanel);
    await expect(receivePanel).toContainText("All files received and verified", {
      timeout: transferTimeout,
    });
    await cli.done;
    await expectDownloads(downloads, fixtures);
  } finally {
    cli.stop();
    await cli.done.catch(() => undefined);
  }
});

test("Web → CLI transfers and verifies multiple files", async ({
  page,
}, testInfo) => {
  const secret = "2222-web-to-cli-e2e";
  const fixtures = await createFixtures(testInfo);
  const destination = testInfo.outputPath("received");
  const configDirectory = testInfo.outputPath("croc-config");
  await Promise.all([
    fs.mkdir(destination, { recursive: true }),
    fs.mkdir(configDirectory, { recursive: true }),
  ]);
  await configurePage(page);
  const sendPanel = await prepareWebSender(page, secret, fixtures);
  await sendPanel.getByRole("button", { name: "Send 3 files" }).click();
  const cli = runCroc(
    [...commonCLIArgs(), "--out", destination],
    secret,
    configDirectory,
  );
  try {
    const metricsVisible = expectTransferMetrics(sendPanel);
    await expect(sendPanel).toContainText("All files arrived safely", {
      timeout: transferTimeout,
    });
    await cli.done;
    await metricsVisible;
    await expectDirectory(destination, fixtures);
  } finally {
    cli.stop();
    await cli.done.catch(() => undefined);
  }
});

test("Web → Web transfers and verifies multiple files", async ({
  browser,
}, testInfo) => {
  const secret = "3333-web-to-web-e2e";
  const fixtures = await createFixtures(testInfo);
  const senderContext = await browser.newContext({ acceptDownloads: true });
  const receiverContext = await browser.newContext({ acceptDownloads: true });
  const senderPage = await senderContext.newPage();
  const receiverPage = await receiverContext.newPage();
  try {
    await Promise.all([
      configurePage(senderPage),
      configurePage(receiverPage),
    ]);
    const sendPanel = await prepareWebSender(senderPage, secret, fixtures);
    const receivePanel = await connectWebReceiver(receiverPage, secret);
    await sendPanel.getByRole("button", { name: "Send 3 files" }).click();
    const downloads = await acceptAsDownloads(receiverPage, receivePanel);
    await Promise.all([
      expect(sendPanel).toContainText("All files arrived safely", {
        timeout: transferTimeout,
      }),
      expect(receivePanel).toContainText("All files received and verified", {
        timeout: transferTimeout,
      }),
    ]);
    await expectDownloads(downloads, fixtures);
  } finally {
    await Promise.all([senderContext.close(), receiverContext.close()]);
  }
});
