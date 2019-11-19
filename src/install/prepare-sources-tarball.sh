#!/bin/bash
tmp=$(mktemp -d)
VERSION=$(cat ./src/cli/cli.go | grep 'Version = "v' | sed 's/[^0-9.]*\([0-9.]*\).*/\1/')
echo $VERSION
git clone --depth 1 https://github.com/schollz/croc $tmp/croc
(cd $tmp/croc && go mod tidy && go mod vendor)
(cd $tmp && tar -cvzf croc_${VERSION}_src.tar.gz croc)
mv $tmp/croc_${VERSION}_src.tar.gz dist/
