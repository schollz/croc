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
  context. It also adds `Server.RemoveAllowedClient`, paired with upstream's
  runtime add method, so one-time SSH-share client keys can be revoked.
- `croc_authorization.go`: exposes the deterministic Tailcat address for a node
  key so croc can bind an authenticated invitation role to its tunnel source.
- `croc_status.go`: adds a client status accessor for direct/DERP path
  transitions and final byte reporting.

All other imported source and test files are unchanged. `SOURCE_HASHES.sha256`
records SHA-256 hashes of the exact upstream files before the local patch.
The resulting croc-patched `tailcat.go` has SHA-256
`f009ffd7902970749d05546cd5e8d28e0c71e003d925c9fa4db6afed429437d9`.

Croc deliberately uses stable `tailscale.com v1.102.3` instead of Tailcat's
pre-release Tailscale pseudo-version. The complete imported upstream test suite
passes with that stable release. The gVisor version remains Tailcat's pinned
`v0.0.0-20260224225140-573d5e7127a8`.

## Updating

1. Select and record an immutable upstream Tailcat commit.
2. Download the source archive and verify its commit identity.
3. Re-copy only the files listed above, preserving `LICENSE`.
4. Regenerate `SOURCE_HASHES.sha256` from the unmodified upstream files.
5. Reapply and review the documented `Server.StartContext` and allowlist
   revocation changes, and keep
   croc-only additions in `croc_*.go` where possible.
6. Run `go test ./internal/tailcat ./src/tailcattransport`, the focused race
   tests, `go test ./...`, and croc's release cross-build matrix.
