# VERSION=8.X.Y make release

release:
	cd ../../ && go generate
	cd ../../ && goreleaser release
	./prepare-sources-tarball.sh
	./upload-src-tarball.sh
