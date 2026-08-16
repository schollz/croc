import { mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";

const webRoot = path.resolve(import.meta.dirname, "..");
const distRoot = path.join(webRoot, "dist");
const metadata = JSON.parse(
  await readFile(path.join(webRoot, "src", "blog-seo.json"), "utf8"),
);
const sourceOrder = new Map(
  metadata.posts.map((post, index) => [post.slug, index]),
);
metadata.posts.sort(
  (left, right) =>
    right.publishedAt.localeCompare(left.publishedAt) ||
    sourceOrder.get(right.slug) - sourceOrder.get(left.slug),
);
const template = await readFile(path.join(distRoot, "index.html"), "utf8");
const routeMarker =
  /<!-- ROUTE_SEO_START -->[\s\S]*?<!-- ROUTE_SEO_END -->/;
const wasmPreloadMarker =
  /\s*<!-- WASM_PRELOAD_START -->[\s\S]*?<!-- WASM_PRELOAD_END -->/;

if (!routeMarker.test(template)) {
  throw new Error("Built index.html is missing the route SEO markers");
}
if (!wasmPreloadMarker.test(template)) {
  throw new Error("Built index.html is missing the WASM preload markers");
}

const blogTemplate = template.replace(wasmPreloadMarker, "");

const absoluteURL = (value) => new URL(value, `${metadata.site.url}/`).href;
const escapeHTML = (value) => String(value)
  .replaceAll("&", "&amp;")
  .replaceAll('"', "&quot;")
  .replaceAll("<", "&lt;")
  .replaceAll(">", "&gt;");
const safeJSON = (value) => JSON.stringify(value).replaceAll("<", "\\u003c");
const postSection = (entry) => entry.kind === "update" ? "Updates" : "Notes";

function commonGraph() {
  return [
    {
      "@type": "WebSite",
      "@id": `${metadata.site.url}/#website`,
      url: `${metadata.site.url}/`,
      name: metadata.site.name,
      inLanguage: metadata.site.language,
      publisher: { "@id": `${metadata.site.url}/#organization` },
    },
    {
      "@type": "Organization",
      "@id": `${metadata.site.url}/#organization`,
      name: metadata.site.publisherName,
      url: `${metadata.site.url}/`,
      logo: {
        "@type": "ImageObject",
        url: absoluteURL(metadata.site.logo),
      },
      sameAs: [metadata.site.projectUrl, metadata.site.repositoryUrl],
    },
    {
      "@type": "Person",
      "@id": `${metadata.site.authorUrl}#person`,
      name: metadata.site.authorName,
      url: metadata.site.authorUrl,
      sameAs: ["https://github.com/schollz"],
    },
  ];
}

function imageObject(entry) {
  return {
    "@type": "ImageObject",
    url: absoluteURL(entry.socialImage),
    contentUrl: absoluteURL(entry.socialImage),
    width: 1200,
    height: 630,
    caption: entry.imageAlt,
  };
}

function indexJSONLD(entry, canonicalURL) {
  const breadcrumbID = `${canonicalURL}#breadcrumb`;
  const blogID = `${canonicalURL}#blog`;
  return {
    "@context": "https://schema.org",
    "@graph": [
      {
        "@type": "CollectionPage",
        "@id": canonicalURL,
        url: canonicalURL,
        name: entry.title,
        description: entry.description,
        inLanguage: metadata.site.language,
        isPartOf: { "@id": `${metadata.site.url}/#website` },
        mainEntity: { "@id": blogID },
        primaryImageOfPage: imageObject(entry),
        breadcrumb: { "@id": breadcrumbID },
      },
      {
        "@type": "Blog",
        "@id": blogID,
        url: canonicalURL,
        name: "croc field notes",
        description: entry.description,
        inLanguage: metadata.site.language,
        author: { "@id": `${metadata.site.authorUrl}#person` },
        publisher: { "@id": `${metadata.site.url}/#organization` },
        blogPost: metadata.posts.map((post) => ({
          "@type": "BlogPosting",
          "@id": `${metadata.site.url}/blog/${post.slug}#article`,
          headline: post.title,
          url: `${metadata.site.url}/blog/${post.slug}`,
          datePublished: post.publishedAt,
          dateModified: post.modifiedAt,
          description: post.description,
          articleSection: postSection(post),
          image: absoluteURL(post.socialImage),
        })),
      },
      {
        "@type": "BreadcrumbList",
        "@id": breadcrumbID,
        itemListElement: [
          {
            "@type": "ListItem",
            position: 1,
            name: "croc",
            item: `${metadata.site.url}/`,
          },
          {
            "@type": "ListItem",
            position: 2,
            name: "Field notes",
            item: canonicalURL,
          },
        ],
      },
      ...commonGraph(),
    ],
  };
}

function postJSONLD(entry, canonicalURL) {
  const articleID = `${canonicalURL}#article`;
  const breadcrumbID = `${canonicalURL}#breadcrumb`;
  return {
    "@context": "https://schema.org",
    "@graph": [
      {
        "@type": "WebPage",
        "@id": canonicalURL,
        url: canonicalURL,
        name: entry.seoTitle ?? entry.title,
        description: entry.description,
        inLanguage: metadata.site.language,
        isPartOf: { "@id": `${metadata.site.url}/#website` },
        mainEntity: { "@id": articleID },
        primaryImageOfPage: imageObject(entry),
        breadcrumb: { "@id": breadcrumbID },
      },
      {
        "@type": "BlogPosting",
        "@id": articleID,
        url: canonicalURL,
        headline: entry.title,
        description: entry.description,
        image: imageObject(entry),
        thumbnailUrl: absoluteURL(entry.socialImage),
        datePublished: entry.publishedAt,
        dateModified: entry.modifiedAt,
        author: { "@id": `${metadata.site.authorUrl}#person` },
        publisher: { "@id": `${metadata.site.url}/#organization` },
        mainEntityOfPage: { "@id": canonicalURL },
        isPartOf: { "@id": `${metadata.site.url}/blog#blog` },
        articleSection: postSection(entry),
        genre: entry.category,
        keywords: entry.keywords,
        about: entry.keywords.map((name) => ({ "@type": "Thing", name })),
        wordCount: entry.wordCount,
        timeRequired: `PT${entry.readingMinutes}M`,
        relatedLink: entry.relatedSlugs.map(
          (slug) => `${metadata.site.url}/blog/${slug}`,
        ),
        inLanguage: metadata.site.language,
        isAccessibleForFree: true,
        license: "https://github.com/schollz/croc/blob/main/LICENSE",
      },
      {
        "@type": "BreadcrumbList",
        "@id": breadcrumbID,
        itemListElement: [
          {
            "@type": "ListItem",
            position: 1,
            name: "croc",
            item: `${metadata.site.url}/`,
          },
          {
            "@type": "ListItem",
            position: 2,
            name: "Field notes",
            item: `${metadata.site.url}/blog`,
          },
          {
            "@type": "ListItem",
            position: 3,
            name: entry.title,
            item: canonicalURL,
          },
        ],
      },
      ...commonGraph(),
    ],
  };
}

function routeSEO(entry, pathname, isPost) {
  const canonicalURL = `${metadata.site.url}${pathname}`;
  const imageURL = absoluteURL(entry.socialImage);
  const pageTitle = isPost
    ? entry.seoTitle ?? `${entry.title} | croc field notes`
    : entry.title;
  const keywords = entry.keywords.join(", ");
  const jsonLD = isPost
    ? postJSONLD(entry, canonicalURL)
    : indexJSONLD(entry, canonicalURL);
  const articleMeta = isPost
    ? [
        ["article:published_time", entry.publishedAt],
        ["article:modified_time", entry.modifiedAt],
        ["article:author", metadata.site.authorUrl],
        ["article:section", postSection(entry)],
        ...entry.keywords.map((tag) => ["article:tag", tag]),
      ].map(([property, content]) =>
        `    <meta property="${escapeHTML(property)}" content="${escapeHTML(content)}" />`,
      ).join("\n")
    : "";
  const imagePreload = isPost
    ? `\n    <link rel="preload" as="image" href="${escapeHTML(absoluteURL(entry.image))}" type="image/webp" fetchpriority="high" />`
    : "";

  return `<!-- ROUTE_SEO_START -->
    <meta name="application-name" content="croc" />
    <meta name="author" content="${escapeHTML(metadata.site.authorName)}" />
    <meta name="title" content="${escapeHTML(pageTitle)}" />
    <meta name="keywords" content="${escapeHTML(keywords)}" />
    <meta name="robots" content="index, follow, max-image-preview:large, max-snippet:-1, max-video-preview:-1" />
    <meta name="referrer" content="strict-origin-when-cross-origin" />
    <meta name="description" content="${escapeHTML(entry.description)}" />
    <link rel="canonical" href="${escapeHTML(canonicalURL)}" />
    <link rel="author" href="${escapeHTML(metadata.site.authorUrl)}" />
    <link rel="image_src" href="${escapeHTML(imageURL)}" />
    <link rel="alternate" type="application/rss+xml" title="croc field notes" href="${metadata.site.url}/blog/feed.xml" />${imagePreload}
    <meta itemprop="image" content="${escapeHTML(imageURL)}" />

    <meta property="og:type" content="${isPost ? "article" : "website"}" />
    <meta property="og:locale" content="${escapeHTML(metadata.site.locale)}" />
    <meta property="og:site_name" content="${escapeHTML(metadata.site.name)}" />
    <meta property="og:url" content="${escapeHTML(canonicalURL)}" />
    <meta property="og:title" content="${escapeHTML(entry.seoTitle ?? entry.title)}" />
    <meta property="og:description" content="${escapeHTML(entry.description)}" />
    <meta property="og:image" content="${escapeHTML(imageURL)}" />
    <meta property="og:image:secure_url" content="${escapeHTML(imageURL)}" />
    <meta property="og:image:type" content="image/jpeg" />
    <meta property="og:image:width" content="1200" />
    <meta property="og:image:height" content="630" />
    <meta property="og:image:alt" content="${escapeHTML(entry.imageAlt)}" />
${articleMeta}
    <meta name="twitter:card" content="summary_large_image" />
    <meta name="twitter:title" content="${escapeHTML(entry.seoTitle ?? entry.title)}" />
    <meta name="twitter:description" content="${escapeHTML(entry.description)}" />
    <meta name="twitter:image" content="${escapeHTML(imageURL)}" />
    <meta name="twitter:image:alt" content="${escapeHTML(entry.imageAlt)}" />

    <script type="application/ld+json" data-croc-blog="true">${safeJSON(jsonLD)}</script>
    <title>${escapeHTML(pageTitle)}</title>
    <!-- ROUTE_SEO_END -->`;
}

const routes = [
  { pathname: "/blog", entry: metadata.index, isPost: false },
  ...metadata.posts.map((entry) => ({
    pathname: `/blog/${entry.slug}`,
    entry,
    isPost: true,
  })),
];

for (const route of routes) {
  const routeHTML = blogTemplate.replace(
    routeMarker,
    routeSEO(route.entry, route.pathname, route.isPost),
  );
  const routeDirectory = path.join(distRoot, route.pathname.slice(1));
  await mkdir(routeDirectory, { recursive: true });
  await writeFile(path.join(routeDirectory, "index.html"), routeHTML);
}

const sitemapEntries = [
  { url: `${metadata.site.url}/`, priority: "1.0", changefreq: "weekly" },
  {
    url: `${metadata.site.url}/blog`,
    lastmod: Math.max(...metadata.posts.map((post) => Date.parse(post.modifiedAt))),
    priority: "0.8",
    changefreq: "weekly",
  },
  ...metadata.posts.map((post) => ({
    url: `${metadata.site.url}/blog/${post.slug}`,
    lastmod: Date.parse(post.modifiedAt),
    priority: "0.7",
    changefreq: "monthly",
  })),
];
const sitemap = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
${sitemapEntries.map((entry) => `  <url>
    <loc>${escapeHTML(entry.url)}</loc>${entry.lastmod ? `
    <lastmod>${new Date(entry.lastmod).toISOString().slice(0, 10)}</lastmod>` : ""}
    <changefreq>${entry.changefreq}</changefreq>
    <priority>${entry.priority}</priority>
  </url>`).join("\n")}
</urlset>
`;
await writeFile(path.join(distRoot, "sitemap.xml"), sitemap);

const feedDirectory = path.join(distRoot, "blog");
await mkdir(feedDirectory, { recursive: true });
const feed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
  <channel>
    <title>croc field notes</title>
    <link>${metadata.site.url}/blog</link>
    <description>${escapeHTML(metadata.index.description)}</description>
    <language>${metadata.site.language}</language>
    <atom:link href="${metadata.site.url}/blog/feed.xml" rel="self" type="application/rss+xml" />
${metadata.posts.map((post) => `    <item>
      <title>${escapeHTML(post.title)}</title>
      <link>${metadata.site.url}/blog/${post.slug}</link>
      <guid isPermaLink="true">${metadata.site.url}/blog/${post.slug}</guid>
      <pubDate>${new Date(`${post.publishedAt}T00:00:00Z`).toUTCString()}</pubDate>
      <description>${escapeHTML(post.description)}</description>
    </item>`).join("\n")}
  </channel>
</rss>
`;
await writeFile(path.join(feedDirectory, "feed.xml"), feed);
