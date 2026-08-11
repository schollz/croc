# Encrypted stored transfers

Stored transfers are croc's opt-in asynchronous mode. A sender encrypts one or
more regular files locally, uploads only ciphertext, and shares a capability
link. Each receiver that finishes authenticating and verifying every file
commits one allowed download. The final allowed commit immediately removes the
ciphertext. An available transfer expires after the sender-selected finite
lifetime, measured from successful upload finalization. The default is one day.

This is deliberately separate from the normal croc relay protocol. Direct
transfers remain the default and continue to use PAKE between two live peers.

## User workflow

Upload with the public service:

```bash
croc send --store photo.jpg document.pdf
croc send --store --store-downloads 3 photo.jpg document.pdf
croc send --store --store-expiration 3d photo.jpg document.pdf
```

Use another service origin when self-hosting:

```bash
croc send --store --store-url https://files.example.com photo.jpg
```

The sender receives:

- a browser link, `https://files.example.com/s/<id>#v1.<key>`;
- a `croc-store-v1...` CLI token;
- the precise expiration time; and
- a transfer ID that can be passed to `croc --revoke`.

On the receiving CLI, run `croc` and paste the token or link at the prompt.
This avoids exposing its decryption key in the UNIX process list. Noninteractive
use can supply the same value through the environment:

```bash
CROC_STORE_TOKEN='croc-store-v1....' croc --out ./received
```

The CLI decrypts the manifest before showing file names and sizes. It asks for
confirmation, downloads to partial files, authenticates every chunk, verifies
each complete file with SHA-256, renames it into place, and only then commits
one allowed download. Its partial state allows completed chunks to resume.

The web client performs the same inspection before asking the user to accept.
It writes through the File System Access API where available. Otherwise a
service worker streams the download without keeping the complete file in page
memory. Blob fallback is limited to 256 MiB; for larger files on browsers that
support neither streaming path, the UI directs the recipient to the CLI.

The sender can revoke a transfer while it remains available:

```bash
croc --revoke <transfer-id>
```

CLI revoke capabilities are stored with mode `0600` in the croc configuration
directory. Browser upload and claim capabilities are limited to the current
tab session.

## Why the browser key follows `#`

The URL fragment is interpreted by the browser and is not sent in HTTP request
targets or `Referer` headers. The service therefore sees `/s/<id>` while the
web client reads `#v1.<key>` locally and uses it to decrypt. This keeps the
decryption key out of server access logs and reverse-proxy logs.

The complete link is still a bearer secret. Anyone or any application that can
read the text can decrypt the files, claim the transfer, or deliberately
consume it without saving the files. Share it over an appropriately private
channel. The fragment protects against normal HTTP transport and logging; it
does not protect a compromised browser, recipient, sender, or messaging
account.

## Cryptographic format

The versioned protocol name is `croc-store-v1`.

- Each transfer uses a random 256-bit master key and a random 128-bit public
  transfer ID.
- HKDF-SHA-256 derives independent `manifest`, `data`, and `redeem` keys or
  capabilities from the master key.
- The manifest and every non-empty 4 MiB file chunk use AES-256-GCM with a
  fresh random 96-bit nonce.
- Associated data binds the protocol version and transfer ID. Chunk associated
  data additionally binds the global object index, file index, per-file chunk
  index, and expected plaintext length, preventing substitution or reordering.
- The encrypted manifest contains safe base file names, byte lengths,
  modification times, SHA-256 file digests, and the exact chunk map.
- The service stores only SHA-256 verifiers for upload, redeem, and active claim
  capabilities. Capability values travel only in `Authorization: Bearer`
  headers and API responses marked `Cache-Control: no-store`.

The service necessarily learns connection metadata, timing, ciphertext byte
lengths, the total plaintext byte count, file and chunk counts declared for
quota enforcement, and the opaque transfer ID. It cannot learn individual file
names, contents, hashes, modification times, or the master key from stored
data. It can deny service, delete data early, or serve stale/corrupt
ciphertext, but official clients authenticate ciphertext and verify the final
plaintext before committing consumption.

## Download-budget state machine

New objects have one of these persistent states:

1. `uploading` — reserved for one hour while ciphertext objects arrive;
2. `available` — complete and redeemable for its accepted lifetime;
3. `claimed` — locked to one receiver for up to 30 minutes, renewed as chunks
   are read;
4. `consumed`, `revoked`, or `expired` — ciphertext is deleted immediately and
   a small tombstone remains for 24 hours so callers receive a stable terminal
   result.

Reading the encrypted manifest does not consume or lock a transfer. A receiver
claims immediately before reading chunks. Only the claim capability can read
chunks or commit. If the receiver disappears, the claim expires and another
receiver may try. Official clients commit only after client-side authentication
and whole-file SHA-256 verification. The service cannot prove that a custom
client actually performed those checks. A successful commit decrements the
remaining-download count exactly once. While downloads remain, the transfer
returns to `available`; at zero it becomes `consumed` and its ciphertext is
deleted. Claims remain exclusive, so configured downloads are served one at a
time rather than concurrently.

## Running a service

Storage is disabled by default. Enable it on the unified web server with a
private persistent directory:

```bash
croc-web \
  --bind 127.0.0.1:9014 \
  --store-dir /var/lib/croc/store \
  files.example.com
```

Put the complete origin behind HTTPS and proxy every path, including
`/api/v1/store`, `/ws`, the web assets, and stored share routes, to the same
process. Do not cache the API. The generated browser links use the origin from
which the browser app is loaded.

The storage controls are:

| Flag | Default | Purpose |
| --- | ---: | --- |
| `--store-max-transfer` | `1GiB` | Maximum plaintext bytes per transfer (up to `2GiB`) |
| `--store-quota` | `5GiB` | Maximum reserved ciphertext across transfers |
| `--store-min-free` | `512MiB` | Disk space that must remain available |
| `--store-max-files` | `100` | Maximum regular files per transfer |
| `--store-downloads` | `1` | Maximum verified downloads a sender may request per transfer |
| `--store-max-expiration` | `0` | Maximum sender-selected lifetime; `0` means no policy ceiling |
| `--store-create-rate` | `5` | Creates allowed per client IP per hour |
| `--store-active-uploads` | `2` | Concurrent incomplete uploads per client IP |
| `--store-trusted-proxy` | none | Repeatable trusted reverse-proxy CIDR |

Set `CROC_STORE_DOWNLOADS` and `CROC_STORE_MAX_EXPIRATION` to configure the
server's download and expiration maxima through the environment; explicit
flags override them. Expiration values use whole `m`, `h`, `d`, or `w` units,
must be at least one minute, and are silently reduced when they exceed the
server maximum. An empty or `0` server maximum permits any finite representable
lifetime. The accepted lifetime is stored when the transfer is created, so a
later policy change does not alter it. Legacy metadata retains the one-day
default.

Byte flags accept integer `B`, `KB`, `MB`, `GB`, `TB`, `KiB`, `MiB`, `GiB`,
and `TiB` suffixes. A service holds an exclusive lock on its configured root;
do not point multiple processes at the same directory. On startup it rebuilds
quota and active-upload accounting from metadata, then sweeps incomplete,
expired, and terminal records every minute.

Run the service as an unprivileged account. Give only that account access to
the storage directory, monitor free space and HTTP `429`/`507` responses, and
exclude ciphertext from backups so deletion semantics are not undermined.
Only configure `--store-trusted-proxy` for infrastructure that overwrites
client forwarding headers; otherwise rate limiting deliberately uses the
socket peer address.

The HTTP API is versioned at `/api/v1/store/transfers`. It is an implementation
boundary for the official croc web and CLI clients, not a promise that
unversioned internals will remain compatible. Create declarations may include
`expiresSeconds`; omitting it preserves the one-day client default, while a
configured maximum may silently clamp the accepted value. The accepted
lifetime is not added to the create response so strict older clients retain
their response shape. The existing completion response reports the final
absolute `expiresAt`.

Browser runtime configuration exposes `expiresSeconds` as the effective
default and `maxExpiresSeconds` as the policy ceiling. A zero maximum means no
policy ceiling.
