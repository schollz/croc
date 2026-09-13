# Distribution preparation

These are local candidate recipes and review materials, not submitted updates.
Use the [status table](status.md) to distinguish completed checks from blockers.
The upstream DEB/RPM workflow can proceed independently of repository acceptance.

## Recipe placement

| Directory | Destination in the distro source tree |
|---|---|
| `arch/` | Existing croc packaging repository, with `croc.1` next to `PKGBUILD` |
| `alpine/` | `community/croc/` in aports; follow existing MR 107977 |
| `void/` | `template` in `srcpkgs/croc/`, manual in `srcpkgs/croc/files/`, both patches in `srcpkgs/croc/patches/`; follow PR 62515 |
| `nix/` | `pkgs/by-name/cr/croc/`; follow PR 562700 rather than opening a duplicate version update |
| `gentoo/` | Ebuild, Manifest, and metadata in `net-misc/croc/`; manual and both patches in `net-misc/croc/files/` |
| `fedora/` | Follow review 2432373; includes a checked vendoring configuration and license report |
| `opensuse/` | Development OBS package; source archive and manual are `Source0` and `Source1` |
| `debian/` | `debian/` in an unpacked upstream source tree; dependency list is still incomplete |

The manual is copied into recipes because v11.5.2 does not contain it. Future
updates can install it from `packaging/croc.1` once that file is released.
The identical `completions.patch` and `package-update.patch` backport the upstream
Bash/Zsh fixes and package-managed update guard to v11.5.2. Drop each patch once
the source release contains it. RPM recipes place them beside their spec and
other source files; Void uses its `patches/` directory and Gentoo its `files/`.
The distro recipes preserve existing architecture policy. Arch retains epoch 1
and its PIE/external-linker flags. Alpine retains `arch="all"` and its existing
update-notification patch, including that patch's skipped notification tests.
Void retains the orphaned maintainer field. Gentoo retains its four unstable
keywords. No candidate adds a running background service.

## Debian and unbundled Go dependencies

The reviewed [pkg.haus packaging](https://github.com/pkghaus/packages/tree/master/croc)
already tracks 11.5.2 and provides useful install, smoke-test, and manual-generation
work. Its build enables automatic toolchain downloads and does not use dh-golang.
Its historical version-restamping workaround should not be copied: release tags
now contain the requested version. No pkg.haus code was copied into this candidate.

The Debian candidate uses dh-golang, `GOTOOLCHAIN=local`, and GOPATH mode so it
does not quietly fetch or vendor missing libraries. It can produce a source
package, but is **not yet buildable for official inclusion**: its Build-Depends
must be expanded after each library has a verified Debian package and compatible
version. Debian sid currently has `golang-1.27` 1.27.1-2 and `golang-defaults`
2:1.27~1. The trixie source-package check used Go 1.24 and could not build it.

`cli-dependencies.json` records 53 module versions used by the Linux CLI with CGO
disabled. `go-dependencies.json` records all 632 entries from `go list -m all`,
including modules needed for other packages, tests, and optional dependency paths.
The module graph is not a list of 632 linked libraries. Neither inventory is a
claim that those modules are already packaged by Debian.

`debian-dependencies.json` maps 45 of those 53 modules to candidates in the
2026-09-13 sid source index using its `Go-Import-Path` metadata. Eight have no
exact metadata match, which requires further investigation rather than proving
that no usable package exists. Candidate versions and binary package names are
recorded for comparison. None of these matches establishes API compatibility.
Reproduce the mapping with `map-debian.py --sources Sources.xz --date YYYY-MM-DD
--output debian-dependencies.json` using the official sid source index.

For each module, map the import path to a Debian source and `-dev` binary package,
compare its version and patches with the tagged `go.mod`/`go.sum`, package missing
modules in dependency order, and record licensing. Pay particular attention to
the Tailscale/gVisor dependency chain and pseudo-versions. Populate Build-Depends,
then run sbuild, lintian, and autopkgtest with network access disabled. Do not
change the upstream dependency versions or remove CLI features to make a build
pass. The existing test suite adds dependencies beyond the linked CLI inventory.

The sponsorship route to investigate is the
[Debian Go Packaging Team](https://go-team.pages.debian.net/packaging.html):
coordinate source ownership, an ITP, dependency packaging, and sponsorship with
the team. These are separate approval-gated messages. Ubuntu official inclusion
can follow Debian and archive synchronization; an upstream DEB is not evidence
of official Debian or Ubuntu repository acceptance.

## Outstanding distro-specific gates

- **Void:** the current compiler template is Go 1.26.5. The candidate requires
  1.27 and remains blocked until an appropriate compiler is available. Test with
  xbps-src on glibc and musl before describing the existing PR 62515 as tested.
- **Fedora:** rawhide's old source repository is retired. Review 2432373 already
  proposes the source rename to `croc` and vendoring. The old linked SRPM now
  returns 404, so its `go-vendor-tools.toml` was recreated with go-vendor-tools
  0.13.0. The vendor archive preserved go.mod/go.sum. Rawhide aarch64 rpmbuild,
  tests and installation passed. Final rpmlint reports zero errors and two
  warnings: the locally generated vendor archive lacks a public source URL, and
  two installed license notices have identical text. The generated expression
  and explicit mappings are recorded for review. Run mock for target Fedora
  releases before submitting updated source URLs.
  Do not add Obsoletes just for the old source-package name: the installed binary
  package was already named croc.
- **openSUSE:** the development recipe passed a native Tumbleweed aarch64 build,
  tests, installation and rpmlint (zero errors and warnings) with Go 1.27.
  It still needs an OBS build. Leap's upstream RPM lifecycle test is separate from an official source
  package build. The historical security ticket was reread in Chrome: it closed
  after package removal. Obtain security-team guidance before pursuing Factory.
- **Gentoo:** the September 20 deadline is urgent, but the incomplete file listing
  before acceptance remains unresolved. The candidate passed native arm64 compile, tests, installation, binary packaging
  and pkgcheck with Go 1.27.1. Other keyworded architectures remain untested.
  The dependency license mappings still need distro review. See [security evidence](security-evidence.md).

Coordinate updates with existing maintainers and open submissions. Packaging
validation and downstream acceptance are separate milestones.
