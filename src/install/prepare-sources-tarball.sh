#!/bin/bash
tmp=$(mktemp -d)
echo $VERSION
git clone -b v${VERSION} --depth 1 https://github.com/schollz/croc $tmp/croc-${VERSION}
(cd $tmp/croc-${VERSION} && go mod tidy && go mod vendor)
(cd $tmp && tar -cvzf croc_${VERSION}_src.tar.gz croc-${VERSION})
mv $tmp/croc_${VERSION}_src.tar.gz dist/
