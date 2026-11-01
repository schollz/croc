# Audio transfer optimization — November 1, 2026

The local croc build transferred `/Users/zns/Music/audio.wav` faster than derphole in all three paired rounds from this Mac to the Toronto server. The middle times were **8.52 seconds for croc** and **10.65 seconds for derphole**. Croc used its normal command, with no compression or transport overrides. These are locally compiled, uncommitted changes; they do not represent a published release.

Input: **317,520,080 bytes** (317.52008 decimal MB), SHA-256 `a692d566b00e9a4173ee20e2b7113cb12b204300e39e2953ab0c3dc8f26d9ec4`. The source file was unchanged and every receiver reproduced the exact size and SHA-256. Direction: Mac14,15 arm64 on macOS 26.5.2 → `root@134.122.43.205`, Ubuntu 24.04.4 LTS amd64, DigitalOcean `tor1` (one CPU).

| Build / tool | Time | Effective rate |
| --- | ---: | ---: |
| croc — new local build | 8.52 s | 37.27 MB/s |
| derphole | 10.65 s | 29.80 MB/s |
| croc — previous local build | 11.67 s | 27.21 MB/s |

The new build reduced elapsed time by **20.0%** against derphole and **27.0%** against the previous croc build in these same rounds. Every croc candidate run was quicker than every derphole run. This is a result for this file and route, not a general ranking.

Time is the middle of three successful transfers. A local monotonic clock starts immediately before launching the sender and stops on receipt of the remote usable-file notification. Sender preparation, receiver startup, connection setup, transfer and automation delays count. SSH establishment and later hash verification do not. Configuration and receive directories were fresh for each trial; output was piped into log-draining threads. The timeout was 900 seconds. Rate is `317,520,080 / 1,000,000 / time_seconds`.

Individual times, in round order:

- New croc: 9.929542 s, 8.519712 s, 8.142030 s.
- derphole: 10.653636 s, 20.351070 s, 10.636568 s.
- Previous croc: 11.586485 s, 11.722892 s, 11.667700 s.

## Change and validation

Croc previously sampled only the first 256 KiB to choose compression. This WAV’s quiet opening compressed much better than its body. In a diagnostic, deflate decoding consumed 8.04 of 9.74 sampled CPU seconds on the receiver. A no-compression control took 10.84 seconds; forcing direct Tailcat without compression took 35.94 seconds, so the transport defaults were retained. Diagnostic runs are excluded from the table.

For files larger than 32 MiB, the same 256 KiB sample budget is now spread across five windows. Compression is selected only when at least three windows save more than 5%. Small files and bounded bundles retain their original probe. The existing per-file compression capability carries the decision; peers without it retain their original global-compression behavior. No wire format, encryption, integrity check or destination safety rule changed.

One focused regression test covers an incompressible body with quiet ends and the inverse case. `go test ./src/croc ./src/compress` passed, as did the targeted race checks. Full-file transfers from the new sender to the released receiver and from the released sender to the new receiver both passed SHA-256 verification. A full 532-file croc Git-clone transfer also passed its manifest, commit and `git fsck --full` checks (3.77 seconds; validation only). No CI workflow or recurring benchmark job was added.

## Reproduce

The base source commit is `f5e643f0dcd76b3b39e768a95ca58720b8a73085`; the uncommitted patch and its hash are retained with the artifacts. Both executables report 11.6.1, so binary hashes and the source patch distinguish this local build from the release. Exact sources, hashes, full-precision trials, round order and diagnostic records are in [CLI_AUDIO_BENCHMARK.json](CLI_AUDIO_BENCHMARK.json). Derphole used matching v0.18.2 release binaries on both platforms.

```sh
CGO_ENABLED=0 go build -buildvcs=false -trimpath -tags netgo,osusergo \
  -ldflags='-s -w -buildid=' -o "$BENCH_ROOT/performance/croc-audio" .
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -trimpath \
  -tags netgo,osusergo -ldflags='-s -w -buildid=' \
  -o "$BENCH_ROOT/performance/croc-audio-linux-amd64" .

# Sender
"$BENCH_ROOT/performance/croc-audio" --yes --ignore-stdin --disable-clipboard \
  send /Users/zns/Music/audio.wav

# Receiver, in a fresh directory
CROC_SECRET=CODE /root/croc-audio-benchmark-20261101T173618Z/performance/croc-audio \
  --yes --ignore-stdin --disable-clipboard
```

The comparison control used `derphole send /Users/zns/Music/audio.wav --hide-progress` and `derphole receive CODE --hide-progress`. Codes were generated normally; SSH only orchestrated commands. Round-order seeds were `202611010201`, `202611010202`, and `202611010203`.

Local artifacts: `/Users/zns/.cache/croc-audio-benchmark/20261101T173618Z`. Remote artifacts: `/root/croc-audio-benchmark-20261101T173618Z`. The nine ranked trial records are `performance/sample-results.json`; individual logs are under `trials/perf-audio-sample-rN-VARIANT/`. Compatibility, profiles, flag diagnostics, the source patch and executables remain under `performance/`. The broader eight-tool matrix was interrupted when the user changed the goal to this optimization; its completed attempts and the interrupted AirPipe attempt remain retained and are not used in this table. Successful received payloads were removed after verification. Source and website changes were not committed, pushed or deployed by this task.

## Complete single-file comparison

The article now uses the complete 317,520,080-byte Mac-to-Toronto comparison below. Each published time is the middle of three successful transfers; every file matched the source SHA-256. Croc and derphole use the final paired optimization runs. The other unchanged tools combine their retained valid attempts with the remaining repeats completed for the article. The interrupted AirPipe attempt is retained in the raw artifacts and excluded. All selected trial IDs, measurements and failures are in the JSON.

| Tool | Time | Effective rate |
| --- | ---: | ---: |
| croc | 8.52 s | 37.27 MB/s |
| derphole | 10.65 s | 29.80 MB/s |
| wormhole-william | 126.81 s | 2.50 MB/s |
| iroh Sendme / DashBeam CLI | 129.72 s | 2.45 MB/s |
| Magic Wormhole | 150.36 s | 2.11 MB/s |
| Floe | 190.69 s | 1.67 MB/s |
| AirPipe | 200.26 s | 1.59 MB/s |
| WebWormhole | Transfer failed | — |

WebWormhole received matching bytes but reported a connection-close error. Its three-run set did not succeed, so it receives no headline time or rate. Default transport and compression settings were used throughout. The current article labels croc by its tool name; exact tested builds remain documented above.

## Archived audio figures

These are the earlier figures replaced in the article. The main experiment was published August 23, 2026: a 303 MB audio file from Hetzner US West to DigitalOcean US East, with one timed receiver run after sender readiness. The DashBeam/Sendme row instead came from October 31, 2026, with a 317.5 MB audio.wav from Mac to Toronto; its SHA-256 matched. These figures use a different clock and input from the complete comparison above.

| Tool | Time | Effective rate |
| --- | ---: | ---: |
| croc | 7.2 s | 42.1 MB/s |
| derphole | 9.6 s | 31.6 MB/s |
| Magic Wormhole | 11.5 s | 26.3 MB/s |
| iroh Sendme | 20.7 s | 14.6 MB/s |
| wormhole-william | 42.4 s | 7.15 MB/s |
| DashBeam (Sendme CLI) | 109.5 s | 2.90 MB/s |
| AirPipe | 110.0 s | 2.75 MB/s |
| Floe | 116.6 s | 2.60 MB/s |
| WebWormhole | 156.5 s | 1.94 MB/s |
