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
  output(): string;
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
  extraEnvironment: NodeJS.ProcessEnv = {},
): RunningCroc {
  let output = "";
  const child = spawn(executable, args, {
    cwd: webDirectory,
    env: {
      ...process.env,
      CROC_CONFIG_DIR: configDirectory,
      CROC_SECRET: secret,
      ...extraEnvironment,
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
    output() {
      return output;
    },
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
  await page.getByLabel("WebSocket gateway").fill("/ws");
  await page
    .getByRole("textbox", { name: "Relay password", exact: true })
    .fill(relayPassword);
  await expect(page.getByLabel("CLI relay addresses")).toHaveValue(relayAddress);
}

async function prepareWebSender(
  page: Page,
  fixtures: FixtureSet,
) {
  const panel = page.locator(".send-panel");
  await panel.locator('input[type="file"]').setInputFiles(fixtures.paths);
  await expect(panel.getByText("3 files", { exact: true })).toBeVisible();
  return panel;
}

async function readGeneratedSecret(panel: Locator) {
  const code = panel.getByLabel("Croc code");
  await expect(code).toHaveText(/^[a-z]+(?:-[a-z]+){2,5}$/);
  return (await code.textContent())!.trim();
}

async function connectWebReceiver(page: Page, secret: string) {
  const panel = page.locator(".receive-panel");
  await panel.getByLabel("Croc code").fill(secret);
  await panel.getByLabel("Croc code").press("Enter");
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
  const domWarnings: string[] = [];
  page.on("console", (message) => {
    if (message.text().startsWith("[DOM]")) {
      domWarnings.push(message.text());
    }
  });
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
    "croc — fast, simple, secure file transfer",
  );
  await expect(page).toHaveTitle("croc — fast, simple, secure file transfer");
  const relayPassword = page.getByLabel("Relay password", { exact: true });
  await expect(relayPassword).toHaveAttribute("autocomplete", "off");
  await expect(relayPassword.locator("xpath=ancestor::form"))
    .toHaveClass("settings-grid");
  expect(domWarnings).toEqual([]);
  const structuredData = JSON.parse(
    (await page.locator('script[type="application/ld+json"]').textContent()) ?? "{}",
  ) as {
    "@type"?: string;
    aggregateRating?: {
      ratingValue?: number;
      ratingCount?: number;
      reviewCount?: number;
    };
    offers?: { price?: number };
    review?: Array<{
      author?: { "@type"?: string; name?: string };
      datePublished?: string;
      reviewBody?: string;
      reviewRating?: { ratingValue?: number };
    }>;
  };
  expect(structuredData["@type"]).toBe("WebApplication");
  expect(structuredData.offers?.price).toBe(0);
  expect(structuredData.aggregateRating).toMatchObject({
    ratingValue: 4.94,
    ratingCount: 50,
    reviewCount: 50,
  });
  expect(structuredData.review).toHaveLength(50);
  const initialReviewerNames = [
    "ioricloud",
    "gazeebo",
    "BelugaBilliam",
    "StartupTree",
    "ntoshev",
    "alexeldeib",
    "njt",
    "Deozaan",
    "poetaster",
    "Tôi_là_người_thật",
  ];
  expect(
    structuredData.review?.slice(0, 10).map((review) => review.author?.name),
  ).toEqual(initialReviewerNames);
  expect(
    structuredData.review?.slice(0, 10).map((review) => review.datePublished),
  ).toEqual([
    "2023-04-14",
    "2023-02-12",
    "2023-11-29",
    "2020-09-18",
    "2021-03-12",
    "2022-06-24",
    "2022-11-03",
    "2021-10-10",
    "2026-01-13",
    "2026-08-10",
  ]);
  expect(
    structuredData.review?.slice(0, 10).map(
      (review) => review.reviewRating?.ratingValue,
    ),
  ).toEqual([5, 4, 5, 5, 5, 5, 4, 5, 5, 4]);
  expect(
    structuredData.review?.every(
      (review) =>
        review.author?.["@type"] === "Person" &&
        /^\d{4}-\d{2}(?:-\d{2})?$/.test(review.datePublished ?? ""),
    ),
  ).toBe(true);
  expect(structuredData.review?.slice(0, 3).map((review) => review.reviewBody))
    .toEqual([
      "I use croc here a lot. Awesome binary for me",
      "Croc's my most used transfer tool",
      "Croc for easy file transfer, Linux compatible and it's quicker than ftp/SFTP",
    ]);
  const additionalReviews = [
    ["justusthane", "2026-07-12", "I've found croc to be more reliable than MW in across different network architectures"],
    ["ikeashark", "2025-10", "100% endorse croc"],
    ["janandonly", "2025-03-12", "I like https://github.com/schollz/croc"],
    ["hoppyhoppy2", "2024-08-17", "A similar project with some nice features that I use is croc"],
    ["robviren", "2024-02-15", "I have gotten a lot of use out of croc."],
    ["robviren", "2024-02-15", "Super pain free."],
    ["robviren", "2024-02-15", "sends stuff pretty easily."],
    ["poopsmithe", "2024-02-15", "croc is like a better magic-wormhole"],
    ["poopsmithe", "2024-02-15", "croc does it automatically."],
    ["dain", "2024-02-15", "Transfer is encrypted, no account needed."],
    ["ytch", "2024-02-15", "I like it too."],
    ["ytch", "2024-02-15", "Croc is easy to install/use in almost all network environments."],
    ["outime", "2023-10-19", "I use croc quite a lot"],
    ["outime", "2023-10-19", "good when sender and receiver are on different networks."],
    ["pepa65", "2023-09-23", "when I discovered croc, I switched to that"],
    ["pepa65", "2023-09-23", "it has been very reliable."],
    ["jrootabega", "2022-10-25", "I think croc is a superior solution here."],
    ["jrootabega", "2022-10-25", "Encrypted transfer. Automatic local peer detection. Human speakable commands."],
    ["bzmrgonz", "2022-07-10", "croc is a more friendly solution"],
    ["bzmrgonz", "2022-07-10", "In any event, quick and easy."],
    ["ntoshev", "2021-03-12", "croc is my favourite way of transferring files between computers I control."],
    ["ntoshev", "2021-03-12", "a bit more polished."],
    ["ntoshev", "2021-03-12", "it offers the best possible UX."],
    ["tptacek", "2021-05-24", "I'm a huge fan of croc!"],
    ["tptacek", "2021-05-24", "there's so much more to love about it."],
    ["smusamashah", "2021-05-24", "On Android, you can install croc in Termux"],
    ["jtbayly", "2020-09-18", "I switched to croc… to send large files."],
    ["jtbayly", "2020-09-18", "Works great across macOS and Windows."],
    ["jtbayly", "2020-09-18", "quick for large files"],
    ["greenbush", "2020-06-27", "I've been using croc for a few months and it works perfectly"],
    ["terrywang", "2020-06-27", "I've been using croc… it's fast and reliable."],
    ["terrywang", "2020-06-27", "Personally I think it's better than magic-wormhole"],
    ["BusTrainBus", "2020-09-03", "Croc is fantastic because it solves a problem that no other tool does."],
    ["BusTrainBus", "2020-09-03", "Magic Wormhole stumbles at the first hurdle (installability)."],
    ["StartupTree", "2020-09-18", "Croc has binaries for windows available for download"],
    ["StartupTree", "2020-09-18", "With croc the clients are first party."],
    ["fredley", "2020-09-18", "It works on Windows"],
    ["fredley", "2020-09-18", "I can connect with most people in the world."],
    ["anotherhue", "2024-03-10", "I used to use MW but switched to croc"],
    ["anotherhue", "2024-03-10", "the single binary was easier to deploy."],
  ] as const;
  expect(
    structuredData.review?.slice(10).map((review) => [
      review.author?.name,
      review.datePublished,
      review.reviewBody,
    ]),
  ).toEqual(additionalReviews);
  expect(
    structuredData.review
      ?.slice(10)
      .every((review) => review.reviewRating?.ratingValue === 5),
  ).toBe(true);
  const homeReviews = page.locator("details.home-reviews");
  await expect(homeReviews).not.toHaveAttribute("open", "");
  await expect(homeReviews.locator("summary")).toContainText(
    /4\.94\/5\s*from 50 reviewers\s*read reviews/,
  );
  await homeReviews.locator("summary").click();
  await expect(homeReviews).toHaveAttribute("open", "");
  const visibleReviews = homeReviews.locator(".home-review-list > li");
  await expect(visibleReviews).toHaveCount(50);
  await expect(homeReviews).not.toContainText(
    /Reddit|Hacker News|Lobsters|DonationCoder|Sailfish|Facebook/,
  );
  const renderedReviewData = await visibleReviews.evaluateAll((items) =>
    items.map((item) => ({
      author: item.querySelector("cite")?.textContent,
      body: item.querySelector("blockquote > p")?.textContent,
      date: item.querySelector("time")?.getAttribute("datetime"),
      rating: item
        .querySelector(".home-review-rating")
        ?.getAttribute("aria-label"),
    })),
  );
  expect(renderedReviewData).toEqual(
    structuredData.review?.map((review) => ({
      author: review.author?.name,
      body: review.reviewBody,
      date: review.datePublished,
      rating: `Rated ${review.reviewRating?.ratingValue} out of 5`,
    })),
  );
  await expect(
    page.getByRole("link", { name: "View croc on GitHub" }),
  ).toHaveAttribute("href", "https://github.com/schollz/croc");
  await expect(
    page.getByRole("link", { name: /Read all 10 posts/i }),
  ).toHaveAttribute("href", "/blog");
  await expect(
    page.getByRole("link", { name: "schollz", exact: true }),
  ).toHaveAttribute("href", "https://github.com/sponsors/schollz");
  const toolsMenu = page.locator("footer details.tools-menu");
  await expect(toolsMenu.locator("summary")).toHaveText("other tools");
  await toolsMenu.locator("summary").click();
  await expect(toolsMenu).toHaveAttribute("open", "");
  await expect(
    page.locator('footer a[href="https://wthrtxt.com"]'),
  ).toContainText("wthrtxt");
  await expect(
    page.locator('footer a[href="https://cowyo.com"]'),
  ).toContainText("cowyo");
  await expect(
    page.locator('footer a[href="https://yesnotice.com"]'),
  ).toContainText("yesnotice");
  await expect(
    page.locator('footer a[href="https://pianos.pub"]'),
  ).toContainText("find a piano wherever you are");
  await expect(
    page.locator('footer a[href="https://makemydrivefun.com"]'),
  ).toContainText("strange roadside detours");
  await expect(
    page.locator('footer a[href="https://makestopmotion.com"]'),
  ).toContainText("claymation in browsers");
  await expect(
    page.getByRole("link", { name: "github", exact: true }),
  ).toHaveAttribute(
    "href",
    "https://github.com/schollz/croc",
  );
  await expect(
    page.getByRole("link", { name: "disco", exact: true }),
  ).toHaveAttribute("href", "https://disco.cloud");
  await expect(
    page.getByRole("heading", { name: "Download croc for macOS." }),
  ).toBeVisible();
  const receivePanel = page.locator(".receive-panel");
  await expect(
    receivePanel.getByRole("button", { name: "Receive", exact: true }),
  ).toHaveCSS("font-size", "12px");
  await expect(
    receivePanel.getByText(
      "Enter a croc code or encrypted stored link. Review before saving or displaying.",
    ),
  ).toHaveCSS("font-size", "12px");
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

test("opens directly without an analytics popover", async ({ page }) => {
  await page.goto("/");
  await expect(
    page.getByRole("dialog", { name: "Optional analytics" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("heading", { name: "Send files, secured end-to-end." }),
  ).toBeVisible();
});

test("mobile puts both transfer directions within one tap", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");

  const sendTab = page.getByRole("tab", { name: "Send" });
  const receiveTab = page.getByRole("tab", { name: "Receive" });
  await expect(sendTab).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(".send-panel")).toBeVisible();
  await expect(page.locator(".receive-panel")).not.toBeVisible();
  const sendPanel = page.locator(".send-panel");
  await expect(sendPanel.getByLabel("Text to send")).toHaveCount(0);
  await expect(sendPanel.getByRole("button", { name: "Send text instead" }))
    .toBeVisible();
  await sendPanel.getByRole("button", { name: "Send text instead" }).click();
  const textComposer = sendPanel.getByLabel("Text to send");
  await expect(textComposer).toBeVisible();
  await expect(textComposer).toHaveCSS("font-size", "16px");
  await expect(sendPanel.getByRole("button", { name: "Send text" })).toBeDisabled();
  await textComposer.fill("hello from mobile");
  await expect(sendPanel.getByRole("button", { name: "Send text" })).toBeEnabled();
  await sendPanel.getByRole("button", { name: "Send files instead" }).click();
  await expect(sendPanel.locator(".drop-zone")).toBeVisible();

  await receiveTab.click();
  await expect(receiveTab).toHaveAttribute("aria-selected", "true");
  await expect(page.locator(".receive-panel")).toBeVisible();
  await expect(page.locator(".send-panel")).not.toBeVisible();

  await page.evaluate(() => window.scrollBy(0, 120));
  await sendTab.click();
  await expect(sendTab).toHaveAttribute("aria-selected", "true");
  await expect
    .poll(() =>
      page.locator(".transfer-grid").evaluate((element) =>
        Math.abs(element.getBoundingClientRect().top),
      ),
    )
    .toBeLessThanOrEqual(1);

  await receiveTab.click();

  const mobileLayout = await page.evaluate(() => ({
    inputFontSize: Number.parseFloat(
      getComputedStyle(document.querySelector("#receive-code")!).fontSize,
    ),
    overflow: document.documentElement.scrollWidth - window.innerWidth,
    targets: [...document.querySelectorAll(".mobile-transfer-switch button")].map(
      (element) => element.getBoundingClientRect().height,
    ),
  }));
  expect(mobileLayout.overflow).toBeLessThanOrEqual(0);
  expect(mobileLayout.inputFontSize).toBeGreaterThanOrEqual(16);
  for (const height of mobileLayout.targets) {
    expect(height).toBeGreaterThanOrEqual(44);
  }
});

test("receive links open with the Receive panel at the top", async ({
  page,
}) => {
  await page.goto("/?code=x");

  const receivePanel = page.locator("#receive");
  await expect(receivePanel).toBeVisible();
  await expect(page.locator(".send-panel")).toHaveCount(0);
  await expect
    .poll(() =>
      receivePanel.evaluate((element) =>
        Math.abs(Math.round(element.getBoundingClientRect().top)),
      ),
    )
    .toBe(0);
  await expect
    .poll(() =>
      page.locator(".site-header").evaluate((element) =>
        element.getBoundingClientRect().bottom,
      ),
    )
    .toBeLessThanOrEqual(0);
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
    "The code or link provides the key",
    "Use another relay when needed",
    "Works with the croc CLI",
    "Read the field notes",
  ];

  for (const [index, expectedTitle] of steps.entries()) {
    await expect(title).toHaveText(expectedTitle);
    if (expectedTitle === "The code or link provides the key") {
      await expect(tour).toContainText(
        "password-authenticated key exchange (PAKE)",
      );
      await expect(tour).toContainText(
        "encrypted before leaving the browser",
      );
      await expect(tour).toContainText("not sent to the server");
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
  await panel.locator('input[type="file"]').setInputFiles({
    name: "copy-test.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("copy test"),
  });
  await panel.getByRole("button", { name: "Send file" }).click();
  const copyButton = panel.getByRole("button", { name: "Copy code" });
  await copyButton.click();
  await expect(panel.getByRole("status")).toHaveText("Copied");
  await expect(panel.getByRole("status")).toHaveClass("visually-hidden");
  await expect(panel.getByRole("button", { name: "Code copied" })).toHaveClass(
    /copied/,
  );
  await expect(panel.getByLabel("Croc code")).toHaveClass(/copied/);
  await expect(panel.getByLabel("Croc code")).toHaveCSS(
    "animation-name",
    "copy-confirmed-code",
  );
});

test("preloads WASM and reveals a mobile-sized code only after Send is pressed", async ({
  page,
}) => {
  const wasmRequests: string[] = [];
  await page.addInitScript(() => {
    const originalStream = Blob.prototype.stream;
    Blob.prototype.stream = function stream() {
      const state = window as typeof window & { __crocHashReads?: number };
      state.__crocHashReads = (state.__crocHashReads ?? 0) + 1;
      return originalStream.call(this);
    };
  });
  page.on("request", (request) => {
    if (new URL(request.url()).pathname.endsWith("/croc.wasm")) {
      wasmRequests.push(request.url());
    }
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await expect(page.locator('link[rel="preload"][href="/croc.wasm"]'))
    .toHaveAttribute("as", "fetch");
  const panel = page.locator(".send-panel");
  const sendButton = panel.getByRole("button", { name: "Send file" });
  await expect(sendButton).toBeDisabled();
  await expect(panel.getByLabel("Croc code")).toHaveCount(0);
  await expect(panel.getByRole("button", { name: "Show QR code" })).toHaveCount(0);
  await expect(panel).not.toContainText("Generated codes use");
  await page.waitForTimeout(250);
  expect(wasmRequests).toHaveLength(1);
  expect(
    new Set(wasmRequests.map((url) => new URL(url).pathname)),
  ).toEqual(new Set(["/croc.wasm"]));
  const wasmRequestCount = wasmRequests.length;
  const hashReadsBeforeSelection = await page.evaluate(
    () =>
      (window as typeof window & { __crocHashReads?: number })
        .__crocHashReads ?? 0,
  );

  await panel.locator('input[type="file"]').setInputFiles({
    name: "mobile-test.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("mobile test"),
  });
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (window as typeof window & { __crocHashReads?: number })
            .__crocHashReads ?? 0,
      ),
    )
    .toBeGreaterThan(hashReadsBeforeSelection);
  expect(wasmRequests).toHaveLength(wasmRequestCount);
  await expect(sendButton).toBeEnabled();
  await sendButton.click();
  await expect(sendButton).toHaveCount(0);
  const code = panel.getByLabel("Croc code");
  const codeLabel = panel.getByText("Use this code:", { exact: true });
  const copyButton = panel.getByRole("button", { name: "Copy code" });
  await expect(code).toHaveText(/^[a-z]+(?:-[a-z]+){2,5}$/);
  await expect(code).toHaveCSS("font-size", "14px");
  await expect(panel.getByRole("button", { name: "Generate a new code" })).toHaveCount(0);
  await expect(panel.getByRole("button", { name: "Show QR code" })).toBeVisible();
  const [labelBox, codeBox, copyBox] = await Promise.all([
    codeLabel.boundingBox(),
    code.boundingBox(),
    copyButton.boundingBox(),
  ]);
  expect(labelBox!.y + labelBox!.height).toBeLessThanOrEqual(codeBox!.y);
  expect(
    Math.abs(
      codeBox!.y + codeBox!.height / 2 - (copyBox!.y + copyBox!.height / 2),
    ),
  ).toBeLessThanOrEqual(1);
  expect(codeBox!.x + codeBox!.width).toBeLessThanOrEqual(copyBox!.x);
  expect(await code.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(
    true,
  );
});

test("CLI → Web transfers and verifies multiple files", async ({
  page,
}, testInfo) => {
  // Keep a five-letter first word here: the complete first word is the room,
  // rather than the legacy protocol's first four characters.
  const secret = "poker-hedge-floss";
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
  const fixtures = await createFixtures(testInfo);
  const destination = testInfo.outputPath("received");
  const configDirectory = testInfo.outputPath("croc-config");
  await Promise.all([
    fs.mkdir(destination, { recursive: true }),
    fs.mkdir(configDirectory, { recursive: true }),
  ]);
  await configurePage(page);
  const sendPanel = await prepareWebSender(page, fixtures);
  await sendPanel.getByRole("button", { name: "Send 3 files" }).click();
  const secret = await readGeneratedSecret(sendPanel);
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

test("Web → CLI sends exact multiline Unicode text", async ({
  page,
}, testInfo) => {
  const message = "hello from croc web\nhttps://example.com/🐊\nfinal line";
  const configDirectory = testInfo.outputPath("text-receiver-config");
  await fs.mkdir(configDirectory, { recursive: true });
  await configurePage(page);
  const panel = page.locator(".send-panel");
  await panel.getByRole("button", { name: "Send text instead" }).click();
  await panel.getByLabel("Text to send").fill(message);
  await panel.getByRole("button", { name: "Send text" }).click();
  const secret = await readGeneratedSecret(panel);
  const cli = runCroc(
    [...commonCLIArgs(), "--quiet"],
    secret,
    configDirectory,
  );
  try {
    await expect(panel).toContainText("Text arrived safely", {
      timeout: transferTimeout,
    });
    await cli.done;
    expect(cli.output()).toBe(message);
  } finally {
    cli.stop();
    await cli.done.catch(() => undefined);
  }
});

test("CLI → Web reviews, verifies, displays, and copies text without a download", async ({
  page,
}, testInfo) => {
  const secret = "1113-cli-text-to-web";
  const message = "sent from the CLI\nmultiline 🐊 text";
  const configDirectory = testInfo.outputPath("text-sender-config");
  await fs.mkdir(configDirectory, { recursive: true });
  await configurePage(page);
  await page.evaluate(() => {
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText: async () => undefined },
    });
  });
  const downloads: Download[] = [];
  page.on("download", (download) => downloads.push(download));
  const panel = await connectWebReceiver(page, secret);
  const cli = runCroc(
    [...commonCLIArgs(), "send", "--no-local", "--text", message],
    secret,
    configDirectory,
    process.env.CROC_E2E_SENDER_BINARY,
  );
  try {
    await expect(panel.getByText("Incoming text", { exact: true })).toBeVisible();
    await expect(panel.getByRole("button", { name: "Accept files" })).toHaveCount(0);
    await expect(panel.getByLabel("Received text")).toHaveCount(0);
    await panel.getByRole("button", { name: "Display text" }).click();
    await expect(panel.getByLabel("Received text")).toHaveText(message, {
      timeout: transferTimeout,
    });
    await expect(panel).toContainText("Text received and verified");
    expect(downloads).toHaveLength(0);
    await panel.getByRole("button", { name: "Copy text" }).click();
    await expect(panel.getByRole("button", { name: "Text copied" })).toBeVisible();
    await expect(panel.locator(".received-text").getByRole("status"))
      .toContainText("Text copied");
    await cli.done;
  } finally {
    cli.stop();
    await cli.done.catch(() => undefined);
  }
});

test("Web → Web transfers and verifies multiple files", async ({
  browser,
}, testInfo) => {
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
    const sendPanel = await prepareWebSender(senderPage, fixtures);
    await sendPanel.getByRole("button", { name: "Send 3 files" }).click();
    const secret = await readGeneratedSecret(sendPanel);
    const receivePanel = await connectWebReceiver(receiverPage, secret);
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

test("Web stored upload → CLI download consumes the transfer", async ({
  page,
}, testInfo) => {
  const fixtures = await createFixtures(testInfo);
  const destination = testInfo.outputPath("stored-received");
  const configDirectory = testInfo.outputPath("stored-croc-config");
  await Promise.all([
    fs.mkdir(destination, { recursive: true }),
    fs.mkdir(configDirectory, { recursive: true }),
  ]);
  await configurePage(page);
  const panel = page.locator(".send-panel");
  await panel.getByRole("button", { name: "Store for 1 day" }).click();
  await panel.locator('input[type="file"]').setInputFiles(fixtures.paths);
  await expect(panel.getByText("Storage lifetime", { exact: true })).toBeVisible();
  await expect(panel.getByText("Verified downloads", { exact: true })).toBeVisible();
  await panel.getByRole("button", { name: "Store 3 files" }).click();
  await expect(panel.getByText("Storage lifetime", { exact: true })).toHaveCount(0);
  await expect(panel.getByText("Verified downloads", { exact: true })).toHaveCount(0);
  await expect(panel.getByText("Encrypted link ready")).toBeVisible({
    timeout: transferTimeout,
  });
  const token = await panel.getByLabel("CLI token").inputValue();
  const receiver = runCroc(
    [...commonCLIArgs(), "--out", destination],
    "unused-for-stored-transfer",
    configDirectory,
    crocBinary,
    { CROC_STORE_TOKEN: token },
  );
  try {
    await receiver.done;
    await expectDirectory(destination, fixtures);
    await expect(panel.getByText("Encrypted link ready")).toBeVisible();
  } finally {
    receiver.stop();
    await receiver.done.catch(() => undefined);
  }
});

test("stored settings return after an upload is revoked", async ({ page }) => {
  await configurePage(page);
  const panel = page.locator(".send-panel");
  await panel.getByRole("button", { name: "Store for 1 day" }).click();
  await panel.locator('input[type="file"]').setInputFiles({
    name: "revoke-test.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("revoke test"),
  });
  await panel.getByRole("button", { name: "Store file" }).click();
  await expect(panel.getByText("Encrypted link ready")).toBeVisible({
    timeout: transferTimeout,
  });
  await expect(panel.getByText("Storage lifetime", { exact: true })).toHaveCount(0);
  await expect(panel.getByText("Verified downloads", { exact: true })).toHaveCount(0);

  await panel.getByRole("button", { name: "Revoke now" }).click();
  await expect(panel).toContainText("Stored transfer revoked", {
    timeout: transferTimeout,
  });
  await expect(panel.getByText("Storage lifetime", { exact: true })).toBeVisible();
  await expect(panel.getByText("Verified downloads", { exact: true })).toBeVisible();
});

test("CLI stored upload → Web download verifies and consumes files", async ({
  page,
}, testInfo) => {
  const fixtures = await createFixtures(testInfo);
  const configDirectory = testInfo.outputPath("stored-croc-config");
  await fs.mkdir(configDirectory, { recursive: true });
  await configurePage(page);
  const origin = new URL(page.url()).origin;
  const sender = runCroc(
    [
      ...commonCLIArgs(),
      "send",
      "--store",
      "--store-url",
      origin,
      ...fixtures.paths,
    ],
    "unused-for-stored-transfer",
    configDirectory,
  );
  try {
    await sender.done;
    const browserURL = sender.output().match(
      /https?:\/\/\S+\/s\/[A-Za-z0-9_-]{22}#v1\.[A-Za-z0-9_-]+/,
    )?.[0];
    expect(browserURL, sender.output()).toBeTruthy();
    const panel = page.locator(".receive-panel");
    await panel.getByLabel("Croc code").fill(browserURL!);
    await panel.getByLabel("Croc code").press("Enter");
    const downloads = await acceptAsDownloads(page, panel);
    await expect(panel).toContainText(
      "All files received and verified; stored ciphertext removed",
      { timeout: transferTimeout },
    );
    await expectDownloads(downloads, fixtures);

    await page.goto("about:blank");
    await page.goto(browserURL!);
    await expect(page.locator(".receive-panel")).toContainText(
      /expired or has no downloads remaining/i,
      { timeout: transferTimeout },
    );
  } finally {
    sender.stop();
    await sender.done.catch(() => undefined);
  }
});
