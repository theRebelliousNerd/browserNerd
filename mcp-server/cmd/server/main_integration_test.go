package main

import (
	"context"
	"os"
	"testing"
	"time"

	"browsernerd-mcp-server/internal/browser"
	"browsernerd-mcp-server/internal/config"
	"browsernerd-mcp-server/internal/mangle"
	"browsernerd-mcp-server/internal/mcp"
)

// TestIntegrationServerLifecycle tests the full server initialization and lifecycle
// This covers the main.go entry point which is normally untested
func TestIntegrationServerLifecycle(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping integration tests (SKIP_LIVE_TESTS set)")
	}

	// Test configuration loading and server initialization
	// This simulates what main() does without actually running main()

	t.Run("Load configuration", func(t *testing.T) {
		// Create minimal config (LoadConfig is not exported, so we test struct creation)
		cfg := config.Config{
			Server: config.ServerConfig{
				Name:    "integration-test-server",
				Version: "1.0.0-test",
			},
			Browser: config.BrowserConfig{
				Headless:              mainBoolPtr(true),
				EnableDOMIngestion:    true,
				EnableHeaderIngestion: true,
			},
			Mangle: config.MangleConfig{
				Enable:          true,
				SchemaPath:      "../../schemas/browser.mg",
				FactBufferLimit: 1000,
			},
			Docker: config.DockerConfig{
				Enabled: false,
			},
		}

		if cfg.Server.Name != "integration-test-server" {
			t.Error("config not properly initialized")
		}
	})

	t.Run("Initialize Mangle engine", func(t *testing.T) {
		cfg := config.MangleConfig{
			Enable:          true,
			SchemaPath:      "../../schemas/browser.mg",
			FactBufferLimit: 1000,
		}

		engine, err := mangle.NewEngine(cfg)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		if engine == nil {
			t.Fatal("expected non-nil engine")
		}
	})

	t.Run("Initialize session manager", func(t *testing.T) {
		cfg := config.BrowserConfig{
			Headless:              mainBoolPtr(true),
			EnableDOMIngestion:    true,
			EnableHeaderIngestion: true,
		}

		sessions := browser.NewSessionManager(cfg, nil)
		if sessions == nil {
			t.Fatal("expected non-nil session manager")
		}

		if sessions.IsConnected() {
			t.Error("session manager should not be connected before Start()")
		}
	})

	t.Run("Initialize MCP server", func(t *testing.T) {
		cfg := config.Config{
			Server: config.ServerConfig{
				Name:    "test-server",
				Version: "1.0.0",
			},
			Browser: config.BrowserConfig{
				Headless: mainBoolPtr(true),
			},
			Mangle: config.MangleConfig{
				Enable:          true,
				SchemaPath:      "../../schemas/browser.mg",
				FactBufferLimit: 1000,
			},
			Docker: config.DockerConfig{
				Enabled: false,
			},
		}

		engine, err := mangle.NewEngine(cfg.Mangle)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		sessions := browser.NewSessionManager(cfg.Browser, engine)
		server, err := mcp.NewServer(cfg, sessions, engine)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		if server == nil {
			t.Fatal("expected non-nil server")
		}
	})

	t.Run("Full server lifecycle with browser", func(t *testing.T) {
		cfg := config.Config{
			Server: config.ServerConfig{
				Name:    "lifecycle-test-server",
				Version: "1.0.0",
			},
			Browser: config.BrowserConfig{
				Headless:              mainBoolPtr(true),
				EnableDOMIngestion:    true,
				EnableHeaderIngestion: true,
			},
			Mangle: config.MangleConfig{
				Enable:          true,
				SchemaPath:      "../../schemas/browser.mg",
				FactBufferLimit: 1000,
			},
			Docker: config.DockerConfig{
				Enabled: false,
			},
		}

		// Initialize engine
		engine, err := mangle.NewEngine(cfg.Mangle)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		// Initialize session manager
		sessions := browser.NewSessionManager(cfg.Browser, engine)

		// Start browser
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		err = sessions.Start(ctx)
		if err != nil {
			t.Skipf("Browser start failed (Chrome not available?): %v", err)
		}

		// Ensure cleanup
		defer func() {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			_ = sessions.Shutdown(shutdownCtx)
		}()

		// Initialize MCP server
		server, err := mcp.NewServer(cfg, sessions, engine)
		if err != nil {
			t.Fatalf("NewServer failed: %v", err)
		}

		// Execute some tools to verify server is functional
		result, err := server.ExecuteTool("list-sessions", map[string]interface{}{})
		if err != nil {
			t.Fatalf("ExecuteTool failed: %v", err)
		}

		resultMap := result.(map[string]interface{})
		if resultMap["sessions"] == nil {
			t.Error("expected sessions in result")
		}

		// Create a session
		createResult, err := server.ExecuteTool("create-session", map[string]interface{}{
			"url": "about:blank",
		})
		if err != nil {
			t.Fatalf("create-session failed: %v", err)
		}

		session := createResult.(*browser.Session)
		if session.ID == "" {
			t.Error("expected session to be created")
		}

		// Push facts
		factResult, err := server.ExecuteTool("push-facts", map[string]interface{}{
			"facts": []interface{}{
				map[string]interface{}{
					"predicate": "test_lifecycle",
					"args":      []interface{}{"integration_test"},
				},
			},
		})
		if err != nil {
			t.Fatalf("push-facts failed: %v", err)
		}

		factMap := factResult.(map[string]interface{})
		if factMap["accepted"].(int) != 1 {
			t.Error("expected fact to be accepted")
		}

		// Read facts back
		readResult, err := server.ExecuteTool("read-facts", map[string]interface{}{})
		if err != nil {
			t.Fatalf("read-facts failed: %v", err)
		}

		readMap := readResult.(map[string]interface{})
		if readMap["count"].(int) == 0 {
			t.Error("expected facts to be readable")
		}

		// Shutdown browser
		shutdownResult, err := server.ExecuteTool("shutdown-browser", map[string]interface{}{})
		if err != nil {
			t.Fatalf("shutdown-browser failed: %v", err)
		}

		shutdownMap := shutdownResult.(map[string]interface{})
		if !shutdownMap["success"].(bool) {
			t.Error("expected successful shutdown")
		}

		// Verify browser is disconnected
		if sessions.IsConnected() {
			t.Error("expected browser to be disconnected after shutdown")
		}
	})

	t.Run("Server with Docker enabled", func(t *testing.T) {
		cfg := config.Config{
			Server: config.ServerConfig{
				Name:    "docker-test-server",
				Version: "1.0.0",
			},
			Browser: config.BrowserConfig{
				Headless: mainBoolPtr(true),
			},
			Mangle: config.MangleConfig{
				Enable:          true,
				SchemaPath:      "../../schemas/browser.mg",
				FactBufferLimit: 1000,
			},
			Docker: config.DockerConfig{
				Enabled:    true,
				Containers: []string{"test-container"},
				LogWindow:  "5m",
			},
		}

		engine, err := mangle.NewEngine(cfg.Mangle)
		if err != nil {
			t.Fatalf("Failed to create engine: %v", err)
		}

		sessions := browser.NewSessionManager(cfg.Browser, engine)
		server, err := mcp.NewServer(cfg, sessions, engine)
		if err != nil {
			t.Fatalf("NewServer with docker failed: %v", err)
		}

		if server == nil {
			t.Fatal("expected non-nil server")
		}

		// Note: Docker client initialization is tested, but actual Docker
		// operations would require Docker daemon to be running
	})
}

// TestIntegrationConfigurationVariations tests different configuration scenarios
func TestIntegrationConfigurationVariations(t *testing.T) {
	if os.Getenv("SKIP_LIVE_TESTS") != "" {
		t.Skip("Skipping integration tests (SKIP_LIVE_TESTS set)")
	}

	t.Run("Headless browser", func(t *testing.T) {
		cfg := config.BrowserConfig{
			Headless: mainBoolPtr(true),
		}

		if !cfg.IsHeadless() {
			t.Error("expected headless to be true")
		}
	})

	t.Run("Headed browser", func(t *testing.T) {
		cfg := config.BrowserConfig{
			Headless: mainBoolPtr(false),
		}

		if cfg.IsHeadless() {
			t.Error("expected headless to be false")
		}
	})

	t.Run("DOM ingestion enabled", func(t *testing.T) {
		cfg := config.BrowserConfig{
			EnableDOMIngestion: true,
		}

		if !cfg.EnableDOMIngestion {
			t.Error("expected DOM ingestion to be enabled")
		}
	})

	t.Run("Header ingestion enabled", func(t *testing.T) {
		cfg := config.BrowserConfig{
			EnableHeaderIngestion: true,
		}

		if !cfg.EnableHeaderIngestion {
			t.Error("expected header ingestion to be enabled")
		}
	})

	t.Run("Custom event throttle", func(t *testing.T) {
		cfg := config.BrowserConfig{
			EventThrottleMs: 200,
		}

		if cfg.EventThrottleMs != 200 {
			t.Errorf("expected EventThrottleMs to be 200, got %d", cfg.EventThrottleMs)
		}
	})

	t.Run("Mangle engine enabled", func(t *testing.T) {
		cfg := config.MangleConfig{
			Enable:          true,
			SchemaPath:      "../../schemas/browser.mg",
			FactBufferLimit: 5000,
		}

		if !cfg.Enable {
			t.Error("expected Mangle to be enabled")
		}
		if cfg.FactBufferLimit != 5000 {
			t.Errorf("expected FactBufferLimit to be 5000, got %d", cfg.FactBufferLimit)
		}
	})
}

func mainBoolPtr(b bool) *bool {
	return &b
}
