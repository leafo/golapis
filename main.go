package main

import (
	"flag"
	"fmt"
	"os"

	"golapis/golapis"
)

func main() {
	httpFlag := flag.Bool("http", false, "Start as HTTP server")
	portFlag := flag.String("port", "8080", "Port for HTTP server")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Printf("Usage: %s [--http] [--port=8080] <lua_file>\n", os.Args[0])
		os.Exit(1)
	}

	filename := args[0]

	if *httpFlag {
		startHTTPServer(filename, *portFlag)
	} else {
		runSingleExecution(filename)
	}
}

func runSingleExecution(filename string) {
	lua := golapis.NewGolapisLuaState()
	if lua == nil {
		fmt.Println("Failed to create Lua state")
		os.Exit(1)
	}
	defer lua.Close()

	if err := lua.RunFile(filename); err != nil {
		fmt.Printf("Error running Lua file: %v\n", err)
		os.Exit(1)
	}
}

func startHTTPServer(filename, port string) {
	golapis.StartHTTPServer(filename, port)
}
