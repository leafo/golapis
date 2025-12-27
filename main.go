package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golapis/golapis"
)

// fileServerFlags implements flag.Value to collect multiple --file-server flags
type fileServerFlags []string

func (f *fileServerFlags) String() string { return strings.Join(*f, ", ") }
func (f *fileServerFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}

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
	var fileServers fileServerFlags
	flag.Var(&fileServers, "file-server", "Serve static files: LOCAL_PATH:URL_PREFIX (can be repeated)")
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
		fmt.Fprintln(os.Stderr, "  --file-server PATH[:URL] serve static files (can be repeated)")
		os.Exit(1)
	}

	var filename string
	var scriptArgs []string
	if len(args) > 0 {
		filename = args[0]
		scriptArgs = args[1:]
	}

	if *httpFlag {
		var entry golapis.EntryPoint
		if *eFlag != "" {
			entry = golapis.CodeEntryPoint{Code: *eFlag}
		} else if filename != "" {
			entry = golapis.FileEntryPoint{Filename: filename}
		} else {
			fmt.Fprintln(os.Stderr, "HTTP mode requires a script file or -e code")
			os.Exit(1)
		}
		startHTTPServer(entry, *portFlag, *ngxFlag, fileServers)
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

func startHTTPServer(entry golapis.EntryPoint, port string, ngxAlias bool, fileServers []string) {
	config := golapis.DefaultHTTPServerConfig()
	config.NgxAlias = ngxAlias

	// Parse file server mappings
	for _, fs := range fileServers {
		var localPath, urlPrefix string
		if parts := strings.SplitN(fs, ":", 2); len(parts) == 2 {
			localPath, urlPrefix = parts[0], parts[1]
		} else {
			// Shorthand: derive URL prefix from directory name
			localPath = fs
			urlPrefix = filepath.Base(fs)
			// Reject if base doesn't produce a valid URL prefix
			if urlPrefix == "." || urlPrefix == ".." || urlPrefix == "/" || urlPrefix == "" {
				fmt.Fprintf(os.Stderr, "cannot derive URL prefix from %q, use explicit format PATH:URL_PREFIX\n", fs)
				os.Exit(1)
			}
		}
		config.FileServers = append(config.FileServers, golapis.FileServerMapping{
			LocalPath: localPath,
			URLPrefix: urlPrefix,
		})
	}

	golapis.StartHTTPServer(entry, port, config)
}
