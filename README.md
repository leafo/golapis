# golapis

A Go library for embedding LuaJIT with async-capable bindings. The eventual
goal of this project is to provide a self contained LuaJIT runtime that is
capable of running Lapis apps with no dependency on Nginx. In order to
accomplish this, this library will implement the `ngx` interface that
[lua-nginx-module](https://github.com/openresty/lua-nginx-module) provides, but
backed by Go concurrency and networking primitives.

This project provides two paths: a `golapis` binary that can either execute
code on the command line or start a webserver with an entry file, and a Go
library that can be embedded into an existing Go project to initiate a Golapis
server within `net/http`.

## Command Line Interface

```
golapis [options] [script [args]]

Options:
  -e stat  execute string 'stat'
  -l name  require library 'name'
  -v       show version information
  --       stop handling options
  -        execute stdin and stop handling options
  --http   start HTTP server mode
  --port   port for HTTP server (default 8080)
  --ngx    alias golapis table to global ngx
  --file-server PATH[:URL] serve static files (can be repeated)
```

### Running Scripts

```bash
# Run a Lua file
golapis script.lua

# Run inline code
golapis -e 'print("hello")'

# Run from stdin
echo 'print("hello")' | golapis -

# Pass arguments to script (accessible via `arg` table)
golapis script.lua arg1 arg2
```

### HTTP Server Mode

Start an HTTP server that executes a Lua script for each request:

```bash
# Start server on default port 8080
golapis --http app.lua

# Specify a different port
golapis --http --port 3000 app.lua

# Use inline code
golapis --http -e 'golapis.say("Hello, " .. golapis.var.remote_addr)'
```

In HTTP mode, the script has access to request-specific APIs like `golapis.var`,
`golapis.req`, `golapis.header`, and `golapis.status`.

### nginx Compatibility (--ngx)

For compatibility with existing lua-nginx-module code, the `--ngx` flag aliases
the `golapis` table to the global `ngx`:

```bash
golapis --http --ngx app.lua
```

This allows existing code using `ngx.say()`, `ngx.var`, etc. to work without
modification:

```lua
-- With --ngx flag, both work:
ngx.say("Hello")      -- nginx-style
golapis.say("Hello")  -- golapis-style
```

### Static File Server (--file-server)

Serve static files alongside your Lua application:

```bash
# Serve ./static directory at /static/ URL prefix
golapis --http --file-server static app.lua

# Explicit mapping: local path to URL prefix
golapis --http --file-server ./assets:/static/ app.lua

# Multiple directories
golapis --http --file-server static --file-server uploads app.lua
```

The shorthand `--file-server static` is equivalent to `--file-server static:static`,
serving the `static` directory at `/static/`. For paths like `./static` or
`/var/www/files`, the URL prefix is derived from the directory name (`static`
and `files` respectively).

Static file routes take precedence over the Lua handler, so requests to
`/static/style.css` will serve the file directly without invoking your Lua script.

## Go Interface

### Creating a State

Create a new Lua state with `NewGolapisLuaState()`:

```go
import "golapis/golapis"

lua := golapis.NewGolapisLuaState()
if lua == nil {
    // handle error
}
defer lua.Close()
```

### Running Code

The state uses an event loop for execution. Start it before running code:

```go
lua.Start()
defer lua.Stop()
```

#### Execute a Lua file

```go
err := lua.RunFile("script.lua", nil)
if err != nil {
    fmt.Printf("Error: %v\n", err)
}
```

#### Execute a Lua string

```go
err := lua.RunString(`golapis.say("Hello from Lua!")`)
if err != nil {
    fmt.Printf("Error: %v\n", err)
}
```

#### Wait for threads

`Wait()` blocks until all active threads have completed execution.

### Precompiled Entry Points

`RunString` and `RunFile` parse and compile the Lua code on each call. For code
that needs to run multiple times (e.g., HTTP request handlers), use
`LoadEntryPoint` to compile & load once and execute many times:

```go
lua := golapis.NewGolapisLuaState()
defer lua.Close()

// Create an entry point from a file or code string
entry := golapis.FileEntryPoint{Filename: "handler.lua"}
// or: entry := golapis.CodeEntryPoint{Code: `local name = ...; golapis.say("hello ", name)`}

// Compile once at startup
if err := lua.LoadEntryPoint(entry); err != nil {
    log.Fatal(err)
}

lua.Start()
defer lua.Stop()

// Execute the precompiled code, with support for arguments to be passed to the chunk
if err := lua.RunEntryPoint("world"); err != nil {
    log.Printf("Error: %v", err)
}
```

The `EntryPoint` interface has two implementations:
- `FileEntryPoint{Filename: "path/to/file.lua"}` - loads from a file
- `CodeEntryPoint{Code: "lua code here"}` - loads from a string

This is how `StartHTTPServer` works internally - it precompiles the entry point
once at startup, then executes it for each incoming request without reparsing.

### Output Handling

By default, output from `golapis.say()` and `golapis.print()` goes to stdout. You can redirect it:

```go
lua.SetOutputWriter(myWriter)
```

### Complete Example

```go
package main

import (
    "fmt"
    "os"
    "golapis/golapis"
)

func main() {
    lua := golapis.NewGolapisLuaState()
    if lua == nil {
        fmt.Println("Failed to create Lua state")
        os.Exit(1)
    }
    defer lua.Close()

    lua.Start()
    defer lua.Stop()

    if err := lua.RunFile("script.lua", nil); err != nil {
        fmt.Printf("Error: %v\n", err)
        os.Exit(1)
    }
}
```

## Lua API

The `golapis` global table provides an ngx-compatible API. Functions use the same signatures as their `ngx.*` equivalents:

| Function | Description |
|----------|-------------|
| `golapis.say(...)` | Output with newline |
| `golapis.print(...)` | Output without newline |
| `golapis.null` | Null sentinel value |
| `golapis.sleep(seconds)` | Async sleep, yields coroutine |
| `golapis.now()` | Returns current Unix timestamp with microsecond precision |
| `golapis.update_time()` | No-op for ngx API compatibility |
| `golapis.req.start_time()` | Returns timestamp when request was created |
| `golapis.escape_uri(str[, type])` | Escape URI string (type 0 or 2) |
| `golapis.unescape_uri(str)` | Unescape URI string |
| `golapis.encode_base64(str[, no_padding])` | Encode string to base64 |
| `golapis.decode_base64(str)` | Decode base64 string (strict) |
| `golapis.decode_base64mime(str)` | Decode base64 MIME (ignores whitespace) |
| `golapis.md5(str)` | Returns hex MD5 digest |
| `golapis.md5_bin(str)` | Returns binary MD5 digest |
| `golapis.sha1_bin(str)` | Returns binary SHA-1 digest |
| `golapis.hmac_sha1(key, str)` | Returns binary HMAC-SHA1 digest |
| `golapis.req.get_uri_args([max])` | Parse query string parameters |
| `golapis.req.read_body()` | Read and cache request body |
| `golapis.req.get_body_data([max_bytes])` | Get raw request body as string |
| `golapis.req.get_post_args([max])` | Parse POST body as form-urlencoded |
| `golapis.req.get_headers([max], [raw])` | Get request headers as table |
| `golapis.timer.at(delay, cb, ...)` | Schedule callback after delay |
| `golapis.socket.udp()` | Create UDP cosocket (see below) |
| `golapis.var.*` | Request variables (read-only, HTTP mode only) |
| `golapis.header.*` | Response headers (write before first output) |
| `golapis.status` | HTTP response status code (read/write, set before first output) |
| `golapis.ctx` | Per-request Lua table for storing data |

### golapis.var Variables

| Variable | Description |
|----------|-------------|
| `request_method` | HTTP method (GET, POST, etc.) |
| `request_uri` | Full request URI including query string |
| `request_body` | Request body (nil if `read_body()` not called) |
| `scheme` | "http" or "https" |
| `host` | Hostname without port |
| `server_port` | Server port number |
| `remote_addr` | Client IP address |
| `args` | Query string (nil if empty) |
| `http_*` | Any HTTP header (e.g., `http_user_agent`, `http_host`) |

### Example Lua Script

```lua
golapis.say("Hello from Lua!")
golapis.say("Version: ", golapis.version)

golapis.sleep(1)
golapis.say("Slept for 1 second")

local body, status, headers = golapis.http.request("https://example.com")
golapis.say("Status: ", status)
```

### golapis.socket.udp

UDP cosocket API compatible with `ngx.socket.udp`.

| Method | Description |
|--------|-------------|
| `golapis.socket.udp()` | Create a new UDP socket object |
| `sock:setpeername(host, port)` | Connect to UDP server |
| `sock:setpeername("unix:/path")` | Connect to Unix datagram socket (Linux only) |
| `sock:send(data)` | Send string, number, boolean, nil, or array table |
| `sock:receive([size])` | Receive up to `size` bytes (default/max 65536) |
| `sock:settimeout(ms)` | Set timeout in milliseconds |
| `sock:bind(addr)` | Bind to local address before connecting |
| `sock:close()` | Close the socket |

## Extensions

Additional golapis functions not part of the ngx API:

| Function | Description |
|----------|-------------|
| `golapis.http.request(url)` | Simple async HTTP GET, returns `body, status, headers` |
| `golapis.version` | Library version string |

### golapis.debug

Debugging utilities for timer inspection and control:

| Function | Description |
|----------|-------------|
| `golapis.debug.pending_timer_count()` | Returns the number of pending timers |
| `golapis.debug.cancel_timers()` | Cancels all pending timers, firing their callbacks immediately with `premature=true` |

## MoonScript Support

The `golapis` command can run MoonScript files directly. Files with a `.moon`
extension are automatically detected and compiled at load time.

The entry file is compiled immediately, and the `moonloader` is installed so
that you can `require()` any module written in MoonScript (with `.moon`) and it
will get compiled on the fly.

### Requirements

MoonScript must be installed for Lua 5.1 (LuaJIT is Lua 5.1 compatible):

```bash
luarocks install --lua-version=5.1 moonscript
```

### Usage

Run a MoonScript file directly:

```bash
golapis script.moon
```

Or start an HTTP server with a MoonScript entry point:

```bash
golapis --http --port=8080 app.moon
```
