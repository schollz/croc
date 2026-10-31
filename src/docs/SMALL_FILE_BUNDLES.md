# Automatic small-file bundles

Direct transfers can spend more time requesting individual small files than
sending their contents. `small-file-bundles-v1` requests several original
manifest entries together and sends their raw concatenated bytes through the
existing data connections. The manifest remains the authority for every name,
path, size, hash, permission, timestamp, and progress entry.

## Selection

Both PAKE feature lists must advertise `small-file-bundles-v1`. The encrypted
sender manifest must also include that feature and valid `BundleLimits` with
`maxBytes` and `maxFiles`. The same metadata is carried in the chunked manifest
header. Browser handshakes advertise only this implemented extension.

| Limit | Value |
| --- | ---: |
| Maximum eligible file | 1 MiB |
| Minimum requested members | 4 |
| Maximum members | 128 |
| Native payload | 32 MiB |
| Browser payload | 8 MiB |

The effective limits are the minimum of both endpoints' limits. Sizes refer to
uncompressed bytes. Planning follows logical manifest order and stops at an
ineligible file or a byte/member limit. A leftover group of fewer than four
requested members uses individual requests. Unchanged native destinations are
skipped before grouping. Overwrite and partial-file resume decisions stay on
the individual path; a file renamed to a fresh destination can join a bundle.
Individual and bundle planning share destination preflight. Only user consent
and accepted destination names are retained; disk contents are checked again
after individual reception and on reconnect. Generated names avoid both disk
entries and all reserved portable manifest paths.

Eligible entries are prepared, nonempty regular files with an exact `xxhash`
wire digest. Empty files/directories, symlinks, temporary archives, other hash
modes, text, stdout, sender confirmation, stored transfers, and SSH retain their
existing behavior. Capability absence selects the original protocol. Invalid
limits, messages, hashes, source snapshots, and destination writes fail the
transfer.

## Control and data

The existing encrypted control envelope carries five operations. Each inner
JSON document is bounded to 16 KiB before its contents are parsed. A positive
32-bit ID increases monotonically within an attempt and resets on reconnect.

| Operation | Inner JSON |
| --- | --- |
| `bundle-request` | `id`, ordered `indices`, receiver `limits`, `MachineID`, `ReconnectVersion`, optional `ExternalIP` |
| `bundle-offer` | matching `id`, exact `size`, base64 eight-byte XXH64 `hash`, `compressed` |
| `bundle-ready` | matching `id` |
| `bundle-done` | matching `id` |
| `bundle-closed` | matching `id` |

The sequence is request, prepare/offer, ready, transfer, verify, commit members,
done, join sender workers, closed. Only one bundle is active. No next request or
session completion may begin before closed. Legacy file-completion messages
during a bundle, unsolicited operations, reused IDs, invalid indices, duplicate
indices, and mismatched offers are errors. Destination paths are never included
in these messages.

Payload bytes are the member contents in request order. Offsets are prefix sums
of manifest sizes. The data engine keeps its 32 KiB chunks, encrypted eight-byte
little-endian offsets, connection pool, compression sampling, buffer reuse, and
upload limiter. Receivers require exact alignment and expected chunk length,
reject duplicates, and count unique chunks. Compression is sampled from the
concatenation and communicated in the offer; the global compression switch
still applies.

The control owner manages lifecycle transitions. Data workers receive immutable
unit state. Before closed, the sender detaches the active unit under the same
lock that admits late workers, then joins its workers. Logical progress maps
unique payload ranges back to original members. Completion follows destination
commit, once per original file.

## Destinations and recovery

The native sender holds one private temporary spool, at most 32 MiB. It checks
source identity before and after each copy and compares copied member hashes
with the original manifest. The receiver holds one bounded payload plus one
staged member set, at most 64 MiB in total. It verifies the payload and every
member before publishing anything. Staging and publication use the receive-root
abstraction. Publication rechecks portable path collisions and symlink rules,
and an atomic no-replace link refuses destinations that appeared after
preflight. Before requesting a bundle, a cached per-directory probe checks
root-to-destination publication. Filesystems without hard-link support and
destinations on separate mounts use individual transfers. Permission, I/O, and
cleanup failures remain errors. Permissions and timestamps are applied to
staged native files.

The browser builds an outgoing bounded Blob from the selected File objects. Its
receive sink holds at most 8 MiB of payload. After payload/member verification,
each file goes through its ordinary `ReceiveDestination` and `ReceiveSink`.
Folder writes, collision confirmation, duplicate download names, and individual
downloads keep their existing meaning. Individual download clicks are paced to
avoid Chromium dropping a burst. Bundle buffers are released after commit or
failure. File transfers require no WASM or archive dependency.

Cancellation, disconnect, and terminal failure close workers and remove private
payload/staging files. A failed discard retains resource ownership, reports an
error, and blocks the next unit and reconnect. Already committed members remain. Native reconnect
renegotiates capabilities and recomputes the plan; retained destination names
are associated with validated logical file metadata, and normal preflight
checks their contents again. An unfinished bundle restarts. Large-file resume
and browser manual retry continue using their existing behavior.

## Verification

`testdata/bundles/v1.json` supplies shared Go/TypeScript planner, payload, offset,
XXH64, limit, and invalid-request fixtures. Native tests also cover source
mutation, corrupt payloads/members, publication conflicts, cancellation,
preflight decisions, reconnect identity, and cleanup. Browser tests cover bounded
sinks, verification before destination creation, committed prefixes after a
write failure, and exactly-once completion callbacks.

Run the checks from the repository root:

```sh
go test ./... ./web/testdata/oracle
go test -race ./src/croc ./src/receivefs ./src/message
npm --prefix web test
npm --prefix web run typecheck
npm --prefix web run test:e2e
```

Build real pre-extension clients and run the native matrix outside the checkout:

```sh
python3 scripts/test-bundle-compatibility.py --artifacts /tmp/croc-bundle-compat
```

The script archives the fixed pre-PR commit
`ebcdc31b413991e4892260b9f43697eb1797f7ca`, builds its executable, and checks
both old/new directions plus new/new with 3, 4, and 256 files. It retains logs,
bundle-message counts, and independent path/size/SHA-256 manifests.

For the browser matrix, install the frozen tree's npm dependencies and build its
embedded site, then point the current E2E stack at those artifacts:

```sh
npm --prefix /tmp/croc-bundle-compat/frozen-ebcdc31b/web ci
npm --prefix /tmp/croc-bundle-compat/frozen-ebcdc31b/web run embed
CROC_E2E_FROZEN_SOURCE=/tmp/croc-bundle-compat/frozen-ebcdc31b \
CROC_E2E_OLD_BINARY=/tmp/croc-bundle-compat/bin/old \
  npm --prefix web run test:e2e -- \
  --grep 'small-file bundles|frozen bundle compatibility'
```

These environment variables configure the test harness only. They add no
transfer settings. Chromium, Firefox, and WebKit run actual individual-download
checks, real folder writes through browser file handles, repeated transfers,
and old-client compatibility. Test instrumentation records control operation
types before encryption and asserts that old-peer sessions emit no bundle
messages. Routine CI runs bundle unit/race checks on Linux and focused native
checks inside the existing macOS/Windows portability jobs. The Linux job also
runs the browser unit suite. Real frozen-client builds and the three-engine
browser matrix run when **CI → Run workflow** is requested manually, avoiding
duplicate builds and browser installation on each push or pull request.

See [measurements and reproduction steps](SMALL_FILE_BUNDLES_BENCHMARKS.md) for
the controlled latency gate, source/video comparisons, WAN results, and storage
audit.
