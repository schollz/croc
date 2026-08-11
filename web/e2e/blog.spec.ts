import { expect, test } from "@playwright/test";
import blogSEO from "../src/blog-seo.json" with { type: "json" };

test("every article exposes complete crawler metadata before JavaScript", async ({
  request,
}) => {
  for (const post of blogSEO.posts) {
    const response = await request.get(`/blog/${post.slug}`);
    expect(response.ok()).toBe(true);
    const html = await response.text();

    expect(html).toContain(
      `<title>${post.title} — croc field notes</title>`,
    );
    expect(html).toContain(
      `<meta name="description" content="${post.description}"`,
    );
    expect(html).toContain(
      `<link rel="canonical" href="https://getcroc.com/blog/${post.slug}"`,
    );
    expect(html).toContain(
      `<meta property="og:image" content="https://getcroc.com${post.socialImage}"`,
    );
    expect(html).toContain(
      '<meta name="twitter:card" content="summary_large_image"',
    );
    expect(html).toContain(
      `<meta property="article:modified_time" content="${post.modifiedAt}"`,
    );
    expect(html).toContain('"@type":"BlogPosting"');
    expect(html).toContain('"@type":"BreadcrumbList"');
    expect(html).toContain(`"wordCount":${post.wordCount}`);
    expect(html).toContain(`"timeRequired":"PT${post.readingMinutes}M"`);
    expect(html).toContain('"relatedLink"');
  }

  const image = await request.get("/blog/images/pake-step-by-step.jpg");
  expect(image.ok()).toBe(true);
  expect(image.headers()["content-type"]).toBe("image/jpeg");
  expect((await image.body()).byteLength).toBeGreaterThan(50_000);
});

test("mobile blog index exposes eight field notes without overflow", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/blog");

  await expect(
    page.getByRole("heading", { name: "Notes from inside the transfer." }),
  ).toBeVisible();
  await expect(page.locator("main article")).toHaveCount(8);
  await expect(
    page.getByRole("link", { name: "Transfer files", exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("link", { name: "croc field notes home" }),
  ).toHaveAttribute("href", "/blog");

  const layout = await page.evaluate(() => {
    const header = document.querySelector(".blog-site-header");
    const brand = document.querySelector(".blog-brand");
    const transferLink = document.querySelector('.blog-site-header nav a[href="/"]');
    return {
      overflow: document.documentElement.scrollWidth - window.innerWidth,
      headerHeight: header?.getBoundingClientRect().height ?? 0,
      brandHeight: brand?.getBoundingClientRect().height ?? 0,
      transferLinkHeight: transferLink?.getBoundingClientRect().height ?? 0,
    };
  });
  expect(layout.overflow).toBeLessThanOrEqual(0);
  expect(layout.headerHeight).toBeLessThanOrEqual(72);
  expect(layout.brandHeight).toBeGreaterThanOrEqual(44);
  expect(layout.transferLinkHeight).toBeGreaterThanOrEqual(44);

  await page.evaluate(() => window.scrollTo(0, 640));
  await expect.poll(
    () => page.locator(".blog-site-header").evaluate(
      (header) => Math.round(header.getBoundingClientRect().top),
    ),
  ).toBe(0);
});

test("direct article routes publish metadata and complete article content", async ({
  page,
}) => {
  await page.goto("/blog/pake-step-by-step");

  await expect(page).toHaveTitle(
    "PAKE, step by step — croc field notes",
  );
  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
    "href",
    "https://getcroc.com/blog/pake-step-by-step",
  );
  await expect(
    page.getByRole("heading", { name: "1. The receiver makes X" }),
  ).toBeVisible();
  await expect(page.getByText("IN ONE SENTENCE")).toBeVisible();
  await expect(
    page.getByRole("navigation", { name: "In this field note" }),
  ).toBeVisible();
  const toc = page.getByRole("navigation", { name: "In this field note" });
  const firstSection = toc.getByRole("link", {
    name: "What I want PAKE to guarantee",
  });
  const secondSection = toc.getByRole("link", {
    name: "1. The receiver makes X",
  });
  await expect(firstSection).toHaveAttribute("aria-current", "location");
  await secondSection.evaluate((link) => {
    const targetID = link.getAttribute("href")?.replace(/^#/, "");
    if (targetID) document.getElementById(targetID)?.scrollIntoView();
  });
  await expect(secondSection).toHaveAttribute("aria-current", "location");
  await expect(firstSection).not.toHaveAttribute("aria-current", "location");
  await expect(page.getByText("Related field notes")).toBeVisible();
  await expect(
    page.getByRole("link", { name: /Next note/ }),
  ).toBeVisible();
});
