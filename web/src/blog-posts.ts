import blogSEO from "./blog-seo.json";

export type BlogVisual =
  | "overview"
  | "relay"
  | "code"
  | "handshake"
  | "browser"
  | "bridge"
  | "stored";

export type BlogBlock =
  | { type: "heading"; text: string }
  | { type: "paragraph"; text: string }
  | { type: "list"; items: string[] }
  | { type: "code"; label: string; lines: string[] }
  | { type: "aside"; eyebrow: string; title: string; text: string };

export type BlogPost = {
  slug: string;
  number: string;
  title: string;
  description: string;
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
  blocks: BlogBlock[];
};

type DraftBlogPost = Omit<
  BlogPost,
  "modifiedAt" | "keywords" | "image" | "socialImage" | "imageAlt" | "wordCount" | "readingMinutes"
>;

const wordsPerMinute = 210;

export function blogWordCount(blocks: BlogBlock[]) {
  return blocks.flatMap((block) => {
    if (block.type === "list") return block.items;
    if (block.type === "code") return block.lines;
    if (block.type === "aside") {
      return [block.eyebrow, block.title, block.text];
    }
    return [block.text];
  }).join(" ").trim().split(/\s+/).filter(Boolean).length;
}

export function readingMinutes(blocks: BlogBlock[]) {
  return Math.max(1, Math.ceil(blogWordCount(blocks) / wordsPerMinute));
}

const drafts: DraftBlogPost[] = [
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
        text: "The code also had to be something I could tell another person without reading a small novel of random characters. Four words are manageable. Four words are not, by themselves, the sort of key I want encrypting gigabytes of data. Making the code fifty characters long would help the cryptography and ruin the interface.",
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
          "Code is: river-cabin-lantern-moss",
          "",
          "$ croc river-cabin-lantern-moss",
        ],
      },
      {
        type: "paragraph",
        text: "That is still more or less my test for croc. If sending one file begins to feel like configuring a file-transfer system, something has gone wrong. I want to choose the file, tell my friend four words, and get back to the turkey documentary.",
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
        text: "Both croc clients can, however, make an outbound connection to a relay they already know how to reach. The relay puts the matching connections together. It can observe mundane network facts—when the clients connect and how many encrypted bytes pass through—but the file names, metadata, and contents are encrypted with a key it does not have.",
      },
      {
        type: "code",
        label: "A complete direct transfer",
        lines: [
          "$ croc send field-notes.pdf",
          "Code is: river-cabin-lantern-moss",
          "",
          "$ croc",
          "Enter receive code: river-cabin-lantern-moss",
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
    title: "What the four words are doing",
    description:
      "I wanted a croc code to be easy to read aloud. Making four ordinary words secure enough for a file transfer takes a little more work.",
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
        text: "A croc code looks almost too friendly for cryptography. It is four ordinary words with hyphens between them. I chose words because they survive being copied into a message, read over the phone, or typed on a tiny keyboard much better than a long string of punctuation. You can also notice when somebody only sent three of them.",
      },
      {
        type: "paragraph",
        text: "The code has two jobs. It helps the clients meet in the same transfer, and it gives them a small secret that both people know. What it does not do is directly encrypt the file. Four words would be carrying far too much weight if that were the complete scheme.",
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
          "Copy all four words, including their order.",
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
        text: "So the four words are doing more than naming a room, and less than encrypting the file. They are the small thing the two people move. PAKE uses that small thing to make the large secret the computers actually need.",
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
        text: "On screen this all becomes four words and a progress bar. Underneath are two curve points, two random values, a transcript, a key schedule, and two confirmation checks. That is quite a lot of machinery for a short code, but it lets the short code remain short—which was the problem in the first place.",
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
        text: "croc makes a four-word code. Copy it into a message, or show the QR code if the other device is close enough to scan it. I added the QR version mostly for laptop-to-phone transfers because typing four hyphenated words on a phone is not difficult, but scanning them is nicer. The code is a live secret, so send it privately when the file is private.",
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
          "Code is: amber-ferry-piano-rain",
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
        lines: ["$ croc", "Enter receive code: amber-ferry-piano-rain"],
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
      "Stored mode encrypts locally and keeps only ciphertext until one verified download or 24 hours, whichever happens first.",
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
        text: "When storage is enabled, the Send panel offers Store for 24 hours. The browser encrypts file names, metadata, and chunks before uploading anything. The service keeps the ciphertext and returns a browser link plus a CLI token. Live transfer remains the default because, when both people are present, not storing the file at all is simpler.",
      },
      {
        type: "aside",
        eyebrow: "A SMALL URL TRICK",
        title: "The key lives after the #",
        text: "A stored link looks like /s/id#v1.key. Browsers leave the fragment after # out of HTTP requests, so the server receives the opaque ID while the web client reads the decryption key locally.",
      },
      {
        type: "paragraph",
        text: "The complete link is still a secret. Anyone who gets it can decrypt the manifest, claim the transfer, and consume the one download. Keeping the key after the # prevents it from appearing in ordinary server and proxy request logs. It cannot prevent me from pasting the whole link into the wrong chat.",
      },
      { type: "heading", text: "Opening the link does not burn it" },
      {
        type: "paragraph",
        text: "I did not want a browser preview, failed download, or accidental refresh to destroy the transfer. A receiver claims it before reading chunks, authenticates those chunks, verifies the completed files, and only then commits the download. The service deletes the ciphertext after that verified commit. If nobody finishes, it expires after 24 hours.",
      },
      {
        type: "list",
        items: [
          "Share the full browser link or CLI token through a private channel.",
          "Keep the sender's revoke receipt until the transfer is consumed or expired.",
          "Expect the service to learn timing, ciphertext size, transfer totals, and connection metadata—not readable names or contents.",
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
          "Browser link: https://host/s/…#v1.…",
          "CLI token: croc-store-v1.…",
        ],
      },
      {
        type: "paragraph",
        text: "The sender can revoke the transfer, the first verified receiver can consume it, or the clock can expire it. Those are deliberately boring endings. Stored mode is not better than a live relay; it stores more and needs more machinery. It is just the version I want when the difficult thing between two computers is the calendar.",
      },
    ],
  },
];

const seoBySlug = new Map(blogSEO.posts.map((entry) => [entry.slug, entry]));

export const blogPosts: BlogPost[] = drafts.map((post) => {
  const seo = seoBySlug.get(post.slug);
  if (!seo) throw new Error(`Missing SEO metadata for blog post ${post.slug}`);
  return {
    ...post,
    modifiedAt: seo.modifiedAt,
    keywords: [...seo.keywords],
    image: seo.image,
    socialImage: seo.socialImage,
    imageAlt: seo.imageAlt,
    wordCount: blogWordCount(post.blocks),
    readingMinutes: readingMinutes(post.blocks),
  };
});

export function getBlogPost(slug: string) {
  return blogPosts.find((post) => post.slug === slug);
}
