import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Blog } from "./Blog";
import blogSEO from "./blog-seo.json";

describe("Blog", () => {
  afterEach(cleanup);

  it("renders one chronological list of posts with page metadata", () => {
    render(<Blog />);

    expect(
      screen.getByRole("heading", { name: "The croc blog" }),
    ).toBeVisible();
    const posts = within(screen.getByRole("region", { name: "All posts" }));
    const articles = posts.getAllByRole("article");
    expect(articles).toHaveLength(15);
    expect(posts.getAllByRole("heading", { level: 2 })[0]).toHaveTextContent(
      "croc v11.5 can update itself",
    );
    expect(posts.getAllByRole("heading", { level: 2 })[4]).toHaveTextContent(
      "How fast is croc?",
    );
    for (const article of articles) {
      expect(within(article).getAllByRole("link")).toHaveLength(1);
      expect(article.querySelector("time")).toBeVisible();
    }
    expect(screen.queryByRole("navigation", {
      name: "Blog post categories",
    })).not.toBeInTheDocument();
    expect(
      screen.getByRole("heading", {
        name: "40 ways to send a file",
      }),
    ).toBeVisible();
    expect(
      screen.getByRole("link", { name: "croc field notes home" }),
    ).toHaveAttribute("href", "/blog");
    expect(screen.queryByRole("navigation", {
      name: "More ways to transfer with croc",
    })).not.toBeInTheDocument();
    expect(document.querySelector(".blog-footer")?.previousElementSibling?.tagName).toBe("MAIN");
    expect(document.title).toBe(blogSEO.index.title);
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

  it("renders the shared SSH terminal field note", () => {
    render(<Blog slug="share-terminal-with-croc-ssh" />);

    expect(
      screen.getByRole("heading", { name: "Share a terminal with croc ssh" }),
    ).toBeVisible();
    expect(document.querySelector(".article-byline time")).toBeVisible();
    expect(screen.getByRole("heading", { name: "Joining the terminal" })).toBeVisible();
    expect(
      screen.getByRole("heading", { name: "How it connects" }),
    ).toBeVisible();
    expect(screen.getByText(/PAKE, pinned-host-key verification/)).toBeVisible();
    for (const link of screen.getAllByRole("link", { name: "Upterm" })) {
      expect(link).toHaveAttribute("href", "https://github.com/owenthereal/upterm");
    }
    for (const link of screen.getAllByRole("link", { name: "sshx" })) {
      expect(link).toHaveAttribute("href", "https://github.com/ekzhang/sshx");
    }
    expect(screen.getByRole("cell", { name: "Six-word read/write and read-only invitations" })).toBeVisible();
    expect(
      screen.getByRole("img", {
        name: /one host shell, a shared PTY, and separate read\/write and read-only invitations/i,
      }),
    ).toHaveClass("blog-article-visual-cover");
    expect(document.querySelector('meta[property="og:image"]')).toHaveAttribute(
      "content",
      "https://getcroc.com/blog/images/share-terminal-with-croc-ssh.jpg",
    );
  });

  it("compares multiple files and one large file with simple time and rate tables", () => {
    render(<Blog slug="croc-cli-speed-comparison" />);

    expect(screen.getByRole("heading", { name: "How fast is croc?" })).toBeVisible();
    expect(document.querySelector(".article-byline time")).toBeVisible();
    expect(screen.getAllByRole("table")).toHaveLength(2);
    const folderTable = within(screen.getByRole("table", {
      name: "Multiple files · Mac to Toronto",
    }));
    expect(folderTable.getAllByRole("columnheader").map(cell => cell.textContent)).toEqual([
      "Tool", "Time", "Effective rate",
    ]);
    expect(folderTable.getAllByRole("row")).toHaveLength(9);
    expect(folderTable.getAllByRole("row").slice(1).map(row =>
      [...row.children].map(cell => cell.textContent),
    )).toEqual([
      ["croc", "4.42 s", "4.69 MB/s"],
      ["derphole", "4.76 s", "4.35 MB/s"],
      ["iroh Sendme / DashBeam CLI", "6.37 s", "3.25 MB/s"],
      ["Magic Wormhole", "9.14 s", "2.27 MB/s"],
      ["wormhole-william", "10.69 s", "1.94 MB/s"],
      ["AirPipe", "15.21 s", "1.36 MB/s"],
      ["Floe", "57.84 s", "0.36 MB/s"],
      ["WebWormhole", "Folder transfer unsupported", "—"],
    ]);
    const audioTable = within(screen.getByRole("table", { name: "Single large file · Mac to Toronto" }));
    expect(audioTable.getAllByRole("columnheader").map(cell => cell.textContent)).toEqual([
      "Tool", "Time", "Effective rate",
    ]);
    expect(audioTable.getAllByRole("row").slice(1).map(row =>
      [...row.children].map(cell => cell.textContent),
    )).toEqual([
      ["croc", "8.52 s", "37.27 MB/s"],
      ["derphole", "10.65 s", "29.80 MB/s"],
      ["wormhole-william", "126.81 s", "2.50 MB/s"],
      ["iroh Sendme / DashBeam CLI", "129.72 s", "2.45 MB/s"],
      ["Magic Wormhole", "150.36 s", "2.11 MB/s"],
      ["Floe", "190.69 s", "1.67 MB/s"],
      ["AirPipe", "200.26 s", "1.59 MB/s"],
      ["WebWormhole", "Transfer failed", "—"],
    ]);
    expect(screen.getByText(/ran each transfer three times in shuffled rounds and used the middle time/)).toBeVisible();
    expect(screen.getByText(/timer started immediately before the sender launched/)).toBeVisible();
    expect(screen.getByText(/532 files totaling 20,708,906 bytes/)).toBeVisible();
    expect(screen.getByText(/cc781806baaa17d29d94209eecf0c85bc178d9e1/)).toBeVisible();
    expect(screen.getByText(/automatic ZIP creation and extraction both counted/)).toBeVisible();
    expect(screen.getByText(/317,520,080 bytes \(317\.52 MB\)/)).toBeVisible();
    expect(screen.getByText(/middle of three successful transfers/)).toBeVisible();
    expect(screen.getByRole("link", { name: "audio benchmark record" })).toHaveAttribute(
      "href", "https://github.com/schollz/croc/blob/main/src/docs/CLI_AUDIO_BENCHMARK.md",
    );
    expect(screen.getByRole("link", { name: "DashBeam" })).toHaveAttribute(
      "href", "https://www.dashbeam.net/en/downloads",
    );
    expect(screen.getByRole("link", { name: "benchmark record" })).toHaveAttribute(
      "href", "https://github.com/schollz/croc/blob/main/src/docs/CLI_FOLDER_BENCHMARK.md",
    );
    const article = document.querySelector(".blog-article-body");
    expect(screen.getAllByRole("heading", { level: 2 }).map(h => h.textContent)).toEqual([
      "Sending multiple files", "Sending one large file",
    ]);
    expect(screen.queryByRole("navigation", { name: "In this field note" })).not.toBeInTheDocument();
    expect(article?.querySelector("details")).toBeNull();
    expect(article?.textContent).not.toMatch(/photos|--zip|median|ranges|local build|local development|unpublished|croc is the fastest|11\.6\.1/i);
    expect(screen.getByRole("img", {
      name: /Two terminal windows exchange an audio file across the United States/i,
    })).toHaveAttribute("src", "/blog/images/croc-cli-speed-comparison.webp");
  });

  it("renders the v11 release update with its release history", () => {
    render(<Blog slug="croc-v11-release-update" />);

    expect(
      screen.getByRole("heading", { name: "From croc v10 to v11" }),
    ).toBeVisible();
    expect(document.querySelector(".article-byline time")).toBeVisible();
    expect(
      screen.getByRole("heading", { name: "Handshake and relay limits" }),
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
    expect(document.querySelector(".article-byline time")).toBeVisible();
    expect(
      screen.getByRole("heading", {
        name: "Direct and relayed paths",
      }),
    ).toBeVisible();
    expect(
      screen.getByText(/UDP hole punching, without manual port forwarding/),
    ).toBeVisible();
    expect(document.querySelector(".blog-article-body")).toHaveTextContent(
      "WireGuard protects packets, magicsock discovers endpoints",
    );
    expect(screen.getByRole("link", { name: "public DERP network" })).toHaveAttribute(
      "href",
      "https://tailscale.com/docs/reference/derp-servers",
    );
    expect(screen.getByRole("link", { name: "NAT traversal explainer" })).toHaveAttribute(
      "href",
      "https://tailscale.com/blog/how-nat-traversal-works",
    );
    expect(screen.getByRole("link", { name: "WireGuard" })).toHaveAttribute(
      "href",
      "https://www.wireguard.com/protocol/",
    );
    expect(screen.getByRole("cell", { name: "sender ↔ receiver" })).toBeVisible();
    expect(screen.getByRole("link", { name: "v11.3.0" })).toHaveAttribute(
      "href",
      "https://github.com/schollz/croc/releases/tag/v11.3.0",
    );
    expect(
      screen.getByRole("img", {
        name: /Tailcat direct transport, public DERP fallback/i,
      }),
    ).toHaveClass("blog-article-visual-cover");
  });

  it("renders a direct article route", () => {
    render(<Blog slug="what-four-word-code-does" />);

    expect(
      screen.getByRole("heading", { name: "What the three words are doing" }),
    ).toBeVisible();
    expect(screen.queryByText("IN ONE SENTENCE")).not.toBeInTheDocument();
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
    ).toHaveAttribute("content", "2026-11-01");
    expect(
      screen.queryByRole("navigation", { name: "In this field note" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getAllByRole("link", { name: /PAKE, step by step/ }),
    ).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          href: expect.stringContaining("/blog/pake-step-by-step"),
        }),
      ]),
    );
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
          wordCount: blogSEO.posts.find(post => post.slug === "compare-file-transfer-tools")?.wordCount,
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
