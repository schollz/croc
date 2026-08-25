# Live DERP AttachGroup benchmark — 2026-08-24

## Result

The fork produces a large, repeatable file-transfer gain on the
Mac-to-server direction, and a smaller aggregate gain in the reverse direction.
It is **not ready for automatic selection** because raw-direct startup succeeded
in only 2 of 5 reverse candidate runs and the server-side RSS gate regressed.

| Direction | Variant | Raw runs | Median wall | Median goodput | Median normalized | CPU seconds/GiB | Server RSS median/max |
|---|---:|---:|---:|---:|---:|---:|---:|
| Mac → server | legacy/1 | — | 376.86 s | 22.79 Mbit/s | 0.302 | 156.66 | 41.5 / 49.7 MiB |
| Mac → server | group/8 | 5/5 | 80.08 s | 107.27 Mbit/s | 2.105 | 68.64 | 26.7 / 46.8 MiB |
| Server → Mac | legacy/1 | — | 47.91 s | 179.31 Mbit/s | 1.635 | 67.68 | 40.6 / 42.8 MiB |
| Server → Mac | group/8 | 2/5 | 40.20 s | 213.66 Mbit/s | 1.945 | 57.30 | 45.4 / 57.4 MiB |

The forward median raw goodput improved by 4.71×. Reverse results including
the three manager fallbacks improved by 19.2% at the median. The two valid
reverse raw pairs were mixed: their normalized ratios were 1.525× and 0.945×.

Paired normalized-goodput bootstrap results use seed `20260824` and 100,000
resamples:

| Sample | Pairs | Geometric-mean ratio | 95% interval |
|---|---:|---:|---:|
| Mac → server, raw | 5 | 5.113× | 2.445×–10.450× |
| Server → Mac, all candidate outcomes | 5 | 1.368× | 1.121×–1.576× |
| Server → Mac, raw only | 2 | 1.201× | 0.945×–1.525× |
| Combined, all candidate outcomes | 10 | 2.644× | 1.584×–4.782× |
| Combined, raw only | 7 | 3.380× | 1.689×–7.121× |

## Method

- Local peer: Apple M2 MacBook Air, Darwin arm64, Go 1.27.0.
- Remote peer: Ubuntu Linux x86_64, one vCPU, `134.122.43.205`.
- Payload: separate 1 GiB `/dev/urandom` fixtures for each direction.
- Five legacy/candidate pairs in each direction, alternating order.
- Every transfer used strict DERP mode, disabled compression, croc's production
  encryption/framing, and an adjacent 10-second single-stream iperf3 control.
- The legacy and candidate binaries differ only in the compile-time AttachGroup
  release gate; candidate stream count is eight.
- Every received file matched the source SHA-256 and byte count.
- Both peer logs were required to report the expected mode and stream count.

The first reverse candidate completed through manager fallback before a harness
proof-parser fix. Its 40.203-second wall time was reconstructed from the sender
log's nanosecond birth/mtime timestamps; its process times, SHA-256, iperf JSON,
and both manager summaries were preserved. Two later harness failures occurred
before DERP setup because a resumed public relay code was reused or the receiver
joined before the SSH-started sender. They were discarded and replaced with
unique-code, sender-ready-coordinated runs.

## Gate evaluation

- PASS: all 20 accepted transfers matched SHA-256 and byte count.
- FAIL: only 7/10 candidates proved raw-direct payload transport. Forward was
  5/5; reverse was 2/5, with each failure exhausting the three-second raw budget
  before successful manager-direct fallback.
- PASS: candidate median normalized goodput was positive in both directions.
- PASS: the combined paired log-ratio interval excluded zero.
- INCOMPLETE: the three observed manager fallbacks were faster than their paired
  legacy runs, but manager-direct and force-relay were not run as dedicated
  no-regression matrices.
- PASS: aggregate measured CPU/GiB improved by 56.2% forward and 15.3% reverse.
- FAIL: server median RSS increased 11.7% and maximum RSS increased 34.2% in the
  reverse direction. Local peak RSS was not captured consistently.
- INCOMPLETE: the 100-run small-transfer setup-p95 and live leak matrices were
  not run.

Keep `derpAttachGroupAutomaticSelection` disabled. Before repeating the full
gate, instrument the authenticated raw setup phases, eliminate the asymmetric
three-second startup misses, and screen four streams against eight to see
whether it retains the goodput gain with lower memory use.
