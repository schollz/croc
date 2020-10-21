# VERSION=8.X.Y make release

release:
	cd ../../ && go run src/install/updateversion.go
	git commit -am "bump ${VERSION}"
	git tag -af v${VERSION} -m "v${VERSION}"	
	cp zsh_autocomplete ../../
	cp bash_autocomplete ../../
	cd ../../ && goreleaser release
	git push
	cd ../../ && ./src/install/prepare-sources-tarball.sh
	cd ../../ && ./src/install/upload-src-tarball.sh

test:
	cd ../../ && go generate
	cd ../../ && goreleaser release --skip-publish
