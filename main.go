package main

import (
	"flag"
	"fmt"
	"os"

	"golapis/golapis"
)

var (
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
)

func main() {
	httpFlag := flag.Bool("http", false, "Start as HTTP server")
	portFlag := flag.String("port", "8080", "Port for HTTP server")
	versionFlag := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("golapis %s\n", version)
		fmt.Printf("  commit: %s\n", gitCommit)
		fmt.Printf("  built:  %s\n", buildDate)
		fmt.Printf("  luajit: %s\n", golapis.LuaJITVersion())
		os.Exit(0)
	}

	args := flag.Args()
	if len(args) < 1 {
		fmt.Printf("Usage: %s [--http] [--port=8080] <lua_file>\n", os.Args[0])
		os.Exit(1)
	}

	filename := args[0]
	scriptArgs := args[1:]

	if *httpFlag {
		startHTTPServer(filename, *portFlag)
	} else {
		runSingleExecution(filename, scriptArgs)
	}
}

func runSingleExecution(filename string, scriptArgs []string) {
	lua := golapis.NewGolapisLuaState()
	if lua == nil {
		fmt.Println("Failed to create Lua state")
		os.Exit(1)
	}
	defer lua.Close()

	lua.Start()
	defer lua.Stop()

	lua.SetupArgTable(os.Args[0], filename, scriptArgs)

	if err := lua.RunFile(filename, scriptArgs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Wait for all threads and timers to complete before stopping
	lua.Wait()
}

func startHTTPServer(filename, port string) {
	config := golapis.DefaultHTTPServerConfig()
	golapis.StartHTTPServer(filename, port, config)
}
