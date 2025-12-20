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
err := lua.RunFile("script.lua")
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

    if err := lua.RunFile("script.lua"); err != nil {
        fmt.Printf("Error: %v\n", err)
        os.Exit(1)
    }
}
```

## Lua API

The `golapis` global table is available in Lua scripts:

| Function | Description |
|----------|-------------|
| `golapis.say(...)` | Output values followed by newline, returns `1` or `nil, err` |
| `golapis.print(...)` | Output values without newline, returns `1` or `nil, err` |
| `golapis.sleep(seconds)` | Async sleep (yields the coroutine) |
| `golapis.http.request(url)` | HTTP GET request, returns `body, status, headers` |
| `golapis.null` | Null sentinel value (outputs as `"null"`) |
| `golapis.version` | Library version string |

### Example Lua Script

```lua
golapis.say("Hello from Lua!")
golapis.say("Version: ", golapis.version)

golapis.sleep(1)
golapis.say("Slept for 1 second")

local body, status, headers = golapis.http.request("https://example.com")
golapis.say("Status: ", status)
```

## ngx API Compatibility

golapis implements a subset of the OpenResty/nginx-lua API:

| golapis | ngx equivalent | Notes |
|---------|----------------|-------|
| `golapis.say(...)` | `ngx.say(...)` | Output with newline, nginx-compatible type coercion |
| `golapis.print(...)` | `ngx.print(...)` | Output without newline, nginx-compatible type coercion |
| `golapis.null` | `ngx.null` | Null sentinel value |
| `golapis.sleep(seconds)` | `ngx.sleep(seconds)` | Async sleep, yields coroutine |
| `golapis.req.get_uri_args([max])` | `ngx.req.get_uri_args([max])` | Parse query string parameters |
| `golapis.req.get_headers([max], [raw])` | `ngx.req.get_headers([max], [raw])` | Get request headers as table |
| `golapis.timer.at(delay, cb, ...)` | `ngx.timer.at(delay, cb, ...)` | Schedule callback after delay |
| `golapis.var.*` | `ngx.var.*` | Request variables (read-only, HTTP mode only) |
| `golapis.header.*` | `ngx.header.*` | Response headers (write before first output) |
| `golapis.ctx` | `ngx.ctx` | Per-request Lua table for storing data |

### golapis.var Variables

| Variable | Description |
|----------|-------------|
| `request_method` | HTTP method (GET, POST, etc.) |
| `request_uri` | Full request URI including query string |
| `scheme` | "http" or "https" |
| `host` | Hostname without port |
| `server_port` | Server port number |
| `remote_addr` | Client IP address |
| `args` | Query string (nil if empty) |
| `http_*` | Any HTTP header (e.g., `http_user_agent`, `http_host`) |

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
