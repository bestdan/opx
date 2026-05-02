BINARY  := opx
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# Strip debug info and symbol tables for a smaller binary (~2-5 MB target).
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build test clean cross lint

## build: compile opx for the current platform.
build:
	go build $(LDFLAGS) -o $(BINARY) .

## test: run all unit tests.
test:
	go test ./...

## lint: run go vet (no external linter required).
lint:
	go vet ./...

## clean: remove compiled binaries.
clean:
	rm -f $(BINARY) \
	      $(BINARY)-darwin-arm64 \
	      $(BINARY)-darwin-amd64 \
	      $(BINARY)-linux-amd64

## cross: cross-compile for macOS (arm64 + x86_64) and Linux (x86_64).
cross:
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64  go build $(LDFLAGS) -o $(BINARY)-darwin-arm64  .
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64  go build $(LDFLAGS) -o $(BINARY)-darwin-amd64  .
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64  go build $(LDFLAGS) -o $(BINARY)-linux-amd64   .

## help: list available targets.
help:
	@grep -E '^## ' Makefile | sed 's/## //'
