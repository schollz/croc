# Copyright 2023-2026 Gentoo Authors
# Distributed under the terms of the GNU General Public License v2

EAPI=8
inherit go-module shell-completion

DESCRIPTION="Easily and securely send things from one computer to another"
HOMEPAGE="https://github.com/schollz/croc"
SRC_URI="https://github.com/schollz/croc/releases/download/v${PV}/${PN}_v${PV}_src.tar.gz -> ${P}.tar.gz"
S="${WORKDIR}/${PN}-v${PV}"
LICENSE="Apache-2.0 BSD BSD-2 CC-BY-4.0 ISC MIT ZLIB"
SLOT="0"
KEYWORDS="~amd64 ~arm64 ~riscv ~x86"
BDEPEND=">=dev-lang/go-1.27:="
RDEPEND="app-misc/ca-certificates"
DOCS=( LICENSE README.md THIRD_PARTY_NOTICES.md )
PATCHES=( "${FILESDIR}/completions.patch" "${FILESDIR}/package-update.patch" )

src_compile() {
	GOTOOLCHAIN=local ego build -mod=vendor -trimpath -buildvcs=false -tags=netgo,osusergo -o croc .
	GOOS= GOARCH= GOARM= CGO_ENABLED=0 GOFLAGS= GOTOOLCHAIN=local \
		go build -mod=vendor -buildvcs=false -o croc-completion . || die
	./croc-completion generate-fish-completion > croc.fish || die
}

src_test() {
	ego test -mod=vendor -skip 'Test(PublicIP|LocalIP|LocalLookupIP|LookupFunction)$' ./...
}

src_install() {
	dobin croc
	doman "${FILESDIR}/croc.1"
	newbashcomp src/install/bash_autocomplete croc
	cp src/install/zsh_autocomplete _croc || die
	dozshcomp _croc
	dofishcomp croc.fish
	newdoc src/codephrase/wordlists/LICENSE.txt wordlists-LICENSE.txt
	einstalldocs
}
