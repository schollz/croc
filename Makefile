BINARY := croc
AIR_VERSION := v1.65.1

.PHONY: build serve

build:
	cd web && if [ ! -d node_modules ]; then npm install; fi && npm run embed
	go build -o $(BINARY) .

serve:
	go run github.com/air-verse/air@$(AIR_VERSION) -c .air.toml
