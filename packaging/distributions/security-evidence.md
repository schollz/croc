# Security acceptance evidence, 2026-09-13

This is an evidence map for [Gentoo 980854](https://bugs.gentoo.org/980854),
[Gentoo 918091](https://bugs.gentoo.org/918091), and the linked
[openSUSE review](https://bugzilla.opensuse.org/show_bug.cgi?id=1215507).
It does not request closure or assert that all reported concerns are resolved.
The Gentoo discussions were reread through their public issue API on this date.
The openSUSE discussion was reread in Chrome. It was closed in October 2024
because croc was absent from supported releases, not because the security team
accepted a complete fix. Comments 2 and 3 describe the broader acceptance concern
and suggest restricting writes to one directory with a packaging wrapper.

The inspected filesystem, protocol, codephrase, terminal, and redaction files
match the v11.5.2 tag. The table uses the first release containing the inspected
implementation where verified from Git ancestry; it does not infer that an
earlier advisory's claimed fix was complete. The 2026 Gentoo comments explicitly
dispute closing the dangerous-file acceptance concern.

| Reported concern | Current implementation and regression evidence | Release/status |
|---|---|---|
| 980854: attacker-controlled cleanup manifest deletes files | `src/utils/utils.go:RemoveMarkedFiles` consumes a process-local list of temporary files, never a received cleanup file. `TestRemoveMarkedFilesIgnoresTransferredCleanupFile`, `TestRemoveMarkedFilesRemovesOnlyLocalMarkedFiles`, and the marked/unmarked archive tests in `src/croc/croc_test.go` cover cleanup and full validation before extraction. | Original deletion fix commit `c0d51f0`, first contained in v11.0.3. Later archive/root hardening is present in v11.5.2. |
| CVE-2023-43616: ZIP traversal and overwrite | `src/utils/utils.go:UnzipDirectoryFromFileAtRootWithLimitContext` validates entries before extraction. `src/receivefs/path.go:Normalize` and `ValidateEntries` reject absolute/traversing paths and portable collisions. `src/receivefs/root.go` uses root-scoped operations. Tests cover ZIP escapes, symlink parents, case/Unicode collisions, and concurrent filesystem mutation. | The current root-scoped implementation entered in `c4e9f077`, first contained in v11.2.5. The sensitive-component addition `cb5273d0` is first contained in v11.3.6. These are narrower code claims than declaring the entire 2023 report settled. |
| CVE-2023-43617: exposing secret fragments to a relay | `src/codephrase/codephrase.go:Parse` separates a room selector from the PAKE passphrase and hashes the selector. `TestParseUsesStableRoomHash` and the parse format cases fix this behavior. `src/pakekey` binds protocol identities and transcripts; cross-wired-room and confirmation tests cover those boundaries. | These implementations and tests are present in v11.5.2; PAKE binding hardening entered with `c4e9f077` in v11.2.5. A hashed selector is not a claim that short, user-chosen codes cannot be guessed. |
| CVE-2023-43618: pre-authentication local IP disclosure | `TestLocalIPExchangeRequiresAuthenticatedEncryption` and `TestFailedPakeConfirmationDoesNotActivateSecureChannel` in `src/croc/croc_test.go` exercise the current authenticated-channel requirement. | Current regression implementation from `c4e9f077`, contained in v11.2.5 and later. |
| CVE-2023-43619: concealed dangerous files and incomplete acceptance UI | Root confinement, basename/path validation, sensitive-component rejection, and symlink defenses address the reported filesystem escape mechanisms. However, `src/croc/croc.go:processMessageFileInfo` still summarizes multiple files by count and size. It does not show a complete manifest before acceptance. | **Unresolved acceptance/design concern.** Blocking selected sensitive names is additional defense, not a replacement for containment or informed acceptance. No removal reversal or full vulnerability closure is justified solely by the version bump. |
| CVE-2023-43620: terminal escapes in names | `src/croc/terminal_display.go:quotedFilename` and `TestQuotedFilename` in `terminal_display_test.go` exercise printable, quoted file names. The hostile-peer tests reject unsafe metadata before it reaches a filesystem operation. | Present in v11.5.2. This review has not established the first fully covered release or audited every terminal output sink. |
| CVE-2023-43621: secrets in process arguments | Unix receive guidance uses an environment variable; shell-quoting tests are in `src/cli/unix_receive_message_test.go`. `src/redact` and `TestClientErrorsRedactSharedCodeAndDerivedSecrets` cover secrets in errors. | Present in v11.5.2. The explicit legacy/classic mode still permits command-line secrets. An environment variable is not protection from every same-user or privileged process. |

Validation includes the repository's native unit and race suites, Linux package
transfers on every release architecture, and the existing portability/security
regressions. Linux fuzz smoke tests exercise path normalization, manifest
collisions, and ZIP preflight. See the dated status for exact completed checks.
Passing these tests is evidence for their tested invariants, not an independent
security audit or a determination that a distro's acceptance criteria are met.

Before requesting reconsideration, present this map, the tested ebuild, and the
complete proposed reply for review. Ask maintainers which unresolved acceptance
requirements they would need satisfied. Do not claim that a general test pass,
an older advisory status, or a package refresh closes their concerns.
