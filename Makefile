.PHONY: build clean luajit test

BINARY_NAME=golapis

all: build

luajit:
	@echo "Building LuaJIT..."
	$(MAKE) -C luajit

build: luajit
	@echo "Building golapis..."
	CGO_ENABLED=1 go build -o $(BINARY_NAME) -ldflags="-s -w" .

clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	$(MAKE) -C luajit clean

test: build
	@echo "Testing with simple Lua script..."
	@echo 'print("Hello from Lua!")' > test.lua
	@./$(BINARY_NAME) test.lua
	@rm -f test.lua