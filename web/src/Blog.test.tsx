import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Blog } from "./Blog";

describe("Blog", () => {
  afterEach(cleanup);

  it("renders the eight-note index and updates page metadata", () => {
    render(<Blog />);

    expect(
      screen.getByRole("heading", { name: "Notes from inside the transfer." }),
    ).toBeVisible();
    expect(screen.getAllByRole("article")).toHaveLength(8);
    expect(
      screen.getByRole("link", { name: "croc field notes home" }),
    ).toHaveAttribute("href", "/blog");
    expect(document.title).toBe(
      "croc field notes — secure file transfer explained",
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

  it("renders a direct article route", () => {
    render(<Blog slug="what-four-word-code-does" />);

    expect(
      screen.getByRole("heading", { name: "What the four words are doing" }),
    ).toBeVisible();
    expect(screen.getByText("IN ONE SENTENCE")).toBeVisible();
    expect(document.title).toBe(
      "What the four words are doing — croc field notes",
    );
    expect(
      screen.getByRole("img", {
        name: /Four blank code tiles converge into one cryptographic key/i,
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

  it("renders a useful not-found page for an unknown slug", () => {
    render(<Blog slug="missing-note" />);

    expect(
      screen.getByRole("heading", { name: "This note wandered off." }),
    ).toBeVisible();
    expect(document.querySelector('meta[name="robots"]')).toHaveAttribute(
      "content",
      "noindex, follow",
    );
  });
});
