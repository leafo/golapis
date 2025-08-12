package main

import (
	"fmt"
	"os"

	"golapis/golapis"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Printf("Usage: %s <lua_file>\n", os.Args[0])
		os.Exit(1)
	}

	lua := golapis.NewLuaState()
	if lua == nil {
		fmt.Println("Failed to create Lua state")
		os.Exit(1)
	}
	defer lua.Close()

	filename := os.Args[1]
	if err := lua.RunFile(filename); err != nil {
		fmt.Printf("Error running Lua file: %v\n", err)
		os.Exit(1)
	}
}
