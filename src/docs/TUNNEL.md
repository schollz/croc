# Local port sharing

`croc tunnel PORT` shares one service already listening on localhost. The host
probes the port once and pins its loopback address for the tunnel's lifetime.
Guests cannot select another destination. Both peers make outbound connections
to a croc relay. This first version uses the relay for all traffic.

## Commands

```sh
croc tunnel 5173
croc tunnel --duration 1h 5173
CROC_SECRET='six-word-invitation' croc tunnel
CROC_SECRET='six-word-invitation' croc tunnel --local-port 8080
```

Without an argument or `CROC_SECRET`, the join command prompts for an invitation.
Windows also accepts the invitation as an argument. On Unix, follow the printed
environment-variable command to keep the invitation out of the process list.

The guest listener binds to `127.0.0.1`. Its default port matches the host port;
use `--local-port` if that port is occupied. The CLI forwards arbitrary TCP,
including HTTPS and application WebSockets, preserving concurrent connections
and TCP half-closes. It does not forward UDP.

The host stops after 12 hours unless `--duration` overrides the lifetime. Ctrl-C
closes current guests and stops accepting new ones. A lost connection triggers
fresh authentication for up to two minutes; interrupted streams fail and are
never replayed. Restarting the host creates a new invitation.

## Browser preview

The printed link opens the Tunnel tab at `https://getcroc.com/#tunnel`. The
invitation travels in the fragment, is removed from the address bar, and stays
in memory. No preview subdomain or tunnel service worker is needed. A deployed
frontend needs this version of the browser assets and `croc-tunnel.wasm`.

Use `--web-url` to print a link to another croc frontend. Custom relay settings
must match on the host and frontend; a link alone does not configure the relay.

The preview supports HTTP apps whose resources and APIs use the shared port:

- Static and dynamic JavaScript module imports, HTML, stylesheets, fonts,
  images, and media assets.
- Asynchronous fetch and XMLHttpRequest, including request bodies.
- WebSocket text and binary messages. Vite update notifications refresh the
  whole preview; component state is reset.
- Link and form navigation, preview back/forward controls, and hash routing.

Use CLI forwarding for HTTPS origins, cookie sessions, third-party resources,
browser storage, workers/service workers, synchronous XHR, or applications
requiring native `window.location` and History API semantics. This is an
isolated app preview, not a general browser proxy. Downloads, popups, nested
frames, and browser permission APIs are outside the preview's scope.

The parent page owns authentication and the encrypted transport. Untrusted app
HTML runs in a `srcdoc` iframe with `sandbox="allow-scripts allow-forms"`, which gives it an
opaque origin. It cannot read croc's document, storage, invitation, or keys.
CSP `form-action` blocks native submissions while the bridge handles form events.
A private MessageChannel bridges requests to the parent. CSP blocks direct
fetches, sockets, and resource loads; assets and CSS references are rewritten, and es-module-shims
loads modules through the bridge. Parent and host independently enforce the
fixed target. App code can issue requests to that target, so share only a
service you intend guests to use in full.

## Authentication and resource limits

Tunnel invitations use six EFF words. Two select the rendezvous room, and four
form the PAKE secret. The room namespace, PAKE purpose, and protocol feature
are distinct from file transfers and SSH sharing. Mutual key confirmation
precedes the encrypted offer containing the target port, ephemeral SSH host
key, and a random per-guest relay room.

Each guest supplies a fresh 32-byte credential inside the PAKE exchange. Both
peers release the invitation room and move to the private room, consume relay
waiting pings, then establish SSH with the pinned host key and a single-use,
30-second credential. The invitation can therefore serve multiple guests
without changing relay software. Only the three tunnel channel types are
accepted; shell sessions, arbitrary SSH forwarding, and incoming guest-side
channels are rejected.

The host allows 16 guests with 64 concurrent channels each. HTTP requests have
a two-minute deadline, 64 KiB metadata/chunk limits, and 64 MiB body limits in
each direction. WebSocket messages are limited to 1 MiB. The browser bounds
buffering and limits retained preview assets to 64 MiB per page. Redirects must
stay on the shared HTTP origin. Host, proxy, connection, and cookie headers are
filtered; the upstream Origin is the shared localhost service.

Relays carry encrypted traffic but can observe timing, sizes, peer IPs, and room
identifiers. They can interrupt connections. The site delivering the croc
browser client is trusted with its runtime, as it is for browser file transfers
and SSH. Anyone with the invitation can access the service until it expires;
there are no read-only guests or separate per-guest invitations.

## Focused verification

Two Go tests cover real-relay forwarding/concurrency/lifecycle and authorization
boundaries. Two Playwright tests exercise a real CLI host and Vite app, then
verify browser isolation and disconnect cleanup:

```sh
go test -race ./src/tunnel
npm run embed --prefix web
cd web
npx playwright test e2e/tunnel.spec.ts
```
