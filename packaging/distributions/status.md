# Linux packaging status, 2026-09-13

This records local validation of upstream release packaging and distro
candidates. Existing submissions below belong to their original authors.
Downstream acceptance remains separate from building a local candidate.

| Distribution | Rechecked starting point | Prepared work and completed checks | Remaining gate / existing submission |
|---|---|---|---|
| Debian | No official croc package found; pkg.haus supplies third-party 11.5.2 packages. Sid has Go 1.27.1-2. | dh-golang source skeleton; final `dpkg-source` build/extraction passed with both backports applied. Inventoried 53 linked modules and 632 module-graph entries. Sid import metadata matches 45 linked modules to candidate packages. Upstream DEB lifecycle passed on Debian 12 and 13 arm64. | Eight imports lack exact metadata matches; all candidate versions need API/version review. Build-Depends, unbundled build, sbuild/lintian/autopkgtest and Go-team sponsorship remain pending. [Team route](https://go-team.pages.debian.net/packaging.html). |
| Ubuntu | Official availability follows Debian inclusion and Ubuntu synchronization. | Upstream DEB install, upgrade fixture, remove, reinstall and purge passed on 22.04, 24.04 and 26.04 arm64. Explicit downloaded-package instructions prepared. | No independent archive submission prepared. Older ESM/Pro releases were not tested. Ubuntu 26.04 amd64 extraction is blocked by this host's emulation: an unrelated nested tar fixture fails with the same ENOSYS error. Native amd64 CI remains required for that case. |
| Arch | Extra `1:11.5.2-1`, confirmed in Chrome. No open merge request found in package repository API. | Final pkgrel 2 preserves epoch and PIE/external-linker flags; adds manual, completion fixes, CA dependency, update protection and notices. x86_64 makepkg build, tests, installed file verification and update guard passed. Namcap only questioned the certificate dependency, retained for HTTPS. | Maintainer review pending. [Package source](https://gitlab.archlinux.org/archlinux/packaging/packages/croc). |
| Nix | Master 11.1.0; update to 11.5.2 already proposed. | Both source and vendor hashes verified by the final aarch64-linux Nix build. Version hook, package-update guard and HTTPS update check passed. Added certificate closure, root notices and native Fish generation for cross builds; ARMv7 cross recipe evaluated. Final local-relay test passed after ensuring relay readiness and process cleanup; an earlier candidate also passed a forced rerun. Cross build was evaluated, not executed. Existing NixOS test retained and evaluated. | NixOS VM execution requires `kvm nixos-test`; this Docker VM exposes no `/dev/kvm`. Follow [PR 562700](https://github.com/NixOS/nixpkgs/pull/562700), not a duplicate update. |
| Alpine | Edge community 11.3.6; stable community arm64 indexes: 3.24 has 10.2.2-r11, 3.23 has 10.2.2-r10, 3.22 has 10.2.1-r10. | Final 11.5.2 candidate built with abuild on edge aarch64/musl, passed its Go tests with the existing update-notification patch's skips, and installed CLI/doc/Bash/Zsh/Fish APKs. Installed update protection and exact Bash/Zsh completion contents passed. `arch="all"` retained. | Other eight edge architectures need distro builders. Follow [MR 107977](https://gitlab.alpinelinux.org/alpine/aports/-/merge_requests/107977), which already proposes 11.5.2. |
| Void | Orphaned 10.4.14; current Go template 1.26.5. Existing 11.5.2 update found on final recheck. | 11.5.2 template, verified source hash, native completion generator, manual/notices, package-update backport and CA dependency prepared. Shell syntax and xtools 0.70 xlint passed. | Required Go 1.27 unavailable in the inspected compiler template. xbps-src glibc/musl builds remain pending. Follow [PR 62515](https://github.com/void-linux/void-packages/pull/62515), which already records the compiler blocker. |
| Fedora | Installed package 9.6.4-7.fc41 in Fedora 43/44; old rawhide source repository retired. Review 2432373 remains open, confirmed in Chrome. | Recreated vendor configuration/archive using go-vendor-tools 0.13.0 without changing tagged module locks. License scan and explicit mappings recorded. Final rawhide aarch64 rpmbuild, tests, installation, update protection and rpm file verification passed. rpmlint: zero errors, two warnings (local vendor source URL and identical notice text). Upstream RPM upgraded the actual old Fedora package on 43/44 arm64. | Mock on target Fedora releases and maintainer review remain pending. Continue [review 2432373](https://bugzilla.redhat.com/show_bug.cgi?id=2432373). |
| openSUSE | Development `network/croc` 10.1.1; community 11.5.2 observed. Software site confirms no official Leap 16 package. | 11.5.2 development spec prepared, preserving PIE flags and adding manual/completions/notices/CA dependency. Upstream RPM lifecycle passed on Leap 16 and Tumbleweed arm64. Security ticket 1215507 reread in Chrome. | Final Tumbleweed aarch64 rpmbuild, tests, installation, update guard and rpm verification passed. Rpmlint: zero errors and zero warnings. Dependency-license expression and notices are recorded. OBS, native Leap source builds and Factory security acceptance remain pending. Ticket closure followed package removal, not acceptance of a fix. [Development package](https://build.opensuse.org/package/show/network/croc). |
| Gentoo | 10.2.7, masked for removal on September 20. Issues 980854 and 918091 reread. No current update PR found. | 11.5.2 ebuild, Manifest, existing metadata and keyword coverage, manual/completions/notices and CA dependency prepared. Hashes and shell syntax checked. Code/test/release evidence mapped in `security-evidence.md`. | Native arm64 ebuild compile/test/install/package and pkgcheck passed, including installed root license and update protection. Go 1.27.1 was built from the official ebuild. Other keyworded architectures and distro review of the license mappings remain pending. Container namespace isolation was unavailable and reported EPERM; no host isolation settings were changed. The full file manifest before acceptance remains unresolved. Do not claim security closure or request removal reversal from test results alone. [980854](https://bugs.gentoo.org/980854), [918091](https://bugs.gentoo.org/918091). |

## Upstream validation record

Local host: macOS arm64 with Go 1.27.1 and Python 3.14.7. Linux checks used
disposable Docker containers. Six Linux CLI binaries were built from base commit
`6ff8cd6f` plus the local package-update guard. Tests use version 11.5.2 as a
fixture; these files are not published v11.5.2 release assets. Distro candidates use the existing published v11.5.2 tag/source archives with
the documented package-update and completion backports.

| Check | Result and limits | Local log/artifact |
|---|---|---|
| Six Linux static builds | Passed: amd64, 386, ARMv7, ARMv5, arm64, riscv64. CGO disabled with netgo/osusergo. | `tmp/linux-builds/` |
| Twelve DEB/RPM inspections | Passed: exact names, versions/revision, architectures, root ownership, modes, dependencies, notices, manual, shell syntax, file lists, absent scripts/services and static ELF. Each package binary matched its Linux archive. | `tmp/package-verification-final.log` |
| Ubuntu 24.04 RPM extractor compatibility | Reproduced the RPM 4.18.2 `rpm2cpio` archive-size failure from the first CI run. With digest verification and `rpm2archive`, all twelve existing package fixtures passed inspection/execution and all six architecture transfers plus the directory transfer passed. A corrupted RPM was rejected by the digest check before extraction. | `tmp/rpm-compat-verification.log`; [initial CI failure](https://github.com/schollz/croc/actions/runs/34777074801/job/103777037362) |
| Six-target execution | Version/help passed for both formats. Foreign targets used QEMU; ARMv5 explicitly used `qemu-arm -cpu arm926`. | Same log |
| Cross-package transfers | Six local-relay transfers around the architecture ring passed SHA-256 comparison. RPM-to-DEB directory transfer with nested and spaced names also passed. | Same log |
| Package lifecycle | Passed on all nine listed Debian/Ubuntu/Fedora/openSUSE arm64 combinations and eight amd64 combinations. Ubuntu 26.04 amd64 is blocked by emulated tar extraction; the same failure reproduces on a tiny unrelated archive. Stale standalone registration did not allow replacement; registration itself was rejected; user config survived removal. Debian used a lower-revision fixture; Fedora used the actual old repository package. | `tmp/lifecycle-*.log`, `tmp/ubuntu26-rosetta-tar.log` |
| Native unit suite | `go test ./...` passed on macOS. Fedora and Alpine ran their candidate's full suite with the documented distro skips. | `tmp/unit-tests.log`, distro build logs |
| Race/security/portability | Selected existing race suite covering CLI, transfer, receivefs, PAKE, codephrase, crypto, storage and networking passed on macOS and Linux musl. Existing control/data interruption-resume tests are included. | `tmp/race-tests.log`, `tmp/linux-race-tests.log` |
| Optional transport portability | No-tailcat build/tests, illumos transport compilation and tailcat benchmark-tag compilation passed. The first attempt used a read-only source mount, which blocked a test writing its README fixture; the writable source run passed. | `tmp/portability-tests-final.log` |
| Linux updater regression | Package paths, stale registration and standalone update tests passed. | `tmp/linux-update-tests.log` |
| Linux fuzz smoke tests | Normalize, ValidateEntries and ZIP preflight each passed 10 seconds. This is bounded fuzz coverage. | `tmp/fuzz-tests.log` |
| Offline source | Two archives of stamped base commit `6ff8cd6f` built with an empty module cache, GOTOOLCHAIN=local and proxy/sumdb disabled. Tagged go.mod/go.sum preserved; no Git directory. Archives were byte-identical. This validates the source preparer against committed source, not an uncommitted source release. | `tmp/source-check/normalized*.tar.gz` |
| Source reproducibility hash | Both normalized archives: `073b4ccc50ebe00ac6557a9aa52bc5347ae351ef33536d4d509a340599f46b56`. | Same archives |
| Exact 34-asset boundary | Passed names and checksums using new Linux archives/packages plus downloaded published non-Linux/web/source fixtures. No claim that all platforms were rebuilt or signed locally. | `tmp/release-dry-run/` |
| Release regression tests | Nine Python tests passed on macOS and Ubuntu 24.04, including missing/extra assets, corruption, unsafe binary entries, remote draft requirements, and package payload paths, links, duplicates, completeness, ownership and permissions. | `python3 -m unittest discover -s packaging -v` |
| Workflow/shell/manual checks | actionlint passed all three changed/new workflows; shell syntax and groff manual rendering passed; Bash registration and Zsh autoload names checked. | `tmp/completion-tests.log` |
| Upstream DEB lint | Lintian reports expected `statically-linked-binary` for the required static payload and `initial-upload-closes-no-bugs` because no ITP has been filed. These were not suppressed or claimed as a clean official Debian package. | `tmp/lintian.log` |

The reusable CI lifecycle matrix targets amd64. Eight matrix cases passed locally
under emulation; Ubuntu 26.04 needs native execution. The first remote package
workflow built all binaries and packages but failed in RPM extraction before
the lifecycle matrix. The compatibility fix is locally verified as recorded above;
remote lifecycle results remain pending. Containers share their host
kernel; these checks do not validate every supported historical kernel, every
distro architecture, native NixOS VMs, Windows signing, or downstream acceptance.

## Final distro build records

| Candidate | Final local evidence |
|---|---|
| Arch | `tmp/arch-final.log`, `tmp/arch-packages/` |
| Alpine | `tmp/alpine-recovered.log`, `tmp/alpine-installed-final.log`, `tmp/alpine-packages/` |
| Fedora | `tmp/fedora-recovered.log`, `tmp/fedora-installed-final.log`, `tmp/fedora-packages/`, `tmp/fedora-source-packages/` |
| openSUSE | `tmp/opensuse-final.log`, `tmp/opensuse-packages/`, `tmp/opensuse-source-packages/` |
| Gentoo | `tmp/gentoo-verified.log`, `tmp/gentoo-packages/` |
| Nix | `tmp/nix-final-verified.log`, `tmp/nix-relay-final.log`, `tmp/nix-installed-final.log`, `tmp/nix-cross-eval.log`, `tmp/nixos-final-eval.json` |
| Debian source | `tmp/debian-source-final.log`, `tmp/debian-source-result/` |
| Void lint | `tmp/void-xlint.log` (empty output, successful exit) |

Docker exhausted local storage during intermediate rebuilds. After recovery and
cleanup, the interrupted Fedora, Alpine, Gentoo and Nix builds were rerun to the
final results above. The failed attempts are not counted as successful checks.
Nix local builds used sandboxing disabled inside the disposable container; Go
telemetry cache residue under its temporary home was removed between builds.
Gentoo reported unavailable namespace isolation in this container. These local
checks do not substitute for each distro's isolated builders.

## Review at each release

Recheck current distro versions and open submissions before preparing updates.
Regenerate module inventories and candidate dependency mappings from the stamped
tag, rerun package/transfer/lifecycle checks, update this dated table with exact
results, and link accepted or superseded submissions. Record security findings
individually and retain unresolved acceptance requirements. Coordinate follow-up
work with maintainers of existing submissions.
