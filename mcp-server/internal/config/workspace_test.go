package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDiscoverWorkspace_Found(t *testing.T) {
	// Create a temp dir with .browsernerd/config.yaml
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, WorkspaceDirName)
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, WorkspaceConfigFile), []byte("server:\n  name: test\n"), 0644); err != nil {
		t.Fatalf("failed to write workspace config: %v", err)
	}

	result, err := DiscoverWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != tmpDir {
		t.Errorf("expected %q, got %q", tmpDir, result)
	}
}

func TestDiscoverWorkspace_WalkUp(t *testing.T) {
	// Create a temp dir with .browsernerd/config.yaml, then start search 2 levels deep
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, WorkspaceDirName)
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, WorkspaceConfigFile), []byte("server:\n  name: test\n"), 0644); err != nil {
		t.Fatalf("failed to write workspace config: %v", err)
	}

	// Create nested dirs 2 levels down
	nested := filepath.Join(tmpDir, "a", "b")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("failed to create nested dirs: %v", err)
	}

	result, err := DiscoverWorkspace(nested)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != tmpDir {
		t.Errorf("expected %q, got %q", tmpDir, result)
	}
}

func TestDiscoverWorkspace_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	result, err := DiscoverWorkspace(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestDiscoverWorkspace_MaxDepth(t *testing.T) {
	// Create workspace at root, but start search deeper than MaxSearchDepth
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, WorkspaceDirName)
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, WorkspaceConfigFile), []byte("server:\n  name: test\n"), 0644); err != nil {
		t.Fatalf("failed to write workspace config: %v", err)
	}

	// Create a path deeper than MaxSearchDepth
	parts := make([]string, MaxSearchDepth+2)
	parts[0] = tmpDir
	for i := 1; i <= MaxSearchDepth+1; i++ {
		parts[i] = "d"
	}
	deepPath := filepath.Join(parts...)
	if err := os.MkdirAll(deepPath, 0755); err != nil {
		t.Fatalf("failed to create deep path: %v", err)
	}

	result, err := DiscoverWorkspace(deepPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string (beyond max depth), got %q", result)
	}
}

// wsConfigWithAutoStartOff returns a workspace config snippet that disables auto_start
// to avoid validation errors requiring debugger_url/launch.
const wsConfigAutoStartOff = `
browser:
  auto_start: false
`

func TestLoadWithWorkspace_DefaultsOnly(t *testing.T) {
	// Disable workspace, provide no explicit config. Defaults have auto_start: true,
	// which requires debugger_url or launch. Override via a minimal explicit config.
	tmpDir := t.TempDir()
	explicitPath := filepath.Join(tmpDir, "minimal.yaml")
	if err := os.WriteFile(explicitPath, []byte(wsConfigAutoStartOff), 0644); err != nil {
		t.Fatalf("failed to write minimal config: %v", err)
	}

	cfg, wsDir, err := LoadWithWorkspace(explicitPath, WorkspaceOptions{Disable: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wsDir != "" {
		t.Errorf("expected empty workspace dir, got %q", wsDir)
	}
	// Verify defaults are intact (except auto_start which we overrode)
	if cfg.Server.Name != "browsernerd-mcp" {
		t.Errorf("expected default server name, got %q", cfg.Server.Name)
	}
	if cfg.Docker.Enabled {
		t.Error("expected Docker.Enabled to be false by default")
	}
}

func TestLoadWithWorkspace_WorkspaceOverridesDefaults(t *testing.T) {
	// Set up workspace with docker enabled
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, WorkspaceDirName)
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	wsConfig := `
browser:
  auto_start: false

docker:
  enabled: true
  containers:
    - test-api
    - test-web
`
	if err := os.WriteFile(filepath.Join(wsDir, WorkspaceConfigFile), []byte(wsConfig), 0644); err != nil {
		t.Fatalf("failed to write workspace config: %v", err)
	}

	cfg, resultDir, err := LoadWithWorkspace("", WorkspaceOptions{ExplicitDir: tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resultDir != tmpDir {
		t.Errorf("expected workspace dir %q, got %q", tmpDir, resultDir)
	}
	if !cfg.Docker.Enabled {
		t.Error("expected Docker.Enabled to be true from workspace config")
	}
	if len(cfg.Docker.Containers) != 2 || cfg.Docker.Containers[0] != "test-api" {
		t.Errorf("expected containers [test-api, test-web], got %v", cfg.Docker.Containers)
	}
	// Defaults for unset fields should remain
	if cfg.Server.Name != "browsernerd-mcp" {
		t.Errorf("expected default server name, got %q", cfg.Server.Name)
	}
}

func TestLoadWithWorkspace_ExplicitOverridesWorkspace(t *testing.T) {
	// Set up workspace with docker containers
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, WorkspaceDirName)
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	wsConfig := `
browser:
  auto_start: false

docker:
  enabled: true
  containers:
    - ws-backend
`
	if err := os.WriteFile(filepath.Join(wsDir, WorkspaceConfigFile), []byte(wsConfig), 0644); err != nil {
		t.Fatalf("failed to write workspace config: %v", err)
	}

	// Create explicit config that overrides docker containers
	explicitPath := filepath.Join(tmpDir, "explicit.yaml")
	explicitConfig := `
docker:
  containers:
    - explicit-backend
    - explicit-frontend
`
	if err := os.WriteFile(explicitPath, []byte(explicitConfig), 0644); err != nil {
		t.Fatalf("failed to write explicit config: %v", err)
	}

	cfg, _, err := LoadWithWorkspace(explicitPath, WorkspaceOptions{ExplicitDir: tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Explicit config should override workspace containers
	if len(cfg.Docker.Containers) != 2 || cfg.Docker.Containers[0] != "explicit-backend" {
		t.Errorf("expected explicit containers to override workspace, got %v", cfg.Docker.Containers)
	}
}

func TestLoadWithWorkspace_PartialYAML(t *testing.T) {
	// Workspace only sets one field
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, WorkspaceDirName)
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	wsConfig := `
browser:
  auto_start: false
  viewport_width: 800
`
	if err := os.WriteFile(filepath.Join(wsDir, WorkspaceConfigFile), []byte(wsConfig), 0644); err != nil {
		t.Fatalf("failed to write workspace config: %v", err)
	}

	cfg, _, err := LoadWithWorkspace("", WorkspaceOptions{ExplicitDir: tmpDir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Changed field
	if cfg.Browser.ViewportWidth != 800 {
		t.Errorf("expected viewport width 800, got %d", cfg.Browser.ViewportWidth)
	}
	// Unchanged defaults
	if cfg.Browser.ViewportHeight != 1080 {
		t.Errorf("expected default viewport height 1080, got %d", cfg.Browser.ViewportHeight)
	}
	if cfg.Server.Name != "browsernerd-mcp" {
		t.Errorf("expected default server name, got %q", cfg.Server.Name)
	}
}

func TestLoadWithWorkspace_Disabled(t *testing.T) {
	// Create a workspace dir, but disable discovery
	tmpDir := t.TempDir()
	wsDir := filepath.Join(tmpDir, WorkspaceDirName)
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatalf("failed to create workspace dir: %v", err)
	}
	wsConfig := `
docker:
  enabled: true
`
	if err := os.WriteFile(filepath.Join(wsDir, WorkspaceConfigFile), []byte(wsConfig), 0644); err != nil {
		t.Fatalf("failed to write workspace config: %v", err)
	}

	// Need to provide explicit config to disable auto_start for validation
	explicitPath := filepath.Join(tmpDir, "minimal.yaml")
	if err := os.WriteFile(explicitPath, []byte(wsConfigAutoStartOff), 0644); err != nil {
		t.Fatalf("failed to write minimal config: %v", err)
	}

	cfg, resultDir, err := LoadWithWorkspace(explicitPath, WorkspaceOptions{Disable: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resultDir != "" {
		t.Errorf("expected empty workspace dir with Disable, got %q", resultDir)
	}
	// Docker should remain at default (false) since workspace was disabled
	if cfg.Docker.Enabled {
		t.Error("expected Docker.Enabled to be false when workspace disabled")
	}
}

func TestResolveWorkspacePaths_Relative(t *testing.T) {
	// Use a temp dir for a platform-valid path
	tmpDir := t.TempDir()

	cfg := Config{
		Server:  ServerConfig{LogFile: "browsernerd-mcp.log"},
		Browser: BrowserConfig{SessionStore: "sessions.json"},
		Mangle:  MangleConfig{SchemaPath: filepath.Join("schemas", "browser.mg")},
	}

	resolved := resolveWorkspacePaths(cfg, tmpDir)

	expected := filepath.Join(tmpDir, "browsernerd-mcp.log")
	if resolved.Server.LogFile != expected {
		t.Errorf("expected log file %q, got %q", expected, resolved.Server.LogFile)
	}
	expected = filepath.Join(tmpDir, "sessions.json")
	if resolved.Browser.SessionStore != expected {
		t.Errorf("expected session store %q, got %q", expected, resolved.Browser.SessionStore)
	}
	expected = filepath.Join(tmpDir, "schemas", "browser.mg")
	if resolved.Mangle.SchemaPath != expected {
		t.Errorf("expected schema path %q, got %q", expected, resolved.Mangle.SchemaPath)
	}
}

func TestResolveWorkspacePaths_AbsoluteUntouched(t *testing.T) {
	wsDir := t.TempDir()

	// Use platform-appropriate absolute paths
	var absLog, absSession, absSchema string
	if runtime.GOOS == "windows" {
		absLog = `C:\var\log\browsernerd.log`
		absSession = `C:\tmp\sessions.json`
		absSchema = `C:\etc\browsernerd\browser.mg`
	} else {
		absLog = "/var/log/browsernerd.log"
		absSession = "/tmp/sessions.json"
		absSchema = "/etc/browsernerd/browser.mg"
	}

	cfg := Config{
		Server:  ServerConfig{LogFile: absLog},
		Browser: BrowserConfig{SessionStore: absSession},
		Mangle:  MangleConfig{SchemaPath: absSchema},
	}

	resolved := resolveWorkspacePaths(cfg, wsDir)

	if resolved.Server.LogFile != absLog {
		t.Errorf("expected absolute log file untouched %q, got %q", absLog, resolved.Server.LogFile)
	}
	if resolved.Browser.SessionStore != absSession {
		t.Errorf("expected absolute session store untouched %q, got %q", absSession, resolved.Browser.SessionStore)
	}
	if resolved.Mangle.SchemaPath != absSchema {
		t.Errorf("expected absolute schema path untouched %q, got %q", absSchema, resolved.Mangle.SchemaPath)
	}
}

func TestInitWorkspace_Creates(t *testing.T) {
	tmpDir := t.TempDir()

	if err := InitWorkspace(tmpDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify directory structure
	wsDir := filepath.Join(tmpDir, WorkspaceDirName)
	checkDir := func(path string) {
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("expected directory %q to exist: %v", path, err)
			return
		}
		if !info.IsDir() {
			t.Errorf("expected %q to be a directory", path)
		}
	}
	checkDir(wsDir)
	checkDir(filepath.Join(wsDir, "schemas"))
	checkDir(filepath.Join(wsDir, "data"))

	// Verify config template
	configPath := filepath.Join(wsDir, WorkspaceConfigFile)
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config template: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty config template")
	}

	// Verify .gitignore
	gitignorePath := filepath.Join(wsDir, ".gitignore")
	data, err = os.ReadFile(gitignorePath)
	if err != nil {
		t.Fatalf("failed to read .gitignore: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty .gitignore")
	}
}

func TestInitWorkspace_AlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Create workspace first
	if err := InitWorkspace(tmpDir); err != nil {
		t.Fatalf("first init failed: %v", err)
	}

	// Second init should fail
	err := InitWorkspace(tmpDir)
	if err == nil {
		t.Error("expected error when workspace already exists")
	}
}
