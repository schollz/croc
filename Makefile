BINARY := croc
WEB_BINARY := croc-web
AIR_VERSION := v1.65.1
BUILD_FLAGS := -buildvcs=false -trimpath -tags netgo,osusergo
LDFLAGS := -s -w -buildid=
STORE_DIR ?= ./tmp/store
STORE_DOWNLOADS ?= 100

.PHONY: build web-assets build-web serve

build:
	CGO_ENABLED=0 go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS)" -o $(BINARY) .

web-assets:
	cd web && if [ ! -d node_modules ]; then npm install; fi && npm run embed

build-web: web-assets
	CGO_ENABLED=0 go build $(BUILD_FLAGS) -ldflags="$(LDFLAGS)" -o $(WEB_BINARY) ./cmd/croc-web

serve:
	go run github.com/air-verse/air@$(AIR_VERSION) -c .air.toml -- --store-dir "$(STORE_DIR)" --store-downloads "$(STORE_DOWNLOADS)"
