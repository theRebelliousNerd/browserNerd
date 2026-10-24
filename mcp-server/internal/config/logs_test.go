package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLogConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNativeLogDefaults(t *testing.T) {
	logs := DefaultConfig().Logs
	if logs.GetLogWindow() != 30*time.Second || logs.MaxReadBytes != 1048576 || logs.MaxEntries != 2000 || len(logs.Files) != 0 {
		t.Fatalf("unexpected native log defaults: %+v", logs)
	}
	if (LogsConfig{}).GetLogWindow() != 30*time.Second {
		t.Fatal("unset window must use default")
	}
}

func TestNativeLogNameCannotCollideWithEnabledDockerSource(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Docker.Containers = []string{"gateway"}
	cfg.Logs.Files = []LogFileConfig{{Name: "gateway", Path: "logs/gateway.log"}}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled Docker cannot reserve a source name: %v", err)
	}
	cfg.Docker.Enabled = true
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "duplicates an enabled docker container source") {
		t.Fatalf("cross-transport name collision was accepted: %v", err)
	}
	cfg.Logs.Files[0].Name = "local-gateway"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("distinct source identities rejected: %v", err)
	}
}

func TestNativeLogConfigValidation(t *testing.T) {
	tests := []struct{ name, body, contains string }{
		{"bad window", "log_window: soon", "logs.log_window"},
		{"zero window", "log_window: 0s", "logs.log_window"},
		{"negative window", "log_window: -1s", "logs.log_window"},
		{"large window", "log_window: 1h1s", "logs.log_window"},
		{"blank window", "log_window: ''", "logs.log_window"},
		{"zero bytes", "max_read_bytes: 0", "logs.max_read_bytes"},
		{"negative bytes", "max_read_bytes: -1", "logs.max_read_bytes"},
		{"large bytes", "max_read_bytes: 67108865", "logs.max_read_bytes"},
		{"zero entries", "max_entries: 0", "logs.max_entries"},
		{"negative entries", "max_entries: -1", "logs.max_entries"},
		{"large entries", "max_entries: 100001", "logs.max_entries"},
		{"all zero", "log_window: ''\n  max_read_bytes: 0\n  max_entries: 0", "logs.log_window"},
		{"null limit", "max_entries: null", "must not be null"},
		{"bad limit type", "max_read_bytes: [1, 2]", "cannot unmarshal"},
		{"bad files type", "files: bad", "cannot unmarshal"},
		{"unknown option", "command: ['tail', '-f', 'server.log']", "unknown logs option"},
		{"unknown file option", "files: [{name: gateway, path: logs/gateway.log, command: tail}]", "unknown logs.files option"},
		{"yaml authority", "files: [{name: gateway, path: logs/gateway.log, allowed_root: /}]", "unknown logs.files option"},
		{"missing name", "files: [{path: logs/gateway.log}]", "nonblank name and path"},
		{"blank name", "files: [{name: ' ', path: logs/gateway.log}]", "nonblank name and path"},
		{"blank path", "files: [{name: gateway, path: ' '}]", "nonblank name and path"},
		{"duplicate names", "files: [{name: gateway, path: a.log}, {name: ' gateway ', path: b.log}]", "duplicate name"},
		{"duplicate paths", "files: [{name: gateway, path: logs/a.log}, {name: worker, path: logs/../logs/a.log}]", "duplicate path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "profile.yaml")
			writeLogConfig(t, path, "logs:\n  "+tt.body+"\n")
			_, err := Load(path)
			if err == nil || !strings.Contains(err.Error(), tt.contains) {
				t.Fatalf("wanted error containing %q, got %v", tt.contains, err)
			}
		})
	}
}

func TestNativeLogLimitsAcceptBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile.yaml")
	writeLogConfig(t, path, "logs:\n  log_window: 1h\n  max_read_bytes: 67108864\n  max_entries: 100000\n")
	cfg, err := Load(path)
	if err != nil || cfg.Logs.GetLogWindow() != time.Hour {
		t.Fatalf("maximum valid limits rejected: %+v, %v", cfg.Logs, err)
	}
}

func TestNativeLogWorkspacePathsAndCreation(t *testing.T) {
	root := t.TempDir()
	writeLogConfig(t, filepath.Join(root, WorkspaceDirName, WorkspaceConfigFile), "logs:\n  files: [{name: gateway, path: logs/not-created-yet.log}]\n")
	cfg, _, err := LoadWithWorkspace("", WorkspaceOptions{ExplicitDir: root})
	if err != nil {
		t.Fatal(err)
	}
	file := cfg.Logs.Files[0]
	if file.Path != filepath.Join(root, "logs", "not-created-yet.log") || file.AllowedRoot != root {
		t.Fatalf("wrong untrusted log scope: %+v", file)
	}
}

func TestNativeLogRelativeWorkspaceDirectory(t *testing.T) {
	root := t.TempDir()
	writeLogConfig(t, filepath.Join(root, WorkspaceDirName, WorkspaceConfigFile), "logs:\n  files: [{name: gateway, path: logs/gateway.log}]\n")
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(original) })
	cfg, _, err := LoadWithWorkspace("", WorkspaceOptions{ExplicitDir: "."})
	if err != nil || cfg.Logs.Files[0].Path != filepath.Join(root, "logs", "gateway.log") {
		t.Fatalf("relative workspace did not normalize file paths: %+v, %v", cfg.Logs, err)
	}
}

func TestNativeLogWorkspaceRefusesEscapes(t *testing.T) {
	for _, kind := range []string{"relative", "absolute", "symlink", "missing symlink target", "symlink loop"} {
		t.Run(kind, func(t *testing.T) {
			root, outside := t.TempDir(), t.TempDir()
			var path string
			switch kind {
			case "relative":
				path = "../outside.log"
			case "absolute":
				path = filepath.Join(outside, "gateway.log")
			case "symlink", "missing symlink target":
				target := outside
				if kind == "missing symlink target" {
					target = filepath.Join(outside, "not-created")
				}
				if err := os.Symlink(target, filepath.Join(root, "logs")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				path = "logs/gateway.log"
			case "symlink loop":
				if err := os.Symlink("logs", filepath.Join(root, "logs")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				path = "logs/gateway.log"
			}
			writeLogConfig(t, filepath.Join(root, WorkspaceDirName, WorkspaceConfigFile), "logs:\n  files: [{name: gateway, path: '"+path+"'}]\n")
			_, _, err := LoadWithWorkspace("", WorkspaceOptions{ExplicitDir: root})
			if err == nil || !strings.Contains(err.Error(), "must remain inside the workspace") {
				t.Fatalf("escape was accepted: %v", err)
			}
		})
	}
}

func TestNativeLogWorkspaceAllowsInternalSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("data", filepath.Join(root, "logs")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	writeLogConfig(t, filepath.Join(root, WorkspaceDirName, WorkspaceConfigFile), "logs:\n  files: [{name: gateway, path: logs/future/gateway.log}]\n")
	if _, _, err := LoadWithWorkspace("", WorkspaceOptions{ExplicitDir: root}); err != nil {
		t.Fatalf("internal symlink refused: %v", err)
	}
}

func TestNativeLogConfigLayerPrecedence(t *testing.T) {
	root, machineDir := t.TempDir(), t.TempDir()
	workspacePath := filepath.Join(root, WorkspaceDirName, WorkspaceConfigFile)
	machinePath := filepath.Join(machineDir, "profile.yaml")
	writeLogConfig(t, workspacePath, "logs:\n  files: [{name: gateway, path: logs/gateway.log}]\n  max_entries: 100\n")
	writeLogConfig(t, machinePath, "logs:\n  log_window: 45s\n")
	cfg, _, err := LoadWithWorkspace(machinePath, WorkspaceOptions{ExplicitDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Logs.Files[0].Path != filepath.Join(root, "logs", "gateway.log") || cfg.Logs.Files[0].AllowedRoot != root || cfg.Logs.MaxEntries != 100 || cfg.Logs.GetLogWindow() != 45*time.Second {
		t.Fatalf("partial profile lost workspace values or authority: %+v", cfg.Logs)
	}
	writeLogConfig(t, machinePath, "logs:\n  files: [{name: jobs, path: logs/jobs.log}]\n")
	cfg, _, err = LoadWithWorkspace(machinePath, WorkspaceOptions{ExplicitDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Logs.Files) != 1 || cfg.Logs.Files[0].Name != "jobs" || cfg.Logs.Files[0].Path != filepath.Join(machineDir, "logs", "jobs.log") || cfg.Logs.Files[0].AllowedRoot != "" {
		t.Fatalf("explicit profile files were not normalized independently: %+v", cfg.Logs)
	}
	loaded, err := Load(machinePath)
	if err != nil || loaded.Logs.Files[0].Path != cfg.Logs.Files[0].Path {
		t.Fatalf("standalone profile path resolution differs: %+v, %v", loaded.Logs, err)
	}
}

func TestNativeLogTrustedWorkspaceAllowsExternalFile(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	path := filepath.Join(outside, "gateway.log")
	writeLogConfig(t, filepath.Join(root, WorkspaceDirName, WorkspaceConfigFile), "logs:\n  files: [{name: gateway, path: '"+path+"'}]\n")
	cfg, _, err := LoadWithWorkspace("", WorkspaceOptions{ExplicitDir: root, Trust: true})
	if err != nil || cfg.Logs.Files[0].Path != path || cfg.Logs.Files[0].AllowedRoot != "" {
		t.Fatalf("trusted external file rejected: %+v, %v", cfg.Logs, err)
	}
}
