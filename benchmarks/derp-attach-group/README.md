# AttachGroup benchmark records

The production candidate is fixed at eight croc streams over four raw paths.
`build_variants.sh` builds the legacy, 8/4 candidate, and force-relay binaries;
`prepare_fixtures.sh` creates incompressible fixtures; and
`live_pair_benchmark.sh` runs the alternating paired test. These topology
settings exist only in benchmark-tagged binaries.

Each dated qualification directory contains compact, sanitized evidence:
aggregate analysis, CSV measurements, environment details, and a written
decision. Exploratory topology screens keep one consolidated CSV and summary;
raw process logs and iperf JSON stay outside Git.

The latest qualification failed the reverse-direction performance condition,
so normal croc binaries continue to advertise legacy Attach only.
