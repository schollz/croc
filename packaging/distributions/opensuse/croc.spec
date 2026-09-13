#
# spec file for package croc
#
# Copyright (c) 2024 SUSE LLC
# Copyright (c) 2021 Orville Q. Song <orville@anislet.dev>
# Copyright (c) 2024 Andreas Stieger <Andreas.Stieger@gmx.de>
#
# All modifications and additions to the file contributed by third parties
# remain the property of their copyright owners, unless otherwise agreed
# upon. The license for this file, and modifications and additions to the
# file, is the same license as for the pristine package itself (unless the
# license for the pristine package is not an Open Source License, in which
# case the license is the MIT License). An "Open Source License" is a
# license that conforms to the Open Source Definition (Version 1.9)
# published by the Open Source Initiative.

# Please submit bugfixes or comments via https://bugs.opensuse.org/
#


Name:           croc
Version:        11.5.2
Release:        0
Summary:        Easily and securely send things from one computer to another
License:        Apache-2.0 AND BSD-2-Clause AND BSD-3-Clause AND CC-BY-4.0 AND ISC AND MIT AND Zlib
URL:            https://github.com/schollz/croc
Source0:        https://github.com/schollz/croc/releases/download/v%{version}/croc_v%{version}_src.tar.gz
Source1:        croc.1
Patch0:         completions.patch
Patch1:         package-update.patch
BuildRequires:  golang(API) >= 1.27
BuildRequires:  golang-packaging
BuildRequires:  fdupes
Requires:       ca-certificates
%{go_nostrip}

%description
croc could easily and securely send things from one computer to another.

%prep
%autosetup -p1 -n croc-v%{version}
export GOTOOLCHAIN=local

%build
export GOTOOLCHAIN=local
# Keep debug information compatible with the distro's debugedit tooling.
export GOEXPERIMENT=nodwarf5
go build -mod=vendor -buildvcs=false -buildmode=pie
./croc generate-fish-completion > croc.fish

%install
install -D -m 755 %{name} %{buildroot}%{_bindir}/%{name}
install -D -m 644 %{SOURCE1} %{buildroot}%{_mandir}/man1/croc.1
install -D -m 644 src/install/bash_autocomplete %{buildroot}%{_datadir}/bash-completion/completions/croc
cp src/install/zsh_autocomplete _croc
install -D -m 644 _croc %{buildroot}%{_datadir}/zsh/site-functions/_croc
install -D -m 644 croc.fish %{buildroot}%{_datadir}/fish/vendor_completions.d/croc.fish
install -D -m 644 LICENSE %{buildroot}%{_defaultlicensedir}/%{name}/LICENSE
install -D -m 644 src/codephrase/wordlists/LICENSE.txt %{buildroot}%{_defaultlicensedir}/%{name}/wordlists-LICENSE.txt
find vendor internal -type f \( -iname 'license*' -o -iname 'copying*' -o -iname 'notice*' -o -iname 'patents*' \) -print0 |
while IFS= read -r -d '' notice; do
    install -D -m 644 "$notice" "%{buildroot}%{_defaultlicensedir}/%{name}/$notice"
done
install -D -m 644 vendor/github.com/go4org/plan9netshell/netshell.go \
    %{buildroot}%{_defaultlicensedir}/%{name}/vendor/github.com/go4org/plan9netshell/netshell.go
%fdupes %{buildroot}%{_defaultlicensedir}/%{name}

%check
export GOTOOLCHAIN=local
# TestLocalIP and TestPublicIP require networking and will fail.
go test -skip "Test(Local|Public)IP" ./...

%files
%license %{_defaultlicensedir}/%{name}/
%doc README.md THIRD_PARTY_NOTICES.md
%{_bindir}/%{name}
%{_mandir}/man1/croc.1*
%{_datadir}/bash-completion/completions/croc
%{_datadir}/zsh/site-functions/_croc
%{_datadir}/fish/vendor_completions.d/croc.fish

%changelog
* Sun Sep 13 2026 Zack Scholl <zack.scholl+croc@gmail.com> - 11.5.2-0
- Update to 11.5.2 with Go 1.27, CLI documentation and shell completions.
- Add certificate dependency and license notices; remove relay service files.
