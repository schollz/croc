# croc web

`croc web` is a React/Vite client that sends and receives files with ordinary
`croc` CLI peers. Its production build and WebAssembly runtime are embedded in
the `croc` binary. `croc serve` serves the website, runtime configuration,
health check, and opaque WebSocket-to-TCP bridge from one HTTP address. File
metadata and contents remain encrypted between the browser and the other croc
client.

The security-sensitive protocol operations are compiled from this repository's
Go packages into WebAssembly:

- croc PAKE for both relay and peer handshakes
- PBKDF2 and AES-GCM encryption
- raw DEFLATE compression
- xxhash verification
- croc mnemonic code generation

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
croc serve localhost:5173
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
go test ./...
```

`npm run embed` builds the WASM and Vite client and copies the result into
`src/webassets/dist`, where Go's embed package includes it in every compiled
binary. A deployed binary does not need this source directory or any external
static files.

The Playwright suite builds a real `croc` binary, starts an isolated local croc
relay and unified embedded server on temporary ports, then verifies CLI → Web,
Web → CLI, and Web → Web transfers byte-for-byte. Install its browser once with
`npx playwright install chromium`. Test processes use an isolated
`CROC_CONFIG_DIR` and do not read or change remembered croc settings.

## Custom relay

The server fixes one upstream host and allowlists every TCP port so `/ws`
cannot be used as an arbitrary network proxy:

```bash
croc --pass YOUR_RELAY_PASSWORD serve \
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

## Production topology

Run `croc serve` behind an HTTPS reverse proxy:

```bash
croc serve --bind 127.0.0.1:9014 getcroc.com
```

Proxy the complete origin—including WebSocket upgrades—to
`127.0.0.1:9014`, preserving the original `Host` header. The server returns the
site at `/` and the WebSocket bridge at `/ws`, so no split routing or external
static file deployment is required. TLS certificates remain at the reverse
proxy.

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
- Local multicast, direct browser-to-browser transfers, reconnect/resume,
  symlinks, text mode, and non-xxhash transfers are not implemented.
- Current evergreen desktop browsers are targeted.
