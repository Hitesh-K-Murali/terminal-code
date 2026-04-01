BINARY := tc
MODULE := github.com/Hitesh-K-Murali/terminal-code
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"

.PHONY: build run test clean fmt lint

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/tc/

run: build
	./$(BINARY)

test:
	go test -race -v ./...

clean:
	rm -f $(BINARY)
	go clean

fmt:
	gofmt -s -w .

lint:
	go vet ./...

deps:
	go mod tidy
	go mod verify

build-release:
	@echo "Release build (requires garble: go install mvdan.cc/garble@latest)"
	garble -literals -seed=random build \
		-ldflags="-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)" \
		-trimpath \
		-o $(BINARY) \
		./cmd/tc/
