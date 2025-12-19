# golapis

A Go library for embedding LuaJIT with async-capable bindings.

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

#### Wait for threads

`Wait()` blocks until all active threads have completed execution.

### Output Handling

By default, output from `golapis.print()` goes to stdout. You can redirect it:

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
| `golapis.print(...)` | Print values to the configured output writer |
| `golapis.sleep(seconds)` | Async sleep (yields the coroutine) |
| `golapis.http.request(url)` | HTTP GET request, returns `body, status, headers` |
| `golapis.version` | Library version string |

### Example Lua Script

```lua
golapis.print("Hello from Lua!")
golapis.print("Version:", golapis.version)

golapis.sleep(1)
golapis.print("Slept for 1 second")

local body, status, headers = golapis.http.request("https://example.com")
golapis.print("Status:", status)
```
