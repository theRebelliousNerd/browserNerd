package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// WorkspaceDirName is the directory name for project-level BrowserNERD config.
	WorkspaceDirName = ".browsernerd"
	// WorkspaceConfigFile is the config file name inside the workspace directory.
	WorkspaceConfigFile = "config.yaml"
	// MaxSearchDepth limits how many parent directories to walk when discovering a workspace.
	MaxSearchDepth = 10
)

// WorkspaceOptions controls workspace discovery behavior.
type WorkspaceOptions struct {
	// Disable skips workspace discovery entirely (--no-workspace flag).
	Disable bool
	// ExplicitDir uses this directory as workspace root instead of walking up (--workspace-dir flag).
	ExplicitDir string
}

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
	// Containers to monitor for error correlation (e.g., ["backend", "frontend"]).
	Containers []string `yaml:"containers"`
	// How far back to query logs when correlating errors (e.g., "30s"). Default: 30s.
	LogWindow string `yaml:"log_window"`
	// Docker host (default: uses DOCKER_HOST env or unix socket).
	Host string `yaml:"host"`
}

type MCPConfig struct {
	// When set, starts an SSE server on this port instead of stdio-only.
	SSEPort int `yaml:"sse_port"`
	// ProgressiveOnly controls whether only progressive disclosure tools are registered.
	// When true (default), agents see 6 tools: launch-browser, shutdown-browser,
	// browser-observe, browser-act, browser-reason, browser-mangle.
	// When false, all individual tools are also registered (~37 total).
	ProgressiveOnly *bool `yaml:"progressive_only"`
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
			Version: "0.0.6",
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
			Containers: []string{"backend", "frontend"},
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

// DiscoverWorkspace walks up from startDir looking for a .browsernerd/config.yaml file.
// Returns the workspace root directory (parent of .browsernerd/) or empty string if not found.
func DiscoverWorkspace(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolving start directory: %w", err)
	}

	for i := 0; i < MaxSearchDepth; i++ {
		candidate := filepath.Join(dir, WorkspaceDirName, WorkspaceConfigFile)
		if _, err := os.Stat(candidate); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return "", nil
}

// LoadWithWorkspace implements multi-layer config merge:
//
//	DefaultConfig() <- .browsernerd/config.yaml <- explicit --config <- CLI flags
//
// Returns the merged config and the workspace directory (empty if none found).
func LoadWithWorkspace(explicitConfig string, opts WorkspaceOptions) (Config, string, error) {
	cfg := DefaultConfig()
	wsDir := ""

	// Layer 1: Workspace config (if not disabled)
	if !opts.Disable {
		var err error
		if opts.ExplicitDir != "" {
			// Verify the explicit workspace dir has a config
			candidate := filepath.Join(opts.ExplicitDir, WorkspaceDirName, WorkspaceConfigFile)
			if _, statErr := os.Stat(candidate); statErr == nil {
				wsDir = opts.ExplicitDir
			}
		} else {
			cwd, cwdErr := os.Getwd()
			if cwdErr != nil {
				return cfg, "", fmt.Errorf("getting working directory: %w", cwdErr)
			}
			wsDir, err = DiscoverWorkspace(cwd)
			if err != nil {
				return cfg, "", fmt.Errorf("discovering workspace: %w", err)
			}
		}

		if wsDir != "" {
			wsConfigPath := filepath.Join(wsDir, WorkspaceDirName, WorkspaceConfigFile)
			raw, err := os.ReadFile(wsConfigPath)
			if err != nil {
				return cfg, "", fmt.Errorf("reading workspace config %s: %w", wsConfigPath, err)
			}
			if err := yaml.Unmarshal(raw, &cfg); err != nil {
				return cfg, "", fmt.Errorf("parsing workspace config %s: %w", wsConfigPath, err)
			}
			cfg = resolveWorkspacePaths(cfg, wsDir)
		}
	}

	// Layer 2: Explicit config file (--config flag)
	if explicitConfig != "" {
		raw, err := os.ReadFile(explicitConfig)
		if err != nil {
			return cfg, wsDir, fmt.Errorf("reading explicit config %s: %w", explicitConfig, err)
		}
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return cfg, wsDir, fmt.Errorf("parsing explicit config %s: %w", explicitConfig, err)
		}
	}

	return cfg, wsDir, cfg.Validate()
}

// InitWorkspace creates a .browsernerd/ directory with template files at root.
func InitWorkspace(root string) error {
	wsDir := filepath.Join(root, WorkspaceDirName)

	// Check if already exists
	if _, err := os.Stat(wsDir); err == nil {
		return fmt.Errorf("workspace directory already exists: %s", wsDir)
	}

	// Create directory structure
	dirs := []string{
		wsDir,
		filepath.Join(wsDir, "schemas"),
		filepath.Join(wsDir, "data"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}

	// Write template config
	templateConfig := `# BrowserNERD project-level configuration
# Values here override defaults but are overridden by --config and CLI flags.
# See: https://github.com/anthropics/browsernerd

# docker:
#   enabled: true
#   containers:
#     - my-app-backend
#     - my-app-frontend
#   log_window: "30s"

# mangle:
#   schema_path: ".browsernerd/schemas/project.mg"

# browser:
#   headless: false
#   viewport_width: 1280
#   viewport_height: 720
`
	configPath := filepath.Join(wsDir, WorkspaceConfigFile)
	if err := os.WriteFile(configPath, []byte(templateConfig), 0644); err != nil {
		return fmt.Errorf("writing config template: %w", err)
	}

	// Write .gitignore for data directory
	gitignoreContent := "# Runtime data (logs, sessions) - do not version control\ndata/\n"
	gitignorePath := filepath.Join(wsDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644); err != nil {
		return fmt.Errorf("writing .gitignore: %w", err)
	}

	return nil
}

// resolveWorkspacePaths resolves relative paths in the config against the workspace directory.
func resolveWorkspacePaths(cfg Config, wsDir string) Config {
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(wsDir, p)
	}

	cfg.Server.LogFile = resolve(cfg.Server.LogFile)
	cfg.Browser.SessionStore = resolve(cfg.Browser.SessionStore)
	cfg.Mangle.SchemaPath = resolve(cfg.Mangle.SchemaPath)
	return cfg
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

// IsProgressiveOnly returns whether only progressive disclosure tools should be registered (default: true).
func (m MCPConfig) IsProgressiveOnly() bool {
	if m.ProgressiveOnly == nil {
		return true // default to progressive-only mode
	}
	return *m.ProgressiveOnly
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
