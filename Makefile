.PHONY: build build-debug clean luajit test

BINARY_NAME=golapis

all: build

luajit:
	@echo "Building LuaJIT..."
	$(MAKE) -C luajit

build: luajit
	@echo "Building golapis..."
	@mkdir -p bin
	CGO_ENABLED=1 go build -o bin/$(BINARY_NAME) -ldflags="-s -w" .

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
