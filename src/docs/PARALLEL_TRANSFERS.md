# Parallel transfers (v2 data plane)

Croc can move file data over a feature-negotiated v2 data plane. Both peers must
support it; older clients, browsers, --stdout, and text transfers automatically
stay on the legacy path.

## What changes

- Data frames carry a batch ID, file index, offset, and flags inside the AEAD
  envelope, so one connection can multiplex several files safely.
- Files are scheduled in batches. Large files use a batch of one; small files
  share a round-robin queue so a single control round trip does not stall every
  file.
- Each data connection keeps a bounded in-flight window, which keeps high-RTT
  links busy without unbounded memory use.
- Chunk size, worker count, and in-flight window start from a conservative
  profile and are adjusted every 500 ms from local throughput, CPU/memory
  pressure, send backpressure, and one-second control-channel RTT probes.
  Chunk-size changes apply to the next batch: a file's chunk size is fixed for
  its whole transfer so offsets and journals stay stable.
- Received chunks are tracked in a per-file journal. After a crash or reconnect,
  only chunks without a durable record are requested again; a final whole-file
  hash still verifies the result.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| --parallel-data | auto | auto, off, or force. force still requires peer support. |
| --parallel-connections | 0 | Maximum active data workers, 1-8; 0 lets the controller decide. |
| --chunk-size | 0 | Requested/capped chunk size (64 KiB-4 MiB, power of two, or 0 for auto). |
| --parallel-files | 0 | Maximum files per small-file batch, 1-16; 0 for auto. |
| --no-autotune | false | Skip probing and use fixed conservative parameters. |
| --no-resume | false | Disable persistent chunk journals. |
| --resume-verify | full | Verify completed chunks on resume: full, sample, or none. |
| --stats | false | Print a local transfer summary when finished. |

--no-multiplexing limits the relay path to one data connection and disables
multi-file batches. Large single files still use the v2 frame format.

## Runtime tuning

Each v2 batch samples every 500 ms. The sender measures goodput, time blocked
on its in-flight queue, process CPU and heap use, and the RTT of probes sent
over the encrypted control channel once per second. The receiver samples
goodput, CPU/memory, and the same RTT probes. Samples drive the AIMD/BDP
controller:

- The sender may activate additional idle data workers and increase the
  in-flight limit. When RTT doubles, sends block, or CPU/memory pressure is
  high, it reduces the limit used by newly started workers and later batches.
- The receiver uses the updated chunk size when it selects the next batch.

Tuning never changes a chunk size in the middle of a file, never exceeds the
relay/Tailcat connection count, and never bypasses --throttleUpload.
--no-autotune disables the samplers and probes and keeps the initial
conservative limits.

## Resume journals

Journals are written next to the destination as <name>.crocstate with mode 0600.
They contain the source size, source hash, chunk size, completed bitmap, and
per-chunk hashes; they do not contain file contents. A successful transfer
removes the journal. If a journal is corrupt or does not match the incoming
file, croc discards it and transfers the file again.

Small files (at most 1 MiB) are written to a hidden `.<name>.crocpart-*`
temporary file and renamed into place only after the whole-file hash passes.
Large files use in-place writes with the sidecar journal. In both cases the
journal is kept on interruption, so a later run resumes the missing chunks and
finishes the rename or in-place file.

Use --no-resume when a filesystem cannot tolerate sidecar files. The final
whole-file hash is always checked when v2 is active.

## Validation

CI runs the resume, performance-controller, protocol, and TestParallel
transfer regressions on Ubuntu 24.04, macOS 15, and Windows 2025. Reconnect
resume tests run on Linux. Local verification also cross-builds the repository
for Windows, macOS arm64, and Linux 386.

## Compatibility

| Peer combination | Behavior |
| --- | --- |
| New + new | v2 batch data plane |
| New + old | legacy path automatically |
| New + web receiver | legacy path; the web client keeps its own parallel sockets |
| Relay or Tailcat | same relay protocol; v2 only changes encrypted data frames |

The relay/Tailcat connection count is unchanged. To use more data connections,
run a relay with more --ports or use Tailcat's configured streams.

## Web receiver

Web receivers do not advertise the v2 features. A CLI sender detects the
missing feature during PAKE and automatically uses the legacy 32 KiB
single-file framing; no flag is needed and no batch-ready or journal state is
created for the browser session. Browser large-file transfers keep their
existing multiple-socket behavior and download fallback.

The browser side is not silently upgraded. A future web v2 implementation must
negotiate `parallel-data-v2` during PAKE, decode the 24-byte header and batch
messages inside the AEAD envelope, and implement batch completion before it
can enable the new scheduler. Until then, Go-to-Go transfers use v2 and
browser transfers remain on the legacy path.
