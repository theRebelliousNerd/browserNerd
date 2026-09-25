package config

import (
	"os"
	"path/filepath"
	"testing"
)

// A workspace's .browsernerd/schemas/ holds project modules that load on top
// of the built-in schema; it is picked up only when it actually has modules.
func TestLoadWithWorkspaceUsesProjectSchemaModules(t *testing.T) {
	root := t.TempDir()
	schemaDir := filepath.Join(root, WorkspaceDirName, "schemas")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, WorkspaceDirName, WorkspaceConfigFile), []byte(wsConfigAutoStartOff), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := LoadWithWorkspace("", WorkspaceOptions{ExplicitDir: root})
	if err != nil {
		t.Fatalf("LoadWithWorkspace: %v", err)
	}
	if cfg.Mangle.SchemaPath != "" {
		t.Fatalf("an empty schemas dir adds no project modules, got schema_path %q", cfg.Mangle.SchemaPath)
	}

	if err := os.WriteFile(filepath.Join(schemaDir, "90_project.mg"), []byte("Decl project_fact(SessionId).\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err = LoadWithWorkspace("", WorkspaceOptions{ExplicitDir: root})
	if err != nil {
		t.Fatalf("LoadWithWorkspace: %v", err)
	}
	if cfg.Mangle.SchemaPath != schemaDir {
		t.Fatalf("expected project modules from %q, got %q", schemaDir, cfg.Mangle.SchemaPath)
	}
	if cfg.Mangle.DisableBuiltin {
		t.Fatal("project modules add to the built-in schema; they must not disable it")
	}
}
