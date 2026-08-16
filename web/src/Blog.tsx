import { useEffect, useState } from "react";
import {
  ArrowLeft,
  ArrowRight,
  BookOpenText,
  Check,
  Circle,
  Clock3,
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
  Users,
  Zap,
} from "lucide-react";
import {
  blogPosts,
  getBlogPost,
  type BlogBlock,
  type BlogPost,
  type BlogVisual,
} from "./blog-posts";
import blogSEO from "./blog-seo.json";
import { TransferLinks } from "./TransferLinks";

const { site, index: indexSEO } = blogSEO;
const siteURL = site.url;
type BlogTheme = "dark" | "light";

function postKindLabel(post: BlogPost) {
  return post.kind === "update" ? "Update" : "Note";
}

function postSection(post: BlogPost) {
  return post.kind === "update" ? "Updates" : "Notes";
}

function postNumberLabel(post: BlogPost) {
  return post.kind === "update"
    ? `UPDATE ${post.number}`
    : `FIELD NOTE ${post.number}`;
}

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
      sameAs: [site.projectUrl, site.repositoryUrl],
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
          name: post.seoTitle,
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
          articleSection: postSection(post),
          genre: post.category,
          keywords: post.keywords,
          about: post.keywords.map((name) => ({ "@type": "Thing", name })),
          wordCount: post.wordCount,
          timeRequired: `PT${post.readingMinutes}M`,
          relatedLink: post.relatedSlugs.map(
            (slug) => `${siteURL}/blog/${slug}`,
          ),
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
          description: entry.description,
          articleSection: postSection(entry),
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
      ? "Article not found | croc field notes"
      : post
        ? post.seoTitle === post.title
          ? `${post.title} | croc field notes`
          : post.seoTitle
        : indexSEO.title;
    const shareTitle = missing ? pageTitle : post?.seoTitle ?? indexSEO.title;
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
      ['meta[name="robots"]', "content", missing ? "noindex, follow" : "index, follow, max-image-preview:large, max-snippet:-1, max-video-preview:-1"],
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
      ['meta[property="article:section"]', "content", postSection(post)],
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
            <small>Notes and release updates</small>
            <strong>croc field notes</strong>
          </span>
        </a>
        <nav aria-label="Blog navigation">
          <a href="/blog">
            <BookOpenText aria-hidden="true" />
            All posts
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
        <a href={site.projectUrl}>croc website</a>
        <a href={site.repositoryUrl}>source code</a>
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
        <code>river-cloud-daisy</code>
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
      <span className="stored-expiry"><Timer />chosen lifetime <i>/</i> download limit</span>
    </div>
  );
}

function ReleaseVisual() {
  return (
    <div className="release-visual">
      <span>
        <ShieldCheck />
        <small>bound handshake</small>
        <strong>safer</strong>
      </span>
      <span>
        <Users />
        <small>stored transfers</small>
        <strong>for groups</strong>
      </span>
      <span>
        <Zap />
        <small>web code</small>
        <strong>faster</strong>
      </span>
      <i>croc v11 · the command stayed small</i>
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
      {visual === "release" ? <ReleaseVisual /> : null}
    </div>
  );
}

function BlogCoverImage({ post }: { post: BlogPost }) {
  if (post.visual === "release") {
    return (
      <figure
        className="blog-article-cover blog-article-visual-cover"
        role="img"
        aria-label={post.imageAlt}
      >
        <BlogVisualCard visual={post.visual} large />
      </figure>
    );
  }

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
      <span className={`blog-post-kind is-${post.kind}`}>{postKindLabel(post)}</span>
      <span aria-hidden="true">·</span>
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
  const notes = blogPosts.filter((post) => post.kind === "note");
  const updates = blogPosts.filter((post) => post.kind === "update");
  const [featured, ...morePosts] = notes;

  return (
    <div className="blog-shell">
      <BlogHeader />
      <main className="blog-main">
        <section className="blog-index-hero">
          <div>
            <p className="blog-kicker"><BookOpenText /> Notes &amp; updates</p>
            <h1>Notes from inside the transfer.</h1>
          </div>
          <p>
            Plainspoken guides to the small code phrase, the relay in the
            middle, the encryption around it, and the useful ways a file gets
            from this computer to one person or a group, plus release updates
            when the software changes underneath it.
          </p>
        </section>

        <nav className="blog-kind-nav" aria-label="Blog post categories">
          <a href="#updates">
            <span>Updates</span>
            <strong>{String(updates.length).padStart(2, "0")}</strong>
            <small>What changed between releases</small>
          </a>
          <a href="#notes">
            <span>Notes</span>
            <strong>{String(notes.length).padStart(2, "0")}</strong>
            <small>Guides from inside the transfer</small>
          </a>
        </nav>

        <section id="updates" className="blog-kind-section" aria-labelledby="updates-title">
          <div className="blog-section-heading">
            <div>
              <p className="blog-kicker"><FileText /> Updates</p>
              <h2 id="updates-title">What changed in croc</h2>
            </div>
            <span>{String(updates.length).padStart(2, "0")} update</span>
          </div>
          <div className="blog-updates-list">
            {updates.map((post) => (
              <article className="featured-note update-note" key={post.slug}>
                <a
                  className="featured-note-visual"
                  href={`/blog/${post.slug}`}
                  aria-label={`Read ${post.title}`}
                >
                  <BlogVisualCard visual={post.visual} large />
                </a>
                <div className="featured-note-copy">
                  <span className="note-number">{postNumberLabel(post)}</span>
                  <PostMeta post={post} />
                  <h2><a href={`/blog/${post.slug}`}>{post.title}</a></h2>
                  <p>{post.description}</p>
                  <a className="blog-read-link" href={`/blog/${post.slug}`}>
                    Read the update <ArrowRight />
                  </a>
                </div>
              </article>
            ))}
          </div>
        </section>

        <section id="notes" className="blog-kind-section" aria-labelledby="notes-title">
          <div className="blog-section-heading">
            <div>
              <p className="blog-kicker"><BookOpenText /> Notes</p>
              <h2 id="notes-title">From inside the transfer</h2>
            </div>
            <span>{String(notes.length).padStart(2, "0")} notes</span>
          </div>

          <article className="featured-note">
            <a
              className="featured-note-visual"
              href={`/blog/${featured.slug}`}
              aria-label={`Read ${featured.title}`}
            >
              <BlogVisualCard visual={featured.visual} large />
            </a>
            <div className="featured-note-copy">
              <span className="note-number">{postNumberLabel(featured)}</span>
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
                <h2 id="more-notes-title">More ways into croc</h2>
              </div>
              <span>{String(morePosts.length).padStart(2, "0")} more notes</span>
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
        </section>

        <section className="blog-transfer-cta">
          <span><Check aria-hidden="true" /></span>
          <div>
            <p className="blog-kicker">Enough reading?</p>
            <h2>Move the file.</h2>
            <p>No account. No port forwarding. The browser is already a croc peer.</p>
          </div>
          <div className="blog-transfer-links">
            <a href="/#send-panel">Send files <ArrowRight /></a>
            <a href="/#receive">Receive files</a>
            <a href={site.projectUrl}>Install croc</a>
            <a href={site.repositoryUrl}>View source</a>
          </div>
        </section>
      </main>
      <TransferLinks />
      <BlogFooter />
    </div>
  );
}

function headingID(text: string, index: number) {
  const slug = text
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "");
  return slug || `section-${index}`;
}

function useActiveArticleSection(sectionIDs: string[]) {
  const sectionKey = sectionIDs.join("\n");
  const [activeSectionID, setActiveSectionID] = useState(sectionIDs[0] ?? "");

  useEffect(() => {
    if (sectionIDs.length === 0) return;

    const updateActiveSection = () => {
      const readingLine = Math.max(120, window.innerHeight * 0.25);
      let currentSectionID = sectionIDs[0];

      for (const sectionID of sectionIDs) {
        const heading = document.getElementById(sectionID);
        if (!heading || heading.getBoundingClientRect().top > readingLine) break;
        currentSectionID = sectionID;
      }

      setActiveSectionID(currentSectionID);
    };

    updateActiveSection();
    window.addEventListener("scroll", updateActiveSection, { passive: true });
    window.addEventListener("resize", updateActiveSection);

    return () => {
      window.removeEventListener("scroll", updateActiveSection);
      window.removeEventListener("resize", updateActiveSection);
    };
  }, [sectionKey]);

  return activeSectionID;
}

type TableIndicatorLevel = "full" | "partial" | "empty";

function parseTableIndicator(value: string) {
  const markers: Record<string, TableIndicatorLevel> = {
    "●": "full",
    "◐": "partial",
    "○": "empty",
  };
  const level = markers[value.charAt(0)];
  if (!level) return undefined;
  const note = value.slice(1).trim();
  return { level, note: note || undefined };
}

function TableStatusIndicator({
  label,
  level,
  note,
}: {
  label: string;
  level: TableIndicatorLevel;
  note?: string;
}) {
  const accessibleLabel = note ? `${label}; ${note}` : label;
  return (
    <span
      className={`blog-status-indicator is-${level}`}
      role="img"
      aria-label={accessibleLabel}
      title={accessibleLabel}
    >
      <Circle className="blog-status-outline" aria-hidden="true" />
      {level !== "empty" && (
        <Circle
          className="blog-status-fill"
          aria-hidden="true"
          fill="currentColor"
          strokeWidth={0}
        />
      )}
    </span>
  );
}

function BlogBlockView({ block, index }: { block: BlogBlock; index: number }) {
  if (block.type === "heading") {
    return <h2 id={headingID(block.text, index)}>{block.text}</h2>;
  }
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
  if (block.type === "table") {
    const indicatorColumns = new Set(block.indicatorColumns ?? []);
    const rowPositions = new Map(
      block.rowOrder?.map((tool, position) => [tool, position]),
    );
    const rows = rowPositions.size > 0
      ? [...block.rows].sort(
          (left, right) =>
            (rowPositions.get(left.cells[0]) ?? Number.MAX_SAFE_INTEGER) -
            (rowPositions.get(right.cells[0]) ?? Number.MAX_SAFE_INTEGER),
        )
      : block.rows;
    const tableID = `blog-table-${index}`;
    const qualifierNotes = rows.flatMap((row) =>
      [...indicatorColumns].flatMap((columnIndex) => {
        const indicator = parseTableIndicator(row.cells[columnIndex]);
        return indicator?.note
          ? [{
              column: block.headers[columnIndex],
              href: row.href,
              note: indicator.note,
              tool: row.cells[0],
            }]
          : [];
      }),
    );

    return (
      <div className="blog-table-block">
        <p className="blog-table-caption" id={`${tableID}-caption`}>
          {block.caption}
        </p>
        {block.indicatorLegend && (
          <div
            className="blog-table-legend"
            id={`${tableID}-legend`}
            role="note"
            aria-label={`${block.caption} legend`}
          >
            <div className="blog-table-status-key">
              <TableStatusIndicator
                level="full"
                label={`Full circle: ${block.indicatorLegend.full}`}
              />
              <span>{block.indicatorLegend.full}</span>
              <TableStatusIndicator
                level="partial"
                label={`Half circle: ${block.indicatorLegend.partial}`}
              />
              <span>{block.indicatorLegend.partial}</span>
              <TableStatusIndicator
                level="empty"
                label={`Empty circle: ${block.indicatorLegend.empty}`}
              />
              <span>{block.indicatorLegend.empty}</span>
            </div>
            {block.indicatorLegend.terms && (
              <dl className="blog-table-terms">
                {block.indicatorLegend.terms.map(({ term, definition }) => (
                  <div key={term}>
                    <dt>{term}</dt>
                    <dd>{definition}</dd>
                  </div>
                ))}
              </dl>
            )}
            {qualifierNotes.length > 0 && (
              <details className="blog-table-qualifiers">
                <summary>Caveats and qualifiers ({qualifierNotes.length})</summary>
                <ul>
                  {qualifierNotes.map(({ column, href, note, tool }) => (
                    <li key={`${tool}-${column}`}>
                      <a href={href}>{tool}</a>
                      <span>{column}: {note}</span>
                    </li>
                  ))}
                </ul>
              </details>
            )}
          </div>
        )}
        <div
          className="blog-table-scroll"
          role="region"
          aria-label={block.caption}
          tabIndex={0}
        >
          <table
            className="blog-comparison-table"
            aria-describedby={block.indicatorLegend ? `${tableID}-legend` : undefined}
          >
            <caption className="blog-visually-hidden">{block.caption}</caption>
            <colgroup>
              {block.headers.map((header, columnIndex) => (
                <col
                  className={indicatorColumns.has(columnIndex) ? "is-indicator-column" : undefined}
                  key={header}
                />
              ))}
            </colgroup>
            <thead>
              <tr>
                {block.headers.map((header, columnIndex) => (
                  <th
                    className={indicatorColumns.has(columnIndex) ? "is-indicator-column" : undefined}
                    scope="col"
                    key={header}
                  >
                    {header}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((row) => (
                <tr className={row.highlight ? "is-highlighted" : undefined} key={row.cells.join("\n")}>
                  {row.cells.map((cell, cellIndex) => {
                    if (cellIndex === 0) {
                      return (
                        <th scope="row" key={cell}>
                          <a href={row.href}>{cell}</a>
                        </th>
                      );
                    }

                    const indicator = indicatorColumns.has(cellIndex)
                      ? parseTableIndicator(cell)
                      : undefined;
                    if (indicator && block.indicatorLegend) {
                      const levelLabel = block.indicatorLegend[indicator.level];
                      return (
                        <td
                          className="is-indicator-column"
                          key={`${cellIndex}-${cell}`}
                          title={indicator.note}
                        >
                          <TableStatusIndicator
                            level={indicator.level}
                            label={`${block.headers[cellIndex]}: ${levelLabel}`}
                            note={indicator.note}
                          />
                        </td>
                      );
                    }

                    return <td key={`${cellIndex}-${cell}`}>{cell}</td>;
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    );
  }
  return <p>{block.text}</p>;
}

function BlogArticle({ post }: { post: BlogPost }) {
  useBlogMetadata(post);
  const index = blogPosts.findIndex((candidate) => candidate.slug === post.slug);
  const previous = index > 0 ? blogPosts[index - 1] : undefined;
  const next = index < blogPosts.length - 1 ? blogPosts[index + 1] : blogPosts[0];
  const sections = post.blocks.flatMap((block, blockIndex) =>
    block.type === "heading"
      ? [{ id: headingID(block.text, blockIndex), text: block.text }]
      : [],
  );
  const activeSectionID = useActiveArticleSection(
    sections.map((section) => section.id),
  );
  const relatedPosts = post.relatedSlugs.flatMap((slug) => {
    const related = getBlogPost(slug);
    return related ? [related] : [];
  });

  return (
    <div className="blog-shell">
      <BlogHeader />
      <main className="article-main">
        <a className="back-to-blog" href="/blog"><ArrowLeft /> All posts</a>
        <article className="blog-article">
          <header className="blog-article-header">
            <p className="blog-kicker">
              {post.kind === "update" ? <FileText /> : <ShieldCheck />}
              {postKindLabel(post)} · {post.category}
            </p>
            <span className="note-number">{postNumberLabel(post)}</span>
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
              <div className="article-rail-summary">
                <span><KeyRound /></span>
                <div>
                  <p>IN ONE SENTENCE</p>
                  <strong>{post.takeaway}</strong>
                </div>
              </div>
              <nav
                className="article-toc"
                aria-label={post.kind === "update" ? "In this update" : "In this field note"}
              >
                <p>{post.kind === "update" ? "IN THIS UPDATE" : "IN THIS NOTE"}</p>
                <ol>
                  {sections.map((section) => (
                    <li key={section.id}>
                      <a
                        href={`#${section.id}`}
                        aria-current={section.id === activeSectionID ? "location" : undefined}
                      >
                        {section.text}
                      </a>
                    </li>
                  ))}
                </ol>
              </nav>
            </aside>
            <div className="blog-article-body">
              {post.blocks.map((block, blockIndex) => (
                <BlogBlockView block={block} index={blockIndex} key={`${block.type}-${blockIndex}`} />
              ))}
              <section className="blog-related-notes" aria-labelledby={`related-${post.slug}`}>
                <p className="blog-kicker"><BookOpenText /> Related field notes</p>
                <h2 id={`related-${post.slug}`}>Keep following the transfer.</h2>
                <div>
                  {relatedPosts.map((related) => (
                    <a href={`/blog/${related.slug}`} key={related.slug}>
                      <span>{postNumberLabel(related)}</span>
                      <strong>{related.title}</strong>
                      <small>{related.description}</small>
                      <ArrowRight aria-hidden="true" />
                    </a>
                  ))}
                </div>
              </section>
              <section className="article-transfer-card">
                <div>
                  <p className="blog-kicker"><Upload /> Try the protocol</p>
                  <h2>Send something small.</h2>
                  <p>The quickest explanation is still a transfer between two devices.</p>
                </div>
                <div className="article-transfer-actions">
                  <a href="/#send-panel">Send files <ArrowRight /></a>
                  <a href="/#receive">Receive files</a>
                  <a href={site.projectUrl}>Install croc</a>
                  <a href={site.repositoryUrl}>Browse source</a>
                </div>
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
      <TransferLinks />
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
        <p>The transfer is fine. This address is not one of the published field notes.</p>
        <a href="/blog">Return to all notes <ArrowRight /></a>
      </main>
      <TransferLinks />
      <BlogFooter />
    </div>
  );
}

export function Blog({ slug }: { slug?: string }) {
  if (!slug) return <BlogIndex />;
  const post = getBlogPost(slug);
  return post ? <BlogArticle post={post} /> : <BlogNotFound slug={slug} />;
}
