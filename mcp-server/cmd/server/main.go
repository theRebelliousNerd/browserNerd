package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	mcpserver "browsernerd-mcp-server/internal/mcp"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to the BrowserNERD MCP config file")
	ssePort := flag.Int("sse-port", 0, "Optional SSE port override (falls back to config)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load(*configPath)
	if err != nil {
		// Before we can redirect logs, write to stderr as last resort
		log.Fatalf("failed to load config: %v", err)
	}

	// Redirect logging to file for stdio mode (stderr interferes with MCP protocol)
	if cfg.MCP.SSEPort == 0 && cfg.Server.LogFile != "" {
		logFile, err := os.OpenFile(cfg.Server.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			log.SetOutput(logFile)
			defer logFile.Close()
		} else {
			// If we can't open log file, disable logging to avoid stderr pollution
			log.SetOutput(io.Discard)
		}
	}
	if *ssePort != 0 {
		cfg.MCP.SSEPort = *ssePort
	}

	mangleEngine, err := mangle.NewEngine(cfg.Mangle)
	if err != nil {
		log.Fatalf("failed to initialize mangle engine: %v", err)
	}

	sessionManager := browser.NewSessionManager(cfg.Browser, mangleEngine)
	if cfg.Browser.AutoStart {
		if err := sessionManager.Start(ctx); err != nil {
			log.Fatalf("failed to initialize Rod session manager: %v", err)
		}
	} else {
		log.Printf("browser auto-start disabled; use MCP tools to launch/attach later")
	}

	server, err := mcpserver.NewServer(cfg, sessionManager, mangleEngine)
	if err != nil {
		log.Fatalf("failed to initialize MCP server: %v", err)
	}

	var startErr error
	if cfg.MCP.SSEPort > 0 {
		log.Printf("starting BrowserNERD MCP SSE server on port %d", cfg.MCP.SSEPort)
		startErr = server.StartSSE(ctx, cfg.MCP.SSEPort)
	} else {
		log.Printf("starting BrowserNERD MCP stdio server")
		startErr = server.Start(ctx)
	}

	if startErr != nil && !errors.Is(startErr, context.Canceled) {
		log.Fatalf("server exited with error: %v", startErr)
	}
}
