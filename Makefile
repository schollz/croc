.PHONY: build

build:
	cd web && if [ ! -d node_modules ]; then npm install; fi && npm run embed
	go build
