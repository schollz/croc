# 8/4 AttachGroup qualification — 2026-08-24

## Decision

Do not enable automatic AttachGroup selection and do not publish the croc
integration. All six candidate transfers established raw-direct eight-stream,
four-path groups and passed SHA-256 verification, but normalized goodput
regressed in the server-to-Mac direction.

| Direction | Legacy median | 8/4 median | Paired ratio | 95% interval |
| --- | ---: | ---: | ---: | ---: |
| Mac to server | 34.84 Mbit/s | 95.13 Mbit/s | 2.449x | 1.082x–6.484x |
| Server to Mac | 122.17 Mbit/s | 106.66 Mbit/s | 0.762x | 0.579x–0.933x |
| Combined | — | — | 1.366x | 0.781x–2.734x |

The median normalized-goodput values were 0.561 versus 1.174 forward and
1.304 versus 0.898 reverse. Candidate CPU/GiB improved in both directions.
Median raw setup was 1.96 seconds forward and 1.95 seconds reverse; total group
setup was about 3.1 seconds.

## Method and gate status

- Local peer: Apple M2 MacBook Air, Darwin arm64, Go 1.27.0.
- Remote peer: Ubuntu Linux amd64 at `134.122.43.205`.
- Three alternating legacy/candidate 256 MiB pairs in each direction.
- Strict DERP, disabled compression, croc production framing/encryption, and an
  adjacent ten-second iperf3 control for every transfer.
- PASS: 12/12 transfers matched byte count and SHA-256.
- PASS: 6/6 candidates proved `raw-direct`, eight streams, and four paths on
  both peers.
- PASS: forward paired normalized goodput improved.
- FAIL: reverse paired normalized goodput regressed.

The compatibility, force-relay, and cancellation stages were not run because
the performance stage is an earlier blocking condition. Peak RSS and CPU are
recorded in `analysis.json`; fixed RSS overhead was observational rather than
blocking. Raw logs and iperf JSON are retained in the external work backup and
are intentionally not checked into Git.
