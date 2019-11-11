

send:
	go build -v; ./webrtc --sender

receive:
	go get -v github.com/schollz/browsersync
	echo "package main" > wasm/offer.go 
	echo "var offerstring =\`" >> wasm/offer.go
	cat offer.json | base64 >> wasm/offer.go
	echo "\`" >> wasm/offer.go 
	cd wasm && GOOS=js GOARCH=wasm go build -o main.wasm
	cd wasm && browsersync
