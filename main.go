package main

import (
	"flag"
	"fmt"
	"io"
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
	vFlag := flag.Bool("v", false, "show version information")
	eFlag := flag.String("e", "", "execute string 'stat'")
	lFlag := flag.String("l", "", "require library 'name'")
	ngxFlag := flag.Bool("ngx", false, "alias golapis table to global ngx")
	flag.Parse()

	if *versionFlag || *vFlag {
		fmt.Printf("golapis %s\n", version)
		fmt.Printf("  commit: %s\n", gitCommit)
		fmt.Printf("  built:  %s\n", buildDate)
		fmt.Printf("  luajit: %s\n", golapis.LuaJITVersion())
		os.Exit(0)
	}

	args := flag.Args()

	// Require at least a filename unless -e is provided
	if len(args) < 1 && *eFlag == "" {
		fmt.Fprintf(os.Stderr, "usage: %s [options] [script [args]]\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Available options are:")
		fmt.Fprintln(os.Stderr, "  -e stat  execute string 'stat'")
		fmt.Fprintln(os.Stderr, "  -l name  require library 'name'")
		fmt.Fprintln(os.Stderr, "  -v       show version information")
		fmt.Fprintln(os.Stderr, "  --       stop handling options")
		fmt.Fprintln(os.Stderr, "  -        execute stdin and stop handling options")
		fmt.Fprintln(os.Stderr, "  --http   start HTTP server mode")
		fmt.Fprintln(os.Stderr, "  --port   port for HTTP server (default 8080)")
		fmt.Fprintln(os.Stderr, "  --ngx    alias golapis table to global ngx")
		os.Exit(1)
	}

	var filename string
	var scriptArgs []string
	if len(args) > 0 {
		filename = args[0]
		scriptArgs = args[1:]
	}

	if *httpFlag {
		if filename == "" {
			fmt.Fprintln(os.Stderr, "HTTP mode requires a script file")
			os.Exit(1)
		}
		startHTTPServer(filename, *portFlag, *ngxFlag)
	} else {
		runSingleExecution(filename, scriptArgs, *lFlag, *eFlag, *ngxFlag)
	}
}

func runSingleExecution(filename string, scriptArgs []string, requireLib string, executeCode string, ngxAlias bool) {
	lua := golapis.NewGolapisLuaState()
	if lua == nil {
		fmt.Println("Failed to create Lua state")
		os.Exit(1)
	}
	defer lua.Close()

	if ngxAlias {
		lua.SetupNgxAlias()
	}

	lua.Start()
	defer lua.Stop()

	// Set up arg table (use "-e" as script name if no file provided)
	scriptName := filename
	if scriptName == "" {
		scriptName = "=(command line)"
	}
	lua.SetupArgTable(os.Args[0], scriptName, scriptArgs)

	// Handle -l: require library
	if requireLib != "" {
		if err := lua.RequireModule(requireLib); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	// Handle -e: execute string (if no filename provided)
	if filename == "" && executeCode != "" {
		if err := lua.RunString(executeCode); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		lua.Wait()
		return
	}

	// Handle "-": read from stdin
	if filename == "-" {
		stdinBytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error reading stdin:", err)
			os.Exit(1)
		}
		if err := lua.RunString(string(stdinBytes)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		lua.Wait()
		return
	}

	// Run the file
	if err := lua.RunFile(filename, scriptArgs); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Wait for all threads and timers to complete before stopping
	lua.Wait()
}

func startHTTPServer(filename, port string, ngxAlias bool) {
	config := golapis.DefaultHTTPServerConfig()
	config.NgxAlias = ngxAlias
	golapis.StartHTTPServer(filename, port, config)
}
