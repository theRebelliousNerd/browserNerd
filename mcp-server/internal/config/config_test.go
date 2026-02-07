package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Server defaults
	if cfg.Server.Name != "browsernerd-mcp" {
		t.Errorf("expected server name 'browsernerd-mcp', got %q", cfg.Server.Name)
	}
	if cfg.Server.Version != "0.0.4" {
		t.Errorf("expected server version '0.0.4', got %q", cfg.Server.Version)
	}
	if cfg.Server.LogFile != "browsernerd-mcp.log" {
		t.Errorf("expected log file 'browsernerd-mcp.log', got %q", cfg.Server.LogFile)
	}

	// Browser defaults
	if !cfg.Browser.AutoStart {
		t.Error("expected AutoStart to be true")
	}
	if cfg.Browser.DefaultNavigationTimeout != "15s" {
		t.Errorf("expected navigation timeout '15s', got %q", cfg.Browser.DefaultNavigationTimeout)
	}
	if cfg.Browser.DefaultAttachTimeout != "10s" {
		t.Errorf("expected attach timeout '10s', got %q", cfg.Browser.DefaultAttachTimeout)
	}
	if cfg.Browser.SessionStore != "sessions.json" {
		t.Errorf("expected session store 'sessions.json', got %q", cfg.Browser.SessionStore)
	}
	if !cfg.Browser.EnableDOMIngestion {
		t.Error("expected EnableDOMIngestion to be true")
	}
	if !cfg.Browser.EnableHeaderIngestion {
		t.Error("expected EnableHeaderIngestion to be true")
	}
	if cfg.Browser.EventLoggingLevel != "normal" {
		t.Errorf("expected event logging level 'normal', got %q", cfg.Browser.EventLoggingLevel)
	}
	if cfg.Browser.ViewportWidth != 1920 {
		t.Errorf("expected viewport width 1920, got %d", cfg.Browser.ViewportWidth)
	}
	if cfg.Browser.ViewportHeight != 1080 {
		t.Errorf("expected viewport height 1080, got %d", cfg.Browser.ViewportHeight)
	}

	// Mangle defaults
	if !cfg.Mangle.Enable {
		t.Error("expected Mangle.Enable to be true")
	}
	if cfg.Mangle.SchemaPath != "schemas/browser.mg" {
		t.Errorf("expected schema path 'schemas/browser.mg', got %q", cfg.Mangle.SchemaPath)
	}
	if cfg.Mangle.FactBufferLimit != 2048 {
		t.Errorf("expected fact buffer limit 2048, got %d", cfg.Mangle.FactBufferLimit)
	}

	// Docker defaults
	if cfg.Docker.Enabled {
		t.Error("expected Docker.Enabled to be false")
	}
	if cfg.Docker.LogWindow != "30s" {
		t.Errorf("expected log window '30s', got %q", cfg.Docker.LogWindow)
	}
}

func TestLoadEmptyPath(t *testing.T) {
	_, err := Load("")
	if err == nil {
		t.Error("expected error for empty path")
	}
	if err.Error() != "config path is required" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestLoadValidConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  name: "test-server"
  version: "1.0.0"
  log_file: "test.log"

browser:
  debugger_url: "ws://localhost:9222"
  auto_start: true
  headless: true
  default_navigation_timeout: "20s"
  default_attach_timeout: "5s"
  viewport_width: 1280
  viewport_height: 720

mangle:
  enable: true
  schema_path: "test-schema.mg"
  fact_buffer_limit: 5000

docker:
  enabled: true
  containers:
    - backend
    - frontend
  log_window: "60s"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Verify loaded values
	if cfg.Server.Name != "test-server" {
		t.Errorf("expected server name 'test-server', got %q", cfg.Server.Name)
	}
	if cfg.Server.Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", cfg.Server.Version)
	}
	if cfg.Browser.DebuggerURL != "ws://localhost:9222" {
		t.Errorf("expected debugger URL 'ws://localhost:9222', got %q", cfg.Browser.DebuggerURL)
	}
	if cfg.Browser.ViewportWidth != 1280 {
		t.Errorf("expected viewport width 1280, got %d", cfg.Browser.ViewportWidth)
	}
	if cfg.Mangle.FactBufferLimit != 5000 {
		t.Errorf("expected fact buffer limit 5000, got %d", cfg.Mangle.FactBufferLimit)
	}
	if !cfg.Docker.Enabled {
		t.Error("expected Docker.Enabled to be true")
	}
	if len(cfg.Docker.Containers) != 2 {
		t.Errorf("expected 2 containers, got %d", len(cfg.Docker.Containers))
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Invalid YAML content
	if err := os.WriteFile(configPath, []byte("invalid: yaml: content:"), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty server name",
			cfg:     Config{Server: ServerConfig{Name: ""}},
			wantErr: true,
			errMsg:  "server.name is required",
		},
		{
			name: "auto_start without debugger_url or launch",
			cfg: Config{
				Server:  ServerConfig{Name: "test"},
				Browser: BrowserConfig{AutoStart: true},
			},
			wantErr: true,
			errMsg:  "browser.debugger_url or browser.launch must be provided",
		},
		{
			name: "auto_start with debugger_url",
			cfg: Config{
				Server:  ServerConfig{Name: "test"},
				Browser: BrowserConfig{AutoStart: true, DebuggerURL: "ws://localhost:9222"},
			},
			wantErr: false,
		},
		{
			name: "auto_start with launch",
			cfg: Config{
				Server:  ServerConfig{Name: "test"},
				Browser: BrowserConfig{AutoStart: true, Launch: []string{"chrome"}},
			},
			wantErr: false,
		},
		{
			name: "auto_start false without debugger_url",
			cfg: Config{
				Server:  ServerConfig{Name: "test"},
				Browser: BrowserConfig{AutoStart: false},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				} else if err.Error() != tt.errMsg {
					t.Errorf("expected error %q, got %q", tt.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestNavigationTimeout(t *testing.T) {
	tests := []struct {
		name     string
		timeout  string
		expected time.Duration
	}{
		{"empty string", "", 15 * time.Second},
		{"valid duration", "20s", 20 * time.Second},
		{"invalid duration", "invalid", 15 * time.Second},
		{"milliseconds", "500ms", 500 * time.Millisecond},
		{"minutes", "2m", 2 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := BrowserConfig{DefaultNavigationTimeout: tt.timeout}
			result := cfg.NavigationTimeout()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestAttachTimeout(t *testing.T) {
	tests := []struct {
		name     string
		timeout  string
		expected time.Duration
	}{
		{"empty string", "", 10 * time.Second},
		{"valid duration", "30s", 30 * time.Second},
		{"invalid duration", "not-a-duration", 10 * time.Second},
		{"milliseconds", "100ms", 100 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := BrowserConfig{DefaultAttachTimeout: tt.timeout}
			result := cfg.AttachTimeout()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestIsHeadless(t *testing.T) {
	t.Run("nil headless defaults to true", func(t *testing.T) {
		cfg := BrowserConfig{Headless: nil}
		if !cfg.IsHeadless() {
			t.Error("expected true when Headless is nil")
		}
	})

	t.Run("explicit true", func(t *testing.T) {
		val := true
		cfg := BrowserConfig{Headless: &val}
		if !cfg.IsHeadless() {
			t.Error("expected true when Headless is true")
		}
	})

	t.Run("explicit false", func(t *testing.T) {
		val := false
		cfg := BrowserConfig{Headless: &val}
		if cfg.IsHeadless() {
			t.Error("expected false when Headless is false")
		}
	})
}

func TestGetViewportWidth(t *testing.T) {
	tests := []struct {
		name     string
		width    int
		expected int
	}{
		{"zero defaults to 1920", 0, 1920},
		{"negative defaults to 1920", -100, 1920},
		{"custom width", 1280, 1280},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := BrowserConfig{ViewportWidth: tt.width}
			result := cfg.GetViewportWidth()
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestGetViewportHeight(t *testing.T) {
	tests := []struct {
		name     string
		height   int
		expected int
	}{
		{"zero defaults to 1080", 0, 1080},
		{"negative defaults to 1080", -50, 1080},
		{"custom height", 720, 720},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := BrowserConfig{ViewportHeight: tt.height}
			result := cfg.GetViewportHeight()
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestGetLogWindow(t *testing.T) {
	tests := []struct {
		name     string
		window   string
		expected time.Duration
	}{
		{"empty string", "", 30 * time.Second},
		{"valid duration", "60s", 60 * time.Second},
		{"invalid duration", "bad", 30 * time.Second},
		{"minutes", "5m", 5 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DockerConfig{LogWindow: tt.window}
			result := cfg.GetLogWindow()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
