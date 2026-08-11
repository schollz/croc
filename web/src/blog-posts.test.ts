import { describe, expect, it } from "vitest";
import blogSEO from "./blog-seo.json";
import {
  blogPosts,
  blogWordCount,
  getBlogPost,
  readingMinutes,
} from "./blog-posts";

describe("blog posts", () => {
  it("ships eight substantial, addressable field notes", () => {
    expect(blogPosts).toHaveLength(8);
    expect(new Set(blogPosts.map((post) => post.slug)).size).toBe(8);

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
      expect(getBlogPost(post.slug)).toBe(post);

      const seo = blogSEO.posts.find((entry) => entry.slug === post.slug);
      expect(seo).toMatchObject({
        title: post.title,
        description: post.description,
        category: post.category,
        publishedAt: post.publishedAt,
        wordCount: post.wordCount,
        readingMinutes: post.readingMinutes,
      });
    }
  });

  it("does not resolve an unknown article", () => {
    expect(getBlogPost("not-a-real-field-note")).toBeUndefined();
  });
});
