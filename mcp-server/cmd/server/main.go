package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/buildinfo"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	mcpserver "browsernerd-mcp-server/internal/mcp"
	"browsernerd-mcp-server/internal/security"
)

var (
	exitCleanupsMu sync.Mutex
	exitCleanups   []func()
)

func registerExitCleanup(fn func()) {
	exitCleanupsMu.Lock()
	defer exitCleanupsMu.Unlock()
	exitCleanups = append(exitCleanups, fn)
}

func runExitCleanups() {
	exitCleanupsMu.Lock()
	cleanups := make([]func(), len(exitCleanups))
	copy(cleanups, exitCleanups)
	exitCleanups = nil
	exitCleanupsMu.Unlock()

	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
}

// fatalf reports an unrecoverable startup failure to BOTH the configured log
// sink and stderr, then exits.
//
// Routine logging is redirected to a file in stdio mode, which means a plain
// log.Fatalf writes the reason into a file the operator has to already know to
// look for. To an MCP client the server just vanishes with no output at all --
// the failure is real, the diagnosis is invisible. Only STDOUT is the MCP
// protocol channel, so mirroring to stderr is safe and is what clients surface.
func fatalf(format string, args ...any) {
	runExitCleanups()
	log.Printf(format, args...)
	fmt.Fprintf(os.Stderr, "browsernerd: "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	configPath := flag.String("config", "", "Path to the BrowserNERD MCP config file (overrides workspace config)")
	httpPort := flag.Int("http-port", 0, "Serve MCP over Streamable HTTP at /mcp on this port instead of stdio (falls back to config mcp.http_port)")
	httpHost := flag.String("http-host", "", "Interface the HTTP server listens on (falls back to config mcp.http_host, default 127.0.0.1)")
	noWorkspace := flag.Bool("no-workspace", false, "Disable .browsernerd/ workspace discovery")
	workspaceDir := flag.String("workspace-dir", "", "Explicit workspace root (skip walk-up discovery)")
	trustWorkspace := flag.Bool("trust-workspace-config", false, "Allow workspace config to launch/attach browsers or access paths outside the workspace")
	initWorkspace := flag.Bool("init-workspace", false, "Create .browsernerd/ template in current directory and exit")
	showVersion := flag.Bool("version", false, "Print the binary's version, revision and build time as JSON and exit")
	runDoctor := flag.Bool("doctor", false, "Resolve the configuration, check paths, schema, environment and server log sources, print a JSON report and exit (nonzero when problems are found)")
	flag.Parse()

	if *showVersion {
		printJSON(buildinfo.Read())
		return
	}

	// Handle --init-workspace early exit
	if *initWorkspace {
		root := "."
		if *workspaceDir != "" {
			root = *workspaceDir
		}
		if err := config.InitWorkspace(root); err != nil {
			log.Fatalf("failed to initialize workspace: %v", err)
		}
		log.Printf("created .browsernerd/ workspace in %s", root)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	defer func() {
		if r := recover(); r != nil {
			runExitCleanups()
			log.Printf("FATAL PANIC IN MAIN: %v", r)
			os.Exit(1)
		}
	}()

	opts := config.WorkspaceOptions{
		Disable:     *noWorkspace,
		ExplicitDir: *workspaceDir,
		Trust:       *trustWorkspace,
	}

	cfg, wsDir, err := config.LoadWithWorkspace(*configPath, opts)
	if err != nil {
		// Before we can redirect logs, write to stderr as last resort
		log.Fatalf("failed to load config: %v", err)
	}
	if wsDir != "" {
		log.Printf("using workspace config from %s", wsDir)
	}

	if *runDoctor {
		os.Exit(doctor(ctx, cfg, wsDir))
	}

	// Route routine logging to a file in stdio mode. Only STDOUT carries MCP
	// protocol frames; stderr is the channel MCP clients surface to operators, so
	// it stays available as the fallback. Never discard logs -- a server that dies
	// without saying why is indistinguishable from one that crashed.
	if cfg.MCP.HTTPPort == 0 && cfg.Server.LogFile != "" {
		logDir := filepath.Dir(cfg.Server.LogFile)
		var dirErr error
		if logDir != "" && logDir != "." {
			dirErr = security.EnsurePrivateDir(logDir)
		}
		if dirErr != nil {
			fmt.Fprintf(os.Stderr, "browsernerd: cannot prepare log directory %s (%v); logging to stderr\n", logDir, dirErr)
		} else {
			logFile, openErr := os.OpenFile(cfg.Server.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
			if openErr == nil {
				_ = logFile.Chmod(0o600)
				log.SetOutput(logFile)
				defer logFile.Close()
			} else {
				fmt.Fprintf(os.Stderr, "browsernerd: cannot open log file %s (%v); logging to stderr\n", cfg.Server.LogFile, openErr)
			}
		}
	}
	if *httpPort != 0 {
		cfg.MCP.HTTPPort = *httpPort
	}
	if *httpHost != "" {
		cfg.MCP.HTTPHost = *httpHost
	}

	mangleEngine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		fatalf("failed to initialize mangle engine: %v", err)
	}

	var browserRedactor *security.Redactor
	if cfg.Security.IsRedactionEnabled() {
		browserRedactor = security.NewRedactor(cfg.Security.ExtraSensitiveKeys)
	}
	sessionManager := browser.NewSessionManagerWithSecurity(cfg.Browser, mangleEngine, browserRedactor)
	cleanupManager := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = sessionManager.Shutdown(cleanupCtx)
	}
	registerExitCleanup(cleanupManager)
	defer cleanupManager()

	if reaped, err := sessionManager.ReapOrphans(ctx); err != nil {
		log.Printf("warning: failed to reap orphaned Chrome processes: %v", err)
	} else if reaped > 0 {
		log.Printf("reaped %d orphaned Chrome process tree(s) and temp profile(s)", reaped)
	}

	if cfg.Browser.AutoStart {
		if err := sessionManager.Start(ctx); err != nil {
			fatalf("failed to initialize Rod session manager: %v", err)
		}
	} else {
		log.Printf("browser auto-start disabled; use MCP tools to launch/attach later")
	}

	server, err := mcpserver.NewServer(cfg, sessionManager, mangleEngine)
	if err != nil {
		fatalf("failed to initialize MCP server: %v", err)
	}
	defer func() {
		if closeErr := server.Close(); closeErr != nil {
			log.Printf("failed to close MCP server resources: %v", closeErr)
		}
	}()

	var startErr error
	if cfg.MCP.HTTPPort > 0 {
		log.Printf("starting BrowserNERD MCP Streamable HTTP server on http://%s/mcp", net.JoinHostPort(cfg.MCP.GetHTTPHost(), strconv.Itoa(cfg.MCP.HTTPPort)))
		startErr = server.StartHTTP(ctx, cfg.MCP.GetHTTPHost(), cfg.MCP.HTTPPort)
	} else {
		log.Printf("starting BrowserNERD MCP stdio server")
		startErr = server.Start(ctx)
	}

	if startErr != nil && !errors.Is(startErr, context.Canceled) {
		fatalf("server exited with error: %v", startErr)
	}
}

// doctor runs the self-diagnosis without starting MCP or Chrome. The schema is
// parsed for real, so a broken module is named here rather than at first use.
// Environment checks describe THIS process: an MCP client passes the server its
// own env block, so run it with the same variables to mirror the server.
func doctor(ctx context.Context, cfg config.Config, wsDir string) int {
	deps := mcpserver.DoctorDeps{Config: cfg}
	schemaErr := ""
	if engine, err := mangle.NewEngine(cfg.Mangle); err != nil {
		schemaErr = err.Error()
	} else {
		deps.Engine = engine
	}
	deps.Logs, _ = mcpserver.NewLogSources(cfg)

	report := mcpserver.DoctorReport(ctx, deps)
	if wsDir != "" {
		report["workspace"] = wsDir
	}
	if schemaErr != "" {
		report["schema_error"] = schemaErr
		report["status"] = "degraded"
		if problems, ok := report["problems"].([]string); ok {
			report["problems"] = append(problems, "Mangle schema does not load: "+schemaErr)
		}
	}
	printJSON(security.NewRedactor(cfg.Security.ExtraSensitiveKeys).Sanitize(report))
	if report["status"] != "ok" {
		return 1
	}
	return 0
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
