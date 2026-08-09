import { useEffect, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  BookOpenText,
  Check,
  Clock3,
  Download,
  FileText,
  KeyRound,
  Laptop,
  Link2,
  RadioTower,
  ShieldCheck,
  Smartphone,
  Moon,
  Sun,
  Terminal,
  Timer,
  Upload,
} from "lucide-react";
import {
  blogPosts,
  getBlogPost,
  type BlogBlock,
  type BlogPost,
  type BlogVisual,
} from "./blog-posts";
import blogSEO from "./blog-seo.json";

const { site, index: indexSEO } = blogSEO;
const siteURL = site.url;
type BlogTheme = "dark" | "light";

function initialBlogTheme(): BlogTheme {
  try {
    const stored = localStorage.getItem("croc-web-theme");
    if (stored === "light" || stored === "dark") return stored;
  } catch {
    // Use the system preference.
  }
  return window.matchMedia?.("(prefers-color-scheme: light)").matches
    ? "light"
    : "dark";
}

function absoluteURL(path: string) {
  return new URL(path, `${siteURL}/`).href;
}

function blogStructuredData(post?: BlogPost) {
  const canonicalURL = post ? `${siteURL}/blog/${post.slug}` : `${siteURL}/blog`;
  const entry = post ?? indexSEO;
  const image = {
    "@type": "ImageObject",
    url: absoluteURL(entry.socialImage),
    contentUrl: absoluteURL(entry.socialImage),
    width: 1200,
    height: 630,
    caption: entry.imageAlt,
  };
  const common = [
    {
      "@type": "WebSite",
      "@id": `${siteURL}/#website`,
      url: `${siteURL}/`,
      name: site.name,
      inLanguage: site.language,
      publisher: { "@id": `${siteURL}/#organization` },
    },
    {
      "@type": "Organization",
      "@id": `${siteURL}/#organization`,
      name: site.publisherName,
      url: `${siteURL}/`,
      logo: { "@type": "ImageObject", url: absoluteURL(site.logo) },
      sameAs: ["https://github.com/schollz/croc"],
    },
    {
      "@type": "Person",
      "@id": `${site.authorUrl}#person`,
      name: site.authorName,
      url: site.authorUrl,
      sameAs: ["https://github.com/schollz"],
    },
  ];

  if (post) {
    const breadcrumbID = `${canonicalURL}#breadcrumb`;
    return {
      "@context": "https://schema.org",
      "@graph": [
        {
          "@type": "WebPage",
          "@id": canonicalURL,
          url: canonicalURL,
          name: post.title,
          description: post.description,
          inLanguage: site.language,
          isPartOf: { "@id": `${siteURL}/#website` },
          mainEntity: { "@id": `${canonicalURL}#article` },
          primaryImageOfPage: image,
          breadcrumb: { "@id": breadcrumbID },
        },
        {
          "@type": "BlogPosting",
          "@id": `${canonicalURL}#article`,
          url: canonicalURL,
          headline: post.title,
          description: post.description,
          image,
          thumbnailUrl: absoluteURL(post.socialImage),
          datePublished: post.publishedAt,
          dateModified: post.modifiedAt,
          author: { "@id": `${site.authorUrl}#person` },
          publisher: { "@id": `${siteURL}/#organization` },
          mainEntityOfPage: { "@id": canonicalURL },
          isPartOf: { "@id": `${siteURL}/blog#blog` },
          articleSection: post.category,
          keywords: post.keywords,
          about: post.keywords.map((name) => ({ "@type": "Thing", name })),
          wordCount: post.wordCount,
          timeRequired: `PT${post.readingMinutes}M`,
          inLanguage: site.language,
          isAccessibleForFree: true,
          license: "https://github.com/schollz/croc/blob/main/LICENSE",
        },
        {
          "@type": "BreadcrumbList",
          "@id": breadcrumbID,
          itemListElement: [
            { "@type": "ListItem", position: 1, name: "croc", item: `${siteURL}/` },
            { "@type": "ListItem", position: 2, name: "Field notes", item: `${siteURL}/blog` },
            { "@type": "ListItem", position: 3, name: post.title, item: canonicalURL },
          ],
        },
        ...common,
      ],
    };
  }

  return {
    "@context": "https://schema.org",
    "@graph": [
      {
        "@type": "CollectionPage",
        "@id": canonicalURL,
        url: canonicalURL,
        name: indexSEO.title,
        description: indexSEO.description,
        inLanguage: site.language,
        isPartOf: { "@id": `${siteURL}/#website` },
        mainEntity: { "@id": `${siteURL}/blog#blog` },
        primaryImageOfPage: image,
        breadcrumb: { "@id": `${canonicalURL}#breadcrumb` },
      },
      {
        "@type": "Blog",
        "@id": `${siteURL}/blog#blog`,
        url: canonicalURL,
        name: "croc field notes",
        description: indexSEO.description,
        inLanguage: site.language,
        author: { "@id": `${site.authorUrl}#person` },
        publisher: { "@id": `${siteURL}/#organization` },
        blogPost: blogPosts.map((entry) => ({
          "@type": "BlogPosting",
          "@id": `${siteURL}/blog/${entry.slug}#article`,
          headline: entry.title,
          url: `${siteURL}/blog/${entry.slug}`,
          datePublished: entry.publishedAt,
          dateModified: entry.modifiedAt,
          image: absoluteURL(entry.socialImage),
        })),
      },
      {
        "@type": "BreadcrumbList",
        "@id": `${canonicalURL}#breadcrumb`,
        itemListElement: [
          { "@type": "ListItem", position: 1, name: "croc", item: `${siteURL}/` },
          { "@type": "ListItem", position: 2, name: "Field notes", item: canonicalURL },
        ],
      },
      ...common,
    ],
  };
}

function useBlogMetadata(post?: BlogPost, missing = false, missingSlug?: string) {
  useEffect(() => {
    const pageTitle = missing
      ? "Article not found — croc field notes"
      : post
        ? `${post.title} — croc field notes`
        : indexSEO.title;
    const shareTitle = missing ? pageTitle : post?.title ?? indexSEO.title;
    const description = missing
      ? "This field note could not be found."
      : post?.description ?? indexSEO.description;
    const keywords = missing ? [] : post?.keywords ?? indexSEO.keywords;
    const imagePath = missing
      ? site.logo
      : post?.socialImage ?? indexSEO.socialImage;
    const imageAlt = missing
      ? "croc file transfer"
      : post?.imageAlt ?? indexSEO.imageAlt;
    const path = post
      ? `/blog/${post.slug}`
      : missing && missingSlug
        ? `/blog/${missingSlug}`
        : "/blog";
    const canonicalURL = `${siteURL}${path}`;
    const imageURL = absoluteURL(imagePath);
    const previousTitle = document.title;
    document.title = pageTitle;

    const updates = [
      ['meta[name="description"]', "content", description],
      ['meta[name="title"]', "content", pageTitle],
      ['meta[name="author"]', "content", site.authorName],
      ['meta[name="keywords"]', "content", keywords.join(", ")],
      ['meta[property="og:title"]', "content", shareTitle],
      ['meta[property="og:description"]', "content", description],
      ['meta[property="og:url"]', "content", canonicalURL],
      ['meta[property="og:type"]', "content", post ? "article" : "website"],
      ['meta[property="og:locale"]', "content", site.locale],
      ['meta[property="og:site_name"]', "content", site.name],
      ['meta[property="og:image"]', "content", imageURL],
      ['meta[property="og:image:secure_url"]', "content", imageURL],
      ['meta[property="og:image:type"]', "content", "image/jpeg"],
      ['meta[property="og:image:width"]', "content", "1200"],
      ['meta[property="og:image:height"]', "content", "630"],
      ['meta[property="og:image:alt"]', "content", imageAlt],
      ['meta[name="twitter:card"]', "content", "summary_large_image"],
      ['meta[name="twitter:title"]', "content", shareTitle],
      ['meta[name="twitter:description"]', "content", description],
      ['meta[name="twitter:image"]', "content", imageURL],
      ['meta[name="twitter:image:alt"]', "content", imageAlt],
      ['meta[itemprop="image"]', "content", imageURL],
      ['meta[name="robots"]', "content", missing ? "noindex, follow" : "index, follow, max-image-preview:large"],
      ['link[rel="canonical"]', "href", canonicalURL],
      ['link[rel="image_src"]', "href", imageURL],
    ] as const;
    const previousValues = updates.map(([selector, attribute, value]) => {
      let element = document.querySelector(selector);
      const created = element === null;
      if (!element) {
        if (selector.startsWith("link")) {
          element = document.createElement("link");
          element.setAttribute("rel", "canonical");
        } else {
          element = document.createElement("meta");
          const identity = selector.match(/\[(name|property|itemprop|rel)="([^"]+)"\]/);
          if (identity) element.setAttribute(identity[1], identity[2]);
        }
        document.head.append(element);
      }
      const previous = element?.getAttribute(attribute) ?? null;
      element?.setAttribute(attribute, value);
      return { element, attribute, previous, created };
    });

    const articleUpdates = post ? [
      ['meta[property="article:published_time"]', "content", post.publishedAt],
      ['meta[property="article:modified_time"]', "content", post.modifiedAt],
      ['meta[property="article:author"]', "content", site.authorUrl],
      ['meta[property="article:section"]', "content", post.category],
    ] as const : [];
    const previousArticleValues = articleUpdates.map(([selector, attribute, value]) => {
      let element = document.querySelector(selector);
      const created = element === null;
      if (!element) {
        element = document.createElement("meta");
        const identity = selector.match(/\[property="([^"]+)"\]/);
        if (identity) element.setAttribute("property", identity[1]);
        document.head.append(element);
      }
      const previous = element.getAttribute(attribute);
      element.setAttribute(attribute, value);
      return { element, attribute, previous, created };
    });

    const createdTags: HTMLMetaElement[] = [];
    if (post && document.querySelectorAll('meta[property="article:tag"]').length === 0) {
      for (const keyword of post.keywords) {
        const tag = document.createElement("meta");
        tag.setAttribute("property", "article:tag");
        tag.content = keyword;
        document.head.append(tag);
        createdTags.push(tag);
      }
    }

    let feedLink = document.querySelector<HTMLLinkElement>(
      'link[rel="alternate"][type="application/rss+xml"]',
    );
    const feedLinkCreated = feedLink === null;
    const previousFeedHref = feedLink?.href ?? null;
    if (!feedLink) {
      feedLink = document.createElement("link");
      feedLink.rel = "alternate";
      feedLink.type = "application/rss+xml";
      feedLink.title = "croc field notes";
      document.head.append(feedLink);
    }
    feedLink.href = `${siteURL}/blog/feed.xml`;

    let structuredData = document.querySelector<HTMLScriptElement>(
      'script[type="application/ld+json"][data-croc-blog="true"]',
    );
    const structuredDataCreated = structuredData === null;
    const previousStructuredData = structuredData?.text ?? null;
    if (!missing) {
      if (!structuredData) {
        structuredData = document.createElement("script");
        structuredData.type = "application/ld+json";
        structuredData.dataset.crocBlog = "true";
        document.head.append(structuredData);
      }
      structuredData.text = JSON.stringify(blogStructuredData(post));
    }

    return () => {
      document.title = previousTitle;
      for (const { element, attribute, previous, created } of previousValues) {
        if (!element) continue;
        if (created) element.remove();
        else if (previous === null) element.removeAttribute(attribute);
        else element.setAttribute(attribute, previous);
      }
      for (const { element, attribute, previous, created } of previousArticleValues) {
        if (created) element.remove();
        else if (previous === null) element.removeAttribute(attribute);
        else element.setAttribute(attribute, previous);
      }
      for (const tag of createdTags) tag.remove();
      if (feedLinkCreated) feedLink.remove();
      else if (previousFeedHref !== null) feedLink.href = previousFeedHref;
      if (structuredDataCreated) structuredData?.remove();
      else if (structuredData && previousStructuredData !== null) {
        structuredData.text = previousStructuredData;
      }
    };
  }, [missing, missingSlug, post]);
}

function BlogHeader() {
  const [theme, setTheme] = useState<BlogTheme>(initialBlogTheme);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    document.documentElement.style.colorScheme = theme;
    document
      .querySelector('meta[name="theme-color"]')
      ?.setAttribute("content", theme === "dark" ? "#121411" : "#f2f1ec");
    try {
      localStorage.setItem("croc-web-theme", theme);
    } catch {
      // Theme persistence is optional.
    }
  }, [theme]);

  return (
    <header className="blog-site-header">
      <div className="blog-site-header-inner">
        <a className="blog-brand" href="/blog" aria-label="croc field notes home">
          <img
            src="/croc.jpg"
            width="408"
            height="196"
            alt=""
            aria-hidden="true"
          />
          <span>
            <small>Notes from inside the transfer</small>
            <strong>croc field notes</strong>
          </span>
        </a>
        <nav aria-label="Blog navigation">
          <a href="/blog">
            <BookOpenText aria-hidden="true" />
            All notes
          </a>
          <a href="/">
            <ArrowLeft aria-hidden="true" />
            Transfer files
          </a>
          <button
            type="button"
            aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} mode`}
            title={`Switch to ${theme === "dark" ? "light" : "dark"} mode`}
            onClick={() => setTheme((current) => current === "dark" ? "light" : "dark")}
          >
            {theme === "dark" ? <Sun aria-hidden="true" /> : <Moon aria-hidden="true" />}
          </button>
        </nav>
      </div>
    </header>
  );
}

function BlogFooter() {
  return (
    <footer className="blog-footer">
      <span>croc field notes · written in the open</span>
      <nav aria-label="Blog footer navigation">
        <a href="/">transfer files</a>
        <a href="https://github.com/schollz/croc">source code</a>
        <a href="https://github.com/sponsors/schollz">support croc</a>
      </nav>
    </footer>
  );
}

function OverviewVisual() {
  return (
    <div className="overview-visual">
      <span>
        <RadioTower />
        <small>live relay</small>
        <strong>fast</strong>
      </span>
      <span>
        <KeyRound />
        <small>fresh session key</small>
        <strong>secure</strong>
      </span>
      <span>
        <Check />
        <small>one short code</small>
        <strong>easy</strong>
      </span>
      <i>Choose all three.</i>
    </div>
  );
}

function RelayVisual() {
  return (
    <div className="relay-visual">
      <span className="visual-node">
        <Laptop />
        <small>sender</small>
      </span>
      <span className="visual-line"><i /></span>
      <span className="visual-node relay-node">
        <RadioTower />
        <small>relay</small>
      </span>
      <span className="visual-line"><i /></span>
      <span className="visual-node">
        <Smartphone />
        <small>receiver</small>
      </span>
    </div>
  );
}

function CodeVisual() {
  return (
    <div className="code-visual">
      <div>
        {['river', 'cabin', 'lantern', 'moss'].map((word) => (
          <span key={word}>{word}</span>
        ))}
      </div>
      <ArrowRight />
      <span className="derived-key">
        <KeyRound />
        fresh session key
      </span>
    </div>
  );
}

function HandshakeVisual() {
  return (
    <div className="handshake-visual">
      <span className="handshake-party">
        <small>receiver · A</small>
        <strong>X</strong>
        <i>fresh a</i>
      </span>
      <span className="handshake-exchange">
        <i>X <ArrowRight /></i>
        <strong><KeyRound /> shared key</strong>
        <i><ArrowLeft /> Y + salt</i>
      </span>
      <span className="handshake-party">
        <small>sender · B</small>
        <strong>Y</strong>
        <i>fresh b</i>
      </span>
      <span className="handshake-confirmation">
        <ShieldCheck /> derive · confirm A · confirm B · encrypt
      </span>
    </div>
  );
}

function BrowserVisual() {
  return (
    <div className="browser-visual">
      <div className="mock-browser-bar"><i /><i /><i /><span>getcroc.com</span></div>
      <div className="mock-browser-body">
        <span><Upload /> Choose files</span>
        <code>river-cabin-lantern-moss</code>
        <strong><ArrowRight /> Send files</strong>
      </div>
    </div>
  );
}

function BridgeVisual() {
  return (
    <div className="bridge-visual">
      <span><Laptop /><small>browser</small></span>
      <div><i>••••</i><ArrowRight /><i>••••</i></div>
      <span><Terminal /><small>terminal</small></span>
    </div>
  );
}

function StoredVisual() {
  return (
    <div className="stored-visual">
      <span className="stored-file"><FileText /><i>ciphertext</i></span>
      <span className="stored-link"><Link2 />/s/id<strong>#v1.key</strong></span>
      <span className="stored-expiry"><Timer />24 hours <i>/</i> one download</span>
    </div>
  );
}

function BlogVisualCard({ visual, large = false }: { visual: BlogVisual; large?: boolean }) {
  return (
    <div className={`blog-visual blog-visual-${visual}${large ? " large" : ""}`} aria-hidden="true">
      {visual === "overview" ? <OverviewVisual /> : null}
      {visual === "relay" ? <RelayVisual /> : null}
      {visual === "code" ? <CodeVisual /> : null}
      {visual === "handshake" ? <HandshakeVisual /> : null}
      {visual === "browser" ? <BrowserVisual /> : null}
      {visual === "bridge" ? <BridgeVisual /> : null}
      {visual === "stored" ? <StoredVisual /> : null}
    </div>
  );
}

function BlogCoverImage({ post }: { post: BlogPost }) {
  return (
    <figure className="blog-article-cover">
      <img
        src={post.image}
        width="1200"
        height="630"
        alt={post.imageAlt}
        decoding="async"
        fetchPriority="high"
      />
    </figure>
  );
}

function PostMeta({ post }: { post: BlogPost }) {
  return (
    <p className="blog-post-meta">
      <span>{post.category}</span>
      <span aria-hidden="true">·</span>
      <time dateTime={post.publishedAt}>{post.publishedLabel}</time>
      <span aria-hidden="true">·</span>
      <span><Clock3 aria-hidden="true" />{post.readingMinutes} min read</span>
    </p>
  );
}

function BlogIndex() {
  useBlogMetadata();
  const [featured, ...morePosts] = blogPosts;

  return (
    <div className="blog-shell">
      <BlogHeader />
      <main className="blog-main">
        <section className="blog-index-hero">
          <div>
            <p className="blog-kicker"><BookOpenText /> Field notes</p>
            <h1>Notes from inside the transfer.</h1>
          </div>
          <p>
            Seven plainspoken guides to the small code phrase, the relay in the
            middle, the encryption around it, and the useful ways a file gets
            from this computer to that one.
          </p>
        </section>

        <article className="featured-note">
          <a
            className="featured-note-visual"
            href={`/blog/${featured.slug}`}
            aria-label={`Read ${featured.title}`}
          >
            <BlogVisualCard visual={featured.visual} large />
          </a>
          <div className="featured-note-copy">
            <span className="note-number">FIELD NOTE {featured.number}</span>
            <PostMeta post={featured} />
            <h2><a href={`/blog/${featured.slug}`}>{featured.title}</a></h2>
            <p>{featured.description}</p>
            <a className="blog-read-link" href={`/blog/${featured.slug}`}>
              Read the field note <ArrowRight />
            </a>
          </div>
        </article>

        <section className="blog-notes-section" aria-labelledby="more-notes-title">
          <div className="blog-section-heading">
            <div>
              <p className="blog-kicker"><FileText /> The notebook</p>
              <h2 id="more-notes-title">Six more ways into croc</h2>
            </div>
            <span>{String(blogPosts.length).padStart(2, "0")} notes</span>
          </div>
          <div className="blog-grid">
            {morePosts.map((post) => (
              <article className="blog-card" key={post.slug}>
                <a
                  className="blog-card-visual"
                  href={`/blog/${post.slug}`}
                  aria-label={`Read ${post.title}`}
                >
                  <BlogVisualCard visual={post.visual} />
                  <span>{post.number}</span>
                </a>
                <div className="blog-card-copy">
                  <PostMeta post={post} />
                  <h3><a href={`/blog/${post.slug}`}>{post.title}</a></h3>
                  <p>{post.description}</p>
                  <a className="blog-read-link" href={`/blog/${post.slug}`}>
                    Keep reading <ArrowRight />
                  </a>
                </div>
              </article>
            ))}
          </div>
        </section>

        <section className="blog-transfer-cta">
          <span><Check aria-hidden="true" /></span>
          <div>
            <p className="blog-kicker">Enough reading?</p>
            <h2>Move the file.</h2>
            <p>No account. No port forwarding. The browser is already a croc peer.</p>
          </div>
          <a href="/">Open croc web <ArrowRight /></a>
        </section>
      </main>
      <BlogFooter />
    </div>
  );
}

function BlogBlockView({ block, index }: { block: BlogBlock; index: number }) {
  if (block.type === "heading") return <h2 id={`section-${index}`}>{block.text}</h2>;
  if (block.type === "list") {
    return <ul>{block.items.map((item) => <li key={item}>{item}</li>)}</ul>;
  }
  if (block.type === "code") {
    return (
      <figure className="blog-code-block">
        <figcaption>{block.label}</figcaption>
        <pre><code>{block.lines.join("\n")}</code></pre>
      </figure>
    );
  }
  if (block.type === "aside") {
    return (
      <aside className="blog-callout">
        <span>{block.eyebrow}</span>
        <h3>{block.title}</h3>
        <p>{block.text}</p>
      </aside>
    );
  }
  return <p>{block.text}</p>;
}

function BlogArticle({ post }: { post: BlogPost }) {
  useBlogMetadata(post);
  const index = blogPosts.findIndex((candidate) => candidate.slug === post.slug);
  const previous = index > 0 ? blogPosts[index - 1] : undefined;
  const next = index < blogPosts.length - 1 ? blogPosts[index + 1] : blogPosts[0];

  return (
    <div className="blog-shell">
      <BlogHeader />
      <main className="article-main">
        <a className="back-to-blog" href="/blog"><ArrowLeft /> All field notes</a>
        <article className="blog-article">
          <header className="blog-article-header">
            <p className="blog-kicker"><ShieldCheck /> {post.category}</p>
            <span className="note-number">FIELD NOTE {post.number}</span>
            <h1>{post.title}</h1>
            <p className="article-dek">{post.description}</p>
            <div className="article-byline">
              <span>By {post.author}</span>
              <span aria-hidden="true">·</span>
              <time dateTime={post.publishedAt}>{post.publishedLabel}</time>
              <span aria-hidden="true">·</span>
              <span>{post.readingMinutes} min read</span>
            </div>
          </header>

          <BlogCoverImage post={post} />

          <div className="blog-article-layout">
            <aside className="article-rail">
              <span><KeyRound /></span>
              <p>IN ONE SENTENCE</p>
              <strong>{post.takeaway}</strong>
            </aside>
            <div className="blog-article-body">
              {post.blocks.map((block, blockIndex) => (
                <BlogBlockView block={block} index={blockIndex} key={`${block.type}-${blockIndex}`} />
              ))}
              <section className="article-transfer-card">
                <div>
                  <p className="blog-kicker"><Upload /> Try the protocol</p>
                  <h2>Send something small.</h2>
                  <p>The quickest explanation is still a transfer between two devices.</p>
                </div>
                <a href="/">Open croc web <ArrowRight /></a>
              </section>
            </div>
          </div>
        </article>

        <nav className="blog-post-navigation" aria-label="More field notes">
          {previous ? (
            <a href={`/blog/${previous.slug}`}>
              <small><ArrowLeft /> Previous note</small>
              <strong>{previous.title}</strong>
            </a>
          ) : <span />}
          <a href={`/blog/${next.slug}`}>
            <small>Next note <ArrowRight /></small>
            <strong>{next.title}</strong>
          </a>
        </nav>
      </main>
      <BlogFooter />
    </div>
  );
}

function BlogNotFound({ slug }: { slug: string }) {
  useBlogMetadata(undefined, true, slug);
  return (
    <div className="blog-shell">
      <BlogHeader />
      <main className="blog-not-found">
        <span>404</span>
        <h1>This note wandered off.</h1>
        <p>The transfer is fine. This address is not one of the seven field notes.</p>
        <a href="/blog">Return to all notes <ArrowRight /></a>
      </main>
      <BlogFooter />
    </div>
  );
}

export function Blog({ slug }: { slug?: string }) {
  if (!slug) return <BlogIndex />;
  const post = getBlogPost(slug);
  return post ? <BlogArticle post={post} /> : <BlogNotFound slug={slug} />;
}
