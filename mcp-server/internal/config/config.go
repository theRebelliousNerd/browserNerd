package config

import (
	"errors"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config captures all tunable settings for the BrowserNERD MCP server.
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Browser BrowserConfig `yaml:"browser"`
	MCP     MCPConfig     `yaml:"mcp"`
	Mangle  MangleConfig  `yaml:"mangle"`
	Docker  DockerConfig  `yaml:"docker"`
}

type ServerConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	LogFile string `yaml:"log_file"`
}

// BrowserConfig configures how we attach to or launch Chrome for Rod.
type BrowserConfig struct {
	// Control endpoint for Rod (e.g., ws://localhost:9222). Required when launch is empty.
	DebuggerURL string `yaml:"debugger_url"`
	// Optional launch command to start Chrome in detached mode (e.g., ["chrome", "--remote-debugging-port=9222"]).
	Launch []string `yaml:"launch"`
	// AutoStart controls whether the MCP server launches/attaches to Chrome at startup.
	AutoStart bool `yaml:"auto_start"`
	// Headless controls whether Chrome runs in headless mode (default: true).
	Headless *bool `yaml:"headless"`
	// Default navigation timeout (e.g., "15s").
	DefaultNavigationTimeout string `yaml:"default_navigation_timeout"`
	// Default timeout when attaching to an existing target (e.g., "10s").
	DefaultAttachTimeout string `yaml:"default_attach_timeout"`
	// Optional path to persist session metadata between server restarts.
	SessionStore string `yaml:"session_store"`
	// Enable DOM ingestion via JS snapshot (sampled to control cost).
	EnableDOMIngestion bool `yaml:"enable_dom_ingestion"`
	// Enable request/response header facts.
	EnableHeaderIngestion bool `yaml:"enable_header_ingestion"`
	// Logging level for event ingestion: minimal | normal | verbose.
	EventLoggingLevel string `yaml:"event_logging_level"`
	// Optional throttle (ms) to sample high-frequency events (console/network/DOM).
	EventThrottleMs int `yaml:"event_throttle_ms"`
	// Viewport width for new sessions (default: 1920).
	ViewportWidth int `yaml:"viewport_width"`
	// Viewport height for new sessions (default: 1080).
	ViewportHeight int `yaml:"viewport_height"`
}

// DockerConfig configures Docker log integration for full-stack error correlation.
type DockerConfig struct {
	// Enable Docker log integration (default: false).
	Enabled bool `yaml:"enabled"`
	// Containers to monitor for error correlation (e.g., ["symbiogen-backend", "symbiogen-worker"]).
	Containers []string `yaml:"containers"`
	// How far back to query logs when correlating errors (e.g., "30s"). Default: 30s.
	LogWindow string `yaml:"log_window"`
	// Docker host (default: uses DOCKER_HOST env or unix socket).
	Host string `yaml:"host"`
}

type MCPConfig struct {
	// When set, starts an SSE server on this port instead of stdio-only.
	SSEPort int `yaml:"sse_port"`
}

// MangleConfig controls the embedded deductive engine.
type MangleConfig struct {
	Enable          bool   `yaml:"enable"`
	SchemaPath      string `yaml:"schema_path"`
	DisableBuiltin  bool   `yaml:"disable_builtin_rules"`
	FactBufferLimit int    `yaml:"fact_buffer_limit"`
}

// DefaultConfig provides reasonable defaults for local development.
func DefaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Name:    "browsernerd-mcp",
			Version: "0.0.3",
			LogFile: "browsernerd-mcp.log",
		},
		Browser: BrowserConfig{
			AutoStart:                true,
			DefaultNavigationTimeout: "15s",
			DefaultAttachTimeout:     "10s",
			SessionStore:             "sessions.json",
			EnableDOMIngestion:       true,
			EnableHeaderIngestion:    true,
			EventLoggingLevel:        "normal",
			EventThrottleMs:          0,
			ViewportWidth:            1920,
			ViewportHeight:           1080,
		},
		MCP: MCPConfig{
			SSEPort: 0,
		},
		Mangle: MangleConfig{
			Enable:          true,
			SchemaPath:      "schemas/browser.mg",
			FactBufferLimit: 2048,
		},
		Docker: DockerConfig{
			Enabled:    false,
			Containers: []string{"symbiogen-backend", "symbiogen-frontend"},
			LogWindow:  "30s",
			Host:       "",
		},
	}
}

// Load reads YAML config from disk and overlays defaults.
func Load(path string) (Config, error) {
	cfg := DefaultConfig()

	if path == "" {
		return cfg, errors.New("config path is required")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}

	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, err
	}

	return cfg, cfg.Validate()
}

// Validate ensures required fields exist so the server can start deterministically.
func (c *Config) Validate() error {
	if c.Server.Name == "" {
		return errors.New("server.name is required")
	}
	if c.Browser.AutoStart {
		if c.Browser.DebuggerURL == "" && len(c.Browser.Launch) == 0 {
			return errors.New("browser.debugger_url or browser.launch must be provided")
		}
	}
	return nil
}

// NavigationTimeout returns the parsed navigation timeout with a sane default.
func (b BrowserConfig) NavigationTimeout() time.Duration {
	if b.DefaultNavigationTimeout == "" {
		return 15 * time.Second
	}
	d, err := time.ParseDuration(b.DefaultNavigationTimeout)
	if err != nil {
		return 15 * time.Second
	}
	return d
}

// AttachTimeout returns the parsed attach timeout with a sane default.
func (b BrowserConfig) AttachTimeout() time.Duration {
	if b.DefaultAttachTimeout == "" {
		return 10 * time.Second
	}
	d, err := time.ParseDuration(b.DefaultAttachTimeout)
	if err != nil {
		return 10 * time.Second
	}
	return d
}

// IsHeadless returns whether Chrome should run in headless mode (default: true).
func (b BrowserConfig) IsHeadless() bool {
	if b.Headless == nil {
		return true // default to headless
	}
	return *b.Headless
}

// GetViewportWidth returns the viewport width with a sane default.
func (b BrowserConfig) GetViewportWidth() int {
	if b.ViewportWidth <= 0 {
		return 1920
	}
	return b.ViewportWidth
}

// GetViewportHeight returns the viewport height with a sane default.
func (b BrowserConfig) GetViewportHeight() int {
	if b.ViewportHeight <= 0 {
		return 1080
	}
	return b.ViewportHeight
}

// GetLogWindow returns the parsed log window duration with a sane default.
func (d DockerConfig) GetLogWindow() time.Duration {
	if d.LogWindow == "" {
		return 30 * time.Second
	}
	dur, err := time.ParseDuration(d.LogWindow)
	if err != nil {
		return 30 * time.Second
	}
	return dur
}
