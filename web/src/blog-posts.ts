import blogSEO from "./blog-seo.json";

export type BlogVisual =
  | "overview"
  | "relay"
  | "code"
  | "handshake"
  | "browser"
  | "bridge"
  | "stored"
  | "derp"
  | "ssh"
  | "ssh-release"
  | "update-release"
  | "release";

export type BlogKind = "note" | "update";

export type BlogTextLink = {
  label: string;
  href: string;
};

export type BlogBlock =
  | { type: "heading"; text: string }
  | { type: "paragraph"; text: string; links?: BlogTextLink[] }
  | { type: "list"; items: string[] }
  | { type: "code"; label: string; lines: string[] }
  | { type: "details"; summary: string; lines: string[] }
  | {
    type: "aside";
    eyebrow: string;
    title: string;
    text: string;
    links?: BlogTextLink[];
  }
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
  "Floe",
  "Syncthing",
  "AirPipe",
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
  "derphole",
  "wormhole-william",
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
    if (block.type === "details") return [block.summary, ...block.lines];
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
    slug: "croc-v11-5-release-update",
    number: "04",
    title: "croc v11.5 can update itself",
    category: "Release notes",
    publishedAt: "2026-09-05",
    publishedLabel: "September 5, 2026",
    author: "schollz",
    visual: "update-release",
    kind: "update",
    description: "croc v11.5 adds a verified standalone updater and clearer messages for shared SSH sessions.",
    takeaway: "Use croc update to prepare the updater, then croc-update to replace a standalone installation. SSH sessions also explain their state and errors more clearly.",
    blocks: [
      {
        type: "paragraph",
        text: "Updating croc used to mean running the installer again. In v11.5, croc update prepares the updater and croc-update replaces croc. The installer puts both executables in the same directory, so each can replace the other without overwriting the program that is running.",
      },
      { type: "heading", text: "Updating croc" },
      {
        type: "code",
        label: "Check availability, then prepare the updater",
        lines: [
          "$ croc update --check",
          "$ croc update",
        ],
      },
      {
        type: "paragraph",
        text: "When croc is current, croc update says so. When a release is available, it installs or refreshes the helper if needed and prints Run: croc-update. The helper shows the version change and exact executable before asking for confirmation, displays download progress, and reports the installed version.",
      },
      {
        type: "code",
        label: "Update the installed executable",
        lines: [
          "$ croc-update",
          "Update croc: v11.5.0 → v11.6.0",
          "Replace /usr/local/bin/croc? (y/N) y",
          "Downloading croc 100% |████████████████████| (8.6/8.6 MB, 1.8 MB/s)",
          "Updated croc to v11.6.0.",
        ],
      },
      {
        type: "paragraph",
        text: "You can run croc-update directly. It finds the croc active on PATH, including ~/go/bin/croc, or installs beside the helper if croc is absent. Use --path to choose an executable. Both commands accept --check without installing anything; croc upgrade is an alias for croc update.",
      },
      {
        type: "paragraph",
        text: "Protected locations need administrator permission. Both commands try ordinary permissions first, then request sudo or doas on Unix or UAC approval on Windows. --yes skips the update confirmation but still requires operating-system authentication.",
      },
      {
        type: "paragraph",
        text: "Package managers retain control of their installations. Homebrew, Nix, Scoop, Chocolatey, Conda, MacPorts, Termux, and system packages get their usual upgrade guidance. On BSD, the updater checks package ownership where install directories overlap. Standalone release binaries and Go bin installations can use the helper directly.",
      },
      {
        type: "paragraph",
        text: "The updater downloads over HTTPS, verifies the release SHA-256 checksum, stages the replacement beside the installed file, and checks its reported version before replacing croc. Windows users must close a running croc first. Neither command replaces a newer version with an older one. The quiet daily check after transfers now points to croc update; executable changes happen through the commands you run and files you confirm.",
      },
      { type: "heading", text: "Clearer SSH sessions" },
      {
        type: "paragraph",
        text: "A v11.4 host could print an invitation and enter the shared shell so quietly that it looked like croc had returned to the ordinary prompt. v11.5 prints an entry message, temporarily sets the terminal title to croc ssh — hosting, restores it afterward, and explains Ctrl-C and Ctrl-].",
      },
      {
        type: "paragraph",
        text: "Detaching says the shell is still running; stopping says it ended. An intentional shutdown closes the shared PTY first and gives guests a short chance to receive the normal SSH exit status. Guests can report that the host ended the session instead of repeatedly reconnecting. Actual transport failures still reconnect.",
      },
      {
        type: "paragraph",
        text: "A client from before croc ssh could mistake an invitation for a receive code and wait at “securing channel...”. A v11.5 host now completes the older authenticated exchange and returns an encrypted message explaining that SSH sharing needs croc v11.4.0 or newer. Other SSH protocol mismatches produce readable encrypted errors in the terminal and browser.",
      },
      {
        type: "paragraph",
        text: "The ordinary relay fallback is tried only after Tailcat authorization succeeds but attaching SSH fails. When a relay limits new admissions, croc explains the limit, waits at least five seconds, and keeps the lower-level cause in debug output.",
      },
      { type: "heading", text: "Installing v11.5" },
      {
        type: "paragraph",
        text: "The official installer places croc and croc-update in the same prefix. You can also extract both from a release archive. Pass -p with a directory such as $HOME/.local/bin for a user installation; package-manager installations should use their manager’s upgrade command.",
      },
      {
        type: "code",
        label: "Install v11.5",
        lines: [
          "$ curl https://getcroc.com | bash",
          "$ croc --version",
          "croc version 11.5.0",
          "$ croc-update --version",
          "croc-update version 11.5.0",
        ],
      },
    ],
  },
  {
    slug: "croc-v11-4-release-update",
    number: "03",
    title: "croc v11.4 shares the terminal",
    category: "Release notes",
    publishedAt: "2026-09-03",
    publishedLabel: "September 3, 2026",
    author: "schollz",
    visual: "ssh-release",
    kind: "update",
    description: "croc v11.4 shares one persistent terminal through separate read/write and read-only invitations, from the CLI or browser.",
    takeaway: "Run croc ssh to host one shell, then share a six-word invitation for read/write or read-only access.",
    blocks: [
      {
        type: "paragraph",
        text: "croc v11.4 can share the terminal where you are working. Run croc ssh, send somebody a six-word invitation, and both of you see the same shell. It starts a temporary collaborative session without enabling the computer’s normal SSH server or arranging accounts and keys. croc send behaves as before.",
      },
      { type: "heading", text: "Sharing a terminal" },
      {
        type: "code",
        label: "Host a shared terminal",
        lines: [
          "$ croc ssh",
          "Shared SSH terminal is ready",
          "  Read/write: CROC_SECRET='...' croc ssh",
          "  Read-only:  CROC_SECRET='...' croc ssh",
          "  Expires:    12h0m0s",
          "  Stop:       Ctrl-C",
          "  Detach:     Ctrl-] (the shared shell keeps running)",
        ],
      },
      {
        type: "paragraph",
        text: "Hosting needs Linux, macOS, FreeBSD, or OpenBSD. Those systems and Windows can join from the CLI. Both invitations enter the same PTY and replay its existing output. Read/write guests can type alongside the attached host. Read-only guests see the same screen, but the host discards their input before it reaches the shell. The codes are independent, so the viewing code does not reveal the typing code.",
      },
      {
        type: "paragraph",
        text: "The host is attached by default. --headless leaves the shell running without occupying the host terminal, --dir sets its starting directory, and --duration changes the twelve-hour lifetime. Any number of people may attach within the host and relay’s practical limits. Guests can detach without ending the shell and automatically reauthenticate after a temporary network loss.",
      },
      { type: "heading", text: "Joining from a browser" },
      {
        type: "paragraph",
        text: "On getcroc.com, choose SSH in the Files / SSH switch and paste either invitation in an evergreen desktop browser. The page shows the authenticated role. Read/write guests can type; read-only input is disabled in the page and independently rejected by the host. Ctrl-C reaches the shared program. Disconnect or Ctrl-] leaves the session.",
      },
      {
        type: "paragraph",
        text: "The browser joins croc sessions; it cannot host a terminal or connect to an arbitrary SSH server. It uses the croc relay and runs PAKE, host-key verification, and the SSH client in WebAssembly. The terminal interface and separate SSH module load when SSH mode is selected. Invitations stay in memory and are excluded from URLs, browser storage, QR codes, logs, and analytics events.",
      },
      { type: "heading", text: "How invitations work" },
      {
        type: "paragraph",
        text: "The first two words of an invitation derive an opaque rendezvous room; the other four are the PAKE secret. Host and guest prove they know the invitation and confirm their shared key without placing the secret on the relay. The authenticated reply carries the exact ephemeral SSH host key, which the client pins without a trust-on-first-use prompt.",
      },
      {
        type: "paragraph",
        text: "Native guests use Tailcat to create an accountless userspace WireGuard network. It starts through DERP and promotes the session to direct UDP when possible. If Tailcat cannot establish SSH, both sides authenticate again and carry the pinned SSH stream over the selected croc relay. No Tailscale account, daemon, public IP, port forwarding, or normal SSH service is needed.",
        links: [
          {
            label: "Tailcat",
            href: "https://github.com/tailscale/tailcat",
          },
        ],
      },
      {
        type: "table",
        caption: "What v11.4 adds",
        headers: ["Part", "New behavior", "Boundary"],
        rows: [
          {
            cells: ["Native CLI", "Host or join one shared terminal", "Hosting needs a PTY-capable Unix platform"],
            href: "https://github.com/schollz/croc/blob/main/src/docs/SSH_SHARING.md",
          },
          {
            cells: ["Browser", "Join read/write or read-only from getcroc.com", "Join-only and croc-relay-only"],
            href: "https://getcroc.com/",
          },
          {
            cells: ["Invitations", "Independent six-word codes for two roles", "Anyone holding a code has its role"],
            href: "https://getcroc.com/blog/share-terminal-with-croc-ssh",
          },
          {
            cells: ["Session", "Detach, reconnect, and replay recent output", "One shared shell, not separate logins"],
            href: "https://github.com/schollz/croc/blob/main/src/docs/SSH_SHARING.md#reconnection",
          },
        ],
      },
      {
        type: "paragraph",
        text: "The field note Share a terminal with croc ssh covers the tmate background, alternatives, protocol, trust model, and risks of sharing a real shell.",
        links: [
          {
            label: "Share a terminal with croc ssh",
            href: "https://getcroc.com/blog/share-terminal-with-croc-ssh",
          },
        ],
      },
      {
        type: "code",
        label: "Install and start sharing",
        lines: [
          "$ curl https://getcroc.com | bash",
          "$ croc --version",
          "croc version 11.4.0",
          "$ croc ssh",
        ],
      },
      {
        type: "paragraph",
        text: "Send the appropriate invitation to somebody with croc or a desktop browser. Everyone joins the same shell, with the access granted by the code they hold.",
      },
    ],
  },
  {
    slug: "share-terminal-with-croc-ssh",
    number: "11",
    title: "Share a terminal with croc ssh",
    category: "Remote terminals",
    publishedAt: "2026-09-03",
    publishedLabel: "September 3, 2026",
    author: "schollz",
    visual: "ssh",
    description: "Run croc ssh to share one terminal, with separate read/write and read-only invitations for CLI and browser guests.",
    takeaway: "croc ssh shares a persistent shell through six-word invitations, without an account, reachable port, or Tailscale setup.",
    blocks: [
      {
        type: "paragraph",
        text: "Sharing a terminal over Zoom gets tedious when I cannot read the screen and we keep copying commands between computers. I wanted another person’s terminal to show the same shell as mine, without arranging an account, public key, reachable port, or VPN. croc ssh, introduced in v11.4, does that with a short invitation.",
      },
      { type: "heading", text: "Joining the terminal" },
      {
        type: "paragraph",
        text: "Run croc ssh on the host. It starts one persistent shell and prints separate six-word invitations: read/write for someone who can type, and read-only for someone who can watch. Guests see the same terminal, including recent output from before they joined.",
      },
      {
        type: "code",
        label: "Start a shared terminal",
        lines: [
          "$ croc ssh",
          "Shared SSH terminal is ready",
          "  Read/write: CROC_SECRET='whoop-flap-alias-mouth-crowd-lived' croc ssh",
          "  Read-only:  CROC_SECRET='help-cause-stage-pork-reset-crowd' croc ssh",
          "  Expires:    12h0m0s",
          "  Stop:       Ctrl-C",
          "  Detach:     Ctrl-] (the shared shell keeps running)",
        ],
      },
      {
        type: "paragraph",
        text: "Send the appropriate line privately. On Unix, CROC_SECRET keeps the words out of the process list; guests can also run croc ssh and paste the code at its prompt. Windows guests can put the code directly after croc ssh.",
      },
      {
        type: "paragraph",
        text: "In a browser, select SSH at getcroc.com and paste either invitation. The full-width terminal shows the authenticated read/write or read-only role. Ctrl-C works as terminal input; Disconnect or Ctrl-] leaves. The terminal code loads only when SSH is selected, and invitations stay in memory, outside URLs and storage. A self-hosted client must use the host’s relay pool and password. Read-only input is disabled in the page and rejected again by the host.",
      },
      {
        type: "paragraph",
        text: "tmate filled this need for years. In July 2025 its maintainer said the public servers were closing permanently. The source is still available and can be self-hosted, but its public meeting point is gone. croc ssh is another option designed to be easy to use and self-host.",
        links: [
          {
            label: "said the public servers were closing permanently",
            href: "https://github.com/tmate-io/tmate/issues/322#issuecomment-3083622301",
          },
          {
            label: "source is still available",
            href: "https://github.com/tmate-io/tmate",
          },
        ],
      },
      {
        type: "table",
        caption: "Several ways to put another person in a terminal",
        headers: ["Tool", "Guest uses", "Meeting point", "Useful distinction"],
        rows: [
          {
            cells: ["croc ssh", "croc or browser", "croc relay; CLI can use direct/DERP", "Six-word read/write and read-only invitations"],
            href: "https://github.com/schollz/croc/blob/5595b7507a27f513e3ea460fdd561ee639642d57/src/docs/SSH_SHARING.md",
            highlight: true,
          },
          {
            cells: ["tmate", "SSH or tmate", "Self-hosted tmate server", "The public tmate.io service has closed"],
            href: "https://github.com/tmate-io/tmate",
          },
          {
            cells: ["Upterm", "SSH", "uptermd", "Public-key authorization and self-hosting"],
            href: "https://github.com/owenthereal/upterm",
          },
          {
            cells: ["sshx", "Web browser", "Hosted sshx service", "Browser-native multi-user terminal and cursors"],
            href: "https://github.com/ekzhang/sshx",
          },
          {
            cells: ["SSH + tmux", "SSH and tmux", "Your SSH service or VPN", "Uses the accounts and network you already manage"],
            href: "https://github.com/tmux/tmux",
          },
        ],
      },
      { type: "heading", text: "How it connects" },
      {
        type: "paragraph",
        text: "Each invitation has six EFF words. The first two derive an opaque room name on the selected croc relay; the other four are the PAKE secret. Host and guest prove they know that secret and confirm a shared key without sending the secret itself. The two invitations are generated independently, so one does not reveal the other.",
        links: [
          {
            label: "PAKE",
            href: "https://getcroc.com/blog/pake-step-by-step",
          },
        ],
      },
      {
        type: "paragraph",
        text: "The guest then sends a fresh Tailcat public key through the encrypted control channel. The host returns connection information, the guest’s role, and an ephemeral SSH host key. That PAKE-authenticated reply lets the guest pin the exact host key, without a trust-on-first-use prompt or a key saved for next time.",
        links: [
          {
            label: "Tailcat",
            href: "https://github.com/tailscale/tailcat",
          },
        ],
      },
      {
        type: "paragraph",
        text: "Tailcat creates an accountless userspace WireGuard network through Tailscale’s DERP infrastructure, then tries direct UDP. It needs no Tailscale account, control plane, daemon, root access, or open inbound port. If it cannot establish SSH, both sides run PAKE again and use the ordinary croc relay as the pinned SSH stream.",
      },
      {
        type: "paragraph",
        text: "The browser joins only croc sessions and uses the ordinary relay path. PAKE, pinned-host-key verification, and SSH run in browser WASM, so croc-web forwards ciphertext. A croc relay sees an opaque room and connection timing; fallback also exposes SSH ciphertext volume and timing. DERP sees similar transport metadata. Neither relay receives the invitation secret, terminal contents, Tailcat authorization, or SSH host key.",
      },
      { type: "heading", text: "The shared shell" },
      {
        type: "paragraph",
        text: "Everyone shares one PTY. The attached host and read/write guests can type; read-only input is discarded before it reaches the shell. Up to 8 MiB of recent output stays in memory for guests who attach later. Ctrl-] detaches without stopping the shell. A disconnected guest reauthenticates and retries for up to two minutes by default.",
      },
      {
        type: "paragraph",
        text: "The host’s Ctrl-C ends the session. A guest’s Ctrl-C goes to the shared terminal and can interrupt the program everyone is using.",
      },
      {
        type: "code",
        label: "A few useful host and guest options",
        lines: [
          "# Keep the shared shell without attaching the host terminal",
          "$ croc ssh --headless",
          "",
          "# Start in a project and expire after thirty minutes",
          "$ croc ssh --dir /path/to/project --duration 30m",
        ],
      },
      {
        type: "paragraph",
        text: "The embedded server accepts interactive terminals only: no remote commands, SFTP, agent forwarding, port forwarding, or arbitrary channels. Hosting works on Linux, macOS, FreeBSD, and OpenBSD. Those systems, Windows, and evergreen desktop browsers can join.",
      },
      {
        type: "paragraph",
        text: "The invitation is a bearer secret. There is no identity screen or way to remove one guest; stopping the host revokes both codes. A read-only guest can still see and record credentials printed on screen. A read/write guest has the host user’s privileges, with no extra sandbox. Use a terminal you can afford to share. The SSH sharing documentation describes the full protocol and trust model.",
        links: [
          {
            label: "SSH sharing documentation",
            href: "https://github.com/schollz/croc/blob/5595b7507a27f513e3ea460fdd561ee639642d57/src/docs/SSH_SHARING.md",
          },
        ],
      },
    ],
  },
  {
    slug: "croc-v11-3-release-update",
    number: "02",
    title: "croc v11.3 goes peer-to-peer",
    category: "Release notes",
    publishedAt: "2026-08-24",
    publishedLabel: "August 24, 2026",
    author: "schollz",
    visual: "derp",
    kind: "update",
    description: "croc v11.3 adds direct WireGuard transfers, with public DERP and croc relays available when a direct route is blocked.",
    takeaway: "Compatible native clients can send directly over WireGuard, stay on DERP when needed, or fall back together to the croc relay.",
    blocks: [
      {
        type: "paragraph",
        text: "The croc command is unchanged: send a file, share the three-word code, and receive it on the other computer. v11.3 changes where the bytes can travel. Compatible native clients try a direct WireGuard connection, use public Tailscale DERP when direct networking is blocked, and coordinate a fallback to croc’s relay if that transport cannot be established.",
      },
      { type: "heading", text: "Choosing a relay" },
      {
        type: "paragraph",
        text: "v11.2 replaced one public relay with three deployments. On a machine’s first send, croc races a small ping against the pool and remembers the first healthy answer. It generates an EFF code whose hash maps to that relay. The receiver and browser repeat the calculation from the code, without adding server details to the phrase. This distributed traffic, but the file bytes still crossed the selected server.",
      },
      {
        type: "paragraph",
        text: "The later v11.2 releases clarified terminal and browser feedback, added browser elapsed time, and deferred update notices until after a transfer. They also rooted filesystem operations inside the receive directory, rejected unsafe or ambiguous paths before writing, validated whole archives before extraction, encrypted local endpoint metadata, and bounded relay admission. The release table below records the sequence.",
      },
      { type: "heading", text: "Direct and relayed paths" },
      {
        type: "paragraph",
        text: "Clients still meet over croc’s control connection and authenticate the three-word code with PAKE. Only after both confirm the expected key do they advertise the new capability and exchange a short-lived Tailcat offer over the encrypted channel. The data route is chosen after authentication.",
      },
      {
        type: "paragraph",
        text: "DERP means Designated Encrypted Relay for Packets. The geographically distributed public DERP network gives each client an outbound meeting connection, usually reachable through routers and firewalls that reject unsolicited inbound connections. Tailcat exchanges possible routes there and carries encrypted packets while testing a direct path.",
        links: [
          {
            label: "public DERP network",
            href: "https://tailscale.com/docs/reference/derp-servers",
          },
          {
            label: "Tailcat",
            href: "https://github.com/tailscale/tailcat",
          },
        ],
      },
      {
        type: "paragraph",
        text: "v11.3 uses Tailcat directly, without a Tailscale account, tailnet, tailscaled daemon, or WireGuard configuration. Croc still handles the code rendezvous, PAKE-authenticated control channel, file protocol, and file-chunk encryption.",
        links: [
          {
            label: "uses Tailcat",
            href: "https://github.com/schollz/croc/blob/v11.3.0/src/tailcattransport/transport_supported.go",
          },
        ],
      },
      {
        type: "paragraph",
        text: "Routers usually translate network addresses, and firewalls admit replies to outbound packets while dropping unexpected inbound traffic. Clients exchange possible local and public UDP addresses over DERP, then send probes toward each other at nearly the same time. The outbound probes create temporary router mappings that replies may cross: UDP hole punching, without manual port forwarding. The NAT traversal explainer covers the details. A successful probe promotes the running session to direct UDP without restarting it. Some NAT combinations, carrier networks, or firewalls that block UDP leave no usable direct route; DERP keeps forwarding the encrypted session.",
        links: [
          {
            label: "NAT traversal explainer",
            href: "https://tailscale.com/blog/how-nat-traversal-works",
          },
        ],
      },
      {
        type: "paragraph",
        text: "Tailcat builds a small userspace Tailscale network with two PAKE-derived node keys. WireGuard protects packets, magicsock discovers endpoints and promotes direct paths, and Tailcat's netstack presents TCP connections to croc. Croc opens a fixed virtual TCP port per configured stream and installs the stream bundle only when every connection is ready. Croc also keeps its own AES-GCM encryption on file chunks, so neither relay network sees their contents.",
        links: [
          {
            label: "WireGuard",
            href: "https://www.wireguard.com/protocol/",
          },
          {
            label: "Tailcat's netstack",
            href: "https://github.com/schollz/croc/blob/v11.3.0/src/tailcattransport/transport_supported.go",
          },
        ],
      },
      {
        type: "paragraph",
        text: "A direct route is peer-to-peer because no relay forwards the file data; servers can still introduce the clients. A DERP route remains relayed. Direct peers can observe each other’s public network endpoint. Tailscale’s direct-versus-relayed distinction describes the same boundary.",
        links: [
          {
            label: "direct-versus-relayed distinction",
            href: "https://tailscale.com/docs/reference/connection-types",
          },
        ],
      },
      {
        type: "table",
        caption: "How a v11.3 CLI-to-CLI transfer can carry file data",
        headers: ["Path", "Where file bytes travel", "Peer-to-peer?", "When it is used"],
        rows: [
          {
            cells: ["Direct WireGuard", "sender ↔ receiver", "Yes", "Preferred after UDP hole punching finds a working route"],
            href: "https://www.wireguard.com/protocol/",
          },
          {
            cells: ["Public DERP", "sender → DERP → receiver", "No", "Encrypted fallback when no direct route is available"],
            href: "https://tailscale.com/docs/reference/derp-servers",
          },
          {
            cells: ["croc relay", "sender → croc relay → receiver", "No", "Compatibility path, or fallback when DERP setup fails"],
            href: "https://github.com/schollz/croc/blob/v11.3.0/src/croc/croc.go",
          },
        ],
      },
      {
        type: "paragraph",
        text: "Removing a relay from the data path can reduce latency, improve throughput, and reduce public relay load. It depends on both networks; a direct route is an opportunity, not a guaranteed speedup.",
      },
      { type: "heading", text: "Compatibility and releases" },
      {
        type: "paragraph",
        text: "The default is --transport auto. Compatible native clients accept the direct/DERP connection together or switch to the croc relay together. Browsers and older clients lack that capability and keep the established relay data path. Stored and local-only transfers retain their existing behavior. DragonFlyBSD and NetBSD also use the relay because Tailcat is unavailable on those targets.",
      },
      {
        type: "code",
        label: "Choose the data transport explicitly",
        lines: [
          "# Require direct/DERP setup; fail instead of using croc relay data ports",
          "$ croc --transport derp send big-file.zip",
          "",
          "# Use croc's established parallel TCP relay data path",
          "$ croc --transport relay send big-file.zip",
        ],
      },
      {
        type: "table",
        caption: "croc v11.2 and v11.3 release sequence",
        headers: ["Release", "Published", "What it brought"],
        rows: [
          {
            cells: ["v11.2.0", "August 18, 2026", "Three public relays, latency-based selection, deterministic code-to-relay mapping, and matching browser support"],
            href: "https://github.com/schollz/croc/releases/tag/v11.2.0",
          },
          {
            cells: ["v11.2.1", "August 18, 2026", "Public-relay corrections, deterministic cancellation tests, and repaired Windows signing"],
            href: "https://github.com/schollz/croc/releases/tag/v11.2.1",
          },
          {
            cells: ["v11.2.2", "August 19, 2026", "Browser link-handling fixes, a security policy, and release-signing maintenance"],
            href: "https://github.com/schollz/croc/releases/tag/v11.2.2",
          },
          {
            cells: ["v11.2.3", "August 20, 2026", "A TCP fix, the current Go toolchain, dependency maintenance, and automated release tagging"],
            href: "https://github.com/schollz/croc/releases/tag/v11.2.3",
          },
          {
            cells: ["v11.2.4", "August 20, 2026", "Deferred update notices, clearer terminal status, improved destination details, and browser fixes"],
            href: "https://github.com/schollz/croc/releases/tag/v11.2.4",
          },
          {
            cells: ["v11.2.5", "August 22, 2026", "Transfer, relay, filesystem, archive, and stored-mode hardening plus browser elapsed time"],
            href: "https://github.com/schollz/croc/releases/tag/v11.2.5",
          },
          {
            cells: ["v11.3.0", "August 24, 2026", "Automatic CLI-to-CLI Tailcat WireGuard transport with direct promotion, DERP fallback, and coordinated croc relay fallback"],
            href: "https://github.com/schollz/croc/releases/tag/v11.3.0",
          },
        ],
      },
      {
        type: "code",
        label: "Install the latest release",
        lines: [
          "$ curl https://getcroc.com | bash",
          "$ croc --version",
          "croc version 11.3.0",
        ],
      },
      {
        type: "paragraph",
        text: "Send and receive as before. A relay can introduce two computers without carrying every byte once they find a working direct route.",
      },
    ],
  },
  {
    slug: "croc-v11-release-update",
    number: "01",
    title: "From croc v10 to v11",
    category: "Release notes",
    publishedAt: "2026-08-16",
    publishedLabel: "August 16, 2026",
    author: "schollz",
    visual: "release",
    kind: "update",
    description: "croc v11 strengthens the handshake and relay limits, adds group stored transfers, and makes browser startup and terminal output clearer.",
    takeaway: "v11 hardens the handshake and failure paths, lets stored links serve a group, and makes the browser ready sooner.",
    blocks: [
      {
        type: "paragraph",
        text: "For anyone coming from v10, the useful v11 update is the work from v11.0.0 through v11.1.1: a stronger handshake, bounded relay state, stored transfers for groups, optional browser analytics, faster code generation, and clearer terminal output. Sending still starts with a file and a short code shared with a terminal or browser receiver.",
      },
      {
        type: "paragraph",
        text: "Current v11 builds generate three words from the EFF short wordlist. They also parse the four-word format from the first v11 builds and legacy custom codes.",
      },
      { type: "heading", text: "Handshake and relay limits" },
      {
        type: "paragraph",
        text: "The PAKE key schedule now gives sender and receiver ordered identities. It binds the traffic key and each role’s separate confirmation tag to the purpose, room, curve, both PAKE messages, protocol version, and fresh salt. Both peers prove they derived the expected result before the channel is ready. Wrong codes, unsupported versions, invalid points, changed transcripts, or swapped roles fail closed; a message from another room, purpose, or session does not belong to this exchange.",
      },
      {
        type: "paragraph",
        text: "Relays allow 128 single-occupant waiting rooms per port by default. Operators can choose another positive limit with --max-rooms-open or CROC_MAX_ROOMS_OPEN. Pending handshakes are capped and have a five-minute deadline; stale deadlines are cleaned up and read deadlines remain active for the entire read. Explicit loopback binding is respected instead of rewriting 127.0.0.1 to 0.0.0.0.",
      },
      {
        type: "paragraph",
        text: "Current three-word codes use the first complete EFF word to choose a relay room and the final two for PAKE, giving room selection more entropy than the old four-byte prefix. Decompression has explicit output limits in the CLI and browser. Temporary-file cleanup stays in memory and accepts only local paths, so a received marker cannot name arbitrary files for deletion.",
      },
      { type: "heading", text: "Stored transfers for a group" },
      {
        type: "paragraph",
        text: "In v11.1.0, the sender can choose a verified-download allowance and a finite lifetime. Recipients share one encrypted browser link or CLI token. An allowance is spent only after a client authenticates every chunk, verifies the completed files, and commits the receive. Opening the link or abandoning a partial download does not consume one.",
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
        text: "The storage service may enforce lower download and expiration limits. Per-transfer locking uses a fixed set of stripes instead of a map that could grow with attacker-chosen IDs. Stored progress clears old text by display width, including Unicode filenames, and v11.1.1 matches its colors to the rest of croc.",
      },
      { type: "heading", text: "Browser and terminal" },
      {
        type: "paragraph",
        text: "The browser asks before loading optional Umami analytics, and rejecting it does not affect transfers. If enabled, analytics excludes codes, link keys, query strings, filenames, and relay settings. You can change the choice in the footer.",
      },
      {
        type: "paragraph",
        text: "v11.1.1 generates a secure three-word EFF code immediately with Web Crypto. WebAssembly waits until the transfer engine is needed and can instantiate as a stream when served with the correct content type. This makes the code field usable sooner, especially on a cold mobile load.",
      },
      {
        type: "paragraph",
        text: "Terminal output uses cyan for active progress, green for completion, yellow for codes and warnings, red for errors, and bold filenames. Styling works in Windows terminals and is limited to real terminals; TERM=dumb, NO_COLOR, and redirected output disable it. Follow-up fixes cover stored progress, Unicode width, and the final success render.",
      },
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
      {
        type: "paragraph",
        text: "The series also refreshed Croc GUI, Swamp, croc-desktop, and F-Droid entries in the app directory; added a favicon; updated default discovery and the temporary relay host; moved the Go module to github.com/schollz/croc/v11; corrected source-build requirements; and advanced the GitHub Actions dependencies for Node setup and container login. Tests were repaired with the protocol, code-phrase, web, and build changes.",
      },
    ],
  },
  {
    slug: "why-croc-works-this-way",
    number: "01",
    title: "Why croc works this way",
    category: "Origins",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "overview",
    description: "Croc began with a turkey documentary I wanted to send to a friend, without asking her to configure a file-transfer system.",
    takeaway: "A live relay and PAKE let two ordinary computers transfer encrypted files without accounts or router configuration.",
    blocks: [
      {
        type: "paragraph",
        text: "I started croc because I had a large PBS documentary about turkeys to send to a friend in another country. She used Windows, disliked the command line, and did not want an afternoon project before watching a movie.",
      },
      {
        type: "paragraph",
        text: "I could upload the file and send a link, explain SSH and port forwarding, or mail a USB drive. I wanted a simpler handoff: run one program, read a short code, and move the file.",
      },
      { type: "heading", text: "A live handoff" },
      {
        type: "paragraph",
        text: "An upload to storage followed by a download makes two separate trips. If they take ten and six minutes, the recipient waits at least sixteen minutes even when both computers were ready. A croc relay overlaps them: the receiver writes earlier blocks while the sender reads later ones. The slower connection still sets the speed, but no complete upload waits in the middle.",
      },
      {
        type: "paragraph",
        text: "Both clients need to be present for that live transfer. Stored mode is a separate option when they cannot be online together.",
      },
      {
        type: "paragraph",
        text: "SSH works well when an SSH server already exists. Two laptops behind home routers, hotel Wi-Fi, or office firewalls usually need more setup. Croc instead makes outbound connections from both clients to a relay. The relay matches them and forwards encrypted traffic; neither person needs an account on the other computer, its public address, or access to the router.",
      },
      { type: "heading", text: "Three words, two interfaces" },
      {
        type: "paragraph",
        text: "The code had to be easy to tell another person. Three words work for that, but I would not use them directly to encrypt gigabytes of data. Croc uses password-authenticated key exchange, or PAKE: both computers combine the phrase with an exchange of public messages and independently derive the same fresh, strong session key. The phrase does not encrypt the file directly, and the key never passes through the relay. File contents, names, and metadata stay encrypted between the endpoints.",
      },
      {
        type: "paragraph",
        text: "The first interface was a command. I later added a browser client so the recipient would not have to become a terminal user. A browser and the CLI can take part in the same transfer, agreeing only on the code. Multiple files can travel together without an account or a permanent link.",
      },
      {
        type: "paragraph",
        text: "Relays and PAKE already existed, and Go could build for several operating systems. The work was arranging them so the complicated part came after the simple command.",
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
        text: "That is still my test for croc: choose the file, tell my friend three words, and get back to the turkey documentary.",
      },
    ],
  },
  {
    slug: "how-croc-moves-a-file",
    number: "02",
    title: "The relay is not a waiting room",
    category: "How it works",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "relay",
    description: "The relay forwards encrypted bytes as they arrive, so the receiver can start before the sender finishes.",
    takeaway: "A normal relay transfer is a live handoff: both clients stay online while encrypted bytes pass between them.",
    blocks: [
      {
        type: "paragraph",
        text: "A croc relay forwards encrypted bytes as they arrive. The receiver can start writing the file while the sender is still reading later blocks. Unlike an ordinary cloud upload followed by a download, the whole file does not have to reach the server first.",
      },
      {
        type: "paragraph",
        text: "If my upload is 20 megabits per second and the receiver’s download is 80, the transfer is still limited by my 20-megabit connection. Overlapping the two sides avoids a complete upload followed by a complete download; it does not make the slower connection faster.",
      },
      {
        type: "paragraph",
        text: "Both clients must stay open until the receiver verifies the files. Stored mode is the separate option when their schedules do not overlap.",
      },
      { type: "heading", text: "Getting through the router" },
      {
        type: "paragraph",
        text: "Home routers, office firewalls, café Wi-Fi, and mobile networks usually prevent unsolicited connections from the public internet. Both clients can instead make outbound connections to a known relay, which joins the matching connections without port forwarding.",
      },
      {
        type: "paragraph",
        text: "The relay can observe connection times and byte volume. File names, metadata, and contents are encrypted with a key it does not have.",
      },
      {
        type: "code",
        label: "A live transfer",
        lines: [
          "$ croc send field-notes.pdf",
          "Code is: river-cloud-daisy",
          "",
          "$ croc",
          "Enter receive code: river-cloud-daisy",
        ],
      },
      { type: "heading", text: "The browser uses the same protocol" },
      {
        type: "paragraph",
        text: "Croc’s protocol code runs in the browser as WebAssembly. A same-origin WebSocket bridge carries its connections to the relay. Keeping the protocol shared lets a browser send to a terminal or a terminal send to a browser; neither person has to choose the other’s interface.",
      },
    ],
  },
  {
    slug: "what-four-word-code-does",
    number: "03",
    title: "What the three words are doing",
    category: "Security",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "code",
    description: "Three words help croc clients meet and authenticate a fresh key. They still need to be kept private while the transfer is live.",
    takeaway: "The words authenticate a fresh key exchange; they do not directly encrypt the file.",
    blocks: [
      {
        type: "paragraph",
        text: "A croc code is three ordinary words joined by hyphens. I chose words because they are easier to copy, dictate, or type on a small keyboard than a string of punctuation. A missing word is also easier to notice.",
      },
      {
        type: "paragraph",
        text: "The code helps the clients meet in the same transfer and gives them a shared secret. It does not directly encrypt the file.",
      },
      { type: "heading", text: "From words to a key" },
      {
        type: "paragraph",
        text: "Croc uses password-authenticated key exchange, or PAKE. Each computer combines the phrase with fresh private randomness and exchanges public messages. Matching phrases produce the same strong session key, fresh for this transfer. The key never crosses the network. A wrong phrase fails the checks before croc treats the connection as secure.",
      },
      {
        type: "paragraph",
        text: "Someone recording the public exchange at the relay learns neither the session key nor a convenient offline test for guessing the phrase later. The words are something people can move between devices; the derived key does the encryption.",
      },
      { type: "heading", text: "Keep the live code private" },
      {
        type: "paragraph",
        text: "Someone who gets the words before the intended receiver can try to join the transfer. Share the code as carefully as the file: privately when the file is private, and with the intended person. Copy all three words in order. A QR code is the same secret in another form. Let croc generate a fresh phrase unless there is a good reason to choose one.",
      },
      {
        type: "paragraph",
        text: "A longer random string would carry more entropy, but it would also be harder to dictate and easier to truncate. I do not want an awkward generated code to encourage a permanent, memorable croc password. PAKE lets the visible code stay short without treating three words as a full encryption key.",
      },
    ],
  },
  {
    slug: "pake-step-by-step",
    number: "04",
    title: "PAKE, step by step",
    category: "Security",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "handshake",
    description: "How croc turns a shared phrase into a fresh key, checks both peers, and keeps the phrase and key off the network.",
    takeaway: "PAKE authenticates the shared phrase; croc derives and confirms a fresh traffic key before opening the file channels.",
    blocks: [
      {
        type: "paragraph",
        text: "Two people know the same short phrase. Their computers need a strong encryption key without sending either the phrase or the key to each other. Password-authenticated key exchange, or PAKE, lets them make one.",
      },
      {
        type: "paragraph",
        text: "The phrase authenticates the exchange. Both peers combine the phrase with fresh private randomness, exchange two public curve points, and independently reach the same secret point. The private random values disappear after the exchange. Fresh randomness keeps a reused phrase from producing one permanent key. Recording the public messages should reveal neither the phrase nor the session key, and should not provide a simple offline test for later guesses. An ordinary password-encrypted file does not give all those guarantees.",
      },
      { type: "heading", text: "The exchange" },
      {
        type: "paragraph",
        text: "The receiver, party A, chooses a fresh 32-byte random value a. It combines a with the curve’s base point G, then masks that point with the password and a fixed point U to produce X.",
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
        text: "Only X is serialized. The password, a, intermediate points, and eventual key stay inside the PAKE participant. The sender checks that X is a valid point on the selected curve. The library hard-codes valid points U and V; letting applications choose them could introduce a trapdoor if somebody knew their discrete logarithms.",
      },
      {
        type: "paragraph",
        text: "The sender, party B, chooses fresh randomness b and uses V to produce Y. Knowing the phrase lets B remove the password mask from X and multiply the remaining point by b, producing the shared point Z.",
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
      {
        type: "paragraph",
        text: "A validates Y, removes its V-based mask, and multiplies by a. Matching phrases lead both peers to G·a·b, without sending a, b, or Z. Different phrases leave different results because the masks do not cancel.",
      },
      {
        type: "code",
        label: "The same destination from the other side",
        lines: ["Zᴬ = a·(Y − V·password) = G·a·b", "Zᴬ = Zᴮ"],
      },
      { type: "heading", text: "Deriving and confirming the key" },
      {
        type: "paragraph",
        text: "The PAKE library produces a 32-byte shared secret from the password, X, Y, and Z. Croc does not use it directly as the file key. It feeds that secret, a fresh 32-byte salt, and a framed transcript into key derivation. The transcript records the protocol version, purpose, room, curve, ordered identities, and exact X and Y messages.",
      },
      {
        type: "paragraph",
        text: "This ties the result to this room and purpose, prevents the roles from being swapped unnoticed, and makes both peers agree on the exact exchange. A valid exchange should not be reusable in a different conversation. The salt adds fresh context. Key derivation produces a traffic-encryption key and separate confirmation values for the two roles, keeping proof of the key separate from file encryption.",
      },
      {
        type: "paragraph",
        text: "The receiver sends its confirmation tag. The sender checks it in constant time and returns its own role-specific tag, which the receiver checks. Croc opens the encrypted file channels only after both checks pass. A wrong phrase, changed transcript, swapped role, invalid point, unsupported version, duplicate message, or bad confirmation stops the handshake.",
      },
      { type: "heading", text: "What the relay sees" },
      {
        type: "paragraph",
        text: "The relay carries X, Y, the curve choice, salt, and confirmation messages. It can see timing and byte volume. It never receives the phrase, a, b, Z, the traffic key, or readable file names, metadata, and contents.",
      },
      {
        type: "paragraph",
        text: "The code still needs to stay private. Someone who obtains it before the intended receiver can try to join as that receiver. PAKE cannot repair a compromised computer or retrieve a code shared in the wrong conversation. Share it through a channel appropriate for the file, generate a fresh code for a new transfer, and stop if PAKE or key confirmation fails. A QR code carries the same secret as the words.",
      },
    ],
  },
  {
    slug: "send-file-from-browser",
    number: "05",
    title: "Send a file without installing anything",
    category: "Start here",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "browser",
    description: "Choose files, share the code, and keep the browser open until the receiver has verified the transfer.",
    takeaway: "A direct browser transfer is live: both pages must stay open until the files are received and verified.",
    blocks: [
      {
        type: "paragraph",
        text: "Someone receiving one photograph should not have to install croc first. The browser client joins the same transfer from a phone or computer without setting up another program.",
      },
      { type: "heading", text: "Send, share, receive" },
      {
        type: "paragraph",
        text: "Choose Send and select one or several files. The page shows the count and total size. Selecting files does not upload them: in direct mode, bytes move after you press Send and a receiver joins with the code.",
      },
      {
        type: "paragraph",
        text: "Share the generated three-word code privately, or show its QR code to a nearby device. I added the QR version mostly for laptop-to-phone transfers; scanning is nicer than typing the words on a phone.",
      },
      {
        type: "paragraph",
        text: "The receiver chooses Receive, enters the code, and reviews names and sizes before accepting. Some browsers let them choose a destination folder; others use ordinary downloads. Allow the destination prompt, or use the downloads fallback if the browser blocks that choice.",
      },
      {
        type: "paragraph",
        text: "Keep both pages awake until the transfer finishes and reports verification. The sending tab is one end of the transfer, so closing it stops the send. Rate and ETA are estimates. Use the CLI for folders, exclusions, pipes, and resumable workflows.",
      },
      { type: "heading", text: "What the relay sees" },
      {
        type: "paragraph",
        text: "The security-sensitive croc protocol code runs in WebAssembly. Metadata and file contents are encrypted before the relay carries them. The relay handles connections and can observe timing and byte volume, but does not receive readable files.",
      },
    ],
  },
  {
    slug: "browser-meets-terminal",
    number: "06",
    title: "A browser and a terminal can share the same file",
    category: "Field guide",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "bridge",
    description: "The browser and CLI can join the same croc transfer, so each person can use the interface that suits their device.",
    takeaway: "Share one croc code between a browser and terminal; neither person has to change interfaces.",
    blocks: [
      {
        type: "paragraph",
        text: "The browser feature I wanted most was sending files to a terminal. A terminal is convenient when it is already open; a browser is convenient when installing a program would become the whole task. The web client uses protocol code from croc, compiled for the browser, so both can join the same transfer.",
      },
      { type: "heading", text: "Terminal to browser" },
      {
        type: "paragraph",
        text: "Send a file or folder from the CLI and share its code. The receiver opens getcroc.com, chooses Receive, pastes the code, reviews the offer, and saves it. Add --qr for a receive link a phone can scan.",
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
        text: "Choose files in the browser and share its code. On the receiving computer, run croc with no arguments and paste the phrase at the prompt. I prefer that to a command-line argument, which can appear in shell history or a process list.",
      },
      {
        type: "code",
        label: "Receive without exposing the phrase as an argument",
        lines: ["$ croc", "Enter receive code: flame-ferry-tiger"],
      },
      {
        type: "paragraph",
        text: "The browser suits a no-install handoff, a phone scanning a QR code, and reviewing files before acceptance. The CLI suits folders, exclusions, scripts, pipes, custom relays, long-running jobs, and workflows where automation or resume behavior matters.",
      },
      {
        type: "paragraph",
        text: "A browser can send several files to an ordinary CLI receiver. It can also receive CLI folders into a chosen directory where the browser supports that. When self-hosting, use the same private relay settings on both sides.",
      },
    ],
  },
  {
    slug: "stored-transfer-one-download",
    number: "07",
    title: "When both computers cannot be online at once",
    category: "Stored transfers",
    publishedAt: "2026-08-08",
    publishedLabel: "August 8, 2026",
    author: "schollz",
    visual: "stored",
    description: "Stored mode lets the sender finish now and the receiver download later, within a chosen lifetime and download limit.",
    takeaway: "Stored mode encrypts locally and keeps ciphertext until its download allowance is used, its lifetime ends, or the sender revokes it.",
    blocks: [
      {
        type: "paragraph",
        text: "A live croc transfer needs both computers online. That is simple until the other person is asleep, on a plane, or not opening their laptop until tomorrow. Stored mode lets the sender finish now and the receiver arrive later.",
      },
      {
        type: "paragraph",
        text: "When storage is enabled, choose Store in the Send panel and select a finite lifetime. The browser encrypts file names, metadata, and chunks before uploading. The service keeps ciphertext and returns a browser link and CLI token. Live transfer stays the default; when both peers are present, it avoids storing a copy.",
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
      { type: "heading", text: "The link contains the key" },
      {
        type: "paragraph",
        text: "A browser link looks like /s/id#v1.key. Browsers omit the fragment after # from HTTP requests, so the server receives the opaque ID while the web client reads the key locally. This keeps the key out of ordinary server and proxy request logs. The complete link is still a secret: anyone with it can decrypt the manifest and claim an allowed download. Share the link or CLI token privately.",
      },
      { type: "heading", text: "When the transfer ends" },
      {
        type: "paragraph",
        text: "Opening the link does not spend an allowance. The receiver claims the transfer, authenticates its chunks, verifies the finished files, and commits the download. Only then does the service decrement the allowance. Browser previews, refreshes, and failed downloads do not consume it.",
      },
      {
        type: "paragraph",
        text: "The ciphertext is deleted after the final allowed verified commit, the selected lifetime expires, or the sender revokes the transfer. The lifetime starts when upload completes. Keep the sender’s revoke receipt until the transfer is consumed or expired.",
      },
      {
        type: "paragraph",
        text: "Storage reveals upload timing, ciphertext size, transfer totals, and connection metadata to the service, but no readable names or contents. An encrypted copy still exists on disk until the transfer ends. I use Direct when both computers are online and Store when the difficult part is the calendar.",
      },
    ],
  },
  {
    slug: "share-stored-file-with-group",
    number: "08",
    title: "Send one file to a group, on their schedule",
    category: "Stored transfers",
    publishedAt: "2026-08-11",
    publishedLabel: "August 11, 2026",
    author: "schollz",
    visual: "stored",
    description: "Upload one encrypted file for a group, then choose how many downloads can finish and when the transfer expires.",
    takeaway: "Allow enough verified downloads for the group and enough time for the last recipient to finish.",
    blocks: [
      {
        type: "paragraph",
        text: "Sending the same report to five people in three time zones should not mean arranging five appointments with their laptops. Stored mode lets me upload one encrypted copy and share one secret link. Two settings decide how long that handoff stays available: completed downloads and expiration.",
      },
      { type: "heading", text: "Downloads and expiration" },
      {
        type: "paragraph",
        text: "Set --store-downloads to at least the number of people who need the file. The service may cap that allowance; asking for more than its configured maximum returns an error. Opening the link, reading the manifest, refreshing, or abandoning an incomplete receive does not use a download. A client must authenticate the chunks, verify every finished file, and commit the receive first.",
      },
      {
        type: "paragraph",
        text: "The allowance counts successful receives, not unique people. One person can download twice, and anyone with the complete link can use it. A small buffer may help if repeated downloads are acceptable, but share the link privately.",
      },
      {
        type: "paragraph",
        text: "Set --store-expiration to a finite lifetime from successful upload completion. It accepts whole minutes, hours, days, or weeks: 90m, 12h, 3d, or 2w. Think about when the last recipient can finish. The service may enforce a shorter maximum, so use the actual expiration printed after upload.",
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
        text: "This asks for five completed receives and three days of storage. The client encrypts names, metadata, and contents before uploading. Once finalized, the accepted lifetime starts and the sender sees the absolute expiration, a browser link, and a CLI token. Keep the transfer ID until the group is finished so you can revoke it if needed.",
      },
      { type: "heading", text: "Sharing the link" },
      {
        type: "paragraph",
        text: "In the browser’s Send panel, switch Direct to Store. Choose a whole-number lifetime and its minutes, hours, days, or weeks unit, then set Verified downloads for the group. The controls follow the storage server’s published limits. The share card appears after the encrypted upload succeeds and shows the expiration returned by the server.",
      },
      {
        type: "paragraph",
        text: "Send the browser link or CLI token through a private channel. Both contain the decryption key; the service stores ciphertext rather than readable names or contents. There are no accounts or invitation lists. Someone who receives a forwarded link can decrypt the file and consume an allowance.",
      },
      {
        type: "paragraph",
        text: "The transfer ends when the final allowed download commits, the accepted lifetime expires, or the sender revokes it, whichever happens first. I use Direct when everyone is present and Store when the handoff has to wait for their calendars.",
      },
    ],
  },
  {
    slug: "compare-file-transfer-tools",
    number: "09",
    title: "40 ways to send a file",
    category: "Field guide",
    publishedAt: "2026-08-12",
    publishedLabel: "August 12, 2026",
    author: "schollz",
    visual: "bridge",
    description: "Forty file-transfer tools compared by resume support, accounts, browser and CLI access, encryption, availability, and transfer routes.",
    takeaway: "Among the available, no-account tools surveyed, croc alone combines resumable CLI transfers with all three browser/CLI routes.",
    blocks: [
      {
        type: "paragraph",
        text: "In 2017 I wanted to send a one-gigabyte documentary about turkeys to a friend in another country. She used Windows, disliked terminals, and did not want to configure a router before watching a movie. I compared seven ways to send it. That survey eventually became croc.",
      },
      {
        type: "paragraph",
        text: "Nine years later, I found 40 tools solving different versions of the problem: a live transfer, a synchronized folder, two browser tabs, or a stored link for somebody who will arrive tomorrow. These tables compare croc with 39 alternatives. I built croc, so I know its row best; AirDrop, Syncthing, OnionShare, and stored-link services have different jobs.",
      },
      { type: "heading", text: "Reading the tables" },
      {
        type: "paragraph",
        text: "I checked official sites, documentation, and repositories on August 23, 2026. A full circle means documented support without an important limitation. A half circle means a platform, mode, third-party adapter, or other caveat applies. An empty circle means I could not find documented support. Resume means restarting an interrupted transfer without beginning at byte zero, rather than retrying packets during one session.",
      },
      {
        type: "paragraph",
        text: "Browser means the browser handles the file, not just a daemon’s settings page. PairDrop’s shell helper hands files to a browser, so that route is partial. App means a native GUI or mobile application. Account support distinguishes no account, an optional account, and a required account. Availability includes hosted services and installable source, with approximate lifetimes for discontinued projects.",
      },
      {
        type: "paragraph",
        text: "I did not benchmark bandwidth here. Geography, NATs, browser limits, providers, and endpoint speeds can outweigh the implementation. The transfer-route table shows how each design moves its bytes.",
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
          { cells: ["derphole", "●", "●", "○", "○", "○", "●", "○"], href: "https://github.com/shayne/derphole" },
          { cells: ["wormhole-william", "●", "●", "○", "○", "○", "●", "○"], href: "https://github.com/psanford/wormhole-william" },
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
          { cells: ["Floe", "●", "●", "○", "●", "●", "●", "◐ Windows desktop beta"], href: "https://github.com/jannskiee/floe" },
          { cells: ["AirPipe", "●", "●", "○", "●", "●", "●", "○"], href: "https://github.com/Sanyam-G/Airpipe" },
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
        text: "The wormhole names need untangling. Magic Wormhole installs the wormhole command; wormhole-william is a compatible Go CLI. WebWormhole is a separate WebRTC project. Wormhole.app is a browser service without a documented CLI. A newer native app also named Wormhole speaks the Magic Wormhole protocol but is unrelated to Wormhole.app.",
      },
      {
        type: "paragraph",
        text: "Croc, WebWormhole, Floe, and AirPipe directly support browser ↔ browser, browser ↔ CLI, and CLI ↔ CLI transfers. MEGA and Filemail connect browsers and terminals through stored cloud files; Firefox Send could do that through ffsend. Partial routes include LocalSend’s third-party CLI, OpenDrop’s experimental AirDrop implementation, and WeTransfer’s unofficial transferwee client. The app column also covers native tools such as LocalSend and AirDrop.",
      },
      {
        type: "paragraph",
        text: "Resume support needs a closer look. Magic Wormhole has an open request for restartable transfers. Iroh Sendme resumes verified downloads, rsync retains partial data when asked, and Syncthing retains temporary blocks. Taildrop has receiving-platform exceptions; MEGA differs between browser and native clients.",
      },
      { type: "heading", text: "Encryption and routes" },
      {
        type: "paragraph",
        text: "TLS to an upload service protects the connection, but the service can ordinarily process the file. End-to-end encryption means the intermediary should not have the key. WebRTC encrypts the peer connection, and some tools add password authentication. These are documented designs, not an independent security audit.",
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
          { cells: ["derphole", "Application E2EE with token-derived AEAD", "Live DERP relay with promotion to direct encrypted UDP"], href: "https://github.com/shayne/derphole" },
          { cells: ["wormhole-william", "Application E2EE with PAKE", "Live direct or transit relay"], href: "https://github.com/psanford/wormhole-william" },
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
          { cells: ["Floe", "WebRTC/DTLS with fingerprint verification", "Peer-to-peer or TURN relay"], href: "https://github.com/jannskiee/floe" },
          { cells: ["AirPipe", "NaCl secretbox E2EE over WebRTC/DTLS", "Live peer-to-peer or encrypted relay; optional 10-minute mailbox"], href: "https://github.com/Sanyam-G/Airpipe" },
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
        text: "A live peer-to-peer or relay transfer usually needs the sender online. A stored link adds a copy and a trust relationship, but the recipient can arrive later. Syncthing and rclone are built for repeated movement rather than one-time handoffs.",
      },
      { type: "heading", text: "Choosing a tool" },
      {
        type: "paragraph",
        text: "Availability matters too. Firefox Send began as a Mozilla Test Pilot experiment in August 2017, became standalone in March 2019, and closed in September 2020: about three years in public, or 18 months as a standalone product. ZeroTier Toss lasted about seven years before archival in April 2024; Peer Copy, a decentralized libp2p experiment, lasted about four before archival in April 2025. Snapdrop Classic and ShareDrop Classic became LimeWire services in 2025, while their roughly nine- and eleven-year-old repositories remained available for self-hosting. Open source does not guarantee that the hosted service will stay open.",
      },
      {
        type: "list",
        items: [
          "Keep a folder synchronized: Syncthing. Taildrop fits devices in one Tailscale account; rclone fits cloud storage.",
          "Send between people on one LAN: LocalSend or PairDrop. AirDrop fits Apple devices; Quick Share covers much of Android, ChromeOS, and Windows.",
          "Let the recipient arrive tomorrow: croc stored mode, Wormhole.app, or a stored-link provider whose limits, retention, account policy, and trust model fit the file.",
          "Use two terminals: croc, Magic Wormhole, Portal, or iroh Sendme. Use rsync when SSH access already exists.",
          "Send from a terminal to a browser: qrcp on a LAN, croc across networks, or OnionShare when Tor anonymity matters.",
          "Install nothing: PairDrop, ToffeeShare, Wormhole.app, Blaze, or another browser tool, with a live sender keeping its tab awake.",
        ],
      },
      {
        type: "paragraph",
        text: "Croc began as a terminal tool alongside Magic Wormhole, Toss, and scp. Getcroc.com now sends to and receives from browsers and the ordinary CLI. Normal transfers are live, relayed, and end-to-end encrypted. Stored mode encrypts in the client and temporarily retains ciphertext for later recipients.",
      },
      {
        type: "paragraph",
        text: "Among the available, no-account tools in this survey, croc alone combines resumable CLI transfers with all three browser/CLI routes. That does not replace Syncthing for a mirrored folder, AirDrop between iPhones, or OnionShare’s Tor anonymity. It makes croc a useful option when I know little about the other computer, its operating system, its owner’s preference for terminals, or the reliability of its Wi-Fi.",
      },
    ],
  },
  {
    slug: "croc-cli-speed-comparison",
    number: "10",
    title: "How fast is croc?",
    category: "Benchmarks",
    publishedAt: "2026-08-23",
    publishedLabel: "August 23, 2026",
    author: "schollz",
    visual: "bridge",
    description: "I timed a Git clone and a 317.52 MB audio file from my Mac to Toronto. croc sent the audio in 8.52 seconds.",
    takeaway: "croc sent audio.wav in 8.52 seconds and the Git clone in 4.42 seconds.",
    blocks: [
      {
        type: "paragraph",
        text: "I compared command-line file-transfer tools with two workloads: a Git repository containing hundreds of files, and one large audio file. Both went from my Mac to Toronto, using each tool’s default transport and compression settings.",
      },
      { type: "heading", text: "Sending multiple files" },
      {
        type: "paragraph",
        text: "On November 1, 2026, I sent the same clone from my Apple Silicon Mac running macOS 26.5.2 to a DigitalOcean server in Toronto running Ubuntu 24.04.4 LTS on x86-64. I cloned it once, then left it untouched for every transfer.",
      },
      {
        type: "code",
        label: "The test folder",
        lines: [
          "$ git clone https://github.com/schollz/croc.git croc",
          "$ git -C croc rev-parse HEAD",
          "cc781806baaa17d29d94209eecf0c85bc178d9e1",
        ],
      },
      {
        type: "paragraph",
        text: "That gave me 532 files totaling 20,708,906 bytes (20.71 MB), including .git and 2,719 commits. I installed no dependencies and built nothing inside the clone.",
      },
      {
        type: "paragraph",
        text: "The timer started immediately before the sender launched and stopped when the received folder was ready to use. Scanning, hashing, packing, connecting, and unpacking all counted. SSH was only there to start the commands; each tool carried its own files. I ran each transfer three times in shuffled rounds and used the middle time.",
      },
      {
        type: "table",
        caption: "Multiple files · Mac to Toronto",
        headers: ["Tool", "Time", "Effective rate"],
        rows: [
          {
            cells: ["croc", "4.42 s", "4.69 MB/s"],
            href: "https://github.com/schollz/croc",
            highlight: true,
          },
          {
            cells: ["derphole", "4.76 s", "4.35 MB/s"],
            href: "https://github.com/shayne/derphole",
          },
          {
            cells: ["iroh Sendme / DashBeam CLI", "6.37 s", "3.25 MB/s"],
            href: "https://github.com/n0-computer/sendme",
          },
          {
            cells: ["Magic Wormhole", "9.14 s", "2.27 MB/s"],
            href: "https://github.com/magic-wormhole/magic-wormhole",
          },
          {
            cells: ["wormhole-william", "10.69 s", "1.94 MB/s"],
            href: "https://github.com/psanford/wormhole-william",
          },
          {
            cells: ["AirPipe", "15.21 s", "1.36 MB/s"],
            href: "https://github.com/Sanyam-G/Airpipe",
          },
          {
            cells: ["Floe", "57.84 s", "0.36 MB/s"],
            href: "https://github.com/jannskiee/floe",
          },
          {
            cells: ["WebWormhole", "Folder transfer unsupported", "—"],
            href: "https://github.com/saljam/webwormhole",
          },
        ],
      },
      {
        type: "paragraph",
        text: "Effective rate is the original folder size divided by the total time, in decimal MB/s. It includes the wait at both ends, so it describes sending this folder rather than the connection’s raw bandwidth.",
      },
      {
        type: "paragraph",
        text: "croc sent the clone in 4.42 seconds; derphole took 4.76 seconds.",
      },
      {
        type: "paragraph",
        text: "croc groups files into bounded transfer units and flushes staged files concurrently. Every file still gets checked and flushed before publication.",
      },
      {
        type: "paragraph",
        text: "Croc used its TCP relay for this folder; forcing the DERP/direct path was slower. AirPipe’s automatic ZIP creation and extraction both counted toward its time. WebWormhole could not send the directory, so it has no time or rate. DashBeam recommends Sendme for CLI transfers, so they share one row.",
        links: [
          {
            label: "DashBeam",
            href: "https://www.dashbeam.net/en/downloads",
          },
        ],
      },
      {
        type: "paragraph",
        text: "Every completed transfer matched the SHA-256 manifest, including .git, and passed the commit check and git fsck --full. Those checks ran after the timer stopped. The benchmark record has the commands, exact builds, individual runs, and logs.",
        links: [
          {
            label: "benchmark record",
            href: "https://github.com/schollz/croc/blob/main/src/docs/CLI_FOLDER_BENCHMARK.md",
          },
        ],
      },
      { type: "heading", text: "Sending one large file" },
      {
        type: "paragraph",
        text: "For the single-file comparison, I sent audio.wav between the same two computers on November 1, 2026. The file was 317,520,080 bytes (317.52 MB). The clock again started with the sender and stopped when the received file was ready to use.",
      },
      {
        type: "paragraph",
        text: "Each time below is the middle of three successful transfers. Every receiver started in an empty directory, and every completed file matched the source SHA-256. Those hash checks ran after the timer stopped.",
      },
      {
        type: "table",
        caption: "Single large file · Mac to Toronto",
        headers: ["Tool", "Time", "Effective rate"],
        rows: [
          {
            cells: ["croc", "8.52 s", "37.27 MB/s"],
            href: "https://github.com/schollz/croc",
            highlight: true,
          },
          {
            cells: ["derphole", "10.65 s", "29.80 MB/s"],
            href: "https://github.com/shayne/derphole",
          },
          {
            cells: ["wormhole-william", "126.81 s", "2.50 MB/s"],
            href: "https://github.com/psanford/wormhole-william",
          },
          {
            cells: ["iroh Sendme / DashBeam CLI", "129.72 s", "2.45 MB/s"],
            href: "https://github.com/n0-computer/sendme",
          },
          {
            cells: ["Magic Wormhole", "150.36 s", "2.11 MB/s"],
            href: "https://github.com/magic-wormhole/magic-wormhole",
          },
          {
            cells: ["Floe", "190.69 s", "1.67 MB/s"],
            href: "https://github.com/jannskiee/floe",
          },
          {
            cells: ["AirPipe", "200.26 s", "1.59 MB/s"],
            href: "https://github.com/Sanyam-G/Airpipe",
          },
          {
            cells: ["WebWormhole", "Transfer failed", "—"],
            href: "https://github.com/saljam/webwormhole",
          },
        ],
      },
      {
        type: "paragraph",
        text: "croc took 8.52 seconds, with derphole next at 10.65 seconds. That is about 20% less time for this file. The compression check samples across a large file, so a quiet opening does not select compression for an audio recording that barely shrinks.",
      },
      {
        type: "paragraph",
        text: "WebWormhole received matching bytes but exited with a connection-close error, so it has no time or rate. The audio benchmark record contains the exact builds, commands, individual runs, and the earlier server-to-server audio figures.",
        links: [
          {
            label: "audio benchmark record",
            href: "https://github.com/schollz/croc/blob/main/src/docs/CLI_AUDIO_BENCHMARK.md",
          },
        ],
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
