.PHONY: build build-debug clean luajit test install

BINARY_NAME=golapis

VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

LDFLAGS := -s -w \
	-X main.version=$(VERSION) \
	-X main.gitCommit=$(GIT_COMMIT) \
	-X main.buildDate=$(BUILD_DATE)

all: build

luajit:
	@echo "Building LuaJIT..."
	$(MAKE) -C luajit $(if $(BUILDMODE),BUILDMODE=$(BUILDMODE))

build: luajit
	@echo "Building golapis..."
	@mkdir -p bin
	CGO_ENABLED=1 go build -o bin/$(BINARY_NAME) -ldflags="$(LDFLAGS)" .

build-debug: luajit
	@echo "Building golapis (debug)..."
	@mkdir -p bin
	CGO_ENABLED=1 go build -tags debug -o bin/$(BINARY_NAME)-debug .

clean:
	@echo "Cleaning..."
	rm -f bin/$(BINARY_NAME)
	$(MAKE) -C luajit clean

test: luajit
	@echo "Running tests..."
	CGO_ENABLED=1 go test ./golapis -v

install: luajit
	@echo "Installing $(BINARY_NAME)..."
	CGO_ENABLED=1 go install -ldflags="$(LDFLAGS)" .
