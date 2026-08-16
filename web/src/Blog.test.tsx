import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Blog } from "./Blog";

describe("Blog", () => {
  afterEach(cleanup);

  it("renders categorized notes and updates with page metadata", () => {
    render(<Blog />);

    expect(
      screen.getByRole("heading", { name: "Notes from inside the transfer." }),
    ).toBeVisible();
    expect(screen.getAllByRole("article")).toHaveLength(10);
    const categories = screen.getByRole("navigation", {
      name: "Blog post categories",
    });
    expect(within(categories).getByRole("link", { name: /Updates/ })).toHaveAttribute(
      "href",
      "#updates",
    );
    expect(within(categories).getByRole("link", { name: /Notes/ })).toHaveAttribute(
      "href",
      "#notes",
    );
    expect(
      screen.getByRole("heading", {
        level: 2,
        name: "36 ways to send a file",
      }),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "croc field notes home" }),
    ).toHaveAttribute("href", "/blog");
    const transferLinks = screen.getByRole("navigation", {
      name: "More ways to transfer with croc",
    });
    expect(transferLinks.previousElementSibling?.tagName).toBe("MAIN");
    expect(transferLinks.nextElementSibling).toHaveClass("blog-footer");
    expect(document.title).toBe(
      "croc field notes: secure file transfer explained",
    );
    expect(document.querySelector('link[rel="canonical"]')).toHaveAttribute(
      "href",
      "https://getcroc.com/blog",
    );
    expect(document.querySelector('meta[property="og:image"]')).toHaveAttribute(
      "content",
      "https://getcroc.com/blog/images/blog-field-notes.jpg",
    );
    expect(document.querySelector('meta[name="twitter:card"]')).toHaveAttribute(
      "content",
      "summary_large_image",
    );
  });

  it("renders the v11 release update with its release history", () => {
    render(<Blog slug="croc-v11-release-update" />);

    expect(
      screen.getByRole("heading", { name: "From croc v10 to v11" }),
    ).toBeVisible();
    expect(screen.getByText("UPDATE 01")).toBeVisible();
    expect(
      screen.getByRole("navigation", { name: "In this update" }),
    ).toBeVisible();
    expect(screen.getByRole("table")).toBeVisible();
    expect(screen.getByRole("link", { name: "v11.1.1" })).toHaveAttribute(
      "href",
      "https://github.com/schollz/croc/releases/tag/v11.1.1",
    );
    expect(
      screen.getByRole("img", {
        name: /three black-and-white panels/i,
      }),
    ).toHaveClass("blog-article-visual-cover");
    expect(document.querySelector('meta[property="article:section"]')).toHaveAttribute(
      "content",
      "Updates",
    );
  });

  it("renders a direct article route", () => {
    render(<Blog slug="what-four-word-code-does" />);

    expect(
      screen.getByRole("heading", { name: "What the three words are doing" }),
    ).toBeVisible();
    expect(screen.getByText("IN ONE SENTENCE")).toBeVisible();
    expect(document.title).toBe(
      "What the three words are doing | croc field notes",
    );
    expect(
      screen.getByRole("img", {
        name: /Blank code tiles converge into one cryptographic key/i,
      }),
    ).toHaveAttribute(
      "src",
      "/blog/images/what-four-word-code-does.webp",
    );
    expect(document.querySelector('meta[property="og:image"]')).toHaveAttribute(
      "content",
      "https://getcroc.com/blog/images/what-four-word-code-does.jpg",
    );
    expect(
      document.querySelector('meta[property="article:published_time"]'),
    ).toHaveAttribute("content", "2026-08-08");
    expect(
      document.querySelector('meta[property="article:modified_time"]'),
    ).toHaveAttribute("content", "2026-08-10");
    expect(
      screen.getByRole("navigation", { name: "In this field note" }),
    ).toBeVisible();
    expect(
      screen.getAllByRole("link", { name: /PAKE, step by step/ }),
    ).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          href: expect.stringContaining("/blog/pake-step-by-step"),
        }),
      ]),
    );
    expect(
      screen.getByRole("link", { name: "Install croc" }),
    ).toHaveAttribute("href", "https://infinitedigits.co/croc/");
    const structuredData = JSON.parse(
      document.querySelector<HTMLScriptElement>(
        'script[data-croc-blog="true"]',
      )?.text ?? "{}",
    ) as { "@graph"?: Array<{ "@type"?: string }> };
    expect(
      structuredData["@graph"]?.some((entry) => entry["@type"] === "BlogPosting"),
    ).toBe(true);
  });

  it("renders the group stored-transfer field guide", () => {
    render(<Blog slug="share-stored-file-with-group" />);

    expect(
      screen.getByRole("heading", {
        name: "Send one file to a group, on their schedule",
      }),
    ).toBeVisible();
    expect(screen.getByText(/--store-downloads 5/)).toBeVisible();
    expect(screen.getByText(/--store-expiration 3d/)).toBeVisible();
    expect(
      screen.getByRole("img", {
        name: /One locked parcel branches toward three recipient devices/i,
      }),
    ).toHaveAttribute(
      "src",
      "/blog/images/share-stored-file-with-group.webp",
    );
  });

  it("renders the file-transfer comparison as two accessible tables", () => {
    render(<Blog slug="compare-file-transfer-tools" />);

    expect(
      screen.getByRole("heading", {
        name: "36 ways to send a file",
      }),
    ).toBeVisible();
    expect(screen.getAllByRole("table")).toHaveLength(2);
    expect(screen.getAllByRole("row")).toHaveLength(74);
    const [capabilityTable, architectureTable] = screen.getAllByRole("table");
    expect(
      within(capabilityTable).getAllByRole("rowheader").slice(0, 3)
        .map((header) => header.textContent),
    ).toEqual(["croc", "MEGA", "Filemail"]);
    expect(
      within(architectureTable).getAllByRole("rowheader").slice(0, 3)
        .map((header) => header.textContent),
    ).toEqual(["croc", "MEGA", "Filemail"]);
    expect(
      screen.getByRole("region", {
        name: "Availability, account requirement, resumption, and supported endpoint combinations",
      }),
    ).toHaveAttribute("tabindex", "0");
    expect(screen.getAllByRole("link", { name: "croc" })[0]).toHaveAttribute(
      "href",
      "https://github.com/schollz/croc",
    );
    const capabilityLegend = screen.getByRole("note", {
      name: "Availability, account requirement, resumption, and supported endpoint combinations legend",
    });
    expect(
      within(capabilityLegend).getByRole("img", {
        name: /Full circle: Meets the column without an important limitation/,
      }),
    ).toBeVisible();
    expect(
      within(capabilityLegend).getByRole("img", {
        name: /Half circle: Meets it only in some modes or with a caveat/,
      }),
    ).toBeVisible();
    expect(
      within(capabilityLegend).getByRole("img", {
        name: /Empty circle: No documented support/,
      }),
    ).toBeVisible();
    expect(
      within(capabilityLegend).getByText(/Caveats and qualifiers/),
    ).toBeVisible();
    const megaAccountIndicator = within(capabilityTable).getByRole("img", {
      name: /Account: No documented support; required to upload/,
    });
    expect(megaAccountIndicator.closest("td")).toHaveAttribute(
      "title",
      "required to upload",
    );
    expect(
      document.querySelectorAll(
        ".blog-comparison-table td.is-indicator-column[title]",
      ),
    ).toHaveLength(
      document.querySelectorAll(".blog-table-qualifiers li").length,
    );

    const toolLinks = document.querySelectorAll(
      ".blog-comparison-table tbody th a",
    );
    expect(toolLinks).toHaveLength(72);
    expect(
      new Set([...toolLinks].map((link) => link.textContent)).size,
    ).toBe(36);
    expect(
      document.querySelectorAll(
        ".blog-comparison-table td.is-indicator-column .blog-status-indicator",
      ),
    ).toHaveLength(252);
    expect(
      screen.getByRole("img", {
        name: /File transfer tools comparison showing one file connected/i,
      }),
    ).toHaveAttribute(
      "src",
      "/blog/images/compare-file-transfer-tools.webp",
    );
    expect(document.title).toBe(
      "36 Ways to Send a File: croc and 35 Alternatives",
    );
    expect(document.querySelector('link[rel="canonical"]')).toHaveAttribute(
      "href",
      "https://getcroc.com/blog/compare-file-transfer-tools",
    );
    expect(document.querySelector('meta[property="og:image"]')).toHaveAttribute(
      "content",
      "https://getcroc.com/blog/images/compare-file-transfer-tools.jpg",
    );
    expect(document.querySelector('meta[name="twitter:card"]')).toHaveAttribute(
      "content",
      "summary_large_image",
    );
    expect(document.querySelector('meta[name="twitter:image"]')).toHaveAttribute(
      "content",
      "https://getcroc.com/blog/images/compare-file-transfer-tools.jpg",
    );
    expect(document.querySelector('meta[name="keywords"]')).toHaveAttribute(
      "content",
      expect.stringContaining("file transfer tools comparison"),
    );
    expect(document.querySelector('meta[name="robots"]')).toHaveAttribute(
      "content",
      expect.stringContaining("max-image-preview:large"),
    );
    expect(
      document.querySelector('meta[property="article:published_time"]'),
    ).toHaveAttribute("content", "2026-08-12");
    const structuredData = JSON.parse(
      document.querySelector<HTMLScriptElement>(
        'script[data-croc-blog="true"]',
      )?.text ?? "{}",
    ) as {
      "@graph"?: Array<{
        "@type"?: string;
        headline?: string;
        wordCount?: number;
      }>;
    };
    expect(structuredData["@graph"]).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          "@type": "BlogPosting",
          headline: "36 ways to send a file",
          wordCount: 2244,
        }),
      ]),
    );
  });

  it("renders a useful not-found page for an unknown slug", () => {
    render(<Blog slug="missing-note" />);

    expect(
      screen.getByRole("heading", { name: "This note wandered off." }),
    ).toBeVisible();
    expect(
      screen.getByRole("navigation", {
        name: "More ways to transfer with croc",
      }),
    ).toBeVisible();
    expect(document.querySelector('meta[name="robots"]')).toHaveAttribute(
      "content",
      "noindex, follow",
    );
  });
});
