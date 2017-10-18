test:
	@go test -race .
	@go test -race ./util/strutil

examples:
	go run -race example/full/full.go
	go run -race example/incr/incr.go
	go run -race example/multi/multi.go
	go run -race example/simple/simple.go

.PHONY: test examples
