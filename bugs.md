# File-transfer security and reliability bugs

Research date: 2026-08-22

## Scope and confidence

This report covers end-user peer-to-peer, synchronization, or ad-hoc file-transfer tools that are useful comparisons for croc: croc, Warpinator, Magic Wormhole, LocalSend, NitroShare, OnionShare, KDE Connect, PairDrop/Snapdrop, LANDrop, Syncthing, and rclone. OpenSSH scp/sftp and rsync are included as mature protocol/filesystem comparators because their advisories expose many of the same bug classes. A historical FileZilla distribution-channel report is included only in the distro-forum section.

The additional distro-forum, mailing-list, and tracker research in this revision deliberately targets tools other than croc. The croc section is retained only as the existing comparison baseline.

This is not a claim that every tool is currently vulnerable. Findings are labeled as:

- **Confirmed CVE**: a published CVE or upstream security advisory.
- **Confirmed upstream bug**: an upstream issue or release note, without a CVE.
- **Open report**: a user report that has not necessarily been reproduced or accepted by a maintainer.
- **Community/deployment warning**: a distro-forum observation about packaging, firewall scope, sandbox permissions, or unsafe operating practice; not a product vulnerability by itself.
- **Design risk**: behavior explicitly documented by the project, not necessarily an implementation bug.
- **Packaging risk**: a distribution version, dependency, or maintenance problem.

The CVE inventory contains 89 identifiers. Versions and status come from the linked upstream advisories, release notes, NVD, or distribution trackers. “No public CVE found” means only that this research pass did not find one; it is not evidence that a tool is secure.

## Executive summary

The main problems repeat across otherwise different transfer architectures:

1. **Receiver filesystem confinement fails.** Sender-controlled names escape the destination through `..`, absolute paths, archives, symlinks, alternate path syntaxes, temporary paths, or unsafe final renames. This affected croc, Warpinator, Magic Wormhole, LocalSend, NitroShare, OnionShare, and rsync.
2. **Path validation is not race-safe.** A lexical “is this under the destination?” check can become false before the write, rename, metadata update, or cleanup. Symlink and parent-directory races recur heavily in Warpinator, OnionShare, and rsync.
3. **Discovery is mistaken for identity.** UDP multicast, IP grouping, a default group code, or a trusted signaling server can let a nearby or infrastructure attacker impersonate peers. LocalSend, KDE Connect, Warpinator, PairDrop, and rsync all have examples.
4. **Untrusted metadata reaches an interpreter.** Filenames have reached terminals, HTML, command lines, and line-oriented helper protocols. The result ranges from misleading prompts to XSS and command injection.
5. **Resource admission is too weak.** Anonymous or malicious peers can consume upload slots, rooms, CPU, memory, disk, compression workers, and connection state. OnionShare, KDE Connect, Magic Wormhole, Warpinator, and rsync provide concrete examples.
6. **Convenience features silently widen impact.** Auto-save, automatic extraction, resumable transfer cleanup, “files disabled” UI controls, and delete/sync behavior have caused arbitrary writes or data loss.
7. **Secrets and metadata leak outside the encrypted payload.** Codes in process arguments or logs, relay-derived room names, local IP exchange, signaling metadata, timestamps, and sizes remain sensitive even when payload encryption is sound.
8. **Release and packaging lag matters.** A fixed upstream release does not protect users when stable distro packages or app stores remain on an affected version, and closed or stale source makes verification harder.
9. **Control planes become code-execution surfaces.** Rclone exposed unauthenticated remote-control operations that could mutate authorization, instantiate command-running backends, or serve local data. Syncthing filename/device-name XSS could alter trusted configuration from the WebUI.
10. **Legacy protocol semantics resist safe validation.** SCP historically mixed peer-chosen filenames, terminal output, wildcard expansion, and remote shell interpretation. Compatibility pressure left one command-injection CVE classified as unimportant and unfixed by Debian even after safer SFTP behavior became available.

## Cross-tool bug map

| Tool | Filesystem/path | Identity/trust | Untrusted display | Resource/availability | Current maintenance signal |
|---|---|---|---|---|---|
| croc | ZIP overwrite and dangerous destination files, fixed in 9.6.16 | Relay secret and local-IP disclosures, fixed in 9.6.16 | Terminal escapes, fixed in 9.6.16 | Relay admission remains a class worth regression-testing | Active |
| Warpinator | Arbitrary write, symlink escape, cleanup deletion | Default group-code trust concern | Approval UI could show a safe path while another path was written | Large-file/connectivity reports | Active; one open data-loss report |
| Magic Wormhole | Two 2026 traversal regressions | 16-bit default code; rendezvous/relay and IP metadata risks | Terminal control-character issue fixed | Large-directory ZIP crash report | Active upstream; Gentoo removed its package over dependency maintenance |
| LocalSend | LAN arbitrary write fixed in 1.17.0 | UDP discovery impersonation advisory | Stored filename XSS advisory | Server restart and compatibility fixes in 1.18.2 | Active rewrite; advisory status needs version verification |
| NitroShare | Unauthenticated LAN arbitrary write through 0.3.4 | LAN discovery and optional TLS in legacy design | Not established | Legacy branch is not being developed | Legacy C++ line paused; rewrite planned |
| OnionShare | Symlink disclosure fixed in 2.6.4 | Onion URL/private key is the access capability | Misleading disabled-upload accounting | Upload-slot DoS and disk/compression concerns | Active |
| KDE Connect | Not the main published class | UDP identity spoofing fixed in 2025 releases | Spoofed device information | Crafted-packet DoS fixed in 20.08.2 | Active |
| PairDrop | Browser download handling limits direct filesystem exposure | Upstream says signaling server must be trusted | Browser/UI metadata remains an input boundary | Reconnect/discovery failures | Active; zero-trust signaling is planned, not complete |
| LANDrop | Latest binary behavior cannot be audited from public source | LAN peer identity not established by a public advisory | Long names can block acceptance UI | Cross-platform receive/discovery failures | Latest releases are not represented by public source |
| Syncthing | Symlink traversal caused arbitrary overwrite | Malformed malicious-relay messages crashed clients and relays | Stored filename/device-name XSS could change configuration | Relay crash/restart loop | Active; older Debian branches retain the XSS issue |
| rclone | Local, archive, symlink, and multi-tenant path escapes | Unauthenticated remote-control and serving endpoints | FTP/SFTP filenames reached command interpreters | Proxy header memory exhaustion and archive-parser loops | Active; latest upstream only, with substantial distro lag |
| OpenSSH scp/sftp | Malicious servers chose unexpected paths and modes | SSH authenticates the server, not the safety of its returned metadata | ANSI terminal manipulation and remote-shell command injection | Less prominent than path/interpreter failures | Active; modern scp defaults to SFTP, but legacy mode remains |
| rsync | Extensive traversal, symlink, metadata, temp, delete, and chroot escapes | ACL, hostname, TLS, proxy-source, and auth parsing failures | Command and helper-protocol injection | CPU, memory, worker, timeout, and daemon-slot exhaustion | Active; 3.5.0 is a major security release |

## Tool-by-tool findings

### croc

All six published croc CVEs below affect versions before 9.6.16 and list 9.6.16 as the patched release. This repository currently identifies itself as 11.2.5, so these are historical regression targets rather than evidence that the current tree is vulnerable.

| CVE | Severity | Main failure | Status |
|---|---:|---|---|
| [CVE-2023-43616](https://github.com/advisories/GHSA-8c8w-f7wp-2jr2) | Moderate | Sender could overwrite files during ZIP extraction | Fixed in 9.6.16 |
| [CVE-2023-43617](https://github.com/advisories/GHSA-hp56-xvf4-g6wr) | Moderate | Parts of a custom shared secret could be disclosed to an untrusted relay through room-name construction | Fixed in 9.6.16 |
| [CVE-2023-43618](https://github.com/advisories/GHSA-7mp6-929p-pqhj) | Moderate | Local IP addresses were sent in cleartext in an `ips?` message | Fixed in 9.6.16 |
| [CVE-2023-43619](https://github.com/advisories/GHSA-ppjh-xp5v-46wc) | High | Sender could create dangerous destination files such as `.ssh/authorized_keys` | Fixed in 9.6.16 |
| [CVE-2023-43620](https://github.com/advisories/GHSA-364c-vvqx-446c) | High | Sender-controlled filenames could inject ANSI/CSI terminal escapes | Fixed in 9.6.16 |
| [CVE-2023-43621](https://github.com/advisories/GHSA-7g3v-4ggr-xvjf) | Moderate | A shared code placed in process arguments could be read by other local users | Fixed in 9.6.16 |

The durable lesson for croc is that protocol authentication and encryption do not make authenticated sender metadata safe. The receiver must still treat filenames, archive markers, sizes, offsets, directory structure, status messages, and relay inputs as malicious.

### Warpinator

- **Confirmed CVE — arbitrary write/overwrite:** [CVE-2022-42725](https://nvd.nist.gov/vuln/detail/CVE-2022-42725) affects Warpinator through 1.2.14. The SUSE review found that sender-controlled paths could escape the configured receive directory and overwrite files such as shell startup files. The detailed [oss-security disclosure](https://www.openwall.com/lists/oss-security/2022/10/24/1) also explains why symlinks and parallel processing make a one-time lexical check insufficient. SUSE tracked the review as [bug 1203037](https://bugzilla.suse.com/show_bug.cgi?id=1203037).
- **Confirmed CVE — cleanup deletion:** [CVE-2023-29380](https://nvd.nist.gov/vuln/detail/CVE-2023-29380) affects 1.0.7 through versions before 1.6.0. Malicious `top_dir_basenames` values could escape the transfer root and cause cleanup to delete files or directories outside it. The [upstream fix](https://github.com/linuxmint/warpinator/commit/9aae768522b7bbb09c836419893802a02221d663) landed for 1.6.0, which also added Bubblewrap/Landlock isolation as defense in depth. Debian records the issue as not packaged there but preserves the affected version and deletion description in its [tracker](https://security-tracker.debian.org/tracker/CVE-2023-29380).
- **Design risk — default group code:** The same SUSE audit found that the group code was the principal authenticity anchor and warned that users retaining the default “Warpinator” code could trust arbitrary LAN peers or permit interception. This is a trust-model weakness distinct from the path CVE.
- **Open report — unexpected receiver deletion:** [Issue #240](https://github.com/linuxmint/warpinator/issues/240) reports that an interrupted/retried mobile-to-desktop transfer can delete destination files that no longer exist on the sender. The issue was open at research time and has not been treated here as a confirmed vulnerability, but its alleged impact is data loss and its cleanup semantics deserve attention.
- **Operational reports:** The current issue list includes connection/firewall failures, poor throughput, and large-file transfer failures. These are reliability signals, not proof of security flaws.

### Magic Wormhole

- **Confirmed CVE — regression in basename handling:** [CVE-2026-32116](https://github.com/advisories/GHSA-4g4c-mfqg-pj8r) affects 0.21.0 through 0.22.x. A malicious sender could overwrite files such as `.ssh/authorized_keys` or `.bashrc`; 0.23.0 restored confinement.
- **Confirmed CVE — output-directory variant:** [CVE-2026-42448](https://github.com/advisories/GHSA-cf92-gfcw-6v53) affects 0.23.0 when `--output` names an existing directory. It is fixed in 0.24.0. The two adjacent regressions are documented together in upstream [NEWS](https://github.com/magic-wormhole/magic-wormhole/blob/master/NEWS.md).
- **Confirmed upstream bug — terminal control characters:** [Issue #476](https://github.com/magic-wormhole/magic-wormhole/issues/476) demonstrated that a malicious filename could rewrite the receiver’s terminal display and misrepresent the proposed name or size. Upstream NEWS records the display sanitization in 0.13.0.
- **Open reliability report — large directory ZIP crash:** [Issue #435](https://github.com/magic-wormhole/magic-wormhole/issues/435) reports a crash while preparing a 51 GB directory because the ZIP was closed with an open write handle. It was reported on 0.11.2 and remains open; current-version reproduction was not established in this review.
- **Documented design risks:** The project’s [protocol security policy](https://github.com/magic-wormhole/magic-wormhole-protocols/security/policy) says the default two-word code has 16 bits of entropy. An active attacker gets a one-in-65,536 chance to guess it and perform a machine-in-the-middle attack; failed guesses disrupt the session and are detectable. It also documents server-visible timing/size metadata and direct-transfer disclosure of public and local IP addresses to the peer.
- **Gentoo packaging risk:** Gentoo [stabilized 0.23.0](https://github.com/gentoo/gentoo/commit/be7d640fb843583a6fd0ae836fd95ea63bc0ca3d) in April 2026, then [added 0.24.0](https://github.com/gentoo/gentoo/commit/1a5ef6f3aa5865749b98497b5efe141aaeaa1042) in May. On 2026-08-21 it [removed the package](https://github.com/gentoo/gentoo/commit/d3447025b46e41d8853c16c807fe8808826a89fc) after last-rite bug 967367 because the stale/broken `pytrie`, Autobahn, and txaio dependency chain was being removed. This is now an availability/maintenance problem for Gentoo users rather than merely a keyword lag.

### LocalSend

- **Confirmed CVE — arbitrary path write:** [CVE-2025-27142 / GHSA-f7jp-p6j4-3522](https://github.com/localsend/localsend/security/advisories/GHSA-f7jp-p6j4-3522) affects versions through 1.16.1 and is fixed in 1.17.0. Missing path sanitization in prepare/upload endpoints let a nearby peer write outside the destination; Windows Startup or shell files could turn this into code execution. “Quick Save” removed the normal confirmation step.
- **Confirmed CVE — discovery impersonation:** [CVE-2025-54792 / GHSA-424h-5f6m-x63f](https://github.com/localsend/localsend/security/advisories/GHSA-424h-5f6m-x63f) describes spoofing unauthenticated UDP discovery to impersonate a device and intercept or modify transfers. The advisory marks versions through 1.17.0 affected and lists no patched version.
- **Confirmed CVE — filename XSS:** [CVE-2026-25154 / GHSA-34v6-52hh-x4r4](https://github.com/localsend/localsend/security/advisories/GHSA-34v6-52hh-x4r4) describes stored XSS in the “Share via Link” web page because a filename was concatenated into HTML. The advisory marks versions through 1.17.0 affected and lists no patched version.
- **Current release caution:** [LocalSend 1.18.2](https://github.com/localsend/localsend/releases/tag/v1.18.2) was released on 2026-08-21 after a Rust networking/file-I/O rewrite. Its notes include a security hardening change to stop following peer-supplied HTTP redirects and fixes for 1.17 compatibility, favorites, server restart, and mixed file/folder drag-and-drop. Because the two advisories above do not name a patched release, users should verify the 1.18 implementation or maintainer statement instead of treating a higher version number alone as proof of remediation.

### NitroShare

- **Confirmed CVE — unauthenticated LAN arbitrary write:** [CVE-2026-66050](https://nvd.nist.gov/vuln/detail/CVE-2026-66050) affects NitroShare Desktop through 0.3.4. A crafted filename in the JSON item header can traverse outside the receive root. On Windows, writing into the Startup directory can obtain persistence and code execution at the next login.
- **Maintenance risk:** The [upstream repository](https://github.com/nitroshare/nitroshare-desktop) says development of the legacy C++ codebase was paused and will not continue; a protocol rewrite is planned in `nitroshare2`. No fixed legacy release was identified. Treat 0.3.4 as unsafe on an untrusted LAN unless a distributor has clearly backported a fix.

### OnionShare

- **Confirmed CVE — upload-slot denial of service:** [CVE-2022-21689](https://github.com/advisories/GHSA-jh82-c5jw-pxpc) affects versions before 2.5. An attacker who can reach Receive mode can occupy its per-second upload allowance and block legitimate uploads. Public mode makes source-based blocking difficult because of Tor anonymity. Fixed in 2.5.
- **Confirmed CVE — symlink disclosure:** [CVE-2026-54706 / GHSA-22p9-r2f5-22mf](https://github.com/onionshare/onionshare/security/advisories/GHSA-22p9-r2f5-22mf) affects versions before 2.6.4. Share or Website mode followed symlinks in a selected directory and could disclose readable files outside it. The advisory explicitly recommends containment checks both while indexing and immediately before opening/zipping to prevent TOCTOU swaps. Fixed in 2.6.4.
- **Confirmed CVE — disabled-upload bypass:** [CVE-2026-54707 / GHSA-v833-3823-cmhp](https://github.com/onionshare/onionshare/security/advisories/GHSA-v833-3823-cmhp) affects versions before 2.6.4. The UI and route accounting honored “Disable uploading files,” but the multipart stream factory had already written the file. Fixed in 2.6.4.
- **Confirmed upstream bug — non-writable receive directory:** [Issue #2062](https://github.com/onionshare/onionshare/issues/2062) shows Receive mode can start successfully with a directory that is not writable, then fail only when data arrives. This is especially confusing in Tails, where AppArmor restricts accessible locations.
- **Open resource report:** [Issue #1328](https://github.com/onionshare/onionshare/issues/1328) reports that large shares could consume temporary-disk space through compression even when the UI suggested individual-file serving. It was reported on 2.3.1; current applicability was not established.
- **Release status:** [OnionShare 2.6.4](https://github.com/onionshare/onionshare/releases/tag/v2.6.4) lists both the symlink and disabled-upload fixes.

### KDE Connect

- **Confirmed CVE — crafted-packet resource exhaustion:** [CVE-2020-26164](https://nvd.nist.gov/vuln/detail/CVE-2020-26164) affects desktop releases before 20.08.2. A LAN attacker could consume CPU, memory, or connection slots. SUSE tracked it as [bug 1176268](https://bugzilla.suse.com/show_bug.cgi?id=1176268), and Gentoo issued [GLSA 202101-16](https://security.gentoo.org/glsa/202101-16).
- **Confirmed CVE — discovery information spoofing:** [CVE-2025-32900](https://nvd.nist.gov/vuln/detail/CVE-2025-32900) covers crafted broadcast-UDP packets that temporarily change displayed device information. Affected versions include Android before 1.33.0, KDE Connect desktop before 25.04, iOS before 0.5, Valent before 1.0.0.alpha.47, and GSConnect before 59.

These CVEs reinforce that discovery packets are untrusted hints. Pairing/cryptographic identity must be independent of names, IP addresses, and discovery announcements, and unauthenticated discovery handlers need strict resource limits.

### PairDrop and Snapdrop

No tool-specific public CVE or upstream security advisory was found in this pass. Two upstream facts still matter:

- PairDrop’s [FAQ](https://github.com/schlagmichdoch/PairDrop/blob/master/docs/faq.md) says files use encrypted WebRTC but the PairDrop server must still be trusted. [Issue #180](https://github.com/schlagmichdoch/PairDrop/issues/180), opened by the maintainer, proposes encrypted signaling and persistent device verification to remove the signaling-server MITM risk. This is a documented design gap, not a published CVE.
- [Issue #498](https://github.com/schlagmichdoch/PairDrop/issues/498) identifies dead reconnect logic that can leave the iPadOS PWA permanently offline after an app switch. [Issue #497](https://github.com/schlagmichdoch/PairDrop/issues/497) reports public-room discovery failure across a reverse proxy. These are availability/reliability reports.

Browser delivery also creates a distinct trust boundary: a hosted instance controls the JavaScript that implements encryption and verification. TLS and WebRTC protect transport but do not make a compromised signaling/web origin trustworthy.

### LANDrop

No tool-specific public CVE or upstream security advisory was found in this pass.

- **Auditability/maintenance risk:** The [official repository](https://github.com/LANDrop/LANDrop) says it does not reflect current releases because the latest application source was temporarily closed. Current binaries therefore cannot be reproduced or audited from the public repository.
- **Open reliability reports:** The [issue list](https://github.com/LANDrop/LANDrop/issues) includes current discovery/receive failures across Windows, iOS, Android, and Linux. [Issue #239](https://github.com/LANDrop/LANDrop/issues/239) reports that a sufficiently long filename can hide the Accept button, turning untrusted metadata into a denial of user control. These reports are not confirmed security vulnerabilities.

### Syncthing

Debian's [Syncthing source-package tracker](https://security-tracker.debian.org/tracker/source-package/syncthing) records three application-specific CVEs. They cover three different trust boundaries:

- **Confirmed CVE — symlink traversal/arbitrary overwrite:** [CVE-2017-1000420](https://security-tracker.debian.org/tracker/CVE-2017-1000420) affects 0.14.33 and older. A malicious shared folder could use symlinks to overwrite arbitrary receiver files. The fix landed in 0.14.34; Debian first packaged it in 0.14.36.
- **Confirmed CVE — malicious-relay crash:** [CVE-2021-21404](https://security-tracker.debian.org/tracker/CVE-2021-21404) affects versions before 1.15.0. A negative relay-message length could crash `strelaysrv`, or crash a Syncthing client that joined a malicious relay. SUSE rated it important and published updates for Syncthing and the relay server in [openSUSE-2021-713](https://packagehub.suse.com/update-infos/openSUSE-2021-713/).
- **Confirmed CVE — stored WebUI XSS:** [CVE-2022-46165](https://security-tracker.debian.org/tracker/CVE-2022-46165) affects versions before 1.23.5. A compromised device could synchronize a filename containing HTML/JavaScript, or propose a malicious device name; viewing the affected UI could execute script capable of changing folders or adding devices. Debian still lists Bullseye and Bookworm as vulnerable/no-DSA or ignored, while Trixie and newer are fixed. SUSE issued a moderate [package update](https://packagehub.suse.com/update-infos/openSUSE-2023-126/).

The combined lesson is that a cryptographically identified synchronization peer is still an untrusted source of paths, symlink topology, relay frames, display strings, and configuration-triggering WebUI content.

### rclone

Rclone is not a rendezvous-code tool, but it is a close comparator for copying attacker-controlled remote objects into local filesystems and for exposing transfer APIs. The current [Debian rclone tracker](https://security-tracker.debian.org/tracker/source-package/rclone) lists 12 open CVEs across stable and unstable branches plus one older resolved CVE. Upstream supports security fixes primarily in the latest release, so distro backports materially change exposure.

**Destination confinement, archives, symlinks, and tenant isolation**

- [CVE-2024-52522](https://security-tracker.debian.org/tracker/CVE-2024-52522): `--links --metadata` followed symlinks while applying ownership and permissions, enabling a privileged copy to change a symlink target. Fixed upstream in 1.68.2; Debian Bookworm and Trixie remain marked vulnerable/no-DSA.
- [CVE-2026-54572](https://security-tracker.debian.org/tracker/CVE-2026-54572): attacker-controlled `.rclonelink` objects could plant an absolute or `../` symlink and route a following write outside the destination. Fixed in 1.74.4.
- [CVE-2026-59732](https://security-tracker.debian.org/tracker/CVE-2026-59732): `rclone archive extract` accepted `../` entries and could write sibling objects outside the selected prefix. Fixed in 1.74.4.
- [CVE-2026-59733](https://security-tracker.debian.org/tracker/CVE-2026-59733): `serve restic --private-repos` authorized a cleaned route but used an uncleaned backend path, allowing one authenticated tenant to read, overwrite, or delete another tenant's repository. Fixed in 1.74.4.
- [CVE-2026-71309](https://security-tracker.debian.org/tracker/CVE-2026-71309): the broader `serve restic` endpoint accepted leading `../` paths for GET, HEAD, POST, and DELETE, escaping an operator-published backend subdirectory. Fixed in 1.75.0.
- [CVE-2026-71313](https://security-tracker.debian.org/tracker/CVE-2026-71313): non-default local filename encodings could decode full-width dots or preserved Windows backslashes into native traversal after validation. Fixed in 1.75.0. Ubuntu still listed all maintained releases as “needs evaluation” at research time in its [CVE record](https://ubuntu.com/security/CVE-2026-71313).

**Unauthenticated control-plane access and command execution**

- [CVE-2026-41176](https://security-tracker.debian.org/tracker/CVE-2026-41176): unauthenticated `options/set` could set `rc.NoAuth=true` and disable authorization for sensitive RC methods on a reachable RC listener lacking global HTTP authentication. Fixed upstream in 1.73.5.
- [CVE-2026-41179](https://security-tracker.debian.org/tracker/CVE-2026-41179): unauthenticated `operations/fsinfo` accepted an inline WebDAV backend whose `bearer_token_command` executed locally. Fixed in 1.73.5.
- [CVE-2026-49980](https://security-tracker.debian.org/tracker/CVE-2026-49980): `rcd --rc-serve` accepted unauthenticated GET/HEAD inline remotes, leading to local file read in older versions and command execution from 1.55.0 onward. Fixed in 1.74.3. Ubuntu backported this as an “unauthenticated remote command execution” update in its [Jammy security upload](https://lists.ubuntu.com/archives/jammy-changes/2026-July/047887.html).

**Interpreter injection, resource exhaustion, and weak secret generation**

- [CVE-2026-71311](https://security-tracker.debian.org/tracker/CVE-2026-71311): a filename encoding that preserved CR/LF allowed an object name to inject authenticated FTP control commands. Fixed in 1.75.0.
- [CVE-2026-71312](https://security-tracker.debian.org/tracker/CVE-2026-71312): Unicode quote characters in an SFTP path could terminate a PowerShell hash-command literal and execute statements as the SSH account. Fixed in 1.75.0.
- [CVE-2026-71310](https://security-tracker.debian.org/tracker/CVE-2026-71310): unbounded HTTP CONNECT response headers let a malicious configured proxy exhaust process memory before SFTP host-key authentication. Fixed in 1.75.0.
- [CVE-2020-28924](https://security-tracker.debian.org/tracker/CVE-2020-28924): rclone used `math/rand` for generated encryption passwords, reducing the possible values to a practical dictionary derived from process start time. Fixed in 1.53.3, but all passwords generated by affected versions need replacement.

[Ubuntu USN-8299-1](https://ubuntu.com/security/notices/USN-8299-1) backported the two April 2026 RC fixes across supported Ubuntu releases. Debian, however, still showed every packaged branch below upstream 1.75.0 and vulnerable to the July 2026 group at research time. This is a concrete example of why a distro version number, upstream fixed version, and actual backport status must be checked together.

### OpenSSH scp and sftp

OpenSSH authenticates the transport and server, but a compromised or malicious authenticated server can still attack the client through returned file metadata. Debian's [2019 OpenSSH advisory](https://lists.debian.org/debian-security-announce/2019/msg00026.html) grouped three scp-client failures:

- [CVE-2018-20685](https://security-tracker.debian.org/tracker/CVE-2018-20685): empty or dot directory names could alter permissions on the target directory.
- [CVE-2019-6109](https://security-tracker.debian.org/tracker/CVE-2019-6109): server-supplied object names could inject terminal control sequences and hide extra transferred files.
- [CVE-2019-6111](https://security-tracker.debian.org/tracker/CVE-2019-6111): the legacy scp protocol let a malicious server choose unexpected filenames and overwrite arbitrary files in the destination; recursive transfers widened the subdirectory impact.

[CVE-2020-15778](https://security-tracker.debian.org/tracker/CVE-2020-15778) covers command injection through anomalous destination arguments in legacy scp. Debian still marks it vulnerable/unfixed and “unimportant,” noting that validation could break long-standing workflows. An [Arch Linux forum mitigation thread](https://bbs.archlinux.org/viewtopic.php?pid=1928929) quotes OpenSSH's explanation that adding security to the rcp-derived argument model is difficult and recommends another transfer mechanism when the threat matters.

Newer releases did not eliminate the underlying metadata lesson:

- [CVE-2026-35385](https://security-tracker.debian.org/tracker/CVE-2026-35385): root downloading in legacy `scp -O` mode without `-p` could unexpectedly preserve setuid/setgid bits. Fixed in 10.3 and backported by Debian.
- [CVE-2026-59995 and CVE-2026-59996](https://ubuntu.com/security/notices/USN-8533-1): before 10.4, a malicious SFTP server could steer `sftp host:/path .` to an unintended local location, and remote-to-remote scp could write into the intended directory's parent. Ubuntu backported both issues.
- [CVE-2026-59997](https://ubuntu.com/security/notices/USN-8533-1): `internal-sftp` recognized only the first nine command-line arguments, silently discarding later security-sensitive restrictions. Fixed/backported with the same update.

OpenSSH's [10.4 release notes](https://www.openssh.org/releasenotes.html#10.4p1) describe the path and option fixes directly. The durable rule is that server authentication is not authorization for the server to choose local filenames, modes, parent directories, terminal output, or command syntax.

## Rsync as a filesystem-safety comparator

Rsync is broader and older than the peer-transfer tools above, but its advisories demonstrate why path safety must cover every operation, not just the initial file create.

### 2024 coordinated disclosure

[CERT VU#952657](https://www.kb.cert.org/vuls/id/952657) and [Gentoo GLSA 202501-01](https://security.gentoo.org/glsa/202501-01) cover six issues in versions through 3.3.0: heap overflow/RCE ([CVE-2024-12084](https://github.com/advisories/GHSA-85h7-m8c3-v9wc)), checksum information leak ([CVE-2024-12085](https://github.com/advisories/GHSA-xh5q-pch5-g3xq)), arbitrary file enumeration ([CVE-2024-12086](https://github.com/advisories/GHSA-82c6-8mfc-c23h)), path traversal ([CVE-2024-12087](https://github.com/advisories/GHSA-9x68-7qq6-v523)), `--safe-links` bypass ([CVE-2024-12088](https://github.com/advisories/GHSA-ffph-g3pc-8r3g)), and a symlink race ([CVE-2024-12747](https://github.com/advisories/GHSA-gp7r-m4cc-qhwq)).

### 3.4.3 security fixes

Gentoo’s [GLSA 202608-03](https://security.gentoo.org/glsa/202608-03) records seven issues fixed by 3.4.3: path-based TOCTOU ([CVE-2026-29518](https://github.com/advisories/GHSA-pfv9-gp3h-73xv)), xattr sorting memory corruption ([CVE-2026-41035](https://github.com/advisories/GHSA-m34r-4v3r-pp9v)), DNS/ACL fail-open ([CVE-2026-43617](https://github.com/RsyncProject/rsync/security/advisories/GHSA-rjfm-3w2m-jf4f)), compressed-token integer overflow and memory disclosure ([CVE-2026-43618](https://github.com/RsyncProject/rsync/security/advisories/GHSA-g37v-g3gj-pmwq)), symlink races ([CVE-2026-43619](https://github.com/RsyncProject/rsync/security/advisories/GHSA-4h9m-w5ff-j735)), receiver out-of-bounds read/DoS ([CVE-2026-43620](https://github.com/RsyncProject/rsync/security/advisories/GHSA-28pw-r563-rxvm)), and an HTTP CONNECT proxy parser off-by-one ([CVE-2026-45232](https://github.com/RsyncProject/rsync/security/advisories/GHSA-8f85-j2cv-59m8)).

### 3.5.0 major security release

Upstream calls [rsync 3.5.0](https://github.com/RsyncProject/rsync/releases/tag/v3.5.0) a major security release. Its 33 CVEs are grouped below by the lesson most relevant to file-transfer implementations. Unless a row says otherwise, the upstream advisory lists 3.5.0 as the patched release.

**Filesystem confinement, symlinks, metadata, and destructive operations**

- [CVE-2026-70460](https://github.com/RsyncProject/rsync/security/advisories/GHSA-w3xf-j2r2-gv4x): `--partial-dir`/`--backup-dir` module-root escape through an in-module symlink.
- [CVE-2026-53801](https://github.com/RsyncProject/rsync/security/advisories/GHSA-mch3-qr4p-chgm): source directory enumeration escapes the transfer root.
- [CVE-2026-53800](https://github.com/RsyncProject/rsync/security/advisories/GHSA-v3vw-pvpg-chwh): `--remove-source-files` deletion follows a parent-symlink race.
- [CVE-2026-53799](https://github.com/RsyncProject/rsync/security/advisories/GHSA-phxh-hjqv-39c9): receiver ACL/xattr application follows a symlink race.
- [CVE-2026-53797](https://github.com/RsyncProject/rsync/security/advisories/GHSA-3jj3-qvc7-jp6x): sender source-tree symlink race discloses out-of-tree files.
- [CVE-2026-53796](https://github.com/RsyncProject/rsync/security/advisories/GHSA-w75h-ccff-w53m): receiver destination-`chdir` TOCTOU race.
- [CVE-2026-53795](https://github.com/RsyncProject/rsync/security/advisories/GHSA-m9vj-637x-v6pq): absolute temp/link-destination options disable write confinement.
- [CVE-2026-53793](https://github.com/RsyncProject/rsync/security/advisories/GHSA-wj7w-vh23-mm44): chroot inner-module escape through a parent symlink.
- [CVE-2026-53789](https://github.com/RsyncProject/rsync/security/advisories/GHSA-fxwg-7hmf-xh5q): malicious sender expands `--delete` scope.
- [CVE-2026-53786](https://github.com/RsyncProject/rsync/security/advisories/GHSA-mrc3-6cwx-hch6): filter merge file bypasses module filters.
- [CVE-2026-53785](https://github.com/RsyncProject/rsync/security/advisories/GHSA-pph3-7xmf-rrqg): implied-parent creation escapes the destination tree.
- [CVE-2026-53784](https://github.com/RsyncProject/rsync/security/advisories/GHSA-ffg2-fr5g-3rxw): daemon module-root `chdir` escape without chroot.
- [CVE-2026-53783](https://github.com/RsyncProject/rsync/security/advisories/GHSA-9cgc-64g4-3gv5): restricted-directory validation-versus-execution race.
- [CVE-2026-53803](https://github.com/RsyncProject/rsync/security/advisories/GHSA-g9f4-7q66-9582): arbitrary write through symlinked operator-supplied output paths.
- [CVE-2026-53802](https://github.com/RsyncProject/rsync/security/advisories/GHSA-4mfr-8jrv-49x4): arbitrary read through symlinked operator-supplied input paths.

**Authentication, discovery, and access-control parsing**

- [CVE-2026-70452](https://github.com/RsyncProject/rsync/security/advisories/GHSA-6692-28cx-wpqq): `hosts deny` fails open on hostname-resolution failure.
- [CVE-2026-70454](https://github.com/RsyncProject/rsync/security/advisories/GHSA-3c3x-ww2w-5r5p): `rsync-ssl` makes an unauthenticated TLS connection.
- [CVE-2026-70463](https://github.com/RsyncProject/rsync/security/advisories/GHSA-pfj8-79vq-xgvr): `auth users` parsing silently skips deny/read-only rules.
- [CVE-2026-53791](https://github.com/RsyncProject/rsync/security/advisories/GHSA-h2q9-5fr8-w635): PROXY mode lets a direct client spoof its source address.
- [CVE-2026-53798](https://github.com/RsyncProject/rsync/security/advisories/GHSA-hx7p-3gvv-pqgv): empty name-converter response maps an unknown identity to UID/GID 0.
- [CVE-2026-53788](https://github.com/RsyncProject/rsync/security/advisories/GHSA-p4c5-8c68-5fjq): newline-bearing names enter a line-oriented name-converter protocol.

**Resource exhaustion and availability**

- [CVE-2026-70453](https://github.com/RsyncProject/rsync/security/advisories/GHSA-8x5r-mjx8-83hv): quadratic checksum-chain CPU exhaustion.
- [CVE-2026-70455](https://github.com/RsyncProject/rsync/security/advisories/GHSA-rjvj-qgqg-cvx9): peer-controlled Zstandard worker exhaustion.
- [CVE-2026-70459](https://github.com/RsyncProject/rsync/security/advisories/GHSA-p4v4-qxw9-q72m): crafted file list crashes a daemon child.
- [CVE-2026-70462](https://github.com/RsyncProject/rsync/security/advisories/GHSA-j9wh-5jmp-2m64): peer-supplied timeout defeats the client’s I/O timeout.
- [CVE-2026-70464](https://github.com/RsyncProject/rsync/security/advisories/GHSA-hrwq-ccf7-rw5m): unauthenticated handshake locks out a daemon module.
- [CVE-2026-53794](https://github.com/RsyncProject/rsync/security/advisories/GHSA-p827-vwcp-m964): remote peer disables the per-allocation cap.
- [CVE-2026-53792](https://github.com/RsyncProject/rsync/security/advisories/GHSA-cg57-rp9g-56hw): zero checksum block length drives matching negative.

**Memory safety and injection**

- [CVE-2026-70456](https://github.com/RsyncProject/rsync/security/advisories/GHSA-78jc-79jv-v6rw): remote out-of-bounds heap write in argument parsing.
- [CVE-2026-70457](https://github.com/RsyncProject/rsync/security/advisories/GHSA-pg7g-xqmr-xpfh): attacker-chosen-offset write in size-error formatting.
- [CVE-2026-70458](https://github.com/RsyncProject/rsync/security/advisories/GHSA-gg3m-4m9m-268h): out-of-bounds write from an unexpected hard-link flag.
- [CVE-2026-70461](https://github.com/RsyncProject/rsync/security/advisories/GHSA-jhxm-j4mq-3fj4): peer-driven one-byte heap out-of-bounds write.
- [CVE-2026-53790](https://github.com/RsyncProject/rsync/security/advisories/GHSA-5hcf-7xxm-rmqq): command/argument injection through unquoted peer- or host-controlled values.

Gentoo’s [rsync package page](https://packages.gentoo.org/packages/net-misc/rsync) showed 3.4.4 stable and 3.5.0 testing-keyworded at research time, with two security bugs linked. That is a reason to check distribution patches and security bug status before deployment; the version table alone cannot prove whether fixes were backported.

## Distro forums, mailing lists, and tracker observations

Distro-hosted discussion is valuable evidence of how software is actually deployed, but a forum post is not equivalent to a reproduced vulnerability. The entries below preserve that distinction.

### Confirmed security records distributed through community channels

- **openSUSE announced Syncthing relay hardening and XSS fixes in its forum.** The [2021 forum announcement](https://forums.opensuse.org/t/opensuse-su-2021-moderate-security-update-for-syncthing/145634) includes the malicious-relay crash fix and warns that the then-new “untrusted encrypted devices” feature was not ready for important data. SUSE later published the WebUI XSS fix through its Package Hub. These are confirmed distro security updates, not forum speculation.
- **openSUSE's rclone forum announcement included credential-recovery action, not just an upgrade.** [openSUSE-SU-2021:0272-1](https://forums.opensuse.org/t/opensuse-su-2021-moderate-security-update-for-rclone/144380) fixes CVE-2020-28924 and points users to a checker for weak passwords generated by affected releases. Replacing derived secrets is necessary because updating cannot restore entropy to old passwords.
- **Debian's 2019 scp mailing-list advisory confirms three malicious-server/client-boundary bugs.** [DSA-4387-1](https://lists.debian.org/debian-security-announce/2019/msg00026.html) covers permission manipulation, terminal escape injection, and arbitrary destination overwrite. The advisory also warns that strict filename validation can regress legitimate wildcard transfers and documents an opt-out flag.
- **Ubuntu's 2026 rclone and OpenSSH uploads show active backporting to old version lines.** [USN-8299-1](https://ubuntu.com/security/notices/USN-8299-1) fixes rclone RC authorization/RCE without rebasing every release to current upstream, while [USN-8533-1](https://ubuntu.com/security/notices/USN-8533-1) backports SFTP/scp path fixes. A low-looking distro version may therefore be patched; it must be checked against the distro tracker rather than compared numerically with upstream.

### Community/deployment warnings, not confirmed product CVEs

- **Warpinator users may be tempted to disable the host firewall to restore discovery.** An [openSUSE forum user](https://forums.opensuse.org/t/warpinator-flatpak-yast-firewall-exceptions/146205) reported that configured TCP/UDP exceptions did not work and asked whether running with the firewall disabled was safe. This does not establish a Warpinator vulnerability, but it shows how discovery failure can erase a security boundary in real deployments.
- **LocalSend forum threads show both overbroad and narrowly scoped firewall workarounds.** In one [openSUSE thread](https://forums.opensuse.org/t/localsend-configuration-with-yast-firewall/181775), receiving worked only after the user allowed all services; the resolution was to configure the specific LocalSend service/port instead. A later [openSUSE guide](https://forums.opensuse.org/t/anyone-else-using-and-liking-localsend/194080) opens TCP and UDP 53317. Fedora users separately noted that the Workstation zone permits every port above 1024, making LocalSend work automatically, while a stricter public zone blocks it in the [firewall discussion](https://discussion.fedoraproject.org/t/question-about-firewall/166827). These are firewall-scope observations, not flaws in LocalSend itself.
- **Syncthing packaging can require nearly unrestricted host access.** A [Fedora discussion about verified Flatpaks](https://discussion.fedoraproject.org/t/how-secure-are-verified-flatpaks/120135) notes that a third-party Syncthing GUI Flatpak had `filesystem=host`; verification identified the GUI publisher, not the Syncthing project, and the permission made the sandbox ineffective against a malicious package. This is a packaging/provenance warning rather than a Syncthing CVE.
- **A Fedora Syncthing support thread exposes a risky configuration pattern.** The user deliberately bound the WebUI to `0.0.0.0:8384` with TLS disabled and then opened the firewall in [this discussion](https://discussion.fedoraproject.org/t/cant-connect-to-syncthings-webui-if-run-on-fedora-server-40/131488). The thread is not a bug report, but it demonstrates why transfer-tool administrative interfaces should default to loopback, authentication, and TLS when remotely exposed.
- **Arch's scp thread records a compatibility-versus-safety impasse.** In the [CVE-2020-15778 mitigation discussion](https://bbs.archlinux.org/viewtopic.php?pid=1928929), participants quote the upstream position that shell-like SCP argument handling is difficult to validate without breaking established workflows. That is a protocol-design warning and supports preferring SFTP mode, not evidence that SSH transport encryption failed.
- **FreeBSD users documented why version-only scanners can be wrong.** A [FreeBSD forum thread](https://forums.freebsd.org/threads/updating-openssh-for-pci-compliance.76366/) asks why the base-system OpenSSH version appears vulnerable to CVE-2019-6111 even though FreeBSD backported the patch. Security inventory must inspect vendor patch level, not only the upstream version string.
- **FreeBSD's W^X hardening exposed a runtime compatibility problem in rclone.** A [forum report](https://forums.freebsd.org/threads/rclone-not-working-with-w-x.80279/) found the packaged Go binary would not run with writable-xor-executable enforcement enabled. This is not proof of exploitable rclone code, but operational workarounds that disable platform hardening increase the impact of a future memory-corruption bug.
- **A historical openSUSE FileZilla thread reports bundled adware/PUPs in a Windows download channel.** The [2014 discussion](https://forums.opensuse.org/t/filezilla-on-sourceforge-includes-ad-malware-win32-64/102933) concerns the SourceForge installer wrapper, not a FileZilla protocol vulnerability or current package. It belongs in the report because transfer-tool security includes provenance and installer integrity, but it must not be read as a statement about present FileZilla downloads.
- **FreeBSD's rsync backup discussion treats the source workstation as compromised.** The [forum thread](https://forums.freebsd.org/threads/options-for-securely-pulling-data-from-lan.69466/#post-418633) recommends pull control, restricted accounts/commands, jails, read-only snapshots, and never interpreting received content. This is threat-model guidance rather than an rsync bug, and it directly addresses ransomware propagating through synchronization semantics.

### Other distro conclusions

- **SUSE's Warpinator review found a broader trust problem.** The default group code and lack of an independent authenticated identity meant path bugs were reachable by more LAN peers than users might expect. The audit also warned that symlinks and parallel transfer undermine naive path checks.
- **Gentoo removed Magic Wormhole on 2026-08-21.** The last-rite commit cites bug 967367 and follows removal of its stale/broken Python dependency chain. Users depending on the distro package now need a supported alternative installation path.
- **Gentoo's rsync advisories and package metadata do not yet tell one simple story.** GLSA 202608-03 requires 3.4.3 for one security wave, while upstream's later 3.5.0 release fixes 33 additional CVEs. The package page offers 3.5.0 only under testing keywords. Administrators should inspect the two open security bugs and ebuild patches.
- **Gentoo and SUSE both tracked KDE Connect's resource-exhaustion issue.** This gives independent distribution confirmation that unauthenticated LAN discovery/control traffic must be bounded before expensive work or persistent state allocation.
- **Upstream issue trackers expose destructive and availability behavior that CVE feeds omit.** Warpinator's interrupted-transfer deletion report, Magic Wormhole's large-directory ZIP crash, OnionShare's non-writable-directory and temp-space reports, and PairDrop/LANDrop discovery failures are operationally important even when they are not security advisories.

## What croc should retain as security invariants

These are regression targets, not claims that the current croc tree violates them:

1. Treat every sender filename, directory, archive entry, temporary name, offset, size, and mode bit as hostile.
2. Reject traversal, absolute paths, NUL/control characters, Windows drive/UNC/device/ADS forms, reserved names, Unicode/case-fold collisions, and ambiguous separators before user display and before filesystem use.
3. Do not let a sender-controlled metadata bit decide that received bytes should be extracted or interpreted as an archive.
4. Resolve and mutate through held directory handles where the platform permits; use no-follow, exclusive create, same-directory temporary files, atomic finalization, and revalidation at the operation boundary.
5. Apply the same containment rules to normal files, directories, ZIP entries, partial files, stored-transfer state, resume cleanup, metadata application, and final rename/remove operations.
6. Keep incomplete data separate from final names; never delete or overwrite unrelated receiver data as implicit retry cleanup.
7. Escape untrusted text for each output context: terminal, HTML, JSON, logs, shell/command execution, URLs, and accessibility labels.
8. Keep shared codes out of process arguments, logs, errors, relay room names, analytics, and crash reports. Encrypt local-address exchange and minimize metadata retained by relays.
9. Authenticate peers independently of discovery names, IP addresses, multicast packets, proxy headers, or a signaling server. Keep PAKE confirmation explicit.
10. Bound work before allocation: per source, code/room, connection, filename length, file count, total bytes, compression ratio, relay rooms, and time window. Failed/abandoned sessions must release state predictably.
11. Test malicious peers and relays, disconnect/resume paths, concurrent symlink swaps, cross-platform path grammars, archive bombs, long metadata, case-insensitive filesystems, and cleanup after every failure point.
12. Preserve regression tests for croc’s published ZIP traversal, terminal escape, encrypted local-IP exchange, PAKE confirmation, shared-code exposure, relay secret disclosure, and marked-file cleanup fixes.

## Bottom line

The dominant cross-tool bug is not broken encryption. It is giving authenticated but untrusted peer metadata too much authority over local paths, UI, cleanup, or resource allocation. The strongest common defense is a narrow receive capability: one authenticated transfer, one confined destination handle, bounded resources, inert metadata, atomic writes, and no implicit extraction, execution, deletion, or trust based solely on discovery.
