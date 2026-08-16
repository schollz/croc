import { expect, test } from "@playwright/test";
import blogSEO from "../src/blog-seo.json" with { type: "json" };

test("every article exposes complete crawler metadata before JavaScript", async ({
  request,
}) => {
  for (const post of blogSEO.posts) {
    const response = await request.get(`/blog/${post.slug}`);
    expect(response.ok()).toBe(true);
    const html = await response.text();
    const pageTitle = "seoTitle" in post
      ? post.seoTitle
      : `${post.title} | croc field notes`;

    expect(html).toContain(
      `<title>${pageTitle}</title>`,
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
    expect(html).not.toContain('href="/croc.wasm"');
  }

  const comparison = blogSEO.posts.find(
    (post) => post.slug === "compare-file-transfer-tools",
  );
  expect(comparison).toBeDefined();
  const comparisonResponse = await request.get(
    "/blog/compare-file-transfer-tools",
  );
  const comparisonHTML = await comparisonResponse.text();
  expect(comparisonHTML).toContain(
    '<meta name="keywords" content="' + comparison?.keywords.join(", ") + '"',
  );
  expect(comparisonHTML).toContain(
    '<meta name="robots" content="index, follow, max-image-preview:large',
  );
  for (const keyword of comparison?.keywords ?? []) {
    expect(comparisonHTML).toContain(
      '<meta property="article:tag" content="' + keyword + '"',
    );
  }

  const feedResponse = await request.get("/blog/feed.xml");
  expect(feedResponse.ok()).toBe(true);
  const feed = await feedResponse.text();
  expect(feed.indexOf("/blog/compare-file-transfer-tools")).toBeLessThan(
    feed.indexOf("/blog/share-stored-file-with-group"),
  );

  const sitemapResponse = await request.get("/sitemap.xml");
  expect(sitemapResponse.ok()).toBe(true);
  expect(await sitemapResponse.text()).toContain(
    "<loc>https://getcroc.com/blog/compare-file-transfer-tools</loc>",
  );

  const image = await request.get("/blog/images/pake-step-by-step.jpg");
  expect(image.ok()).toBe(true);
  expect(image.headers()["content-type"]).toBe("image/jpeg");
  expect((await image.body()).byteLength).toBeGreaterThan(50_000);
});

test("mobile blog index exposes notes and updates without overflow", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/blog");

  await expect(
    page.getByRole("heading", { name: "Notes from inside the transfer." }),
  ).toBeVisible();
  await expect(page.locator("main article")).toHaveCount(10);
  await expect(
    page.locator("main article").first().getByRole("heading", {
      name: "From croc v10 to v11",
    }),
  ).toBeVisible();
  await expect(
    page.getByRole("navigation", { name: "Blog post categories" }),
  ).toBeVisible();
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
    "PAKE, step by step | croc field notes",
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

test("the comparison tables scroll without widening the mobile article", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/blog/compare-file-transfer-tools");

  await expect(
    page.getByRole("heading", {
      name: "36 ways to send a file",
    }),
  ).toBeVisible();
  const tableRegion = page.getByRole("region", {
    name: "Availability, account requirement, resumption, and supported endpoint combinations",
  });
  await expect(tableRegion).toBeVisible();
  const legend = page.getByRole("note", {
    name: "Availability, account requirement, resumption, and supported endpoint combinations legend",
  });
  await expect(legend).toBeVisible();
  await expect(
    legend.getByRole("img", {
      name: /Full circle: Meets the column without an important limitation/,
    }),
  ).toBeVisible();
  await expect(legend.getByText(/Caveats and qualifiers/)).toBeVisible();
  await expect(
    tableRegion.locator("tbody th a").nth(0),
  ).toHaveText("croc");
  await expect(
    tableRegion.locator("tbody th a").nth(1),
  ).toHaveText("MEGA");
  await expect(
    tableRegion.locator("tbody th a").nth(2),
  ).toHaveText("Filemail");
  await expect(
    tableRegion.locator("tbody tr").nth(1).locator('td[title="required to upload"]'),
  ).toHaveAttribute(
    "title",
    "required to upload",
  );

  const layout = await tableRegion.evaluate((region) => ({
    documentOverflow: document.documentElement.scrollWidth - window.innerWidth,
    tableOverflow: region.scrollWidth - region.clientWidth,
  }));
  expect(layout.documentOverflow).toBeLessThanOrEqual(0);
  expect(layout.tableOverflow).toBeGreaterThan(0);

  const indicatorWidths = await tableRegion
    .locator("thead th.is-indicator-column")
    .evaluateAll((headers) => headers.map((header) => header.getBoundingClientRect().width));
  expect(indicatorWidths.length).toBe(7);
  expect(indicatorWidths.every((width) => width <= 74)).toBe(true);
  await expect(
    page.locator(".blog-comparison-table tbody th a"),
  ).toHaveCount(72);
});
