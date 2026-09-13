# Fedora candidate

Continue [review 2432373](https://bugzilla.redhat.com/show_bug.cgi?id=2432373).
The installed binary package is already named `croc`; the review changes the
source name from the retired `golang-github-schollz-croc` repository.

The candidate preserves Fedora's Go/RPM hardening macros, requires Go 1.27,
installs the manual and three completions, and removes relay units and service
scriptlets. It retains the existing review's skipped public-IP test.

Recreate Source1 from the unmodified v11.5.2 tag with go-vendor-tools 0.13.0:

```sh
cd croc-11.5.2
sha256sum go.mod go.sum > ../module-locks.sha256
GOTOOLCHAIN=local go_vendor_archive create . --no-tidy \
  -c ../go-vendor-tools.toml -O ../croc-11.5.2-vendor.tar.bz2
sha256sum -c ../module-locks.sha256
tar -xf ../croc-11.5.2-vendor.tar.bz2
go_vendor_license -c ../go-vendor-tools.toml -C . report --no-prompt
```

The checked configuration records the hashes of five explicit license mappings.
The Plan 9-only module declares BSD-3-Clause in its source header. The YAML and
Staticcheck notices contain multiple license texts. The murmur3 file contains
two BSD notices. Askalono's three undetected files were cross-checked with
ScanCode and their contents; no missing-license warning remains in the report.
Askalono initially misidentified creachadair/msync as BSD-3-Clause-HP. Its license
does not contain the patent-infringement addition identified in the
[SPDX definition](https://spdx.org/licenses/BSD-3-Clause-HP.html), checked in
Chrome. An explicit BSD-3-Clause mapping corrects that detector result.
`license-report.json` preserves the detector output for review. Distro review
still needs to assess the expression, included material, and mappings.

Place Source0, Source1, the TOML configuration, and `croc.1` in the RPM sources
directory. Run `rpmbuild -ba croc.spec`, then mock and rpmlint for target Fedora
releases. See the dated status for checks completed locally. A container
rpmbuild is not a mock build, and neither constitutes acceptance of the review.
