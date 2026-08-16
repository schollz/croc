import { describe, expect, it } from "vitest";
import blogSEO from "./blog-seo.json";
import {
  blogPosts,
  blogWordCount,
  getBlogPost,
  readingMinutes,
} from "./blog-posts";

describe("blog posts", () => {
  it("ships nine notes and one substantial, addressable release update", () => {
    expect(blogPosts).toHaveLength(10);
    expect(new Set(blogPosts.map((post) => post.slug)).size).toBe(10);
    expect(blogPosts.map((post) => post.slug)).toEqual([
      "croc-v11-release-update",
      "compare-file-transfer-tools",
      "share-stored-file-with-group",
      "stored-transfer-one-download",
      "browser-meets-terminal",
      "send-file-from-browser",
      "pake-step-by-step",
      "what-four-word-code-does",
      "how-croc-moves-a-file",
      "why-croc-works-this-way",
    ]);
    expect(blogPosts.filter((post) => post.kind === "note")).toHaveLength(9);
    expect(blogPosts.filter((post) => post.kind === "update")).toHaveLength(1);

    for (const post of blogPosts) {
      expect(post.slug).toMatch(/^[a-z0-9]+(?:-[a-z0-9]+)*$/);
      expect(post.blocks.length).toBeGreaterThanOrEqual(8);
      expect(post.wordCount).toBe(blogWordCount(post.blocks));
      expect(post.readingMinutes).toBe(readingMinutes(post.blocks));
      expect(post.readingMinutes).toBeGreaterThanOrEqual(2);
      expect(post.keywords.length).toBeGreaterThanOrEqual(4);
      expect(post.relatedSlugs).toHaveLength(2);
      expect(new Set(post.relatedSlugs).size).toBe(2);
      expect(post.relatedSlugs).not.toContain(post.slug);
      for (const relatedSlug of post.relatedSlugs) {
        expect(getBlogPost(relatedSlug)).toBeDefined();
      }
      expect(post.image).toBe(`/blog/images/${post.slug}.webp`);
      expect(post.socialImage).toBe(`/blog/images/${post.slug}.jpg`);
      expect(post.imageAlt.length).toBeGreaterThan(40);
      expect(["note", "update"]).toContain(post.kind);
      expect(getBlogPost(post.slug)).toBe(post);

      const seo = blogSEO.posts.find((entry) => entry.slug === post.slug);
      const seoTitle = seo && "seoTitle" in seo ? seo.seoTitle : post.title;
      expect(post.seoTitle).toBe(seoTitle);
      expect(seo).toMatchObject({
        title: post.title,
        description: post.description,
        category: post.category,
        publishedAt: post.publishedAt,
        wordCount: post.wordCount,
        readingMinutes: post.readingMinutes,
      });
      expect(seo && "kind" in seo ? seo.kind : "note").toBe(post.kind);
    }
  });

  it("does not resolve an unknown article", () => {
    expect(getBlogPost("not-a-real-field-note")).toBeUndefined();
  });

  it("orders both comparison matrices from most filled circles to least", () => {
    const post = getBlogPost("compare-file-transfer-tools");
    const tables = post?.blocks.filter((block) => block.type === "table") ?? [];
    const [capabilityTable, architectureTable] = tables;
    expect(capabilityTable).toBeDefined();
    expect(architectureTable).toBeDefined();

    const scores = capabilityTable.rowOrder?.map((tool) => {
      const row = capabilityTable.rows.find(
        (candidate) => candidate.cells[0] === tool,
      );
      expect(row).toBeDefined();
      return capabilityTable.indicatorColumns?.reduce((score, columnIndex) => {
        const marker = row?.cells[columnIndex].charAt(0);
        return score + (marker === "●" ? 1 : marker === "◐" ? 0.5 : 0);
      }, 0) ?? 0;
    }) ?? [];

    expect(capabilityTable.rowOrder).toHaveLength(capabilityTable.rows.length);
    expect(scores).toEqual([...scores].sort((left, right) => right - left));
    expect(architectureTable.rowOrder).toEqual(capabilityTable.rowOrder);
  });
});
