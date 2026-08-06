# croc web

The croc web client is a React/Vite app that sends and receives files with ordinary
`croc` CLI peers. Its production build and WebAssembly runtime are embedded in
the separate `croc-web` binary. `croc-web` serves the website, runtime
configuration, health check, and opaque WebSocket-to-TCP bridge from one HTTP
address. File metadata and contents remain encrypted between the browser and
the other croc client.

When the server is started with `--store-dir`, the UI also offers an explicit
**Store for 24 hours** mode. It encrypts file names, metadata, and 4 MiB chunks
in the browser, uploads only ciphertext, and produces both a browser link and a
CLI token. The transfer is deleted after one fully verified download or 24
hours, whichever comes first. The normal direct-transfer mode remains the
default.

Both send modes can display a QR code for the browser receive URL. Direct-mode
codes open the receive page with the croc code filled in and connecting, while
stored-mode codes contain the complete encrypted share link.

The security-sensitive protocol operations are compiled from this repository's
Go packages into WebAssembly:

- croc PAKE for both relay and peer handshakes
- identity- and session-bound HKDF plus mutual confirmation for peer channels
- PBKDF2 for the legacy-compatible relay hop and AES-GCM encryption
- raw DEFLATE compression
- xxhash verification
- Orchard Street four-word code generation and compatibility parsing

Active sends and receives show total and per-file progress, measured bytes per
second, and an ETA calculated with `arrival-time`.

## Local development

From this directory:

```bash
npm install
npm run dev:stack
```

This builds and embeds the complete client, then runs:

```bash
croc-web localhost:5173
```

The local shortcut binds directly to `localhost:5173`; both the website and
WebSocket relay are available there. For frontend hot reloading, run:

```bash
npm run dev:hot
```

Useful checks:

```bash
npm test
npm run test:e2e
npm run typecheck
npm run build
npm run embed
make build-web
go test ./...
```

`npm run embed` builds the WASM and Vite client and copies the result into
the ignored `src/webassets/dist` directory, where Go's embed package includes
it only in `croc-web`. The generated files are not committed. A deployed
`croc-web` binary does not need this directory or any external static files.

The Playwright suite builds real `croc` and `croc-web` binaries, starts an
isolated local croc relay and unified embedded server on temporary ports, then
verifies CLI → Web, Web → CLI, Web → Web, CLI stored → Web, and Web stored →
CLI transfers byte-for-byte. Install its browser once with
`npx playwright install chromium`. Test processes use an isolated
`CROC_CONFIG_DIR` and storage directory and do not read or change remembered
croc settings.

## Custom relay

The server fixes one upstream host and allowlists every TCP port so `/ws`
cannot be used as an arbitrary network proxy:

```bash
croc-web --pass YOUR_RELAY_PASSWORD \
  --bind 127.0.0.1:9014 \
  --relay relay.example.com \
  --ports 9009,9010,9011,9012,9013,9014,9015,9016,9017 \
  files.example.com
```

The server injects the selected relay address and password as browser defaults
through `/config.js`. Users can still override them from the advanced settings
panel.

The unified server exposes:

- `GET /` and embedded static client assets; curl and wget receive the
  installer script at `/` while browsers receive the web client
- `GET /config.js`
- `GET /healthz`
- `GET /ws?port=<allowlisted-port>` upgraded to a binary WebSocket
- `/api/v1/store/transfers` and its descendants when `--store-dir` is set

## Production topology

Run `croc-web` behind an HTTPS reverse proxy:

```bash
croc-web --bind 127.0.0.1:9014 getcroc.com
```

Optional Umami analytics are enabled only when both runtime variables are set:

```bash
UMAMI_URL=https://umami.schollz.com \
UMAMI_WEBSITE_ID=website-uuid \
croc-web --bind 127.0.0.1:9014 getcroc.com
```

Successful browser transfers emit the custom events `send-direct`,
`send-with-storage`, and `receive`. When Umami is not configured, event
tracking is disabled and transfers behave identically. The browser receives
only inert analytics configuration from the server: the Umami script is loaded
after an explicit opt-in in the first-visit privacy dialog. Automatic
pageviews are disabled, and custom transfer events remove query strings and
fragments from the reported URL.

Proxy the complete origin—including WebSocket upgrades—to
`127.0.0.1:9014`, preserving the original `Host` header. The server returns the
site at `/` and the WebSocket bridge at `/ws`, so no split routing or external
static file deployment is required. TLS certificates remain at the reverse
proxy.

Temporary storage is disabled unless a directory is explicitly configured:

```bash
croc-web \
  --bind 127.0.0.1:9014 \
  --store-dir /var/lib/croc/store \
  --store-max-transfer 1GiB \
  --store-quota 5GiB \
  --store-min-free 512MiB \
  getcroc.com
```

The store directory is locked to one server process and should be persistent,
private, writable only by the croc service account, and excluded from backups.
If a trusted reverse proxy supplies `X-Forwarded-For`, identify its network
with repeatable `--store-trusted-proxy CIDR` flags; untrusted forwarding
headers are ignored. See
[`src/docs/STORED_TRANSFERS.md`](../src/docs/STORED_TRANSFERS.md) for all
limits and operational details.

`--bind` defaults to `127.0.0.1:9014`. When an explicit loopback website
address with a port is used, such as `localhost:5173`, the local shortcut binds
there unless `--bind` is explicitly provided.

## Current boundaries

- One peer and one transfer at a time.
- Multiple selected files can be sent; sending folders and ZIP creation are not
  implemented.
- Nested folders and empty folders sent by a CLI can be received when the
  browser supports directory access. The individual-download fallback flattens
  names and adds numeric suffixes for collisions.
- Local multicast, direct browser-to-browser transfers, and non-xxhash live
  transfers are not implemented.
- Stored uploads accept regular files only. CLI stored downloads can resume
  completed chunks; browser stored downloads stream when supported and do not
  resume after the tab closes.
- Current evergreen desktop browsers are targeted.
