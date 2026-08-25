# DERP AttachGroup activation and benchmark record

Beginning with v11.3.2, normal builds advertise
`experimental-derp-attach-group-v1` whenever DERP is available. Two advertising
peers automatically use the fixed eight-stream/four-path profile; a peer that
does not advertise the feature continues to use legacy single-stream Attach.
There is no user-facing tuning control.

The normal-build setting is provided by `derpAttachGroupBuildEnabled` in
`src/croc/derp_attach_group_config_default.go`. The benchmark-only build tag
retains internal topology and fallback controls for reproducible testing.

## Activation decision

The original performance gate below found faster Mac-to-server transfers but a
normalized server-to-Mac regression. Automatic selection was subsequently
enabled for v11.3.2 as an explicit product decision accepting that tradeoff.
The historical measurements and gate criteria remain unchanged.

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
