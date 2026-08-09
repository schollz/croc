import { expect, test } from "@playwright/test";

test("production HTML exposes crawler metadata before JavaScript", async ({
  request,
}) => {
  const response = await request.get("/blog/pake-step-by-step");
  expect(response.ok()).toBe(true);
  const html = await response.text();

  expect(html).toContain(
    '<title>PAKE, step by step — croc field notes</title>',
  );
  expect(html).toContain(
    '<link rel="canonical" href="https://getcroc.com/blog/pake-step-by-step"',
  );
  expect(html).toContain(
    '<meta property="og:image" content="https://getcroc.com/blog/images/pake-step-by-step.jpg"',
  );
  expect(html).toContain(
    '<meta name="twitter:card" content="summary_large_image"',
  );
  expect(html).toContain('property="article:published_time"');
  expect(html).toContain('"@type":"BlogPosting"');
  expect(html).toContain('"@type":"BreadcrumbList"');

  const image = await request.get("/blog/images/pake-step-by-step.jpg");
  expect(image.ok()).toBe(true);
  expect(image.headers()["content-type"]).toBe("image/jpeg");
  expect((await image.body()).byteLength).toBeGreaterThan(50_000);
});

test("mobile blog index exposes seven field notes without overflow", async ({
  page,
}) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/blog");

  await expect(
    page.getByRole("heading", { name: "Notes from inside the transfer." }),
  ).toBeVisible();
  await expect(page.locator("main article")).toHaveCount(7);
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
    page.getByRole("link", { name: /Next note/ }),
  ).toBeVisible();
});
