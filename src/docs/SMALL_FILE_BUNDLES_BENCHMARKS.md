# Small-file bundle measurements

These measurements compare the uncommitted bundle implementation with the real
pre-extension source at `ebcdc31b413991e4892260b9f43697eb1797f7ca`. The `q`
reference remains unchanged at `05f75b6dbae40352f0c43d6900f244b6602cce32`.
No adaptive controller, resume journal, release change, or tuning setting from
that reference is included in the implementation.

## Inputs and builds

Inputs were frozen outside the checkout before timing. Every successful trial
independently compared received paths, sizes, and SHA-256 digests with the
frozen input manifest. Verification runs after the timed process interval and
is additional to croc's normal wire-hash verification.

| Fixture | Files | Uncompressed bytes |
| --- | ---: | ---: |
| Generated small files, 1,024–1,039 bytes each | 256 | 264,064 |
| Frozen source tree | 278 | 13,699,876 |
| Frozen video | 1 | 773,207,161 |

The video's SHA-256 is
`4a234f1607a136ad648f91439090bfc90fc4f5f85137f84aacc399f34bbd557f`.
The small fixture is generated deterministically by
`scripts/benchmark-bundles.py`. Source and video manifests accompany the raw
results; the private video itself is not part of this repository.

Both native versions use Go 1.27.2 with the same release build settings:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -buildvcs=false -trimpath -tags netgo,osusergo \
  -ldflags='-s -w -buildid=' -o /outside/checkout/bin/new-linux-amd64 .
```

The baseline uses that command in a `git archive` of the fixed pre-PR commit.
The WAN's local executables use `GOOS=darwin GOARCH=arm64` with the same settings.
Binary SHA-256 digests are retained with the measurement artifacts. Frozen
compatibility clients are built separately by
`scripts/test-bundle-compatibility.py`.

A final portability review added a Windows-only writable staging handle before
`Sync` and made the Unix permission-bit assertion platform-aware. Windows'
[`FlushFileBuffers` requires write access](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-flushfilebuffers).
The conditional branch is eliminated on the measured Linux/macOS platforms;
their transfer and publication paths are unchanged. The measurement binary
digests and the final source checksums are retained separately.

## Controlled TCP path

`scripts/benchmark-bundles.py` creates two private Linux network namespaces,
joins them with a veth pair, and applies 37.5 ms of `tc netem` delay in each
direction. A ping records the resulting 75 ms RTT. The relay runs in the second
namespace. “Up” sends from the first namespace to the second; “down” reverses
the endpoints. The existing eight data connections, 32 KiB chunks, xxhash,
compression selection, and relay transport are used by both versions. The
video uses the existing global `--no-compress` option in both versions.

Trials alternate baseline/new order. Each begins with an empty destination and
uses a fresh relay room. End-to-end time includes process startup, metadata and
hash preparation, transport setup, payload delivery, normal destination
verification, and completion. Native output is captured through pipes, then
drained to log files: writing progress directly to a regular file makes the
existing progress library call `fsync` on every render and distorts the result.
Early pilots with that artifact are excluded.

Private outgoing spools, incoming spools, and destination staging files are
sampled every 5 ms. Established data sockets are sampled every 100 ms. Debug
records separate preparation, time between bundles, payload reception, and
verification/commit. The monitor also rejects leaked bundle files after each
successful transfer. This observes peak storage; the contract and filesystem
tests separately exercise the hard limits and failure cleanup.

Reproduce on an otherwise idle Linux host with root, Python 3.12+, `ip`, `tc`,
and `ping`. Supply frozen inputs and identically built executables:

```sh
python3 scripts/benchmark-bundles.py \
  --base /outside/checkout/bin/base-linux-amd64 \
  --new /outside/checkout/bin/new-linux-amd64 \
  --source-dir /outside/checkout/inputs/src \
  --video /outside/checkout/inputs/video \
  --artifacts /outside/checkout/results/controlled \
  --repeats 3 --rtt-ms 75
```

The harness changes only its own namespaces and removes them on exit. Its
`--resume` option reuses successful cases only after matching both binary
digests and all frozen input digests. Failed attempts are retained separately
and never become timing observations.

The corrected final build's 256-file results use three alternating paired
trials per direction. Times are seconds, shown as median [minimum–maximum].

| Direction | Individual baseline | Automatic bundles | Median speedup |
| --- | ---: | ---: | ---: |
| Up | 41.936 [41.806–42.147] | 4.756 [3.763–4.789] | 8.82× |
| Down | 42.010 [41.842–42.046] | 4.415 [3.874–4.427] | 9.52× |

Each updated session used two 128-member bundles over eight data connections.
The protocol uses five control operations per bundle, so the 256 members
required ten bundle controls. This passes the 4× gate in both directions.

The source/video end-to-end results use three trials per build and direction,
with six upward video trials after an additional paired check of the WAN
difference. Times are seconds, shown as median [minimum–maximum].

| Input and mode | Direction | Baseline | Updated | Trials per build |
| --- | --- | ---: | ---: | ---: |
| Source, ordinary files | Up | 48.071 [47.630–48.121] | 8.285 [6.557–8.333] | 3 |
| Source, ordinary files | Down | 48.449 [48.145–49.893] | 8.556 [6.859–8.904] | 3 |
| Source, explicit ZIP reference | Up | 5.164 [4.560–5.175] | 4.838 [4.466–7.190] | 3 |
| Source, explicit ZIP reference | Down | 7.947 [7.583–9.036] | 8.064 [7.241–8.548] | 3 |
| 773 MB video | Up | 26.155 [24.907–28.155] | 23.519 [15.100–27.998] | 6 |
| 773 MB video | Down | 83.760 [81.982–93.313] | 63.491 [45.882–154.342] | 3 |

The video also has a separate payload measurement: the log reader timestamps
the existing first-decrypted-byte and finished-receiving events as pipe bytes
arrive. This excludes startup and wire-hash verification from throughput.
MB/s uses decimal megabytes.

| Direction | Baseline MB/s | Updated MB/s | Median change | Trials per build |
| --- | ---: | ---: | ---: | ---: |
| Up | 53.60 [48.47–56.28] | 56.62 [45.40–99.78] | +5.63% | 6 |
| Down | 10.66 [9.12–10.67] | 14.41 [5.36–22.17] | +35.18% | 3 |

No median throughput loss above 5% was reproduced in either direction. The
reverse path varied substantially despite the fixed RTT; a qdisc audit recorded
zero drops at the observation point. The extra upward pairs also failed to
reproduce the slower WAN upload. All ranges remain visible, and the median
differences are not treated as evidence of a large-file optimization. The
large-file framing, connection counts, and compression behavior remain shared
with the original individual-file path.

## WAN method

`scripts/benchmark-bundles-wan.py` uses SSH only to orchestrate the remote peer
and verify output. File data and control traffic use a private croc relay with
the same existing transport settings for both builds. Copy
`scripts/bundle_remote_runner.py` and `scripts/bundle_bench_common.py` into the
remote artifact root. The shared helper supplies room allocation, independent
SHA-256 manifests, and bounded subprocess cleanup. Pass paths to
the frozen local/remote inputs, both local/remote executables, an input
SHA-256 manifest, and a private relay settings JSON containing `port` and
`password`:

```sh
python3 scripts/benchmark-bundles-wan.py \
  --artifacts /outside/checkout/results/wan \
  --host user@host --relay-host host \
  --remote-root /remote/bundle-benchmark \
  --relay-settings /private/relay-settings.json \
  --manifest /outside/checkout/input-manifests.json \
  --source /outside/checkout/inputs/src \
  --video /outside/checkout/inputs/video \
  --remote-source /remote/inputs/src --remote-video /remote/inputs/video \
  --local-base /outside/checkout/bin/base-darwin-arm64 \
  --local-new /outside/checkout/bin/new-darwin-arm64 \
  --remote-base /remote/bundle-benchmark/bin/base \
  --remote-new /remote/bundle-benchmark/bin/new --repeats 3
```

The manifest has `src` and `large` mappings, each keyed by received relative
path with `{ "size": N, "sha256": "..." }` values. The WAN is measured after
the controlled run finishes so the two experiments do not contend for remote
CPU or bandwidth. WAN medians and ranges are reported separately. Explicit
`--zip` source-tree trials are a reference; automatic bundling saves ordinary
files and never introduces a ZIP download.

## WAN results

WAN times include native process completion and orchestration. Values are
seconds, shown as median [minimum–maximum]. Trials alternate baseline/new
order. The video upload and upward ZIP reference were repeated to six trials
per build after the first three showed an inconclusive difference or outlier.
All other groups have three trials per build. All 48 WAN transfers passed
independent received-path, size, and SHA-256 verification.

| Input and mode | Direction | Baseline | Updated | Trials per build |
| --- | --- | ---: | ---: | ---: |
| Source, ordinary files | Up | 41.881 [41.142–42.128] | 8.479 [6.690–8.645] | 3 |
| Source, ordinary files | Down | 64.182 [61.919–70.075] | 6.548 [6.546–7.309] | 3 |
| Source, explicit ZIP reference | Up | 4.794 [4.171–5.299] | 4.591 [4.141–33.094] | 6 |
| Source, explicit ZIP reference | Down | 4.490 [3.835–4.636] | 3.995 [3.533–4.399] | 3 |
| 773 MB video | Up | 64.833 [58.866–69.720] | 71.792 [61.645–76.317] | 6 |
| 773 MB video | Down | 39.022 [36.431–43.001] | 37.772 [37.191–37.828] | 3 |

The ordinary source tree was 4.94× faster upward and 9.80× faster downward by
median elapsed time. The repeated video upload measured 71.792 versus 64.833
seconds, or 9.69% lower end-to-end throughput. Existing debug events, with
one-second timestamp resolution, put most of that difference in payload
transfer. The original outliers remain in the reported ranges. The downward
video median was 37.772 versus 39.022 seconds.

After the measurements, the user reported other activity on the shared network
and suggested that it explains the upload difference. Competing traffic was
neither measured nor held constant, so these WAN timings cannot isolate the
implementation's effect. They remain observational results under shared
network load, rather than evidence of an implementation regression. Performance
acceptance uses the controlled 75 ms path above, where the expanded paired
checks did not reproduce a large-file throughput loss.

## Storage, phases, and cleanup

Across 54 successful controlled trials, the monitor observed eight established
data connections for both builds. Bundle storage peaks were independent of
the total input size:

| Input | Peak sender spool bytes | Peak receiver spool + staging bytes |
| --- | ---: | ---: |
| 256 small files | 132,032 | 264,064 |
| 14 MB source tree | 3,324,559 | 6,649,118 |
| 773 MB video | 0 | 0 |

The hard contract limits remain 32 MiB for one native outgoing spool and
64 MiB for one receiving payload plus its staged members. The table records
observed peaks sampled every 5 ms. Boundary tests and on-demand ownership
separately enforce the caps. Every successful trial checked for leftover
bundle spools and staging directories.

The 256-file fixture used two bundles and ten bundle control operations. The
source tree used four bundles and twenty bundle control operations; its
ineligible entries retained individual requests. Control count therefore
follows bundle count. Original file counts, paths, and hashes remain separate
from those internal units.

Existing debug events yielded the following phase timings across 36 bundles.
Values are milliseconds, shown as median [minimum–maximum].

| Phase | Time, ms | Samples |
| --- | ---: | ---: |
| Spool preparation and sampling | 17.36 [8.30–85.66] | 36 |
| Local gap after closed before next request | 11.25 [2.68–1111.06] | 24 |
| Ready through final received chunk | 200.55 [110.88–573.68] | 36 |
| Full/member verification and destination commits | 954.96 [93.82–2135.00] | 36 |

The final audit found no bundle spools or staging files locally or remotely,
and no remaining test network namespaces. The private benchmark relay was
stopped after verifying its executable and PID. Frozen inputs, binaries,
verification manifests, and diagnostic logs remain in the external artifact
directory for review. Empty harness destination directories were removed.

## Validation

The native matrix uses real frozen native clients in both directions with
3, 4, and 256 files. Old-peer sessions emit no bundle messages. Updated peers
use the individual path for three files, one bundle for four, and two bundles
for 256. Browser E2E tests exercise real frozen native/browser combinations in
both directions, updated combinations, actual individual downloads, real folder
writes, repeated transfers, and active-payload cancellation followed by manual
retry in the same pages.

The local checks include the complete Go suite and oracle, Go race checks for
croc/receivefs/message, 297 browser unit tests, TypeScript typechecking, and
121 E2E tests across Chromium, Firefox, and WebKit. Routine CI shares bundle
checks with the existing Linux unit/race and macOS/Windows portability jobs.
Frozen-client builds, the three-engine browser matrix, fuzz smoke tests, and
exhaustive cross-builds are available through **CI → Run workflow**, rather than
running automatically on every change. Hosted jobs have not run against this
uncommitted checkout.

## Maintenance follow-up

The recorded timing results precede the destination-policy, cleanup ownership,
publication-capability probe, and browser progress refactors. Measured binary
SHA-256 values and frozen input manifests still identify those historical runs.
The follow-up uses the same wire contract, chunk sizes, workers and fixed caps,
and reruns correctness, race, and frozen-peer compatibility checks. The timing
experiments have not been repeated against the refactored source; the original
numbers should not be read as new measurements of that source.
