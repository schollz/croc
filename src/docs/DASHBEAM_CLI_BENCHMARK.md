# DashBeam CLI transfer check — October 31, 2026

DashBeam's [official CLI download](https://www.dashbeam.net/en/downloads)
installs [iroh Sendme](https://github.com/n0-computer/sendme). This check used
the published Sendme 0.37.1 binaries on macOS arm64 and Linux x86_64.
Both Sendme release archives were checked against
the SHA-256 digests supplied by GitHub before installation.

The sender was the local Mac; the receiver was `root@134.122.43.205`
(DigitalOcean's `tor1` region, Toronto).
The input was `~/Music/audio.wav`, exactly 317,520,080 bytes (302.81 MiB).
The sender was waiting with its ticket before timing began.
The transfer used a new, empty receive directory and the default transport
settings. The receiver's Python `time.perf_counter()`
measured command startup through successful command exit; SSH connection time
and the subsequent SHA-256 check were outside the timed interval.

| CLI | Receiver elapsed | Effective rate | Received bytes | Exit status |
| --- | ---: | ---: | ---: | ---: |
| DashBeam / Sendme 0.37.1 | 109.481942 s | 2.900205 MB/s | 317,520,080 | 0 |

Rates use decimal MB: received bytes / 1,000,000 / elapsed seconds.
The source and received file had the same SHA-256:

```text
a692d566b00e9a4173ee20e2b7113cb12b204300e39e2953ab0c3dc8f26d9ec4
```

Sender command:

```sh
sendme send --no-progress ~/Music/audio.wav
```

Receiver command, in an empty directory:

```sh
sendme receive --no-progress YOUR-TICKET
```

Raw receiver results and logs remain on the receiver in:

```text
/root/croc-dashbeam-trial-20261031/sendme-1/result.json
/root/croc-dashbeam-trial-20261031/sendme-1/receiver.log
```
