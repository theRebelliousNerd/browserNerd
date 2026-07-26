package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	mcpserver "browsernerd-mcp-server/internal/mcp"
	"browsernerd-mcp-server/internal/security"
)

// fatalf reports an unrecoverable startup failure to BOTH the configured log
// sink and stderr, then exits.
//
// Routine logging is redirected to a file in stdio mode, which means a plain
// log.Fatalf writes the reason into a file the operator has to already know to
// look for. To an MCP client the server just vanishes with no output at all --
// the failure is real, the diagnosis is invisible. Only STDOUT is the MCP
// protocol channel, so mirroring to stderr is safe and is what clients surface.
func fatalf(format string, args ...any) {
	log.Printf(format, args...)
	fmt.Fprintf(os.Stderr, "browsernerd: "+format+"\n", args...)
	os.Exit(1)
}

func main() {
	configPath := flag.String("config", "", "Path to the BrowserNERD MCP config file (overrides workspace config)")
	ssePort := flag.Int("sse-port", 0, "Optional SSE port override (falls back to config)")
	noWorkspace := flag.Bool("no-workspace", false, "Disable .browsernerd/ workspace discovery")
	workspaceDir := flag.String("workspace-dir", "", "Explicit workspace root (skip walk-up discovery)")
	trustWorkspace := flag.Bool("trust-workspace-config", false, "Allow workspace config to launch/attach browsers or access paths outside the workspace")
	initWorkspace := flag.Bool("init-workspace", false, "Create .browsernerd/ template in current directory and exit")
	flag.Parse()

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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	defer func() {
		if r := recover(); r != nil {
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

	// Route routine logging to a file in stdio mode. Only STDOUT carries MCP
	// protocol frames; stderr is the channel MCP clients surface to operators, so
	// it stays available as the fallback. Never discard logs -- a server that dies
	// without saying why is indistinguishable from one that crashed.
	if cfg.MCP.SSEPort == 0 && cfg.Server.LogFile != "" {
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
	if *ssePort != 0 {
		cfg.MCP.SSEPort = *ssePort
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
	if cfg.MCP.SSEPort > 0 {
		log.Printf("starting BrowserNERD MCP SSE server on port %d", cfg.MCP.SSEPort)
		startErr = server.StartSSE(ctx, cfg.MCP.SSEPort)
	} else {
		log.Printf("starting BrowserNERD MCP stdio server")
		startErr = server.Start(ctx)
	}

	if startErr != nil && !errors.Is(startErr, context.Canceled) {
		fatalf("server exited with error: %v", startErr)
	}
}
