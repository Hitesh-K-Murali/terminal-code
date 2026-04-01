BINARY := tc
MODULE := github.com/Hitesh-K-Murali/terminal-code
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)"
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: build run test clean fmt lint deps install release build-release

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/tc/

run: build
	./$(BINARY)

test:
	go test -race -v ./...

clean:
	rm -f $(BINARY)
	rm -rf dist/
	go clean

fmt:
	gofmt -s -w .

lint:
	go vet ./...

deps:
	go mod tidy
	go mod verify

# Install to ~/.local/bin (no root needed)
install: build
	@mkdir -p $(HOME)/.local/bin
	@cp $(BINARY) $(HOME)/.local/bin/$(BINARY)
	@chmod +x $(HOME)/.local/bin/$(BINARY)
	@echo "Installed to $(HOME)/.local/bin/$(BINARY)"
	@echo "Make sure $(HOME)/.local/bin is in your PATH"

# Cross-compile for all platforms
release:
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		os_name=$$(echo $$platform | cut -d/ -f1); \
		arch=$$(echo $$platform | cut -d/ -f2); \
		output="dist/tc-$$os_name-$$arch"; \
		echo "Building $$output..."; \
		GOOS=$$os_name GOARCH=$$arch CGO_ENABLED=0 go build $(LDFLAGS) -trimpath \
			-o $$output ./cmd/tc/; \
	done
	@cd dist && sha256sum tc-* > checksums.txt 2>/dev/null || shasum -a 256 tc-* > checksums.txt
	@echo ""
	@echo "Release binaries:"
	@ls -lh dist/
	@echo ""
	@cat dist/checksums.txt

# Obfuscated release build (requires: go install mvdan.cc/garble@latest)
build-release:
	garble -literals -seed=random build \
		-ldflags="-s -w -X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME)" \
		-trimpath \
		-o $(BINARY) \
		./cmd/tc/
