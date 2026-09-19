.PHONY: test test-race lint build

test:
	go test ./...

test-race:
	go test -race ./...

lint:
	gofmt -l .
	go vet ./...

build:
	mkdir -p bin
	go build -o bin/pairlinkd ./cmd/pairlinkd
	go build -o bin/echo ./examples/echo

check: lint test-race
