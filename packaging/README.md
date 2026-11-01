# Linux release packages

The Prepare Release workflow owns version stamping, the tag, artifacts, and the
draft release. This directory only adds package production and verification.
It does not create a repository service, start a relay, or package croc-web.

## Payload and names

All twelve packages use the same CLI binaries as the six Linux archives, with
CGO disabled, `netgo,osusergo`, and explicit ARMv7/ARMv5 variants. Go 1.27.1 or newer
is required. Distro recipes may preserve their native hardening flags.

| Build | DEB | RPM |
|---|---|---|
| Linux-64bit | `croc_VERSION-1_amd64.deb` | `croc-VERSION-1.x86_64.rpm` |
| Linux-32bit | `croc_VERSION-1_i386.deb` | `croc-VERSION-1.i386.rpm` |
| Linux-ARM | `croc_VERSION-1_armhf.deb` | `croc-VERSION-1.armv7hl.rpm` |
| Linux-ARMv5 | `croc_VERSION-1_armel.deb` | `croc-VERSION-1.armv5tel.rpm` |
| Linux-ARM64 | `croc_VERSION-1_arm64.deb` | `croc-VERSION-1.aarch64.rpm` |
| Linux-RISCV64 | `croc_VERSION-1_riscv64.deb` | `croc-VERSION-1.riscv64.rpm` |

The payload contains `/usr/bin/croc`, the manual, Bash/Zsh/Fish completions, README,
upstream and word-list licenses, third-party notices, a bundled module license
collection, and package changelog/copyright files. Module notices are collected
from the exact module versions recorded in all six executables. Missing notices
stop packaging. The package license field identifies croc's own MIT license;
the additional component terms accompany the binary. Fedora and other distro
license expressions require their own complete license audit.

All files belong to root. Only the executable and directories have mode 0755;
other files have mode 0644. The only declared runtime dependency is
`ca-certificates`. There are no maintainer scripts, services, installer
registrations, or files in users' home directories.

Linux `/usr/bin/croc`, `/bin/croc`, and `/nix/store/` executables reject standalone
registration and replacement, including stale registrations. `croc update --check`
can still check upstream versions. Standalone `/usr/local/bin/croc` installations
use `croc update` to maintain the adjacent `croc-update` helper, then run the
helper to replace croc. The printed command pins the running croc with --path;
direct helper use prefers croc on PATH, including user-owned Go bin installs.
Protected standalone prefixes request elevation.
DEB/RPM packages contain only croc and never install the standalone helper.

## Local reproduction

Run from the repository root using Python 3.12+ and Go 1.27.1+. `VERSION` must match
`src/version/version.go`. Tools and generated files can live under ignored `tmp/`.

```sh
VERSION=11.5.2
mkdir -p tmp/packaging-tools
GOBIN="$PWD/tmp/packaging-tools" go install "github.com/goreleaser/nfpm/v2/cmd/nfpm@$(cat packaging/nfpm-version)"
python3 packaging/release.py build --version "$VERSION" --output tmp/linux-builds
python3 packaging/release.py packages --version "$VERSION" --artifacts tmp/linux-builds \
  --native-binary tmp/linux-builds/native-croc --nfpm tmp/packaging-tools/nfpm
python3 -m unittest discover -s packaging -v
```

nFPM is pinned in `nfpm-version`, and the script checks the tool's build metadata.
Fish completion comes from the native executable even when packaging foreign
architectures. Output files appear atomically once complete.

On Ubuntu, install `python3 rpm rpm2cpio qemu-user binutils bash zsh fish`
(`rpm2cpio` supplies the `rpm2archive` command), then run:

```sh
python3 packaging/verify.py --version "$VERSION" --artifacts tmp/linux-builds
```

This inspects every package's metadata, exact file list, owners, permissions,
dependency, license notices, completions, manual, and static ELF binary. It checks
binary equality with the corresponding archive, runs version/help for all twelve
packages, and transfers data around all six architectures through a local relay.
ARMv5 executes with `qemu-arm -cpu arm926`. A directory transfer is also checked.
The existing `TestReconnectResumesControlDrop` and `TestReconnectResumesDataDrop`
tests cover interrupted transfers and resumed data integrity.

RPM verification checks header and payload digests with `rpm -K --nosignature`
before extracting with `rpm2archive`. The packages are unsigned. This extractor
avoids the [rpmpack archive-size mismatch](https://github.com/google/rpmpack/issues/60)
that makes RPM 4.18's `rpm2cpio` exit with an error after copying the payload.
Both package formats undergo the same TAR file, ownership and permission checks
before extraction; links, duplicate files and unexpected paths are rejected.

`lifecycle.sh` runs only in a disposable root container. It installs, upgrades,
removes, reinstalls, and removes the package; tests update protection; and checks
that user configuration survives. Debian upgrades use a lower-revision fixture
with the current payload, not a claimed third-party historical package. Fedora
upgrades use the repository package when one exists. openSUSE tests a fresh
installation and reinstallation because an official prior package is absent.
The reusable lifecycle workflow covers Debian 12/13, Ubuntu 22.04/24.04/26.04,
Fedora 43/44, Leap 16, and Tumbleweed on amd64. Container tests share the host
kernel and do not prove compatibility with every historical kernel.

## Source archives and release verification

```sh
python3 packaging/release.py source --version "$VERSION" --ref TAG_OR_COMMIT \
  --verify --output tmp/croc_v${VERSION}_src.tar.gz
```

Source preparation starts with `git archive` of the stamped commit, runs
`go mod vendor` without `go mod tidy`, and checks that `go.mod` and `go.sum` are
unchanged. The offline build uses an empty module cache, `GOPROXY=off`,
`GOSUMDB=off`, and `GOTOOLCHAIN=local`. Tar entries use a fixed commit timestamp,
root ownership, normalized permissions, sorted paths, and deterministic gzip
metadata. No `.git` directory is included. Build-time dependencies are fetched
during preparation, before the offline build.

Place only release assets in a separate directory, then run:

```sh
python3 packaging/release.py checksums --version "$VERSION" --artifacts DIST
python3 packaging/release.py verify-assets --version "$VERSION" --artifacts DIST
```

Each of the 19 standalone CLI archives contains both `croc` and `croc-update`
(with `.exe` names on Windows), built from the same stamped source version.
`verify-assets` checks both executable members as well as archive checksums.
The Windows amd64 signing job signs and verifies both executables.

The exact set is 19 CLI archives, one croc-web archive, one source archive,
twelve Linux packages, and one checksum file: **34 assets**. All 33 payload files
are covered by SHA-256. Missing, extra, mislabeled, or corrupted files fail the
checks. The workflow also checks the remote draft's exact asset names after
uploading. Review and publication remain under the existing release instructions.

See the [dated status and validation record](distributions/status.md),
[source-packaging notes](distributions/README.md), and
[security evidence](distributions/security-evidence.md) before downstream submissions.

## Standalone updater validation

The updater CI job runs compiled two-command upgrade fixtures on Linux, macOS,
and Windows. Linux also exercises a root-owned prefix with passwordless sudo on
its disposable runner. Checks and current versions do not request elevation;
permission failures re-execute only the pinned installation operation. Installer
registration is a compatibility no-op for standalone installs, and old
`install.json` files are no longer required or consulted for eligibility.

Before publishing the first release containing croc-update, download the Windows
amd64 draft archive and check both Authenticode signatures and `--version`
outputs. In a disposable Windows machine, put an older standalone croc and the
new helper in an administrator-owned directory, then use an ordinary PowerShell:

1. Run `croc-update --check`; verify that it does not trigger UAC or change files.
2. Run `croc-update --yes` and decline UAC. Verify a failing exit status, an
   actionable message, and the original croc hash and version.
3. Repeat and approve UAC. Verify the new croc version, the unchanged helper,
   and success in the original terminal.
4. Remove the helper (or place an older helper beside current croc). Run
   `croc update --yes`, approve
   UAC, and verify that only the helper changes. Run again and verify no UAC.
5. Keep croc running while invoking the helper. Verify a close-and-retry error
   and the original binary; no process should be terminated automatically.

An older updater that cannot replace its root-owned executable needs one final
bootstrap with the curl installer. The new installer verifies both executables
before installing the helper first and then croc. Publish the paired release
before deploying the new embedded installer against it. The normal published
release event triggers the existing Docker/web build containing that installer.
