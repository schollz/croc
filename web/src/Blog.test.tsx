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
    expect(screen.getAllByRole("article")).toHaveLength(12);
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
        name: "40 ways to send a file",
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

  it("renders the CLI speed comparison and its receiver timings", () => {
    render(<Blog slug="croc-cli-speed-comparison" />);

    expect(
      screen.getByRole("heading", {
        name: "How fast is croc?",
      }),
    ).toBeVisible();
    expect(screen.getByText("FIELD NOTE 10")).toBeVisible();
    expect(
      screen.getByText(/the alternatives took 1\.3× to 21\.7× as long/),
    ).toBeVisible();
    expect(screen.getByRole("heading", { name: "TL;DR" })).toBeVisible();
    expect(
      screen.getByText(/averaging 34\.8 MB\/s across its three timed runs/),
    ).toBeVisible();
    expect(screen.getAllByRole("table")).toHaveLength(3);
    expect(screen.getByRole("cell", { name: "7.2 s" })).toBeVisible();
    expect(screen.getByRole("cell", { name: "20.0 s" })).toBeVisible();
    expect(screen.getByRole("cell", { name: "42.4 s" })).toBeVisible();
    expect(screen.getByRole("cell", { name: "91.7 s" })).toBeVisible();
    expect(screen.getByRole("cell", { name: "156.5 s" })).toBeVisible();
    expect(screen.getByRole("cell", { name: "9.6 s" })).toBeVisible();
    expect(screen.getByRole("cell", { name: "110.0 s" })).toBeVisible();
    expect(screen.getByRole("cell", { name: "116.6 s" })).toBeVisible();
    expect(screen.getByRole("cell", { name: "231.4 s" })).toBeVisible();
    expect(screen.getByRole("cell", { name: "354.9 s" })).toBeVisible();
    expect(
      screen.getAllByRole("rowheader", { name: "wormhole-william" }),
    ).toHaveLength(3);
    expect(
      screen.getByRole("heading", {
        name: "How the tools move a file",
      }),
    ).toBeVisible();
    expect(screen.getByRole("columnheader", { name: "Transport" })).toBeVisible();
    expect(
      screen.queryByRole("columnheader", { name: "Folder behavior" }),
    ).not.toBeInTheDocument();
    const installHeading = screen.getByRole("heading", { name: "Install and run" });
    const conclusionHeading = screen.getByRole("heading", {
      name: "croc is the fastest",
    });
    expect(
      conclusionHeading.compareDocumentPosition(installHeading) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    expect(
      screen.getByText(/These are the commands I used/),
    ).toBeVisible();
    const commandSummaries = document.querySelectorAll(
      "details.blog-code-details > summary",
    );
    expect(commandSummaries).toHaveLength(8);
    expect(commandSummaries[0]).toHaveTextContent("croc — 7.2 seconds");
    expect(commandSummaries[0].parentElement).not.toHaveAttribute("open");
    (commandSummaries[0] as HTMLElement).click();
    expect(screen.getByText(/time croc YOUR-CODE/)).toBeVisible();
    expect(screen.getByText(/croc send --zip photos\//)).toBeVisible();
    expect(
      screen.getByRole("img", {
        name: /Two terminal windows exchange an audio file across the United States/i,
      }),
    ).toHaveAttribute(
      "src",
      "/blog/images/croc-cli-speed-comparison.webp",
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

  it("renders the v11.3 update with its DERP explanation and fallbacks", () => {
    render(<Blog slug="croc-v11-3-release-update" />);

    expect(
      screen.getByRole("heading", { name: "croc v11.3 goes peer-to-peer" }),
    ).toBeVisible();
    expect(screen.getByText("UPDATE 02")).toBeVisible();
    expect(
      screen.getByRole("heading", {
        name: "A server can make the introduction without carrying the file",
      }),
    ).toBeVisible();
    expect(
      screen.getByRole("heading", {
        name: "How two computers behind routers find a direct path",
      }),
    ).toBeVisible();
    expect(screen.getByRole("heading", { name: "Why QUIC is the direct stream" })).toBeVisible();
    expect(screen.getByRole("link", { name: "public DERP network" })).toHaveAttribute(
      "href",
      "https://tailscale.com/docs/reference/derp-servers",
    );
    expect(screen.getByRole("link", { name: "NAT traversal explainer" })).toHaveAttribute(
      "href",
      "https://tailscale.com/blog/how-nat-traversal-works",
    );
    expect(screen.getByRole("link", { name: "QUIC" })).toHaveAttribute(
      "href",
      "https://www.rfc-editor.org/rfc/rfc9000.html",
    );
    expect(screen.getByRole("cell", { name: "sender ↔ receiver" })).toBeVisible();
    expect(screen.getByRole("link", { name: "v11.3.0" })).toHaveAttribute(
      "href",
      "https://github.com/schollz/croc/releases/tag/v11.3.0",
    );
    expect(
      screen.getByRole("img", {
        name: /direct QUIC, public DERP fallback/i,
      }),
    ).toHaveClass("blog-article-visual-cover");
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
        name: "40 ways to send a file",
      }),
    ).toBeVisible();
    expect(screen.getAllByRole("table")).toHaveLength(2);
    expect(screen.getAllByRole("row")).toHaveLength(82);
    const [capabilityTable, architectureTable] = screen.getAllByRole("table");
    for (const tool of ["derphole", "wormhole-william", "Floe", "AirPipe"]) {
      expect(screen.getAllByRole("rowheader", { name: tool })).toHaveLength(2);
    }
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
    expect(toolLinks).toHaveLength(80);
    expect(
      new Set([...toolLinks].map((link) => link.textContent)).size,
    ).toBe(40);
    expect(
      document.querySelectorAll(
        ".blog-comparison-table td.is-indicator-column .blog-status-indicator",
      ),
    ).toHaveLength(280);
    expect(
      screen.getByRole("img", {
        name: /File transfer tools comparison showing one file connected/i,
      }),
    ).toHaveAttribute(
      "src",
      "/blog/images/compare-file-transfer-tools.webp",
    );
    expect(document.title).toBe(
      "40 Ways to Send a File: croc and 39 Alternatives",
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
          headline: "40 ways to send a file",
          wordCount: 2347,
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
