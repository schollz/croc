# SCP and rsync transfer comparisons — November 1, 2026

These SSH baselines add two tools to both workload tables in [How fast is croc?](https://getcroc.com/blog/croc-cli-speed-comparison). The [JSON record](CLI_SSH_BENCHMARK.json) retains exact versions, sources, executable hashes, all individual trials, commands and integrity results. Raw payloads, build trees and logs remain outside tracked source.

## Inputs and timing

The sender was the same Mac14,15 arm64 running macOS 26.5.2; the receiver was the same Ubuntu 24.04.4 LTS amd64 DigitalOcean `tor1` server, `root@134.122.43.205` (one CPU, 961 MiB RAM). Both inputs were checked against the existing comparison records before and after the experiment.

- Repository: the unchanged full croc clone, commit `cc781806baaa17d29d94209eecf0c85bc178d9e1`, **532 files / 20,708,906 bytes**, including all 28 `.git` files. Its path/size/SHA-256 manifest matches the existing folder comparison.
- Single file: `/Users/zns/Music/audio.wav`, **317,520,080 bytes**, SHA-256 `a692d566b00e9a4173ee20e2b7113cb12b204300e39e2953ab0c3dc8f26d9ec4`.

Four small untimed preflights checked each tool with a directory containing hidden and empty files, and with a single file. Then three sequential rounds shuffled the four tool/workload pairs using seeds `202611010601`, `202611010602`, and `202611010603`. The twelve measured transfers all used new, empty destination directories. This is a full-copy test; rsync had no prior data to skip, resume or delta against. These are additional runs, rather than part of the earlier tools’ shuffled rounds.

A local monotonic clock starts immediately before launching `scp` or `rsync` and stops at successful sender exit, after the remote completion acknowledgement. It includes a fresh SSH connection, key exchange, authentication, protocol setup, scanning and the complete copy. Preparing the receive directory and the independent integrity checks are outside the clock. The other tool rows use sender launch through their usable-payload notification over the control channel; no cross-machine timestamp subtraction is used in either case. Per-trial timeout: 900 seconds.

SCP used its current default SFTP protocol and `-r` for the clone. Rsync used `-a` over SSH. Neither command enabled compression, prepacked an archive, forced a cipher, cleared filesystem caches, or requested extra filesystem synchronization. Payload connections used `ControlPath=none` and `ControlMaster=no`; the separate SSH control connection only prepared and verified destinations. Process output went through pipes into retained logs. The untimed connection diagnostic selected `chacha20-poly1305@openssh.com`, `sntrup761x25519-sha512@openssh.com`, and no compression.

Every successful repository copy matched the complete relative-path manifest and byte count, checked the commit, and passed `git fsck --full`. Every single-file copy matched its exact size and SHA-256. Received payloads were removed only after verification. Git checks use command-scoped safe.directory for the exact prepared benchmark path, because rsync archive mode preserves source ownership. The first rsync clone already matched every file but Git initially rejected its ownership; that same copy then passed HEAD and fsck checks without a resend or timing change. The initial diagnostic and recheck are retained, and no global Git setting changed.

## Results

Time is the middle of three successful runs. Effective rate is `source_bytes / 1,000,000 / time_seconds`. A failed three-run set would receive no time or rate.

| Workload | Tool | Round 1 (s) | Round 2 (s) | Round 3 (s) | Time (s) | MB/s |
| --- | --- | ---: | ---: | ---: | ---: | ---: |
| Git repository | SCP | 158.463289 | 158.516781 | 162.817416 | 158.516781 | 0.130642 |
| Git repository | rsync | 4.303942 | 4.497940 | 5.049810 | 4.497940 | 4.604087 |
| Single audio file | SCP | 159.515595 | 140.410412 | 174.762733 | 159.515595 | 1.990527 |
| Single audio file | rsync | 133.133530 | 171.762514 | 186.117099 | 171.762514 | 1.848599 |

SCP and rsync need access to a reachable SSH server, such as a public server, a forwarded port, or a host on a private network. The other tools in the article arrange rendezvous/relaying without requiring the user to expose an SSH service. The server was already publicly reachable; no SSH daemon settings, host keys, firewall rules or listening ports were changed.

## Versions, builds and commands

SCP and its SSH client were built from the current OpenSSH **10.6p1** portable release. The receiver used its existing Ubuntu OpenSSH **9.6p1** SSH/SFTP service. The source SHA-256 matched the checksum in the official [OpenSSH release notes](https://www.openssh.org/txt/release-10.6). Rsync **3.5.1** was built on both computers from the same official source tarball; its signature verified with Andrew Tridgell’s release key, fingerprint `9FEF112DCE19A0DC7E882CB81BB24997A8535F6F`. All executable hashes, release URLs, capabilities and build flags are in the JSON.

Rsync used `./configure --prefix=BENCH_ROOT --with-included-popt --disable-idn`, then `make` and `make install`. macOS include/library flags point to Homebrew OpenSSL, xxhash, zstd and lz4. Linux build dependencies came from Ubuntu apt. IDN support is disabled on both builds; the destination is a numeric IP. OpenSSH used `./configure --prefix=BENCH_ROOT --with-ssl-dir=/opt/homebrew/opt/openssl@3` and `make ssh scp`; only those two clients were copied into the isolated prefix. Initial configure failures and their corrections remain in the raw build logs and are excluded from the trial results.

The following commands illustrate the measured payloads. `SSH_OPTS` is `-F /dev/null -o BatchMode=yes -o StrictHostKeyChecking=yes -o ControlMaster=no -o ControlPath=none -o ControlPersist=no -o ConnectTimeout=15`. It isolates personal transport settings while retaining known-host verification and normal SSH authentication. Each destination is new. Exact argument arrays are in the JSON.

```sh
# Repository, including hidden files and .git
"$BENCH_ROOT/bin/scp" -B -S "$BENCH_ROOT/bin/ssh" $SSH_OPTS -r \
  "$FROZEN_CLONE" root@134.122.43.205:"$FRESH_DEST/"
"$BENCH_ROOT/bin/rsync" -a -e "$BENCH_ROOT/bin/ssh $SSH_OPTS" \
  --rsync-path="$REMOTE_ROOT/bin/rsync" \
  "$FROZEN_CLONE" root@134.122.43.205:"$FRESH_DEST/"

# Single file
"$BENCH_ROOT/bin/scp" -B -S "$BENCH_ROOT/bin/ssh" $SSH_OPTS \
  /Users/zns/Music/audio.wav root@134.122.43.205:"$FRESH_DEST/"
"$BENCH_ROOT/bin/rsync" -a -e "$BENCH_ROOT/bin/ssh $SSH_OPTS" \
  --rsync-path="$REMOTE_ROOT/bin/rsync" \
  /Users/zns/Music/audio.wav root@134.122.43.205:"$FRESH_DEST/"
```

Protocol references: [SCP manual](https://man.openbsd.org/scp), [rsync manual](https://download.samba.org/pub/rsync/rsync.1).

## Evidence

Local prefix: `/Users/zns/.cache/croc-ssh-benchmark/20261101T213256Z`. Remote prefix: `/root/croc-ssh-benchmark-20261101T213256Z`. Each `trials/rN-TOOL-PAYLOAD/` directory retains the request, sender log, orchestration diagnostics and result. Remote trial directories retain verification results. The frozen manifest, four preflights, selected SSH algorithms, source signature status, source archives, build logs and executable hashes remain alongside them. `benchmark.py` and `verify_remote.py` implement the timing and verification; their SHA-256 hashes are recorded. No CI workflow, source-code optimization, commit, push or deployment was added.

The two focused blog test files passed all 12 tests, TypeScript checking and the production build passed, and both tables fit at 1440 × 1000 and 390 × 844 without page overflow or console errors. Static and rendered metadata agree on the ten-tool title, original publication date, November 1 modification date, 654 words and four-minute reading time. Screenshots remain in the local prefix.
