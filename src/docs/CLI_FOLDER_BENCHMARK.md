# Repository transfer benchmark — November 1, 2026

This is the refreshed multiple-file comparison used in [How fast is croc?](https://getcroc.com/blog/croc-cli-speed-comparison). It replaces the earlier draft measurements. The same frozen clone and current croc executables used in the audio comparison were retained. Exact installations, binary hashes, individual trials and routes are in [CLI_FOLDER_BENCHMARK.json](CLI_FOLDER_BENCHMARK.json).

Sender: Mac14,15, macOS 26.5.2 arm64, 8 CPUs and 8 GiB RAM. Receiver: `root@134.122.43.205`, Ubuntu 24.04.4 LTS amd64, one CPU and 961 MiB RAM, DigitalOcean `tor1` (Toronto).

## Frozen payload

```sh
git clone https://github.com/schollz/croc.git croc
```

Commit: `cc781806baaa17d29d94209eecf0c85bc178d9e1`. The ordinary full clone contains **532 files / 20,708,906 logical bytes**, including **28 `.git` files** and **2,719 commits** of history. No dependencies or builds were added to the input. It contains no symlinks. The manifest SHA-256 is `5f2140965030d1db1929c193e99867b74ac70db055e9edc41c749866f9313b88`. Every relative path, size and SHA-256 was rechecked before and after the rerun; Git integrity passed.

## Method and results

A small untimed preflight preceded three sequential rounds, with shuffled orders using seeds `202611010401`, `202611010402` and `202611010403`. Each receiver used a fresh directory and isolated configuration. Tool-generated codes/tickets, ordinary folder handling, default compression, transport and concurrency were used. SSH only orchestrated the commands.

The local monotonic clock starts immediately before sender launch and stops when the remote usable-folder notification arrives. Preparation, code exchange, receiver startup, connection setup, payload transfer, required extraction and automation delays count. SSH establishment and subsequent verification do not. AirPipe ZIP creation and extraction are both timed. Process output goes through pipes drained into retained logs, avoiding artificial progress-log fsync overhead. The per-trial timeout is 900 seconds.

Each published time is the middle of three successful transfers. Effective rate is `20,708,906 / 1,000,000 / time_seconds`. All successful receivers reproduced every file and byte, including `.git`, matched the commit and passed `git fsck --full`. Failed sets receive no rate.

| Tool | Round 1 (s) | Round 2 (s) | Round 3 (s) | Time (s) | MB/s |
| --- | ---: | ---: | ---: | ---: | ---: |
| croc | 4.702182 | 4.280003 | 4.418808 | 4.418808 | 4.686537 |
| derphole | 4.608819 | 4.758592 | 4.919216 | 4.758592 | 4.351898 |
| iroh Sendme / DashBeam CLI | 6.371759 | 4.350014 | 8.406795 | 6.371759 | 3.250108 |
| Magic Wormhole | 6.665642 | 12.191651 | 9.138338 | 9.138338 | 2.266157 |
| wormhole-william | 12.971378 | 10.692962 | 10.008926 | 10.692962 | 1.936686 |
| AirPipe | 15.207018 | 13.317141 | 19.468238 | 15.207018 | 1.361799 |
| Floe | 57.839122 | 66.783369 | 53.253666 | 57.839122 | 0.358043 |
| WebWormhole | — | — | — | Folder transfer unsupported | — |

WebWormhole rejected the directory during preflight (`is a directory`); its waiting receiver reached the 60-second preflight limit. No tar/ZIP workaround or measured folder rate was used. Sendme represents the CLI also recommended by DashBeam, with one shared measurement set.

Recorded order:

1. wormhole-william → derphole → Floe → AirPipe → iroh Sendme / DashBeam CLI → Magic Wormhole → croc.
2. Magic Wormhole → wormhole-william → iroh Sendme / DashBeam CLI → Floe → derphole → AirPipe → croc.
3. Floe → wormhole-william → derphole → iroh Sendme / DashBeam CLI → Magic Wormhole → croc → AirPipe.

## Builds and commands

Croc was built from source commit `f41e167877b0cb2900ef1c76b2ba02f9c259de23` and reports `croc version 11.6.1`. Its tested macOS arm64 SHA-256 is `6874d4bf37d18a8115d971527064cc04c9c9d27ccb75a631f09343931a4b6a9c`; Linux amd64 is `ee13875c09610e6c52ad3de5857e0168c577ff15efe1a3835ec8e181367d1372`. The receiver hash was checked before execution. This is the same tested build recorded in [CLI_AUDIO_BENCHMARK.md](CLI_AUDIO_BENCHMARK.md).

Build flags: `CGO_ENABLED=0 go build -buildvcs=false -trimpath -tags netgo,osusergo -ldflags='-s -w -buildid=' -o OUTPUT .`; Linux additionally uses `GOOS=linux GOARCH=amd64`. The complete source fingerprints and executable paths are in the JSON.

| Tool | Version / source |
| --- | --- |
| croc | source commit `f41e167877b0cb2900ef1c76b2ba02f9c259de23` |
| derphole | v0.18.2 |
| Sendme / DashBeam CLI | v0.37.1 |
| Magic Wormhole | 0.24.0, matching normalized Python dependency freezes |
| wormhole-william | v1.0.8 |
| AirPipe | v5.0.0 |
| Floe | v1.11.7 |
| WebWormhole | `abf852af0458ba79772d9c26ef01434165f217d8` |

Native release asset URLs, archive checksums and executable hashes are retained in the JSON. Magic Wormhole used isolated Python environments (3.12.13 on Mac, 3.12.3 on Linux); WebWormhole was compiled from upstream source. The croc executable path below is the current tested build; `BENCH_BIN` selects the matching isolated installation for each platform.

| Tool | Sender | Receiver |
| --- | --- | --- |
| croc | `croc-audio --yes --ignore-stdin --disable-clipboard send SOURCE` | `CROC_SECRET=CODE croc-audio --yes --ignore-stdin --disable-clipboard` |
| derphole | `derphole send SOURCE --hide-progress` | `derphole receive CODE --hide-progress` |
| Magic Wormhole | `wormhole send --hide-progress SOURCE` | `wormhole receive --accept-file --hide-progress CODE` |
| Sendme | `sendme send --no-progress SOURCE` | `sendme receive --no-progress TICKET` |
| wormhole-william | `wormhole-william send --hide-progress SOURCE` | `wormhole-william receive --hide-progress CODE`, with `y\n` on stdin |
| AirPipe | `airpipe send --mode p2p SOURCE` | `airpipe download WORD WORD WORD WORD NN`, then timed Python ZIP extraction |
| Floe | `floe send SOURCE` | `floe receive --yes CODE` |

`SOURCE` is the absolute path to the frozen `croc` clone. Each trial used new `CROC_CONFIG_DIR` and `XDG_CONFIG_HOME` directories, `NO_COLOR=1`, and `TERM=dumb`. Inherited tool-specific relay/proxy overrides were cleared. The sender used normal code generation; only the croc receiver received `CROC_SECRET`. Exact orchestration commands are in the retained harness.

## Retained evidence

Current artifacts: `/Users/zns/.cache/croc-cli-benchmark/20261101T151339Z/folder-refresh-20261101T191225Z`. Payload, manifest, installations and raw per-trial logs remain under `/Users/zns/.cache/croc-cli-benchmark/20261101T151339Z`; remote originals are under `/root/croc-cli-benchmark-20261101T151339Z/trials/`. Each trial has a receiver result and log, plus local sender and SSH logs. Successful receive trees and expendable archives were removed after verification; persistent senders were stopped. Previous full records and diagnostics remain retained alongside this refresh and are excluded from the current table.

Per-trial routes are recorded where exposed: croc uses the TCP relay for this sub-128-MiB input; Magic Wormhole and Floe identify direct paths in their logs. AirPipe reports p2p mode without identifying its final candidate. Other tools did not expose a final path in default output. No route override was used.

The two focused blog test files passed all 12 tests, TypeScript checking passed, and the production web build passed. The rendered article was checked at 1440 × 1000 and 390 × 844: all three table columns fit without page overflow or browser errors. Static and rendered metadata agree on the original publication date, November 1 modification date, 584 words and three-minute reading time. The single-file article section, publication identity and artwork are unchanged. Screenshots, the build log and the QA record are retained with the current artifacts.

No CI job, commit, push or deployment was performed for this rerun.
