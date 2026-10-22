# Native browser file and text transfers

Direct files, direct text, and stored files run without WebAssembly. SSH uses
the same native authentication primitives and retains its separate Go WASM
transport. The CLI protocol version and relay deployment requirements are
unchanged.

## Implementation

`web/src/protocol/engine.ts` exposes `CrocProtocolEngine`. Code generation,
compatibility parsing, and SHA-256 relay selection stay on the calling thread;
none starts a worker. A module worker starts on the first protocol operation
and owns PAKE, AES-GCM cipher, compression, and incremental XXH64 contexts.
Operations are ordered, and callers continue applying the existing transfer
backpressure. Each data cipher imports its AES key once. Contexts have explicit
release operations; closing or crashing a worker rejects pending requests and
destroys its state. Cancellation during PAKE or hash allocation rejects the
caller immediately and releases any handle returned later.

PAKE uses exactly pinned `@noble/curves` 2.4.0 and repository adapters for
P-256, P-384, P-521, SIEC, and Ed25519. `lossless-json` 4.3.3 preserves Go's
large decimal coordinates. The adapter reproduces fixed points, scalar
normalization, Ed25519 clamping, public JSON field order, and all public null
fields. It rejects invalid roles, unknown curves, oversized coordinate
integers, and invalid points before deriving a session key. Key derivation
uses the exact received wire bytes, including their representation.

Web Crypto supplies PBKDF2-SHA-256 with 100 iterations and 32-byte relay keys,
HMAC-SHA-256, SHA-256, and AES-GCM. Peer keys use the existing identity-bound
HKDF schedule and role-specific confirmation tags. Some Web Crypto providers
cap HKDF's `info` argument at 1024 bytes, below croc's transcript size. The
adapter therefore performs RFC 5869 extract and expand with Web Crypto HMAC;
it neither truncates nor hashes the transcript before expansion. The Go
oracle verifies identical traffic keys and confirmations.

AES-GCM retains the 12-byte nonce followed by ciphertext and the 16-byte tag.
Browser `CompressionStream` and `DecompressionStream` use raw DEFLATE.
Decompression counts output while reading, cancels on overflow, and requires
successful stream completion. Browser and Go compressed bytes may differ;
interoperability is checked by decompression in both directions. Empty browser
DOMException messages remain errors across the worker boundary.

XXH64 uses paired 32-bit words, zero seed, incremental state with a 32-byte
tail, and an eight-byte big-endian digest. Files are read incrementally.
Caller-owned input is copied before transferring a backing buffer to the
worker. Chunk sizes, eight-byte offsets, compression negotiation, hash caching,
progress callbacks, destinations, and the 1 MiB UTF-8 text limit are retained.
Store retains its SHA-256 worker and encrypted format.

The build removes stale `croc.wasm` and `croc-worker.js` before Vite copies
public assets. Only SSH WASM and its Go runtime are generated. There is no
production WASM fallback for files or text. Direct transfers check Web Crypto
and raw DEFLATE support before preparation or connection; HTTPS or localhost
and a current evergreen browser are required.

## Validation

Run from the repository root:

```sh
npm --prefix web test
npm --prefix web run typecheck
npm --prefix web run embed
go test ./... ./web/testdata/oracle
```

Run from `web/` after embedding:

```sh
CROC_E2E_PRIVACY=1 npx playwright test
```

The frontend suite builds a test-only Go JSON-lines oracle against the pinned
PAKE dependency and croc packages. Live exchanges cover JavaScript in both
roles on every curve, unbound relay exchanges, all three identity-bound
purposes, exact wire serialization, and derived keys and confirmations.
Additional cases cover empty, short, and long password scalars; altered rooms
and transcripts; wrong passwords; invalid roles, points and integers; unsupported
versions; AES-GCM tampering; raw DEFLATE boundaries; fragmented XXH64; worker
failure; cancellation; released contexts; and retained input buffers. Go and
TypeScript both consume the frozen `web/testdata/native-vectors.json` fixture.

The browser matrix runs Chromium, Firefox, and WebKit with a 390 × 844 mobile
viewport for WebKit. It sets `WebAssembly` to `undefined` in pages and native
worker scripts, blocks both WASM modules and Go/legacy worker glue, and asserts
zero requests for those assets. It tests CLI-to-browser, browser-to-CLI, and
browser-to-browser files and text; repeated transfers in the same pages;
multiple and empty files; a large executable; legacy, three-word, four-word,
and `yo-yo` codes; download fallback; nested destinations through a simulated
File System Access picker; and wrong-password rejection before metadata.
Browser-side primitive probes also exercise all five curves and malformed,
truncated, exact-limit, and oversized compressed output. The Store suite
preserves privacy, corruption, consumption, and cancellation checks. The two
SSH integration tests run with WASM enabled and execute real CLI-hosted shell
commands. CI runs this complete matrix.

Local acceptance on 2026-10-22: 88 browser tests passed, including the two SSH
tests. The frontend suite, type checking, embedded-asset checks, Go packages,
and shared Go vectors also passed. The isolated test relay uses higher
admission limits because the matrix repeatedly joins the same fixture rooms
from localhost; production defaults are unchanged.

## Local performance observations

Measured on local macOS arm64 with Chromium 149.0.7827.55. Microbenchmark
values below are medians of three fresh browser contexts. The source baseline
is `a5f4c54a7576e88911b7cb293fcfc6f8a431f8be`.

| Measurement | Go WASM baseline | Native engine |
| --- | ---: | ---: |
| Additional direct protocol worker/runtime assets, raw bytes | 6,489,668 | 75,683 |
| Engine import to first hash context, ms | 44.4 | 21.8 |
| Incremental XXH64, MiB/s | 1523.8 | 252.7 |
| 32 KiB encrypt/decrypt round trips, MiB/s | 16.5 | 325.2 |
| 16 MiB CLI → browser fixture, MiB/s (one run) | 13.3 | 28.1 |
| Retained worker JS heap + backing storage + WASM linear memory after GC, MiB | 10.26 | 2.64 |

These are local observations, not production or mobile performance guarantees.
The startup probe uses Vite's development server: the first run can include
module transformation, and browser contexts are cold while the server and OS
caches warm. The raw asset comparison excludes the common application bundle,
images, and the separately retained SSH assets; it measures uncompressed
representations rather than HTTP transfer sizes.

The hash probe writes 64 MiB in 1 MiB blocks through the worker. The chunk probe
performs 512 uncompressed 32 KiB encrypt/decrypt round trips using a reusable
cipher. Its rate counts plaintext bytes once, including worker dispatch and
both crypto operations. The transfer probe uses the same deterministic,
incompressible 16 MiB file through a real CLI, relay, bridge, and browser;
its timer includes accepting the offer and verifying the resulting download,
including UI polling. It is a single observation in each implementation.

Memory figures use Chromium's worker `Runtime.getHeapUsage` after an explicit
GC, plus the Go runtime's allocated WASM linear-memory size. They are not total
browser RSS or peak memory. Hash state does not retain the processed 64 MiB;
file reads and chunk processing remain bounded. The existing Blob download
fallback still buffers a destination file, and its existing 256 MiB limit is
unchanged.

XXH64 is slower in TypeScript than Go WASM in this probe. The native engine
reduces direct-transfer startup assets and retained runtime memory, while
Web Crypto substantially improves the chunk round trip in this environment.
Correct interoperability, bounded processing memory, cancellation, and zero
WASM use for files and text remain the acceptance conditions.

Raw results are in `web/testdata/native-performance.json`. Repeat the worker
measurements from `web/` with:

```sh
node scripts/benchmark-engine.mjs --baseline-ref=a5f4c54a7576e88911b7cb293fcfc6f8a431f8be --output=.e2e/baseline.json
node scripts/benchmark-engine.mjs --output=.e2e/native.json
```

The baseline option reads historical files with `git show`, builds them in an
ignored temporary directory, and serves them only to the benchmark. It does
not change the checkout or add a production fallback. For transfer timing,
set `CROC_E2E_PERF_FIXTURE` to an absolute file path and
`CROC_E2E_PERF_OUTPUT` to an absolute JSON output path, then run the Chromium
`large croc executable` Playwright test. Capturing the old transfer baseline
requires the original engine build; the saved baseline is included above.

