import blogSEO from "./blog-seo.json";

export type BlogVisual =
  | "overview"
  | "relay"
  | "code"
  | "handshake"
  | "browser"
  | "bridge"
  | "stored"
  | "release";

export type BlogKind = "note" | "update";

export type BlogBlock =
  | { type: "heading"; text: string }
  | { type: "paragraph"; text: string }
  | { type: "list"; items: string[] }
  | { type: "code"; label: string; lines: string[] }
  | { type: "aside"; eyebrow: string; title: string; text: string }
  | {
      type: "table";
      caption: string;
      headers: string[];
      indicatorColumns?: number[];
      indicatorLegend?: {
        full: string;
        partial: string;
        empty: string;
        terms?: Array<{ term: string; definition: string }>;
      };
      rowOrder?: string[];
      rows: Array<{
        cells: string[];
        href: string;
        highlight?: boolean;
      }>;
    };

export type BlogPost = {
  slug: string;
  number: string;
  title: string;
  seoTitle: string;
  description: string;
  kind: BlogKind;
  category: string;
  publishedAt: string;
  modifiedAt: string;
  publishedLabel: string;
  author: string;
  keywords: string[];
  image: string;
  socialImage: string;
  imageAlt: string;
  visual: BlogVisual;
  takeaway: string;
  wordCount: number;
  readingMinutes: number;
  relatedSlugs: string[];
  blocks: BlogBlock[];
};

type DraftBlogPost = Omit<
  BlogPost,
  "seoTitle" | "modifiedAt" | "keywords" | "image" | "socialImage" | "imageAlt" | "wordCount" | "readingMinutes" | "relatedSlugs" | "kind"
> & { kind?: BlogKind };

const wordsPerMinute = 210;

const fileTransferToolOrder = [
  "croc",
  "MEGA",
  "Filemail",
  "Syncthing",
  "WebWormhole",
  "SwissTransfer",
  "transfer.sh",
  "TransferNow",
  "WeTransfer",
  "iroh Sendme",
  "Tailscale Taildrop",
  "PairDrop",
  "Send Anywhere",
  "LocalSend",
  "KDE Connect",
  "Magic Wormhole",
  "qrcp",
  "OnionShare",
  "FilePizza",
  "ToffeeShare",
  "Wormhole.app",
  "Warpinator",
  "pCloud Transfer",
  "rclone",
  "Smash",
  "Firefox Send",
  "Apple AirDrop",
  "Portal",
  "rsync",
  "Blaze",
  "LANDrop",
  "Google Quick Share",
  "ZeroTier Toss",
  "Peer Copy",
  "Snapdrop Classic",
  "ShareDrop Classic",
];

export function blogWordCount(blocks: BlogBlock[]) {
  return blocks.flatMap((block) => {
    if (block.type === "list") return block.items;
    if (block.type === "code") return block.lines;
    if (block.type === "aside") {
      return [block.eyebrow, block.title, block.text];
    }
    if (block.type === "table") {
      return [
        block.caption,
        ...block.headers,
        ...(block.indicatorLegend
          ? [
              block.indicatorLegend.full,
              block.indicatorLegend.partial,
              block.indicatorLegend.empty,
              ...(block.indicatorLegend.terms?.flatMap(({ term, definition }) => [
                term,
                definition,
              ]) ?? []),
            ]
          : []),
        ...block.rows.flatMap((row) => row.cells),
      ];
    }
    return [block.text];
  }).join(" ").trim().split(/\s+/).filter(Boolean).length;
}

export function readingMinutes(blocks: BlogBlock[]) {
  return Math.max(1, Math.ceil(blogWordCount(blocks) / wordsPerMinute));
}

const drafts: DraftBlogPost[] = [
  {
    slug: "croc-v11-release-update",
    number: "01",
    title: "From croc v10 to v11",
    description:
      "croc v11 makes the handshake harder to confuse, lets one encrypted stored transfer serve a group, starts the web client faster, and closes several unsafe edge cases.",
    kind: "update",
    category: "Release notes",
    publishedAt: "2026-08-16",
    publishedLabel: "August 16, 2026",
    author: "schollz",
    visual: "release",
    takeaway:
      "v11 combines a stronger peer handshake, safer relay and stored-transfer limits, multi-recipient stored sends, faster browser startup, and clearer terminal output.",
    blocks: [
      {
        type: "paragraph",
        text: "croc v11 did not arrive as one enormous switch. The useful version is the accumulation of v11.0.0 through v11.1.1: a hardened peer handshake, tighter relay limits, multi-recipient stored transfers, a privacy choice in the browser, quicker code generation, and a long list of fixes around the edges. This is the combined update for anyone coming from v10.",
      },
      {
        type: "paragraph",
        text: "The ordinary workflow is still deliberately small. Send a file, share the three-word code, and let the other person receive it from a terminal or browser. Most of the v11 work is underneath that workflow, making the rendezvous safer and making unusual network or storage conditions fail more cleanly.",
      },
      { type: "heading", text: "The short version" },
      {
        type: "list",
        items: [
          "Peer handshakes are bound to the sender, receiver, room, purpose, protocol version, curve, salt, and exact transcript, with separate confirmation values for both roles.",
          "Relay operators can cap waiting rooms and pending handshakes so idle or deliberately incomplete connections cannot grow server state without a bound.",
          "Stored mode can allow several verified downloads and a sender-chosen finite expiration instead of always stopping after one receiver or one day.",
          "The web client asks before loading optional analytics and now generates a secure three-word code immediately, without waiting for the WebAssembly transfer engine.",
          "Terminal output uses restrained, TTY-aware color for codes, filenames, progress, warnings, and success while respecting NO_COLOR and redirected output.",
        ],
      },
      {
        type: "aside",
        eyebrow: "THE COMPATIBLE PART",
        title: "Old-looking codes still have a route forward",
        text: "Current v11 builds generate three words from the EFF short wordlist. They also retain parsing support for the four-word format used by the first v11 builds and for legacy custom codes.",
      },
      { type: "heading", text: "A handshake tied to this transfer" },
      {
        type: "paragraph",
        text: "The largest security change is the PAKE key schedule. v11 gives the receiver and sender ordered identities, then binds the derived traffic key and confirmation tags to the transfer purpose, room, curve, both PAKE messages, protocol version, and a fresh salt. A message copied from another role, room, purpose, or session no longer belongs to the same authenticated transcript.",
      },
      {
        type: "paragraph",
        text: "Both peers also prove that they derived the expected result before croc treats the channel as ready. An unsupported version, changed transcript, swapped role, invalid point, or wrong code fails closed instead of continuing with a channel that only looks related to the intended exchange.",
      },
      { type: "heading", text: "Relay rooms now have hard edges" },
      {
        type: "paragraph",
        text: "v11 limits a relay to 128 single-occupant waiting rooms per port by default. A self-hosted relay can choose another positive value with --max-rooms-open or CROC_MAX_ROOMS_OPEN. The server also caps pending handshakes, gives them a five-minute deadline, and cleans up stale deadlines correctly. That keeps a slow or abandoned connection from occupying an open-ended amount of relay state.",
      },
      {
        type: "paragraph",
        text: "Explicit loopback binding is respected again. If an operator asks a relay to listen on 127.0.0.1, croc no longer silently rewrites that address to 0.0.0.0. The generated code format also gives room selection more entropy than the old four-byte prefix: current three-word codes use the first complete EFF word to select the relay room and the final two words for PAKE.",
      },
      { type: "heading", text: "One stored upload can serve a group" },
      {
        type: "paragraph",
        text: "v11.1.0 turns stored mode into a practical group handoff. The sender chooses a verified-download allowance and a finite lifetime. Each recipient gets the same encrypted browser link or CLI token, but an allowance is spent only after a client authenticates every chunk, verifies the completed files, and commits the receive. Opening a link or abandoning a partial download does not consume one.",
      },
      {
        type: "code",
        label: "Five verified downloads, available for three days",
        lines: [
          "$ croc send --store \\",
          "    --store-downloads 5 \\",
          "    --store-expiration 3d \\",
          "    release-notes.pdf",
        ],
      },
      {
        type: "paragraph",
        text: "The storage service can enforce lower maxima for download count and expiration. Its per-transfer locking is now a fixed set of stripes instead of a map that could grow for attacker-chosen IDs. Stored progress also clears old terminal text by display width, including Unicode filenames, and v11.1.1 brings stored-transfer colors in line with the rest of croc.",
      },
      { type: "heading", text: "Safer failure paths" },
      {
        type: "list",
        items: [
          "Decompression has explicit output limits, so a small compressed network message cannot expand without a bound in either the CLI or web path.",
          "Temporary-file cleanup is kept in memory and accepts only local paths, preventing a received cleanup marker from naming arbitrary files for deletion.",
          "Idle relay handshakes time out, pending handshakes are bounded, and connection deadlines are applied for the entire read instead of being cleared too early.",
          "Stored-transfer lock state stays bounded even when requests contain many different transfer IDs.",
        ],
      },
      { type: "heading", text: "The web client starts cleaner" },
      {
        type: "paragraph",
        text: "The browser now shows an explicit privacy choice before optional Umami analytics can load. Rejecting analytics does not change transfers. If enabled, the configuration excludes codes, link keys, query strings, filenames, and relay settings; the choice remains available from the footer.",
      },
      {
        type: "paragraph",
        text: "In v11.1.1 the send panel can make its three-word EFF code immediately with Web Crypto instead of booting WebAssembly just to choose a phrase. The larger transfer engine waits until it is actually needed and can use streaming WebAssembly instantiation when the server provides the correct content type. The result is a usable code field sooner, especially on a cold mobile load.",
      },
      { type: "heading", text: "A calmer terminal" },
      {
        type: "paragraph",
        text: "v11 adds semantic color without making logs noisy: active progress is cyan, completed work is green, codes and warnings are yellow, errors are red, and filenames are bold. Styling is enabled only for a real terminal, works on Windows terminals, turns off for TERM=dumb or NO_COLOR, and stays out of redirected output. Follow-up fixes cover stored-transfer progress, Unicode display width, and the final success render.",
      },
      { type: "heading", text: "What changed in each v11 release" },
      {
        type: "table",
        caption: "croc v11 release sequence",
        headers: ["Release", "Published", "What it brought"],
        rows: [
          {
            cells: ["v11.0.0", "July 31, 2026", "PAKE transcript binding, safer relay limits, stronger room selection, terminal color, and deployment and app-directory updates"],
            href: "https://github.com/schollz/croc/releases/tag/v11.0.0",
          },
          {
            cells: ["v11.0.1", "July 31, 2026", "Loopback binding, idle connection, deadline, and decompression-limit fixes"],
            href: "https://github.com/schollz/croc/releases/tag/v11.0.1",
          },
          {
            cells: ["v11.0.2", "August 6, 2026", "Optional-analytics privacy controls, favicon and GUI links, dependency maintenance, and the temporary relay host change"],
            href: "https://github.com/schollz/croc/releases/tag/v11.0.2",
          },
          {
            cells: ["v11.0.3", "August 9, 2026", "Safe in-memory temporary-file cleanup and CI dependency maintenance"],
            href: "https://github.com/schollz/croc/releases/tag/v11.0.3",
          },
          {
            cells: ["v11.1.0", "August 11, 2026", "Multi-recipient stored transfers, sender-selected expiration, bounded storage locks, and cleaner --store rendering"],
            href: "https://github.com/schollz/croc/releases/tag/v11.1.0",
          },
          {
            cells: ["v11.1.1", "August 15, 2026", "Faster web phrase generation, corrected terminal color, the /v11 module upgrade, build documentation, and test fixes"],
            href: "https://github.com/schollz/croc/releases/tag/v11.1.1",
          },
        ],
      },
      { type: "heading", text: "Smaller changes that still matter" },
      {
        type: "paragraph",
        text: "The release series also refreshed the app directory with Croc GUI, Swamp, croc-desktop, and F-Droid details; added a favicon; updated the default discovery and temporary relay host; moved the Go module to github.com/schollz/croc/v11; corrected the source-build requirements; and advanced the GitHub Actions dependencies used for Node setup and container login. Tests were repaired alongside the protocol, code-phrase, web, and build changes.",
      },
      {
        type: "aside",
        eyebrow: "THE RESULT",
        title: "The command stayed small",
        text: "The visible upgrade is a faster code, clearer progress, and a stored link that can serve a group. The deeper upgrade is that relay, handshake, decompression, cleanup, and storage state now have sharper boundaries.",
      },
    ],
  },
  {
    slug: "why-croc-works-this-way",
    number: "01",
    title: "Why croc works this way",
    description:
      "croc started with a large documentary, a friend in another country, and the surprisingly difficult question of how to send her the file.",
    category: "Origins",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "overview",
    takeaway:
      "croc uses a live relay and PAKE so two ordinary computers can make an encrypted transfer without first turning either computer into a server.",
    blocks: [
      {
        type: "paragraph",
        text: "I did not start croc because the world was missing file-transfer programs. There were already plenty. I started it because I had a large PBS documentary about turkeys and wanted to send it to a friend in another country. She used Windows, did not enjoy the command line, and quite reasonably did not want an afternoon project before watching a movie.",
      },
      {
        type: "paragraph",
        text: "I tried the usual answers. I could upload the whole thing somewhere and send a link. I could explain SSH, user accounts, IP addresses, and port forwarding. I could mail a USB drive. Every method made one side of the transfer easy by making the other side slow, insecure, or absurd. croc grew out of wanting the boring answer: run one program, read a short code, and move the file.",
      },
      { type: "heading", text: "The file should start moving now" },
      {
        type: "paragraph",
        text: "Uploading to storage makes a file take two trips. The sender uploads every byte, waits, and then the receiver downloads every byte. If those two parts take ten minutes and six minutes, the file has spent at least sixteen minutes traveling even though both computers may have been ready the whole time.",
      },
      {
        type: "paragraph",
        text: "A croc relay lets the two sides overlap. While the sender is reading one part of the file, the receiver can already be writing an earlier part. The slower connection still wins (physics remains undefeated), but there is no complete upload sitting in the middle waiting for permission to become a download.",
      },
      {
        type: "aside",
        eyebrow: "THE IMPORTANT PART",
        title: "The relay is a pipe, not a shelf",
        text: "In a normal croc transfer both computers are present and encrypted bytes pass through in real time. Stored mode is a separate option for the times when that is impossible.",
      },
      { type: "heading", text: "A short code should stay short" },
      {
        type: "paragraph",
        text: "The code also had to be something I could tell another person without reading a small novel of random characters. Three words are manageable. Three words are not, by themselves, the sort of key I want encrypting gigabytes of data. Making the code fifty characters long would help the cryptography and ruin the interface.",
      },
      {
        type: "paragraph",
        text: "This is why croc uses password-authenticated key exchange, or PAKE. The two computers begin with the same little phrase, exchange public messages, and independently make the same strong session key. The phrase is never used directly to encrypt the file, and the finished key is never sent through the relay. PAKE lets the human part remain human-sized.",
      },
      { type: "heading", text: "Nobody should have to touch the router" },
      {
        type: "paragraph",
        text: "SSH is wonderful when there is already an SSH server. The words ‘when there is already’ are doing most of the work in that sentence. Two laptops in two homes are usually behind routers and firewalls. One of them may be on hotel Wi-Fi or a company network. Explaining how to open a port is a very strange prerequisite for sending a photograph.",
      },
      {
        type: "paragraph",
        text: "Instead, both croc clients make an ordinary outbound connection to the relay. The relay matches them and forwards their encrypted traffic. Neither side needs an account on the other computer, its public address, or administrative access to a router. This is less clever than defeating every possible network and much more useful.",
      },
      { type: "heading", text: "One protocol, several ways in" },
      {
        type: "paragraph",
        text: "The first croc interface was one command. Years later I added the browser client for almost the same reason I started the project: the person receiving a file should not need to become a different sort of computer user. One side can use a terminal and the other can use a browser. They only have to agree on the code.",
      },
      {
        type: "list",
        items: [
          "Two computers can meet through a relay without port forwarding.",
          "PAKE turns the shared phrase into a fresh, strong session key.",
          "Files, names, and metadata are encrypted between the endpoints.",
          "The CLI and browser can participate in the same transfer.",
          "Multiple files can travel together without first becoming an account or a permanent link.",
        ],
      },
      { type: "heading", text: "The command stayed small" },
      {
        type: "paragraph",
        text: "None of these pieces was invented for croc. Relays existed. PAKE existed. Go could already build programs for several operating systems. Most of the work was in arranging the pieces so the complicated part happened after the person typed the simple part.",
      },
      {
        type: "code",
        label: "The whole interface",
        lines: [
          "$ croc send sketchbook.zip",
          "Code is: river-cloud-daisy",
          "",
          "$ croc river-cloud-daisy",
        ],
      },
      {
        type: "paragraph",
        text: "That is still more or less my test for croc. If sending one file begins to feel like configuring a file-transfer system, something has gone wrong. I want to choose the file, tell my friend three words, and get back to the turkey documentary.",
      },
    ],
  },
  {
    slug: "how-croc-moves-a-file",
    number: "02",
    title: "The relay is not a waiting room",
    description:
      "The croc relay looks a little like an upload server, except it does not wait for the whole file before sending it onward.",
    category: "How it works",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "relay",
    takeaway:
      "The relay matches two computers and forwards encrypted bytes between them; it does not need to understand the file.",
    blocks: [
      {
        type: "paragraph",
        text: "The word relay makes croc sound a little like an upload service. Bytes leave my computer, pass through somebody else's server, and arrive at another computer. That description is true, but it misses the part I care about: the relay does not wait for the whole file.",
      },
      {
        type: "paragraph",
        text: "With ordinary cloud sharing I upload the complete file, copy a link, and then the other person downloads the complete file. croc connects both computers at once. A block can leave the sender, pass through the relay, and reach the receiver while the sender is already working on the next block. It is streaming through the server, not checking into the server for the night.",
      },
      { type: "heading", text: "Why bother doing both halves at once?" },
      {
        type: "paragraph",
        text: "Suppose I can upload at 20 megabits per second and the receiver can download at 80. The transfer cannot outrun my 20 megabit connection. Relaying does not perform magic on the slow side. It simply avoids spending one complete file filling storage and another complete file emptying it. The receiver can start before I finish.",
      },
      {
        type: "aside",
        eyebrow: "THE CATCH",
        title: "Both computers have to be there",
        text: "A direct transfer is a live handoff. Keep both croc clients open until the receiver verifies the files. If the schedules do not overlap, that is what stored mode is for.",
      },
      { type: "heading", text: "The relay also solves the router problem" },
      {
        type: "paragraph",
        text: "Most computers cannot accept a surprise connection from the public internet. They are behind home routers, office firewalls, café Wi-Fi, or mobile networks. This is generally a good thing. It is also why ‘just connect directly’ becomes several pages of instructions about NAT and port forwarding.",
      },
      {
        type: "paragraph",
        text: "Both croc clients can, however, make an outbound connection to a relay they already know how to reach. The relay puts the matching connections together. It can observe mundane network facts, such as when the clients connect and how many encrypted bytes pass through, but the file names, metadata, and contents are encrypted with a key it does not have.",
      },
      {
        type: "code",
        label: "A complete direct transfer",
        lines: [
          "$ croc send field-notes.pdf",
          "Code is: river-cloud-daisy",
          "",
          "$ croc",
          "Enter receive code: river-cloud-daisy",
        ],
      },
      { type: "heading", text: "The browser is another croc peer" },
      {
        type: "paragraph",
        text: "The browser version uses the same arrangement. croc's protocol code runs as WebAssembly, and a same-origin WebSocket bridge carries its connections to the relay. I could have made a separate browser upload service, but then it would be a separate browser upload service. Keeping the protocol the same means a browser can send to the command line, the command line can send to a browser, and neither side needs to know which interface the other picked.",
      },
    ],
  },
  {
    slug: "what-four-word-code-does",
    number: "03",
    title: "What the three words are doing",
    description:
      "I wanted a croc code to be easy to read aloud. Making three ordinary words secure enough for a file transfer takes a little more work.",
    category: "Security",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "code",
    takeaway:
      "The words are a shared secret for PAKE, which lets both computers produce a fresh session key without sending that key to each other.",
    blocks: [
      {
        type: "paragraph",
        text: "A croc code looks almost too friendly for cryptography. It is three ordinary words with hyphens between them. I chose words because they survive being copied into a message, read over the phone, or typed on a tiny keyboard much better than a long string of punctuation. You can also notice when somebody only sent two of them.",
      },
      {
        type: "paragraph",
        text: "The code has two jobs. It helps the clients meet in the same transfer, and it gives them a small secret that both people know. What it does not do is directly encrypt the file. Three words would be carrying far too much weight if that were the complete scheme.",
      },
      { type: "heading", text: "The words start the exchange" },
      {
        type: "paragraph",
        text: "croc uses password-authenticated key exchange, usually shortened to PAKE. Each computer combines the phrase with fresh private randomness, then the two sides trade carefully constructed public messages. If they began with the same phrase, they finish with the same strong session key. The finished key is new for this transfer and never crosses the network.",
      },
      {
        type: "aside",
        eyebrow: "THE SHORT VERSION",
        title: "Same words, same key",
        text: "Matching phrases let the two computers produce matching keys. A wrong phrase produces the wrong key, and croc stops before treating the connection as secure.",
      },
      {
        type: "paragraph",
        text: "This is the useful division: the words are for the people and the derived key is for the computers. Somebody recording the public exchange at the relay does not get a convenient test for guessing the phrase later, and does not learn the session key from the messages going by.",
      },
      { type: "heading", text: "It is still a live secret" },
      {
        type: "paragraph",
        text: "PAKE is not a spell that makes the code safe to publish. If somebody gets the live words before the intended receiver, they can try to join the transfer themselves. I share a croc code the same way I would share the file: privately if the file is private, and with the person I actually mean to send it to.",
      },
      {
        type: "list",
        items: [
          "Copy all three words, including their order.",
          "Treat a QR code as another representation of the same secret.",
          "Do not post a live code in a public room for a private file.",
          "Let croc generate a fresh phrase unless there is a good reason to choose one.",
        ],
      },
      { type: "heading", text: "Why not use a longer code?" },
      {
        type: "paragraph",
        text: "A long random string would contain more raw entropy. It would also be annoying to dictate, easy to truncate, and tempting to replace with something memorable. I do not want people inventing a permanent croc password because the generated one is impossible to move between devices. The protocol exists so the visible code can remain small without pretending that the code is a full encryption key.",
      },
      {
        type: "paragraph",
        text: "So the three words are doing more than naming a room, and less than encrypting the file. They are the small thing the two people move. PAKE uses that small thing to make the large secret the computers actually need.",
      },
    ],
  },
  {
    slug: "pake-step-by-step",
    number: "04",
    title: "PAKE, step by step",
    description:
      "PAKE is the part of croc that turns a small shared phrase into a strong key. Here is the exchange without skipping the interesting bits.",
    category: "Security",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "handshake",
    takeaway:
      "PAKE lets both peers derive the same strong secret without putting either the croc phrase or the resulting secret on the network.",
    blocks: [
      {
        type: "paragraph",
        text: "I have used PAKE in croc for years, and ‘password-authenticated key exchange’ still sounds more forbidding than the problem it solves. Two people know the same short phrase. Their computers need the same strong encryption key. They need to make that key without sending the phrase or the key to each other.",
      },
      {
        type: "paragraph",
        text: "PAKE solves this with a short conversation. Both sides mix the phrase with private random values, exchange two public curve points, and independently arrive at the same secret point. The private random values disappear after the exchange. The phrase and the shared point never go over the wire. That is the whole trick; the rest of this post is the accounting that makes the trick trustworthy.",
      },
      { type: "heading", text: "What I want PAKE to guarantee" },
      {
        type: "paragraph",
        text: "The phrase should not turn into one fixed key that reappears every time somebody reuses the words. Fresh randomness makes each result belong to one exchange. Recording the public messages should not reveal the phrase, the session key, or a simple offline test for trying guesses later. An ordinary password-encrypted file does not give us all of that.",
      },
      {
        type: "aside",
        eyebrow: "THE PART TO REMEMBER",
        title: "The phrase is not the file key",
        text: "The phrase authenticates the exchange. PAKE produces a new shared secret, and croc derives a traffic key plus separate confirmation values from it.",
      },
      { type: "heading", text: "1. The receiver makes X" },
      {
        type: "paragraph",
        text: "The receiver goes first and is called party A in the code. It chooses a fresh 32-byte random value, a. It combines that randomness with the curve's base point G, then masks the result with the password and a fixed point named U. The public result is X.",
      },
      {
        type: "code",
        label: "Message one, conceptually",
        lines: [
          "A chooses a fresh random value a",
          "X = U·password + G·a",
          "A → B: X",
        ],
      },
      {
        type: "paragraph",
        text: "Only X is serialized. The password, a, the intermediate points, and the eventual key remain inside the PAKE participant. When the sender receives X it first checks that X is actually a valid point on the selected curve. Cryptographic input deserves less trust than ordinary input, not more.",
      },
      {
        type: "aside",
        eyebrow: "BORING ON PURPOSE",
        title: "U and V are fixed",
        text: "The library hard-codes valid points U and V. Letting applications invent convenient points could add a trapdoor if somebody knew their discrete logarithms.",
      },
      { type: "heading", text: "2. The sender answers with Y" },
      {
        type: "paragraph",
        text: "The sender is party B and does the mirror image. It chooses a fresh b and uses the other fixed point, V, to produce Y. Because B knows the phrase, it can remove A's password mask from X and multiply what remains by b. This produces the shared point Z.",
      },
      {
        type: "code",
        label: "Message two, conceptually",
        lines: [
          "B chooses a fresh random value b",
          "Y = V·password + G·b",
          "Zᴮ = b·(X − U·password) = G·a·b",
          "B → A: Y + a fresh salt",
        ],
      },
      { type: "heading", text: "3. The receiver reaches the same point" },
      {
        type: "paragraph",
        text: "A checks Y, removes the V-based password mask, and multiplies what remains by a. If the phrases match, A arrives at the same G·a·b point as B. Neither computer sent a, b, or Z. If the phrases are different, the masks do not cancel and the two sides quietly end up with different material.",
      },
      {
        type: "code",
        label: "The same destination from the other side",
        lines: ["Zᴬ = a·(Y − V·password) = G·a·b", "Zᴬ = Zᴮ"],
      },
      { type: "heading", text: "4. croc records what just happened" },
      {
        type: "paragraph",
        text: "At this point the PAKE library can produce a 32-byte shared secret from the password, X, Y, and Z. I do not use that value immediately. croc also records who was A, who was B, which room they used, and what the exchange was for. Otherwise a valid exchange could be lifted out of one conversation and dropped into another one where it did not belong.",
      },
      {
        type: "paragraph",
        text: "croc feeds the shared secret, a fresh 32-byte salt, and a framed transcript into a key-derivation step. The transcript includes the protocol version, purpose, room, curve, both identities, and the exact X and Y messages. The result is split into a traffic-encryption key and a different confirmation value for each role. I prefer separate keys for separate jobs; it makes fewer assumptions about how one value might be reused.",
      },
      {
        type: "list",
        items: [
          "The room prevents the same phrase from meaning the same session everywhere.",
          "Ordered sender and receiver identities prevent the roles from being swapped unnoticed.",
          "The exact wire transcript makes both peers agree on what was exchanged.",
          "A fresh salt gives the final key schedule its own randomness and context.",
          "Separate confirmation values keep proving the key apart from encrypting files.",
        ],
      },
      { type: "heading", text: "5. Both sides check their work" },
      {
        type: "paragraph",
        text: "Having a key is not enough if neither side knows whether the other side has the same one. The receiver sends its confirmation tag. The sender checks it in constant time and answers with the sender-specific tag. The receiver checks that response. croc opens the encrypted file channels only after both checks pass.",
      },
      {
        type: "aside",
        eyebrow: "NO MAYBE",
        title: "A failed check ends the transfer",
        text: "A wrong phrase, changed transcript, swapped role, invalid point, unsupported version, duplicate message, or bad confirmation stops the handshake before croc calls the channel secure.",
      },
      { type: "heading", text: "What the relay can see" },
      {
        type: "paragraph",
        text: "The relay carries X, Y, the curve choice, the salt, and the confirmation messages. It also sees ordinary network facts such as timing and byte volume. It never receives the croc phrase, a, b, Z, the final traffic key, or readable file metadata and contents. I think it is useful to say exactly what a server can see instead of reducing all of this to a lock icon.",
      },
      { type: "heading", text: "PAKE does not make the code public" },
      {
        type: "paragraph",
        text: "PAKE protects a small shared secret during an active exchange. It does not make that secret public. Somebody who gets the live code before the intended receiver can try to become the receiver. It also cannot fix a compromised computer or pull a code back out of the wrong conversation. Cryptography is useful, but it does not get to edit the surrounding world.",
      },
      {
        type: "list",
        items: [
          "Share the live croc code through a channel appropriate for the file.",
          "Use a newly generated code for a new transfer.",
          "Stop if croc reports that PAKE or key confirmation failed.",
          "Remember that a QR code contains the same secret as the written words.",
        ],
      },
      {
        type: "paragraph",
        text: "On screen this all becomes three words and a progress bar. Underneath are two curve points, two random values, a transcript, a key schedule, and two confirmation checks. That is quite a lot of machinery for a short code, but it lets the short code remain short. That was the problem in the first place.",
      },
    ],
  },
  {
    slug: "send-file-from-browser",
    number: "05",
    title: "Send a file without installing anything",
    description:
      "The browser can join an ordinary croc transfer now. Choose the files, share the code, and keep the tab open until the handoff finishes.",
    category: "Start here",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "browser",
    takeaway:
      "A direct browser transfer is live: choose the files, share the generated code, and leave the tab open until the receiver verifies them.",
    blocks: [
      {
        type: "paragraph",
        text: "For a long time, croc's answer to ‘how do I receive this without installing croc?’ was, unfortunately, ‘install croc.’ The executable is small, but that is beside the point when somebody only wants one photograph or is holding a phone. The browser client gives that person a way into the same transfer without setting anything up first.",
      },
      { type: "heading", text: "1. Pick Send and choose the files" },
      {
        type: "paragraph",
        text: "Select one file or several. The page shows the count and total size before the transfer begins. Choosing a file does not secretly upload it somewhere; in direct mode, bytes start moving when you press Send and a receiver joins with the code.",
      },
      { type: "heading", text: "2. Send the code to the other person" },
      {
        type: "paragraph",
        text: "croc makes a three-word code. Copy it into a message, or show the QR code if the other device is close enough to scan it. I added the QR version mostly for laptop-to-phone transfers because typing three hyphenated words on a phone is not difficult, but scanning them is nicer. The code is a live secret, so send it privately when the file is private.",
      },
      {
        type: "aside",
        eyebrow: "ONE EASY MISTAKE",
        title: "Leave the tab open",
        text: "The browser is actually sending the file. Closing the tab closes one end of the transfer, so wait for the completed and verified message.",
      },
      { type: "heading", text: "3. The receiver gets to look first" },
      {
        type: "paragraph",
        text: "The receiver chooses Receive, enters the code, and sees the file names and sizes before accepting anything. Some browsers can ask for a destination folder. Others have to use the normal download system. I would prefer one beautiful file API everywhere, but browser support is what it is, so the page uses the best path available and falls back when it has to.",
      },
      {
        type: "list",
        items: [
          "Keep both pages awake until the progress bar completes.",
          "Treat the rate and ETA as estimates; the network has not signed a contract.",
          "If the browser blocks a destination choice, allow the prompt or use the ordinary downloads fallback.",
          "For folders, exclusions, pipes, and resumable CLI workflows, use the command-line client.",
        ],
      },
      { type: "heading", text: "What stays on the device" },
      {
        type: "paragraph",
        text: "I compiled the security-sensitive croc protocol code to WebAssembly instead of rewriting a look-alike protocol in JavaScript. File metadata and contents are encrypted before the relay carries them. The relay handles the connections and can observe timing and byte volume, but it is not handed a readable copy of the selected files.",
      },
      {
        type: "paragraph",
        text: "The finished workflow is not very dramatic: choose, share, review, receive. That is exactly what I wanted. There is plenty of drama in browsers, networks, and cryptography already; none of it needs to happen to the person trying to send a photograph.",
      },
    ],
  },
  {
    slug: "browser-meets-terminal",
    number: "06",
    title: "A browser and a terminal can share the same file",
    description:
      "The browser client was meant to join normal croc transfers, not become another file-sharing island with its own links and accounts.",
    category: "Field guide",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "bridge",
    takeaway:
      "The browser and command line speak the same croc protocol, so each person can use the interface that suits their device.",
    blocks: [
      {
        type: "paragraph",
        text: "The feature I wanted most from the browser client was not browser-to-browser transfer. It was browser-to-terminal transfer. There are already many websites that can send a file to the same website. I wanted the web page to join the croc that people were already running.",
      },
      {
        type: "paragraph",
        text: "A terminal is the quickest interface when it is already open. A browser is the quickest interface when installing a program would become the entire task. There is no good reason the person on one end should have to copy the habits of the person on the other end.",
      },
      { type: "heading", text: "Terminal to browser" },
      {
        type: "paragraph",
        text: "On the sending computer, run croc with a file or folder. The command prints a code. The other person opens this website, chooses Receive, pastes the code, reviews the offer, and saves it. Adding the QR option gives them a receive link they can scan instead, which is the route I use when the receiving device is a phone.",
      },
      {
        type: "code",
        label: "Send from the CLI",
        lines: [
          "$ croc send --qr photos.zip",
          "Sending 'photos.zip' (842 MB)",
          "Code is: flame-ferry-tiger",
        ],
      },
      { type: "heading", text: "Browser to terminal" },
      {
        type: "paragraph",
        text: "The other direction works the same way. Choose files in the browser and copy its code. On the receiving computer, run croc with no arguments and paste the phrase at the prompt. I prefer the prompt to putting the secret directly in the command because command arguments can end up in shell history or a process list.",
      },
      {
        type: "code",
        label: "Receive without exposing the phrase as an argument",
        lines: ["$ croc", "Enter receive code: flame-ferry-tiger"],
      },
      {
        type: "aside",
        eyebrow: "THIS WAS THE POINT",
        title: "It is one croc transfer",
        text: "The web client is not a separate sharing service. Its protocol code comes from the croc codebase and is compiled for the browser, so the file can cross the interface boundary without changing systems.",
      },
      { type: "heading", text: "The two interfaces are not identical" },
      {
        type: "paragraph",
        text: "I have not tried to pretend the browser and command line are equally good at everything. The browser is better for a no-install handoff, a QR-assisted phone receive, and a visible review step. The CLI is better for folders, exclusions, scripts, pipes, custom relays, and long-running jobs. Compatibility lets each side keep the useful parts of its own interface.",
      },
      {
        type: "list",
        items: [
          "Send several files from the browser to a normal croc CLI receiver.",
          "Send folders from the CLI and receive them into a chosen browser directory when supported.",
          "Use the same private relay settings on both sides when self-hosting.",
          "Move to the CLI when automation or resume behavior matters more than a graphical flow.",
        ],
      },
      {
        type: "paragraph",
        text: "I used to think cross-platform mostly meant producing Windows, macOS, and Linux binaries. It also means the person on the other end may have a different device, different software, and no interest in learning my preferred interface. Four shared words are enough common ground.",
      },
    ],
  },
  {
    slug: "stored-transfer-one-download",
    number: "07",
    title: "When both computers cannot be online at once",
    description:
      "Direct croc transfers expect both computers to be awake. Stored mode is for the inconvenient times when the people are not.",
    category: "Stored transfers",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "stored",
    takeaway:
      "Stored mode encrypts locally and keeps only ciphertext until its verified-download limit or sender-selected lifetime, whichever happens first.",
    blocks: [
      {
        type: "paragraph",
        text: "The original croc transfer assumes both computers are online. I like this model because it is easy to reason about: one sender, one receiver, and an encrypted stream that exists while they are talking. It becomes less charming when the other person is asleep, on a plane, or not opening that computer until tomorrow.",
      },
      {
        type: "paragraph",
        text: "I briefly considered treating ‘leave the browser tab open until morning’ as documentation. That would technically work and would also be terrible. Stored mode exists so the sender can finish now and the receiver can arrive later, without turning the service into a permanent folder of everybody's files.",
      },
      { type: "heading", text: "Stored mode is a different bargain" },
      {
        type: "paragraph",
        text: "When storage is enabled, the Send panel offers Store with a finite lifetime selected by the sender. The browser encrypts file names, metadata, and chunks before uploading anything. The service keeps the ciphertext and returns a browser link plus a CLI token. Live transfer remains the default because, when both people are present, not storing the file at all is simpler.",
      },
      {
        type: "aside",
        eyebrow: "A SMALL URL TRICK",
        title: "The key lives after the #",
        text: "A stored link looks like /s/id#v1.key. Browsers leave the fragment after # out of HTTP requests, so the server receives the opaque ID while the web client reads the decryption key locally.",
      },
      {
        type: "paragraph",
        text: "The complete link is still a secret. Anyone who gets it can decrypt the manifest and claim one of the allowed downloads. Keeping the key after the # prevents it from appearing in ordinary server and proxy request logs. It cannot prevent me from pasting the whole link into the wrong chat.",
      },
      { type: "heading", text: "Opening the link does not burn it" },
      {
        type: "paragraph",
        text: "I did not want a browser preview, failed download, or accidental refresh to destroy the transfer. A receiver claims it before reading chunks, authenticates those chunks, verifies the completed files, and only then commits the download. The service decrements the configured allowance after each verified commit and deletes the ciphertext after the last one. If nobody finishes, it expires after the selected lifetime, measured from upload completion.",
      },
      {
        type: "list",
        items: [
          "Share the full browser link or CLI token through a private channel.",
          "Keep the sender's revoke receipt until the transfer is consumed or expired.",
          "Expect the service to learn timing, ciphertext size, transfer totals, and connection metadata, but not readable names or contents.",
          "Use direct mode when both peers are online; it avoids storing even ciphertext between them.",
        ],
      },
      { type: "heading", text: "What storage learns" },
      {
        type: "paragraph",
        text: "Stored mode necessarily reveals more than direct mode. The service knows when the upload happened, the ciphertext size, transfer totals, and connection metadata. It does not receive readable names or contents, but a temporary encrypted copy still exists on disk. This is why storage is an option rather than a silent change to every croc transfer.",
      },
      {
        type: "code",
        label: "The same mode from the CLI",
        lines: [
          "$ croc send --store photo.jpg document.pdf",
          "$ croc send --store --store-downloads 3 photo.jpg document.pdf",
          "$ croc send --store --store-expiration 3d photo.jpg document.pdf",
          "Browser link: https://host/s/…#v1.…",
          "CLI token: croc-store-v1.…",
        ],
      },
      {
        type: "paragraph",
        text: "The sender can revoke the transfer, the final allowed verified receiver can consume it, or the clock can expire it. Those are deliberately boring endings. Stored mode is not better than a live relay; it stores more and needs more machinery. It is just the version I want when the difficult thing between two computers is the calendar.",
      },
    ],
  },
  {
    slug: "share-stored-file-with-group",
    number: "08",
    title: "Send one file to a group, on their schedule",
    description:
      "Stored mode can give several people time to retrieve the same encrypted file. Choose a download allowance, choose a deadline, and share one link.",
    category: "Stored transfers",
    publishedAt: "2026-08-11",
    publishedLabel: "August 11, 2026",
    author: "schollz",
    visual: "stored",
    takeaway:
      "Set the verified-download allowance to at least the number of intended recipients, then choose a lifetime long enough for the last person to finish.",
    blocks: [
      {
        type: "paragraph",
        text: "A live croc transfer is a conversation between two computers. That is ideal when one person is waiting on each side. It is less ideal when I need to send the same report to five people in three time zones and do not want to arrange five separate appointments with their laptops.",
      },
      {
        type: "paragraph",
        text: "Stored mode makes that handoff asynchronous. I upload one encrypted copy, receive one secret link, and let each recipient arrive while it is still available. Two settings define the useful window: how many verified downloads may finish, and how long the encrypted copy may remain after the upload completes.",
      },
      { type: "heading", text: "Count completed downloads, not clicks" },
      {
        type: "paragraph",
        text: "The sender's --store-downloads value is the download allowance. If five people need the file, I set it to at least five. A storage service can cap this number; asking for more than its configured maximum returns an error. Looking at the manifest, opening the link, refreshing a page, or abandoning an incomplete receive does not spend an allowance. A download is counted only after a client authenticates the chunks, verifies every finished file, and commits the receive.",
      },
      {
        type: "aside",
        eyebrow: "A DOWNLOAD IS NOT AN IDENTITY",
        title: "croc counts successful receives, not unique people",
        text: "The same person can download twice, and anyone with the complete link can use an allowance. Set a modest buffer only when that behavior is acceptable, and share the link through a private channel.",
      },
      { type: "heading", text: "Give the slowest recipient enough time" },
      {
        type: "paragraph",
        text: "The --store-expiration value is a finite lifetime measured from successful upload completion. It accepts whole minutes, hours, days, or weeks: 90m, 12h, 3d, or 2w. I choose a time by thinking about the last recipient, not the first. For a workday handoff across time zones, three days is more forgiving than twelve hours. The service may enforce a shorter maximum, so the absolute expiration printed after upload is the time that matters.",
      },
      {
        type: "code",
        label: "One encrypted file for five recipients",
        lines: [
          "$ croc send --store \\",
          "    --store-downloads 5 \\",
          "    --store-expiration 3d \\",
          "    quarterly-report.pdf",
          "Browser link: https://host/s/…#v1.…",
          "Available until: Fri, 14 Aug 2026 …",
        ],
      },
      {
        type: "paragraph",
        text: "That command asks for five completed receives and three days of storage. The client encrypts the file name, metadata, and contents before uploading. When the upload is finalized, the server starts the accepted lifetime and the sender sees the actual absolute expiration. The same output also includes a CLI token for recipients who prefer the terminal.",
      },
      {
        type: "list",
        items: [
          "Set --store-downloads to at least the number of people who must finish a receive.",
          "Set --store-expiration from the time the last person is realistically able to download, with sensible slack.",
          "Send the browser link to browser recipients or the CLI token to croc command-line recipients.",
          "Keep the sender's transfer ID until the group is finished in case the link needs to be revoked.",
        ],
      },
      { type: "heading", text: "The browser exposes the same two choices" },
      {
        type: "paragraph",
        text: "On getcroc.com, switch the Send panel from Direct to Store. Choose a whole-number storage lifetime and its minutes, hours, days, or weeks unit, then set Verified downloads to the size of the group. The controls stay inside the limits published by the storage server. The share card appears only after the encrypted upload succeeds and shows the real expiration time returned at completion.",
      },
      { type: "heading", text: "The link is still the key" },
      {
        type: "paragraph",
        text: "A group transfer does not create accounts or an invitation list. The browser link and CLI token both contain the decryption key, while the server stores only ciphertext. This keeps readable names and contents away from the storage service, but it also means the complete share value is a bearer secret. A forwarded link can be used by someone outside the intended group and can consume one of the downloads.",
      },
      {
        type: "paragraph",
        text: "The transfer ends in whichever way happens first: the final allowed verified download commits, the accepted clock expires, or the sender revokes it. I still use Direct when everybody is present. For the awkward case where one file has several destinations and several calendars, one encrypted stored upload is the simpler appointment.",
      },
    ],
  },
  {
    slug: "compare-file-transfer-tools",
    number: "09",
    title: "36 ways to send a file",
    description:
      "Compare croc with 35 file transfer tools by resume support, account requirements, browser and CLI transfers, encryption, availability, and transfer paths.",
    category: "Field guide",
    publishedAt: "2026-08-12",
    publishedLabel: "August 12, 2026",
    author: "schollz",
    visual: "bridge",
    takeaway:
      "The right tool depends on the trip, but croc is the only available no-account option here combining resumable CLI transfers with browser-to-browser, browser-to-CLI, and CLI-to-CLI routes.",
    blocks: [
      {
        type: "paragraph",
        text: "In 2017 I wanted to send a one-gigabyte documentary about turkeys to a friend in another country. She used Windows, did not enjoy terminals, and quite reasonably did not want to configure a router before watching a movie. I compared seven ways to move the file. That little survey eventually became croc.",
      },
      {
        type: "paragraph",
        text: "Nine years later, sending a file should be a solved problem. Instead I found 36 solutions and at least four different definitions of the problem. Some make a live pipe between two computers. Some synchronize a folder forever. Some work only while two browser tabs remain awake. Others store a copy and produce a link for somebody who may arrive tomorrow.",
      },
      {
        type: "paragraph",
        text: "This field guide compares croc with 35 file transfer tools, including Magic Wormhole, Syncthing, LocalSend, PairDrop, WeTransfer, MEGA, and Firefox Send. The tables cover resumable transfers, account requirements, browser, command-line, and app endpoints, availability, encryption, and the path each file takes.",
      },
      {
        type: "aside",
        eyebrow: "BIAS, DISCLOSED",
        title: "I know one row from the inside",
        text: "I built croc, so naturally I know that row best. AirDrop, Syncthing, OnionShare, and stored-link services each solve different jobs that croc should not pretend are identical.",
      },
      { type: "heading", text: "How I compared 36 file transfer tools" },
      {
        type: "paragraph",
        text: "I checked official sites, documentation, and repositories on August 12, 2026. A full circle means the project documents that capability without an important limitation. A half-filled circle means it works only on some platforms, in some modes, through a documented third-party adapter, or with another caveat. An empty circle means I could not find documented support. Under Resume, that specifically means I could not find a promise that a stopped transfer can restart without beginning the file at byte zero.",
      },
      {
        type: "list",
        items: [
          "Account: none required, optional for some modes, or required before sending.",
          "Resume: recovery after an interrupted transfer, not merely retrying a packet inside the same session.",
          "Browser ↔ browser, browser ↔ CLI, and CLI ↔ CLI: the three interface combinations I most often need.",
          "App ↔ app: included so good native tools such as LocalSend and AirDrop do not appear to do nothing.",
          "Availability: whether the hosted service or installable source still exists, with approximate lifetimes for historical projects.",
          "Encryption, accounts, and byte path: who can read the file, what identity is required, and whether the transfer is live, relayed, synchronized, or stored.",
        ],
      },
      {
        type: "aside",
        eyebrow: "WHY THERE IS NO SPEED COLUMN",
        title: "I did not benchmark bandwidth",
        text: "A nearby relay on fiber and a direct transfer through hotel Wi-Fi are not a useful comparison of two programs. Geography, NATs, browser limits, providers, and endpoint upload speeds would overwhelm the implementation. The byte-path table is the more honest performance hint.",
      },
      { type: "heading", text: "File transfer tools comparison table" },
      {
        type: "paragraph",
        text: "Browser means the browser handles the file payload, not merely that a daemon has a web settings page. PairDrop's shell helper opens the browser and hands files to it, so that route is partial. App means a native GUI or mobile application. The complete field is croc plus 35 alternatives.",
      },
      {
        type: "table",
        caption: "Availability, account requirement, resumption, and supported endpoint combinations",
        headers: ["Tool", "Available", "Account", "Resume", "B ↔ B", "B ↔ CLI", "CLI ↔ CLI", "App ↔ app"],
        indicatorColumns: [1, 2, 3, 4, 5, 6, 7],
        rowOrder: fileTransferToolOrder,
        indicatorLegend: {
          full: "Meets the column without an important limitation",
          partial: "Meets it only in some modes or with a caveat",
          empty: "No documented support",
          terms: [
            { term: "B", definition: "A browser handles the file itself, not merely a settings page." },
            { term: "CLI", definition: "A command-line client sends or receives the file. An API that could be scripted does not count by itself." },
            { term: "App", definition: "A native desktop or mobile application." },
            { term: "Account", definition: "Filled means no account is required. Half means optional or mode-dependent. Empty means an account or existing credentials are required." },
          ],
        },
        rows: [
          { cells: ["croc", "●", "●", "●", "●", "●", "●", "●"], href: "https://github.com/schollz/croc", highlight: true },
          { cells: ["Magic Wormhole", "●", "●", "○", "○", "○", "● installed command is wormhole", "○"], href: "https://github.com/magic-wormhole/magic-wormhole" },
          { cells: ["iroh Sendme", "◐ example application", "●", "●", "○", "○", "●", "○"], href: "https://github.com/n0-computer/sendme" },
          { cells: ["ZeroTier Toss", "○ archived; ~7 years (2017–2024)", "●", "○", "○", "○", "●", "○"], href: "https://github.com/zerotier/toss" },
          { cells: ["Portal", "◐ quiet project", "●", "○", "○", "○", "●", "○"], href: "https://github.com/SpatiumPortae/portal" },
          { cells: ["qrcp", "●", "●", "○", "○", "●", "○ CLI endpoint serves a browser page", "○"], href: "https://github.com/claudiodangelis/qrcp" },
          { cells: ["Peer Copy", "○ archived; ~4 years (2021–2025)", "●", "○", "○", "○", "●", "○"], href: "https://github.com/dennis-tra/pcp" },
          { cells: ["transfer.sh", "◐ source available; self-hosting advised", "● default", "○", "◐ depends on instance", "●", "●", "○"], href: "https://github.com/dutchcoders/transfer.sh" },
          { cells: ["rsync", "●", "○ remote login or key required", "◐ opt-in", "○", "○", "●", "○"], href: "https://rsync.samba.org/" },
          { cells: ["rclone", "●", "○ storage credentials usually required", "◐ depends on backend and file", "○", "◐ via serve mode", "●", "○"], href: "https://rclone.org/" },
          { cells: ["Tailscale Taildrop", "●", "○ Tailscale account required", "◐ platform-dependent", "○", "○", "●", "●"], href: "https://tailscale.com/kb/1106/taildrop" },
          { cells: ["OnionShare", "●", "●", "○", "○", "●", "○ CLI hosts the onion service; the other endpoint uses Tor Browser", "○"], href: "https://onionshare.org/" },
          { cells: ["Syncthing", "●", "● no service account", "● automatic", "○", "○", "● headless", "●"], href: "https://syncthing.net/" },
          { cells: ["LocalSend", "●", "●", "○", "○", "○", "◐ unofficial localsend-rs client sends and receives", "●"], href: "https://localsend.org/" },
          { cells: ["PairDrop", "●", "●", "○", "●", "◐ helper opens browser", "○ helper opens a browser; there is no terminal receiver", "○"], href: "https://pairdrop.net/" },
          { cells: ["Snapdrop Classic", "○ classic ended; ~9 years (2015–2025)", "●", "○", "●", "○", "○", "○"], href: "https://github.com/SnapDrop/snapdrop" },
          { cells: ["ShareDrop Classic", "○ classic ended; ~11 years (2014–2025)", "●", "○", "●", "○", "○", "○"], href: "https://github.com/ShareDropio/sharedrop" },
          { cells: ["FilePizza", "●", "●", "○", "●", "○", "○", "○"], href: "https://transfer.gattini.ninja/" },
          { cells: ["WebWormhole", "◐ experimental", "●", "○", "●", "●", "● ww send and ww receive", "○"], href: "https://github.com/saljam/webwormhole" },
          { cells: ["ToffeeShare", "●", "●", "○", "●", "○", "○", "○"], href: "https://toffeeshare.com/" },
          { cells: ["Wormhole.app", "●", "●", "○", "●", "○", "○ browser service; unrelated to the Magic Wormhole CLI", "○"], href: "https://wormhole.app/" },
          { cells: ["Blaze", "◐ quiet project", "●", "○", "●", "○", "○", "○"], href: "https://blaze.now.sh/" },
          { cells: ["LANDrop", "◐ public source outdated", "●", "○", "○", "○", "○", "●"], href: "https://landrop.app/" },
          { cells: ["Warpinator", "●", "●", "○", "○", "○", "○ GUI application; no documented transfer CLI", "●"], href: "https://github.com/linuxmint/warpinator" },
          { cells: ["KDE Connect", "●", "●", "○", "○", "○", "◐ kdeconnect-cli sends; the receiver is a daemon or app", "●"], href: "https://kdeconnect.kde.org/" },
          { cells: ["Apple AirDrop", "●", "◐ Everyone mode", "○", "○", "○", "◐ unofficial OpenDrop; experimental and limited to macOS or Linux with AWDL", "●"], href: "https://support.apple.com/guide/security/airdrop-security-sec2261183f4/web" },
          { cells: ["Google Quick Share", "●", "◐ optional", "○", "○", "○", "○ no documented Quick Share CLI", "●"], href: "https://www.android.com/better-together/quick-share-app/" },
          { cells: ["Firefox Send", "○ discontinued; ~3 years (2017–2020)", "◐ optional historically", "○", "● historical", "◐ historical ffsend", "◐ historical ffsend", "◐ historical Android beta"], href: "https://support.mozilla.org/kb/what-happened-firefox-send" },
          { cells: ["WeTransfer", "●", "○ required to send", "○", "●", "◐ unofficial transferwee client", "◐ unofficial transferwee client uploads and downloads", "●"], href: "https://wetransfer.com/" },
          { cells: ["SwissTransfer", "●", "●", "○", "●", "○", "○ unofficial Swish client can download, but uploads no longer work", "●"], href: "https://www.swisstransfer.com/" },
          { cells: ["Send Anywhere", "●", "◐ optional", "○", "●", "○", "○ current documentation lists web, desktop, and mobile apps, not a CLI", "●"], href: "https://send-anywhere.com/" },
          { cells: ["Smash", "●", "◐ free tier", "○", "●", "○", "○ official Node.js SDKs are libraries, not a CLI", "◐ some integrations"], href: "https://fromsmash.com/" },
          { cells: ["Filemail", "●", "◐ free use", "○", "●", "●", "● official CLI uploads and downloads", "●"], href: "https://www.filemail.com/" },
          { cells: ["TransferNow", "●", "◐ free use", "◐ uploads", "●", "○", "○ API access is not a documented CLI client", "●"], href: "https://www.transfernow.net/" },
          { cells: ["pCloud Transfer", "●", "●", "○", "●", "○", "○ upload API is not a packaged CLI client", "○"], href: "https://transfer.pcloud.com/" },
          { cells: ["MEGA", "●", "○ required to upload", "◐ client and mode caveats", "●", "●", "● official MEGAcmd uploads and downloads", "●"], href: "https://mega.io/" },
        ],
      },
      {
        type: "paragraph",
        text: "The wormhole names need a map. Magic Wormhole installs a command named wormhole and supports CLI-to-CLI transfers. Wormhole.app is a separate browser service with no documented CLI. A newer native app also named Wormhole speaks the Magic Wormhole protocol, but it is not the Wormhole.app website.",
      },
      {
        type: "paragraph",
        text: "Only a few tools bridge browser and terminal worlds. Croc and WebWormhole support all three combinations directly. MEGA and Filemail support terminal upload and download through stored cloud files. Historical Firefox Send could do it through ffsend. Half circles capture less direct routes such as LocalSend's third-party CLI, OpenDrop's experimental AirDrop implementation, and WeTransfer's unofficial transferwee client.",
      },
      {
        type: "paragraph",
        text: "Resume is rarer than a progress bar suggests. Magic Wormhole still has an open request for restartable transfers. Iroh Sendme explicitly resumes verified downloads, rsync keeps partial data when asked, and Syncthing retains temporary blocks. Taildrop has receiving-platform exceptions, while MEGA's behavior changes between browsers and native apps.",
      },
      { type: "heading", text: "File transfer encryption and transfer paths" },
      {
        type: "paragraph",
        text: "Encrypted is a stretchy word. TLS to an upload service protects a file on the way to the service, but the service can ordinarily process it. End-to-end encryption means an intermediary should not have the key. WebRTC encrypts the connection between peers; some tools add a password-authenticated layer above it. This table describes documented designs, not an independent security audit.",
      },
      {
        type: "table",
        caption: "Encryption model and transfer architecture",
        headers: ["Tool", "Encryption model", "Path of the file"],
        rowOrder: fileTransferToolOrder,
        rows: [
          { cells: ["croc", "Application E2EE; PAKE + AES-GCM", "Live relay; optional client-encrypted storage"], href: "https://github.com/schollz/croc", highlight: true },
          { cells: ["Magic Wormhole", "Application E2EE with PAKE", "Live direct or transit relay"], href: "https://github.com/magic-wormhole/magic-wormhole" },
          { cells: ["iroh Sendme", "Authenticated TLS to node ID", "Live hole-punched path or encrypted relay"], href: "https://github.com/n0-computer/sendme" },
          { cells: ["ZeroTier Toss", "No encryption; token authentication", "Direct TCP, mainly LAN/virtual LAN"], href: "https://github.com/zerotier/toss" },
          { cells: ["Portal", "Application E2EE with PAKE2", "Live direct connection or relay"], href: "https://github.com/SpatiumPortae/portal" },
          { cells: ["qrcp", "HTTP default; optional user TLS", "Direct HTTP server on LAN"], href: "https://github.com/claudiodangelis/qrcp" },
          { cells: ["Peer Copy", "Application E2EE with PAKE", "libp2p direct or decentralized relay"], href: "https://github.com/dennis-tra/pcp" },
          { cells: ["transfer.sh", "TLS; provider-readable unless pre-encrypted", "Stored on public/self-hosted server"], href: "https://github.com/dutchcoders/transfer.sh" },
          { cells: ["rsync", "SSH encrypted; raw daemon is not", "Direct to remote host/daemon"], href: "https://rsync.samba.org/" },
          { cells: ["rclone", "Backend TLS; optional client crypt", "Cloud/backend or served protocol"], href: "https://rclone.org/" },
          { cells: ["Tailscale Taildrop", "WireGuard E2EE", "Direct when possible; DERP otherwise"], href: "https://tailscale.com/kb/1106/taildrop" },
          { cells: ["OnionShare", "Tor end-to-end transport", "Sender hosts temporary onion service"], href: "https://onionshare.org/" },
          { cells: ["Syncthing", "Mutually authenticated TLS", "Continuous direct sync or encrypted relay"], href: "https://docs.syncthing.net/users/security.html" },
          { cells: ["LocalSend", "HTTPS on local network", "Direct LAN transfer"], href: "https://github.com/localsend/localsend" },
          { cells: ["PairDrop", "WebRTC/DTLS; server fallbacks", "Peer-to-peer, TURN, or WebSocket fallback"], href: "https://github.com/schlagmichdoch/PairDrop" },
          { cells: ["Snapdrop Classic", "WebRTC/DTLS", "Peer-to-peer; signaling server"], href: "https://github.com/SnapDrop/snapdrop" },
          { cells: ["ShareDrop Classic", "WebRTC/DTLS", "Peer-to-peer; Firebase signaling"], href: "https://github.com/ShareDropio/sharedrop" },
          { cells: ["FilePizza", "WebRTC/DTLS; optional password", "Peer-to-peer or encrypted TURN relay"], href: "https://transfer.gattini.ninja/" },
          { cells: ["WebWormhole", "PAKE-authenticated WebRTC; unreviewed", "Peer-to-peer or relay"], href: "https://github.com/saljam/webwormhole" },
          { cells: ["ToffeeShare", "WebRTC/DTLS", "Peer-to-peer; sender stays online"], href: "https://toffeeshare.com/" },
          { cells: ["Wormhole.app", "Client-side AES-GCM E2EE", "≤5 GB encrypted storage; larger live P2P"], href: "https://wormhole.app/security" },
          { cells: ["Blaze", "WebRTC direct; TLS/WebSocket fallback", "Peer-to-peer or server fallback"], href: "https://github.com/blenderskool/blaze" },
          { cells: ["LANDrop", "Project claims app encryption; source closed", "Direct LAN transfer"], href: "https://github.com/LANDrop/LANDrop" },
          { cells: ["Warpinator", "Encrypted LAN; shared group code", "Direct on same local subnet"], href: "https://github.com/linuxmint/warpinator" },
          { cells: ["KDE Connect", "Paired TLS connection", "Direct on local network"], href: "https://github.com/KDE/kdeconnect-kde" },
          { cells: ["Apple AirDrop", "TLS plus Apple identity checks", "Nearby peer-to-peer Wi-Fi"], href: "https://support.apple.com/guide/security/airdrop-security-sec2261183f4/web" },
          { cells: ["Google Quick Share", "Encrypted nearby transfer", "Bluetooth discovery + direct Wi-Fi"], href: "https://support.google.com/android/answer/13801258" },
          { cells: ["Firefox Send", "Client-side E2EE", "Encrypted temporary cloud storage"], href: "https://support.mozilla.org/kb/what-happened-firefox-send" },
          { cells: ["WeTransfer", "TLS transit; AES-256 at rest", "Provider storage + download link"], href: "https://wetransfer.com/help-center/security-privacy/how-do-we-protect-your-files" },
          { cells: ["SwissTransfer", "Encrypted transit + provider storage", "Swiss provider storage, up to 30 days"], href: "https://www.infomaniak.com/en/support/faq/1755/understanding-swisstransfer-data-security" },
          { cells: ["Send Anywhere", "Encrypted transport", "Live six-digit transfer or 48-hour link"], href: "https://support.send-anywhere.com/hc/en-us/articles/360005733434" },
          { cells: ["Smash", "TLS transit; AES-256 at rest", "Provider storage + download link"], href: "https://fromsmash.com/help/articles/13179049-is-smash-secure" },
          { cells: ["Filemail", "TLS/provider encryption; optional E2EE", "Provider storage + download link"], href: "https://support.filemail.com/en/articles/10313859-end-to-end-encryption-with-filemail" },
          { cells: ["TransferNow", "TLS/provider AES; password options", "Provider storage + download link"], href: "https://www.transfernow.net/en/features" },
          { cells: ["pCloud Transfer", "TLS; optional client password encryption", "Provider storage + download link"], href: "https://help.pcloud.com/article/pcloud-transfer" },
          { cells: ["MEGA", "Client-side E2EE", "Encrypted cloud storage"], href: "https://mega.io/security" },
        ],
      },
      {
        type: "paragraph",
        text: "The path reveals the big trade. A live peer-to-peer or relay transfer usually needs the sender to remain online. A stored-link service makes another copy and adds another trust relationship, but the recipient can be asleep. Syncthing and rclone are different again: they are durable systems for repeated movement rather than one-time handoffs.",
      },
      { type: "heading", text: "Discontinued file transfer tools" },
      {
        type: "paragraph",
        text: "Firefox Send began as a Mozilla Test Pilot experiment in August 2017, became a standalone product in March 2019, and was discontinued in September 2020. That is about three years as a public project, or only 18 months as a standalone product. It remains a useful warning that open source and a thoughtful privacy design do not guarantee a hosted service will live forever.",
      },
      {
        type: "paragraph",
        text: "ZeroTier Toss lived for about seven years before archival in April 2024. Peer Copy was a decentralized libp2p experiment for about four years before archival in April 2025. Snapdrop Classic and ShareDrop Classic had softer endings: their domains became LimeWire services in 2025, while their roughly nine- and eleven-year-old classic repositories remained available for self-hosting.",
      },
      { type: "heading", text: "Choose the tool you need" },
      {
        type: "list",
        items: [
          "A folder should stay synchronized between my devices: Syncthing. Taildrop is convenient inside one Tailscale account; rclone is excellent when the destination is cloud storage.",
          "Two nontechnical people are on one LAN: LocalSend or PairDrop. AirDrop is wonderfully frictionless inside Apple's devices, and Quick Share covers much of Android, ChromeOS, and Windows.",
          "The recipient will arrive tomorrow: croc stored mode, Wormhole.app, or a conventional stored-link provider chosen for its limits, retention, account policy, and trust model.",
          "Both people have terminals: croc, Magic Wormhole, Portal, and iroh Sendme are understandable one-time exchanges. Rsync is a natural fit when SSH access already exists.",
          "The recipient has a browser and I have a terminal: qrcp is lovely on a LAN; croc crosses networks; OnionShare is the special answer when Tor anonymity matters.",
          "Nobody will install anything: PairDrop, ToffeeShare, Wormhole.app, Blaze, and the other browser tools are the shortest path, provided the live sender can keep a tab awake.",
        ],
      },
      { type: "heading", text: "How croc compares with other file transfer tools" },
      {
        type: "paragraph",
        text: "Croc started as a terminal program alongside tools such as Magic Wormhole, Toss, and scp. The browser changed its shape. Getcroc.com can now send to or receive from another browser or the ordinary command-line client. A normal transfer is live, relayed, and end-to-end encrypted. Stored mode encrypts in the client and temporarily retains only ciphertext for recipients who arrive later.",
      },
      {
        type: "aside",
        eyebrow: "A NARROW DISTINCTION",
        title: "One useful combination is still unusual",
        text: "Among the currently available, no-account tools in this survey, croc is the only one combining resumable CLI transfers with browser ↔ browser, browser ↔ CLI, and CLI ↔ CLI routes.",
      },
      {
        type: "paragraph",
        text: "That does not make croc the correct answer to every row. I would not replace Syncthing for a continuously mirrored folder, make two iPhone users skip AirDrop, or promise OnionShare's anonymity without Tor. But when I know almost nothing about the computer on the other side, including its operating system, whether its owner likes terminals, or whether the Wi-Fi is about to disappear, croc has become a pretty good default answer to the surprisingly durable question: how do I send this file?",
      },
    ],
  },
];

const seoBySlug = new Map(blogSEO.posts.map((entry) => [entry.slug, entry]));

export const blogPosts: BlogPost[] = drafts
  .map((post) => {
    const seo = seoBySlug.get(post.slug);
    if (!seo) throw new Error(`Missing SEO metadata for blog post ${post.slug}`);
    return {
      ...post,
      kind: post.kind ?? "note",
      seoTitle: seo.seoTitle ?? post.title,
      modifiedAt: seo.modifiedAt,
      keywords: [...seo.keywords],
      image: seo.image,
      socialImage: seo.socialImage,
      imageAlt: seo.imageAlt,
      wordCount: blogWordCount(post.blocks),
      readingMinutes: readingMinutes(post.blocks),
      relatedSlugs: [...seo.relatedSlugs],
    };
  })
  .sort(
    (left, right) =>
      right.publishedAt.localeCompare(left.publishedAt) ||
      right.number.localeCompare(left.number),
  );

export function getBlogPost(slug: string) {
  return blogPosts.find((post) => post.slug === slug);
}
