VERSION=$(shell git describe --abbrev=0)
LDFLAGS=-ldflags "-X main.Version=${VERSION}"

.PHONY: linux
linux:
	GO111MODULE=off qtdeploy ${LDFLAGS} --tags='wincroc' --debug build desktop

.PHONY: fast
fast:
	GO111MODULE=off qtdeploy ${LDFLAGS} --fast --tags='wincroc' --debug build desktop

windows:
	GO111MODULE=off qtdeploy ${LDFLAGS} --tags='wincroc' --debug --docker build windows_64_static