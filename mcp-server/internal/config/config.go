package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	// Trust permits an auto-discovered workspace config to request browser
	// attachment, process launch, or paths outside the workspace.
	Trust bool
}

// Config captures all tunable settings for the BrowserNERD MCP server.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Browser  BrowserConfig  `yaml:"browser"`
	MCP      MCPConfig      `yaml:"mcp"`
	Mangle   MangleConfig   `yaml:"mangle"`
	Specs    SpecsConfig    `yaml:"specs"`
	Docker   DockerConfig   `yaml:"docker"`
	Recorder RecorderConfig `yaml:"recorder"`
	Security SecurityConfig `yaml:"security"`
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
	// MultiTabDefault opens new sessions as tabs in a shared browser context by default.
	MultiTabDefault *bool `yaml:"multi_tab_default"`
	// MaxTabs bounds all tracked tabs across browser instances.
	MaxTabs int `yaml:"max_tabs"`
	// MaxBrowsers bounds concurrently managed Chrome instances.
	MaxBrowsers int `yaml:"max_browsers"`
	// IdleTabTimeout optionally reaps inactive tabs (zero disables reaping).
	IdleTabTimeout string `yaml:"idle_tab_timeout"`
	// RepoTrace controls bounded repo/code tracing from current browser context.
	RepoTrace RepoTraceConfig `yaml:"repo_trace"`
}

// RepoTraceConfig configures on-demand repo/code tracing without a prebuilt index.
type RepoTraceConfig struct {
	// Enable bounded repo tracing defaults for current browser context audits.
	Enabled bool `yaml:"enabled"`
	// Optional explicit repo/workspace root to trace. When empty, BrowserNERD will discover
	// a .browsernerd workspace or fall back to the current working directory.
	RootDir string `yaml:"root_dir"`
	// Optional roots under RootDir to scan. Defaults to the repo root when empty.
	SearchRoots []string `yaml:"search_roots"`
	// Directory names to skip while walking the repo tree.
	IgnoreDirs []string `yaml:"ignore_dirs"`
	// Maximum number of files to inspect during one trace run.
	MaxFiles int `yaml:"max_files"`
	// Maximum file size (bytes) eligible for scanning.
	MaxFileBytes int64 `yaml:"max_file_bytes"`
	// Maximum number of browser-derived seed hints to retain.
	MaxSeedHints int `yaml:"max_seed_hints"`
	// Maximum number of navigation link hints to retain for non-mutating audit discovery.
	MaxNavigationHints int `yaml:"max_navigation_hints"`
	// Maximum number of control hints to retain for non-mutating audit discovery.
	MaxControlHints int `yaml:"max_control_hints"`
	// Maximum number of deterministic audit plan steps to emit.
	MaxPlanSteps int `yaml:"max_plan_steps"`
	// Maximum number of frontend request-site matches to return.
	MaxFrontendMatches int `yaml:"max_frontend_matches"`
	// Maximum number of backend route/auth/payload matches to return.
	MaxBackendMatches int `yaml:"max_backend_matches"`
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
	// When true (default), agents see 11 tools: browser/session lifecycle,
	// observe/act/reason/audit/mangle, spec delivery/conformance, and tests.
	// When false, all individual tools are also registered (48 total).
	ProgressiveOnly *bool `yaml:"progressive_only"`
}

// MangleConfig controls the embedded deductive engine.
type MangleConfig struct {
	Enable            bool   `yaml:"enable"`
	SchemaPath        string `yaml:"schema_path"`
	DisableBuiltin    bool   `yaml:"disable_builtin_rules"`
	FactBufferLimit   int    `yaml:"fact_buffer_limit"`
	MaxRuleBytes      int    `yaml:"max_rule_bytes"`
	MaxQueryBytes     int    `yaml:"max_query_bytes"`
	MaxRuleClauses    int    `yaml:"max_rule_clauses"`
	MaxPremises       int    `yaml:"max_premises"`
	MaxRules          int    `yaml:"max_rules"`
	MaxCreatedFacts   int    `yaml:"max_created_facts"`
	MaxQueryResults   int    `yaml:"max_query_results"`
	EvaluationTimeout string `yaml:"evaluation_timeout"`
}

// SpecsConfig controls bounded ingestion of project documentation corpora.
// Generic Markdown documents are deliverable context; documents containing
// BrowserNERD invariant directives are also executable conformance specs.
type SpecsConfig struct {
	Enabled         *bool              `yaml:"enabled"`
	Sources         []SpecSourceConfig `yaml:"sources"`
	MaxFiles        int                `yaml:"max_files"`
	MaxFileBytes    int64              `yaml:"max_file_bytes"`
	MaxResults      int                `yaml:"max_results"`
	MaxExcerptBytes int                `yaml:"max_excerpt_bytes"`
}

// SpecSourceConfig names one reusable corpus. Roots are scanned recursively;
// indexes are parsed first for Markdown links and used as an ordering hint.
type SpecSourceConfig struct {
	Name    string   `yaml:"name"`
	Roots   []string `yaml:"roots"`
	Indexes []string `yaml:"indexes"`
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

// RecorderConfig controls optional flight-recorder output for raw MCP diagnostics.
type RecorderConfig struct {
	// Enable recorder logging and export support.
	Enabled bool `yaml:"enabled"`
	// Directory for JSONL trace files.
	TraceDir string `yaml:"trace_dir"`
	// Number of rotated trace files to keep.
	MaxRotatedFiles int `yaml:"max_rotated_files"`
}

// SecurityConfig controls credential persistence and model-directed file writes.
type SecurityConfig struct {
	// RedactSensitiveData controls credential redaction (default: true).
	RedactSensitiveData *bool `yaml:"redact_sensitive_data"`
	// AllowUnsafeJavascript enables arbitrary page-context JavaScript.
	// It is disabled by default because arbitrary scripts can read credentials.
	AllowUnsafeJavascript *bool `yaml:"allow_unsafe_javascript"`
	// ExtraSensitiveKeys extends the built-in password, token, cookie, and API-key set.
	ExtraSensitiveKeys []string `yaml:"extra_sensitive_keys"`
	// WritableRoots confines screenshots and evidence exports to these directories.
	WritableRoots []string `yaml:"writable_roots"`
	// BaseDir is resolved at runtime and is never loaded from YAML.
	BaseDir string `yaml:"-"`
}

// DefaultConfig provides reasonable defaults for local development.
func DefaultConfig() Config {
	dataRoot := defaultDataRoot()
	redactSensitiveData := true
	multiTabDefault := true
	specsEnabled := true
	return Config{
		Server: ServerConfig{
			Name:    "browsernerd-mcp",
			Version: "1.1.0",
			LogFile: filepath.Join(dataRoot, "browsernerd-mcp.log"),
		},
		Browser: BrowserConfig{
			AutoStart:                true,
			DefaultNavigationTimeout: "15s",
			DefaultAttachTimeout:     "10s",
			SessionStore:             filepath.Join(dataRoot, "sessions.json"),
			EnableDOMIngestion:       true,
			EnableHeaderIngestion:    true,
			EventLoggingLevel:        "normal",
			EventThrottleMs:          0,
			ViewportWidth:            1920,
			ViewportHeight:           1080,
			MultiTabDefault:          &multiTabDefault,
			MaxTabs:                  32,
			MaxBrowsers:              4,
			IdleTabTimeout:           "0s",
			RepoTrace: RepoTraceConfig{
				Enabled:            true,
				SearchRoots:        []string{"."},
				IgnoreDirs:         []string{".git", ".browsernerd", "node_modules", ".next", "dist", "build", "coverage", "vendor", "tmp", "temp", "bin", "obj", ".turbo", ".cache"},
				MaxFiles:           4000,
				MaxFileBytes:       1 << 20,
				MaxSeedHints:       24,
				MaxNavigationHints: 16,
				MaxControlHints:    24,
				MaxPlanSteps:       16,
				MaxFrontendMatches: 12,
				MaxBackendMatches:  12,
			},
		},
		MCP: MCPConfig{
			SSEPort: 0,
		},
		Mangle: MangleConfig{
			Enable:            true,
			SchemaPath:        "schemas/browser.mg",
			FactBufferLimit:   2048,
			MaxRuleBytes:      64 << 10,
			MaxQueryBytes:     32 << 10,
			MaxRuleClauses:    16,
			MaxPremises:       64,
			MaxRules:          128,
			MaxCreatedFacts:   10000,
			MaxQueryResults:   1000,
			EvaluationTimeout: "2s",
		},
		Specs: SpecsConfig{
			Enabled:         &specsEnabled,
			MaxFiles:        2000,
			MaxFileBytes:    2 << 20,
			MaxResults:      12,
			MaxExcerptBytes: 1200,
		},
		Docker: DockerConfig{
			Enabled:    false,
			Containers: []string{"backend", "frontend"},
			LogWindow:  "30s",
			Host:       "",
		},
		Recorder: RecorderConfig{
			Enabled:         true,
			TraceDir:        filepath.Join(dataRoot, "traces"),
			MaxRotatedFiles: 3,
		},
		Security: SecurityConfig{
			RedactSensitiveData: &redactSensitiveData,
			WritableRoots: []string{
				filepath.Join(dataRoot, "screenshots"),
				filepath.Join(dataRoot, "traces"),
			},
			BaseDir: dataRoot,
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
			// The built-in default schema path ("schemas/browser.mg") is relative to
			// the server's own directory, not to a workspace root. InitWorkspace
			// provisions the schema inside .browsernerd/, so anchor the default there
			// before any layer is applied -- otherwise it resolves to
			// <wsDir>/schemas/browser.mg, which no workspace ever creates, and the
			// mangle engine fails to initialize. A schema_path set by either layer
			// still overrides this.
			wsSchema := filepath.Join(wsDir, WorkspaceDirName, "schemas", "browser.mg")
			if _, statErr := os.Stat(wsSchema); statErr == nil {
				cfg.Mangle.SchemaPath = wsSchema
			}

			wsConfigPath := filepath.Join(wsDir, WorkspaceDirName, WorkspaceConfigFile)
			raw, err := os.ReadFile(wsConfigPath)
			if err != nil {
				return cfg, "", fmt.Errorf("reading workspace config %s: %w", wsConfigPath, err)
			}
			if err := validateWorkspaceAuthority(raw, wsDir, opts.Trust); err != nil {
				return cfg, "", fmt.Errorf("workspace config %s is not trusted: %w", wsConfigPath, err)
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
		// Relative paths in an explicit --config are relative to THAT FILE. The
		// process working directory belongs to whichever MCP client launched the
		// server and is not something the config author can predict.
		explicitDir, absErr := filepath.Abs(filepath.Dir(explicitConfig))
		if absErr != nil {
			return cfg, wsDir, fmt.Errorf("resolving directory of config %s: %w", explicitConfig, absErr)
		}
		cfg = resolveRelativePaths(cfg, explicitDir)
	}

	baseDir, err := configBaseDir(wsDir)
	if err != nil {
		return cfg, wsDir, err
	}
	cfg.Security.BaseDir = baseDir
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
#
# specs:
#   enabled: true
#   sources:
#     - name: product-specs
#       roots: ["docs/specs"]
#       indexes: ["docs/indexes/specs.md"]
#       include: ["*.md", "**/*.md"]
#       exclude: ["**/archive/**"]
#   max_files: 2000
#   max_file_bytes: 2097152
#   max_results: 12
#   max_excerpt_bytes: 1200

# browser:
#   headless: false
#   viewport_width: 1280
#   viewport_height: 720
#   repo_trace:
#     root_dir: "."
#     search_roots: ["frontend", "backend"]
#     max_files: 2500
#     max_navigation_hints: 16
#     max_control_hints: 24
#     max_plan_steps: 16

# recorder:
#   enabled: true
#   trace_dir: ".browsernerd/data/traces"
#   max_rotated_files: 5
#
# security:
#   redact_sensitive_data: true
#   allow_unsafe_javascript: false
#   writable_roots: [".browsernerd/data/screenshots", ".browsernerd/data/traces"]
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
	cfg = resolveRelativePaths(cfg, wsDir)
	cfg.Security.BaseDir = wsDir
	return cfg
}

// resolveRelativePaths anchors every relative path field in cfg to baseDir.
//
// Absolute paths are left untouched, which is what makes this safe to apply once
// per configuration layer: the workspace layer anchors its own relative paths to
// the workspace root, and a later --config layer anchors ONLY the fields it
// re-introduced as relative. Anything an earlier layer already made absolute
// survives unchanged.
//
// Anchoring matters because an MCP server is launched by a client with an
// arbitrary working directory. Leaving paths relative silently resolves them
// against that cwd, which is how a correct config ends up pointing at files that
// do not exist.
func resolveRelativePaths(cfg Config, baseDir string) Config {
	resolve := func(p string) string {
		if p == "" || filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(baseDir, p)
	}

	cfg.Server.LogFile = resolve(cfg.Server.LogFile)
	cfg.Browser.SessionStore = resolve(cfg.Browser.SessionStore)
	cfg.Browser.RepoTrace.RootDir = resolve(cfg.Browser.RepoTrace.RootDir)
	cfg.Mangle.SchemaPath = resolve(cfg.Mangle.SchemaPath)
	cfg.Recorder.TraceDir = resolve(cfg.Recorder.TraceDir)
	for sourceIdx := range cfg.Specs.Sources {
		for rootIdx, root := range cfg.Specs.Sources[sourceIdx].Roots {
			cfg.Specs.Sources[sourceIdx].Roots[rootIdx] = resolve(root)
		}
		for indexIdx, index := range cfg.Specs.Sources[sourceIdx].Indexes {
			cfg.Specs.Sources[sourceIdx].Indexes[indexIdx] = resolve(index)
		}
	}
	for i, root := range cfg.Security.WritableRoots {
		cfg.Security.WritableRoots[i] = resolve(root)
	}
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
	if c.Browser.RepoTrace.Enabled {
		if c.Browser.RepoTrace.MaxFiles <= 0 {
			return errors.New("browser.repo_trace.max_files must be > 0 when browser.repo_trace.enabled is true")
		}
		if c.Browser.RepoTrace.MaxFileBytes <= 0 {
			return errors.New("browser.repo_trace.max_file_bytes must be > 0 when browser.repo_trace.enabled is true")
		}
		if c.Browser.RepoTrace.MaxSeedHints <= 0 {
			return errors.New("browser.repo_trace.max_seed_hints must be > 0 when browser.repo_trace.enabled is true")
		}
		if c.Browser.RepoTrace.MaxNavigationHints <= 0 {
			return errors.New("browser.repo_trace.max_navigation_hints must be > 0 when browser.repo_trace.enabled is true")
		}
		if c.Browser.RepoTrace.MaxControlHints <= 0 {
			return errors.New("browser.repo_trace.max_control_hints must be > 0 when browser.repo_trace.enabled is true")
		}
		if c.Browser.RepoTrace.MaxPlanSteps <= 0 {
			return errors.New("browser.repo_trace.max_plan_steps must be > 0 when browser.repo_trace.enabled is true")
		}
		if c.Browser.RepoTrace.MaxFrontendMatches <= 0 {
			return errors.New("browser.repo_trace.max_frontend_matches must be > 0 when browser.repo_trace.enabled is true")
		}
		if c.Browser.RepoTrace.MaxBackendMatches <= 0 {
			return errors.New("browser.repo_trace.max_backend_matches must be > 0 when browser.repo_trace.enabled is true")
		}
	}
	if c.Recorder.Enabled && c.Recorder.MaxRotatedFiles <= 0 {
		return errors.New("recorder.max_rotated_files must be > 0 when recorder.enabled is true")
	}
	if c.Specs.IsEnabled() {
		if c.Specs.GetMaxFiles() <= 0 || c.Specs.GetMaxFileBytes() <= 0 {
			return errors.New("specs max_files and max_file_bytes must be > 0")
		}
		if c.Specs.GetMaxResults() <= 0 || c.Specs.GetMaxExcerptBytes() <= 0 {
			return errors.New("specs max_results and max_excerpt_bytes must be > 0")
		}
		for idx, source := range c.Specs.Sources {
			if strings.TrimSpace(source.Name) == "" {
				return fmt.Errorf("specs.sources[%d].name is required", idx)
			}
			if len(source.Roots) == 0 {
				return fmt.Errorf("specs.sources[%d].roots must not be empty", idx)
			}
		}
	}
	return nil
}

// IsRedactionEnabled returns whether persisted diagnostics must be sanitized.
func (s SecurityConfig) IsRedactionEnabled() bool {
	if s.RedactSensitiveData == nil {
		return true
	}
	return *s.RedactSensitiveData
}

// AllowsUnsafeJavaScript reports whether the operator explicitly enabled the
// arbitrary evaluate-js escape hatch. Structured browser tools remain enabled.
func (s SecurityConfig) AllowsUnsafeJavaScript() bool {
	return s.AllowUnsafeJavascript != nil && *s.AllowUnsafeJavascript
}

type workspaceAuthorityDocument struct {
	Server struct {
		LogFile *string `yaml:"log_file"`
	} `yaml:"server"`
	Browser struct {
		DebuggerURL  *string   `yaml:"debugger_url"`
		Launch       *[]string `yaml:"launch"`
		AutoStart    *bool     `yaml:"auto_start"`
		SessionStore *string   `yaml:"session_store"`
		RepoTrace    struct {
			RootDir *string `yaml:"root_dir"`
		} `yaml:"repo_trace"`
	} `yaml:"browser"`
	Mangle struct {
		SchemaPath *string `yaml:"schema_path"`
	} `yaml:"mangle"`
	Recorder struct {
		TraceDir *string `yaml:"trace_dir"`
	} `yaml:"recorder"`
	Security struct {
		WritableRoots         *[]string `yaml:"writable_roots"`
		AllowUnsafeJavascript *bool     `yaml:"allow_unsafe_javascript"`
	} `yaml:"security"`
	Specs struct {
		Sources []struct {
			Roots   []string `yaml:"roots"`
			Indexes []string `yaml:"indexes"`
		} `yaml:"sources"`
	} `yaml:"specs"`
}

func validateWorkspaceAuthority(raw []byte, wsDir string, trusted bool) error {
	if trusted {
		return nil
	}
	var doc workspaceAuthorityDocument
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if doc.Browser.Launch != nil && len(*doc.Browser.Launch) > 0 {
		return errors.New("browser.launch requires --trust-workspace-config")
	}
	if doc.Browser.AutoStart != nil && *doc.Browser.AutoStart {
		return errors.New("browser.auto_start=true requires --trust-workspace-config")
	}
	if doc.Browser.DebuggerURL != nil && strings.TrimSpace(*doc.Browser.DebuggerURL) != "" {
		return errors.New("browser.debugger_url requires --trust-workspace-config")
	}
	if doc.Security.AllowUnsafeJavascript != nil && *doc.Security.AllowUnsafeJavascript {
		return errors.New("security.allow_unsafe_javascript=true requires --trust-workspace-config")
	}
	paths := []struct {
		name  string
		value *string
	}{
		{"server.log_file", doc.Server.LogFile},
		{"browser.session_store", doc.Browser.SessionStore},
		{"browser.repo_trace.root_dir", doc.Browser.RepoTrace.RootDir},
		{"mangle.schema_path", doc.Mangle.SchemaPath},
		{"recorder.trace_dir", doc.Recorder.TraceDir},
	}
	for _, item := range paths {
		if item.value != nil && !workspacePathAllowed(wsDir, *item.value) {
			return fmt.Errorf("%s must remain inside the workspace", item.name)
		}
	}
	if doc.Security.WritableRoots != nil {
		for _, root := range *doc.Security.WritableRoots {
			if !workspacePathAllowed(wsDir, root) {
				return errors.New("security.writable_roots must remain inside the workspace")
			}
		}
	}
	for _, source := range doc.Specs.Sources {
		for _, root := range source.Roots {
			if !workspacePathAllowed(wsDir, root) {
				return errors.New("specs.sources.roots must remain inside the workspace")
			}
		}
		for _, index := range source.Indexes {
			if !workspacePathAllowed(wsDir, index) {
				return errors.New("specs.sources.indexes must remain inside the workspace")
			}
		}
	}
	return nil
}

func workspacePathAllowed(wsDir, raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	if filepath.IsAbs(raw) {
		return false
	}
	root, err := filepath.Abs(wsDir)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(filepath.Join(root, raw))
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func configBaseDir(wsDir string) (string, error) {
	if wsDir != "" {
		return filepath.Abs(wsDir)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting config base directory: %w", err)
	}
	return filepath.Abs(cwd)
}

func defaultDataRoot() string {
	if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
		return filepath.Join(cacheDir, "BrowserNERD")
	}
	return filepath.Join(os.TempDir(), "BrowserNERD")
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

// IsMultiTabDefault reports whether create-session opens a shared-context tab.
func (b BrowserConfig) IsMultiTabDefault() bool {
	if b.MultiTabDefault == nil {
		return true
	}
	return *b.MultiTabDefault
}

func (b BrowserConfig) GetMaxTabs() int {
	if b.MaxTabs <= 0 {
		return 32
	}
	return b.MaxTabs
}

func (b BrowserConfig) GetMaxBrowsers() int {
	if b.MaxBrowsers <= 0 {
		return 4
	}
	return b.MaxBrowsers
}

func (b BrowserConfig) GetIdleTabTimeout() time.Duration {
	if b.IdleTabTimeout == "" {
		return 0
	}
	duration, err := time.ParseDuration(b.IdleTabTimeout)
	if err != nil || duration < 0 {
		return 0
	}
	return duration
}

func (s SpecsConfig) IsEnabled() bool {
	if s.Enabled == nil {
		return true
	}
	return *s.Enabled
}

func (s SpecsConfig) GetMaxFiles() int {
	if s.MaxFiles <= 0 {
		return 2000
	}
	return s.MaxFiles
}

func (s SpecsConfig) GetMaxFileBytes() int64 {
	if s.MaxFileBytes <= 0 {
		return 2 << 20
	}
	return s.MaxFileBytes
}

func (s SpecsConfig) GetMaxResults() int {
	if s.MaxResults <= 0 {
		return 12
	}
	return s.MaxResults
}

func (s SpecsConfig) GetMaxExcerptBytes() int {
	if s.MaxExcerptBytes <= 0 {
		return 1200
	}
	return s.MaxExcerptBytes
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

func (m MangleConfig) GetMaxRuleBytes() int {
	if m.MaxRuleBytes <= 0 {
		return 64 << 10
	}
	return m.MaxRuleBytes
}

func (m MangleConfig) GetMaxQueryBytes() int {
	if m.MaxQueryBytes <= 0 {
		return 32 << 10
	}
	return m.MaxQueryBytes
}

func (m MangleConfig) GetMaxRuleClauses() int {
	if m.MaxRuleClauses <= 0 {
		return 16
	}
	return m.MaxRuleClauses
}

func (m MangleConfig) GetMaxPremises() int {
	if m.MaxPremises <= 0 {
		return 64
	}
	return m.MaxPremises
}

func (m MangleConfig) GetMaxRules() int {
	if m.MaxRules <= 0 {
		return 128
	}
	return m.MaxRules
}

func (m MangleConfig) GetMaxCreatedFacts() int {
	if m.MaxCreatedFacts <= 0 {
		return 10000
	}
	return m.MaxCreatedFacts
}

func (m MangleConfig) GetMaxQueryResults() int {
	if m.MaxQueryResults <= 0 {
		return 1000
	}
	return m.MaxQueryResults
}

func (m MangleConfig) GetEvaluationTimeout() time.Duration {
	if m.EvaluationTimeout == "" {
		return 2 * time.Second
	}
	duration, err := time.ParseDuration(m.EvaluationTimeout)
	if err != nil || duration <= 0 {
		return 2 * time.Second
	}
	return duration
}
