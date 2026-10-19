# Tailcat upstream snapshot

This directory vendors the networking core of
[`tailscale/tailcat`](https://github.com/tailscale/tailcat) at commit
`c04c5afee401df40e620db8ae108e957ae07bcd9` (2026-08-27).

Imported upstream files:

- `LICENSE`
- `disco.go`
- `pickregion.go`
- `pickregion_js.go`
- `tailcat.go`
- `wire.go`
- `export_test.go`
- `tailcat_test.go`
- `wire_test.go`

The CLI, SSH support, README embedding, web demo, image, Nix files, module files,
and repository configuration are intentionally omitted. Croc uses the Tailcat
networking and wire layers directly and does not expose Tailcat's standalone
wire protocol.

## Croc-local changes

- `tailcat.go`: `Server.StartContext` is a minimal variant of `Server.Start`
  that lets croc's coordinated 30-second setup deadline cancel DERP-map
  discovery. `Start` retains upstream behavior by calling it with a background
  context. Its package overview omits the standalone Tailcat SSH-server claim,
  because croc does not import that CLI integration.
- `croc_authorization.go`: exposes the deterministic Tailcat address for a node
  key so croc can bind an authenticated invitation role to its tunnel source,
  and adds `Server.RemoveAllowedClient` so one-time client keys can be revoked.
- `croc_status.go`: adds a client status accessor for direct/DERP path
  transitions and final byte reporting.
- `tailcat.go`, `pickregion.go`, `pickregion_js.go`, `wire.go`: backport the
  DERP region ID migration from upstream commit
  [`93727442357477eed62e939327dff26c648aabd1`](https://github.com/tailscale/tailcat/commit/93727442357477eed62e939327dff26c648aabd1).
  Networking uses `tailcfg.DERPRegionID`; the independent wire types use
  `int64`, preserving existing CBOR encodings and numeric JSON fields.
- `tailcat.go`: adapt the peer configuration callback to return
  `wgcfg.PeerConfig`, following the API migration in upstream commit
  [`522df6f59825dc8da3a1fbcaaf4e549594d3e458`](https://github.com/tailscale/tailcat/commit/522df6f59825dc8da3a1fbcaaf4e549594d3e458).
  Only `AllowedIPs` is populated; the optional preshared key stays zero.
- `tailcat_test.go`: includes croc's client revocation regression test.
- `croc_compat_test.go`: verifies historical connection blobs, 64-bit region
  IDs, numeric JSON display, and peer configuration compatibility.

`SOURCE_HASHES.sha256` records SHA-256 hashes of the exact original upstream
files before local patches. These hashes retain the snapshot's provenance;
selective compatibility backports do not change the recorded snapshot commit.
The patched networking source hashes are listed below.

```text
b35b0c74dd88209210756d72e0c9413b8f53078cdf8c57d7cf3b569336fa6af3  tailcat.go
6db28d7ab032ac35e399468b328a74cfc563066bcc256d8883041cb3cfcceab4  wire.go
749cf062c79f531b02b6891dcbcf5fce1e8b1b1286e95a3755c49a2538424a0b  pickregion.go
9c188c59c09aa67ab5d96d2c95eeeadc46e5e6258a9ef52a648d87464fe6e91b  pickregion_js.go
```

Croc uses stable `tailscale.com v1.104.1` instead of Tailcat's pre-release
Tailscale pseudo-version, requiring Go 1.27.1 or newer. The gVisor dependency
remains pinned at `v0.0.0-20260926034703-66ed76bcb72f`.

## Updating

1. Select and record an immutable upstream Tailcat commit.
2. Download the source archive and verify its commit identity.
3. Re-copy only the files listed above, preserving `LICENSE`.
4. Regenerate `SOURCE_HASHES.sha256` from the unmodified upstream files.
5. Reapply and review the documented croc-local changes and compatibility
   backports, refresh the patched source hashes above, and keep croc-only
   additions in `croc_*.go` where possible.
6. Run `go test ./internal/tailcat ./src/tailcattransport`, the focused race
   tests, `go test ./...`, and croc's release cross-build matrix.
