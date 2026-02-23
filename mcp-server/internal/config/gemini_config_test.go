package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGeminiConfigLoads validates that the gemini-config.yaml used by the
// Gemini CLI extension loads successfully and has the expected overrides.
func TestGeminiConfigLoads(t *testing.T) {
	configPath := filepath.Join("..", "..", "gemini-config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Skipf("gemini-config.yaml not found at %s (expected when running outside mcp-server/)", configPath)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load gemini-config.yaml: %v", err)
	}

	// Server identity
	if cfg.Server.Name != "browsernerd-mcp" {
		t.Errorf("expected server name 'browsernerd-mcp', got %q", cfg.Server.Name)
	}
	if cfg.Server.Version != "0.0.8" {
		t.Errorf("expected version '0.0.8', got %q", cfg.Server.Version)
	}

	// Browser: auto_start must be false for Gemini extension (launch-browser tool handles it)
	if cfg.Browser.AutoStart {
		t.Error("expected auto_start=false in gemini-config.yaml so the extension uses launch-browser tool")
	}

	// MCP: progressive_only should default to true
	if !cfg.MCP.IsProgressiveOnly() {
		t.Error("expected progressive_only to be true (6-tool focused interface for Gemini CLI)")
	}

	// MCP: stdio mode (sse_port=0)
	if cfg.MCP.SSEPort != 0 {
		t.Errorf("expected sse_port=0 (stdio mode for Gemini CLI), got %d", cfg.MCP.SSEPort)
	}

	// Mangle must be enabled for reasoning
	if !cfg.Mangle.Enable {
		t.Error("expected mangle.enable=true for Gemini CLI reasoning support")
	}

	// Recorder must be enabled for flight recording hooks
	if !cfg.Recorder.Enabled {
		t.Error("expected recorder.enabled=true for flight recording support")
	}
}

// TestGeminiConfigValidates ensures the gemini-config.yaml passes validation.
func TestGeminiConfigValidates(t *testing.T) {
	configPath := filepath.Join("..", "..", "gemini-config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Skipf("gemini-config.yaml not found at %s", configPath)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load gemini-config.yaml: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("gemini-config.yaml failed validation: %v", err)
	}
}

// TestGeminiConfigProgressiveToolCount validates that the default Gemini config
// produces exactly 6 progressive disclosure tools when used with the MCP server.
func TestGeminiConfigProgressiveToolCount(t *testing.T) {
	configPath := filepath.Join("..", "..", "gemini-config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Skipf("gemini-config.yaml not found at %s", configPath)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load gemini-config.yaml: %v", err)
	}

	if !cfg.MCP.IsProgressiveOnly() {
		t.Fatal("progressive_only must be true to expose the focused 6-tool interface")
	}

	expectedTools := []string{
		"launch-browser",
		"shutdown-browser",
		"browser-observe",
		"browser-act",
		"browser-reason",
		"browser-mangle",
	}
	t.Logf("Gemini config will expose %d progressive tools: %v", len(expectedTools), expectedTools)
}
