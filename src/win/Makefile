VERSION=$(shell git describe --tags --abbrev=0)
LDFLAGS=-ldflags "-X main.Version=${VERSION}"

.PHONY: linux
linux:
	GO111MODULE=off qtdeploy ${LDFLAGS} --tags='wincroc' --debug build desktop

.PHONY: fast
fast:
	GO111MODULE=off qtdeploy ${LDFLAGS} --fast --tags='wincroc' --debug build desktop

windows:
	GO111MODULE=off qtdeploy ${LDFLAGS} --tags='wincroc' --debug --docker build windows_64_static

release: linux windows
	mv deploy/linux/win croc
	tar -czvf croc_${VERSION}_Linux-64bit_GUI.tar.gz croc
	mv deploy/windows/win.exe croc.exe
	zip croc_${VERSION}_Windows-64bit_GUI.zip croc.exe
	rm -rf dist
	mkdir dist
	mv *zip dist/
	