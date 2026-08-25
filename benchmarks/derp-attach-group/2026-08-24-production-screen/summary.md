# AttachGroup productionization screen — 2026-08-24

> Historical decision: automatic 8/4 selection was later enabled for v11.3.2.
> The measurements and original promotion assessment below are preserved
> unchanged.

This is a short engineering screen, not a promotion record. It used one paired
256 MiB incompressible transfer per direction and topology between the local
macOS arm64 host and `134.122.43.205` (Linux amd64). Every received file matched
its source SHA-256.

## Reliability finding and fix

The first 8-stream/4-path run established raw-direct forward, but the reverse
attempt fell back to the one-stream manager path with
`raw-candidates-timeout`/`raw-selection-timeout`. AttachGroup negotiation had
created a second ready-message subscription after the claim decision, so an
early authenticated candidate message could remain queued on the original
lossless subscription.

The implementation now keeps the subscription created before the claim and
uses it for candidate exchange, selection, QUIC readiness, and the final peer
result. After that fix, all six screened directions across 8/4, 8/2, and 4/2
proved raw-direct on both peers. Five consecutive hermetic raw setups also
passed under the race detector.

## One-pair screen results

| Streams/paths | Forward legacy → candidate | Reverse legacy → candidate | Candidate raw setup | Resource observation |
| --- | ---: | ---: | ---: | --- |
| 8/4 | 66.80 s → 18.25 s | 14.39 s → 12.77 s | 1.93–1.94 s | Fastest wall samples; peak RSS exceeded the 5% gate. |
| 8/2 | 84.48 s → 48.81 s | 13.04 s → 12.66 s | 1.92–1.95 s | Best memory screen; forward Mac RSS was +3.8%, reverse Linux RSS was +10.0%. |
| 4/2 | 65.77 s → 34.46 s | 10.29 s → 13.77 s | 1.92–1.94 s | Reverse normalized goodput regressed in this sample. |

The adjacent iperf3 capacity results varied heavily, including one shortened
forward control that was inconsistent with the subsequent croc goodput.
Consequently these single-pair normalized ratios cannot support a confidence
interval. The compact initial and post-fix retry measurements are consolidated
in `results.csv`; raw records and per-peer logs remain outside Git.

## Decision

Eight streams over four raw paths remains the selected topology because it was
the fastest candidate in both directions during the broader topology screen.
Automatic feature advertisement remains disabled because the paired 8/4
qualification did not improve normalized reverse-direction goodput. Promotion
still requires a future qualification to pass the documented performance and
lifecycle gates.
