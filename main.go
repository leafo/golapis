package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golapis/golapis"
)

// fileServerFlags implements flag.Value to collect multiple --file-server flags
type fileServerFlags []string

func (f *fileServerFlags) String() string { return strings.Join(*f, ", ") }
func (f *fileServerFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// sharedDictFlags implements flag.Value to collect multiple --shared-dict flags
type sharedDictFlags []string

func (f *sharedDictFlags) String() string { return strings.Join(*f, ", ") }
func (f *sharedDictFlags) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// httpAddrFlag holds a listen address, empty when HTTP mode is off. Validating
// in Set stops `--http app.lua` from silently consuming the script argument.
type httpAddrFlag string

func (f *httpAddrFlag) String() string { return string(*f) }
func (f *httpAddrFlag) Set(s string) error {
	addr, err := golapis.NormalizeListenAddr(s)
	if err != nil {
		return err
	}
	*f = httpAddrFlag(addr)
	return nil
}

// durationFlag holds a ParseHumanDuration value, tracking whether it was given
// so unset flags keep the server defaults
type durationFlag struct {
	value time.Duration
	set   bool
}

func (f *durationFlag) String() string {
	if !f.set {
		return ""
	}
	return f.value.String()
}

func (f *durationFlag) Set(s string) error {
	d, err := golapis.ParseHumanDuration(s)
	if err != nil {
		return err
	}
	f.value, f.set = d, true
	return nil
}

func (f *durationFlag) apply(dst *time.Duration) {
	if !f.set {
		return
	}
	if f.value == 0 {
		*dst = golapis.NoTimeout
	} else {
		*dst = f.value
	}
}

type httpTimeoutFlags struct {
	readHeader, read, write, idle, shutdown durationFlag
}

func (t *httpTimeoutFlags) apply(config *golapis.HTTPServerConfig) {
	t.readHeader.apply(&config.ReadHeaderTimeout)
	t.read.apply(&config.ReadTimeout)
	t.write.apply(&config.WriteTimeout)
	t.idle.apply(&config.IdleTimeout)
	t.shutdown.apply(&config.ShutdownTimeout)
}

var (
	version   = "dev"
	gitCommit = "unknown"
	buildDate = "unknown"
)

func main() {
	var httpAddr httpAddrFlag
	flag.Var(&httpAddr, "http", "Start HTTP server on PORT or HOST:PORT")
	versionFlag := flag.Bool("version", false, "Print version information and exit")
	vFlag := flag.Bool("v", false, "show version information")
	eFlag := flag.String("e", "", "execute string 'stat'")
	lFlag := flag.String("l", "", "require library 'name'")
	ngxFlag := flag.Bool("ngx", false, "alias golapis table to global ngx")
	workersFlag := flag.Int("workers", 1, "Lua states serving HTTP requests, 0 for one per CPU")
	initWorkerFlag := flag.String("init-worker", "", "Lua file to run once per worker before it serves requests")
	sslTrustedFlag := flag.String("ssl-trusted-certificate", "", "PEM bundle of CA certificates for cosocket ssl_verify (default: system roots)")
	var fileServers fileServerFlags
	flag.Var(&fileServers, "file-server", "Serve static files: LOCAL_PATH:URL_PREFIX (can be repeated)")
	var sharedDicts sharedDictFlags
	flag.Var(&sharedDicts, "shared-dict", "Declare a shared dictionary: NAME:SIZE (e.g. cache:10m, can be repeated)")
	var timeouts httpTimeoutFlags
	flag.Var(&timeouts.readHeader, "http-read-header-timeout", "Max time to read request headers, 0 to disable (default "+formatSeconds(golapis.DefaultReadHeaderTimeout)+")")
	flag.Var(&timeouts.read, "http-read-timeout", "Max time to read the full request, 0 to disable (default "+formatSeconds(golapis.DefaultReadTimeout)+")")
	flag.Var(&timeouts.write, "http-write-timeout", "Max time from end of request headers to end of response, 0 to disable (default "+formatSeconds(golapis.DefaultWriteTimeout)+")")
	flag.Var(&timeouts.idle, "http-idle-timeout", "Max keep-alive idle time, 0 to disable (default "+formatSeconds(golapis.DefaultIdleTimeout)+")")
	flag.Var(&timeouts.shutdown, "http-shutdown-timeout", "Max time to wait for requests on graceful shutdown, 0 to disable (default "+formatSeconds(golapis.DefaultShutdownTimeout)+")")
	flag.Parse()

	if *versionFlag || *vFlag {
		fmt.Printf("golapis %s\n", version)
		fmt.Printf("  commit: %s\n", gitCommit)
		fmt.Printf("  built:  %s\n", buildDate)
		fmt.Printf("  luajit: %s\n", golapis.LuaJITVersion())
		fmt.Printf("  go:     %s\n", runtime.Version())
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
		fmt.Fprintln(os.Stderr, "  --http ADDR  start HTTP server on PORT or HOST:PORT")
		fmt.Fprintln(os.Stderr, "  --ngx    alias golapis table to global ngx")
		fmt.Fprintln(os.Stderr, "  --workers N  Lua states serving HTTP requests, 0 for one per CPU (default 1)")
		fmt.Fprintln(os.Stderr, "  --init-worker FILE  run FILE once per worker before it serves requests")
		fmt.Fprintln(os.Stderr, "  --ssl-trusted-certificate FILE  CA bundle for cosocket ssl_verify")
		fmt.Fprintln(os.Stderr, "  --file-server PATH[:URL] serve static files (can be repeated)")
		fmt.Fprintln(os.Stderr, "  --shared-dict NAME:SIZE  declare a shared dictionary (can be repeated)")
		fmt.Fprintf(os.Stderr, "  --http-read-header-timeout TIME  (default %s)\n", formatSeconds(golapis.DefaultReadHeaderTimeout))
		fmt.Fprintf(os.Stderr, "  --http-read-timeout TIME         (default %s)\n", formatSeconds(golapis.DefaultReadTimeout))
		fmt.Fprintf(os.Stderr, "  --http-write-timeout TIME        (default %s)\n", formatSeconds(golapis.DefaultWriteTimeout))
		fmt.Fprintf(os.Stderr, "  --http-idle-timeout TIME         (default %s)\n", formatSeconds(golapis.DefaultIdleTimeout))
		fmt.Fprintf(os.Stderr, "  --http-shutdown-timeout TIME     (default %s)\n", formatSeconds(golapis.DefaultShutdownTimeout))
		fmt.Fprintln(os.Stderr, "      TIME is N[ms|s|m|h|d], bare N is seconds, 0 disables")
		os.Exit(1)
	}

	var filename string
	var scriptArgs []string
	if len(args) > 0 {
		filename = args[0]
		scriptArgs = args[1:]
	}

	// Register shared dicts before any Lua state is created
	for _, sd := range sharedDicts {
		if err := registerSharedDictFlag(sd); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	if *initWorkerFlag != "" && httpAddr == "" {
		fmt.Fprintln(os.Stderr, "--init-worker only applies in HTTP server mode (--http)")
		os.Exit(1)
	}

	if httpAddr != "" {
		if *workersFlag < 0 {
			fmt.Fprintln(os.Stderr, "--workers must be a positive number of workers, or 0 for one per CPU")
			os.Exit(1)
		}
		var entry golapis.EntryPoint
		if *eFlag != "" {
			entry = golapis.CodeEntryPoint{Code: *eFlag}
		} else if filename != "" {
			entry = golapis.FileEntryPoint{Filename: filename}
		} else {
			fmt.Fprintln(os.Stderr, "HTTP mode requires a script file or -e code")
			os.Exit(1)
		}
		startHTTPServer(entry, string(httpAddr), *ngxFlag, *sslTrustedFlag, fileServers, &timeouts, *workersFlag, *initWorkerFlag)
	} else {
		runSingleExecution(filename, scriptArgs, *lFlag, *eFlag, *ngxFlag, *sslTrustedFlag)
	}
}

// formatSeconds prints d in the --http-*-timeout input format rather than Go's
func formatSeconds(d time.Duration) string {
	if d%time.Second != 0 {
		return fmt.Sprintf("%dms", int64(d/time.Millisecond))
	}
	return fmt.Sprintf("%ds", int64(d/time.Second))
}

// registerSharedDictFlag parses a NAME:SIZE flag value and registers the dict
func registerSharedDictFlag(spec string) error {
	name, size, ok := strings.Cut(spec, ":")
	if !ok || name == "" || size == "" {
		return fmt.Errorf("invalid shared dict %q, expected NAME:SIZE (e.g. cache:10m)", spec)
	}
	bytes, err := golapis.ParseHumanSize(size)
	if err != nil {
		return fmt.Errorf("invalid shared dict size %q: %w", size, err)
	}
	return golapis.RegisterSharedDict(name, bytes)
}

func runSingleExecution(filename string, scriptArgs []string, requireLib string, executeCode string, ngxAlias bool, sslTrusted string) {
	lua := golapis.NewGolapisLuaState()
	if lua == nil {
		fmt.Println("Failed to create Lua state")
		os.Exit(1)
	}
	defer lua.Close()

	if ngxAlias {
		lua.SetupNgxAlias()
	}
	if sslTrusted != "" {
		if err := lua.SetSSLTrustedCertificate(sslTrusted); err != nil {
			fmt.Fprintln(os.Stderr, "ssl trusted certificate:", err)
			os.Exit(1)
		}
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

func startHTTPServer(entry golapis.EntryPoint, addr string, ngxAlias bool, sslTrusted string, fileServers []string, timeouts *httpTimeoutFlags, workers int, initWorker string) {
	config := golapis.DefaultHTTPServerConfig()
	config.Workers = workers
	if initWorker != "" {
		config.InitWorker = golapis.FileEntryPoint{Filename: initWorker}
	}
	config.NgxAlias = ngxAlias
	config.SSLTrustedCert = sslTrusted
	timeouts.apply(config)

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

	golapis.StartHTTPServer(entry, addr, config)
}
