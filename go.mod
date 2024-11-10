module github.com/schollz/croc/v10

go 1.22

toolchain go1.23.1

require (
	github.com/cespare/xxhash v1.1.0
	github.com/chzyer/readline v1.5.1
	github.com/denisbrodbeck/machineid v1.0.1
	github.com/kalafut/imohash v1.1.0
	github.com/magisterquis/connectproxy v0.0.0-20200725203833-3582e84f0c9b
	github.com/minio/highwayhash v1.0.3
	github.com/sabhiram/go-gitignore v0.0.0-20210923224102-525f6e181f06
	github.com/schollz/cli/v2 v2.2.1
	github.com/schollz/logger v1.2.0
	github.com/schollz/pake/v3 v3.0.5
	github.com/schollz/peerdiscovery v1.7.5
	github.com/schollz/progressbar/v3 v3.17.1
	github.com/stretchr/testify v1.9.0
	golang.org/x/crypto v0.29.0
	golang.org/x/net v0.31.0
	golang.org/x/sys v0.27.0
	golang.org/x/term v0.26.0
	golang.org/x/time v0.8.0
	gortc.io/stun v1.23.0
)

require (
	github.com/cpuguy83/go-md2man/v2 v2.0.5 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/tscholl2/siec v0.0.0-20240310163802-c2c6f6198406 // indirect
	github.com/twmb/murmur3 v1.1.8 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace gortc.io/stun => github.com/gortc/stun v1.23.0
