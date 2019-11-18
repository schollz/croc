tmp=$(mktemp -d)
git clone --depth 1 https://github.com/schollz/croc $tmp/croc
(cd $tmp/croc && go mod tidy && go mod vendor)
(cd $tmp && tar -cvzf croc-src.tar.gz croc)
mv $tmp/croc-src.tar.gz dist/