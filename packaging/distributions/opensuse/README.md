# openSUSE development candidate

This updates the existing `network/croc` recipe. The separate `.changes` file is
for OBS. The inline spec changelog lets a direct local rpmbuild be linted; use
the OBS-generated changelog when preparing the downstream source submission.

The source is the published vendored v11.5.2 release archive. Its go.mod and go.sum
match the Git tag. No dependency upgrade or feature removal is performed.
The license expression follows the scan recorded in `../fedora/license-report.json`
for the same module versions. All found vendor/internal notices and the Plan 9
module's source license declaration are installed for review. The word-list
license is included explicitly. Downstream licensing review remains required.

Build with Go 1.27, golang-packaging and the current distro RPM tools. The recipe
retains PIE, emits DWARF 4 for debugedit compatibility, and drops the obsolete
external Go provides generator, which is unnecessary for a CLI package. It keeps
the previous recipe's local/public-IP test exclusions. No relay unit or service
scriptlet is installed.

Run `rpmbuild -ba croc.spec` and rpmlint locally, then an OBS build before a
development-project submission. See `../status.md` for current checks. Leap's
upstream static RPM installation test does not prove that Leap can build this
source with its repository compiler.

The historical security ticket was closed after package removal. A successful
package build does not justify Factory inclusion or resolve the incomplete
file listing before acceptance. Review `../security-evidence.md` first.
