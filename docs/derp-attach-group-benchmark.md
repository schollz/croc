# DERP AttachGroup activation gate

Production selection of `experimental-derp-attach-group-v1` is controlled by
`derpAttachGroupAutomaticSelection` in `src/croc/croc.go`. The normal build
keeps this disabled until the fixed eight-stream/four-path profile passes the
live gate below. With the gate disabled, croc continues to use legacy
single-stream Attach even though the adapter and benchmark support AttachGroup.

## Hermetic framing check

The Go benchmark compares legacy Attach with an eight-stream local AttachGroup
using croc's 32 KiB framing, range offsets, deterministic incompressible data,
and production AES-GCM encryption:

```bash
GOWORK=off go test -tags croc_attach_bench ./src/croc -run '^$' \
  -bench '^BenchmarkDERPAttachFramedTransfer$' -benchtime=8192x -count=3
```

This is a CPU and allocation regression check, not network promotion evidence.

## Streamlined live gate

Build only the selected 8/4 profile and its one-stream force-relay fallback:

```bash
GOWORK=off BENCH_UPLOAD=1 \
  benchmarks/derp-attach-group/build_variants.sh
BENCH_FIXTURE_MIB=256 BENCH_FIXTURE_SUFFIX=256m \
  benchmarks/derp-attach-group/prepare_fixtures.sh
```

Run three alternating legacy/candidate pairs in each direction. Every transfer
uses strict DERP, disabled compression, production encryption/framing, and an
adjacent ten-second same-direction iperf3 control:

```bash
BENCH_PAIRS=3 \
BENCH_LOCAL_SOURCE=/tmp/croc-attach-bench/data/fixture-local-256m.bin \
BENCH_REMOTE_SOURCE=/tmp/croc-attach-bench/data/fixture-remote-256m.bin \
BENCH_RESULT_DIR=/absolute/path/to/results \
  benchmarks/derp-attach-group/live_pair_benchmark.sh
python3 benchmarks/derp-attach-group/analyze_results.py \
  /absolute/path/to/results --candidate group-s8-p4 >analysis.json
```

The performance portion passes only when all twelve transfers match byte count
and SHA-256, all six candidate runs prove `mode=raw-direct`, `streams=8`, and
`raw_paths=4` on both peers, and the candidate's paired normalized-goodput
geometric mean exceeds one in both directions. CPU, fixed peak RSS, and setup
timings are recorded but do not block the selected throughput profile.

Only after the performance portion passes, run one compatibility transfer in
each direction against the latest stable croc and require legacy Attach; run a
force-relay transfer and require one manager stream; then run ten small
cancellation/reconnect cycles and require bounded shutdown without descriptor,
socket, or process growth. A failure in any required stage leaves automatic
selection disabled and stops the gate without running later stages.

Keep only the scripts, aggregate analysis, CSV rows, environment summary, and
decision in Git. Preserve raw process logs and iperf JSON outside the repository
while the result is being reviewed.
